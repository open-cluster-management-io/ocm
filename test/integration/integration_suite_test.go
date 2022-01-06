package integration

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

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
	eventuallyTimeout    = 30 // seconds
	eventuallyInterval   = 1  // seconds
	hubNamespace         = "open-cluster-management-hub"
	spokeNamespace       = "open-cluster-management-agent"
	clusterManagerName   = "hub"
	hubNamespaceDetached = "hub"
)

// default mode
var testEnv *envtest.Environment
var kubeClient kubernetes.Interface
var apiExtensionClient apiextensionsclient.Interface
var restConfig *rest.Config
var operatorClient operatorclient.Interface

// detachedTestEnv, detachedKubeClient, detachedAPIExtensionClient and detachedRestConfig is using in Detached mode.
// for cluster manager, it represents the hub cluster;
// while for klusterlet, it represents the managed cluster.
var (
	detachedTestEnv            *envtest.Environment
	detachedKubeClient         kubernetes.Interface
	detachedAPIExtensionClient apiextensionsclient.Interface
	detachedRestConfig         *rest.Config
	detachedOperatorClient     operatorclient.Interface
)

var cancel context.CancelFunc

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

	apiExtensionClient, err = apiextensionsclient.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(apiExtensionClient).ToNot(gomega.BeNil())

	operatorClient, err = operatorclient.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(operatorClient).ToNot(gomega.BeNil())

	// start a local kube-apiserver as the hub/managed cluster for Detached mode.
	detachedTestEnv = &envtest.Environment{
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths: []string{
			filepath.Join(".", "deploy", "cluster-manager", "olm-catalog", "cluster-manager", "manifests"),
			filepath.Join(".", "deploy", "klusterlet", "olm-catalog", "klusterlet", "manifests"),
		},
	}
	detachedConfig, err := detachedTestEnv.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	// prepare clients
	detachedKubeClient, err = kubernetes.NewForConfig(detachedConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(kubeClient).ToNot(gomega.BeNil())

	detachedAPIExtensionClient, err = apiextensionsclient.NewForConfig(detachedConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(apiExtensionClient).ToNot(gomega.BeNil())

	detachedOperatorClient, err = operatorclient.NewForConfig(detachedConfig)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(operatorClient).ToNot(gomega.BeNil())

	// prepare a ClusterManager
	_, err = operatorClient.OperatorV1().ClusterManagers().Create(context.Background(), &operatorapiv1.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterManagerName,
		},
		Spec: operatorapiv1.ClusterManagerSpec{
			RegistrationImagePullSpec: "quay.io/open-cluster-management/registration",
			WorkImagePullSpec:         "quay.io/open-cluster-management/work",
			PlacementImagePullSpec:    "quay.io/open-cluster-management/placement",
			DeployOption: operatorapiv1.DeployOption{
				Mode: operatorapiv1.InstallModeDefault,
			},
		},
	}, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	_, err = detachedOperatorClient.OperatorV1().ClusterManagers().Create(context.Background(), &operatorapiv1.ClusterManager{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterManagerName,
		},
		Spec: operatorapiv1.ClusterManagerSpec{
			RegistrationImagePullSpec: "quay.io/open-cluster-management/registration",
			WorkImagePullSpec:         "quay.io/open-cluster-management/work",
			PlacementImagePullSpec:    "quay.io/open-cluster-management/placement",
			DeployOption: operatorapiv1.DeployOption{
				Mode: operatorapiv1.InstallModeDetached,
			},
		},
	}, metav1.CreateOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	restConfig = cfg
	detachedRestConfig = detachedConfig

	ctx, c := context.WithCancel(context.TODO())
	cancel = c

	go ServiceAccountCtl(ctx)

	close(done)
}, 60)

var _ = ginkgo.AfterSuite(func() {
	ginkgo.By("tearing down the test environment")

	var err error

	err = testEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	err = detachedTestEnv.Stop()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	cancel()
})

// ServiceAccountCtl watch service accounts and create a corresponding secret for it.
func ServiceAccountCtl(ctx context.Context) {
	w, err := detachedKubeClient.CoreV1().ServiceAccounts("").Watch(ctx, metav1.ListOptions{})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
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
				klog.Infof("not a service account, contine")
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

			_, err = detachedKubeClient.CoreV1().Secrets(sa.Namespace).Create(ctx, secret, metav1.CreateOptions{})
			if errors.IsAlreadyExists(err) {
				klog.Infof("secret %s/%s already exist", secret.Namespace, secret.Name)
			} else {
				gomega.Expect(err).ToNot(gomega.HaveOccurred())
			}

			_, err = detachedKubeClient.CoreV1().Secrets(sa.Namespace).Get(ctx, secret.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			err := retry.OnError(retry.DefaultBackoff,
				func(e error) bool {
					return true
				},
				func() error {
					serviceAccount, err := detachedKubeClient.CoreV1().ServiceAccounts(sa.Namespace).Get(ctx, sa.Name, metav1.GetOptions{})
					if err != nil {
						return err
					}
					serviceAccount.Secrets = []corev1.ObjectReference{
						{
							Namespace: secret.Namespace,
							Name:      secret.Name,
						},
					}
					_, err = detachedKubeClient.CoreV1().ServiceAccounts(serviceAccount.Namespace).Update(ctx, serviceAccount, metav1.UpdateOptions{})
					return err
				})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		case <-ctx.Done():
			klog.Infof("service account controller exit")
			return
		}
	}

}
