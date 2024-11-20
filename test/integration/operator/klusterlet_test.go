package operator

import (
	"context"
	"fmt"
	"strings"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"

	ocmfeature "open-cluster-management.io/api/feature"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/pkg/operator/operators/klusterlet"
	"open-cluster-management.io/ocm/test/integration/util"
)

func startKlusterletOperator(ctx context.Context) {
	o := &klusterlet.Options{EnableSyncLabels: true}
	err := o.RunKlusterletOperator(ctx, &controllercmd.ControllerContext{
		KubeConfig:    restConfig,
		EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

var _ = ginkgo.Describe("Klusterlet", func() {
	var cancel context.CancelFunc
	var klusterlet *operatorapiv1.Klusterlet
	var hubKubeConfigSecret *corev1.Secret
	var klusterletNamespace string
	var registrationManagementRoleName string
	var registrationManagedRoleName string
	var addonManagementRoleName, addonManagementRoleName2 string
	var workExecutionRoleName string
	var workAggregateRoleName string
	var workExecutionRoleBindingName string
	var registrationDeploymentName string
	var registrationSAName string
	var workManagementRoleName string
	var workManagedRoleName string
	var workDeploymentName string
	var workSAName string
	var agentLabelSelector string

	ginkgo.BeforeEach(func() {
		var ctx context.Context

		klusterletNamespace = fmt.Sprintf("open-cluster-management-%s", rand.String(6))
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: klusterletNamespace,
			},
		}
		_, err := kubeClient.CoreV1().Namespaces().Create(context.Background(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		klusterlet = &operatorapiv1.Klusterlet{
			ObjectMeta: metav1.ObjectMeta{
				Name:   fmt.Sprintf("klusterlet-%s", rand.String(6)),
				Labels: map[string]string{"test": "123", "component": "klusterlet", "123": "312"},
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
				Namespace:   klusterletNamespace,
			},
		}

		agentLabelSelector = metav1.FormatLabelSelector(&metav1.LabelSelector{
			MatchLabels: helpers.GetKlusterletAgentLabels(klusterlet),
		})

		hubKubeConfigSecret = &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      helpers.HubKubeConfig,
				Namespace: klusterletNamespace,
			},
			Data: map[string][]byte{
				"placeholder": []byte("placeholder"),
			},
		}
		_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Create(context.Background(), hubKubeConfigSecret, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ctx, cancel = context.WithCancel(context.Background())
		go startKlusterletOperator(ctx)
	})

	ginkgo.AfterEach(func() {
		err := kubeClient.CoreV1().Namespaces().Delete(context.Background(), klusterletNamespace, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		if cancel != nil {
			cancel()
		}
	})

	ginkgo.Context("Deploy and clean klusterlet component", func() {
		ginkgo.BeforeEach(func() {
			registrationDeploymentName = fmt.Sprintf("%s-registration-agent", klusterlet.Name)
			workDeploymentName = fmt.Sprintf("%s-work-agent", klusterlet.Name)
			registrationManagementRoleName = fmt.Sprintf("open-cluster-management:management:%s-registration:agent", klusterlet.Name)
			workManagementRoleName = fmt.Sprintf("open-cluster-management:management:%s-work:agent", klusterlet.Name)

			registrationManagedRoleName = fmt.Sprintf("open-cluster-management:%s-registration:agent", klusterlet.Name)
			addonManagementRoleName2 = fmt.Sprintf("open-cluster-management:%s-registration:addon-management", klusterlet.Name)
			addonManagementRoleName = fmt.Sprintf("open-cluster-management:management:%s-registration:addon-management", klusterlet.Name)
			workExecutionRoleName = fmt.Sprintf("open-cluster-management:%s-work:execution", klusterlet.Name)
			workManagedRoleName = fmt.Sprintf("open-cluster-management:%s-work:agent", klusterlet.Name)
			workAggregateRoleName = fmt.Sprintf("open-cluster-management:%s-work:aggregate", klusterlet.Name)
			workExecutionRoleBindingName = fmt.Sprintf("open-cluster-management:%s-work:execution-admin", klusterlet.Name)

			registrationSAName = fmt.Sprintf("%s-registration-sa", klusterlet.Name)
			workSAName = fmt.Sprintf("%s-work-sa", klusterlet.Name)
		})

		ginkgo.AfterEach(func() {
			gomega.Expect(operatorClient.OperatorV1().Klusterlets().Delete(context.Background(), klusterlet.Name, metav1.DeleteOptions{})).To(gomega.BeNil())
		})

		ginkgo.It("should have expected resource created successfully", func() {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Check if relatedResources are correct
			gomega.Eventually(func() error {
				actual, err := operatorClient.OperatorV1().Klusterlets().Get(context.Background(), klusterlet.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// 11 managed static manifests + 11 management static manifests + 2CRDs + 2 deployments(2 duplicated SAs)
				if len(actual.Status.RelatedResources) != 24 {
					return fmt.Errorf("should get 26 relatedResources, actual got %v", len(actual.Status.RelatedResources))
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			// Check CRDs
			gomega.Eventually(func() bool {
				if _, err := apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(
					context.Background(), "appliedmanifestworks.work.open-cluster-management.io", metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				if _, err := apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(
					context.Background(), "clusterclaims.cluster.open-cluster-management.io", metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check clusterrole/clusterrolebinding
			gomega.Eventually(func() error {
				clusterRoles, err := kubeClient.RbacV1().ClusterRoles().List(context.Background(),
					metav1.ListOptions{LabelSelector: agentLabelSelector})
				if err != nil {
					return fmt.Errorf("unable to list cluster roles: %v", err)
				}
				if len(clusterRoles.Items) != 6 {
					return fmt.Errorf("expected 6 clusterRoles.Items, got %v", len(clusterRoles.Items))
				}
				for _, clusterRole := range clusterRoles.Items {
					if clusterRole.GetName() != registrationManagedRoleName &&
						clusterRole.GetName() != workManagedRoleName &&
						clusterRole.GetName() != addonManagementRoleName &&
						clusterRole.GetName() != addonManagementRoleName2 &&
						clusterRole.GetName() != workExecutionRoleName &&
						clusterRole.GetName() != workAggregateRoleName {
						return fmt.Errorf("unexpected clusterRole %s", clusterRole.GetName())
					}
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			gomega.Eventually(func() bool {
				clusterRoleBindings, err := kubeClient.RbacV1().ClusterRoleBindings().List(context.Background(),
					metav1.ListOptions{LabelSelector: agentLabelSelector})
				if err != nil {
					return false
				}
				if len(clusterRoleBindings.Items) != 6 {
					return false
				}
				for _, clusterRoleBinding := range clusterRoleBindings.Items {
					if clusterRoleBinding.GetName() != registrationManagedRoleName &&
						clusterRoleBinding.GetName() != workManagedRoleName &&
						clusterRoleBinding.GetName() != workExecutionRoleBindingName &&
						clusterRoleBinding.GetName() != addonManagementRoleName &&
						clusterRoleBinding.GetName() != addonManagementRoleName2 &&
						clusterRoleBinding.GetName() != workAggregateRoleName {
						return false
					}
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check role/rolebinding and extension apiserver rolebinding
			gomega.Eventually(func() bool {
				roles, err := kubeClient.RbacV1().Roles(klusterletNamespace).List(context.Background(),
					metav1.ListOptions{LabelSelector: agentLabelSelector})
				if err != nil {
					return false
				}
				if len(roles.Items) != 2 {
					return false
				}
				for _, role := range roles.Items {
					if role.GetName() != registrationManagementRoleName &&
						role.GetName() != workManagementRoleName {
						return false
					}
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			gomega.Eventually(func() bool {
				roleBindings, err := kubeClient.RbacV1().RoleBindings(metav1.NamespaceAll).List(context.Background(),
					metav1.ListOptions{LabelSelector: agentLabelSelector})
				if err != nil {
					return false
				}
				if len(roleBindings.Items) != 4 {
					return false
				}
				for _, roleBinding := range roleBindings.Items {
					if roleBinding.GetNamespace() != klusterletNamespace &&
						roleBinding.GetNamespace() != "kube-system" {
						return false
					}

					if roleBinding.GetName() != registrationManagementRoleName &&
						roleBinding.GetName() != workManagementRoleName {
						return false
					}
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check service account
			gomega.Eventually(func() bool {
				serviceaccouts, err := kubeClient.CoreV1().ServiceAccounts(klusterletNamespace).List(context.Background(),
					metav1.ListOptions{LabelSelector: agentLabelSelector})
				if err != nil {
					return false
				}
				if len(serviceaccouts.Items) != 2 {
					return false
				}
				for _, serviceAccount := range serviceaccouts.Items {
					if serviceAccount.GetName() != registrationSAName &&
						serviceAccount.GetName() != workSAName {
						return false
					}
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check deployment
			gomega.Eventually(func() bool {
				deployments, err := kubeClient.AppsV1().Deployments(klusterletNamespace).List(context.Background(),
					metav1.ListOptions{LabelSelector: agentLabelSelector})
				if err != nil {
					return false
				}
				if len(deployments.Items) != 2 {
					return false
				}
				for _, deployment := range deployments.Items {
					if deployment.GetName() != registrationDeploymentName &&
						deployment.GetName() != workDeploymentName {
						return false
					}
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check addon namespace
			gomega.Eventually(func() bool {
				namespaces, err := kubeClient.CoreV1().Namespaces().List(context.Background(), metav1.ListOptions{LabelSelector: agentLabelSelector})
				if err != nil {
					return false
				}
				if len(namespaces.Items) != 2 {
					return false
				}
				for _, namespace := range namespaces.Items {
					if namespace.GetName() != klusterletNamespace &&
						namespace.GetName() != helpers.DefaultAddonNamespace {
						return false
					}
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "Applied", "KlusterletApplied", metav1.ConditionTrue)
		})

		ginkgo.It("Deployment should be added nodeSelector and toleration when add nodePlacement into klusterlet", func() {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Check deployment without nodeSelector and toleration
			gomega.Eventually(func() bool {
				deployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), registrationDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				if len(deployment.Spec.Template.Spec.NodeSelector) != 0 {
					return false
				}

				if len(deployment.Spec.Template.Spec.Tolerations) != 0 {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			gomega.Eventually(func() error {
				KlusterletObj, err := operatorClient.OperatorV1().Klusterlets().Get(context.Background(), klusterlet.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				KlusterletObj.Spec.NodePlacement = operatorapiv1.NodePlacement{
					NodeSelector: map[string]string{infraNodeLabel: ""},
					Tolerations: []corev1.Toleration{
						{
							Key:      infraNodeLabel,
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				}
				_, err = operatorClient.OperatorV1().Klusterlets().Update(context.Background(), KlusterletObj, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check deployment with nodeSelector and toleration
			gomega.Eventually(func() bool {
				deployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), registrationDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				if len(deployment.Spec.Template.Spec.NodeSelector) == 0 {
					return false
				}
				if _, ok := deployment.Spec.Template.Spec.NodeSelector[infraNodeLabel]; !ok {
					return false
				}
				if len(deployment.Spec.Template.Spec.Tolerations) == 0 {
					return false
				}
				for _, toleration := range deployment.Spec.Template.Spec.Tolerations {
					if toleration.Key == infraNodeLabel {
						return true
					}
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})

		ginkgo.It("Deployment should have priorityclass configurated when setting priorityclass in klusterlet", func() {
			priorityClassName := "test-priority-class"
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Check deployment without priorityclass
			gomega.Eventually(func() bool {
				deployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), registrationDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				if len(deployment.Spec.Template.Spec.PriorityClassName) != 0 {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			gomega.Eventually(func() error {
				KlusterletObj, err := operatorClient.OperatorV1().Klusterlets().Get(context.Background(), klusterlet.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}

				KlusterletObj.Spec.PriorityClassName = priorityClassName
				_, err = operatorClient.OperatorV1().Klusterlets().Update(context.Background(), KlusterletObj, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check deployment with priorityclass
			gomega.Eventually(func() bool {
				deployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), registrationDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				return deployment.Spec.Template.Spec.PriorityClassName == priorityClassName
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})

		ginkgo.It("should have correct registration deployment when server url is empty", func() {
			klusterlet.Spec.ExternalServerURLs = []operatorapiv1.ServerURL{}
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Eventually(func() bool {
				if _, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), registrationDeploymentName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			deployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), registrationDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(len(deployment.Spec.Template.Spec.Containers)).Should(gomega.Equal(1))
			// external-server-url should not be set
			for _, arg := range deployment.Spec.Template.Spec.Containers[0].Args {
				gomega.Expect(strings.Contains(arg, "--spoke-external-server-urls")).NotTo(gomega.BeTrue())
			}
		})

		ginkgo.It("should have correct work deployment until HubConnectionDegraded is False when clusterName is empty", func() {
			klusterlet.Spec.ClusterName = "" // The clusterName is empty, the controller get clusterName from hubKubeConfigSecret.
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// do not create work deployment if ClusterName is empty
			gomega.Eventually(func() bool {
				_, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// get correct work deployment when get valid cluster name from hub kubeConfig secret
			hubSecret, err := kubeClient.CoreV1().Secrets(klusterletNamespace).Get(context.Background(), helpers.HubKubeConfig, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Update hub secret with cluster name
			hubSecret.Data["cluster-name"] = []byte("testcluster")
			_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Update(context.Background(), hubSecret, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Eventually(func() bool {
				if _, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			deployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Expect(len(deployment.Spec.Template.Spec.Containers)).Should(gomega.Equal(1))

			for _, arg := range deployment.Spec.Template.Spec.Containers[0].Args {
				if strings.HasPrefix(arg, "--spoke-cluster-name") {
					gomega.Expect(arg).Should(gomega.Equal("--spoke-cluster-name=testcluster"))
				}
			}

			// Update hub config secret to trigger work deployment update
			hubSecret, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Get(context.Background(), helpers.HubKubeConfig, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Update hub secret
			hubSecret.Data["kubeconfig"] = []byte("update dummy")
			_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Update(context.Background(), hubSecret, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Check that work deployment is updated
			gomega.Eventually(func() bool {
				deployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				for _, arg := range deployment.Spec.Template.Spec.Containers[0].Args {
					if arg == "--spoke-cluster-name=testcluster" {
						return true
					}
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})

		ginkgo.It("Should change work deployment replicas to 0 when hubConfigSecret is missing", func() {
			klusterlet.Spec.ClusterName = "testcluster"
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// replicas of work deployment should be 0 if hubConfigSecret is missing
			err = kubeClient.CoreV1().Secrets(klusterletNamespace).Delete(context.Background(), helpers.HubKubeConfig, metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				deployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if deployment.Spec.Replicas == nil {
					return err
				}
				if *deployment.Spec.Replicas != 0 {
					return fmt.Errorf("replicas of work deployment should be 0")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// recreate the hubConfigSecret, replicas of work deployment should not be 0
			hubKubeConfigSecret = &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      helpers.HubKubeConfig,
					Namespace: klusterletNamespace,
				},
				Data: map[string][]byte{
					"kubeconfig": []byte("update dummy"),
				},
			}
			_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Create(context.Background(), hubKubeConfigSecret, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				deployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if deployment.Spec.Replicas == nil {
					return err
				}
				if *deployment.Spec.Replicas == 0 {
					return fmt.Errorf("replicas of work deployment should not be 0")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
		})

		ginkgo.It("Deployment should be updated when klusterlet is changed", func() {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Eventually(func() bool {
				if _, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check if generations are correct
			gomega.Eventually(func() bool {
				actual, err := operatorClient.OperatorV1().Klusterlets().Get(context.Background(), klusterlet.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}

				if actual.Generation != actual.Status.ObservedGeneration {
					return false
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			gomega.Eventually(func() error {
				klusterlet, err = operatorClient.OperatorV1().Klusterlets().Get(context.Background(), klusterlet.Name, metav1.GetOptions{})
				if err != nil {
					return err
				}
				klusterlet.Spec.ClusterName = "cluster2"
				_, err = operatorClient.OperatorV1().Klusterlets().Update(context.Background(), klusterlet, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

			gomega.Eventually(func() bool {
				actual, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				gomega.Expect(len(actual.Spec.Template.Spec.Containers)).Should(gomega.Equal(1))
				// klusterlet has no condition, replica is 0
				gomega.Expect(actual.Status.Replicas).Should(gomega.Equal(int32(0)))
				gomega.Expect(len(actual.Spec.Template.Spec.Containers[0].Args)).Should(gomega.Equal(9))
				return actual.Spec.Template.Spec.Containers[0].Args[2] != "--spoke-cluster-name=cluster2"
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			gomega.Eventually(func() bool {
				actual, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), registrationDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				gomega.Expect(len(actual.Spec.Template.Spec.Containers)).Should(gomega.Equal(1))
				gomega.Expect(len(actual.Spec.Template.Spec.Containers[0].Args)).Should(gomega.Equal(6))
				return actual.Spec.Template.Spec.Containers[0].Args[2] == "--spoke-cluster-name=cluster2"
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check if generations are correct
			gomega.Eventually(func() bool {
				actual, err := operatorClient.OperatorV1().Klusterlets().Get(context.Background(), klusterlet.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}

				if actual.Generation != actual.Status.ObservedGeneration {
					return false
				}

				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})

		ginkgo.It("Deployment should be reconciled when manually updated", func() {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Eventually(func() bool {
				if _, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			workDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			workDeployment.Spec.Template.Spec.Containers[0].Image = "test:latest"
			_, err = kubeClient.AppsV1().Deployments(klusterletNamespace).Update(context.Background(), workDeployment, metav1.UpdateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			gomega.Eventually(func() bool {
				workDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}
				if workDeployment.Spec.Template.Spec.Containers[0].Image != "quay.io/open-cluster-management/work" {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check if generations are correct
			gomega.Eventually(func() bool {
				actual, err := operatorClient.OperatorV1().Klusterlets().Get(context.Background(), klusterlet.Name, metav1.GetOptions{})
				if err != nil {
					return false
				}

				workDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				deploymentGeneration := helpers.NewGenerationStatus(appsv1.SchemeGroupVersion.WithResource("deployments"), workDeployment)
				actualGeneration := helpers.FindGenerationStatus(actual.Status.Generations, deploymentGeneration)
				return deploymentGeneration.LastGeneration == actualGeneration.LastGeneration
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})

		ginkgo.It("Deployment should have correct replica", func() {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			bootStrapSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      helpers.BootstrapHubKubeConfig,
					Namespace: klusterletNamespace,
				},
				Data: map[string][]byte{
					"kubeconfig": util.NewKubeConfig(restConfig),
				},
			}
			_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Create(context.Background(), bootStrapSecret, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Update hub secret with cluster name and kubeconfig
			hubSecret, err := kubeClient.CoreV1().Secrets(klusterletNamespace).Get(context.Background(), helpers.HubKubeConfig, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			hubSecret.Data["cluster-name"] = []byte("testcluster")
			hubSecret.Data["kubeconfig"] = util.NewKubeConfig(restConfig)
			_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Update(context.Background(), hubSecret, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Expect 1 replica since no nodes exists currently
			gomega.Eventually(func() int32 {
				workDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
				if err != nil {
					return 0
				}

				return *workDeployment.Spec.Replicas
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.Equal(int32(1)))

			// Create master nodes and recreate klusterlet
			_, err = kubeClient.CoreV1().Nodes().Create(
				context.Background(),
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node1", Labels: map[string]string{"node-role.kubernetes.io/master": ""}}},
				metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			_, err = kubeClient.CoreV1().Nodes().Create(
				context.Background(),
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node2", Labels: map[string]string{"node-role.kubernetes.io/master": ""}}},
				metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			_, err = kubeClient.CoreV1().Nodes().Create(
				context.Background(),
				&corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node3", Labels: map[string]string{"node-role.kubernetes.io/master": ""}}},
				metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// update klusterlet to trigger another reconcile
			klusterlet, err = operatorClient.OperatorV1().Klusterlets().Get(context.Background(), klusterlet.Name, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			klusterlet.Labels = map[string]string{"test": "test"}
			_, err = operatorClient.OperatorV1().Klusterlets().Update(context.Background(), klusterlet, metav1.UpdateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				workDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if *workDeployment.Spec.Replicas != 3 {
					return fmt.Errorf("expect 3 deployment but got %d", *workDeployment.Spec.Replicas)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("Deployment should be added hostAliases when add hostAlias into klusterlet", func() {
			var hubApiServerHostAliasIP = "11.22.33.44"
			var hubApiServerHostAliasHostname = "open-cluster-management.io"

			klusterlet.Spec.HubApiServerHostAlias = &operatorapiv1.HubApiServerHostAlias{
				IP:       hubApiServerHostAliasIP,
				Hostname: hubApiServerHostAliasHostname,
			}

			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// Check registration agent deployment
			gomega.Eventually(func() bool {
				actual, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), registrationDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				gomega.Expect(len(actual.Spec.Template.Spec.HostAliases)).Should(gomega.Equal(1))
				gomega.Expect(actual.Spec.Template.Spec.HostAliases[0].IP).Should(gomega.Equal(hubApiServerHostAliasIP))
				gomega.Expect(len(actual.Spec.Template.Spec.HostAliases[0].Hostnames)).Should(gomega.Equal(1))
				gomega.Expect(actual.Spec.Template.Spec.HostAliases[0].Hostnames[0]).Should(gomega.Equal(hubApiServerHostAliasHostname))
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check work agent deployment
			gomega.Eventually(func() bool {
				actual, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
				if err != nil {
					return false
				}

				gomega.Expect(len(actual.Spec.Template.Spec.HostAliases)).Should(gomega.Equal(1))
				gomega.Expect(actual.Spec.Template.Spec.HostAliases[0].IP).Should(gomega.Equal(hubApiServerHostAliasIP))
				gomega.Expect(len(actual.Spec.Template.Spec.HostAliases[0].Hostnames)).Should(gomega.Equal(1))
				gomega.Expect(actual.Spec.Template.Spec.HostAliases[0].Hostnames[0]).Should(gomega.Equal(hubApiServerHostAliasHostname))
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
		})
	})

	ginkgo.Context("klusterlet statuses", func() {
		ginkgo.BeforeEach(func() {
			registrationDeploymentName = fmt.Sprintf("%s-registration-agent", klusterlet.Name)
			workDeploymentName = fmt.Sprintf("%s-work-agent", klusterlet.Name)
		})
		ginkgo.AfterEach(func() {
			gomega.Expect(operatorClient.OperatorV1().Klusterlets().Delete(context.Background(), klusterlet.Name, metav1.DeleteOptions{})).To(gomega.BeNil())
		})
		ginkgo.It("should have correct degraded conditions", func() {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "HubConnectionDegraded", "BootstrapSecretMissing,HubKubeConfigMissing", metav1.ConditionTrue)

			// Create a bootstrap secret and make sure the kubeconfig can work
			bootStrapSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      helpers.BootstrapHubKubeConfig,
					Namespace: klusterletNamespace,
				},
				Data: map[string][]byte{
					"kubeconfig": util.NewKubeConfig(restConfig),
				},
			}
			_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Create(context.Background(), bootStrapSecret, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertKlusterletCondition(
				klusterlet.Name, operatorClient, "HubConnectionDegraded", "BootstrapSecretFunctional,HubKubeConfigMissing", metav1.ConditionTrue)

			hubSecret, err := kubeClient.CoreV1().Secrets(klusterletNamespace).Get(context.Background(), helpers.HubKubeConfig, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Update hub secret and make sure the kubeconfig can work
			hubSecret = hubSecret.DeepCopy()
			hubSecret.Data["cluster-name"] = []byte("testcluster")
			hubSecret.Data["kubeconfig"] = util.NewKubeConfig(restConfig)
			hubSecret, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Update(context.Background(), hubSecret, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "HubConnectionDegraded", "HubConnectionFunctional", metav1.ConditionFalse)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "Applied", "KlusterletApplied", metav1.ConditionTrue)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "RegistrationDesiredDegraded", "UnavailablePods", metav1.ConditionTrue)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "WorkDesiredDegraded", "UnavailablePods", metav1.ConditionTrue)

			// Update replica of deployment
			registrationDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(
				context.Background(), registrationDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			registrationDeployment = registrationDeployment.DeepCopy()
			registrationDeployment.Status.AvailableReplicas = 3
			registrationDeployment.Status.Replicas = 3
			registrationDeployment.Status.ReadyReplicas = 3
			_, err = kubeClient.AppsV1().Deployments(klusterletNamespace).UpdateStatus(context.Background(), registrationDeployment, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			workDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			workDeployment = workDeployment.DeepCopy()
			workDeployment.Status.AvailableReplicas = 3
			workDeployment.Status.Replicas = 3
			workDeployment.Status.ReadyReplicas = 3
			_, err = kubeClient.AppsV1().Deployments(klusterletNamespace).UpdateStatus(context.Background(), workDeployment, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "HubConnectionDegraded", "HubConnectionFunctional", metav1.ConditionFalse)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "RegistrationDesiredDegraded", "DeploymentsFunctional", metav1.ConditionFalse)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "WorkDesiredDegraded", "DeploymentsFunctional", metav1.ConditionFalse)

			// Delete the hub kubeconfig secret
			err = kubeClient.CoreV1().Secrets(klusterletNamespace).Delete(context.Background(), hubSecret.Name, metav1.DeleteOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			// Update replica of deployment
			registrationDeployment, err = kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), registrationDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			registrationDeployment = registrationDeployment.DeepCopy()
			registrationDeployment.Status.AvailableReplicas = 0
			_, err = kubeClient.AppsV1().Deployments(klusterletNamespace).UpdateStatus(context.Background(), registrationDeployment, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertKlusterletCondition(
				klusterlet.Name, operatorClient,
				"HubConnectionDegraded",
				"BootstrapSecretFunctional,HubKubeConfigSecretMissing", metav1.ConditionTrue)
		})

		ginkgo.It("should have correct available conditions", func() {
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(), klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Eventually(func() bool {
				if _, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), registrationDeploymentName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			registrationDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(
				context.Background(), registrationDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			gomega.Eventually(func() bool {
				if _, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{}); err != nil {
					return false
				}
				return true
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			workDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(context.Background(), workDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "Available", "NoAvailablePods", metav1.ConditionFalse)

			// Update replica of deployment, more than 0 AvailableReplicas makes the Available=true
			registrationDeployment.Status.AvailableReplicas = 1
			registrationDeployment.Status.Replicas = 3
			registrationDeployment.Status.ReadyReplicas = 3
			_, err = kubeClient.AppsV1().Deployments(klusterletNamespace).UpdateStatus(context.Background(), registrationDeployment, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			workDeployment.Status.AvailableReplicas = 1
			workDeployment.Status.Replicas = 3
			workDeployment.Status.ReadyReplicas = 3
			_, err = kubeClient.AppsV1().Deployments(klusterletNamespace).UpdateStatus(context.Background(), workDeployment, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, "Available", "KlusterletAvailable", metav1.ConditionTrue)
		})
	})

	ginkgo.Context("klusterlet feature gates and configuration", func() {
		ginkgo.BeforeEach(func() {
			registrationDeploymentName = fmt.Sprintf("%s-registration-agent", klusterlet.Name)
			workDeploymentName = fmt.Sprintf("%s-work-agent", klusterlet.Name)
		})
		ginkgo.AfterEach(func() {
			gomega.Expect(operatorClient.OperatorV1().Klusterlets().Delete(context.Background(),
				klusterlet.Name, metav1.DeleteOptions{})).To(gomega.BeNil())
		})

		ginkgo.It("feature gates configuration is nil or empty", func() {
			klusterlet.Spec.RegistrationConfiguration = nil
			klusterlet.Spec.WorkConfiguration = &operatorapiv1.WorkAgentConfiguration{}

			ginkgo.By("Create the klusterlet with RegistrationConfiguration nil and WorkConfiguration empty")
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(),
				klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("Create a bootstrap secret and make sure the kubeconfig can work")
			bootStrapSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      helpers.BootstrapHubKubeConfig,
					Namespace: klusterletNamespace,
				},
				Data: map[string][]byte{
					"kubeconfig": util.NewKubeConfig(restConfig),
				},
			}
			_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Create(context.Background(),
				bootStrapSecret, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			hubSecret, err := kubeClient.CoreV1().Secrets(klusterletNamespace).Get(context.Background(),
				helpers.HubKubeConfig, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Update hub secret and make sure the kubeconfig can work")
			hubSecret = hubSecret.DeepCopy()
			hubSecret.Data["cluster-name"] = []byte("testcluster")
			hubSecret.Data["kubeconfig"] = util.NewKubeConfig(restConfig)
			_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Update(context.Background(),
				hubSecret, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient,
				"HubConnectionDegraded", "HubConnectionFunctional", metav1.ConditionFalse)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient,
				"RegistrationDesiredDegraded", "UnavailablePods", metav1.ConditionTrue)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient,
				"WorkDesiredDegraded", "UnavailablePods", metav1.ConditionTrue)

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, helpers.FeatureGatesTypeValid,
				helpers.FeatureGatesReasonAllValid, metav1.ConditionTrue)

			ginkgo.By("Check the registration-agent has the expected feature gates")
			registrationDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(
				context.Background(), registrationDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(registrationDeployment.Spec.Template.Spec.Containers[0].Args).ShouldNot(
				gomega.ContainElement("--feature-gates=AddonManagement=true"))

			ginkgo.By("Check the work-agent has the expected feature gates")
			workDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(
				context.Background(), workDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(workDeployment.Spec.Template.Spec.Containers[0].Args).ShouldNot(
				gomega.ContainElement("--feature-gates="))

		})

		ginkgo.It("should set certDurationSeconds correctly", func() {
			klusterlet.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationConfiguration{
				ClientCertExpirationSeconds: 120,
			}

			ginkgo.By("Create the klusterlet with valid RegistrationConfiguration")
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(),
				klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("Create a bootstrap secret and make sure the kubeconfig can work")
			bootStrapSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      helpers.BootstrapHubKubeConfig,
					Namespace: klusterletNamespace,
				},
				Data: map[string][]byte{
					"kubeconfig": util.NewKubeConfig(restConfig),
				},
			}
			_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Create(context.Background(),
				bootStrapSecret, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			hubSecret, err := kubeClient.CoreV1().Secrets(klusterletNamespace).Get(context.Background(),
				helpers.HubKubeConfig, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Update hub secret and make sure the kubeconfig can work")
			hubSecret = hubSecret.DeepCopy()
			hubSecret.Data["cluster-name"] = []byte("testcluster")
			hubSecret.Data["kubeconfig"] = util.NewKubeConfig(restConfig)
			_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Update(context.Background(),
				hubSecret, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient,
				"HubConnectionDegraded", "HubConnectionFunctional", metav1.ConditionFalse)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient,
				"RegistrationDesiredDegraded", "UnavailablePods", metav1.ConditionTrue)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient,
				"WorkDesiredDegraded", "UnavailablePods", metav1.ConditionTrue)

			ginkgo.By("Check the registration-agent has the expected agrs")
			registrationDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(
				context.Background(), registrationDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(registrationDeployment.Spec.Template.Spec.Containers[0].Args).Should(
				gomega.ContainElement("--client-cert-expiration-seconds=120"))
		})

		ginkgo.It("should be set correctly", func() {
			klusterlet.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationConfiguration{
				FeatureGates: []operatorapiv1.FeatureGate{
					{
						Feature: string(ocmfeature.ClusterClaim),
						Mode:    operatorapiv1.FeatureGateModeTypeDisable,
					},
				},
				ClusterAnnotations: map[string]string{
					"foo":                                  "bar", // should be ignored
					"agent.open-cluster-management.io/foo": "bar",
				},
			}
			klusterlet.Spec.WorkConfiguration = &operatorapiv1.WorkAgentConfiguration{
				FeatureGates: []operatorapiv1.FeatureGate{
					{
						Feature: string(ocmfeature.ExecutorValidatingCaches),
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
				},
			}

			ginkgo.By("Create the klusterlet with valid RegistrationConfiguration and WorkConfiguration")
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(),
				klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("Create a bootstrap secret and make sure the kubeconfig can work")
			bootStrapSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      helpers.BootstrapHubKubeConfig,
					Namespace: klusterletNamespace,
				},
				Data: map[string][]byte{
					"kubeconfig": util.NewKubeConfig(restConfig),
				},
			}
			_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Create(context.Background(),
				bootStrapSecret, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			hubSecret, err := kubeClient.CoreV1().Secrets(klusterletNamespace).Get(context.Background(),
				helpers.HubKubeConfig, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Update hub secret and make sure the kubeconfig can work")
			hubSecret = hubSecret.DeepCopy()
			hubSecret.Data["cluster-name"] = []byte("testcluster")
			hubSecret.Data["kubeconfig"] = util.NewKubeConfig(restConfig)
			_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Update(context.Background(),
				hubSecret, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient,
				"HubConnectionDegraded", "HubConnectionFunctional", metav1.ConditionFalse)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient,
				"RegistrationDesiredDegraded", "UnavailablePods", metav1.ConditionTrue)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient,
				"WorkDesiredDegraded", "UnavailablePods", metav1.ConditionTrue)

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, helpers.FeatureGatesTypeValid,
				helpers.FeatureGatesReasonAllValid, metav1.ConditionTrue)

			ginkgo.By("Check the registration-agent has the expected feature gates")
			registrationDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(
				context.Background(), registrationDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(registrationDeployment.Spec.Template.Spec.Containers[0].Args).Should(
				gomega.ContainElement("--feature-gates=ClusterClaim=false"))

			ginkgo.By("Check the registration-agent has the expected cluster-annotations")
			gomega.Expect(registrationDeployment.Spec.Template.Spec.Containers[0].Args).Should(
				gomega.ContainElement("--cluster-annotations=agent.open-cluster-management.io/foo=bar"))

			ginkgo.By("Check the work-agent has the expected feature gates")
			workDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(
				context.Background(), workDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(workDeployment.Spec.Template.Spec.Containers[0].Args).Should(
				gomega.ContainElement("--feature-gates=ExecutorValidatingCaches=true"))
		})

		ginkgo.It("has invalid feature gates", func() {
			klusterlet.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationConfiguration{
				FeatureGates: []operatorapiv1.FeatureGate{
					{
						Feature: "Foo",
						Mode:    operatorapiv1.FeatureGateModeTypeDisable,
					},
					{
						Feature: string(ocmfeature.ClusterClaim),
						Mode:    operatorapiv1.FeatureGateModeTypeDisable,
					},
				},
			}
			klusterlet.Spec.WorkConfiguration = &operatorapiv1.WorkAgentConfiguration{
				FeatureGates: []operatorapiv1.FeatureGate{
					{
						Feature: "Bar",
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
					{
						Feature: string(ocmfeature.ExecutorValidatingCaches),
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
				},
			}

			ginkgo.By("Create the klusterlet with invalid RegistrationConfiguration and WorkConfiguration")
			_, err := operatorClient.OperatorV1().Klusterlets().Create(context.Background(),
				klusterlet, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			ginkgo.By("Create a bootstrap secret and make sure the kubeconfig can work")
			bootStrapSecret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      helpers.BootstrapHubKubeConfig,
					Namespace: klusterletNamespace,
				},
				Data: map[string][]byte{
					"kubeconfig": util.NewKubeConfig(restConfig),
				},
			}
			_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Create(context.Background(),
				bootStrapSecret, metav1.CreateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			hubSecret, err := kubeClient.CoreV1().Secrets(klusterletNamespace).Get(context.Background(),
				helpers.HubKubeConfig, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			ginkgo.By("Update hub secret and make sure the kubeconfig can work")
			hubSecret = hubSecret.DeepCopy()
			hubSecret.Data["cluster-name"] = []byte("testcluster")
			hubSecret.Data["kubeconfig"] = util.NewKubeConfig(restConfig)
			_, err = kubeClient.CoreV1().Secrets(klusterletNamespace).Update(context.Background(),
				hubSecret, metav1.UpdateOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient,
				"HubConnectionDegraded", "HubConnectionFunctional", metav1.ConditionFalse)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient,
				"RegistrationDesiredDegraded", "UnavailablePods", metav1.ConditionTrue)
			util.AssertKlusterletCondition(klusterlet.Name, operatorClient,
				"WorkDesiredDegraded", "UnavailablePods", metav1.ConditionTrue)

			util.AssertKlusterletCondition(klusterlet.Name, operatorClient, helpers.FeatureGatesTypeValid,
				helpers.FeatureGatesReasonInvalidExisting, metav1.ConditionFalse)

			ginkgo.By("Check the registration-agent only have the valid feature gates")
			registrationDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(
				context.Background(), registrationDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(registrationDeployment.Spec.Template.Spec.Containers[0].Args).Should(
				gomega.ContainElement("--feature-gates=ClusterClaim=false"))
			gomega.Expect(registrationDeployment.Spec.Template.Spec.Containers[0].Args).ShouldNot(
				gomega.ContainElement("--feature-gates=Foo=false"))

			ginkgo.By("Check the work-agent only have the valid feature gates")
			workDeployment, err := kubeClient.AppsV1().Deployments(klusterletNamespace).Get(
				context.Background(), workDeploymentName, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
			gomega.Expect(workDeployment.Spec.Template.Spec.Containers[0].Args).Should(
				gomega.ContainElement("--feature-gates=ExecutorValidatingCaches=true"))
			gomega.Expect(workDeployment.Spec.Template.Spec.Containers[0].Args).ShouldNot(
				gomega.ContainElement("--feature-gates=Bar=true"))
		})
	})
})
