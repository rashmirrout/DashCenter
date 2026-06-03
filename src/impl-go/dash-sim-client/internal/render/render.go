// Package render formats Objects + maps for human consumption.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	dashapi "github.com/rashmirrout/DashCenter/src/impl-go/gen/go/dashapi/v1"
	"github.com/rashmirrout/DashCenter/src/impl-go/dashapi-runtime/kinds"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"gopkg.in/yaml.v3"
)

// Format is one of "json", "yaml", "table".
type Format string

const (
	FormatJSON  Format = "json"
	FormatYAML  Format = "yaml"
	FormatTable Format = "table"
)

// ParseFormat returns a normalized Format or an error.
func ParseFormat(s string) (Format, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "json", "":
		return FormatJSON, nil
	case "yaml", "yml":
		return FormatYAML, nil
	case "table", "tab":
		return FormatTable, nil
	}
	return "", fmt.Errorf("unknown output format %q (want json|yaml|table)", s)
}

var prettyJSON = protojson.MarshalOptions{
	Multiline: true, Indent: "  ", UseProtoNames: true, EmitUnpopulated: false,
}
var tightJSON = protojson.MarshalOptions{
	UseProtoNames: true, EmitUnpopulated: false,
}

// Object renders one *dashapi.Object.
func Object(w io.Writer, format Format, obj *dashapi.Object) error {
	if obj == nil {
		_, err := fmt.Fprintln(w, "(nil)")
		return err
	}
	switch format {
	case FormatJSON:
		b, err := json.MarshalIndent(envelope(obj), "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(b))
		return err
	case FormatYAML:
		b, err := yaml.Marshal(envelope(obj))
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(w, string(b))
		return err
	case FormatTable:
		return writeTable(w, []string{"KIND", "KEY", "VALUE"}, [][]string{rowFor(obj)})
	}
	return fmt.Errorf("unsupported format %q", format)
}

// Objects renders many Objects.
func Objects(w io.Writer, format Format, objs []*dashapi.Object) error {
	switch format {
	case FormatJSON:
		out := make([]map[string]interface{}, 0, len(objs))
		for _, obj := range objs {
			out = append(out, envelope(obj))
		}
		b, err := json.MarshalIndent(out, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(b))
		return err
	case FormatYAML:
		for _, obj := range objs {
			_, _ = fmt.Fprintln(w, "---")
			b, err := yaml.Marshal(envelope(obj))
			if err != nil {
				return err
			}
			_, _ = fmt.Fprint(w, string(b))
		}
		return nil
	case FormatTable:
		rows := make([][]string, 0, len(objs))
		for _, obj := range objs {
			rows = append(rows, rowFor(obj))
		}
		return writeTable(w, []string{"KIND", "KEY", "VALUE"}, rows)
	}
	return fmt.Errorf("unsupported format %q", format)
}

// CountersMap renders a counter snapshot.
func CountersMap(w io.Writer, format Format, m map[string]int64) error {
	switch format {
	case FormatJSON:
		b, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(b))
		return err
	case FormatYAML:
		b, err := yaml.Marshal(m)
		if err != nil {
			return err
		}
		_, err = fmt.Fprint(w, string(b))
		return err
	case FormatTable:
		rows := make([][]string, 0, len(m))
		for k, v := range m {
			rows = append(rows, []string{k, fmt.Sprintf("%d", v)})
		}
		return writeTable(w, []string{"COUNTER", "VALUE"}, rows)
	}
	return fmt.Errorf("unsupported format %q", format)
}

func envelope(obj *dashapi.Object) map[string]interface{} {
	out := map[string]interface{}{"kind": "", "key": obj.GetKey()}
	if info, err := kinds.Lookup(obj.GetKind()); err == nil {
		out["kind"] = info.Name
	}
	if payload, err := kinds.PayloadOf(obj); err == nil && payload != nil {
		out["value"] = jsonValue(payload, prettyJSON)
	} else {
		out["value"] = map[string]interface{}{}
	}
	return out
}

func jsonValue(m proto.Message, opts protojson.MarshalOptions) interface{} {
	raw, err := opts.Marshal(m)
	if err != nil {
		return fmt.Sprintf("(err: %v)", err)
	}
	var v interface{}
	_ = json.Unmarshal(raw, &v)
	return v
}

func rowFor(obj *dashapi.Object) []string {
	info, _ := kinds.Lookup(obj.GetKind())
	tight := ""
	if payload, err := kinds.PayloadOf(obj); err == nil && payload != nil {
		raw, _ := tightJSON.Marshal(payload)
		tight = string(raw)
	}
	return []string{info.Name, strings.Join(obj.GetKey(), ":"), tight}
}

func writeTable(w io.Writer, cols []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, strings.Join(cols, "\t"))
	for _, r := range rows {
		_, _ = fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	return tw.Flush()
}
