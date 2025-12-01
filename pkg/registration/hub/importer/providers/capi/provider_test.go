package capi

import (
	"context"
	"testing"

	"github.com/ghodss/yaml"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/dynamicinformer"
	fakedynamic "k8s.io/client-go/dynamic/fake"
	fakekube "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
	clientcmdapiv1 "k8s.io/client-go/tools/clientcmd/api/v1"

	fakecluster "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/factory"

	testingcommon "open-cluster-management.io/ocm/pkg/common/testing"
)

func TestEnqueu(t *testing.T) {
	cases := []struct {
		name          string
		cluster       *clusterv1.ManagedCluster
		capiName      string
		capiNamespace string
		expectedKey   string
	}{
		{
			name:          "enqueu by name",
			cluster:       &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}},
			capiName:      "cluster1",
			capiNamespace: "cluster1",
			expectedKey:   "cluster1",
		},
		{
			name: "enqueu by annotation",
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster2",
					Annotations: map[string]string{
						CAPIAnnotationKey: "capi/cluster1",
					},
				},
			},
			capiName:      "cluster1",
			capiNamespace: "capi",
			expectedKey:   "cluster2",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			client := fakecluster.NewSimpleClientset(c.cluster)
			informerFactory := clusterinformers.NewSharedInformerFactory(client, 0)
			clusterInformer := informerFactory.Cluster().V1().ManagedClusters()
			if err := clusterInformer.Informer().AddIndexers(cache.Indexers{
				ByCAPIResource: indexByCAPIResource,
			}); err != nil {
				t.Fatal(err)
			}
			if err := clusterInformer.Informer().GetStore().Add(c.cluster); err != nil {
				t.Fatal(err)
			}

			provider := &CAPIProvider{
				managedClusterIndexer: clusterInformer.Informer().GetIndexer(),
			}
			syncCtx := factory.NewSyncContext("test")
			provider.enqueueManagedClusterByCAPI(&metav1.PartialObjectMetadata{
				ObjectMeta: metav1.ObjectMeta{
					Name:      c.capiName,
					Namespace: c.capiNamespace,
				},
			}, syncCtx)
			if i, _ := syncCtx.Queue().Get(); i != c.expectedKey {
				t.Errorf("expected key %s but got %s", c.expectedKey, i)
			}
		})
	}
}

