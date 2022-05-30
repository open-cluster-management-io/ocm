package e2e

import (
	"bytes"
	"context"
	goerrors "errors"
	"fmt"
	"io/ioutil"
	"reflect"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/runtime/serializer/streaming"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"

	"open-cluster-management.io/registration/deploy"
	"open-cluster-management.io/registration/pkg/clientcert"
	"open-cluster-management.io/registration/pkg/helpers"
)

var _ = ginkgo.Describe("Loopback registration [development]", func() {
	var clusterId string
	ginkgo.BeforeEach(func() {
		// apply ClusterClaim crd
		claimCrd, err := claimCrd()
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gvr := schema.GroupVersionResource{
			Group:    "apiextensions.k8s.io",
			Version:  "v1",
			Resource: "customresourcedefinitions",
		}
		err = hubDynamicClient.Resource(gvr).Delete(context.TODO(), claimCrd.GetName(), metav1.DeleteOptions{})
		if !errors.IsNotFound(err) {
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}
		err = wait.Poll(1*time.Second, 5*time.Second, func() (done bool, err error) {
			_, err = hubDynamicClient.Resource(gvr).Create(context.TODO(), claimCrd, metav1.CreateOptions{})
			if err != nil {
				return false, err
			}

			return true, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// create ClusterClaim cr
		clusterId = rand.String(12)
		claim := &clusterv1alpha1.ClusterClaim{
			ObjectMeta: metav1.ObjectMeta{
				Name: "id.k8s.io",
			},
			Spec: clusterv1alpha1.ClusterClaimSpec{
				Value: clusterId,
			},
		}
		// delete the claim if exists
		err = clusterClient.ClusterV1alpha1().ClusterClaims().Delete(context.TODO(), claim.Name, metav1.DeleteOptions{})
		if !errors.IsNotFound(err) {
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}
		// create the claim
		err = wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
			var err error
			_, err = clusterClient.ClusterV1alpha1().ClusterClaims().Create(context.TODO(), claim, metav1.CreateOptions{})
			if err != nil {
				return false, err
			}

			return true, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})

	ginkgo.It("Should register the hub as a managed cluster", func() {
		var (
			err    error
			suffix = rand.String(6)
			nsName = fmt.Sprintf("loopback-spoke-%v", suffix)
			ns     = &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: nsName,
				},
			}
		)
		ginkgo.By(fmt.Sprintf("Deploying the agent using suffix=%q ns=%q", suffix, nsName))
		err = wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
			var err error
			ns, err = hubClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
			if err != nil {
				return false, err
			}

			return true, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// This test expects a bootstrap secret to exist in open-cluster-management-agent/e2e-bootstrap-secret
		e2eBootstrapSecret, err := hubClient.CoreV1().Secrets("open-cluster-management-agent").Get(context.TODO(), "e2e-bootstrap-secret", metav1.GetOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		bootstrapSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: nsName,
				Name:      "bootstrap-secret",
			},
		}
		bootstrapSecret.Data = e2eBootstrapSecret.Data
		err = wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
			var err error
			_, err = hubClient.CoreV1().Secrets(nsName).Create(context.TODO(), bootstrapSecret, metav1.CreateOptions{})
			if err != nil {
				return false, err
			}

			return true, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var (
			cr         *unstructured.Unstructured
			crResource = schema.GroupVersionResource{
				Group:    "rbac.authorization.k8s.io",
				Version:  "v1",
				Resource: "clusterroles",
			}
		)
		cr, err = spokeCR(suffix)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
			var err error
			_, err = hubDynamicClient.Resource(crResource).Create(context.TODO(), cr, metav1.CreateOptions{})
			if err != nil {
				return false, err
			}

			return true, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var (
			crb         *unstructured.Unstructured
			crbResource = schema.GroupVersionResource{
				Group:    "rbac.authorization.k8s.io",
				Version:  "v1",
				Resource: "clusterrolebindings",
			}
		)
		crb, err = spokeCRB(nsName, suffix)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
			var err error
			_, err = hubDynamicClient.Resource(crbResource).Create(context.TODO(), crb, metav1.CreateOptions{})
			if err != nil {
				return false, err
			}

			return true, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var (
			role         *unstructured.Unstructured
			roleResource = schema.GroupVersionResource{
				Group:    "rbac.authorization.k8s.io",
				Version:  "v1",
				Resource: "roles",
			}
		)
		role, err = spokeRole(nsName, suffix)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
			var err error
			_, err = hubDynamicClient.Resource(roleResource).Namespace(nsName).Create(context.TODO(), role, metav1.CreateOptions{})
			if err != nil {
				return false, err
			}

			return true, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var (
			roleBinding         *unstructured.Unstructured
			roleBindingResource = schema.GroupVersionResource{
				Group:    "rbac.authorization.k8s.io",
				Version:  "v1",
				Resource: "rolebindings",
			}
		)
		roleBinding, err = spokeRoleBinding(nsName, suffix)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
			var err error
			_, err = hubDynamicClient.Resource(roleBindingResource).Namespace(nsName).Create(context.TODO(), roleBinding, metav1.CreateOptions{})
			if err != nil {
				return false, err
			}

			return true, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		sa := &corev1.ServiceAccount{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: nsName,
				Name:      "spoke-agent-sa",
			},
		}
		err = wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
			var err error
			_, err = hubClient.CoreV1().ServiceAccounts(nsName).Create(context.TODO(), sa, metav1.CreateOptions{})
			if err != nil {
				return false, err
			}

			return true, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var (
			deployment         *unstructured.Unstructured
			deploymentResource = schema.GroupVersionResource{
				Group:    "apps",
				Version:  "v1",
				Resource: "deployments",
			}
		)
		clusterName := fmt.Sprintf("loopback-e2e-%v", suffix)
		deployment, err = spokeDeployment(nsName, clusterName, registrationImage)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		err = wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
			var err error
			_, err = hubDynamicClient.Resource(deploymentResource).Namespace(nsName).Create(context.TODO(), deployment, metav1.CreateOptions{})
			if err != nil {
				return false, err
			}

			return true, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		var (
			csrs      *certificatesv1.CertificateSigningRequestList
			csrClient = hubClient.CertificatesV1().CertificateSigningRequests()
		)

		ginkgo.By(fmt.Sprintf("Waiting for the CSR for cluster %q to exist", clusterName))
		err = wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
			var err error
			csrs, err = csrClient.List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("open-cluster-management.io/cluster-name = %v", clusterName),
			})
			if err != nil {
				return false, err
			}

			if len(csrs.Items) >= 1 {
				return true, nil
			}

			return false, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Approving all pending CSRs")
		var csr *certificatesv1.CertificateSigningRequest
		for i := range csrs.Items {
			csr = &csrs.Items[i]

			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				csr, err = csrClient.Get(context.TODO(), csr.Name, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				if helpers.IsCSRInTerminalState(&csr.Status) {
					return nil
				}

				csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
					Type:    certificatesv1.CertificateApproved,
					Status:  corev1.ConditionTrue,
					Reason:  "Approved by E2E",
					Message: "Approved as part of Loopback e2e",
				})
				_, err := csrClient.UpdateApproval(context.TODO(), csr.Name, csr, metav1.UpdateOptions{})
				return err
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}

		var (
			managedCluster  *clusterv1.ManagedCluster
			managedClusters = clusterClient.ClusterV1().ManagedClusters()
		)

		ginkgo.By(fmt.Sprintf("Waiting for ManagedCluster %q to exist", clusterName))
		err = wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
			var err error
			managedCluster, err = managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{})
			if errors.IsNotFound(err) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			return true, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(managedCluster.Spec.HubAcceptsClient).To(gomega.Equal(false))

		ginkgo.By(fmt.Sprintf("Accepting ManagedCluster %q", clusterName))
		err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
			var err error
			managedCluster, err = managedClusters.Get(context.TODO(), managedCluster.Name, metav1.GetOptions{})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())

			managedCluster.Spec.HubAcceptsClient = true
			managedCluster.Spec.LeaseDurationSeconds = 5
			managedCluster, err = managedClusters.Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
			return err
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Waiting for ManagedCluster to have HubAccepted=true")
		err = wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
			var err error
			managedCluster, err := managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}

			if meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
				return true, nil
			}

			return false, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Waiting for ManagedCluster to join the hub cluser")
		err = wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
			var err error
			managedCluster, err := managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}

			return meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined), nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Waiting for ManagedCluster available")
		err = wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
			managedCluster, err := managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}

			return meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable), nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		leaseName := "managed-cluster-lease"
		ginkgo.By(fmt.Sprintf("Make sure ManagedCluster lease %q exists", leaseName))
		var lastRenewTime *metav1.MicroTime
		err = wait.Poll(1*time.Second, 30*time.Second, func() (bool, error) {
			lease, err := hubClient.CoordinationV1().Leases(clusterName).Get(context.TODO(), leaseName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			lastRenewTime = lease.Spec.RenewTime
			return true, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("Make sure ManagedCluster lease %q is updated", leaseName))
		err = wait.Poll(1*time.Second, 30*time.Second, func() (bool, error) {
			lease, err := hubClient.CoordinationV1().Leases(clusterName).Get(context.TODO(), leaseName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			leaseUpdated := lastRenewTime.Before(lease.Spec.RenewTime)
			if leaseUpdated {
				lastRenewTime = lease.Spec.RenewTime
			}
			return leaseUpdated, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("Make sure ManagedCluster lease %q is updated again", leaseName))
		err = wait.Poll(1*time.Second, 30*time.Second, func() (bool, error) {
			lease, err := hubClient.CoordinationV1().Leases(clusterName).Get(context.TODO(), leaseName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}
			return lastRenewTime.Before(lease.Spec.RenewTime), nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Make sure ManagedCluster is still available")
		err = wait.Poll(1*time.Second, 30*time.Second, func() (bool, error) {
			managedCluster, err := managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}

			return meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable), nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		// make sure the cpu and memory are still in the status, for compatibility
		ginkgo.By("Make sure cpu and memory exist in status")
		err = wait.Poll(1*time.Second, 30*time.Second, func() (bool, error) {
			managedCluster, err := managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}

			if _, exist := managedCluster.Status.Allocatable[clusterv1.ResourceCPU]; !exist {
				return false, fmt.Errorf("Resource %v doesn't exist in Allocatable", clusterv1.ResourceCPU)
			}

			if _, exist := managedCluster.Status.Allocatable[clusterv1.ResourceMemory]; !exist {
				return false, fmt.Errorf("Resource %v doesn't exist in Allocatable", clusterv1.ResourceMemory)
			}

			if _, exist := managedCluster.Status.Capacity[clusterv1.ResourceCPU]; !exist {
				return false, fmt.Errorf("Resource %v doesn't exist in Capacity", clusterv1.ResourceCPU)
			}

			if _, exist := managedCluster.Status.Capacity[clusterv1.ResourceMemory]; !exist {
				return false, fmt.Errorf("Resource %v doesn't exist in Capacity", clusterv1.ResourceMemory)
			}

			return true, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Make sure ClusterClaims are synced")
		clusterClaims := []clusterv1.ManagedClusterClaim{
			{
				Name:  "id.k8s.io",
				Value: clusterId,
			},
		}
		err = wait.Poll(1*time.Second, 30*time.Second, func() (bool, error) {
			managedCluster, err := managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{})
			if err != nil {
				return false, err
			}

			return reflect.DeepEqual(clusterClaims, managedCluster.Status.ClusterClaims), nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Create addon on hub")
		addOnName := fmt.Sprintf("loopback-e2e-addon-%v", suffix)
		// create namespace for addon on spoke
		addOnNs := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: addOnName,
			},
		}
		_, err = hubClient.CoreV1().Namespaces().Create(context.TODO(), addOnNs, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		// create an addon
		addOn := &addonv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				Name:      addOnName,
				Namespace: clusterName,
			},
			Spec: addonv1alpha1.ManagedClusterAddOnSpec{
				InstallNamespace: addOnName,
			},
		}
		_, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Create(context.TODO(), addOn, metav1.CreateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		created, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Get(context.TODO(), addOnName, metav1.GetOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())
		created.Status = addonv1alpha1.ManagedClusterAddOnStatus{
			Registrations: []addonv1alpha1.RegistrationConfig{
				{
					SignerName: "kubernetes.io/kube-apiserver-client",
				},
			},
		}
		_, err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).UpdateStatus(context.TODO(), created, metav1.UpdateOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("Waiting for the CSR for addOn %q to exist", addOnName))
		err = wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
			var err error
			csrs, err = csrClient.List(context.TODO(), metav1.ListOptions{
				LabelSelector: fmt.Sprintf("open-cluster-management.io/cluster-name=%s,open-cluster-management.io/addon-name=%s", clusterName, addOnName),
			})
			if err != nil {
				return false, err
			}

			if len(csrs.Items) >= 1 {
				return true, nil
			}

			return false, nil
		})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By("Approving all pending CSRs")
		for i := range csrs.Items {
			csr = &csrs.Items[i]

			err = retry.RetryOnConflict(retry.DefaultRetry, func() error {
				csr, err = csrClient.Get(context.TODO(), csr.Name, metav1.GetOptions{})
				gomega.Expect(err).ToNot(gomega.HaveOccurred())

				if helpers.IsCSRInTerminalState(&csr.Status) {
					return nil
				}

				csr.Status.Conditions = append(csr.Status.Conditions, certificatesv1.CertificateSigningRequestCondition{
					Type:    certificatesv1.CertificateApproved,
					Status:  corev1.ConditionTrue,
					Reason:  "Approved by E2E",
					Message: "Approved as part of Loopback e2e",
				})
				_, err := csrClient.UpdateApproval(context.TODO(), csr.Name, csr, metav1.UpdateOptions{})
				return err
			})
			gomega.Expect(err).ToNot(gomega.HaveOccurred())
		}

		ginkgo.By("Check addon client certificate in secret")
		secretName := fmt.Sprintf("%s-hub-kubeconfig", addOnName)
		gomega.Eventually(func() bool {
			secret, err := hubClient.CoreV1().Secrets(addOnName).Get(context.TODO(), secretName, metav1.GetOptions{})
			if err != nil {
				return false
			}
			if _, ok := secret.Data[clientcert.TLSKeyFile]; !ok {
				return false
			}
			if _, ok := secret.Data[clientcert.TLSCertFile]; !ok {
				return false
			}
			if _, ok := secret.Data[clientcert.KubeconfigFile]; !ok {
				return false
			}
			return true
		}, 90*time.Second, 1*time.Second).Should(gomega.BeTrue())

		ginkgo.By("Check addon status")
		gomega.Eventually(func() error {
			found, err := hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Get(context.TODO(), addOn.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

			if !meta.IsStatusConditionTrue(found.Status.Conditions, clientcert.ClusterCertificateRotatedCondition) {
				return fmt.Errorf("Client cert condition is not correct")
			}

			return nil
		}, 90*time.Second, 1*time.Second).Should(gomega.Succeed())

		ginkgo.By("Delete the addon and check if secret is gone")
		err = hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(clusterName).Delete(context.TODO(), addOnName, metav1.DeleteOptions{})
		gomega.Expect(err).NotTo(gomega.HaveOccurred())

		gomega.Eventually(func() bool {
			_, err = hubClient.CoreV1().Secrets(addOnName).Get(context.TODO(), secretName, metav1.GetOptions{})
			return errors.IsNotFound(err)
		}, 90*time.Second, 1*time.Second).Should(gomega.BeTrue())

		ginkgo.By(fmt.Sprintf("Cleaning managed cluster %q", clusterName))
		err = cleanupManagedCluster(clusterName, suffix)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("Cleaning managed cluster addon installation namespace %q", addOnName))
		err = hubClient.CoreV1().Namespaces().Delete(context.TODO(), addOnName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		ginkgo.By(fmt.Sprintf("Cleaning managed cluster spoken namespace %q", nsName))
		err = hubClient.CoreV1().Namespaces().Delete(context.TODO(), nsName, metav1.DeleteOptions{})
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	})
})

