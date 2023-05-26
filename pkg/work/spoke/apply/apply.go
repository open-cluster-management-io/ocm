package apply

import (
	"context"

	"github.com/openshift/library-go/pkg/operator/events"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

type Applier interface {
	Apply(ctx context.Context,
		gvr schema.GroupVersionResource,
		required *unstructured.Unstructured,
		owner metav1.OwnerReference,
		applyOption *workapiv1.ManifestConfigOption,
		recorder events.Recorder) (runtime.Object, error)
}

type Appliers struct {
	appliers map[workapiv1.UpdateStrategyType]Applier
}

func NewAppliers(dynamicClient dynamic.Interface, kubeclient kubernetes.Interface, apiExtensionClient apiextensionsclient.Interface) *Appliers {
	return &Appliers{
		appliers: map[workapiv1.UpdateStrategyType]Applier{
			workapiv1.UpdateStrategyTypeCreateOnly:      NewCreateOnlyApply(dynamicClient),
			workapiv1.UpdateStrategyTypeServerSideApply: NewServerSideApply(dynamicClient),
			workapiv1.UpdateStrategyTypeUpdate:          NewUpdateApply(dynamicClient, kubeclient, apiExtensionClient),
		},
	}
}

func (a *Appliers) GetApplier(strategy workapiv1.UpdateStrategyType) Applier {
	return a.appliers[strategy]
}
