package controller

import (
	"context"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	autoscalingv1 "github.com/joaomo/k8s_op_vpa/api/v1"
	"github.com/joaomo/k8s_op_vpa/internal/metrics"
)

// Test: Automatically create VPA resources for deployments
func TestReconcile_CreatesVPAForMatchingDeployment(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	// Create a namespace with the matching label
	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Labels: map[string]string{
				"vpa-enabled": "true",
			},
		},
	}

	// Create a deployment with matching labels
	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-ns",
			Labels: map[string]string{
				"vpa-enabled": "true",
			},
			UID: "test-uid-123",
		},
		Spec: appsv1.DeploymentSpec{
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
		},
	}

	// Create VpaManager with selectors
	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-vpamanager",
		},
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
		WithObjects(namespace, deployment, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{
		Client:          fakeClient,
		Scheme:          scheme,
		Metrics:         createTestMetrics(),
		WorkloadConfigs: DefaultWorkloadConfigs(),
	}

	// Reconcile
	result, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})

	require.NoError(t, err)
	assert.True(t, result.RequeueAfter > 0, "should requeue after interval")

	// Verify VPA was created for the deployment
	vpaList := newVPAList()
	err = fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 1, "should create exactly one VPA")

	// Verify VPA references the correct deployment
	vpa := vpaList.Items[0]
	assert.Equal(t, "test-deployment-vpa", vpa.GetName())
	targetRef := vpa.Object["spec"].(map[string]interface{})["targetRef"].(map[string]interface{})
	assert.Equal(t, "Deployment", targetRef["kind"])
	assert.Equal(t, "test-deployment", targetRef["name"])
}

// Test: Filter deployments by namespace labels
func TestReconcile_FiltersDeploymentsByNamespaceSelector(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	// Namespace WITH matching label
	matchingNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "matching-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	// Namespace WITHOUT matching label
	nonMatchingNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "non-matching-ns",
			Labels: map[string]string{"vpa-enabled": "false"},
		},
	}

	// Deployment in matching namespace
	deploymentInMatchingNs := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deployment-matching",
			Namespace: "matching-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "uid-1",
		},
		Spec: createDeploymentSpec(),
	}

	// Deployment in non-matching namespace
	deploymentInNonMatchingNs := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deployment-non-matching",
			Namespace: "non-matching-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "uid-2",
		},
		Spec: createDeploymentSpec(),
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
		WithObjects(matchingNs, nonMatchingNs, deploymentInMatchingNs, deploymentInNonMatchingNs, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})
	require.NoError(t, err)

	// VPA should only be created in matching namespace
	vpaListMatching := newVPAList()
	err = fakeClient.List(ctx, vpaListMatching, client.InNamespace("matching-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaListMatching.Items, 1, "should create VPA in matching namespace")

	vpaListNonMatching := newVPAList()
	err = fakeClient.List(ctx, vpaListNonMatching, client.InNamespace("non-matching-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaListNonMatching.Items, 0, "should NOT create VPA in non-matching namespace")
}

// Test: Filter deployments by deployment labels
func TestReconcile_FiltersDeploymentsByDeploymentSelector(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	// Deployment WITH matching label
	matchingDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matching-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "uid-1",
		},
		Spec: createDeploymentSpec(),
	}

	// Deployment WITHOUT matching label
	nonMatchingDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "non-matching-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "false"},
			UID:       "uid-2",
		},
		Spec: createDeploymentSpec(),
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
		WithObjects(namespace, matchingDeployment, nonMatchingDeployment, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})
	require.NoError(t, err)

	// Verify only one VPA was created (for matching deployment)
	vpaList := newVPAList()
	err = fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 1, "should create VPA only for matching deployment")
	assert.Equal(t, "matching-deployment-vpa", vpaList.Items[0].GetName())
}

