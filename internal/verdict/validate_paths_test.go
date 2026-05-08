package verdict

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCategorizeFindingsByPath_KeepsDiffFiles(t *testing.T) {
	findings := []Finding{
		{File: "apps/backend/src/lib/tools/plan-tools.ts", LineStart: 10, Title: "real issue"},
		{File: "src/main.ts", LineStart: 5, Title: "another real issue"},
	}
	diffPaths := []string{"apps/backend/src/lib/tools/plan-tools.ts", "src/main.ts", "README.md"}

	inDiff, dangling, hallucinated := CategorizeFindingsByPath(findings, diffPaths)
	require.Len(t, inDiff, 2)
	require.Empty(t, dangling)
	require.Empty(t, hallucinated)
}

func TestCategorizeFindingsByPath_HallucinatedFiles(t *testing.T) {
	findings := []Finding{
		{File: "service/get_plan_context.go", LineStart: 10, Critic: "perf_smell", Title: "hallucinated Go file"},
		{File: "src/services/planContextService.js", LineStart: 5, Critic: "test_coverage_smell", Title: "hallucinated JS file"},
		{File: "apps/backend/src/lib/tools/plan-tools.ts", LineStart: 20, Critic: "risk_classifier", Title: "real file"},
	}
	diffPaths := []string{"apps/backend/src/lib/tools/plan-tools.ts", ".charters/ch-2026-05-07-12f7a9.spec.md"}

	inDiff, dangling, hallucinated := CategorizeFindingsByPath(findings, diffPaths)
	require.Len(t, inDiff, 1)
	require.Equal(t, "apps/backend/src/lib/tools/plan-tools.ts", inDiff[0].File)
	require.Empty(t, dangling)
	require.Len(t, hallucinated, 2)
}

func TestCategorizeFindingsByPath_TestFileSuggestionsAreDangling(t *testing.T) {
	findings := []Finding{
		{File: "apps/backend/src/lib/tools/plan-tools.test.ts", LineStart: 0, Critic: "test_coverage_smell", Title: "Missing test for empty plan context"},
		{File: "apps/backend/src/lib/tools/plan-tools.ts", LineStart: 42, Critic: "risk_classifier", Title: "SQL injection"},
	}
	diffPaths := []string{"apps/backend/src/lib/tools/plan-tools.ts", ".charters/ch-2026-05-07-12f7a9.spec.md"}

	inDiff, dangling, hallucinated := CategorizeFindingsByPath(findings, diffPaths)
	require.Len(t, inDiff, 1)
	require.Equal(t, "apps/backend/src/lib/tools/plan-tools.ts", inDiff[0].File)
	require.Len(t, dangling, 1)
	require.Equal(t, "apps/backend/src/lib/tools/plan-tools.test.ts", dangling[0].File)
	require.Empty(t, hallucinated)
}

func TestCategorizeFindingsByPath_SpecFileSuggestionsAreDangling(t *testing.T) {
	findings := []Finding{
		{File: "src/component.spec.tsx", LineStart: 0, Critic: "test_coverage_smell", Title: "Missing spec"},
		{File: "tests/unit/migrations/20240507_add_plan_context_index.sql", LineStart: 0, Critic: "test_coverage_smell", Title: "Missing migration test"},
	}
	diffPaths := []string{"src/component.tsx", "db/migrations/20240507.sql"}

	inDiff, dangling, hallucinated := CategorizeFindingsByPath(findings, diffPaths)
	require.Len(t, inDiff, 0)
	require.Len(t, dangling, 2)
	require.Empty(t, hallucinated)
}

func TestCategorizeFindingsByPath_EmptyFileFindingsAreInDiff(t *testing.T) {
	findings := []Finding{
		{File: "", LineStart: 0, Title: "general finding no file"},
		{File: "real.ts", LineStart: 1, Title: "file finding"},
	}
	diffPaths := []string{"real.ts"}

	inDiff, dangling, hallucinated := CategorizeFindingsByPath(findings, diffPaths)
	require.Len(t, inDiff, 2)
	require.Empty(t, dangling)
	require.Empty(t, hallucinated)
}

