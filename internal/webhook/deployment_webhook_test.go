package webhook

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	autoscalingv1 "github.com/joaomo/k8s_op_vpa/api/v1"
	"github.com/joaomo/k8s_op_vpa/internal/metrics"
)

// Test: Webhook creates VPA for new deployment
func TestDeploymentWebhook_CreatesVPAOnDeploymentCreate(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpamanager"},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:    true,
			UpdateMode: "Auto",
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
			DeploymentSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, vpaManager).
		Build()

	handler := &DeploymentWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createTestMetrics(),
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "new-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "new-uid",
		},
		Spec: createDeploymentSpec(),
	}

	req := createAdmissionRequest(t, admissionv1.Create, deployment, nil)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed, "deployment should be allowed")

	// Verify VPA was created
	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 1, "VPA should be created for new deployment")
	assert.Equal(t, "new-deployment-vpa", vpaList.Items[0].GetName())
}

// Test: Webhook does not create VPA for non-matching deployment
func TestDeploymentWebhook_SkipsNonMatchingDeployment(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpamanager"},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:    true,
			UpdateMode: "Auto",
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
			DeploymentSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, vpaManager).
		Build()

	handler := &DeploymentWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createTestMetrics(),
	}

	// Deployment WITHOUT matching label
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "non-matching-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "false"},
			UID:       "non-matching-uid",
		},
		Spec: createDeploymentSpec(),
	}

	req := createAdmissionRequest(t, admissionv1.Create, deployment, nil)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed, "deployment should be allowed")

	// Verify no VPA was created
	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 0, "VPA should not be created for non-matching deployment")
}

// Test: Webhook removes VPA when deployment is deleted
func TestDeploymentWebhook_RemovesVPAOnDeploymentDelete(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpamanager"},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:    true,
			UpdateMode: "Auto",
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
			DeploymentSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
		Status: autoscalingv1.VpaManagerStatus{
			ManagedVPAs: 1,
			ManagedDeployments: []autoscalingv1.DeploymentReference{
				{
					Name:      "existing-deployment",
					Namespace: "test-ns",
					UID:       "existing-uid",
					VpaName:   "existing-deployment-vpa",
				},
			},
		},
	}

	// Pre-create the VPA that should be deleted
	existingVPA := createUnstructuredVPA("existing-deployment-vpa", "test-ns", "existing-deployment")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, vpaManager, existingVPA).
		Build()

	handler := &DeploymentWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createTestMetrics(),
	}

	// Deployment being deleted
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "existing-uid",
		},
		Spec: createDeploymentSpec(),
	}

	req := createAdmissionRequest(t, admissionv1.Delete, nil, deployment)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed, "delete should be allowed")

	// Verify VPA was deleted
	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 0, "VPA should be deleted when deployment is deleted")
}

// Test: Webhook skips delete when VpaManager is disabled
func TestDeploymentWebhook_SkipsDeleteWhenDisabled(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpamanager"},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:    false, // Disabled
			UpdateMode: "Auto",
		},
	}

	existingVPA := createUnstructuredVPA("existing-deployment-vpa", "test-ns", "existing-deployment")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, vpaManager, existingVPA).
		Build()

	handler := &DeploymentWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createTestMetrics(),
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "existing-uid",
		},
		Spec: createDeploymentSpec(),
	}

	req := createAdmissionRequest(t, admissionv1.Delete, nil, deployment)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed, "delete should be allowed")

	// Verify VPA was NOT deleted (manager is disabled)
	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 1, "VPA should not be deleted when manager is disabled")
}

// Test: Webhook handles update by re-evaluating selector match
func TestDeploymentWebhook_HandlesDeploymentUpdate(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpamanager"},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:    true,
			UpdateMode: "Auto",
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
			DeploymentSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, vpaManager).
		Build()

	handler := &DeploymentWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createTestMetrics(),
	}

	// Old deployment without matching label
	oldDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "false"},
			UID:       "test-uid",
		},
		Spec: createDeploymentSpec(),
	}

	// New deployment WITH matching label (label was added)
	newDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "test-uid",
		},
		Spec: createDeploymentSpec(),
	}

	req := createAdmissionRequest(t, admissionv1.Update, newDeployment, oldDeployment)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed, "update should be allowed")

	// Verify VPA was created because deployment now matches
	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 1, "VPA should be created when deployment label is added")
}

