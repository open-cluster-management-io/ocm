package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"

	ginkgo "github.com/onsi/ginkgo"
	gomega "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/retry"
	apiregistrationclient "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset/typed/apiregistration/v1"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
)

var hubNamespace = "open-cluster-management-hub"
var mutatingWebhookName = "managedcluster-admission"
var mutatingWebhookContainerName = "managedcluster-admission"

func TestE2E(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2E suite")
}

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

var (
	hubClient           kubernetes.Interface
	hubDynamicClient    dynamic.Interface
	hubAPIServiceClient *apiregistrationclient.ApiregistrationV1Client
	hubAddOnClient      addonclient.Interface
	clusterClient       clusterclient.Interface
	registrationImage   string
	clusterCfg          *rest.Config
)

// This suite is sensitive to the following environment variables:
//
// - IMAGE_NAME sets the exact image to deploy for the registration agent
// - IMAGE_REGISTRY sets the image registry to use to build the IMAGE_NAME if
//   IMAGE_NAME is unset: IMAGE_REGISTRY/registration:latest
// - KUBECONFIG is the location of the kubeconfig file to use
var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))
	registrationImage = os.Getenv("IMAGE_NAME")
	if registrationImage == "" {
		imageRegistry := os.Getenv("IMAGE_REGISTRY")
		if imageRegistry == "" {
			imageRegistry = "quay.io/open-cluster-management"
		}
		registrationImage = fmt.Sprintf("%v/registration:latest", imageRegistry)
	}

	kubeconfig := os.Getenv("KUBECONFIG")
	err := func() error {
		var err error
		clusterCfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return err
		}

		hubClient, err = kubernetes.NewForConfig(clusterCfg)
		if err != nil {
			return err
		}

		hubDynamicClient, err = dynamic.NewForConfig(clusterCfg)
		if err != nil {
			return err
		}

		hubAPIServiceClient, err = apiregistrationclient.NewForConfig(clusterCfg)
		if err != nil {
			return err
		}

		hubAddOnClient, err = addonclient.NewForConfig(clusterCfg)
		if err != nil {
			return err
		}

		clusterClient, err = clusterclient.NewForConfig(clusterCfg)
		//Enable DefaultClusterSet feature gates in mutatingWebhook
		mutatingWebhookDeployment, err := hubClient.AppsV1().Deployments(hubNamespace).Get(context.Background(), mutatingWebhookName, metav1.GetOptions{})
		webhookContainers := mutatingWebhookDeployment.Spec.Template.Spec.Containers
		var updatedContainer []v1.Container
		for _, webhookContainer := range webhookContainers {
			if webhookContainer.Name == mutatingWebhookContainerName {
				webhookContainer.Args = append(webhookContainer.Args, "--feature-gates=DefaultClusterSet=true")
			}
			updatedContainer = append(updatedContainer, webhookContainer)
		}
		mutatingWebhookDeployment.Spec.Template.Spec.Containers = updatedContainer
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			_, err := hubClient.AppsV1().Deployments(hubNamespace).Update(context.Background(), mutatingWebhookDeployment, metav1.UpdateOptions{})
			return err
		})
		return err
	}()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})
