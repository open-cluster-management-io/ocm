package addonmanager

import (
	"context"
	"fmt"

	"github.com/open-cluster-management/addon-framework/pkg/agent"
	"k8s.io/client-go/rest"
)

// AddonManager is the interface to initialize a manager on hub to manage the addon
// agents on all managedcluster
type AddonManager interface {
	// AddAgent register an addon agent to the manager.
	AddAgent(addon agent.AgentAddon) error

	// Start starts all registered addon agent.
	Start(ctx context.Context) error
}

type addonManager struct {
	addonAgents map[string]agent.AgentAddon
	config      *rest.Config
}

func (a *addonManager) AddAgent(addon agent.AgentAddon) error {
	addonOption := addon.GetAgentAddonOptions()
	if len(addonOption.AddonName) == 0 {
		return fmt.Errorf("Addon name should be set")
	}
	a.addonAgents[addonOption.AddonName] = addon
	return nil
}

func (a *addonManager) Start(ctx context.Context) error {
	return nil
}

// New returns a new Manager for creating addon agents.
func New(config *rest.Config) (AddonManager, error) {
	return &addonManager{
		config:      config,
		addonAgents: map[string]agent.AgentAddon{},
	}, nil
}