// Test: Configure VPA update mode (Off, Initial, Auto)
func TestReconcile_ConfiguresVPAUpdateMode(t *testing.T) {
	testCases := []struct {
		name       string
		updateMode string
	}{
		{"Off mode", "Off"},
		{"Initial mode", "Initial"},
		{"Auto mode", "Auto"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			scheme := setupScheme(t)
			ctx := context.Background()

			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-ns",
					Labels: map[string]string{"vpa-enabled": "true"},
				},
			}

			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-deployment",
					Namespace: "test-ns",
					Labels:    map[string]string{"vpa-enabled": "true"},
					UID:       "uid-1",
				},
				Spec: createDeploymentSpec(),
			}

			vpaManager := &autoscalingv1.VpaManager{
				ObjectMeta: metav1.ObjectMeta{Name: "test-vpamanager"},
				Spec: autoscalingv1.VpaManagerSpec{
					Enabled:    true,
					UpdateMode: tc.updateMode,
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
				WithObjects(namespace, deployment, vpaManager).
				WithStatusSubresource(vpaManager).
				Build()

			reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

			_, err := reconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
			})
			require.NoError(t, err)

			// Verify VPA has correct update mode
			vpaList := newVPAList()
			err = fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
			require.NoError(t, err)
			require.Len(t, vpaList.Items, 1)

			vpa := vpaList.Items[0]
			updatePolicy := vpa.Object["spec"].(map[string]interface{})["updatePolicy"].(map[string]interface{})
			assert.Equal(t, tc.updateMode, updatePolicy["updateMode"])
		})
	}
}

// Test: Set resource policies for containers
func TestReconcile_SetsResourcePoliciesForContainers(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "uid-1",
		},
		Spec: createDeploymentSpec(),
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
			ResourcePolicy: &autoscalingv1.ResourcePolicy{
				ContainerPolicies: []autoscalingv1.ContainerResourcePolicy{
					{
						ContainerName: "*",
						MinAllowed: map[string]string{
							"cpu":    "100m",
							"memory": "100Mi",
						},
						MaxAllowed: map[string]string{
							"cpu":    "1",
							"memory": "1Gi",
						},
					},
				},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, deployment, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})
	require.NoError(t, err)

	// Verify VPA has resource policy
	vpaList := newVPAList()
	err = fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	require.Len(t, vpaList.Items, 1)

	vpa := vpaList.Items[0]
	resourcePolicy := vpa.Object["spec"].(map[string]interface{})["resourcePolicy"].(map[string]interface{})
	containerPolicies := resourcePolicy["containerPolicies"].([]interface{})

	require.Len(t, containerPolicies, 1)
	policy := containerPolicies[0].(map[string]interface{})
	assert.Equal(t, "*", policy["containerName"])

	minAllowed := policy["minAllowed"].(map[string]interface{})
	assert.Equal(t, "100m", minAllowed["cpu"])
	assert.Equal(t, "100Mi", minAllowed["memory"])

	maxAllowed := policy["maxAllowed"].(map[string]interface{})
	assert.Equal(t, "1", maxAllowed["cpu"])
	assert.Equal(t, "1Gi", maxAllowed["memory"])
}

// Test: Disabled VpaManager should not create VPAs
func TestReconcile_DisabledManagerDoesNotCreateVPAs(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "uid-1",
		},
		Spec: createDeploymentSpec(),
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpamanager"},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:    false, // Disabled
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
		WithObjects(namespace, deployment, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})
	require.NoError(t, err)

	// Verify no VPA was created
	vpaList := newVPAList()
	err = fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 0, "should not create VPA when manager is disabled")
}

// Test: VpaManager not found should not error
func TestReconcile_VpaManagerNotFound(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	result, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "non-existent"},
	})

	require.NoError(t, err, "should not error when VpaManager not found")
	assert.False(t, result.Requeue)
}

// Test: Updates status with managed VPAs count
func TestReconcile_UpdatesStatusWithManagedVPAsCount(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	// Create multiple deployments
	deployment1 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deployment-1",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "uid-1",
		},
		Spec: createDeploymentSpec(),
	}

	deployment2 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "deployment-2",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "uid-2",
		},
		Spec: createDeploymentSpec(),
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
		WithObjects(namespace, deployment1, deployment2, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})
	require.NoError(t, err)

	// Verify status was updated
	updatedManager := &autoscalingv1.VpaManager{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-vpamanager"}, updatedManager)
	require.NoError(t, err)

	assert.Equal(t, 2, updatedManager.Status.ManagedVPAs, "should track 2 managed VPAs")
	assert.Equal(t, 2, updatedManager.Status.DeploymentCount, "should track 2 deployments")
	assert.NotNil(t, updatedManager.Status.LastReconcileTime, "should set last reconcile time")
}

// Test: Removes VPA when deployment is deleted
func TestReconcile_RemovesVPAWhenDeploymentDeleted(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	// VpaManager with status showing a managed deployment that no longer exists
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
					Name:      "deleted-deployment",
					Namespace: "test-ns",
					UID:       "deleted-uid",
					VpaName:   "deleted-deployment-vpa",
				},
			},
		},
	}

	// Pre-create the orphaned VPA
	orphanedVPA := createUnstructuredVPA("deleted-deployment-vpa", "test-ns", "deleted-deployment")

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, vpaManager, orphanedVPA).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})
	require.NoError(t, err)

	// Verify orphaned VPA was deleted
	vpaList := newVPAList()
	err = fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 0, "orphaned VPA should be deleted")

	// Verify status was updated
	updatedManager := &autoscalingv1.VpaManager{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-vpamanager"}, updatedManager)
	require.NoError(t, err)
	assert.Equal(t, 0, updatedManager.Status.ManagedVPAs)
	assert.Len(t, updatedManager.Status.ManagedDeployments, 0)
}

