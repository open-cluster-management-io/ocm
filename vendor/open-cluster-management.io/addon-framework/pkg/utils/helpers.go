package utils

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	jsonpatch "github.com/evanphx/json-patch"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterv1 "open-cluster-management.io/api/cluster/v1"

	"open-cluster-management.io/addon-framework/pkg/agent"
)

func MergeRelatedObjects(modified *bool, objs *[]addonapiv1alpha1.ObjectReference, obj addonapiv1alpha1.ObjectReference) {
	if *objs == nil {
		*objs = []addonapiv1alpha1.ObjectReference{}
	}

	for _, o := range *objs {
		if o.Group == obj.Group && o.Resource == obj.Resource && o.Name == obj.Name && o.Namespace == obj.Namespace {
			return
		}
	}

	*objs = append(*objs, obj)
	*modified = true
}

// ApplyConfigMap merges objectmeta, requires data, ref from openshift/library-go
func ApplyConfigMap(ctx context.Context, client coreclientv1.ConfigMapsGetter, required *corev1.ConfigMap) (*corev1.ConfigMap, bool, error) {
	existing, err := client.ConfigMaps(required.Namespace).Get(ctx, required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.ConfigMaps(requiredCopy.Namespace).
			Create(ctx, requiredCopy, metav1.CreateOptions{})
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	existingCopy := existing.DeepCopy()

	var modifiedKeys []string
	for existingCopyKey, existingCopyValue := range existingCopy.Data {
		// if we're injecting a ca-bundle and the required isn't forcing the value, then don't use the value of existing
		// to drive a diff detection. If required has set the value then we need to force the value in order to have apply
		// behave predictably.
		if requiredValue, ok := required.Data[existingCopyKey]; !ok || (existingCopyValue != requiredValue) {
			modifiedKeys = append(modifiedKeys, "data."+existingCopyKey)
		}
	}
	for existingCopyKey, existingCopyBinValue := range existingCopy.BinaryData {
		if requiredBinValue, ok := required.BinaryData[existingCopyKey]; !ok || !bytes.Equal(existingCopyBinValue, requiredBinValue) {
			modifiedKeys = append(modifiedKeys, "binaryData."+existingCopyKey)
		}
	}
	for requiredKey := range required.Data {
		if _, ok := existingCopy.Data[requiredKey]; !ok {
			modifiedKeys = append(modifiedKeys, "data."+requiredKey)
		}
	}
	for requiredBinKey := range required.BinaryData {
		if _, ok := existingCopy.BinaryData[requiredBinKey]; !ok {
			modifiedKeys = append(modifiedKeys, "binaryData."+requiredBinKey)
		}
	}

	dataSame := len(modifiedKeys) == 0
	if dataSame {
		return existingCopy, false, nil
	}
	existingCopy.Data = required.Data
	existingCopy.BinaryData = required.BinaryData

	actual, err := client.ConfigMaps(required.Namespace).Update(ctx, existingCopy, metav1.UpdateOptions{})

	return actual, true, err
}

// ApplySecret merges objectmeta, requires data. ref from openshift/library-go
func ApplySecret(ctx context.Context, client coreclientv1.SecretsGetter, requiredInput *corev1.Secret) (*corev1.Secret, bool, error) {
	// copy the stringData to data.  Error on a data content conflict inside required.  This is usually a bug.

	existing, err := client.Secrets(requiredInput.Namespace).Get(ctx, requiredInput.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, false, err
	}

	required := requiredInput.DeepCopy()
	if required.Data == nil {
		required.Data = map[string][]byte{}
	}
	for k, v := range required.StringData {
		if dataV, ok := required.Data[k]; ok {
			if string(dataV) != v {
				return nil, false, fmt.Errorf("Secret.stringData[%q] conflicts with Secret.data[%q]", k, k)
			}
		}
		required.Data[k] = []byte(v)
	}
	required.StringData = nil

	if apierrors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.Secrets(requiredCopy.Namespace).
			Create(ctx, requiredCopy, metav1.CreateOptions{})
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	existingCopy := existing.DeepCopy()

	switch required.Type {
	case corev1.SecretTypeServiceAccountToken:
		// Secrets for ServiceAccountTokens will have data injected by kube controller manager.
		// We will apply only the explicitly set keys.
		if existingCopy.Data == nil {
			existingCopy.Data = map[string][]byte{}
		}

		for k, v := range required.Data {
			existingCopy.Data[k] = v
		}

	default:
		existingCopy.Data = required.Data
	}

	existingCopy.Type = required.Type

	// Server defaults some values and we need to do it as well or it will never equal.
	if existingCopy.Type == "" {
		existingCopy.Type = corev1.SecretTypeOpaque
	}

	if equality.Semantic.DeepEqual(existingCopy, existing) {
		return existing, false, nil
	}

	var actual *corev1.Secret
	/*
	 * Kubernetes validation silently hides failures to update secret type.
	 * https://github.com/kubernetes/kubernetes/blob/98e65951dccfd40d3b4f31949c2ab8df5912d93e/pkg/apis/core/validation/validation.go#L5048
	 * We need to explicitly opt for delete+create in that case.
	 */
	if existingCopy.Type == existing.Type {
		actual, err = client.Secrets(required.Namespace).Update(ctx, existingCopy, metav1.UpdateOptions{})

		if err == nil {
			return actual, true, nil
		}
		if !strings.Contains(err.Error(), "field is immutable") {
			return actual, true, err
		}
	}

	// if the field was immutable on a secret, we're going to be stuck until we delete it.  Try to delete and then create
	deleteErr := client.Secrets(required.Namespace).Delete(ctx, existingCopy.Name, metav1.DeleteOptions{})
	if deleteErr != nil {
		return actual, false, deleteErr
	}

	// clear the RV and track the original actual and error for the return like our create value.
	existingCopy.ResourceVersion = ""
	actual, err = client.Secrets(required.Namespace).Create(ctx, existingCopy, metav1.CreateOptions{})
	return actual, true, err
}

func MergeOwnerRefs(existing *[]metav1.OwnerReference, required metav1.OwnerReference, removeOwner bool) bool {
	if *existing == nil {
		*existing = []metav1.OwnerReference{}
	}

	existedIndex := 0

	for existedIndex < len(*existing) {
		if ownerRefMatched(required, (*existing)[existedIndex]) {
			break
		}
		existedIndex++
	}

	if existedIndex == len(*existing) {
		// There is no matched ownerref found, append the ownerref
		// if it is not to be removed.
		if !removeOwner {
			*existing = append(*existing, required)
			return true
		}

		return false
	}

	if removeOwner {
		*existing = append((*existing)[:existedIndex], (*existing)[existedIndex+1:]...)
		return true
	}

	if !reflect.DeepEqual(required, (*existing)[existedIndex]) {
		(*existing)[existedIndex] = required
		return true
	}

	return false
}

func ownerRefMatched(existing, required metav1.OwnerReference) bool {
	if existing.Name != required.Name {
		return false
	}

	if existing.Kind != required.Kind {
		return false
	}

	existingGV, err := schema.ParseGroupVersion(existing.APIVersion)

	if err != nil {
		return false
	}

	requiredGV, err := schema.ParseGroupVersion(required.APIVersion)

	if err != nil {
		return false
	}

	if existingGV.Group != requiredGV.Group {
		return false
	}

	return true
}

func PatchAddonCondition(ctx context.Context, addonClient addonv1alpha1client.Interface, new, old *addonapiv1alpha1.ManagedClusterAddOn) error {
	if equality.Semantic.DeepEqual(new.Status.Conditions, old.Status.Conditions) {
		return nil
	}

	oldData, err := json.Marshal(&addonapiv1alpha1.ManagedClusterAddOn{
		Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
			Conditions: old.Status.Conditions,
		},
	})
	if err != nil {
		return err
	}

	newData, err := json.Marshal(&addonapiv1alpha1.ManagedClusterAddOn{
		ObjectMeta: metav1.ObjectMeta{
			UID:             new.UID,
			ResourceVersion: new.ResourceVersion,
		},
		Status: addonapiv1alpha1.ManagedClusterAddOnStatus{
			Conditions: new.Status.Conditions,
		},
	})
	if err != nil {
		return err
	}

	patchBytes, err := jsonpatch.CreateMergePatch(oldData, newData)
	if err != nil {
		return fmt.Errorf("failed to create patch for addon %s: %w", new.Name, err)
	}

	klog.V(2).Infof("Patching addon %s/%s condition with %s", new.Namespace, new.Name, string(patchBytes))
	_, err = addonClient.AddonV1alpha1().ManagedClusterAddOns(new.Namespace).Patch(
		ctx, new.Name, types.MergePatchType, patchBytes, metav1.PatchOptions{}, "status")
	return err
}

