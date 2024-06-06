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
	"k8s.io/apimachinery/pkg/types"
	testclient "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/rest"
	clienttesting "k8s.io/client-go/testing"
)

func TestTokenGetter(t *testing.T) {
	saName := "test-sa"
	saNamespace := "test-ns"
	saUID := "test-uid"

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
						UID:       types.UID(saUID),
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
			token, _, additionalData, err := tokenGetter()
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
			if !tt.wantErr {
				if additionalData == nil {
					t.Errorf("additional data is nil")
				}
				if !reflect.DeepEqual(additionalData["serviceaccount_namespace"], []byte(saNamespace)) {
					t.Errorf("serviceaccount_namespace is not correct, got %s", string(additionalData["serviceaccount_namespace"]))
				}
				if !reflect.DeepEqual(additionalData["serviceaccount_name"], []byte(saName)) {
					t.Errorf("serviceaccount_name is not correct, got %s", string(additionalData["serviceaccount_name"]))
				}
				if !reflect.DeepEqual(additionalData["serviceaccount_uid"], []byte(saUID)) {
					t.Errorf("serviceaccount_uid is not correct, got %s", string(additionalData["serviceaccount_uid"]))
				}
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
		name           string
		secrets        []runtime.Object
		token          []byte
		additionalData map[string][]byte
		tokenGetError  error
		expiration     *metav1.Time

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
		{
			name:  "update secret if additional data changed",
			token: []byte("aaa"),
			secrets: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      secretName,
						Namespace: secretNamespace,
					},
					Data: map[string][]byte{
						"token":                    []byte("aaa"),
						"serviceaccount_name":      []byte("test-sa"),
						"serviceaccount_namespace": []byte("test-ns"),
					},
				},
			},
			additionalData: map[string][]byte{
				"serviceaccount_name":      []byte("test-sa"),
				"serviceaccount_namespace": []byte("test-ns-new"),
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
			name:  "update secret if new additional data added",
			token: []byte("aaa"),
			secrets: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      secretName,
						Namespace: secretNamespace,
					},
					Data: map[string][]byte{
						"token": []byte("aaa"),
					},
				},
			},
			additionalData: map[string][]byte{
				"serviceaccount_name":      []byte("test-sa"),
				"serviceaccount_namespace": []byte("test-ns"),
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
			tokenGetter := func() ([]byte, []byte, map[string][]byte, error) {
				if tt.expiration == nil {
					return tt.token, nil, tt.additionalData, tt.tokenGetError
				}

				expiration, _ := tt.expiration.MarshalText()
				return tt.token, expiration, tt.additionalData, tt.tokenGetError
			}
			client := testclient.NewSimpleClientset(tt.secrets...)
			err := SyncKubeConfigSecret(
				context.TODO(), secretName, secretNamespace,
				"/tmp/kubeconfig", tkc, client.CoreV1(), tokenGetter,
				eventstesting.NewTestingEventRecorder(t), nil)
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
