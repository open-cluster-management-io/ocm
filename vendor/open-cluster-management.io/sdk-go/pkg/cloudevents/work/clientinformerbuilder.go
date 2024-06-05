package work

import (
	"context"
	"fmt"

	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	workv1informers "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	workv1 "open-cluster-management.io/api/work/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
	agentclient "open-cluster-management.io/sdk-go/pkg/cloudevents/work/agent/client"
	agenthandler "open-cluster-management.io/sdk-go/pkg/cloudevents/work/agent/handler"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/internal"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/work/watcher"
)

// NewAgentClientHolderWithInformer returns a ClientHolder with a ManifestWorkInformer for an agent.
// This function is suitable for the scenarios where a ManifestWorkInformer is required
//
// Note: the ClientHolder should be used after the ManifestWorkInformer starts
//
// TODO enhance the manifestwork agent client with WorkClientWatcherCache
func (b *ClientHolderBuilder) NewAgentClientHolderWithInformer(
	ctx context.Context) (*ClientHolder, workv1informers.ManifestWorkInformer, error) {
	switch config := b.config.(type) {
	case *rest.Config:
		kubeWorkClientSet, err := workclientset.NewForConfig(config)
		if err != nil {
			return nil, nil, err
		}

		factory := workinformers.NewSharedInformerFactoryWithOptions(kubeWorkClientSet, b.informerResyncTime, b.informerOptions...)
		return &ClientHolder{workClientSet: kubeWorkClientSet}, factory.Work().V1().ManifestWorks(), nil
	default:
		options, err := generic.BuildCloudEventsAgentOptions(config, b.clusterName, b.clientID)
		if err != nil {
			return nil, nil, err
		}
		return b.newAgentClients(ctx, options)
	}
}

func (b *ClientHolderBuilder) newAgentClients(
	ctx context.Context,
	agentOptions *options.CloudEventsAgentOptions,
) (*ClientHolder, workv1informers.ManifestWorkInformer, error) {
	if len(b.clientID) == 0 {
		return nil, nil, fmt.Errorf("client id is required")
	}

	if len(b.clusterName) == 0 {
		return nil, nil, fmt.Errorf("cluster name is required")
	}

	workLister := &ManifestWorkLister{}
	watcher := watcher.NewManifestWorkWatcher()
	cloudEventsClient, err := generic.NewCloudEventAgentClient[*workv1.ManifestWork](
		ctx,
		agentOptions,
		workLister,
		ManifestWorkStatusHash,
		b.codecs...,
	)
	if err != nil {
		return nil, nil, err
	}

	manifestWorkClient := agentclient.NewManifestWorkAgentClient(cloudEventsClient, watcher)
	workClient := &internal.WorkV1ClientWrapper{ManifestWorkClient: manifestWorkClient}
	workClientSet := &internal.WorkClientSetWrapper{WorkV1ClientWrapper: workClient}
	factory := workinformers.NewSharedInformerFactoryWithOptions(workClientSet, b.informerResyncTime, b.informerOptions...)
	informers := factory.Work().V1().ManifestWorks()
	manifestWorkLister := informers.Lister()
	namespacedLister := manifestWorkLister.ManifestWorks(b.clusterName)

	// Set informer lister back to work lister and client.
	workLister.Lister = manifestWorkLister
	// TODO the work client and informer share a same store in the current implementation, ideally, the store should be
	// only written from the server. we may need to revisit the implementation in the future.
	manifestWorkClient.SetLister(namespacedLister)

	cloudEventsClient.Subscribe(ctx, agenthandler.NewManifestWorkAgentHandler(namespacedLister, watcher))

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-cloudEventsClient.ReconnectedChan():
				// when receiving a client reconnected signal, we resync all sources for this agent
				// TODO after supporting multiple sources, we should only resync agent known sources
				if err := cloudEventsClient.Resync(ctx, types.SourceAll); err != nil {
					klog.Errorf("failed to send resync request, %v", err)
				}
			}
		}
	}()

	return &ClientHolder{workClientSet: workClientSet}, informers, nil
}
