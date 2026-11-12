package operator

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = Describe("Klusterlet Hosted mode", func() {
	var cancel context.CancelFunc
	var klusterlet *operatorapiv1.Klusterlet
	var klusterletNamespace string
	var agentNamespace string
	var registrationManagementRoleName string
	var registrationManagedRoleName string
	var registrationDeploymentName string
	var registrationSAName string
	var workManagementRoleName string
	var workManagedRoleName string
	var workDeploymentName string
	var workSAName string

	BeforeEach(func(ctx SpecContext) {
		var runCtx context.Context
		klusterlet = &operatorapiv1.Klusterlet{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("klusterlet-%s", rand.String(6)),
			},
			Spec: operatorapiv1.KlusterletSpec{
				RegistrationImagePullSpec: "quay.io/open-cluster-management/registration",
				WorkImagePullSpec:         "quay.io/open-cluster-management/work",
				ExternalServerURLs: []operatorapiv1.ServerURL{
					{
						URL: "https://localhost",
					},
				},
				ClusterName: "testcluster",
				DeployOption: operatorapiv1.KlusterletDeployOption{
					Mode: operatorapiv1.InstallModeHosted,
				},
				RegistrationConfiguration: &operatorapiv1.RegistrationConfiguration{
					FeatureGates: []operatorapiv1.FeatureGate{
						{
							Feature: "NetworkPolicies",
							Mode:    operatorapiv1.FeatureGateModeTypeEnable,
						},
					},
				},
			},
		}
		klusterletNamespace = helpers.KlusterletNamespace(klusterlet)
		agentNamespace = helpers.AgentNamespace(klusterlet)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: agentNamespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		// Create the external managed kubeconfig secret
		managedKubeconfigSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      helpers.ExternalManagedKubeConfig,
				Namespace: agentNamespace,
			},
			Data: map[string][]byte{
				"kubeconfig": util.NewKubeConfig(hostedRestConfig),
			},
		}
		_, err = kubeClient.CoreV1().Secrets(agentNamespace).Create(ctx, managedKubeconfigSecret, metav1.CreateOptions{})
		Expect(err).ToNot(HaveOccurred())

		runCtx, cancel = context.WithCancel(context.Background())
		go startKlusterletOperator(runCtx)
	})

	AfterEach(func(ctx SpecContext) {
		err := kubeClient.CoreV1().Namespaces().Delete(ctx, agentNamespace, metav1.DeleteOptions{})
		Expect(err).NotTo(HaveOccurred())

		if cancel != nil {
			cancel()
		}
	})

	Context("Deploy and clean klusterlet component", func() {
		BeforeEach(func() {
			registrationDeploymentName = fmt.Sprintf("%s-registration-agent", klusterlet.Name)
			workDeploymentName = fmt.Sprintf("%s-work-agent", klusterlet.Name)
			registrationManagementRoleName = fmt.Sprintf("open-cluster-management:management:%s-registration:agent", klusterlet.Name)
			workManagementRoleName = fmt.Sprintf("open-cluster-management:management:%s-work:agent", klusterlet.Name)
			registrationManagedRoleName = fmt.Sprintf("open-cluster-management:%s-registration:agent", klusterlet.Name)
			workManagedRoleName = fmt.Sprintf("open-cluster-management:%s-work:agent", klusterlet.Name)
			registrationSAName = fmt.Sprintf("%s-registration-sa", klusterlet.Name)
			workSAName = fmt.Sprintf("%s-work-sa", klusterlet.Name)

		})

		AfterEach(func(ctx SpecContext) {
			Expect(operatorClient.OperatorV1().Klusterlets().Delete(ctx, klusterlet.Name, metav1.DeleteOptions{})).To(BeNil())
		})

		It("should have expected resource created successfully", func(ctx SpecContext) {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(ctx, klusterlet, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// Check if relatedResources are correct
			Eventually(func() error {
				actual, err := operatorClient.OperatorV1().Klusterlets().Get(ctx, klusterlet.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// 11 managed static manifests + 11 management static manifests + 4 networkpolicies +
				// 2CRDs + 2 deployments(2 duplicated CRDs, but status also recorded in the klusterlet's status)
				if len(actual.Status.RelatedResources) != 30 {
					return fmt.Errorf("should get 30 relatedResources, actual got %v", len(actual.Status.RelatedResources))
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			// Check CRDs
			Eventually(func() bool {
				if _, err := hostedAPIExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(
					ctx, "appliedmanifestworks.work.open-cluster-management.io", metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := hostedAPIExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(
					ctx, "clusterclaims.cluster.open-cluster-management.io", metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check clusterrole/clusterrolebinding
			Eventually(func() bool {
				if _, err := hostedKubeClient.RbacV1().ClusterRoles().Get(
					ctx, registrationManagedRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := hostedKubeClient.RbacV1().ClusterRoles().Get(ctx, workManagedRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := hostedKubeClient.RbacV1().ClusterRoleBindings().Get(ctx, registrationManagedRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := hostedKubeClient.RbacV1().ClusterRoleBindings().Get(ctx, workManagedRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check role/rolebinding
			Eventually(func() bool {
				if _, err := kubeClient.RbacV1().Roles(agentNamespace).Get(ctx, registrationManagementRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := kubeClient.RbacV1().Roles(agentNamespace).Get(ctx, workManagementRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := kubeClient.RbacV1().RoleBindings(agentNamespace).Get(ctx, registrationManagementRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := kubeClient.RbacV1().RoleBindings(agentNamespace).Get(ctx, workManagementRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			// Check extension apiserver rolebinding
			Eventually(func() bool {
				if _, err := kubeClient.RbacV1().RoleBindings("kube-system").Get(ctx, registrationManagementRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := kubeClient.RbacV1().RoleBindings("kube-system").Get(ctx, workManagementRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check service account
			Eventually(func() bool {
				if _, err := kubeClient.CoreV1().ServiceAccounts(agentNamespace).Get(ctx, registrationSAName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := kubeClient.CoreV1().ServiceAccounts(agentNamespace).Get(ctx, workSAName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := hostedKubeClient.CoreV1().ServiceAccounts(klusterletNamespace).Get(ctx, registrationSAName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := hostedKubeClient.CoreV1().ServiceAccounts(klusterletNamespace).Get(ctx, workSAName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check deployment
			Eventually(func() bool {
				if _, err := kubeClient.AppsV1().Deployments(agentNamespace).Get(ctx, registrationDeploymentName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := kubeClient.AppsV1().Deployments(agentNamespace).Get(ctx, workDeploymentName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check addon namespace
			Eventually(func() bool {
				if _, err := hostedKubeClient.CoreV1().Namespaces().Get(ctx, helpers.DefaultAddonNamespace, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "Applied", "KlusterletApplied", metav1.ConditionTrue)
		})

		It("should render self-report flags and RBAC when reportHostingCluster is enabled (KEP-188)", func(ctx SpecContext) {
			// Unique namespace: the shared default may still be terminating from the prior spec.
			klusterlet.Spec.Namespace = fmt.Sprintf("open-cluster-management-%s", klusterlet.Name)
			klusterlet.Spec.DeployOption.ReportHostingCluster = operatorapiv1.ReportHostingClusterModeEnable
			klusterlet.Spec.DeployOption.Hosted = &operatorapiv1.KlusterletHostedConfiguration{
				ManagementClusterName: "management-cluster-x",
			}
			_, err := operatorClient.OperatorV1().Klusterlets().Create(ctx, klusterlet, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() []string {
				deployment, err := kubeClient.AppsV1().Deployments(agentNamespace).Get(
					ctx, registrationDeploymentName, metav1.GetOptions{})
				if err != nil {
					return nil
				}
				return deployment.Spec.Template.Spec.Containers[0].Args
			}, eventuallyTimeout, eventuallyInterval).Should(ContainElements(
				"--report-hosting-cluster=true", "--hosting-cluster-name=management-cluster-x"))

			Eventually(func() error {
				role, err := hostedKubeClient.RbacV1().ClusterRoles().Get(
					ctx, registrationManagedRoleName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				for _, rule := range role.Rules {
					if len(rule.ResourceNames) == 1 && rule.ResourceNames[0] == "hosting-cluster.open-cluster-management.io" {
						return nil
					}
				}
				return fmt.Errorf("no rule scoped to the reserved hosting-cluster claim: %+v", role.Rules)
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})
	})
})
