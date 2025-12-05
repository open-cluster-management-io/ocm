/*
 * Copyright 2022 Contributors to the Open Cluster Management project
 */

package klusterletcontroller

import (
	"context"

	"github.com/openshift/library-go/pkg/assets"
	"github.com/openshift/library-go/pkg/operator/resource/resourceapply"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	operatorapiv1 "open-cluster-management.io/api/operator/v1"
	"open-cluster-management.io/sdk-go/pkg/basecontroller/events"

	"open-cluster-management.io/ocm/manifests"
	"open-cluster-management.io/ocm/pkg/operator/helpers"
	"open-cluster-management.io/ocm/pkg/operator/operators/crdmanager"
)

var (
	crdV1StaticFiles = []string{
		"klusterlet/managed/0000_01_work.open-cluster-management.io_appliedmanifestworks.crd.yaml",
		"klusterlet/managed/0000_02_clusters.open-cluster-management.io_clusterclaims.crd.yaml",
	}

	aboutAPIFile = "klusterlet/managed/clusterproperties.crd.yaml"
)

// crdReconcile apply crds to managed clusters
type crdReconcile struct {
	managedClusterClients *managedClusterClients
	recorder              events.Recorder
	cache                 resourceapply.ResourceCache
}

func (r *crdReconcile) reconcile(ctx context.Context, klusterlet *operatorapiv1.Klusterlet,
	config klusterletConfig) (*operatorapiv1.Klusterlet, reconcileState, error) {
	crdManager := crdmanager.NewManager[*apiextensionsv1.CustomResourceDefinition](
		r.managedClusterClients.apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions(),
		crdmanager.EqualV1,
	)

	var crdFiles []string
	crdFiles = append(crdFiles, crdV1StaticFiles...)
	if config.AboutAPIEnabled {
		crdFiles = append(crdFiles, aboutAPIFile)
	}

	applyErr := crdManager.Apply(ctx,
		func(name string) ([]byte, error) {
			template, err := manifests.KlusterletManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
			helpers.SetRelatedResourcesStatusesWithObj(ctx, &klusterlet.Status.RelatedResources, objData)
			return objData, nil
		},
		crdFiles...,
	)

	if applyErr != nil {
		meta.SetStatusCondition(&klusterlet.Status.Conditions, metav1.Condition{
			Type: operatorapiv1.ConditionKlusterletApplied, Status: metav1.ConditionFalse, Reason: operatorapiv1.ReasonKlusterletCRDApplyFailed,
			Message: applyErr.Error(),
		})
		return klusterlet, reconcileStop, applyErr
	}

	return klusterlet, reconcileContinue, nil
}

// no longer remove the CRDs (AppliedManifestWork & ClusterClaim), because they might be shared
// by multiple klusterlets. Consequently, the CRs of those CRDs will not be deleted as well when deleting a klusterlet.
// Only clean the version label on crds, so another klusterlet can update crds later.
func (r *crdReconcile) clean(ctx context.Context, klusterlet *operatorapiv1.Klusterlet,
	config klusterletConfig) (*operatorapiv1.Klusterlet, reconcileState, error) {
	crdManager := crdmanager.NewManager[*apiextensionsv1.CustomResourceDefinition](
		r.managedClusterClients.apiExtensionClient.ApiextensionsV1().CustomResourceDefinitions(),
		crdmanager.EqualV1,
	)

	var crdFiles []string
	crdFiles = append(crdFiles, crdV1StaticFiles...)
	if config.AboutAPIEnabled {
		crdFiles = append(crdFiles, aboutAPIFile)
	}

	deleteErr := crdManager.Clean(ctx, true,
		func(name string) ([]byte, error) {
			template, err := manifests.KlusterletManifestFiles.ReadFile(name)
			if err != nil {
				return nil, err
			}
			objData := assets.MustCreateAssetFromTemplate(name, template, config).Data
			helpers.SetRelatedResourcesStatusesWithObj(ctx, &klusterlet.Status.RelatedResources, objData)
			return objData, nil
		},
		crdFiles...,
	)

	if deleteErr != nil {
		return klusterlet, reconcileStop, deleteErr
	}

	return klusterlet, reconcileContinue, nil
}
