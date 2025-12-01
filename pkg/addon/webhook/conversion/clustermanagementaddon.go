// Copyright Contributors to the Open Cluster Management project

package conversion

import (
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
)

const (
	// ReservedNoDefaultConfigName is a reserved sentinel value used internally during API version conversion.
	// It indicates that a v1alpha1 ConfigMeta had no defaultConfig when converting to v1beta1.
	// This value is intentionally invalid as a Kubernetes resource name (starts with "__") to prevent
	// collision with legitimate user-provided config names and ensure data integrity on round-trip conversions.
	// WARNING: This value is reserved and MUST NOT be used as a real config name.
	ReservedNoDefaultConfigName = "__reserved_no_default__"
)

// ConvertAddOnConfigsToV1Beta1 converts addon configs from v1alpha1 to v1beta1
// This is a generic helper used by both ClusterManagementAddOn and ManagedClusterAddOn conversions
func ConvertAddOnConfigsToV1Beta1(configs []addonv1alpha1.AddOnConfig) []addonv1beta1.AddOnConfig {
	if configs == nil {
		return nil
	}
	result := make([]addonv1beta1.AddOnConfig, len(configs))
	for i, config := range configs {
		result[i] = addonv1beta1.AddOnConfig{
			ConfigGroupResource: addonv1beta1.ConfigGroupResource(config.ConfigGroupResource),
			ConfigReferent:      addonv1beta1.ConfigReferent(config.ConfigReferent),
		}
	}
	return result
}

// ConvertAddOnConfigsFromV1Beta1 converts addon configs from v1beta1 to v1alpha1
// This is a generic helper used by both ClusterManagementAddOn and ManagedClusterAddOn conversions
func ConvertAddOnConfigsFromV1Beta1(configs []addonv1beta1.AddOnConfig) []addonv1alpha1.AddOnConfig {
	if configs == nil {
		return nil
	}
	result := make([]addonv1alpha1.AddOnConfig, len(configs))
	for i, config := range configs {
		result[i] = addonv1alpha1.AddOnConfig{
			ConfigGroupResource: addonv1alpha1.ConfigGroupResource(config.ConfigGroupResource),
			ConfigReferent:      addonv1alpha1.ConfigReferent(config.ConfigReferent),
		}
	}
	return result
}

// ConvertConfigMetaToAddOnConfig converts v1alpha1 ConfigMeta to v1beta1 AddOnConfig
func ConvertConfigMetaToAddOnConfig(configMetas []addonv1alpha1.ConfigMeta) []addonv1beta1.AddOnConfig {
	var addOnConfigs []addonv1beta1.AddOnConfig

	for _, configMeta := range configMetas {
		addOnConfig := addonv1beta1.AddOnConfig{
			ConfigGroupResource: addonv1beta1.ConfigGroupResource{
				Group:    configMeta.Group,
				Resource: configMeta.Resource,
			},
		}

		// If there's a default config, use it; otherwise use the reserved sentinel
		if configMeta.DefaultConfig != nil {
			addOnConfig.ConfigReferent = addonv1beta1.ConfigReferent{
				Namespace: configMeta.DefaultConfig.Namespace,
				Name:      configMeta.DefaultConfig.Name,
			}
		} else {
			// Use reserved sentinel as name when there's no default config
			// This sentinel is intentionally invalid as a K8s resource name to prevent collision
			addOnConfig.ConfigReferent = addonv1beta1.ConfigReferent{
				Name: ReservedNoDefaultConfigName,
			}
		}

		addOnConfigs = append(addOnConfigs, addOnConfig)
	}

	return addOnConfigs
}

// ConvertAddOnConfigToConfigMeta converts v1beta1 AddOnConfig to v1alpha1 ConfigMeta
func ConvertAddOnConfigToConfigMeta(addOnConfigs []addonv1beta1.AddOnConfig) []addonv1alpha1.ConfigMeta {
	var configMetas []addonv1alpha1.ConfigMeta

	for _, addOnConfig := range addOnConfigs {
		configMeta := addonv1alpha1.ConfigMeta{
			ConfigGroupResource: addonv1alpha1.ConfigGroupResource{
				Group:    addOnConfig.Group,
				Resource: addOnConfig.Resource,
			},
		}

		// If there's a config referent and it's not the reserved sentinel, convert it to default config
		// The reserved sentinel indicates there was no defaultConfig in the original v1alpha1
		if addOnConfig.Name != "" && addOnConfig.Name != ReservedNoDefaultConfigName {
			configMeta.DefaultConfig = &addonv1alpha1.ConfigReferent{
				Namespace: addOnConfig.Namespace,
				Name:      addOnConfig.Name,
			}
		}

		configMetas = append(configMetas, configMeta)
	}

	return configMetas
}

