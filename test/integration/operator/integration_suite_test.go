package operator

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	"open-cluster-management.io/api/feature"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"

	"open-cluster-management.io/ocm/pkg/operator/operators/klusterlet/controllers/ssarcontroller"
)

func TestIntegration(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Integration Suite")
}

const (
	eventuallyTimeout  = 30 // seconds
	eventuallyInterval = 1  // seconds
	hubNamespace       = "open-cluster-management-hub"
	clusterManagerName = "hub"
	hubNamespaceHosted = "hub"
)

// default mode
var testEnv *envtest.Environment
var kubeClient kubernetes.Interface
var apiExtensionClient apiextensionsclient.Interface
var restConfig *rest.Config
var operatorClient operatorclient.Interface

// hostedTestEnv, hostedKubeClient, hostedAPIExtensionClient and hostedRestConfig is using in Hosted mode.
// for cluster manager, it represents the hub cluster;
// while for klusterlet, it represents the managed cluster.
var (
	hostedTestEnv            *envtest.Environment
	hostedKubeClient         kubernetes.Interface
	hostedAPIExtensionClient apiextensionsclient.Interface
	hostedRestConfig         *rest.Config
	hostedOperatorClient     operatorclient.Interface
)

var envCancel context.CancelFunc
var envCtx context.Context

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")

	// crank up the sync speed
	ssarcontroller.SSARReSyncTime = 1 * time.Second

	var err error

	// install operator CRDs and start a local kube-apiserver
	testEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join(".", "deploy", "cluster-manager", "olm-catalog", "latest", "manifests"),
			filepath.Join(".", "deploy", "klusterlet", "olm-catalog", "latest", "manifests"),
		},
	}
	cfg, err := testEnv.Start()
	cfg.QPS = 100
	cfg.Burst = 200

	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	// prepare clients
	kubeClient, err = kubernetes.NewForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())
	Expect(kubeClient).ToNot(BeNil())

	apiExtensionClient, err = apiextensionsclient.NewForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())
	Expect(apiExtensionClient).ToNot(BeNil())

	operatorClient, err = operatorclient.NewForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())
	Expect(operatorClient).ToNot(BeNil())

	// start a local kube-apiserver as the hub/managed cluster for Hosted mode.
	hostedTestEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join(".", "deploy", "cluster-manager", "olm-catalog", "latest", "manifests"),
			filepath.Join(".", "deploy", "klusterlet", "olm-catalog", "latest", "manifests"),
		},
	}
	hostedConfig, err := hostedTestEnv.Start()
	Expect(err).ToNot(HaveOccurred())
	Expect(cfg).ToNot(BeNil())

	// prepare clients
	hostedKubeClient, err = kubernetes.NewForConfig(hostedConfig)
	Expect(err).ToNot(HaveOccurred())
	Expect(kubeClient).ToNot(BeNil())

	hostedAPIExtensionClient, err = apiextensionsclient.NewForConfig(hostedConfig)
	Expect(err).ToNot(HaveOccurred())
	Expect(apiExtensionClient).ToNot(BeNil())

	hostedOperatorClient, err = operatorclient.NewForConfig(hostedConfig)
	Expect(err).ToNot(HaveOccurred())
	Expect(operatorClient).ToNot(BeNil())

	// prepare a ClusterManager
	_, err = operatorClient.OperatorV1().ClusterManagers().Create(context.Background(), &operatorapiv1.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterManagerName,
		},
		Spec: operatorapiv1.ClusterManagerSpec{
			RegistrationImagePullSpec: "quay.io/open-cluster-management/registration",
			WorkImagePullSpec:         "quay.io/open-cluster-management/work",
			PlacementImagePullSpec:    "quay.io/open-cluster-management/placement",
			AddOnManagerImagePullSpec: "quay.io/open-cluster-management/addon-manager",
			DeployOption: operatorapiv1.ClusterManagerDeployOption{
				Mode: operatorapiv1.InstallModeDefault,
			},
			RegistrationConfiguration: &operatorapiv1.RegistrationHubConfiguration{
				FeatureGates: []operatorapiv1.FeatureGate{
					{
						Feature: "NetworkPolicies",
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
				},
			},
			WorkConfiguration: &operatorapiv1.WorkConfiguration{
				WorkDriver: operatorapiv1.WorkDriverTypeKube,
				FeatureGates: []operatorapiv1.FeatureGate{
					{
						Feature: string(feature.NilExecutorValidating),
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
					{
						Feature: string(feature.ManifestWorkReplicaSet),
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
				},
			},
		},
	}, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())

	_, err = hostedOperatorClient.OperatorV1().ClusterManagers().Create(context.Background(), &operatorapiv1.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterManagerName,
		},
		Spec: operatorapiv1.ClusterManagerSpec{
			RegistrationImagePullSpec: "quay.io/open-cluster-management/registration",
			WorkImagePullSpec:         "quay.io/open-cluster-management/work",
			PlacementImagePullSpec:    "quay.io/open-cluster-management/placement",
			AddOnManagerImagePullSpec: "quay.io/open-cluster-management/addon-manager",
			DeployOption: operatorapiv1.ClusterManagerDeployOption{
				Mode: operatorapiv1.InstallModeHosted,
				Hosted: &operatorapiv1.HostedClusterManagerConfiguration{
					RegistrationWebhookConfiguration: operatorapiv1.HostedWebhookConfiguration{
						Address: "localhost",
						Port:    443,
					},
					WorkWebhookConfiguration: operatorapiv1.HostedWebhookConfiguration{
						Address: "localhost",
						Port:    443,
					},
				},
			},
			RegistrationConfiguration: &operatorapiv1.RegistrationHubConfiguration{
				FeatureGates: []operatorapiv1.FeatureGate{
					{
						Feature: "NetworkPolicies",
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
				},
			},
			WorkConfiguration: &operatorapiv1.WorkConfiguration{
				WorkDriver: operatorapiv1.WorkDriverTypeKube,
				FeatureGates: []operatorapiv1.FeatureGate{
					{
						Feature: string(feature.ManifestWorkReplicaSet),
						Mode:    operatorapiv1.FeatureGateModeTypeEnable,
					},
				},
			},
		},
	}, metav1.CreateOptions{})
	Expect(err).ToNot(HaveOccurred())

	restConfig = cfg
	hostedRestConfig = hostedConfig

	envCtx, envCancel = context.WithCancel(context.TODO())

	go ServiceAccountCtl(envCtx, kubeClient)
	go ServiceAccountCtl(envCtx, hostedKubeClient)
})

