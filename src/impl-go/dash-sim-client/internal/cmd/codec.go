package cmd

import (
	"encoding/json"
	"strings"

	"gopkg.in/yaml.v3"
)

func trimSpace(s string) string  { return strings.TrimSpace(s) }
func jsonUnmarshal(b []byte, v *interface{}) error { return json.Unmarshal(b, v) }
func yamlUnmarshal(b []byte, v *interface{}) error { return yaml.Unmarshal(b, v) }
