package webhook

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
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
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	autoscalingv1 "github.com/joaomo/k8s_op_vpa/api/v1"
	"github.com/joaomo/k8s_op_vpa/internal/metrics"
)

var (
	vpaGVK = schema.GroupVersionKind{
		Group:   "autoscaling.k8s.io",
		Version: "v1",
		Kind:    "VerticalPodAutoscaler",
	}
)

// DeploymentWebhookHandler handles admission requests for Deployments
type DeploymentWebhookHandler struct {
	Client  client.Client
	Scheme  *runtime.Scheme
	Metrics *metrics.Metrics
	decoder *admission.Decoder
}

// Handle implements the admission.Handler interface
func (h *DeploymentWebhookHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	start := time.Now()
	log := ctrl.LoggerFrom(ctx).WithValues("webhook", "deployment", "operation", req.Operation)

	var err error
	defer func() {
		h.Metrics.RecordWebhookRequest(string(req.Operation), start, err)
	}()

	switch req.Operation {
	case admissionv1.Create:
		err = h.handleCreate(ctx, req)
	case admissionv1.Update:
		err = h.handleUpdate(ctx, req)
	case admissionv1.Delete:
		err = h.handleDelete(ctx, req)
	}

	if err != nil {
		log.Error(err, "webhook handler error")
		// Still allow the deployment operation, just log the error
	}

	return admission.Allowed("deployment processed")
}

// handleCreate handles deployment creation
func (h *DeploymentWebhookHandler) handleCreate(ctx context.Context, req admission.Request) error {
	deployment := &appsv1.Deployment{}
	if err := json.Unmarshal(req.Object.Raw, deployment); err != nil {
		return fmt.Errorf("failed to decode deployment: %w", err)
	}

	// Find matching VpaManager
	vpaManager, err := h.findMatchingVpaManager(ctx, deployment)
	if err != nil {
		return err
	}
	if vpaManager == nil {
		return nil // No matching VpaManager
	}

	// Create VPA for this deployment
	vpaName := fmt.Sprintf("%s-vpa", deployment.Name)
	if err := h.createVPA(ctx, vpaManager, deployment, vpaName); err != nil {
		return err
	}

	h.Metrics.RecordVPAOperation("create", vpaManager.Name)
	return nil
}

// handleUpdate handles deployment updates
func (h *DeploymentWebhookHandler) handleUpdate(ctx context.Context, req admission.Request) error {
	newDeployment := &appsv1.Deployment{}
	if err := json.Unmarshal(req.Object.Raw, newDeployment); err != nil {
		return fmt.Errorf("failed to decode new deployment: %w", err)
	}

	oldDeployment := &appsv1.Deployment{}
	if err := json.Unmarshal(req.OldObject.Raw, oldDeployment); err != nil {
		return fmt.Errorf("failed to decode old deployment: %w", err)
	}

	// Check if deployment now matches a VpaManager
	newVpaManager, err := h.findMatchingVpaManager(ctx, newDeployment)
	if err != nil {
		return err
	}

	// Check if deployment previously matched
	oldVpaManager, err := h.findMatchingVpaManager(ctx, oldDeployment)
	if err != nil {
		return err
	}

	vpaName := fmt.Sprintf("%s-vpa", newDeployment.Name)

	// Handle state transitions
	if oldVpaManager == nil && newVpaManager != nil {
		// Deployment now matches - create VPA
		if err := h.createVPA(ctx, newVpaManager, newDeployment, vpaName); err != nil {
			return err
		}
		h.Metrics.RecordVPAOperation("create", newVpaManager.Name)
	} else if oldVpaManager != nil && newVpaManager == nil {
		// Deployment no longer matches - delete VPA
		if err := h.deleteVPA(ctx, newDeployment.Namespace, vpaName); err != nil {
			return err
		}
		h.Metrics.RecordVPAOperation("delete", oldVpaManager.Name)
	} else if newVpaManager != nil {
		// Still matches - update VPA if needed
		if err := h.updateVPA(ctx, newVpaManager, newDeployment, vpaName); err != nil {
			return err
		}
	}

	return nil
}

// handleDelete handles deployment deletion
func (h *DeploymentWebhookHandler) handleDelete(ctx context.Context, req admission.Request) error {
	deployment := &appsv1.Deployment{}
	if err := json.Unmarshal(req.OldObject.Raw, deployment); err != nil {
		return fmt.Errorf("failed to decode deployment: %w", err)
	}

	// Only delete VPA if deployment was managed by an enabled VpaManager
	vpaManager, err := h.findMatchingVpaManager(ctx, deployment)
	if err != nil {
		return err
	}
	if vpaManager == nil {
		return nil // No enabled manager, skip deletion
	}

	// Delete the VPA for this deployment
	vpaName := fmt.Sprintf("%s-vpa", deployment.Name)
	if err := h.deleteVPA(ctx, deployment.Namespace, vpaName); err != nil {
		return err
	}

	h.Metrics.RecordVPAOperation("delete", vpaManager.Name)
	return nil
}

