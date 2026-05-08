package verdict

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterHallucinatedPaths_KeepsValidFindings(t *testing.T) {
	findings := []Finding{
		{File: "apps/backend/src/lib/tools/plan-tools.ts", LineStart: 10, Title: "real issue"},
		{File: "src/main.ts", LineStart: 5, Title: "another real issue"},
	}
	diffPaths := []string{"apps/backend/src/lib/tools/plan-tools.ts", "src/main.ts", "README.md"}

	result := FilterHallucinatedPaths(findings, diffPaths)
	require.Len(t, result, 2)
	require.Equal(t, "apps/backend/src/lib/tools/plan-tools.ts", result[0].File)
	require.Equal(t, "src/main.ts", result[1].File)
}

func TestFilterHallucinatedPaths_RemovesHallucinatedPaths(t *testing.T) {
	findings := []Finding{
		{File: "service/get_plan_context.go", LineStart: 10, Critic: "perf_smell", Title: "hallucinated Go file"},
		{File: "src/services/planContextService.js", LineStart: 5, Critic: "test_coverage_smell", Title: "hallucinated JS file"},
		{File: "apps/backend/src/lib/tools/plan-tools.ts", LineStart: 20, Critic: "risk_classifier", Title: "real file"},
	}
	diffPaths := []string{"apps/backend/src/lib/tools/plan-tools.ts", ".charters/ch-2026-05-07-12f7a9.spec.md"}

	result := FilterHallucinatedPaths(findings, diffPaths)
	require.Len(t, result, 1)
	require.Equal(t, "apps/backend/src/lib/tools/plan-tools.ts", result[0].File)
}

func TestFilterHallucinatedPaths_KeepsEmptyFileFindings(t *testing.T) {
	findings := []Finding{
		{File: "", LineStart: 0, Title: "general finding no file"},
		{File: "real.ts", LineStart: 1, Title: "file finding"},
	}
	diffPaths := []string{"real.ts"}

	result := FilterHallucinatedPaths(findings, diffPaths)
	require.Len(t, result, 2)
}

func TestFilterHallucinatedPaths_EmptyDiffPathsKeepsAll(t *testing.T) {
	findings := []Finding{
		{File: "madeup.go", LineStart: 10, Title: "hallucinated"},
		{File: "also_madeup.js", LineStart: 20, Title: "also hallucinated"},
	}

	result := FilterHallucinatedPaths(findings, nil)
	require.Len(t, result, 2)

	result = FilterHallucinatedPaths(findings, []string{})
	require.Len(t, result, 2)
}

func TestFilterHallucinatedPaths_AllHallucinated(t *testing.T) {
	findings := []Finding{
		{File: "service/get_plan_context.go", LineStart: 1, Critic: "perf_smell", Title: "hallucination 1"},
		{File: "repository/plan_context_repository.go", LineStart: 5, Critic: "perf_smell", Title: "hallucination 2"},
		{File: "handlers/context_handler.go", LineStart: 10, Critic: "test_coverage_smell", Title: "hallucination 3"},
	}
	diffPaths := []string{".charters/ch-2026-05-07-12f7a9.spec.md"}

	result := FilterHallucinatedPaths(findings, diffPaths)
	require.Len(t, result, 0)
}

func TestFilterHallucinatedPaths_NilFindings(t *testing.T) {
	result := FilterHallucinatedPaths(nil, []string{"a.txt"})
	require.Len(t, result, 0)
}

func TestFilterHallucinatedPaths_PreservesFindingFields(t *testing.T) {
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

	result := FilterHallucinatedPaths(findings, diffPaths)
	require.Len(t, result, 1)
	require.Equal(t, "risk_classifier", result[0].Critic)
	require.Equal(t, SeverityHigh, result[0].Severity)
	require.Equal(t, "SQL injection", result[0].Title)
	require.Equal(t, "src/db.ts", result[0].File)
	require.Equal(t, 42, result[0].LineStart)
	require.Equal(t, 45, result[0].LineEnd)
	require.Equal(t, "use parameterized query", result[0].SuggestedFix)
}