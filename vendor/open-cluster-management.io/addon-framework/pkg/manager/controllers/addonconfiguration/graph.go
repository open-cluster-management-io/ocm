package addonconfiguration

import (
	"fmt"
	"sort"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/sets"

	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
	clusterlisterv1beta1 "open-cluster-management.io/api/client/cluster/listers/cluster/v1beta1"
	clusterv1alpha1 "open-cluster-management.io/api/cluster/v1alpha1"
	clusterv1beta1 "open-cluster-management.io/api/cluster/v1beta1"
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
	pdTracker       *clusterv1beta1.PlacementDecisionClustersTracker
	rolloutStrategy clusterv1alpha1.RolloutStrategy
	rolloutResult   clusterv1alpha1.RolloutResult
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
	status         *clusterv1alpha1.ClusterRolloutStatus
}

type addonConfigMap map[addonv1alpha1.ConfigGroupResource]addonv1alpha1.ConfigReference

// set addon rollout status
func (n *addonNode) setRolloutStatus() {
	// desired configs doesn't match actual configs, set to ToApply
	if len(n.mca.Status.ConfigReferences) != len(n.desiredConfigs) {
		n.status = &clusterv1alpha1.ClusterRolloutStatus{Status: clusterv1alpha1.ToApply}
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
		if desired, ok := n.desiredConfigs[actual.ConfigGroupResource]; ok {
			// desired config spec hash doesn't match actual, set to ToApply
			if !equality.Semantic.DeepEqual(desired.DesiredConfig, actual.DesiredConfig) {
				n.status = &clusterv1alpha1.ClusterRolloutStatus{Status: clusterv1alpha1.ToApply}
				return
				// desired config spec hash matches actual, but last applied config spec hash doesn't match actual
			} else if !equality.Semantic.DeepEqual(actual.LastAppliedConfig, actual.DesiredConfig) {
				switch progressingCond.Reason {
				case addonv1alpha1.ProgressingReasonInstallFailed, addonv1alpha1.ProgressingReasonUpgradeFailed:
					n.status = &clusterv1alpha1.ClusterRolloutStatus{Status: clusterv1alpha1.Failed, LastTransitionTime: &progressingCond.LastTransitionTime}
				case addonv1alpha1.ProgressingReasonInstalling, addonv1alpha1.ProgressingReasonUpgrading:
					n.status = &clusterv1alpha1.ClusterRolloutStatus{Status: clusterv1alpha1.Progressing, LastTransitionTime: &progressingCond.LastTransitionTime}
				default:
					n.status = &clusterv1alpha1.ClusterRolloutStatus{Status: clusterv1alpha1.Progressing}
				}
				return
			}
		} else {
			n.status = &clusterv1alpha1.ClusterRolloutStatus{Status: clusterv1alpha1.ToApply}
			return
		}
	}

	// succeed
	if progressingCond.Reason == addonv1alpha1.ProgressingReasonInstallSucceed || progressingCond.Reason == addonv1alpha1.ProgressingReasonUpgradeSucceed {
		n.status = &clusterv1alpha1.ClusterRolloutStatus{
			Status:             clusterv1alpha1.Succeeded,
			LastTransitionTime: &progressingCond.LastTransitionTime,
		}
	} else {
		n.status = &clusterv1alpha1.ClusterRolloutStatus{
			Status: clusterv1alpha1.Succeeded,
		}
	}

}

func (d addonConfigMap) copy() addonConfigMap {
	output := addonConfigMap{}
	for k, v := range d {
		output[k] = v
	}
	return output
}

