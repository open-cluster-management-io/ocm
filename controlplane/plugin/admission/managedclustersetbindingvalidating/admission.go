package managedclustersetbindingvalidating

import (
	"context"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apiserver/pkg/admission"
	genericadmissioninitializer "k8s.io/apiserver/pkg/admission/initializer"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/generic"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/request"
	"k8s.io/client-go/kubernetes"
	clusterv1beta2api "open-cluster-management.io/api/cluster/v1beta2"
	webhookv1beta2 "open-cluster-management.io/registration/pkg/webhook/v1beta2"
	runtimeadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const PluginName = "ManagedClusterSetBindingValidating"

func Register(plugins *admission.Plugins) {
	plugins.Register(PluginName, func(config io.Reader) (admission.Interface, error) {
		return NewPlugin(), nil
	})
}

type Plugin struct {
	*admission.Handler
	webhook *webhookv1beta2.ManagedClusterSetBindingWebhook
}

func (p *Plugin) SetExternalKubeClientSet(client kubernetes.Interface) {
	p.webhook.SetExternalKubeClientSet(client)
}

func (p *Plugin) ValidateInitialization() error {
	if p.webhook == nil {
		return fmt.Errorf("missing admission")
	}
	return nil
}

var _ admission.ValidationInterface = &Plugin{}
var _ admission.InitializationValidator = &Plugin{}
var _ = genericadmissioninitializer.WantsExternalKubeClientSet(&Plugin{})

func NewPlugin() *Plugin {
	return &Plugin{
		Handler: admission.NewHandler(admission.Create, admission.Update),
		webhook: &webhookv1beta2.ManagedClusterSetBindingWebhook{},
	}
}

func (p *Plugin) Validate(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) error {
	v := generic.VersionedAttributes{
		Attributes:         a,
		VersionedOldObject: a.GetOldObject(),
		VersionedObject:    a.GetObject(),
		VersionedKind:      a.GetKind(),
	}

	gvr := clusterv1beta2api.GroupVersion.WithResource("managedclustersetbindings")
	gvk := clusterv1beta2api.GroupVersion.WithKind("ManagedClusterSetBinding")

	// resource is not mcl
	if a.GetKind() != gvk {
		return nil
	}

	// don't set kind cause do not use it in code logical
	i := generic.WebhookInvocation{
		Resource: gvr,
		Kind:     gvk,
	}

	uid := types.UID(uuid.NewUUID())
	ar := request.CreateV1AdmissionReview(uid, &v, &i)

	binding := &clusterv1beta2api.ManagedClusterSetBinding{}
	obj := a.GetObject().(*unstructured.Unstructured)
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, binding)
	if err != nil {
		return err
	}

	r := runtimeadmission.Request{AdmissionRequest: *ar.Request}
	admissionContext := runtimeadmission.NewContextWithRequest(ctx, r)
	switch a.GetOperation() {
	case admission.Create:
		return p.webhook.ValidateCreate(admissionContext, binding)
	case admission.Update:
		oldBinding := &clusterv1beta2api.ManagedClusterSetBinding{}
		oldObj := a.GetOldObject().(*unstructured.Unstructured)
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(oldObj.Object, oldBinding)
		if err != nil {
			return err
		}
		return p.webhook.ValidateUpdate(admissionContext, oldBinding, binding)
	}

	return nil
}
