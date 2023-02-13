package manager

import (
	"context"
	"k8s.io/client-go/rest"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	"time"
)

func RunManager(ctx context.Context, kubeConfig *rest.Config) error {
	hubClusterClient, err := clusterclientset.NewForConfig(kubeConfig)
	if err != nil {
		return err
	}

	clusterInformerFactory := clusterinformers.NewSharedInformerFactory(hubClusterClient, 30*time.Minute)

	go clusterInformerFactory.Start(ctx.Done())
	return nil
}