// Test: No namespace selector means all namespaces
func TestReconcile_NoNamespaceSelectorMatchesAllNamespaces(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	ns1 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "ns1"},
	}
	ns2 := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: "ns2"},
	}

	deployment1 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dep1",
			Namespace: "ns1",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "uid-1",
		},
		Spec: createDeploymentSpec(),
	}

	deployment2 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dep2",
			Namespace: "ns2",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "uid-2",
		},
		Spec: createDeploymentSpec(),
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpamanager"},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:           true,
			UpdateMode:        "Auto",
			NamespaceSelector: nil, // No selector = all namespaces
			DeploymentSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(ns1, ns2, deployment1, deployment2, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})
	require.NoError(t, err)

	// VPAs should be created in both namespaces
	var totalVPAs int
	for _, nsName := range []string{"ns1", "ns2"} {
		vpaList := newVPAList()
		err = fakeClient.List(ctx, vpaList, client.InNamespace(nsName))
		require.NoError(t, err)
		totalVPAs += len(vpaList.Items)
	}
	assert.Equal(t, 2, totalVPAs, "should create VPAs in all namespaces")
}

// Test: No deployment selector means all deployments
func TestReconcile_NoDeploymentSelectorMatchesAllDeployments(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	deployment1 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dep1",
			Namespace: "test-ns",
			Labels:    map[string]string{"app": "frontend"},
			UID:       "uid-1",
		},
		Spec: createDeploymentSpec(),
	}

	deployment2 := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "dep2",
			Namespace: "test-ns",
			Labels:    map[string]string{"app": "backend"},
			UID:       "uid-2",
		},
		Spec: createDeploymentSpec(),
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpamanager"},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:    true,
			UpdateMode: "Auto",
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
			DeploymentSelector: &metav1.LabelSelector{}, // Empty selector = all deployments
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, deployment1, deployment2, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})
	require.NoError(t, err)

	vpaList := newVPAList()
	err = fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 2, "should create VPAs for all deployments when using empty selector")
}

// Test: Automatically create VPA resources for StatefulSets
func TestReconcile_CreatesVPAForMatchingStatefulSet(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Labels: map[string]string{
				"vpa-enabled": "true",
			},
		},
	}

	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test-ns",
			Labels: map[string]string{
				"vpa-enabled": "true",
			},
			UID: "sts-uid-123",
		},
		Spec: createStatefulSetSpec(),
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-vpamanager",
		},
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
		WithObjects(namespace, statefulset, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{
		Client:          fakeClient,
		Scheme:          scheme,
		Metrics:         createTestMetrics(),
		WorkloadConfigs: DefaultWorkloadConfigs(),
	}

	result, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})

	require.NoError(t, err)
	assert.True(t, result.RequeueAfter > 0, "should requeue after interval")

	vpaList := newVPAList()
	err = fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 1, "should create exactly one VPA")

	vpa := vpaList.Items[0]
	assert.Equal(t, "test-statefulset-vpa", vpa.GetName())
	targetRef := vpa.Object["spec"].(map[string]interface{})["targetRef"].(map[string]interface{})
	assert.Equal(t, "StatefulSet", targetRef["kind"])
	assert.Equal(t, "test-statefulset", targetRef["name"])
}

