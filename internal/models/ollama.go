package models

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type OllamaClient struct {
	host       string
	apiKey     string
	httpClient *http.Client
	isCloud    bool
}

func NewOllamaClient(host, apiKey string) *OllamaClient {
	isCloud := !isLocalHost(host)
	return &OllamaClient{
		host:   strings.TrimRight(host, "/"),
		apiKey: apiKey,
		httpClient: &http.Client{
			Timeout: 180 * time.Second,
		},
		isCloud: isCloud,
	}
}

func (c *OllamaClient) Name() string {
	if c.isCloud {
		return "ollama_cloud"
	}
	return "ollama_local"
}

func (c *OllamaClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	return c.chatNative(ctx, req)
}

func (c *OllamaClient) chatNative(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	url := c.host + "/api/chat"

	options := map[string]any{
		"temperature": req.Temperature,
		"num_predict": req.MaxTokens,
	}
	if req.TopP > 0 {
		options["top_p"] = req.TopP
	}
	if len(req.Stop) > 0 {
		options["stop"] = req.Stop
	}

	body := map[string]any{
		"model":    req.Model,
		"messages": req.Messages,
		"stream":   false,
		"options":  options,
	}

	// JSONSchema (if provided) takes precedence over plain JSONMode: Ollama
	// accepts a JSON Schema object directly in `format` to constrain output.
	if len(req.JSONSchema) > 0 {
		var schema any
		if err := json.Unmarshal(req.JSONSchema, &schema); err == nil {
			body["format"] = schema
		} else {
			// Fall back to plain JSON mode if the schema is malformed.
			body["format"] = "json"
		}
	} else if req.JSONMode {
		body["format"] = "json"
	}

	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling ollama request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("creating ollama request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("ollama request: %w", err)
	}
	defer resp.Body.Close()

	respBody, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("reading ollama response: %w", readErr)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ollama returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Model   string `json:"model"`
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Done        bool `json:"done"`
		PromptCount int  `json:"prompt_count"`
		DoneCount   int  `json:"eval_count"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("decoding ollama response: %w", err)
	}

	tokensIn := result.PromptCount
	tokensOut := result.DoneCount
	if tokensIn == 0 {
		tokensIn = EstimateTokens(messagesText(req.Messages))
	}
	if tokensOut == 0 {
		tokensOut = EstimateTokens(result.Message.Content)
	}

	return &ChatResponse{
		Content:   result.Message.Content,
		TokensIn:  tokensIn,
		TokensOut: tokensOut,
		Model:     result.Model,
	}, nil
}

func (c *OllamaClient) IsCloud() bool { return c.isCloud }

func (c *OllamaClient) Pull(ctx context.Context, model string) error {
	if c.isCloud {
		return nil
	}

	url := c.host + "/api/pull"
	body := map[string]any{"name": model, "stream": false}
	b, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("marshaling pull request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("creating pull request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("pull request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("ollama pull returned %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}

func isLocalHost(host string) bool {
	return strings.HasPrefix(host, "http://localhost") ||
		strings.HasPrefix(host, "http://127.0") ||
		strings.HasPrefix(host, "http://0.0.0")
}

func messagesText(msgs []ChatMessage) string {
	var buf bytes.Buffer
	for _, m := range msgs {
		buf.WriteString(m.Content)
		buf.WriteByte('\n')
	}
	return buf.String()
}