// ConvertInstallStrategyToV1Beta1 converts install strategy from v1alpha1 to v1beta1
func ConvertInstallStrategyToV1Beta1(src addonv1alpha1.InstallStrategy) addonv1beta1.InstallStrategy {
	// Note: All fields in InstallStrategy are explicitly handled:
	// - Type: copied below (same type)
	// - Placements: converted in loop (PlacementStrategy types are different)
	dst := addonv1beta1.InstallStrategy{Type: src.Type}

	for _, placement := range src.Placements {
		// Note: All fields in PlacementStrategy are explicitly handled:
		// - PlacementRef: converted below (identical structure, using type conversion)
		// - Configs: converted below (AddOnConfig types are different)
		// - RolloutStrategy: copied below (same type from cluster/v1alpha1)

		dst.Placements = append(dst.Placements, addonv1beta1.PlacementStrategy{
			// PlacementRef has identical structure, so we can use type conversion
			PlacementRef:    addonv1beta1.PlacementRef(placement.PlacementRef),
			Configs:         ConvertAddOnConfigsToV1Beta1(placement.Configs),
			RolloutStrategy: placement.RolloutStrategy,
		})
	}

	return dst
}

// ConvertInstallStrategyFromV1Beta1 converts install strategy from v1beta1 to v1alpha1
func ConvertInstallStrategyFromV1Beta1(src addonv1beta1.InstallStrategy) addonv1alpha1.InstallStrategy {
	// Note: All fields in InstallStrategy are explicitly handled:
	// - Type: copied below (same type)
	// - Placements: converted in loop (PlacementStrategy types are different)
	dst := addonv1alpha1.InstallStrategy{Type: src.Type}

	for _, placement := range src.Placements {
		// Note: All fields in PlacementStrategy are explicitly handled:
		// - PlacementRef: converted below (identical structure, using type conversion)
		// - Configs: converted below (AddOnConfig types are different)
		// - RolloutStrategy: copied below (same type from cluster/v1alpha1)

		dst.Placements = append(dst.Placements, addonv1alpha1.PlacementStrategy{
			// PlacementRef has identical structure, so we can use type conversion
			PlacementRef:    addonv1alpha1.PlacementRef(placement.PlacementRef),
			Configs:         ConvertAddOnConfigsFromV1Beta1(placement.Configs),
			RolloutStrategy: placement.RolloutStrategy,
		})
	}

	return dst
}

// Note: RolloutStrategy is defined in cluster/v1alpha1 and is the same type
// for both addon v1alpha1 and v1beta1, so no conversion is needed

// ConvertDefaultConfigReferencesToV1Beta1 converts default config references from v1alpha1 to v1beta1
func ConvertDefaultConfigReferencesToV1Beta1(refs []addonv1alpha1.DefaultConfigReference) []addonv1beta1.DefaultConfigReference {
	var converted []addonv1beta1.DefaultConfigReference

	for _, ref := range refs {
		converted = append(converted, addonv1beta1.DefaultConfigReference{
			// ConfigGroupResource has identical structure, so we can use type conversion
			ConfigGroupResource: addonv1beta1.ConfigGroupResource(ref.ConfigGroupResource),
			DesiredConfig:       ConvertConfigSpecHashToV1Beta1(ref.DesiredConfig),
		})
	}

	return converted
}

// ConvertDefaultConfigReferencesFromV1Beta1 converts default config references from v1beta1 to v1alpha1
func ConvertDefaultConfigReferencesFromV1Beta1(refs []addonv1beta1.DefaultConfigReference) []addonv1alpha1.DefaultConfigReference {
	var converted []addonv1alpha1.DefaultConfigReference

	for _, ref := range refs {
		converted = append(converted, addonv1alpha1.DefaultConfigReference{
			// ConfigGroupResource has identical structure, so we can use type conversion
			ConfigGroupResource: addonv1alpha1.ConfigGroupResource(ref.ConfigGroupResource),
			DesiredConfig:       ConvertConfigSpecHashFromV1Beta1(ref.DesiredConfig),
		})
	}

	return converted
}

