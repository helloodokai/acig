package reporters

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/go-github/v66/github"
	"github.com/helloodokai/acig/internal/githubclient"
	"github.com/helloodokai/acig/internal/verdict"
)

const acigMarker = "<!-- acig:review -->"

type GitHubReporter struct {
	client  GitHubClient
	headSHA string
}

type GitHubClient interface {
	ListPRFiles(ctx context.Context, owner, repo string, prNumber int) ([]string, error)
	ListReviews(ctx context.Context, owner, repo string, prNumber int) ([]*github.PullRequestReview, error)
	DeleteReviewComments(ctx context.Context, owner, repo string, prNumber int, reviewID int64) error
	DismissReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64, message string) error
	CreateReview(ctx context.Context, owner, repo string, prNumber int, body string, comments []githubclient.ReviewComment, event string) error
	PostStickyComment(ctx context.Context, owner, repo string, prNumber int, marker, body string) error
	RemoveStaleAcigComments(ctx context.Context, owner, repo string, prNumber int, marker string)
	CreateCheckRun(ctx context.Context, owner, repo, name, conclusion, title, summary, headSHA string) error
}

func NewGitHubReporter(client GitHubClient, headSHA string) *GitHubReporter {
	return &GitHubReporter{client: client, headSHA: headSHA}
}

func (r *GitHubReporter) Report(ctx context.Context, v *verdict.Verdict, owner, repo string, prNumber int) error {
	if err := r.dismissOldReviews(ctx, owner, repo, prNumber); err != nil {
		slog.Warn("failed to dismiss old reviews", "error", err)
	}

	r.client.RemoveStaleAcigComments(ctx, owner, repo, prNumber, acigMarker)

	reviewBody := r.buildReviewBody(v)

	prFiles, err := r.client.ListPRFiles(ctx, owner, repo, prNumber)
	if err != nil {
		slog.Warn("failed to list PR files, using all findings", "error", err)
		prFiles = nil
	}
	reviewComments := buildReviewComments(v, prFiles)
	event := "COMMENT"
	if v.Decision == verdict.DecisionBlock {
		event = "REQUEST_CHANGES"
	}

	if err := r.client.CreateReview(ctx, owner, repo, prNumber, reviewBody, reviewComments, event); err != nil {
		slog.Warn("failed to create review with comments, retrying without inline comments", "error", err)
		if err := r.client.CreateReview(ctx, owner, repo, prNumber, reviewBody, nil, event); err != nil {
			slog.Warn("failed to create review, falling back to comment", "error", err)
			md := FormatMarkdown(v)
			var body strings.Builder
			body.WriteString(acigMarker + "\n")
			body.WriteString(md)
			body.WriteString("\n\n<details>\n<summary>Verdict JSON</summary>\n\n```json\n")
			jsonBody, err := verdictJSON(v)
			if err == nil {
				body.WriteString(jsonBody)
			}
			body.WriteString("\n```\n</details>\n")
			return r.client.PostStickyComment(ctx, owner, repo, prNumber, acigMarker, body.String())
		}
	}

	conclusion := "success"
	title := fmt.Sprintf("acig: %s", v.Decision)
	if v.Decision == verdict.DecisionBlock {
		conclusion = "failure"
	} else if v.Decision == verdict.DecisionWarn {
		conclusion = "neutral"
	}

	summary := fmt.Sprintf("Decision: %s | Risk: %s | %d findings | Cost: $%.4f",
		v.Decision, v.Risk, len(v.Findings), v.TotalCostUSD)

	if err := r.client.CreateCheckRun(ctx, owner, repo, "acig", conclusion, title, summary, r.headSHA); err != nil {
		slog.Warn("failed to create check run", "error", err)
	}

	return nil
}

