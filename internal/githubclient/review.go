package githubclient

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/go-github/v66/github"
	"github.com/helloodokai/acig/internal/diff"
)

const cleanupPageSize = 100

func (c *Client) ListReviews(ctx context.Context, owner, repo string, prNumber int) ([]*github.PullRequestReview, error) {
	var all []*github.PullRequestReview
	opts := &github.ListOptions{PerPage: cleanupPageSize}
	for {
		page, resp, err := c.client.PullRequests.ListReviews(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("listing reviews: %w", err)
		}
		all = append(all, page...)
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return all, nil
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

func (c *Client) DeletePendingReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64) error {
	_, _, err := c.client.PullRequests.DeletePendingReview(ctx, owner, repo, prNumber, reviewID)
	if err != nil {
		return fmt.Errorf("deleting pending review %d: %w", reviewID, err)
	}
	return nil
}

// EditReview overwrites the body of an existing review. Used to replace the
// body of COMMENTED reviews (which can neither be dismissed nor deleted via
// the API) with a "superseded" marker on re-runs.
func (c *Client) EditReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64, body string) error {
	_, _, err := c.client.PullRequests.UpdateReview(ctx, owner, repo, prNumber, reviewID, body)
	if err != nil {
		return fmt.Errorf("editing review %d: %w", reviewID, err)
	}
	return nil
}

func (c *Client) DeleteReviewComments(ctx context.Context, owner, repo string, prNumber int, reviewID int64) error {
	opts := &github.ListOptions{PerPage: cleanupPageSize}
	for {
		comments, resp, err := c.client.PullRequests.ListReviewComments(ctx, owner, repo, prNumber, reviewID, opts)
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
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return nil
}

type ReviewComment struct {
	Path      string
	Line      int
	StartLine int    // optional: for multi-line comments (0 means single-line)
	Side      string // "RIGHT" (default, new file) or "LEFT" (original file, e.g. for deleted-file comments)
	Body      string
}

func (c *Client) CreateReview(ctx context.Context, owner, repo string, prNumber int, body string, comments []ReviewComment, event string) error {
	var reviewComments []*github.DraftReviewComment
	for i := range comments {
		rc := comments[i]
		if rc.Line <= 0 {
			continue
		}
		side := rc.Side
		if side == "" {
			side = "RIGHT"
		}
		// Take addresses of locals so each comment gets its own pointers.
		path := rc.Path
		line := rc.Line
		bodyStr := rc.Body
		sideCopy := side
		drc := &github.DraftReviewComment{
			Path: &path,
			Line: &line,
			Side: &sideCopy,
			Body: &bodyStr,
		}
		if rc.StartLine > 0 && rc.StartLine < rc.Line {
			startLine := rc.StartLine
			startSide := side
			drc.StartLine = &startLine
			drc.StartSide = &startSide
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
	opts := &github.IssueListCommentsOptions{
		ListOptions: github.ListOptions{PerPage: cleanupPageSize},
	}
	for {
		comments, resp, err := c.client.Issues.ListComments(ctx, owner, repo, prNumber, opts)
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
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
}

func (c *Client) ListPRFiles(ctx context.Context, owner, repo string, prNumber int) ([]string, error) {
	var paths []string
	opts := &github.ListOptions{PerPage: cleanupPageSize}
	for {
		files, resp, err := c.client.PullRequests.ListFiles(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("listing PR files: %w", err)
		}
		for _, f := range files {
			paths = append(paths, f.GetFilename())
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return paths, nil
}

// GetPRFileDiffs returns FileDiff structs keyed by path, with DiffLines and
// OrigLines populated from each file's headerless `Patch` body. The previous
// implementation tried to feed the body through ParseMultiFileDiff which
// requires `--- a/x +++ b/x` headers and silently produced empty results,
// causing every line to be treated as "outside the diff".
func (c *Client) GetPRFileDiffs(ctx context.Context, owner, repo string, prNumber int) (map[string]*diff.FileDiff, error) {
	result := make(map[string]*diff.FileDiff)
	opts := &github.ListOptions{PerPage: cleanupPageSize}
	for {
		files, resp, err := c.client.PullRequests.ListFiles(ctx, owner, repo, prNumber, opts)
		if err != nil {
			return nil, fmt.Errorf("listing PR files for diffs: %w", err)
		}
		for _, f := range files {
			path := f.GetFilename()
			isNew := f.GetStatus() == "added"
			isDelete := f.GetStatus() == "removed"
			patch := ""
			if f.Patch != nil {
				patch = *f.Patch
			}
			fd := diff.ParseHunkBody(path, patch, isNew, isDelete)
			result[path] = fd
		}
		if resp == nil || resp.NextPage == 0 {
			break
		}
		opts.Page = resp.NextPage
	}
	return result, nil
}
