package store

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/generic/types"
)

// AgentWatcherStoreLister list the resources from ClientWatcherStore on an agent
type AgentWatcherStoreLister[T generic.ResourceObject] struct {
	store ClientWatcherStore[T]
}

func NewAgentWatcherStoreLister[T generic.ResourceObject](store ClientWatcherStore[T]) *AgentWatcherStoreLister[T] {
	return &AgentWatcherStoreLister[T]{
		store: store,
	}
}

// List returns the resources from a ClientWatcherStore with list options
func (l *AgentWatcherStoreLister[T]) List(options types.ListOptions) ([]T, error) {
	opts := metav1.ListOptions{}

	// TODO we might want to specify source when list

	list, err := l.store.List("", opts)
	if err != nil {
		return nil, err
	}

	return list.Items, nil
}

// SourceWatcherStoreLister list the resources from a ClientWatcherStore on a source.
type SourceWatcherStoreLister[T generic.ResourceObject] struct {
	store ClientWatcherStore[T]
}

func NewSourceWatcherStoreLister[T generic.ResourceObject](store ClientWatcherStore[T]) *SourceWatcherStoreLister[T] {
	return &SourceWatcherStoreLister[T]{
		store: store,
	}
}

// List returns the resources from a ClientWatcherStore with list options.
func (l *SourceWatcherStoreLister[T]) List(options types.ListOptions) ([]T, error) {
	list, err := l.store.List(options.ClusterName, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	return list.Items, nil
}
