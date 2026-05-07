package githubclient

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/go-github/v66/github"
)

func (c *Client) ListReviews(ctx context.Context, owner, repo string, prNumber int) ([]*github.PullRequestReview, error) {
	reviews, _, err := c.client.PullRequests.ListReviews(ctx, owner, repo, prNumber, nil)
	if err != nil {
		return nil, fmt.Errorf("listing reviews: %w", err)
	}
	return reviews, nil
}

func (c *Client) DismissReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64, message string) error {
	_, _, err := c.client.PullRequests.DismissReview(ctx, owner, repo, prNumber, reviewID, &github.PullRequestReviewDismissalRequest{
		Message: &message,
	})
	if err != nil {
		return fmt.Errorf("dismissing review %d: %w", reviewID, err)
	}
	return nil
}

func (c *Client) DeleteReviewComments(ctx context.Context, owner, repo string, prNumber int, reviewID int64) error {
	comments, _, err := c.client.PullRequests.ListReviewComments(ctx, owner, repo, prNumber, reviewID, &github.ListOptions{PerPage: 100})
	if err != nil {
		return fmt.Errorf("listing review comments: %w", err)
	}
	for _, rc := range comments {
		if rc.GetPullRequestReviewID() == reviewID {
			_, err := c.client.PullRequests.DeleteComment(ctx, owner, repo, rc.GetID())
			if err != nil {
				slog.Warn("failed to delete review comment", "id", rc.GetID(), "error", err)
			}
		}
	}
	return nil
}

type ReviewComment struct {
	Path      string
	Line      int
	StartLine int // optional: for multi-line comments (0 means single-line)
	Body      string
}

func (c *Client) CreateReview(ctx context.Context, owner, repo string, prNumber int, body string, comments []ReviewComment, event string) error {
	var reviewComments []*github.DraftReviewComment
	side := "RIGHT"
	for _, rc := range comments {
		if rc.Line <= 0 {
			continue
		}
		drc := &github.DraftReviewComment{
			Path: &rc.Path,
			Line: &rc.Line,
			Side: &side,
			Body: &rc.Body,
		}
		if rc.StartLine > 0 && rc.StartLine < rc.Line {
			drc.StartLine = &rc.StartLine
			drc.StartSide = &side
		}
		reviewComments = append(reviewComments, drc)
	}

	_, _, err := c.client.PullRequests.CreateReview(ctx, owner, repo, prNumber, &github.PullRequestReviewRequest{
		Body:     &body,
		Event:    &event,
		Comments: reviewComments,
	})
	if err != nil {
		return fmt.Errorf("creating review: %w", err)
	}
	return nil
}

func (c *Client) RemoveStaleAcigComments(ctx context.Context, owner, repo string, prNumber int, marker string) {
	comments, _, err := c.client.Issues.ListComments(ctx, owner, repo, prNumber, &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: 100},
	})
	if err != nil {
		slog.Warn("failed to list comments for cleanup", "error", err)
		return
	}
	for _, comment := range comments {
		if comment.Body != nil && strings.Contains(*comment.Body, marker) {
			_, err := c.client.Issues.DeleteComment(ctx, owner, repo, comment.GetID())
			if err != nil {
				slog.Warn("failed to delete stale comment", "id", comment.GetID(), "error", err)
			} else {
				slog.Info("deleted stale acig comment", "id", comment.GetID())
			}
		}
	}
}

func (c *Client) ListPRFiles(ctx context.Context, owner, repo string, prNumber int) ([]string, error) {
	files, _, err := c.client.PullRequests.ListFiles(ctx, owner, repo, prNumber, &github.ListOptions{PerPage: 100})
	if err != nil {
		return nil, fmt.Errorf("listing PR files: %w", err)
	}
	paths := make([]string, len(files))
	for i, f := range files {
		paths[i] = f.GetFilename()
	}
	return paths, nil
}