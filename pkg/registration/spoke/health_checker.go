package spoke

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"sync"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
)

type hubKubeConfigHealthChecker struct {
	checkFunc    wait.ConditionWithContextFunc
	bootstrapped bool
	lock         sync.RWMutex
}

func (hc *hubKubeConfigHealthChecker) Name() string {
	return "hub-kube-config"
}

// clientCertHealthChecker returns an error when the client certificate created for the
// hub kubeconfig exists and expires.
func (hc *hubKubeConfigHealthChecker) Check(_ *http.Request) error {
	hc.lock.RLock()
	defer hc.lock.RUnlock()
	if !hc.bootstrapped {
		return nil
	}
	valid, err := hc.checkFunc(context.Background())
	if err != nil {
		return err
	}
	if !valid {
		return errors.New("the client certificate expires and rebootstrap is required.")
	}
	return nil
}

func (hc *hubKubeConfigHealthChecker) setBootstrapped() {
	hc.lock.Lock()
	defer hc.lock.Unlock()
	hc.bootstrapped = true
}

type bootstrapKubeconfigHealthChecker struct {
	bootstrapKubeconfigSecretName *string
	changed                       bool
}

func (hc *bootstrapKubeconfigHealthChecker) Name() string {
	return "bootstrap-hub-kubeconfig"
}

// bootstrapKubeconfigHealthChecker returns an error when the bootstrap kubeconfig changes.
func (hc *bootstrapKubeconfigHealthChecker) Check(_ *http.Request) error {
	if hc.changed {
		return errors.New("the bootstrap kubeconfig changes and rebootstrap is required.")
	}
	return nil
}

// implement cache.ResourceEventHandler.OnAdd
func (hc *bootstrapKubeconfigHealthChecker) OnAdd(obj interface{}, isInInitialList bool) {
}

// implement cache.ResourceEventHandler.OnUpdate
func (hc *bootstrapKubeconfigHealthChecker) OnUpdate(oldObj, newObj interface{}) {
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
		hc.changed = true
	}
}

// implement cache.ResourceEventHandler.OnDelete
func (hc *bootstrapKubeconfigHealthChecker) OnDelete(obj interface{}) {
	switch t := obj.(type) {
	case *corev1.Secret:
		if t.Name == *hc.bootstrapKubeconfigSecretName {
			hc.changed = true
		}
	case cache.DeletedFinalStateUnknown:
		secret, ok := t.Obj.(*corev1.Secret)
		if ok && secret.Name == *hc.bootstrapKubeconfigSecretName {
			hc.changed = true
		}
	}
}
