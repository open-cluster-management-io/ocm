package operator

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ocmfeature "open-cluster-management.io/api/feature"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

var _ = Describe("Deploy Klusterlet with Multiplehubs enabled", func() {
	var cancel context.CancelFunc
	var klusterlet *operatorapiv1.Klusterlet
	var agentNamespace string
	var (
		boostrapKubeConfigSecretHub1 = "bootstraphub1" // #nosec G101
		boostrapKubeConfigSecretHub2 = "bootstraphub2" // #nosec G101
	)

	BeforeEach(func() {
		By("Configure Klusterlet CR")
		klusterlet = &operatorapiv1.Klusterlet{
			ObjectMeta: metav1.ObjectMeta{
				Name: "klusterlet-multiplehubs-enabled",
			},
			Spec: operatorapiv1.KlusterletSpec{
				ImagePullSpec: "quay.io/open-cluster-management/registration-operator",
				ClusterName:   "testcluster",
				Namespace:     "open-cluster-management-agent-multiplehubs",
				RegistrationConfiguration: &operatorapiv1.RegistrationConfiguration{
					FeatureGates: []operatorapiv1.FeatureGate{
						{
							Feature: string(ocmfeature.MultipleHubs),
							Mode:    operatorapiv1.FeatureGateModeTypeEnable,
						},
					},
					BootstrapKubeConfigs: operatorapiv1.BootstrapKubeConfigs{
						Type: operatorapiv1.LocalSecrets,
						LocalSecrets: &operatorapiv1.LocalSecretsConfig{
							KubeConfigSecrets: []operatorapiv1.KubeConfigSecret{
								{
									Name: boostrapKubeConfigSecretHub1,
								},
								{
									Name: boostrapKubeConfigSecretHub2,
								},
							},
						},
					},
				},
				DeployOption: operatorapiv1.KlusterletDeployOption{
					Mode: operatorapiv1.InstallModeSingleton,
				},
			},
		}

		By("Create Agentnamespace")
		agentNamespace = helpers.AgentNamespace(klusterlet)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: agentNamespace,
			},
		}

		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go startKlusterletOperator(ctx)
	})
	AfterEach(func() {
		Expect(kubeClient.CoreV1().Namespaces().Delete(context.Background(), agentNamespace, metav1.DeleteOptions{})).Should(Succeed())
		cancel()
	})

	It("should have expected resources deployed", func() {
		var err error

		By("Create 2 bootstrapKubeConfig secrets")
		_, err = kubeClient.CoreV1().Secrets(agentNamespace).Create(context.TODO(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      boostrapKubeConfigSecretHub1,
				Namespace: agentNamespace,
			},
		}, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())
		_, err = kubeClient.CoreV1().Secrets(agentNamespace).Create(context.TODO(), &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      boostrapKubeConfigSecretHub2,
				Namespace: agentNamespace,
			},
		}, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		By("Create the Klusterlet")
		_, err = operatorClient.OperatorV1().Klusterlets().Create(context.TODO(), klusterlet, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		Eventually(func() error {
			actual, err := operatorClient.OperatorV1().Klusterlets().Get(context.Background(), klusterlet.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			fmt.Printf("related resources are %v\n", actual.Status.RelatedResources)

			// 10 managed static manifests + 9 management static manifests + 2CRDs + 1 deployments
			// The number is the same as the singletone mode.
			if len(actual.Status.RelatedResources) != 22 {
				return fmt.Errorf("should get 22 relatedResources, actual got %v", len(actual.Status.RelatedResources))
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

		By("Check deployment")
		Eventually(func() error {
			deployment, err := kubeClient.AppsV1().Deployments(agentNamespace).Get(context.Background(), fmt.Sprintf("%s-agent", klusterlet.Name), metav1.GetOptions{})
			if err != nil {
				return err
			}

			containers := deployment.Spec.Template.Spec.Containers
			if len(containers) != 1 {
				return fmt.Errorf("should have 1 container, got %v", len(containers))
			}

			// Check BoostrapKubeConfig Secrets are used
			args := containers[0].Args
			bootstrapKubeConfigSecretsNumber := 0
			for _, arg := range args {
				if strings.Contains(arg, "--bootstrap-kubeconfigs") {
					bootstrapKubeConfigSecretsNumber++
				}
			}
			if bootstrapKubeConfigSecretsNumber != 2 {
				return fmt.Errorf("should have 2 bootstrap kubeconfig secrets, got %v", bootstrapKubeConfigSecretsNumber)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
	})
})
