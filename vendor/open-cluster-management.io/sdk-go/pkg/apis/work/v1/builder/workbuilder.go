package builder

import (
	"context"
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/errors"
	"open-cluster-management.io/api/utils/work/v1/workapplier"
	workapiv1 "open-cluster-management.io/api/work/v1"
)

// DefaultManifestLimit is the max size of manifests data which is 50k bytes by default.
const DefaultManifestLimit = 50 * 1024

// DefaultManifestThreshold is the threshold size of manifests for the first new created manifestWork.
const DefaultManifestThreshold = 0.8

type manifestKey struct {
	gvk             schema.GroupVersionKind
	namespace, name string
}

type manifestSize struct {
	manifest workapiv1.Manifest
	// size is the size of the manifest
	size int
}

type manifestWorkBuffer struct {
	work *workapiv1.ManifestWork
	// buffer is the remaining size to append the manifests in the manifestWork
	buffer int
}

type requiredManifestMapper map[manifestKey]workapiv1.Manifest

// GenerateManifestWorkObjectMeta is to generate a new manifestWork meta, need to make sure the name is unique.
type GenerateManifestWorkObjectMeta func(index int) metav1.ObjectMeta

type WorkBuilder struct {
	manifestLimit int
}

type internalWorkBuilder struct {
	workBuilder                    *WorkBuilder
	generateManifestWorkObjectMeta GenerateManifestWorkObjectMeta
	deletionOption                 *workapiv1.DeleteOption
	executorOption                 *workapiv1.ManifestWorkExecutor
	existingManifestWorks          []workapiv1.ManifestWork
	manifestConfigOption           []workapiv1.ManifestConfigOption
	annotations                    map[string]string
}
type WorkBuilderOption func(*internalWorkBuilder) *internalWorkBuilder

func ExistingManifestWorksOption(works []workapiv1.ManifestWork) WorkBuilderOption {
	return func(factory *internalWorkBuilder) *internalWorkBuilder {
		factory.existingManifestWorks = works
		return factory
	}
}

func DeletionOption(option *workapiv1.DeleteOption) WorkBuilderOption {
	return func(builder *internalWorkBuilder) *internalWorkBuilder {
		builder.deletionOption = option
		return builder
	}
}

func ManifestConfigOption(option []workapiv1.ManifestConfigOption) WorkBuilderOption {
	return func(builder *internalWorkBuilder) *internalWorkBuilder {
		builder.manifestConfigOption = option
		return builder
	}
}

func ManifestWorkExecutorOption(executor *workapiv1.ManifestWorkExecutor) WorkBuilderOption {
	return func(builder *internalWorkBuilder) *internalWorkBuilder {
		builder.executorOption = executor
		return builder
	}
}

func ManifestAnnotations(annotations map[string]string) WorkBuilderOption {
	return func(builder *internalWorkBuilder) *internalWorkBuilder {
		builder.annotations = annotations
		return builder
	}
}

func NewWorkBuilder() *WorkBuilder {
	return &WorkBuilder{
		manifestLimit: int(float64(DefaultManifestLimit) * DefaultManifestThreshold),
	}
}

// WithManifestsLimit is to set the total limit size of manifests in manifestWork. the unit of manifestsLimit is byte.
// the actual size of manifests will be the 80% of the limit.
// This is an optional setting, and the default limit is 50k.
func (w *WorkBuilder) WithManifestsLimit(manifestsLimit int) *WorkBuilder {
	w.manifestLimit = int(float64(manifestsLimit) * DefaultManifestThreshold)
	return w
}

func (w *WorkBuilder) newInternalWorkBuilder(generateManifestWorkObjectMeta GenerateManifestWorkObjectMeta,
	options ...WorkBuilderOption) *internalWorkBuilder {
	factory := &internalWorkBuilder{
		workBuilder:                    w,
		generateManifestWorkObjectMeta: generateManifestWorkObjectMeta,
	}

	for _, opt := range options {
		factory = opt(factory)
	}
	return factory
}

// Build is to build a set of applied/deleted manifestWorks with the objects and options.
func (w *WorkBuilder) Build(objects []runtime.Object,
	generateManifestWorkObjectMeta GenerateManifestWorkObjectMeta,
	options ...WorkBuilderOption) (applied, deleted []*workapiv1.ManifestWork, err error) {
	builder := w.newInternalWorkBuilder(generateManifestWorkObjectMeta, options...)
	return builder.buildManifestWorks(objects)
}

