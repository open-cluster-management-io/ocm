package grpc

import (
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"

	addonv1beta1 "open-cluster-management.io/api/addon/v1beta1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

type fakeCloser struct {
	clusters []string
	addOns   []string
}

func (f *fakeCloser) CloseCluster(cluster string) int {
	f.clusters = append(f.clusters, cluster)
	return 1
}

func (f *fakeCloser) CloseAddOn(cluster, addon string) int {
	f.addOns = append(f.addOns, cluster+"/"+addon)
	return 1
}

func managedCluster(name string, accepted, deleting bool) *clusterv1.ManagedCluster {
	c := &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: name},
		Spec:       clusterv1.ManagedClusterSpec{HubAcceptsClient: accepted},
	}
	if deleting {
		now := metav1.Now()
		c.DeletionTimestamp = &now
	}
	return c
}

func TestClusterRevocationHandler(t *testing.T) {
	cases := []struct {
		name       string
		old, new   *clusterv1.ManagedCluster
		wantClosed []string
	}{
		{"deny", managedCluster("cluster1", true, false), managedCluster("cluster1", false, false), []string{"cluster1"}},
		{"accept", managedCluster("cluster1", false, false), managedCluster("cluster1", true, false), nil},
		{"never accepted: status update", managedCluster("cluster3", false, false), managedCluster("cluster3", false, false), nil},
		{"accepted: status update", managedCluster("cluster1", true, false), managedCluster("cluster1", true, false), nil},
		{"delete starts", managedCluster("cluster1", true, false), managedCluster("cluster1", true, true), []string{"cluster1"}},
		{"never accepted, delete starts", managedCluster("cluster3", false, false), managedCluster("cluster3", false, true), []string{"cluster3"}},
		{"already deleting", managedCluster("cluster1", true, true), managedCluster("cluster1", true, true), nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			closer := &fakeCloser{}
			clusterRevocationHandler(closer).OnUpdate(c.old, c.new)
			require.Equal(t, c.wantClosed, closer.clusters)
		})
	}

	t.Run("deleted", func(t *testing.T) {
		closer := &fakeCloser{}
		clusterRevocationHandler(closer).OnDelete(managedCluster("cluster1", true, false))
		require.Equal(t, []string{"cluster1"}, closer.clusters)
	})
	t.Run("deleted, tombstone", func(t *testing.T) {
		closer := &fakeCloser{}
		clusterRevocationHandler(closer).OnDelete(cache.DeletedFinalStateUnknown{
			Key: "cluster1", Obj: managedCluster("cluster1", true, false)})
		require.Equal(t, []string{"cluster1"}, closer.clusters)
	})
	t.Run("unexpected object", func(t *testing.T) {
		closer := &fakeCloser{}
		h := clusterRevocationHandler(closer)
		h.OnUpdate("x", "y")
		h.OnDelete("x")
		require.Empty(t, closer.clusters)
	})
}

func TestAddOnRevocationHandler(t *testing.T) {
	addon := func(deleting bool) *addonv1beta1.ManagedClusterAddOn {
		a := &addonv1beta1.ManagedClusterAddOn{ObjectMeta: metav1.ObjectMeta{Namespace: "cluster1", Name: "my-addon"}}
		if deleting {
			now := metav1.Now()
			a.DeletionTimestamp = &now
		}
		return a
	}

	closer := &fakeCloser{}
	h := addOnRevocationHandler(closer)
	h.OnUpdate(addon(false), addon(false))
	require.Empty(t, closer.addOns, "a status update closes nothing")
	h.OnUpdate(addon(false), addon(true))
	require.Equal(t, []string{"cluster1/my-addon"}, closer.addOns)
	h.OnUpdate(addon(true), addon(true))
	require.Len(t, closer.addOns, 1, "already deleting")
	h.OnDelete(cache.DeletedFinalStateUnknown{Key: "cluster1/my-addon", Obj: addon(true)})
	require.Equal(t, []string{"cluster1/my-addon", "cluster1/my-addon"}, closer.addOns)
	require.Empty(t, closer.clusters)
}

// A stopped informer refuses new handlers: the error is returned.
func TestRegisterRevocationHandlers(t *testing.T) {
	newInformer := func(stopped bool) cache.SharedIndexInformer {
		lw := &cache.ListWatch{
			ListFunc:  func(metav1.ListOptions) (runtime.Object, error) { return &clusterv1.ManagedClusterList{}, nil },
			WatchFunc: func(metav1.ListOptions) (watch.Interface, error) { return watch.NewFake(), nil },
		}
		i := cache.NewSharedIndexInformer(lw, &clusterv1.ManagedCluster{}, 0, cache.Indexers{})
		if stopped {
			stop := make(chan struct{})
			close(stop)
			i.Run(stop)
		}
		return i
	}
	if err := registerRevocationHandlers(newInformer(false), newInformer(false), &fakeCloser{}); err != nil {
		t.Errorf("running informers: %v", err)
	}
	if err := registerRevocationHandlers(newInformer(true), newInformer(false), &fakeCloser{}); err == nil {
		t.Error("no error from a stopped cluster informer")
	}
	if err := registerRevocationHandlers(newInformer(false), newInformer(true), &fakeCloser{}); err == nil {
		t.Error("no error from a stopped add-on informer")
	}
}
