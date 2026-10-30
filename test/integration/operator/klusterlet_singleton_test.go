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

var _ = Describe("Klusterlet Singleton mode", func() {
	var cancel context.CancelFunc
	var klusterlet *operatorapiv1.Klusterlet
	var agentNamespace string
	var registrationManagementRoleName string
	var registrationManagedRoleName string
	var deploymentName string
	var saName string
	var workManagementRoleName string
	var workManagedRoleName string

	BeforeEach(func(ctx SpecContext) {
		var runCtx context.Context
		klusterlet = &operatorapiv1.Klusterlet{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("klusterlet-%s", rand.String(6)),
			},
			Spec: operatorapiv1.KlusterletSpec{
				ImagePullSpec: "quay.io/open-cluster-management/registration-operator",
				ExternalServerURLs: []operatorapiv1.ServerURL{
					{
						URL: "https://localhost",
					},
				},
				ClusterName: "testcluster",
				DeployOption: operatorapiv1.KlusterletDeployOption{
					Mode: operatorapiv1.InstallModeSingleton,
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
		agentNamespace = helpers.AgentNamespace(klusterlet)
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: agentNamespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
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
			deploymentName = fmt.Sprintf("%s-agent", klusterlet.Name)
			registrationManagementRoleName = fmt.Sprintf("open-cluster-management:management:%s-registration:agent", klusterlet.Name)
			workManagementRoleName = fmt.Sprintf("open-cluster-management:management:%s-work:agent", klusterlet.Name)
			registrationManagedRoleName = fmt.Sprintf("open-cluster-management:%s-registration:agent", klusterlet.Name)
			workManagedRoleName = fmt.Sprintf("open-cluster-management:%s-work:agent", klusterlet.Name)
			saName = fmt.Sprintf("%s-work-sa", klusterlet.Name)
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

				// 10 managed static manifests + 9 management static manifests + 4 networkpolicies + 2CRDs + 1 deployments
				if len(actual.Status.RelatedResources) != 26 {
					return fmt.Errorf("should get 26 relatedResources, actual got %v", len(actual.Status.RelatedResources))
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			// Check CRDs
			Eventually(func() bool {
				if _, err := apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(
					ctx, "appliedmanifestworks.work.open-cluster-management.io", metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(
					ctx, "clusterclaims.cluster.open-cluster-management.io", metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check clusterrole/clusterrolebinding
			Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, registrationManagedRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, workManagedRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, registrationManagedRoleName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, workManagedRoleName, metav1.GetOptions{}); err != nil {
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
				sa, err := kubeClient.CoreV1().ServiceAccounts(agentNamespace).Get(ctx, saName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				_, present := sa.ObjectMeta.Annotations[util.IrsaAnnotationKey]
				return !present
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check deployment
			Eventually(func() bool {
				deployment, err := kubeClient.AppsV1().Deployments(agentNamespace).Get(ctx, deploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				return !util.AllCommandLineOptionsPresent(*deployment) && !util.AwsCliSpecificVolumesMounted(*deployment)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check addon namespace
			Eventually(func() bool {
				if _, err := kubeClient.CoreV1().Namespaces().Get(ctx, helpers.DefaultAddonNamespace, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "Applied", "KlusterletApplied", metav1.ConditionTrue)
		})
	})
})
