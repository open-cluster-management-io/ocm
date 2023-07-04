package templateagent

import (
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

type deploymentDecorator interface {
	// decorate modifies the deployment in place
	decorate(deployment *appsv1.Deployment) error
}

type environmentDecorator struct {
	orderedValues orderedValues
}

func newEnvironmentDecorator(orderedValues orderedValues) deploymentDecorator {
	return &environmentDecorator{
		orderedValues: orderedValues,
	}
}
func (d *environmentDecorator) decorate(deployment *appsv1.Deployment) error {
	envVars := make([]corev1.EnvVar, len(d.orderedValues))
	for index, value := range d.orderedValues {
		envVars[index] = corev1.EnvVar{
			Name:  value.name,
			Value: value.value,
		}
	}

	for j := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[j].Env = append(
			deployment.Spec.Template.Spec.Containers[j].Env,
			envVars...)
	}

	return nil
}

type volumeDecorator struct {
	template  *addonapiv1alpha1.AddOnTemplate
	addonName string
}

func newVolumeDecorator(addonName string, template *addonapiv1alpha1.AddOnTemplate) deploymentDecorator {
	return &volumeDecorator{
		addonName: addonName,
		template:  template,
	}
}

func (d *volumeDecorator) decorate(deployment *appsv1.Deployment) error {

	volumeMounts := []corev1.VolumeMount{}
	volumes := []corev1.Volume{}

	for _, registration := range d.template.Spec.Registration {
		if registration.Type == addonapiv1alpha1.RegistrationTypeKubeClient {
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      "hub-kubeconfig",
				MountPath: hubKubeconfigSecretMountPath(),
			})
			volumes = append(volumes, corev1.Volume{
				Name: "hub-kubeconfig",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: HubKubeconfigSecretName(d.addonName),
					},
				},
			})
		}

		if registration.Type == addonapiv1alpha1.RegistrationTypeCustomSigner {
			if registration.CustomSigner == nil {
				return fmt.Errorf("custom signer is nil")
			}
			name := fmt.Sprintf("cert-%s", strings.ReplaceAll(
				strings.ReplaceAll(registration.CustomSigner.SignerName, "/", "-"),
				".", "-"))
			volumeMounts = append(volumeMounts, corev1.VolumeMount{
				Name:      name,
				MountPath: customSignedSecretMountPath(registration.CustomSigner.SignerName),
			})
			volumes = append(volumes, corev1.Volume{
				Name: name,
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: CustomSignedSecretName(d.addonName, registration.CustomSigner.SignerName),
					},
				},
			})
		}
	}

	if len(volumeMounts) == 0 || len(volumes) == 0 {
		return nil
	}

	for j := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[j].VolumeMounts = append(
			deployment.Spec.Template.Spec.Containers[j].VolumeMounts, volumeMounts...)
	}

	deployment.Spec.Template.Spec.Volumes = append(deployment.Spec.Template.Spec.Volumes, volumes...)

	return nil
}

type nodePlacementDecorator struct {
	privateValues addonfactory.Values
}

func newNodePlacementDecorator(privateValues addonfactory.Values) deploymentDecorator {
	return &nodePlacementDecorator{
		privateValues: privateValues,
	}
}

func (d *nodePlacementDecorator) decorate(deployment *appsv1.Deployment) error {
	nodePlacement, ok := d.privateValues[NodePlacementPrivateValueKey]
	if !ok {
		return nil
	}

	np, ok := nodePlacement.(*addonapiv1alpha1.NodePlacement)
	if !ok {
		return fmt.Errorf("node placement value is invalid")
	}

	if np.NodeSelector != nil {
		deployment.Spec.Template.Spec.NodeSelector = np.NodeSelector
	}

	if np.NodeSelector != nil {
		deployment.Spec.Template.Spec.Tolerations = np.Tolerations
	}

	return nil
}

type imageDecorator struct {
	privateValues addonfactory.Values
}

func newImageDecorator(privateValues addonfactory.Values) deploymentDecorator {
	return &imageDecorator{
		privateValues: privateValues,
	}
}

func (d *imageDecorator) decorate(deployment *appsv1.Deployment) error {
	registries, ok := d.privateValues[RegistriesPrivateValueKey]
	if !ok {
		return nil
	}

	ims, ok := registries.([]addonapiv1alpha1.ImageMirror)
	if !ok {
		return fmt.Errorf("registries value is invalid")
	}

	for i := range deployment.Spec.Template.Spec.Containers {
		deployment.Spec.Template.Spec.Containers[i].Image = addonfactory.OverrideImage(
			ims, deployment.Spec.Template.Spec.Containers[i].Image)
	}

	return nil
}

func hubKubeconfigSecretMountPath() string {
	return "/managed/hub-kubeconfig"
}

func HubKubeconfigSecretName(addonName string) string {
	return fmt.Sprintf("%s-hub-kubeconfig", addonName)
}

func CustomSignedSecretName(addonName, signerName string) string {
	return fmt.Sprintf("%s-%s-client-cert", addonName, strings.ReplaceAll(signerName, "/", "-"))
}

func customSignedSecretMountPath(signerName string) string {
	return fmt.Sprintf("/managed/%s", strings.ReplaceAll(signerName, "/", "-"))
}
