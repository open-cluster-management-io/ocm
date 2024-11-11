package addonconfiguration

import (
	"fmt"
	"sort"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clustersdkv1alpha1 "open-cluster-management.io/sdk-go/pkg/apis/cluster/v1alpha1"
	clustersdkv1beta1 "open-cluster-management.io/sdk-go/pkg/apis/cluster/v1beta1"

	"open-cluster-management.io/ocm/pkg/common/helpers"
)

// configurationTree is a 2 level snapshot tree on the configuration of addons
// the first level is a list of nodes that represents a install strategy and a desired configuration for this install
// strategy. The second level is a list of nodes that represent each mca and its desired configuration
type configurationGraph struct {
	// nodes maintains a list between a installStrategy and its related mcas
	nodes []*installStrategyNode
	// defaults is the nodes with no install strategy
	defaults *installStrategyNode
}

// installStrategyNode is a node in configurationGraph defined by a install strategy
type installStrategyNode struct {
	placementRef    addonv1alpha1.PlacementRef
	pdTracker       *clustersdkv1beta1.PlacementDecisionClustersTracker
	rolloutStrategy clusterv1alpha1.RolloutStrategy
	rolloutResult   clustersdkv1alpha1.RolloutResult
	desiredConfigs  addonConfigMap
	// children keeps a map of addons node as the children of this node
	children map[string]*addonNode
	clusters sets.Set[string]
}

// addonNode is node as a child of installStrategy node represting a mca
// addonnode
type addonNode struct {
	desiredConfigs addonConfigMap
	mca            *addonv1alpha1.ManagedClusterAddOn
	status         *clustersdkv1alpha1.ClusterRolloutStatus
}

type addonConfigMap map[addonv1alpha1.ConfigGroupResource][]addonv1alpha1.ConfigReference

// set addon rollout status
func (n *addonNode) setRolloutStatus() {
	n.status = &clustersdkv1alpha1.ClusterRolloutStatus{ClusterName: n.mca.Namespace}

	// desired configs doesn't match actual configs, set to ToApply
	if len(n.mca.Status.ConfigReferences) != n.desiredConfigs.len() {
		n.status.Status = clustersdkv1alpha1.ToApply
		return
	}

	var progressingCond metav1.Condition
	for _, cond := range n.mca.Status.Conditions {
		if cond.Type == addonv1alpha1.ManagedClusterAddOnConditionProgressing {
			progressingCond = cond
			break
		}
	}

	for _, actual := range n.mca.Status.ConfigReferences {
		if index, exist := n.desiredConfigs.containsConfig(actual.ConfigGroupResource, actual.DesiredConfig.ConfigReferent); exist {
			// desired config spec hash doesn't match actual, set to ToApply
			if !equality.Semantic.DeepEqual(n.desiredConfigs[actual.ConfigGroupResource][index].DesiredConfig, actual.DesiredConfig) {
				n.status.Status = clustersdkv1alpha1.ToApply
				return
				// desired config spec hash matches actual, but last applied config spec hash doesn't match actual
			} else if !equality.Semantic.DeepEqual(actual.LastAppliedConfig, actual.DesiredConfig) {
				switch progressingCond.Reason {
				case addonv1alpha1.ProgressingReasonFailed:
					n.status.Status = clustersdkv1alpha1.Failed
					n.status.LastTransitionTime = &progressingCond.LastTransitionTime
				case addonv1alpha1.ProgressingReasonProgressing:
					n.status.Status = clustersdkv1alpha1.Progressing
					n.status.LastTransitionTime = &progressingCond.LastTransitionTime
				default:
					n.status.Status = clustersdkv1alpha1.Progressing
				}
				return
			}

		} else {
			n.status.Status = clustersdkv1alpha1.ToApply
			return
		}
	}

	// succeed
	n.status.Status = clustersdkv1alpha1.Succeeded
	if progressingCond.Reason == addonv1alpha1.ProgressingReasonCompleted {
		n.status.LastTransitionTime = &progressingCond.LastTransitionTime
	}
}

