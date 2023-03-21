package addonsecretcontroller

import (
	"context"
	"testing"
	"time"

	"github.com/openshift/library-go/pkg/operator/events/eventstesting"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"
	testinghelpers "open-cluster-management.io/registration-operator/pkg/helpers/testing"
)

func TestSync(t *testing.T) {
	testcases := []struct {
		name       string
		queueKey   string
		objects    []runtime.Object
		namespaces []runtime.Object
		verify     func(t *testing.T, client *kubefake.Clientset)
	}{
		{
			name: "no namespace in queueKey",
			verify: func(t *testing.T, client *kubefake.Clientset) {
				if len(client.Actions()) != 0 {
					t.Errorf("expected no actions, got: %v", client.Actions())
				}
			},
		},
		{
			name:     "namespace without annotation created",
			queueKey: "ns1",
			namespaces: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ns1",
					},
				},
			},
			verify: func(t *testing.T, client *kubefake.Clientset) {
				if len(client.Actions()) != 1 {
					t.Errorf("expected one get action, got: %v", client.Actions())
				}
			},
		},
		{
			name:     "namespace with annotation created",
			queueKey: "ns1",
			objects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      imagePullSecret,
						Namespace: "open-cluster-management",
					},
					Data: map[string][]byte{
						"username": []byte("foo"),
					},
				},
			},
			namespaces: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ns1",
						Labels: map[string]string{
							"addon.open-cluster-management.io/namespace": "true"},
					},
				},
			},
			verify: func(t *testing.T, client *kubefake.Clientset) {
				secret, err := client.CoreV1().Secrets("ns1").Get(context.TODO(), imagePullSecret, metav1.GetOptions{})
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if secret.Data["username"] == nil {
					t.Errorf("expected username in secret, got: %v", secret.Data)
				}
			},
		},
	}

	for _, tc := range testcases {
		recorder := eventstesting.NewTestingEventRecorder(t)
		objs := append(tc.objects, tc.namespaces...)
		kubeClient := kubefake.NewSimpleClientset(objs...)
		kubeInformer := informers.NewSharedInformerFactory(kubeClient, 5*time.Minute)
		namespceStore := kubeInformer.Core().V1().Namespaces().Informer().GetStore()
		for _, ns := range tc.namespaces {
			_ = namespceStore.Add(ns)
		}

		controller := &addonPullImageSecretController{
			operatorNamespace: "open-cluster-management",
			kubeClient:        kubeClient,
			recorder:          recorder,
			namespaceInformer: kubeInformer.Core().V1().Namespaces(),
		}

		err := controller.sync(context.TODO(), testinghelpers.NewFakeSyncContext(t, tc.queueKey))
		if err != nil {
			t.Errorf("%s: unexpected error: %v", tc.name, err)
		}

		tc.verify(t, kubeClient)
	}
}
