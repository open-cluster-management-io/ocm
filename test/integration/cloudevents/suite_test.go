package cloudevents

import (
	"context"
	"fmt"
	"os"
	"path"
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"go.uber.org/zap/zapcore"
	"gopkg.in/yaml.v2"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
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
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/mqtt"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"

	"open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/work/spoke"
	"open-cluster-management.io/ocm/test/integration/cloudevents/source"
	"open-cluster-management.io/ocm/test/integration/util"
)

type sourceInfoGetter func() (workclientset.Interface, string, string, string)
type clusterNameGetter func() string

type kafkaClusterNameGenerator struct{ count int }

func (g *kafkaClusterNameGenerator) generate() string {
	g.count = g.count + 1
	return fmt.Sprintf("kafka%d", g.count)
}

const (
	eventuallyTimeout  = 60 // seconds
	eventuallyInterval = 1  // seconds
	cm1, cm2, cm3, cm4 = "cm1", "cm2", "cm3", "cm4"
)

// TODO consider to use one integration with work integration
const (
	mqttDriver  = "mqtt"
	kafkaDriver = "kafka"
)

var tempDir string

var testEnv *envtest.Environment
var envCtx context.Context
var envCancel context.CancelFunc

var mqttSource source.Source
var mqttSourceConfigPath string
var mqttSourceWorkClient workclientset.Interface

var kafkaSource source.Source
var kafkaSourceConfigPath string
var kafkaSourceWorkClient workclientset.Interface

var mwrsConfigFileName string

var hubRestConfig *rest.Config
var hubClusterClient clusterclientset.Interface
var hubWorkClient workclientset.Interface

var spokeRestConfig *rest.Config
var spokeKubeClient kubernetes.Interface
var spokeWorkClient workclientset.Interface

var kClusterNameGenerator *kafkaClusterNameGenerator = &kafkaClusterNameGenerator{}

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

	hubRestConfig = cfg
	hubClusterClient, err = clusterclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	hubWorkClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// create source client with mqtt
	mqttSourceConfigPath = path.Join(tempDir, "mqttconfig")
	mqttSource = source.NewMQTTSource(mqttSourceConfigPath)
	err = mqttSource.Start(envCtx)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	mqttSourceWorkClient = mqttSource.Workclientset()
	gomega.Expect(mqttSourceWorkClient).ToNot(gomega.BeNil())

	// create source client with kafka
	kafkaSourceConfigPath = path.Join(tempDir, "kafkaconfig")
	kafkaSource = source.NewKafkaSource(kafkaSourceConfigPath)
	err = kafkaSource.Start(envCtx)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	kafkaSourceWorkClient = kafkaSource.Workclientset()
	gomega.Expect(kafkaSourceWorkClient).ToNot(gomega.BeNil())

	// create mqttconfig file for mwrsctrl in a tmp dir
	mwrsConfigFileName = path.Join(tempDir, "mwrsctrl-mqttconfig")
	config := mqtt.MQTTConfig{
		BrokerHost: mqttSource.Host(),
		Topics: &types.Topics{
			SourceEvents:    "sources/mwrsctrl/clusters/+/sourceevents",
			AgentEvents:     "sources/mwrsctrl/clusters/+/agentevents",
			SourceBroadcast: "sources/mwrsctrl/sourcebroadcast",
		},
	}
	configData, err := yaml.Marshal(config)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	err = os.WriteFile(mwrsConfigFileName, configData, 0600)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

})

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	envCancel()

	err := mqttSource.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = kafkaSource.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	if tempDir != "" {
		os.RemoveAll(tempDir)
	}
})

func runWorkAgent(ctx context.Context, o *spoke.WorkloadAgentOptions, commOption *options.AgentOptions) {
	agentConfig := spoke.NewWorkAgentConfig(commOption, o)
	err := agentConfig.RunWorkloadAgent(ctx, &controllercmd.ControllerContext{
		KubeConfig:    spokeRestConfig,
		EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func mqttSourceInfo() (workclientset.Interface, string, string, string) {
	return mqttSourceWorkClient, mqttDriver, mqttSourceConfigPath, mqttSource.Hash()
}

func kafkaSourceInfo() (workclientset.Interface, string, string, string) {
	return kafkaSourceWorkClient, kafkaDriver, kafkaSourceConfigPath, kafkaSource.Hash()
}

func clusterName() string {
	return utilrand.String(5)
}

func toAppliedManifestWorkName(hash string, work *workapiv1.ManifestWork) string {
	// if the source is not kube, the uid will be used as the manifestwork name
	return fmt.Sprintf("%s-%s", hash, work.UID)
}
