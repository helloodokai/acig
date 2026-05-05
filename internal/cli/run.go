package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/helloodokai/acig/internal/budget"
	"github.com/helloodokai/acig/internal/config"
	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/githubclient"
	"github.com/helloodokai/acig/internal/pipeline"
	"github.com/helloodokai/acig/internal/reporters"
	"github.com/helloodokai/acig/internal/routing"
	"github.com/helloodokai/acig/internal/verdict"
)

var runCmd = &cobra.Command{
	Use:   "run",
	Short: "Run the critic pipeline on a diff",
	RunE:  runRun,
}

func init() {
	runCmd.Flags().StringVar(&diffRange, "diff", "", "git diff range (e.g. HEAD~3..HEAD). Default: auto-detect from upstream")
	runCmd.Flags().StringVar(&prRef, "pr", "", "GitHub PR URL or number to review")
	runCmd.Flags().StringVar(&format, "format", "json", "output format: json, md, both")
	runCmd.Flags().StringVar(&outputPath, "out", "", "output file path (without extension; .json/.md added as needed)")
	runCmd.Flags().Float64Var(&budgetUSD, "budget", 0, "per-run budget in USD (overrides config)")
	runCmd.Flags().StringVar(&profile, "profile", "", "profile to use: cloud, local (overrides config)")
	runCmd.Flags().StringVar(&charterPath, "charter", "", "path to charter.yaml for conformance checking")

	rootCmd.AddCommand(runCmd)
}

func runRun(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	if profile != "" {
		cfg.Models.DefaultProfile = profile
	}

	if budgetUSD > 0 {
		cfg.Budget.PerRunUSD = budgetUSD
	}

	if charterPath != "" {
		cfg.Charter.Path = charterPath
	}

	var d *diff.Diff
	var baseSHA string

	if prRef != "" {
		var baseRef string
		d, baseRef, err = diff.FromPR(prRef)
		if err != nil {
			return fmt.Errorf("getting PR diff: %w", err)
		}
		baseSHA = baseRef
		slog.Info("reviewing PR", "pr", prRef, "base", baseRef, "files", d.Stats.FilesChanged)
	} else {
		if diffRange == "" {
			diffRange, err = detectDiffRange()
			if err != nil {
				return fmt.Errorf("detecting diff range: %w", err)
			}
		}

		d, err = diff.FromGitRange(diffRange)
		if err != nil {
			return fmt.Errorf("getting diff: %w", err)
		}
	}

	if d.Stats.FilesChanged == 0 {
		slog.Info("no changes detected, passing")
		v := &verdict.Verdict{
			SchemaVersion:      "1",
			Decision:           verdict.DecisionPass,
			Risk:               verdict.RiskLow,
			Summary:            "No changes detected",
			GeneratedAt:        time.Now().UTC(),
			BudgetRemainingUSD: cfg.Budget.PerRunUSD,
		}
		writeOutput(v)
		os.Exit(0)
		return nil
	}

	ledger, err := budget.NewLedger(cfg.Budget.PerRunUSD)
	if err != nil {
		return fmt.Errorf("initializing budget: %w", err)
	}

	router := routing.NewRouter(cfg)

	repo := detectRepo()
	sha := detectSHA()
	if baseSHA == "" {
		baseSHA = detectBaseSHA()
	}

	slog.Info("running pipeline", "diff_range", diffRange, "files", d.Stats.FilesChanged, "profile", cfg.Models.DefaultProfile)

	pipe := pipeline.New(cfg, router, ledger, d)
	v, err := pipe.Execute(ctx, repo, sha, baseSHA)
	if err != nil {
		return fmt.Errorf("pipeline execution: %w", err)
	}

	slog.Info("verdict", "decision", v.Decision, "risk", v.Risk, "findings", len(v.Findings), "cost_usd", fmt.Sprintf("%.4f", v.TotalCostUSD))

	if shouldReportGitHub() {
		if err := reportToGitHub(ctx, cfg, v); err != nil {
			slog.Error("github report failed", "error", err)
		}
	}

	writeOutput(v)
	os.Exit(exitCodeForDecision(string(v.Decision)))
	return nil
}

func detectDiffRange() (string, error) {
	githubBase := os.Getenv("GITHUB_BASE_REF")
	if githubBase != "" {
		return fmt.Sprintf("%s..HEAD", githubBase), nil
	}
	return diff.AutoDetectRange()
}

func detectRepo() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func detectSHA() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func detectBaseSHA() string {
	githubBase := os.Getenv("GITHUB_BASE_REF")
	if githubBase != "" {
		out, err := exec.Command("git", "rev-parse", githubBase).Output()
		if err != nil {
			return "unknown"
		}
		return strings.TrimSpace(string(out))
	}
	return "unknown"
}



func writeOutput(v *verdict.Verdict) {
	switch format {
	case "json":
		path := outputPath
		if path != "" && !strings.HasSuffix(path, ".json") {
			path += ".json"
		}
		if err := reporters.WriteJSON(v, path); err != nil {
			fatalf("writing JSON: %v", err)
		}
	case "md":
		path := outputPath
		if path != "" && !strings.HasSuffix(path, ".md") {
			path += ".md"
		}
		if err := reporters.WriteMarkdown(v, path); err != nil {
			fatalf("writing markdown: %v", err)
		}
	case "both":
		jsonPath := outputPath
		mdPath := outputPath
		if outputPath != "" {
			if !strings.HasSuffix(jsonPath, ".json") {
				jsonPath += ".json"
			}
			if !strings.HasSuffix(mdPath, ".md") {
				mdPath += ".md"
			}
		}
		if err := reporters.WriteJSON(v, jsonPath); err != nil {
			fatalf("writing JSON: %v", err)
		}
		if err := reporters.WriteMarkdown(v, mdPath); err != nil {
			fatalf("writing markdown: %v", err)
		}
	}
}

func shouldReportGitHub() bool {
	return os.Getenv("GITHUB_ACTIONS") == "true" &&
		os.Getenv("GITHUB_TOKEN") != "" &&
		os.Getenv("GITHUB_EVENT_NAME") == "pull_request"
}

func reportToGitHub(ctx context.Context, cfg *config.Config, v *verdict.Verdict) error {
	repo := os.Getenv("GITHUB_REPOSITORY")
	if repo == "" {
		return fmt.Errorf("GITHUB_REPOSITORY not set")
	}
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid GITHUB_REPOSITORY format: %s", repo)
	}
	owner, name := parts[0], parts[1]

	prNumber, err := detectPRNumber()
	if err != nil {
		return fmt.Errorf("detecting PR number: %w", err)
	}

	client := githubclient.NewClient(os.Getenv("GITHUB_TOKEN"))
	reporter := reporters.NewGitHubReporter(client)
	return reporter.Report(ctx, v, owner, name, prNumber)
}

func detectPRNumber() (int, error) {
	eventPath := os.Getenv("GITHUB_EVENT_PATH")
	if eventPath == "" {
		return 0, fmt.Errorf("GITHUB_EVENT_PATH not set")
	}

	data, err := os.ReadFile(eventPath)
	if err != nil {
		return 0, fmt.Errorf("reading event file: %w", err)
	}

	var event struct {
		Number int `json:"number"`
		PullRequest struct {
			Number int `json:"number"`
		} `json:"pull_request"`
	}
	if err := json.Unmarshal(data, &event); err != nil {
		return 0, fmt.Errorf("parsing event: %w", err)
	}

	if event.PullRequest.Number > 0 {
		return event.PullRequest.Number, nil
	}
	if event.Number > 0 {
		return event.Number, nil
	}
	return 0, fmt.Errorf("no PR number found in event")
}