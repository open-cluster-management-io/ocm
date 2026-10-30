package operator

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = Describe("Klusterlet Singleton mode with aws auth", func() {
	var cancel context.CancelFunc
	var klusterlet *operatorapiv1.Klusterlet
	var agentNamespace string
	var deploymentName string
	var saName string

	BeforeEach(func(ctx SpecContext) {
		var runCtx context.Context
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
					FeatureGates: []operatorapiv1.FeatureGate{
						{
							Feature: "NetworkPolicies",
							Mode:    operatorapiv1.FeatureGateModeTypeEnable,
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
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		runCtx, cancel = context.WithCancel(context.Background())
		go startKlusterletOperator(runCtx)
	})

	AfterEach(func(ctx SpecContext) {
		err := kubeClient.CoreV1().Namespaces().Delete(ctx, agentNamespace, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		if cancel != nil {
			cancel()
		}
	})

	Context("Deploy and clean klusterlet component with aws auth", func() {
		BeforeEach(func() {
			deploymentName = fmt.Sprintf("%s-agent", klusterlet.Name)
			saName = fmt.Sprintf("%s-work-sa", klusterlet.Name)
		})

		AfterEach(func(ctx SpecContext) {
			Expect(operatorClient.OperatorV1().Klusterlets().Delete(ctx, klusterlet.Name, metav1.DeleteOptions{})).To(BeNil())
		})

		It("should have expected resource created successfully when registered with aws auth", func(ctx SpecContext) {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(ctx, klusterlet, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// Check service account
			Eventually(func() bool {
				sa, err := kubeClient.CoreV1().ServiceAccounts(agentNamespace).Get(ctx, saName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return sa.ObjectMeta.Annotations[util.IrsaAnnotationKey] == util.PrerequisiteSpokeRoleArn
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check deployment
			Eventually(func() bool {
				deployment, err := kubeClient.AppsV1().Deployments(agentNamespace).Get(ctx, deploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return util.AllCommandLineOptionsPresent(*deployment) && util.AwsCliSpecificVolumesMounted(*deployment)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "Applied", "KlusterletApplied", metav1.ConditionTrue)
		})
	})
})
