package crds

import (
	"context"
	"embed"
	"fmt"
	"sync"
	"time"

	crdhelpers "k8s.io/apiextensions-apiserver/pkg/apihelpers"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	extensionsapiserver "k8s.io/apiextensions-apiserver/pkg/apiserver"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apiextensionsv1client "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset/typed/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	utilnet "k8s.io/apimachinery/pkg/util/net"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
)

//go:embed *.yaml
var raw embed.FS

var baseCRD = []string{
	// managed cluster
	"0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml",
	// managed clusterset & managed clusterset binding
	"0000_00_clusters.open-cluster-management.io_managedclustersets.crd.yaml",
	"0000_01_clusters.open-cluster-management.io_managedclustersetbindings.crd.yaml",
	// manifest work
	"0000_00_work.open-cluster-management.io_manifestworks.crd.yaml",
	// addon
	"0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml",
	"0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml",
	"0000_02_addon.open-cluster-management.io_addondeploymentconfigs.crd.yaml",
	// placement & placementdecision
	"0000_02_clusters.open-cluster-management.io_placements.crd.yaml",
	"0000_03_clusters.open-cluster-management.io_placementdecisions.crd.yaml",
	"0000_05_clusters.open-cluster-management.io_addonplacementscores.crd.yaml",
}

func Bootstrap(ctx context.Context, crdClient apiextensionsclient.Interface, discoveryClient discovery.DiscoveryInterface, dynamicClient dynamic.Interface) error {
	// poll here, call create to create base crds
	if err := wait.PollImmediateInfiniteWithContext(ctx, time.Second, func(ctx context.Context) (bool, error) {
		if err := Create(ctx, crdClient.ApiextensionsV1().CustomResourceDefinitions(), baseCRD); err != nil {
			klog.Errorf("failed to bootstrap system CRDs: %v", err)
			return false, nil // keep retrying
		}
		return true, nil
	}); err != nil {
		return fmt.Errorf("failed to bootstrap system CRDs: %w", err)
	}

	return nil
}

// Create creates the given CRDs using the target client and waits
// for all of them to become established in parallel. This call is blocking.
func Create(ctx context.Context, client apiextensionsv1client.CustomResourceDefinitionInterface, crds []string) error {
	return CreateFromFile(ctx, client, crds, raw)
}

// CreateFromFile call createOneFromFile for each crd in filenames in parallel
func CreateFromFile(ctx context.Context, client apiextensionsv1client.CustomResourceDefinitionInterface, filenames []string, fs embed.FS) error {
	wg := sync.WaitGroup{}
	bootstrapErrChan := make(chan error, len(filenames))
	for _, fname := range filenames {
		wg.Add(1)
		go func(fn string) {
			defer wg.Done()
			err := retryError(func() error {
				return createOneFromFile(ctx, client, fn, fs)
			})

			if ctx.Err() != nil {
				err = ctx.Err()
			}
			bootstrapErrChan <- err
		}(fname)
	}
	wg.Wait()
	close(bootstrapErrChan)
	var bootstrapErrors []error
	for err := range bootstrapErrChan {
		bootstrapErrors = append(bootstrapErrors, err)
	}
	if err := utilerrors.NewAggregate(bootstrapErrors); err != nil {
		return fmt.Errorf("could not bootstrap CRDs: %w", err)
	}
	return nil
}

func createOneFromFile(ctx context.Context, client apiextensionsv1client.CustomResourceDefinitionInterface, filename string, fs embed.FS) error {
	start := time.Now()
	klog.V(4).Infof("Bootstrapping %v", filename)
	raw, err := fs.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("could not read CRD %s: %w", filename, err)
	}
	// set expected crd GVK
	expectedGvk := &schema.GroupVersionKind{Group: apiextensionsv1.GroupName, Version: "v1", Kind: "CustomResourceDefinition"}
	// read obj and gvk from file
	obj, gvk, err := extensionsapiserver.Codecs.UniversalDeserializer().Decode(raw, expectedGvk, &apiextensionsv1.CustomResourceDefinition{})
	if err != nil {
		return fmt.Errorf("could not decode raw CRD %s: %w", filename, err)
	}
	if !equality.Semantic.DeepEqual(gvk, expectedGvk) {
		return fmt.Errorf("decoded CRD %s into incorrect GroupVersionKind, got %#v, wanted %#v", filename, gvk, expectedGvk)
	}
	// transform obj into type crd
	rawCrd, ok := obj.(*apiextensionsv1.CustomResourceDefinition)
	if !ok {
		return fmt.Errorf("decoded CRD %s into incorrect type, got %T, wanted %T", filename, rawCrd, &apiextensionsv1.CustomResourceDefinition{})
	}

	updateNeeded := false
	crd, err := client.Get(ctx, rawCrd.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			// if crd not found, create it
			crd, err = client.Create(ctx, rawCrd, metav1.CreateOptions{})
			if err != nil {
				// from KCP
				//
				// If multiple post-start hooks specify the same CRD, they could race with each other, so we need to
				// handle the scenario where another hook created this CRD after our Get() call returned not found.
				if apierrors.IsAlreadyExists(err) {
					// Re-get so we have the correct resourceVersion
					crd, err = client.Get(ctx, rawCrd.Name, metav1.GetOptions{})
					if err != nil {
						return fmt.Errorf("error getting CRD %s: %w", filename, err)
					}
					updateNeeded = true
				} else {
					return fmt.Errorf("error creating CRD %s: %w", filename, err)
				}
			} else {
				klog.Infof("Bootstrapped CRD %v after %s", filename, time.Since(start).String())
			}
		} else {
			return fmt.Errorf("error fetching CRD %s: %w", filename, err)
		}
	} else {
		updateNeeded = true
	}

	// if the version of already existing crd is not equal to the applying, update it
	if updateNeeded {
		rawCrd.ResourceVersion = crd.ResourceVersion
		_, err := client.Update(ctx, rawCrd, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
		klog.Infof("Updated CRD %v after %s", filename, time.Since(start).String())
	}

	// poll until crd condition is true
	return wait.PollImmediateInfiniteWithContext(ctx, 100*time.Millisecond, func(ctx context.Context) (bool, error) {
		crd, err := client.Get(ctx, rawCrd.Name, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return false, fmt.Errorf("CRD %s was deleted before being established", filename)
			}
			return false, fmt.Errorf("error fetching CRD %s: %w", filename, err)
		}

		return crdhelpers.IsCRDConditionTrue(crd, apiextensionsv1.Established), nil
	})
}

func retryError(f func() error) error {
	return retry.OnError(retry.DefaultBackoff, func(err error) bool {
		return utilnet.IsConnectionRefused(err) || apierrors.IsTooManyRequests(err) || apierrors.IsConflict(err)
	}, f)
}
