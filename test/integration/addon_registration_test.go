package integration_test

import (
	"context"
	"fmt"
	"path"
	"reflect"
	"strings"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/util/rand"

	certificates "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/pkg/clientcert"
	"open-cluster-management.io/registration/pkg/features"
	"open-cluster-management.io/registration/pkg/spoke"
	"open-cluster-management.io/registration/test/integration/util"
)

var _ = ginkgo.Describe("Addon Registration", func() {
	var managedClusterName, hubKubeconfigSecret, hubKubeconfigDir, addOnName string
	var err error
	var cancel context.CancelFunc

	ginkgo.BeforeEach(func() {
		suffix := rand.String(5)
		managedClusterName = fmt.Sprintf("managedcluster-%s", suffix)
		hubKubeconfigSecret = fmt.Sprintf("hub-kubeconfig-secret-%s", suffix)
		hubKubeconfigDir = path.Join(util.TestDir, fmt.Sprintf("addontest-%s", suffix), "hub-kubeconfig")
		addOnName = fmt.Sprintf("addon-%s", suffix)

		err := features.DefaultSpokeMutableFeatureGate.Set("AddonManagement=true")
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		agentOptions := spoke.SpokeAgentOptions{
			ClusterName:              managedClusterName,
			BootstrapKubeconfig:      bootstrapKubeConfigFile,
			HubKubeconfigSecret:      hubKubeconfigSecret,
			HubKubeconfigDir:         hubKubeconfigDir,
			ClusterHealthCheckPeriod: 1 * time.Minute,
		}

		// run registration agent
		cancel = util.RunAgent("addontest", agentOptions, spokeCfg)
	})

	ginkgo.AfterEach(
		func() {
			cancel()
		})

	assertSuccessClusterBootstrap := func() {
		// the spoke cluster and csr should be created after bootstrap
		ginkgo.By("Check existence of ManagedCluster & CSR")
		gomega.Eventually(func() bool {
			if _, err := util.GetManagedCluster(clusterClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		gomega.Eventually(func() bool {
			if _, err := util.FindUnapprovedSpokeCSR(kubeClient, managedClusterName); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// the spoke cluster should has finalizer that is added by hub controller
		gomega.Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
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
		err = util.AcceptManagedCluster(clusterClient, managedClusterName)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		err = authn.ApproveSpokeClusterCSR(kubeClient, managedClusterName, time.Hour*24)
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// the managed cluster should have accepted condition after it is accepted
		gomega.Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			accpeted := meta.FindStatusCondition(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted)
			return accpeted != nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// the hub kubeconfig secret should be filled after the csr is approved
		gomega.Eventually(func() bool {
			if _, err := util.GetFilledHubKubeConfigSecret(kubeClient, testNamespace, hubKubeconfigSecret); err != nil {
				return false
			}
			return true
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("ManagedCluster joins the hub")
		// the spoke cluster should have joined condition finally
		gomega.Eventually(func() bool {
			spokeCluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			joined := meta.FindStatusCondition(spokeCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined)
			return joined != nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		// ensure cluster namespace is in place
		gomega.Eventually(func() bool {
			_, err := kubeClient.CoreV1().Namespaces().Get(context.TODO(), managedClusterName, metav1.GetOptions{})
			return err == nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertSuccessCSRApproval := func() {
		ginkgo.By("Approve bootstrap csr")
		var csr *certificates.CertificateSigningRequest
		gomega.Eventually(func() bool {
			csr, err = util.FindUnapprovedAddOnCSR(kubeClient, managedClusterName, addOnName)
			return err == nil
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		now := time.Now()
		err = authn.ApproveCSR(kubeClient, csr, now.UTC(), now.Add(30*time.Second).UTC())
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
	}

	assertValidClientCertificate := func(secretNamespace, secretName, signerName string) {
		ginkgo.By("Check client certificate in secret")
		gomega.Eventually(func() bool {
			secret, err := kubeClient.CoreV1().Secrets(secretNamespace).Get(context.TODO(), secretName, metav1.GetOptions{})
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

	assertAddonLabel := func(clusterName, addonName, status string) {
		ginkgo.By("Check addon status label on managed cluster")
		gomega.Eventually(func() bool {
			cluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
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

	assertClientCertCondition := func(clusterName, addonName string) {
		ginkgo.By("Check clientcert addon status condition")
		gomega.Eventually(func() bool {
			addon, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			return meta.IsStatusConditionTrue(addon.Status.Conditions, clientcert.ClusterCertificateRotatedCondition)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertHasNoAddonLabel := func(clusterName, addonName string) {
		ginkgo.By("Check if addon status label on managed cluster deleted")
		gomega.Eventually(func() bool {
			cluster, err := util.GetManagedCluster(clusterClient, managedClusterName)
			if err != nil {
				return false
			}
			if len(cluster.Labels) == 0 {
				return true
			}
			key := fmt.Sprintf("feature.open-cluster-management.io/addon-%s", addonName)
			_, ok := cluster.Labels[key]
			return !ok
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	assertSuccessAddOnBootstrap := func(signerName string) {
		ginkgo.By("Create ManagedClusterAddOn cr with required annotations")
		// create addon namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: addOnName,
			},
		}
		_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
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
		_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.TODO(), addOn, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		created, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		created.Status = addonv1alpha1.ManagedClusterAddOnStatus{
			Registrations: []addonv1alpha1.RegistrationConfig{
				{
					SignerName: signerName,
				},
			},
		}
		_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.TODO(), created, metav1.UpdateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		assertSuccessCSRApproval()
		assertValidClientCertificate(addOnName, getSecretName(addOnName, signerName), signerName)
		assertAddonLabel(managedClusterName, addOnName, "unreachable")
		assertClientCertCondition(managedClusterName, addOnName)
	}

	assertSecretGone := func(secretNamespace, secretName string) {
		gomega.Eventually(func() bool {
			_, err = kubeClient.CoreV1().Secrets(secretNamespace).Get(context.TODO(), secretName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	}

	ginkgo.It("should register addon successfully", func() {
		assertSuccessClusterBootstrap()
		signerName := "kubernetes.io/kube-apiserver-client"
		assertSuccessAddOnBootstrap(signerName)

		ginkgo.By("Delete the addon and check if secret is gone")
		err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(context.TODO(), addOnName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		assertSecretGone(addOnName, getSecretName(addOnName, signerName))

		assertHasNoAddonLabel(managedClusterName, addOnName)
	})

	ginkgo.It("should register addon successfully even when the install namespace is not available at the beginning", func() {
		assertSuccessClusterBootstrap()

		signerName := "kubernetes.io/kube-apiserver-client"
		ginkgo.By("Create ManagedClusterAddOn cr with required annotations")

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
		_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Create(context.TODO(), addOn, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		created, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		created.Status = addonv1alpha1.ManagedClusterAddOnStatus{
			Registrations: []addonv1alpha1.RegistrationConfig{
				{
					SignerName: signerName,
				},
			},
		}
		_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.TODO(), created, metav1.UpdateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		assertSuccessCSRApproval()

		ginkgo.By("Wait for addon namespace")
		gomega.Consistently(func() bool {
			csrs, err := util.FindAddOnCSRs(kubeClient, managedClusterName, addOnName)
			if err != nil {
				return false
			}
			return len(csrs) == 1
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())

		ginkgo.By("Create addon namespace")
		// create addon namespace
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: addOnName,
			},
		}
		_, err = kubeClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		assertValidClientCertificate(addOnName, getSecretName(addOnName, signerName), signerName)

		ginkgo.By("Delete the addon and check if secret is gone")
		err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Delete(context.TODO(), addOnName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		assertSecretGone(addOnName, getSecretName(addOnName, signerName))
	})

	ginkgo.It("should register addon with custom signer successfully", func() {
		assertSuccessClusterBootstrap()
		signerName := "example.com/signer1"
		assertSuccessAddOnBootstrap(signerName)
	})

	ginkgo.It("should addon registraton config updated successfully", func() {
		assertSuccessClusterBootstrap()
		signerName := "kubernetes.io/kube-apiserver-client"
		assertSuccessAddOnBootstrap(signerName)

		// update registration config and change the signer
		addOn, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		newSignerName := "example.com/signer1"
		addOn.Status = addonv1alpha1.ManagedClusterAddOnStatus{
			Registrations: []addonv1alpha1.RegistrationConfig{
				{
					SignerName: newSignerName,
				},
			},
		}
		_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.TODO(), addOn, metav1.UpdateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		assertSecretGone(addOnName, getSecretName(addOnName, signerName))

		assertSuccessCSRApproval()
		assertValidClientCertificate(addOnName, getSecretName(addOnName, newSignerName), newSignerName)
	})

	ginkgo.It("should rotate addon client cert successfully", func() {
		assertSuccessClusterBootstrap()
		signerName := "kubernetes.io/kube-apiserver-client"
		assertSuccessAddOnBootstrap(signerName)

		secretName := getSecretName(addOnName, signerName)
		secret, err := kubeClient.CoreV1().Secrets(addOnName).Get(context.TODO(), secretName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		ginkgo.By("Wait for cert rotation")
		assertSuccessCSRApproval()
		gomega.Eventually(func() bool {
			newSecret, err := kubeClient.CoreV1().Secrets(addOnName).Get(context.TODO(), secretName, metav1.GetOptions{})
			if err != nil {
				return false
			}

			return !reflect.DeepEqual(secret.Data, newSecret.Data)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.BeTrue())
	})

	ginkgo.It("should stop addon client cert update if too frequent", func() {
		assertSuccessClusterBootstrap()
		signerName := "kubernetes.io/kube-apiserver-client"
		assertSuccessAddOnBootstrap(signerName)

		// update subject for 15 times
		for i := 1; i <= 15; i++ {
			addOn, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			addOn.Status = addonv1alpha1.ManagedClusterAddOnStatus{
				Registrations: []addonv1alpha1.RegistrationConfig{
					{
						SignerName: addOn.Status.Registrations[0].SignerName,
						Subject: addonv1alpha1.Subject{
							User: fmt.Sprintf("test-%d", i),
						},
					},
				},
			}
			_, err = addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).UpdateStatus(context.TODO(), addOn, metav1.UpdateOptions{})
			gomega.Expect(err).NotTo(gomega.HaveOccurred())
			// sleep 1 second to ensure controller issue a new csr.
			time.Sleep(1 * time.Second)
		}

		ginkgo.By("CSR should not exceed 10")
		csrs, err := kubeClient.CertificatesV1().CertificateSigningRequests().List(context.TODO(), metav1.ListOptions{
			LabelSelector: fmt.Sprintf("%s=%s,%s=%s", clientcert.ClusterNameLabel, managedClusterName, clientcert.AddonNameLabel, addOnName),
		})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		gomega.Expect(len(csrs.Items) >= 10).ShouldNot(gomega.BeFalse())

		gomega.Eventually(func() error {
			addOn, err := addOnClient.AddonV1alpha1().ManagedClusterAddOns(managedClusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if meta.IsStatusConditionFalse(addOn.Status.Conditions, "ClusterCertificateRotated") {
				return nil
			}

			return fmt.Errorf("addon status is not correct, got %v", addOn.Status.Conditions)
		}, eventuallyTimeout, eventuallyInterval).Should(gomega.Succeed())

	})
})

func getSecretName(addOnName, signerName string) string {
	if signerName == "kubernetes.io/kube-apiserver-client" {
		return fmt.Sprintf("%s-hub-kubeconfig", addOnName)
	}
	return fmt.Sprintf("%s-%s-client-cert", addOnName, strings.ReplaceAll(signerName, "/", "-"))
}
