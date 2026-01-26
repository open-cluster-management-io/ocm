// Copyright Contributors to the Open Cluster Management project
package v1beta1

import (
	"fmt"

	certificates "k8s.io/api/certificates/v1"
	"k8s.io/apimachinery/pkg/conversion"
	"open-cluster-management.io/api/addon/v1alpha1"
)

const (
	// ReservedNoDefaultConfigName is a reserved sentinel value used internally during API version conversion.
	// It indicates that a v1alpha1 ConfigMeta had no defaultConfig when converting to v1beta1.
	// This value is intentionally invalid as a Kubernetes resource name (starts with "__") to prevent
	// collision with legitimate user-provided config names and ensure data integrity on round-trip conversions.
	// WARNING: This value is reserved and MUST NOT be used as a real config name.
	ReservedNoDefaultConfigName = "__reserved_no_default__"
)

// nolint:staticcheck
func Convert_v1beta1_ClusterManagementAddOnSpec_To_v1alpha1_ClusterManagementAddOnSpec(in *ClusterManagementAddOnSpec, out *v1alpha1.ClusterManagementAddOnSpec, s conversion.Scope) error {
	if err := autoConvert_v1beta1_ClusterManagementAddOnSpec_To_v1alpha1_ClusterManagementAddOnSpec(in, out, s); err != nil {
		return err
	}

	config := []v1alpha1.ConfigMeta{}
	for _, inConfig := range in.DefaultConfigs {
		c := v1alpha1.ConfigMeta{
			ConfigGroupResource: v1alpha1.ConfigGroupResource{
				Group:    inConfig.Group,
				Resource: inConfig.Resource,
			},
		}

		// If there's a config referent and it's not the reserved sentinel, convert it to default config
		// The reserved sentinel indicates there was no defaultConfig in the original v1alpha1
		if inConfig.Name != "" && inConfig.Name != ReservedNoDefaultConfigName {
			c.DefaultConfig = &v1alpha1.ConfigReferent{
				Namespace: inConfig.Namespace,
				Name:      inConfig.Name,
			}
		}
		config = append(config, c)
	}
	out.SupportedConfigs = config
	return nil
}

// nolint:staticcheck
func Convert_v1alpha1_ClusterManagementAddOnSpec_To_v1beta1_ClusterManagementAddOnSpec(in *v1alpha1.ClusterManagementAddOnSpec, out *ClusterManagementAddOnSpec, s conversion.Scope) error {
	if err := autoConvert_v1alpha1_ClusterManagementAddOnSpec_To_v1beta1_ClusterManagementAddOnSpec(in, out, s); err != nil {
		return err
	}

	configs := []AddOnConfig{}
	for _, inConfig := range in.SupportedConfigs {
		c := AddOnConfig{
			ConfigGroupResource: ConfigGroupResource{
				Resource: inConfig.Resource,
				Group:    inConfig.Group,
			},
		}

		if inConfig.DefaultConfig != nil {
			c.ConfigReferent = ConfigReferent{
				Name:      inConfig.DefaultConfig.Name,
				Namespace: inConfig.DefaultConfig.Namespace,
			}
		} else {
			c.ConfigReferent = ConfigReferent{
				Name: ReservedNoDefaultConfigName,
			}
		}
		configs = append(configs, c)
	}
	out.DefaultConfigs = configs
	return nil
}

func Convert_v1alpha1_ConfigReference_To_v1beta1_ConfigReference(in *v1alpha1.ConfigReference, out *ConfigReference, s conversion.Scope) error {
	if err := autoConvert_v1alpha1_ConfigReference_To_v1beta1_ConfigReference(in, out, s); err != nil {
		return err
	}

	return nil
}

func Convert_v1alpha1_ManagedClusterAddOnSpec_To_v1beta1_ManagedClusterAddOnSpec(in *v1alpha1.ManagedClusterAddOnSpec, out *ManagedClusterAddOnSpec, s conversion.Scope) error {
	// installNamespace should be treated outside this converter since it will be set on annotation
	for _, inConfig := range in.Configs {
		outConfig := AddOnConfig{}
		if err := Convert_v1alpha1_AddOnConfig_To_v1beta1_AddOnConfig(&inConfig, &outConfig, s); err != nil {
			return err
		}
		out.Configs = append(out.Configs, outConfig)
	}

	return nil
}

