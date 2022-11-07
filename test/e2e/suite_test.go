package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/onsi/ginkgo"
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

var hubNamespace = "open-cluster-management-hub"
var mutatingWebhookName = "work-webhook"

var (
	nameSuffix         string
	clusterName        string
	workImage          string
	hubClient          kubernetes.Interface
	restConfig         *rest.Config
	spokeKubeClient    kubernetes.Interface
	spokeDynamicClient dynamic.Interface
	hubWorkClient      workclientset.Interface
	agentDeployer      workAgentDeployer
)

func TestE2e(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "E2e Suite")
}

// This suite is sensitive to the following environment variables:
//
//   - IMAGE_NAME sets the exact image to deploy for the work agent
//   - IMAGE_REGISTRY sets the image registry to use to build the IMAGE_NAME if
//     IMAGE_NAME is unset: IMAGE_REGISTRY/work:latest
//   - KUBECONFIG is the location of the kubeconfig file to use
var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))
	workImage = os.Getenv("IMAGE_NAME")
	if workImage == "" {
		imageRegistry := os.Getenv("IMAGE_REGISTRY")
		if imageRegistry == "" {
			imageRegistry = "quay.io/open-cluster-management"
		}
		workImage = fmt.Sprintf("%v/work:latest", imageRegistry)
	}

	var err error
	kubeconfig := os.Getenv("KUBECONFIG")
	restConfig, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	spokeKubeClient, err = kubernetes.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	spokeDynamicClient, err = dynamic.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	hubWorkClient, err = workclientset.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	hubClient, err = kubernetes.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	spokeApiExtensionsClient, err := apiextensionsclient.NewForConfig(restConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	nameSuffix = rand.String(5)

	// create cluster namespace
	clusterName = fmt.Sprintf("cluster-%s", nameSuffix)
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterName,
		},
	}
	_, err = spokeKubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// deploy work agent
	ginkgo.By(fmt.Sprintf("deploy work agent with cluster name %q...", clusterName))
	agentDeployer = newDefaultWorkAgentDeployer(
		clusterName,
		nameSuffix,
		workImage,
		spokeKubeClient,
		spokeDynamicClient,
		spokeApiExtensionsClient,
		hubWorkClient)
	err = agentDeployer.Deploy()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	gomega.Eventually(func() error {
		var err error
		var workWebhookPods *v1.PodList
		var mutatingWebhookDeployment *appsv1.Deployment

		if workWebhookPods, err = hubClient.CoreV1().Pods(hubNamespace).List(context.Background(),
			metav1.ListOptions{LabelSelector: fmt.Sprintf("app=%s", mutatingWebhookName)}); err != nil {
			return err
		}
		if mutatingWebhookDeployment, err = hubClient.AppsV1().Deployments(hubNamespace).Get(context.Background(),
			mutatingWebhookName, metav1.GetOptions{}); err != nil {
			return err
		}

		pods := len(workWebhookPods.Items)
		replicas := *mutatingWebhookDeployment.Spec.Replicas
		readyReplicas := mutatingWebhookDeployment.Status.ReadyReplicas

		if pods != (int)(replicas) {
			return fmt.Errorf("deployment %s pods should have %d but got %d ", mutatingWebhookName, replicas, pods)
		}

		if readyReplicas != replicas {
			return fmt.Errorf("deployment %s should have %d but got %d ready replicas", mutatingWebhookName, replicas, readyReplicas)
		}

		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeNil())

})

var _ = ginkgo.AfterSuite(func() {
	// delete cluster namespace
	err := spokeKubeClient.CoreV1().Namespaces().Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
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