// Test: Webhook removes VPA when deployment label is removed
func TestDeploymentWebhook_RemovesVPAWhenLabelRemoved(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpamanager"},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:    true,
			UpdateMode: "Auto",
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
			DeploymentSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	existingVPA := createUnstructuredVPA("test-deployment-vpa", "test-ns", "test-deployment")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, vpaManager, existingVPA).
		Build()

	handler := &DeploymentWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createTestMetrics(),
	}

	// Old deployment WITH matching label
	oldDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "test-uid",
		},
		Spec: createDeploymentSpec(),
	}

	// New deployment WITHOUT matching label (label was removed)
	newDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "false"},
			UID:       "test-uid",
		},
		Spec: createDeploymentSpec(),
	}

	req := createAdmissionRequest(t, admissionv1.Update, newDeployment, oldDeployment)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed, "update should be allowed")

	// Verify VPA was deleted because deployment no longer matches
	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 0, "VPA should be deleted when deployment label is removed")
}

// Test: Webhook does not fail if no VpaManager exists
func TestDeploymentWebhook_AllowsDeploymentWhenNoVpaManager(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace).
		Build()

	handler := &DeploymentWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createTestMetrics(),
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "test-uid",
		},
		Spec: createDeploymentSpec(),
	}

	req := createAdmissionRequest(t, admissionv1.Create, deployment, nil)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed, "deployment should be allowed even without VpaManager")

	// Verify no VPA was created
	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 0, "no VPA should be created without VpaManager")
}

// Test: Webhook correctly applies resource policy from VpaManager
func TestDeploymentWebhook_AppliesResourcePolicy(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpamanager"},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:    true,
			UpdateMode: "Initial",
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
			DeploymentSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
			ResourcePolicy: &autoscalingv1.ResourcePolicy{
				ContainerPolicies: []autoscalingv1.ContainerResourcePolicy{
					{
						ContainerName: "*",
						MinAllowed: map[string]string{
							"cpu":    "50m",
							"memory": "64Mi",
						},
						MaxAllowed: map[string]string{
							"cpu":    "2",
							"memory": "2Gi",
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, vpaManager).
		Build()

	handler := &DeploymentWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createTestMetrics(),
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "test-uid",
		},
		Spec: createDeploymentSpec(),
	}

	req := createAdmissionRequest(t, admissionv1.Create, deployment, nil)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed)

	// Verify VPA was created with correct resource policy
	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	require.Len(t, vpaList.Items, 1)

	vpa := vpaList.Items[0]
	spec := vpa.Object["spec"].(map[string]interface{})

	// Verify update mode
	updatePolicy := spec["updatePolicy"].(map[string]interface{})
	assert.Equal(t, "Initial", updatePolicy["updateMode"])

	// Verify resource policy
	resourcePolicy := spec["resourcePolicy"].(map[string]interface{})
	containerPolicies := resourcePolicy["containerPolicies"].([]interface{})
	require.Len(t, containerPolicies, 1)

	policy := containerPolicies[0].(map[string]interface{})
	minAllowed := policy["minAllowed"].(map[string]interface{})
	assert.Equal(t, "50m", minAllowed["cpu"])
	assert.Equal(t, "64Mi", minAllowed["memory"])
}