func (m addonConfigMap) copy() addonConfigMap {
	output := make(addonConfigMap, len(m))
	for k, v := range m {
		// copy the key ConfigGroupResource
		newk := k.DeepCopy()
		// copy the slice of ConfigReference
		newv := make([]addonv1alpha1.ConfigReference, len(v))
		for i, cr := range v {
			newv[i] = *cr.DeepCopy()
		}
		output[*newk] = newv
	}
	return output
}

func (m addonConfigMap) len() int {
	totalLength := 0
	for _, v := range m {
		totalLength += len(v)
	}
	return totalLength
}

func (m addonConfigMap) containsConfig(expectConfigGr addonv1alpha1.ConfigGroupResource,
	expectConfigRef addonv1alpha1.ConfigReferent) (int, bool) {
	if existArray, ok := m[expectConfigGr]; ok {
		for i, e := range existArray {
			if expectConfigRef == e.DesiredConfig.ConfigReferent {
				return i, true
			}
		}
	}

	return -1, false
}

func (m addonConfigMap) orderedKeys() []addonv1alpha1.ConfigGroupResource {
	gvks := []addonv1alpha1.ConfigGroupResource{}
	for gvk := range m {
		gvks = append(gvks, gvk)
	}
	sort.Slice(gvks, func(i, j int) bool {
		if gvks[i].Group == gvks[j].Group {
			return gvks[i].Resource < gvks[j].Resource
		}
		return gvks[i].Group < gvks[j].Group
	})
	return gvks
}

func newGraph(supportedConfigs []addonv1alpha1.ConfigMeta, defaultConfigReferences []addonv1alpha1.DefaultConfigReference) *configurationGraph {
	graph := &configurationGraph{
		nodes: []*installStrategyNode{},
		defaults: &installStrategyNode{
			desiredConfigs: addonConfigMap{},
			children:       map[string]*addonNode{},
		},
	}

	// init graph.defaults.desiredConfigs with supportedConfigs
	for _, config := range supportedConfigs {
		if config.DefaultConfig != nil {
			graph.defaults.desiredConfigs[config.ConfigGroupResource] = []addonv1alpha1.ConfigReference{
				{
					ConfigGroupResource: config.ConfigGroupResource,
					ConfigReferent:      *config.DefaultConfig,
					DesiredConfig: &addonv1alpha1.ConfigSpecHash{
						ConfigReferent: *config.DefaultConfig,
					},
				},
			}
		}
	}
	// copy the spechash from cma status defaultConfigReferences
	for _, configRef := range defaultConfigReferences {
		if configRef.DesiredConfig == nil {
			continue
		}
		if defaultsDesiredConfigArray, ok := graph.defaults.desiredConfigs[configRef.ConfigGroupResource]; ok {
			for i, defaultsDesiredConfig := range defaultsDesiredConfigArray {
				if defaultsDesiredConfig.DesiredConfig.ConfigReferent == configRef.DesiredConfig.ConfigReferent {
					graph.defaults.desiredConfigs[configRef.ConfigGroupResource][i].DesiredConfig.SpecHash = configRef.DesiredConfig.SpecHash
				}
			}
		}
	}

	return graph
}

// addAddonNode to the graph, starting from placement with the highest order
func (g *configurationGraph) addAddonNode(mca *addonv1alpha1.ManagedClusterAddOn) {
	for i := len(g.nodes) - 1; i >= 0; i-- {
		if g.nodes[i].clusters.Has(mca.Namespace) {
			g.nodes[i].addNode(mca)
			return
		}
	}

	g.defaults.addNode(mca)
}

