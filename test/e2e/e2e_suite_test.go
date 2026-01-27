package e2e

import (
	"context"
	"crypto/sha256"
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"

	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/test/framework"
)

var (
	// kubeconfigs
	hubKubeconfig     string
	managedKubeconfig string

	// customized test parameters
	klusterletDeployMode  string
	nilExecutorValidating bool

	// images
	registrationImage string
	workImage         string
	singletonImage    string
	images            framework.Images

	// expected image tag for validation
	expectedImageTag string

	// hub hash
	hubHash string

	// registration driver
	registrationDriver string

	// bootstrap-hub-kubeconfig
	// It's a secret named 'bootstrap-hub-kubeconfig' under the namespace 'open-cluster-management-agent',
	// the content of the secret is a kubeconfig file.
	//
	// The secret is used when:
	// 1. klusterlet is created not in the namespace 'open-cluster-management-agent', but a customized namespace.
	// 2. in hosted,bootstrap-hub-kubeconfig secret under the ns "open-cluster-management-agent".
	bootstrapHubKubeConfigSecret *corev1.Secret
)

func init() {
	flag.StringVar(&hubKubeconfig, "hub-kubeconfig", "", "The kubeconfig of the hub cluster")
	flag.StringVar(&managedKubeconfig, "managed-kubeconfig", "", "The kubeconfig of the managed cluster")

	flag.StringVar(&klusterletDeployMode, "klusterlet-deploy-mode", string(operatorapiv1.InstallModeDefault), "The image of the work")
	flag.BoolVar(&nilExecutorValidating, "nil-executor-validating", false, "Whether validate the nil executor or not (default false)")

	flag.StringVar(&registrationImage, "registration-image", "", "The image of the registration")
	flag.StringVar(&workImage, "work-image", "", "The image of the work")
	flag.StringVar(&singletonImage, "singleton-image", "", "The image of the klusterlet agent")
	flag.StringVar(&expectedImageTag, "expected-image-tag", "", "The expected image tag for all OCM components (e.g., 'e2e')")
	flag.StringVar(&registrationDriver, "registration-driver", "csr", "The registration driver (csr or grpc)")
}

var hub *framework.Hub
var spoke *framework.Spoke

// The e2e will always create one universal klusterlet, the developers can reuse this klusterlet in their case
// but also pay attention, because the klusterlet is shared, so the developers should not delete the klusterlet.
// And there might be some side effects on other cases if the developers change the klusterlet's spec for their cases.
const (
	universalClusterName    = "e2e-universal-managedcluster"            //nolint:gosec
	universalKlusterletName = "e2e-universal-klusterlet"                //nolint:gosec
	universalAgentNamespace = "open-cluster-management-agent-universal" //nolint:gosec
	universalClusterSetName = "universal"                               //nolint:gosec
)

func TestE2E(tt *testing.T) {
	OutputFail := func(message string, callerSkip ...int) {
		Fail(message, callerSkip...)
	}

	RegisterFailHandler(OutputFail)
	RunSpecs(tt, "ocm E2E Suite")
}

