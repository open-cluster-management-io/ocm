package agentdeploy

import (
	"fmt"
	"strings"

	"open-cluster-management.io/addon-framework/pkg/addonmanager/constants"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

const (
	byAddon           = "byAddon"
	byHostedAddon     = "byHostedAddon"
	hookByHostedAddon = "hookByHostedAddon"
)

func indexByAddon(obj interface{}) ([]string, error) {
	work, ok := obj.(*workapiv1.ManifestWork)
	if !ok {
		return []string{}, fmt.Errorf("obj is supposed to be a ManifestWork, but is %T", obj)
	}

	addonName, addonNamespace, isHook := extractAddonFromWork(work)

	if len(addonName) == 0 || len(addonNamespace) > 0 || isHook {
		return []string{}, nil
	}

	return []string{fmt.Sprintf("%s/%s", work.Namespace, addonName)}, nil
}

func indexByHostedAddon(obj interface{}) ([]string, error) {
	work, ok := obj.(*workapiv1.ManifestWork)
	if !ok {
		return []string{}, fmt.Errorf("obj is supposed to be a ManifestWork, but is %T", obj)
	}

	addonName, addonNamespace, isHook := extractAddonFromWork(work)

	if len(addonName) == 0 || len(addonNamespace) == 0 || isHook {
		return []string{}, nil
	}

	return []string{fmt.Sprintf("%s/%s", addonNamespace, addonName)}, nil
}

func indexHookByHostedAddon(obj interface{}) ([]string, error) {
	work, ok := obj.(*workapiv1.ManifestWork)
	if !ok {
		return []string{}, fmt.Errorf("obj is supposed to be a ManifestWork, but is %T", obj)
	}

	addonName, addonNamespace, isHook := extractAddonFromWork(work)

	if len(addonName) == 0 || len(addonNamespace) == 0 || !isHook {
		return []string{}, nil
	}

	return []string{fmt.Sprintf("%s/%s", addonNamespace, addonName)}, nil
}

func extractAddonFromWork(work *workapiv1.ManifestWork) (string, string, bool) {
	if len(work.Labels) == 0 {
		return "", "", false
	}

	addonName, ok := work.Labels[constants.AddonLabel]
	if !ok {
		return "", "", false
	}

	addonNamespace := work.Labels[constants.AddonNamespaceLabel]

	isHook := false
	if strings.HasPrefix(work.Name, constants.PreDeleteHookWorkName(addonName)) {
		isHook = true
	}

	return addonName, addonNamespace, isHook
}
