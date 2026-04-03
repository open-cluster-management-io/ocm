package debugger

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	authorizationv1 "k8s.io/api/authorization/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apiserver/pkg/endpoints/request"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"

	clusterinformerv1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1"
	clusterinformerv1beta1 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta1"
	clusterinformerv1beta2 "open-cluster-management.io/api/client/cluster/informers/externalversions/cluster/v1beta2"
	clusterlisterv1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterlisterv1beta2 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta2"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"

	"open-cluster-management.io/ocm/pkg/placement/controllers/scheduling"
)

const (
	DebugPath = "/debug/placements/"
	// maxRequestBodyBytes is the maximum size for a POST request body (1MB)
	maxRequestBodyBytes = 1 * 1024 * 1024
)

// Debugger provides a debug http endpoint for scheduler
type Debugger struct {
	scheduler               scheduling.Scheduler
	kubeClient              kubernetes.Interface
	clusterLister           clusterlisterv1.ManagedClusterLister
	clusterSetLister        clusterlisterv1beta2.ManagedClusterSetLister
	clusterSetBindingLister clusterlisterv1beta2.ManagedClusterSetBindingLister
	placementLister         clusterlisterv1beta1.PlacementLister
}

// ClusterScore represents a cluster with its score
type ClusterScore struct {
	ClusterName string `json:"clusterName"`
	Score       int64  `json:"score"`
}

// DebugResult is the result returned by debugger
type DebugResult struct {
	Placement         *clusterv1beta1.Placement      `json:"placement,omitempty"`
	FilterResults     []scheduling.FilterResult      `json:"filteredPipelineResults,omitempty"`
	PrioritizeResults []scheduling.PrioritizerResult `json:"prioritizeResults,omitempty"`
	AggregatedScores  []ClusterScore                 `json:"aggregatedScores,omitempty"`
	Error             string                         `json:"error,omitempty"`
}

func NewDebugger(
	scheduler scheduling.Scheduler,
	kubeClient kubernetes.Interface,
	placementInformer clusterinformerv1beta1.PlacementInformer,
	clusterInformer clusterinformerv1.ManagedClusterInformer,
	clusterSetInformer clusterinformerv1beta2.ManagedClusterSetInformer,
	clusterSetBindingInformer clusterinformerv1beta2.ManagedClusterSetBindingInformer,
) *Debugger {
	return &Debugger{
		scheduler:               scheduler,
		kubeClient:              kubeClient,
		clusterLister:           clusterInformer.Lister(),
		clusterSetLister:        clusterSetInformer.Lister(),
		clusterSetBindingLister: clusterSetBindingInformer.Lister(),
		placementLister:         placementInformer.Lister(),
	}
}

func (d *Debugger) Handler(w http.ResponseWriter, r *http.Request) {
	var placement *clusterv1beta1.Placement
	var err error

	// Support both GET (fetch from API) and POST (accept JSON body)
	if r.Method == http.MethodPost {
		// POST: Parse Placement from request body
		placement, err = d.parsePlacementFromBody(r)
		if err != nil {
			d.reportErr(w, err)
			return
		}
	} else {
		// GET: Fetch Placement from API (original behavior)
		namespace, name, err := d.parsePath(r.URL.Path)
		if err != nil {
			d.reportErr(w, err)
			return
		}

		placement, err = d.placementLister.Placements(namespace).Get(name)
		if err != nil {
			d.reportErr(w, err)
			return
		}
	}

	// Check if user has permission to create placements in this namespace
	if err := d.checkPermission(r, placement.Namespace); err != nil {
		d.reportErr(w, err)
		return
	}

	// Get valid clustersetbindings in the placement namespace
	bindings, err := scheduling.GetValidManagedClusterSetBindings(placement.Namespace, d.clusterSetBindingLister, d.clusterSetLister)
	if err != nil {
		d.reportErr(w, err)
		return
	}

	// Get eligible clustersets for the placement
	clusterSetNames := scheduling.GetEligibleClusterSets(placement, bindings)

	// Get available clusters for the placement
	clusters, err := scheduling.GetAvailableClusters(clusterSetNames, d.clusterSetLister, d.clusterLister)
	if err != nil {
		d.reportErr(w, err)
		return
	}

	scheduleResults, _ := d.scheduler.Schedule(r.Context(), placement, clusters)

	// Create placement without status and runtime metadata
	placementCopy := placement.DeepCopy()
	placementCopy.ObjectMeta = metav1.ObjectMeta{
		Name:        placement.Name,
		Namespace:   placement.Namespace,
		Labels:      placement.Labels,
		Annotations: placement.Annotations,
	}
	placementCopy.Status = clusterv1beta1.PlacementStatus{}

	result := DebugResult{
		Placement:         placementCopy,
		FilterResults:     scheduleResults.FilterResults(),
		PrioritizeResults: scheduleResults.PrioritizerResults(),
		AggregatedScores:  convertAndSortScores(scheduleResults.PrioritizerScores()),
	}

	resultByte, _ := json.Marshal(result)

	_, _ = w.Write(resultByte)
}

