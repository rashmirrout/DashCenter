// json_helpers.go — tiny isolated JSON helpers so counters.go doesn't
// import "encoding/json" (keeps the import surface minimal + grep-able).
package grpcclient

import "encoding/json"

func jsonUnmarshalCounterEvent(data []byte, v any) error {
	return json.Unmarshal(data, v)
}