// This suite is sensitive to the following environment variables:
//
// - KUBECONFIG is the location of the kubeconfig file to use
var _ = BeforeSuite(func() {
	var err error

	// Setup kubeconfigs
	if hubKubeconfig == "" {
		hubKubeconfig = os.Getenv("KUBECONFIG")
	}
	if managedKubeconfig == "" {
		managedKubeconfig = os.Getenv("KUBECONFIG")
	}

	// Setup images
	images = framework.Images{
		RegistrationImage: registrationImage,
		WorkImage:         workImage,
		SingletonImage:    singletonImage,
	}

	// In most OCM cases, we expect user should see the result in 90 seconds.
	// For cases that need more than 90 seconds, please set the timeout in the test case EXPLICITLY.
	SetDefaultEventuallyTimeout(90 * time.Second)
	SetDefaultEventuallyPollingInterval(5 * time.Second)

	By("Setup hub")
	hub, err = framework.NewHub(hubKubeconfig)
	Expect(err).ToNot(HaveOccurred())

	By("Setup spokeTestHelper")
	spoke, err = framework.NewSpoke(managedKubeconfig)
	Expect(err).ToNot(HaveOccurred())

	By("Setup default bootstrap-hub-kubeconfig")
	bootstrapHubKubeConfigSecret, err = spoke.KubeClient.CoreV1().Secrets(helpers.KlusterletDefaultNamespace).
		Get(context.TODO(), helpers.BootstrapHubKubeConfig, metav1.GetOptions{})
	Expect(err).ToNot(HaveOccurred())
	// The secret will used via copy and create another secret in other ns, so we need to clean the resourceVersion and namespace
	bootstrapHubKubeConfigSecret.ObjectMeta.ResourceVersion = ""
	bootstrapHubKubeConfigSecret.ObjectMeta.Namespace = ""

	By("Calculate Hub Hash")
	kubeconfigData, err := clientcmd.Load(bootstrapHubKubeConfigSecret.Data["kubeconfig"])
	Expect(err).NotTo(HaveOccurred())
	kubeconfig, err := clientcmd.NewDefaultClientConfig(*kubeconfigData, nil).ClientConfig()
	Expect(err).NotTo(HaveOccurred())
	hubHash = fmt.Sprintf("%x", sha256.Sum256([]byte(kubeconfig.Host)))

	By("Check Hub Ready")
	Eventually(func() error {
		return hub.CheckHubReady()
	}).Should(Succeed())

	By("Check Klusterlet Operator Ready")
	Eventually(func() error {
		return framework.CheckDeploymentReady(context.TODO(), spoke.KubeClient, spoke.KlusterletOperatorNamespace, spoke.KlusterletOperator)
	}).Should(Succeed())

	By("Enable Work Feature")
	if nilExecutorValidating {
		Eventually(func() error {
			return hub.EnableHubWorkFeature("NilExecutorValidating")
		}).Should(Succeed())
	}

	By("Enable ManifestWorkReplicaSet Feature")
	Eventually(func() error {
		return hub.EnableHubWorkFeature("ManifestWorkReplicaSet")
	}).Should(Succeed())

	By("Enable ClusterImporter Feature")
	Eventually(func() error {
		return hub.EnableHubRegistrationFeature("ClusterImporter")
	}).Should(Succeed())

	By("Enable CleanUpCompletedManifestWork feature gate")
	Eventually(func() error {
		return hub.EnableHubWorkFeature("CleanUpCompletedManifestWork")
	}).Should(Succeed())
	Eventually(func() error {
		return hub.CheckHubReady()
	}).Should(Succeed())

	By("Validate image configuration")
	validateImageConfiguration()

	By("Create a universal Klusterlet/managedcluster")
	framework.CreateAndApproveKlusterlet(
		hub, spoke,
		universalKlusterletName, universalClusterName, universalAgentNamespace, operatorapiv1.InstallMode(klusterletDeployMode),
		bootstrapHubKubeConfigSecret, images, registrationDriver,
	)

	By("Create a universal ClusterSet and bind it with the universal managedcluster")
	_, err = hub.ClusterClient.ClusterV1beta2().ManagedClusterSets().Create(context.TODO(), &clusterv1beta2.ManagedClusterSet{
		ObjectMeta: metav1.ObjectMeta{
			Name: universalClusterSetName,
		},
		Spec: clusterv1beta2.ManagedClusterSetSpec{
			ClusterSelector: clusterv1beta2.ManagedClusterSelector{
				SelectorType: clusterv1beta2.ExclusiveClusterSetLabel,
			},
		},
	}, metav1.CreateOptions{})
	if err != nil {
		// ignore the already exists error so we can run the e2e test multiple times locally
		if !errors.IsAlreadyExists(err) {
			Expect(err).ToNot(HaveOccurred())
		}
	}

	Eventually(func() error {
		umc, err := hub.ClusterClient.ClusterV1().ManagedClusters().Get(context.TODO(), universalClusterName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		labels := umc.GetLabels()
		if labels == nil {
			labels = make(map[string]string)
		}
		labels[clusterv1beta2.ClusterSetLabel] = universalClusterSetName
		umc.SetLabels(labels)
		_, err = hub.ClusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), umc, metav1.UpdateOptions{})
		return err
	}).Should(Succeed())
})

var _ = AfterSuite(func() {
	By(fmt.Sprintf("clean klusterlet %v resources after the test case", universalKlusterletName))
	framework.CleanKlusterletRelatedResources(hub, spoke, universalKlusterletName, universalClusterName)
})

// validateImageConfiguration validates that all image configurations use the expected tag
func validateImageConfiguration() {
	if expectedImageTag == "" {
		By("Skipping image validation: no expected image tag specified")
		return
	}

	By(fmt.Sprintf("Validating image configuration uses expected tag: %s", expectedImageTag))

	// Validate test image variables
	validateTestImageVariables()

	// Validate ClusterManager spec after it's deployed
	Eventually(validateClusterManagerImageSpecs).Should(Succeed())

	// Validate actual deployment containers
	validateDeploymentContainers()
}

func validateTestImageVariables() {
	if registrationImage != "" && !strings.HasSuffix(registrationImage, ":"+expectedImageTag) {
		Fail(fmt.Sprintf("registrationImage does not end with expected tag: %s (actual: %s)", expectedImageTag, registrationImage))
	}

	if workImage != "" && !strings.HasSuffix(workImage, ":"+expectedImageTag) {
		Fail(fmt.Sprintf("workImage does not end with expected tag: %s (actual: %s)", expectedImageTag, workImage))
	}

	if singletonImage != "" && !strings.HasSuffix(singletonImage, ":"+expectedImageTag) {
		Fail(fmt.Sprintf("singletonImage does not end with expected tag: %s (actual: %s)", expectedImageTag, singletonImage))
	}
}

