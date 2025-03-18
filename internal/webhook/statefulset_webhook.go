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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	autoscalingv1 "github.com/joaomo/k8s_op_vpa/api/v1"
	"github.com/joaomo/k8s_op_vpa/internal/metrics"
)

// StatefulSetWebhookHandler handles admission requests for StatefulSets
type StatefulSetWebhookHandler struct {
	Client  client.Client
	Scheme  *runtime.Scheme
	Metrics *metrics.Metrics
	decoder *admission.Decoder
}

// Handle implements the admission.Handler interface
func (h *StatefulSetWebhookHandler) Handle(ctx context.Context, req admission.Request) admission.Response {
	start := time.Now()
	log := ctrl.LoggerFrom(ctx).WithValues("webhook", "statefulset", "operation", req.Operation)

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
	}

	return admission.Allowed("statefulset processed")
}

// handleCreate handles statefulset creation
func (h *StatefulSetWebhookHandler) handleCreate(ctx context.Context, req admission.Request) error {
	sts := &appsv1.StatefulSet{}
	if err := json.Unmarshal(req.Object.Raw, sts); err != nil {
		return fmt.Errorf("failed to decode statefulset: %w", err)
	}

	vpaManager, err := h.findMatchingVpaManager(ctx, sts)
	if err != nil {
		return err
	}
	if vpaManager == nil {
		return nil
	}

	vpaName := fmt.Sprintf("%s-vpa", sts.Name)
	if err := h.createVPA(ctx, vpaManager, sts, vpaName); err != nil {
		return err
	}

	h.Metrics.RecordVPAOperation("create", vpaManager.Name)
	return nil
}

// handleUpdate handles statefulset updates
func (h *StatefulSetWebhookHandler) handleUpdate(ctx context.Context, req admission.Request) error {
	newSts := &appsv1.StatefulSet{}
	if err := json.Unmarshal(req.Object.Raw, newSts); err != nil {
		return fmt.Errorf("failed to decode new statefulset: %w", err)
	}

	oldSts := &appsv1.StatefulSet{}
	if err := json.Unmarshal(req.OldObject.Raw, oldSts); err != nil {
		return fmt.Errorf("failed to decode old statefulset: %w", err)
	}

	newVpaManager, err := h.findMatchingVpaManager(ctx, newSts)
	if err != nil {
		return err
	}

	oldVpaManager, err := h.findMatchingVpaManager(ctx, oldSts)
	if err != nil {
		return err
	}

	vpaName := fmt.Sprintf("%s-vpa", newSts.Name)

	if oldVpaManager == nil && newVpaManager != nil {
		if err := h.createVPA(ctx, newVpaManager, newSts, vpaName); err != nil {
			return err
		}
		h.Metrics.RecordVPAOperation("create", newVpaManager.Name)
	} else if oldVpaManager != nil && newVpaManager == nil {
		if err := h.deleteVPA(ctx, newSts.Namespace, vpaName); err != nil {
			return err
		}
		h.Metrics.RecordVPAOperation("delete", oldVpaManager.Name)
	} else if newVpaManager != nil {
		if err := h.updateVPA(ctx, newVpaManager, newSts, vpaName); err != nil {
			return err
		}
	}

	return nil
}

// handleDelete handles statefulset deletion
func (h *StatefulSetWebhookHandler) handleDelete(ctx context.Context, req admission.Request) error {
	sts := &appsv1.StatefulSet{}
	if err := json.Unmarshal(req.OldObject.Raw, sts); err != nil {
		return fmt.Errorf("failed to decode statefulset: %w", err)
	}

	vpaManager, err := h.findMatchingVpaManager(ctx, sts)
	if err != nil {
		return err
	}
	if vpaManager == nil {
		return nil
	}

	vpaName := fmt.Sprintf("%s-vpa", sts.Name)
	if err := h.deleteVPA(ctx, sts.Namespace, vpaName); err != nil {
		return err
	}

	h.Metrics.RecordVPAOperation("delete", vpaManager.Name)
	return nil
}

// findMatchingVpaManager finds a VpaManager that matches the statefulset
func (h *StatefulSetWebhookHandler) findMatchingVpaManager(ctx context.Context, sts *appsv1.StatefulSet) (*autoscalingv1.VpaManager, error) {
	vpaManagerList := &autoscalingv1.VpaManagerList{}
	if err := h.Client.List(ctx, vpaManagerList); err != nil {
		return nil, err
	}

	namespace := &corev1.Namespace{}
	if err := h.Client.Get(ctx, types.NamespacedName{Name: sts.Namespace}, namespace); err != nil {
		return nil, err
	}

	for _, vm := range vpaManagerList.Items {
		if !vm.Spec.Enabled {
			continue
		}

		if !matchesLabelSelector(namespace.Labels, vm.Spec.NamespaceSelector) {
			continue
		}

		if !matchesLabelSelector(sts.Labels, vm.Spec.StatefulSetSelector) {
			continue
		}

		return &vm, nil
	}

	return nil, nil
}

// createVPA creates a VPA for a statefulset
func (h *StatefulSetWebhookHandler) createVPA(ctx context.Context, vpaManager *autoscalingv1.VpaManager, sts *appsv1.StatefulSet, vpaName string) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(vpaGVK)
	err := h.Client.Get(ctx, types.NamespacedName{Name: vpaName, Namespace: sts.Namespace}, existing)
	if err == nil {
		return nil
	}
	if !errors.IsNotFound(err) {
		return err
	}

	vpa := h.buildVPA(vpaManager, sts, vpaName)
	return h.Client.Create(ctx, vpa)
}

// updateVPA updates a VPA for a statefulset
func (h *StatefulSetWebhookHandler) updateVPA(ctx context.Context, vpaManager *autoscalingv1.VpaManager, sts *appsv1.StatefulSet, vpaName string) error {
	existing := &unstructured.Unstructured{}
	existing.SetGroupVersionKind(vpaGVK)
	err := h.Client.Get(ctx, types.NamespacedName{Name: vpaName, Namespace: sts.Namespace}, existing)
	if err != nil {
		if errors.IsNotFound(err) {
			return h.createVPA(ctx, vpaManager, sts, vpaName)
		}
		return err
	}

	newVPA := h.buildVPA(vpaManager, sts, vpaName)
	existing.Object["spec"] = newVPA.Object["spec"]
	return h.Client.Update(ctx, existing)
}

// deleteVPA deletes a VPA
func (h *StatefulSetWebhookHandler) deleteVPA(ctx context.Context, namespace, vpaName string) error {
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

// buildVPA creates a VPA unstructured object for a statefulset
func (h *StatefulSetWebhookHandler) buildVPA(vpaManager *autoscalingv1.VpaManager, sts *appsv1.StatefulSet, vpaName string) *unstructured.Unstructured {
	vpa := &unstructured.Unstructured{}
	vpa.SetGroupVersionKind(vpaGVK)
	vpa.SetName(vpaName)
	vpa.SetNamespace(sts.Namespace)

	vpa.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "vpa-operator",
		"app.kubernetes.io/created-by": vpaManager.Name,
	})

	vpa.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion: "apps/v1",
			Kind:       "StatefulSet",
			Name:       sts.Name,
			UID:        sts.UID,
		},
	})

	spec := map[string]interface{}{
		"targetRef": map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "StatefulSet",
			"name":       sts.Name,
		},
		"updatePolicy": map[string]interface{}{
			"updateMode": vpaManager.Spec.UpdateMode,
		},
	}

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
func (h *StatefulSetWebhookHandler) InjectDecoder(d *admission.Decoder) error {
	h.decoder = d
	return nil
}
