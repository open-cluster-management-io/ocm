package e2e

import (
	"bytes"
	"context"
	goerrors "errors"
	"fmt"
	"io"
	"reflect"
	"time"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/registration/deploy"
	"open-cluster-management.io/registration/pkg/helpers"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"k8s.io/apimachinery/pkg/runtime/serializer/streaming"
	"k8s.io/apimachinery/pkg/runtime/serializer/yaml"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterclient "open-cluster-management.io/api/client/cluster/clientset/versioned"
)

type utilClients struct {
	hubClient         kubernetes.Interface
	hubDynamicClient  dynamic.Interface
	hubAddOnClient    addonclient.Interface
	clusterClient     clusterclient.Interface
	registrationImage string
}

func (u *utilClients) createManagedCluster(clusterName, suffix string) (*clusterv1.ManagedCluster, error) {
	nsName := fmt.Sprintf("loopback-spoke-%v", suffix)
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: nsName,
		},
	}

	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		ns, err = u.hubClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return nil, err
	}

	// This test expects a bootstrap secret to exist in open-cluster-management-agent/e2e-bootstrap-secret
	e2eBootstrapSecret, err := u.hubClient.CoreV1().Secrets("open-cluster-management-agent").Get(context.TODO(), "e2e-bootstrap-secret", metav1.GetOptions{})
	if err != nil {
		return nil, err
	}
	bootstrapSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: nsName,
			Name:      "bootstrap-secret",
		},
	}
	bootstrapSecret.Data = e2eBootstrapSecret.Data
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		_, err = u.hubClient.CoreV1().Secrets(nsName).Create(context.TODO(), bootstrapSecret, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return nil, err
	}

	err = u.createSpokeCR("spoke/clusterrole.yaml", suffix)
	if err != nil {
		return nil, err
	}

	err = u.createSpokeCRB("spoke/clusterrole_binding.yaml", nsName, suffix)
	if err != nil {
		return nil, err
	}

	err = u.createSpokeCR("spoke/clusterrole_addon-management.yaml", suffix)
	if err != nil {
		return nil, err
	}

	err = u.createSpokeCRB("spoke/clusterrole_binding_addon-management.yaml", nsName, suffix)
	if err != nil {
		return nil, err
	}

	err = u.createSpokeRole("spoke/role.yaml", nsName, suffix)
	if err != nil {
		return nil, err
	}

	err = u.createSpokeRoleBinding("spoke/role_binding.yaml", nsName, suffix)
	if err != nil {
		return nil, err
	}

	err = u.createSpokeRole("spoke/role_extension-apiserver.yaml", nsName, suffix)
	if err != nil {
		return nil, err
	}

	err = u.createSpokeRoleBinding("spoke/role_binding_extension-apiserver.yaml", nsName, suffix)
	if err != nil {
		return nil, err
	}

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: nsName,
			Name:      "spoke-agent-sa",
		},
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		_, err = u.hubClient.CoreV1().ServiceAccounts(nsName).Create(context.TODO(), sa, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return nil, err
	}

	var (
		deployment         *unstructured.Unstructured
		deploymentResource = schema.GroupVersionResource{
			Group:    "apps",
			Version:  "v1",
			Resource: "deployments",
		}
	)
	deployment, err = spokeDeploymentWithAddonManagement(nsName, clusterName, u.registrationImage)
	if err != nil {
		return nil, err
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		_, err = u.hubDynamicClient.Resource(deploymentResource).Namespace(nsName).Create(context.TODO(), deployment, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return nil, err
	}

	var (
		csrs      *certificatesv1.CertificateSigningRequestList
		csrClient = u.hubClient.CertificatesV1().CertificateSigningRequests()
	)

	if err := wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
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
	}); err != nil {
		return nil, err
	}

	var csr *certificatesv1.CertificateSigningRequest
	for i := range csrs.Items {
		csr = &csrs.Items[i]

		if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
			csr, err = csrClient.Get(context.TODO(), csr.Name, metav1.GetOptions{})
			if err != nil {
				return err
			}

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
		}); err != nil {
			return nil, err
		}
	}

	var (
		managedCluster  *clusterv1.ManagedCluster
		managedClusters = u.clusterClient.ClusterV1().ManagedClusters()
	)

	if err := wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
		var err error
		managedCluster, err = managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		return true, nil
	}); err != nil {
		return nil, err
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		var err error
		managedCluster, err = managedClusters.Get(context.TODO(), managedCluster.Name, metav1.GetOptions{})
		if err != nil {
			return err
		}
		managedCluster.Spec.HubAcceptsClient = true
		managedCluster.Spec.LeaseDurationSeconds = 5
		managedCluster, err = managedClusters.Update(context.TODO(), managedCluster, metav1.UpdateOptions{})
		return err
	}); err != nil {
		return nil, err
	}

	if err := wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
		var err error
		managedCluster, err := managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		if meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionHubAccepted) {
			return true, nil
		}

		return false, nil
	}); err != nil {
		return nil, err
	}

	if err := wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
		var err error
		managedCluster, err := managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		return meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionJoined), nil
	}); err != nil {
		return nil, err
	}

	err = wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
		managedCluster, err := managedClusters.Get(context.TODO(), clusterName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}

		return meta.IsStatusConditionTrue(managedCluster.Status.Conditions, clusterv1.ManagedClusterConditionAvailable), nil
	})
	return managedCluster, err
}

