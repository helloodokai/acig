package config

import (
	"os"
	"testing"
)

func TestDefault(t *testing.T) {
	cfg := Default()
	if cfg.Budget.PerRunUSD != 0.25 {
		t.Errorf("default budget = %f, want 0.25", cfg.Budget.PerRunUSD)
	}
	if cfg.Models.DefaultProfile != "cloud" {
		t.Errorf("default profile = %s, want cloud", cfg.Models.DefaultProfile)
	}
	if !cfg.Models.FallbackToLocal {
		t.Error("fallback to local should be true by default")
	}
}

func TestLoadMissing(t *testing.T) {
	cfg, err := Load("/nonexistent/path.acig.toml")
	if err != nil {
		t.Fatalf("Load missing file: %v", err)
	}
	if cfg.Budget.PerRunUSD != 0.25 {
		t.Errorf("should return defaults, got budget = %f", cfg.Budget.PerRunUSD)
	}
}

func TestLoadFromData(t *testing.T) {
	content := `
[budget]
per_run_usd = 0.50

[models]
default_profile = "local"
fallback_to_local = false
`
	tmpFile, err := os.CreateTemp("", "acig-test-*.toml")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	cfg, err := Load(tmpFile.Name())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Budget.PerRunUSD != 0.50 {
		t.Errorf("budget = %f, want 0.50", cfg.Budget.PerRunUSD)
	}
	if cfg.Models.DefaultProfile != "local" {
		t.Errorf("profile = %s, want local", cfg.Models.DefaultProfile)
	}
}

func TestModelForTier(t *testing.T) {
	cfg := Default()

	cheap := cfg.ModelForTier("cheap")
	if cheap.Provider != "ollama_cloud" {
		t.Errorf("cheap provider = %s, want ollama_cloud", cheap.Provider)
	}

	frontier := cfg.ModelForTier("frontier")
	if frontier.Provider != "anthropic" {
		t.Errorf("frontier provider = %s, want anthropic", frontier.Provider)
	}
}

func TestExpandEnv(t *testing.T) {
	os.Setenv("TEST_ACIG_KEY", "secret123")
	defer os.Unsetenv("TEST_ACIG_KEY")

	result := expandEnv("${TEST_ACIG_KEY}")
	if result != "secret123" {
		t.Errorf("expandEnv = %s, want secret123", result)
	}

	result = expandEnv("${NONEXISTENT_VAR}")
	if result != "" {
		t.Errorf("expandEnv for missing var = %s, want empty", result)
	}
}