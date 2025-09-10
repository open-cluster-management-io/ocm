package registration_test

import (
	"bytes"
	"fmt"
	"path"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/tools/clientcmd"

	operatorv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/hub"
	"open-cluster-management.io/ocm/pkg/registration/register"
	awsirsa "open-cluster-management.io/ocm/pkg/registration/register/aws_irsa"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

// use ordered container since we need to run beforeAll to restart the hub with aws option
var _ = ginkgo.Describe("Joining Process for aws flow", ginkgo.Ordered, func() {
	var bootstrapKubeconfig string
	var managedClusterName string
	var hubKubeconfigSecret string
	var hubKubeconfigDir string

	ginkgo.BeforeAll(func() {
		// stop the hub and start new hub with the updated option
		stopHub()

		awsHubOption := hub.NewHubManagerOptions()
		awsHubOption.EnabledRegistrationDrivers = []string{operatorv1.CSRAuthType, operatorv1.AwsIrsaAuthType}
		awsHubOption.HubClusterArn = "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster1"
		awsHubOption.AutoApprovedARNPatterns = []string{"arn:aws:eks:us-west-2:123456789012:cluster/.*"}
		startHub(awsHubOption)

		// stop hub with awsOption and restart hub with default option
		ginkgo.DeferCleanup(func() {
			stopHub()
			startHub(hubOption)
		})
	})

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
				RegisterDriverOption: &registerfactory.Options{
					RegistrationAuth: operatorv1.AwsIrsaAuthType,
					AWSIRSAOption: &awsirsa.AWSOption{
						HubClusterArn:            hubClusterArn,
						ManagedClusterArn:        managedClusterArn,
						ManagedClusterRoleSuffix: managedClusterRoleSuffix,
					},
				},
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

			// Kubeconfig secret in integration test for AWS won't be able to connect to hub server, since it is not in the eks environment
			// So we only  ensure that generated hub-kubeconfig-secret has a correct format

			gomega.Eventually(func() error {
				secret, err := util.GetHubKubeConfigFromSecret(kubeClient, testNamespace, hubKubeconfigSecret)
				if err != nil {
					return err
				}

				hubKubeConfig, err := clientcmd.Load(secret.Data["kubeconfig"])
				if err != nil {
					return err
				}
				hubCurrentContext, ok := hubKubeConfig.Contexts[hubKubeConfig.CurrentContext]
				if !ok {
					return fmt.Errorf("context pointed to by the current-context property is missing")
				}
				hubCluster, ok := hubKubeConfig.Clusters[hubCurrentContext.Cluster]
				if !ok {
					return fmt.Errorf("cluster pointed to by the current-context is missing")
				}

				hubUser, ok := hubKubeConfig.AuthInfos[hubCurrentContext.AuthInfo]
				if !ok {
					return fmt.Errorf("user pointed to by the current-context is missing")
				}
				if hubUser.Exec.APIVersion != "client.authentication.k8s.io/v1beta1" {
					return fmt.Errorf("user exec plugin apiVersion is invalid")
				}
				if hubUser.Exec.Command != "/awscli/dist/aws" {
					return fmt.Errorf("user exec plugin command is invalid")
				}

				hubClusterAccountId, hubClusterName := helpers.GetAwsAccountIdAndClusterName(hubClusterArn)
				awsRegion := helpers.GetAwsRegion(hubClusterArn)

				if !contains(hubUser.Exec.Args, fmt.Sprintf("arn:aws:iam::%s:role/ocm-hub-%s", hubClusterAccountId, managedClusterRoleSuffix)) ||
					!contains(hubUser.Exec.Args, hubClusterName) ||
					!contains(hubUser.Exec.Args, awsRegion) {
					return fmt.Errorf("aws get-token command is not well formed")
				}

				bootstrapKubeConfig, err := clientcmd.LoadFromFile(agentOptions.BootstrapKubeconfig)
				if err != nil {
					return err
				}
				bootstrapCurrentContext, ok := bootstrapKubeConfig.Contexts[bootstrapKubeConfig.CurrentContext]
				if !ok {
					return fmt.Errorf("context pointed to by the current-context property is missing")
				}
				bootstrapCluster, ok := bootstrapKubeConfig.Clusters[bootstrapCurrentContext.Cluster]
				if !ok {
					return fmt.Errorf("cluster pointed to by the current-context is missing")
				}

				if bootstrapCurrentContext.Cluster != hubCurrentContext.Cluster {
					return fmt.Errorf("cluster name mismatch in hub kubeconfig and bootstrap kubeconfig")
				}
				if hubCluster.Server == "" {
					return fmt.Errorf("serverUrl missing in hub kubeconfig")
				}
				if hubCluster.Server != bootstrapCluster.Server {
					return fmt.Errorf("serverUrl mismatch in hub kubeconfig and bootstrap kubeconfig")
				}
				if hubCluster.CertificateAuthorityData == nil {
					return fmt.Errorf("certificateAuthorityData missing in hub kubeconfig")
				}
				if !bytes.Equal(hubCluster.CertificateAuthorityData, bootstrapCluster.CertificateAuthorityData) {
					return fmt.Errorf("certificateAuthorityData mismatch in hub kubeconfig and bootstrap kubeconfig")
				}

				if string(secret.Data[register.ClusterNameFile]) != managedClusterName {
					return fmt.Errorf("invalid clustername in hub-kubeconfig-secret")
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		})

		ginkgo.It("managedcluster should join successfully with auto approval of managed cluster in patterns for aws flow", func() {

			managedClusterArn := "arn:aws:eks:us-west-2:123456789012:cluster/managed-cluster1"
			managedClusterRoleSuffix := "7f8141296c75f2871e3d030f85c35692"
			hubClusterArn := "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster1"

			// run registration agent
			agentOptions := &spoke.SpokeAgentOptions{
				RegisterDriverOption: &registerfactory.Options{
					RegistrationAuth: operatorv1.AwsIrsaAuthType,
					AWSIRSAOption: &awsirsa.AWSOption{
						HubClusterArn:            hubClusterArn,
						ManagedClusterArn:        managedClusterArn,
						ManagedClusterRoleSuffix: managedClusterRoleSuffix,
					},
				},
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
			gomega.Eventually(func() bool {
				cluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return false
				}
				// Check for the presence of expected AWS IRSA annotations
				_, hasArn := cluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+awsirsa.ManagedClusterArn]
				_, hasRole := cluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+awsirsa.ManagedClusterIAMRoleSuffix]
				if hasArn && hasRole {
					return cluster.Spec.HubAcceptsClient
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})

		ginkgo.It("managedcluster should not join successfully with auto approval rejected of managed cluster not in patterns for aws flow", func() {

			managedClusterArn := "arn:aws:eks:us-west-1:123456789012:cluster/managed-cluster2"
			managedClusterRoleSuffix := "7f8141296c75f2871e3d030f85c35692"
			hubClusterArn := "arn:aws:eks:us-west-2:123456789012:cluster/hub-cluster1"

			// run registration agent
			agentOptions := &spoke.SpokeAgentOptions{
				RegisterDriverOption: &registerfactory.Options{
					RegistrationAuth: operatorv1.AwsIrsaAuthType,
					AWSIRSAOption: &awsirsa.AWSOption{
						HubClusterArn:            hubClusterArn,
						ManagedClusterArn:        managedClusterArn,
						ManagedClusterRoleSuffix: managedClusterRoleSuffix,
					},
				},
				BootstrapKubeconfig:      bootstrapKubeconfig,
				HubKubeconfigSecret:      hubKubeconfigSecret,
				ClusterHealthCheckPeriod: 1 * time.Minute,
			}
			commOptions := commonoptions.NewAgentOptions()
			commOptions.HubKubeconfigDir = hubKubeconfigDir
			commOptions.SpokeClusterName = managedClusterName

			cancel := runAgent("joiningtest", agentOptions, commOptions, spokeCfg)
			defer cancel()

			// The ManagedCluster CR should be created
			gomega.Eventually(func() error {
				_, err := util.GetManagedCluster(clusterClient, managedClusterName)
				return err
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Wait for the managed cluster to have AWS IRSA annotations populated
			gomega.Eventually(func() bool {
				cluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				if err != nil {
					return false
				}
				// Check for the presence of expected AWS IRSA annotations
				_, hasArn := cluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+awsirsa.ManagedClusterArn]
				_, hasRole := cluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+awsirsa.ManagedClusterIAMRoleSuffix]
				return hasArn && hasRole
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue(), "ManagedCluster should have AWS IRSA annotations")

			// The ManagedCluster CR should never be accepted
			gomega.Consistently(func() bool {
				cluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
				return cluster.Spec.HubAcceptsClient
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeFalse(), "HubAcceptsClient should never become true for ARN not matching auto-approval pattern")
		})
	}

	ginkgo.Context("without proxy", func() {
		ginkgo.BeforeEach(func() {
			bootstrapKubeconfig = bootstrapKubeConfigFile
		})
		assertJoiningSucceed()
	})

})

func contains(slice []string, value string) bool {
	stringSet := sets.New[string](slice...)
	return stringSet.Has(value)
}
