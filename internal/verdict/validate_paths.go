package verdict

import "log/slog"

// FilterHallucinatedPaths removes findings whose File field does not appear in
// the set of known diff paths. Findings with an empty File or a File that is
// present in diffPaths are kept. Findings referencing unknown files are logged
// as warnings and dropped.
func FilterHallucinatedPaths(findings []Finding, diffPaths []string) []Finding {
	if len(diffPaths) == 0 {
		return findings
	}

	valid := make(map[string]bool, len(diffPaths))
	for _, p := range diffPaths {
		valid[p] = true
	}

	var kept []Finding
	for _, f := range findings {
		if f.File == "" {
			kept = append(kept, f)
			continue
		}
		if valid[f.File] {
			kept = append(kept, f)
			continue
		}
		slog.Warn("filtering hallucinated file path from finding",
			"critic", f.Critic,
			"file", f.File,
			"line_start", f.LineStart,
			"title", f.Title,
		)
	}
	return kept
}