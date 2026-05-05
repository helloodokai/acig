package routing

import (
	"fmt"
	"os"

	"github.com/helloodokai/acig/internal/config"
	"github.com/helloodokai/acig/internal/models"
)

type Router struct {
	cfg     *config.Config
	clients map[string]models.Client
}

func NewRouter(cfg *config.Config) *Router {
	return &Router{
		cfg:     cfg,
		clients: make(map[string]models.Client),
	}
}

func (r *Router) ClientForTier(tier string) (models.Client, string, error) {
	ref := r.cfg.ModelForTier(tier)
	client, err := r.clientForProvider(ref.Provider, ref.Host)
	if err != nil && r.cfg.Models.FallbackToLocal && ref.Provider == "ollama_cloud" {
		localRef := config.ModelRef{
			Provider: "ollama_local",
			Name:     "qwen2.5-coder:7b",
			Host:     "http://localhost:11434",
		}
		fallback, fallbackErr := r.clientForProvider(localRef.Provider, localRef.Host)
		if fallbackErr != nil {
			return nil, "", fmt.Errorf("cloud error: %w; local fallback also failed: %v", err, fallbackErr)
		}
		return fallback, localRef.Name, nil
	}
	if err != nil {
		return nil, "", err
	}
	return client, ref.Name, nil
}

func (r *Router) clientForProvider(provider, host string) (models.Client, error) {
	if c, ok := r.clients[provider+"/"+host]; ok {
		return c, nil
	}

	var client models.Client
	var err error

	switch provider {
	case "ollama_cloud":
		apiKey := r.cfg.Models.OllamaCloud.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("OLLAMA_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("ollama cloud requires OLLAMA_API_KEY (set env or .acig.toml)")
		}
		h := r.cfg.Models.OllamaCloud.Host
		if h == "" {
			h = "https://ollama.com"
		}
		client = models.NewOllamaClient(h, apiKey)

	case "ollama_local":
		h := host
		if h == "" {
			h = "http://localhost:11434"
		}
		client = models.NewOllamaClient(h, "")

	case "anthropic":
		apiKey := r.cfg.Models.Anthropic.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("anthropic requires ANTHROPIC_API_KEY (set env or .acig.toml)")
		}
		client = models.NewAnthropicClient(apiKey, "")

	case "openai":
		apiKey := r.cfg.Models.OpenAI.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("openai requires OPENAI_API_KEY (set env or .acig.toml)")
		}
		client = models.NewOpenAIClient(apiKey, "")

	default:
		return nil, fmt.Errorf("unknown provider: %s", provider)
	}

	if err != nil {
		return nil, err
	}

	r.clients[provider+"/"+host] = client
	return client, nil
}