// AddonManagementFilterFunc is to check if the addon should be managed by addon manager or self-managed
type AddonManagementFilterFunc func(cma *addonapiv1alpha1.ClusterManagementAddOn) bool

func ManagedByAddonManager(obj interface{}) bool {
	accessor, _ := meta.Accessor(obj)
	annotations := accessor.GetAnnotations()
	if len(annotations) == 0 {
		return false
	}

	value, ok := annotations[addonapiv1alpha1.AddonLifecycleAnnotationKey]
	if !ok {
		return false
	}

	return value == addonapiv1alpha1.AddonLifecycleAddonManagerAnnotationValue
}

func ManagedBySelf(agentAddons map[string]agent.AgentAddon) func(obj interface{}) bool {
	return func(obj interface{}) bool {
		accessor, _ := meta.Accessor(obj)
		if _, ok := agentAddons[accessor.GetName()]; !ok {
			return false
		}

		annotations := accessor.GetAnnotations()

		if len(annotations) == 0 {
			return true
		}

		value, ok := annotations[addonapiv1alpha1.AddonLifecycleAnnotationKey]
		if !ok {
			return true
		}

		return value == addonapiv1alpha1.AddonLifecycleSelfManageAnnotationValue
	}
}

func FilterByAddonName(agentAddons map[string]agent.AgentAddon) func(obj interface{}) bool {
	return func(obj interface{}) bool {
		accessor, _ := meta.Accessor(obj)
		_, ok := agentAddons[accessor.GetName()]
		return ok
	}
}

