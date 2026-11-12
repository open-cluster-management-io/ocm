package registration_test

import (
	"fmt"
	"path"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = Describe("Agent Recovery", func() {

	It("agent recovery from invalid bootstrap kubeconfig", func(ctx SpecContext) {
		var err error

		managedClusterName := "bootstrap-recoverytest-spokecluster"

		//#nosec G101
		hubKubeconfigSecret := "bootstrap-recoverytest-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "bootstrap-recoverytest", "hub-kubeconfig")

		bootstrapFile := path.Join(util.TestDir, "bootstrap-recoverytest", "kubeconfig")
		// create an INVALID bootstrap kubeconfig file with an expired cert
		err = authn.CreateBootstrapKubeConfigWithCertAge(bootstrapFile, serverCertFile, securePort, -1*time.Hour)
		Expect(err).NotTo(HaveOccurred())

		// run registration agent with an invalid bootstrap kubeconfig
		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			RegisterDriverOption:     registerfactory.NewOptions(),
		}
		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName

		cancel := runAgent("bootstrap-recoverytest", agentOptions, commOptions, spokeCfg)
		defer cancel()

		// the managedcluster should not be created
		retryToGetSpokeClusterTimes := 0
		Eventually(func() int {
			_, err = util.GetManagedCluster(clusterClient, managedClusterName)
			Expect(err).To(HaveOccurred())
			Expect(errors.IsNotFound(err)).Should(BeTrue())
			retryToGetSpokeClusterTimes++
			return retryToGetSpokeClusterTimes
		}, eventuallyTimeout, eventuallyInterval).Should(BeNumerically(">=", 3))

		// the csr should not be created
		retryToGetSpokeCSRTimes := 0
		Eventually(func() int {
			_, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName)
			Expect(err).To(HaveOccurred())
			retryToGetSpokeCSRTimes++
			return retryToGetSpokeCSRTimes
		}, eventuallyTimeout, eventuallyInterval).Should(BeNumerically(">=", 3))

		// recover the invalid bootstrap kubeconfig file
		err = authn.CreateBootstrapKubeConfigWithCertAge(bootstrapFile, serverCertFile, securePort, 24*time.Hour)
		Expect(err).NotTo(HaveOccurred())

		// the csr should be created after the bootstrap kubeconfig was recovered
		Eventually(func() error {
			if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

		// the spoke cluster should be created after the bootstrap kubeconfig was recovered
		Eventually(func() bool {
			if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		// simulate hub cluster admin accept the spoke cluster and approve the csr
		err = util.AcceptManagedCluster(clusterClient, managedClusterName)
		Expect(err).NotTo(HaveOccurred())

		err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Hour*24)
		Expect(err).NotTo(HaveOccurred())

		// the hub kubeconfig secret should be filled after the csr is approved
		Eventually(func() error {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// the spoke cluster should have joined condition finally
		Eventually(func() error {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return err
			}
			if !meta.IsStatusConditionTrue(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
				return fmt.Errorf("cluster should be joined")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
	})

	It("agent recovery from invalid hub kubeconfig", func(ctx SpecContext) {
		var err error

		spokeClusterName := "hubkubeconfig-recoverytest-spokecluster"

		//#nosec G101
		hubKubeconfigSecret := "hubkubeconfig-recoverytest-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "hubkubeconfig-recoverytest", "hub-kubeconfig")

		// run registration agent
		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			RegisterDriverOption:     registerfactory.NewOptions(),
		}
		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = spokeClusterName

		cancel := runAgent("hubkubeconfig-recoverytest", agentOptions, commOptions, spokeCfg)
		defer cancel()

		// after bootstrap the spokecluster and csr should be created
		Eventually(func() bool {
			if _, err := util.GetManagedCluster(clusterClient, spokeClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		var firstCSRName string
		Eventually(func() bool {
			csr, err := util.FindUnapprovedSpokeCSR(kubeClient, spokeClusterName)
			if err != nil {
				return false
			}
			firstCSRName = csr.Name
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		// simulate hub cluster admin accept the spoke cluster
		err = util.AcceptManagedCluster(clusterClient, spokeClusterName)
		Expect(err).NotTo(HaveOccurred())

		// simulate hub cluster admin approve the csr with an INVALID hub config
		err = authn.ApproveSpokeClusterCSRWithExpiredCert(kubeClient, spokeClusterName)
		Expect(err).NotTo(HaveOccurred())

		var firstHubKubeConfigSecret *corev1.Secret
		// the hub kubeconfig secret should be filled after the csr is approved
		Eventually(func() error {
			firstHubKubeConfigSecret, err = util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret)
			return err
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

		// agent should bootstrap again due to the invalid hub config
		var secondCSRName string
		Eventually(func() bool {
			csr, err := util.FindUnapprovedSpokeCSR(kubeClient, spokeClusterName)
			if err != nil {
				return false
			}
			secondCSRName = csr.Name
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		// a new csr should be recreated
		Expect(firstCSRName).ShouldNot(BeEquivalentTo(secondCSRName))

		// approve the new csr with a valid hub config
		err = authn.ApproveSpokeClusterCSR(kubeClient, spokeClusterName, time.Hour*24)
		Expect(err).NotTo(HaveOccurred())

		// wait the hub kubeconfig secret is updated with the valid hub config
		Eventually(func() bool {
			secondHubKubeConfigSecret, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret)
			if err != nil {
				return false
			}

			// the hub kubeconfig secret should be updated
			if reflect.DeepEqual(firstHubKubeConfigSecret.Data, secondHubKubeConfigSecret.Data) {
				return false
			}

			return true
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		// the spoke cluster should have joined condition finally
		Eventually(func() error {
			spokeCluster, err := util.GetManagedCluster(clusterClient, spokeClusterName)
			if err != nil {
				return err
			}
			if !meta.IsStatusConditionTrue(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
				return fmt.Errorf("cluster should be joined")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
	})
})
