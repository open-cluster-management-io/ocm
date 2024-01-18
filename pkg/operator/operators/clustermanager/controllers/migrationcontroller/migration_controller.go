package migrationcontroller

import (
	"context"
	"fmt"
	"time"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehelper"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	migrationv1alpha1 "sigs.k8s.io/kube-storage-version-migrator/pkg/apis/migration/v1alpha1"
	migrationv1alpha1client "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/typed/migration/v1alpha1"

	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/patcher"

	"open-cluster-management.io/ocm/manifests"
	"open-cluster-management.io/ocm/pkg/common/queue"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
)

var (
	genericScheme = runtime.NewScheme()
	genericCodecs = serializer.NewCodecFactory(genericScheme)
	genericCodec  = genericCodecs.UniversalDeserializer()

	_ = migrationv1alpha1.AddToScheme(genericScheme)

	// Fill in this slice with StorageVersionMigration CRs.
	// For example:
	// migrationRequestFiles = []string{
	//		"cluster-manager/cluster-manager-managedclustersets-migration.yaml",
	// }
	migrationRequestFiles = []string{}
)

const (
	clusterManagerApplied = "Applied"
	MigrationSucceeded    = "MigrationSucceeded"

	migrationRequestCRDName = "storageversionmigrations.migration.k8s.io"
	reSyncTime              = time.Second * 5
)

type crdMigrationController struct {
	kubeconfig                *rest.Config
	kubeClient                kubernetes.Interface
	patcher                   patcher.Patcher[*operatorapiv1.ClusterManager, operatorapiv1.ClusterManagerSpec, operatorapiv1.ClusterManagerStatus]
	clusterManagerLister      operatorlister.ClusterManagerLister
	recorder                  events.Recorder
	generateHubClusterClients func(hubConfig *rest.Config) (apiextensionsclient.Interface, migrationv1alpha1client.StorageVersionMigrationsGetter, error)
	parseMigrations           func() ([]*migrationv1alpha1.StorageVersionMigration, error)
}

// NewCRDMigrationController construct crd migration controller
func NewCRDMigrationController(
	kubeconfig *rest.Config,
	kubeClient kubernetes.Interface,
	clusterManagerClient operatorv1client.ClusterManagerInterface,
	clusterManagerInformer operatorinformer.ClusterManagerInformer,
	recorder events.Recorder) factory.Controller {
	controller := &crdMigrationController{
		kubeconfig: kubeconfig,
		kubeClient: kubeClient,
		patcher: patcher.NewPatcher[
			*operatorapiv1.ClusterManager, operatorapiv1.ClusterManagerSpec, operatorapiv1.ClusterManagerStatus](
			clusterManagerClient),
		clusterManagerLister:      clusterManagerInformer.Lister(),
		parseMigrations:           parseStorageVersionMigrationFiles,
		recorder:                  recorder,
		generateHubClusterClients: generateHubClients,
	}

	return factory.New().WithSync(controller.sync).
		WithInformersQueueKeysFunc(queue.QueueKeyByMetaName, clusterManagerInformer.Informer()).
		ToController("CRDMigrationController", recorder)
}