// BuildAndApply is to build a set manifestWorks using the objects and update the existing manifestWorks.
func (w *WorkBuilder) BuildAndApply(ctx context.Context,
	objects []runtime.Object,
	generateManifestWorkObjectMeta GenerateManifestWorkObjectMeta,
	workApplier *workapplier.WorkApplier,
	options ...WorkBuilderOption) error {
	appliedWorks, deletedWorks, err := w.Build(objects, generateManifestWorkObjectMeta, options...)
	if err != nil {
		return err
	}

	var errs []error
	for _, work := range appliedWorks {
		if _, err = workApplier.Apply(ctx, work); err != nil {
			errs = append(errs, err)
		}
	}

	for _, work := range deletedWorks {
		if err = workApplier.Delete(ctx, work.Namespace, work.Name); err != nil {
			errs = append(errs, err)
		}
	}

	if len(errs) != 0 {
		return errors.NewAggregate(errs)
	}
	return nil
}

func (f *internalWorkBuilder) buildManifestWorks(objects []runtime.Object) (appliedWorks, deletedWorks []*workapiv1.ManifestWork, err error) {
	var updatedWorks []manifestWorkBuffer

	requiredMapper, err := generateRequiredManifestMapper(objects)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate required mapper.err %v", err)
	}

	// this step to update the existing manifestWorks, update the existing manifests and delete non-existing manifest
	for _, existingWork := range f.existingManifestWorks {
		// new a work with init work meta and keep the existing work name.
		requiredWork := f.initManifestWorkWithName(existingWork.Name)

		for _, manifest := range existingWork.Spec.Workload.Manifests {
			key, err := generateManifestKey(manifest)
			if err != nil {
				return nil, nil, err
			}

			// currently,we have 80% threshold for the size of manifests, update directly.
			// TODO: need to consider if the size of updated manifests is more then the limit of manifestWork.
			if _, ok := requiredMapper[key]; ok {
				requiredWork.Spec.Workload.Manifests = append(requiredWork.Spec.Workload.Manifests, requiredMapper[key])
				delete(requiredMapper, key)
				continue
			}
		}
		updatedWorks = append(updatedWorks, manifestWorkBuffer{
			work:   requiredWork,
			buffer: f.bufferOfManifestWork(requiredWork),
		})
	}

	// there are new added manifests
	if len(requiredMapper) != 0 {
		var requiredManifests []manifestSize
		for _, manifest := range requiredMapper {
			requiredManifests = append(requiredManifests, manifestSize{manifest: manifest, size: manifest.Size()})
		}

		// sort from big to small by size
		sort.SliceStable(requiredManifests, func(i, j int) bool {
			return requiredManifests[j].size < requiredManifests[i].size
		})

		var newManifests []workapiv1.Manifest
		// the manifest will be filled into the existing work with the max buffer.
		// if cannot, will be filled into a new work.
		for manifestIndex := 0; manifestIndex < len(requiredManifests); manifestIndex++ {
			maxWorkBuffer := getMaxManifestWorkBuffer(updatedWorks)
			if maxWorkBuffer != nil && requiredManifests[manifestIndex].size < maxWorkBuffer.buffer {
				maxWorkBuffer.work.Spec.Workload.Manifests = append(maxWorkBuffer.work.Spec.Workload.Manifests,
					requiredManifests[manifestIndex].manifest)
				maxWorkBuffer.buffer = maxWorkBuffer.buffer - requiredManifests[manifestIndex].size
				continue
			}

			newManifests = append(newManifests, requiredManifests[manifestIndex].manifest)
		}

		if len(newManifests) != 0 {
			newWorks, err := f.newManifestWorks(newManifests, len(updatedWorks))
			if err != nil {
				return nil, nil, err
			}
			appliedWorks = append(appliedWorks, newWorks...)
		}
	}

	for workIndex := 0; workIndex < len(updatedWorks); workIndex++ {
		if len(updatedWorks[workIndex].work.Spec.Workload.Manifests) == 0 {
			deletedWorks = append(deletedWorks, updatedWorks[workIndex].work)
			continue
		}
		appliedWorks = append(appliedWorks, updatedWorks[workIndex].work)
	}

	return appliedWorks, deletedWorks, nil
}

