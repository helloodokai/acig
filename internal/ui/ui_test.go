package ui

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"
)

func TestProgressIncrementOverTotal(t *testing.T) {
	var buf bytes.Buffer
	p := &Progress{out: &buf, total: 3, start: time.Now()}
	// Increment more times than total (simulates adjudicator running after critics)
	p.Increment("risk_classifier")
	p.Increment("security_smell")
	p.Increment("style_conformance")
	p.Increment("adjudicator") // 4th increment with total=3 — must not panic
	output := buf.String()
	// Should cap at 3/3 and not panic
	if !strings.Contains(output, "3/3") {
		t.Errorf("expected 3/3 capped display, got: %s", output)
	}
}

func TestProgressIncrementZeroTotal(t *testing.T) {
	var buf bytes.Buffer
	p := &Progress{out: &buf, total: 0, start: time.Now()}
	p.Increment("test_critic")
	if buf.Len() == 0 {
		t.Error("expected output for zero-total progress")
	}
}

func TestProgressIncrementNormal(t *testing.T) {
	var buf bytes.Buffer
	p := &Progress{out: &buf, total: 3, start: time.Now()}
	p.Increment("risk_classifier")
	p.Increment("security_smell")
	p.Increment("style_conformance")
	output := buf.String()
	if !strings.Contains(output, "3/3") {
		t.Errorf("expected '3/3' in output, got: %s", output)
	}
}

func TestProgressIncrementErrorLabel(t *testing.T) {
	var buf bytes.Buffer
	p := &Progress{out: &buf, total: 1, start: time.Now()}
	p.Increment("test (error)")
	output := buf.String()
	if !strings.Contains(output, "✗") {
		t.Errorf("expected ✗ for error label, got: %s", output)
	}
}

func TestProgressDone(t *testing.T) {
	var buf bytes.Buffer
	p := &Progress{out: &buf, total: 1, start: time.Now()}
	p.Increment("test")
	p.Done()
	output := buf.String()
	if !strings.Contains(output, "Done") {
		t.Errorf("expected 'Done' in output, got: %s", output)
	}
}

func TestShouldShowUIGitHub(t *testing.T) {
	orig := os.Getenv("GITHUB_ACTIONS")
	os.Setenv("GITHUB_ACTIONS", "true")
	defer os.Setenv("GITHUB_ACTIONS", orig)
	if ShouldShowUI() {
		t.Error("should not show UI in GitHub Actions")
	}
}

func TestShouldShowUINoColor(t *testing.T) {
	orig := os.Getenv("NO_COLOR")
	os.Setenv("NO_COLOR", "1")
	defer os.Setenv("NO_COLOR", orig)
	if ShouldShowUI() {
		t.Error("should not show UI with NO_COLOR")
	}
}

func TestSeverityIcon(t *testing.T) {
	cases := map[string]string{
		"blocking": ">>",
		"high":     ">",
		"medium":   ">",
		"low":      ">",
		"info":     "-",
		"unknown":  "-",
	}
	for sev, wantContains := range cases {
		icon := severityIcon(sev)
		if !strings.Contains(icon, wantContains) {
			t.Errorf("severityIcon(%q) = %q, want to contain %q", sev, icon, wantContains)
		}
	}
}

func TestPrintVerdict(t *testing.T) {
	var buf bytes.Buffer
	orig := output
	output = &buf
	defer func() { output = orig }()

	v := VerdictSummary{
		Decision:     "pass",
		RiskLevel:    "low",
		Findings:     0,
		CostUSD:      0.0012,
		DurationMS:   5432,
		Suppressions: 0,
	}
	PrintVerdict(v)
	out := buf.String()
	if !strings.Contains(out, "PASS") {
		t.Errorf("expected PASS in verdict output, got: %s", out)
	}
	if !strings.Contains(out, "risk=low") {
		t.Errorf("expected 'risk=low' in verdict output, got: %s", out)
	}
}