func (c *crdMigrationController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	clusterManagerName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ClusterManager %q", clusterManagerName)

	// if no migration files exist, do nothing and exit the reconcile
	migrations, err := c.parseMigrations()
	if err != nil {
		return err
	}
	if len(migrations) == 0 {
		return nil
	}

	clusterManager, err := c.clusterManagerLister.Get(clusterManagerName)
	if errors.IsNotFound(err) {
		// ClusterManager not found, could have been deleted, do nothing.
		return nil
	}
	if err != nil {
		return err
	}

	// If mode is default, then config is management kubeconfig, else it would use management kubeconfig to find the hub
	hubKubeconfig, err := helpers.GetHubKubeconfig(ctx, c.kubeconfig, c.kubeClient, clusterManager.Name, clusterManager.Spec.DeployOption.Mode)
	if err != nil {
		return err
	}
	apiExtensionClient, migrationClient, err := c.generateHubClusterClients(hubKubeconfig)
	if err != nil {
		return err
	}

	// find whether the storageversionmigration CRD is supported
	supported, err := supportStorageVersionMigration(ctx, apiExtensionClient)
	if err != nil {
		return err
	}
	if !supported {
		newClusterManager := clusterManager.DeepCopy()
		meta.SetStatusCondition(&newClusterManager.Status.Conditions, metav1.Condition{
			Type:    MigrationSucceeded,
			Status:  metav1.ConditionFalse,
			Reason:  "StorageVersionMigrationFailed",
			Message: "Do not support StorageVersionMigration",
		})
		_, err = c.patcher.PatchStatus(ctx, newClusterManager, newClusterManager.Status, clusterManager.Status)
		return err
	}

	// do not apply storage version migrations until other resources are applied
	if applied := meta.IsStatusConditionTrue(clusterManager.Status.Conditions, clusterManagerApplied); !applied {
		controllerContext.Queue().AddRateLimited(clusterManagerName)
		return nil
	}

	var migrationCond metav1.Condition
	// Update the status of the ClusterManager to indicate that the migration is in progress.
	defer func() {
		newClusterManager := clusterManager.DeepCopy()
		meta.SetStatusCondition(&newClusterManager.Status.Conditions, migrationCond)

		_, err = c.patcher.PatchStatus(ctx, newClusterManager, newClusterManager.Status, clusterManager.Status)
		if err != nil {
			klog.Errorf("Failed to update ClusterManager status. %v", err)
			controllerContext.Queue().AddRateLimited(clusterManagerName)
			return
		}

		//If migration not succeed, wait for all StorageVersionMigrations succeed.
		if migrationCond.Status != metav1.ConditionTrue {
			klog.V(4).Infof("Wait all StorageVersionMigrations succeed. migrationCond: %v. error: %v", migrationCond, err)
			controllerContext.Queue().AddRateLimited(clusterManagerName)
		}
	}()

	err = checkCRDStorageVersion(ctx, migrations, apiExtensionClient)
	if err != nil {
		klog.Errorf("Failed to check CRD current storage version. %v", err)
		controllerContext.Queue().AddRateLimited(clusterManagerName)
		c.recorder.Warningf("StorageVersionMigrationFailed", "Failed to check CRD current storage version. %v", err)

		migrationCond = metav1.Condition{
			Type:    MigrationSucceeded,
			Status:  metav1.ConditionFalse,
			Reason:  "StorageVersionMigrationFailed",
			Message: fmt.Sprintf("Failed to check CRD current storage version. %v", err),
		}
		return nil
	}

	err = createStorageVersionMigrations(ctx, migrations, newClusterManagerOwner(clusterManager), migrationClient, c.recorder)
	if err != nil {
		klog.Errorf("Failed to apply StorageVersionMigrations. %v", err)

		migrationCond = metav1.Condition{
			Type:    MigrationSucceeded,
			Status:  metav1.ConditionFalse,
			Reason:  "StorageVersionMigrationFailed",
			Message: fmt.Sprintf("Failed to create StorageVersionMigrations. %v", err),
		}
		return err
	}

	migrationCond, err = syncStorageVersionMigrationsCondition(ctx, migrations, migrationClient)
	if err != nil {
		klog.Errorf("Failed to sync StorageVersionMigrations condition. %v", err)
		return err
	}

	return nil
}

