package operator

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/cert"

	"open-cluster-management.io/api/feature"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	cloudeventsconstants "open-cluster-management.io/sdk-go/pkg/cloudevents/constants"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/pkg/operator/operators/clustermanager"
	certrotation "open-cluster-management.io/ocm/pkg/operator/operators/clustermanager/controllers/certrotationcontroller"
	"open-cluster-management.io/ocm/test/integration/util"
)

const (
	testImage      = "testimage:latest"
	infraNodeLabel = "node-role.kubernetes.io/infra"
)

func startHubOperator(ctx context.Context, mode operatorapiv1.InstallMode) {
	certrotation.SigningCertValidity = time.Second * 30
	certrotation.TargetCertValidity = time.Second * 10
	certrotation.ResyncInterval = time.Second * 1

	var config *rest.Config
	switch mode {
	case operatorapiv1.InstallModeDefault:
		config = restConfig
	case operatorapiv1.InstallModeHosted:
		config = hostedRestConfig
	}

	o := &clustermanager.Options{}
	o.EnableSyncLabels = true
	err := o.RunClusterManagerOperator(ctx, &controllercmd.ControllerContext{
		KubeConfig:        config,
		EventRecorder:     util.NewIntegrationTestEventRecorder("integration"),
		OperatorNamespace: metav1.NamespaceDefault,
	})
	Expect(err).NotTo(HaveOccurred())
}

