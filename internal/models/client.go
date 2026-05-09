package models

import (
	"context"
	"encoding/json"
)

type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ChatRequest struct {
	Model       string
	Messages    []ChatMessage
	MaxTokens   int
	Temperature float64
	JSONMode    bool
	// JSONSchema, when set, asks the backend to constrain output to the given
	// JSON Schema. Ollama (recent versions) honors this via the `format` field;
	// other backends fall back to JSONMode-only.
	JSONSchema json.RawMessage
	// Stop, when non-empty, sets the stop sequences for the request. Ollama
	// honors this via options.stop; other backends ignore it for now.
	Stop []string
	// TopP overrides nucleus sampling. 0 means provider default.
	TopP float64
}

type ChatResponse struct {
	Content    string
	TokensIn   int
	TokensOut  int
	Model      string
	RawCostUSD float64
}

type Client interface {
	Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error)
	Name() string
}

func EstimateTokens(text string) int {
	if len(text) == 0 {
		return 0
	}
	return len(text) / 4
}
