package e2e

import (
	"context"
	"flag"
	"fmt"
	"os"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	clusterName           string
	hubKubeconfig         string
	nilExecutorValidating bool

	deployWorkAgent       bool
	workImage             string
	managedKubeconfig     string
	webhookDeploymentName string

	hubNamespace = "open-cluster-management-hub"
)

func init() {
	flag.StringVar(&clusterName, "cluster-name", "", "The name of the managed cluster on which the testing will be run")
	flag.StringVar(&hubKubeconfig, "hub-kubeconfig", "", "The kubeconfig of the hub cluster")
	flag.BoolVar(&nilExecutorValidating, "nil-executor-validating", false, "Whether validate the nil executor or not")
	flag.BoolVar(&deployWorkAgent, "deploy-agent", false, "Whether deploy the work agent on the managed cluster or not")
	flag.StringVar(&workImage, "work-image", "", "The image used to deploy the work agent")
	flag.StringVar(&managedKubeconfig, "managed-kubeconfig", "", "The kubeconfig of the managed cluster")
	flag.StringVar(&webhookDeploymentName, "webhook-deployment-name", "", "The deployment name of the work validating webhook on the hub cluster. If specified, the webhook status will be checked before run the test cases")
}

var (
	hubClient          kubernetes.Interface
	hubRestConfig      *rest.Config
	managedRestConfig  *rest.Config
	spokeKubeClient    kubernetes.Interface
	spokeDynamicClient dynamic.Interface
	hubWorkClient      workclientset.Interface
	spokeWorkClient    workclientset.Interface
	agentDeployer      workAgentDeployer
)

func TestE2e(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "ManifestWork E2E Suite")
}

// This suite is sensitive to the following environment variables:
//
//   - IMAGE_NAME sets the exact image to deploy for the work agent
//   - IMAGE_REGISTRY sets the image registry to use to build the IMAGE_NAME if
//     IMAGE_NAME is unset: IMAGE_REGISTRY/work:latest
//   - KUBECONFIG is the location of the kubeconfig file to use
var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))
	var err error

	// pick up hubKubeconfig from argument first,
	// and then fall back to environment variable
	if len(hubKubeconfig) == 0 {
		hubKubeconfig = os.Getenv("KUBECONFIG")
	}
	gomega.Expect(hubKubeconfig).ToNot(gomega.BeEmpty(), "hubKubeconfig is not specified.")

	hubRestConfig, err = clientcmd.BuildConfigFromFlags("", hubKubeconfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	hubClient, err = kubernetes.NewForConfig(hubRestConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	hubWorkClient, err = workclientset.NewForConfig(hubRestConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// pick up managedKubeconfig from argument first,
	// and then fall back to hubKubeconfig
	if len(managedKubeconfig) == 0 {
		managedKubeconfig = hubKubeconfig
	}
	gomega.Expect(managedKubeconfig).ToNot(gomega.BeEmpty(), "managedKubeconfig is not specified.")

	managedRestConfig, err = clientcmd.BuildConfigFromFlags("", managedKubeconfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	spokeKubeClient, err = kubernetes.NewForConfig(managedRestConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	spokeWorkClient, err = workclientset.NewForConfig(managedRestConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	spokeDynamicClient, err = dynamic.NewForConfig(managedRestConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	if !deployWorkAgent {
		gomega.Expect(clusterName).ToNot(gomega.BeEmpty(), "clusterName is not specified")
	} else {
		if len(clusterName) == 0 {
			clusterName = fmt.Sprintf("cluster-%s", rand.String(5))
		}

		spokeApiExtensionsClient, err := apiextensionsclient.NewForConfig(managedRestConfig)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// create cluster namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: clusterName,
			},
		}
		_, err = hubClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// pick up work image from argument first,
		// and then fall back to environment variable
		if len(workImage) == 0 {
			workImage = os.Getenv("IMAGE_NAME")
		}
		if len(workImage) == 0 {
			imageRegistry := os.Getenv("IMAGE_REGISTRY")
			if len(imageRegistry) == 0 {
				imageRegistry = "quay.io/open-cluster-management"
			}
			gomega.Expect(imageRegistry).ToNot(gomega.BeEmpty())
			workImage = fmt.Sprintf("%v/work:latest", imageRegistry)
		}
		gomega.Expect(workImage).ToNot(gomega.BeEmpty())

		// deploy work agent
		ginkgo.By(fmt.Sprintf("deploy work agent with cluster name %q...", clusterName))
		agentDeployer = newDefaultWorkAgentDeployer(
			clusterName,
			workImage,
			spokeKubeClient,
			spokeDynamicClient,
			spokeApiExtensionsClient,
			hubWorkClient)
		err = agentDeployer.Deploy()
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	if len(webhookDeploymentName) == 0 {
		return
	}

	// verify the status of the work validating webhook on the hub cluster
	ginkgo.By(fmt.Sprintf("wait until the work validating webhook %s/%s is ready...", hubNamespace, webhookDeploymentName))
	gomega.Eventually(func() error {
		var err error
		var workWebhookPods *v1.PodList
		var mutatingWebhookDeployment *appsv1.Deployment

		if workWebhookPods, err = hubClient.CoreV1().Pods(hubNamespace).List(context.Background(),
			metav1.ListOptions{LabelSelector: fmt.Sprintf("app=%s", webhookDeploymentName)}); err != nil {
			return err
		}
		if mutatingWebhookDeployment, err = hubClient.AppsV1().Deployments(hubNamespace).Get(context.Background(),
			webhookDeploymentName, metav1.GetOptions{}); err != nil {
			return err
		}

		pods := len(workWebhookPods.Items)
		replicas := *mutatingWebhookDeployment.Spec.Replicas
		readyReplicas := mutatingWebhookDeployment.Status.ReadyReplicas

		if pods != (int)(replicas) {
			return fmt.Errorf("deployment %s pods should have %d but got %d ", webhookDeploymentName, replicas, pods)
		}

		if readyReplicas != replicas {
			return fmt.Errorf("deployment %s should have %d but got %d ready replicas", webhookDeploymentName, replicas, readyReplicas)
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())
})

var _ = ginkgo.AfterSuite(func() {
	if !deployWorkAgent {
		return
	}

	// delete cluster namespace on the hub cluster
	err := hubClient.CoreV1().Namespaces().Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
	if err != nil {
		gomega.Expect(errors.IsNotFound(err)).To(gomega.BeTrue())
	}

	if agentDeployer == nil {
		return
	}

	// undeploy work agent
	ginkgo.By("undeploy work agent...")
	err = agentDeployer.Undeploy()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})
