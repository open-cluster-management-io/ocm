package cloudevents

import (
	"context"
	"fmt"
	"os"
	"path"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	ocmfeature "open-cluster-management.io/api/feature"
	workapiv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/work/helper"
	"open-cluster-management.io/ocm/test/integration/cloudevents/source"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
	cm1, cm2           = "cm1", "cm2"
)

// TODO consider to use one integration with part
// focus on source is a MQTT broker
const workSourceDriver = "mqtt"

var tempDir string

var testEnv *envtest.Environment
var envCtx context.Context
var envCancel context.CancelFunc

var workSource source.Source
var workSourceConfigFileName string
var workSourceWorkClient workclientset.Interface
var workSourceHash string

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
	logf.SetLogger(zap.New(zap.WriteTo(ginkgo.GinkgoWriter), zap.UseDevMode(true), zap.Level(zapcore.DebugLevel)))
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

	tempDir, err = os.MkdirTemp("", "test")
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(tempDir).ToNot(gomega.BeEmpty())

	err = workapiv1.Install(scheme.Scheme)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates)

	spokeRestConfig = cfg
	spokeKubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	spokeWorkClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	switch workSourceDriver {
	case "mqtt":
		// create kubeconfig file for hub in a tmp dir
		workSourceConfigFileName = path.Join(tempDir, "mqttconfig")

		workSource = source.NewMQTTSource(workSourceConfigFileName)
		err := workSource.Start(envCtx)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		workSourceHash = helper.HubHash(workSource.Host())

		workSourceWorkClient = workSource.Workclientset()
		gomega.Expect(workSourceWorkClient).ToNot(gomega.BeNil())
	default:
		ginkgo.AbortSuite(fmt.Sprintf("unsupported source driver: %s", workSourceDriver))
	}
})

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	envCancel()

	err := workSource.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	if tempDir != "" {
		os.RemoveAll(tempDir)
	}
})
