// Copyright Contributors to the Open Cluster Management project

package conversion

import (
	certificates "k8s.io/api/certificates/v1"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
)

const (
	// InstallNamespaceAnnotation is the annotation key used to preserve v1alpha1 installNamespace
	// when converting to v1beta1 (where the field is removed)
	InstallNamespaceAnnotation = "addon.open-cluster-management.io/v1alpha1-install-namespace"
)

// ConvertRegistrationsToV1Beta1 converts v1alpha1 registrations to v1beta1
// If signerName is KubeAPIServerClientSignerName, convert to KubeClient type, otherwise CSR type
func ConvertRegistrationsToV1Beta1(registrations []addonv1alpha1.RegistrationConfig) []addonv1beta1.RegistrationConfig {
	var converted []addonv1beta1.RegistrationConfig

	for _, reg := range registrations {
		if reg.SignerName == certificates.KubeAPIServerClientSignerName {
			// Convert to KubeClient type
			converted = append(converted, addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.KubeClient,
				KubeClient: &addonv1beta1.KubeClientConfig{
					Subject: addonv1beta1.KubeClientSubject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   reg.Subject.User,
							Groups: reg.Subject.Groups,
						},
					},
				},
			})
		} else {
			// Convert to CSR type
			converted = append(converted, addonv1beta1.RegistrationConfig{
				Type: addonv1beta1.CSR,
				CSR: &addonv1beta1.CSRConfig{
					SignerName: reg.SignerName,
					Subject: addonv1beta1.Subject{
						BaseSubject: addonv1beta1.BaseSubject{
							User:   reg.Subject.User,
							Groups: reg.Subject.Groups,
						},
						OrganizationUnits: reg.Subject.OrganizationUnits,
					},
				},
			})
		}
	}

	return converted
}

// ConvertRegistrationsFromV1Beta1 converts v1beta1 registrations back to v1alpha1
func ConvertRegistrationsFromV1Beta1(registrations []addonv1beta1.RegistrationConfig) []addonv1alpha1.RegistrationConfig {
	var converted []addonv1alpha1.RegistrationConfig

	for _, reg := range registrations {
		switch reg.Type {
		case addonv1beta1.CSR:
			if reg.CSR != nil {
				converted = append(converted, addonv1alpha1.RegistrationConfig{
					SignerName: reg.CSR.SignerName,
					Subject: addonv1alpha1.Subject{
						User:              reg.CSR.Subject.User,
						Groups:            reg.CSR.Subject.Groups,
						OrganizationUnits: reg.CSR.Subject.OrganizationUnits,
					},
				})
			}
		case addonv1beta1.KubeClient:
			// Map KubeClient to default CSR config
			if reg.KubeClient != nil {
				converted = append(converted, addonv1alpha1.RegistrationConfig{
					SignerName: certificates.KubeAPIServerClientSignerName,
					Subject: addonv1alpha1.Subject{
						User:   reg.KubeClient.Subject.User,
						Groups: reg.KubeClient.Subject.Groups,
					},
				})
			}
		}
	}

	return converted
}

// ConvertConfigReferencesToV1Beta1 converts config references from v1alpha1 to v1beta1
func ConvertConfigReferencesToV1Beta1(refs []addonv1alpha1.ConfigReference) []addonv1beta1.ConfigReference {
	var converted []addonv1beta1.ConfigReference

	for _, ref := range refs {
		v1beta1Ref := addonv1beta1.ConfigReference{
			ConfigGroupResource: addonv1beta1.ConfigGroupResource{
				Group:    ref.Group,
				Resource: ref.Resource,
			},
			DesiredConfig:          ConvertConfigSpecHashToV1Beta1(ref.DesiredConfig),
			LastAppliedConfig:      ConvertConfigSpecHashToV1Beta1(ref.LastAppliedConfig),
			LastObservedGeneration: ref.LastObservedGeneration,
		}

		// Handle deprecated ConfigReferent field: if DesiredConfig is nil but ConfigReferent has values,
		// create DesiredConfig from ConfigReferent for backward compatibility.
		// In normal controller code paths (pkg/addon/controllers), ConfigReferent and DesiredConfig are always set together.
		// This backward compatibility ensures no data loss during API version conversion.
		if v1beta1Ref.DesiredConfig == nil && (ref.ConfigReferent.Name != "" || ref.ConfigReferent.Namespace != "") {
			v1beta1Ref.DesiredConfig = &addonv1beta1.ConfigSpecHash{
				ConfigReferent: addonv1beta1.ConfigReferent{
					Namespace: ref.ConfigReferent.Namespace,
					Name:      ref.ConfigReferent.Name,
				},
			}
		}

		converted = append(converted, v1beta1Ref)
	}

	return converted
}