// addNode delete clusters on existing graph so the new configuration overrides the previous
func (g *configurationGraph) addPlacementNode(
	installStrategy addonv1alpha1.PlacementStrategy,
	installProgression addonv1alpha1.InstallProgression,
	placementLister clusterlisterv1beta1.PlacementLister,
	placementDecisionGetter helpers.PlacementDecisionGetter,
) error {
	placementRef := installProgression.PlacementRef
	installConfigReference := installProgression.ConfigReferences

	// get placement
	if placementLister == nil {
		return fmt.Errorf("invalid placement lister %v", placementLister)
	}
	placement, err := placementLister.Placements(placementRef.Namespace).Get(placementRef.Name)
	if err != nil {
		return err
	}

	// new decision tracker
	pdTracker := clustersdkv1beta1.NewPlacementDecisionClustersTracker(placement, placementDecisionGetter, nil)

	// refresh and get existing decision clusters
	err = pdTracker.Refresh()
	if err != nil {
		return err
	}
	clusters := pdTracker.ExistingClusterGroupsBesides().GetClusters()

	node := &installStrategyNode{
		placementRef:    placementRef,
		pdTracker:       pdTracker,
		rolloutStrategy: installStrategy.RolloutStrategy,
		desiredConfigs:  g.defaults.desiredConfigs,
		children:        map[string]*addonNode{},
		clusters:        clusters,
	}

	// Set MaxConcurrency
	// If progressive strategy is not initialized or MaxConcurrency is not specified, set MaxConcurrency to the default value
	if node.rolloutStrategy.Type == clusterv1alpha1.Progressive {
		progressiveStrategy := node.rolloutStrategy.Progressive

		if progressiveStrategy == nil {
			progressiveStrategy = &clusterv1alpha1.RolloutProgressive{}
		}
		if progressiveStrategy.MaxConcurrency.StrVal == "" && progressiveStrategy.MaxConcurrency.IntVal == 0 {
			progressiveStrategy.MaxConcurrency = placement.Spec.DecisionStrategy.GroupStrategy.ClustersPerDecisionGroup
		}

		node.rolloutStrategy.Progressive = progressiveStrategy
	}

	// overrides configuration by install strategy
	if len(installConfigReference) > 0 {
		node.desiredConfigs = node.desiredConfigs.copy()
		overrideConfigMapByInstallConfigRef(node.desiredConfigs, installConfigReference)
	}

	// remove addon in defaults and other placements.
	for _, cluster := range node.clusters.UnsortedList() {
		if _, ok := g.defaults.children[cluster]; ok {
			node.addNode(g.defaults.children[cluster].mca)
			delete(g.defaults.children, cluster)
		}
		for _, placementNode := range g.nodes {
			if _, ok := placementNode.children[cluster]; ok {
				node.addNode(placementNode.children[cluster].mca)
				delete(placementNode.children, cluster)
			}
		}
	}
	g.nodes = append(g.nodes, node)
	return nil
}

func (g *configurationGraph) generateRolloutResult() error {
	for _, node := range g.nodes {
		if err := node.generateRolloutResult(); err != nil {
			return err
		}
	}
	if err := g.defaults.generateRolloutResult(); err != nil {
		return err
	}
	return nil
}

func (g *configurationGraph) getPlacementNodes() map[addonv1alpha1.PlacementRef]*installStrategyNode {
	placementNodeMap := map[addonv1alpha1.PlacementRef]*installStrategyNode{}
	for _, node := range g.nodes {
		placementNodeMap[node.placementRef] = node
	}

	return placementNodeMap
}

// getAddonsToUpdate returns the list of addons to be updated based on the rollout strategy.
// It is a subset of the addons returned by getAddonsToApply.
// For example, if there are 10 addons whose desired config has not yet been synced to status,
// all 10 addons will be returned by getAddonsToApply().
// Given a Progressive rollout strategy with a maxConcurrency of 3, only 3 of these addons
// will be returned by getAddonsToUpdate() for update.
func (g *configurationGraph) getAddonsToUpdate() []*addonNode {
	var addons []*addonNode
	for _, node := range g.nodes {
		addons = append(addons, node.getAddonsToUpdate()...)
	}

	addons = append(addons, g.defaults.getAddonsToUpdate()...)

	return addons
}