func TestPrintVerdictWarn(t *testing.T) {
	var buf bytes.Buffer
	orig := output
	output = &buf
	defer func() { output = orig }()

	v := VerdictSummary{
		Decision:     "warn",
		RiskLevel:    "medium",
		Findings:     3,
		CostUSD:      0.05,
		DurationMS:   12000,
		Suppressions:  2,
	}
	PrintVerdict(v)
	out := buf.String()
	if !strings.Contains(out, "WARN") {
		t.Errorf("expected WARN, got: %s", out)
	}
	if !strings.Contains(out, "suppressions=2") {
		t.Errorf("expected suppressions=2, got: %s", out)
	}
}

func TestPrintFindingsEmpty(t *testing.T) {
	var buf bytes.Buffer
	orig := output
	output = &buf
	defer func() { output = orig }()

	PrintFindings(nil)
	out := buf.String()
	if !strings.Contains(out, "clean") {
		t.Errorf("expected 'clean' for empty findings, got: %s", out)
	}
}

func TestPrintFindingsNonEmpty(t *testing.T) {
	var buf bytes.Buffer
	orig := output
	output = &buf
	defer func() { output = orig }()

	findings := []FindingDisplay{
		{Critic: "security_smell", Severity: "high", Title: "Hardcoded secret", File: "auth.go", Line: 42},
	}
	PrintFindings(findings)
	out := buf.String()
	if !strings.Contains(out, "Hardcoded secret") {
		t.Errorf("expected finding title in output, got: %s", out)
	}
	if !strings.Contains(out, "security_smell") {
		t.Errorf("expected critic name in output, got: %s", out)
	}
	if !strings.Contains(out, "auth.go:42") {
		t.Errorf("expected file:line in output, got: %s", out)
	}
}

func TestPrintHeader(t *testing.T) {
	var buf bytes.Buffer
	orig := output
	output = &buf
	defer func() { output = orig }()

	PrintHeader("1.5.0", "Reviewing origin/main..HEAD — 5 file(s)")
	out := buf.String()
	if !strings.Contains(out, "ACIG") {
		t.Errorf("expected 'ACIG' in header, got: %s", out)
	}
	if !strings.Contains(out, "1.5.0") {
		t.Errorf("expected version in header, got: %s", out)
	}
	if !strings.Contains(out, "5 file") {
		t.Errorf("expected '5 file' in header, got: %s", out)
	}
}

func TestPrintSuccess(t *testing.T) {
	var buf bytes.Buffer
	orig := output
	output = &buf
	defer func() { output = orig }()

	PrintSuccess("All checks passed")
	out := buf.String()
	if !strings.Contains(out, "All checks passed") {
		t.Errorf("expected success message, got: %s", out)
	}
}

func TestPrintWarning(t *testing.T) {
	var buf bytes.Buffer
	orig := output
	output = &buf
	defer func() { output = orig }()

	PrintWarning("Budget low")
	if !strings.Contains(buf.String(), "Budget low") {
		t.Errorf("expected warning message, got: %s", buf.String())
	}
}

func TestPrintError(t *testing.T) {
	var buf bytes.Buffer
	orig := output
	output = &buf
	defer func() { output = orig }()

	PrintError("Connection failed")
	if !strings.Contains(buf.String(), "Connection failed") {
		t.Errorf("expected error message, got: %s", buf.String())
	}
}

func TestSpinnerStartStop(t *testing.T) {
	s := NewSpinner("testing")
	s.out = &bytes.Buffer{}
	s.Start()
	s.Stop()
}

func TestSetupLoggerDiscardsInUI(t *testing.T) {
	orig := os.Getenv("GITHUB_ACTIONS")
	os.Setenv("GITHUB_ACTIONS", "")
	defer os.Setenv("GITHUB_ACTIONS", orig)
	SetupLogger(false)
}

func TestSetupLoggerVerbose(t *testing.T) {
	SetupLogger(true)
}