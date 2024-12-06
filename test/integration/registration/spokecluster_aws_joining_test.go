package registration_test

import (
	"fmt"
	"path"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("Joining Process for aws flow", func() {
	var bootstrapKubeconfig string
	var managedClusterName string
	var hubKubeconfigSecret string
	var hubKubeconfigDir string

	ginkgo.BeforeEach(func() {
		postfix := rand.String(5)
		managedClusterName = fmt.Sprintf("joiningtest-managedcluster-%s", postfix)
		hubKubeconfigSecret = fmt.Sprintf("joiningtest-hub-kubeconfig-secret-%s", postfix)
		hubKubeconfigDir = path.Join(util.TestDir, fmt.Sprintf("joiningtest-%s", postfix), "hub-kubeconfig")
	})

	assertJoiningSucceed := func() {
		ginkgo.It("managedcluster should join successfully for aws flow", func() {
			var err error

			managedClusterArn := "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1"
			managedClusterRoleSuffix := "7f8141296c75f2871e3d030f85c35692"
			hubClusterArn := "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster1"

			// run registration agent
			agentOptions := &spoke.SpokeAgentOptions{
				RegistrationAuth:         spoke.AwsIrsaAuthType,
				HubClusterArn:            hubClusterArn,
				ManagedClusterArn:        managedClusterArn,
				ManagedClusterRoleSuffix: managedClusterRoleSuffix,
				BootstrapKubeconfig:      bootstrapKubeconfig,
				HubKubeconfigSecret:      hubKubeconfigSecret,
				ClusterHealthCheckPeriod: 1 * time.Minute,
			}
			commOptions := commonoptions.NewAgentOptions()
			commOptions.HubKubeconfigDir = hubKubeconfigDir
			commOptions.SpokeClusterName = managedClusterName

			cancel := runAgent("joiningtest", agentOptions, commOptions, spokeCfg)
			defer cancel()

			// the ManagedCluster CR should be created after bootstrap
			gomega.Eventually(func() error {
				if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// the csr should not be created for aws flow after bootstrap
			gomega.Eventually(func() error {
				if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.HaveOccurred())

			// simulate hub cluster admin to accept the managedcluster
			err = util.AcceptManagedCluster(clusterClient, managedClusterName)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Hour*24)
			gomega.Expect(err).To(gomega.HaveOccurred())

			// the hub kubeconfig secret should be filled after the ManagedCluster is accepted
			// TODO: Revisit while implementing slice 3
			//gomega.Eventually(func() error {
			//	secret, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret)
			//	if err != nil {
			//		return err
			//	}
			//
			//	// check if the proxyURL is set correctly
			//	proxyURL, err := getProxyURLFromKubeconfigData(secret.Data["kubeconfig"])
			//	if err != nil {
			//		return err
			//	}
			//	if proxyURL != expectedProxyURL {
			//		return fmt.Errorf("expected proxy url %q, but got %q", expectedProxyURL, proxyURL)
			//	}
			//	return nil
			//}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// the spoke cluster should have joined condition finally
			// TODO: Revisit while implementing slice 3
			//gomega.Eventually(func() error {
			//	spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			//	if err != nil {
			//		return err
			//	}
			//	if !meta.IsStatusConditionTrue(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
			//		return fmt.Errorf("cluster should be joined")
			//	}
			//	return nil
			//}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	}

	ginkgo.Context("without proxy", func() {
		ginkgo.BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigFile
		})
		assertJoiningSucceed()
	})

})
