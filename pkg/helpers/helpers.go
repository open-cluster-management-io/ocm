package helpers

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"net/url"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/openshift/api"
	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehelper"
	errorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"

	certificatesv1 "k8s.io/api/certificates/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/util/retry"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	utilruntime.Must(api.InstallKube(genericScheme))
}

type UpdateManagedClusterStatusFunc func(status *clusterv1.ManagedClusterStatus) error

func UpdateManagedClusterStatus(
	ctx context.Context,
	client clusterclientset.Interface,
	spokeClusterName string,
	updateFuncs ...UpdateManagedClusterStatusFunc) (*clusterv1.ManagedClusterStatus, bool, error) {
	updated := false
	var updatedManagedClusterStatus *clusterv1.ManagedClusterStatus

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		managedCluster, err := client.ClusterV1().ManagedClusters().Get(ctx, spokeClusterName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		oldStatus := &managedCluster.Status

		newStatus := oldStatus.DeepCopy()
		for _, update := range updateFuncs {
			if err := update(newStatus); err != nil {
				return err
			}
		}
		if equality.Semantic.DeepEqual(oldStatus, newStatus) {
			// We return the newStatus which is a deep copy of oldStatus but with all update funcs applied.
			updatedManagedClusterStatus = newStatus
			return nil
		}

		oldData, err := json.Marshal(clusterv1.ManagedCluster{
			Status: *oldStatus,
		})

		if err != nil {
			return fmt.Errorf("failed to Marshal old data for cluster status %s: %w", managedCluster.Name, err)
		}

		newData, err := json.Marshal(clusterv1.ManagedCluster{
			ObjectMeta: metav1.ObjectMeta{
				UID:             managedCluster.UID,
				ResourceVersion: managedCluster.ResourceVersion,
			}, // to ensure they appear in the patch as preconditions
			Status: *newStatus,
		})
		if err != nil {
			return fmt.Errorf("failed to Marshal new data for cluster status %s: %w", managedCluster.Name, err)
		}

		patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
		if err != nil {
			return fmt.Errorf("failed to create patch for cluster %s: %w", managedCluster.Name, err)
		}

		updatedManagedCluster, err := client.ClusterV1().ManagedClusters().Patch(ctx, managedCluster.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")

		updatedManagedClusterStatus = &updatedManagedCluster.Status
		updated = err == nil
		return err
	})

	return updatedManagedClusterStatus, updated, err
}

func UpdateManagedClusterConditionFn(cond metav1.Condition) UpdateManagedClusterStatusFunc {
	return func(oldStatus *clusterv1.ManagedClusterStatus) error {
		meta.SetStatusCondition(&oldStatus.Conditions, cond)
		return nil
	}
}

type UpdateManagedClusterAddOnStatusFunc func(status *addonv1alpha1.ManagedClusterAddOnStatus) error

