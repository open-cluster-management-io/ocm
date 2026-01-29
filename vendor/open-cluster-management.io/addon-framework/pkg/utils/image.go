package utils

import (
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/klog/v2"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

// OverrideImageByAnnotation is to override the image by image-registries annotation of managedCluster.
// The source registry will be replaced by the Mirror.
// The larger index will work if the Sources are the same.
func OverrideImageByAnnotation(annotations map[string]string, imageName string) (string, error) {
	if len(annotations) == 0 {
		return imageName, nil
	}

	if _, ok := annotations[clusterv1.ClusterImageRegistriesAnnotationKey]; !ok {
		return imageName, nil
	}

	type ImageRegistries struct {
		Registries []addonapiv1alpha1.ImageMirror `json:"registries"`
	}

	imageRegistries := ImageRegistries{}
	err := json.Unmarshal([]byte(annotations[clusterv1.ClusterImageRegistriesAnnotationKey]), &imageRegistries)
	if err != nil {
		klog.Errorf("failed to unmarshal the annotation %v,err %v", annotations[clusterv1.ClusterImageRegistriesAnnotationKey], err)
		return imageName, err
	}

	if len(imageRegistries.Registries) == 0 {
		return imageName, nil
	}
	overrideImageName := imageName
	for i := 0; i < len(imageRegistries.Registries); i++ {
		registry := imageRegistries.Registries[i]
		name := imageOverride(registry.Source, registry.Mirror, imageName)
		if name != imageName {
			overrideImageName = name
		}
	}
	return overrideImageName, nil
}

func imageOverride(source, mirror, imageName string) string {
	source = strings.TrimSuffix(source, "/")
	mirror = strings.TrimSuffix(mirror, "/")
	imageSegments := strings.Split(imageName, "/")
	imageNameTag := imageSegments[len(imageSegments)-1]
	if source == "" {
		if mirror == "" {
			return imageNameTag
		}
		return fmt.Sprintf("%s/%s", mirror, imageNameTag)
	}

	if !strings.HasPrefix(imageName, source) {
		return imageName
	}

	trimSegment := strings.TrimPrefix(imageName, source)
	return fmt.Sprintf("%s%s", mirror, trimSegment)
}
