package debugger

import (
	"encoding/json"
	"net/http"
	"strings"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	scheduling "open-cluster-management.io/placement/pkg/controllers/scheduling"
)

const DebugPath = "/debug/placements/"

// Debugger provides a debug http endpoint for scheduler
type Debugger struct {
	scheduler       scheduling.Scheduler
	clusterLister   clusterlisterv1.ManagedClusterLister
	placementLister clusterlisterv1beta1.PlacementLister
}

// DebugResult is the result returned by debugger
type DebugResult struct {
	FilterResults     []scheduling.FilterResult      `json:"filteredPiplieResults,omitempty"`
	PrioritizeResults []scheduling.PrioritizerResult `json:"prioritizeResults,omitempty"`
	Error             string                         `json:"error,omitempty"`
}

func NewDebugger(
	scheduler scheduling.Scheduler,
	placementInformer clusterinformerv1beta1.PlacementInformer,
	clusterInformer clusterinformerv1.ManagedClusterInformer) *Debugger {
	return &Debugger{
		scheduler:       scheduler,
		clusterLister:   clusterInformer.Lister(),
		placementLister: placementInformer.Lister(),
	}
}

func (d *Debugger) Handler(w http.ResponseWriter, r *http.Request) {
	namespace, name, err := d.parsePath(r.URL.Path)
	if err != nil {
		d.reportErr(w, err)
		return
	}

	placement, err := d.placementLister.Placements(namespace).Get(name)
	if err != nil {
		d.reportErr(w, err)
		return
	}

	clusters, err := d.clusterLister.List(labels.Everything())
	if err != nil {
		d.reportErr(w, err)
		return
	}

	scheduleResults, _ := d.scheduler.Schedule(r.Context(), placement, clusters)

	result := DebugResult{FilterResults: scheduleResults.FilterResults(), PrioritizeResults: scheduleResults.PrioritizerResults()}

	resultByte, _ := json.Marshal(result)

	w.Write(resultByte)
}

func (d *Debugger) parsePath(path string) (string, string, error) {
	metaNamespaceKey := strings.TrimPrefix(path, DebugPath)
	return cache.SplitMetaNamespaceKey(metaNamespaceKey)
}

func (d *Debugger) reportErr(w http.ResponseWriter, err error) {
	result := &DebugResult{Error: err.Error()}

	resultByte, _ := json.Marshal(result)

	w.Write(resultByte)
}
