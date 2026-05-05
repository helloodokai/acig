package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/helloodokai/acig/internal/verdict"
)

var explainCmd = &cobra.Command{
	Use:   "explain <verdict.json>",
	Short: "Pretty-print a verdict for humans",
	Args:  cobra.ExactArgs(1),
	RunE:  runExplain,
}

func init() {
	rootCmd.AddCommand(explainCmd)
}

func runExplain(cmd *cobra.Command, args []string) error {
	data, err := os.ReadFile(args[0])
	if err != nil {
		return fmt.Errorf("reading verdict file: %w", err)
	}

	var v verdict.Verdict
	if err := json.Unmarshal(data, &v); err != nil {
		return fmt.Errorf("parsing verdict: %w", err)
	}

	fmt.Printf("=== Acig Verdict ===\n\n")
	fmt.Printf("Decision:   %s\n", v.Decision)
	fmt.Printf("Risk:       %s\n", v.Risk)
	fmt.Printf("SHA:        %s\n", v.SHA)
	fmt.Printf("Base SHA:   %s\n", v.BaseSHA)
	fmt.Printf("Cost:       $%.4f\n", v.TotalCostUSD)
	fmt.Printf("Duration:   %dms\n", v.TotalDurationMS)
	fmt.Printf("Budget:     $%.4f remaining\n", v.BudgetRemainingUSD)
	fmt.Printf("\n%s\n\n", v.Summary)

	if len(v.Findings) == 0 {
		fmt.Println("No findings.")
	} else {
		fmt.Printf("Findings (%d):\n\n", len(v.Findings))
		for i, f := range v.Findings {
			fmt.Printf("  %d. [%s/%s] %s\n", i+1, f.Critic, f.Severity, f.Title)
			fmt.Printf("     %s\n", f.Detail)
			if f.File != "" {
				fmt.Printf("     @ %s:%d\n", f.File, f.LineStart)
			}
			if f.SuggestedFix != "" {
				fmt.Printf("     Fix: %s\n", f.SuggestedFix)
			}
			fmt.Println()
		}
	}

	fmt.Printf("\nCritic results: %d critics ran\n", len(v.CriticResults))
	for _, cr := range v.CriticResults {
		errStr := ""
		if cr.Error != "" {
			errStr = fmt.Sprintf(" (error: %s)", cr.Error)
		}
		fmt.Printf("  - %s [%s]: %d findings, $%.4f, %dms%s\n",
			cr.Critic, cr.Model, len(cr.Findings), cr.CostUSD, cr.DurationMS, errStr)
	}

	return nil
}