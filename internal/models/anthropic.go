package models

import (
	"context"
	"fmt"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

type AnthropicClient struct {
	client *anthropic.Client
	model  string
}

func NewAnthropicClient(apiKey, model string) *AnthropicClient {
	client := anthropic.NewClient(option.WithAPIKey(apiKey))
	return &AnthropicClient{client: &client, model: model}
}

func (c *AnthropicClient) Name() string { return "anthropic" }

func (c *AnthropicClient) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	messages := make([]anthropic.MessageParam, 0, len(req.Messages))
	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			continue
		case "user":
			messages = append(messages, anthropic.NewUserMessage(
				anthropic.NewTextBlock(m.Content),
			))
		case "assistant":
			messages = append(messages, anthropic.NewAssistantMessage(
				anthropic.NewTextBlock(m.Content),
			))
		}
	}

	var sysPrompt string
	for _, m := range req.Messages {
		if m.Role == "system" {
			sysPrompt = m.Content
			break
		}
	}

	params := anthropic.MessageNewParams{
		Model:     c.model,
		MaxTokens: int64(req.MaxTokens),
		Messages:  messages,
	}

	if sysPrompt != "" {
		params.System = []anthropic.TextBlockParam{{Text: sysPrompt}}
	}

	msg, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("anthropic chat: %w", err)
	}

	var content string
	for _, b := range msg.Content {
		if b.Type == "text" {
			content += b.Text
		}
	}

	tokensIn := int(msg.Usage.InputTokens)
	tokensOut := int(msg.Usage.OutputTokens)
	if tokensIn == 0 {
		tokensIn = EstimateTokens(sysPrompt + content)
	}
	if tokensOut == 0 {
		tokensOut = EstimateTokens(content)
	}

	return &ChatResponse{
		Content:   content,
		TokensIn:  tokensIn,
		TokensOut: tokensOut,
		Model:     string(msg.Model),
	}, nil
}