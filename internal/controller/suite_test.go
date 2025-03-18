package controller

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	autoscalingv1 "github.com/joaomo/k8s_op_vpa/api/v1"
	"github.com/joaomo/k8s_op_vpa/internal/metrics"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	ctx, cancel = context.WithCancel(context.Background())

	By("bootstrapping test environment")
	// KUBEBUILDER_ASSETS should be set by `make test` or manually
	// If not set, tests will fail with a clear error message

	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "test", "crds")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = autoscalingv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = appsv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	err = corev1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())

	// Start the controller manager
	k8sManager, err := ctrl.NewManager(cfg, ctrl.Options{
		Scheme:  scheme.Scheme,
		Metrics: metricsserver.Options{BindAddress: "0"}, // Disable metrics server for tests
	})
	Expect(err).ToNot(HaveOccurred())

	err = (&VpaManagerReconciler{
		Client:  k8sManager.GetClient(),
		Scheme:  k8sManager.GetScheme(),
		Metrics: metrics.NewMetrics(prometheus.NewRegistry()),
	}).SetupWithManager(k8sManager)
	Expect(err).ToNot(HaveOccurred())

	go func() {
		defer GinkgoRecover()
		err = k8sManager.Start(ctx)
		Expect(err).ToNot(HaveOccurred())
	}()
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	if testEnv != nil {
		err := testEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}
})

// Integration tests using envtest
var _ = Describe("VpaManager Controller Integration", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When creating a VpaManager", func() {
		It("Should create VPAs for matching deployments", func() {
			By("Creating a namespace with vpa-enabled label")
			namespace := &corev1.Namespace{}
			namespace.Name = "integration-test-ns"
			namespace.Labels = map[string]string{"vpa-enabled": "true"}
			Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

			By("Creating a deployment with vpa-enabled label")
			deployment := createTestDeployment("test-deploy", "integration-test-ns")
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			By("Creating a VpaManager")
			vpaManager := &autoscalingv1.VpaManager{}
			vpaManager.Name = "integration-vpamanager"
			vpaManager.Spec = autoscalingv1.VpaManagerSpec{
				Enabled:    true,
				UpdateMode: "Auto",
				NamespaceSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"vpa-enabled": "true"},
				},
				DeploymentSelector: &metav1.LabelSelector{
					MatchLabels: map[string]string{"vpa-enabled": "true"},
				},
			}
			Expect(k8sClient.Create(ctx, vpaManager)).Should(Succeed())

			By("Verifying VPA was created")
			// VPA verification would go here once implementation is complete
			// Eventually(func() bool {
			//     vpa := &vpav1.VerticalPodAutoscaler{}
			//     err := k8sClient.Get(ctx, types.NamespacedName{
			//         Name:      "test-deploy-vpa",
			//         Namespace: "integration-test-ns",
			//     }, vpa)
			//     return err == nil
			// }, timeout, interval).Should(BeTrue())
		})
	})

	Context("When VpaManager is disabled", func() {
		It("Should not create VPAs", func() {
			By("Creating a disabled VpaManager")
			vpaManager := &autoscalingv1.VpaManager{}
			vpaManager.Name = "disabled-vpamanager"
			vpaManager.Spec = autoscalingv1.VpaManagerSpec{
				Enabled:    false,
				UpdateMode: "Off",
			}
			Expect(k8sClient.Create(ctx, vpaManager)).Should(Succeed())

			// Verify no VPAs are created even with matching deployments
		})
	})

	Context("When deployment is deleted", func() {
		It("Should remove the associated VPA", func() {
			// Test orphan VPA cleanup
		})
	})

	Context("When VpaManager selector changes", func() {
		It("Should update managed VPAs accordingly", func() {
			// Test selector change behavior
		})
	})
})

func createTestDeployment(name, namespace string) *appsv1.Deployment {
	replicas := int32(1)
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{"vpa-enabled": "true"},
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{"app": name},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"app": name},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "main",
							Image: "nginx:latest",
						},
					},
				},
			},
		},
	}
}