func UpdateManagedClusterAddOnStatus(
	ctx context.Context,
	client addonv1alpha1client.Interface,
	addOnNamespace, addOnName string,
	updateFuncs ...UpdateManagedClusterAddOnStatusFunc) (*addonv1alpha1.ManagedClusterAddOnStatus, bool, error) {
	updated := false
	var updatedAddOnStatus *addonv1alpha1.ManagedClusterAddOnStatus

	err := retry.RetryOnConflict(retry.DefaultBackoff, func() error {
		addOn, err := client.AddonV1alpha1().ManagedClusterAddOns(addOnNamespace).Get(ctx, addOnName, metav1.GetOptions{})
		if err != nil {
			return err
		}
		oldStatus := &addOn.Status

		newStatus := oldStatus.DeepCopy()
		for _, update := range updateFuncs {
			if err := update(newStatus); err != nil {
				return err
			}
		}
		if equality.Semantic.DeepEqual(oldStatus, newStatus) {
			// We return the newStatus which is a deep copy of oldStatus but with all update funcs applied.
			updatedAddOnStatus = newStatus
			return nil
		}

		oldData, err := json.Marshal(addonv1alpha1.ManagedClusterAddOn{
			Status: *oldStatus,
		})

		if err != nil {
			return fmt.Errorf("failed to Marshal old data for addon status %s: %w", addOn.Name, err)
		}

		newData, err := json.Marshal(addonv1alpha1.ManagedClusterAddOn{
			ObjectMeta: metav1.ObjectMeta{
				UID:             addOn.UID,
				ResourceVersion: addOn.ResourceVersion,
			}, // to ensure they appear in the patch as preconditions
			Status: *newStatus,
		})
		if err != nil {
			return fmt.Errorf("failed to Marshal new data for addon status %s: %w", addOn.Name, err)
		}

		patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
		if err != nil {
			return fmt.Errorf("failed to create patch for cluster %s: %w", addOn.Name, err)
		}

		updatedAddOn, err := client.AddonV1alpha1().ManagedClusterAddOns(addOnNamespace).Patch(ctx, addOn.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
		if err != nil {
			return err
		}
		updatedAddOnStatus = &updatedAddOn.Status
		updated = err == nil
		return err
	})

	return updatedAddOnStatus, updated, err
}

func UpdateManagedClusterAddOnStatusFn(cond metav1.Condition) UpdateManagedClusterAddOnStatusFunc {
	return func(oldStatus *addonv1alpha1.ManagedClusterAddOnStatus) error {
		meta.SetStatusCondition(&oldStatus.Conditions, cond)
		return nil
	}
}

// Check whether a CSR is in terminal state
func IsCSRInTerminalState(status *certificatesv1.CertificateSigningRequestStatus) bool {
	for _, c := range status.Conditions {
		if c.Type == certificatesv1.CertificateApproved {
			return true
		}
		if c.Type == certificatesv1.CertificateDenied {
			return true
		}
	}
	return false
}

// IsValidHTTPSURL validate whether a URL is https URL
func IsValidHTTPSURL(serverURL string) bool {
	if serverURL == "" {
		return false
	}

	parsedServerURL, err := url.Parse(serverURL)
	if err != nil {
		return false
	}

	if parsedServerURL.Scheme != "https" {
		return false
	}

	return true
}

// CleanUpManagedClusterManifests clean up managed cluster resources from its manifest files
func CleanUpManagedClusterManifests(
	ctx context.Context,
	client kubernetes.Interface,
	recorder events.Recorder,
	assetFunc resourceapply.AssetFunc,
	files ...string) error {
	errs := []error{}
	for _, file := range files {
		objectRaw, err := assetFunc(file)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		object, _, err := genericCodec.Decode(objectRaw, nil, nil)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		switch t := object.(type) {
		case *corev1.Namespace:
			err = client.CoreV1().Namespaces().Delete(ctx, t.Name, metav1.DeleteOptions{})
		case *rbacv1.Role:
			err = client.RbacV1().Roles(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
		case *rbacv1.RoleBinding:
			err = client.RbacV1().RoleBindings(t.Namespace).Delete(ctx, t.Name, metav1.DeleteOptions{})
		case *rbacv1.ClusterRole:
			err = client.RbacV1().ClusterRoles().Delete(ctx, t.Name, metav1.DeleteOptions{})
		case *rbacv1.ClusterRoleBinding:
			err = client.RbacV1().ClusterRoleBindings().Delete(ctx, t.Name, metav1.DeleteOptions{})
		default:
			err = fmt.Errorf("unhandled type %T", object)
		}
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		gvk := resourcehelper.GuessObjectGroupVersionKind(object)
		recorder.Eventf(fmt.Sprintf("ManagedCluster%sDeleted", gvk.Kind), "Deleted %s", resourcehelper.FormatResourceForCLIWithNamespace(object))
	}
	return errorhelpers.NewMultiLineAggregate(errs)
}

func ManagedClusterAssetFn(fs embed.FS, managedClusterName string) resourceapply.AssetFunc {
	return func(name string) ([]byte, error) {
		config := struct {
			ManagedClusterName string
		}{
			ManagedClusterName: managedClusterName,
		}

		template, err := fs.ReadFile(name)
		if err != nil {
			return nil, err
		}
		return assets.MustCreateAssetFromTemplate(name, template, config).Data, nil
	}
}

// FindTaintByKey returns a taint if the managed cluster has a taint with the given key.
func FindTaintByKey(managedCluster *clusterv1.ManagedCluster, key string) *clusterv1.Taint {
	if managedCluster == nil {
		return nil
	}
	for _, taint := range managedCluster.Spec.Taints {
		if key != taint.Key {
			continue
		}
		return &taint
	}
	return nil
}

func IsTaintEqual(taint1, taint2 clusterv1.Taint) bool {
	// Ignore the comparison of time
	return taint1.Key == taint2.Key && taint1.Value == taint2.Value && taint1.Effect == taint2.Effect
}

// AddTaints add taints to the specified slice, if it did not already exist.
// Return a boolean indicating whether the slice has been updated.
func AddTaints(taints *[]clusterv1.Taint, taint clusterv1.Taint) bool {
	if taints == nil || *taints == nil {
		*taints = make([]clusterv1.Taint, 0)
	}
	if FindTaint(*taints, taint) != nil {
		return false
	}
	*taints = append(*taints, taint)
	return true
}

func RemoveTaints(taints *[]clusterv1.Taint, targets ...clusterv1.Taint) (updated bool) {
	if taints == nil || len(*taints) == 0 || len(targets) == 0 {
		return false
	}

	newTaints := make([]clusterv1.Taint, 0)
	for _, v := range *taints {
		if FindTaint(targets, v) == nil {
			newTaints = append(newTaints, v)
		}
	}
	updated = len(*taints) != len(newTaints)
	*taints = newTaints
	return updated
}

func FindTaint(taints []clusterv1.Taint, taint clusterv1.Taint) *clusterv1.Taint {
	for i := range taints {
		if IsTaintEqual(taints[i], taint) {
			return &taints[i]
		}
	}

	return nil
}

// IsCSRSupported checks whether the cluster supports v1 or v1beta1 csr api.
func IsCSRSupported(nativeClient kubernetes.Interface) (bool, bool, error) {
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(nativeClient.Discovery()))
	mappings, err := mapper.RESTMappings(schema.GroupKind{
		Group: certificatesv1.GroupName,
		Kind:  "CertificateSigningRequest",
	})
	if err != nil {
		return false, false, err
	}
	v1CSRSupported := false
	for _, mapping := range mappings {
		if mapping.GroupVersionKind.Version == "v1" {
			v1CSRSupported = true
		}
	}
	v1beta1CSRSupported := false
	for _, mapping := range mappings {
		if mapping.GroupVersionKind.Version == "v1beta1" {
			v1beta1CSRSupported = true
		}
	}
	return v1CSRSupported, v1beta1CSRSupported, nil
}