func (u *utilClients) createManagedClusterAddOn(managedCluster *clusterv1.ManagedCluster, addOnName string) (*addonv1alpha1.ManagedClusterAddOn, error) {
	addOn := &addonv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: managedCluster.Name,
			Name:      addOnName,
		},
		Spec: addonv1alpha1.ManagedClusterAddOnSpec{
			InstallNamespace: addOnName,
		},
	}
	return u.hubAddOnClient.AddonV1alpha1().ManagedClusterAddOns(managedCluster.Name).Create(context.TODO(), addOn, metav1.CreateOptions{})
}

// cleanupManagedCluster deletes the managed cluster related resources, including the namespace
func (u *utilClients) cleanupManagedCluster(clusterName, suffix string) error {
	nsName := fmt.Sprintf("loopback-spoke-%v", suffix)

	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		err := u.hubClient.CoreV1().Namespaces().Delete(context.TODO(), nsName, metav1.DeleteOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("delete ns %q failed: %v", nsName, err)
	}

	if err := u.deleteManageClusterAndRelatedNamespace(clusterName); err != nil {
		return fmt.Errorf("delete manage cluster and related namespace for cluster %q failed: %v", clusterName, err)
	}

	err := u.deleteSpokeCR("spoke/clusterrole.yaml", suffix)
	if err != nil {
		return err
	}

	err = u.deleteSpokeCRB("spoke/clusterrole_binding.yaml", nsName, suffix)
	if err != nil {
		return err
	}

	err = u.deleteSpokeCR("spoke/clusterrole_addon-management.yaml", suffix)
	if err != nil {
		return err
	}

	err = u.deleteSpokeCRB("spoke/clusterrole_binding_addon-management.yaml", nsName, suffix)
	if err != nil {
		return err
	}

	return nil
}

