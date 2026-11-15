package registration

import (
	"context"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubefake "k8s.io/client-go/kubernetes/fake"

	clusterfake "open-cluster-management.io/api/client/cluster/clientset/versioned/fake"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

func TestCleanupLegacyHubCredentials(t *testing.T) {
	legacyCredentialsPollInterval = 20 * time.Millisecond
	const ns = "open-cluster-management-agent"
	secret := func(name string) *corev1.Secret {
		return &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: ns}}
	}
	kubeClient := kubefake.NewSimpleClientset(secret("hub-kubeconfig-secret"), secret("bootstrap-hub-kubeconfig"), secret("other"))
	cluster := &clusterv1.ManagedCluster{ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}}
	clusterClient := clusterfake.NewSimpleClientset(cluster)
	informers := clusterinformers.NewSharedInformerFactory(clusterClient, 0)
	clusterInformer := informers.Cluster().V1().ManagedClusters()
	clusterInformer.Informer() // register it before the factory starts

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		CleanupLegacyHubCredentials(ctx, "cluster1", clusterInformer, kubeClient.CoreV1().Secrets(ns),
			"hub-kubeconfig-secret", "bootstrap-hub-kubeconfig")
		close(done)
	}()
	informers.Start(ctx.Done())

	exists := func(name string) bool {
		_, err := kubeClient.CoreV1().Secrets(ns).Get(context.TODO(), name, metav1.GetOptions{})
		return !errors.IsNotFound(err)
	}

	// Not accepted yet: the old credentials stay, so an admin can still switch back.
	time.Sleep(200 * time.Millisecond)
	if !exists("hub-kubeconfig-secret") || !exists("bootstrap-hub-kubeconfig") {
		t.Fatal("deleted the old credentials before the cluster was accepted")
	}

	cluster.Spec.HubAcceptsClient = true
	if _, err := clusterClient.ClusterV1().ManagedClusters().Update(context.TODO(), cluster, metav1.UpdateOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup didn't finish")
	}
	if exists("hub-kubeconfig-secret") || exists("bootstrap-hub-kubeconfig") {
		t.Error("old credentials are still there")
	}
	if !exists("other") {
		t.Error("deleted an unrelated Secret")
	}
}

// The agent stops before the cluster is accepted (here, before the informer even syncs): the old
// credentials stay.
func TestCleanupLegacyHubCredentialsStopped(t *testing.T) {
	legacyCredentialsPollInterval = 10 * time.Millisecond
	const ns = "open-cluster-management-agent"
	kubeClient := kubefake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "hub-kubeconfig-secret", Namespace: ns}})
	informers := clusterinformers.NewSharedInformerFactory(clusterfake.NewSimpleClientset(), 0)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	// The informer is never started, so it never syncs.
	CleanupLegacyHubCredentials(ctx, "cluster1", informers.Cluster().V1().ManagedClusters(),
		kubeClient.CoreV1().Secrets(ns), "hub-kubeconfig-secret")
	if _, err := kubeClient.CoreV1().Secrets(ns).Get(context.TODO(), "hub-kubeconfig-secret", metav1.GetOptions{}); err != nil {
		t.Errorf("the old credential was deleted although the cluster was never accepted: %v", err)
	}
}

// The cluster doesn't exist yet when the cleanup starts, and unset Secret names are skipped.
func TestCleanupLegacyHubCredentialsClusterCreatedLater(t *testing.T) {
	legacyCredentialsPollInterval = 10 * time.Millisecond
	const ns = "open-cluster-management-agent"
	kubeClient := kubefake.NewSimpleClientset(&corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: "hub-kubeconfig-secret", Namespace: ns}})
	clusterClient := clusterfake.NewSimpleClientset()
	informers := clusterinformers.NewSharedInformerFactory(clusterClient, 0)
	clusterInformer := informers.Cluster().V1().ManagedClusters()
	clusterInformer.Informer()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	informers.Start(ctx.Done())
	done := make(chan struct{})
	go func() {
		CleanupLegacyHubCredentials(ctx, "cluster1", clusterInformer, kubeClient.CoreV1().Secrets(ns), "", "hub-kubeconfig-secret")
		close(done)
	}()
	time.Sleep(100 * time.Millisecond) // the lister has no cluster1 yet
	if _, err := clusterClient.ClusterV1().ManagedClusters().Create(context.TODO(), &clusterv1.ManagedCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster1"}, Spec: clusterv1.ManagedClusterSpec{HubAcceptsClient: true}},
		metav1.CreateOptions{}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("cleanup didn't finish")
	}
	if _, err := kubeClient.CoreV1().Secrets(ns).Get(context.TODO(), "hub-kubeconfig-secret", metav1.GetOptions{}); err == nil {
		t.Error("the old credential is still there")
	}
}
