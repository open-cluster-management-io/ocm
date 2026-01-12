package work

import (
	"context"
	"flag"
	"fmt"
	"net"
	"os"
	"path"
	"testing"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"google.golang.org/grpc"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	ocmfeature "open-cluster-management.io/api/feature"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	sourcecodec "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/source/codec"
	workstore "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/store"
	pbv1 "open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options/grpc/protobuf/v1"
	cloudeventsgrpc "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc"
	cemetrics "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/metrics"
	sdkgrpc "open-cluster-management.io/sdk-go/pkg/server/grpc"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	serviceswork "open-cluster-management.io/ocm/pkg/server/services/work"
	"open-cluster-management.io/ocm/pkg/work/helper"
	"open-cluster-management.io/ocm/pkg/work/hub"
	"open-cluster-management.io/ocm/pkg/work/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
	cm1, cm2           = "cm1", "cm2"
)

var sourceDriver = util.KubeDriver

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

func init() {
	klog.InitFlags(nil)
	klog.SetOutput(ginkgo.GinkgoWriter)

	flag.StringVar(&sourceDriver, "test.driver", util.KubeDriver, "Driver of test, default is kube")
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

	err = workapiv1.Install(scheme.Scheme)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	err = features.SpokeMutableFeatureGate.Add(ocmfeature.DefaultSpokeWorkFeatureGates)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	err = features.HubMutableFeatureGate.Add(ocmfeature.DefaultHubWorkFeatureGates)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// enable ManifestWorkReplicaSet feature gate
	err = features.HubMutableFeatureGate.Set("ManifestWorkReplicaSet=true")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	// enable CleanUpCompletedManifestWork feature gate
	err = features.HubMutableFeatureGate.Set("CleanUpCompletedManifestWork=true")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	switch sourceDriver {
	case util.KubeDriver:
		sourceConfigFileName = path.Join(tempDir, "kubeconfig")
		err = util.CreateKubeconfigFile(cfg, sourceConfigFileName)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		hubHash = helper.HubHash(cfg.Host)

		hubWorkClient, err = workclientset.NewForConfig(cfg)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// start hub controller
		go func() {
			defer ginkgo.GinkgoRecover()
			opts := hub.NewWorkHubManagerOptions()
			opts.WorkDriver = "kube"
			opts.WorkDriverConfig = sourceConfigFileName
			hubConfig := hub.NewWorkHubManagerConfig(opts)
			err := hubConfig.RunWorkHubManager(envCtx, &controllercmd.ControllerContext{
				KubeConfig:    cfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("hub"),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()
	case util.MQTTDriver:
		sourceID := "work-test-mqtt"
		err = util.RunMQTTBroker()
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		sourceConfigFileName = path.Join(tempDir, "mqttconfig")
		err = util.CreateMQTTConfigFile(sourceConfigFileName, sourceID)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		hubHash = helper.HubHash(util.MQTTBrokerHost)

		watcherStore, err := workstore.NewSourceLocalWatcherStore(envCtx, func(ctx context.Context) ([]*workapiv1.ManifestWork, error) {
			return []*workapiv1.ManifestWork{}, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		clientOptions := options.NewGenericClientOptions(
			util.NewMQTTSourceOptions(sourceID),
			sourcecodec.NewManifestBundleCodec(),
			fmt.Sprintf("%s-%s", sourceID, rand.String(5)),
		).WithSourceID(sourceID).WithClientWatcherStore(watcherStore)
		sourceClient, err := work.NewSourceClientHolder(envCtx, clientOptions)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		hubWorkClient = sourceClient.WorkInterface()
	case util.GRPCDriver:
		hubWorkClient, err = workclientset.NewForConfig(cfg)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		sourceConfigFileName, hubHash = startGRPCServer(envCtx, tempDir, cfg)

		// start hub controller
		go func() {
			defer ginkgo.GinkgoRecover()
			// in grpc driver, hub controller still calls hub kube-apiserver.
			sourceKubeConfigFileName := path.Join(tempDir, "kubeconfig")
			err = util.CreateKubeconfigFile(cfg, sourceKubeConfigFileName)
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			opts := hub.NewWorkHubManagerOptions()
			opts.WorkDriver = "kube"
			opts.WorkDriverConfig = sourceKubeConfigFileName
			hubConfig := hub.NewWorkHubManagerConfig(opts)
			err := hubConfig.RunWorkHubManager(envCtx, &controllercmd.ControllerContext{
				KubeConfig:    cfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("hub"),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()
	default:
		ginkgo.Fail(fmt.Sprintf("unsupported test driver %s", sourceDriver))
	}

	spokeRestConfig = cfg

	spokeKubeClient, err = kubernetes.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	spokeWorkClient, err = workclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	hubClusterClient, err = clusterclientset.NewForConfig(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
})

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	envCancel()

	err := testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = util.StopMQTTBroker()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	if tempDir != "" {
		os.RemoveAll(tempDir)
	}
})

type agentOptionsDecorator func(opt *spoke.WorkloadAgentOptions, commonOpt *commonoptions.AgentOptions) (
	*spoke.WorkloadAgentOptions, *commonoptions.AgentOptions)

func startWorkAgent(ctx context.Context, clusterName string, decorators ...agentOptionsDecorator) {
	defer ginkgo.GinkgoRecover()
	o := spoke.NewWorkloadAgentOptions()
	o.StatusSyncInterval = 3 * time.Second
	o.WorkloadSourceDriver = sourceDriver
	o.WorkloadSourceConfig = sourceConfigFileName
	if sourceDriver != util.KubeDriver {
		o.CloudEventsClientID = fmt.Sprintf("%s-work-agent", clusterName)
		o.CloudEventsClientCodecs = []string{"manifestbundle"}
	}

	commOptions := commonoptions.NewAgentOptions()
	commOptions.SpokeClusterName = clusterName

	for _, decorator := range decorators {
		o, commOptions = decorator(o, commOptions)
	}

	agentConfig := spoke.NewWorkAgentConfig(commOptions, o)
	err := agentConfig.RunWorkloadAgent(ctx, &controllercmd.ControllerContext{
		KubeConfig:    spokeRestConfig,
		EventRecorder: util.NewIntegrationTestEventRecorder("integration"),
	})
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func startGRPCServer(ctx context.Context, temp string, cfg *rest.Config) (string, string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
	bindPort := fmt.Sprintf("%d", ln.Addr().(*net.TCPAddr).Port)
	_ = ln.Close()

	configFileName := path.Join(temp, "grpcconfig")
	gRPCURL, gRPCServerOptions, _, err := util.CreateGRPCConfigs(configFileName, bindPort)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	hubHash := helper.HubHash(gRPCURL)

	hook, err := util.NewGRPCServerWorkHook(cfg)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	grpcEventServer := cloudeventsgrpc.NewGRPCBroker(cloudeventsgrpc.NewBrokerOptions())
	grpcEventServer.RegisterService(ctx, payload.ManifestBundleEventDataType,
		serviceswork.NewWorkService(hook.WorkClient, hook.WorkInformers.Work().V1().ManifestWorks()))

	go func() {
		defer ginkgo.GinkgoRecover()
		hook.Run(ctx)
	}()

	authorizer := util.NewMockAuthorizer()
	server := sdkgrpc.NewGRPCServer(gRPCServerOptions).
		WithUnaryAuthorizer(authorizer).
		WithStreamAuthorizer(authorizer).
		WithRegisterFunc(func(s *grpc.Server) {
			pbv1.RegisterCloudEventServiceServer(s, grpcEventServer)
		}).
		WithExtraMetrics(cemetrics.CloudEventsGRPCMetrics()...)

	go func() {
		defer ginkgo.GinkgoRecover()
		err := server.Run(ctx)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	return configFileName, hubHash
}