func (f *internalWorkBuilder) newManifestWorks(manifests []workapiv1.Manifest, workIndex int) ([]*workapiv1.ManifestWork, error) {
	var manifestWorks []*workapiv1.ManifestWork
	var totalSize = 0

	work := f.initManifestWork(workIndex)
	manifestWorks = append(manifestWorks, work)
	for i := 0; i < len(manifests); i++ {
		if totalSize+manifests[i].Size() < f.workBuilder.manifestLimit {
			work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, manifests[i])
			totalSize = totalSize + manifests[i].Size()
			continue
		}

		workIndex = workIndex + 1
		work = f.initManifestWork(workIndex)
		manifestWorks = append(manifestWorks, work)
		work.Spec.Workload.Manifests = append(work.Spec.Workload.Manifests, manifests[i])
		totalSize = manifests[i].Size()
	}

	return manifestWorks, nil
}

func (f *internalWorkBuilder) initManifestWork(index int) *workapiv1.ManifestWork {
	work := &workapiv1.ManifestWork{
		ObjectMeta: f.generateManifestWorkObjectMeta(index),
	}
	f.setManifestWorkOptions(work)
	f.setAnnotations(work)
	return work
}

// init a work with existing name
func (f *internalWorkBuilder) initManifestWorkWithName(workName string) *workapiv1.ManifestWork {
	work := f.initManifestWork(0)
	work.SetName(workName)
	return work
}

func (f *internalWorkBuilder) setManifestWorkOptions(work *workapiv1.ManifestWork) {
	// currently we set the options to each generated manifestWorks
	work.Spec.DeleteOption = f.deletionOption
	work.Spec.ManifestConfigs = f.manifestConfigOption
	work.Spec.Executor = f.executorOption
}

func (f *internalWorkBuilder) setAnnotations(work *workapiv1.ManifestWork) {
	work.SetAnnotations(f.annotations)
}

func (f *internalWorkBuilder) bufferOfManifestWork(work *workapiv1.ManifestWork) int {
	totalSize := 0
	for _, manifest := range work.Spec.Workload.Manifests {
		totalSize = totalSize + manifest.Size()
	}
	if totalSize > f.workBuilder.manifestLimit {
		return 0
	}
	return f.workBuilder.manifestLimit - totalSize
}

func getMaxManifestWorkBuffer(workBuffers []manifestWorkBuffer) *manifestWorkBuffer {
	var maxWorkBuffer *manifestWorkBuffer
	maxBuffer := 0
	for key, workBuffer := range workBuffers {
		if workBuffer.buffer > maxBuffer {
			maxWorkBuffer = &workBuffers[key]
			maxBuffer = workBuffer.buffer
		}
	}
	return maxWorkBuffer
}

func buildManifest(object runtime.Object) (workapiv1.Manifest, error) {
	rawObject, err := runtime.Encode(unstructured.UnstructuredJSONScheme, object)
	if err != nil {
		return workapiv1.Manifest{}, fmt.Errorf("failed to encode object %v, err: %v", object, err)
	}
	manifest := workapiv1.Manifest{RawExtension: runtime.RawExtension{Raw: rawObject}}
	return manifest, nil
}

func generateManifestKey(manifest workapiv1.Manifest) (manifestKey, error) {
	var object runtime.Object
	var err error
	if manifest.Object != nil {
		object = manifest.Object
	} else {
		object, err = runtime.Decode(unstructured.UnstructuredJSONScheme, manifest.Raw)
		if err != nil {
			return manifestKey{}, err
		}
	}

	gvk := object.GetObjectKind().GroupVersionKind()
	if gvk.Kind == "" || gvk.Version == "" {
		return manifestKey{}, fmt.Errorf("got empty kind/version from object %v", object)
	}
	key := manifestKey{
		gvk: gvk,
	}

	accessor, err := meta.Accessor(object)
	if err != nil {
		return key, err
	}
	key.namespace = accessor.GetNamespace()
	key.name = accessor.GetName()
	return key, nil
}

func generateRequiredManifestMapper(objects []runtime.Object) (requiredManifestMapper, error) {
	var mapper = requiredManifestMapper{}
	for _, object := range objects {
		rawObject, err := runtime.Encode(unstructured.UnstructuredJSONScheme, object)
		if err != nil {
			return nil, fmt.Errorf("failed to encode object %v, err: %v", object, err)
		}

		gvk := object.GetObjectKind().GroupVersionKind()
		if gvk.Kind == "" || gvk.Version == "" {
			return nil, fmt.Errorf("got empty kind/version from object %v", object)
		}
		key := manifestKey{
			gvk: gvk,
		}
		accessor, err := meta.Accessor(object)
		if err != nil {
			return nil, err
		}
		key.namespace = accessor.GetNamespace()
		key.name = accessor.GetName()
		mapper[key] = workapiv1.Manifest{RawExtension: runtime.RawExtension{Raw: rawObject}}
	}
	return mapper, nil
}