func Convert_v1beta1_ManagedClusterAddOnStatus_To_v1alpha1_ManagedClusterAddOnStatus(in *ManagedClusterAddOnStatus, out *v1alpha1.ManagedClusterAddOnStatus, s conversion.Scope) error {
	if err := autoConvert_v1beta1_ManagedClusterAddOnStatus_To_v1alpha1_ManagedClusterAddOnStatus(in, out, s); err != nil {
		return err
	}

	// Extract the kubeClientDriver from kubeClient registration config to status level
	for i := range in.Registrations {
		if in.Registrations[i].Type == KubeClient && in.Registrations[i].KubeClient != nil {
			out.KubeClientDriver = in.Registrations[i].KubeClient.Driver
			break
		}
	}

	return nil
}

func Convert_v1alpha1_ManagedClusterAddOnStatus_To_v1beta1_ManagedClusterAddOnStatus(in *v1alpha1.ManagedClusterAddOnStatus, out *ManagedClusterAddOnStatus, s conversion.Scope) error {
	if err := autoConvert_v1alpha1_ManagedClusterAddOnStatus_To_v1beta1_ManagedClusterAddOnStatus(in, out, s); err != nil {
		return err
	}

	// Set the kubeClientDriver from status level to the kubeClient registration config
	if in.KubeClientDriver != "" {
		for i := range out.Registrations {
			if out.Registrations[i].Type == KubeClient {
				if out.Registrations[i].KubeClient == nil {
					out.Registrations[i].KubeClient = &KubeClientConfig{}
				}
				out.Registrations[i].KubeClient.Driver = in.KubeClientDriver
			}
		}
	}

	return nil
}

// nolint:staticcheck
func Convert_v1beta1_RegistrationConfig_To_v1alpha1_RegistrationConfig(in *RegistrationConfig, out *v1alpha1.RegistrationConfig, s conversion.Scope) error {
	if in.Type == KubeClient {
		out.SignerName = certificates.KubeAPIServerClientSignerName
		if in.KubeClient == nil {
			return fmt.Errorf("nil KubeClient")
		}
		out.Subject = v1alpha1.Subject{
			User:   in.KubeClient.Subject.User,
			Groups: in.KubeClient.Subject.Groups,
		}
		// Driver is now handled at status level, not in RegistrationConfig
	} else {
		if in.CustomSigner == nil {
			return fmt.Errorf("nil CustomSigner")
		}
		out.SignerName = in.CustomSigner.SignerName
		if err := Convert_v1beta1_Subject_To_v1alpha1_Subject(&in.CustomSigner.Subject, &out.Subject, s); err != nil {
			return err
		}
	}

	return nil
}

// nolint:staticcheck
func Convert_v1alpha1_RegistrationConfig_To_v1beta1_RegistrationConfig(in *v1alpha1.RegistrationConfig, out *RegistrationConfig, s conversion.Scope) error {
	if in.SignerName == certificates.KubeAPIServerClientSignerName {
		out.Type = KubeClient
		out.KubeClient = &KubeClientConfig{
			Subject: KubeClientSubject{
				BaseSubject{
					User:   in.Subject.User,
					Groups: in.Subject.Groups,
				},
			},
			// Driver is now handled at status level, not in RegistrationConfig
		}
	} else {
		out.Type = CustomSigner
		out.CustomSigner = &CustomSignerConfig{
			SignerName: in.SignerName,
			Subject: Subject{
				BaseSubject: BaseSubject{
					User:   in.Subject.User,
					Groups: in.Subject.Groups,
				},
				OrganizationUnits: in.Subject.OrganizationUnits,
			},
		}
	}

	return nil
}

func Convert_v1beta1_Subject_To_v1alpha1_Subject(in *Subject, out *v1alpha1.Subject, s conversion.Scope) error {
	out.User = in.User
	out.Groups = in.Groups
	out.OrganizationUnits = in.OrganizationUnits
	return nil
}

func Convert_v1alpha1_Subject_To_v1beta1_Subject(in *v1alpha1.Subject, out *Subject, s conversion.Scope) error {
	out.User = in.User
	out.Groups = in.Groups
	out.OrganizationUnits = in.OrganizationUnits
	return nil
}