func (u *utilClients) deleteManageClusterAndRelatedNamespace(clusterName string) error {
	if err := wait.Poll(1*time.Second, 90*time.Second, func() (bool, error) {
		err := u.clusterClient.ClusterV1().ManagedClusters().Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
		if err != nil && !errors.IsNotFound(err) {
			return false, err
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("delete managed cluster %q failed: %v", clusterName, err)
	}

	// delete namespace created by hub automaticly
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		err := u.hubClient.CoreV1().Namespaces().Delete(context.TODO(), clusterName, metav1.DeleteOptions{})
		// some managed cluster just created, but the csr is not approved,
		// so there is not a related namespace
		if err != nil && !errors.IsNotFound(err) {
			return false, err
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("delete related namespace %q failed: %v", clusterName, err)
	}

	return nil
}

func spokeDeploymentWithAddonManagement(nsName, clusterName, image string) (*unstructured.Unstructured, error) {
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

func (u *utilClients) createSpokeCR(file, suffix string) error {
	var (
		cr         *unstructured.Unstructured
		crResource = schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "clusterroles",
		}
	)
	cr, err := spokeCR(file, suffix)
	if err != nil {
		return err
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		_, err = u.hubDynamicClient.Resource(crResource).Create(context.TODO(), cr, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return err
	}

	return nil
}

func (u *utilClients) deleteSpokeCR(file, suffix string) error {
	var (
		err        error
		cr         *unstructured.Unstructured
		crResource = schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "clusterroles",
		}
	)
	cr, err = spokeCR(file, suffix)
	if err != nil {
		return err
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		err := u.hubDynamicClient.Resource(crResource).Delete(context.TODO(), cr.GetName(), metav1.DeleteOptions{})
		if err != nil {
			return false, err
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("delete cr %q failed: %v", cr.GetName(), err)
	}

	return nil
}

func (u *utilClients) createSpokeCRB(file, nsName, suffix string) error {
	var (
		crb         *unstructured.Unstructured
		crbResource = schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "clusterrolebindings",
		}
	)
	crb, err := spokeCRB(file, nsName, suffix)
	if err != nil {
		return err
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		_, err = u.hubDynamicClient.Resource(crbResource).Create(context.TODO(), crb, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return err
	}

	return nil
}

func (u *utilClients) deleteSpokeCRB(file, nsName, suffix string) error {
	var (
		crb         *unstructured.Unstructured
		crbResource = schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "clusterrolebindings",
		}
	)
	crb, err := spokeCRB(file, nsName, suffix)
	if err != nil {
		return err
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		err := u.hubDynamicClient.Resource(crbResource).Delete(context.TODO(), crb.GetName(), metav1.DeleteOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return fmt.Errorf("delete crb %q failed: %v", crb.GetName(), err)
	}

	return nil
}

func (u *utilClients) createSpokeRole(file, nsName, suffix string) error {
	var (
		role         *unstructured.Unstructured
		roleResource = schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "roles",
		}
	)
	role, err := spokeRole(file, nsName, suffix)
	if err != nil {
		return err
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		_, err = u.hubDynamicClient.Resource(roleResource).Namespace(role.GetNamespace()).Create(context.TODO(), role, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return err
	}
	return nil
}

func (u *utilClients) createSpokeRoleBinding(file, nsName, suffix string) error {
	var (
		roleBinding         *unstructured.Unstructured
		roleBindingResource = schema.GroupVersionResource{
			Group:    "rbac.authorization.k8s.io",
			Version:  "v1",
			Resource: "rolebindings",
		}
	)
	roleBinding, err := spokeRoleBinding(file, nsName, suffix)
	if err != nil {
		return err
	}
	if err := wait.Poll(1*time.Second, 5*time.Second, func() (bool, error) {
		var err error
		_, err = u.hubDynamicClient.Resource(roleBindingResource).Namespace(roleBinding.GetNamespace()).Create(context.TODO(), roleBinding, metav1.CreateOptions{})
		if err != nil {
			return false, err
		}

		return true, nil
	}); err != nil {
		return err
	}
	return nil
}

func assetToUnstructured(name string) (*unstructured.Unstructured, error) {
	yamlDecoder := yaml.NewDecodingSerializer(unstructured.UnstructuredJSONScheme)
	raw, err := deploy.SpokeManifestFiles.ReadFile(name)
	if err != nil {
		return nil, err
	}

	reader := json.YAMLFramer.NewFrameReader(io.NopCloser(bytes.NewReader(raw)))
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

func spokeCR(file, suffix string) (*unstructured.Unstructured, error) {
	cr, err := assetToUnstructured(file)
	if err != nil {
		return nil, err
	}
	name := cr.GetName()
	name = fmt.Sprintf("%v-%v", name, suffix)
	cr.SetName(name)
	return cr, nil
}

func spokeCRB(file, nsName, suffix string) (*unstructured.Unstructured, error) {
	crb, err := assetToUnstructured(file)
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

func spokeRole(file, nsName, suffix string) (*unstructured.Unstructured, error) {
	r, err := assetToUnstructured(file)
	if err != nil {
		return nil, err
	}
	name := r.GetName()
	name = fmt.Sprintf("%v-%v", name, suffix)
	r.SetName(name)
	if r.GetNamespace() != "kube-system" {
		r.SetNamespace(nsName)
	}
	return r, nil
}

func spokeRoleBinding(file, nsName, suffix string) (*unstructured.Unstructured, error) {
	rb, err := assetToUnstructured(file)
	if err != nil {
		return nil, err
	}

	name := rb.GetName()
	name = fmt.Sprintf("%v-%v", name, suffix)
	rb.SetName(name)
	if rb.GetNamespace() != "kube-system" {
		rb.SetNamespace(nsName)
	}

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
