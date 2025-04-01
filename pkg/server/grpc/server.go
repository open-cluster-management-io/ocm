package grpc

import (
	"context"
	"time"

	"github.com/openshift/library-go/pkg/controller/controllercmd"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kubeinformers "k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/clock"

	addonv1alpha1client "open-cluster-management.io/api/client/addon/clientset/versioned"
	addoninformers "open-cluster-management.io/api/client/addon/informers/externalversions"
	clusterv1client "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterv1informers "open-cluster-management.io/api/client/cluster/informers/externalversions"
	workclientset "open-cluster-management.io/api/client/work/clientset/versioned"
	workinformers "open-cluster-management.io/api/client/work/informers/externalversions"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	addonce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/addon"
	clusterce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/cluster"
	csrce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/csr"
	eventce "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/event"
	leasece "open-cluster-management.io/sdk-go/pkg/cloudevents/clients/lease"
	"open-cluster-management.io/sdk-go/pkg/cloudevents/clients/work/payload"
	grpcauthn "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/authn"
	grpcauthz "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/authz/kube"
	grpcoptions "open-cluster-management.io/sdk-go/pkg/cloudevents/server/grpc/options"

	commonoptions "open-cluster-management.io/ocm/pkg/common/options"
	"open-cluster-management.io/ocm/pkg/server/services/addon"
	"open-cluster-management.io/ocm/pkg/server/services/cluster"
	"open-cluster-management.io/ocm/pkg/server/services/csr"
	"open-cluster-management.io/ocm/pkg/server/services/event"
	"open-cluster-management.io/ocm/pkg/server/services/lease"
	"open-cluster-management.io/ocm/pkg/server/services/work"
	"open-cluster-management.io/ocm/pkg/version"
)

func NewGRPCServer() *cobra.Command {
	opts := commonoptions.NewOptions()
	grpcServerOpts := grpcoptions.NewGRPCServerOptions()
	cmdConfig := opts.
		NewControllerCommandConfig(
			"grpc-server",
			version.Get(),
			func(ctx context.Context, controllerContext *controllercmd.ControllerContext) error {
				clients, err := newClients(controllerContext)
				if err != nil {
					return err
				}

				return grpcoptions.NewServer(grpcServerOpts).WithPreStartHooks(clients).WithAuthenticator(
					grpcauthn.NewTokenAuthenticator(clients.kubeClient),
				).WithAuthenticator(
					grpcauthn.NewMtlsAuthenticator(),
				).WithAuthorizer(
					grpcauthz.NewSARAuthorizer(clients.kubeClient),
				).WithService(
					clusterce.ManagedClusterEventDataType,
					cluster.NewClusterService(clients.clusterClient, clients.clusterInformers.Cluster().V1().ManagedClusters()),
				).WithService(
					csrce.CSREventDataType,
					csr.NewCSRService(clients.kubeClient, clients.kubeInformers.Certificates().V1().CertificateSigningRequests()),
				).WithService(
					addonce.ManagedClusterAddOnEventDataType,
					addon.NewAddonService(clients.addonClient, clients.addonInformers.Addon().V1alpha1().ManagedClusterAddOns()),
				).WithService(
					eventce.EventEventDataType,
					event.NewEventService(clients.kubeClient),
				).WithService(
					leasece.LeaseEventDataType,
					lease.NewLeaseService(clients.kubeClient, clients.kubeInformers.Coordination().V1().Leases()),
				).WithService(
					payload.ManifestBundleEventDataType,
					work.NewWorkService(clients.workClient, clients.workInformers.Work().V1().ManifestWorks()),
				).Run(ctx)
			},
			clock.RealClock{},
		)
	cmd := cmdConfig.NewCommandWithContext(context.TODO())
	cmd.Use = "grpc-server"
	cmd.Short = "Start the gRPC Server"

	flags := cmd.Flags()
	opts.AddFlags(flags)
	grpcServerOpts.AddFlags(flags)

	return cmd
}

type clients struct {
	kubeClient       kubernetes.Interface
	clusterClient    clusterv1client.Interface
	workClient       workclientset.Interface
	addonClient      addonv1alpha1client.Interface
	kubeInformers    kubeinformers.SharedInformerFactory
	clusterInformers clusterv1informers.SharedInformerFactory
	workInformers    workinformers.SharedInformerFactory
	addonInformers   addoninformers.SharedInformerFactory
}

func newClients(controllerContext *controllercmd.ControllerContext) (*clients, error) {
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
	return &clients{
		kubeClient:    kubeClient,
		clusterClient: clusterClient,
		addonClient:   addonClient,
		workClient:    workClient,
		kubeInformers: kubeinformers.NewSharedInformerFactoryWithOptions(kubeClient, 30*time.Minute,
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
		clusterInformers: clusterv1informers.NewSharedInformerFactory(clusterClient, 30*time.Minute),
		workInformers:    workinformers.NewSharedInformerFactoryWithOptions(workClient, 30*time.Minute),
		addonInformers:   addoninformers.NewSharedInformerFactory(addonClient, 30*time.Minute),
	}, nil
}

func (h *clients) Run(ctx context.Context) {
	go h.kubeInformers.Start(ctx.Done())
	go h.clusterInformers.Start(ctx.Done())
	go h.workInformers.Start(ctx.Done())
	go h.addonInformers.Start(ctx.Done())
}
