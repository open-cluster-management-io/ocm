package grpc

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"

	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
)

type Clients struct {
	KubeClient       kubernetes.Interface
	ClusterClient    clusterv1client.Interface
	WorkClient       workclientset.Interface
	AddOnClient      addonv1alpha1client.Interface
	KubeInformers    kubeinformers.SharedInformerFactory
	ClusterInformers clusterv1informers.SharedInformerFactory
	WorkInformers    workinformers.SharedInformerFactory
	AddOnInformers   addoninformers.SharedInformerFactory
}

func NewClients(controllerContext *controllercmd.ControllerContext) (*Clients, error) {
	kubeClient, err := kubernetes.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return nil, err
	}
	clusterClient, err := clusterv1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return nil, err
	}
	addonClient, err := addonv1alpha1client.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return nil, err
	}
	workClient, err := workclientset.NewForConfig(controllerContext.KubeConfig)
	if err != nil {
		return nil, err
	}
	return &Clients{
		KubeClient:    kubeClient,
		ClusterClient: clusterClient,
		AddOnClient:   addonClient,
		WorkClient:    workClient,
		KubeInformers: kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 30*time.Minute,
			kubeinformers.WithTweakListOptions(func(listOptions *metav1.ListOptions) {
				selector := &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      clusterv1.ClusterNameLabelKey,
							Operator: metav1.LabelSelectorOpExists,
						},
					},
				}
				listOptions.LabelSelector = metav1.FormatLabelSelector(selector)
			})),
		ClusterInformers: clusterv1informers.NewSharedInformerFactory(clusterClient, 30*time.Minute),
		WorkInformers:    workinformers.NewSharedInformerFactoryWithOptions(workClient, 30*time.Minute),
		AddOnInformers:   addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute),
	}, nil
}

func (h *Clients) Run(ctx context.Context) {
	go h.KubeInformers.Start(ctx.Done())
	go h.ClusterInformers.Start(ctx.Done())
	go h.WorkInformers.Start(ctx.Done())
	go h.AddOnInformers.Start(ctx.Done())
}
