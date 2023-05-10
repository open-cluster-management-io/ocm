package addonconfiguration

import (
	"fmt"
	"math"
	"sort"
	"strconv"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/sets"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
)

var (
	defaultMaxConcurrency = intstr.FromString("25%")
	maxMaxConcurrency     = intstr.FromString("100%")
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
	placementRef   addonv1alpha1.PlacementRef
	maxConcurrency intstr.IntOrString
	desiredConfigs addonConfigMap
	// children keeps a map of addons node as the children of this node
	children map[string]*addonNode
	clusters sets.Set[string]
}

// addonNode is node as a child of installStrategy node represting a mca
// addonnode
type addonNode struct {
	desiredConfigs addonConfigMap
	mca            *addonv1alpha1.ManagedClusterAddOn
	// record mca upgrade status
	mcaUpgradeStatus upgradeStatus
}

type upgradeStatus int

const (
	// mca desired configs not synced from desiredConfigs yet
	toupgrade upgradeStatus = iota
	// mca desired configs upgraded and last applied configs not upgraded
	upgrading
	// both desired configs and last applied configs are upgraded
	upgraded
)

type addonConfigMap map[addonv1alpha1.ConfigGroupResource]addonv1alpha1.ConfigReference

// set addon upgrade status
func (n *addonNode) setUpgradeStatus() {
	if len(n.mca.Status.ConfigReferences) != len(n.desiredConfigs) {
		n.mcaUpgradeStatus = toupgrade
		return
	}

	for _, actual := range n.mca.Status.ConfigReferences {
		if desired, ok := n.desiredConfigs[actual.ConfigGroupResource]; ok {
			if !equality.Semantic.DeepEqual(desired.DesiredConfig, actual.DesiredConfig) {
				n.mcaUpgradeStatus = toupgrade
				return
			} else if !equality.Semantic.DeepEqual(actual.LastAppliedConfig, actual.DesiredConfig) {
				n.mcaUpgradeStatus = upgrading
				return
			}
		} else {
			n.mcaUpgradeStatus = toupgrade
			return
		}
	}

	n.mcaUpgradeStatus = upgraded
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
			maxConcurrency: maxMaxConcurrency,
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
	clusters []string,
) {
	placementRef := installProgression.PlacementRef
	installConfigReference := installProgression.ConfigReferences

	node := &installStrategyNode{
		placementRef:   placementRef,
		maxConcurrency: maxMaxConcurrency,
		desiredConfigs: g.defaults.desiredConfigs,
		children:       map[string]*addonNode{},
		clusters:       sets.New[string](clusters...),
	}

	// set max concurrency
	if installStrategy.RolloutStrategy.Type == addonv1alpha1.AddonRolloutStrategyRollingUpdate {
		if installStrategy.RolloutStrategy.RollingUpdate != nil {
			node.maxConcurrency = installStrategy.RolloutStrategy.RollingUpdate.MaxConcurrency
		} else {
			node.maxConcurrency = defaultMaxConcurrency
		}
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
	for _, cluster := range clusters {
		if _, ok := g.defaults.children[cluster]; ok {
			node.addNode(g.defaults.children[cluster].mca)
			delete(g.defaults.children, cluster)
		}
		for _, placement := range g.nodes {
			if _, ok := placement.children[cluster]; ok {
				node.addNode(placement.children[cluster].mca)
				delete(placement.children, cluster)
			}
		}
	}
	g.nodes = append(g.nodes, node)
}

func (g *configurationGraph) getPlacementNodes() map[addonv1alpha1.PlacementRef]*installStrategyNode {
	placementNodeMap := map[addonv1alpha1.PlacementRef]*installStrategyNode{}
	for _, node := range g.nodes {
		placementNodeMap[node.placementRef] = node
	}

	return placementNodeMap
}

func (g *configurationGraph) addonToUpdate() []*addonNode {
	var addons []*addonNode
	for _, node := range g.nodes {
		addons = append(addons, node.addonToUpdate()...)
	}

	addons = append(addons, g.defaults.addonToUpdate()...)

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

	// set addon node upgrade status
	n.children[addon.Namespace].setUpgradeStatus()
}

func (n *installStrategyNode) addonUpgraded() int {
	count := 0
	for _, addon := range n.children {
		if addon.mcaUpgradeStatus == upgraded {
			count += 1
		}
	}
	return count
}

func (n *installStrategyNode) addonUpgrading() int {
	count := 0
	for _, addon := range n.children {
		if addon.mcaUpgradeStatus == upgrading {
			count += 1
		}
	}
	return count
}

// addonToUpdate finds the addons to be updated by placement
func (n *installStrategyNode) addonToUpdate() []*addonNode {
	var addons []*addonNode

	// sort the children by key
	keys := make([]string, 0, len(n.children))
	for k := range n.children {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	total := len(n.clusters)
	if total == 0 {
		total = len(n.children)
	}

	length, _ := parseMaxConcurrency(n.maxConcurrency, total)
	if length == 0 {
		return addons
	}

	for i, k := range keys {
		if (i%length == 0) && len(addons) > 0 {
			return addons
		}

		addon := n.children[k]
		if addon.mcaUpgradeStatus != upgraded {
			addons = append(addons, addon)
		}
	}

	return addons
}

func parseMaxConcurrency(maxConcurrency intstr.IntOrString, total int) (int, error) {
	var length int

	switch maxConcurrency.Type {
	case intstr.String:
		str := maxConcurrency.StrVal
		f, err := strconv.ParseFloat(str[:len(str)-1], 64)
		if err != nil {
			return length, err
		}
		length = int(math.Ceil(f / 100 * float64(total)))
	case intstr.Int:
		length = maxConcurrency.IntValue()
	default:
		return length, fmt.Errorf("incorrect MaxConcurrency type %v", maxConcurrency.Type)
	}

	return length, nil
}
