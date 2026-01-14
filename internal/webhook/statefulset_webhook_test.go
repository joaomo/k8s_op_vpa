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
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	autoscalingv1 "github.com/joaomo/k8s_op_vpa/api/v1"
	"github.com/joaomo/k8s_op_vpa/internal/metrics"
)

// Test: Webhook creates VPA for new StatefulSet
func TestStatefulSetWebhook_CreatesVPAOnStatefulSetCreate(t *testing.T) {
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
			StatefulSetSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, vpaManager).
		Build()

	handler := &StatefulSetWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createStatefulSetTestMetrics(),
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "new-statefulset",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "new-uid",
		},
		Spec: createStatefulSetSpec(),
	}

	req := createStatefulSetAdmissionRequest(t, admissionv1.Create, sts, nil)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed, "statefulset should be allowed")

	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 1, "VPA should be created for new statefulset")
	assert.Equal(t, "new-statefulset-vpa", vpaList.Items[0].GetName())

	// Verify VPA targets StatefulSet
	targetRef := vpaList.Items[0].Object["spec"].(map[string]interface{})["targetRef"].(map[string]interface{})
	assert.Equal(t, "StatefulSet", targetRef["kind"])
	assert.Equal(t, "new-statefulset", targetRef["name"])
}

// Test: Webhook does not create VPA for non-matching StatefulSet
func TestStatefulSetWebhook_SkipsNonMatchingStatefulSet(t *testing.T) {
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
			StatefulSetSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, vpaManager).
		Build()

	handler := &StatefulSetWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createStatefulSetTestMetrics(),
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "non-matching-statefulset",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "false"},
			UID:       "non-matching-uid",
		},
		Spec: createStatefulSetSpec(),
	}

	req := createStatefulSetAdmissionRequest(t, admissionv1.Create, sts, nil)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed, "statefulset should be allowed")

	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 0, "VPA should not be created for non-matching statefulset")
}

// Test: Webhook removes VPA when StatefulSet is deleted
func TestStatefulSetWebhook_RemovesVPAOnStatefulSetDelete(t *testing.T) {
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
			StatefulSetSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	existingVPA := createUnstructuredVPAForStatefulSet("existing-statefulset-vpa", "test-ns", "existing-statefulset")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, vpaManager, existingVPA).
		Build()

	handler := &StatefulSetWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createStatefulSetTestMetrics(),
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "existing-statefulset",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "existing-uid",
		},
		Spec: createStatefulSetSpec(),
	}

	req := createStatefulSetAdmissionRequest(t, admissionv1.Delete, nil, sts)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed, "delete should be allowed")

	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 0, "VPA should be deleted when statefulset is deleted")
}

// Test: Webhook handles StatefulSet update - creates VPA when label added
func TestStatefulSetWebhook_CreatesVPAWhenLabelAdded(t *testing.T) {
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
			StatefulSetSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, vpaManager).
		Build()

	handler := &StatefulSetWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createStatefulSetTestMetrics(),
	}

	oldSts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "false"},
			UID:       "test-uid",
		},
		Spec: createStatefulSetSpec(),
	}

	newSts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "test-uid",
		},
		Spec: createStatefulSetSpec(),
	}

	req := createStatefulSetAdmissionRequest(t, admissionv1.Update, newSts, oldSts)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed, "update should be allowed")

	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 1, "VPA should be created when statefulset label is added")
}

// Test: Webhook removes VPA when StatefulSet label is removed
func TestStatefulSetWebhook_RemovesVPAWhenLabelRemoved(t *testing.T) {
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
			StatefulSetSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	existingVPA := createUnstructuredVPAForStatefulSet("test-statefulset-vpa", "test-ns", "test-statefulset")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, vpaManager, existingVPA).
		Build()

	handler := &StatefulSetWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createStatefulSetTestMetrics(),
	}

	oldSts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "test-uid",
		},
		Spec: createStatefulSetSpec(),
	}

	newSts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "false"},
			UID:       "test-uid",
		},
		Spec: createStatefulSetSpec(),
	}

	req := createStatefulSetAdmissionRequest(t, admissionv1.Update, newSts, oldSts)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed, "update should be allowed")

	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 0, "VPA should be deleted when statefulset label is removed")
}

// Test: Webhook allows StatefulSet when no VpaManager exists
func TestStatefulSetWebhook_AllowsStatefulSetWhenNoVpaManager(t *testing.T) {
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

	handler := &StatefulSetWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createStatefulSetTestMetrics(),
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "test-uid",
		},
		Spec: createStatefulSetSpec(),
	}

	req := createStatefulSetAdmissionRequest(t, admissionv1.Create, sts, nil)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed, "statefulset should be allowed even without VpaManager")

	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 0, "no VPA should be created without VpaManager")
}

// Test: Webhook applies resource policy from VpaManager
func TestStatefulSetWebhook_AppliesResourcePolicy(t *testing.T) {
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
			StatefulSetSelector: &metav1.LabelSelector{
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

	handler := &StatefulSetWebhookHandler{
		Client:  fakeClient,
		Scheme:  scheme,
		Metrics: createStatefulSetTestMetrics(),
	}

	sts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "test-uid",
		},
		Spec: createStatefulSetSpec(),
	}

	req := createStatefulSetAdmissionRequest(t, admissionv1.Create, sts, nil)
	resp := handler.Handle(ctx, req)

	assert.True(t, resp.Allowed)

	vpaList := newVPAList()
	err := fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	require.Len(t, vpaList.Items, 1)

	vpa := vpaList.Items[0]
	spec := vpa.Object["spec"].(map[string]interface{})

	updatePolicy := spec["updatePolicy"].(map[string]interface{})
	assert.Equal(t, "Initial", updatePolicy["updateMode"])

	resourcePolicy := spec["resourcePolicy"].(map[string]interface{})
	containerPolicies := resourcePolicy["containerPolicies"].([]interface{})
	require.Len(t, containerPolicies, 1)

	policy := containerPolicies[0].(map[string]interface{})
	minAllowed := policy["minAllowed"].(map[string]interface{})
	assert.Equal(t, "50m", minAllowed["cpu"])
	assert.Equal(t, "64Mi", minAllowed["memory"])
}

// Helper functions

func createStatefulSetSpec() appsv1.StatefulSetSpec {
	return appsv1.StatefulSetSpec{
		ServiceName: "test-service",
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

func createStatefulSetAdmissionRequest(t *testing.T, operation admissionv1.Operation, newObj, oldObj *appsv1.StatefulSet) admission.Request {
	req := admission.Request{
		AdmissionRequest: admissionv1.AdmissionRequest{
			UID:       types.UID("test-request-uid"),
			Operation: operation,
			Resource: metav1.GroupVersionResource{
				Group:    "apps",
				Version:  "v1",
				Resource: "statefulsets",
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

func createStatefulSetTestMetrics() *metrics.Metrics {
	reg := prometheus.NewRegistry()
	return metrics.NewMetrics(reg)
}

func createUnstructuredVPAForStatefulSet(name, namespace, targetStatefulSet string) *unstructured.Unstructured {
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
			"kind":       "StatefulSet",
			"name":       targetStatefulSet,
		},
	}
	return vpa
}