func IsOwnedByCMA(addon *addonapiv1alpha1.ManagedClusterAddOn) bool {
	for _, owner := range addon.OwnerReferences {
		if owner.Kind != "ClusterManagementAddOn" {
			continue
		}
		if owner.Name != addon.Name {
			continue
		}
		return true
	}
	return false
}

// GetSpecHash returns the sha256 hash of the spec field or other config fields of the given object
func GetSpecHash(obj *unstructured.Unstructured) (string, error) {
	if obj == nil {
		return "", fmt.Errorf("object is nil")
	}

	// create a map for config fields
	configObj := map[string]interface{}{}
	for k, v := range obj.Object {
		switch k {
		case "apiVersion", "kind", "metadata", "status":
			// skip these non config related fields
		default:
			configObj[k] = v
		}
	}

	// the object key will also be included in the hash calculation to ensure that different fields with the same value have different hashes
	configBytes, err := json.Marshal(configObj)
	if err != nil {
		return "", err
	}

	hash := sha256.Sum256(configBytes)

	return fmt.Sprintf("%x", hash), nil
}

// MapValueChanged returns true if the value of the given key in the new map is different from the old map
func MapValueChanged(old, new map[string]string, key string) bool {
	oval, ok := old[key]
	nval, nk := new[key]
	if !ok && !nk {
		return false
	}
	if ok && nk {
		return oval != nval
	}
	return true
}

// ClusterImageRegistriesAnnotationChanged returns true if the value of the ClusterImageRegistriesAnnotationKey
// in the new managed cluster annotation is different from the old managed cluster annotation
func ClusterImageRegistriesAnnotationChanged(old, new *clusterv1.ManagedCluster) bool {
	return ClusterAnnotationChanged(old, new, clusterv1.ClusterImageRegistriesAnnotationKey)
}

// ClusterAnnotationChanged returns true if the value of the specified annotation in the new managed cluster annotation
// is different from the old managed cluster annotation
func ClusterAnnotationChanged(old, new *clusterv1.ManagedCluster, annotation string) bool {
	if new == nil || old == nil {
		return false
	}
	return MapValueChanged(old.Annotations, new.Annotations, annotation)
}

// ClusterAvailableConditionChanged returns true if the value of the Available condition in the new managed cluster
// is different from the old managed cluster
func ClusterAvailableConditionChanged(old, new *clusterv1.ManagedCluster) bool {
	return ClusterConditionChanged(old, new, clusterv1.ManagedClusterConditionAvailable)
}

// ClusterAvailableConditionChanged returns true if the value of the specified conditionType in the new managed cluster
// is different from the old managed cluster
func ClusterConditionChanged(old, new *clusterv1.ManagedCluster, conditionType string) bool {
	if new == nil || old == nil {
		return false
	}

	oldAvailableCondition := meta.FindStatusCondition(old.Status.Conditions, conditionType)
	newAvailableCondition := meta.FindStatusCondition(new.Status.Conditions, conditionType)

	return (oldAvailableCondition == nil && newAvailableCondition != nil) ||
		(oldAvailableCondition != nil && newAvailableCondition == nil) ||
		(oldAvailableCondition != nil && newAvailableCondition != nil &&
			oldAvailableCondition.Status != newAvailableCondition.Status)
}