func validateClusterManagerImageSpecs() error {
	ctx := context.TODO()

	// Get the ClusterManager resource
	clusterManager, err := hub.OperatorClient.OperatorV1().ClusterManagers().Get(ctx, "cluster-manager", metav1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get ClusterManager: %v", err)
	}

	// Check each imagePullSpec field
	spec := clusterManager.Spec

	if spec.RegistrationImagePullSpec != "" {
		if err := validateImageTagInSpec(spec.RegistrationImagePullSpec, "registrationImagePullSpec"); err != nil {
			return err
		}
	}

	if spec.WorkImagePullSpec != "" {
		if err := validateImageTagInSpec(spec.WorkImagePullSpec, "workImagePullSpec"); err != nil {
			return err
		}
	}

	if spec.PlacementImagePullSpec != "" {
		if err := validateImageTagInSpec(spec.PlacementImagePullSpec, "placementImagePullSpec"); err != nil {
			return err
		}
	}

	if spec.AddOnManagerImagePullSpec != "" {
		if err := validateImageTagInSpec(spec.AddOnManagerImagePullSpec, "addOnManagerImagePullSpec"); err != nil {
			return err
		}
	}

	return nil
}

func validateImageTagInSpec(imageSpec, fieldName string) error {
	if !strings.HasSuffix(imageSpec, ":"+expectedImageTag) {
		return fmt.Errorf("ClusterManager.spec.%s does not end with expected tag: %s (actual: %s)",
			fieldName, expectedImageTag, imageSpec)
	}
	return nil
}

func validateDeploymentContainers() {
	By("Validating deployment container images")

	validateClusterManagerImageTags()
	validateKlusterletImageTags()
}

func validateClusterManagerImageTags() {
	By("Checking cluster-manager deployment containers")
	ctx := context.TODO()

	Eventually(func() error {
		deployment, err := hub.KubeClient.AppsV1().Deployments("open-cluster-management").Get(ctx, "cluster-manager", metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get cluster-manager deployment: %v", err)
		}

		for _, container := range deployment.Spec.Template.Spec.Containers {
			if err := validateImageTag(container.Image, container.Name, "cluster-manager"); err != nil {
				return err
			}
		}

		// Also check init containers if any
		for _, container := range deployment.Spec.Template.Spec.InitContainers {
			if err := validateImageTag(container.Image, container.Name, "cluster-manager"); err != nil {
				return err
			}
		}

		return nil
	}).Should(Succeed())
}

func validateKlusterletImageTags() {
	By("Checking klusterlet deployment containers")
	ctx := context.TODO()

	Eventually(func() error {
		deployment, err := hub.KubeClient.AppsV1().Deployments("open-cluster-management").Get(ctx, "klusterlet", metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get klusterlet deployment: %v", err)
		}

		for _, container := range deployment.Spec.Template.Spec.Containers {
			if err := validateImageTag(container.Image, container.Name, "klusterlet"); err != nil {
				return err
			}
		}

		// Also check init containers if any
		for _, container := range deployment.Spec.Template.Spec.InitContainers {
			if err := validateImageTag(container.Image, container.Name, "klusterlet"); err != nil {
				return err
			}
		}

		return nil
	}).Should(Succeed())
}

func validateImageTag(image, containerName, deploymentName string) error {
	// Extract the tag from the image
	// Image format: registry/repo:tag or registry/repo@digest
	var actualTag string

	switch {
	case strings.Contains(image, "@"):
		// Handle digest format (registry/repo@sha256:...)
		return fmt.Errorf("container %s in %s deployment uses digest format (%s), cannot validate tag",
			containerName, deploymentName, image)
	case strings.Contains(image, ":"):
		// Handle tag format (registry/repo:tag)
		parts := strings.Split(image, ":")
		actualTag = parts[len(parts)-1]
	default:
		// No tag specified, defaults to "latest"
		actualTag = "latest"
	}

	// Only validate OCM-related images (skip system images like kube-rbac-proxy, etc.)
	if isOCMImage(image) {
		if actualTag != expectedImageTag {
			return fmt.Errorf("container %s in %s deployment has incorrect image tag: expected %s, got %s (image: %s)",
				containerName, deploymentName, expectedImageTag, actualTag, image)
		}
	}

	return nil
}

func isOCMImage(image string) bool {
	// Check if this is an OCM component image
	ocmPatterns := []string{
		"open-cluster-management",
		"registration",
		"work",
		"placement",
		"addon-manager",
		"klusterlet",
	}

	for _, pattern := range ocmPatterns {
		if strings.Contains(image, pattern) {
			return true
		}
	}

	return false
}
