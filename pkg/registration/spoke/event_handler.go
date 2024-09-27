package spoke

import (
	"context"
	"fmt"
	"reflect"

	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type bootstrapKubeconfigEventHandler struct {
	bootstrapKubeconfigSecretName *string
	// The cancel function allows the agent to restart immediately if the bootstrap kubeconfig changes.
	// It should be point to the root context of the agent.
	cancel context.CancelFunc
}

// implement cache.ResourceEventHandler.OnAdd
func (hc *bootstrapKubeconfigEventHandler) OnAdd(obj interface{}, isInInitialList bool) {
}

// implement cache.ResourceEventHandler.OnUpdate
func (hc *bootstrapKubeconfigEventHandler) OnUpdate(oldObj, newObj interface{}) {
	newSecret, ok := newObj.(*corev1.Secret)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("invalid secret object: %v", newObj))
		return
	}

	if newSecret.Name != *hc.bootstrapKubeconfigSecretName {
		return
	}

	oldSecret, ok := oldObj.(*corev1.Secret)
	if !ok {
		utilruntime.HandleError(fmt.Errorf("invalid secret object: %v", oldObj))
		return
	}

	if !reflect.DeepEqual(newSecret.Data, oldSecret.Data) {
		// Restart immediately if the bootstrap kubeconfig changes. Otherwise,
		// the work agent may resync a wrong bootstrap kubeconfig from the cache.
		klog.Info("the bootstrap kubeconfig changes and rebootstrap is required, cancel the context")
		hc.cancel()
	}
}

// implement cache.ResourceEventHandler.OnDelete
func (hc *bootstrapKubeconfigEventHandler) OnDelete(obj interface{}) {
	switch t := obj.(type) {
	case *corev1.Secret:
		if t.Name == *hc.bootstrapKubeconfigSecretName {
			klog.Info("the bootstrap kubeconfig deletes and rebootstrap is required, cancel the context")
			hc.cancel()
		}
	case cache.DeletedFinalStateUnknown:
		secret, ok := t.Obj.(*corev1.Secret)
		if ok && secret.Name == *hc.bootstrapKubeconfigSecretName {
			klog.Info("the bootstrap kubeconfig deletes and rebootstrap is required, cancel the context")
			hc.cancel()
		}
	}
}
