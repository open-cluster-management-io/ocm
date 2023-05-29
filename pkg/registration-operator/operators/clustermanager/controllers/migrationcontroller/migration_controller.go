package migrationcontroller

import (
	"context"
	"fmt"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	"github.com/openshift/library-go/pkg/operator/resource/resourcehelper"
	"github.com/openshift/library-go/pkg/operator/resource/resourcemerge"
	operatorhelpers "github.com/openshift/library-go/pkg/operator/v1helpers"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	operatorv1client "open-cluster-management.io/api/client/operator/clientset/versioned/typed/operator/v1"
	operatorinformer "open-cluster-management.io/api/client/operator/informers/externalversions/operator/v1"
	operatorlister "open-cluster-management.io/api/client/operator/listers/operator/v1"
	"open-cluster-management.io/ocm/manifests"
	"open-cluster-management.io/ocm/pkg/registration-operator/helpers"
	migrationv1alpha1 "sigs.k8s.io/kube-storage-version-migrator/pkg/apis/migration/v1alpha1"
	migrationv1alpha1client "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/typed/migration/v1alpha1"
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
	migrationRequestFiles = []string{
		"cluster-manager/cluster-manager-managedclustersets-migration.yaml",
		"cluster-manager/cluster-manager-managedclustersetbindings-migration.yaml",
	}
)

const (
	clusterManagerApplied = "Applied"
	MigrationSucceeded    = "MigrationSucceeded"

	migrationRequestCRDName = "storageversionmigrations.migration.k8s.io"
)

type crdMigrationController struct {
	kubeconfig                *rest.Config
	kubeClient                kubernetes.Interface
	clusterManagerClient      operatorv1client.ClusterManagerInterface
	clusterManagerLister      operatorlister.ClusterManagerLister
	recorder                  events.Recorder
	generateHubClusterClients func(hubConfig *rest.Config) (apiextensionsclient.Interface, migrationv1alpha1client.StorageVersionMigrationsGetter, error)
}

// NewClusterManagerController construct cluster manager hub controller
func NewCRDMigrationController(
	kubeconfig *rest.Config,
	kubeClient kubernetes.Interface,
	clusterManagerClient operatorv1client.ClusterManagerInterface,
	clusterManagerInformer operatorinformer.ClusterManagerInformer,
	recorder events.Recorder) factory.Controller {
	controller := &crdMigrationController{
		kubeconfig:                kubeconfig,
		kubeClient:                kubeClient,
		clusterManagerClient:      clusterManagerClient,
		clusterManagerLister:      clusterManagerInformer.Lister(),
		recorder:                  recorder,
		generateHubClusterClients: generateHubClients,
	}

	return factory.New().WithSync(controller.sync).
		WithInformersQueueKeyFunc(func(obj runtime.Object) string {
			accessor, _ := meta.Accessor(obj)
			return accessor.GetName()
		}, clusterManagerInformer.Informer()).
		ToController("CRDMigrationController", recorder)
}