var _ = AfterSuite(func() {
	By("tearing down the test environment")

	if envCancel != nil {
		envCancel()
	}

	if testEnv != nil {
		err := testEnv.Stop()
		Expect(err).ToNot(HaveOccurred())
	}

	if hostedTestEnv != nil {
		err := hostedTestEnv.Stop()
		Expect(err).ToNot(HaveOccurred())
	}
})

// ServiceAccountCtl watch service accounts and create a corresponding secret for it.
func ServiceAccountCtl(ctx context.Context, kubeClient kubernetes.Interface) {
	w, err := kubeClient.CoreV1().ServiceAccounts("").Watch(ctx, metav1.ListOptions{})
	Expect(err).ToNot(HaveOccurred())
	klog.Infof("service account controller start")

	for {
		select {
		case event, ok := <-w.ResultChan():
			if !ok {
				klog.Infof("channel closed, service account controller exit")
				return
			}

			sa, ok := event.Object.(*corev1.ServiceAccount)
			if !ok {
				klog.Infof("not a service account, continue")
				continue
			}

			if len(sa.Secrets) != 0 {
				klog.Infof("service account secret already exist")
				continue
			}

			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      fmt.Sprintf("%s-token-test", sa.Name),
					Namespace: sa.Namespace,
					Annotations: map[string]string{
						"kubernetes.io/service-account.name": sa.Name,
					},
				},
				Data: map[string][]byte{
					"token": []byte("test-token"),
				},
				Type: corev1.SecretTypeServiceAccountToken,
			}

			_, err = kubeClient.CoreV1().Secrets(sa.Namespace).Create(ctx, secret, metav1.CreateOptions{})
			if errors.IsAlreadyExists(err) {
				klog.Infof("secret %s/%s already exist", secret.Namespace, secret.Name)
			} else {
				Expect(err).ToNot(HaveOccurred())
			}

			_, err = kubeClient.CoreV1().Secrets(sa.Namespace).Get(ctx, secret.Name, metav1.GetOptions{})
			Expect(err).ToNot(HaveOccurred())

			err := retry.OnError(retry.DefaultBackoff,
				func(e error) bool {
					return true
				},
				func() error {
					serviceAccount, err := kubeClient.CoreV1().ServiceAccounts(sa.Namespace).Get(ctx, sa.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}
					serviceAccount.Secrets = []corev1.ObjectReference{
						{
							Namespace: secret.Namespace,
							Name:      secret.Name,
						},
					}
					_, err = kubeClient.CoreV1().ServiceAccounts(serviceAccount.Namespace).Update(ctx, serviceAccount, metav1.UpdateOptions{})
					return err
				})
			Expect(err).ToNot(HaveOccurred())
		case <-ctx.Done():
			klog.Infof("service account controller exit")
			return
		}
	}

}
