// Package render formats proto messages for human consumption. Supports JSON
// (via protojson, which honors snake_case field names + enum names), YAML
// (translation of the JSON), and table (a tabwriter line per item).
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

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

var jsonMarshal = protojson.MarshalOptions{
	Multiline:     true,
	Indent:        "  ",
	UseProtoNames: true,
	EmitUnpopulated: true,
}

// One renders a single proto.Message.
func One[T proto.Message](w io.Writer, format Format, m T, tableCols []string, tableRow func(T) []string) error {
	switch format {
	case FormatJSON:
		b, err := jsonMarshal.Marshal(m)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(b))
		return err
	case FormatYAML:
		return writeYAML(w, m)
	case FormatTable:
		return writeTable(w, tableCols, [][]string{tableRow(m)})
	}
	return fmt.Errorf("unsupported format %q", format)
}

// Many renders a slice of proto.Message values of the same concrete type.
func Many[T proto.Message](w io.Writer, format Format, items []T, tableCols []string, tableRow func(T) []string) error {
	switch format {
	case FormatJSON:
		// emit a JSON array
		_, _ = fmt.Fprintln(w, "[")
		for i, it := range items {
			b, err := jsonMarshal.Marshal(it)
			if err != nil {
				return err
			}
			suffix := ","
			if i == len(items)-1 {
				suffix = ""
			}
			_, _ = fmt.Fprintf(w, "  %s%s\n", string(b), suffix)
		}
		_, err := fmt.Fprintln(w, "]")
		return err
	case FormatYAML:
		for _, it := range items {
			_, _ = fmt.Fprintln(w, "---")
			if err := writeYAML(w, it); err != nil {
				return err
			}
		}
		return nil
	case FormatTable:
		rows := make([][]string, 0, len(items))
		for _, it := range items {
			rows = append(rows, tableRow(it))
		}
		return writeTable(w, tableCols, rows)
	}
	return fmt.Errorf("unsupported format %q", format)
}

// Map renders an arbitrary map[string]int64 (used for counters).
func Map(w io.Writer, format Format, m map[string]int64) error {
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

func writeYAML(w io.Writer, m proto.Message) error {
	b, err := jsonMarshal.Marshal(m)
	if err != nil {
		return err
	}
	var v interface{}
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	out, err := yaml.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(w, string(out))
	return err
}

func writeTable(w io.Writer, cols []string, rows [][]string) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(tw, strings.Join(cols, "\t"))
	for _, r := range rows {
		_, _ = fmt.Fprintln(tw, strings.Join(r, "\t"))
	}
	return tw.Flush()
}
