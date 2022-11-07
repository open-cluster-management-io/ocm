package managedclustermutating

import (
	"context"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apiserver/pkg/admission"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/generic"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/request"
	clusterv1api "open-cluster-management.io/api/cluster/v1"
	clusterwebhookv1 "open-cluster-management.io/registration/pkg/webhook/v1"
	runtimeadmission "sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	admissionutil "open-cluster-management.io/ocm-controlplane/plugin/admission/util"
)

const PluginName = "ManagedClusterMutating"

func Register(plugins *admission.Plugins) {
	plugins.Register(PluginName, func(config io.Reader) (admission.Interface, error) {
		return NewPlugin(), nil
	})
}

type Plugin struct {
	*admission.Handler
	webhook *clusterwebhookv1.ManagedClusterWebhook
}

func (p *Plugin) ValidateInitialization() error {
	return nil
}

var _ admission.MutationInterface = &Plugin{}
var _ admission.InitializationValidator = &Plugin{}

func NewPlugin() *Plugin {
	return &Plugin{
		Handler: admission.NewHandler(admission.Create, admission.Update),
		webhook: &clusterwebhookv1.ManagedClusterWebhook{},
	}
}

func (p *Plugin) Admit(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) error {
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

	// we just need gvr in code logic
	i := generic.WebhookInvocation{
		Kind:     gvk,
		Resource: gvr,
	}

	uid := types.UID(uuid.NewUUID())
	ar := request.CreateV1AdmissionReview(uid, &v, &i)

	old := a.GetOldObject()
	oldRaw := runtime.RawExtension{}
	err := admissionutil.Convert_runtime_Object_To_runtime_RawExtension_Raw(&old, &oldRaw)
	if err != nil {
		return fmt.Errorf("error occured in ManagedClusterMutating: failed to convert Object to RawExtension.")
	}
	ar.Request.OldObject = oldRaw

	r := runtimeadmission.Request{AdmissionRequest: *ar.Request}
	admissionContext := runtimeadmission.NewContextWithRequest(ctx, r)
	if err := p.webhook.Default(admissionContext, a.GetObject()); err != nil {
		return err
	}

	return nil
}
