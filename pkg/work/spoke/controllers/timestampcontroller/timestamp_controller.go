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
	annotationKeyGenerationTime = "metrics.open-cluster-management.io/generation-time"
)

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
	logger := klog.FromContext(ctx)
	logger.WithName("manifestwork-timestamp-controller").WithValues("manifestwork", manifestWorkName)
	logger.V(4).Info("Reconciling ManifestWork")
	manifestWork, err := c.manifestWorkLister.Get(manifestWorkName)
	if errors.IsNotFound(err) {
		// work not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return fmt.Errorf("unable to fetch manifestwork %q: %w", manifestWorkName, err)
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
		!oldGenerationTime.StartTime.IsZero() && !oldGenerationTime.AppliedTime.IsZero() {
		// generation does not change, will not update the applied time
		return nil
	}

	generationTime := GenerationTimestamp{
		Generation: manifestWork.Generation,
		StartTime:  manifestWork.CreationTimestamp.Time,
	}

	for _, field := range manifestWork.ManagedFields {
		if field.Time == nil || field.FieldsV1 == nil {
			continue
		}

		if strings.Contains(string(field.FieldsV1.Raw), "\"f:spec\":") {
			if field.Time.After(generationTime.StartTime) {
				generationTime.StartTime = field.Time.Time
			}
		}

	}

	// set the applied time if it matches
	cond := meta.FindStatusCondition(manifestWork.Status.Conditions, workapiv1.WorkApplied)
	if cond != nil && cond.Status == metav1.ConditionTrue && cond.ObservedGeneration == generationTime.Generation {
		generationTime.AppliedTime = cond.LastTransitionTime.Time

		// if the generation changes, but the status of the manifestwork Applied condition does not change, the
		// lastTransitionTime will not change, so it is possible that the lastTransitionTime is befor the generation
		// start time
		if generationTime.AppliedTime.Before(generationTime.StartTime) {
			// TODO: review whether time.Now() makes sence here
			generationTime.AppliedTime = time.Now()
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

type GenerationTimestamp struct {
	Generation  int64     `json:"generation"`
	StartTime   time.Time `json:"startTime"`
	AppliedTime time.Time `json:"appliedTime"`
}
