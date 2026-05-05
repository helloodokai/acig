package critics

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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

func TestChartersConformanceSkipsWithoutPath(t *testing.T) {
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

func TestChartersConformanceMissingFile(t *testing.T) {
	cc := &CharterConformance{baseCritic: baseCritic{id: "charter_conformance", tier: TierCheap}}
	pc := &Context{
		Config: &config.Config{
			Charter: config.CharterConfig{
				Path: "/nonexistent/ch-2026-05-04-test.yaml",
			},
		},
		Diff: &diff.Diff{RawPatch: ""},
	}
	result, err := cc.Run(context.Background(), pc.Diff, pc)
	require.NoError(t, err)
	require.Equal(t, "charter_conformance", result.Critic)
	require.Empty(t, result.Findings)
	require.Contains(t, result.Error, "charter file not found")
	require.Contains(t, result.Error, "/nonexistent/ch-2026-05-04-test.yaml")
}

func TestCharterConformanceMissingBinary(t *testing.T) {
	origLookPath := LookPathFunc
	defer func() { LookPathFunc = origLookPath }()

	LookPathFunc = func(file string) (string, error) {
		return "", fmt.Errorf("executable not found in $PATH: charter")
	}

	cc := &CharterConformance{baseCritic: baseCritic{id: "charter_conformance", tier: TierCheap}}

	tmp := t.TempDir()
	charterFile := tmp + "/ch-test.yaml"
	require.NoError(t, createMinimalCharterFile(charterFile))

	pc := &Context{
		Config: &config.Config{
			Charter: config.CharterConfig{Path: charterFile},
		},
		Diff: &diff.Diff{RawPatch: ""},
	}

	result, err := cc.Run(context.Background(), pc.Diff, pc)
	require.NoError(t, err)
	require.Equal(t, "charter_conformance", result.Critic)
	require.Empty(t, result.Findings)
	require.Empty(t, result.Error, "missing binary should skip silently, no error")
}

func TestCharterConformanceExitError(t *testing.T) {
	origLookPath := LookPathFunc
	origCmdBuilder := CommandBuilder
	defer func() {
		LookPathFunc = origLookPath
		CommandBuilder = origCmdBuilder
	}()

	LookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/charter", nil
	}

	CommandBuilder = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "echo 'some stderr' >&2; exit 2")
	}

	cc := &CharterConformance{baseCritic: baseCritic{id: "charter_conformance", tier: TierCheap}}

	tmp := t.TempDir()
	charterFile := tmp + "/ch-test.yaml"
	require.NoError(t, createMinimalCharterFile(charterFile))

	pc := &Context{
		Config: &config.Config{
			Charter: config.CharterConfig{Path: charterFile},
		},
		Diff: &diff.Diff{RawPatch: "diff content"},
	}

	result, err := cc.Run(context.Background(), pc.Diff, pc)
	require.NoError(t, err)
	require.Equal(t, "charter_conformance", result.Critic)
	require.NotEmpty(t, result.Error)
	require.Contains(t, result.Error, "charter conformance exited 2")
}

func TestCharterConformanceGenericError(t *testing.T) {
	origLookPath := LookPathFunc
	origCmdBuilder := CommandBuilder
	defer func() {
		LookPathFunc = origLookPath
		CommandBuilder = origCmdBuilder
	}()

	LookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/charter", nil
	}

	CommandBuilder = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "nonexistent_binary_for_test")
	}

	cc := &CharterConformance{baseCritic: baseCritic{id: "charter_conformance", tier: TierCheap}}

	tmp := t.TempDir()
	charterFile := tmp + "/ch-test.yaml"
	require.NoError(t, createMinimalCharterFile(charterFile))

	pc := &Context{
		Config: &config.Config{
			Charter: config.CharterConfig{Path: charterFile},
		},
		Diff: &diff.Diff{RawPatch: "diff content"},
	}

	_, err := cc.Run(context.Background(), pc.Diff, pc)
	require.Error(t, err)
	require.Contains(t, err.Error(), "running charter conformance")
}

func TestChartersConformanceInvalidJSON(t *testing.T) {
	origLookPath := LookPathFunc
	origCmdBuilder := CommandBuilder
	defer func() {
		LookPathFunc = origLookPath
		CommandBuilder = origCmdBuilder
	}()

	LookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/charter", nil
	}

	CommandBuilder = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", "echo 'this is not json'")
	}

	cc := &CharterConformance{baseCritic: baseCritic{id: "charter_conformance", tier: TierCheap}}

	tmp := t.TempDir()
	charterFile := tmp + "/ch-test.yaml"
	require.NoError(t, createMinimalCharterFile(charterFile))

	pc := &Context{
		Config: &config.Config{
			Charter: config.CharterConfig{Path: charterFile},
		},
		Diff: &diff.Diff{RawPatch: "diff content"},
	}

	result, err := cc.Run(context.Background(), pc.Diff, pc)
	require.NoError(t, err)
	require.Equal(t, "charter_conformance", result.Critic)
	require.NotEmpty(t, result.Error)
	require.Contains(t, result.Error, "failed to parse charter output")
}

