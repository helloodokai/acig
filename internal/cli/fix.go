package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	"github.com/helloodokai/acig/internal/config"
	"github.com/helloodokai/acig/internal/fix"
)

var fixCmd = &cobra.Command{
	Use:   "fix",
	Short: "Auto-fix findings from code review",
	Long:  "Run the critic pipeline and automatically create fixes for findings as a PR.",
	RunE:  runFix,
}

var (
	fixDryRun    bool
	fixNoPush    bool
	fixMaxIter   int
	fixBranchPfx string
)

func init() {
	fixCmd.Flags().StringVar(&diffRange, "diff", "", "git diff range (e.g. HEAD~3..HEAD or origin/main..HEAD)")
	fixCmd.Flags().StringVar(&prRef, "pr", "", "GitHub PR URL or number to fix")
	fixCmd.Flags().StringVar(&profile, "profile", "", "profile to use: cloud, local (overrides config)")
	fixCmd.Flags().Float64Var(&budgetUSD, "budget", 0, "per-run budget in USD (overrides config)")
	fixCmd.Flags().BoolVar(&fixDryRun, "dry-run", false, "show patches without applying them")
	fixCmd.Flags().BoolVar(&fixNoPush, "no-push", false, "apply fixes but don't push or create PR")
	fixCmd.Flags().IntVar(&fixMaxIter, "max-iterations", 10, "maximum number of fix iterations")
	fixCmd.Flags().StringVar(&fixBranchPfx, "branch-prefix", "acig-fix", "prefix for the fix branch name")

	rootCmd.AddCommand(fixCmd)
}

func runFix(cmd *cobra.Command, args []string) error {
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

	if prRef != "" && diffRange == "" {
		slog.Info("fetching diff from PR", "pr", prRef)
	}

	if prRef != "" {
		diffRange = "pr:" + prRef
	} else if diffRange == "" {
		diffRange, err = detectDiffRange()
		if err != nil {
			return fmt.Errorf("detecting diff range: %w", err)
		}
	}

	opts := fix.Options{
		Profile:      profile,
		BudgetUSD:    budgetUSD,
		DiffRange:    diffRange,
		MaxIter:      fixMaxIter,
		DryRun:       fixDryRun,
		NoPush:       fixNoPush,
		BranchPrefix: fixBranchPfx,
	}

	slog.Info("running fix pipeline", "diff", diffRange, "dry_run", fixDryRun)

	results, err := fix.Run(ctx, cfg, opts)
	if err != nil {
		return fmt.Errorf("fix pipeline: %w", err)
	}

	if len(results) == 0 {
		slog.Info("no fixes needed")
		return nil
	}

	applied := 0
	for _, r := range results {
		status := "skipped"
		if r.Applied {
			applied++
			status = "applied"
		} else if r.Error != "" {
			status = "error"
		}
		slog.Info("fix result", "file", r.Group.File, "findings", len(r.Group.Findings), "status", status)
		if fixDryRun && r.Patch != "" {
			fmt.Fprintf(os.Stderr, "\n--- %s ---\n%s\n", r.Group.File, r.Patch)
		}
	}

	slog.Info("fix summary", "applied", applied, "total", len(results))

	if applied > 0 && !fixDryRun && !fixNoPush {
		if err := fix.CreatePR(ctx, results, opts); err != nil {
			slog.Error("PR creation failed", "error", err)
		}
	}

	return nil
}