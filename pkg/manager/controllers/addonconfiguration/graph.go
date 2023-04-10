package addonconfiguration

import (
	"k8s.io/apimachinery/pkg/util/sets"
	addonv1alpha1 "open-cluster-management.io/api/addon/v1alpha1"
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
	desiredConfigs addonConfigMap
	// children keeps a map of addons node as the children of this node
	children map[string]*addonNode
	clusters sets.Set[string]
}

// addonNode is node as a child of installStrategy node represting a mca
type addonNode struct {
	desiredConfigs addonConfigMap
	mca            *addonv1alpha1.ManagedClusterAddOn
}

type addonConfigMap map[addonv1alpha1.ConfigGroupResource]addonv1alpha1.ConfigReference

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
func (g *configurationGraph) addPlacementNode(installConfigReference []addonv1alpha1.InstallConfigReference, clusters []string) {
	node := &installStrategyNode{
		desiredConfigs: g.defaults.desiredConfigs,
		children:       map[string]*addonNode{},
		clusters:       sets.New[string](clusters...),
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
}

// addonToUpdate finds the addons to be updated by placement
func (n *installStrategyNode) addonToUpdate() []*addonNode {
	var addons []*addonNode

	for _, addon := range n.children {
		addons = append(addons, addon)
	}

	return addons
}
