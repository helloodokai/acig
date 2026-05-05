package verdict

import (
	"strings"
)

func DedupeFindings(findings []Finding) []Finding {
	seen := make(map[string]bool, len(findings))
	result := make([]Finding, 0, len(findings))
	for _, f := range findings {
		key := dedupeKey(f)
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, f)
	}
	return result
}

func dedupeKey(f Finding) string {
	file := f.File
	if file == "" {
		file = "*"
	}
	normalizedTitle := strings.ToLower(strings.TrimSpace(f.Title))
	normalizedDetail := strings.ToLower(strings.TrimSpace(f.Detail))
	if len(normalizedDetail) > 80 {
		normalizedDetail = normalizedDetail[:80]
	}
	return string(f.Severity) + "|" + file + "|" + normalizedTitle + "|" + normalizedDetail
}

func WorstSeverity(a, b Severity) Severity {
	order := map[Severity]int{
		SeverityInfo:     0,
		SeverityLow:      1,
		SeverityMedium:   2,
		SeverityHigh:     3,
		SeverityBlocking: 4,
	}
	if order[a] >= order[b] {
		return a
	}
	return b
}