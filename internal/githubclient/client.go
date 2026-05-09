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
	opts := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	}
	var firstMatch *github.IssueComment
	var duplicates []*github.IssueComment
	for {
		comments, resp, err := c.client.Issues.ListComments(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return fmt.Errorf("listing comments: %w", err)
		}
		for _, comment := range comments {
			if comment.Body == nil || !strings.Contains(*comment.Body, marker) {
				continue
			}
			if firstMatch == nil {
				firstMatch = comment
			} else {
				duplicates = append(duplicates, comment)
			}
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}

	// Tidy up duplicates created by older buggy runs.
	for _, dup := range duplicates {
		if _, err := c.client.Issues.DeleteComment(ctx, owner, repo, dup.GetID()); err != nil {
			slog.Warn("failed to delete duplicate sticky comment", "id", dup.GetID(), "error", err)
		}
	}

	if firstMatch != nil {
		if _, _, err := c.client.Issues.EditComment(ctx, owner, repo, firstMatch.GetID(), &github.IssueComment{
			Body: github.String(body),
		}); err != nil {
			return fmt.Errorf("updating sticky comment: %w", err)
		}
		slog.Info("updated existing sticky comment", "pr", prNumber)
		return nil
	}

	if _, _, err := c.client.Issues.CreateComment(ctx, owner, repo, prNumber, &github.IssueComment{
		Body: github.String(body),
	}); err != nil {
		return fmt.Errorf("creating comment: %w", err)
	}
	slog.Info("created new sticky comment", "pr", prNumber)
	return nil
}

func (c *Client) PostComment(ctx context.Context, owner, repo string, prNumber int, body string) error {
	_, _, err := c.client.Issues.CreateComment(ctx, owner, repo, prNumber, &github.IssueComment{
		Body: github.String(body),
	})
	if err != nil {
		return fmt.Errorf("posting comment: %w", err)
	}
	return nil
}

func (c *Client) CreateCheckRun(ctx context.Context, owner, repo, name, conclusion, title, summary, headSHA string) error {
	status := "completed"
	_, _, err := c.client.Checks.CreateCheckRun(ctx, owner, repo, github.CreateCheckRunOptions{
		Name:       name,
		Status:     &status,
		Conclusion: &conclusion,
		HeadSHA:    headSHA,
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
