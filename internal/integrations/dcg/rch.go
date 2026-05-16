package dcg

import "strings"

var rchWhitelistPatterns = []string{
	"rch exec *",
	"rch hook *",
	"rch status *",
	"rch check *",
	"rch doctor *",
}

// RCHWhitelistPatterns returns the default DCG whitelist patterns for RCH commands.
func RCHWhitelistPatterns() []string {
	out := make([]string, len(rchWhitelistPatterns))
	copy(out, rchWhitelistPatterns)
	return out
}

// AppendRCHWhitelist merges the default RCH whitelist patterns into the provided list.
// It trims whitespace and deduplicates entries while preserving order.
func AppendRCHWhitelist(base []string) []string {
	return mergeWhitelist(base, rchWhitelistPatterns)
}

func mergeWhitelist(base, extra []string) []string {
	seen := make(map[string]struct{}, len(base)+len(extra))
	merged := make([]string, 0, len(base)+len(extra))
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, ok := seen[value]; ok {
			return
		}
		seen[value] = struct{}{}
		merged = append(merged, value)
	}
	for _, value := range base {
		add(value)
	}
	for _, value := range extra {
		add(value)
	}
	return merged
}