// Test: Webhook handles multiple VpaManagers (uses first enabled matching one)
func TestDeploymentWebhook_HandlesMultipleVpaManagers(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true", "tier": "production"},
		},
	}

	// First VpaManager - matches but is disabled
	vpaManager1 := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{Name: "disabled-manager"},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:    false,
			UpdateMode: "Off",
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	// Second VpaManager - matches and is enabled
	vpaManager2 := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{Name: "enabled-manager"},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:    true,
			UpdateMode: "Auto",
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"tier": "production"},
			},
			DeploymentSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, vpaManager1, vpaManager2).
		Build()

	handler := &DeploymentWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createTestMetrics(),
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "test-uid",
		},
		Spec: createDeploymentSpec(),
	}

	req := createAdmissionRequest(t, admissionv1.Create, deployment, nil)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed)

	// VPA should be created using the enabled manager
	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 1)

	vpa := vpaList.Items[0]
	updatePolicy := vpa.Object["spec"].(map[string]interface{})["updatePolicy"].(map[string]interface{})
	assert.Equal(t, "Auto", updatePolicy["updateMode"], "should use enabled manager's updateMode")
}

// Test: Webhook is idempotent - doesn't duplicate VPA on retry
func TestDeploymentWebhook_IsIdempotent(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpamanager"},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:    true,
			UpdateMode: "Auto",
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
			DeploymentSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	// VPA already exists
	existingVPA := createUnstructuredVPA("test-deployment-vpa", "test-ns", "test-deployment")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, vpaManager, existingVPA).
		Build()

	handler := &DeploymentWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createTestMetrics(),
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "test-uid",
		},
		Spec: createDeploymentSpec(),
	}

	// Simulate retry - create request when VPA already exists
	req := createAdmissionRequest(t, admissionv1.Create, deployment, nil)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed, "should allow deployment")

	// Should still have exactly one VPA
	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 1, "should not create duplicate VPA")
}

// Helper functions

func setupScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	require.NoError(t, autoscalingv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	require.NoError(t, admissionv1.AddToScheme(scheme))
	return scheme
}

func createDeploymentSpec() appsv1.DeploymentSpec {
	return appsv1.DeploymentSpec{
		Selector: &metav1.LabelSelector{
			MatchLabels: map[string]string{"app": "test"},
		},
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{"app": "test"},
			},
			Spec: corev1.PodSpec{
				Containers: []corev1.Container{
					{Name: "main", Image: "nginx:latest"},
				},
			},
		},
	}
}

func createAdmissionRequest(t *testing.T, operation admissionv1.Operation, newObj, oldObj *appsv1.Deployment) admission.Request {
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       types.UID("test-request-uid"),
			Operation: operation,
			Resource: metav1.GroupVersionResource{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
			},
		},
	}

	if newObj != nil {
		raw, err := json.Marshal(newObj)
		require.NoError(t, err)
		req.Object.Raw = raw
		req.Namespace = newObj.Namespace
		req.Name = newObj.Name
	}

	if oldObj != nil {
		raw, err := json.Marshal(oldObj)
		require.NoError(t, err)
		req.OldObject.Raw = raw
		if req.Namespace == "" {
			req.Namespace = oldObj.Namespace
		}
		if req.Name == "" {
			req.Name = oldObj.Name
		}
	}

	return req
}

// Helper to create test metrics
func createTestMetrics() *metrics.Metrics {
	reg := prometheus.NewRegistry()
	return metrics.NewMetrics(reg)
}

func newVPAList() *unstructured.UnstructuredList {
	list := &unstructured.UnstructuredList{}
	list.SetAPIVersion("autoscaling.k8s.io/v1")
	list.SetKind("VerticalPodAutoscalerList")
	return list
}

func createUnstructuredVPA(name, namespace, targetDeployment string) *unstructured.Unstructured {
	vpa := &unstructured.Unstructured{}
	vpa.SetAPIVersion("autoscaling.k8s.io/v1")
	vpa.SetKind("VerticalPodAutoscaler")
	vpa.SetName(name)
	vpa.SetNamespace(namespace)
	vpa.SetLabels(map[string]string{
		"app.kubernetes.io/managed-by": "vpa-operator",
		"app.kubernetes.io/created-by": "test-vpamanager",
	})
	vpa.Object["spec"] = map[string]interface{}{
		"targetRef": map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"name":       targetDeployment,
		},
	}
	return vpa
}