// getAddonsToApply returns the list of addons that need their configurations synchronized.
// ToApply indicates that the resource's desired status has not been applied yet.
func (g *configurationGraph) getAddonsToApply() []*addonNode {
	var addons []*addonNode
	for _, node := range g.nodes {
		addons = append(addons, node.getAddonsToApply()...)
	}

	addons = append(addons, g.defaults.getAddonsToApply()...)

	return addons
}

// getAddonsSucceeded returns the list of addons that their configurations desired status is applied
// and last applied status is successful.
func (g *configurationGraph) getAddonsSucceeded() []*addonNode {
	var addons []*addonNode
	for _, node := range g.nodes {
		addons = append(addons, node.getAddonsSucceeded()...)
	}

	addons = append(addons, g.defaults.getAddonsSucceeded()...)

	return addons
}

func (g *configurationGraph) getRequeueTime() time.Duration {
	minRequeue := maxRequeueTime

	for _, node := range g.nodes {
		nodeRecheckAfter := node.rolloutResult.RecheckAfter
		if nodeRecheckAfter != nil && *nodeRecheckAfter < minRequeue {
			minRequeue = *nodeRecheckAfter
		}
	}

	return minRequeue
}

func (n *installStrategyNode) addNode(addon *addonv1alpha1.ManagedClusterAddOn) {
	n.children[addon.Namespace] = &addonNode{
		mca:            addon,
		desiredConfigs: n.desiredConfigs,
	}

	// override configuration by mca spec
	if len(addon.Spec.Configs) > 0 {
		n.children[addon.Namespace].desiredConfigs = n.children[addon.Namespace].desiredConfigs.copy()
		// TODO we should also filter out the configs which are not supported configs.
		overrideConfigMapByAddOnConfigs(n.children[addon.Namespace].desiredConfigs, addon.Spec.Configs)

		//	go through mca spec configs and copy the specHash from status if they match
		for _, config := range addon.Spec.Configs {
			for _, configRef := range addon.Status.ConfigReferences {
				if configRef.DesiredConfig == nil {
					continue
				}
				// compare the ConfigGroupResource and ConfigReferent
				if configRef.ConfigGroupResource != config.ConfigGroupResource || configRef.DesiredConfig.ConfigReferent != config.ConfigReferent {
					continue
				}
				// copy the spec hash to desired configs
				nodeDesiredConfigArray, ok := n.children[addon.Namespace].desiredConfigs[configRef.ConfigGroupResource]
				if !ok {
					continue
				}
				for i, nodeDesiredConfig := range nodeDesiredConfigArray {
					if nodeDesiredConfig.DesiredConfig.ConfigReferent == configRef.DesiredConfig.ConfigReferent {
						n.children[addon.Namespace].desiredConfigs[configRef.ConfigGroupResource][i].DesiredConfig.SpecHash = configRef.DesiredConfig.SpecHash
					}
				}
			}
		}
	}

	// set addon node rollout status
	n.children[addon.Namespace].setRolloutStatus()
}

