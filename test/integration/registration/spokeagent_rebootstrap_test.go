package registration_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"path"
	"reflect"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/dsl/decorators"
	"github.com/onsi/gomega"
	"github.com/openshift/library-go/pkg/controller/controllercmd"
	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	certutil "k8s.io/client-go/util/cert"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
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

var _ = ginkgo.Describe("Rebootstrap", func() {
	var managedClusterName, bootstrapFile, hubKubeconfigSecret, addOnName, suffix string
	var signerName = certificates.KubeAPIServerClientSignerName
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

		bootstrapKubeConfigFile := path.Join(util.TestDir, "recovery-test", "kubeconfig-hub-b")
		err = newAuthn.CreateBootstrapKubeConfigWithCertAge(bootstrapKubeConfigFile, anotherServerCertFile, newSecurePort, 24*time.Hour)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// prepare clients
		kubeClient, err := kubernetes.NewForConfig(cfg)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(kubeClient).ToNot(gomega.BeNil())

		clusterClient, err := clusterclientset.NewForConfig(cfg)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(clusterClient).ToNot(gomega.BeNil())

		addOnClient, err := addonclientset.NewForConfig(cfg)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(clusterClient).ToNot(gomega.BeNil())

		// start hub controller
		go func() {
			err := hub.NewHubManagerOptions().RunControllerManager(ctx, &controllercmd.ControllerContext{
				KubeConfig:    cfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("hub"),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()

		return bootstrapKubeConfigFile, kubeClient, clusterClient, addOnClient, env, newAuthn
	}

	assertSuccessClusterBootstrap := func(testNamespace, managedClusterName, hubKubeconfigSecret string,
		hubKubeClient, spokeKubeClient kubernetes.Interface, hubClusterClient clusterclientset.Interface,
		auth *util.TestAuthn, certAget time.Duration) {
		// the spoke cluster and csr should be created after bootstrap
		ginkgo.By("Check existence of ManagedCluster & CSR")
		gomega.Eventually(func() bool {
			if _, err := util.GetManagedCluster(hubClusterClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() error {
			if _, err := util.FindUnapprovedSpokeCSR(hubKubeClient, managedClusterName); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		// the spoke cluster should has finalizer that is added by hub controller
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
		// simulate hub cluster admin to accept the managedcluster and approve the csr
		gomega.Eventually(func() error {
			return util.AcceptManagedCluster(hubClusterClient, managedClusterName)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
		err := auth.ApproveSpokeClusterCSR(hubKubeClient, managedClusterName, certAget)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// the managed cluster should have accepted condition after it is accepted
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

		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() error {
			if _, err := util.GetFilledHubKubeConfigSecret(spokeKubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		ginkgo.By("ManagedCluster joins the hub")
		// the spoke cluster should have joined condition finally
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

		// ensure cluster namespace is in place
		gomega.Eventually(func() error {
			_, err := hubKubeClient.CoreV1().Namespaces().Get(context.TODO(), managedClusterName, metav1.GetOptions{})
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())
	}

	assertSuccessCSRApproval := func(managedClusterName, addOnName string, hubKubeClient kubernetes.Interface) {
		ginkgo.By("Approve bootstrap csr")
		var csr *certificates.CertificateSigningRequest
		var err error
		gomega.Eventually(func() error {
			csr, err = util.FindUnapprovedAddOnCSR(hubKubeClient, managedClusterName, addOnName)
			if err != nil {
				return err
			}
			return nil
		}, eventuallyTimeout, eventuallyInterval).ShouldNot(gomega.HaveOccurred())

		now := time.Now()
		err = authn.ApproveCSR(hubKubeClient, csr, now.UTC(), now.Add(300*time.Second).UTC())
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	assertValidClientCertificate := func(secretNamespace, secretName, signerName string, spokeKubeClient kubernetes.Interface) {
		ginkgo.By("Check client certificate in secret")
		gomega.Eventually(func() bool {
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
			if !ok && signerName == certificates.KubeAPIServerClientSignerName {
				return false
			}
			if ok && signerName != certificates.KubeAPIServerClientSignerName {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertAddonLabel := func(managedClusterName, addonName, status string, hubClusterClient clusterclientset.Interface) {
		ginkgo.By("Check addon status label on managed cluster")
		gomega.Eventually(func() bool {
			cluster, err := util.GetManagedCluster(hubClusterClient, managedClusterName)
			if err != nil {
				return false
			}
			if len(cluster.Labels) == 0 {
				return false
			}
			key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addonName)
			return cluster.Labels[key] == status
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertSuccessAddOnBootstrap := func(
		managedClusterName, addOnName, signerName string,
		hubKubeClient, spokeKubeClient kubernetes.Interface,
		hubClusterClient clusterclientset.Interface, hubAddOnClient addonclientset.Interface) {
		ginkgo.By("Create ManagedClusterAddOn cr with required annotations")
		// create addon namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: addOnName,
			},
		}
		_, err := hubKubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// create addon
		addOn := &addonv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: managedClusterName,
			},
			Spec: addonv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: addOnName,
			},
		}
		_, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.TODO(), addOn, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		created, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		created.Status = addonv1alpha1.ManagedClusterAddOnStatus{
			Registrations: []addonv1alpha1.RegistrationConfig{
				{
					SignerName: signerName,
					Subject: addonv1alpha1.Subject{
						User: addOnName,
						Groups: []string{
							addOnName,
						},
					},
				},
			},
		}
		_, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.TODO(), created, metav1.UpdateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		assertSuccessCSRApproval(managedClusterName, addOnName, hubKubeClient)
		assertValidClientCertificate(addOnName, getSecretName(addOnName, signerName), signerName, spokeKubeClient)
		assertAddonLabel(managedClusterName, addOnName, "unreachable", hubClusterClient)
	}

	startAgent := func(ctx context.Context, managedClusterName, hubKubeconfigDir string,
		agentOptions *spoke.SpokeAgentOptions) (context.Context, context.CancelFunc, *spoke.SpokeAgentConfig) {
		ginkgo.By("run registration agent")
		commOptions := commonoptions.NewAgentOptions()
		commOptions.HubKubeconfigDir = hubKubeconfigDir
		commOptions.SpokeClusterName = managedClusterName

		agentCtx, stopAgent := context.WithCancel(ctx)
		agentConfig := spoke.NewSpokeAgentConfig(commOptions, agentOptions, stopAgent)
		runAgentWithContext(agentCtx, "rebootstrap-test", agentConfig, spokeCfg)

		return agentCtx, stopAgent, agentConfig
	}

	ginkgo.JustBeforeEach(func() {
		var spokeCtx, agentCtx context.Context
		var agentConfig *spoke.SpokeAgentConfig
		var stopAgent context.CancelFunc

		// ensure there is no remaining bootstrap-hub-kubeconfig secret
		err := kubeClient.CoreV1().Secrets(testNamespace).Delete(context.Background(), "bootstrap-hub-kubeconfig", metav1.DeleteOptions{})
		if err != nil {
			gomega.Expect(apierrors.IsNotFound(err)).To(gomega.BeTrue())
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
					agentCtx, stopAgent, _ = startAgent(spokeCtx, managedClusterName, hubKubeconfigDir, agentOptions)
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
			files, err := ioutil.ReadDir(bootstrapKubeconfigDir)
			if err != nil {
				return err
			}

			data := map[string][]byte{}
			for _, file := range files {
				if file.IsDir() {
					continue
				}

				key := file.Name()
				value, err := ioutil.ReadFile(path.Join(bootstrapKubeconfigDir, key))
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

	ginkgo.AfterEach(func() {
		if stopSpoke != nil {
			stopSpoke()
		}

		if stopNewHub != nil {
			stopNewHub()
		}
	})

	ginkgo.Context("bootstrapping", func() {
		ginkgo.BeforeEach(func() {
			createBootstrapKubeconfig = func(bootstrapFile, serverCertFile, securePort string, certAge time.Duration) {
				ginkgo.By("Create a bootstrap kubeconfig with an invalid port")
				err := authn.CreateBootstrapKubeConfigWithCertAge(bootstrapFile, serverCertFile, "700000", 10*time.Minute)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			}
		})

		ginkgo.It("should join the hub once the bootstrap kubeconfig becomes vaid", func() {
			// the spoke cluster should not be created
			gomega.Consistently(func() bool {
				if _, err := util.GetManagedCluster(clusterClient, managedClusterName); apierrors.IsNotFound(err) {
					return true
				}
				return false
			}, 15, 3).Should(gomega.BeTrue())

			ginkgo.By("Replace the bootstrap kubeconfig with a valid one")
			err := authn.CreateBootstrapKubeConfigWithCertAge(bootstrapFile, serverCertFile, securePort, 10*time.Minute)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			assertSuccessClusterBootstrap(testNamespace, managedClusterName, hubKubeconfigSecret, kubeClient, kubeClient, clusterClient, authn, time.Hour*24)
			assertSuccessAddOnBootstrap(managedClusterName, addOnName, signerName, kubeClient, kubeClient, clusterClient, addOnClient)
		})
	})

	ginkgo.Context("bootstrapped", func() {
		ginkgo.BeforeEach(func() {
			createBootstrapKubeconfig = func(bootstrapFile, serverCertFile, securePort string, certAge time.Duration) {
				ginkgo.By("Create a bootstrap kubeconfig")
				err := authn.CreateBootstrapKubeConfigWithCertAge(bootstrapFile, serverCertFile, securePort, 10*time.Minute)
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			}
		})

		ginkgo.It("should switch to the new hub successfully", decorators.Label("disaster recovery"), func() {
			assertSuccessClusterBootstrap(testNamespace, managedClusterName, hubKubeconfigSecret, kubeClient, kubeClient, clusterClient, authn, time.Hour*24)
			assertSuccessAddOnBootstrap(managedClusterName, addOnName, signerName, kubeClient, kubeClient, clusterClient, addOnClient)

			ginkgo.By("start a new hub")
			ctx, stopHub := context.WithCancel(context.Background())
			hubBootstrapKubeConfigFile, hubKubeClient, hubClusterClient, hubAddOnClient, testHubEnv, testAuthn := startNewHub(ctx)

			stopNewHub = func() {
				ginkgo.By("Stop the new hub")
				stopHub()
				err := testHubEnv.Stop()
				gomega.Expect(err).NotTo(gomega.HaveOccurred())
			}

			ginkgo.By("Update the bootstrap kubeconfig to connect to the new hub ")
			input, err := ioutil.ReadFile(hubBootstrapKubeConfigFile)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			err = ioutil.WriteFile(bootstrapFile, input, 0600)
			gomega.Expect(err).NotTo(gomega.HaveOccurred())

			assertSuccessClusterBootstrap(testNamespace, managedClusterName, hubKubeconfigSecret, hubKubeClient, kubeClient, hubClusterClient, testAuthn, time.Hour*24)
			assertSuccessAddOnBootstrap(managedClusterName, addOnName, signerName, hubKubeClient, kubeClient, hubClusterClient, hubAddOnClient)
		})

		ginkgo.It("should rebootstrap once the client certificate expires", func() {
			assertSuccessClusterBootstrap(testNamespace, managedClusterName, hubKubeconfigSecret, kubeClient, kubeClient, clusterClient, authn, 10*time.Second)

			ginkgo.By("stop the hub")
			stopHub()

			ginkgo.By("wait until the client cert expires")
			gomega.Eventually(func() bool {
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
			}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

			ginkgo.By("start the hub again")
			startHub()

			assertSuccessClusterBootstrap(testNamespace, managedClusterName, hubKubeconfigSecret, kubeClient, kubeClient, clusterClient, authn, time.Hour*24)
		})
	})
})
