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

	decisionEmoji := "✅"
	switch v.Decision {
	case verdict.DecisionWarn:
		decisionEmoji = "⚠️"
	case verdict.DecisionBlock:
		decisionEmoji = "🚫"
	}
	body.WriteString(fmt.Sprintf("## %s acig verdict: %s | risk=%s | %d finding(s) | $%.4f\n\n",
		decisionEmoji, strings.ToUpper(string(v.Decision)), v.Risk, len(v.Findings)+len(v.DanglingFindings), v.TotalCostUSD))

	if len(v.Findings) > 0 {
		body.WriteString("### Inline Findings\n\n")
		body.WriteString("| Severity | Critic | Title | File |\n|----------|--------|-------|------|\n")
		for _, f := range v.Findings {
			file := f.File
			if file == "" {
				file = "—"
			}
			body.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n", severityEmoji(f.Severity), f.Critic, f.Title, file))
		}
		body.WriteString("\n")
	}

	if len(v.DanglingFindings) > 0 {
		body.WriteString("### Observations (files not in this PR)\n\n")
		body.WriteString("The following findings reference files not changed in this PR. They are legitimate suggestions but cannot be placed as inline review comments.\n\n")
		for _, f := range v.DanglingFindings {
			body.WriteString(fmt.Sprintf("- %s **%s** (%s)%s\n",
				severityEmoji(f.Severity), f.Title, f.Critic, formatFileNote(f)))
			if f.Detail != "" {
				body.WriteString(fmt.Sprintf("  \n  %s\n", f.Detail))
			}
			if f.SuggestedFix != "" {
				body.WriteString(fmt.Sprintf("  \n  **Suggested fix:** %s\n", f.SuggestedFix))
			}
		}
		body.WriteString("\n")
	}

	if len(v.CriticResults) > 0 {
		body.WriteString("<details>\n<summary>Critic Results</summary>\n\n")
		body.WriteString("| Critic | Model | Findings | Cost | Duration |\n|--------|-------|----------|------|----------|\n")
		for _, cr := range v.CriticResults {
			errIndicator := ""
			if cr.Error != "" {
				errIndicator = " ⚠️"
			}
			body.WriteString(fmt.Sprintf("| %s%s | %s | %d | $%.4f | %dms |\n",
				cr.Critic, errIndicator, cr.Model, len(cr.Findings), cr.CostUSD, cr.DurationMS))
		}
		body.WriteString("\n</details>\n\n")
	}

	body.WriteString("<details>\n<summary>Verdict JSON</summary>\n\n```json\n")
	jsonBody, err := verdictJSON(v)
	if err == nil {
		body.WriteString(jsonBody)
	}
	body.WriteString("\n```\n</details>\n")

	return body.String()
}

func severityEmoji(s verdict.Severity) string {
	switch s {
	case verdict.SeverityBlocking:
		return "🚫"
	case verdict.SeverityHigh:
		return "🔴"
	case verdict.SeverityMedium:
		return "🟡"
	case verdict.SeverityLow:
		return "🟢"
	case verdict.SeverityInfo:
		return "ℹ️"
	default:
		return string(s)
	}
}

func formatFileNote(f verdict.Finding) string {
	if f.File == "" {
		return ""
	}
	return fmt.Sprintf(" — `%s`", f.File)
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
			slog.Warn("skipping finding for file not in PR", "file", findings[0].File, "critic", findings[0].Critic, "title", findings[0].Title)
			continue
		}

		fd := fileDiffs[findings[0].File]
		if fd == nil {
			slog.Warn("skipping finding with no diff data", "file", findings[0].File, "critic", findings[0].Critic, "title", findings[0].Title)
			continue
		}
		validStart := validateLine(fd, findings[0].LineStart)
		if validStart <= 0 {
			continue
		}

		lineStart := validStart
		lineEnd := lineStart
		if findings[0].LineEnd > findings[0].LineStart {
			validEnd := validateLine(fd, findings[0].LineEnd)
			if validEnd > 0 {
				lineEnd = validEnd
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
		if lineEnd > lineStart {
			comment.Line = lineEnd
			comment.StartLine = lineStart
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

func validateLine(fd *diff.FileDiff, requestedLine int) int {
	if fd == nil || fd.IsDelete {
		return 0
	}
	if len(fd.HunkRanges) == 0 {
		return 0
	}
	for _, hr := range fd.HunkRanges {
		if requestedLine >= hr.Start && requestedLine <= hr.End {
			return requestedLine
		}
	}
	nearest := fd.HunkRanges[0]
	for _, hr := range fd.HunkRanges[1:] {
		if abs(requestedLine-hr.Start) < abs(requestedLine-nearest.Start) {
			nearest = hr
		}
	}
	return nearest.Start
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func verdictJSON(v *verdict.Verdict) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}