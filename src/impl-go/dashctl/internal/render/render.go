// Package render implements dashctl's output formatters: json, yaml,
// table, wide, name, jsonpath, and template.
//
// The package is intentionally pure: no logging, no clock, no I/O other
// than to the io.Writer passed in. Every renderer is a single function
// that takes (writer, value, options) and produces deterministic bytes.
package render

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"text/template"

	"golang.org/x/term"
	"gopkg.in/yaml.v3"
)

// Format is the user-facing -o choice.
type Format string

const (
	FormatTable    Format = "table"
	FormatWide     Format = "wide"
	FormatName     Format = "name"
	FormatJSON     Format = "json"
	FormatYAML     Format = "yaml"
	FormatJSONPath Format = "jsonpath"
	FormatTemplate Format = "template"
)

// ParseFormat normalises a user-supplied -o string. Supports
// "jsonpath=..." and "template=..." with embedded expression.
func ParseFormat(s string) (Format, string, error) {
	if s == "" {
		return "", "", nil
	}
	if strings.HasPrefix(s, "jsonpath=") {
		return FormatJSONPath, strings.TrimPrefix(s, "jsonpath="), nil
	}
	if strings.HasPrefix(s, "template=") {
		return FormatTemplate, strings.TrimPrefix(s, "template="), nil
	}
	switch Format(s) {
	case FormatTable, FormatWide, FormatName, FormatJSON, FormatYAML, FormatJSONPath, FormatTemplate:
		return Format(s), "", nil
	}
	return "", "", fmt.Errorf("render: unknown output format %q", s)
}

// DefaultFor returns "table" if w is a TTY, else "json".
func DefaultFor(w io.Writer) Format {
	if isTTY(w) {
		return FormatTable
	}
	return FormatJSON
}

func isTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}

// Options tunes formatters. Wide implies all columns; Color is one of
// auto/always/never. NoHeader suppresses the table header line.
type Options struct {
	Format     Format
	Expression string // for jsonpath / template
	Wide       bool
	Color      string
	NoHeader   bool
	Columns    []Column // optional override; if empty the renderer picks from a registered column set
}

// Column describes one table column.
type Column struct {
	Header string
	// Get returns the string for this row given the row value.
	Get func(any) string
	// Wide=true → only shown in -o wide.
	Wide bool
}

// Render writes value in the chosen format to w.
func Render(w io.Writer, value any, opts Options) error {
	switch opts.Format {
	case "":
		opts.Format = DefaultFor(w)
		return Render(w, value, opts)
	case FormatJSON:
		return renderJSON(w, value)
	case FormatYAML:
		return renderYAML(w, value)
	case FormatName:
		return renderName(w, value)
	case FormatJSONPath:
		return renderJSONPath(w, value, opts.Expression)
	case FormatTemplate:
		return renderTemplate(w, value, opts.Expression)
	case FormatTable, FormatWide:
		return renderTable(w, value, opts)
	}
	return fmt.Errorf("render: unsupported format %q", opts.Format)
}

func renderJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func renderYAML(w io.Writer, v any) error {
	// Round-trip through JSON to get stable key order matching renderJSON.
	bs, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var via any
	if err := yaml.Unmarshal(bs, &via); err != nil {
		return err
	}
	enc := yaml.NewEncoder(w)
	enc.SetIndent(2)
	if err := enc.Encode(via); err != nil {
		return err
	}
	return enc.Close()
}

// Namer is implemented by values that know how to print themselves in
// `name` mode (e.g. "vnet/v1").
type Namer interface {
	Name() string
}

func renderName(w io.Writer, v any) error {
	switch x := v.(type) {
	case Namer:
		_, err := fmt.Fprintln(w, x.Name())
		return err
	case []Namer:
		for _, n := range x {
			if _, err := fmt.Fprintln(w, n.Name()); err != nil {
				return err
			}
		}
		return nil
	case []any:
		for _, e := range x {
			if n, ok := e.(Namer); ok {
				if _, err := fmt.Fprintln(w, n.Name()); err != nil {
					return err
				}
			}
		}
		return nil
	}
	// Fallback: try reflective slice-of-Namer.
	if names, ok := tryNameSlice(v); ok {
		for _, s := range names {
			if _, err := fmt.Fprintln(w, s); err != nil {
				return err
			}
		}
		return nil
	}
	return fmt.Errorf("render: -o name requires a Namer or []Namer")
}