func TestCharterConformanceValidJSON(t *testing.T) {
	origLookPath := LookPathFunc
	origCmdBuilder := CommandBuilder
	defer func() {
		LookPathFunc = origLookPath
		CommandBuilder = origCmdBuilder
	}()

	LookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/charter", nil
	}

	cv := charterVerdict{
		CharterID: "ch-2026-05-04-test",
		Goal:      "Add login page",
		Status:    "pass",
		Score:     1.0,
		Findings:  []charterFinding{},
	}
	cvJSON, _ := json.Marshal(cv)

	CommandBuilder = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("echo '%s'", string(cvJSON)))
	}

	cc := &CharterConformance{baseCritic: baseCritic{id: "charter_conformance", tier: TierCheap}}

	tmp := t.TempDir()
	charterFile := tmp + "/ch-test.yaml"
	require.NoError(t, createMinimalCharterFile(charterFile))

	pc := &Context{
		Config: &config.Config{
			Charter: config.CharterConfig{Path: charterFile},
		},
		Diff: &diff.Diff{RawPatch: "diff content"},
	}

	result, err := cc.Run(context.Background(), pc.Diff, pc)
	require.NoError(t, err)
	require.Equal(t, "charter_conformance", result.Critic)
	require.Empty(t, result.Error)
	require.Empty(t, result.Findings, "pass with empty findings")
}

func TestCharterConformanceValidJSONWithFindings(t *testing.T) {
	origLookPath := LookPathFunc
	origCmdBuilder := CommandBuilder
	defer func() {
		LookPathFunc = origLookPath
		CommandBuilder = origCmdBuilder
	}()

	LookPathFunc = func(file string) (string, error) {
		return "/usr/local/bin/charter", nil
	}

	cv := charterVerdict{
		CharterID: "ch-2026-05-04-test",
		Goal:      "Add login page",
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
				Critic:   "charter:unknown_gating",
				Severity: "blocking",
				Message:  "blocking unknown found",
				Detail:   "what API version?",
			},
		},
	}
	cvJSON, _ := json.Marshal(cv)

	CommandBuilder = func(ctx context.Context, name string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, "sh", "-c", fmt.Sprintf("echo '%s'", string(cvJSON)))
	}

	cc := &CharterConformance{baseCritic: baseCritic{id: "charter_conformance", tier: TierCheap}}

	tmp := t.TempDir()
	charterFile := tmp + "/ch-test.yaml"
	require.NoError(t, createMinimalCharterFile(charterFile))

	pc := &Context{
		Config: &config.Config{
			Charter: config.CharterConfig{Path: charterFile},
		},
		Diff: &diff.Diff{RawPatch: "diff content"},
	}

	result, err := cc.Run(context.Background(), pc.Diff, pc)
	require.NoError(t, err)
	require.Equal(t, "charter_conformance", result.Critic)
	require.Len(t, result.Findings, 2)
	require.Equal(t, verdict.SeverityMedium, result.Findings[0].Severity)
	require.Equal(t, verdict.SeverityBlocking, result.Findings[1].Severity)
	require.Contains(t, result.Findings[0].SuggestedFix, "ch-2026-05-04-test")
}

func TestConvertCharterFindingsFailWithNoFindings(t *testing.T) {
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

func TestResolveCharterPath(t *testing.T) {
	require.Equal(t, resolveCharterPath(&Context{Config: nil}), "")
	require.Equal(t, resolveCharterPath(&Context{Config: &config.Config{}}), "")
	require.Equal(t, ".charters/test.yaml", resolveCharterPath(&Context{
		Config: &config.Config{Charter: config.CharterConfig{Path: ".charters/test.yaml"}},
	}))
}

func TestCharterRefToSuggestion(t *testing.T) {
	require.Equal(t, "", charterRefToSuggestion("", ""))
	require.Equal(t, "", charterRefToSuggestion("ref", ""))
	require.Equal(t, "", charterRefToSuggestion("", "ch-test"))
	require.Contains(t, charterRefToSuggestion("blast_radius.files", "ch-test"), "blast_radius.files")
	require.Contains(t, charterRefToSuggestion("blast_radius.files", "ch-test"), "ch-test")
}

func createMinimalCharterFile(path string) error {
	content := []byte("schema_version: \"1\"\nid: ch-test\ngoal: Test goal\nstatus: ready\n")
	return os.WriteFile(path, content, 0644)
}