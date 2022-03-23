package integration_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	"github.com/openshift/library-go/pkg/controller/controllercmd"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/transport"

	addonclientset "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/clientcert"
	"open-cluster-management.io/registration/pkg/features"
	"open-cluster-management.io/registration/pkg/hub"
	"open-cluster-management.io/registration/pkg/spoke"
	"open-cluster-management.io/registration/pkg/spoke/addon"
	"open-cluster-management.io/registration/pkg/spoke/managedcluster"
	"open-cluster-management.io/registration/test/integration/util"

	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
)

var spokeCfg *rest.Config
var bootstrapKubeConfigFile string

var testEnv *envtest.Environment
var securePort string
var serverCertFile string

var kubeClient kubernetes.Interface
var clusterClient clusterclientset.Interface
var addOnClient addonclientset.Interface
var workClient workclientset.Interface

var testNamespace string

var authn *util.TestAuthn

var ctx context.Context
var cancel context.CancelFunc

func TestIntegration(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecsWithDefaultAndCustomReporters(t, "Integration Suite", []ginkgo.Reporter{printer.NewlineReporter{}})
}

var _ = ginkgo.BeforeSuite(func(done ginkgo.Done) {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))

	ginkgo.By("bootstrapping test environment")

	var err error

	ctx, cancel = context.WithCancel(context.TODO())

	// crank up the sync speed
	transport.CertCallbackRefreshDuration = 5 * time.Second
	clientcert.ControllerResyncInterval = 5 * time.Second
	managedcluster.CreatingControllerSyncInterval = 1 * time.Second
	hub.ResyncInterval = 5 * time.Second

	// crank up the addon lease sync and udpate speed
	spoke.AddOnLeaseControllerSyncInterval = 5 * time.Second
	addon.AddOnLeaseControllerLeaseDurationSeconds = 1

	// install cluster CRD and start a local kube-apiserver
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	authn = util.DefaultTestAuthn
	apiserver := &envtest.APIServer{}
	apiserver.SecureServing.Authn = authn

	testEnv = &envtest.Environment{
		ControlPlane: envtest.ControlPlane{
			APIServer: apiserver,
		},
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join(".", "deploy", "hub"),
			filepath.Join(".", "deploy", "spoke"),
		},
	}

	cfg, err := testEnv.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	err = clusterv1.AddToScheme(scheme.Scheme)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// prepare configs
	securePort = testEnv.ControlPlane.APIServer.SecureServing.Port
	gomega.Expect(len(securePort)).ToNot(gomega.BeZero())

	serverCertFile = fmt.Sprintf("%s/apiserver.crt", testEnv.ControlPlane.APIServer.CertDir)

	spokeCfg = cfg
	gomega.Expect(spokeCfg).ToNot(gomega.BeNil())

	bootstrapKubeConfigFile = path.Join(util.TestDir, "bootstrap", "kubeconfig")
	err = authn.CreateBootstrapKubeConfigWithCertAge(bootstrapKubeConfigFile, serverCertFile, securePort, 24*time.Hour)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// prepare clients
	kubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(kubeClient).ToNot(gomega.BeNil())

	clusterClient, err = clusterclientset.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(clusterClient).ToNot(gomega.BeNil())

	addOnClient, err = addonclientset.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(clusterClient).ToNot(gomega.BeNil())

	workClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(clusterClient).ToNot(gomega.BeNil())

	// prepare test namespace
	nsBytes, err := ioutil.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/namespace")
	if err != nil {
		testNamespace = "open-cluster-management-agent"
	} else {
		testNamespace = string(nsBytes)
	}
	err = util.PrepareSpokeAgentNamespace(kubeClient, testNamespace)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	// enable DefaultClusterSet feature gate
	features.DefaultHubMutableFeatureGate.Set("DefaultClusterSet=true")
	features.DefaultHubMutableFeatureGate.Set("V1beta1CSRAPICompatibility=true")

	// start hub controller
	go func() {
		err := hub.RunControllerManager(ctx, &controllercmd.ControllerContext{
			KubeConfig:    cfg,
			EventRecorder: util.NewIntegrationTestEventRecorder("hub"),
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	close(done)
}, 60)

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	cancel()

	err := testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = os.RemoveAll(util.TestDir)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
})
