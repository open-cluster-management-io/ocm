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

var _ = ginkgo.Describe("Cluster Annotations for aws", func() {
	ginkgo.It("Cluster Annotations for aws flow should be created on the managed cluster", func() {
		managedClusterName := "clusterannotations-spokecluster-aws"
		//#nosec G101
		hubKubeconfigSecret := "clusterannotations-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "clusterannotations", "hub-kubeconfig")

		managedClusterArn := "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1"
		managedClusterRoleSuffix := "7f8141296c75f2871e3d030f85c35692"
		hubClusterArn := "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster1"
		agentOptions := &spoke.SpokeAgentOptions{
			RegisterDriverOption: &registerfactory.Options{
				RegistrationAuth: operatorv1.AwsIrsaAuthType,
				AWSIRSAOption: &aws_irsa.AWSOption{
					HubClusterArn:            hubClusterArn,
					ManagedClusterArn:        managedClusterArn,
					ManagedClusterRoleSuffix: managedClusterRoleSuffix,
				},
			},
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			ClusterAnnotations: map[string]string{
				"agent.open-cluster-management.io/foo": "bar",
				"foo":                                  "bar", // this annotation should be filtered out
			},
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

			if mc.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+aws_irsa.ManagedClusterArn] != managedClusterArn {
				return fmt.Errorf("expected annotation "+operatorv1.ClusterAnnotationsKeyPrefix+"/"+aws_irsa.ManagedClusterArn+" to be "+
					""+managedClusterArn+", got %s", mc.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+aws_irsa.ManagedClusterArn])
			}

			if mc.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+aws_irsa.ManagedClusterIAMRoleSuffix] != managedClusterRoleSuffix {
				return fmt.Errorf("expected annotation "+operatorv1.ClusterAnnotationsKeyPrefix+"/"+aws_irsa.ManagedClusterIAMRoleSuffix+" "+
					"to be "+managedClusterRoleSuffix+", got %s", mc.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+aws_irsa.ManagedClusterIAMRoleSuffix])
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

	})
})
