package registration_test

import (
	"context"
	"fmt"
	"os"
	"path"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/dsl/decorators"
	. "github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	certutil "k8s.io/client-go/util/cert"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	addonclientset "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	commonhelpers "open-cluster-management.io/ocm/pkg/common/helpers"
	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/registration/hub"
	"open-cluster-management.io/ocm/pkg/registration/register"
	"open-cluster-management.io/ocm/pkg/registration/register/csr"
	"open-cluster-management.io/ocm/pkg/registration/spoke"
	"open-cluster-management.io/ocm/test/integration/util"
)

var _ = Describe("Rebootstrap", func() {
	var managedClusterName, bootstrapFile, hubKubeconfigSecret, addOnName, suffix string
	var stopSpoke context.CancelFunc
	var createBootstrapKubeconfig func(bootstrapFile, serverCertFile, securePort string, certAge time.Duration)
	var stopNewHub func()

	startNewHub := func(ctx context.Context) (
		string,
		kubernetes.Interface,
		clusterclientset.Interface,
		addonclientset.Interface,
		*envtest.Environment, *util.TestAuthn) {
		apiserver := &envtest.APIServer{}
		newAuthn := util.NewTestAuthn(path.Join(util.CertDir, "another-ca.crt"), path.Join(util.CertDir, "another-ca.key"))
		apiserver.Authn = newAuthn

		env := &envtest.Environment{
			ControlPlane: envtest.ControlPlane{
				APIServer: apiserver,
			},
			ErrorIfCRDPathMissing: true,
			CRDDirectoryPaths:     CRDPaths,
			WebhookInstallOptions: envtest.WebhookInstallOptions{},
		}

		cfg, err := env.Start()
		Expect(err).ToNot(HaveOccurred())
		Expect(cfg).ToNot(BeNil())

		err = clusterv1.Install(scheme.Scheme)
		Expect(err).NotTo(HaveOccurred())

		// patch addon for conversion webhook and connect to started webhook server
		apiExtensionClient, err := apiextensionsclient.NewForConfig(cfg)
		Expect(err).ToNot(HaveOccurred())
		err = util.AddConversionForAddonAPI(ctx, apiExtensionClient, testEnv, "managedclusteraddons.addon.open-cluster-management.io")
		Expect(err).ToNot(HaveOccurred())

		// prepare configs
		newSecurePort := env.ControlPlane.APIServer.Port
		Expect(len(newSecurePort)).ToNot(BeZero())

		anotherServerCertFile := fmt.Sprintf("%s/apiserver.crt", env.ControlPlane.APIServer.CertDir)

		bootstrapKubeConfigFile := path.Join(util.TestDir, "recovery-test", "kubeconfig-hub-b")
		err = newAuthn.CreateBootstrapKubeConfigWithCertAge(bootstrapKubeConfigFile, anotherServerCertFile, newSecurePort, 24*time.Hour)
		Expect(err).NotTo(HaveOccurred())

		// prepare clients
		kubeClient, err := kubernetes.NewForConfig(cfg)
		Expect(err).ToNot(HaveOccurred())
		Expect(kubeClient).ToNot(BeNil())

		clusterClient, err := clusterclientset.NewForConfig(cfg)
		Expect(err).ToNot(HaveOccurred())
		Expect(clusterClient).ToNot(BeNil())

		addOnClient, err := addonclientset.NewForConfig(cfg)
		Expect(err).ToNot(HaveOccurred())
		Expect(clusterClient).ToNot(BeNil())

		// start hub controller
		go func() {
			err := hub.NewHubManagerOptions().RunControllerManager(ctx, &controllercmd.ControllerContext{
				KubeConfig:    cfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("hub"),
			})
			Expect(err).NotTo(HaveOccurred())
		}()

		return bootstrapKubeConfigFile, kubeClient, clusterClient, addOnClient, env, newAuthn
	}

	assertSuccessClusterBootstrap := func(testNamespace, managedClusterName, hubKubeconfigSecret string,
		hubKubeClient, spokeKubeClient kubernetes.Interface, hubClusterClient clusterclientset.Interface,
		auth *util.TestAuthn, certAget time.Duration) {
		// the spoke cluster and csr should be created after bootstrap
		By("Check existence of ManagedCluster & CSR")
		Eventually(func() bool {
			if _, err := util.GetManagedCluster(hubClusterClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		Eventually(func() error {
			if _, err := util.FindUnapprovedSpokeCSR(hubKubeClient, managedClusterName); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

		// the spoke cluster should has finalizer that is added by hub controller
		Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(hubClusterClient, managedClusterName)
			if err != nil {
				return false
			}

			if !commonhelpers.HasFinalizer(spokeCluster.Finalizers, clusterv1.ManagedClusterFinalizer) {
				return false
			}

			return true
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

		By("Accept and approve the ManagedCluster")
		// simulate hub cluster admin to accept the managedcluster and approve the csr
		Eventually(func() error {
			return util.AcceptManagedCluster(hubClusterClient, managedClusterName)
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())
		err := auth.ApproveSpokeClusterCSR(hubKubeClient, managedClusterName, certAget)
		Expect(err).NotTo(HaveOccurred())

		// the managed cluster should have accepted condition after it is accepted
		Eventually(func() error {
			spokeCluster, err := util.GetManagedCluster(hubClusterClient, managedClusterName)
			if err != nil {
				return err
			}
			if meta.IsStatusConditionFalse(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
				return fmt.Errorf("cluster should be accepted")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

		// the hub kubeconfig secret should be filled after the csr is approved
		Eventually(func() error {
			if _, err := util.GetFilledHubKubeConfigSecret(spokeKubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

		By("ManagedCluster joins the hub")
		// the spoke cluster should have joined condition finally
		Eventually(func() error {
			spokeCluster, err := util.GetManagedCluster(hubClusterClient, managedClusterName)
			if err != nil {
				return err
			}
			joined := meta.FindStatusCondition(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined)
			if joined == nil {
				return fmt.Errorf("cluster should be joined")
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

		// ensure cluster namespace is in place
		Eventually(func() error {
			_, err := hubKubeClient.CoreV1().Namespaces().Get(context.TODO(), managedClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())
	}

	assertSuccessCSRApproval := func(managedClusterName, addOnName string, hubKubeClient kubernetes.Interface) {
		By("Approve bootstrap csr")
		var csr *certificates.CertificateSigningRequest
		var err error
		Eventually(func() error {
			csr, err = util.FindUnapprovedAddOnCSR(hubKubeClient, managedClusterName, addOnName)
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(HaveOccurred())

		now := time.Now()
		err = authn.ApproveCSR(hubKubeClient, csr, now.UTC(), now.Add(300*time.Second).UTC())
		Expect(err).NotTo(HaveOccurred())
	}

	assertValidClientCertificate := func(registrationType addonv1beta1.RegistrationType, secretNamespace, secretName string, spokeKubeClient kubernetes.Interface) {
		By("Check client certificate in secret")
		Eventually(func() bool {
			secret, err := spokeKubeClient.CoreV1().Secrets(secretNamespace).Get(context.TODO(), secretName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			if _, ok := secret.Data[csr.TLSKeyFile]; !ok {
				return false
			}
			if _, ok := secret.Data[csr.TLSCertFile]; !ok {
				return false
			}
			_, ok := secret.Data[register.KubeconfigFile]
			if !ok && registrationType == addonv1beta1.KubeClient {
				return false
			}
			if ok && registrationType != addonv1beta1.KubeClient {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	}

	assertAddonLabel := func(managedClusterName, addonName, status string, hubClusterClient clusterclientset.Interface) {
		By("Check addon status label on managed cluster")
		Eventually(func() bool {
			cluster, err := util.GetManagedCluster(hubClusterClient, managedClusterName)
			if err != nil {
				return false
			}
			if len(cluster.Labels) == 0 {
				return false
			}
			key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addonName)
			return cluster.Labels[key] == status
		}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())
	}

	assertSuccessAddOnBootstrap := func(
		managedClusterName, addOnName string,
		hubKubeClient, spokeKubeClient kubernetes.Interface,
		hubClusterClient clusterclientset.Interface, hubAddOnClient addonclientset.Interface) {
		By("Create ManagedClusterAddOn cr with required annotations")
		// create addon namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: addOnName,
			},
		}
		_, err := hubKubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		// create addon
		addOn := &addonv1beta1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: managedClusterName,
			},
			Spec: addonv1beta1.ManagedClusterAddOnSpec{},
		}
		_, err = hubAddOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Create(context.TODO(), addOn, metav1.CreateOptions{})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error {
			created, err := hubAddOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			created.Status.Registrations = []addonv1beta1.RegistrationConfig{
				{
					Type: addonv1beta1.KubeClient,
					KubeClient: &addonv1beta1.KubeClientConfig{
						Subject: addonv1beta1.KubeClientSubject{
							BaseSubject: addonv1beta1.BaseSubject{
								User:   addOnName,
								Groups: []string{addOnName},
							},
						},
					},
				},
			}
			created.Status.Namespace = addOnName
			_, err = hubAddOnClient.AddonV1beta1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.TODO(), created, metav1.UpdateOptions{})
			return err
		}, eventuallyTimeout, eventuallyInterval).Should(Succeed())

		assertSuccessCSRApproval(managedClusterName, addOnName, hubKubeClient)
		assertValidClientCertificate(addonv1beta1.KubeClient, addOnName, getSecretName(addonv1beta1.KubeClient, addOnName, ""), spokeKubeClient)
		assertAddonLabel(managedClusterName, addOnName, "unreachable", hubClusterClient)
	}

	startAgent := func(ctx context.Context, managedClusterName, hubKubeconfigDir string,
		agentOptions *spoke.SpokeAgentOptions) (context.Context, context.CancelFunc, *spoke.SpokeAgentConfig) {
		By("run registration agent")
		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName

		agentCtx, stopAgent := context.WithCancel(ctx)
		agentConfig := spoke.NewSpokeAgentConfig(commOptions, agentOptions, stopAgent)
		runAgentWithContext(agentCtx, "rebootstrap-test", agentConfig, spokeCfg)

		return agentCtx, stopAgent, agentConfig
	}

	JustBeforeEach(func() {
		var spokeCtx, agentCtx context.Context
		var agentConfig *spoke.SpokeAgentConfig
		var stopAgent context.CancelFunc

		// ensure there is no remaining bootstrap-hub-kubeconfig secret
		err := kubeClient.CoreV1().Secrets(testNamespace).Delete(context.Background(), "bootstrap-hub-kubeconfig", metav1.DeleteOptions{})
		if err != nil {
			Expect(apierrors.IsNotFound(err)).To(BeTrue())
		}

		// start agent
		suffix = rand.String(5)
		managedClusterName = fmt.Sprintf("cluster-%s", suffix)
		addOnName = fmt.Sprintf("addon-%s", suffix)
		hubKubeconfigSecret = fmt.Sprintf("hub-kubeconfig-secret-%s", suffix)
		hubKubeconfigDir := path.Join(util.TestDir, fmt.Sprintf("rebootstrap-%s", suffix), "hub-kubeconfig")
		bootstrapFile = path.Join(util.TestDir, fmt.Sprintf("rebootstrap-bootstrap-%s", suffix), "kubeconfig")

		createBootstrapKubeconfig(bootstrapFile, serverCertFile, securePort, 10*time.Minute)

		agentOptions := spoke.NewSpokeAgentOptions()
		agentOptions.BootstrapKubeconfig = bootstrapFile
		agentOptions.HubKubeconfigSecret = hubKubeconfigSecret

		spokeCtx, stopSpoke = context.WithCancel(context.Background())
		agentCtx, stopAgent, agentConfig = startAgent(spokeCtx, managedClusterName, hubKubeconfigDir, agentOptions)

		// simulate k8s scheduler to perform heath check and restart the agent if it is down/unhealth
		go func() {
			fmt.Println("[agent-scheduler] - start running...")
			ticker := time.NewTicker(3 * time.Second)
			for {
				select {
				case <-ticker.C:
					// health check
					fmt.Println("[agent-scheduler] - start health checking...")
					for _, healthchecker := range agentConfig.HealthCheckers() {
						if err := healthchecker.Check(nil); err != nil {
							fmt.Printf("[agent-scheduler] - stop agent because it is not health: %v\n", err)
							stopAgent()

							// reset the health checker
							agentOptions = spoke.NewSpokeAgentOptions()
							agentOptions.BootstrapKubeconfig = bootstrapFile
							agentOptions.HubKubeconfigSecret = hubKubeconfigSecret
							break
						}
					}
				case <-agentCtx.Done():
					// restart agent
					fmt.Println("[agent-scheduler] - restart agent...")
					agentCtx, stopAgent, agentConfig = startAgent(spokeCtx, managedClusterName, hubKubeconfigDir, agentOptions)
				case <-spokeCtx.Done():
					// exit
					fmt.Println("[agent-scheduler] - shutting down...")
					return
				}
			}
		}()

		// update the bootstrap kubeconfig secret once the bootstrap kubeconfig file changes
		syncBootstrapKubeconfig := func() error {
			bootstrapKubeconfigDir := path.Dir(bootstrapFile)
			files, err := os.ReadDir(bootstrapKubeconfigDir)
			if err != nil {
				return err
			}

			data := map[string][]byte{}
			for _, file := range files {
				if file.IsDir() {
					continue
				}

				key := file.Name()
				value, err := os.ReadFile(path.Join(bootstrapKubeconfigDir, key))
				if err != nil {
					return err
				}
				data[key] = value
			}

			secret, err := kubeClient.CoreV1().Secrets(testNamespace).Get(context.Background(), "bootstrap-hub-kubeconfig", metav1.GetOptions{})
			if apierrors.IsNotFound(err) {
				secret = &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "bootstrap-hub-kubeconfig",
						Namespace: testNamespace,
					},
					Data: data,
				}
				_, err = kubeClient.CoreV1().Secrets(testNamespace).Create(context.Background(), secret, metav1.CreateOptions{})
				if err == nil {
					fmt.Println("[bootstrap-kubeconfig-syncer] - bootstrap kubeconfig secret created")
				}
				return err
			}

			if err != nil {
				return err
			}

			if reflect.DeepEqual(secret.Data, data) {
				return nil
			}
			secretCopy := secret.DeepCopy()
			secretCopy.Data = data
			_, err = kubeClient.CoreV1().Secrets(testNamespace).Update(context.Background(), secretCopy, metav1.UpdateOptions{})
			if err == nil {
				fmt.Println("[bootstrap-kubeconfig-syncer] - bootstrap kubeconfig secret updated")
			}
			return err
		}

		go func() {
			fmt.Println("[bootstrap-kubeconfig-syncer] - start running...")
			ticker := time.NewTicker(3 * time.Second)
			for {
				select {
				case <-ticker.C:
					fmt.Println("[bootstrap-kubeconfig-syncer] - start file->secret syncing...")
					if err := syncBootstrapKubeconfig(); err != nil {
						fmt.Printf("[bootstrap-kubeconfig-syncer] - failed to sync file to secret: %v\n", err)
					}
				case <-spokeCtx.Done():
					// exit
					fmt.Println("[bootstrap-kubeconfig-syncer] - shutting down...")
					return
				}
			}
		}()
	})

	AfterEach(func() {
		if stopSpoke != nil {
			stopSpoke()
		}

		if stopNewHub != nil {
			stopNewHub()
		}
	})

	Context("bootstrapping", func() {
		BeforeEach(func() {
			createBootstrapKubeconfig = func(bootstrapFile, serverCertFile, securePort string, certAge time.Duration) {
				By("Create a bootstrap kubeconfig with an invalid port")
				err := authn.CreateBootstrapKubeConfigWithCertAge(bootstrapFile, serverCertFile, "700000", 10*time.Minute)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("should join the hub once the bootstrap kubeconfig becomes valid", func() {
			// the spoke cluster should not be created
			Consistently(func() error {
				if _, err := util.GetManagedCluster(clusterClient, managedClusterName); apierrors.IsNotFound(err) {
					return nil
				}
				return fmt.Errorf("managed cluster should not be created")
			}, 15, 3).Should(Succeed())

			By("Replace the bootstrap kubeconfig with a valid one")
			err := authn.CreateBootstrapKubeConfigWithCertAge(bootstrapFile, serverCertFile, securePort, 10*time.Minute)
			Expect(err).NotTo(HaveOccurred())

			assertSuccessClusterBootstrap(testNamespace, managedClusterName, hubKubeconfigSecret, kubeClient, kubeClient, clusterClient, authn, time.Hour*24)
			assertSuccessAddOnBootstrap(managedClusterName, addOnName, kubeClient, kubeClient, clusterClient, addOnClient)
		})
	})

	Context("bootstrapped", func() {
		BeforeEach(func() {
			createBootstrapKubeconfig = func(bootstrapFile, serverCertFile, securePort string, certAge time.Duration) {
				By("Create a bootstrap kubeconfig")
				err := authn.CreateBootstrapKubeConfigWithCertAge(bootstrapFile, serverCertFile, securePort, 10*time.Minute)
				Expect(err).NotTo(HaveOccurred())
			}
		})

		It("should switch to the new hub successfully", decorators.Label("disaster recovery"), func() {
			assertSuccessClusterBootstrap(testNamespace, managedClusterName, hubKubeconfigSecret, kubeClient, kubeClient, clusterClient, authn, time.Hour*24)
			assertSuccessAddOnBootstrap(managedClusterName, addOnName, kubeClient, kubeClient, clusterClient, addOnClient)

			By("start a new hub")
			ctx, stopHub := context.WithCancel(context.Background())
			hubBootstrapKubeConfigFile, hubKubeClient, hubClusterClient, hubAddOnClient, testHubEnv, testAuthn := startNewHub(ctx)

			stopNewHub = func() {
				By("Stop the new hub")
				stopHub()
				err := testHubEnv.Stop()
				Expect(err).NotTo(HaveOccurred())
			}

			By("Update the bootstrap kubeconfig to connect to the new hub ")
			input, err := os.ReadFile(hubBootstrapKubeConfigFile)
			Expect(err).NotTo(HaveOccurred())
			err = os.WriteFile(bootstrapFile, input, 0600)
			Expect(err).NotTo(HaveOccurred())

			assertSuccessClusterBootstrap(testNamespace, managedClusterName, hubKubeconfigSecret, hubKubeClient, kubeClient, hubClusterClient, testAuthn, time.Hour*24)
			assertSuccessAddOnBootstrap(managedClusterName, addOnName, hubKubeClient, kubeClient, hubClusterClient, hubAddOnClient)
		})

		It("should rebootstrap once the client certificate expires", func() {
			assertSuccessClusterBootstrap(testNamespace, managedClusterName, hubKubeconfigSecret, kubeClient, kubeClient, clusterClient, authn, 10*time.Second)

			By("stop the hub")
			stopHub()

			By("wait until the client cert expires")
			Eventually(func() bool {
				secret, err := kubeClient.CoreV1().Secrets(testNamespace).Get(context.Background(), hubKubeconfigSecret, metav1.GetOptions{})
				if err != nil {
					return false
				}
				data, ok := secret.Data[csr.TLSCertFile]
				if !ok {
					return false
				}
				certs, err := certutil.ParseCertsPEM(data)
				if err != nil {
					return false
				}
				now := time.Now()
				for _, cert := range certs {
					if now.After(cert.NotAfter) {
						return true
					}
				}
				return false
			}, eventuallyTimeout, eventuallyInterval).Should(BeTrue())

			By("start the hub again")
			startHub(hubOption)

			assertSuccessClusterBootstrap(testNamespace, managedClusterName, hubKubeconfigSecret, kubeClient, kubeClient, clusterClient, authn, time.Hour*24)
		})
	})
})
