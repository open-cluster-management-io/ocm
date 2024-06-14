package templateagent

import (
	"fmt"
	"strings"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"

	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/utils"
	addonapiv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

// decorator mutate the unstructured and returns an unstructured.
type decorator interface {
	decorate(obj *unstructured.Unstructured) (*unstructured.Unstructured, error)
}

type namespaceDecorator struct {
	installNamespace string
	// paths is the paths of a resource kind that the decorator needs to set namespace field in it.
	// if the returned object is a list, decorator will set namespace for each item.
	paths map[string][]string
}

func newNamespaceDecorator(privateValues addonfactory.Values) *namespaceDecorator {
	decorator := &namespaceDecorator{
		paths: map[string][]string{
			"ClusterRoleBinding": {"subjects", "namespace"},
			"RoleBinding":        {"subjects", "namespace"},
			"Namespace":          {"metadata", "name"},
		},
	}
	namespace, ok := privateValues[InstallNamespacePrivateValueKey]
	if ok {
		decorator.installNamespace = namespace.(string)
	}

	return decorator
}

func (d *namespaceDecorator) decorate(obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	if len(d.installNamespace) == 0 {
		return obj, nil
	}

	// If obj has no namespace set, we do not mutate namespace assuming it is cluster scoped.
	if len(obj.GetNamespace()) > 0 {
		obj.SetNamespace(d.installNamespace)
	}

	paths, ok := d.paths[obj.GetKind()]
	if !ok {
		return obj, nil
	}

	err := setUnstructuredNestedField(obj.Object, d.installNamespace, paths)
	return obj, err
}

// search the object to set the val, if an array is found, find every item in the array.
func setUnstructuredNestedField(obj interface{}, val string, paths []string) error {
	switch f := obj.(type) {
	case []interface{}:
		for _, item := range f {
			if err := setUnstructuredNestedField(item, val, paths); err != nil {
				return err
			}
		}
	case map[string]interface{}:
		if len(paths) == 1 {
			f[paths[0]] = val
			return nil
		}
		field, ok := f[paths[0]]
		if !ok {
			return fmt.Errorf("failed to find field %s", paths[0])
		}
		return setUnstructuredNestedField(field, val, paths[1:])
	}
	return nil
}

type deploymentDecorator struct {
	decorators []podTemplateSpecDecorator
}

func newDeploymentDecorator(
	addonName string,
	template *addonapiv1alpha1.AddOnTemplate,
	orderedValues orderedValues,
	privateValues addonfactory.Values,
) decorator {
	return &deploymentDecorator{
		decorators: []podTemplateSpecDecorator{
			newEnvironmentDecorator(orderedValues),
			newVolumeDecorator(addonName, template),
			newNodePlacementDecorator(privateValues),
			newImageDecorator(privateValues),
		},
	}
}

func (d *deploymentDecorator) decorate(obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	deployment, err := utils.ConvertToDeployment(obj)
	// not a deployment, directly return
	if err != nil {
		return obj, nil
	}

	for _, decorator := range d.decorators {
		err = decorator.decorate(&deployment.Spec.Template)
		if err != nil {
			return obj, err
		}
	}

	result, err := runtime.DefaultUnstructuredConverter.ToUnstructured(deployment)
	if err != nil {
		return obj, err
	}

	return &unstructured.Unstructured{Object: result}, nil
}

type daemonSetDecorator struct {
	decorators []podTemplateSpecDecorator
}

func newDaemonSetDecorator(
	addonName string,
	template *addonapiv1alpha1.AddOnTemplate,
	orderedValues orderedValues,
	privateValues addonfactory.Values,
) decorator {
	return &daemonSetDecorator{
		decorators: []podTemplateSpecDecorator{
			newEnvironmentDecorator(orderedValues),
			newVolumeDecorator(addonName, template),
			newNodePlacementDecorator(privateValues),
			newImageDecorator(privateValues),
		},
	}
}

func (d *daemonSetDecorator) decorate(obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	daemonSet, err := utils.ConvertToDaemonSet(obj)
	// not a deployment, directly return
	if err != nil {
		return obj, nil
	}

	for _, decorator := range d.decorators {
		err = decorator.decorate(&daemonSet.Spec.Template)
		if err != nil {
			return obj, err
		}
	}

	result, err := runtime.DefaultUnstructuredConverter.ToUnstructured(daemonSet)
	if err != nil {
		return obj, err
	}

	return &unstructured.Unstructured{Object: result}, nil
}

type podTemplateSpecDecorator interface {
	// decorate modifies the deployment in place
	decorate(pod *corev1.PodTemplateSpec) error
}

type environmentDecorator struct {
	orderedValues orderedValues
}

func newEnvironmentDecorator(orderedValues orderedValues) podTemplateSpecDecorator {
	return &environmentDecorator{
		orderedValues: orderedValues,
	}
}
func (d *environmentDecorator) decorate(pod *corev1.PodTemplateSpec) error {
	envVars := make([]corev1.EnvVar, len(d.orderedValues))
	for index, value := range d.orderedValues {
		envVars[index] = corev1.EnvVar{
			Name:  value.name,
			Value: value.value,
		}
	}

	for j := range pod.Spec.Containers {
		pod.Spec.Containers[j].Env = append(
			pod.Spec.Containers[j].Env,
			envVars...)
	}

	return nil
}

type volumeDecorator struct {
	template  *addonapiv1alpha1.AddOnTemplate
	addonName string
}

func newVolumeDecorator(addonName string, template *addonapiv1alpha1.AddOnTemplate) podTemplateSpecDecorator {
	return &volumeDecorator{
		addonName: addonName,
		template:  template,
	}
}

func (d *volumeDecorator) decorate(pod *corev1.PodTemplateSpec) error {

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

	for j := range pod.Spec.Containers {
		pod.Spec.Containers[j].VolumeMounts = append(
			pod.Spec.Containers[j].VolumeMounts, volumeMounts...)
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, volumes...)

	return nil
}

type nodePlacementDecorator struct {
	privateValues addonfactory.Values
}

func newNodePlacementDecorator(privateValues addonfactory.Values) podTemplateSpecDecorator {
	return &nodePlacementDecorator{
		privateValues: privateValues,
	}
}

func (d *nodePlacementDecorator) decorate(pod *corev1.PodTemplateSpec) error {
	nodePlacement, ok := d.privateValues[NodePlacementPrivateValueKey]
	if !ok {
		return nil
	}

	np, ok := nodePlacement.(*addonapiv1alpha1.NodePlacement)
	if !ok {
		return fmt.Errorf("node placement value is invalid")
	}

	if np.NodeSelector != nil {
		pod.Spec.NodeSelector = np.NodeSelector
	}

	if np.NodeSelector != nil {
		pod.Spec.Tolerations = np.Tolerations
	}

	return nil
}

type imageDecorator struct {
	privateValues addonfactory.Values
}

func newImageDecorator(privateValues addonfactory.Values) podTemplateSpecDecorator {
	return &imageDecorator{
		privateValues: privateValues,
	}
}

func (d *imageDecorator) decorate(pod *corev1.PodTemplateSpec) error {
	registries, ok := d.privateValues[RegistriesPrivateValueKey]
	if !ok {
		return nil
	}

	ims, ok := registries.([]addonapiv1alpha1.ImageMirror)
	if !ok {
		return fmt.Errorf("registries value is invalid")
	}

	for i := range pod.Spec.Containers {
		pod.Spec.Containers[i].Image = addonfactory.OverrideImage(
			ims, pod.Spec.Containers[i].Image)
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
