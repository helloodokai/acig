package critics

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/verdict"
)

// LookPathFunc is the function signature for finding executables.
// Defaults to exec.LookPath but can be overridden in tests.
var LookPathFunc = exec.LookPath

// CommandBuilder creates an exec.Cmd. Defaults to exec.CommandContext but can
// be overridden in tests to inject mock behavior.
var CommandBuilder = func(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}

// CharterConformance checks a diff against a charter.yaml specification for conformance.
// It shells out to the charter binary and maps findings into acig's verdict shape.
type CharterConformance struct {
	baseCritic
}

func init() {
	Register(&CharterConformance{
		baseCritic: baseCritic{id: "charter_conformance", tier: TierCheap},
	})
}

// Run executes the charter conformance check by shelling out to the charter binary.
func (cc *CharterConformance) Run(ctx context.Context, d *diff.Diff, pc *Context) (*verdict.CriticResult, error) {
	start := time.Now()

	charterPath := resolveCharterPath(pc)
	if charterPath == "" {
		slog.Debug("charter_conformance: no charter referenced, skipping")
		return &verdict.CriticResult{
			Critic:     cc.id,
			DurationMS: time.Since(start).Milliseconds(),
		}, nil
	}

	if _, err := os.Stat(charterPath); err != nil {
		slog.Warn("charter_conformance: charter file not found", "path", charterPath, "error", err)
		return &verdict.CriticResult{
			Critic:     cc.id,
			DurationMS: time.Since(start).Milliseconds(),
			Error:      fmt.Sprintf("charter file not found: %s", charterPath),
		}, nil
	}

	charterBin, err := LookPathFunc("charter")
	if err != nil {
		slog.Debug("charter_conformance: charter binary not found in PATH, skipping")
		return &verdict.CriticResult{
			Critic:     cc.id,
			DurationMS: time.Since(start).Milliseconds(),
		}, nil
	}

	diffContent := d.RawPatch

	cmd := CommandBuilder(ctx, charterBin, "conformance", charterPath, "--diff", "-", "--format", "json")
	cmd.Stdin = strings.NewReader(diffContent)

	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			slog.Warn("charter_conformance: charter binary failed", "exit_code", exitErr.ExitCode(), "stderr", string(exitErr.Stderr))
			return &verdict.CriticResult{
				Critic:     cc.id,
				DurationMS: time.Since(start).Milliseconds(),
				Error:      fmt.Sprintf("charter conformance exited %d: %s", exitErr.ExitCode(), strings.TrimSpace(string(exitErr.Stderr))),
			}, nil
		}
		return nil, fmt.Errorf("running charter conformance: %w", err)
	}

	var cv charterVerdict
	if err := json.Unmarshal(output, &cv); err != nil {
		slog.Warn("charter_conformance: failed to parse charter output", "error", err)
		return &verdict.CriticResult{
			Critic:     cc.id,
			DurationMS: time.Since(start).Milliseconds(),
			Error:      fmt.Sprintf("failed to parse charter output: %v", err),
		}, nil
	}

	findings := convertCharterFindings(cv)

	slog.Info("charter_conformance: completed", "charter_id", cv.CharterID, "status", cv.Status, "findings", len(findings), "score", cv.Score)

	return &verdict.CriticResult{
		Critic:     cc.id,
		Findings:   findings,
		DurationMS: time.Since(start).Milliseconds(),
	}, nil
}

func resolveCharterPath(pc *Context) string {
	if pc.Config != nil && pc.Config.Charter.Path != "" {
		return pc.Config.Charter.Path
	}
	return ""
}

type charterVerdict struct {
	CharterID string            `json:"charter_id"`
	Goal      string            `json:"goal"`
	Status    string            `json:"status"`
	Score     float64           `json:"score"`
	Findings  []charterFinding  `json:"findings"`
}

type charterFinding struct {
	Critic     string `json:"critic"`
	Severity   string `json:"severity"`
	Message    string `json:"message"`
	Detail     string `json:"detail,omitempty"`
	CharterRef string `json:"charter_ref,omitempty"`
	File       string `json:"file,omitempty"`
}

func convertCharterFindings(cv charterVerdict) []verdict.Finding {
	var findings []verdict.Finding
	for _, cf := range cv.Findings {
		findings = append(findings, verdict.Finding{
			Critic:       cf.Critic,
			Severity:     mapCharterSeverity(cf.Severity),
			Title:        cf.Message,
			Detail:       cf.Detail,
			File:         cf.File,
			SuggestedFix: charterRefToSuggestion(cf.CharterRef, cv.CharterID),
		})
	}

	if cv.Status == "fail" && len(findings) == 0 {
		findings = append(findings, verdict.Finding{
			Critic:   "charter:conformance",
			Severity: verdict.SeverityHigh,
			Title:    "Charter conformance check failed",
			Detail:   fmt.Sprintf("Charter %s failed conformance (score: %.1f) but no specific findings were returned.", cv.CharterID, cv.Score),
		})
	}

	return findings
}

func mapCharterSeverity(s string) verdict.Severity {
	switch strings.ToLower(s) {
	case "blocking":
		return verdict.SeverityBlocking
	case "error", "high":
		return verdict.SeverityHigh
	case "warning", "medium":
		return verdict.SeverityMedium
	case "info", "low":
		return verdict.SeverityLow
	default:
		return verdict.SeverityInfo
	}
}

func charterRefToSuggestion(ref, charterID string) string {
	if ref == "" || charterID == "" {
		return ""
	}
	return fmt.Sprintf("See %s in charter %s", ref, charterID)
}