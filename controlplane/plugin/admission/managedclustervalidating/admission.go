package managedclustervalidating

import (
	"context"
	"fmt"
	"io"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/apiserver/pkg/admission"
	genericadmissioninitializer "k8s.io/apiserver/pkg/admission/initializer"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/generic"
	"k8s.io/apiserver/pkg/admission/plugin/webhook/request"
	"k8s.io/client-go/kubernetes"
	clusterv1api "open-cluster-management.io/api/cluster/v1"
	clusterwebhook "open-cluster-management.io/registration/pkg/webhook/cluster"

	admissionutil "open-cluster-management.io/ocm-controlplane/plugin/admission/util"
)

const PluginName = "ManagedClusterValidating"

func Register(plugins *admission.Plugins) {
	plugins.Register(PluginName, func(config io.Reader) (admission.Interface, error) {
		return NewPlugin(), nil
	})
}

type Plugin struct {
	*admission.Handler
	client kubernetes.Interface
}

func (p *Plugin) SetExternalKubeClientSet(client kubernetes.Interface) {
	p.client = client
}

func (p *Plugin) ValidateInitialization() error {
	if p.client == nil {
		return fmt.Errorf("missing client")
	}
	return nil
}

var _ admission.ValidationInterface = &Plugin{}
var _ admission.InitializationValidator = &Plugin{}
var _ = genericadmissioninitializer.WantsExternalKubeClientSet(&Plugin{})

func NewPlugin() *Plugin {
	return &Plugin{
		Handler: admission.NewHandler(admission.Create, admission.Update),
	}
}

func (p *Plugin) Validate(ctx context.Context, a admission.Attributes, o admission.ObjectInterfaces) error {
	var mcv = clusterwebhook.ManagedClusterValidatingAdmissionHook{}
	mcv.SetKubeClient(p.client)

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

	res := mcv.Validate(ar.Request)

	if !res.Allowed {
		return fmt.Errorf("error occured in ManagedClusterValidating: [%d] %s", res.Result.Code, res.Result.Message)
	}

	return nil
}