func (n *installStrategyNode) generateRolloutResult() error {
	if n.placementRef.Name == "" {
		// default addons
		rolloutResult := clustersdkv1alpha1.RolloutResult{}
		rolloutResult.ClustersToRollout = []clustersdkv1alpha1.ClusterRolloutStatus{}
		for name, addon := range n.children {
			if addon.status == nil {
				return fmt.Errorf("failed to get rollout status on cluster %v", name)
			}
			if addon.status.Status != clustersdkv1alpha1.Succeeded {
				rolloutResult.ClustersToRollout = append(rolloutResult.ClustersToRollout, *addon.status)
			}
		}
		n.rolloutResult = rolloutResult
	} else {
		// placement addons
		rolloutHandler, err := clustersdkv1alpha1.NewRolloutHandler(n.pdTracker, getClusterRolloutStatus)
		if err != nil {
			return err
		}

		// get existing addons
		existingRolloutClusters := []clustersdkv1alpha1.ClusterRolloutStatus{}
		for name, addon := range n.children {
			clsRolloutStatus, err := getClusterRolloutStatus(name, addon)
			if err != nil {
				return err
			}
			existingRolloutClusters = append(existingRolloutClusters, clsRolloutStatus)
		}

		// sort by cluster name
		sort.SliceStable(existingRolloutClusters, func(i, j int) bool {
			return existingRolloutClusters[i].ClusterName < existingRolloutClusters[j].ClusterName
		})

		_, rolloutResult, err := rolloutHandler.GetRolloutCluster(n.rolloutStrategy, existingRolloutClusters)
		if err != nil {
			return err
		}
		n.rolloutResult = rolloutResult
	}

	return nil
}

// addonToUpdate finds the addons to be updated by placement
func (n *installStrategyNode) getAddonsToUpdate() []*addonNode {
	var addons []*addonNode
	var clusters []string

	// get addon to update from rollout result
	for _, c := range n.rolloutResult.ClustersToRollout {
		if _, exist := n.children[c.ClusterName]; exist {
			clusters = append(clusters, c.ClusterName)
		}
	}

	// sort addons by name
	sort.Strings(clusters)
	for _, k := range clusters {
		addons = append(addons, n.children[k])
	}
	return addons
}

// getAddonsToApply return the addons to sync configurations
// ToApply indicates that the resource's desired status has not been applied yet.
func (n *installStrategyNode) getAddonsToApply() []*addonNode {
	var addons []*addonNode

	for i, addon := range n.children {
		if addon.status.Status == clustersdkv1alpha1.ToApply {
			addons = append(addons, n.children[i])
		}
	}
	return addons
}

// getAddonsSucceeded return the addons already rollout successfully or has no configurations
func (n *installStrategyNode) getAddonsSucceeded() []*addonNode {
	var addons []*addonNode

	for i, addon := range n.children {
		if addon.status.Status == clustersdkv1alpha1.Succeeded {
			addons = append(addons, n.children[i])
		}
	}
	return addons
}

// Return the number of succeed addons.
// Including the addons with status Succeed after MinSuccessTime.
func (n *installStrategyNode) countAddonUpgradeSucceed() int {
	count := 0
	for _, addon := range n.children {
		if desiredConfigsEqual(addon.desiredConfigs, n.desiredConfigs) &&
			addon.status.Status == clustersdkv1alpha1.Succeeded &&
			!rolloutStatusHasCluster(n.rolloutResult.ClustersToRollout, addon.mca.Namespace) {
			count += 1
		}
	}
	return count
}

// Return the number of failed addons after ProgressDeadline.
func (n *installStrategyNode) countAddonUpgradeFailed() int {
	count := 0
	for _, addon := range n.children {
		if desiredConfigsEqual(addon.desiredConfigs, n.desiredConfigs) &&
			addon.status.Status == clustersdkv1alpha1.Failed &&
			!rolloutStatusHasCluster(n.rolloutResult.ClustersToRollout, addon.mca.Namespace) {
			count += 1
		}
	}
	return count
}

// Return the number of exiting addons in rolloutResult.ClustersToRollout.
// Including the addons with status ToApply, Progressing within ProgressDeadline, Failed within ProgressDeadline and Succeed within MinSuccessTime.
func (n *installStrategyNode) countAddonUpgrading() int {
	count := 0
	for _, addon := range n.children {
		if rolloutStatusHasCluster(n.rolloutResult.ClustersToRollout, addon.mca.Namespace) {
			count += 1
		}
	}
	return count
}

