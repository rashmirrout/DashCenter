package rest

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/rashmirrout/DashCenter/src/impl-go/dashctl/pkg/client"
)

// Selector is a parsed Kubernetes-style label selector (subset).
//
// Grammar supported in Phase 1:
//
//	expr   := term ("," term)*
//	term   := key "=" value
//	       |  key "==" value
//	       |  key "!=" value
//	       |  "!" key                 (key must NOT be set)
//	       |  key                     (key must be set, any value)
//
// Composite predicates and set-based operators (`in`, `notin`) are
// deferred to Phase 2.
type Selector struct {
	preds []predicate
}

type predicate struct {
	key   string
	op    string // "=", "!=", "exists", "notexists"
	value string
}

// ParseSelector parses a comma-separated selector expression.
func ParseSelector(expr string) (*Selector, error) {
	s := &Selector{}
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return s, nil
	}
	for _, raw := range strings.Split(expr, ",") {
		term := strings.TrimSpace(raw)
		if term == "" {
			continue
		}
		p, err := parseTerm(term)
		if err != nil {
			return nil, err
		}
		s.preds = append(s.preds, p)
	}
	return s, nil
}

func parseTerm(t string) (predicate, error) {
	// Note: order matters. Check the two-char operators (!=, ==) before
	// the single-char ones (!, =), otherwise "!=v" would be misparsed as
	// "! followed by =v" (an exists-not with bogus key).
	if i := strings.Index(t, "!="); i >= 0 {
		k := strings.TrimSpace(t[:i])
		v := strings.TrimSpace(t[i+2:])
		if k == "" {
			return predicate{}, fmt.Errorf("selector: empty key in %q", t)
		}
		return predicate{key: k, op: "!=", value: v}, nil
	}
	if i := strings.Index(t, "=="); i >= 0 {
		k := strings.TrimSpace(t[:i])
		v := strings.TrimSpace(t[i+2:])
		if k == "" {
			return predicate{}, fmt.Errorf("selector: empty key in %q", t)
		}
		return predicate{key: k, op: "=", value: v}, nil
	}
	if strings.HasPrefix(t, "!") {
		k := strings.TrimSpace(t[1:])
		if k == "" {
			return predicate{}, fmt.Errorf("selector: dangling '!'")
		}
		return predicate{key: k, op: "notexists"}, nil
	}
	if i := strings.Index(t, "="); i >= 0 {
		k := strings.TrimSpace(t[:i])
		v := strings.TrimSpace(t[i+1:])
		if k == "" {
			return predicate{}, fmt.Errorf("selector: empty key in %q", t)
		}
		return predicate{key: k, op: "=", value: v}, nil
	}
	// Bare key → "exists".
	if t == "" {
		return predicate{}, fmt.Errorf("selector: empty term")
	}
	return predicate{key: t, op: "exists"}, nil
}

// Matches returns true if the labels satisfy every predicate.
func (s *Selector) Matches(labels map[string]string) bool {
	if s == nil || len(s.preds) == 0 {
		return true
	}
	for _, p := range s.preds {
		v, ok := labels[p.key]
		switch p.op {
		case "=":
			if !ok || v != p.value {
				return false
			}
		case "!=":
			if ok && v == p.value {
				return false
			}
		case "exists":
			if !ok {
				return false
			}
		case "notexists":
			if ok {
				return false
			}
		}
	}
	return true
}

// filterBySelector returns the StoredItems whose spec.labels match sel.
// Labels are read out of the StoredItem.Spec JSON envelope.
func filterBySelector(items []*client.StoredItem, sel *Selector) []*client.StoredItem {
	if sel == nil {
		return items
	}
	out := items[:0:cap(items)]
	for _, it := range items {
		if sel.Matches(extractLabels(it)) {
			out = append(out, it)
		}
	}
	return out
}

// extractLabels parses spec.labels (a JSON object of string→string)
// out of a StoredItem's spec body. Returns empty map on parse failure.
func extractLabels(it *client.StoredItem) map[string]string {
	if it == nil || len(it.Spec) == 0 {
		return nil
	}
	var raw struct {
		Labels map[string]string `json:"labels"`
	}
	_ = json.Unmarshal(it.Spec, &raw)
	return raw.Labels
}
