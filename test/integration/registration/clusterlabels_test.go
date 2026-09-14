package registration_test

import (
	"fmt"
	"path"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("Cluster Labels", func() {
	ginkgo.It("Cluster Labels should be created on the managed cluster", func() {
		managedClusterName := "clusterlabels-spokecluster"
		//#nosec G101
		hubKubeconfigSecret := "clusterlabels-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "clusterlabels", "hub-kubeconfig")

		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			ClusterLabels: map[string]string{
				"env":    "production",
				"region": "us-west-2",
			},
			RegisterDriverOption: registerfactory.NewOptions(),
		}

		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName

		// run registration agent
		cancel := runAgent("clusterlabelstest", agentOptions, commOptions, spokeCfg)
		defer cancel()

		// after bootstrap the spokecluster and csr should be created
		gomega.Eventually(func() error {
			mc, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return err
			}

			if mc.Labels["env"] != "production" {
				return fmt.Errorf("expected label env=production, got %s", mc.Labels["env"])
			}
			if mc.Labels["region"] != "us-west-2" {
				return fmt.Errorf("expected label region=us-west-2, got %s", mc.Labels["region"])
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
	})
})
