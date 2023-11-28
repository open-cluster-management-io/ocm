package watcher

import (
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"
)

// ManifestWorkWatcher implements the watch.Interface. It returns a chan which will receive all the events.
type ManifestWorkWatcher struct {
	sync.Mutex

	result chan watch.Event
	done   chan struct{}
}

var _ watch.Interface = &ManifestWorkWatcher{}

func NewManifestWorkWatcher() *ManifestWorkWatcher {
	mw := &ManifestWorkWatcher{
		// It's easy for a consumer to add buffering via an extra
		// goroutine/channel, but impossible for them to remove it,
		// so nonbuffered is better.
		result: make(chan watch.Event),
		// If the watcher is externally stopped there is no receiver anymore
		// and the send operations on the result channel, especially the
		// error reporting might block forever.
		// Therefore a dedicated stop channel is used to resolve this blocking.
		done: make(chan struct{}),
	}

	return mw
}

// ResultChan implements Interface.
func (mw *ManifestWorkWatcher) ResultChan() <-chan watch.Event {
	return mw.result
}

// Stop implements Interface.
func (mw *ManifestWorkWatcher) Stop() {
	// Call Close() exactly once by locking and setting a flag.
	mw.Lock()
	defer mw.Unlock()
	// closing a closed channel always panics, therefore check before closing
	select {
	case <-mw.done:
		close(mw.result)
	default:
		close(mw.done)
	}
}

// Receive a event from the work client and sends down the result channel.
func (mw *ManifestWorkWatcher) Receive(evt watch.Event) {
	if klog.V(4).Enabled() {
		obj, _ := meta.Accessor(evt.Object)
		klog.V(4).Infof("Receive the event %v for %v", evt.Type, obj.GetName())
	}

	mw.result <- evt
}
