// Package scenarios loads a YAML scenario into the model.Store. Each entry
// names a kind, a key (list of strings), and a value rendered as the upstream
// proto's JSON form (which protojson can decode).
//
// Example:
//
//   apiVersion: dashapi.dashcenter.io/v1
//   kind: Scenario
//   metadata:
//     name: small
//     device_id: dpu-sim-01
//   spec:
//     - kind: appliance
//       key: ["appliance-01"]
//       value:
//         sip: { ipv4: 100663360 }              # 6.0.0.0 in network byte order
//         vm_vni: 1000
//         local_region_id: 1
//     - kind: vnet
//       key: ["vnet-prod"]
//       value:
//         vni: 1001
//     - kind: eni
//       key: ["eni-001"]
//       value:
//         eni_id: "11111111-1111-1111-1111-111111111111"
//         mac_address: ""
//         vnet: "vnet-prod"
//         admin_state: STATE_ENABLED
//
// Numbers, enum strings, bytes (base64), and nested messages all follow
// protojson semantics.
package scenarios

import (
	"encoding/json"
	"fmt"
	"os"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
	"github.com/rashmirrout/DashCenter/src/impl-go/dash-sim/internal/sim/model"
	"google.golang.org/protobuf/encoding/protojson"
	"gopkg.in/yaml.v3"
)

// Document is the on-disk YAML shape.
type Document struct {
	APIVersion string                 `yaml:"apiVersion"`
	Kind       string                 `yaml:"kind"`
	Metadata   map[string]interface{} `yaml:"metadata"`
	Spec       []Entry                `yaml:"spec"`
}

// Entry is a single (kind, key, value) tuple.
type Entry struct {
	Kind  string      `yaml:"kind"`
	Key   []string    `yaml:"key"`
	Value interface{} `yaml:"value"`
}

// LoadFile reads, parses, and applies path.
func LoadFile(path string, store *model.Store) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("scenario: read %q: %w", path, err)
	}
	return LoadBytes(raw, store)
}

// LoadBytes parses raw YAML and applies the scenario.
func LoadBytes(raw []byte, store *model.Store) error {
	var doc Document
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("scenario: parse: %w", err)
	}
	return Apply(&doc, store)
}

// Apply pushes the parsed scenario into the store, in declaration order.
func Apply(doc *Document, store *model.Store) error {
	unmarshaler := protojson.UnmarshalOptions{DiscardUnknown: false}
	for i, entry := range doc.Spec {
		info, err := kinds.LookupByName(entry.Kind)
		if err != nil {
			return fmt.Errorf("scenario: entry[%d]: %w", i, err)
		}
		// Round-trip yaml value -> json -> protojson into a fresh message.
		var valJSON []byte
		if entry.Value == nil {
			valJSON = []byte("{}")
		} else {
			valJSON, err = json.Marshal(toJSONCompatible(entry.Value))
			if err != nil {
				return fmt.Errorf("scenario: entry[%d] kind=%s json: %w", i, entry.Kind, err)
			}
		}
		msg := info.NewZero()
		if err := unmarshaler.Unmarshal(valJSON, msg); err != nil {
			return fmt.Errorf("scenario: entry[%d] kind=%s decode: %w", i, entry.Kind, err)
		}
		obj, err := kinds.WrapObject(info.Kind, entry.Key, msg)
		if err != nil {
			return fmt.Errorf("scenario: entry[%d] kind=%s wrap: %w", i, entry.Kind, err)
		}
		if _, _, err := store.Apply(obj); err != nil {
			return fmt.Errorf("scenario: entry[%d] kind=%s key=%v apply: %w",
				i, entry.Kind, entry.Key, err)
		}
	}
	return nil
}

// toJSONCompatible recursively coerces yaml.v3-style map[interface{}]interface{}
// to map[string]interface{}, the only shape encoding/json accepts.
func toJSONCompatible(v interface{}) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, vv := range x {
			out[k] = toJSONCompatible(vv)
		}
		return out
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(x))
		for k, vv := range x {
			out[fmt.Sprintf("%v", k)] = toJSONCompatible(vv)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(x))
		for i, vv := range x {
			out[i] = toJSONCompatible(vv)
		}
		return out
	default:
		_ = dashapi.ObjectKind_OBJECT_KIND_UNSPECIFIED // keep dashapi import for forward use
		return x
	}
}
