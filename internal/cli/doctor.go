package cli

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/helloodokai/acig/internal/config"
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check that all configured backends are reachable and API keys are set",
	RunE:  runDoctor,
}

func init() {
	rootCmd.AddCommand(doctorCmd)
}

func runDoctor(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	allOK := true

	fmt.Println("acig doctor — checking backends...")
	fmt.Println()

	ollamaKey := os.Getenv("OLLAMA_API_KEY")
	if ollamaKey == "" {
		ollamaKey = cfg.Models.OllamaCloud.APIKey
	}

	fmt.Print("  Ollama Cloud: ")
	if ollamaKey == "" {
		fmt.Println("MISSING — OLLAMA_API_KEY not set")
		allOK = false
	} else {
		if checkEndpoint(ctx, cfg.Models.OllamaCloud.Host+"/api/tags", ollamaKey) {
			fmt.Println("OK")
		} else {
			fmt.Println("UNREACHABLE — check network/key")
			allOK = false
		}
	}

	fmt.Print("  Local Ollama: ")
	localHost := "http://localhost:11434"
	if checkEndpoint(ctx, localHost+"/api/tags", "") {
		fmt.Println("OK")
	} else {
		fmt.Println("UNREACHABLE — is ollama serve running?")
		if !cfg.Models.FallbackToLocal {
			// not a failure if not used
			slog.Info("local ollama not required")
		}
	}

	fmt.Print("  Anthropic: ")
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		fmt.Println("API key set")
	} else {
		fmt.Println("MISSING — ANTHROPIC_API_KEY not set (frontier tier unavailable)")
	}

	fmt.Print("  OpenAI: ")
	if os.Getenv("OPENAI_API_KEY") != "" {
		fmt.Println("API key set")
	} else {
		fmt.Println("MISSING — OPENAI_API_KEY not set (optional)")
	}

	fmt.Print("  Git: ")
	if isGitRepo() {
		fmt.Println("OK")
	} else {
		fmt.Println("NOT A GIT REPO")
		allOK = false
	}

	fmt.Println()

	if allOK {
		fmt.Println("All required checks passed.")
	} else {
		fmt.Println("Some checks failed. See above for details.")
	}

	return nil
}

func checkEndpoint(ctx context.Context, url, apiKey string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		slog.Debug("endpoint check failed", "url", url, "error", err)
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 400
}

func isGitRepo() bool {
	_, err := os.Stat(".git")
	return err == nil
}