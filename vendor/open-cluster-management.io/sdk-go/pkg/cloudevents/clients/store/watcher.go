package store

import (
	"sync"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/klog/v2"
)

// Watcher implements the watch.Interface.
type Watcher struct {
	sync.RWMutex

	result  chan watch.Event
	done    chan struct{}
	stopped bool
}

var _ watch.Interface = &Watcher{}

func NewWatcher() *Watcher {
	return &Watcher{
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
}

// ResultChan implements Interface.
func (w *Watcher) ResultChan() <-chan watch.Event {
	return w.result
}

// Stop implements Interface.
func (w *Watcher) Stop() {
	// Call Close() exactly once by locking and setting a flag.
	w.Lock()
	defer w.Unlock()
	// closing a closed channel always panics, therefore check before closing
	select {
	case <-w.done:
		close(w.result)
	default:
		w.stopped = true
		close(w.done)
	}
}

// Receive a event from the work client and sends down the result channel.
func (w *Watcher) Receive(evt watch.Event) {
	if w.isStopped() {
		// this watcher is stopped, do nothing.
		return
	}

	if klog.V(4).Enabled() {
		obj, _ := meta.Accessor(evt.Object)
		klog.V(4).Infof("Receive the event %v for %v", evt.Type, obj.GetName())
	}

	w.result <- evt
}

func (w *Watcher) isStopped() bool {
	w.RLock()
	defer w.RUnlock()

	return w.stopped
}
