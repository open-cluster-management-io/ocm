package importer

import (
	"context"
	"errors"
	"fmt"

	"github.com/openshift/api"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehelper"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	appsv1 "k8s.io/api/apps/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"

	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	operatorclient "open-cluster-management.io/api/client/operator/clientset/versioned"
	v1 "open-cluster-management.io/api/cluster/v1"
	operatorv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/helpers"
	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/operator/helpers/chart"
	cloudproviders "open-cluster-management.io/ocm/pkg/registration/hub/importer/providers"
)

const (
	kluterletNamespace              = "open-cluster-management-agent"
	ManagedClusterConditionImported = "ManagedClusterImportSucceeded"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	utilruntime.Must(api.InstallKube(genericScheme))
	utilruntime.Must(apiextensionsv1.AddToScheme(genericScheme))
	utilruntime.Must(operatorv1.Install(genericScheme))
}

// KlusterletConfigRenderer renders the config for klusterlet chart.
type KlusterletConfigRenderer func(
	ctx context.Context, config *chart.KlusterletChartConfig) (*chart.KlusterletChartConfig, error)

type Importer struct {
	providers     []cloudproviders.Interface
	clusterClient clusterclientset.Interface
	clusterLister clusterlisterv1.ManagedClusterLister
	renders       []KlusterletConfigRenderer
	patcher       patcher.Patcher[*v1.ManagedCluster, v1.ManagedClusterSpec, v1.ManagedClusterStatus]
}

// NewImporter creates an auto import controller
func NewImporter(
	renders []KlusterletConfigRenderer,
	clusterClient clusterclientset.Interface,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	providers []cloudproviders.Interface,
	recorder events.Recorder) factory.Controller {
	controllerName := "managed-cluster-importer"
	syncCtx := factory.NewSyncContext(controllerName, recorder)

	i := &Importer{
		providers:     providers,
		clusterClient: clusterClient,
		clusterLister: clusterInformer.Lister(),
		renders:       renders,
		patcher: patcher.NewPatcher[
			*v1.ManagedCluster, v1.ManagedClusterSpec, v1.ManagedClusterStatus](
			clusterClient.ClusterV1().ManagedClusters()),
	}

	for _, provider := range providers {
		provider.Register(syncCtx)
	}

	return factory.New().WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, clusterInformer.Informer()).
		WithSyncContext(syncCtx).WithSync(i.sync).ToController(controllerName, recorder)
}

func (i *Importer) sync(ctx context.Context, syncCtx factory.SyncContext) error {
	clusterName := syncCtx.QueueKey()
	logger := klog.FromContext(ctx)
	logger.V(4).Info("Reconciling key", "clusterName", clusterName)

	cluster, err := i.clusterLister.Get(clusterName)
	switch {
	case apierrors.IsNotFound(err):
		return nil
	case err != nil:
		return err
	}

	// If the cluster is imported, skip the reconcile
	if meta.IsStatusConditionTrue(cluster.Status.Conditions, ManagedClusterConditionImported) {
		return nil
	}

	// get provider from the provider list
	var provider cloudproviders.Interface
	for _, p := range i.providers {
		if p.IsManagedClusterOwner(cluster) {
			provider = p
			break
		}
	}
	if provider == nil {
		logger.V(2).Info("provider not found for cluster", "cluster", cluster.Name)
		return nil
	}

	newCluster := cluster.DeepCopy()
	newCluster, err = i.reconcile(ctx, logger, syncCtx.Recorder(), provider, newCluster)
	updated, updatedErr := i.patcher.PatchStatus(ctx, newCluster, newCluster.Status, cluster.Status)
	if updatedErr != nil {
		return updatedErr
	}
	if updated {
		syncCtx.Recorder().Eventf(
			"ManagedClusterImported", "managed cluster %s is imported", clusterName)
	}
	var rqe helpers.RequeueError
	if err != nil && errors.As(err, &rqe) {
		syncCtx.Queue().AddAfter(clusterName, rqe.RequeueTime)
		return nil
	}

	return err
}

