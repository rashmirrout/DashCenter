// Package manifest defines the dashctl YAML/JSON envelope schema
// (apiVersion / kind / metadata / spec) and the kind registry that
// maps user-facing kind names to dashd's REST routes.
//
// The envelope shape is intentionally kubectl-aligned:
//
//	apiVersion: dashcenter.v1
//	kind: Eni
//	metadata:
//	  namespace: team-a
//	  name: eni-001
//	  generation: 7         # optional; sent as expected_generation (CAS)
//	  labels: { tier: prod }
//	spec:                   # exact dashd spec for this kind
//	  vnetName: vnet-prod
//	  macAddress: "00:..."
package manifest

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// APIVersion is the only currently supported envelope apiVersion.
const APIVersion = "dashcenter.v1"

// Envelope is the user-facing manifest shape.
type Envelope struct {
	APIVersion string         `yaml:"apiVersion" json:"apiVersion"`
	Kind       string         `yaml:"kind"       json:"kind"`
	Metadata   Metadata       `yaml:"metadata"   json:"metadata"`
	Spec       map[string]any `yaml:"spec"       json:"spec"`
}

// Metadata holds the addressing + CAS fields.
type Metadata struct {
	Namespace  string            `yaml:"namespace,omitempty"  json:"namespace,omitempty"`
	Name       string            `yaml:"name"                 json:"name"`
	Generation uint64            `yaml:"generation,omitempty" json:"generation,omitempty"`
	Labels     map[string]string `yaml:"labels,omitempty"     json:"labels,omitempty"`
}

// KindInfo describes one supported spec kind.
type KindInfo struct {
	// Canonical capitalised kind name as it appears in YAML envelopes,
	// e.g. "Vnet", "Eni", "VnetMapping", "AclPolicy".
	Kind string
	// Lowercase singular used internally by dashd ("vnet", "eni",
	// "vnet_mapping", "acl_policy", "route_policy", "ha_set",
	// "service_tunnel"). Matches dashd's store kind names.
	StoreKind string
	// Plural URL segment for dashd's REST routes ("vnets", "enis",
	// "vnet-mappings", "acl-policies", "route-policies", "ha-sets",
	// "service-tunnels"). Matches src/impl-go/dashd/internal/server/rest/server.go.
	URLPlural string
	// Lower-case aliases accepted from the CLI for convenience.
	Aliases []string
	// Phase the spec kind is supported in (1 = today; 2 = post dashd Phase 2).
	Phase int
}

// Built-in kind registry (Phase 1 supports all kinds dashd ships today).
var registry = map[string]KindInfo{
	"Vnet":          {Kind: "Vnet", StoreKind: "vnet", URLPlural: "vnets", Aliases: []string{"vnet", "vn", "vnets"}, Phase: 1},
	"Eni":           {Kind: "Eni", StoreKind: "eni", URLPlural: "enis", Aliases: []string{"eni", "enis"}, Phase: 1},
	"VnetMapping":   {Kind: "VnetMapping", StoreKind: "vnet_mapping", URLPlural: "vnet-mappings", Aliases: []string{"vnetmapping", "vnet-mapping", "mapping", "mappings"}, Phase: 1},
	"AclPolicy":     {Kind: "AclPolicy", StoreKind: "acl_policy", URLPlural: "acl-policies", Aliases: []string{"acl", "acls", "aclpolicy", "acl-policy"}, Phase: 1},
	"RoutePolicy":   {Kind: "RoutePolicy", StoreKind: "route_policy", URLPlural: "route-policies", Aliases: []string{"route", "routes", "routepolicy", "route-policy"}, Phase: 1},
	"HaSet":         {Kind: "HaSet", StoreKind: "ha_set", URLPlural: "ha-sets", Aliases: []string{"ha", "haset", "ha-set"}, Phase: 1},
	"ServiceTunnel": {Kind: "ServiceTunnel", StoreKind: "service_tunnel", URLPlural: "service-tunnels", Aliases: []string{"st", "tunnel", "service-tunnel"}, Phase: 2},
	"Inventory":     {Kind: "Inventory", StoreKind: "inventory", URLPlural: "inventory", Aliases: []string{"inventory", "inv"}, Phase: 1},
}

// LookupKind resolves a CLI-supplied kind string (any case, alias, plural)
// into a KindInfo. Returns false if unknown.
func LookupKind(s string) (KindInfo, bool) {
	if s == "" {
		return KindInfo{}, false
	}
	// Exact match (canonical capitalised).
	if k, ok := registry[s]; ok {
		return k, true
	}
	lower := strings.ToLower(s)
	for _, k := range registry {
		if strings.ToLower(k.Kind) == lower || strings.ToLower(k.StoreKind) == lower || strings.ToLower(k.URLPlural) == lower {
			return k, true
		}
		for _, a := range k.Aliases {
			if strings.ToLower(a) == lower {
				return k, true
			}
		}
	}
	return KindInfo{}, false
}

// Kinds returns the registry as a stable, sorted slice. Used by completion
// and `dashctl explain`.
func Kinds() []KindInfo {
	out := make([]KindInfo, 0, len(registry))
	for _, k := range registry {
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Kind < out[j].Kind })
	return out
}

// Parse decodes a single YAML / JSON document into an Envelope. The
// document MUST carry apiVersion=dashcenter.v1 and a kind known to the
// registry; otherwise a descriptive error is returned.
func Parse(doc []byte) (*Envelope, error) {
	env := &Envelope{}
	if err := yaml.Unmarshal(doc, env); err != nil {
		return nil, fmt.Errorf("manifest: parse: %w", err)
	}
	return Validate(env)
}

