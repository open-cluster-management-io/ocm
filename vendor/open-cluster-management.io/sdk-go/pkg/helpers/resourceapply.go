package helpers

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

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
