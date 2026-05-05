package githubclient

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/go-github/v66/github"
)

type Client struct {
	client *github.Client
}

func NewClient(token string) *Client {
	tc := github.NewClient(nil)
	if token != "" {
		tc = tc.WithAuthToken(token)
	}
	return &Client{client: tc}
}

func (c *Client) PostStickyComment(ctx context.Context, owner, repo string, prNumber int, marker, body string) error {
	comments, _, err := c.client.Issues.ListComments(ctx, owner, repo, prNumber, &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		return fmt.Errorf("listing comments: %w", err)
	}

	for _, comment := range comments {
		if comment.Body != nil && strings.Contains(*comment.Body, marker) {
			_, _, err = c.client.Issues.EditComment(ctx, owner, repo, *comment.ID, &github.IssueComment{
				Body: github.String(body),
			})
			if err != nil {
				return fmt.Errorf("updating sticky comment: %w", err)
			}
			slog.Info("updated existing sticky comment", "pr", prNumber)
			return nil
		}
	}

	_, _, err = c.client.Issues.CreateComment(ctx, owner, repo, prNumber, &github.IssueComment{
		Body: github.String(body),
	})
	if err != nil {
		return fmt.Errorf("creating comment: %w", err)
	}
	slog.Info("created new sticky comment", "pr", prNumber)
	return nil
}

func (c *Client) CreateCheckRun(ctx context.Context, owner, repo, name, conclusion, title, summary string) error {
	status := "completed"
	_, _, err := c.client.Checks.CreateCheckRun(ctx, owner, repo, github.CreateCheckRunOptions{
		Name:       name,
		Status:     &status,
		Conclusion: &conclusion,
		HeadSHA:    "",
		Output: &github.CheckRunOutput{
			Title:   github.String(title),
			Summary: github.String(summary),
		},
	})
	if err != nil {
		return fmt.Errorf("creating check run: %w", err)
	}
	return nil
}

func (c *Client) SetCheckRunSHA(sha string) {
	// The SHA needs to be passed contextually; this is handled in the caller
}