func assetToUnstructured(name string) (*unstructured.Unstructured, error) {
	yamlDecoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	raw, err := deploy.SpokeManifestFiles.ReadFile(name)
	if err != nil {
		return nil, err
	}

	reader := json.YAMLFramer.NewFrameReader(ioutil.NopCloser(bytes.NewReader(raw)))
	d := streaming.NewDecoder(reader, yamlDecoder)
	obj, _, err := d.Decode(nil, nil)
	if err != nil {
		return nil, err
	}

	switch t := obj.(type) {
	case *unstructured.Unstructured:
		return t, nil
	default:
		return nil, fmt.Errorf("failed to convert object, unexpected type %s", reflect.TypeOf(obj))
	}
}

func claimCrd() (*unstructured.Unstructured, error) {
	crd, err := assetToUnstructured("spoke/0000_02_clusters.open-cluster-management.io_clusterclaims.crd.yaml")
	if err != nil {
		return nil, err
	}
	return crd, nil
}

func spokeCR(suffix string) (*unstructured.Unstructured, error) {
	cr, err := assetToUnstructured("spoke/clusterrole.yaml")
	if err != nil {
		return nil, err
	}
	name := cr.GetName()
	name = fmt.Sprintf("%v-%v", name, suffix)
	cr.SetName(name)
	return cr, nil
}

