package registration_test

import (
	"fmt"
	"path"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	operatorv1 "open-cluster-management.io/api/operator/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/register/aws_irsa"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = ginkgo.Describe("Cluster Annotations", func() {
	ginkgo.It("Cluster Annotations should be created on the managed cluster", func() {
		managedClusterName := "clusterannotations-spokecluster"
		//#nosec G101
		hubKubeconfigSecret := "clusterannotations-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "clusterannotations", "hub-kubeconfig")

		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			ClusterAnnotations: map[string]string{
				"agent.open-cluster-management.io/foo": "bar",
				"foo":                                  "bar", // this annotation should be filtered out
			},
			RegisterDriverOption: registerfactory.NewOptions(),
		}

		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName

		// run registration agent
		cancel := runAgent("rotationtest", agentOptions, commOptions, spokeCfg)
		defer cancel()

		// after bootstrap the spokecluster and csr should be created
		gomega.Eventually(func() error {
			mc, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return err
			}

			if _, ok := mc.Annotations["foo"]; ok {
				return fmt.Errorf("unexpected annotations %v", mc.Annotations)
			}

			if _, ok := mc.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+aws_irsa.ManagedClusterArn]; ok {
				return fmt.Errorf("unexpected annotations %v", mc.Annotations)
			}

			if _, ok := mc.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+aws_irsa.ManagedClusterIAMRoleSuffix]; ok {
				return fmt.Errorf("unexpected annotations %v", mc.Annotations)
			}

			if mc.Annotations["agent.open-cluster-management.io/foo"] != "bar" {
				return fmt.Errorf("expected annotation agent.open-cluster-management.io/foo to be bar, got %s", mc.Annotations["agent.open-cluster-management.io/foo"])
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

	})
})
