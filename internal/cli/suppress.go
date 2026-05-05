package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"

	"github.com/helloodokai/acig/internal/verdict"
)

var suppressCmd = &cobra.Command{
	Use:   "suppress [finding-index...]",
	Short: "Suppress findings from the last acig run",
	Long: `Suppress findings from the last acig run.

Reads the most recent verdict JSON and adds the specified findings
to .acig-suppressions.toml so they are ignored on subsequent runs.

Pass finding indices (0-based) from the verdict, or use --all to
suppress all findings. Use --reason to attach a reason.

Examples:
  acig suppress 0 2 5           # suppress findings 0, 2, 5
  acig suppress --all --reason "acceptable tech debt"  # suppress all
  acig suppress --list           # list current suppressions`,
	RunE: runSuppress,
}

var (
	suppressAll   bool
	suppressReason string
	suppressList   bool
)

func init() {
	suppressCmd.Flags().BoolVar(&suppressAll, "all", false, "suppress all findings from the last run")
	suppressCmd.Flags().StringVar(&suppressReason, "reason", "", "reason for suppression")
	suppressCmd.Flags().BoolVar(&suppressList, "list", false, "list current suppressions")
	rootCmd.AddCommand(suppressCmd)
}

func runSuppress(cmd *cobra.Command, args []string) error {
	suppressionsPath := detectSuppressionsPath()

	if suppressList {
		existing, err := verdict.LoadSuppressions(suppressionsPath)
		if err != nil {
			return fmt.Errorf("loading suppressions: %w", err)
		}
		if len(existing) == 0 {
			fmt.Println("No suppressions found.")
			return nil
		}
		fmt.Printf("Suppressions (%s):\n", suppressionsPath)
		for i, s := range existing {
			reason := s.Reason
			if reason == "" {
				reason = "(no reason)"
			}
			fmt.Printf("  %d: critic=%s title=%s file=%s reason=%s", i, s.Critic, s.Title, s.File, reason)
			if s.Expires != "" {
				fmt.Printf(" expires=%s", s.Expires)
			}
			fmt.Println()
		}
		return nil
	}

	verdictPath := lastVerdictPath()
	data, err := os.ReadFile(verdictPath)
	if err != nil {
		return fmt.Errorf("reading verdict %s: %w\nhint: run 'acig run' first to generate a verdict", verdictPath, err)
	}

	var v verdict.Verdict
	if unmarshalErr := json.Unmarshal(data, &v); unmarshalErr != nil {
		return fmt.Errorf("parsing verdict: %w", unmarshalErr)
	}

	if len(v.Findings) == 0 {
		fmt.Println("No findings to suppress.")
		return nil
	}

	var indices []int
	if suppressAll {
		for i := range v.Findings {
			indices = append(indices, i)
		}
	} else {
		for _, arg := range args {
			idx := 0
			if _, scanErr := fmt.Sscanf(arg, "%d", &idx); scanErr != nil {
				return fmt.Errorf("invalid index %q: %w", arg, scanErr)
			}
			if idx < 0 || idx >= len(v.Findings) {
				return fmt.Errorf("index %d out of range (0-%d)", idx, len(v.Findings)-1)
			}
			indices = append(indices, idx)
		}
	}

	if len(indices) == 0 {
		fmt.Println("Findings from last run:")
		for i, f := range v.Findings {
			fmt.Printf("  %d: [%s] %s (%s) — %s\n", i, f.Severity, f.Title, f.Critic, f.File)
		}
		fmt.Println("\nPass finding indices or --all to suppress.")
		return nil
	}

	existing, err := verdict.LoadSuppressions(suppressionsPath)
	if err != nil {
		return fmt.Errorf("loading existing suppressions: %w", err)
	}

	added := 0
	for _, idx := range indices {
		f := v.Findings[idx]
		dup := false
		for _, s := range existing {
			if s.Critic == f.Critic && s.Title == f.Title && s.File == f.File {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		existing = append(existing, verdict.Suppression{
			Critic: f.Critic,
			Title:  f.Title,
			File:   f.File,
			Reason: suppressReason,
		})
		added++
	}

	if err := verdict.SaveSuppressions(suppressionsPath, existing); err != nil {
		return fmt.Errorf("saving suppressions: %w", err)
	}

	fmt.Printf("Added %d suppression(s) to %s\n", added, suppressionsPath)
	return nil
}

func lastVerdictPath() string {
	if outputPath != "" {
		p := outputPath
		if !strings.HasSuffix(p, ".json") {
			p += ".json"
		}
		return p
	}
	for _, candidate := range []string{
		"/tmp/acig-pre-push.json",
		"/tmp/acig-review.json",
	} {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return "/tmp/acig-review.json"
}