package spoke

import (
	"context"
	"net/http"
	"sync"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/util/wait"
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
