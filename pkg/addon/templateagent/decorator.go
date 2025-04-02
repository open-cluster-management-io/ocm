package templateagent

import (
	"fmt"
	"path"
	"regexp"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"

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
	logger     klog.Logger
	decorators []podTemplateSpecDecorator
}

func newDeploymentDecorator(
	logger klog.Logger,
	addonName string,
	template *addonapiv1alpha1.AddOnTemplate,
	orderedValues orderedValues,
	privateValues addonfactory.Values,
) decorator {
	return &deploymentDecorator{
		logger: logger,
		decorators: []podTemplateSpecDecorator{
			newEnvironmentDecorator(orderedValues),
			newVolumeDecorator(addonName, template),
			newNodePlacementDecorator(privateValues),
			newImageDecorator(privateValues),
			newProxyHandler(logger, addonName, privateValues),
			newResourceRequirementsDecorator(logger, supportResourceDeployment, privateValues),
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
		err = decorator.decorate(deployment.Name, &deployment.Spec.Template)
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
	logger     klog.Logger
	decorators []podTemplateSpecDecorator
}

func newDaemonSetDecorator(
	logger klog.Logger,
	addonName string,
	template *addonapiv1alpha1.AddOnTemplate,
	orderedValues orderedValues,
	privateValues addonfactory.Values,
) decorator {
	return &daemonSetDecorator{
		logger: logger,
		decorators: []podTemplateSpecDecorator{
			newEnvironmentDecorator(orderedValues),
			newVolumeDecorator(addonName, template),
			newNodePlacementDecorator(privateValues),
			newImageDecorator(privateValues),
			newProxyHandler(logger, addonName, privateValues),
			newResourceRequirementsDecorator(logger, supportResourceDaemonset, privateValues),
		},
	}
}

func (d *daemonSetDecorator) decorate(obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	daemonSet, err := utils.ConvertToDaemonSet(obj)
	// not a daemonset, directly return
	if err != nil {
		return obj, nil
	}

	for _, decorator := range d.decorators {
		err = decorator.decorate(daemonSet.Name, &daemonSet.Spec.Template)
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
	// decorate modifies the pod template in place
	//   resourceName is the name of the resource, could be a deployment name or a daemonset name
	decorate(resourceName string, pod *corev1.PodTemplateSpec) error
}

type environmentDecorator struct {
	orderedValues orderedValues
}

func newEnvironmentDecorator(orderedValues orderedValues) podTemplateSpecDecorator {
	return &environmentDecorator{
		orderedValues: orderedValues,
	}
}
func (d *environmentDecorator) decorate(_ string, pod *corev1.PodTemplateSpec) error {
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

func (d *volumeDecorator) decorate(_ string, pod *corev1.PodTemplateSpec) error {

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

func (d *nodePlacementDecorator) decorate(_ string, pod *corev1.PodTemplateSpec) error {
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

func (d *imageDecorator) decorate(_ string, pod *corev1.PodTemplateSpec) error {
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

// objectsInjector injects additional runtime objects to the manifests, these objects will be created
// in the managed clusters
type objectsInjector interface {
	// inject returns a list of runtime objects to be created in the managed cluster
	inject() ([]runtime.Object, error)
}

// podTemplateHandler is a combination of podTemplateSpecDecorator and objectsInjector, it can decorate
// the pod in the deployments/daemonsets and inject additional runtime objects into the manifests
type podTemplateHandler interface {
	podTemplateSpecDecorator
	objectsInjector
}

type proxyHandler struct {
	logger        klog.Logger
	addonName     string
	privateValues addonfactory.Values
}

func newProxyHandler(logger klog.Logger, addonName string, privateValues addonfactory.Values) podTemplateHandler {
	return &proxyHandler{
		logger:        logger,
		addonName:     addonName,
		privateValues: privateValues,
	}
}

func (d *proxyHandler) decorate(name string, pod *corev1.PodTemplateSpec) error {
	pc, ok := d.getProxyConfig()
	if !ok {
		return nil
	}

	keyValues := []keyValuePair{}
	if len(pc.HTTPProxy) > 0 {
		keyValues = append(keyValues,
			keyValuePair{name: "HTTP_PROXY", value: pc.HTTPProxy},
			keyValuePair{name: "http_proxy", value: pc.HTTPProxy},
		)
	}
	if len(pc.HTTPSProxy) > 0 {
		keyValues = append(keyValues,
			keyValuePair{name: "HTTPS_PROXY", value: pc.HTTPSProxy},
			keyValuePair{name: "https_proxy", value: pc.HTTPSProxy},
		)
	}
	if len(pc.NoProxy) > 0 {
		keyValues = append(keyValues,
			keyValuePair{name: "NO_PROXY", value: pc.NoProxy},
			keyValuePair{name: "no_proxy", value: pc.NoProxy},
		)
	}

	if len(keyValues) == 0 {
		return nil
	}

	err := newEnvironmentDecorator(keyValues).decorate(name, pod)
	if err != nil {
		return err
	}

	if len(pc.CABundle) == 0 {
		return nil
	}

	return newCABundleDecorator(d.addonName, pc.CABundle).decorate(name, pod)
}

func (d *proxyHandler) getProxyConfig() (addonapiv1alpha1.ProxyConfig, bool) {
	proxyConfig, ok := d.privateValues[ProxyPrivateValueKey]
	if !ok {
		return addonapiv1alpha1.ProxyConfig{}, false
	}

	pc, ok := proxyConfig.(addonapiv1alpha1.ProxyConfig)
	if !ok {
		d.logger.Error(nil, "proxy config value is invalid", "value", proxyConfig)
		return addonapiv1alpha1.ProxyConfig{}, false
	}

	return pc, true
}

func (d *proxyHandler) inject() ([]runtime.Object, error) {
	pc, ok := d.getProxyConfig()
	if !ok {
		return nil, nil
	}

	if len(pc.CABundle) == 0 {
		return nil, nil
	}

	return []runtime.Object{
		&corev1.ConfigMap{
			// add TypeMeta to prevent error:
			//   "failed to generate required mapper.err got empty kind/version from object"
			TypeMeta: metav1.TypeMeta{
				Kind:       "ConfigMap",
				APIVersion: "v1",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: proxyCABundleConfigMapName(d.addonName),
				// use the default namespace, will be decorated by the namespaceDecorator
				Namespace: "open-cluster-management-agent-addon",
			},
			Data: map[string]string{
				proxyCABundleConfigMapDataKey(): string(pc.CABundle),
			},
		},
	}, nil
}

type caBundleDecorator struct {
	addonName    string
	caBundle     []byte
	envDecorator podTemplateSpecDecorator
}

func newCABundleDecorator(addonName string, caBundle []byte) podTemplateSpecDecorator {
	keyValues := []keyValuePair{}
	keyValues = append(keyValues,
		keyValuePair{name: "CA_BUNDLE_FILE_PATH", value: proxyCABundleFilePath()},
	)

	return &caBundleDecorator{
		addonName:    addonName,
		caBundle:     caBundle,
		envDecorator: newEnvironmentDecorator(keyValues),
	}
}

func (d *caBundleDecorator) decorate(name string, pod *corev1.PodTemplateSpec) error {
	err := d.envDecorator.decorate(name, pod)
	if err != nil {
		return err
	}

	volumeMounts := []corev1.VolumeMount{
		{
			Name:      "proxy-ca-bundle",
			MountPath: proxyCABundleConfigMapMountPath(),
		},
	}
	volumes := []corev1.Volume{
		{
			Name: "proxy-ca-bundle",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: proxyCABundleConfigMapName(d.addonName),
					},
				},
			},
		},
	}

	for j := range pod.Spec.Containers {
		pod.Spec.Containers[j].VolumeMounts = append(
			pod.Spec.Containers[j].VolumeMounts, volumeMounts...)
	}

	pod.Spec.Volumes = append(pod.Spec.Volumes, volumes...)
	return nil
}

type supportResource string

const (
	supportResourceDeployment supportResource = "deployments"
	supportResourceDaemonset  supportResource = "daemonsets"
)

type resourceRequirementsDecorator struct {
	privateValues addonfactory.Values
	resource      supportResource // only support daemonsets, deployments for now
	logger        klog.Logger
}

func newResourceRequirementsDecorator(logger klog.Logger, resource supportResource,
	privateValues addonfactory.Values) podTemplateSpecDecorator {
	return &resourceRequirementsDecorator{
		resource:      resource,
		privateValues: privateValues,
	}
}

func (d *resourceRequirementsDecorator) decorate(name string, pod *corev1.PodTemplateSpec) error {
	requirements, ok := d.privateValues[ResourceRequirementsPrivateValueKey]
	if !ok {
		return nil
	}

	regexRequirements, ok := requirements.([]addonfactory.RegexResourceRequirements)
	if !ok {
		return fmt.Errorf("resource requirements value is invalid")
	}

	for i := range pod.Spec.Containers {
		containerID := fmt.Sprintf("%s:%s:%s", d.resource, name, pod.Spec.Containers[i].Name)
		// revese the requirements array to make the later elements in the array have higher priority
		for j := len(regexRequirements) - 1; j >= 0; j-- {
			matched, err := regexp.MatchString(regexRequirements[j].ContainerIDRegex, containerID)
			if err != nil {
				d.logger.Info("regex match container id failed", "pattern",
					regexRequirements[j].ContainerIDRegex, "containerID", containerID)
				continue
			}
			if !matched {
				continue
			}

			pod.Spec.Containers[i].Resources = regexRequirements[j].ResourcesRaw
			break
		}
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

func proxyCABundleConfigMapMountPath() string {
	return "/managed/proxy-ca"
}

func proxyCABundleConfigMapName(addonName string) string {
	return fmt.Sprintf("%s-proxy-ca", addonName)
}

func proxyCABundleConfigMapDataKey() string {
	return "ca-bundle.crt"
}

func proxyCABundleFilePath() string {
	return path.Join(proxyCABundleConfigMapMountPath(), proxyCABundleConfigMapDataKey())
}