// ConvertConfigReferencesFromV1Beta1 converts config references from v1beta1 to v1alpha1
func ConvertConfigReferencesFromV1Beta1(refs []addonv1beta1.ConfigReference) []addonv1alpha1.ConfigReference {
	var converted []addonv1alpha1.ConfigReference

	for _, ref := range refs {
		v1alpha1Ref := addonv1alpha1.ConfigReference{
			ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
				Group:    ref.Group,
				Resource: ref.Resource,
			},
			DesiredConfig:          ConvertConfigSpecHashFromV1Beta1(ref.DesiredConfig),
			LastAppliedConfig:      ConvertConfigSpecHashFromV1Beta1(ref.LastAppliedConfig),
			LastObservedGeneration: ref.LastObservedGeneration,
		}
		// Extract namespace and name from DesiredConfig if available
		if ref.DesiredConfig != nil {
			v1alpha1Ref.ConfigReferent = addonv1alpha1.ConfigReferent{
				Namespace: ref.DesiredConfig.Namespace,
				Name:      ref.DesiredConfig.Name,
			}
		}
		converted = append(converted, v1alpha1Ref)
	}

	return converted
}

// ConvertRelatedObjectsToV1Beta1 converts related objects from v1alpha1 to v1beta1
func ConvertRelatedObjectsToV1Beta1(objects []addonv1alpha1.ObjectReference) []addonv1beta1.ObjectReference {
	if objects == nil {
		return nil
	}
	result := make([]addonv1beta1.ObjectReference, len(objects))
	for i := range objects {
		result[i] = addonv1beta1.ObjectReference(objects[i])
	}
	return result
}

// ConvertRelatedObjectsFromV1Beta1 converts related objects from v1beta1 to v1alpha1
func ConvertRelatedObjectsFromV1Beta1(objects []addonv1beta1.ObjectReference) []addonv1alpha1.ObjectReference {
	if objects == nil {
		return nil
	}
	result := make([]addonv1alpha1.ObjectReference, len(objects))
	for i := range objects {
		result[i] = addonv1alpha1.ObjectReference(objects[i])
	}
	return result
}

// ConvertSupportedConfigsToV1Beta1 converts supported configs from v1alpha1 to v1beta1
func ConvertSupportedConfigsToV1Beta1(configs []addonv1alpha1.ConfigGroupResource) []addonv1beta1.ConfigGroupResource {
	if configs == nil {
		return nil
	}
	result := make([]addonv1beta1.ConfigGroupResource, len(configs))
	for i := range configs {
		result[i] = addonv1beta1.ConfigGroupResource(configs[i])
	}
	return result
}

// ConvertSupportedConfigsFromV1Beta1 converts supported configs from v1beta1 to v1alpha1
func ConvertSupportedConfigsFromV1Beta1(configs []addonv1beta1.ConfigGroupResource) []addonv1alpha1.ConfigGroupResource {
	if configs == nil {
		return nil
	}
	result := make([]addonv1alpha1.ConfigGroupResource, len(configs))
	for i := range configs {
		result[i] = addonv1alpha1.ConfigGroupResource(configs[i])
	}
	return result
}
