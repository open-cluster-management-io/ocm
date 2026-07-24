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

		reservedLabel := "cluster.open-cluster-management.io/clusterset"
		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			ClusterLabels: map[string]string{
				"env":         "prod",
				reservedLabel: "should-be-dropped", // reserved, should be filtered out
			},
			RegisterDriverOption: registerfactory.NewOptions(),
		}

		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName

		// run registration agent
		cancel := runAgent("clusterlabelstest", agentOptions, commOptions, spokeCfg)
		defer cancel()

		// after bootstrap the user-defined label should be set and the reserved one filtered out
		gomega.Eventually(func() error {
			mc, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return err
			}

			if mc.Labels["env"] != "prod" {
				return fmt.Errorf("expected label env=prod, got %q in %v", mc.Labels["env"], mc.Labels)
			}

			// Assert the spoke-injected value never wins, rather than absence: the hub may
			// legitimately own this label (a mutating webhook can default it), so the contract
			// is only that the value supplied by the spoke is dropped.
			if mc.Labels[reservedLabel] == "should-be-dropped" {
				return fmt.Errorf("reserved label %q should have been filtered out, got %v", reservedLabel, mc.Labels)
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
	})
})