func TestClients(t *testing.T) {
	cases := []struct {
		name        string
		capiObjects []runtime.Object
		kubeObjects []runtime.Object
		cluster     *clusterv1.ManagedCluster
		expectErr   bool
	}{
		{
			name:    "capi cluster not found",
			cluster: &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}},
		},
		{
			name:    "capi cluster not provisioned",
			cluster: &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}},
			capiObjects: []runtime.Object{
				testingcommon.NewUnstructuredWithContent("cluster.x-k8s.io/v1beta1", "Cluster", "cluster1", "cluster1",
					map[string]interface{}{
						"status": map[string]interface{}{
							"phase": "Provisioning",
						},
					},
				),
			},
		},
		{
			name:    "secret not found",
			cluster: &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}},
			capiObjects: []runtime.Object{
				testingcommon.NewUnstructuredWithContent("cluster.x-k8s.io/v1beta1", "Cluster", "cluster1", "cluster1",
					map[string]interface{}{
						"status": map[string]interface{}{
							"phase": "Provisioned",
						},
					},
				),
			},
			expectErr: true,
		},
		{
			name:    "secret found with invalid key",
			cluster: &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}},
			capiObjects: []runtime.Object{
				testingcommon.NewUnstructuredWithContent("cluster.x-k8s.io/v1beta1", "Cluster", "cluster1", "cluster1",
					map[string]interface{}{
						"status": map[string]interface{}{
							"phase": "Provisioned",
						},
					},
				),
			},
			kubeObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cluster1-kubeconfig",
						Namespace: "cluster1",
					},
				},
			},
			expectErr: true,
		},
		{
			name:    "build client successfully",
			cluster: &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}},
			capiObjects: []runtime.Object{
				testingcommon.NewUnstructuredWithContent("cluster.x-k8s.io/v1beta1", "Cluster", "cluster1", "cluster1",
					map[string]interface{}{
						"status": map[string]interface{}{
							"phase": "Provisioned",
						},
					},
				)},
			kubeObjects: []runtime.Object{
				func() *corev1.Secret {
					clientConfig := clientcmdapiv1.Config{
						// Define a cluster stanza based on the bootstrap kubeconfig.
						Clusters: []clientcmdapiv1.NamedCluster{
							{
								Name: "hub",
								Cluster: clientcmdapiv1.Cluster{
									Server: "https://test",
								},
							},
						},
						// Define auth based on the obtained client cert.
						AuthInfos: []clientcmdapiv1.NamedAuthInfo{
							{
								Name: "bootstrap",
								AuthInfo: clientcmdapiv1.AuthInfo{
									Token: "test",
								},
							},
						},
						// Define a context that connects the auth info and cluster, and set it as the default
						Contexts: []clientcmdapiv1.NamedContext{
							{
								Name: "bootstrap",
								Context: clientcmdapiv1.Context{
									Cluster:   "hub",
									AuthInfo:  "bootstrap",
									Namespace: "default",
								},
							},
						},
						CurrentContext: "bootstrap",
					}
					bootstrapConfigBytes, _ := yaml.Marshal(clientConfig)
					return &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "cluster1-kubeconfig",
							Namespace: "cluster1",
						},
						Data: map[string][]byte{
							"value": bootstrapConfigBytes,
						},
					}
				}(),
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dynamicClient := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), c.capiObjects...)
			kubeClient := fakekube.NewClientset(c.kubeObjects...)
			dynamicInformers := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)
			for _, capiObj := range c.capiObjects {
				if err := dynamicInformers.ForResource(ClusterAPIGVR).Informer().GetStore().Add(capiObj); err != nil {
					t.Fatal(err)
				}
			}
			provider := &CAPIProvider{
				kubeClient: kubeClient,
				informer:   dynamicInformers,
				lister:     dynamicInformers.ForResource(ClusterAPIGVR).Lister(),
			}
			_, err := provider.Clients(context.TODO(), c.cluster)
			if c.expectErr && err == nil {
				t.Errorf("expected error but got nil")
			}
			if !c.expectErr && err != nil {
				t.Errorf("expected no error but got %v", err)
			}
		})
	}
}

func TestIsManagedClusterOwner(t *testing.T) {
	cases := []struct {
		name        string
		capiObjects []runtime.Object
		cluster     *clusterv1.ManagedCluster
		expectedOwn bool
	}{
		{
			name: "by cluster name",
			capiObjects: []runtime.Object{
				testingcommon.NewUnstructured(
					"cluster.x-k8s.io/v1beta1", "Cluster", "cluster1", "cluster1")},
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{Name: "cluster1"},
			},
			expectedOwn: true,
		},
		{
			name: "by cluster annotation",
			capiObjects: []runtime.Object{
				testingcommon.NewUnstructured(
					"cluster.x-k8s.io/v1beta1", "Cluster", "capi", "cluster1")},
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster2",
					Annotations: map[string]string{
						CAPIAnnotationKey: "capi/cluster1",
					},
				},
			},
			expectedOwn: true,
		},
		{
			name: "by cluster annotation",
			capiObjects: []runtime.Object{
				testingcommon.NewUnstructured(
					"cluster.x-k8s.io/v1beta1", "Cluster", "capi", "cluster2")},
			cluster: &clusterv1.ManagedCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster2",
					Annotations: map[string]string{
						CAPIAnnotationKey: "capi/cluster1",
					},
				},
			},
			expectedOwn: false,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			dynamicClient := fakedynamic.NewSimpleDynamicClient(runtime.NewScheme(), c.capiObjects...)
			dynamicInformers := dynamicinformer.NewDynamicSharedInformerFactory(dynamicClient, 0)
			for _, capiObj := range c.capiObjects {
				if err := dynamicInformers.ForResource(ClusterAPIGVR).Informer().GetStore().Add(capiObj); err != nil {
					t.Fatal(err)
				}
			}
			provider := &CAPIProvider{
				lister: dynamicInformers.ForResource(ClusterAPIGVR).Lister(),
			}
			owned := provider.IsManagedClusterOwner(c.cluster)
			if c.expectedOwn != owned {
				t.Errorf("expected owned cluster %t but got %t", c.expectedOwn, owned)
			}
		})
	}
}