// Return the number of addons in rolloutResult.ClustersTimeOut.
// Including the addons with status Progressing after ProgressDeadline, Failed after ProgressDeadline.
func (n *installStrategyNode) countAddonTimeOut() int {
	return len(n.rolloutResult.ClustersTimeOut)
}

func getClusterRolloutStatus(clusterName string, addonNode *addonNode) (clustersdkv1alpha1.ClusterRolloutStatus, error) {
	if addonNode.status == nil {
		return clustersdkv1alpha1.ClusterRolloutStatus{}, fmt.Errorf("failed to get rollout status on cluster %v", clusterName)
	}
	return *addonNode.status, nil
}

func desiredConfigsEqual(a, b addonConfigMap) bool {
	if len(a) != len(b) {
		return false
	}

	for configgrA := range a {
		if len(a[configgrA]) != len(b[configgrA]) {
			return false
		}
		for i := range a[configgrA] {
			if a[configgrA][i] != b[configgrA][i] {
				return false
			}
		}
	}

	return true
}

func rolloutStatusHasCluster(clusterRolloutStatus []clustersdkv1alpha1.ClusterRolloutStatus, clusterName string) bool {
	for _, s := range clusterRolloutStatus {
		if s.ClusterName == clusterName {
			return true
		}
	}
	return false
}

// Override the desired addonConfigMap by a slice of InstallConfigReference (from cma status),
func overrideConfigMapByInstallConfigRef(
	desiredConfigs addonConfigMap,
	installConfigRefs []addonv1alpha1.InstallConfigReference,
) {
	gvkOverwritten := sets.New[addonv1alpha1.ConfigGroupResource]()
	// Go through the cma installConfigReferences,
	// for a group of configs with same gvk, cma installConfigReferences override the desiredConfigs.
	for _, configRef := range installConfigRefs {
		if configRef.DesiredConfig == nil {
			continue
		}
		gr := configRef.ConfigGroupResource
		referent := configRef.DesiredConfig.ConfigReferent
		if !gvkOverwritten.Has(gr) {
			desiredConfigs[gr] = []addonv1alpha1.ConfigReference{}
			gvkOverwritten.Insert(gr)
		}
		// If a config not exist in the desiredConfigs, append it.
		// This is to avoid adding duplicate configs (name + namespace).
		if _, exist := desiredConfigs.containsConfig(gr, referent); !exist {
			desiredConfigs[gr] = append(desiredConfigs[gr], addonv1alpha1.ConfigReference{
				ConfigGroupResource: gr,
				ConfigReferent:      referent,
				DesiredConfig:       configRef.DesiredConfig.DeepCopy(),
			})
		}
	}
}

// Override the desired addonConfigMap by a slice of InstallConfigReference (from mca spec),
func overrideConfigMapByAddOnConfigs(
	desiredConfigs addonConfigMap,
	addOnConfigs []addonv1alpha1.AddOnConfig,
) {
	gvkOverwritten := sets.New[addonv1alpha1.ConfigGroupResource]()
	// Go through the mca addOnConfigs,
	// for a group of configs with same gvk, mca addOnConfigs override the desiredConfigs.
	for _, config := range addOnConfigs {
		gr := config.ConfigGroupResource
		referent := config.ConfigReferent
		if !gvkOverwritten.Has(gr) {
			desiredConfigs[gr] = []addonv1alpha1.ConfigReference{}
			gvkOverwritten.Insert(gr)
		}
		// If a config not exist in the desiredConfigs, append it.
		// This is to avoid adding duplicate configs (name + namespace).
		if _, exist := desiredConfigs.containsConfig(gr, referent); !exist {
			desiredConfigs[gr] = append(desiredConfigs[gr], addonv1alpha1.ConfigReference{
				ConfigGroupResource: gr,
				ConfigReferent:      referent,
				DesiredConfig: &addonv1alpha1.ConfigSpecHash{
					ConfigReferent: referent,
				},
			})

		}
	}
}
