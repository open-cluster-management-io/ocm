package e2e

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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
	Eventually(func() error {
		return hub.CheckHubReady()
	}).Should(Succeed())

	By("Enable ClusterImporter Feature")
	Eventually(func() error {
		return hub.EnableHubRegistrationFeature("ClusterImporter")
	}).Should(Succeed())
	Eventually(func() error {
		return hub.CheckHubReady()
	}).Should(Succeed())

	By("Create a universal Klusterlet/managedcluster")
	framework.CreateAndApproveKlusterlet(
		hub, spoke,
		universalKlusterletName, universalClusterName, universalAgentNamespace, operatorapiv1.InstallMode(klusterletDeployMode),
		bootstrapHubKubeConfigSecret, images,
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
