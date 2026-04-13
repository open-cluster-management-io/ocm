package addontlsconfigcontroller

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	kubefake "k8s.io/client-go/kubernetes/fake"

	tlslib "open-cluster-management.io/sdk-go/pkg/tls"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
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
			name: "empty queue key — no actions",
			verify: func(t *testing.T, client *kubefake.Clientset) {
				if len(client.Actions()) != 0 {
					t.Errorf("expected no actions, got: %v", client.Actions())
				}
			},
		},
		{
			name:     "namespace without addon label — no ConfigMap operations",
			queueKey: "ns1",
			namespaces: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: "ns1",
					},
				},
			},
			verify: func(t *testing.T, client *kubefake.Clientset) {
				// Only the namespace Get action
				if len(client.Actions()) != 1 {
					t.Errorf("expected 1 action (namespace get), got %d: %v",
						len(client.Actions()), client.Actions())
				}
			},
		},
		{
			name:     "namespace with addon label, source ConfigMap exists — copied",
			queueKey: "ns1",
			objects: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tlslib.ConfigMapName,
						Namespace: "open-cluster-management",
					},
					Data: map[string]string{
						"minTLSVersion": "VersionTLS13",
					},
				},
			},
			namespaces: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "ns1",
						Labels: map[string]string{addonInstallNamespaceLabelKey: "true"},
					},
				},
			},
			verify: func(t *testing.T, client *kubefake.Clientset) {
				cm, err := client.CoreV1().ConfigMaps("ns1").Get(
					context.TODO(), tlslib.ConfigMapName, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("expected ConfigMap to be copied, got error: %v", err)
				}
				if cm.Data["minTLSVersion"] != "VersionTLS13" {
					t.Errorf("expected minTLSVersion=VersionTLS13, got %v", cm.Data)
				}
			},
		},
		{
			name:     "namespace with addon label, source ConfigMap missing — target deleted",
			queueKey: "ns1",
			objects: []runtime.Object{
				// Target ConfigMap exists but source does not
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tlslib.ConfigMapName,
						Namespace: "ns1",
					},
					Data: map[string]string{
						"minTLSVersion": "VersionTLS12",
					},
				},
			},
			namespaces: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "ns1",
						Labels: map[string]string{addonInstallNamespaceLabelKey: "true"},
					},
				},
			},
			verify: func(t *testing.T, client *kubefake.Clientset) {
				_, err := client.CoreV1().ConfigMaps("ns1").Get(
					context.TODO(), tlslib.ConfigMapName, metav1.GetOptions{})
				if err == nil {
					t.Error("expected target ConfigMap to be deleted, but it still exists")
				}
			},
		},
		{
			name:     "namespace with addon label, target stale — updated",
			queueKey: "ns1",
			objects: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tlslib.ConfigMapName,
						Namespace: "open-cluster-management",
					},
					Data: map[string]string{
						"minTLSVersion": "VersionTLS13",
					},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tlslib.ConfigMapName,
						Namespace: "ns1",
					},
					Data: map[string]string{
						"minTLSVersion": "VersionTLS12",
					},
				},
			},
			namespaces: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "ns1",
						Labels: map[string]string{addonInstallNamespaceLabelKey: "true"},
					},
				},
			},
			verify: func(t *testing.T, client *kubefake.Clientset) {
				cm, err := client.CoreV1().ConfigMaps("ns1").Get(
					context.TODO(), tlslib.ConfigMapName, metav1.GetOptions{})
				if err != nil {
					t.Fatalf("expected ConfigMap to exist, got error: %v", err)
				}
				if cm.Data["minTLSVersion"] != "VersionTLS13" {
					t.Errorf("expected minTLSVersion=VersionTLS13 after update, got %v", cm.Data)
				}
			},
		},
		{
			name:     "namespace with addon label, target already up-to-date — no update",
			queueKey: "ns1",
			objects: []runtime.Object{
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tlslib.ConfigMapName,
						Namespace: "open-cluster-management",
					},
					Data: map[string]string{
						"minTLSVersion": "VersionTLS13",
					},
				},
				&corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      tlslib.ConfigMapName,
						Namespace: "ns1",
					},
					Data: map[string]string{
						"minTLSVersion": "VersionTLS13",
					},
				},
			},
			namespaces: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:   "ns1",
						Labels: map[string]string{addonInstallNamespaceLabelKey: "true"},
					},
				},
			},
			verify: func(t *testing.T, client *kubefake.Clientset) {
				// Should have: namespace Get, source ConfigMap Get, target ConfigMap Get — no Create/Update
				for _, action := range client.Actions() {
					if action.GetVerb() == "create" || action.GetVerb() == "update" {
						t.Errorf("expected no create/update, got: %s %s",
							action.GetVerb(), action.GetResource().Resource)
					}
				}
			},
		},
	}

	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			objs := append(tc.objects, tc.namespaces...) //nolint:gocritic
			kubeClient := kubefake.NewSimpleClientset(objs...)
			kubeInformer := informers.NewSharedInformerFactory(kubeClient, 5*time.Minute)
			namespaceStore := kubeInformer.Core().V1().Namespaces().Informer().GetStore()
			for _, ns := range tc.namespaces {
				_ = namespaceStore.Add(ns)
			}

			controller := &addonTLSConfigController{
				operatorNamespace: "open-cluster-management",
				kubeClient:        kubeClient,
			}

			err := controller.sync(context.TODO(), testingcommon.NewFakeSyncContext(t, tc.queueKey), tc.queueKey)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			tc.verify(t, kubeClient)
		})
	}
}
