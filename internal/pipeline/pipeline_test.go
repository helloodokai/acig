package pipeline

import (
	"testing"

	"github.com/helloodokai/acig/internal/verdict"
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