// supportStorageVersionMigration returns ture if StorageVersionMigration CRD exists; otherwise returns false.
func supportStorageVersionMigration(ctx context.Context, apiExtensionClient apiextensionsclient.Interface) (bool, error) {
	_, err := apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx, migrationRequestCRDName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// 1.The CRD must exists before the migration CR is created.
// 2.The CRD must have at least 2 version.
// 3.The version set in the migration CR must exist in the CRD.
// 4.The currrent storage vesion in CRD should not be the version in the migration CR, otherwise during the migration, the
// objects will still be stored in as this version.[This one requires creating migration CR with the version you want to migrate from]
func checkCRDStorageVersion(ctx context.Context, toCreateMigrations []*migrationv1alpha1.StorageVersionMigration,
	apiExtensionClient apiextensionsclient.Interface) error {
	errs := []error{}
	for _, migration := range toCreateMigrations {
		// The CRD must exist
		crd, err := apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions().Get(ctx,
			resourceToCRDName(migration.Spec.Resource.Resource, migration.Spec.Resource.Group), metav1.GetOptions{})
		if err != nil {
			errs = append(errs, err)
			continue
		}

		// The CRD must have at least 2 versions
		if len(crd.Spec.Versions) < 2 {
			errs = append(errs, fmt.Errorf("the CRD %v must have at least 2 versions", crd.Name))
			continue
		}

		// The version set in the migration CR must exist in the CRD
		var found bool
		for _, version := range crd.Spec.Versions {
			if version.Name == migration.Spec.Resource.Version {
				found = true
				break
			}
		}
		if !found {
			errs = append(errs, fmt.Errorf("the version %v in the migration CR %v does not exist in the CRD %v",
				migration.Spec.Resource.Version, migration.Name, crd.Name))
			continue
		}

		// The currrent storage vesion in CRD should not be the version in the migration CR
		var storageVersion string
		for _, version := range crd.Spec.Versions {
			if version.Name == migration.Spec.Resource.Version && version.Storage {
				storageVersion = version.Name // find the current storage version of the CRD
				break
			}
		}
		if storageVersion == migration.Spec.Resource.Version {
			errs = append(errs, fmt.Errorf("the current storage version of %v is %v, which is the same as the version in the migration CR %v",
				resourceToCRDName(migration.Spec.Resource.Resource, migration.Spec.Resource.Group),
				storageVersion, migration.Name))
			continue
		}
	}
	return operatorhelpers.NewMultiLineAggregate(errs)
}

// StorageVersionMigration is a create-only, job-style CR, once it's done, updating the spec won't trigger a new migration.
// See code details in:
// https://github.com/kubernetes-sigs/kube-storage-version-migrator/blob/5c8923c5ff96ceb4435f66b986b5aec2dd0cbc22/pkg/controller/kubemigrator.go#L105-L108
func createStorageVersionMigrations(ctx context.Context,
	toCreateMigrations []*migrationv1alpha1.StorageVersionMigration,
	ownerRef metav1.OwnerReference,
	migrationClient migrationv1alpha1client.StorageVersionMigrationsGetter, recorder events.Recorder) error {
	errs := []error{}
	for _, migration := range toCreateMigrations {
		err := createStorageVersionMigration(ctx, migrationClient, migration, ownerRef, recorder)
		if err != nil {
			errs = append(errs, err)
			continue
		}
	}
	return operatorhelpers.NewMultiLineAggregate(errs)
}

func resourceToCRDName(resource, group string) string {
	return fmt.Sprintf("%s.%s", resource, group)
}

// syncStorageVersionMigrationsCondition sync the migration condition based on all the StorageVersionMigrations status
// 1. migrationSucceeded is true only when all the StorageVersionMigrations resources succeed.
// 2. migrationSucceeded is false when any of the StorageVersionMigrations resources failed or running
func syncStorageVersionMigrationsCondition(ctx context.Context, toSyncMigrations []*migrationv1alpha1.StorageVersionMigration,
	migrationClient migrationv1alpha1client.StorageVersionMigrationsGetter) (metav1.Condition, error) {
	for _, migration := range toSyncMigrations {
		existing, err := migrationClient.StorageVersionMigrations().Get(ctx, migration.Name, metav1.GetOptions{})
		if err != nil {
			return metav1.Condition{}, err
		}
		migrationStatusCondition := getStorageVersionMigrationStatusCondition(existing)
		if migrationStatusCondition == nil {
			return metav1.Condition{
				Type:    MigrationSucceeded,
				Status:  metav1.ConditionFalse,
				Reason:  "StorageVersionMigrationProcessing",
				Message: fmt.Sprintf("Wait StorageVersionMigration %v succeed.", existing.Name),
			}, nil
		}
		switch migrationStatusCondition.Type {
		case migrationv1alpha1.MigrationSucceeded:
			continue
		case migrationv1alpha1.MigrationFailed:
			return metav1.Condition{
				Type:    MigrationSucceeded,
				Status:  metav1.ConditionFalse,
				Reason:  fmt.Sprintf("StorageVersionMigration Failed. %v", migrationStatusCondition.Reason),
				Message: fmt.Sprintf("Failed to wait StorageVersionMigration %v succeed. %v", existing.Name, migrationStatusCondition.Message),
			}, nil
		case migrationv1alpha1.MigrationRunning:
			return metav1.Condition{
				Type:    MigrationSucceeded,
				Status:  metav1.ConditionFalse,
				Reason:  fmt.Sprintf("StorageVersionMigration Running. %v", migrationStatusCondition.Reason),
				Message: fmt.Sprintf("Wait StorageVersionMigration %v succeed. %v", existing.Name, migrationStatusCondition.Message),
			}, nil
		}
	}
	return metav1.Condition{
		Type:    MigrationSucceeded,
		Status:  metav1.ConditionTrue,
		Reason:  "StorageVersionMigrationSucceed",
		Message: "All StorageVersionMigrations Succeed",
	}, nil
}

func parseStorageVersionMigrationFiles() ([]*migrationv1alpha1.StorageVersionMigration, error) {
	var errs []error
	var migrations []*migrationv1alpha1.StorageVersionMigration
	for _, file := range migrationRequestFiles {
		migration, err := parseStorageVersionMigrationFile(
			func(name string) ([]byte, error) {
				template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
				if err != nil {
					return nil, err
				}
				return assets.MustCreateAssetFromTemplate(name, template, struct{}{}).Data, nil
			},
			file)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		migrations = append(migrations, migration)
	}
	if len(errs) > 0 {
		return nil, operatorhelpers.NewMultiLineAggregate(errs)
	}
	return migrations, nil
}

func parseStorageVersionMigrationFile(
	manifests resourceapply.AssetFunc,
	file string,
) (*migrationv1alpha1.StorageVersionMigration, error) {
	objBytes, err := manifests(file)
	if err != nil {
		return nil, fmt.Errorf("missing %q: %v", file, err)
	}
	svmObj, _, err := genericCodec.Decode(objBytes, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("cannot decode %q: %v", file, err)
	}

	svm, ok := svmObj.(*migrationv1alpha1.StorageVersionMigration)
	if !ok {
		return nil, fmt.Errorf("invalid StorageVersionMigration in file %q: %v", file, svmObj)
	}

	return svm, nil
}

func createStorageVersionMigration(
	ctx context.Context,
	client migrationv1alpha1client.StorageVersionMigrationsGetter,
	migration *migrationv1alpha1.StorageVersionMigration,
	ownerRefs metav1.OwnerReference,
	recorder events.Recorder,
) error {
	if migration == nil {
		return fmt.Errorf("required StorageVersionMigration is nil")
	}
	existing, err := client.StorageVersionMigrations().Get(ctx, migration.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		migration.ObjectMeta.OwnerReferences = append(migration.ObjectMeta.OwnerReferences, ownerRefs)
		actual, err := client.StorageVersionMigrations().Create(context.TODO(), migration, metav1.CreateOptions{})
		if err != nil {
			recorder.Warningf("StorageVersionMigrationCreateFailed", "Failed to create %s: %v", resourcehelper.FormatResourceForCLIWithNamespace(migration), err)
			return err
		}

		recorder.Eventf("StorageVersionMigrationCreated", "Created %s because it was missing", resourcehelper.FormatResourceForCLIWithNamespace(actual))
		return err
	}
	if err != nil {
		return err
	}

	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()
	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, migration.ObjectMeta)
	if !equality.Semantic.DeepEqual(existingCopy.Spec, migration.Spec) {
		*modified = true
	}
	if !*modified {
		return nil // nothing change in the spec
	}

	return fmt.Errorf("StorageVersionMigrationConflict: Trying to set %s with different spec", migration.Name)
}

