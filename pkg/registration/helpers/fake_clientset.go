package helpers

import (
	"fmt"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/discovery"
	fakediscovery "k8s.io/client-go/discovery/fake"
	"k8s.io/client-go/testing"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clientset "sigs.k8s.io/about-api/pkg/generated/clientset/versioned"
	aboutscheme "sigs.k8s.io/about-api/pkg/generated/clientset/versioned/fake"
	aboutv1alpha1 "sigs.k8s.io/about-api/pkg/generated/clientset/versioned/typed/apis/v1alpha1"
	fakeaboutv1alpha1 "sigs.k8s.io/about-api/pkg/generated/clientset/versioned/typed/apis/v1alpha1/fake"
)

// scheme is a test-specific scheme that includes both about-api and cluster types
var scheme = runtime.NewScheme()
var codecs = serializer.NewCodecFactory(scheme)

func init() {
	// Add the about-api types from the generated scheme
	if err := aboutscheme.AddToScheme(scheme); err != nil {
		panic(err)
	}
	// Add the ManagedCluster types from open-cluster-management.io
	if err := clusterv1.AddToScheme(scheme); err != nil {
		panic(err)
	}
}

// NewFakeClientset returns a clientset that will respond with the provided objects.
// This is a test helper that extends the sigs.k8s.io/about-api fake clientset to support ManagedCluster.
func NewFakeClientset(objects ...runtime.Object) *Clientset {
	o := testing.NewObjectTracker(scheme, codecs.UniversalDecoder())
	for _, obj := range objects {
		fmt.Printf("Obj: %+v\n", obj)
		if err := o.Add(obj); err != nil {
			panic(err)
		}
	}

	cs := &Clientset{tracker: o}
	cs.discovery = &fakediscovery.FakeDiscovery{Fake: &cs.Fake}
	cs.AddReactor("*", "*", testing.ObjectReaction(o))
	cs.AddWatchReactor("*", func(action testing.Action) (handled bool, ret watch.Interface, err error) {
		gvr := action.GetResource()
		ns := action.GetNamespace()
		watch, err := o.Watch(gvr, ns)
		if err != nil {
			return false, nil, err
		}
		return true, watch, nil
	})

	return cs
}

// Clientset implements clientset.Interface. Meant to be embedded into a
// struct to get a default implementation. This makes faking out just the method
// you want to test easier.
type Clientset struct {
	testing.Fake
	discovery *fakediscovery.FakeDiscovery
	tracker   testing.ObjectTracker
}

func (c *Clientset) Discovery() discovery.DiscoveryInterface {
	return c.discovery
}

func (c *Clientset) Tracker() testing.ObjectTracker {
	return c.tracker
}

var (
	_ clientset.Interface = &Clientset{}
	_ testing.FakeClient  = &Clientset{}
)

// AboutV1alpha1 retrieves the AboutV1alpha1Client
func (c *Clientset) AboutV1alpha1() aboutv1alpha1.AboutV1alpha1Interface {
	return &fakeaboutv1alpha1.FakeAboutV1alpha1{Fake: &c.Fake}
}