// ConvertInstallProgressionsToV1Beta1 converts install progressions from v1alpha1 to v1beta1
func ConvertInstallProgressionsToV1Beta1(progressions []addonv1alpha1.InstallProgression) []addonv1beta1.InstallProgression {
	var converted []addonv1beta1.InstallProgression

	for _, prog := range progressions {
		converted = append(converted, addonv1beta1.InstallProgression{
			// PlacementRef has identical structure, so we can use type conversion
			PlacementRef:     addonv1beta1.PlacementRef(prog.PlacementRef),
			ConfigReferences: ConvertInstallConfigReferencesToV1Beta1(prog.ConfigReferences),
			Conditions:       prog.Conditions,
		})
	}

	return converted
}

// ConvertInstallProgressionsFromV1Beta1 converts install progressions from v1beta1 to v1alpha1
func ConvertInstallProgressionsFromV1Beta1(progressions []addonv1beta1.InstallProgression) []addonv1alpha1.InstallProgression {
	var converted []addonv1alpha1.InstallProgression

	for _, prog := range progressions {
		converted = append(converted, addonv1alpha1.InstallProgression{
			// PlacementRef has identical structure, so we can use type conversion
			PlacementRef:     addonv1alpha1.PlacementRef(prog.PlacementRef),
			ConfigReferences: ConvertInstallConfigReferencesFromV1Beta1(prog.ConfigReferences),
			Conditions:       prog.Conditions,
		})
	}

	return converted
}

// ConvertInstallConfigReferencesToV1Beta1 converts install config references from v1alpha1 to v1beta1
func ConvertInstallConfigReferencesToV1Beta1(refs []addonv1alpha1.InstallConfigReference) []addonv1beta1.InstallConfigReference {
	var converted []addonv1beta1.InstallConfigReference

	for _, ref := range refs {
		converted = append(converted, addonv1beta1.InstallConfigReference{
			// ConfigGroupResource has identical structure, so we can use type conversion
			ConfigGroupResource: addonv1beta1.ConfigGroupResource(ref.ConfigGroupResource),
			DesiredConfig:       ConvertConfigSpecHashToV1Beta1(ref.DesiredConfig),
			LastAppliedConfig:   ConvertConfigSpecHashToV1Beta1(ref.LastAppliedConfig),
			LastKnownGoodConfig: ConvertConfigSpecHashToV1Beta1(ref.LastKnownGoodConfig),
		})
	}

	return converted
}

// ConvertInstallConfigReferencesFromV1Beta1 converts install config references from v1beta1 to v1alpha1
func ConvertInstallConfigReferencesFromV1Beta1(refs []addonv1beta1.InstallConfigReference) []addonv1alpha1.InstallConfigReference {
	var converted []addonv1alpha1.InstallConfigReference

	for _, ref := range refs {
		converted = append(converted, addonv1alpha1.InstallConfigReference{
			// ConfigGroupResource has identical structure, so we can use type conversion
			ConfigGroupResource: addonv1alpha1.ConfigGroupResource(ref.ConfigGroupResource),
			DesiredConfig:       ConvertConfigSpecHashFromV1Beta1(ref.DesiredConfig),
			LastAppliedConfig:   ConvertConfigSpecHashFromV1Beta1(ref.LastAppliedConfig),
			LastKnownGoodConfig: ConvertConfigSpecHashFromV1Beta1(ref.LastKnownGoodConfig),
		})
	}

	return converted
}

// Helper functions for ConfigSpecHash conversion (shared by both ClusterManagementAddOn and ManagedClusterAddOn)

// ConvertConfigSpecHashToV1Beta1 converts ConfigSpecHash from v1alpha1 to v1beta1
// This is used for DesiredConfig, LastAppliedConfig, and LastKnownGoodConfig fields
func ConvertConfigSpecHashToV1Beta1(config *addonv1alpha1.ConfigSpecHash) *addonv1beta1.ConfigSpecHash {
	if config == nil {
		return nil
	}
	return &addonv1beta1.ConfigSpecHash{
		ConfigReferent: addonv1beta1.ConfigReferent(config.ConfigReferent),
		SpecHash:       config.SpecHash,
	}
}

// ConvertConfigSpecHashFromV1Beta1 converts ConfigSpecHash from v1beta1 to v1alpha1
// This is used for DesiredConfig, LastAppliedConfig, and LastKnownGoodConfig fields
func ConvertConfigSpecHashFromV1Beta1(config *addonv1beta1.ConfigSpecHash) *addonv1alpha1.ConfigSpecHash {
	if config == nil {
		return nil
	}
	return &addonv1alpha1.ConfigSpecHash{
		ConfigReferent: addonv1alpha1.ConfigReferent(config.ConfigReferent),
		SpecHash:       config.SpecHash,
	}
}
