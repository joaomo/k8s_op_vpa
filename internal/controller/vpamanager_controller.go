package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	autoscalingv1 "github.com/joaomo/k8s_op_vpa/api/v1"
	"github.com/joaomo/k8s_op_vpa/internal/metrics"
	"github.com/joaomo/k8s_op_vpa/internal/workload"
)

var (
	vpaGVK = schema.GroupVersionKind{
		Group:   "autoscaling.k8s.io",
		Version: "v1",
		Kind:    "VerticalPodAutoscaler",
	}
)

// WorkloadConfig maps a workload kind to its selector in VpaManagerSpec
type WorkloadConfig struct {
	Provider workload.Provider
	Selector func(*autoscalingv1.VpaManagerSpec) *metav1.LabelSelector
}

// VpaManagerReconciler reconciles a VpaManager object
type VpaManagerReconciler struct {
	client.Client
	Scheme          *runtime.Scheme
	Metrics         *metrics.Metrics
	Log             logr.Logger
	WorkloadConfigs []WorkloadConfig
}

// +kubebuilder:rbac:groups=operators.joaomo.io,resources=vpamanagers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=operators.joaomo.io,resources=vpamanagers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=operators.joaomo.io,resources=vpamanagers/finalizers,verbs=update
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=autoscaling.k8s.io,resources=verticalpodautoscalers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch

// Reconcile implements the reconciliation loop for VpaManager
func (r *VpaManagerReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	start := time.Now()
	log := ctrl.LoggerFrom(ctx).WithValues("vpamanager", req.Name)

	// Fetch VpaManager instance
	vpaManager := &autoscalingv1.VpaManager{}
	if err := r.Get(ctx, req.NamespacedName, vpaManager); err != nil {
		if errors.IsNotFound(err) {
			log.Info("VpaManager not found, likely deleted")
			return reconcile.Result{}, nil
		}
		r.Metrics.RecordReconcile(req.Name, start, err)
		return reconcile.Result{}, err
	}

	// If disabled, clean up managed VPAs and return
	if !vpaManager.Spec.Enabled {
		log.Info("VpaManager is disabled, skipping reconciliation")
		r.Metrics.RecordReconcile(vpaManager.Name, start, nil)
		return reconcile.Result{}, nil
	}

	// Get matching namespaces
	matchingNamespaces, err := r.getMatchingNamespaces(ctx, vpaManager.Spec.NamespaceSelector)
	if err != nil {
		log.Error(err, "failed to get matching namespaces")
		r.Metrics.RecordReconcile(vpaManager.Name, start, err)
		return reconcile.Result{}, err
	}

	// Track managed workloads
	managedWorkloads := []autoscalingv1.WorkloadReference{}
	watchedWorkloadsCount := 0

	// For each matching namespace, process all workload types
	for _, ns := range matchingNamespaces {
		for _, wc := range r.WorkloadConfigs {
			selector := wc.Selector(&vpaManager.Spec)
			if selector == nil {
				continue
			}

			workloads, err := wc.Provider.List(ctx, r.Client, ns.Name, selector)
			if err != nil {
				log.Error(err, "failed to list workloads", "kind", wc.Provider.Kind(), "namespace", ns.Name)
				continue
			}

			watchedWorkloadsCount += len(workloads)
			for _, wl := range workloads {
				vpaName := fmt.Sprintf("%s-vpa", wl.GetName())
				created, err := r.ensureVPAForWorkload(ctx, vpaManager, wl.GetKind(), wl.GetName(), wl.GetNamespace(), wl.GetUID(), vpaName)
				if err != nil {
					log.Error(err, "failed to ensure VPA", "kind", wl.GetKind(), "name", wl.GetName(), "namespace", wl.GetNamespace())
					continue
				}
				if created {
					r.Metrics.RecordVPAOperation("create", vpaManager.Name)
				}
				managedWorkloads = append(managedWorkloads, autoscalingv1.WorkloadReference{
					Kind:      wl.GetKind(),
					Name:      wl.GetName(),
					Namespace: wl.GetNamespace(),
					UID:       string(wl.GetUID()),
					VpaName:   vpaName,
				})
			}
		}
	}

	// Clean up orphaned VPAs
	orphansDeleted, err := r.cleanupOrphanedVPAs(ctx, vpaManager, managedWorkloads)
	if err != nil {
		log.Error(err, "failed to cleanup orphaned VPAs")
	}
	for i := 0; i < orphansDeleted; i++ {
		r.Metrics.RecordVPAOperation("delete", vpaManager.Name)
	}

	// Update status using Patch to avoid conflicts with stale resourceVersion
	now := metav1.Now()
	statusUpdate := vpaManager.DeepCopy()
	statusUpdate.Status.ManagedVPAs = len(managedWorkloads)
	statusUpdate.Status.ManagedDeployments = managedWorkloads // backward compatibility
	statusUpdate.Status.ManagedWorkloads = managedWorkloads
	statusUpdate.Status.LastReconcileTime = &now

	if err := r.Status().Patch(ctx, statusUpdate, client.MergeFrom(vpaManager)); err != nil {
		log.Error(err, "failed to patch VpaManager status")
		r.Metrics.RecordReconcile(vpaManager.Name, start, err)
		return reconcile.Result{}, err
	}

	// Update metrics
	r.Metrics.UpdateManagedResources(vpaManager.Name, len(managedWorkloads), watchedWorkloadsCount)
	r.Metrics.RecordReconcile(vpaManager.Name, start, nil)

	log.Info("reconciliation complete", "managedVPAs", len(managedWorkloads), "watchedWorkloads", watchedWorkloadsCount)
	return reconcile.Result{RequeueAfter: 5 * time.Minute}, nil
}

