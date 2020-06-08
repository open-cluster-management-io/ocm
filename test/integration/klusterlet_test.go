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

		ginkgo.AfterEach(func() {
			operatorClient.OperatorV1().Klusterlets().Delete(context.Background(), klusterlet.Name, metav1.DeleteOptions{})
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

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "Applied", "KlusterletApplied", metav1.ConditionTrue)
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

	ginkgo.Context("klusterlet statuses", func() {
		ginkgo.BeforeEach(func() {
			registrationDeploymentName = fmt.Sprintf("%s-registration-agent", klusterlet.Name)
			workDeploymentName = fmt.Sprintf("%s-work-agent", klusterlet.Name)
		})
		ginkgo.It("should have correct degraded conditions", func() {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "KlusterletRegistrationDegraded", "BootStrapSecretMissing", metav1.ConditionTrue)

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

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "KlusterletRegistrationDegraded", "KubeConfigMissing", metav1.ConditionTrue)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "KlusterletWorkDegraded", "KubeConfigMissing", metav1.ConditionTrue)

			hubSecret, err := kubeClient.CoreV1().Secrets(klusterletNamespace).Get(context.Background(), "hub-kubeconfig-secret", metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Update hub secret
			hubSecret.Data["cluster-name"] = []byte("testcluster")
			hubSecret.Data["kubeconfig"] = []byte("dummy")
			_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Update(context.Background(), hubSecret, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "KlusterletRegistrationDegraded", "UnavailableRegistrationPod", metav1.ConditionTrue)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "KlusterletWorkDegraded", "UnavailableWorkPod", metav1.ConditionTrue)

			// Update replica of deployment
			registrationDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), registrationDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			registrationDeployment.Status.AvailableReplicas = 3
			registrationDeployment.Status.Replicas = 3
			registrationDeployment.Status.ReadyReplicas = 3
			_, err = kubeClient.AppsV1().Deployments(klusterletNamespace).UpdateStatus(context.Background(), registrationDeployment, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			workDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			workDeployment.Status.AvailableReplicas = 3
			workDeployment.Status.Replicas = 3
			workDeployment.Status.ReadyReplicas = 3
			_, err = kubeClient.AppsV1().Deployments(klusterletNamespace).UpdateStatus(context.Background(), workDeployment, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "KlusterletRegistrationDegraded", "RegistrationFunctional", metav1.ConditionFalse)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "KlusterletWorkDegraded", "WorkFunctional", metav1.ConditionFalse)
		})
	})
})
