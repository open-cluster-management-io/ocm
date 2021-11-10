package integration

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/registration-operator/pkg/operators/klusterlet/controllers/bootstrapcontroller"
	"open-cluster-management.io/registration-operator/pkg/operators/klusterlet/controllers/ssarcontroller"
)

func TestIntegration(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecsWithDefaultAndCustomReporters(t, "Integration Suite", []ginkgo.Reporter{printer.NewlineReporter{}})
}

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
	hubNamespace       = "open-cluster-management-hub"
	spokeNamespace     = "open-cluster-management-agent"
	clusterManagerName = "hub"
)

var testEnv *envtest.Environment

var kubeClient kubernetes.Interface

var restConfig *rest.Config

var operatorClient operatorclient.Interface

var _ = ginkgo.BeforeSuite(func(done ginkgo.Done) {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))

	ginkgo.By("bootstrapping test environment")

	// crank up the sync speed
	bootstrapcontroller.BootstrapControllerSyncInterval = 2 * time.Second
	ssarcontroller.SSARReSyncTime = 1 * time.Second

	var err error

	// install registration-operator CRDs and start a local kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join(".", "deploy", "cluster-manager", "olm-catalog", "cluster-manager", "manifests"),
			filepath.Join(".", "deploy", "klusterlet", "olm-catalog", "klusterlet", "manifests"),
		},
	}
	cfg, err := testEnv.Start()

	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	// prepare clients
	kubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(kubeClient).ToNot(gomega.BeNil())
	operatorClient, err = operatorclient.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(kubeClient).ToNot(gomega.BeNil())

	// prepare a ClusterManager
	clusterManager := &operatorapiv1.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterManagerName,
		},
		Spec: operatorapiv1.ClusterManagerSpec{
			RegistrationImagePullSpec: "quay.io/open-cluster-management/registration",
			WorkImagePullSpec:         "quay.io/open-cluster-management/work",
			PlacementImagePullSpec:    "quay.io/open-cluster-management/placement",
		},
	}
	_, err = operatorClient.OperatorV1().ClusterManagers().Create(context.Background(), clusterManager, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	restConfig = cfg

	close(done)
}, 60)

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	err := testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})
