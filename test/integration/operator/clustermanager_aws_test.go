package operator

import (
	"context"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

var _ = ginkgo.Describe("ClusterManager Default Mode with aws registration", func() {
	var cancel context.CancelFunc
	var hubRegistrationSA = "registration-controller-sa"

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

		ginkgo.It("should have IAM role annotation when initialized with awsirsa", func() {

			clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(context.Background(), clusterManagerName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			if clusterManager.Spec.RegistrationConfiguration == nil {
				clusterManager.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{}
				clusterManager.Spec.RegistrationConfiguration.RegistrationDrivers = []operatorapiv1.RegistrationDriverHub{
					{
						AuthType:      "awsirsa",
						HubClusterArn: "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster",
					},
				}
			}
			_, err = operatorClient.OperatorV1().ClusterManagers().Update(context.Background(), clusterManager, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

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
	})

})
