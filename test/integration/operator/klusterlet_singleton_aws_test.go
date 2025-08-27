package operator

import (
	"context"
	"fmt"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("Klusterlet Singleton mode with aws auth", func() {
	var cancel context.CancelFunc
	var klusterlet *operatorapiv1.Klusterlet
	var agentNamespace string
	var deploymentName string
	var saName string

	ginkgo.BeforeEach(func() {
		var ctx context.Context
		klusterlet = &operatorapiv1.Klusterlet{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("klusterlet-%s", rand.String(6)),
			},
			Spec: operatorapiv1.KlusterletSpec{
				Namespace:     fmt.Sprintf("%s-singleton-aws", helpers.KlusterletDefaultNamespace),
				ImagePullSpec: "quay.io/open-cluster-management/registration-operator",
				ExternalServerURLs: []operatorapiv1.ServerURL{
					{
						URL: "https://localhost",
					},
				},
				ClusterName: "testcluster",
				DeployOption: operatorapiv1.KlusterletDeployOption{
					Mode: operatorapiv1.InstallModeSingleton,
				},
				RegistrationConfiguration: &operatorapiv1.RegistrationConfiguration{
					RegistrationDriver: operatorapiv1.RegistrationDriver{
						AuthType: operatorapiv1.AwsIrsaAuthType,
						AwsIrsa: &operatorapiv1.AwsIrsa{
							HubClusterArn:     util.HubClusterArn,
							ManagedClusterArn: util.ManagedClusterArn,
						},
					},
				},
			},
		}
		agentNamespace = helpers.AgentNamespace(klusterlet)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: agentNamespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ctx, cancel = context.WithCancel(context.Background())
		go startKlusterletOperator(ctx)
	})

	ginkgo.AfterEach(func() {
		err := kubeClient.CoreV1().Namespaces().Delete(context.Background(), agentNamespace, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		if cancel != nil {
			cancel()
		}
	})

	ginkgo.Context("Deploy and clean klusterlet component with aws auth", func() {
		ginkgo.BeforeEach(func() {
			deploymentName = fmt.Sprintf("%s-agent", klusterlet.Name)
			saName = fmt.Sprintf("%s-work-sa", klusterlet.Name)
		})

		ginkgo.AfterEach(func() {
			gomega.Expect(operatorClient.OperatorV1().Klusterlets().Delete(context.Background(), klusterlet.Name, metav1.DeleteOptions{})).To(gomega.BeNil())
		})

		ginkgo.It("should have expected resource created successfully when registered with aws auth", func() {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Check service account
			gomega.Eventually(func() bool {
				sa, err := kubeClient.CoreV1().ServiceAccounts(agentNamespace).Get(context.Background(), saName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return sa.ObjectMeta.Annotations[util.IrsaAnnotationKey] == util.PrerequisiteSpokeRoleArn
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check deployment
			gomega.Eventually(func() bool {
				deployment, err := kubeClient.AppsV1().Deployments(agentNamespace).Get(context.Background(), deploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return util.AllCommandLineOptionsPresent(*deployment) && util.AwsCliSpecificVolumesMounted(*deployment)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "Applied", "KlusterletApplied", metav1.ConditionTrue)
		})
	})
})
