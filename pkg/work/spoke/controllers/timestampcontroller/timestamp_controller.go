package timestampcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"

	workv1client "open-cluster-management.io/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "open-cluster-management.io/api/client/work/informers/externalversions/work/v1"
	worklister "open-cluster-management.io/api/client/work/listers/work/v1"
	workapiv1 "open-cluster-management.io/api/work/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/pkg/common/queue"
)

const (
	// TODO: move to the api repo
	annotationKeyGenerationTime = "metrics.open-cluster-management.io/observed-generation-time"
)

type GenerationTimestamp struct {
	Generation                 int64     `json:"generation"`
	CreatedTime                time.Time `json:"createdTime"`
	AppliedTime                time.Time `json:"appliedTime"`
	FirstGenerationAppliedTime time.Time `json:"firstGenerationAppliedTime"`
}

// TimestampController is to record the timestamp when the manifestwork Applied condition becomes True.
type TimestampController struct {
	patcher            patcher.Patcher[*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus]
	manifestWorkLister worklister.ManifestWorkNamespaceLister
}

// NewTimestampController returns a TimestampController
func NewTimestampController(
	recorder events.Recorder,
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister,
	syncInterval time.Duration,
) factory.Controller {
	controller := &TimestampController{
		patcher: patcher.NewPatcher[
			*workapiv1.ManifestWork, workapiv1.ManifestWorkSpec, workapiv1.ManifestWorkStatus](
			manifestWorkClient),
		manifestWorkLister: manifestWorkLister,
	}

	return factory.New().
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, manifestWorkInformer.Informer()).
		WithSync(controller.sync).ResyncEvery(syncInterval).ToController("TimestampController", recorder)
}

func (c *TimestampController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	manifestWorkName := controllerContext.QueueKey()
	logger := klog.FromContext(ctx).WithName("manifestwork-timestamp-ctrl").WithValues("manifestwork", manifestWorkName)
	logger.V(4).Info("Reconciling ManifestWork")
	manifestWork, err := c.manifestWorkLister.Get(manifestWorkName)
	if errors.IsNotFound(err) {
		// work not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to fetch manifestwork %q: %w", manifestWorkName, err)
	}

	if !manifestWork.DeletionTimestamp.IsZero() {
		return nil
	}

	err = c.syncManifestWork(ctx, logger, manifestWork)
	if err != nil {
		return fmt.Errorf("unable to sync manifestwork %q: %w", manifestWork.Name, err)
	}
	return nil
}

func (c *TimestampController) syncManifestWork(ctx context.Context, logger klog.Logger,
	originalManifestWork *workapiv1.ManifestWork) error {
	manifestWork := originalManifestWork.DeepCopy()

	oldGenerationTime := c.getGenerationTime(manifestWork)
	if oldGenerationTime != nil && oldGenerationTime.Generation == manifestWork.Generation &&
		!oldGenerationTime.CreatedTime.IsZero() && !oldGenerationTime.AppliedTime.IsZero() {
		// generation does not change, will not update the applied time
		return nil
	}

	generationTime := GenerationTimestamp{
		Generation:  manifestWork.Generation,
		CreatedTime: manifestWork.CreationTimestamp.Time,
	}
	if oldGenerationTime != nil {
		logger.V(4).Info("keep the first generation applied time we observed unchanged",
			"firstGenerationAppliedTime", oldGenerationTime.FirstGenerationAppliedTime)
		generationTime.FirstGenerationAppliedTime = oldGenerationTime.FirstGenerationAppliedTime
	}

	// set the generation created time
	for _, field := range manifestWork.ManagedFields {
		if field.Time == nil || field.FieldsV1 == nil {
			continue
		}

		if strings.Contains(string(field.FieldsV1.Raw), "\"f:spec\":") {
			if field.Time.After(generationTime.CreatedTime) {
				generationTime.CreatedTime = field.Time.Time
			}
		}
	}

	// set the generation applied time if it matches
	cond := meta.FindStatusCondition(manifestWork.Status.Conditions, workapiv1.WorkApplied)
	if cond != nil && cond.Status == metav1.ConditionTrue {
		// if the first generation applied time does not set, set the value we observed
		if generationTime.FirstGenerationAppliedTime.IsZero() {
			logger.V(4).Info("set the first generation applied time to the Applied condition last transtion time",
				"lastTransitionTime", cond.LastTransitionTime.Time)
			generationTime.FirstGenerationAppliedTime = cond.LastTransitionTime.Time
		}
		if cond.ObservedGeneration == generationTime.Generation {
			generationTime.AppliedTime = cond.LastTransitionTime.Time

			// if the generation changes, but the status of the manifestwork Applied condition does not change, the
			// lastTransitionTime will not change, so it is possible that the lastTransitionTime is befor the generation
			// created time
			if generationTime.AppliedTime.Before(generationTime.CreatedTime) {
				// TODO: review whether time.Now() makes sence here
				generationTime.AppliedTime = time.Now()
				if generationTime.AppliedTime.Before(generationTime.CreatedTime) {
					// the created time is retrieved from the managed field, which is generated on the hub cluster,
					// if the hub cluster clock and the managed cluster clock have a slightly difference(not
					// totally synced), it may run into here
					generationTime.AppliedTime = generationTime.CreatedTime
				}
			}
		}
	}

	// skip if the generation time annotation does not change.
	if equality.Semantic.DeepEqual(oldGenerationTime, generationTime) {
		logger.V(4).Info("generation time has been recorded, skip", "generation", generationTime.Generation)
		return nil
	}

	generationTimeValue, err := json.Marshal(generationTime)
	if err != nil {
		return err
	}
	if manifestWork.Annotations == nil {
		manifestWork.Annotations = map[string]string{}
	}
	manifestWork.Annotations[annotationKeyGenerationTime] = string(generationTimeValue)

	// update annotation of manifestwork. if this conflicts, try again later
	_, err = c.patcher.PatchLabelAnnotations(ctx, manifestWork,
		manifestWork.ObjectMeta, originalManifestWork.ObjectMeta)
	if err != nil {
		return err
	}
	logger.V(4).Info("generation timestamp annotation updated", "value", string(generationTimeValue))
	return nil
}

func (c *TimestampController) getGenerationTime(mw *workapiv1.ManifestWork) *GenerationTimestamp {
	value, ok := mw.Annotations[annotationKeyGenerationTime]
	if !ok || len(value) == 0 {
		return nil
	}

	generationTime := &GenerationTimestamp{}
	err := json.Unmarshal([]byte(value), generationTime)
	if err != nil {
		return nil
	}
	return generationTime
}
