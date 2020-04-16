package controllers

import (
	"context"
	"time"

	workv1client "github.com/open-cluster-management/api/client/work/clientset/versioned/typed/work/v1"
	workinformer "github.com/open-cluster-management/api/client/work/informers/externalversions/work/v1"
	worklister "github.com/open-cluster-management/api/client/work/listers/work/v1"
	"github.com/openshift/library-go/pkg/controller/factory"
	"github.com/openshift/library-go/pkg/operator/events"
)

// ManifestWorkController is to reconcile the workload resources
// fetched from hub cluster on spoke cluster.
type ManifestWorkController struct {
	manifestWorkClient   workv1client.ManifestWorkInterface
	manifestWorkInformer workinformer.ManifestWorkInformer
	manifestWorkLister   worklister.ManifestWorkNamespaceLister
}

// NewManifestWorkController returns a ManifestWorkController
func NewManifestWorkController(
	manifestWorkClient workv1client.ManifestWorkInterface,
	manifestWorkInformer workinformer.ManifestWorkInformer,
	manifestWorkLister worklister.ManifestWorkNamespaceLister) *ManifestWorkController {

	return &ManifestWorkController{
		manifestWorkClient:   manifestWorkClient,
		manifestWorkInformer: manifestWorkInformer,
		manifestWorkLister:   manifestWorkLister,
	}
}

// sync is the main reconcile loop for manefist work. It is triggered in two scenarios
// 1. ManifestWork API changes
// 2. Resources defined in manifest changed on spoke
func (m *ManifestWorkController) sync(ctx context.Context, controllerContext factory.SyncContext) error {
	return nil
}

// Run starts the controller using factory utility
func (m *ManifestWorkController) Run(ctx context.Context, recorder events.Recorder) {
	factory.New().
		WithInformers(m.manifestWorkInformer.Informer()).
		WithSync(m.sync).ResyncEvery(5*time.Minute).ToController("ManifestWorkAgent", recorder).Run(ctx, 1)
}