func (r *GitHubReporter) dismissOldReviews(ctx context.Context, owner, repo string, prNumber int) error {
	reviews, err := r.client.ListReviews(ctx, owner, repo, prNumber)
	if err != nil {
		return err
	}

	for _, rev := range reviews {
		if rev.Body == nil || !strings.Contains(*rev.Body, acigMarker) {
			continue
		}
		state := rev.GetState()
		if state != "CHANGES_REQUESTED" && state != "APPROVED" {
			slog.Info("skipping dismiss of non-dismissable review", "id", rev.GetID(), "state", state)
			continue
		}
		slog.Info("dismissing old acig review", "id", rev.GetID(), "state", state)
		if err := r.client.DeleteReviewComments(ctx, owner, repo, prNumber, rev.GetID()); err != nil {
			slog.Warn("failed to delete old review comments", "error", err)
		}
		msg := "acig re-run: replacing with updated review"
		if err := r.client.DismissReview(ctx, owner, repo, prNumber, rev.GetID(), msg); err != nil {
			slog.Warn("failed to dismiss old review", "id", rev.GetID(), "error", err)
		}
	}
	return nil
}

func (r *GitHubReporter) buildReviewBody(v *verdict.Verdict) string {
	var body strings.Builder
	body.WriteString(acigMarker + "\n")
	body.WriteString(fmt.Sprintf("## acig: %s | risk=%s | %d finding(s) | $%.4f\n\n", v.Decision, v.Risk, len(v.Findings), v.TotalCostUSD))

	body.WriteString("| Severity | Critic | Title | File |\n|----------|--------|-------|------|\n")
	for _, f := range v.Findings {
		file := f.File
		if file == "" {
			file = "—"
		}
		body.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", f.Severity, f.Critic, f.Title, file))
	}

	body.WriteString("\n<details>\n<summary>Verdict JSON</summary>\n\n```json\n")
	jsonBody, err := verdictJSON(v)
	if err == nil {
		body.WriteString(jsonBody)
	}
	body.WriteString("\n```\n</details>\n")

	return body.String()
}

func buildReviewComments(v *verdict.Verdict, prFiles []string) []githubclient.ReviewComment {
	fileFindings := groupFindings(v.Findings)
	var comments []githubclient.ReviewComment

	prFilesSet := make(map[string]bool, len(prFiles))
	for _, f := range prFiles {
		prFilesSet[f] = true
	}

	for _, findings := range fileFindings {
		if len(findings) == 0 || findings[0].File == "" || findings[0].LineStart <= 0 {
			continue
		}
		if len(prFiles) > 0 && !prFilesSet[findings[0].File] {
			continue
		}
		var body strings.Builder
		body.WriteString(fmt.Sprintf("**acig** found %d issue(s) here:\n\n", len(findings)))
		for i, f := range findings {
			if i > 0 {
				body.WriteString("---\n")
			}
			body.WriteString(fmt.Sprintf("- [%s] **%s** (%s)\n  %s\n", f.Severity, f.Title, f.Critic, f.Detail))
			if f.SuggestedFix != "" {
				body.WriteString(fmt.Sprintf("  \n  **Suggested fix:** %s\n", f.SuggestedFix))
			}
		}
		comment := githubclient.ReviewComment{
			Path: findings[0].File,
			Line: findings[0].LineStart,
			Body: body.String(),
		}
		if findings[0].LineEnd > findings[0].LineStart {
			comment.Line = findings[0].LineEnd
			comment.StartLine = findings[0].LineStart
		}
		comments = append(comments, comment)
	}

	return comments
}

func groupFindings(findings []verdict.Finding) [][]verdict.Finding {
	groups := map[string][]verdict.Finding{}
	var order []string
	for _, f := range findings {
		if f.File == "" || f.LineStart <= 0 {
			continue
		}
		key := fmt.Sprintf("%s:%d", f.File, f.LineStart)
		if _, exists := groups[key]; !exists {
			order = append(order, key)
		}
		groups[key] = append(groups[key], f)
	}

	var result [][]verdict.Finding
	for _, key := range order {
		result = append(result, groups[key])
	}
	return result
}

func verdictJSON(v *verdict.Verdict) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}