// getMatchingNamespaces returns namespaces that match the selector
func (r *VpaManagerReconciler) getMatchingNamespaces(ctx context.Context, selector *metav1.LabelSelector) ([]corev1.Namespace, error) {
	namespaceList := &corev1.NamespaceList{}

	if selector == nil {
		// No selector means all namespaces
		if err := r.List(ctx, namespaceList); err != nil {
			return nil, err
		}
		return namespaceList.Items, nil
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return nil, err
	}

	if err := r.List(ctx, namespaceList, client.MatchingLabelsSelector{Selector: labelSelector}); err != nil {
		return nil, err
	}

	return namespaceList.Items, nil
}

// ensureVPAForWorkload creates or updates a VPA for a workload (Deployment or StatefulSet)
func (r *VpaManagerReconciler) ensureVPAForWorkload(ctx context.Context, vpaManager *autoscalingv1.VpaManager, kind, name, namespace string, uid types.UID, vpaName string) (bool, error) {
	vpa := r.buildVPAForWorkload(vpaManager, kind, name, namespace, uid, vpaName)

	// Check if VPA already exists
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(vpaGVK)
	err := r.Get(ctx, types.NamespacedName{Name: vpaName, Namespace: namespace}, existing)

	if err != nil {
		if errors.IsNotFound(err) {
			// Create VPA
			if err := r.Create(ctx, vpa); err != nil {
				return false, err
			}
			return true, nil
		}
		return false, err
	}

	// Update existing VPA if needed
	existing.Object["spec"] = vpa.Object["spec"]
	if err := r.Update(ctx, existing); err != nil {
		return false, err
	}

	return false, nil
}

// buildVPAForWorkload creates a VPA unstructured object for any workload type
func (r *VpaManagerReconciler) buildVPAForWorkload(vpaManager *autoscalingv1.VpaManager, kind, name, namespace string, uid types.UID, vpaName string) *unstructured.Unstructured {
	vpa := &unstructured.Unstructured{}
	vpa.SetGroupVersionKind(vpaGVK)
	vpa.SetName(vpaName)
	vpa.SetNamespace(namespace)

	// Set labels
	vpa.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "vpa-operator",
		"app.kubernetes.io/created-by": vpaManager.Name,
	})

	// Set owner reference to workload for garbage collection
	controller := true
	blockOwnerDeletion := true
	vpa.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion:         "apps/v1",
			Kind:               kind,
			Name:               name,
			UID:                uid,
			Controller:         &controller,
			BlockOwnerDeletion: &blockOwnerDeletion,
		},
	})

	// Build spec
	spec := map[string]interface{}{
		"targetRef": map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       kind,
			"name":       name,
		},
		"updatePolicy": map[string]interface{}{
			"updateMode": vpaManager.Spec.UpdateMode,
		},
	}

	// Add resource policy if specified
	if vpaManager.Spec.ResourcePolicy != nil && len(vpaManager.Spec.ResourcePolicy.ContainerPolicies) > 0 {
		containerPolicies := make([]interface{}, 0, len(vpaManager.Spec.ResourcePolicy.ContainerPolicies))
		for _, cp := range vpaManager.Spec.ResourcePolicy.ContainerPolicies {
			policy := map[string]interface{}{
				"containerName": cp.ContainerName,
			}
			if cp.MinAllowed != nil {
				minAllowed := make(map[string]interface{})
				for k, v := range cp.MinAllowed {
					minAllowed[k] = v
				}
				policy["minAllowed"] = minAllowed
			}
			if cp.MaxAllowed != nil {
				maxAllowed := make(map[string]interface{})
				for k, v := range cp.MaxAllowed {
					maxAllowed[k] = v
				}
				policy["maxAllowed"] = maxAllowed
			}
			containerPolicies = append(containerPolicies, policy)
		}
		spec["resourcePolicy"] = map[string]interface{}{
			"containerPolicies": containerPolicies,
		}
	}

	vpa.Object["spec"] = spec
	return vpa
}

