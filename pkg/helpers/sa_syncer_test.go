package helpers

import (
	"context"
	"fmt"
	"testing"

	"github.com/openshift/library-go/pkg/operator/events"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	testclient "k8s.io/client-go/kubernetes/fake"
	coreclientv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
)

func TestEnsureSAToken(t *testing.T) {
	type args struct {
		ctx           context.Context
		saName        string
		saNamespace   string
		renderSAToken func([]byte) error
		client        kubernetes.Interface
	}

	simpleRender := func(data []byte) error {
		expected := "sa-token"
		if expected != string(data) {
			return fmt.Errorf("render result not as expected, data: %q", data)
		}
		return nil
	}

	sa := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Secrets: []corev1.ObjectReference{
			{
				Name:      "test-token",
				Namespace: "test",
			},
		},
	}

	wrongNameSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test",
		},
		Type: corev1.SecretTypeServiceAccountToken,
		Data: map[string][]byte{
			"token": []byte("sa-token"),
		},
	}

	wrongTypeSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-token",
			Namespace: "test",
		},
		Type: corev1.SecretTypeDockerConfigJson,
		Data: map[string][]byte{
			"token": []byte("sa-token"),
		},
	}

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-token",
			Namespace: "test",
		},
		Type: corev1.SecretTypeServiceAccountToken,
		Data: map[string][]byte{
			"token": []byte("sa-token"),
		},
	}

	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "sa doesn't exist",
			args: args{
				saName:        "test",
				saNamespace:   "test",
				renderSAToken: simpleRender,
				client:        testclient.NewSimpleClientset(),
			},
			wantErr: true,
		},
		{
			name: "sa exist, but token secret doesn't exist",
			args: args{
				saName:        "test",
				saNamespace:   "test",
				renderSAToken: simpleRender,
				client:        testclient.NewSimpleClientset(sa),
			},
			wantErr: true,
		}, {
			name: "no token secret",
			args: args{
				saName:        "test",
				saNamespace:   "test",
				renderSAToken: simpleRender,
				client:        testclient.NewSimpleClientset(sa, wrongNameSecret),
			},
			wantErr: true,
		}, {
			name: "no token secret",
			args: args{
				saName:        "test",
				saNamespace:   "test",
				renderSAToken: simpleRender,
				client:        testclient.NewSimpleClientset(sa, wrongTypeSecret),
			},
			wantErr: true,
		},
		{
			name: "success",
			args: args{
				saName:        "test",
				saNamespace:   "test",
				renderSAToken: simpleRender,
				client:        testclient.NewSimpleClientset(sa, secret),
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := EnsureSAToken(tt.args.ctx, tt.args.saName, tt.args.saNamespace, tt.args.client, tt.args.renderSAToken); (err != nil) != tt.wantErr {
				t.Errorf("EnsureKubeconfigFromSA() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestRenderToKubeconfigSecret(t *testing.T) {
	type args struct {
		secretName         string
		secretNamespace    string
		templateKubeconfig *rest.Config
		client             coreclientv1.SecretsGetter
		recorder           events.Recorder
	}

	tkc := &rest.Config{
		Host: "host",
		TLSClientConfig: rest.TLSClientConfig{
			CAData: []byte("caData"),
		},
	}

	client := testclient.NewSimpleClientset(&corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "secretNamespace",
		},
	})

	tests := []struct {
		name string
		args args
	}{
		{
			name: "the secret content should be the content of a kubeconfig",
			args: args{
				secretName:         "secretName",
				secretNamespace:    "secretNamespace",
				templateKubeconfig: tkc,
				client:             client.CoreV1(),
				recorder:           newTestingEventRecorder(t),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			render := RenderToKubeconfigSecret(context.TODO(), tt.args.secretName, tt.args.secretNamespace, tt.args.templateKubeconfig, tt.args.client, tt.args.recorder)
			err := render([]byte("sa-token"))
			if err != nil {
				t.Errorf("renderSaToken, err should be nil but got %s", err.Error())
				return
			}
			secret, err := client.CoreV1().Secrets("secretNamespace").Get(context.Background(), "secretName", metav1.GetOptions{})
			if err != nil {
				t.Errorf("get secret, err should be nil but got %s", err.Error())
				return
			}
			if kubeconfigContent, ok := secret.Data["kubeconfig"]; !ok {
				t.Errorf("kubeconfig data not exist")
				return
			} else if string(kubeconfigContent) != "apiVersion: v1\nclusters:\n- cluster:\n    certificate-authority-data: Y2FEYXRh\n    server: host\n  name: cluster\ncontexts:\n- context:\n    cluster: cluster\n    user: user\n  name: context\ncurrent-context: context\nkind: Config\npreferences: {}\nusers:\n- name: user\n  user:\n    token: sa-token\n" {
				t.Errorf("kubeconfig data doesn't correct, got %q", string(kubeconfigContent))
				return
			}
		})
	}
}

type testingEventRecorder struct {
	t         *testing.T
	component string
}

// NewTestingEventRecorder provides event recorder that will log all recorded events to the error log.
func newTestingEventRecorder(t *testing.T) events.Recorder {
	return &testingEventRecorder{t: t, component: "test"}
}

func (r *testingEventRecorder) ComponentName() string {
	return r.component
}

func (r *testingEventRecorder) WithContext(ctx context.Context) events.Recorder {
	return r
}

func (r *testingEventRecorder) ForComponent(c string) events.Recorder {
	return &testingEventRecorder{t: r.t, component: c}
}

func (r *testingEventRecorder) Shutdown() {}

func (r *testingEventRecorder) WithComponentSuffix(suffix string) events.Recorder {
	return r.ForComponent(fmt.Sprintf("%s-%s", r.ComponentName(), suffix))
}

func (r *testingEventRecorder) Event(reason, message string) {
	r.t.Logf("Event: %v: %v", reason, message)
}

func (r *testingEventRecorder) Eventf(reason, messageFmt string, args ...interface{}) {
	r.Event(reason, fmt.Sprintf(messageFmt, args...))
}

func (r *testingEventRecorder) Warning(reason, message string) {
	r.t.Logf("Warning: %v: %v", reason, message)
}

func (r *testingEventRecorder) Warningf(reason, messageFmt string, args ...interface{}) {
	r.Warning(reason, fmt.Sprintf(messageFmt, args...))
}