func TestCategorizeFindingsByPath_EmptyDiffPathsKeepsAll(t *testing.T) {
	findings := []Finding{
		{File: "madeup.go", LineStart: 10, Title: "hallucinated"},
		{File: "also_madeup.js", LineStart: 20, Title: "also hallucinated"},
	}

	inDiff, _, _ := CategorizeFindingsByPath(findings, nil)
	require.Len(t, inDiff, 2)

	inDiff, dangling, hallucinated := CategorizeFindingsByPath(findings, []string{})
	require.Len(t, inDiff, 2)
	require.Empty(t, dangling)
	require.Empty(t, hallucinated)
}

func TestCategorizeFindingsByPath_AllHallucinated(t *testing.T) {
	findings := []Finding{
		{File: "service/get_plan_context.go", LineStart: 1, Critic: "perf_smell", Title: "hallucination 1"},
		{File: "repository/plan_context_repository.go", LineStart: 5, Critic: "perf_smell", Title: "hallucination 2"},
		{File: "handlers/context_handler.go", LineStart: 10, Critic: "test_coverage_smell", Title: "hallucination 3"},
	}
	diffPaths := []string{".charters/ch-2026-05-07-12f7a9.spec.md"}

	inDiff, dangling, hallucinated := CategorizeFindingsByPath(findings, diffPaths)
	require.Empty(t, inDiff)
	require.Empty(t, dangling)
	require.Len(t, hallucinated, 3)
}

func TestCategorizeFindingsByPath_NilFindings(t *testing.T) {
	inDiff, dangling, hallucinated := CategorizeFindingsByPath(nil, []string{"a.txt"})
	require.Len(t, inDiff, 0)
	require.Empty(t, dangling)
	require.Empty(t, hallucinated)
}

func TestCategorizeFindingsByPath_PreservesFindingFields(t *testing.T) {
	findings := []Finding{
		{
			Critic:       "risk_classifier",
			Severity:     SeverityHigh,
			Title:        "SQL injection",
			Detail:       "dangerous query construction",
			File:         "src/db.ts",
			LineStart:    42,
			LineEnd:      45,
			SuggestedFix: "use parameterized query",
		},
	}
	diffPaths := []string{"src/db.ts"}

	inDiff, _, _ := CategorizeFindingsByPath(findings, diffPaths)
	require.Len(t, inDiff, 1)
	require.Equal(t, "risk_classifier", inDiff[0].Critic)
	require.Equal(t, SeverityHigh, inDiff[0].Severity)
	require.Equal(t, "SQL injection", inDiff[0].Title)
	require.Equal(t, "src/db.ts", inDiff[0].File)
	require.Equal(t, 42, inDiff[0].LineStart)
	require.Equal(t, 45, inDiff[0].LineEnd)
	require.Equal(t, "use parameterized query", inDiff[0].SuggestedFix)
}

func TestIsTestVariant(t *testing.T) {
	tests := []struct {
		filePath string
		diffPath string
		want     bool
	}{
		{"plan-tools.test.ts", "plan-tools.ts", true},
		{"app.test.ts", "app.ts", true},
		{"Component.spec.tsx", "Component.tsx", true},
		{"handler_test.go", "handler.go", true},
		{"service_spec.py", "service.py", true},
		{"plan-tools.ts", "plan-tools.ts", false}, // same file, not a test variant
		{"unrelated.ts", "plan-tools.ts", false},
		{"app.ts", "plan-tools.test.ts", false}, // reversed, not a test variant
	}

	for _, tt := range tests {
		t.Run(tt.filePath+"_"+tt.diffPath, func(t *testing.T) {
			got := isTestVariant(tt.filePath, tt.diffPath)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestLooksLikeComplementaryFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"src/app.test.ts", true},
		{"src/app.spec.tsx", true},
		{"test/unit/handler_test.go", true},
		{"__tests__/component.test.tsx", true},
		{"migrations/20240507_add_index.sql", true},
		{"src/app.config.ts", true},
		{"src/main.ts", false},
		{"service/get_plan_context.go", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := looksLikeComplementaryFile(tt.path)
			require.Equal(t, tt.want, got)
		})
	}
}