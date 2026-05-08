package verdict

import "log/slog"

// CategorizeFindingsByPath splits findings into three groups based on whether
// their File field appears in the diff paths:
//   - inDiff: findings whose file is in the diff (can be inline-commented)
//   - dangling: findings whose file is NOT in the diff but is a plausible
//     suggestion (e.g. a test file that should exist alongside a changed source file)
//   - hallucinated: findings whose file appears fabricated (no clear relationship
//     to any file in the diff and the file is not a common test/config variant)
//
// In-diff findings are always kept. Dangling findings are kept but should NOT be
// posted as inline review comments (GitHub will reject them). Hallucinated
// findings are dropped entirely with a warning log.
func CategorizeFindingsByPath(findings []Finding, diffPaths []string) (inDiff, dangling, hallucinated []Finding) {
	if len(diffPaths) == 0 {
		return findings, nil, nil
	}

	valid := make(map[string]bool, len(diffPaths))
	for _, p := range diffPaths {
		valid[p] = true
	}

	for _, f := range findings {
		if f.File == "" {
			inDiff = append(inDiff, f)
			continue
		}
		if valid[f.File] {
			inDiff = append(inDiff, f)
			continue
		}

		if isPlausibleSuggestion(f.File, diffPaths) {
			dangling = append(dangling, f)
			continue
		}

		slog.Warn("filtering hallucinated file path from finding",
			"critic", f.Critic,
			"file", f.File,
			"line_start", f.LineStart,
			"title", f.Title,
		)
		hallucinated = append(hallucinated, f)
	}
	return inDiff, dangling, hallucinated
}

// isPlausibleSuggestion checks whether a file path that doesn't appear in the
// diff could be a legitimate suggestion (e.g. a test file that should exist
// alongside a changed source file, or a migration/config that pairs with a
// change). It returns true if:
//   - The basename of the suggested file matches or is a test variant of a
//     file that IS in the diff
//   - The suggested file is in the same directory tree as a diff file AND
//     looks like a complementary file type
//   - The suggested file looks like a complementary file regardless of
//     directory (test files, migrations in common locations)
func isPlausibleSuggestion(filePath string, diffPaths []string) bool {
	for _, dp := range diffPaths {
		if isTestVariant(filePath, dp) {
			return true
		}
		if sameDirectoryTree(filePath, dp) && looksLikeComplementaryFile(filePath) {
			return true
		}
	}
	// Files that look like complementary types regardless of directory
	if looksLikeComplementaryFile(filePath) {
		return true
	}
	return false
}

// isTestVariant checks if filePath is a test/spec variant of diffPath.
// e.g. plan-tools.test.ts is a test variant of plan-tools.ts
//      PlanTools.spec.tsx is a test variant of PlanTools.tsx
// Returns false if filePath equals diffPath (same file, not a variant).
func isTestVariant(filePath, diffPath string) bool {
	if filePath == diffPath {
		return false
	}

	fb := filepathBase(filePath)
	db := filepathBase(diffPath)

	fCore := stripAllExtensions(fb)
	dCore := stripAllExtensions(db)

	fstripped := stripTestInfix(fCore)

	return fstripped == dCore
}

// stripAllExtensions removes all extensions from a filename.
// e.g. "plan-tools.test.ts" -> "plan-tools"
// e.g. "PlanTools.spec.tsx" -> "PlanTools"
// e.g. "handler.go" -> "handler"
func stripAllExtensions(name string) string {
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			return name[:i]
		}
	}
	return name
}

// stripTestInfix removes test/spec infixes from a filename core.
// e.g. "plan-tools_test" -> "plan-tools"
// e.g. "PlanTools.spec" -> "PlanTools"
// e.g. "plan-tools-test" -> "plan-tools"
func stripTestInfix(s string) string {
	for _, infix := range []string{"_test", "_spec", ".test", ".spec", "-test", "-spec"} {
		if idx := indexOf(s, infix); idx != -1 {
			return s[:idx]
		}
	}
	return s
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func filepathBase(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[i+1:]
		}
	}
	return path
}

// sameDirectoryTree checks if two paths share a common parent directory.
func sameDirectoryTree(a, b string) bool {
	aDir := dirOf(a)
	bDir := dirOf(b)
	if aDir == "" || bDir == "" {
		return false
	}
	return aDir == bDir
}

func dirOf(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		if path[i] == '/' {
			return path[:i]
		}
	}
	return ""
}

// looksLikeComplementaryFile returns true if the file path looks like a
// test, spec, migration, or config file that would logically accompany
// a source file in the diff.
func looksLikeComplementaryFile(path string) bool {
	lower := toLower(path)
	// Test/spec files (compound extensions like .test.ts, .spec.tsx)
	if contains(lower, ".test.") || contains(lower, ".spec.") ||
		contains(lower, "_test.") || contains(lower, "_spec.") {
		return true
	}
	// Test directories
	if contains(lower, "/test/") || contains(lower, "/tests/") ||
		contains(lower, "/__tests__/") || contains(lower, "/spec/") {
		return true
	}
	// Also match paths starting with test directories
	if hasPrefix(lower, "test/") || hasPrefix(lower, "tests/") ||
		hasPrefix(lower, "spec/") || hasPrefix(lower, "__tests__/") {
		return true
	}
	// Migration directories and files
	if contains(lower, "/migrations/") || contains(lower, "/migration/") ||
		contains(lower, "/db/") || contains(lower, "/migrate/") {
		return true
	}
	if hasPrefix(lower, "migrations/") || hasPrefix(lower, "migration/") {
		return true
	}
	// Config files (ending in .config.ts, .config.js, .rc.js, etc.)
	base := filepathBase(lower)
	if contains(base, ".config.") || contains(base, ".rc.") {
		return true
	}
	return false
}

func toLower(s string) string {
	result := make([]byte, len(s))
	for i := range s {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result[i] = c
	}
	return string(result)
}

func contains(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func hasSuffix(s, suffix string) bool {
	if len(suffix) > len(s) {
		return false
	}
	return s[len(s)-len(suffix):] == suffix
}

func hasPrefix(s, prefix string) bool {
	if len(prefix) > len(s) {
		return false
	}
	return s[:len(prefix)] == prefix
}