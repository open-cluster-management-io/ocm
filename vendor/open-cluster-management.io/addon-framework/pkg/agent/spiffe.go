package agent

import (
	"context"
	"fmt"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/sets"
	"k8s.io/klog/v2"

	addonapiv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

const (
	// SPIFFEDriver is the add-on driver with which each add-on agent authenticates to the hub's
	// SPIFFE listener with its own SPIRE-issued SVID.
	SPIFFEDriver = "spiffe"
	// SPIFFERoleLabel tells the platform's SPIRE entries which agent a pod is. Add-on pods carry
	// the role addon; the Klusterlet's admission policy pairs it with their ServiceAccount.
	SPIFFERoleLabel = "open-cluster-management.io/spiffe-role"
	// SPIFFEAddOnNamespaceLabel marks a dedicated add-on namespace of the Klusterlet named in its
	// value. Only that Klusterlet's work agent may create workloads there.
	SPIFFEAddOnNamespaceLabel = "open-cluster-management.io/spiffe-addon-namespace"
	// KlusterletNameAnnotation is the ManagedCluster annotation in which a spiffe registration
	// agent reports its Klusterlet's name.
	KlusterletNameAnnotation = "agent.open-cluster-management.io/klusterlet-name"
	// SPIFFEWorkloadAPIVolume is the volume with the local SPIRE agent's Workload API socket.
	SPIFFEWorkloadAPIVolume = "spiffe-workload-api"
	// SPIFFEWorkloadAPIMountPath is where it is mounted.
	SPIFFEWorkloadAPIMountPath = "/spiffe-workload-api"
)

// SPIFFEConfigMapName is the ConfigMap the registration agent writes in the add-on's install
// namespace with the add-on agent's hub settings and SPIFFE ID. It replaces the
// <addon>-hub-kubeconfig Secret; nothing in it is secret.
func SPIFFEConfigMapName(addonName string) string { return addonName + "-spiffe" }

// UsesSPIFFE reports whether the add-on's kubeClient registration uses the spiffe driver on its
// cluster, as the registration agent reports in status.registrations.
func UsesSPIFFE(addon *addonapiv1beta1.ManagedClusterAddOn) bool {
	for _, reg := range addon.Status.Registrations {
		if reg.Type == addonapiv1beta1.KubeClient && reg.KubeClient != nil && reg.KubeClient.Driver == SPIFFEDriver {
			return true
		}
	}
	return false
}

// ApplySPIFFE adapts an add-on's manifests to a cluster whose add-ons use the spiffe driver:
//   - in every Deployment, DaemonSet and StatefulSet, the <addon>-hub-kubeconfig Secret volume
//     becomes the <addon>-spiffe ConfigMap (same volume name and mounts, so the agent finds its
//     settings where it found the kubeconfig), every container mounts the Workload API, and the pod
//     template gets the spiffe-role addon label;
//   - every Namespace gets the dedicated add-on namespace label with the Klusterlet's name, which
//     the cluster reports in its klusterlet-name annotation.
func ApplySPIFFE(objects []runtime.Object, cluster *clusterv1.ManagedCluster, addonName string) ([]runtime.Object, error) {
	klusterlet := ""
	if cluster != nil {
		klusterlet = cluster.Annotations[KlusterletNameAnnotation]
	}
	out := make([]runtime.Object, 0, len(objects))
	for _, obj := range objects {
		adapted, err := applySPIFFE(obj, klusterlet, addonName)
		if err != nil {
			return nil, err
		}
		out = append(out, adapted)
	}
	return out, nil
}

func applySPIFFE(obj runtime.Object, klusterlet, addonName string) (runtime.Object, error) {
	if u, ok := obj.(*unstructured.Unstructured); ok {
		typed, err := typedFor(u)
		if err != nil || typed == nil {
			return obj, err
		}
		// A typed object is always adapted without an error.
		adapted, _ := applySPIFFE(typed, klusterlet, addonName)
		content, err := toUnstructured(adapted)
		if err != nil {
			return nil, err
		}
		return &unstructured.Unstructured{Object: content}, nil
	}
	switch o := obj.(type) {
	case *appsv1.Deployment:
		o = o.DeepCopy()
		applySPIFFEToPod(&o.Spec.Template, addonName)
		return o, nil
	case *appsv1.DaemonSet:
		o = o.DeepCopy()
		applySPIFFEToPod(&o.Spec.Template, addonName)
		return o, nil
	case *appsv1.StatefulSet:
		o = o.DeepCopy()
		applySPIFFEToPod(&o.Spec.Template, addonName)
		return o, nil
	case *corev1.Namespace:
		if klusterlet == "" {
			klog.Warningf("add-on %s: the cluster doesn't report its Klusterlet name; namespace %s is not labeled as a dedicated add-on namespace",
				addonName, o.Name)
			return o, nil
		}
		o = o.DeepCopy()
		if o.Labels == nil {
			o.Labels = map[string]string{}
		}
		o.Labels[SPIFFEAddOnNamespaceLabel] = klusterlet
		return o, nil
	}
	return obj, nil
}

// toUnstructured converts an adapted object back; replaceable in tests.
var toUnstructured = runtime.DefaultUnstructuredConverter.ToUnstructured

func typedFor(u *unstructured.Unstructured) (runtime.Object, error) {
	var typed runtime.Object
	switch u.GroupVersionKind().GroupKind().String() {
	case "Deployment.apps":
		typed = &appsv1.Deployment{}
	case "DaemonSet.apps":
		typed = &appsv1.DaemonSet{}
	case "StatefulSet.apps":
		typed = &appsv1.StatefulSet{}
	case "Namespace":
		typed = &corev1.Namespace{}
	default:
		return nil, nil
	}
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, typed); err != nil {
		return nil, fmt.Errorf("failed to convert %s %s: %w", u.GetKind(), u.GetName(), err)
	}
	return typed, nil
}

