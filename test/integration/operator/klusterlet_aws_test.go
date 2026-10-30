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

var _ = Describe("Klusterlet using aws auth", func() {
	var cancel context.CancelFunc
	var klusterlet *operatorapiv1.Klusterlet
	var hubKubeConfigSecret *corev1.Secret
	var klusterletNamespace string
	var registrationDeploymentName string
	var registrationSAName string
	var workDeploymentName string
	var workSAName string
	var agentLabelSelector string

	BeforeEach(func(ctx SpecContext) {
		var runCtx context.Context

		klusterletNamespace = fmt.Sprintf("open-cluster-management-aws-%s", rand.String(6))
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: klusterletNamespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		klusterlet = &operatorapiv1.Klusterlet{
			ObjectMeta: metav1.ObjectMeta{
				Name:   fmt.Sprintf("klusterlet-%s", rand.String(6)),
				Labels: map[string]string{"test": "123", "component": "klusterlet", "123": "312"},
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

		agentLabelSelector = metav1.FormatLabelSelector(&metav1.LabelSelector{
			MatchLabels: helpers.GetKlusterletAgentLabels(klusterlet, false),
		})

		hubKubeConfigSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      helpers.HubKubeConfig,
				Namespace: klusterletNamespace,
			},
			Data: map[string][]byte{
				"placeholder": []byte("placeholder"),
			},
		}
		_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Create(ctx, hubKubeConfigSecret, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		runCtx, cancel = context.WithCancel(context.Background())
		go startKlusterletOperator(runCtx)
	})

	AfterEach(func(ctx SpecContext) {
		err := kubeClient.CoreV1().Namespaces().Delete(ctx, klusterletNamespace, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		if cancel != nil {
			cancel()
		}
	})

	Context("Deploy and clean klusterlet component using aws auth", func() {
		BeforeEach(func() {
			registrationDeploymentName = fmt.Sprintf("%s-registration-agent", klusterlet.Name)
			workDeploymentName = fmt.Sprintf("%s-work-agent", klusterlet.Name)

			registrationSAName = fmt.Sprintf("%s-registration-sa", klusterlet.Name)
			workSAName = fmt.Sprintf("%s-work-sa", klusterlet.Name)
		})

		AfterEach(func(ctx SpecContext) {
			Expect(operatorClient.OperatorV1().Klusterlets().Delete(ctx, klusterlet.Name, metav1.DeleteOptions{})).To(BeNil())
		})

		It("should have expected resource created successfully using aws auth", func(ctx SpecContext) {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(ctx, klusterlet, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// Check service account
			Eventually(func() bool {
				serviceaccouts, err := kubeClient.CoreV1().ServiceAccounts(klusterletNamespace).List(ctx,
					metav1.ListOptions{LabelSelector: agentLabelSelector})
				if err != nil {
					return false
				}
				if len(serviceaccouts.Items) != 2 {
					return false
				}
				for _, serviceAccount := range serviceaccouts.Items {
					if serviceAccount.GetName() != registrationSAName &&
						serviceAccount.GetName() != workSAName {
						return false
					}
					if serviceAccount.ObjectMeta.Annotations[util.IrsaAnnotationKey] != util.PrerequisiteSpokeRoleArn {
						return false
					}
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check deployment
			Eventually(func() bool {
				deployments, err := kubeClient.AppsV1().Deployments(klusterletNamespace).List(ctx,
					metav1.ListOptions{LabelSelector: agentLabelSelector})
				if err != nil {
					return false
				}
				if len(deployments.Items) != 2 {
					return false
				}

				for _, deployment := range deployments.Items {
					if deployment.GetName() != registrationDeploymentName &&
						deployment.GetName() != workDeploymentName {
						return false
					}
					if deployment.GetName() == registrationDeploymentName {
						if !util.AllCommandLineOptionsPresent(deployment) || !util.AwsCliSpecificVolumesMounted(deployment) {
							return false
						}
					}
					if deployment.GetName() == workDeploymentName {
						if !util.AwsCliSpecificVolumesMounted(deployment) {
							return false
						}
					}
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "Applied", "KlusterletApplied", metav1.ConditionTrue)
		})

	})

})
