package pipeline

import (
	"testing"

	"github.com/helloodokai/acig/internal/budget"
	"github.com/helloodokai/acig/internal/verdict"
	"github.com/stretchr/testify/require"
)

func TestComputeDecision(t *testing.T) {
	tests := []struct {
		name     string
		findings []verdict.Finding
		want     verdict.Decision
	}{
		{
			"no findings",
			nil,
			verdict.DecisionPass,
		},
		{
			"info only",
			[]verdict.Finding{{Severity: verdict.SeverityInfo}},
			verdict.DecisionPass,
		},
		{
			"low only",
			[]verdict.Finding{{Severity: verdict.SeverityLow}},
			verdict.DecisionPass,
		},
		{
			"medium",
			[]verdict.Finding{{Severity: verdict.SeverityMedium}},
			verdict.DecisionWarn,
		},
		{
			"high",
			[]verdict.Finding{{Severity: verdict.SeverityHigh}},
			verdict.DecisionWarn,
		},
		{
			"blocking",
			[]verdict.Finding{{Severity: verdict.SeverityBlocking}},
			verdict.DecisionBlock,
		},
		{
			"mixed with blocking",
			[]verdict.Finding{{Severity: verdict.SeverityLow}, {Severity: verdict.SeverityBlocking}},
			verdict.DecisionBlock,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeDecision(tt.findings)
			if got != tt.want {
				t.Errorf("computeDecision() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestComputeRisk(t *testing.T) {
	tests := []struct {
		name        string
		findings    []verdict.Finding
		currentRisk verdict.Risk
		want        verdict.Risk
	}{
		{"no findings low", nil, verdict.RiskLow, verdict.RiskLow},
		{"medium finding bumps low", []verdict.Finding{{Severity: verdict.SeverityMedium}}, verdict.RiskLow, verdict.RiskMedium},
		{"high finding bumps medium", []verdict.Finding{{Severity: verdict.SeverityHigh}}, verdict.RiskMedium, verdict.RiskHigh},
		{"blocking is critical", []verdict.Finding{{Severity: verdict.SeverityBlocking}}, verdict.RiskLow, verdict.RiskCritical},
		{"critical stays critical", []verdict.Finding{{Severity: verdict.SeverityLow}}, verdict.RiskCritical, verdict.RiskCritical},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := computeRisk(tt.findings, tt.currentRisk)
			if got != tt.want {
				t.Errorf("computeRisk() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFinalize_FilterHallucinatedPaths(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "apps/backend/src/lib/tools/plan-tools.ts", LineStart: 10, Severity: verdict.SeverityMedium, Title: "real issue"},
			{File: "service/get_plan_context.go", LineStart: 5, Severity: verdict.SeverityHigh, Critic: "perf_smell", Title: "hallucinated Go file"},
			{File: "src/services/planContextService.js", LineStart: 20, Severity: verdict.SeverityLow, Critic: "test_coverage_smell", Title: "hallucinated JS file"},
		},
	}
	ledger, _ := budget.NewLedger(1.0)
	diffPaths := []string{"apps/backend/src/lib/tools/plan-tools.ts", ".charters/ch-2026-05-07-12f7a9.spec.md"}

	finalize(v, ledger, nil, diffPaths)

	require.Len(t, v.Findings, 1)
	require.Equal(t, "apps/backend/src/lib/tools/plan-tools.ts", v.Findings[0].File)
}

func TestFinalize_EmptyDiffPathsPreservesAll(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "any/file.go", LineStart: 1, Severity: verdict.SeverityLow, Title: "some issue"},
		},
	}
	ledger, _ := budget.NewLedger(1.0)

	finalize(v, ledger, nil, nil)

	require.Len(t, v.Findings, 1)
}

func TestFinalize_SuppressionsAppliedAfterPathFilter(t *testing.T) {
	suppressions := []verdict.Suppression{
		{Critic: "perf_smell", Title: "real issue", Reason: "known false positive"},
	}
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "real.ts", LineStart: 10, Severity: verdict.SeverityHigh, Critic: "perf_smell", Title: "real issue"},
			{File: "hallucinated.go", LineStart: 5, Severity: verdict.SeverityMedium, Critic: "perf_smell", Title: "hallucinated"},
		},
	}
	ledger, _ := budget.NewLedger(1.0)
	diffPaths := []string{"real.ts"}

	finalize(v, ledger, suppressions, diffPaths)

	require.Len(t, v.Findings, 0)
}

func TestClassifyRisk(t *testing.T) {
	tests := []struct {
		name   string
		result *verdict.CriticResult
		want   verdict.Risk
	}{
		{"no findings", &verdict.CriticResult{}, verdict.RiskLow},
		{"high finding", &verdict.CriticResult{Findings: []verdict.Finding{{Severity: verdict.SeverityHigh}}}, verdict.RiskHigh},
		{"medium finding", &verdict.CriticResult{Findings: []verdict.Finding{{Severity: verdict.SeverityMedium}}}, verdict.RiskMedium},
		{"blocking finding", &verdict.CriticResult{Findings: []verdict.Finding{{Severity: verdict.SeverityBlocking}}}, verdict.RiskHigh},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := classifyRisk(tt.result)
			if got != tt.want {
				t.Errorf("classifyRisk() = %v, want %v", got, tt.want)
			}
		})
	}
}