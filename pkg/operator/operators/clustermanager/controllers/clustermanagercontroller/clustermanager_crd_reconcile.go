/*
 * Copyright 2022 Contributors to the Open Cluster Management project
 */

package clustermanagercontroller

import (
	"context"
	"fmt"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	migrationclient "sigs.k8s.io/kube-storage-version-migrator/pkg/clients/clientset/typed/migration/v1alpha1"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	"open-cluster-management.io/ocm/manifests"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/pkg/operator/operators/crdmanager"
)

var (
	// crdNames is the list of CRDs to be wiped out before deleting other resources when clusterManager is deleted.
	// The order of the list matters, the managedclusteraddon crd needs to be deleted at first so all addon related
	// manifestwork is deleted, then other manifestworks.
	crdNames = []string{
		"managedclusteraddons.addon.open-cluster-management.io",
		"manifestworks.work.open-cluster-management.io",
		"managedclusters.cluster.open-cluster-management.io",
		"manifestworkreplicasets.work.open-cluster-management.io",
	}

	// crdResourceFiles should be deployed in the hub cluster
	hubCRDResourceFiles = []string{
		"cluster-manager/hub/crds/0000_00_addon.open-cluster-management.io_clustermanagementaddons.crd.yaml",
		"cluster-manager/hub/crds/0000_00_clusters.open-cluster-management.io_managedclusters.crd.yaml",
		"cluster-manager/hub/crds/0000_00_clusters.open-cluster-management.io_managedclustersets.crd.yaml",
		"cluster-manager/hub/crds/0000_00_work.open-cluster-management.io_manifestworks.crd.yaml",
		"cluster-manager/hub/crds/0000_00_work.open-cluster-management.io_manifestworkreplicasets.crd.yaml",
		"cluster-manager/hub/crds/0000_01_addon.open-cluster-management.io_managedclusteraddons.crd.yaml",
		"cluster-manager/hub/crds/0000_01_clusters.open-cluster-management.io_managedclustersetbindings.crd.yaml",
		"cluster-manager/hub/crds/0000_02_clusters.open-cluster-management.io_placements.crd.yaml",
		"cluster-manager/hub/crds/0000_02_addon.open-cluster-management.io_addondeploymentconfigs.crd.yaml",
		"cluster-manager/hub/crds/0000_03_addon.open-cluster-management.io_addontemplates.crd.yaml",
		"cluster-manager/hub/crds/0000_03_clusters.open-cluster-management.io_placementdecisions.crd.yaml",
		"cluster-manager/hub/crds/0000_05_clusters.open-cluster-management.io_addonplacementscores.crd.yaml",
	}

	hubClusterProfileCRDResourceFiles = []string{
		// clusterprofile crd
		"cluster-manager/hub/crds/0000_00_multicluster.x-k8s.io_clusterprofiles.crd.yaml",
	}
)

type crdReconcile struct {
	hubAPIExtensionClient apiextensionsclient.Interface
	hubMigrationClient    migrationclient.StorageVersionMigrationsGetter
	skipRemoveCRDs        bool

	cache    resourceapply.ResourceCache
	recorder events.Recorder
}

func (c *crdReconcile) reconcile(ctx context.Context, cm *operatorapiv1.ClusterManager,
	config manifests.HubConfig) (*operatorapiv1.ClusterManager, reconcileState, error) {
	crdManager := crdmanager.NewManager[*apiextensionsv1.CustomResourceDefinition](
		c.hubAPIExtensionClient.ApiextensionsV1().CustomResourceDefinitions(),
		crdmanager.EqualV1,
	)

	// CRD resource files to deploy
	hubDeployCRDResources := hubCRDResourceFiles

	// If featuregate ClusterProfile is enabled, include the ClusterProfile CRD.
	// The ClusterProfile CRD will not be removed when featuregate is disabled (same behavior as other crds)
	// and will not be cleaned when ClusterManager is deleting since the CRD might be used by other projects.
	if config.ClusterProfileEnabled {
		hubDeployCRDResources = append(hubDeployCRDResources, hubClusterProfileCRDResourceFiles...)
	}

	if err := crdManager.Apply(ctx,
		func(name string) ([]byte, error) {
			template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, config).Data

			objData, err = helpers.AddLabelsToYaml(objData, cm.Labels)
			if err != nil {
				return nil, fmt.Errorf("failed to add labels to template %s: %w", name, err)
			}

			helpers.SetRelatedResourcesStatusesWithObj(ctx, &cm.Status.RelatedResources, objData)
			return objData, nil
		},
		hubDeployCRDResources...); err != nil {
		meta.SetStatusCondition(&cm.Status.Conditions, metav1.Condition{
			Type:    operatorapiv1.ConditionClusterManagerApplied,
			Status:  metav1.ConditionFalse,
			Reason:  operatorapiv1.ReasonClusterManagerCRDApplyFailed,
			Message: fmt.Sprintf("Failed to apply crd: %v", err),
		})
		return cm, reconcileStop, err
	}

	return cm, reconcileContinue, nil
}

func (c *crdReconcile) clean(ctx context.Context, cm *operatorapiv1.ClusterManager,
	config manifests.HubConfig) (*operatorapiv1.ClusterManager, reconcileState, error) {
	crdManager := crdmanager.NewManager[*apiextensionsv1.CustomResourceDefinition](
		c.hubAPIExtensionClient.ApiextensionsV1().CustomResourceDefinitions(),
		crdmanager.EqualV1,
	)

	// Remove crds in order at first
	for _, name := range crdNames {
		if err := crdManager.CleanOne(ctx, name, c.skipRemoveCRDs); err != nil {
			return cm, reconcileStop, err
		}
		c.recorder.Eventf(ctx, "CRDDeleted", "crd %s is deleted", name)
	}
	if c.skipRemoveCRDs {
		return cm, reconcileContinue, nil
	}

	// Only the crds list in the hubCRDResourceFiles will be cleaned.
	// Never clean the ClusterProfile CRD since it might be used by other projects.
	if err := crdManager.Clean(ctx, c.skipRemoveCRDs,
		func(name string) ([]byte, error) {
			template, err := manifests.ClusterManagerManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
			helpers.SetRelatedResourcesStatusesWithObj(ctx, &cm.Status.RelatedResources, objData)
			return objData, nil
		},
		hubCRDResourceFiles...); err != nil {
		return cm, reconcileStop, err
	}

	return cm, reconcileContinue, nil
}
