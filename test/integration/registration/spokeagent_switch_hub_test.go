package registration_test

import (
	"context"
	"fmt"
	"path"
	"time"

	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apiserver/pkg/server/healthz"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	addonclientset "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	ocmfeature "open-cluster-management.io/api/feature"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/features"
	"open-cluster-management.io/ocm/pkg/registration/hub"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

// In the switch-hub test, we will start 2 hubs, and a spoke, we will test 2 cases:
// 1. the spoke first connects to hub1, we set the hub1 to not accept the spoke, then the spoke should switch to hub2.
// 2. the spoke first connects to hub1, we stop hub1, then the spoke should switch to hub2.
var _ = Describe("switch-hub", Ordered, func() {
	var managedClusterName, hubKubeconfigSecret, suffix string
	var hub1, hub2 *mockHub
	var spokeCancel context.CancelFunc

	BeforeEach(func() {
		features.SpokeMutableFeatureGate.SetFromMap(map[string]bool{
			string(ocmfeature.MultipleHubs): true,
		})

		var err error
		suffix = rand.String(5)
		managedClusterName = fmt.Sprintf("cluster-%s", suffix)
		hubKubeconfigSecret = fmt.Sprintf("hub-kubeconfig-secret-%s", suffix)
		hubKubeconfigDir := path.Join(util.TestDir, fmt.Sprintf("switch-hub-%s", suffix), "hub-kubeconfig")

		// Ensure there is no remaining bootstrap-hub-kubeconfig secret
		err = kubeClient.CoreV1().Secrets(testNamespace).Delete(context.Background(), "bootstrap-hub-kubeconfig", metav1.DeleteOptions{})
		if err != nil {
			gomega.Expect(apierrors.IsNotFound(err)).To(gomega.BeTrue())
		}

		// Start 2 hubs
		hub1 = startNewHub(context.Background(), fmt.Sprintf("hub1-%s", suffix))
		fmt.Println("hub1 bootstrap file path: ", hub1.bootstrapFilePath)

		hub2 = startNewHub(context.Background(), fmt.Sprintf("hub2-%s", suffix))
		fmt.Println("hub2 bootstrap file path: ", hub2.bootstrapFilePath)

		// start a auto restart agent
		By("Starting a auto restart spoke agent")
		var spokeCtx context.Context
		spokeCtx, spokeCancel = context.WithCancel(context.Background())
		go startAutoRestartAgent(spokeCtx,
			managedClusterName, hubKubeconfigDir,
			func() *spoke.SpokeAgentOptions {
				agentOptions := spoke.NewSpokeAgentOptions()
				agentOptions.HubKubeconfigSecret = hubKubeconfigSecret
				agentOptions.BootstrapKubeconfigs = []string{hub1.bootstrapFilePath, hub2.bootstrapFilePath}
				agentOptions.BootstrapKubeconfigSecret = "bootstrap-hub-kubeconfig"
				agentOptions.HubConnectionTimeoutSeconds = 10
				return agentOptions
			},
			func(ctx context.Context, stopAgent context.CancelFunc, agentConfig *spoke.SpokeAgentConfig) {
				startAgentHealthChecker(ctx, stopAgent, agentConfig.HealthCheckers())
			})

		approveAndAcceptManagedCluster(managedClusterName, hub1.kubeClient, hub1.clusterClient, hub1.authn, 10*time.Minute)

		assertManagedClusterSuccessfullyJoined(testNamespace, managedClusterName, hubKubeconfigSecret,
			hub1.kubeClient, kubeClient, hub1.clusterClient)
	})

	AfterEach(func() {
		// stop hubs
		hub1.env.Stop()
		hub2.env.Stop()

		// stop spoke
		spokeCancel()

		features.SpokeMutableFeatureGate.SetFromMap(map[string]bool{
			string(ocmfeature.MultipleHubs): false,
		})
	})

	Context("Hub1 doesn't accept client", func() {
		It("Should switch to hub2", func() {
			// Update managed cluster to not accept
			By("Update ManagedCluster to not accept")
			Eventually(func() error {
				mc, err := util.GetManagedCluster(hub1.clusterClient, managedClusterName)
				if err != nil {
					return err
				}
				mc.Spec.HubAcceptsClient = false
				_, err = hub1.clusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), mc, metav1.UpdateOptions{})
				return err
			}, eventuallyTimeout, eventuallyInterval).Should(BeNil())

			// The spoke should switch to hub2
			By("Approve and accept the ManagedCluster on hub2")
			approveAndAcceptManagedCluster(managedClusterName, hub2.kubeClient, hub2.clusterClient, hub2.authn, 10*time.Minute)

			assertManagedClusterSuccessfullyJoined(testNamespace, managedClusterName, hubKubeconfigSecret,
				hub2.kubeClient, kubeClient, hub2.clusterClient)
		})
	})

	Context("Hub1 is down and timeout", func() {
		It("Should switch to hub2", func() {
			// Stop hub1
			hub1.env.Stop()

			// The timeoutSeconds is 10s, so we need to wait for 30s to make sure the agent is restarted
			time.Sleep(30 * time.Second)

			// The spoke should switch to hub2
			By("Approve and accept the ManagedCluster on hub2")
			approveAndAcceptManagedCluster(managedClusterName, hub2.kubeClient, hub2.clusterClient, hub2.authn, 10*time.Minute)

			assertManagedClusterSuccessfullyJoined(testNamespace, managedClusterName, hubKubeconfigSecret,
				hub2.kubeClient, kubeClient, hub2.clusterClient)
		})
	})
})

