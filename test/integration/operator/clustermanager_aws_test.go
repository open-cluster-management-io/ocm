package operator

import (
	"context"
	"fmt"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

var _ = ginkgo.Describe("ClusterManager Default Mode with aws registration", func() {
	var cancel context.CancelFunc
	var hubRegistrationSA = "registration-controller-sa"
	var hubClusterArn = "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster"
	var tags = "product:v1:tenant:app-name=My-App,product:v1:tenant:created-by=Team-1"

	ginkgo.BeforeEach(func() {
		var ctx context.Context
		ctx, cancel = context.WithCancel(context.Background())
		go startHubOperator(ctx, operatorapiv1.InstallModeDefault)
	})

	ginkgo.AfterEach(func() {
		// delete deployment for clustermanager here so tests are not impacted with each other
		err := kubeClient.AppsV1().Deployments(hubNamespace).DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		if cancel != nil {
			cancel()
		}
	})

	ginkgo.Context("Deploy hub with aws auth", func() {
		ginkgo.BeforeEach(func() {
			gomega.Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(context.Background(),
					clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if clusterManager.Spec.RegistrationConfiguration == nil {
					clusterManager.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{}
					clusterManager.Spec.RegistrationConfiguration.RegistrationDrivers = []operatorapiv1.RegistrationDriverHub{
						{
							AuthType: operatorapiv1.AwsIrsaAuthType,
							AwsIrsa: &operatorapiv1.AwsIrsaConfig{
								HubClusterArn: hubClusterArn,
								Tags: []string{
									"product:v1:tenant:app-name=My-App",
									"product:v1:tenant:created-by=Team-1",
								},
								AutoApprovedIdentities: []string{"arn:aws:eks:us-west-2:123456789013:cluster/.*", "arn:aws:eks:us-west-2:123456789012:cluster/.*"},
							},
						},
					}
				}
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(context.Background(),
					clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
		ginkgo.AfterEach(func() {
			gomega.Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(context.Background(),
					clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				clusterManager.Spec.RegistrationConfiguration = nil
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(context.Background(),
					clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
		ginkgo.It("should have IAM role annotation when initialized with awsirsa", func() {
			gomega.Eventually(func() bool {
				registrationControllerSA, err := kubeClient.CoreV1().ServiceAccounts(hubNamespace).Get(
					context.Background(), hubRegistrationSA, metav1.GetOptions{})
				if err != nil {
					return false
				}
				annotation := registrationControllerSA.Annotations["eks.amazonaws.com/role-arn"]

				return annotation == "arn:aws:iam::123456789012:role/hub-cluster_managed-cluster-identity-creator"
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})
		ginkgo.It("registration-controller should have command line option hub cluster arn when initialized with awsirsa", func() {
			gomega.Eventually(func() bool {

				registrationControllerDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).
					Get(context.Background(), fmt.Sprintf("%s-registration-controller", clusterManagerName), metav1.GetOptions{})
				if err != nil {
					return false
				}
				commandLineArgs := registrationControllerDeployment.Spec.Template.Spec.Containers[0].Args
				hubClusterArnArg, present := findMatchingArg(commandLineArgs, "--hub-cluster-arn")
				return present && strings.Split(hubClusterArnArg, "=")[1] == hubClusterArn
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})
		ginkgo.It("registration-controller should have command line option aws-resource-tags when initialized with awsirsa", func() {
			gomega.Eventually(func() bool {

				registrationControllerDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).
					Get(context.Background(), fmt.Sprintf("%s-registration-controller", clusterManagerName), metav1.GetOptions{})
				if err != nil {
					return false
				}
				commandLineArgs := registrationControllerDeployment.Spec.Template.Spec.Containers[0].Args
				tagsArg, present := findMatchingArg(commandLineArgs, "--aws-resource-tags")

				return present && strings.SplitN(tagsArg, "=", 2)[1] == tags
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})
		ginkgo.It("should have auto approved arn patterns separated by comma with awsirsa", func() {
			gomega.Eventually(func() bool {
				registrationControllerDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).
					Get(context.Background(), fmt.Sprintf("%s-registration-controller", clusterManagerName), metav1.GetOptions{})
				if err != nil {
					return false
				}
				commandLineArgs := registrationControllerDeployment.Spec.Template.Spec.Containers[0].Args
				autoApprovedArnPatterns, present := findMatchingArg(commandLineArgs, "--auto-approved-arn-patterns")
				return present && strings.Split(autoApprovedArnPatterns, "=")[1] ==
					"arn:aws:eks:us-west-2:123456789013:cluster/.*,arn:aws:eks:us-west-2:123456789012:cluster/.*"
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})
	})

	ginkgo.Context("Deploy hub with aws auth with no tags", func() {
		ginkgo.BeforeEach(func() {
			gomega.Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(context.Background(),
					clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if clusterManager.Spec.RegistrationConfiguration == nil {
					clusterManager.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{}
					clusterManager.Spec.RegistrationConfiguration.RegistrationDrivers = []operatorapiv1.RegistrationDriverHub{
						{
							AuthType: operatorapiv1.AwsIrsaAuthType,
							AwsIrsa: &operatorapiv1.AwsIrsaConfig{
								HubClusterArn: hubClusterArn,
							},
						},
					}
				}
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(context.Background(),
					clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
		ginkgo.AfterEach(func() {
			gomega.Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(context.Background(),
					clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				clusterManager.Spec.RegistrationConfiguration = nil
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(context.Background(),
					clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
		ginkgo.It("registration-controller should not have command line option aws-resource-tags when not initialized with awsirsa", func() {
			gomega.Eventually(func() bool {

				registrationControllerDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).
					Get(context.Background(), fmt.Sprintf("%s-registration-controller", clusterManagerName), metav1.GetOptions{})
				if err != nil {
					return false
				}
				commandLineArgs := registrationControllerDeployment.Spec.Template.Spec.Containers[0].Args
				_, present := findMatchingArg(commandLineArgs, "--aws-resource-tags")

				return !present
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})
	})
})

func findMatchingArg(args []string, pattern string) (string, bool) {
	for _, commandLineArg := range args {
		if strings.Split(commandLineArg, "=")[0] == pattern {
			return commandLineArg, true
		}
	}
	return "", false
}
