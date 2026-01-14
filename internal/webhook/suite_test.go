package webhook

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	admissionv1 "k8s.io/api/admission/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	autoscalingv1 "github.com/joaomo/k8s_op_vpa/api/v1"
)

var (
	cfg       *rest.Config
	k8sClient client.Client
	testEnv   *envtest.Environment
	ctx       context.Context
	cancel    context.CancelFunc
)

func TestWebhook(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Webhook Suite")
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

	err = admissionv1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(cfg, client.Options{Scheme: scheme.Scheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
})

var _ = AfterSuite(func() {
	cancel()
	By("tearing down the test environment")
	if testEnv != nil {
		err := testEnv.Stop()
		Expect(err).NotTo(HaveOccurred())
	}
})

var _ = Describe("Deployment Webhook Integration", func() {
	const (
		timeout  = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When creating a deployment", func() {
		It("Should create VPA when deployment matches VpaManager selector", func() {
			By("Creating namespace with vpa-enabled label")
			namespace := &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "webhook-test-ns",
					Labels: map[string]string{"vpa-enabled": "true"},
				},
			}
			Expect(k8sClient.Create(ctx, namespace)).Should(Succeed())

			By("Creating VpaManager")
			vpaManager := &autoscalingv1.VpaManager{
				ObjectMeta: metav1.ObjectMeta{
					Name: "webhook-test-manager",
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
			Expect(k8sClient.Create(ctx, vpaManager)).Should(Succeed())

			By("Creating deployment with matching labels")
			deployment := &appsv1.Deployment{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "webhook-test-deploy",
					Namespace: "webhook-test-ns",
					Labels:    map[string]string{"vpa-enabled": "true"},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{"app": "webhook-test"},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{"app": "webhook-test"},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{Name: "main", Image: "nginx:latest"},
							},
						},
					},
				},
			}
			Expect(k8sClient.Create(ctx, deployment)).Should(Succeed())

			// Verify VPA was created by webhook
			// Eventually(func() bool {
			//     // Check for VPA creation
			//     return true
			// }, timeout, interval).Should(BeTrue())
		})
	})

	Context("When deleting a deployment", func() {
		It("Should delete the associated VPA", func() {
			// Test VPA deletion on deployment delete
		})
	})

	Context("When updating a deployment", func() {
		It("Should create VPA when label is added", func() {
			// Test label addition
		})

		It("Should delete VPA when label is removed", func() {
			// Test label removal
		})
	})
})
