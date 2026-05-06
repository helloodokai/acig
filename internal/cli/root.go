package cli

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/spf13/cobra"

	_ "github.com/helloodokai/acig/internal/critics"
	"github.com/helloodokai/acig/internal/version"
)

var (
	verbose       bool
	configPath    string
	profile       string
	budgetUSD     float64
	format        string
	outputPath    string
	diffRange     string
	prRef         string
	charterPath   string
	commitSHA     string
	suppressPath  string
)

var rootCmd = &cobra.Command{
	Use:   "acig",
	Short: "Agentic CI Gateway — tiered code review via cheap critics + frontier adjudication",
	Version: version.Version,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if verbose {
			handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})
			slog.SetDefault(slog.New(handler))
		}
	},
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&verbose, "verbose", false, "enable debug logging (JSON to stderr)")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "path to .acig.toml (default: .acig.toml)")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(11)
	}
}

func exitCodeForDecision(decision string, blocking bool) int {
	switch decision {
	case "pass":
		return 0
	case "warn":
		if blocking {
			return 2
		}
		return 0
	case "block":
		return 2
	default:
		return 0
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "error: "+format+"\n", args...)
	os.Exit(11)
}