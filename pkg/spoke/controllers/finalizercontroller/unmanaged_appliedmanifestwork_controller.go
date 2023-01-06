package finalizercontroller

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/work/pkg/helper"
)

const byWorkNameAndAgentID = "UnManagedAppliedManifestWork-byWorkNameAndAgentID"

// UnManagedAppliedWorkController deletes unmanaged applied works.
type UnManagedAppliedWorkController struct {
	manifestWorkLister         worklister.ManifestWorkNamespaceLister
	appliedManifestWorkClient  workv1client.AppliedManifestWorkInterface
	appliedManifestWorkLister  worklister.AppliedManifestWorkLister
	appliedManifestWorkIndexer cache.Indexer
	hubHash                    string
	agentID                    string
}

func NewUnManagedAppliedWorkController(
	recorder events.Recorder,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	appliedManifestWorkClient workv1client.AppliedManifestWorkInterface,
	appliedManifestWorkInformer workinformer.AppliedManifestWorkInformer,
	hubHash, agentID string,
) factory.Controller {

	controller := &UnManagedAppliedWorkController{
		manifestWorkLister:         manifestWorkLister,
		appliedManifestWorkClient:  appliedManifestWorkClient,
		appliedManifestWorkLister:  appliedManifestWorkInformer.Lister(),
		appliedManifestWorkIndexer: appliedManifestWorkInformer.Informer().GetIndexer(),
		hubHash:                    hubHash,
		agentID:                    agentID,
	}

	err := appliedManifestWorkInformer.Informer().AddIndexers(cache.Indexers{
		byWorkNameAndAgentID: indexByWorkNameAndAgentID,
	})
	if err != nil {
		utilruntime.HandleError(err)
	}

	return factory.New().
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return fmt.Sprintf("%s-%s", hubHash, accessor.GetName())
		}, manifestWorkInformer.Informer()).
		WithFilteredEventsInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, helper.AppliedManifestworkHubHashFilter(hubHash), appliedManifestWorkInformer.Informer()).
		WithSync(controller.sync).ToController("UnManagedAppliedManifestWork", recorder)
}

func (m *UnManagedAppliedWorkController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	appliedManifestWorkName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ManifestWork %q", appliedManifestWorkName)

	appliedManifestWork, err := m.appliedManifestWorkLister.Get(appliedManifestWorkName)
	if errors.IsNotFound(err) {
		// work not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	// We delete the old AppliedManifestWork only when the related ManifestWork is applied with the new
	// AppliedManifestWork as the new owner. This can avoid deleting the old AppliedManifestWork prematurely
	// before the new AppliedManifestWork takes the ownership of the applied resources.
	manifestWork, err := m.manifestWorkLister.Get(appliedManifestWork.Spec.ManifestWorkName)
	if errors.IsNotFound(err) {
		// work not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}
	if !meta.IsStatusConditionTrue(manifestWork.Status.Conditions, workapiv1.WorkApplied) {
		// the work is not applied, do nothing.
		return nil
	}

	unManagedAppliedWorks, err := m.getUnManagedAppliedManifestWorksByIndex(appliedManifestWork.Spec.ManifestWorkName, appliedManifestWork.Spec.AgentID)
	if err != nil {
		return err
	}

	var errs []error
	for _, appliedWork := range unManagedAppliedWorks {
		klog.V(2).Infof("Delete appliedWork %s since it is not managed by agent %s anymore", appliedWork.Name, m.agentID)
		err := m.appliedManifestWorkClient.Delete(ctx, appliedWork.Name, metav1.DeleteOptions{})
		if err != nil {
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)
}

// getUnManagedAppliedManifestWorksByIndex finds appliedmanifestwork with the same workname and agent ID but different hubhash.
// These appliedManifestWorks is considered to be not managed by this work agent anymore and should be deleted.
// The reason of marking them as unmanaged is because the only reason under this conditions is work agent is switched to connect
// to a recovered hub or a fresh new hub. Those appliedmanifestwork needs to be deleted to avoid conflict with the newly connected
// hub.
func (m *UnManagedAppliedWorkController) getUnManagedAppliedManifestWorksByIndex(workName, agentID string) ([]*workapiv1.AppliedManifestWork, error) {
	index := agentIDWorkNameIndex(workName, agentID)
	items, err := m.appliedManifestWorkIndexer.ByIndex(byWorkNameAndAgentID, index)
	if err != nil {
		return nil, err
	}

	ret := make([]*workapiv1.AppliedManifestWork, 0, len(items))
	for _, item := range items {
		appliedWork := item.(*workapiv1.AppliedManifestWork)
		if appliedWork.Spec.HubHash == m.hubHash {
			continue
		}
		ret = append(ret, item.(*workapiv1.AppliedManifestWork))
	}

	return ret, nil
}

func indexByWorkNameAndAgentID(obj interface{}) ([]string, error) {
	appliedWork, ok := obj.(*workapiv1.AppliedManifestWork)
	if !ok {
		return []string{}, fmt.Errorf("obj is supposed to be a AppliedManifestWork, but is %T", obj)
	}

	return []string{agentIDWorkNameIndex(appliedWork.Spec.ManifestWorkName, appliedWork.Spec.AgentID)}, nil
}

func agentIDWorkNameIndex(workName, agentID string) string {
	return fmt.Sprintf("%s/%s", workName, agentID)
}