func spokeCRB(nsName, suffix string) (*unstructured.Unstructured, error) {
	crb, err := assetToUnstructured("spoke/clusterrole_binding.yaml")
	if err != nil {
		return nil, err
	}

	name := crb.GetName()
	name = fmt.Sprintf("%v-%v", name, suffix)
	crb.SetName(name)

	roleRef, found, err := unstructured.NestedMap(crb.Object, "roleRef")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, goerrors.New("couldn't find CRB roleRef")
	}
	roleRef["name"] = name
	err = unstructured.SetNestedMap(crb.Object, roleRef, "roleRef")
	if err != nil {
		return nil, err
	}

	subjects, found, err := unstructured.NestedSlice(crb.Object, "subjects")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, goerrors.New("couldn't find CRB subjects")
	}

	err = unstructured.SetNestedField(subjects[0].(map[string]interface{}), nsName, "namespace")
	if err != nil {
		return nil, err
	}

	err = unstructured.SetNestedField(crb.Object, subjects, "subjects")
	if err != nil {
		return nil, err
	}

	return crb, nil
}

func spokeRole(nsName, suffix string) (*unstructured.Unstructured, error) {
	r, err := assetToUnstructured("spoke/role.yaml")
	if err != nil {
		return nil, err
	}
	name := r.GetName()
	name = fmt.Sprintf("%v-%v", name, suffix)
	r.SetName(name)
	r.SetNamespace(nsName)
	return r, nil
}

