package critics

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/helloodokai/acig/internal/config"
	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/verdict"
)

func TestMapCharterSeverity(t *testing.T) {
	tests := []struct {
		input string
		want  verdict.Severity
	}{
		{"blocking", verdict.SeverityBlocking},
		{"error", verdict.SeverityHigh},
		{"high", verdict.SeverityHigh},
		{"warning", verdict.SeverityMedium},
		{"medium", verdict.SeverityMedium},
		{"info", verdict.SeverityLow},
		{"low", verdict.SeverityLow},
		{"unknown", verdict.SeverityInfo},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := mapCharterSeverity(tt.input)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCharterConformanceSkipsWithoutPath(t *testing.T) {
	cc := &CharterConformance{baseCritic: baseCritic{id: "charter_conformance", tier: TierCheap}}
	pc := &Context{
		Config: &config.Config{},
		Diff:   &diff.Diff{RawPatch: ""},
	}
	result, err := cc.Run(context.Background(), pc.Diff, pc)
	require.NoError(t, err)
	require.Equal(t, "charter_conformance", result.Critic)
	require.Empty(t, result.Findings)
	require.Empty(t, result.Error)
}

func TestCharterConformanceSkipsWithMissingBinary(t *testing.T) {
	cc := &CharterConformance{baseCritic: baseCritic{id: "charter_conformance", tier: TierCheap}}
	pc := &Context{
		Config: &config.Config{
			Charter: config.CharterConfig{
				Path: "/nonexistent/charter.yaml",
			},
		},
		Diff: &diff.Diff{RawPatch: ""},
	}
	result, err := cc.Run(context.Background(), pc.Diff, pc)
	require.NoError(t, err)
	require.Equal(t, "charter_conformance", result.Critic)
	require.NotEmpty(t, result.Error)
}

func TestCharterConformanceConvertsFindings(t *testing.T) {
	cv := charterVerdict{
		CharterID: "ch-2026-05-04-test",
		Status:    "fail",
		Score:     0.3,
		Findings: []charterFinding{
			{
				Critic:     "charter:blast_radius",
				Severity:   "warning",
				Message:    "file outside blast radius",
				Detail:     "src/new_file.go",
				CharterRef: "blast_radius.files",
				File:       "src/new_file.go",
			},
			{
				Critic:     "charter:unknown_gating",
				Severity:   "blocking",
				Message:    "blocking unknown found",
				Detail:     "what API version?",
			},
		},
	}

	findings := convertCharterFindings(cv)
	require.Len(t, findings, 2)
	require.Equal(t, verdict.SeverityMedium, findings[0].Severity)
	require.Equal(t, verdict.SeverityBlocking, findings[1].Severity)
	require.Equal(t, "file outside blast radius", findings[0].Title)
	require.Contains(t, findings[0].SuggestedFix, "charter ch-2026-05-04-test")
}

func TestCharterConformanceAddFallbackOnFail(t *testing.T) {
	cv := charterVerdict{
		CharterID: "ch-test",
		Status:    "fail",
		Score:     0.0,
		Findings:  []charterFinding{},
	}
	findings := convertCharterFindings(cv)
	require.Len(t, findings, 1)
	require.Equal(t, verdict.SeverityHigh, findings[0].Severity)
	require.Contains(t, findings[0].Title, "Charter conformance check failed")
}