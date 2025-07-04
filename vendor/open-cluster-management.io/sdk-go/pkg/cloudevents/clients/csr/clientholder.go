package csr

import (
	"context"
	"fmt"
	"time"

	certificatev1 "k8s.io/api/certificates/v1"
	"k8s.io/client-go/tools/cache"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/options"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/store"
)

// ClientHolder holds a client that implements list/watch for CSR and a SharedIndexInformer for CSR
type ClientHolder struct {
	informer cache.SharedIndexInformer
	client   *CSRClient
}

func (h *ClientHolder) Informer() cache.SharedIndexInformer {
	return h.informer
}

func (h *ClientHolder) Clients() *CSRClient {
	return h.client
}

// NewAgentClientHolder returns a ClientHolder for an agent
func NewAgentClientHolder(ctx context.Context, opt *options.GenericClientOptions[*certificatev1.CertificateSigningRequest]) (*ClientHolder, error) {
	cloudEventsClient, err := opt.AgentClient(ctx)
	if err != nil {
		return nil, err
	}

	csrClient := NewCSRClient(cloudEventsClient, opt.WatcherStore(), opt.ClusterName())

	csrInformer := cache.NewSharedIndexInformer(
		csrClient, &certificatev1.CertificateSigningRequest{}, 30*time.Second,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	agentStore, ok := opt.WatcherStore().(*store.AgentInformerWatcherStore[*certificatev1.CertificateSigningRequest])
	if !ok {
		return nil, fmt.Errorf("watcher store must be of type AgentInformerWatcherStore")
	}
	agentStore.SetInformer(csrInformer)

	return &ClientHolder{client: csrClient, informer: csrInformer}, nil
}