// Test: Filter StatefulSets by namespace labels
func TestReconcile_FiltersStatefulSetsByNamespaceSelector(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	matchingNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "matching-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	nonMatchingNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "non-matching-ns",
			Labels: map[string]string{"vpa-enabled": "false"},
		},
	}

	stsInMatchingNs := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sts-matching",
			Namespace: "matching-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "uid-1",
		},
		Spec: createStatefulSetSpec(),
	}

	stsInNonMatchingNs := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "sts-non-matching",
			Namespace: "non-matching-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "uid-2",
		},
		Spec: createStatefulSetSpec(),
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
		WithObjects(matchingNs, nonMatchingNs, stsInMatchingNs, stsInNonMatchingNs, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})
	require.NoError(t, err)

	vpaListMatching := newVPAList()
	err = fakeClient.List(ctx, vpaListMatching, client.InNamespace("matching-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaListMatching.Items, 1, "should create VPA in matching namespace")

	vpaListNonMatching := newVPAList()
	err = fakeClient.List(ctx, vpaListNonMatching, client.InNamespace("non-matching-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaListNonMatching.Items, 0, "should NOT create VPA in non-matching namespace")
}

// Test: Filter StatefulSets by StatefulSet labels
func TestReconcile_FiltersStatefulSetsByStatefulSetSelector(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	matchingSts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matching-sts",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "uid-1",
		},
		Spec: createStatefulSetSpec(),
	}

	nonMatchingSts := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "non-matching-sts",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "false"},
			UID:       "uid-2",
		},
		Spec: createStatefulSetSpec(),
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
		WithObjects(namespace, matchingSts, nonMatchingSts, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})
	require.NoError(t, err)

	vpaList := newVPAList()
	err = fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 1, "should create VPA only for matching StatefulSet")
	assert.Equal(t, "matching-sts-vpa", vpaList.Items[0].GetName())
}

// Test: Both Deployments and StatefulSets are processed together
func TestReconcile_ProcessesBothDeploymentsAndStatefulSets(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "dep-uid",
		},
		Spec: createDeploymentSpec(),
	}

	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "sts-uid",
		},
		Spec: createStatefulSetSpec(),
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
			StatefulSetSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, deployment, statefulset, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})
	require.NoError(t, err)

	vpaList := newVPAList()
	err = fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 2, "should create VPAs for both Deployment and StatefulSet")

	// Verify status has both workloads using count fields
	updatedManager := &autoscalingv1.VpaManager{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-vpamanager"}, updatedManager)
	require.NoError(t, err)
	assert.Equal(t, 2, updatedManager.Status.ManagedVPAs)
	assert.Equal(t, 1, updatedManager.Status.DeploymentCount)
	assert.Equal(t, 1, updatedManager.Status.StatefulSetCount)
}

// Test: Automatically create VPA resources for DaemonSets
func TestReconcile_CreatesVPAForMatchingDaemonSet(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-ns",
			Labels: map[string]string{
				"vpa-enabled": "true",
			},
		},
	}

	daemonset := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-daemonset",
			Namespace: "test-ns",
			Labels: map[string]string{
				"vpa-enabled": "true",
			},
			UID: "ds-uid-123",
		},
		Spec: createDaemonSetSpec(),
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-vpamanager",
		},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:    true,
			UpdateMode: "Auto",
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
			DaemonSetSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, daemonset, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{
		Client:          fakeClient,
		Scheme:          scheme,
		Metrics:         createTestMetrics(),
		WorkloadConfigs: DefaultWorkloadConfigs(),
	}

	result, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})

	require.NoError(t, err)
	assert.True(t, result.RequeueAfter > 0, "should requeue after interval")

	vpaList := newVPAList()
	err = fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 1, "should create exactly one VPA")

	vpa := vpaList.Items[0]
	assert.Equal(t, "test-daemonset-vpa", vpa.GetName())
	targetRef := vpa.Object["spec"].(map[string]interface{})["targetRef"].(map[string]interface{})
	assert.Equal(t, "DaemonSet", targetRef["kind"])
	assert.Equal(t, "test-daemonset", targetRef["name"])
}

// Test: Filter DaemonSets by namespace labels
func TestReconcile_FiltersDaemonSetsByNamespaceSelector(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	matchingNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "matching-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	nonMatchingNs := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "non-matching-ns",
			Labels: map[string]string{"vpa-enabled": "false"},
		},
	}

	dsInMatchingNs := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ds-matching",
			Namespace: "matching-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "uid-1",
		},
		Spec: createDaemonSetSpec(),
	}

	dsInNonMatchingNs := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ds-non-matching",
			Namespace: "non-matching-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "uid-2",
		},
		Spec: createDaemonSetSpec(),
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpamanager"},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:    true,
			UpdateMode: "Auto",
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
			DaemonSetSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(matchingNs, nonMatchingNs, dsInMatchingNs, dsInNonMatchingNs, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})
	require.NoError(t, err)

	vpaListMatching := newVPAList()
	err = fakeClient.List(ctx, vpaListMatching, client.InNamespace("matching-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaListMatching.Items, 1, "should create VPA in matching namespace")

	vpaListNonMatching := newVPAList()
	err = fakeClient.List(ctx, vpaListNonMatching, client.InNamespace("non-matching-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaListNonMatching.Items, 0, "should NOT create VPA in non-matching namespace")
}

// Test: Filter DaemonSets by DaemonSet labels
func TestReconcile_FiltersDaemonSetsByDaemonSetSelector(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	matchingDs := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "matching-ds",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "uid-1",
		},
		Spec: createDaemonSetSpec(),
	}

	nonMatchingDs := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "non-matching-ds",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "false"},
			UID:       "uid-2",
		},
		Spec: createDaemonSetSpec(),
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{Name: "test-vpamanager"},
		Spec: autoscalingv1.VpaManagerSpec{
			Enabled:    true,
			UpdateMode: "Auto",
			NamespaceSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
			DaemonSetSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, matchingDs, nonMatchingDs, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})
	require.NoError(t, err)

	vpaList := newVPAList()
	err = fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 1, "should create VPA only for matching DaemonSet")
	assert.Equal(t, "matching-ds-vpa", vpaList.Items[0].GetName())
}

