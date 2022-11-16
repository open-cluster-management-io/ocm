package integration_test

import (
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonclientset "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/clientcert"
	"open-cluster-management.io/registration/pkg/features"
	"open-cluster-management.io/registration/pkg/hub"
	"open-cluster-management.io/registration/pkg/spoke"
	"open-cluster-management.io/registration/test/integration/util"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
)

var _ = ginkgo.Describe("Disaster Recovery", func() {
	startHub := func(ctx context.Context) (string, kubernetes.Interface, clusterclientset.Interface, addonclientset.Interface, *envtest.Environment, *util.TestAuthn) {
		apiserver := &envtest.APIServer{}
		newAuthn := util.NewTestAuthn(path.Join(util.CertDir, "another-ca.crt"), path.Join(util.CertDir, "another-ca.key"))
		apiserver.SecureServing.Authn = newAuthn

		env := &envtest.Environment{
			ControlPlane: envtest.ControlPlane{
				APIServer: apiserver,
			},
			ErrorIfCRDPathMissing: true,
			CRDDirectoryPaths: []string{
				filepath.Join(".", "deploy", "hub"),
			},
		}

		cfg, err := env.Start()
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(cfg).ToNot(gomega.BeNil())

		err = clusterv1.AddToScheme(scheme.Scheme)
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
			err := hub.RunControllerManager(ctx, &controllercmd.ControllerContext{
				KubeConfig:    cfg,
				EventRecorder: util.NewIntegrationTestEventRecorder("hub"),
			})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()

		return bootstrapKubeConfigFile, kubeClient, clusterClient, addOnClient, env, newAuthn
	}

	startRegistrationAgent := func(managedClusterName, bootstrapKubeConfigFile, hubKubeconfigSecret, hubKubeconfigDir string) context.CancelFunc {
		err := features.DefaultSpokeMutableFeatureGate.Set("AddonManagement=true")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		agentOptions := spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}
		return util.RunAgent("addontest", agentOptions, spokeCfg)
	}

	assertSuccessClusterBootstrap := func(testNamespace, managedClusterName, hubKubeconfigSecret string, hubKubeClient, spokeKubeClient kubernetes.Interface, hubClusterClient clusterclientset.Interface, auth *util.TestAuthn) {
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
			if len(spokeCluster.Finalizers) != 1 {
				return false
			}

			if spokeCluster.Finalizers[0] != "cluster.open-cluster-management.io/api-resource-cleanup" {
				return false
			}

			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("Accept and approve the ManagedCluster")
		// simulate hub cluster admin to accept the managedcluster and approve the csr
		gomega.Eventually(func() error {
			return util.AcceptManagedCluster(hubClusterClient, managedClusterName)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())
		err := auth.ApproveSpokeClusterCSR(hubKubeClient, managedClusterName, time.Hour*24)
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
			if _, ok := secret.Data[clientcert.TLSKeyFile]; !ok {
				return false
			}
			if _, ok := secret.Data[clientcert.TLSCertFile]; !ok {
				return false
			}
			_, ok := secret.Data[clientcert.KubeconfigFile]
			if !ok && signerName == "kubernetes.io/kube-apiserver-client" {
				return false
			}
			if ok && signerName != "kubernetes.io/kube-apiserver-client" {
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

	assertSuccessAddOnBootstrap := func(managedClusterName, addOnName, signerName string, hubKubeClient, spokeKubeClient kubernetes.Interface, hubClusterClient clusterclientset.Interface, hubAddOnClient addonclientset.Interface) {
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

	ginkgo.It("should register addon successfully", func() {
		suffix := rand.String(5)
		managedClusterName := fmt.Sprintf("managedcluster-%s", suffix)
		hubKubeconfigSecret := fmt.Sprintf("hub-kubeconfig-secret-%s", suffix)
		hubKubeconfigDir := path.Join(util.TestDir, fmt.Sprintf("recoverytest-%s", suffix), "hub-kubeconfig")
		addOnName := fmt.Sprintf("addon-%s", suffix)
		signerName := "kubernetes.io/kube-apiserver-client"

		hubKubeClient := kubeClient
		hubClusterClient := clusterClient
		hubAddOnClient := addOnClient
		spokeKubeClient := kubeClient

		ginkgo.By("start registation agent to connect to hub A")
		stopAgent := startRegistrationAgent(managedClusterName, bootstrapKubeConfigFile, hubKubeconfigSecret, hubKubeconfigDir)

		assertSuccessClusterBootstrap(testNamespace, managedClusterName, hubKubeconfigSecret, hubKubeClient, spokeKubeClient, hubClusterClient, authn)
		assertSuccessAddOnBootstrap(managedClusterName, addOnName, signerName, hubKubeClient, spokeKubeClient, hubClusterClient, hubAddOnClient)

		ginkgo.By("simulate klusterlet to stop registation agent and delete hub kubeconfig secret")
		stopAgent()
		err := spokeKubeClient.CoreV1().Secrets(testNamespace).Delete(context.Background(), hubKubeconfigSecret, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		err = os.RemoveAll(hubKubeconfigDir)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By("start hub B")
		ctx, stopHub := context.WithCancel(context.Background())
		hubBootstrapKubeConfigFile, hubKubeClient, hubClusterClient, hubAddOnClient, testHubEnv, testAuthn := startHub(ctx)
		defer func() {
			err := testHubEnv.Stop()
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
		}()
		defer stopHub()

		ginkgo.By("simulate klusterlet to restart registation agent and connect to hub B")
		stopAgent = startRegistrationAgent(managedClusterName, hubBootstrapKubeConfigFile, hubKubeconfigSecret, hubKubeconfigDir)
		defer stopAgent()

		assertSuccessClusterBootstrap(testNamespace, managedClusterName, hubKubeconfigSecret, hubKubeClient, spokeKubeClient, hubClusterClient, testAuthn)
		assertSuccessAddOnBootstrap(managedClusterName, addOnName, signerName, hubKubeClient, spokeKubeClient, hubClusterClient, hubAddOnClient)
	})
})
