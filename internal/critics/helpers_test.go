package critics

import (
	"strings"
	"testing"

	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/verdict"
	"github.com/stretchr/testify/require"
)

func TestStripMarkdownFence(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"no fence", `{"findings": []}`, `{"findings": []}`},
		{"json fence", "```json\n{\"findings\": []}\n```", `{"findings": []}`},
		{"plain fence", "```\n{\"findings\": []}\n```", `{"findings": []}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripMarkdownFence(tt.input)
			if got != tt.want {
				t.Errorf("stripMarkdownFence(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestStripOutputTags(t *testing.T) {
	cases := map[string]string{
		"<output>{\"x\":1}</output>":               `{"x":1}`,
		"prefix <output>{\"x\":1}</output> suffix": `{"x":1}`,
		"no tags {\"x\":1}":                        `no tags {"x":1}`,
		"<output>multi\nline</output>":             "multi\nline",
	}
	for in, want := range cases {
		got := stripOutputTags(in)
		require.Equal(t, want, got, "stripOutputTags(%q)", in)
	}
}

func TestTruncate(t *testing.T) {
	if truncate("hello", 10) != "hello" {
		t.Error("should not truncate short strings")
	}
	if truncate("hello world this is long", 10) != "hello worl..." {
		t.Error("should truncate long strings with ellipsis")
	}
}

func TestNormalizeRisk(t *testing.T) {
	require.Equal(t, verdict.RiskLow, normalizeRisk("low"))
	require.Equal(t, verdict.RiskLow, normalizeRisk("LOW"))
	require.Equal(t, verdict.RiskMedium, normalizeRisk("medium"))
	require.Equal(t, verdict.RiskMedium, normalizeRisk("med"))
	require.Equal(t, verdict.RiskHigh, normalizeRisk("HIGH"))
	require.Equal(t, verdict.RiskCritical, normalizeRisk("critical"))
	require.Equal(t, verdict.RiskCritical, normalizeRisk(" crit "))
	require.Equal(t, verdict.Risk(""), normalizeRisk("nonsense"))
	require.Equal(t, verdict.Risk(""), normalizeRisk(""))
}

// helpers for building diffs in tests
func makeDiff(files ...diff.FileDiff) *diff.Diff {
	d := &diff.Diff{}
	for i := range files {
		f := files[i]
		if f.DiffLines == nil {
			f.DiffLines = map[int]bool{}
		}
		if f.OrigLines == nil {
			f.OrigLines = map[int]bool{}
		}
		d.Files = append(d.Files, f)
	}
	d.Stats.FilesChanged = len(files)
	return d
}

func TestBuildFileSummary(t *testing.T) {
	d := makeDiff(
		diff.FileDiff{Path: "src/a.go", DiffLines: map[int]bool{10: true, 11: true, 12: true, 20: true}, Added: []string{"a", "b", "c"}},
		diff.FileDiff{Path: "README.md", IsDelete: true, OrigLines: map[int]bool{1: true, 2: true}, Removed: []string{"x", "y"}},
	)
	out := buildFileSummary(d)
	require.Contains(t, out, "src/a.go")
	require.Contains(t, out, "kind=code")
	require.Contains(t, out, "status=modified")
	require.Contains(t, out, "README.md")
	require.Contains(t, out, "kind=docs")
	require.Contains(t, out, "status=deleted")
	require.Contains(t, out, "10-12")
	require.Contains(t, out, "20")
}

func TestBuildValidLines(t *testing.T) {
	d := makeDiff(
		diff.FileDiff{Path: "src/a.go", DiffLines: map[int]bool{10: true, 11: true, 12: true}},
		diff.FileDiff{Path: "deleted.md", IsDelete: true, OrigLines: map[int]bool{1: true, 2: true}},
	)
	out := buildValidLines(d)
	require.Contains(t, out, "[RIGHT]")
	require.Contains(t, out, "[LEFT]")
	require.Contains(t, out, "10-12")
}

func TestBuildNumberedDiff(t *testing.T) {
	d := makeDiff(
		diff.FileDiff{
			Path:      "src/a.go",
			Patch:     "@@ -10,3 +10,4 @@\n context\n+added line\n unchanged\n-removed line\n",
			DiffLines: map[int]bool{10: true, 11: true, 12: true},
			OrigLines: map[int]bool{10: true, 11: true, 12: true},
		},
	)
	out := buildNumberedDiff(d, 1000)
	require.Contains(t, out, "FILE: src/a.go")
	require.Contains(t, out, "added line")
	require.Contains(t, out, "removed line")
	// new-side line numbers should be visible alongside content
	require.Regexp(t, `\+\s+11\s+\| added line`, out)
}

func TestSeverityRubricAndOutputContractNonEmpty(t *testing.T) {
	require.NotEmpty(t, severityRubric())
	require.NotEmpty(t, outputContract())
	require.Contains(t, severityRubric(), "blocking")
	require.Contains(t, outputContract(), "<output>")
}

func TestValidateFinding_DropsFileNotInPR(t *testing.T) {
	d := makeDiff(diff.FileDiff{Path: "a.go", DiffLines: map[int]bool{1: true}})
	vc := newValidationContext(d)
	_, reason := vc.validateFinding("security_smell", TierMid, verdict.Finding{
		File: "missing.go", LineStart: 1, Severity: verdict.SeverityHigh,
	})
	require.NotEmpty(t, reason)
}

func TestValidateFinding_DropsLineOutsideDiff(t *testing.T) {
	d := makeDiff(diff.FileDiff{Path: "a.go", DiffLines: map[int]bool{10: true, 11: true}})
	vc := newValidationContext(d)
	_, reason := vc.validateFinding("security_smell", TierMid, verdict.Finding{
		File: "a.go", LineStart: 100, Severity: verdict.SeverityHigh,
	})
	require.NotEmpty(t, reason)
}

func TestValidateFinding_AcceptsLineWithinSnap(t *testing.T) {
	d := makeDiff(diff.FileDiff{Path: "a.go", DiffLines: map[int]bool{10: true, 11: true, 12: true}})
	vc := newValidationContext(d)
	_, reason := vc.validateFinding("security_smell", TierMid, verdict.Finding{
		File: "a.go", LineStart: 13, Severity: verdict.SeverityHigh, // within ±3 of 10..12
	})
	require.Empty(t, reason)
}

func TestValidateFinding_DropsSecurityOnDocs(t *testing.T) {
	d := makeDiff(diff.FileDiff{Path: "README.md", DiffLines: map[int]bool{1: true}})
	vc := newValidationContext(d)
	_, reason := vc.validateFinding("security_smell", TierMid, verdict.Finding{
		File: "README.md", LineStart: 1, Severity: verdict.SeverityHigh,
	})
	require.Contains(t, reason, "kind=docs")
}

func TestValidateFinding_DowngradesBlockingFromCheap(t *testing.T) {
	d := makeDiff(diff.FileDiff{Path: "a.go", DiffLines: map[int]bool{1: true}})
	vc := newValidationContext(d)
	got, reason := vc.validateFinding("style_conformance", TierCheap, verdict.Finding{
		File: "a.go", LineStart: 1, Severity: verdict.SeverityBlocking,
	})
	require.Empty(t, reason)
	require.Equal(t, verdict.SeverityHigh, got.Severity)
}

func TestValidateFinding_CoercesUnknownSeverity(t *testing.T) {
	d := makeDiff(diff.FileDiff{Path: "a.go", DiffLines: map[int]bool{1: true}})
	vc := newValidationContext(d)
	got, _ := vc.validateFinding("security_smell", TierMid, verdict.Finding{
		File: "a.go", LineStart: 1, Severity: verdict.Severity("URGENT"),
	})
	require.Equal(t, verdict.SeverityInfo, got.Severity)
}

func TestPromptsContainScaffoldSections(t *testing.T) {
	prompts := map[string]string{
		"security_smell":      securitySmellPrompt,
		"perf_smell":          perfSmellPrompt,
		"style_conformance":   styleConformancePrompt,
		"test_coverage_smell": testCoverageSmellPrompt,
		"adjudicator":         adjudicatorPrompt,
	}
	for name, p := range prompts {
		t.Run(name, func(t *testing.T) {
			require.Contains(t, p, "HARD RULES", "prompt %s missing HARD RULES", name)
			require.Contains(t, p, "DO NOT", "prompt %s missing DO NOT", name)
			require.Contains(t, p, "{{.SeverityRubric}}", "prompt %s missing SeverityRubric placeholder", name)
			require.Contains(t, p, "{{.OutputContract}}", "prompt %s missing OutputContract placeholder", name)
			require.Contains(t, p, "{{.NumberedDiff}}", "prompt %s missing NumberedDiff placeholder", name)
			require.Contains(t, p, "SELF-CHECK", "prompt %s missing SELF-CHECK", name)
		})
	}
	// risk_classifier has its own shape (no findings); check separately.
	require.Contains(t, riskClassifierPrompt, "HARD RULES")
	require.Contains(t, riskClassifierPrompt, "{{.NumberedDiff}}")
	require.NotContains(t, riskClassifierPrompt, "{{.SeverityRubric}}", "risk_classifier should not include severity rubric")
	require.False(t, strings.Contains(riskClassifierPrompt, `"findings"`), "risk_classifier prompt should not reference findings")
}
