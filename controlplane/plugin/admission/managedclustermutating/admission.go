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
	clusterwebhook "open-cluster-management.io/registration/pkg/webhook/cluster"

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
}

func (p *Plugin) ValidateInitialization() error {
	return nil
}

var _ admission.MutationInterface = &Plugin{}
var _ admission.InitializationValidator = &Plugin{}

func NewPlugin() *Plugin {
	return &Plugin{
		Handler: admission.NewHandler(admission.Create, admission.Update),
	}
}

func (p *Plugin) Admit(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) error {
	var mcm clusterwebhook.ManagedClusterMutatingAdmissionHook

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
	ar := request.CreateV1beta1AdmissionReview(uid, &v, &i)

	obj := a.GetObject()
	raw := runtime.RawExtension{}
	err := admissionutil.Convert_runtime_Object_To_runtime_RawExtension_Raw(&obj, &raw)
	if err != nil {
		return fmt.Errorf("error occured in ManagedClusterMutating: failed to convert Object to RawExtension.")
	}
	ar.Request.Object = raw

	old := a.GetOldObject()
	oldRaw := runtime.RawExtension{}
	err = admissionutil.Convert_runtime_Object_To_runtime_RawExtension_Raw(&old, &oldRaw)
	if err != nil {
		return fmt.Errorf("error occured in ManagedClusterMutating: failed to convert Object to RawExtension.")
	}
	ar.Request.OldObject = oldRaw

	ar.Response = mcm.Admit(ar.Request)

	if !ar.Response.Allowed {
		return fmt.Errorf("error occured in ManagedClusterMutating: [%d] %s", ar.Response.Result.Code, ar.Response.Result.Message)
	}
	return nil
}