func spokeRoleBinding(nsName, suffix string) (*unstructured.Unstructured, error) {
	rb, err := assetToUnstructured("spoke/role_binding.yaml")
	if err != nil {
		return nil, err
	}

	name := rb.GetName()
	name = fmt.Sprintf("%v-%v", name, suffix)
	rb.SetName(name)
	rb.SetNamespace(nsName)

	roleRef, found, err := unstructured.NestedMap(rb.Object, "roleRef")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, goerrors.New("couldn't find RB roleRef")
	}
	roleRef["name"] = name
	err = unstructured.SetNestedMap(rb.Object, roleRef, "roleRef")
	if err != nil {
		return nil, err
	}

	subjects, found, err := unstructured.NestedSlice(rb.Object, "subjects")
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, goerrors.New("couldn't find RB subjects")
	}

	err = unstructured.SetNestedField(subjects[0].(map[string]interface{}), nsName, "namespace")
	if err != nil {
		return nil, err
	}

	err = unstructured.SetNestedField(rb.Object, subjects, "subjects")
	if err != nil {
		return nil, err
	}

	return rb, nil
}

func spokeDeployment(nsName, clusterName, image string) (*unstructured.Unstructured, error) {
	deployment, err := assetToUnstructured("spoke/deployment.yaml")
	if err != nil {
		return nil, err
	}
	err = unstructured.SetNestedField(deployment.Object, nsName, "meta", "namespace")
	if err != nil {
		return nil, err
	}

	containers, found, err := unstructured.NestedSlice(deployment.Object, "spec", "template", "spec", "containers")
	if err != nil || !found || containers == nil {
		return nil, fmt.Errorf("deployment containers not found or error in spec: %v", err)
	}

	if err := unstructured.SetNestedField(containers[0].(map[string]interface{}), image, "image"); err != nil {
		return nil, err
	}

	args, found, err := unstructured.NestedSlice(containers[0].(map[string]interface{}), "args")
	if err != nil || !found || args == nil {
		return nil, fmt.Errorf("container args not found or error in spec: %v", err)
	}

	clusterNameArg := fmt.Sprintf("--cluster-name=%v", clusterName)
	args[2] = clusterNameArg

	args = append(args, "--feature-gates=AddonManagement=true")
	if err := unstructured.SetNestedField(containers[0].(map[string]interface{}), args, "args"); err != nil {
		return nil, err
	}

	if err := unstructured.SetNestedField(deployment.Object, containers, "spec", "template", "spec", "containers"); err != nil {
		return nil, err
	}

	return deployment, nil
}
