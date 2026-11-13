package authn

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
	"k8s.io/apimachinery/pkg/util/validation"

	"open-cluster-management.io/sdk-go/pkg/spiffe/idtemplate"
)

// AgentRole is the role of a Klusterlet agent.
type AgentRole string

const (
	RoleRegistration AgentRole = "registration"
	RoleWork         AgentRole = "work"
	RoleKlusterlet   AgentRole = "klusterlet"
)

// OCMIdentity is what an agent SPIFFE ID means to OCM.
type OCMIdentity struct {
	TrustDomain string // the SVID's trust domain, for pinning (Day 11)
	ClusterName string
	Role        AgentRole // set for Klusterlet agents
	AddOnName   string    // set for add-on agents
	AgentName   string    // set for add-on agents
	Namespace   string    // from {namespace} if the template has it; for logs only
	User        string
	Groups      []string
}

// IsAddOn reports whether the identity is an add-on agent.
func (i OCMIdentity) IsAddOn() bool { return i.AddOnName != "" }

// IDScheme maps agent SPIFFE IDs to OCM identities.
type IDScheme struct {
	agent, addOn *idtemplate.Template
	trustDomains []spiffeid.TrustDomain
}

// NewIDScheme checks both templates and the accepted trust domains (agentTrustDomains,
// which always include the hub's own).
func NewIDScheme(agentTemplate, addOnTemplate string, trustDomains []string) (*IDScheme, error) {
	agent, err := idtemplate.Compile(idtemplate.KindAgent, agentTemplate)
	if err != nil {
		return nil, err
	}
	addOn, err := idtemplate.Compile(idtemplate.KindAddOnAgent, addOnTemplate)
	if err != nil {
		return nil, err
	}
	if idtemplate.Overlaps(agent, addOn) {
		return nil, fmt.Errorf("agentIDTemplate %q and addOnAgentIDTemplate %q can match the same SPIFFE ID",
			agent, addOn)
	}
	if len(trustDomains) == 0 {
		return nil, fmt.Errorf("no agent trust domain")
	}
	s := &IDScheme{agent: agent, addOn: addOn}
	for _, name := range trustDomains {
		td, err := spiffeid.TrustDomainFromString(name)
		if err != nil {
			return nil, fmt.Errorf("agent trust domain %q: %w", name, err)
		}
		s.trustDomains = append(s.trustDomains, td)
	}
	return s, nil
}

// CheckServerID refuses a hub server ID that an agent could also present.
func (s *IDScheme) CheckServerID(id spiffeid.ID) error {
	for _, t := range []*idtemplate.Template{s.agent, s.addOn} {
		if _, ok := t.Match(id); ok {
			return fmt.Errorf("serverSPIFFEID %q matches %q; agents could present it", id, t)
		}
	}
	return nil
}

// TrustDomains returns the accepted agent trust domains, for the server TLS authorizer.
func (s *IDScheme) TrustDomains() []spiffeid.TrustDomain { return s.trustDomains }

// Parse maps an agent SPIFFE ID to its OCM identity.
func (s *IDScheme) Parse(id spiffeid.ID) (OCMIdentity, error) {
	if !slices.Contains(s.trustDomains, id.TrustDomain()) {
		return OCMIdentity{}, fmt.Errorf("SPIFFE ID %q: trust domain %q is not in agentTrustDomains",
			id, id.TrustDomain().Name())
	}
	if v, ok := s.agent.Match(id); ok {
		return agentIdentity(id, v)
	}
	if v, ok := s.addOn.Match(id); ok {
		return addOnIdentity(id, v)
	}
	return OCMIdentity{}, fmt.Errorf("SPIFFE ID %q matches neither agentIDTemplate %q nor addOnAgentIDTemplate %q",
		id, s.agent, s.addOn)
}

func agentIdentity(id spiffeid.ID, v map[string]string) (OCMIdentity, error) {
	cluster, role, ns := v[idtemplate.Cluster], AgentRole(v[idtemplate.Role]), v[idtemplate.Namespace]
	if err := validNames(map[string]string{"cluster": cluster, "namespace": ns}); err != nil {
		return OCMIdentity{}, err
	}
	switch role {
	case RoleRegistration, RoleWork, RoleKlusterlet:
	default:
		return OCMIdentity{}, fmt.Errorf("unknown agent role %q in %q", role, id)
	}
	return OCMIdentity{
		TrustDomain: id.TrustDomain().Name(),
		ClusterName: cluster,
		Role:        role,
		Namespace:   ns,
		User:        fmt.Sprintf("system:open-cluster-management:%s:%s", cluster, role),
		Groups: []string{
			"system:open-cluster-management:" + cluster,
			"system:open-cluster-management:managed-clusters",
			"system:authenticated",
		},
	}, nil
}

func addOnIdentity(id spiffeid.ID, v map[string]string) (OCMIdentity, error) {
	cluster, addon, agent, ns := v[idtemplate.Cluster], v[idtemplate.AddOn], v[idtemplate.Agent], v[idtemplate.Namespace]
	if err := validNames(map[string]string{"cluster": cluster, "add-on": addon, "agent": agent, "namespace": ns}); err != nil {
		return OCMIdentity{}, err
	}
	return OCMIdentity{
		TrustDomain: id.TrustDomain().Name(),
		ClusterName: cluster,
		AddOnName:   addon,
		AgentName:   agent,
		Namespace:   ns,
		User:        fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:%s:agent:%s", cluster, addon, agent),
		Groups: []string{
			fmt.Sprintf("system:open-cluster-management:cluster:%s:addon:%s", cluster, addon),
			"system:open-cluster-management:addon:" + addon,
			"system:authenticated",
		},
	}, nil
}

// validNames checks every non-empty value; an empty value is an unused optional placeholder.
func validNames(names map[string]string) error {
	for kind, v := range names {
		if v == "" {
			continue
		}
		if errs := validation.IsDNS1123Label(v); len(errs) > 0 {
			return fmt.Errorf("invalid %s name %q: %s", kind, v, strings.Join(errs, "; "))
		}
	}
	return nil
}