func newGraph(supportedConfigs []addonv1alpha1.ConfigMeta, defaultConfigReferences []addonv1alpha1.DefaultConfigReference) *configurationGraph {
	graph := &configurationGraph{
		nodes: []*installStrategyNode{},
		defaults: &installStrategyNode{
			desiredConfigs: map[addonv1alpha1.ConfigGroupResource]addonv1alpha1.ConfigReference{},
			children:       map[string]*addonNode{},
		},
	}

	// init graph.defaults.desiredConfigs with supportedConfigs
	for _, config := range supportedConfigs {
		if config.DefaultConfig != nil {
			graph.defaults.desiredConfigs[config.ConfigGroupResource] = addonv1alpha1.ConfigReference{
				ConfigGroupResource: config.ConfigGroupResource,
				ConfigReferent:      *config.DefaultConfig,
				DesiredConfig: &addonv1alpha1.ConfigSpecHash{
					ConfigReferent: *config.DefaultConfig,
				},
			}
		}
	}
	// copy the spechash from cma status defaultConfigReferences
	for _, configRef := range defaultConfigReferences {
		if configRef.DesiredConfig == nil {
			continue
		}
		defaultsDesiredConfig, ok := graph.defaults.desiredConfigs[configRef.ConfigGroupResource]
		if ok && (defaultsDesiredConfig.DesiredConfig.ConfigReferent == configRef.DesiredConfig.ConfigReferent) {
			defaultsDesiredConfig.DesiredConfig.SpecHash = configRef.DesiredConfig.SpecHash
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
	placementDecisionGetter PlacementDecisionGetter,
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
	pdTracker := clusterv1beta1.NewPlacementDecisionClustersTracker(placement, placementDecisionGetter, nil)

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
		for _, configRef := range installConfigReference {
			if configRef.DesiredConfig == nil {
				continue
			}
			node.desiredConfigs[configRef.ConfigGroupResource] = addonv1alpha1.ConfigReference{
				ConfigGroupResource: configRef.ConfigGroupResource,
				ConfigReferent:      configRef.DesiredConfig.ConfigReferent,
				DesiredConfig:       configRef.DesiredConfig.DeepCopy(),
			}
		}
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

func (g *configurationGraph) getAddonsToUpdate() []*addonNode {
	var addons []*addonNode
	for _, node := range g.nodes {
		addons = append(addons, node.getAddonsToUpdate()...)
	}

	addons = append(addons, g.defaults.getAddonsToUpdate()...)

	return addons
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
		for _, config := range addon.Spec.Configs {
			n.children[addon.Namespace].desiredConfigs[config.ConfigGroupResource] = addonv1alpha1.ConfigReference{
				ConfigGroupResource: config.ConfigGroupResource,
				ConfigReferent:      config.ConfigReferent,
				DesiredConfig: &addonv1alpha1.ConfigSpecHash{
					ConfigReferent: config.ConfigReferent,
				},
			}
			// copy the spechash from mca status
			for _, configRef := range addon.Status.ConfigReferences {
				if configRef.DesiredConfig == nil {
					continue
				}
				nodeDesiredConfig, ok := n.children[addon.Namespace].desiredConfigs[configRef.ConfigGroupResource]
				if ok && (nodeDesiredConfig.DesiredConfig.ConfigReferent == configRef.DesiredConfig.ConfigReferent) {
					nodeDesiredConfig.DesiredConfig.SpecHash = configRef.DesiredConfig.SpecHash
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
		rolloutResult := clusterv1alpha1.RolloutResult{}
		rolloutResult.ClustersToRollout = map[string]clusterv1alpha1.ClusterRolloutStatus{}
		for k, addon := range n.children {
			if addon.status.Status != clusterv1alpha1.Succeeded {
				rolloutResult.ClustersToRollout[k] = *addon.status
			}
		}
		n.rolloutResult = rolloutResult
	} else {
		// placement addons
		rolloutHandler, err := clusterv1alpha1.NewRolloutHandler(n.pdTracker)
		if err != nil {
			return err
		}
		_, rolloutResult, err := rolloutHandler.GetRolloutCluster(n.rolloutStrategy, n.getUpgradeStatus)
		if err != nil {
			return err
		}
		n.rolloutResult = rolloutResult
	}

	return nil
}

func (n *installStrategyNode) getUpgradeStatus(clusterName string) clusterv1alpha1.ClusterRolloutStatus {
	if node, exist := n.children[clusterName]; exist {
		return *node.status
	} else {
		// if children not exist, return succeed status to skip
		return clusterv1alpha1.ClusterRolloutStatus{Status: clusterv1alpha1.Skip}
	}
}

// addonToUpdate finds the addons to be updated by placement
func (n *installStrategyNode) getAddonsToUpdate() []*addonNode {
	var addons []*addonNode
	var clusters []string

	// get addon to update from rollout result
	for c := range n.rolloutResult.ClustersToRollout {
		clusters = append(clusters, c)
	}

	// sort addons by name
	sort.Strings(clusters)
	for _, k := range clusters {
		addons = append(addons, n.children[k])
	}
	return addons
}

func (n *installStrategyNode) countAddonUpgradeSucceed() int {
	count := 0
	for _, addon := range n.children {
		if desiredConfigsEqual(addon.desiredConfigs, n.desiredConfigs) && addon.status.Status == clusterv1alpha1.Succeeded {
			count += 1
		}
	}
	return count
}

func (n *installStrategyNode) countAddonUpgrading() int {
	count := 0
	for _, addon := range n.children {
		if desiredConfigsEqual(addon.desiredConfigs, n.desiredConfigs) && addon.status.Status == clusterv1alpha1.Progressing {
			count += 1
		}
	}
	return count
}

func (n *installStrategyNode) countAddonTimeOut() int {
	return len(n.rolloutResult.ClustersTimeOut)
}

func desiredConfigsEqual(a, b addonConfigMap) bool {
	if len(a) != len(b) {
		return false
	}

	for configgrA := range a {
		if a[configgrA] != b[configgrA] {
			return false
		}
	}

	return true
}
