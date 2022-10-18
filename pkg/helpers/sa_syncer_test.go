package helpers

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	authv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	testclient "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
)

func TestTokenGetter(t *testing.T) {
	saName := "test-sa"
	saNamespace := "test-ns"

	tests := []struct {
		name           string
		objects        []runtime.Object
		expirationTime metav1.Time

		wantToken []byte
		wantErr   bool
	}{
		{
			name:    "no sa",
			wantErr: true,
		},
		{
			name: "no secret",
			objects: []runtime.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      saName,
						Namespace: saNamespace,
					},
					Secrets: []corev1.ObjectReference{
						{
							Name: "test",
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "token request",
			objects: []runtime.Object{
				&corev1.ServiceAccount{
					ObjectMeta: metav1.ObjectMeta{
						Name:      saName,
						Namespace: saNamespace,
					},
					Secrets: []corev1.ObjectReference{},
				},
			},
			wantToken: []byte("aaa"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testclient.NewSimpleClientset(tt.objects...)
			client.PrependReactor("create", "serviceaccounts/token", func(action clienttesting.Action) (handled bool, ret runtime.Object, err error) {
				return true, &authv1.TokenRequest{Status: authv1.TokenRequestStatus{Token: "aaa", ExpirationTimestamp: tt.expirationTime}}, nil
			})
			tokenGetter := SATokenGetter(context.TODO(), saName, saNamespace, client)
			token, _, err := tokenGetter()
			fmt.Printf("client action is %v\n", client.Actions())
			if err != nil && !tt.wantErr {
				t.Error(err)
			}
			if err == nil && tt.wantErr {
				t.Errorf("expect to get error")
			}
			if !reflect.DeepEqual(token, tt.wantToken) {
				t.Errorf("token is not correct, got %s", string(token))
			}
		})
	}
}

func TestApplyKubeconfigSecret(t *testing.T) {
	tkc := &rest.Config{
		Host: "host",
		TLSClientConfig: rest.TLSClientConfig{
			CAData: []byte("caData"),
		},
	}

	secretName := "test-secret"
	secretNamespace := "test-ns"

	now, _ := metav1.Now().MarshalText()

	tests := []struct {
		name          string
		secrets       []runtime.Object
		token         []byte
		tokenGetError error
		expiration    *metav1.Time

		validateActions func(t *testing.T, actions []clienttesting.Action)
		wantErr         bool
	}{
		{
			name:  "create secret if none",
			token: []byte("aaa"),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 3 {
					t.Errorf("expect 3 actions, but get %d", len(actions))
				}

				secret := actions[2].(clienttesting.CreateAction).GetObject().(*corev1.Secret)
				if !reflect.DeepEqual(secret.Data["token"], []byte("aaa")) {
					t.Errorf("secret update is not correct, got %v", secret.Data)
				}
			},
		},
		{
			name:          "get token fails",
			token:         []byte("aaa"),
			tokenGetError: fmt.Errorf("unexpected error"),
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 1 {
					t.Errorf("expect 1 actions, but get %d", len(actions))
				}
			},
			wantErr: true,
		},
		{
			name:  "update secret if secret is not correct",
			token: []byte("aaa"),
			secrets: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      secretName,
						Namespace: secretNamespace,
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 4 {
					t.Fatalf("expect 4 actions, but get %d", len(actions))
				}

				secret := actions[3].(clienttesting.CreateAction).GetObject().(*corev1.Secret)
				if !reflect.DeepEqual(secret.Data["token"], []byte("aaa")) {
					t.Errorf("secret update is not correct, got %v", secret.Data)
				}
			},
		},
		{
			name:  "update secret if expired",
			token: []byte("aaa"),
			secrets: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      secretName,
						Namespace: secretNamespace,
					},
					Data: map[string][]byte{
						"token":      []byte("aaa"),
						"expiration": now,
					},
				},
			},
			validateActions: func(t *testing.T, actions []clienttesting.Action) {
				if len(actions) != 4 {
					t.Fatalf("expect 4 actions, but get %d", len(actions))
				}

				secret := actions[3].(clienttesting.CreateAction).GetObject().(*corev1.Secret)
				if !reflect.DeepEqual(secret.Data["token"], []byte("aaa")) {
					t.Errorf("secret update is not correct, got %v", secret.Data)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tokenGetter := func() ([]byte, []byte, error) {
				if tt.expiration == nil {
					return tt.token, nil, tt.tokenGetError
				}

				expiration, _ := tt.expiration.MarshalText()
				return tt.token, expiration, tt.tokenGetError
			}
			client := testclient.NewSimpleClientset(tt.secrets...)
			err := SyncKubeConfigSecret(context.TODO(), secretName, secretNamespace, "/tmp/kubeconfig", tkc, client.CoreV1(), tokenGetter, eventstesting.NewTestingEventRecorder(t))
			if err != nil && !tt.wantErr {
				t.Error(err)
			}
			if err == nil && tt.wantErr {
				t.Errorf("expect to get error")
			}

			tt.validateActions(t, client.Actions())
		})
	}
}
