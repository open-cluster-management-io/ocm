package spoke

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"reflect"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/tools/cache"
	certutil "k8s.io/client-go/util/cert"
)

type clientCertHealthChecker struct {
	interval time.Duration
	expired  bool
}

func (hc *clientCertHealthChecker) Name() string {
	return "hub-client-certificate"
}

// clientCertHealthChecker returns an error when the client certificate created for the
// hub kubeconfig exists and expires.
func (hc *clientCertHealthChecker) Check(_ *http.Request) error {
	if hc.expired {
		return errors.New("the client certificate expires and rebootstrap is required.")
	}
	return nil
}

func (hc *clientCertHealthChecker) start(ctx context.Context, tlsCertFile string) {
	if err := wait.PollUntilContextCancel(ctx, hc.interval, false, func(ctx context.Context) (bool, error) {
		data, err := os.ReadFile(tlsCertFile)
		if err != nil {
			// no work because the client certificate file may not exist yet.
			return false, nil
		}

		certs, err := certutil.ParseCertsPEM(data)
		if err != nil {
			utilruntime.HandleError(fmt.Errorf("unable to parse client certificates %q: %w", tlsCertFile, err))
			return false, nil
		}

		now := time.Now()
		for _, cert := range certs {
			if now.After(cert.NotAfter) {
				hc.expired = true
			}
		}

		return false, nil
	}); err != nil {
		utilruntime.HandleError(fmt.Errorf("unable to check the health of the client certificate %q: %w", tlsCertFile, err))
	}
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