// findMatchingVpaManager finds a VpaManager that matches the deployment
func (h *DeploymentWebhookHandler) findMatchingVpaManager(ctx context.Context, deployment *appsv1.Deployment) (*autoscalingv1.VpaManager, error) {
	vpaManagerList := &autoscalingv1.VpaManagerList{}
	if err := h.Client.List(ctx, vpaManagerList); err != nil {
		return nil, err
	}

	// Get the namespace
	namespace := &corev1.Namespace{}
	if err := h.Client.Get(ctx, types.NamespacedName{Name: deployment.Namespace}, namespace); err != nil {
		return nil, err
	}

	for _, vm := range vpaManagerList.Items {
		if !vm.Spec.Enabled {
			continue
		}

		// Check namespace selector
		if !h.matchesSelector(namespace.Labels, vm.Spec.NamespaceSelector) {
			continue
		}

		// Check deployment selector
		if !h.matchesSelector(deployment.Labels, vm.Spec.DeploymentSelector) {
			continue
		}

		return &vm, nil
	}

	return nil, nil
}

// matchesSelector checks if labels match a selector
func (h *DeploymentWebhookHandler) matchesSelector(objLabels map[string]string, selector *metav1.LabelSelector) bool {
	if selector == nil {
		return true
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false
	}

	return labelSelector.Matches(labels.Set(objLabels))
}

// createVPA creates a VPA for a deployment
func (h *DeploymentWebhookHandler) createVPA(ctx context.Context, vpaManager *autoscalingv1.VpaManager, deployment *appsv1.Deployment, vpaName string) error {
	// Check if VPA already exists
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(vpaGVK)
	err := h.Client.Get(ctx, types.NamespacedName{Name: vpaName, Namespace: deployment.Namespace}, existing)
	if err == nil {
		// VPA already exists
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}

	vpa := h.buildVPA(vpaManager, deployment, vpaName)
	return h.Client.Create(ctx, vpa)
}

// updateVPA updates a VPA for a deployment
func (h *DeploymentWebhookHandler) updateVPA(ctx context.Context, vpaManager *autoscalingv1.VpaManager, deployment *appsv1.Deployment, vpaName string) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(vpaGVK)
	err := h.Client.Get(ctx, types.NamespacedName{Name: vpaName, Namespace: deployment.Namespace}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			// VPA doesn't exist, create it
			return h.createVPA(ctx, vpaManager, deployment, vpaName)
		}
		return err
	}

	// Update VPA spec
	newVPA := h.buildVPA(vpaManager, deployment, vpaName)
	existing.Object["spec"] = newVPA.Object["spec"]
	return h.Client.Update(ctx, existing)
}

// deleteVPA deletes a VPA
func (h *DeploymentWebhookHandler) deleteVPA(ctx context.Context, namespace, vpaName string) error {
	vpa := &unstructured.Unstructured{}
	vpa.SetGroupVersionKind(vpaGVK)
	vpa.SetName(vpaName)
	vpa.SetNamespace(namespace)

	err := h.Client.Delete(ctx, vpa)
	if errors.IsNotFound(err) {
		return nil
	}
	return err
}

// buildVPA creates a VPA unstructured object
func (h *DeploymentWebhookHandler) buildVPA(vpaManager *autoscalingv1.VpaManager, deployment *appsv1.Deployment, vpaName string) *unstructured.Unstructured {
	vpa := &unstructured.Unstructured{}
	vpa.SetGroupVersionKind(vpaGVK)
	vpa.SetName(vpaName)
	vpa.SetNamespace(deployment.Namespace)

	// Set labels
	vpa.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "vpa-operator",
		"app.kubernetes.io/created-by": vpaManager.Name,
	})

	// Set owner reference to deployment for garbage collection
	vpa.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
			Name:       deployment.Name,
			UID:        deployment.UID,
		},
	})

	// Build spec
	spec := map[string]interface{}{
		"targetRef": map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"name":       deployment.Name,
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

// InjectDecoder injects the decoder
func (h *DeploymentWebhookHandler) InjectDecoder(d *admission.Decoder) error {
	h.decoder = d
	return nil
}

// matchesLabelSelector checks if labels match a selector (shared helper)
func matchesLabelSelector(objLabels map[string]string, selector *metav1.LabelSelector) bool {
	if selector == nil {
		return false // Require explicit selector for webhooks
	}

	labelSelector, err := metav1.LabelSelectorAsSelector(selector)
	if err != nil {
		return false
	}

	return labelSelector.Matches(labels.Set(objLabels))
}
