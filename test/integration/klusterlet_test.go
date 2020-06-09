package integration

import (
	"context"
	"fmt"
	"strings"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	operatorapiv1 "github.com/open-cluster-management/api/operator/v1"
	"github.com/open-cluster-management/registration-operator/pkg/operators"
	"github.com/open-cluster-management/registration-operator/test/integration/util"
)

func startKlusterletOperator(ctx context.Context) {
	err := operators.RunKlusterletOperator(ctx, &controllercmd.ControllerContext{
		KubeConfig:    restConfig,
		EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

var _ = ginkgo.Describe("Klusterlet", func() {
	var cancel context.CancelFunc
	var klusterlet *operatorapiv1.Klusterlet
	var klusterletNamespace string
	var registrationRoleName string
	var registrationDeploymentName string
	var registrationSAName string
	var workRoleName string
	var workDeploymentName string
	var workSAName string

	ginkgo.BeforeEach(func() {
		var ctx context.Context

		klusterletNamespace = fmt.Sprintf("open-cluster-manager-%s", rand.String(6))
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: klusterletNamespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// Create a dummy bootstrap secret
		bootStrapSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "bootstrap-hub-kubeconfig",
				Namespace: klusterletNamespace,
			},
			Data: map[string][]byte{
				"kubeconfig": []byte("dummy"),
			},
		}
		_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Create(context.Background(), bootStrapSecret, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		klusterlet = &operatorapiv1.Klusterlet{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("klusterlet-%s", rand.String(6)),
			},
			Spec: operatorapiv1.KlusterletSpec{
				RegistrationImagePullSpec: "quay.io/open-cluster-management/registration",
				WorkImagePullSpec:         "quay.io/open-cluster-management/work",
				ExternalServerURLs: []operatorapiv1.ServerURL{
					{
						URL: "https://localhost",
					},
				},
				ClusterName: "testcluster",
				Namespace:   klusterletNamespace,
			},
		}

		ctx, cancel = context.WithCancel(context.Background())
		go startKlusterletOperator(ctx)
	})

	ginkgo.AfterEach(func() {
		err := kubeClient.CoreV1().Namespaces().Delete(context.Background(), klusterletNamespace, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		if cancel != nil {
			cancel()
		}
	})

	ginkgo.Context("Deploy and clean klusterlet component", func() {
		ginkgo.BeforeEach(func() {
			registrationDeploymentName = fmt.Sprintf("%s-registration-agent", klusterlet.Name)
			workDeploymentName = fmt.Sprintf("%s-work-agent", klusterlet.Name)
			registrationRoleName = fmt.Sprintf("system:open-cluster-management:%s", registrationDeploymentName)
			workRoleName = fmt.Sprintf("system:open-cluster-management:%s", workDeploymentName)
			registrationSAName = fmt.Sprintf("%s-registration-sa", klusterlet.Name)
			workSAName = fmt.Sprintf("%s-work-sa", klusterlet.Name)
		})

		ginkgo.It("should have expected resource created successfully", func() {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Check clusterrole/clusterrolebinding
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(), registrationRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(context.Background(), workRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(), registrationRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(), workRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check role/rolebinding
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().Roles(klusterletNamespace).Get(context.Background(), registrationRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.RbacV1().RoleBindings(klusterletNamespace).Get(context.Background(), registrationRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check service account
			gomega.Eventually(func() bool {
				if _, err := kubeClient.CoreV1().ServiceAccounts(klusterletNamespace).Get(context.Background(), registrationSAName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.CoreV1().ServiceAccounts(klusterletNamespace).Get(context.Background(), workSAName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check deployment
			gomega.Eventually(func() bool {
				if _, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), registrationDeploymentName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "Applied", metav1.ConditionTrue, eventuallyTimeout, eventuallyInterval)
		})

		ginkgo.It("should have correct registration deployment when server url is empty", func() {
			klusterlet.Spec.ExternalServerURLs = []operatorapiv1.ServerURL{}
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Eventually(func() bool {
				if _, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), registrationDeploymentName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			deployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), registrationDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(len(deployment.Spec.Template.Spec.Containers)).Should(gomega.Equal(1))
			// external-server-url should not be set
			for _, arg := range deployment.Spec.Template.Spec.Containers[0].Args {
				gomega.Expect(strings.Contains(arg, "--spoke-external-server-urls")).NotTo(gomega.BeTrue())
			}
		})

		ginkgo.It("should have correct work deployment when clusterName is empty", func() {
			klusterlet.Spec.ClusterName = ""
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Eventually(func() bool {
				if _, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			deployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(len(deployment.Spec.Template.Spec.Containers)).Should(gomega.Equal(1))

			for _, arg := range deployment.Spec.Template.Spec.Containers[0].Args {
				if strings.HasPrefix(arg, "--spoke-cluster-name") {
					gomega.Expect(arg).Should(gomega.Equal("--spoke-cluster-name="))
				}
			}
		})
	})
})
