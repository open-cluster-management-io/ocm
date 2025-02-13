package registration_test

import (
	"fmt"
	"path"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/tools/clientcmd"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("Joining Process", func() {
	var bootstrapKubeconfig string
	var managedClusterName string
	var hubKubeconfigSecret string
	var hubKubeconfigDir string
	var expectedProxyURL string

	ginkgo.BeforeEach(func() {
		postfix := rand.String(5)
		managedClusterName = fmt.Sprintf("joiningtest-managedcluster-%s", postfix)
		hubKubeconfigSecret = fmt.Sprintf("joiningtest-hub-kubeconfig-secret-%s", postfix)
		hubKubeconfigDir = path.Join(util.TestDir, fmt.Sprintf("joiningtest-%s", postfix), "hub-kubeconfig")
	})

	assertJoiningSucceed := func() {
		ginkgo.It("managedcluster should join successfully", func() {
			var err error

			// run registration agent
			agentOptions := &spoke.SpokeAgentOptions{
				BootstrapKubeconfig:      bootstrapKubeconfig,
				HubKubeconfigSecret:      hubKubeconfigSecret,
				ClusterHealthCheckPeriod: 1 * time.Minute,
			}
			commOptions := commonoptions.NewAgentOptions()
			commOptions.HubKubeconfigDir = hubKubeconfigDir
			commOptions.SpokeClusterName = managedClusterName

			cancel := runAgent("joiningtest", agentOptions, commOptions, spokeCfg)
			defer cancel()

			// the spoke cluster and csr should be created after bootstrap
			gomega.Eventually(func() error {
				if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// the spoke cluster should has finalizer that is added by hub controller
			gomega.Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}

				if !commonhelpers.HasFinalizer(spokeCluster.Finalizers, clusterv1.ManagedClusterFinalizer) {
					return fmt.Errorf("finalizer is not correct")
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// simulate hub cluster admin to accept the managedcluster and approve the csr
			err = util.AcceptManagedCluster(clusterClient, managedClusterName)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Hour*24)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// the managed cluster should have accepted condition after it is accepted
			gomega.Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !meta.IsStatusConditionTrue(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
					return fmt.Errorf("cluster should be accepted")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// the hub kubeconfig secret should be filled after the csr is approved
			gomega.Eventually(func() error {
				secret, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret)
				if err != nil {
					return err
				}

				// check if the proxyURL is set correctly
				proxyURL, err := getProxyURLFromKubeconfigData(secret.Data["kubeconfig"])
				if err != nil {
					return err
				}
				if proxyURL != expectedProxyURL {
					return fmt.Errorf("expected proxy url %q, but got %q", expectedProxyURL, proxyURL)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// the spoke cluster should have joined condition finally
			gomega.Eventually(func() error {
				spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				if !meta.IsStatusConditionTrue(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined) {
					return fmt.Errorf("cluster should be joined")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})
	}

	ginkgo.Context("without proxy", func() {
		ginkgo.BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigFile
			expectedProxyURL = ""
		})
		assertJoiningSucceed()
	})

	ginkgo.Context("with http proxy", func() {
		ginkgo.BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigHTTPProxyFile
			expectedProxyURL = httpProxyURL
		})
		assertJoiningSucceed()
	})

	ginkgo.Context("with https proxy", func() {
		ginkgo.BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigHTTPSProxyFile
			expectedProxyURL = httpsProxyURL
		})
		assertJoiningSucceed()
	})
})

func getProxyURLFromKubeconfigData(kubeconfigData []byte) (string, error) {
	config, err := clientcmd.Load(kubeconfigData)
	if err != nil {
		return "", err
	}

	currentContext, ok := config.Contexts[config.CurrentContext]
	if !ok {
		return "", nil
	}

	cluster, ok := config.Clusters[currentContext.Cluster]
	if !ok {
		return "", nil
	}

	return cluster.ProxyURL, nil
}