// Validate enforces the envelope contract on an already-parsed Envelope.
// It is safe to call on an in-memory Envelope (e.g. one built by `edit`).
func Validate(env *Envelope) (*Envelope, error) {
	if env == nil {
		return nil, fmt.Errorf("manifest: nil envelope")
	}
	if env.APIVersion == "" {
		return nil, fmt.Errorf("manifest: apiVersion is required (want %q)", APIVersion)
	}
	if env.APIVersion != APIVersion {
		return nil, fmt.Errorf("manifest: unsupported apiVersion %q (want %q)", env.APIVersion, APIVersion)
	}
	if env.Kind == "" {
		return nil, fmt.Errorf("manifest: kind is required")
	}
	ki, ok := LookupKind(env.Kind)
	if !ok {
		return nil, fmt.Errorf("manifest: unknown kind %q", env.Kind)
	}
	env.Kind = ki.Kind // canonicalise
	if ki.Kind != "Inventory" {
		if env.Metadata.Name == "" {
			return nil, fmt.Errorf("manifest: metadata.name is required for kind %s", ki.Kind)
		}
	}
	if env.Spec == nil {
		env.Spec = map[string]any{}
	}
	return env, nil
}

// ParseMulti splits a YAML byte stream on `---` boundaries and returns
// every parsed envelope in document order. Empty / comment-only documents
// are skipped. Errors include the doc index for ergonomic diagnostics.
func ParseMulti(data []byte) ([]*Envelope, error) {
	dec := yaml.NewDecoder(strings.NewReader(string(data)))
	var out []*Envelope
	idx := 0
	for {
		var raw any
		if err := dec.Decode(&raw); err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, fmt.Errorf("manifest: doc %d: %w", idx, err)
		}
		idx++
		if raw == nil {
			continue
		}
		// Re-marshal to bytes so Parse() can run YAML→Envelope+validation uniformly.
		bs, err := yaml.Marshal(raw)
		if err != nil {
			return nil, fmt.Errorf("manifest: doc %d: re-marshal: %w", idx, err)
		}
		env, err := Parse(bs)
		if err != nil {
			return nil, fmt.Errorf("manifest: doc %d: %w", idx, err)
		}
		out = append(out, env)
	}
	return out, nil
}

// SpecJSON returns the spec as a JSON byte slice suitable for HTTP PUT.
// Stable key ordering is produced by encoding/json (Go map iteration is
// randomised; json.Marshal sorts string keys).
func (e *Envelope) SpecJSON() ([]byte, error) {
	if e == nil {
		return nil, fmt.Errorf("manifest: nil envelope")
	}
	// Inject metadata.name into spec.name when the spec omits it. Also
	// project expected_generation from metadata.generation when set.
	spec := map[string]any{}
	for k, v := range e.Spec {
		spec[k] = v
	}
	if _, ok := spec["name"]; !ok && e.Metadata.Name != "" {
		spec["name"] = e.Metadata.Name
	}
	if _, ok := spec["namespace"]; !ok && e.Metadata.Namespace != "" {
		spec["namespace"] = e.Metadata.Namespace
	}
	if e.Metadata.Generation > 0 {
		if _, ok := spec["expected_generation"]; !ok {
			spec["expected_generation"] = e.Metadata.Generation
		}
	}
	if len(e.Metadata.Labels) > 0 {
		if _, ok := spec["labels"]; !ok {
			spec["labels"] = e.Metadata.Labels
		}
	}
	return json.Marshal(spec)
}

// EnvelopeFromStoredItem builds an Envelope from a dashd `StoredItem`
// response (used by Get/List rendering and `dashctl edit`).
//
// kind, name, namespace are the typed addressing fields; generation is the
// dashd-assigned current generation; specJSON is the raw spec body
// returned by dashd. labels is nil if not separately known.
func EnvelopeFromStoredItem(kind, namespace, name string, generation uint64, specJSON []byte, labels map[string]string) (*Envelope, error) {
	ki, ok := LookupKind(kind)
	if !ok {
		return nil, fmt.Errorf("manifest: unknown kind %q", kind)
	}
	spec := map[string]any{}
	if len(specJSON) > 0 {
		if err := json.Unmarshal(specJSON, &spec); err != nil {
			return nil, fmt.Errorf("manifest: parse spec: %w", err)
		}
	}
	// Strip fields we project from metadata to avoid duplication on round-trip.
	delete(spec, "name")
	delete(spec, "namespace")
	delete(spec, "expected_generation")
	if labels == nil {
		if l, ok := spec["labels"]; ok {
			if m, ok := l.(map[string]any); ok {
				lbls := make(map[string]string, len(m))
				for k, v := range m {
					if s, ok := v.(string); ok {
						lbls[k] = s
					}
				}
				labels = lbls
			}
		}
	}
	delete(spec, "labels")
	return &Envelope{
		APIVersion: APIVersion,
		Kind:       ki.Kind,
		Metadata:   Metadata{Namespace: namespace, Name: name, Generation: generation, Labels: labels},
		Spec:       spec,
	}, nil
}

// Marshal returns the envelope as YAML in canonical key order.
func (e *Envelope) Marshal() ([]byte, error) {
	return yaml.Marshal(e)
}

// MarshalJSON returns the envelope as indented JSON.
func (e *Envelope) MarshalJSON() ([]byte, error) {
	return json.MarshalIndent(struct {
		APIVersion string         `json:"apiVersion"`
		Kind       string         `json:"kind"`
		Metadata   Metadata       `json:"metadata"`
		Spec       map[string]any `json:"spec"`
	}{e.APIVersion, e.Kind, e.Metadata, e.Spec}, "", "  ")
}
