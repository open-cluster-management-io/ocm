package e2e

import (
	"context"
	"fmt"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("Image Tag Validation", func() {
	Context("When OCM components are deployed", func() {
		BeforeEach(func() {
			if expectedImageTag == "" {
				Skip("Skipping image tag validation: no expected image tag specified")
			}
		})

		It("should use the expected image tag in all containers", func() {
			By(fmt.Sprintf("Validating all containers use image tag: %s", expectedImageTag))

			validateClusterManagerImageTags()
			validateKlusterletImageTags()
		})

		It("should have correct image tags in test variables", func() {
			By(fmt.Sprintf("Validating test image variables end with tag: %s", expectedImageTag))

			if registrationImage != "" && !strings.HasSuffix(registrationImage, ":"+expectedImageTag) {
				Fail(fmt.Sprintf("registrationImage does not end with expected tag: %s (actual: %s)", expectedImageTag, registrationImage))
			}

			if workImage != "" && !strings.HasSuffix(workImage, ":"+expectedImageTag) {
				Fail(fmt.Sprintf("workImage does not end with expected tag: %s (actual: %s)", expectedImageTag, workImage))
			}

			if singletonImage != "" && !strings.HasSuffix(singletonImage, ":"+expectedImageTag) {
				Fail(fmt.Sprintf("singletonImage does not end with expected tag: %s (actual: %s)", expectedImageTag, singletonImage))
			}
		})

		It("should have correct image tags in ClusterManager spec", func() {
			By(fmt.Sprintf("Validating ClusterManager imagePullSpec fields end with tag: %s", expectedImageTag))

			validateClusterManagerImageSpecs()
		})
	})
})

func validateClusterManagerImageTags() {
	By("Checking cluster-manager deployment containers")
	ctx := context.TODO()

	Eventually(func() error {
		deployment, err := hub.KubeClient.AppsV1().Deployments("open-cluster-management").Get(ctx, "cluster-manager", metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get cluster-manager deployment: %v", err)
		}

		for _, container := range deployment.Spec.Template.Spec.Containers {
			if err := validateImageTag(container.Image, container.Name, "cluster-manager"); err != nil {
				return err
			}
		}

		// Also check init containers if any
		for _, container := range deployment.Spec.Template.Spec.InitContainers {
			if err := validateImageTag(container.Image, container.Name, "cluster-manager"); err != nil {
				return err
			}
		}

		return nil
	}).Should(Succeed())
}

func validateKlusterletImageTags() {
	By("Checking klusterlet deployment containers")
	ctx := context.TODO()

	Eventually(func() error {
		deployment, err := hub.KubeClient.AppsV1().Deployments("open-cluster-management").Get(ctx, "klusterlet", metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get klusterlet deployment: %v", err)
		}

		for _, container := range deployment.Spec.Template.Spec.Containers {
			if err := validateImageTag(container.Image, container.Name, "klusterlet"); err != nil {
				return err
			}
		}

		// Also check init containers if any
		for _, container := range deployment.Spec.Template.Spec.InitContainers {
			if err := validateImageTag(container.Image, container.Name, "klusterlet"); err != nil {
				return err
			}
		}

		return nil
	}).Should(Succeed())
}

func validateImageTag(image, containerName, deploymentName string) error {
	// Extract the tag from the image
	// Image format: registry/repo:tag or registry/repo@digest
	var actualTag string

	if strings.Contains(image, "@") {
		// Handle digest format (registry/repo@sha256:...)
		return fmt.Errorf("container %s in %s deployment uses digest format (%s), cannot validate tag",
			containerName, deploymentName, image)
	} else if strings.Contains(image, ":") {
		// Handle tag format (registry/repo:tag)
		parts := strings.Split(image, ":")
		actualTag = parts[len(parts)-1]
	} else {
		// No tag specified, defaults to "latest"
		actualTag = "latest"
	}

	// Only validate OCM-related images (skip system images like kube-rbac-proxy, etc.)
	if isOCMImage(image) {
		if actualTag != expectedImageTag {
			return fmt.Errorf("container %s in %s deployment has incorrect image tag: expected %s, got %s (image: %s)",
				containerName, deploymentName, expectedImageTag, actualTag, image)
		}
	}

	return nil
}

func isOCMImage(image string) bool {
	// Check if this is an OCM component image
	ocmPatterns := []string{
		"open-cluster-management",
		"registration",
		"work",
		"placement",
		"addon-manager",
		"klusterlet",
	}

	for _, pattern := range ocmPatterns {
		if strings.Contains(image, pattern) {
			return true
		}
	}

	return false
}

func validateClusterManagerImageSpecs() {
	ctx := context.TODO()

	Eventually(func() error {
		// Get the ClusterManager resource
		clusterManager, err := hub.OperatorClient.OperatorV1().ClusterManagers().Get(ctx, "cluster-manager", metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get ClusterManager: %v", err)
		}

		// Check each imagePullSpec field
		spec := clusterManager.Spec

		if spec.RegistrationImagePullSpec != "" {
			if err := validateImageTagInSpec(spec.RegistrationImagePullSpec, "registrationImagePullSpec"); err != nil {
				return err
			}
		}

		if spec.WorkImagePullSpec != "" {
			if err := validateImageTagInSpec(spec.WorkImagePullSpec, "workImagePullSpec"); err != nil {
				return err
			}
		}

		if spec.PlacementImagePullSpec != "" {
			if err := validateImageTagInSpec(spec.PlacementImagePullSpec, "placementImagePullSpec"); err != nil {
				return err
			}
		}

		if spec.AddOnManagerImagePullSpec != "" {
			if err := validateImageTagInSpec(spec.AddOnManagerImagePullSpec, "addOnManagerImagePullSpec"); err != nil {
				return err
			}
		}

		return nil
	}).Should(Succeed())
}

func validateImageTagInSpec(imageSpec, fieldName string) error {
	if !strings.HasSuffix(imageSpec, ":"+expectedImageTag) {
		return fmt.Errorf("ClusterManager.spec.%s does not end with expected tag: %s (actual: %s)",
			fieldName, expectedImageTag, imageSpec)
	}
	return nil
}
