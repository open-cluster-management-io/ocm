package registration_test

import (
	"fmt"
	"path"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	certificates "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	clusterv1 "open-cluster-management.io/api/cluster/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

const expectedAnnotation = "open-cluster-management.io/automatically-accepted-on"

var _ = Describe("Cluster Auto Approval", func() {
	It("Cluster should be automatically approved", func(ctx SpecContext) {
		var err error

		managedClusterName := "autoapprovaltest-spokecluster"
		//#nosec G101
		hubKubeconfigSecret := "autoapprovaltest-hub-kubeconfig-secret"
		hubKubeconfigDir := path.Join(util.TestDir, "autoapprovaltest", "hub-kubeconfig")

		bootstrapFile := path.Join(util.TestDir, "bootstrap-autoapprovaltest", "kubeconfig")
		err = authn.CreateBootstrapKubeConfigWithUser(bootstrapFile, serverCertFile, securePort, util.AutoApprovalBootstrapUser)
		Expect(err).NotTo(HaveOccurred())

		agentOptions := &spoke.SpokeAgentOptions{
			BootstrapKubeconfig:      bootstrapFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
			RegisterDriverOption:     registerfactory.NewOptions(),
		}
		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName

		// run registration agent
		cancel := runAgent("autoapprovaltest", agentOptions, commOptions, spokeCfg)
		defer cancel()

		// after bootstrap the spokecluster should be accepted and its csr should be auto approved
		Eventually(func() error {
			cluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return err
			}

			if _, ok := cluster.Annotations[expectedAnnotation]; !ok {
				return fmt.Errorf("cluster should have accepted annotation")
			}

			if !cluster.Spec.HubAcceptsClient {
				return fmt.Errorf("cluster should be accepted")
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		var approvedCSR *certificates.CertificateSigningRequest
		Eventually(func() error {
			approvedCSR, err = util.FindAutoApprovedSpokeCSR(kubeClient, managedClusterName)
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// simulate hub cluster to fill a certificate
		now := time.Now()
		err = authn.FillCertificateToApprovedCSR(kubeClient, approvedCSR, now.UTC(), now.Add(30*time.Second).UTC())
		Expect(err).NotTo(HaveOccurred())

		// the hub kubeconfig secret should be filled after the csr is approved
		Eventually(func() error {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

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

	It("Cluster can be denied by manually after automatically approved", func(ctx SpecContext) {
		clusterName := "autoapprovaltest-spoke-cluster1"
		_, err := clusterClient.ClusterV1().ManagedClusters().Create(ctx, &clusterv1.ManagedCluster{
			ObjectMeta: v1.ObjectMeta{
				Name: clusterName,
			},
		}, v1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			cluster, err := util.GetManagedCluster(clusterClient, clusterName)
			if err != nil {
				return err
			}

			if _, ok := cluster.Annotations[expectedAnnotation]; !ok {
				return fmt.Errorf("cluster should have accepted annotation")
			}

			if !cluster.Spec.HubAcceptsClient {
				return fmt.Errorf("cluster should be accepted")
			}

			return nil
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		// we can deny the cluster after it is accepted
		Eventually(func() error {
			cluster, err := util.GetManagedCluster(clusterClient, clusterName)
			Expect(err).NotTo(HaveOccurred())

			updatedCluster := cluster.DeepCopy()
			updatedCluster.Spec.HubAcceptsClient = false
			_, err = clusterClient.ClusterV1().ManagedClusters().Update(ctx, updatedCluster, v1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		Consistently(func() error {
			cluster, err := util.GetManagedCluster(clusterClient, clusterName)
			if err != nil {
				return err
			}

			if _, ok := cluster.Annotations[expectedAnnotation]; !ok {
				return fmt.Errorf("cluster should have accepted annotation")
			}

			if cluster.Spec.HubAcceptsClient {
				return fmt.Errorf("cluster should be denied")
			}

			return nil
		}, 10*time.Second, eventuallyInterval).Should(Succeed())
	})
})