var _ = Describe("ClusterManager Default Mode", Ordered, func() {
	var cancel context.CancelFunc
	var hubRegistrationDeployment = fmt.Sprintf("%s-registration-controller", clusterManagerName)
	var hubPlacementDeployment = fmt.Sprintf("%s-placement-controller", clusterManagerName)
	var hubRegistrationWebhookDeployment = fmt.Sprintf("%s-registration-webhook", clusterManagerName)
	var hubWorkWebhookDeployment = fmt.Sprintf("%s-work-webhook", clusterManagerName)
	var hubAddOnManagerDeployment = fmt.Sprintf("%s-addon-manager-controller", clusterManagerName)
	var hubWorkControllerDeployment = fmt.Sprintf("%s-work-controller", clusterManagerName)
	var hubAddOnWebhookDeployment = fmt.Sprintf("%s-addon-webhook", clusterManagerName)
	var hubRegistrationClusterRole = fmt.Sprintf("open-cluster-management:%s-registration:controller", clusterManagerName)
	var hubRegistrationWebhookClusterRole = fmt.Sprintf("open-cluster-management:%s-registration:webhook", clusterManagerName)
	var hubWorkWebhookClusterRole = fmt.Sprintf("open-cluster-management:%s-work:webhook", clusterManagerName)
	var hubWorkControllerClusterRole = fmt.Sprintf("open-cluster-management:%s-work:controller", clusterManagerName)
	var hubAddOnManagerClusterRole = fmt.Sprintf("open-cluster-management:%s-addon-manager:controller", clusterManagerName)
	var hubRegistrationSA = "registration-controller-sa"
	var hubRegistrationWebhookSA = "registration-webhook-sa"
	var hubWorkWebhookSA = "work-webhook-sa"
	var hubWorkControllerSA = "work-controller-sa"
	var hubAddOnManagerSA = "addon-manager-controller-sa"

	BeforeEach(func() {
		var runCtx context.Context
		runCtx, cancel = context.WithCancel(context.Background())
		go startHubOperator(runCtx, operatorapiv1.InstallModeDefault)
	})
	AfterEach(func(ctx SpecContext) {
		// delete deployment for clustermanager here so tests are not impacted with each other
		err := kubeClient.AppsV1().Deployments(hubNamespace).DeleteCollection(ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())
		if cancel != nil {
			cancel()
		}
	})

	Context("Deploy and clean hub component", func() {
		It("should have expected resource created successfully", func(ctx SpecContext) {
			// Check namespace
			Eventually(func() error {
				if _, err := kubeClient.CoreV1().Namespaces().Get(ctx, hubNamespace, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			// Check clusterrole/clusterrolebinding
			Eventually(func() error {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, hubRegistrationClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, hubRegistrationWebhookClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(func() error {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, hubWorkWebhookClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(func() error {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, hubWorkControllerClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(func() error {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, hubRegistrationClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(func() error {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, hubRegistrationWebhookClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, hubWorkWebhookClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, hubWorkControllerClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check service account
			Eventually(func() error {
				registrationControllerSA, err := kubeClient.CoreV1().ServiceAccounts(hubNamespace).Get(ctx, hubRegistrationSA, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if _, ok := registrationControllerSA.Annotations["eks.amazonaws.com/role-arn"]; ok {
					return fmt.Errorf("Annotation applicable to awsirsa registration only")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(func() error {
				if _, err := kubeClient.CoreV1().ServiceAccounts(hubNamespace).Get(ctx, hubRegistrationWebhookSA, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(func() error {
				if _, err := kubeClient.CoreV1().ServiceAccounts(hubNamespace).Get(ctx, hubWorkWebhookSA, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(func() error {
				if _, err := kubeClient.CoreV1().ServiceAccounts(hubNamespace).Get(ctx, hubWorkControllerSA, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check deployment
			Eventually(func() error {
				if _, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubRegistrationDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				if _, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubRegistrationWebhookDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				if _, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubWorkWebhookDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				if _, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubWorkControllerDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				if _, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubAddOnManagerDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				if _, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubAddOnWebhookDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check service
			Eventually(func() error {
				if _, err := kubeClient.CoreV1().Services(hubNamespace).Get(ctx, "cluster-manager-registration-webhook", metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				if _, err := kubeClient.CoreV1().Services(hubNamespace).Get(ctx, "cluster-manager-work-webhook", metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check webhook secret
			registrationWebhookSecret := "registration-webhook-serving-cert"
			Eventually(func() error {
				s, err := kubeClient.CoreV1().Secrets(hubNamespace).Get(ctx, registrationWebhookSecret, metav1.GetOptions{})
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
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// #nosec G101
			workWebhookSecret := "work-webhook-serving-cert"
			Eventually(func() error {
				s, err := kubeClient.CoreV1().Secrets(hubNamespace).Get(ctx, workWebhookSecret, metav1.GetOptions{})
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
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			By("Update the deployment status to fail to prevent other cases from interfering")
			// Ensure addon webhook deployment exists before updating status
			Eventually(func() error {
				if _, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubAddOnWebhookDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			updateAllDeploymentsStatusFail(ctx, kubeClient, hubNamespace)

			// Check validating webhook
			registrationValidatingWebhook := "managedclustervalidators.admission.cluster.open-cluster-management.io"
			// Should not apply the webhook config if the replica and observed is not set
			_, err := kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
				ctx, registrationValidatingWebhook, metav1.GetOptions{})
			Expect(err).To(HaveOccurred())

			workValidtingWebhook := "manifestworkvalidators.admission.work.open-cluster-management.io"
			// Should not apply the webhook config if the replica and observed is not set
			_, err = kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
				ctx, workValidtingWebhook, metav1.GetOptions{})
			Expect(err).To(HaveOccurred())

			// Update ready replica of deployment
			// Ensure addon webhook deployment exists before updating status
			Eventually(func() error {
				if _, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubAddOnWebhookDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			updateAllDeploymentsStatusSuccess(ctx, kubeClient, hubNamespace)

			// Wait for ConditionProgressing to become False before checking webhooks.
			// This ensures the operator has reconciled all deployment status updates
			// and avoids a race condition where the webhook check starts before the
			// operator sees all deployments as ready.
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(
					ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				cond := meta.FindStatusCondition(clusterManager.Status.Conditions, operatorapiv1.ConditionProgressing)
				if cond == nil {
					return fmt.Errorf("ConditionProgressing not found")
				}
				if cond.Status != metav1.ConditionFalse {
					return fmt.Errorf("ConditionProgressing is %s, waiting for False", cond.Status)
				}
				return nil
			}, eventuallyTimeout*2, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				_, err := kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
					ctx, registrationValidatingWebhook, metav1.GetOptions{})
				return err
			}, eventuallyTimeout*10, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				_, err := kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(
					ctx, workValidtingWebhook, metav1.GetOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			util.AssertClusterManagerCondition(clusterManagerName, operatorClient, "Applied", "ClusterManagerApplied", metav1.ConditionTrue)
		})

		It("should have expected resource created/deleted when feature gates manifestwork replicaset enabled/disabled", func(ctx SpecContext) {
			By("Disable manifestwork replicaset feature gate")
			// Check manifestwork replicaset disable
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(
					ctx, clusterManagerName, metav1.GetOptions{})
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
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(
					ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check clusterrole/clusterrolebinding
			Eventually(func() bool {
				_, err := kubeClient.RbacV1().ClusterRoles().Get(
					ctx, hubWorkControllerClusterRole, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				_, err := kubeClient.RbacV1().ClusterRoleBindings().Get(
					ctx, hubWorkControllerClusterRole, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check service account
			Eventually(func() bool {
				_, err := kubeClient.CoreV1().ServiceAccounts(hubNamespace).Get(
					ctx, hubWorkControllerSA, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check deployment
			Eventually(func() bool {
				_, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(
					ctx, hubWorkControllerDeployment, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			updateDeploymentsStatusSuccess(ctx, kubeClient, hubNamespace,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubAddOnManagerDeployment)

			// Check if relatedResources are correct
			Eventually(func() error {
				actual, err := operatorClient.OperatorV1().ClusterManagers().Get(
					ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if len(actual.Status.RelatedResources) != 49 {
					return fmt.Errorf("should get 49 relatedResources, actual got %v, %v",
						len(actual.Status.RelatedResources), actual.Status.RelatedResources)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			By("Revert manifestwork replicaset to enable mode")
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(
					ctx, clusterManagerName, metav1.GetOptions{})
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
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(
					ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check clusterrole/clusterrolebinding
			Eventually(func() error {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(
					ctx, hubWorkControllerClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(func() error {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(
					ctx, hubWorkControllerClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check service account
			Eventually(func() error {
				if _, err := kubeClient.CoreV1().ServiceAccounts(hubNamespace).Get(
					ctx, hubWorkControllerSA, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check deployment
			Eventually(func() error {
				if _, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(
					ctx, hubWorkControllerDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			updateDeploymentsStatusSuccess(ctx, kubeClient, hubNamespace,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubWorkControllerDeployment, hubAddOnManagerDeployment, hubAddOnWebhookDeployment)
			// Check if relatedResources are correct
			Eventually(func() error {
				actual, err := operatorClient.OperatorV1().ClusterManagers().Get(
					ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if len(actual.Status.RelatedResources) != 53 {
					return fmt.Errorf("should get 53 relatedResources, actual got %v, %v",
						len(actual.Status.RelatedResources), actual.Status.RelatedResources)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})

		It("should have expected work driver when work driver is updated", func(ctx SpecContext) {
			By("Update work driver to grpc")
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(
					ctx, clusterManagerName, metav1.GetOptions{})
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
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(
					ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Eventually(func() error {
			// 	actual, err := operatorClient.OperatorV1().ClusterManagers().Get(
			// 		ctx, clusterManagerName, metav1.GetOptions{})
			// 	if err != nil {
			// 		return err
			// 	}
			// 	if !meta.IsStatusConditionFalse(actual.Status.Conditions, "SecretSynced") {
			// 		return fmt.Errorf("should get WorkDriverConfigSecretSynced condition false")
			// 	}
			// 	return nil
			// }, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			_, err := kubeClient.CoreV1().Secrets("default").Create(ctx, &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "work-driver-config",
					Namespace: "default",
				},
				Data: map[string][]byte{
					"config.yaml": []byte("url: grpc.example.com:8443"),
				},
			}, metav1.CreateOptions{})
			Expect(err).NotTo(HaveOccurred())

			// Eventually(func() error {
			// 	actual, err := operatorClient.OperatorV1().ClusterManagers().Get(
			// 		ctx, clusterManagerName, metav1.GetOptions{})
			// 	if err != nil {
			// 		return err
			// 	}
			// 	if !meta.IsStatusConditionTrue(actual.Status.Conditions, "SecretSynced") {
			// 		return fmt.Errorf("should get WorkDriverConfigSecretSynced condition true")
			// 	}
			// 	return nil
			// }, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			Eventually(func() error {
				actual, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx,
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
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				workConfigSecret, err := kubeClient.CoreV1().Secrets(hubNamespace).Get(ctx,
					"work-driver-config", metav1.GetOptions{})
				if err != nil {
					return err
				}
				if string(workConfigSecret.Data["config.yaml"]) != "url: grpc.example.com:8443" {
					return fmt.Errorf("do not find the expected config.yaml, got %v", string(workConfigSecret.Data["config.yaml"]))
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			By("Revert work driver back to kube")
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(
					ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				clusterManager.Spec.WorkConfiguration.WorkDriver = operatorapiv1.WorkDriverTypeKube
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(
					ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				actual, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx,
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
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			err = kubeClient.CoreV1().Secrets("default").Delete(ctx,
				"work-driver-config", metav1.DeleteOptions{})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should have expected resource created/deleted successfully when feature gates AddOnManager enabled/disabled", func(ctx SpecContext) {
			By("Check addon manager disable mode")
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
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
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check clusterrole/clusterrolebinding
			Eventually(func() bool {
				_, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, hubAddOnManagerClusterRole, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
			Eventually(func() bool {
				_, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, hubAddOnManagerClusterRole, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check service account
			Eventually(func() bool {
				_, err := kubeClient.CoreV1().ServiceAccounts(hubNamespace).Get(ctx, hubAddOnManagerSA, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check deployment
			Eventually(func() bool {
				_, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubAddOnManagerDeployment, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			updateDeploymentsStatusSuccess(ctx, kubeClient, hubNamespace,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubWorkControllerDeployment)
			// Check if relatedResources are correct
			Eventually(func() error {
				actual, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if len(actual.Status.RelatedResources) != 48 {
					return fmt.Errorf("should get 48 relatedResources, actual got %v", len(actual.Status.RelatedResources))
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

			By("Revert addon manager to enable mode")
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				clusterManager.Spec.AddOnManagerConfiguration = nil
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			// Check clusterrole/clusterrolebinding
			Eventually(func() error {
				if _, err := kubeClient.RbacV1().ClusterRoles().Get(ctx, hubAddOnManagerClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(func() error {
				if _, err := kubeClient.RbacV1().ClusterRoleBindings().Get(ctx, hubAddOnManagerClusterRole, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check service account
			Eventually(func() error {
				if _, err := kubeClient.CoreV1().ServiceAccounts(hubNamespace).Get(ctx, hubAddOnManagerSA, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check deployment
			Eventually(func() error {
				if _, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubAddOnManagerDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			updateDeploymentsStatusSuccess(ctx, kubeClient, hubNamespace,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubWorkControllerDeployment, hubAddOnManagerDeployment, hubAddOnWebhookDeployment)
			// Check if relatedResources are correct
			Eventually(func() error {
				actual, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if len(actual.Status.RelatedResources) != 53 {
					return fmt.Errorf("should get 53 relatedResources, actual got %v", len(actual.Status.RelatedResources))
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})

		It("should have expected resource created/deleted when feature gates PlacementDebugServer enabled/disabled", func(ctx SpecContext) {
			placementServiceName := clusterManagerName + "-placement"

			By("Enable PlacementDebugServer feature gate")
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				clusterManager.Spec.PlacementConfiguration = &operatorapiv1.PlacementConfiguration{
					FeatureGates: []operatorapiv1.FeatureGate{
						{
							Feature: string(feature.PlacementDebugServer),
							Mode:    operatorapiv1.FeatureGateModeTypeEnable,
						},
					},
				}
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check placement service is created
			Eventually(func() error {
				_, err := kubeClient.CoreV1().Services(hubNamespace).Get(ctx, placementServiceName, metav1.GetOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check placement deployment has debug-server container
			Eventually(func() error {
				deployment, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubPlacementDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				for _, container := range deployment.Spec.Template.Spec.Containers {
					if container.Name == "debug-server" {
						return nil
					}
				}
				return fmt.Errorf("debug-server container not found")
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			By("Disable PlacementDebugServer feature gate")
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				clusterManager.Spec.PlacementConfiguration = &operatorapiv1.PlacementConfiguration{
					FeatureGates: []operatorapiv1.FeatureGate{
						{
							Feature: string(feature.PlacementDebugServer),
							Mode:    operatorapiv1.FeatureGateModeTypeDisable,
						},
					},
				}
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check placement service is deleted
			Eventually(func() bool {
				_, err := kubeClient.CoreV1().Services(hubNamespace).Get(ctx, placementServiceName, metav1.GetOptions{})
				if err == nil {
					return false
				}
				return errors.IsNotFound(err)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Check placement deployment has no debug-server container
			Eventually(func() error {
				deployment, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubPlacementDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				for _, container := range deployment.Spec.Template.Spec.Containers {
					if container.Name == "debug-server" {
						return fmt.Errorf("debug-server container should not exist")
					}
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// remove PlacementConfiguration to not affect subsequent tests
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				clusterManager.Spec.PlacementConfiguration = nil
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should have expected resource created/deleted when feature gates ClusterProfile enabled/disabled", func(ctx SpecContext) {
			By("Enable ClusterProfile feature gate")
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(
					ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				featureGate := []operatorapiv1.FeatureGate{
					{
						Feature: string(feature.ClusterProfile),
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
				}
				if clusterManager.Spec.RegistrationConfiguration != nil {
					for _, fg := range clusterManager.Spec.RegistrationConfiguration.FeatureGates {
						if fg.Feature != string(feature.ClusterProfile) {
							featureGate = append(featureGate, fg)
						}
					}
				}
				clusterManager.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{
					FeatureGates: featureGate,
				}
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(
					ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check clusterrole
			Eventually(func() error {
				cr, err := kubeClient.RbacV1().ClusterRoles().Get(
					ctx, hubRegistrationClusterRole, metav1.GetOptions{})
				if err != nil {
					return err
				}
				for _, rule := range cr.Rules {
					apiGrouSet := sets.New[string](rule.APIGroups...)
					if apiGrouSet.Has("multicluster.x-k8s.io") {
						return nil
					}
				}
				return fmt.Errorf("expected multicluster.x-k8s.io rules to exist")
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			By("Revert ClusterProfile to disable mode")
			// Check ClusterProfile disable
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(
					ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				featureGate := []operatorapiv1.FeatureGate{
					{
						Feature: string(feature.ClusterProfile),
						Mode:    operatorapiv1.FeatureGateModeTypeDisable,
					},
				}
				if clusterManager.Spec.RegistrationConfiguration != nil {
					for _, fg := range clusterManager.Spec.RegistrationConfiguration.FeatureGates {
						if fg.Feature != string(feature.ClusterProfile) {
							featureGate = append(featureGate, fg)
						}
					}
				}
				clusterManager.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{
					FeatureGates: featureGate,
				}
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(
					ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				cr, err := kubeClient.RbacV1().ClusterRoles().Get(
					ctx, hubRegistrationClusterRole, metav1.GetOptions{})
				if err != nil {
					return err
				}
				for _, rule := range cr.Rules {
					apiGrouSet := sets.New[string](rule.APIGroups...)
					if apiGrouSet.Has("multicluster.x-k8s.io") {
						return fmt.Errorf("expected multicluster.x-k8s.io rules to not exist")
					}
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should have expected resource created/deleted when feature gates ClusterImporter enabled/disabled", func(ctx SpecContext) {
			By("Enable ClusterImporter feature gate")
			os.Setenv("AGENT_IMAGE", "test-agent:latest")
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(
					ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				featureGate := []operatorapiv1.FeatureGate{
					{
						Feature: string(feature.ClusterImporter),
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
				}
				if clusterManager.Spec.RegistrationConfiguration != nil {
					for _, fg := range clusterManager.Spec.RegistrationConfiguration.FeatureGates {
						if fg.Feature != string(feature.ClusterImporter) {
							featureGate = append(featureGate, fg)
						}
					}
				}
				clusterManager.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{
					FeatureGates: featureGate,
				}
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(
					ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check clusterrole
			Eventually(func() error {
				cr, err := kubeClient.RbacV1().ClusterRoles().Get(
					ctx, hubRegistrationClusterRole, metav1.GetOptions{})
				if err != nil {
					return err
				}
				for _, rule := range cr.Rules {
					apiGrouSet := sets.New[string](rule.APIGroups...)
					if apiGrouSet.Has("cluster.x-k8s.io") {
						return nil
					}
				}
				return fmt.Errorf("expected cluster.x-k8s.io rules to exist")
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// check deployment
			Eventually(func() error {
				deploy, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(
					ctx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				argSet := sets.New[string](deploy.Spec.Template.Spec.Containers[0].Args...)
				if argSet.Has("--agent-image=test-agent:latest") {
					return nil
				}
				return fmt.Errorf("expected agent-image flag to be set")
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			By("Revert ClusterImporter to disable mode")
			// Check ClusterProfile disable
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(
					ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				featureGate := []operatorapiv1.FeatureGate{
					{
						Feature: string(feature.ClusterImporter),
						Mode:    operatorapiv1.FeatureGateModeTypeDisable,
					},
				}
				if clusterManager.Spec.RegistrationConfiguration != nil {
					for _, fg := range clusterManager.Spec.RegistrationConfiguration.FeatureGates {
						if fg.Feature != string(feature.ClusterImporter) {
							featureGate = append(featureGate, fg)
						}
					}
				}
				clusterManager.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{
					FeatureGates: featureGate,
				}
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(
					ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check deployment
			Eventually(func() error {
				deploy, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(
					ctx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				argSet := sets.New[string](deploy.Spec.Template.Spec.Containers[0].Args...)
				if argSet.Has("--agent-image=test-agent:latest") {
					return fmt.Errorf("expected agent-image flag not to be set")
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// check cluster role
			Eventually(func() error {
				cr, err := kubeClient.RbacV1().ClusterRoles().Get(
					ctx, hubRegistrationClusterRole, metav1.GetOptions{})
				if err != nil {
					return err
				}
				for _, rule := range cr.Rules {
					apiGrouSet := sets.New[string](rule.APIGroups...)
					if apiGrouSet.Has("cluster.x-k8s.io") {
						return fmt.Errorf("expected cluster.x-k8s.io rules to not exist")
					}
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
		It("Deployment should be updated when clustermanager is changed", func(ctx SpecContext) {
			Eventually(func() error {
				if _, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubRegistrationDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check if generations are correct
			Eventually(func() error {
				actual, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if actual.Generation != actual.Status.ObservedGeneration {
					return fmt.Errorf("except generation to be %d, but got %d", actual.Status.ObservedGeneration, actual.Generation)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			clusterManager.Spec.RegistrationImagePullSpec = testImage
			_, err = operatorClient.OperatorV1().ClusterManagers().Update(ctx, clusterManager, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() error {
				actual, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				Expect(len(actual.Spec.Template.Spec.Containers)).Should(Equal(1))
				if actual.Spec.Template.Spec.Containers[0].Image != testImage {
					return fmt.Errorf("expected image to be testimage:latest but get %s", actual.Spec.Template.Spec.Containers[0].Image)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			updateDeploymentsStatusSuccess(ctx, kubeClient, hubNamespace,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubWorkControllerDeployment, hubAddOnManagerDeployment, hubAddOnWebhookDeployment)

			// Check if generations are correct
			Eventually(func() error {
				actual, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				if actual.Generation != actual.Status.ObservedGeneration {
					return fmt.Errorf("except generation to be %d, but got %d", actual.Status.ObservedGeneration, actual.Generation)
				}

				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check if relatedResources are correct
			Eventually(func() error {
				actual, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if len(actual.Status.RelatedResources) != 54 {
					return fmt.Errorf("should get 54 relatedResources, actual got %v", len(actual.Status.RelatedResources))
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
		})

		It("Deployment should be added nodeSelector and toleration when add nodePlacement into clustermanager", func(ctx SpecContext) {
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
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
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				actual, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				Expect(len(actual.Spec.Template.Spec.Containers)).Should(Equal(1))
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
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			updateDeploymentsStatusSuccess(ctx, kubeClient, hubNamespace,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubWorkControllerDeployment, hubAddOnManagerDeployment, hubAddOnWebhookDeployment)
		})

		It("Deployment should be reconciled when manually updated", func(ctx SpecContext) {
			Eventually(func() error {
				if _, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubRegistrationDeployment, metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			registrationoDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubRegistrationDeployment, metav1.GetOptions{})
			Expect(err).NotTo(HaveOccurred())
			registrationoDeployment.Spec.Template.Spec.Containers[0].Image = "testimage2:latest"
			_, err = kubeClient.AppsV1().Deployments(hubNamespace).Update(ctx, registrationoDeployment, metav1.UpdateOptions{})
			Expect(err).NotTo(HaveOccurred())
			Eventually(func() error {
				registrationoDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				if registrationoDeployment.Spec.Template.Spec.Containers[0].Image != testImage {
					return fmt.Errorf("image should be testimage:latest, but get %s", registrationoDeployment.Spec.Template.Spec.Containers[0].Image)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Check if generations are correct
			Eventually(func() error {
				actual, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				registrationDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}

				deploymentGeneration := helpers.NewGenerationStatus(appsv1.SchemeGroupVersion.WithResource("deployments"), registrationDeployment)
				actualGeneration := helpers.FindGenerationStatus(actual.Status.Generations, deploymentGeneration)
				if deploymentGeneration.LastGeneration != actualGeneration.LastGeneration {
					return fmt.Errorf("expected LastGeneration shoud be %d, but get %d", actualGeneration.LastGeneration, deploymentGeneration.LastGeneration)
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should have auto approver user set on registration when configured", func(ctx SpecContext) {
			// Update cluster manager configuration
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// Check addon manager enabled mode
				if clusterManager.Spec.RegistrationConfiguration == nil {
					clusterManager.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{}
				}
				clusterManager.Spec.RegistrationConfiguration.AutoApproveUsers = []string{"user1", "user2"}
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				actual, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				Expect(len(actual.Spec.Template.Spec.Containers)).Should(Equal(1))
				for _, arg := range actual.Spec.Template.Spec.Containers[0].Args {
					if arg == "--cluster-auto-approval-users=user1,user2" {
						return nil
					}
				}
				return fmt.Errorf("do not find the cluster-auto-approval-users args, got %v", actual.Spec.Template.Spec.Containers[0].Args)
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should have auto approved csr users set on registration-controller if csr driver is present", func(ctx SpecContext) {
			// Update cluster manager configuration
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// Check addon manager enabled mode
				if clusterManager.Spec.RegistrationConfiguration == nil {
					clusterManager.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{}
				}
				clusterManager.Spec.RegistrationConfiguration.RegistrationDrivers = []operatorapiv1.RegistrationDriverHub{
					{
						AuthType: "csr",
						CSR: &operatorapiv1.CSRConfig{
							AutoApprovedIdentities: []string{"user3", "user4"},
						},
					},
				}
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			Eventually(func() error {
				actual, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				Expect(len(actual.Spec.Template.Spec.Containers)).Should(Equal(1))
				for _, arg := range actual.Spec.Template.Spec.Containers[0].Args {
					if arg == "--auto-approved-csr-users=user3,user4" {
						return nil
					}
				}
				return fmt.Errorf("do not find the auto-approved-csr-users args, got %v", actual.Spec.Template.Spec.Containers[0].Args)
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})

		It("should have labels on resources created by clustermanager", func(ctx SpecContext) {

			labels := map[string]string{helpers.AppLabelKey: "clustermanager", helpers.HubLabelKey: "hub",
				helpers.LabelPrefix + "/cluster-name": "test", "test-label": "test-value", "test-label2": "test-value2"}
			// app and createdByClusterManager are reserved label keys, and will not be changed to the hub resources.
			expectedDeploymentLabels := map[string]string{helpers.AppLabelKey: "deployment name", helpers.HubLabelKey: clusterManagerName,
				"test-label": "test-value", "test-label2": "test-value2"}
			expectedRegistrationLabels := map[string]string{"test-label": "test-value", "test-label2": "test-value2"}
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				// Updating the cluster manager with labels
				clusterManager.Labels = labels
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(ctx, clusterManager, metav1.UpdateOptions{})
				if err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(func() error {
				registrationDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}

				expectedDeploymentLabels[helpers.AppLabelKey] = clusterManagerName + "-registration-controller"
				if !helpers.MapCompare(registrationDeployment.GetLabels(), expectedDeploymentLabels) {
					return fmt.Errorf("expected registration-controller labels to be %v, but got %v", expectedDeploymentLabels, registrationDeployment.GetLabels())
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Ensure labels list string is present on registration-controller as command line args
			Eventually(func() bool {
				registrationDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return false
				}
				commandLineArgs := registrationDeployment.Spec.Template.Spec.Containers[0].Args
				labelsArg, present := findMatchingArg(commandLineArgs, "--labels")
				return present && strings.SplitN(labelsArg, "=", 2)[1] == helpers.ConvertLabelsMapToString(expectedRegistrationLabels)
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			// Compare labels on registration-webhook
			Eventually(func() error {
				registrationDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx,
					hubRegistrationWebhookDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				expectedDeploymentLabels[helpers.AppLabelKey] = clusterManagerName + "-registration-webhook"
				if !helpers.MapCompare(registrationDeployment.GetLabels(), expectedDeploymentLabels) {
					return fmt.Errorf("expected registration-webhook labels to be %v, but got %v", expectedDeploymentLabels, registrationDeployment.GetLabels())
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Compare labels on work-webhook
			Eventually(func() error {
				registrationDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubWorkWebhookDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				expectedDeploymentLabels[helpers.AppLabelKey] = clusterManagerName + "-work-webhook"
				if !helpers.MapCompare(registrationDeployment.GetLabels(), expectedDeploymentLabels) {
					return fmt.Errorf("expected work-webhook labels to be %v, but got %v", expectedDeploymentLabels, registrationDeployment.GetLabels())
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Compare labels on placement-controller
			Eventually(func() error {
				registrationDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubPlacementDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				expectedDeploymentLabels[helpers.AppLabelKey] = clusterManagerName + "-placement-controller"
				if !helpers.MapCompare(registrationDeployment.GetLabels(), expectedDeploymentLabels) {
					return fmt.Errorf("expected placement-controller labels to be %v, but got %v", expectedDeploymentLabels, registrationDeployment.GetLabels())
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Compare labels on addon-manager-controller
			Eventually(func() error {
				registrationDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubAddOnManagerDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				expectedDeploymentLabels[helpers.AppLabelKey] = clusterManagerName + "-addon-manager-controller"
				if !helpers.MapCompare(registrationDeployment.GetLabels(), expectedDeploymentLabels) {
					return fmt.Errorf("expected labels addon-manager-controller to be %v, but got %v", expectedDeploymentLabels, registrationDeployment.GetLabels())
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// Compare labels on work-controller
			Eventually(func() error {
				registrationDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx, hubWorkControllerDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				expectedDeploymentLabels[helpers.AppLabelKey] = clusterManagerName + "-work-controller"
				if !helpers.MapCompare(registrationDeployment.GetLabels(), expectedDeploymentLabels) {
					return fmt.Errorf("expected work-controller labels to be %v, but got %v", expectedDeploymentLabels, registrationDeployment.GetLabels())
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		})
	})

	Context("Cluster manager statuses", func() {
		It("should have correct degraded conditions", func(ctx SpecContext) {
			Eventually(func() error {
				_, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(
					ctx, hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// The cluster manager should be unavailable at first
			util.AssertClusterManagerCondition(clusterManagerName, operatorClient,
				"HubRegistrationDegraded", "UnavailableRegistrationPod", metav1.ConditionTrue)
			util.AssertClusterManagerCondition(clusterManagerName, operatorClient,
				"Progressing", "ClusterManagerDeploymentRolling", metav1.ConditionTrue)
			// Update replica of deployment
			updateDeploymentsStatusSuccess(ctx, kubeClient, hubNamespace,
				hubRegistrationDeployment, hubPlacementDeployment, hubRegistrationWebhookDeployment,
				hubWorkWebhookDeployment, hubWorkControllerDeployment, hubAddOnManagerDeployment, hubAddOnWebhookDeployment)
			// The cluster manager should be functional at last
			util.AssertClusterManagerCondition(clusterManagerName, operatorClient,
				"HubRegistrationDegraded", "RegistrationFunctional", metav1.ConditionFalse)
			util.AssertClusterManagerCondition(clusterManagerName, operatorClient,
				"Progressing", "ClusterManagerUpToDate", metav1.ConditionFalse)
		})
	})

	Context("Serving cert rotation", func() {
		It("should rotate both serving cert and signing cert before they become expired", func(ctx SpecContext) {
			secretNames := []string{"signer-secret", "registration-webhook-serving-cert", "work-webhook-serving-cert"}
			// wait until all secrets and configmap are in place
			Eventually(func() error {
				for _, name := range secretNames {
					if _, err := kubeClient.CoreV1().Secrets(hubNamespace).Get(ctx, name, metav1.GetOptions{}); err != nil {
						return err
					}
				}
				if _, err := kubeClient.CoreV1().ConfigMaps(hubNamespace).Get(ctx, "ca-bundle-configmap", metav1.GetOptions{}); err != nil {
					return err
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// both serving cert and signing cert should always be valid
			Consistently(func() error {
				configmap, err := kubeClient.CoreV1().ConfigMaps(hubNamespace).Get(ctx, "ca-bundle-configmap", metav1.GetOptions{})
				if err != nil {
					return err
				}
				for _, name := range []string{"signer-secret", "registration-webhook-serving-cert", "work-webhook-serving-cert"} {
					secret, err := kubeClient.CoreV1().Secrets(hubNamespace).Get(ctx, name, metav1.GetOptions{})
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
			}, eventuallyTimeout*3, eventuallyInterval*3).Should(BeNil())
		})
	})

	Context("Cluster manager feature gates", func() {
		It("default should be set correctly", func(ctx SpecContext) {
			// remove cluster manager configuration
			Eventually(func() error {
				clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(ctx, clusterManagerName, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// Check addon manager enabled mode
				clusterManager.Spec.RegistrationConfiguration = nil
				// Earlier cases enable ManifestWorkReplicaSet explicitly. Keep only NilExecutorValidating
				// so ManifestWorkReplicaSet falls back to its default.
				clusterManager.Spec.WorkConfiguration = &operatorapiv1.WorkConfiguration{
					WorkDriver: operatorapiv1.WorkDriverTypeKube,
					FeatureGates: []operatorapiv1.FeatureGate{
						{
							Feature: string(feature.NilExecutorValidating),
							Mode:    operatorapiv1.FeatureGateModeTypeEnable,
						},
					},
				}
				_, err = operatorClient.OperatorV1().ClusterManagers().Update(ctx, clusterManager, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
			Eventually(func() error {
				actual, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx,
					hubRegistrationDeployment, metav1.GetOptions{})
				if err != nil {
					return err
				}

				// Check if any argument contains "--feature-gates=DefaultClusterSet=true"
				for _, arg := range actual.Spec.Template.Spec.Containers[0].Args {
					if arg == "--feature-gates=DefaultClusterSet=true" {
						return fmt.Errorf("unexpected argument '--feature-gates=DefaultClusterSet=true' found in args: %v", actual.Spec.Template.Spec.Containers[0].Args)
					}
				}

				return nil // no matching argument found, which is the expected condition
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil(), "Expected '--feature-gates=DefaultClusterSet=true' to be absent in Args, but it was found")

			util.AssertClusterManagerCondition(clusterManagerName, operatorClient,
				helpers.FeatureGatesTypeValid, helpers.FeatureGatesReasonAllValid, metav1.ConditionTrue)

			workDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx,
				hubWorkWebhookDeployment, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(workDeployment.Spec.Template.Spec.Containers[0].Args).Should(
				ContainElement("--feature-gates=NilExecutorValidating=true"))
			// ManifestWorkReplicaSet is enabled by default, so enabling it adds no flag.
			Expect(workDeployment.Spec.Template.Spec.Containers[0].Args).ShouldNot(
				ContainElement("--feature-gates=ManifestWorkReplicaSet=true"))

			workHubControllerDeployment, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(ctx,
				hubWorkControllerDeployment, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())
			Expect(workHubControllerDeployment.Spec.Template.Spec.Containers[0].Args).Should(
				ContainElement("manager"))
		})
	})

})

// ClusterManager TLS profile tests verify that TLS flags are injected into all hub deployments
// when the ocm-tls-profile ConfigMap is present at operator startup.
var _ = Describe("ClusterManager TLS Profile", func() {
	var cancel context.CancelFunc
	var hubRegistrationDeployment = fmt.Sprintf("%s-registration-controller", clusterManagerName)
	var hubPlacementDeployment = fmt.Sprintf("%s-placement-controller", clusterManagerName)
	var hubRegistrationWebhookDeployment = fmt.Sprintf("%s-registration-webhook", clusterManagerName)
	var hubWorkWebhookDeployment = fmt.Sprintf("%s-work-webhook", clusterManagerName)
	var hubAddOnManagerDeployment = fmt.Sprintf("%s-addon-manager-controller", clusterManagerName)
	var hubWorkControllerDeployment = fmt.Sprintf("%s-work-controller", clusterManagerName)
	var hubAddOnWebhookDeployment = fmt.Sprintf("%s-addon-webhook", clusterManagerName)

	BeforeEach(func(ctx SpecContext) {
		// Enable PlacementDebugServer feature gate
		Eventually(func() error {
			clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(
				ctx, clusterManagerName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			clusterManager.Spec.PlacementConfiguration = &operatorapiv1.PlacementConfiguration{
				FeatureGates: []operatorapiv1.FeatureGate{
					{
						Feature: string(feature.PlacementDebugServer),
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
				},
			}
			_, err = operatorClient.OperatorV1().ClusterManagers().Update(
				ctx, clusterManager, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

		// Create the ocm-tls-profile ConfigMap BEFORE starting the operator so that:
		// 1. The watcher seeds its hash from the ConfigMap at startup (no change detected).
		// 2. The operator initialises with TLS settings from the ConfigMap.
		// If the ConfigMap were created after startup the watcher would detect a hash change
		// and call os.Exit(0), which would kill the test process.
		// The ConfigMap is created in metav1.NamespaceDefault ("default") which matches
		// the OperatorNamespace set in startHubOperator (also metav1.NamespaceDefault).
		tlsCM := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "ocm-tls-profile",
				Namespace: metav1.NamespaceDefault,
			},
			Data: map[string]string{
				"minTLSVersion": "VersionTLS13",
			},
		}
		_, err := kubeClient.CoreV1().ConfigMaps(metav1.NamespaceDefault).Create(
			ctx, tlsCM, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		var runCtx context.Context
		runCtx, cancel = context.WithCancel(context.Background())
		go startHubOperator(runCtx, operatorapiv1.InstallModeDefault)
	})

	AfterEach(func(ctx SpecContext) {
		if cancel != nil {
			cancel()
		}
		err := kubeClient.CoreV1().ConfigMaps(metav1.NamespaceDefault).Delete(
			ctx, "ocm-tls-profile", metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			GinkgoT().Logf("failed to delete ocm-tls-profile ConfigMap: %v", err)
		}
		err = kubeClient.AppsV1().Deployments(hubNamespace).DeleteCollection(
			ctx, metav1.DeleteOptions{}, metav1.ListOptions{})
		Expect(err).NotTo(HaveOccurred())

		// Clean up PlacementConfiguration and RegistrationConfiguration to avoid affecting other tests,
		// but keep any existing feature gates (e.g. NetworkPolicies) intact.
		Eventually(func() error {
			clusterManager, err := operatorClient.OperatorV1().ClusterManagers().Get(
				ctx, clusterManagerName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			clusterManager.Spec.PlacementConfiguration = nil
			var featureGates []operatorapiv1.FeatureGate
			if clusterManager.Spec.RegistrationConfiguration != nil {
				featureGates = clusterManager.Spec.RegistrationConfiguration.FeatureGates
			}
			if len(featureGates) == 0 {
				clusterManager.Spec.RegistrationConfiguration = nil
			} else {
				clusterManager.Spec.RegistrationConfiguration = &operatorapiv1.RegistrationHubConfiguration{
					FeatureGates: featureGates,
				}
			}
			_, err = operatorClient.OperatorV1().ClusterManagers().Update(
				ctx, clusterManager, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
	})

	It("should inject tls-min-version into all hub deployments when ocm-tls-profile ConfigMap exists at startup", func(ctx SpecContext) {
		// All hub deployments that get TLS flags injected.
		tlsDeployments := []string{
			hubRegistrationDeployment,
			hubRegistrationWebhookDeployment,
			hubPlacementDeployment,
			hubWorkWebhookDeployment,
			hubWorkControllerDeployment,
			hubAddOnManagerDeployment,
			hubAddOnWebhookDeployment,
		}
		for _, deployName := range tlsDeployments {
			deployName := deployName // capture
			Eventually(func() error {
				actual, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(
					ctx, deployName, metav1.GetOptions{})
				if err != nil {
					return err
				}
				for i, container := range actual.Spec.Template.Spec.Containers {
					found := false
					for _, arg := range container.Args {
						if arg == "--tls-min-version=VersionTLS13" {
							found = true
							break
						}
					}
					if !found {
						return fmt.Errorf("deployment %s container[%d] %s: --tls-min-version=VersionTLS13 not found in args %v",
							deployName, i, container.Name, container.Args)
					}
				}
				return nil
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
		}
	})

	It("should inject tls-min-version into gRPC server deployment when gRPC auth is enabled", func(ctx SpecContext) {
		// Enable gRPC auth so the operator creates the gRPC server deployment.
		Eventually(func() error {
			return enableGRPCAuth(ctx, operatorClient, clusterManagerName)
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

		hubGRPCServerDeployment := fmt.Sprintf("%s-grpc-server", clusterManagerName)
		Eventually(func() error {
			actual, err := kubeClient.AppsV1().Deployments(hubNamespace).Get(
				ctx, hubGRPCServerDeployment, metav1.GetOptions{})
			if err != nil {
				return err
			}
			for _, arg := range actual.Spec.Template.Spec.Containers[0].Args {
				if arg == "--tls-min-version=VersionTLS13" {
					return nil
				}
			}
			return fmt.Errorf("deployment %s: --tls-min-version=VersionTLS13 not found in args %v",
				hubGRPCServerDeployment, actual.Spec.Template.Spec.Containers[0].Args)
		}, eventuallyTimeout, eventuallyInterval).Should(BeNil())
	})
})