// Test: All workload types (Deployment, StatefulSet, DaemonSet) are processed together
func TestReconcile_ProcessesAllWorkloadTypes(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "dep-uid",
		},
		Spec: createDeploymentSpec(),
	}

	statefulset := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-statefulset",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "sts-uid",
		},
		Spec: createStatefulSetSpec(),
	}

	daemonset := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-daemonset",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "ds-uid",
		},
		Spec: createDaemonSetSpec(),
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
			StatefulSetSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
			DaemonSetSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"vpa-enabled": "true"},
			},
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(namespace, deployment, statefulset, daemonset, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})
	require.NoError(t, err)

	vpaList := newVPAList()
	err = fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	assert.Len(t, vpaList.Items, 3, "should create VPAs for Deployment, StatefulSet, and DaemonSet")

	// Verify status has all workloads using count fields
	updatedManager := &autoscalingv1.VpaManager{}
	err = fakeClient.Get(ctx, types.NamespacedName{Name: "test-vpamanager"}, updatedManager)
	require.NoError(t, err)
	assert.Equal(t, 3, updatedManager.Status.ManagedVPAs)

	// Verify each workload type count
	assert.Equal(t, 1, updatedManager.Status.DeploymentCount)
	assert.Equal(t, 1, updatedManager.Status.StatefulSetCount)
	assert.Equal(t, 1, updatedManager.Status.DaemonSetCount)
}

// Test: VPA is owned by VpaManager for garbage collection
func TestReconcile_VPAHasOwnerReference(t *testing.T) {
	scheme := setupScheme(t)
	ctx := context.Background()

	namespace := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   "test-ns",
			Labels: map[string]string{"vpa-enabled": "true"},
		},
	}

	deployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-deployment",
			Namespace: "test-ns",
			Labels:    map[string]string{"vpa-enabled": "true"},
			UID:       "dep-uid",
		},
		Spec: createDeploymentSpec(),
	}

	vpaManager := &autoscalingv1.VpaManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-vpamanager",
			UID:  "manager-uid",
		},
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
		WithObjects(namespace, deployment, vpaManager).
		WithStatusSubresource(vpaManager).
		Build()

	reconciler := &VpaManagerReconciler{Client: fakeClient, Scheme: scheme, Metrics: createTestMetrics(), WorkloadConfigs: DefaultWorkloadConfigs()}

	_, err := reconciler.Reconcile(ctx, reconcile.Request{
		NamespacedName: types.NamespacedName{Name: "test-vpamanager"},
	})
	require.NoError(t, err)

	vpaList := newVPAList()
	err = fakeClient.List(ctx, vpaList, client.InNamespace("test-ns"))
	require.NoError(t, err)
	require.Len(t, vpaList.Items, 1)

	// Verify owner reference is set to Deployment (for garbage collection)
	ownerRefs := vpaList.Items[0].GetOwnerReferences()
	require.Len(t, ownerRefs, 1, "VPA should have owner reference")
	assert.Equal(t, "Deployment", ownerRefs[0].Kind)
	assert.Equal(t, "test-deployment", ownerRefs[0].Name)
}

// Helper functions

func createTestMetrics() *metrics.Metrics {
	reg := prometheus.NewRegistry()
	return metrics.NewMetrics(reg)
}

func setupScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()
	require.NoError(t, autoscalingv1.AddToScheme(scheme))
	require.NoError(t, corev1.AddToScheme(scheme))
	require.NoError(t, appsv1.AddToScheme(scheme))
	// VPA scheme would be added here
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

func createDaemonSetSpec() appsv1.DaemonSetSpec {
	return appsv1.DaemonSetSpec{
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
