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

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("Klusterlet using aws auth", func() {
	var cancel context.CancelFunc
	var klusterlet *operatorapiv1.Klusterlet
	var hubKubeConfigSecret *corev1.Secret
	var klusterletNamespace string
	var registrationDeploymentName string
	var registrationSAName string
	var workDeploymentName string
	var workSAName string
	var agentLabelSelector string

	ginkgo.BeforeEach(func() {
		var ctx context.Context

		klusterletNamespace = fmt.Sprintf("open-cluster-management-aws-%s", rand.String(6))
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: klusterletNamespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

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
						AuthType: commonhelpers.AwsIrsaAuthType,
						AwsIrsa: &operatorapiv1.AwsIrsa{
							HubClusterArn:     util.HubClusterArn,
							ManagedClusterArn: util.ManagedClusterArn,
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
		_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Create(context.Background(), hubKubeConfigSecret, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

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

	ginkgo.Context("Deploy and clean klusterlet component using aws auth", func() {
		ginkgo.BeforeEach(func() {
			registrationDeploymentName = fmt.Sprintf("%s-registration-agent", klusterlet.Name)
			workDeploymentName = fmt.Sprintf("%s-work-agent", klusterlet.Name)

			registrationSAName = fmt.Sprintf("%s-registration-sa", klusterlet.Name)
			workSAName = fmt.Sprintf("%s-work-sa", klusterlet.Name)
		})

		ginkgo.AfterEach(func() {
			gomega.Expect(operatorClient.OperatorV1().Klusterlets().Delete(context.Background(), klusterlet.Name, metav1.DeleteOptions{})).To(gomega.BeNil())
		})

		ginkgo.It("should have expected resource created successfully using aws auth", func() {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Check service account
			gomega.Eventually(func() bool {
				serviceaccouts, err := kubeClient.CoreV1().ServiceAccounts(klusterletNamespace).List(context.Background(),
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
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check deployment
			gomega.Eventually(func() bool {
				deployments, err := kubeClient.AppsV1().Deployments(klusterletNamespace).List(context.Background(),
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
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "Applied", "KlusterletApplied", metav1.ConditionTrue)
		})

	})

})
