package config

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
)

type Config struct {
	Budget    BudgetConfig  `toml:"budget"`
	Models    ModelsConfig   `toml:"models"`
	Critics   CriticsConfig  `toml:"critics"`
	Charter   CharterConfig  `toml:"charter"`
	Paths     PathsConfig    `toml:"paths"`
	Blocking BlockingConfig `toml:"blocking"`
}

type BudgetConfig struct {
	PerRunUSD float64 `toml:"per_run_usd"`
}

type BlockingConfig struct {
	Enabled bool `toml:"enabled"`
}

type ModelsConfig struct {
	DefaultProfile   string                    `toml:"default_profile"`
	FallbackToLocal  bool                      `toml:"fallback_to_local"`
	Profiles         map[string]ProfileConfig   `toml:"profiles"`
	OllamaCloud      OllamaCloudConfig         `toml:"ollama_cloud"`
	Anthropic        ProviderConfig            `toml:"anthropic"`
	OpenAI           ProviderConfig            `toml:"openai"`
}

type ProfileConfig struct {
	Cheap    ModelRef `toml:"cheap"`
	Mid      ModelRef `toml:"mid"`
	Frontier ModelRef `toml:"frontier"`
}

type ModelRef struct {
	Provider string `toml:"provider"`
	Name     string `toml:"name"`
	Host     string `toml:"host,omitempty"`
}

type OllamaCloudConfig struct {
	Host   string `toml:"host"`
	APIKey string `toml:"api_key"`
}

type ProviderConfig struct {
	APIKey string `toml:"api_key"`
}

type CriticsConfig struct {
	Enabled    []string        `toml:"enabled"`
	Adjudicator AdjudicatorCfg `toml:"adjudicator"`
}

type AdjudicatorCfg struct {
	TriggerOn []string `toml:"trigger_on"`
}

type PathsConfig struct {
	Critical []string `toml:"critical"`
}

type CharterConfig struct {
	Path string `toml:"path"`
	Auto bool   `toml:"auto"`
}

func Default() *Config {
	return &Config{
		Budget: BudgetConfig{
			PerRunUSD: 0.25,
		},
		Models: ModelsConfig{
			DefaultProfile:  "cloud",
			FallbackToLocal: true,
			Profiles: map[string]ProfileConfig{
				"cloud": {
					Cheap:    ModelRef{Provider: "ollama_cloud", Name: "gpt-oss:20b"},
					Mid:      ModelRef{Provider: "ollama_cloud", Name: "qwen3-coder:480b"},
					Frontier: ModelRef{Provider: "ollama_cloud", Name: "glm-5.1"},
				},
				"local": {
					Cheap:    ModelRef{Provider: "ollama_local", Name: "qwen2.5-coder:7b", Host: "http://localhost:11434"},
					Mid:      ModelRef{Provider: "ollama_local", Name: "qwen2.5-coder:32b", Host: "http://localhost:11434"},
					Frontier: ModelRef{Provider: "ollama_cloud", Name: "glm-5.1"},
				},
			},
			OllamaCloud: OllamaCloudConfig{
				Host:   "https://ollama.com",
				APIKey: "${OLLAMA_API_KEY}",
			},
			Anthropic: ProviderConfig{
				APIKey: "${ANTHROPIC_API_KEY}",
			},
			OpenAI: ProviderConfig{
				APIKey: "${OPENAI_API_KEY}",
			},
		},
		Critics: CriticsConfig{
			Enabled: []string{"risk_classifier", "style_conformance", "test_coverage_smell", "security_smell", "perf_smell"},
			Adjudicator: AdjudicatorCfg{
				TriggerOn: []string{"risk:high", "risk:critical", "conflict"},
			},
		},
		Blocking: BlockingConfig{
			Enabled: true,
		},
	}
}

func Load(path string) (*Config, error) {
	cfg := Default()
	if path == "" {
		path = ".acig.toml"
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg.interpolate()
			cfg.resolveAPIKeys()
			return cfg, nil
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}

	if err := toml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}

	cfg.interpolate()
	cfg.resolveAPIKeys()

	return cfg, nil
}

func (c *Config) interpolate() {
	c.Models.OllamaCloud.APIKey = expandEnv(c.Models.OllamaCloud.APIKey)
	c.Models.Anthropic.APIKey = expandEnv(c.Models.Anthropic.APIKey)
	c.Models.OpenAI.APIKey = expandEnv(c.Models.OpenAI.APIKey)
}

func (c *Config) resolveAPIKeys() {
	if c.Models.OllamaCloud.APIKey == "" {
		c.Models.OllamaCloud.APIKey = os.Getenv("OLLAMA_API_KEY")
	}
	if c.Models.Anthropic.APIKey == "" {
		c.Models.Anthropic.APIKey = os.Getenv("ANTHROPIC_API_KEY")
	}
	if c.Models.OpenAI.APIKey == "" {
		c.Models.OpenAI.APIKey = os.Getenv("OPENAI_API_KEY")
	}
}

func expandEnv(s string) string {
	return os.Expand(s, func(key string) string {
		if val, ok := os.LookupEnv(key); ok {
			return val
		}
		if strings.HasPrefix(s, "${"+key+"}") {
			return ""
		}
		return ""
	})
}

func (c *Config) Profile() string { return c.Models.DefaultProfile }

func (c *Config) ModelForTier(tier string) ModelRef {
	profile := c.Models.Profiles[c.Models.DefaultProfile]
	switch tier {
	case "cheap":
		return profile.Cheap
	case "mid":
		return profile.Mid
	case "frontier":
		return profile.Frontier
	default:
		return profile.Cheap
	}
}