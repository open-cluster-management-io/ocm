package klusterlet

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	admissionv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
)

func TestSPIFFEAdmissionInformers(t *testing.T) {
	served := fakekube.NewSimpleClientset()
	served.Resources = []*metav1.APIResourceList{{GroupVersion: admissionv1.SchemeGroupVersion.String(),
		APIResources: []metav1.APIResource{{Name: "validatingadmissionpolicies"}}}}
	got, err := spiffeAdmissionInformers(served, informers.NewSharedInformerFactory(served, time.Minute))
	if err != nil || len(got) != 2 {
		t.Errorf("served: %d informers, %v; want the policy and binding informers", len(got), err)
	}

	// Kubernetes before 1.30: nothing to watch.
	old := fakekube.NewSimpleClientset()
	if got, err := spiffeAdmissionInformers(old, informers.NewSharedInformerFactory(old, time.Minute)); err != nil || len(got) != 0 {
		t.Errorf("not served: %d informers, %v", len(got), err)
	}

	broken := fakekube.NewSimpleClientset()
	broken.PrependReactor("get", "resource", func(clienttesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("discovery failed")
	})
	if _, err := spiffeAdmissionInformers(broken, informers.NewSharedInformerFactory(broken, time.Minute)); err == nil {
		t.Error("no error when discovery fails")
	}
}

// The operator doesn't start when it can't learn whether the policy API is served.
func TestRunKlusterletOperatorDiscoveryError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			_, _ = w.Write([]byte(`{"major":"1","minor":"30","gitVersion":"v1.30.0"}`))
			return
		}
		http.Error(w, "discovery broken", http.StatusInternalServerError)
	}))
	defer server.Close()
	o := &Options{}
	err := o.RunKlusterletOperator(context.Background(), &controllercmd.ControllerContext{
		KubeConfig: &rest.Config{Host: server.URL}})
	if err == nil {
		t.Fatal("the operator started although discovery failed")
	}
}
