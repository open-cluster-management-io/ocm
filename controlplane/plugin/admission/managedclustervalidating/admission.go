package managedclustervalidating

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
	clusterv1api "open-cluster-management.io/api/cluster/v1"
	clusterwebhookv1 "open-cluster-management.io/registration/pkg/webhook/v1"
	runtimeadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const PluginName = "ManagedClusterValidating"

func Register(plugins *admission.Plugins) {
	plugins.Register(PluginName, func(config io.Reader) (admission.Interface, error) {
		return NewPlugin(), nil
	})
}

type Plugin struct {
	*admission.Handler
	webhook *clusterwebhookv1.ManagedClusterWebhook
}

func (p *Plugin) SetExternalKubeClientSet(client kubernetes.Interface) {
	p.webhook.SetExternalKubeClientSet(client)
}

func (p *Plugin) ValidateInitialization() error {
	if p.webhook == nil {
		return fmt.Errorf("missing webhook")
	}
	return nil
}

var _ admission.ValidationInterface = &Plugin{}
var _ admission.InitializationValidator = &Plugin{}
var _ = genericadmissioninitializer.WantsExternalKubeClientSet(&Plugin{})

func NewPlugin() *Plugin {
	return &Plugin{
		Handler: admission.NewHandler(admission.Create, admission.Update),
		webhook: &clusterwebhookv1.ManagedClusterWebhook{},
	}
}

func (p *Plugin) Validate(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) error {
	v := generic.VersionedAttributes{
		Attributes:         a,
		VersionedOldObject: a.GetOldObject(),
		VersionedObject:    a.GetObject(),
		VersionedKind:      a.GetKind(),
	}

	gvr := clusterv1api.GroupVersion.WithResource("managedclusters")
	gvk := clusterv1api.GroupVersion.WithKind("ManagedCluster")

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

	r := runtimeadmission.Request{AdmissionRequest: *ar.Request}
	admissionContext := runtimeadmission.NewContextWithRequest(ctx, r)

	cluster := &clusterv1api.ManagedCluster{}
	obj := a.GetObject().(*unstructured.Unstructured)
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, cluster)
	if err != nil {
		return err
	}

	switch a.GetOperation() {
	case admission.Create:
		return p.webhook.ValidateCreate(admissionContext, cluster)
	case admission.Update:
		oldCluster := &clusterv1api.ManagedCluster{}
		oldObj := a.GetOldObject().(*unstructured.Unstructured)
		err := runtime.DefaultUnstructuredConverter.FromUnstructured(oldObj.Object, oldCluster)
		if err != nil {
			return err
		}
		return p.webhook.ValidateUpdate(admissionContext, oldCluster, cluster)
	}

	return nil
}