func applySPIFFEToPod(t *corev1.PodTemplateSpec, addonName string) {
	if t.Labels == nil {
		t.Labels = map[string]string{}
	}
	t.Labels[SPIFFERoleLabel] = "addon"

	hubSecret := addonName + "-hub-kubeconfig"
	for i := range t.Spec.Volumes {
		v := &t.Spec.Volumes[i]
		if v.Secret != nil && v.Secret.SecretName == hubSecret {
			v.VolumeSource = corev1.VolumeSource{ConfigMap: &corev1.ConfigMapVolumeSource{
				LocalObjectReference: corev1.LocalObjectReference{Name: SPIFFEConfigMapName(addonName)}}}
		}
	}
	names := sets.New[string]()
	for _, v := range t.Spec.Volumes {
		names.Insert(v.Name)
	}
	if !names.Has(SPIFFEWorkloadAPIVolume) {
		readOnly := true
		t.Spec.Volumes = append(t.Spec.Volumes, corev1.Volume{Name: SPIFFEWorkloadAPIVolume,
			VolumeSource: corev1.VolumeSource{CSI: &corev1.CSIVolumeSource{Driver: "csi.spiffe.io", ReadOnly: &readOnly}}})
	}
	for i := range t.Spec.Containers {
		c := &t.Spec.Containers[i]
		mounted := false
		for _, m := range c.VolumeMounts {
			mounted = mounted || m.Name == SPIFFEWorkloadAPIVolume
		}
		if !mounted {
			c.VolumeMounts = append(c.VolumeMounts, corev1.VolumeMount{
				Name: SPIFFEWorkloadAPIVolume, MountPath: SPIFFEWorkloadAPIMountPath, ReadOnly: true})
		}
	}
}

// spiffeAgentAddon applies ApplySPIFFE to an add-on's manifests on clusters that use the spiffe
// add-on driver.
type spiffeAgentAddon struct {
	AgentAddon
}

// WithSPIFFE wraps agentAddon so that its manifests are adapted to clusters whose add-ons use the
// spiffe driver. The add-on manager wraps every add-on with it.
func WithSPIFFE(agentAddon AgentAddon) AgentAddon {
	if _, ok := agentAddon.(*spiffeAgentAddon); ok {
		return agentAddon
	}
	return &spiffeAgentAddon{AgentAddon: agentAddon}
}

func (a *spiffeAgentAddon) Manifests(ctx context.Context, cluster *clusterv1.ManagedCluster,
	addon *addonapiv1beta1.ManagedClusterAddOn) ([]runtime.Object, error) {
	objects, err := a.AgentAddon.Manifests(ctx, cluster, addon)
	if err != nil {
		return objects, err
	}
	if UsesSPIFFE(addon) {
		return ApplySPIFFE(objects, cluster, addon.Name)
	}
	waiting, err := a.awaitingDriver(ctx, cluster, addon)
	if err != nil {
		return nil, err
	}
	if waiting {
		return nil, fmt.Errorf("waiting for the registration agent of cluster %s to report the driver of add-on %s",
			cluster.Name, addon.Name)
	}
	return objects, nil
}

// awaitingDriver reports whether the add-on's manifests must wait. On a cluster whose Klusterlet
// uses spiffe (its registration agent reports the Klusterlet's name in KlusterletNameAnnotation),
// an add-on with a kubeClient registration is rendered only once the registration agent has
// reported that registration's driver. Rendered before, it would mount a hub kubeconfig Secret
// that is never created, and its namespace would be created without the dedicated add-on label,
// which the admission policy allows only when a namespace is created.
func (a *spiffeAgentAddon) awaitingDriver(ctx context.Context, cluster *clusterv1.ManagedCluster,
	addon *addonapiv1beta1.ManagedClusterAddOn) (bool, error) {
	if cluster == nil || cluster.Annotations[KlusterletNameAnnotation] == "" {
		return false, nil
	}
	for _, reg := range addon.Status.Registrations {
		if reg.Type == addonapiv1beta1.KubeClient {
			return reg.KubeClient == nil || reg.KubeClient.Driver == "", nil
		}
	}
	// No kubeClient registration in the status yet: wait if the add-on will have one.
	registration := a.GetAgentAddonOptions().Registration
	if registration == nil || registration.Configurations == nil {
		return false, nil
	}
	configs, err := registration.Configurations(ctx, cluster, addon)
	if err != nil {
		return false, err
	}
	for _, c := range configs {
		if c.RegistrationAPI().Type == addonapiv1beta1.KubeClient {
			return true, nil
		}
	}
	return false, nil
}

// SubjectCarriedBySVID reports whether the add-on's kubeClient subject is one an add-on agent's
// SVID maps to on the hub: user system:open-cluster-management:cluster:<cluster>:addon:<addon>:agent:<agent>,
// and only the add-on's groups.
func SubjectCarriedBySVID(cluster, addon string, s addonapiv1beta1.KubeClientSubject) bool {
	prefix := fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:%s:agent:", cluster, addon)
	agentName, ok := strings.CutPrefix(s.User, prefix)
	if !ok || agentName == "" || strings.Contains(agentName, ":") {
		return false
	}
	allowed := sets.New(
		fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:%s", cluster, addon),
		"system:open-cluster-management:addon:"+addon,
		"system:authenticated",
	)
	return allowed.HasAll(s.Groups...)
}
