package operator

import (
	"context"
	"fmt"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/test/integration/util"
)

var hubClusterArn string = "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster1"
var managedClusterArn string = "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1"
var managedClusterRoleSuffix string = "7f8141296c75f2871e3d030f85c35692"
var prerequisiteSpokeRoleArn string = "arn:aws:iam::123456789012:role/ocm-managed-cluster-" + managedClusterRoleSuffix
var irsaAnnotationKey string = "eks.amazonaws.com/role-arn"

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
				Namespace:     fmt.Sprintf("%s-aws", helpers.KlusterletDefaultNamespace),
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
						AuthType: "awsirsa",
						AwsIrsa: &operatorapiv1.AwsIrsa{
							HubClusterArn:     hubClusterArn,
							ManagedClusterArn: managedClusterArn,
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
				return sa.ObjectMeta.Annotations[irsaAnnotationKey] == prerequisiteSpokeRoleArn
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check deployment
			gomega.Eventually(func() bool {
				deployment, err := kubeClient.AppsV1().Deployments(agentNamespace).Get(context.Background(), deploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				isRegistrationAuthPresent := false
				isManagedClusterArnPresent := false
				isManagedClusterRoleSuffixPresent := false
				for _, arg := range deployment.Spec.Template.Spec.Containers[0].Args {
					if strings.Contains(arg, "--registration-auth=awsirsa") {
						isRegistrationAuthPresent = true
					}
					if strings.Contains(arg, "--managed-cluster-arn=arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1") {
						isManagedClusterArnPresent = true
					}
					if strings.Contains(arg, "--managed-cluster-role-suffix="+managedClusterRoleSuffix) {
						isManagedClusterRoleSuffixPresent = true
					}
				}
				allCommandLineOptionsPresent := isRegistrationAuthPresent && isManagedClusterArnPresent && isManagedClusterRoleSuffixPresent

				isDotAwsMounted := false
				for _, volumeMount := range deployment.Spec.Template.Spec.Containers[0].VolumeMounts {
					if volumeMount.Name == "dot-aws" && volumeMount.MountPath == "/.aws" {
						isDotAwsMounted = true
					}
				}

				awsCliSpecificVolumesMounted := isDotAwsMounted
				return allCommandLineOptionsPresent && awsCliSpecificVolumesMounted
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "Applied", "KlusterletApplied", metav1.ConditionTrue)
		})
	})
})
