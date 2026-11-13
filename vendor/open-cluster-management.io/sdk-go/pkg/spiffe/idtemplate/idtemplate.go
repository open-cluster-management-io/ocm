// Package idtemplate compiles, renders and matches OCM agent SPIFFE ID templates.
package idtemplate

import (
	"fmt"
	"slices"
	"strings"

	"github.com/spiffe/go-spiffe/v2/spiffeid"
)

// Kind says which agents a template is for. Its value is the API field name, for messages.
type Kind string

const (
	KindAgent      Kind = "agentIDTemplate"
	KindAddOnAgent Kind = "addOnAgentIDTemplate"
)

// Placeholder names.
const (
	Cluster   = "cluster"
	Role      = "role"
	AddOn     = "addon"
	Agent     = "agent"
	Namespace = "namespace"
)

const prefix = "spiffe://{trustDomain}/"

var (
	allowed = map[Kind][]string{
		KindAgent:      {Cluster, Role, Namespace},
		KindAddOnAgent: {Cluster, AddOn, Agent, Namespace},
	}
	required = map[Kind][]string{
		KindAgent:      {Cluster, Role},
		KindAddOnAgent: {Cluster, AddOn, Agent},
	}
)

type segment struct {
	literal     string // fixed text
	placeholder string // name of a {placeholder}
}

// Template is a checked agent ID template.
type Template struct {
	kind Kind
	raw  string
	segs []segment
}

// Compile parses s and checks it against the rules for kind.
func Compile(kind Kind, s string) (*Template, error) {
	if !strings.HasPrefix(s, prefix) {
		return nil, fmt.Errorf("%s %q must start with %s", kind, s, prefix)
	}
	t := &Template{kind: kind, raw: s}
	seen := map[string]bool{}
	fixed := 0
	for _, p := range strings.Split(strings.TrimPrefix(s, prefix), "/") {
		switch {
		case strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}"):
			name := p[1 : len(p)-1]
			if !slices.Contains(allowed[kind], name) {
				return nil, fmt.Errorf("%s %q: unknown placeholder {%s}, allowed: %s",
					kind, s, name, strings.Join(allowed[kind], ", "))
			}
			if seen[name] {
				return nil, fmt.Errorf("%s %q: {%s} appears more than once", kind, s, name)
			}
			seen[name] = true
			t.segs = append(t.segs, segment{placeholder: name})
		case strings.ContainsAny(p, "{}"):
			return nil, fmt.Errorf("%s %q: a placeholder must fill a whole path segment, got %q", kind, s, p)
		default:
			if err := spiffeid.ValidatePathSegment(p); err != nil {
				return nil, fmt.Errorf("%s %q: segment %q: %w", kind, s, p, err)
			}
			fixed++
			t.segs = append(t.segs, segment{literal: p})
		}
	}
	for _, r := range required[kind] {
		if !seen[r] {
			return nil, fmt.Errorf("%s %q must contain {%s}", kind, s, r)
		}
	}
	if fixed == 0 {
		return nil, fmt.Errorf("%s %q needs at least one fixed path segment", kind, s)
	}
	return t, nil
}

func (t *Template) String() string { return t.raw }

// Has reports whether the template uses the placeholder.
func (t *Template) Has(name string) bool {
	return slices.ContainsFunc(t.segs, func(s segment) bool { return s.placeholder == name })
}

// Match returns the placeholder values when the path of id fits the template. It checks
// neither the trust domain nor the values.
func (t *Template) Match(id spiffeid.ID) (map[string]string, bool) {
	parts := strings.Split(strings.TrimPrefix(id.Path(), "/"), "/")
	if len(parts) != len(t.segs) {
		return nil, false
	}
	values := map[string]string{}
	for i, seg := range t.segs {
		if seg.placeholder == "" {
			if parts[i] != seg.literal {
				return nil, false
			}
			continue
		}
		values[seg.placeholder] = parts[i]
	}
	return values, true
}

// Render fills the template. Every placeholder the template uses needs a value; extra
// values are ignored, so callers can always pass the namespace.
func (t *Template) Render(trustDomain string, values map[string]string) (spiffeid.ID, error) {
	td, err := spiffeid.TrustDomainFromString(trustDomain)
	if err != nil {
		return spiffeid.ID{}, err
	}
	parts := make([]string, 0, len(t.segs))
	for _, seg := range t.segs {
		if seg.placeholder == "" {
			parts = append(parts, seg.literal)
			continue
		}
		v := values[seg.placeholder]
		if v == "" {
			return spiffeid.ID{}, fmt.Errorf("%s %q: no value for {%s}", t.kind, t.raw, seg.placeholder)
		}
		parts = append(parts, v)
	}
	return spiffeid.FromSegments(td, parts...)
}

// Overlaps reports whether some SPIFFE ID could match both templates.
func Overlaps(a, b *Template) bool {
	if len(a.segs) != len(b.segs) {
		return false
	}
	for i := range a.segs {
		x, y := a.segs[i], b.segs[i]
		if x.placeholder == "" && y.placeholder == "" && x.literal != y.literal {
			return false
		}
	}
	return true
}