func (i *Importer) reconcile(
	ctx context.Context,
	logger klog.Logger,
	recorder events.Recorder,
	provider cloudproviders.Interface,
	cluster *v1.ManagedCluster) (*v1.ManagedCluster, error) {
	clients, err := provider.Clients(ctx, cluster)
	if err != nil {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   ManagedClusterConditionImported,
			Status: metav1.ConditionFalse,
			Reason: "KubeConfigGetFailed",
			Message: fmt.Sprintf("failed to get kubeconfig. See errors:\n%s",
				err.Error()),
		})
		return cluster, err
	}

	if clients == nil {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:    ManagedClusterConditionImported,
			Status:  metav1.ConditionFalse,
			Reason:  "KubeConfigNotFound",
			Message: "Secret for kubeconfig is not found.",
		})
		return cluster, nil
	}

	// render the klusterlet chart config
	klusterletChartConfig := &chart.KlusterletChartConfig{
		ReplicaCount:    1,
		CreateNamespace: true,
		Klusterlet: chart.KlusterletConfig{
			Create:      true,
			ClusterName: cluster.Name,
			ResourceRequirement: &operatorv1.ResourceRequirement{
				Type: operatorv1.ResourceQosClassDefault,
			},
		},
	}
	for _, renderer := range i.renders {
		klusterletChartConfig, err = renderer(ctx, klusterletChartConfig)
		if err != nil {
			meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
				Type:   ManagedClusterConditionImported,
				Status: metav1.ConditionFalse,
				Reason: "ConfigRendererFailed",
				Message: fmt.Sprintf("failed to render config. See errors:\n%s",
					err.Error()),
			})
			return cluster, err
		}
	}
	crdObjs, rawObjs, err := chart.RenderKlusterletChart(klusterletChartConfig, kluterletNamespace)
	if err != nil {
		return cluster, err
	}
	rawManifests := append(crdObjs, rawObjs...) //nolint:gocritic

	clientHolder := resourceapply.NewKubeClientHolder(clients.KubeClient).
		WithAPIExtensionsClient(clients.APIExtClient).WithDynamicClient(clients.DynamicClient)
	cache := resourceapply.NewResourceCache()
	var results []resourceapply.ApplyResult
	for _, manifest := range rawManifests {
		requiredObj, _, err := genericCodec.Decode(manifest, nil, nil)
		if err != nil {
			logger.Error(err, "failed to decode manifest", "manifest", manifest)
			return cluster, err
		}
		result := resourceapply.ApplyResult{}
		switch t := requiredObj.(type) {
		case *appsv1.Deployment:
			result.Result, result.Changed, result.Error = resourceapply.ApplyDeployment(
				ctx, clients.KubeClient.AppsV1(), recorder, t, 0)
			results = append(results, result)
		case *operatorv1.Klusterlet:
			result.Result, result.Changed, result.Error = ApplyKlusterlet(
				ctx, clients.OperatorClient, recorder, t)
			results = append(results, result)
		default:
			tempResults := resourceapply.ApplyDirectly(ctx, clientHolder, recorder, cache,
				func(name string) ([]byte, error) {
					return manifest, nil
				},
				"manifest")
			results = append(results, tempResults...)
		}
	}

	var errs []error
	for _, result := range results {
		if result.Error != nil {
			errs = append(errs, result.Error)
		}
	}
	if len(errs) > 0 {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   ManagedClusterConditionImported,
			Status: metav1.ConditionFalse,
			Reason: "ImportFailed",
			Message: fmt.Sprintf("failed to import the klusterlet. See errors:\n%s",
				utilerrors.NewAggregate(errs).Error()),
		})
	} else {
		meta.SetStatusCondition(&cluster.Status.Conditions, metav1.Condition{
			Type:   ManagedClusterConditionImported,
			Status: metav1.ConditionTrue,
			Reason: "ImportSucceed",
		})
	}

	return cluster, utilerrors.NewAggregate(errs)
}

func ApplyKlusterlet(
	ctx context.Context,
	client operatorclient.Interface,
	recorder events.Recorder,
	required *operatorv1.Klusterlet) (*operatorv1.Klusterlet, bool, error) {
	existing, err := client.OperatorV1().Klusterlets().Get(ctx, required.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		requiredCopy := required.DeepCopy()
		actual, err := client.OperatorV1().Klusterlets().Create(ctx, requiredCopy, metav1.CreateOptions{})
		resourcehelper.ReportCreateEvent(recorder, required, err)
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	modified := pointer.Bool(false)
	existingCopy := existing.DeepCopy()
	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)

	if !*modified && equality.Semantic.DeepEqual(existingCopy.Spec, required.Spec) {
		return existingCopy, false, nil
	}

	existingCopy.Spec = required.Spec
	actual, err := client.OperatorV1().Klusterlets().Update(ctx, existingCopy, metav1.UpdateOptions{})
	resourcehelper.ReportUpdateEvent(recorder, required, err)
	return actual, true, err
}