func (d *Debugger) parsePath(path string) (string, string, error) {
	metaNamespaceKey := strings.TrimPrefix(path, DebugPath)
	return cache.SplitMetaNamespaceKey(metaNamespaceKey)
}

func (d *Debugger) parsePlacementFromBody(r *http.Request) (*clusterv1beta1.Placement, error) {
	defer r.Body.Close()

	// Check Content-Length header if present
	if r.ContentLength > maxRequestBodyBytes {
		return nil, fmt.Errorf("request body too large: %d bytes exceeds maximum of %d bytes", r.ContentLength, maxRequestBodyBytes)
	}

	// Use LimitReader to enforce size limit during reading
	limitedReader := io.LimitReader(r.Body, maxRequestBodyBytes+1)
	body, err := io.ReadAll(limitedReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read request body: %w", err)
	}

	// Check if we read more than the limit
	if len(body) > maxRequestBodyBytes {
		return nil, fmt.Errorf("request body too large: exceeds maximum of %d bytes", maxRequestBodyBytes)
	}

	var placement clusterv1beta1.Placement
	if err := json.Unmarshal(body, &placement); err != nil {
		return nil, fmt.Errorf("failed to unmarshal placement JSON: %w", err)
	}

	return &placement, nil
}

func (d *Debugger) reportErr(w http.ResponseWriter, err error) {
	result := &DebugResult{Error: err.Error()}

	resultByte, _ := json.Marshal(result)

	_, _ = w.Write(resultByte)
}

// checkPermission checks if the user has permission to create placements in the namespace using SAR
func (d *Debugger) checkPermission(r *http.Request, namespace string) error {
	// Get user from request context (authenticated by GenericAPIServer)
	ctx := r.Context()
	userInfo, ok := request.UserFrom(ctx)
	if !ok {
		return fmt.Errorf("user information not found in request context")
	}

	username := userInfo.GetName()
	groups := userInfo.GetGroups()
	extra := make(map[string]authorizationv1.ExtraValue)
	for k, v := range userInfo.GetExtra() {
		extra[k] = authorizationv1.ExtraValue(v)
	}

	// Create SubjectAccessReview to check if user can create placements
	sar := &authorizationv1.SubjectAccessReview{
		Spec: authorizationv1.SubjectAccessReviewSpec{
			User:   username,
			Groups: groups,
			Extra:  extra,
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace: namespace,
				Verb:      "create",
				Group:     "cluster.open-cluster-management.io",
				Version:   "v1beta1",
				Resource:  "placements",
			},
		},
	}

	// Perform the SAR check
	// TODO: Cache successful permission checks to avoid SAR API calls on every request.
	result, err := d.kubeClient.AuthorizationV1().SubjectAccessReviews().Create(
		context.TODO(), sar, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to check permissions: %w", err)
	}

	if !result.Status.Allowed {
		return fmt.Errorf("user does not have permission to create placements in namespace %s: %s",
			namespace, result.Status.Reason)
	}

	return nil
}

// convertAndSortScores converts a map of cluster scores to a sorted slice.
// Scores are sorted in descending order, with cluster names as tiebreaker (ascending).
func convertAndSortScores(scoreMap scheduling.PrioritizerScore) []ClusterScore {
	clusterScores := make([]ClusterScore, 0, len(scoreMap))
	for name, score := range scoreMap {
		clusterScores = append(clusterScores, ClusterScore{
			ClusterName: name,
			Score:       score,
		})
	}
	sort.Slice(clusterScores, func(i, j int) bool {
		if clusterScores[i].Score == clusterScores[j].Score {
			return clusterScores[i].ClusterName < clusterScores[j].ClusterName
		}
		return clusterScores[i].Score > clusterScores[j].Score
	})
	return clusterScores
}
