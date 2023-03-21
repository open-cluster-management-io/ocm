package resource

import (
	"context"
	"fmt"
	"regexp"
	"sort"

	clusterapiv1 "open-cluster-management.io/api/cluster/v1"
	clusterapiv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
	"open-cluster-management.io/placement/pkg/controllers/framework"
	"open-cluster-management.io/placement/pkg/plugins"
)

const (
	placementLabel = clusterapiv1beta1.PlacementLabel
	description    = `
	ResourceAllocatableCPU and ResourceAllocatableMemory prioritizer makes the scheduling 
	decisions based on the resource allocatable of managed clusters. 
	The clusters that has the most allocatable are given the highest score, 
	while the least is given the lowest score.
	`
)

var _ plugins.Prioritizer = &ResourcePrioritizer{}

var resourceMap = map[string]clusterapiv1.ResourceName{
	"CPU":    clusterapiv1.ResourceCPU,
	"Memory": clusterapiv1.ResourceMemory,
}

type ResourcePrioritizer struct {
	handle          plugins.Handle
	prioritizerName string
	algorithm       string
	resource        clusterapiv1.ResourceName
}

type ResourcePrioritizerBuilder struct {
	resourcePrioritizer *ResourcePrioritizer
}

func NewResourcePrioritizerBuilder(handle plugins.Handle) *ResourcePrioritizerBuilder {
	return &ResourcePrioritizerBuilder{
		resourcePrioritizer: &ResourcePrioritizer{
			handle: handle,
		},
	}
}

func (r *ResourcePrioritizerBuilder) WithPrioritizerName(name string) *ResourcePrioritizerBuilder {
	r.resourcePrioritizer.prioritizerName = name
	return r
}

func (r *ResourcePrioritizerBuilder) Build() *ResourcePrioritizer {
	algorithm, resource := parsePrioritizerName(r.resourcePrioritizer.prioritizerName)
	r.resourcePrioritizer.algorithm = algorithm
	r.resourcePrioritizer.resource = resource
	return r.resourcePrioritizer
}

// parese prioritizerName to algorithm and resource.
// For example, prioritizerName ResourceAllocatableCPU will return Allocatable, CPU.
func parsePrioritizerName(prioritizerName string) (algorithm string, resource clusterapiv1.ResourceName) {
	s := regexp.MustCompile("[A-Z]+[a-z]*").FindAllString(prioritizerName, -1)
	if len(s) == 3 {
		return s[1], resourceMap[s[2]]
	}
	return "", ""
}

func (r *ResourcePrioritizer) Name() string {
	return r.prioritizerName
}

func (r *ResourcePrioritizer) Description() string {
	return description
}

func (r *ResourcePrioritizer) Score(ctx context.Context, placement *clusterapiv1beta1.Placement, clusters []*clusterapiv1.ManagedCluster) (plugins.PluginScoreResult, *framework.Status) {
	status := framework.NewStatus(r.Name(), framework.Success, "")
	if r.algorithm == "Allocatable" {
		return mostResourceAllocatableScores(r.resource, clusters), status
	}
	return plugins.PluginScoreResult{}, status
}

func (r *ResourcePrioritizer) RequeueAfter(ctx context.Context, placement *clusterapiv1beta1.Placement) (plugins.PluginRequeueResult, *framework.Status) {
	return plugins.PluginRequeueResult{}, framework.NewStatus(r.Name(), framework.Success, "")
}

// Calculate clusters scores based on the resource allocatable.
// The clusters that has the most allocatable are given the highest score, while the least is given the lowest score.
// The score range is from -100 to 100.
func mostResourceAllocatableScores(resourceName clusterapiv1.ResourceName, clusters []*clusterapiv1.ManagedCluster) plugins.PluginScoreResult {
	scores := map[string]int64{}

	// get resourceName's min and max allocatable among all the clusters
	minAllocatable, maxAllocatable, err := getClustersMinMaxAllocatableResource(clusters, resourceName)
	if err != nil {
		return plugins.PluginScoreResult{
			Scores: scores,
		}
	}

	for _, cluster := range clusters {
		// get one cluster resourceName's allocatable
		allocatable, _, err := getClusterResource(cluster, resourceName)
		if err != nil {
			continue
		}

		// score = ((resource_x_allocatable - min(resource_x_allocatable)) / (max(resource_x_allocatable) - min(resource_x_allocatable)) - 0.5) * 2 * 100
		if (maxAllocatable - minAllocatable) != 0 {
			ratio := float64(allocatable-minAllocatable) / float64(maxAllocatable-minAllocatable)
			scores[cluster.Name] = int64((ratio - 0.5) * 2.0 * 100.0)
		} else {
			scores[cluster.Name] = 100.0
		}
	}

	return plugins.PluginScoreResult{
		Scores: scores,
	}
}

// Go through one cluster resources and return the allocatable and capacity of the resourceName.
func getClusterResource(cluster *clusterapiv1.ManagedCluster, resourceName clusterapiv1.ResourceName) (allocatable, capacity float64, err error) {
	if v, exist := cluster.Status.Allocatable[resourceName]; exist {
		allocatable = v.AsApproximateFloat64()
	} else {
		return allocatable, capacity, fmt.Errorf("no allocatable %s found in cluster %s", resourceName, cluster.ObjectMeta.Name)
	}

	if v, exist := cluster.Status.Capacity[resourceName]; exist {
		capacity = v.AsApproximateFloat64()
	} else {
		return allocatable, capacity, fmt.Errorf("no capacity %s found in cluster %s", resourceName, cluster.ObjectMeta.Name)
	}

	return allocatable, capacity, nil
}

// Go through all the cluster resources and return the min and max allocatable value of the resourceName.
func getClustersMinMaxAllocatableResource(clusters []*clusterapiv1.ManagedCluster, resourceName clusterapiv1.ResourceName) (minAllocatable, maxAllocatable float64, err error) {
	allocatable := sort.Float64Slice{}

	// get allocatable resources
	for _, cluster := range clusters {
		if alloc, _, err := getClusterResource(cluster, resourceName); err == nil {
			allocatable = append(allocatable, alloc)
		}
	}

	// return err if no allocatable resource
	if len(allocatable) == 0 {
		return 0, 0, fmt.Errorf("no allocatable %s found in clusters", resourceName)
	}

	// sort to get min and max
	sort.Float64s(allocatable)
	return allocatable[0], allocatable[len(allocatable)-1], nil
}
