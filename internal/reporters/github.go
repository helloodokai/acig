package reporters

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/google/go-github/v66/github"
	"github.com/helloodokai/acig/internal/diff"
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
	GetPRFileDiffs(ctx context.Context, owner, repo string, prNumber int) (map[string]*diff.FileDiff, error)
	ListReviews(ctx context.Context, owner, repo string, prNumber int) ([]*github.PullRequestReview, error)
	DeleteReviewComments(ctx context.Context, owner, repo string, prNumber int, reviewID int64) error
	DismissReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64, message string) error
	CreateReview(ctx context.Context, owner, repo string, prNumber int, body string, comments []githubclient.ReviewComment, event string) error
	PostStickyComment(ctx context.Context, owner, repo string, prNumber int, marker, body string) error
	PostComment(ctx context.Context, owner, repo string, prNumber int, body string) error
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

	fileDiffs, err := r.client.GetPRFileDiffs(ctx, owner, repo, prNumber)
	if err != nil {
		slog.Warn("failed to get PR file diffs, skipping line validation", "error", err)
		fileDiffs = nil
	}

	reviewComments := buildReviewComments(v, prFiles, fileDiffs)
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

	// Post general findings (no file/line), missing-file findings (file not in the PR),
	// and findings whose line falls outside the diff as individual PR comments so they
	// are visible and actionable rather than buried in the review body table.
	prFilesSet := make(map[string]bool, len(prFiles))
	for _, f := range prFiles {
		prFilesSet[f] = true
	}
	for _, comment := range buildGeneralComments(v, prFilesSet, fileDiffs) {
		if err := r.client.PostComment(ctx, owner, repo, prNumber, comment); err != nil {
			slog.Warn("failed to post general finding comment", "error", err)
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

func buildReviewComments(v *verdict.Verdict, prFiles []string, fileDiffs map[string]*diff.FileDiff) []githubclient.ReviewComment {
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
		if len(prFilesSet) > 0 && !prFilesSet[findings[0].File] {
			continue
		}

		lineStart := findings[0].LineStart
		lineEnd := findings[0].LineEnd

		if fileDiffs != nil {
			fd := fileDiffs[findings[0].File]
			if fd == nil {
				continue
			}
			validStart := validateLine(fd, lineStart)
			if validStart <= 0 {
				// Line is not in the diff — will be posted as a general comment instead.
				continue
			}
			lineStart = validStart
			if lineEnd > findings[0].LineStart {
				validEnd := validateLine(fd, lineEnd)
				if validEnd > 0 {
					lineEnd = validEnd
				} else {
					lineEnd = lineStart
				}
			} else {
				lineEnd = lineStart
			}
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
			Line: lineStart,
			Body: body.String(),
		}
		if lineEnd > findings[0].LineStart && lineEnd != lineStart {
			comment.Line = lineEnd
			comment.StartLine = lineStart
		}
		comments = append(comments, comment)
	}

	return comments
}

// buildGeneralComments returns one markdown string per finding that cannot be
// posted as an inline review comment: findings with no file, findings whose
// file is not part of the PR, and findings whose line number falls outside the
// visible diff hunks.
func buildGeneralComments(v *verdict.Verdict, prFilesSet map[string]bool, fileDiffs map[string]*diff.FileDiff) []string {
	var comments []string
	for _, f := range v.Findings {
		reason := generalReason(f, prFilesSet, fileDiffs)
		if reason == "" {
			continue // will be (or already was) posted as an inline review comment
		}

		var body strings.Builder
		body.WriteString(acigMarker + "\n")
		if f.File != "" {
			body.WriteString(fmt.Sprintf("**acig** [%s] — `%s`", reason, f.File))
			if f.LineStart > 0 {
				body.WriteString(fmt.Sprintf(" (line %d)", f.LineStart))
			}
		} else {
			body.WriteString(fmt.Sprintf("**acig** [%s]", reason))
		}
		body.WriteString(fmt.Sprintf("\n\n**[%s] %s** (%s)\n\n%s", f.Severity, f.Title, f.Critic, f.Detail))
		if f.SuggestedFix != "" {
			body.WriteString(fmt.Sprintf("\n\n**Suggested fix:** %s", f.SuggestedFix))
		}
		comments = append(comments, body.String())
	}
	return comments
}

// generalReason returns a short label explaining why a finding cannot be an
// inline comment, or "" if it can be posted inline.
func generalReason(f verdict.Finding, prFilesSet map[string]bool, fileDiffs map[string]*diff.FileDiff) string {
	if f.File == "" || f.LineStart <= 0 {
		return "general"
	}
	if len(prFilesSet) > 0 && !prFilesSet[f.File] {
		return "file not in PR"
	}
	if fileDiffs != nil {
		fd := fileDiffs[f.File]
		if fd == nil || validateLine(fd, f.LineStart) <= 0 {
			return "outside diff"
		}
	}
	return ""
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

// validateLine returns the line number to use for a GitHub PR review inline
// comment, or 0 if no valid position can be determined.
//
// When fd.DiffLines is populated (the normal path from GetPRFileDiffs), only
// lines that are actually visible in the diff are accepted; an exact match is
// required so that GitHub never rejects the position.  If DiffLines is empty
// we fall back to the legacy count-based heuristic (used in unit tests that
// build FileDiff structs manually without DiffLines).
func validateLine(fd *diff.FileDiff, requestedLine int) int {
	if fd == nil || fd.IsDelete {
		return 0
	}

	// Preferred path: use the real new-file line numbers tracked during parse.
	if len(fd.DiffLines) > 0 {
		if fd.DiffLines[requestedLine] {
			return requestedLine
		}
		// Line is not visible in any diff hunk — caller should treat as general.
		return 0
	}

	// Legacy fallback: fd.Added holds line contents; use its length as a rough
	// upper bound.  This is intentionally kept for tests that build FileDiff
	// without DiffLines.
	addedCount := len(fd.Added)
	if addedCount == 0 {
		return 0
	}
	if requestedLine <= addedCount {
		return requestedLine
	}
	return addedCount
}

func verdictJSON(v *verdict.Verdict) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}