// renderTable writes a tabwriter table. Columns come from opts.Columns;
// if empty, a generic key=value fallback is used (works on []map[string]any).
func renderTable(w io.Writer, v any, opts Options) error {
	cols := opts.Columns
	wide := opts.Format == FormatWide || opts.Wide
	if !wide {
		filtered := cols[:0:cap(cols)]
		for _, c := range cols {
			if c.Wide {
				continue
			}
			filtered = append(filtered, c)
		}
		cols = filtered
	}

	tw := tabwriter.NewWriter(w, 0, 4, 3, ' ', 0)
	defer tw.Flush()

	if !opts.NoHeader && len(cols) > 0 {
		headers := make([]string, len(cols))
		for i, c := range cols {
			headers[i] = c.Header
		}
		if _, err := fmt.Fprintln(tw, strings.Join(headers, "\t")); err != nil {
			return err
		}
	}

	rows := asSlice(v)
	if len(cols) == 0 {
		// Fallback: dump JSON one per line.
		for _, row := range rows {
			bs, _ := json.Marshal(row)
			if _, err := fmt.Fprintln(tw, string(bs)); err != nil {
				return err
			}
		}
		return nil
	}
	for _, row := range rows {
		cells := make([]string, len(cols))
		for i, c := range cols {
			cells[i] = c.Get(row)
		}
		if _, err := fmt.Fprintln(tw, strings.Join(cells, "\t")); err != nil {
			return err
		}
	}
	return nil
}

// asSlice normalises v into a []any. Single objects become a 1-row slice.
func asSlice(v any) []any {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case []any:
		return x
	}
	// Slice-of-pointer common case: convert via reflection-free fast path.
	if s, ok := tryAnySlice(v); ok {
		return s
	}
	return []any{v}
}

// renderJSONPath evaluates a minimal dotted path expression of the form
// "{.foo.bar.baz}" (kubectl-compatible subset). Slice indexing and filters
// are not supported in Phase 1; numeric path segments select array
// elements (e.g. "{.items.0.spec.vni}").
func renderJSONPath(w io.Writer, v any, expr string) error {
	expr = strings.TrimSpace(expr)
	expr = strings.TrimPrefix(expr, "{")
	expr = strings.TrimSuffix(expr, "}")
	expr = strings.TrimSpace(expr)
	expr = strings.TrimPrefix(expr, ".")
	parts := []string{}
	if expr != "" {
		parts = strings.Split(expr, ".")
	}
	// Walk through a JSON-shaped representation (round-trip via json to
	// avoid reflection on user types).
	bs, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var cursor any
	if err := json.Unmarshal(bs, &cursor); err != nil {
		return err
	}
	for _, p := range parts {
		cursor, err = stepJSONPath(cursor, p)
		if err != nil {
			return err
		}
	}
	switch x := cursor.(type) {
	case string:
		_, err = fmt.Fprintln(w, x)
	case float64:
		_, err = fmt.Fprintln(w, formatNumber(x))
	case nil:
		_, err = fmt.Fprintln(w)
	default:
		out, _ := json.Marshal(cursor)
		_, err = fmt.Fprintln(w, string(out))
	}
	return err
}

func stepJSONPath(cursor any, p string) (any, error) {
	if cursor == nil {
		return nil, fmt.Errorf("jsonpath: nil at %q", p)
	}
	switch v := cursor.(type) {
	case map[string]any:
		c, ok := v[p]
		if !ok {
			return nil, fmt.Errorf("jsonpath: key %q not found", p)
		}
		return c, nil
	case []any:
		idx, err := parseIndex(p, len(v))
		if err != nil {
			return nil, err
		}
		return v[idx], nil
	default:
		return nil, fmt.Errorf("jsonpath: cannot descend into %T at %q", cursor, p)
	}
}

func parseIndex(p string, length int) (int, error) {
	n := 0
	for _, r := range p {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("jsonpath: %q is not a numeric index", p)
		}
		n = n*10 + int(r-'0')
	}
	if n < 0 || n >= length {
		return 0, fmt.Errorf("jsonpath: index %d out of range [0,%d)", n, length)
	}
	return n, nil
}

func formatNumber(f float64) string {
	// Integer-friendly formatting: drop the .0 when value is exact.
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}

// renderTemplate evaluates a Go text/template against v.
func renderTemplate(w io.Writer, v any, expr string) error {
	t, err := template.New("dashctl").Parse(expr)
	if err != nil {
		return fmt.Errorf("template: parse: %w", err)
	}
	// Round-trip to map so template field accessors work regardless of
	// the caller passing a typed struct or a generic map.
	bs, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var via any
	if err := json.Unmarshal(bs, &via); err != nil {
		return err
	}
	return t.Execute(w, via)
}

// tryAnySlice and tryNameSlice avoid hard-coding reflect for the most
// common cases (slices of pointers).
func tryAnySlice(v any) ([]any, bool) {
	switch x := v.(type) {
	case []map[string]any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = e
		}
		return out, true
	}
	return nil, false
}

func tryNameSlice(v any) ([]string, bool) {
	_, _ = json.Marshal(v) // smoke
	switch x := v.(type) {
	case []map[string]any:
		out := make([]string, 0, len(x))
		for _, m := range x {
			out = append(out, fmt.Sprintf("%v/%v", m["kind"], m["name"]))
		}
		return out, true
	}
	return nil, false
}
