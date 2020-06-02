package integration

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"github.com/open-cluster-management/nucleus/pkg/operators"
	"github.com/open-cluster-management/nucleus/test/integration/util"

	operatorapiv1 "github.com/open-cluster-management/api/operator/v1"
)

func startHubOperator(ctx context.Context) {
	err := operators.RunClusterManagerOperator(ctx, &controllercmd.ControllerContext{
		KubeConfig:    restConfig,
		EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

var _ = ginkgo.Describe("HubCore", func() {
	var cancel context.CancelFunc
	var err error
	var clusterManagerName string
	var hubRegistrationClusterRole string
	var hubWebhookClusterRole string
	var hubRegistrationSA string
	var hubWebhookSA string
	var hubRegistrationDeployment string
	var hubWebhookDeployment string
	var webhookSecret string
	var validtingWebhook string

	ginkgo.BeforeEach(func() {
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go startHubOperator(ctx)
	})

	ginkgo.JustBeforeEach(func() {
		clusterManager := &operatorapiv1.ClusterManager{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterManagerName,
			},
			Spec: operatorapiv1.ClusterManagerSpec{
				RegistrationImagePullSpec: "quay.io/open-cluster-management/registration",
			},
		}
		_, err = operatorClient.OperatorV1().ClusterManagers().Create(context.Background(), clusterManager, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.AfterEach(func() {
		if cancel != nil {
			cancel()
		}
	})

	ginkgo.Context("Deploy and clean hub component", func() {
		ginkgo.BeforeEach(func() {
			clusterManagerName = "hub"
			hubRegistrationClusterRole = fmt.Sprintf("system:open-cluster-management:%s-registration-controller", clusterManagerName)
			hubWebhookClusterRole = fmt.Sprintf("system:open-cluster-management:%s-registration-webhook", clusterManagerName)
			hubRegistrationSA = fmt.Sprintf("%s-registration-controller-sa", clusterManagerName)
			hubWebhookSA = fmt.Sprintf("%s-registration-webhook-sa", clusterManagerName)
			hubRegistrationDeployment = fmt.Sprintf("%s-registration-controller", clusterManagerName)
			hubWebhookDeployment = fmt.Sprintf("%s-registration-webhook", clusterManagerName)
			webhookSecret = "webhook-serving-cert"
			validtingWebhook = "spokeclustervalidators.admission.cluster.open-cluster-management.io"
		})

		ginkgo.It("should have expected resource created successfully", func() {
			// Check namespace
			gomega.Eventually(func() bool {
				if _, err := kubeClient.CoreV1().Namespaces().Get(context.Background(), hubNamespace, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check clusterrole/clusterrolebinding
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(), hubRegistrationClusterRole, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(), hubWebhookClusterRole, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(), hubRegistrationClusterRole, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(), hubWebhookClusterRole, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check service account
			gomega.Eventually(func() bool {
				if _, err := kubeClient.CoreV1().ServiceAccounts(hubNamespace).Get(context.Background(), hubRegistrationSA, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.CoreV1().ServiceAccounts(hubNamespace).Get(context.Background(), hubWebhookSA, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check deployment
			gomega.Eventually(func() bool {
				if _, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(context.Background(), hubRegistrationDeployment, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(context.Background(), hubWebhookDeployment, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check service
			gomega.Eventually(func() bool {
				if _, err := kubeClient.CoreV1().Services(hubNamespace).Get(context.Background(), hubWebhookDeployment, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check webhook secret
			gomega.Eventually(func() bool {
				s, err := kubeClient.CoreV1().Secrets(hubNamespace).Get(context.Background(), webhookSecret, metav1.GetOptions{})
				if err != nil {
					return false
				}
				if s.Data == nil || s.Data["ca.crt"] == nil || s.Data["tls.crt"] == nil || s.Data["tls.key"] == nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check validating webhook
			gomega.Eventually(func() bool {
				if _, err := kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(context.Background(), validtingWebhook, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			err := operatorClient.OperatorV1().ClusterManagers().Delete(context.Background(), clusterManagerName, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Check namespace deletion
			gomega.Eventually(func() bool {
				if _, err := kubeClient.CoreV1().Namespaces().Get(context.Background(), hubNamespace, metav1.GetOptions{}); errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check clusterrole/clusterrolebinding deletion
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(), hubRegistrationClusterRole, metav1.GetOptions{}); errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(), hubWebhookClusterRole, metav1.GetOptions{}); errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(), hubRegistrationClusterRole, metav1.GetOptions{}); errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(), hubWebhookClusterRole, metav1.GetOptions{}); errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check validating webhook deletion
			gomega.Eventually(func() bool {
				if _, err := kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(context.Background(), validtingWebhook, metav1.GetOptions{}); errors.IsNotFound(err) {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})
	})

})