func getStorageVersionMigrationStatusCondition(svmcr *migrationv1alpha1.StorageVersionMigration) *migrationv1alpha1.MigrationCondition {
	for _, c := range svmcr.Status.Conditions {
		switch c.Type {
		case migrationv1alpha1.MigrationSucceeded:
			if c.Status == corev1.ConditionTrue {
				return &c
			}
			continue
		case migrationv1alpha1.MigrationFailed:
			if c.Status == corev1.ConditionTrue {
				return &c
			}
			continue
		case migrationv1alpha1.MigrationRunning:
			if c.Status == corev1.ConditionTrue {
				return &c
			}
			continue
		}
	}
	return nil
}

func generateHubClients(hubKubeConfig *rest.Config) (apiextensionsclient.Interface, migrationv1alpha1client.StorageVersionMigrationsGetter, error) {
	hubApiExtensionClient, err := apiextensionsclient.NewForConfig(hubKubeConfig)
	if err != nil {
		return nil, nil, err
	}

	hubMigrationClient, err := migrationv1alpha1client.NewForConfig(hubKubeConfig)
	if err != nil {
		return nil, nil, err
	}
	return hubApiExtensionClient, hubMigrationClient, nil
}

func newClusterManagerOwner(clusterManager *operatorapiv1.ClusterManager) metav1.OwnerReference {
	return metav1.OwnerReference{
		APIVersion: operatorapiv1.GroupVersion.WithKind("ClusterManager").GroupVersion().String(),
		Kind:       operatorapiv1.GroupVersion.WithKind("ClusterManager").Kind,
		Name:       clusterManager.Name,
		UID:        clusterManager.UID,
	}
}
