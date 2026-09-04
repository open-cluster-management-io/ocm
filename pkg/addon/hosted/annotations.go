package hosted

import (
	"strings"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
)

const (
	// AnnotationKlusterletDeployMode is the ManagedCluster annotation for klusterlet deploy mode.
	AnnotationKlusterletDeployMode = "import.open-cluster-management.io/klusterlet-deploy-mode"

	// AnnotationKlusterletHostingClusterName is the ManagedCluster annotation for the hosting cluster name.
	AnnotationKlusterletHostingClusterName = "import.open-cluster-management.io/hosting-cluster-name"

	// AnnotationEnableHostedModeAddons indicates hosted-mode addons are enabled for the cluster.
	AnnotationEnableHostedModeAddons = "addon.open-cluster-management.io/enable-hosted-mode-addons"

	addonAnnotationPrefix = addonv1beta1.GroupName + "/"
)

// AddonAnnotationsFromManagedCluster returns addon.open-cluster-management.io/* annotations to set on a
// ManagedClusterAddOn at create time. It copies existing addon-prefix annotations from the cluster and
// derives addon.open-cluster-management.io/hosting-cluster-name from import annotations when hosted addons
// are enabled.
func AddonAnnotationsFromManagedCluster(cluster *clusterv1.ManagedCluster) map[string]string {
	addonAnnotations := map[string]string{}
	if cluster == nil {
		return addonAnnotations
	}

	for k, v := range cluster.Annotations {
		if strings.HasPrefix(k, addonAnnotationPrefix) {
			addonAnnotations[k] = v
		}
	}

	if hostingClusterName := DerivedAddonHostingClusterName(cluster); hostingClusterName != "" {
		addonAnnotations[addonv1beta1.HostingClusterNameAnnotationKey] = hostingClusterName
	}

	return addonAnnotations
}

// DerivedAddonHostingClusterName returns the addon hosting cluster name to propagate to ManagedClusterAddOns,
// or "" when the cluster is not configured for hosted-mode addons.
func DerivedAddonHostingClusterName(cluster *clusterv1.ManagedCluster) string {
	if cluster == nil || len(cluster.Annotations) == 0 {
		return ""
	}

	annotations := cluster.Annotations
	if !strings.EqualFold(annotations[AnnotationEnableHostedModeAddons], "true") {
		return ""
	}
	if !isHostedKlusterletDeployMode(annotations[AnnotationKlusterletDeployMode]) {
		return ""
	}

	hostingClusterName := annotations[AnnotationKlusterletHostingClusterName]
	if hostingClusterName == "" {
		return ""
	}

	return hostingClusterName
}

func isHostedKlusterletDeployMode(mode string) bool {
	return strings.EqualFold(mode, string(operatorapiv1.InstallModeHosted)) ||
		strings.EqualFold(mode, string(operatorapiv1.InstallModeSingletonHosted))
}