type mockHub struct {
	bootstrapFilePath string
	kubeClient        kubernetes.Interface
	clusterClient     clusterclientset.Interface
	addonClient       addonclientset.Interface
	env               *envtest.Environment
	authn             *util.TestAuthn
}

func startNewHub(ctx context.Context, hubName string) *mockHub {
	apiserver := &envtest.APIServer{}
	newAuthn := util.NewTestAuthn(path.Join(util.CertDir, fmt.Sprintf("%s-another-ca.crt", hubName)),
		path.Join(util.CertDir, fmt.Sprintf("%s-another-ca.key", hubName)))
	apiserver.SecureServing.Authn = newAuthn

	env := &envtest.Environment{
		ControlPlane: envtest.ControlPlane{
			APIServer: apiserver,
		},
		ErrorIfCRDPathMissing: true,
		CRDDirectoryPaths:     CRDPaths,
	}

	cfg, err := env.Start()
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(cfg).ToNot(gomega.BeNil())

	err = clusterv1.Install(scheme.Scheme)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// prepare configs
	newSecurePort := env.ControlPlane.APIServer.SecureServing.Port
	gomega.Expect(len(newSecurePort)).ToNot(gomega.BeZero())

	anotherServerCertFile := fmt.Sprintf("%s/apiserver.crt", env.ControlPlane.APIServer.CertDir)

	// If the input is hub1, eventually we wil have: /tmp/<TestDir>/hub1/bootstrap-kubeconfig.
	// This is because under the /tmp/<TestDir>/hub1, we will also create cert files for hub1.
	bootstrapKubeConfigFile := path.Join(util.TestDir, hubName, "bootstrap-kubeconfig")
	err = newAuthn.CreateBootstrapKubeConfigWithCertAge(bootstrapKubeConfigFile, anotherServerCertFile, newSecurePort, 24*time.Hour)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())

	// Prepare clients
	kubeClient, err := kubernetes.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(kubeClient).ToNot(gomega.BeNil())

	clusterClient, err := clusterclientset.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(clusterClient).ToNot(gomega.BeNil())

	addOnClient, err := addonclientset.NewForConfig(cfg)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(clusterClient).ToNot(gomega.BeNil())

	// Start hub controller
	go func() {
		err := hub.NewHubManagerOptions().RunControllerManager(ctx, &controllercmd.ControllerContext{
			KubeConfig:    cfg,
			EventRecorder: util.NewIntegrationTestEventRecorder(hubName),
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}()

	return &mockHub{
		bootstrapFilePath: bootstrapKubeConfigFile,
		kubeClient:        kubeClient,
		clusterClient:     clusterClient,
		addonClient:       addOnClient,
		env:               env,
		authn:             newAuthn,
	}
}

func startAgent(ctx context.Context, managedClusterName, hubKubeconfigDir string,
	agentOptions *spoke.SpokeAgentOptions) (context.Context, context.CancelFunc, *spoke.SpokeAgentConfig) {
	ginkgo.By("run registration agent")
	commOptions := commonoptions.NewAgentOptions()
	commOptions.HubKubeconfigDir = hubKubeconfigDir
	commOptions.SpokeClusterName = managedClusterName

	agentCtx, stopAgent := context.WithCancel(ctx)
	agentConfig := spoke.NewSpokeAgentConfig(commOptions, agentOptions, stopAgent)
	runAgentWithContext(agentCtx, "switch-hub", agentConfig, spokeCfg)

	return agentCtx, stopAgent, agentConfig
}

func approveAndAcceptManagedCluster(managedClusterName string,
	hubKubeClient kubernetes.Interface, hubClusterClient clusterclientset.Interface,
	auth *util.TestAuthn, certAget time.Duration) {
	// The spoke cluster and csr should be created after bootstrap
	ginkgo.By("Check existence of ManagedCluster & CSR")
	gomega.Eventually(func() error {
		if _, err := util.GetManagedCluster(hubClusterClient, managedClusterName); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

	gomega.Eventually(func() error {
		if _, err := util.FindUnapprovedSpokeCSR(hubKubeClient, managedClusterName); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	// The spoke cluster should has finalizer that is added by hub controller
	gomega.Eventually(func() bool {
		spokeCluster, err := util.GetManagedCluster(hubClusterClient, managedClusterName)
		if err != nil {
			return false
		}
		if !commonhelpers.HasFinalizer(spokeCluster.Finalizers, clusterv1.ManagedClusterFinalizer) {
			return false
		}

		return true
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

	ginkgo.By("Accept and approve the ManagedCluster")
	// Simulate hub cluster admin to accept the managedcluster and approve the csr
	gomega.Eventually(func() error {
		return util.AcceptManagedCluster(hubClusterClient, managedClusterName)
	}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
	err := auth.ApproveSpokeClusterCSR(hubKubeClient, managedClusterName, certAget)
	gomega.Expect(err).NotTo(gomega.HaveOccurred())
}

func assertManagedClusterSuccessfullyJoined(testNamespace, managedClusterName, hubKubeconfigSecret string,
	hubKubeClient, spokeKubeClient kubernetes.Interface, hubClusterClient clusterclientset.Interface) {
	// The managed cluster should have accepted condition after it is accepted
	gomega.Eventually(func() error {
		spokeCluster, err := util.GetManagedCluster(hubClusterClient, managedClusterName)
		if err != nil {
			return err
		}
		if meta.IsStatusConditionFalse(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
			return fmt.Errorf("cluster should be accepted")
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	// The hub kubeconfig secret should be filled after the csr is approved
	gomega.Eventually(func() error {
		if _, err := util.GetFilledHubKubeConfigSecret(spokeKubeClient, testNamespace, hubKubeconfigSecret); err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	ginkgo.By("ManagedCluster joins the hub")

	// The spoke cluster should have joined condition finally
	gomega.Eventually(func() error {
		spokeCluster, err := util.GetManagedCluster(hubClusterClient, managedClusterName)
		if err != nil {
			return err
		}
		joined := meta.FindStatusCondition(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined)
		if joined == nil {
			return fmt.Errorf("cluster should be joined")
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

	// Ensure cluster namespace is in place
	gomega.Eventually(func() error {
		_, err := hubKubeClient.CoreV1().Namespaces().Get(context.TODO(), managedClusterName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		return nil
	}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
}

func startAutoRestartAgent(ctx context.Context,
	managedClusterName, hubKubeconfigDir string,
	getNewAgentOptions func() *spoke.SpokeAgentOptions,
	watchStop func(ctx context.Context, stopAgent context.CancelFunc, agentConfig *spoke.SpokeAgentConfig),
) {
	fmt.Println("[auto-restart-agent] - start agent...")
	newAgentOptions := getNewAgentOptions()
	agentCtx, stopAgent, agentConfig := startAgent(ctx, managedClusterName, hubKubeconfigDir, newAgentOptions)
	go watchStop(ctx, stopAgent, agentConfig)
	for {
		select {
		case <-agentCtx.Done():
			// restart agent
			time.Sleep(10 * time.Second) // Wait for new secret sync to files

			fmt.Println("[auto-restart-agent] - restart agent...")
			newAgentOptions := getNewAgentOptions()
			agentCtx, stopAgent, agentConfig = startAgent(ctx, managedClusterName, hubKubeconfigDir, newAgentOptions)
			go watchStop(ctx, stopAgent, agentConfig)
		case <-ctx.Done():
			// exit
			fmt.Println("[auto-restart-agent] - shutting down...")
			return
		}
	}
}

func startAgentHealthChecker(ctx context.Context, stopAgent context.CancelFunc, healthCheckers []healthz.HealthChecker) {
	ticker := time.NewTicker(3 * time.Second)
	fmt.Println("[agent-health-checker] - start health checking...")
	for {
		select {
		case <-ticker.C:
			for _, healthchecker := range healthCheckers {
				if err := healthchecker.Check(nil); err != nil {
					fmt.Printf("[agent-health-checker] - agent is not health: %v\n", err)
					stopAgent()
					return
				}
			}
			fmt.Println("[agent-health-checker] - agent is health")
		case <-ctx.Done():
			// exit
			fmt.Println("[agent-health-checker] - shutting down...")
			return
		}
	}
}
