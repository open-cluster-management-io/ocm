package registration_test

import (
	"context"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"

	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/hub"
	azureauth "open-cluster-management.io/ocm/pkg/registration/register/azure_auth"
	registerfactory "open-cluster-management.io/ocm/pkg/registration/register/factory"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

// The azure-auth flow can't reach Azure AD from envtest, so these specs exercise
// everything up to the token exchange: the ManagedCluster annotation, the hub driver's
// Accept/CreatePermissions/Cleanup against a real apiserver, and the exec-plugin
// kubeconfig persisted to the hub-kubeconfig secret.
//
// Ordered, since BeforeAll restarts the hub with the azure driver enabled.
var _ = ginkgo.Describe("Joining Process for azure flow", ginkgo.Ordered, func() {
	const autoApprovedAzureIDPattern = "^aaaaaaaa-.*$"

	ginkgo.BeforeAll(func() {
		stopHub()

		azureHubOption := hub.NewHubManagerOptions()
		azureHubOption.EnabledRegistrationDrivers = []string{operatorv1.CSRAuthType, operatorv1.AzureAuthType}
		azureHubOption.AutoApprovedAzureIDPatterns = []string{autoApprovedAzureIDPattern}
		startHub(azureHubOption)

		ginkgo.DeferCleanup(func() {
			stopHub()
			startHub(hubOption)
		})
	})

	// runAzureAgent starts a registration agent for a new managed cluster using the given
	// Azure AD object ID, and returns the cluster and hub-kubeconfig secret names.
	runAzureAgent := func(azureID string) (string, string) {
		postfix := rand.String(5)
		managedClusterName := fmt.Sprintf("azuretest-managedcluster-%s", postfix)
		hubKubeconfigSecret := fmt.Sprintf("azuretest-hub-kubeconfig-secret-%s", postfix)

		azureOption := azureauth.NewAzureOption()
		azureOption.Credential = string(operatorv1.AzureManagedIdentityCredential)
		azureOption.ManagedClusterAzureID = azureID

		agentOptions := &spoke.SpokeAgentOptions{
			RegisterDriverOption: &registerfactory.Options{
				RegistrationAuth: operatorv1.AzureAuthType,
				AzureOption:      azureOption,
			},
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}
		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = path.Join(util.TestDir, fmt.Sprintf("azuretest-%s", postfix), "hub-kubeconfig")
		commOptions.SpokeClusterName = managedClusterName

		cancel := runAgent("azuretest", agentOptions, commOptions, spokeCfg)
		ginkgo.DeferCleanup(cancel)

		gomega.Eventually(func() error {
			cluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return err
			}
			if got := cluster.Annotations[operatorv1.ClusterAnnotationsKeyPrefix+"/"+azureauth.ManagedClusterAzureIdentityAnnotation]; got != azureID {
				return fmt.Errorf("expected azure identity annotation %q, got %q", azureID, got)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		return managedClusterName, hubKubeconfigSecret
	}

	// assertBindings checks the three bindings CreatePermissions creates for a cluster.
	assertBindings := func(managedClusterName, azureID string) {
		gomega.Eventually(func() error {
			crb, err := kubeClient.RbacV1().ClusterRoleBindings().Get(context.TODO(),
				fmt.Sprintf("open-cluster-management:managedcluster:%s:azure", managedClusterName), metav1.GetOptions{})
			if err != nil {
				return err
			}
			if err := checkBinding(crb.ObjectMeta, crb.RoleRef, crb.Subjects, managedClusterName, azureID,
				fmt.Sprintf("open-cluster-management:managedcluster:%s", managedClusterName)); err != nil {
				return err
			}
			for name, role := range map[string]string{
				fmt.Sprintf("open-cluster-management:managedcluster:%s:registration:azure", managedClusterName): "open-cluster-management:managedcluster:registration",
				fmt.Sprintf("open-cluster-management:managedcluster:%s:work:azure", managedClusterName):         "open-cluster-management:managedcluster:work",
			} {
				rb, err := kubeClient.RbacV1().RoleBindings(managedClusterName).Get(context.TODO(), name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if err := checkBinding(rb.ObjectMeta, rb.RoleRef, rb.Subjects, managedClusterName, azureID, role); err != nil {
					return err
				}
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	}

	azureBindingCount := func(managedClusterName string) int {
		crbs, err := kubeClient.RbacV1().ClusterRoleBindings().List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s", clusterv1.ClusterNameLabelKey, managedClusterName)})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		rbs, err := kubeClient.RbacV1().RoleBindings(managedClusterName).List(context.TODO(), metav1.ListOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		count := 0
		for _, crb := range crbs.Items {
			if _, ok := crb.Labels[azureauth.AzureIdentityLabelKey]; ok {
				count++
			}
		}
		for _, rb := range rbs.Items {
			if _, ok := rb.Labels[azureauth.AzureIdentityLabelKey]; ok {
				count++
			}
		}
		return count
	}

	ginkgo.It("should join with manual approval, persist only the exec plugin config, and clean up on deny", func() {
		azureID := "bbbbbbbb-0000-0000-0000-000000000001"
		managedClusterName, hubKubeconfigSecret := runAzureAgent(azureID)

		// no CSR is created for the azure flow
		gomega.Consistently(func() error {
			_, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName)
			return err
		}, 3, eventuallyInterval).Should(gomega.HaveOccurred())

		// not auto-approved: the object ID doesn't match the pattern
		cluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(cluster.Spec.HubAcceptsClient).To(gomega.BeFalse())

		gomega.Expect(util.AcceptManagedCluster(clusterClient, managedClusterName)).To(gomega.Succeed())
		assertBindings(managedClusterName, azureID)

		gomega.Eventually(func() error {
			secret, err := util.GetHubKubeConfigFromSecret(kubeClient, testNamespace, hubKubeconfigSecret)
			if err != nil {
				return err
			}
			hubKubeConfig, err := clientcmd.Load(secret.Data["kubeconfig"])
			if err != nil {
				return err
			}
			hubContext, ok := hubKubeConfig.Contexts[hubKubeConfig.CurrentContext]
			if !ok {
				return fmt.Errorf("context pointed to by the current-context property is missing")
			}
			hubUser, ok := hubKubeConfig.AuthInfos[hubContext.AuthInfo]
			if !ok || hubUser.Exec == nil {
				return fmt.Errorf("exec plugin user pointed to by the current-context is missing")
			}
			if hubUser.Token != "" || hubUser.ClientCertificateData != nil || hubUser.ClientKeyData != nil {
				return fmt.Errorf("expected no credential material in the hub kubeconfig")
			}
			if hubUser.Exec.Command != "/ocm-agent" {
				return fmt.Errorf("unexpected exec plugin command %q", hubUser.Exec.Command)
			}
			if !contains(hubUser.Exec.Args, "get-azure-token") ||
				!contains(hubUser.Exec.Args, "--azure-credential=managed-identity-credential") {
				return fmt.Errorf("unexpected exec plugin args %v", hubUser.Exec.Args)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// denying the cluster removes the bindings
		gomega.Expect(setHubAcceptsClient(managedClusterName, false)).To(gomega.Succeed())
		gomega.Eventually(func() int {
			return azureBindingCount(managedClusterName)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Equal(0))
	})

	ginkgo.It("should be auto approved when the object ID matches a pattern", func() {
		azureID := "aaaaaaaa-0000-0000-0000-000000000001"
		managedClusterName, _ := runAzureAgent(azureID)

		gomega.Eventually(func() bool {
			cluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			return err == nil && cluster.Spec.HubAcceptsClient
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		assertBindings(managedClusterName, azureID)
	})

	ginkgo.It("should refuse to bind an identity already bound to a different cluster", func() {
		azureID := "bbbbbbbb-0000-0000-0000-000000000002"
		firstCluster, _ := runAzureAgent(azureID)
		gomega.Expect(util.AcceptManagedCluster(clusterClient, firstCluster)).To(gomega.Succeed())
		assertBindings(firstCluster, azureID)

		secondCluster, _ := runAzureAgent(azureID)
		gomega.Expect(util.AcceptManagedCluster(clusterClient, secondCluster)).To(gomega.Succeed())

		gomega.Eventually(func() error {
			cluster, err := util.GetManagedCluster(clusterClient, secondCluster)
			if err != nil {
				return err
			}
			cond := meta.FindStatusCondition(cluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted)
			if cond == nil || cond.Status != metav1.ConditionFalse || !strings.Contains(cond.Message, "already bound") {
				return fmt.Errorf("expected HubAccepted=False reporting the identity conflict, got %+v", cond)
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		gomega.Expect(azureBindingCount(secondCluster)).To(gomega.Equal(0))

		// the first cluster's grant is untouched
		assertBindings(firstCluster, azureID)
	})
})

func checkBinding(objMeta metav1.ObjectMeta, roleRef rbacv1.RoleRef, subjects []rbacv1.Subject,
	managedClusterName, azureID, roleName string) error {
	if roleRef.Kind != "ClusterRole" || roleRef.Name != roleName {
		return fmt.Errorf("%s: expected ClusterRole %q, got %+v", objMeta.Name, roleName, roleRef)
	}
	if len(subjects) != 1 || subjects[0].Kind != rbacv1.UserKind || subjects[0].Name != azureID {
		return fmt.Errorf("%s: expected a single User subject %q, got %+v", objMeta.Name, azureID, subjects)
	}
	if objMeta.Labels[clusterv1.ClusterNameLabelKey] != managedClusterName || objMeta.Labels[azureauth.AzureIdentityLabelKey] != azureID {
		return fmt.Errorf("%s: unexpected labels %v", objMeta.Name, objMeta.Labels)
	}
	return nil
}

func setHubAcceptsClient(managedClusterName string, accepted bool) error {
	return retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		cluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
		if err != nil {
			return err
		}
		cluster.Spec.HubAcceptsClient = accepted
		_, err = clusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), cluster, metav1.UpdateOptions{})
		return err
	})
}
