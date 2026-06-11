// Shared spec matchers used by both TraceFlow and ExplainMatch.
// Keeping them in one file makes the algorithm guarantees auditable in
// isolation; both diagnostics surface the same per-field reasons.
package flow

import (
	"fmt"
	"net/netip"
	"strconv"
	"strings"
)

// ipInPrefix reports whether ip is contained in one of the given CIDR
// prefixes. An empty/nil prefix list is treated as "match anything"
// (the dashd / DASH convention — an ACL rule with no src_prefixes
// matches every source IP).
//
// Returns the matching prefix string (when a list is non-empty) so the
// reason string can quote which prefix was selected.
func ipInPrefix(ip string, prefixes []string) (matched bool, hitPrefix string, reason string) {
	if len(prefixes) == 0 {
		return true, "", "any (empty prefix list)"
	}
	parsed, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return false, "", fmt.Sprintf("ip %q invalid: %v", ip, err)
	}
	for _, p := range prefixes {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		pf, err := netip.ParsePrefix(p)
		if err != nil {
			// A malformed prefix counts as no-match for that entry; we
			// surface it in the reason so operators see it.
			continue
		}
		if pf.Contains(parsed) {
			return true, p, fmt.Sprintf("%s in %s", parsed.String(), p)
		}
	}
	return false, "", fmt.Sprintf("%s not in any of [%s]", parsed.String(), strings.Join(prefixes, ", "))
}

// portMatches reports whether the candidate port falls into any of the
// rule's port specifiers. An empty/nil list matches anything.
//
// A specifier is either a bare number ("443") or a range ("1000-2000",
// inclusive on both ends). Negative widths or non-numeric tokens count
// as no-match and are recorded in the reason.
func portMatches(port uint32, specs []string) (matched bool, reason string) {
	if len(specs) == 0 {
		return true, "any (empty port list)"
	}
	for _, s := range specs {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if idx := strings.Index(s, "-"); idx >= 0 {
			lo, errLo := strconv.ParseUint(strings.TrimSpace(s[:idx]), 10, 32)
			hi, errHi := strconv.ParseUint(strings.TrimSpace(s[idx+1:]), 10, 32)
			if errLo != nil || errHi != nil || hi < lo {
				continue
			}
			if uint32(lo) <= port && port <= uint32(hi) {
				return true, fmt.Sprintf("%d in range %s", port, s)
			}
		} else {
			only, err := strconv.ParseUint(s, 10, 32)
			if err != nil {
				continue
			}
			if uint32(only) == port {
				return true, fmt.Sprintf("%d == %s", port, s)
			}
		}
	}
	return false, fmt.Sprintf("%d not in any of [%s]", port, strings.Join(specs, ", "))
}

// protoMatches reports whether proto matches one of the rule's protocol
// specifiers. Both string ("tcp"/"udp"/"icmp"/"icmpv6") and numeric
// ("6"/"17"/"1"/"58") forms are accepted; comparison is case-insensitive
// for the string form. An empty list matches anything.
func protoMatches(proto string, specs []string) (matched bool, reason string) {
	if len(specs) == 0 {
		return true, "any (empty protocol list)"
	}
	want := strings.TrimSpace(strings.ToLower(proto))
	wantNum := protoNameToNumber(want)
	for _, s := range specs {
		s = strings.TrimSpace(strings.ToLower(s))
		if s == "" {
			continue
		}
		if s == want {
			return true, fmt.Sprintf("%q matches %q", proto, s)
		}
		// Numeric ↔ string equivalence: try to normalize both sides
		// into numbers and compare.
		if num, ok := strconv.Atoi(s); ok == nil {
			if wantNum != 0 && uint32(num) == wantNum {
				return true, fmt.Sprintf("%q == protocol number %d", proto, num)
			}
		}
		if wantNum != 0 && protoNameToNumber(s) == wantNum {
			return true, fmt.Sprintf("%q ≡ %q (proto %d)", proto, s, wantNum)
		}
	}
	return false, fmt.Sprintf("%q not in any of [%s]", proto, strings.Join(specs, ", "))
}

// protoNameToNumber returns the IANA protocol number for the common
// dashd protocol names. Unknown names return 0.
func protoNameToNumber(name string) uint32 {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "icmp":
		return 1
	case "tcp":
		return 6
	case "udp":
		return 17
	case "icmpv6":
		return 58
	}
	if n, err := strconv.Atoi(name); err == nil && n > 0 {
		return uint32(n)
	}
	return 0
}
