package crdmanager

import (
	"context"
	"errors"
	"fmt"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	"k8s.io/utils/pointer"
	"strings"

	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	versionutil "k8s.io/apimachinery/pkg/util/version"
	"k8s.io/klog/v2"
	"open-cluster-management.io/registration-operator/pkg/version"
)

// versionAnnotationKey is an annotation key on crd resources to mark the ocm version of the crds.
const (
	versionAnnotationKey = "operator.open-cluster-management.io/version"
	// defaultVersion is set if gitVersion cannot be obtained. It is the lownest version so crd is updated as long
	// as it has a higher version. It also ensures the crd spec is still compared
	// for update when version is not obtained.
	defaultVersion = "0.0.0"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()
)

func init() {
	utilruntime.Must(apiextensionsv1.AddToScheme(genericScheme))
	utilruntime.Must(apiextensionsv1beta1.AddToScheme(genericScheme))
}

type CRD interface {
	*apiextensionsv1.CustomResourceDefinition | *apiextensionsv1beta1.CustomResourceDefinition
}

type Manager[T CRD] struct {
	client  crdClient[T]
	equal   func(old, new T) bool
	version *versionutil.Version
}

type crdClient[T CRD] interface {
	Get(ctx context.Context, name string, opt metav1.GetOptions) (T, error)
	Create(ctx context.Context, obj T, opt metav1.CreateOptions) (T, error)
	Update(ctx context.Context, obj T, opt metav1.UpdateOptions) (T, error)
	Delete(ctx context.Context, name string, opt metav1.DeleteOptions) error
}

type RemainingCRDError struct {
	RemainingCRDs []string
}

func (r *RemainingCRDError) Error() string {
	return fmt.Sprintf("Thera are still reaming CRDs: %s", strings.Join(r.RemainingCRDs, ","))
}

func NewManager[T CRD](client crdClient[T], equalFunc func(old, new T) bool) *Manager[T] {
	gitVersion := version.Get().GitVersion
	if len(gitVersion) == 0 {
		gitVersion = defaultVersion
	}
	v, err := versionutil.ParseGeneric(gitVersion)
	if err != nil {
		utilruntime.HandleError(err)
	}
	manager := &Manager[T]{
		client:  client,
		equal:   equalFunc,
		version: v,
	}

	return manager
}

func (m *Manager[T]) CleanOne(ctx context.Context, name string, skip bool) error {
	// remove version annotation if skip clean
	if skip {
		existing, err := m.client.Get(ctx, name, metav1.GetOptions{})
		switch {
		case apierrors.IsNotFound(err):
			return nil
		case err != nil:
			return err
		}
		accessor, err := meta.Accessor(existing)
		if err != nil {
			return err
		}
		annotations := accessor.GetAnnotations()
		if annotations == nil {
			return nil
		}
		v, ok := annotations[versionAnnotationKey]
		if !ok {
			return nil
		}
		cnt, err := m.version.Compare(v)
		if err != nil {
			return err
		}
		if cnt != 0 {
			return nil
		}
		delete(annotations, versionAnnotationKey)
		accessor.SetAnnotations(annotations)
		_, err = m.client.Update(ctx, existing, metav1.UpdateOptions{})
		return err
	}

	err := m.client.Delete(ctx, name, metav1.DeleteOptions{})
	switch {
	case apierrors.IsNotFound(err):
		return nil
	case err == nil:
		return &RemainingCRDError{RemainingCRDs: []string{name}}
	}

	return err
}

func (m *Manager[T]) Clean(ctx context.Context, skip bool, manifests resourceapply.AssetFunc, files ...string) error {
	var errs []error
	var remainingCRDs []string

	for _, file := range files {
		objBytes, err := manifests(file)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		requiredObj, _, err := genericCodec.Decode(objBytes, nil, nil)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		accessor, err := meta.Accessor(requiredObj)
		if err != nil {
			return err
		}

		err = m.CleanOne(ctx, accessor.GetName(), skip)
		var remainingErr *RemainingCRDError
		switch {
		case errors.As(err, &remainingErr):
			remainingCRDs = append(remainingCRDs, accessor.GetName())
		case err != nil:
			errs = append(errs, err)
		}
	}

	if len(errs) > 0 {
		return utilerrors.NewAggregate(errs)
	}
	if len(remainingCRDs) > 0 {
		return &RemainingCRDError{RemainingCRDs: remainingCRDs}
	}

	return nil
}

func (m *Manager[T]) Apply(ctx context.Context, manifests resourceapply.AssetFunc, files ...string) error {
	var errs []error

	for _, file := range files {
		objBytes, err := manifests(file)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		requiredObj, _, err := genericCodec.Decode(objBytes, nil, nil)
		if err != nil {
			errs = append(errs, err)
			continue
		}

		err = m.applyOne(ctx, requiredObj.(T))
		if err != nil {
			errs = append(errs, err)
		}
	}

	return utilerrors.NewAggregate(errs)
}

func (m *Manager[T]) applyOne(ctx context.Context, required T) error {
	accessor, err := meta.Accessor(required)
	if err != nil {
		return err
	}
	existing, err := m.client.Get(ctx, accessor.GetName(), metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		_, err := m.client.Create(ctx, required, metav1.CreateOptions{})
		klog.Infof("crd %s is created", accessor.GetName())
		return err
	}
	if err != nil {
		return err
	}

	ok, err := m.shouldUpdate(existing, required)
	if err != nil {
		return err
	}

	if !ok {
		return nil
	}

	existingAccessor, err := meta.Accessor(existing)
	if err != nil {
		return err
	}

	annotations := accessor.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[versionAnnotationKey] = m.version.String()
	accessor.SetAnnotations(annotations)
	accessor.SetResourceVersion(existingAccessor.GetResourceVersion())

	_, err = m.client.Update(ctx, required, metav1.UpdateOptions{})
	if err != nil {
		return err
	}

	klog.Infof("crd %s is updated to version %s", accessor.GetName(), m.version.String())

	return nil
}

func (m *Manager[T]) shouldUpdate(old, new T) (bool, error) {
	// if existingVersion is higher than the required version, do not update crd.
	accessor, err := meta.Accessor(old)
	if err != nil {
		return false, err
	}

	var existingVersion string
	if accessor.GetAnnotations() != nil {
		existingVersion = accessor.GetAnnotations()[versionAnnotationKey]
	}

	// alwasy update if existing doest not have version annotation
	if len(existingVersion) == 0 {
		return true, nil
	}

	cnt, err := m.version.Compare(existingVersion)
	if err != nil {
		return false, err
	}

	// if the version are the same, compare the spec
	if cnt == 0 {
		return !m.equal(old, new), nil
	}

	// do not update when version is higher
	return cnt > 0, nil
}

func EqualV1(old, new *apiextensionsv1.CustomResourceDefinition) bool {
	modified := pointer.Bool(false)

	resourcemerge.EnsureCustomResourceDefinitionV1(modified, old, *new)
	return !*modified
}

func EqualV1Beta1(old, new *apiextensionsv1beta1.CustomResourceDefinition) bool {
	modified := pointer.Bool(false)

	resourcemerge.EnsureCustomResourceDefinitionV1Beta1(modified, old, *new)
	return !*modified
}
