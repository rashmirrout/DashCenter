package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
	"google.golang.org/protobuf/encoding/protojson"
	"gopkg.in/yaml.v3"
)

// parseKindArg resolves either a short name ("vnet_mapping") or the full enum
// name ("OBJECT_KIND_VNET_MAPPING") to an ObjectKind.
func parseKindArg(s string) (dashapi.ObjectKind, error) {
	if s == "" {
		return 0, fmt.Errorf("--kind is required")
	}
	if info, err := kinds.LookupByName(strings.ToLower(s)); err == nil {
		return info.Kind, nil
	}
	upper := strings.ToUpper(s)
	if v, ok := dashapi.ObjectKind_value[upper]; ok {
		return dashapi.ObjectKind(v), nil
	}
	if v, ok := dashapi.ObjectKind_value["OBJECT_KIND_"+upper]; ok {
		return dashapi.ObjectKind(v), nil
	}
	return 0, fmt.Errorf("unknown --kind %q (run `dash-sim-client kinds` for the list)", s)
}

func parseKeyArg(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, ":")
}

// readDocument loads --file as JSON or YAML. Returns one or more {kind, key,
// value} documents. The file may contain a single doc or a YAML stream.
type docEntry struct {
	Kind  string      `yaml:"kind" json:"kind"`
	Key   []string    `yaml:"key"  json:"key"`
	Value interface{} `yaml:"value" json:"value"`
}

func readDocuments(path string) ([]docEntry, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	trimmed := strings.TrimSpace(string(raw))
	// Try JSON array.
	if strings.HasPrefix(trimmed, "[") {
		var docs []docEntry
		if err := json.Unmarshal(raw, &docs); err == nil {
			return docs, nil
		}
	}
	// Try JSON single object.
	if strings.HasPrefix(trimmed, "{") {
		var d docEntry
		if err := json.Unmarshal(raw, &d); err == nil {
			return []docEntry{d}, nil
		}
	}
	// YAML stream — may have multiple docs.
	dec := yaml.NewDecoder(strings.NewReader(string(raw)))
	var docs []docEntry
	for {
		var d docEntry
		if err := dec.Decode(&d); err != nil {
			if err.Error() == "EOF" {
				break
			}
			return nil, err
		}
		if d.Kind == "" {
			continue
		}
		docs = append(docs, d)
	}
	return docs, nil
}

// docToObject turns a docEntry into a fully formed *dashapi.Object.
func docToObject(d docEntry) (*dashapi.Object, error) {
	info, err := kinds.LookupByName(strings.ToLower(d.Kind))
	if err != nil {
		return nil, err
	}
	if len(d.Key) != len(info.KeyParts) {
		return nil, fmt.Errorf("kind %s expects %d key parts %v, got %d",
			info.Name, len(info.KeyParts), info.KeyParts, len(d.Key))
	}

	msg := info.NewZero()
	if d.Value != nil {
		valJSON, err := json.Marshal(toJSONCompatible(d.Value))
		if err != nil {
			return nil, fmt.Errorf("kind %s value: %w", info.Name, err)
		}
		if err := (protojson.UnmarshalOptions{DiscardUnknown: false}).Unmarshal(valJSON, msg); err != nil {
			return nil, fmt.Errorf("kind %s decode value: %w", info.Name, err)
		}
	}
	return kinds.WrapObject(info.Kind, d.Key, msg)
}

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
		return x
	}
}

func printAck(ack *dashapi.Ack) error {
	if ack == nil {
		return fmt.Errorf("nil ack")
	}
	status := "OK"
	if !ack.GetAccepted() {
		status = "REJECTED"
	}
	if ack.GetError() != "" {
		fmt.Printf("%s txn=%s error=%s ts=%d\n", status, ack.GetTxnId(), ack.GetError(), ack.GetServerTsNs())
		if !ack.GetAccepted() {
			return fmt.Errorf("server rejected: %s", ack.GetError())
		}
	} else {
		fmt.Printf("%s txn=%s ts=%d\n", status, ack.GetTxnId(), ack.GetServerTsNs())
	}
	return nil
}
