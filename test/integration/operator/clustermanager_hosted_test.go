package operator

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/cert"

	"open-cluster-management.io/api/feature"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	cloudeventsconstants "open-cluster-management.io/sdk-go/pkg/cloudevents/constants"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/test/integration/util"
)

func updateDeploymentsStatusSuccess(kubeClient kubernetes.Interface, namespace string, deployments ...string) {
	for _, deploymentName := range deployments {
		gomega.Eventually(func() error {
			deployment, err := kubeClient.AppsV1().Deployments(namespace).Get(
				context.Background(), deploymentName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			deployment.Status.Replicas = *deployment.Spec.Replicas
			deployment.Status.ReadyReplicas = *deployment.Spec.Replicas
			deployment.Status.AvailableReplicas = *deployment.Spec.Replicas
			deployment.Status.ObservedGeneration = deployment.Generation
			_, err = kubeClient.AppsV1().Deployments(namespace).UpdateStatus(
				context.Background(), deployment, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	}
}

func updateDeploymentsStatusFail(kubeClient kubernetes.Interface, namespace string, deployments ...string) {
	for _, deploymentName := range deployments {
		gomega.Eventually(func() error {
			deployment, err := kubeClient.AppsV1().Deployments(namespace).Get(context.Background(), deploymentName, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return nil
			}
			if err != nil {
				return err
			}
			deployment.Status.Replicas = 0
			deployment.Status.ReadyReplicas = 0
			deployment.Status.AvailableReplicas = 0
			deployment.Status.ObservedGeneration = 0
			_, err = kubeClient.AppsV1().Deployments(namespace).UpdateStatus(context.Background(), deployment, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
	}
}

var _ = ginkgo.Describe("ClusterManager Hosted Mode", func() {
	var hostedCtx context.Context
	var hostedCancel context.CancelFunc
	var hubRegistrationDeployment = fmt.Sprintf("%s-registration-controller", clusterManagerName)
	var hubPlacementDeployment = fmt.Sprintf("%s-placement-controller", clusterManagerName)
	var hubRegistrationWebhookDeployment = fmt.Sprintf("%s-registration-webhook", clusterManagerName)
	var hubWorkWebhookDeployment = fmt.Sprintf("%s-work-webhook", clusterManagerName)
	var hubAddOnManagerDeployment = fmt.Sprintf("%s-addon-manager-controller", clusterManagerName)
	var hubAddOnWebhookDeployment = fmt.Sprintf("%s-addon-webhook", clusterManagerName)
	var hubWorkControllerDeployment = fmt.Sprintf("%s-work-controller", clusterManagerName)
	var hubRegistrationClusterRole = fmt.Sprintf("open-cluster-management:%s-registration:controller", clusterManagerName)
	var hubRegistrationWebhookClusterRole = fmt.Sprintf("open-cluster-management:%s-registration:webhook", clusterManagerName)
	var hubWorkWebhookClusterRole = fmt.Sprintf("open-cluster-management:%s-registration:webhook", clusterManagerName)
	var hubWorkControllerClusterRole = fmt.Sprintf("open-cluster-management:%s-work:controller", clusterManagerName)
	var hubAddOnManagerClusterRole = fmt.Sprintf("open-cluster-management:%s-addon-manager:controller", clusterManagerName)
	var hubRegistrationSA = "registration-controller-sa"
	var hubRegistrationWebhookSA = "registration-webhook-sa"
	var hubWorkWebhookSA = "work-webhook-sa"
	var hubWorkControllerSA = "work-controller-sa"
	var hubAddOnManagerSA = "addon-manager-controller-sa"

	ginkgo.BeforeEach(func() {
		hostedCtx, hostedCancel = context.WithCancel(context.Background())

		recorder := util.NewIntegrationTestEventRecorder("integration")

		// Create the hosted hub namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: hubNamespaceHosted,
			},
		}
		_, _, err := resourceapply.ApplyNamespace(hostedCtx, hostedKubeClient.CoreV1(), recorder, ns)
		gomega.Expect(err).To(gomega.BeNil())

		// Create the external hub kubeconfig secret
		// #nosec G101
		hubKubeconfigSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      helpers.ExternalHubKubeConfig,
				Namespace: hubNamespaceHosted,
			},
			Data: map[string][]byte{
				"kubeconfig": util.NewKubeConfig(hostedRestConfig),
			},
		}
		_, _, err = resourceapply.ApplySecret(hostedCtx, hostedKubeClient.CoreV1(), recorder, hubKubeconfigSecret)
		gomega.Expect(err).To(gomega.BeNil())

		go startHubOperator(hostedCtx, operatorapiv1.InstallModeHosted)
	})

	ginkgo.AfterEach(func() {
		// delete deployment for clustermanager here so tests are not impacted with each other
		err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).DeleteCollection(context.TODO(), metav1.DeleteOptions{}, metav1.ListOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		if hostedCancel != nil {
			hostedCancel()
		}
	})

	ginkgo.Context("Deploy and clean hub component", func() {
		ginkgo.It("should have expected resource created successfully", func() {
			// Check namespace
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.CoreV1().Namespaces().Get(hostedCtx, hubNamespaceHosted, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check clusterrole/clusterrolebinding
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.RbacV1().ClusterRoles().Get(hostedCtx, hubRegistrationClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.RbacV1().ClusterRoles().Get(hostedCtx, hubRegistrationWebhookClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.RbacV1().ClusterRoles().Get(hostedCtx, hubWorkWebhookClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.RbacV1().ClusterRoleBindings().Get(hostedCtx, hubRegistrationClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.RbacV1().ClusterRoleBindings().Get(hostedCtx, hubRegistrationWebhookClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.RbacV1().ClusterRoleBindings().Get(hostedCtx, hubWorkWebhookClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check service account
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.CoreV1().ServiceAccounts(hubNamespaceHosted).Get(hostedCtx, hubRegistrationSA, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.CoreV1().ServiceAccounts(hubNamespaceHosted).Get(hostedCtx, hubRegistrationWebhookSA, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.CoreV1().ServiceAccounts(hubNamespaceHosted).Get(hostedCtx, hubWorkWebhookSA, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.CoreV1().ServiceAccounts(hubNamespaceHosted).Get(hostedCtx, hubWorkControllerSA, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check deployment
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(hostedCtx, hubRegistrationDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(hostedCtx, hubRegistrationWebhookDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(hostedCtx, hubWorkWebhookDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(hostedCtx, hubWorkControllerDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(hostedCtx, hubAddOnManagerDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check service
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.CoreV1().Services(hubNamespaceHosted).Get(
					hostedCtx, "cluster-manager-registration-webhook", metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.CoreV1().Services(hubNamespaceHosted).Get(
					hostedCtx, "cluster-manager-work-webhook", metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check webhook secret
			registrationWebhookSecret := "registration-webhook-serving-cert"
			gomega.Eventually(func() error {
				s, err := hostedKubeClient.CoreV1().Secrets(hubNamespaceHosted).Get(hostedCtx, registrationWebhookSecret, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if s.Data == nil {
					return fmt.Errorf("s.Data is nil")
				} else if s.Data["tls.crt"] == nil {
					return fmt.Errorf("s.Data doesn't contain key 'tls.crt'")
				} else if s.Data["tls.key"] == nil {
					return fmt.Errorf("s.Data doesn't contain key 'tls.key'")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			//#nosec G101
			workWebhookSecret := "work-webhook-serving-cert"
			gomega.Eventually(func() error {
				s, err := hostedKubeClient.CoreV1().Secrets(hubNamespaceHosted).Get(hostedCtx, workWebhookSecret, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if s.Data == nil {
					return fmt.Errorf("s.Data is nil")
				} else if s.Data["tls.crt"] == nil {
					return fmt.Errorf("s.Data doesn't contain key 'tls.crt'")
				} else if s.Data["tls.key"] == nil {
					return fmt.Errorf("s.Data doesn't contain key 'tls.key'")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			ginkgo.By("Update the deployment status to fail to prevent other cases from interfering")
			updateDeploymentsStatusFail(hostedKubeClient, hubNamespaceHosted,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubWorkControllerDeployment, hubAddOnManagerDeployment, hubAddOnWebhookDeployment)

			// Check validating webhook
			registrationValidtingWebhook := "managedclustervalidators.admission.cluster.open-cluster-management.io"

			// Should not apply the webhook config if the replica and observed is not set
			_, err := hostedKubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
				hostedCtx, registrationValidtingWebhook, metav1.GetOptions{})
			gomega.Expect(err).To(gomega.HaveOccurred())

			workValidtingWebhook := "manifestworkvalidators.admission.work.open-cluster-management.io"
			// Should not apply the webhook config if the replica and observed is not set
			_, err = hostedKubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
				hostedCtx, workValidtingWebhook, metav1.GetOptions{})
			gomega.Expect(err).To(gomega.HaveOccurred())

			updateDeploymentsStatusSuccess(hostedKubeClient, hubNamespaceHosted,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubWorkControllerDeployment, hubAddOnManagerDeployment, hubAddOnWebhookDeployment)

			gomega.Eventually(func() error {
				_, err := hostedKubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
					hostedCtx, registrationValidtingWebhook, metav1.GetOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			gomega.Eventually(func() error {
				_, err := hostedKubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
					hostedCtx, workValidtingWebhook, metav1.GetOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			util.AssertClusterManagerCondition(clusterManagerName, hostedOperatorClient, "Applied", "ClusterManagerApplied", metav1.ConditionTrue)
		})

		ginkgo.It("should have expected resource created/deleted when feature gates manifestwork replicaset enabled/disabled", func() {
			ginkgo.By("Disable manifestwork replicaset feature gate")
			// Check manifestwork replicaset disable
			gomega.Eventually(func() error {
				clusterManager, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(
					context.Background(), clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				featureGate := []operatorapiv1.FeatureGate{
					{
						Feature: string(feature.ManifestWorkReplicaSet),
						Mode:    operatorapiv1.FeatureGateModeTypeDisable,
					},
				}
				if clusterManager.Spec.WorkConfiguration != nil {
					for _, fg := range clusterManager.Spec.WorkConfiguration.FeatureGates {
						if fg.Feature != string(feature.ManifestWorkReplicaSet) {
							featureGate = append(featureGate, fg)
						}
					}
				}
				clusterManager.Spec.WorkConfiguration = &operatorapiv1.WorkConfiguration{
					FeatureGates: featureGate,
				}
				_, err = hostedOperatorClient.OperatorV1().ClusterManagers().Update(
					context.Background(), clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check clusterrole/clusterrolebinding
			gomega.Eventually(func() bool {
				_, err := hostedKubeClient.RbacV1().ClusterRoles().Get(
					context.Background(), hubWorkControllerClusterRole, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				_, err := hostedKubeClient.RbacV1().ClusterRoleBindings().Get(
					context.Background(), hubWorkControllerClusterRole, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check service account
			gomega.Eventually(func() bool {
				_, err := hostedKubeClient.CoreV1().ServiceAccounts(hubNamespaceHosted).Get(
					context.Background(), hubWorkControllerSA, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check deployment
			gomega.Eventually(func() bool {
				_, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(
					context.Background(), hubWorkControllerDeployment, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			updateDeploymentsStatusSuccess(hostedKubeClient, hubNamespaceHosted,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubAddOnManagerDeployment, hubAddOnWebhookDeployment)

			// Check if relatedResources are correct
			gomega.Eventually(func() error {
				actual, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(
					context.Background(), clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if len(actual.Status.RelatedResources) != 43 {
					return fmt.Errorf("should get 43 relatedResources, actual got %v, %v",
						len(actual.Status.RelatedResources), actual.Status.RelatedResources)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("Revert manifestwork replicaset to enable mode")
			gomega.Eventually(func() error {
				clusterManager, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(
					context.Background(), clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				featureGate := []operatorapiv1.FeatureGate{
					{
						Feature: string(feature.ManifestWorkReplicaSet),
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
				}
				if clusterManager.Spec.WorkConfiguration != nil {
					for _, fg := range clusterManager.Spec.WorkConfiguration.FeatureGates {
						if fg.Feature != string(feature.ManifestWorkReplicaSet) {
							featureGate = append(featureGate, fg)
						}
					}
				}
				clusterManager.Spec.WorkConfiguration = &operatorapiv1.WorkConfiguration{
					FeatureGates: featureGate,
				}
				_, err = hostedOperatorClient.OperatorV1().ClusterManagers().Update(
					context.Background(), clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check clusterrole/clusterrolebinding
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.RbacV1().ClusterRoles().Get(
					context.Background(), hubWorkControllerClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.RbacV1().ClusterRoleBindings().Get(
					context.Background(), hubWorkControllerClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check service account
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.CoreV1().ServiceAccounts(hubNamespaceHosted).Get(
					context.Background(), hubWorkControllerSA, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check deployment
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(
					context.Background(), hubWorkControllerDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			updateDeploymentsStatusSuccess(hostedKubeClient, hubNamespaceHosted,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubWorkControllerDeployment, hubAddOnManagerDeployment, hubAddOnWebhookDeployment)
			// Check if relatedResources are correct
			gomega.Eventually(func() error {
				actual, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(
					context.Background(), clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if len(actual.Status.RelatedResources) != 47 {
					return fmt.Errorf("should get 47 relatedResources, actual got %v, %v",
						len(actual.Status.RelatedResources), actual.Status.RelatedResources)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("should have expected work driver when work driver is updated", func() {
			ginkgo.By("Update work driver to grpc")
			gomega.Eventually(func() error {
				clusterManager, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(
					context.Background(), clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				featureGates := []operatorapiv1.FeatureGate{
					{
						Feature: string(feature.ManifestWorkReplicaSet),
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
					{
						Feature: string(feature.CloudEventsDrivers),
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
				}
				if clusterManager.Spec.WorkConfiguration != nil {
					for _, fg := range clusterManager.Spec.WorkConfiguration.FeatureGates {
						if fg.Feature != string(feature.ManifestWorkReplicaSet) &&
							fg.Feature != string(feature.CloudEventsDrivers) {
							featureGates = append(featureGates, fg)
						}
					}
				}
				clusterManager.Spec.WorkConfiguration = &operatorapiv1.WorkConfiguration{
					FeatureGates: featureGates,
					WorkDriver:   cloudeventsconstants.ConfigTypeGRPC,
				}
				_, err = hostedOperatorClient.OperatorV1().ClusterManagers().Update(
					context.Background(), clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// gomega.Eventually(func() error {
			// 	actual, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(
			// 		context.Background(), clusterManagerName, metav1.GetOptions{})
			// 	if err != nil {
			// 		return err
			// 	}
			// 	if !meta.IsStatusConditionFalse(actual.Status.Conditions, "SecretSynced") {
			// 		return fmt.Errorf("should get WorkDriverConfigSecretSynced condition false")
			// 	}
			// 	return nil
			// }, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			_, err := hostedKubeClient.CoreV1().Secrets("default").Create(context.TODO(), &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "work-driver-config",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"config.yaml": []byte("url: grpc.example.com:8443"),
				},
			}, metav1.CreateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			// gomega.Eventually(func() error {
			// 	actual, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(
			// 		context.Background(), clusterManagerName, metav1.GetOptions{})
			// 	if err != nil {
			// 		return err
			// 	}
			// 	if !meta.IsStatusConditionTrue(actual.Status.Conditions, "SecretSynced") {
			// 		return fmt.Errorf("should get WorkDriverConfigSecretSynced condition true")
			// 	}
			// 	return nil
			// }, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			gomega.Eventually(func() error {
				actual, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(context.Background(),
					hubWorkControllerDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				foundArg := false
				for _, arg := range actual.Spec.Template.Spec.Containers[0].Args {
					if arg == "--work-driver=grpc" {
						foundArg = true
					}
				}
				if !foundArg {
					return fmt.Errorf("do not find the --work-driver=grpc args, got %v", actual.Spec.Template.Spec.Containers[0].Args)
				}
				foundVol := false
				for _, vol := range actual.Spec.Template.Spec.Volumes {
					if vol.Name == "workdriverconfig" && vol.Secret.SecretName == "work-driver-config" {
						foundVol = true
					}
				}
				if !foundVol {
					return fmt.Errorf("do not find the workdriverconfig volume, got %v", actual.Spec.Template.Spec.Volumes)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			gomega.Eventually(func() error {
				workConfigSecret, err := hostedKubeClient.CoreV1().Secrets(hubNamespaceHosted).Get(context.Background(),
					"work-driver-config", metav1.GetOptions{})
				if err != nil {
					return err
				}
				if string(workConfigSecret.Data["config.yaml"]) != "url: grpc.example.com:8443" {
					return fmt.Errorf("do not find the expected config.yaml, got %v", string(workConfigSecret.Data["config.yaml"]))
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			ginkgo.By("Revert work driver back to kube")
			gomega.Eventually(func() error {
				clusterManager, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(
					context.Background(), clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				clusterManager.Spec.WorkConfiguration.WorkDriver = operatorapiv1.WorkDriverTypeKube
				_, err = hostedOperatorClient.OperatorV1().ClusterManagers().Update(
					context.Background(), clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			gomega.Eventually(func() error {
				actual, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(context.Background(),
					hubWorkControllerDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				for _, arg := range actual.Spec.Template.Spec.Containers[0].Args {
					if arg == "--work-driver=grpc" {
						return err
					}
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			err = hostedKubeClient.CoreV1().Secrets("default").Delete(context.Background(),
				"work-driver-config", metav1.DeleteOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		})

		ginkgo.It("should have expected resource created/deleted successfully when feature gates AddOnManager enabled/disabled", func() {
			ginkgo.By("Disable AddOnManager feature gate")
			// Check addon manager disable mode
			gomega.Eventually(func() error {
				clusterManager, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(context.Background(), clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				clusterManager.Spec.AddOnManagerConfiguration = &operatorapiv1.AddOnManagerConfiguration{
					FeatureGates: []operatorapiv1.FeatureGate{
						{
							Feature: string(feature.AddonManagement),
							Mode:    operatorapiv1.FeatureGateModeTypeDisable,
						},
					},
				}
				_, err = hostedOperatorClient.OperatorV1().ClusterManagers().Update(context.Background(), clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check clusterrole/clusterrolebinding
			gomega.Eventually(func() bool {
				_, err := hostedKubeClient.RbacV1().ClusterRoles().Get(context.Background(), hubAddOnManagerClusterRole, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
			gomega.Eventually(func() bool {
				_, err := hostedKubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(), hubAddOnManagerClusterRole, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check service account
			gomega.Eventually(func() bool {
				_, err := hostedKubeClient.CoreV1().ServiceAccounts(hubNamespaceHosted).Get(context.Background(), hubAddOnManagerSA, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			// Check deployment
			gomega.Eventually(func() bool {
				_, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(context.Background(), hubAddOnManagerDeployment, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			updateDeploymentsStatusSuccess(hostedKubeClient, hubNamespaceHosted,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubWorkControllerDeployment)

			// Check if relatedResources are correct
			gomega.Eventually(func() error {
				actual, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(context.Background(), clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if len(actual.Status.RelatedResources) != 42 {
					return fmt.Errorf("should get 42 relatedResources, actual got %v, %v",
						len(actual.Status.RelatedResources), actual.Status.RelatedResources)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

			ginkgo.By("Revert addon manager to enable mode")
			gomega.Eventually(func() error {
				clusterManager, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(
					context.Background(), clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// Check addon manager enabled mode
				clusterManager.Spec.AddOnManagerConfiguration = nil
				_, err = hostedOperatorClient.OperatorV1().ClusterManagers().Update(context.Background(), clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check clusterrole/clusterrolebinding
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.RbacV1().ClusterRoles().Get(context.Background(), hubAddOnManagerClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.RbacV1().ClusterRoleBindings().Get(context.Background(), hubAddOnManagerClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check service account
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.CoreV1().ServiceAccounts(hubNamespaceHosted).Get(context.Background(), hubAddOnManagerSA, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check deployment
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(
					context.Background(), hubAddOnManagerDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			updateDeploymentsStatusSuccess(hostedKubeClient, hubNamespaceHosted,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubWorkControllerDeployment, hubAddOnManagerDeployment, hubAddOnWebhookDeployment)
			// Check if relatedResources are correct
			gomega.Eventually(func() error {
				actual, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(context.Background(), clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if len(actual.Status.RelatedResources) != 47 {
					return fmt.Errorf("should get 47 relatedResources, actual got %v, %v", len(actual.Status.RelatedResources), actual.Status.RelatedResources)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
		})

		ginkgo.It("Deployment should be updated when clustermanager is changed", func() {
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(hostedCtx, hubRegistrationDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// Check if generations are correct
			gomega.Eventually(func() error {
				actual, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(hostedCtx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if actual.Generation != actual.Status.ObservedGeneration {
					return fmt.Errorf("except generation to be %d, but got %d", actual.Status.ObservedGeneration, actual.Generation)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			gomega.Eventually(func() error {
				clusterManager, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(hostedCtx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				clusterManager.Spec.RegistrationImagePullSpec = testImage
				_, err = hostedOperatorClient.OperatorV1().ClusterManagers().Update(hostedCtx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			gomega.Eventually(func() error {
				actual, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(hostedCtx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				gomega.Expect(len(actual.Spec.Template.Spec.Containers)).Should(gomega.Equal(1))
				if actual.Spec.Template.Spec.Containers[0].Image != testImage {
					return fmt.Errorf("expected image to be testimage:latest but get %s", actual.Spec.Template.Spec.Containers[0].Image)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			updateDeploymentsStatusSuccess(hostedKubeClient, hubNamespaceHosted,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubWorkControllerDeployment, hubAddOnManagerDeployment, hubAddOnWebhookDeployment)

			// Check if generations are correct
			gomega.Eventually(func() error {
				actual, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(hostedCtx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if actual.Generation != actual.Status.ObservedGeneration {
					return fmt.Errorf("except generation to be %d, but got %d", actual.Status.ObservedGeneration, actual.Generation)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
		})

		ginkgo.It("Deployment should be added nodeSelector and toleration when add nodePlacement into clustermanager", func() {
			gomega.Eventually(func() error {
				clusterManager, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(hostedCtx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				clusterManager.Spec.NodePlacement = operatorapiv1.NodePlacement{
					NodeSelector: map[string]string{infraNodeLabel: ""},
					Tolerations: []corev1.Toleration{
						{
							Key:      infraNodeLabel,
							Operator: corev1.TolerationOpExists,
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
				}
				_, err = hostedOperatorClient.OperatorV1().ClusterManagers().Update(hostedCtx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			gomega.Eventually(func() error {
				actual, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(hostedCtx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				gomega.Expect(len(actual.Spec.Template.Spec.Containers)).Should(gomega.Equal(1))
				if len(actual.Spec.Template.Spec.NodeSelector) == 0 {
					return fmt.Errorf("length of node selector should not equals to 0")
				}
				if _, ok := actual.Spec.Template.Spec.NodeSelector[infraNodeLabel]; !ok {
					return fmt.Errorf("node-role.kubernetes.io/infra not exist")
				}
				if len(actual.Spec.Template.Spec.Tolerations) == 0 {
					return fmt.Errorf("length of node selecor should not equals to 0")
				}
				for _, toleration := range actual.Spec.Template.Spec.Tolerations {
					if toleration.Key == infraNodeLabel {
						return nil
					}
				}

				return fmt.Errorf("no key equals to node-role.kubernetes.io/infra")
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			updateDeploymentsStatusSuccess(hostedKubeClient, hubNamespaceHosted,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubWorkControllerDeployment, hubAddOnManagerDeployment, hubAddOnWebhookDeployment)
		})

		ginkgo.It("Deployment should be reconciled when manually updated", func() {
			gomega.Eventually(func() error {
				registrationDeployment, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(hostedCtx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				actual, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(hostedCtx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				deploymentGeneration := helpers.NewGenerationStatus(appsv1.SchemeGroupVersion.WithResource("deployments"), registrationDeployment)
				actualGeneration := helpers.FindGenerationStatus(actual.Status.Generations, deploymentGeneration)
				if deploymentGeneration.LastGeneration != actualGeneration.LastGeneration {
					return fmt.Errorf("expected LastGeneration shoud be %d, but get %d", actualGeneration.LastGeneration, deploymentGeneration.LastGeneration)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
			registrationoDeployment, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(hostedCtx, hubRegistrationDeployment, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			registrationoDeployment.Spec.Template.Spec.Containers[0].Image = "testimage2:latest"
			// Check if generations are correct
			gomega.Eventually(func() error {
				actual, err := hostedOperatorClient.OperatorV1().ClusterManagers().Get(hostedCtx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				registrationDeployment, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(hostedCtx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploymentGeneration := helpers.NewGenerationStatus(appsv1.SchemeGroupVersion.WithResource("deployments"), registrationDeployment)
				actualGeneration := helpers.FindGenerationStatus(actual.Status.Generations, deploymentGeneration)
				if deploymentGeneration.LastGeneration != actualGeneration.LastGeneration {
					return fmt.Errorf("expected LastGeneration shoud be %d, but get %d", actualGeneration.LastGeneration, deploymentGeneration.LastGeneration)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
			gomega.Eventually(func() error {
				registrationoDeployment, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(hostedCtx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if registrationoDeployment.Spec.Template.Spec.Containers[0].Image != testImage {
					return fmt.Errorf("image should be testimage:latest, but get %s", registrationoDeployment.Spec.Template.Spec.Containers[0].Image)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
		})
	})

	ginkgo.Context("Cluster manager statuses", func() {
		ginkgo.It("should have correct degraded conditions", func() {
			gomega.Eventually(func() error {
				if _, err := hostedKubeClient.AppsV1().Deployments(hubNamespaceHosted).Get(
					hostedCtx, hubRegistrationDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// The cluster manager should be unavailable at first
			util.AssertClusterManagerCondition(clusterManagerName, hostedOperatorClient,
				"HubRegistrationDegraded", "UnavailableRegistrationPod", metav1.ConditionTrue)

			// Update replica of deployment
			updateDeploymentsStatusSuccess(hostedKubeClient, hubNamespaceHosted,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubWorkControllerDeployment, hubAddOnManagerDeployment, hubAddOnWebhookDeployment)

			// The cluster manager should be functional at last
			util.AssertClusterManagerCondition(clusterManagerName, hostedOperatorClient, "HubRegistrationDegraded", "RegistrationFunctional", metav1.ConditionFalse)
		})
	})

	ginkgo.Context("Serving cert rotation", func() {
		ginkgo.It("should rotate both serving cert and signing cert before they become expired", func() {
			secretNames := []string{"signer-secret", "registration-webhook-serving-cert", "work-webhook-serving-cert"}
			// wait until all secrets and configmap are in place
			gomega.Eventually(func() error {
				for _, name := range secretNames {
					if _, err := hostedKubeClient.CoreV1().Secrets(hubNamespaceHosted).Get(hostedCtx, name, metav1.GetOptions{}); err != nil {
						return err
					}
				}
				if _, err := hostedKubeClient.CoreV1().ConfigMaps(hubNamespaceHosted).Get(hostedCtx, "ca-bundle-configmap", metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

			// both serving cert and signing cert should aways be valid
			gomega.Consistently(func() error {
				configmap, err := hostedKubeClient.CoreV1().ConfigMaps(hubNamespaceHosted).Get(hostedCtx, "ca-bundle-configmap", metav1.GetOptions{})
				if err != nil {
					return err
				}
				for _, name := range []string{"signer-secret", "registration-webhook-serving-cert", "work-webhook-serving-cert"} {
					secret, err := hostedKubeClient.CoreV1().Secrets(hubNamespaceHosted).Get(hostedCtx, name, metav1.GetOptions{})
					if err != nil {
						return err
					}

					certificates, err := cert.ParseCertsPEM(secret.Data["tls.crt"])
					if err != nil {
						return err
					}
					if len(certificates) == 0 {
						return fmt.Errorf("certificates length equals to 0")
					}

					now := time.Now()
					certificate := certificates[0]
					if now.After(certificate.NotAfter) {
						return fmt.Errorf("certificate after NotAfter")
					}
					if now.Before(certificate.NotBefore) {
						return fmt.Errorf("certificate before NotBefore")
					}

					if name == "signer-secret" {
						continue
					}

					// ensure signing cert of serving certs in the ca bundle configmap
					caCerts, err := cert.ParseCertsPEM([]byte(configmap.Data["ca-bundle.crt"]))
					if err != nil {
						return err
					}

					found := false
					for _, caCert := range caCerts {
						if certificate.Issuer.CommonName != caCert.Subject.CommonName {
							continue
						}
						if now.After(caCert.NotAfter) {
							return fmt.Errorf("certificate after NotAfter")
						}
						if now.Before(caCert.NotBefore) {
							return fmt.Errorf("certificate before NotBefore")
						}
						found = true
						break
					}
					if !found {
						return fmt.Errorf("not found")
					}
				}
				return nil
			}, eventuallyTimeout*3, eventuallyInterval*3).Should(gomega.BeNil())
		})
	})
})