// cleanupOrphanedVPAs removes VPAs for workloads that no longer match
func (r *VpaManagerReconciler) cleanupOrphanedVPAs(ctx context.Context, vpaManager *autoscalingv1.VpaManager, currentWorkloads []autoscalingv1.WorkloadReference) (int, error) {
	// Build a set of current VPA names
	currentVPAs := make(map[string]bool)
	for _, wl := range currentWorkloads {
		key := fmt.Sprintf("%s/%s", wl.Namespace, wl.VpaName)
		currentVPAs[key] = true
	}

	// List all VPAs managed by this operator
	vpaList := &unstructured.UnstructuredList{}
	vpaList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "autoscaling.k8s.io",
		Version: "v1",
		Kind:    "VerticalPodAutoscalerList",
	})

	if err := r.List(ctx, vpaList, client.MatchingLabels{
		"app.kubernetes.io/managed-by": "vpa-operator",
		"app.kubernetes.io/created-by": vpaManager.Name,
	}); err != nil {
		return 0, err
	}

	deleted := 0
	for _, vpa := range vpaList.Items {
		key := fmt.Sprintf("%s/%s", vpa.GetNamespace(), vpa.GetName())
		if !currentVPAs[key] {
			if err := r.Delete(ctx, &vpa); err != nil && !errors.IsNotFound(err) {
				return deleted, err
			}
			deleted++
		}
	}

	return deleted, nil
}

// SetupWithManager sets up the controller with the Manager
func (r *VpaManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	r.Log = ctrl.Log.WithName("controllers").WithName("VpaManager")

	// Initialize workload configs if not set
	if len(r.WorkloadConfigs) == 0 {
		r.WorkloadConfigs = DefaultWorkloadConfigs()
	}

	builder := ctrl.NewControllerManagedBy(mgr).
		For(&autoscalingv1.VpaManager{}).
		Watches(
			&corev1.Namespace{},
			handler.EnqueueRequestsFromMapFunc(r.findVpaManagersForNamespace),
		)

	// Add watches for all workload types
	for _, wc := range r.WorkloadConfigs {
		builder = builder.Watches(
			wc.Provider.NewObject(),
			handler.EnqueueRequestsFromMapFunc(r.findVpaManagersForWorkload),
		)
	}

	return builder.Complete(r)
}

// DefaultWorkloadConfigs returns the default workload configurations
func DefaultWorkloadConfigs() []WorkloadConfig {
	return []WorkloadConfig{
		{
			Provider: &workload.DeploymentProvider{},
			Selector: func(spec *autoscalingv1.VpaManagerSpec) *metav1.LabelSelector {
				return spec.DeploymentSelector
			},
		},
		{
			Provider: &workload.StatefulSetProvider{},
			Selector: func(spec *autoscalingv1.VpaManagerSpec) *metav1.LabelSelector {
				return spec.StatefulSetSelector
			},
		},
		{
			Provider: &workload.DaemonSetProvider{},
			Selector: func(spec *autoscalingv1.VpaManagerSpec) *metav1.LabelSelector {
				return spec.DaemonSetSelector
			},
		},
	}
}

// findVpaManagersForWorkload returns reconcile requests for VpaManagers that might manage this workload
func (r *VpaManagerReconciler) findVpaManagersForWorkload(ctx context.Context, obj client.Object) []reconcile.Request {
	vpaManagerList := &autoscalingv1.VpaManagerList{}
	if err := r.List(ctx, vpaManagerList); err != nil {
		return nil
	}

	requests := []reconcile.Request{}
	for _, vm := range vpaManagerList.Items {
		if vm.Spec.Enabled {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: vm.Name},
			})
		}
	}
	return requests
}

// findVpaManagersForNamespace returns reconcile requests for VpaManagers when namespace changes
func (r *VpaManagerReconciler) findVpaManagersForNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	vpaManagerList := &autoscalingv1.VpaManagerList{}
	if err := r.List(ctx, vpaManagerList); err != nil {
		return nil
	}

	ns := obj.(*corev1.Namespace)
	requests := []reconcile.Request{}

	for _, vm := range vpaManagerList.Items {
		if vm.Spec.Enabled && r.namespaceMatchesSelector(ns, vm.Spec.NamespaceSelector) {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: vm.Name},
			})
		}
	}
	return requests
}

// namespaceMatchesSelector checks if a namespace matches a label selector
func (r *VpaManagerReconciler) namespaceMatchesSelector(ns *corev1.Namespace, selector *metav1.LabelSelector) bool {
	if selector == nil {
		return true
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false
	}

	return labelSelector.Matches(labels.Set(ns.Labels))
}
