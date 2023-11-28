package work

import (
	"context"
	"os"
	"path"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	ocmfeature "open-cluster-management.io/api/feature"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/work/helper"
	"open-cluster-management.io/ocm/pkg/work/hub"
	"open-cluster-management.io/ocm/test/integration/util"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
	cm1, cm2           = "cm1", "cm2"
)

// focus on hub is a kube cluster
const sourceDriver = "kube"

var tempDir string

var testEnv *envtest.Environment
var envCtx context.Context
var envCancel context.CancelFunc

var sourceConfigFileName string
var hubWorkClient workclientset.Interface

var hubClusterClient clusterclientset.Interface
var hubHash string

var spokeRestConfig *rest.Config
var spokeKubeClient kubernetes.Interface
var spokeWorkClient workclientset.Interface

var CRDPaths = []string{
	// hub
	"./vendor/open-cluster-management.io/api/work/v1/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml",
	"./vendor/open-cluster-management.io/api/work/v1alpha1/0000_00_work.open-cluster-management.io_manifestworkreplicasets.crd.yaml",
	"./vendor/open-cluster-management.io/api/cluster/v1beta1/0000_02_clusters.open-cluster-management.io_placements.crd.yaml",
	"./vendor/open-cluster-management.io/api/cluster/v1beta1/0000_03_clusters.open-cluster-management.io_placementdecisions.crd.yaml",
	// spoke
	"./vendor/open-cluster-management.io/api/work/v1/0000_01_work.open-cluster-management.io_appliedmanifestworks.crd.yaml",
}

func TestIntegration(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "Integration Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true)))
	ginkgo.By("bootstrapping test environment")

	// start a kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths:     CRDPaths,
	}
	envCtx, envCancel = context.WithCancel(context.TODO())
	cfg, err := testEnv.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	// create kubeconfig file for hub in a tmp dir
	tempDir, err = os.MkdirTemp("", "test")
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(tempDir).ToNot(gomega.BeEmpty())

	sourceConfigFileName = path.Join(tempDir, "kubeconfig")
	err = util.CreateKubeconfigFile(cfg, sourceConfigFileName)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = workapiv1.Install(scheme.Scheme)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates)

	spokeRestConfig = cfg
	hubHash = helper.HubHash(spokeRestConfig.Host)
	spokeKubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	hubWorkClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	spokeWorkClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	hubClusterClient, err = clusterclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// start hub controller
	go func() {
		err := hub.RunWorkHubManager(envCtx, &controllercmd.ControllerContext{
			KubeConfig:    cfg,
			EventRecorder: util.NewIntegrationTestEventRecorder("hub"),
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}()
})

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	envCancel()

	err := testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	if tempDir != "" {
		os.RemoveAll(tempDir)
	}
})