func (c *crdMigrationController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	clusterManagerName := controllerContext.QueueKey()
	klog.V(4).Infof("Reconciling ClusterManager %q", clusterManagerName)

	if len(migrationRequestFiles) == 0 {
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

	// ClusterManager is deleting, we remove its related resources on hub
	if !clusterManager.DeletionTimestamp.IsZero() {
		return removeStorageVersionMigrations(ctx, migrationClient)
	}

	// apply storage version migrations if it is supported
	supported, err := supportStorageVersionMigration(ctx, apiExtensionClient)
	if err != nil {
		return err
	}

	if !supported {
		migrationCond := metav1.Condition{
			Type:    MigrationSucceeded,
			Status:  metav1.ConditionFalse,
			Reason:  "StorageVersionMigrationFailed",
			Message: fmt.Sprintf("Do not support StorageVersionMigration"),
		}
		_, _, err = helpers.UpdateClusterManagerStatus(ctx, c.clusterManagerClient, clusterManagerName,
			helpers.UpdateClusterManagerConditionFn(migrationCond),
		)
		return err
	}

	// do not apply storage version migrations until other resources are applied
	if applied := meta.IsStatusConditionTrue(clusterManager.Status.Conditions, clusterManagerApplied); !applied {
		controllerContext.Queue().AddRateLimited(clusterManagerName)
		return nil
	}

	err = applyStorageVersionMigrations(ctx, migrationClient, c.recorder)
	if err != nil {
		klog.Errorf("Failed to apply StorageVersionMigrations. %v", err)
		return err
	}

	migrationCond, err := syncStorageVersionMigrationsCondition(ctx, migrationClient)
	if err != nil {
		klog.Errorf("Failed to sync StorageVersionMigrations condition. %v", err)
		return err
	}

	_, _, err = helpers.UpdateClusterManagerStatus(ctx, c.clusterManagerClient, clusterManagerName,
		helpers.UpdateClusterManagerConditionFn(migrationCond),
	)
	if err != nil {
		return err
	}

	//If migration not succeed, wait for all StorageVersionMigrations succeed.
	if migrationCond.Status != metav1.ConditionTrue {
		klog.V(4).Infof("Wait all StorageVersionMigrations succeed. migrationCond: %v. error: %v", migrationCond, err)
		controllerContext.Queue().AddRateLimited(clusterManagerName)
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

func removeStorageVersionMigrations(
	ctx context.Context,
	migrationClient migrationv1alpha1client.StorageVersionMigrationsGetter) error {
	// Reomve storage version migrations
	for _, file := range migrationRequestFiles {
		err := removeStorageVersionMigration(
			ctx,
			migrationClient,
			func(name string) ([]byte, error) {
				template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
				if err != nil {
					return nil, err
				}
				return assets.MustCreateAssetFromTemplate(name, template, struct{}{}).Data, nil
			},
			file,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func applyStorageVersionMigrations(ctx context.Context, migrationClient migrationv1alpha1client.StorageVersionMigrationsGetter, recorder events.Recorder) error {
	errs := []error{}
	for _, file := range migrationRequestFiles {
		required, err := parseStorageVersionMigrationFile(
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

		_, _, err = applyStorageVersionMigration(migrationClient, required, recorder)
		if err != nil {
			errs = append(errs, err)
			continue
		}
	}

	return operatorhelpers.NewMultiLineAggregate(errs)
}

// syncStorageVersionMigrationsCondition sync the migration condition based on all the StorageVersionMigrations status
// 1. migrationSucceeded is true only when all the StorageVersionMigrations resources succeed.
// 2. migrationSucceeded is false when any of the StorageVersionMigrations resources failed or running
func syncStorageVersionMigrationsCondition(ctx context.Context, migrationClient migrationv1alpha1client.StorageVersionMigrationsGetter) (metav1.Condition, error) {
	for _, file := range migrationRequestFiles {
		required, err := parseStorageVersionMigrationFile(
			func(name string) ([]byte, error) {
				template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
				if err != nil {
					return nil, err
				}
				return assets.MustCreateAssetFromTemplate(name, template, struct{}{}).Data, nil
			},
			file)
		if err != nil {
			return metav1.Condition{}, err
		}
		existing, err := migrationClient.StorageVersionMigrations().Get(ctx, required.Name, metav1.GetOptions{})
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
		Message: fmt.Sprintf("All StorageVersionMigrations Succeed"),
	}, nil
}

func removeStorageVersionMigration(
	ctx context.Context,
	migrationClient migrationv1alpha1client.StorageVersionMigrationsGetter,
	manifests resourceapply.AssetFunc,
	file string) error {
	required, err := parseStorageVersionMigrationFile(manifests, file)
	if err != nil {
		return err
	}
	err = migrationClient.StorageVersionMigrations().Delete(ctx, required.Name, metav1.DeleteOptions{})
	if errors.IsNotFound(err) {
		return nil
	}
	return err
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

func applyStorageVersionMigration(
	client migrationv1alpha1client.StorageVersionMigrationsGetter,
	required *migrationv1alpha1.StorageVersionMigration,
	recorder events.Recorder,
) (*migrationv1alpha1.StorageVersionMigration, bool, error) {
	if required == nil {
		return nil, false, fmt.Errorf("required StorageVersionMigration is nil")
	}
	existing, err := client.StorageVersionMigrations().Get(context.TODO(), required.Name, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		actual, err := client.StorageVersionMigrations().Create(context.TODO(), required, metav1.CreateOptions{})
		if err != nil {
			recorder.Warningf("StorageVersionMigrationCreateFailed", "Failed to create %s: %v", resourcehelper.FormatResourceForCLIWithNamespace(required), err)
			return actual, true, err
		}

		recorder.Eventf("StorageVersionMigrationCreated", "Created %s because it was missing", resourcehelper.FormatResourceForCLIWithNamespace(actual))
		return actual, true, err
	}
	if err != nil {
		return nil, false, err
	}

	modified := resourcemerge.BoolPtr(false)
	existingCopy := existing.DeepCopy()
	resourcemerge.EnsureObjectMeta(modified, &existingCopy.ObjectMeta, required.ObjectMeta)
	if !equality.Semantic.DeepEqual(existingCopy.Spec, required.Spec) {
		*modified = true
		existing.Spec = required.Spec
	}
	if !*modified {
		return existing, false, nil
	}

	actual, err := client.StorageVersionMigrations().Update(context.TODO(), existingCopy, metav1.UpdateOptions{})
	if err != nil {
		recorder.Warningf("StorageVersionMigrationUpdateFailed", "Failed to update %s: %v", resourcehelper.FormatResourceForCLIWithNamespace(existingCopy), err)
		return actual, true, err
	}
	recorder.Eventf("StorageVersionMigrationUpdated", "Updated %s because it changed", resourcehelper.FormatResourceForCLIWithNamespace(actual))
	return actual, true, nil
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
