package reporters

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"

	"github.com/google/go-github/v66/github"
	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/githubclient"
	"github.com/helloodokai/acig/internal/verdict"
)

const (
	acigMarker       = "<!-- acig:review -->"
	acigLogoURL      = "https://raw.githubusercontent.com/helloodokai/acig/main/acig-logo.png"
	supersededMarker = "<!-- acig:review -->\n_Superseded by a newer acig run._"
	snapWindow       = 3
)

// logoHeader returns a small inline-logo header used at the top of every
// ACIG-authored markdown surface (review body, sticky comment, inline
// comments, check run summary).
func logoHeader(title string) string {
	return fmt.Sprintf(`<img src="%s" height="20" align="left" alt="acig" /> <strong>ACIG</strong> · %s`, acigLogoURL, title)
}

type GitHubReporter struct {
	client  GitHubClient
	headSHA string
	runURL  string // optional: link to the GitHub Actions run for the check-run summary
}

type GitHubClient interface {
	ListPRFiles(ctx context.Context, owner, repo string, prNumber int) ([]string, error)
	GetPRFileDiffs(ctx context.Context, owner, repo string, prNumber int) (map[string]*diff.FileDiff, error)
	ListReviews(ctx context.Context, owner, repo string, prNumber int) ([]*github.PullRequestReview, error)
	DeleteReviewComments(ctx context.Context, owner, repo string, prNumber int, reviewID int64) error
	DismissReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64, message string) error
	DeletePendingReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64) error
	EditReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64, body string) error
	CreateReview(ctx context.Context, owner, repo string, prNumber int, body string, comments []githubclient.ReviewComment, event string) error
	PostStickyComment(ctx context.Context, owner, repo string, prNumber int, marker, body string) error
	PostComment(ctx context.Context, owner, repo string, prNumber int, body string) error
	RemoveStaleAcigComments(ctx context.Context, owner, repo string, prNumber int, marker string)
	CreateCheckRun(ctx context.Context, owner, repo, name, conclusion, title, summary, headSHA string) error
}

func NewGitHubReporter(client GitHubClient, headSHA string) *GitHubReporter {
	return &GitHubReporter{client: client, headSHA: headSHA}
}

// SetRunURL sets the deep link to the GitHub Actions run, embedded in the
// check-run summary so reviewers can jump straight to logs/artifacts.
func (r *GitHubReporter) SetRunURL(url string) { r.runURL = url }

func (r *GitHubReporter) Report(ctx context.Context, v *verdict.Verdict, owner, repo string, prNumber int) error {
	// 1. Clean up everything ACIG produced on prior runs.
	if err := r.cleanupOldReviews(ctx, owner, repo, prNumber); err != nil {
		slog.Warn("failed to clean up old reviews", "error", err)
	}
	r.client.RemoveStaleAcigComments(ctx, owner, repo, prNumber, acigMarker)

	// 2. Resolve PR file metadata for line validation.
	prFiles, err := r.client.ListPRFiles(ctx, owner, repo, prNumber)
	if err != nil {
		slog.Warn("failed to list PR files; line validation will be lax", "error", err)
		prFiles = nil
	}
	fileDiffs, err := r.client.GetPRFileDiffs(ctx, owner, repo, prNumber)
	if err != nil {
		slog.Warn("failed to fetch PR file diffs; line validation will be skipped", "error", err)
		fileDiffs = nil
	}

	// 3. Build inline review comments. Each finding either maps to an inline
	//    comment (RIGHT side for additions/context, LEFT for deleted files) or
	//    is dropped. Dropped findings remain visible in the sticky comment.
	reviewComments, droppedFindings := buildReviewComments(v, prFiles, fileDiffs)

	event := "COMMENT"
	if v.Decision == verdict.DecisionBlock {
		event = "REQUEST_CHANGES"
	}

	// 4. Inline-only review body — short summary; full table lives in the
	//    sticky comment so we don't double up content.
	reviewBody := r.buildReviewBody(v, len(reviewComments), len(droppedFindings))

	// 5. Submit the review. If GitHub rejects an inline comment (rare, e.g.
	//    line happens to be on a comment-line GitHub doesn't accept), retry
	//    without inline comments so the review body still posts.
	if err := r.client.CreateReview(ctx, owner, repo, prNumber, reviewBody, reviewComments, event); err != nil {
		slog.Warn("failed to create review with comments, retrying without inline comments", "error", err)
		if err := r.client.CreateReview(ctx, owner, repo, prNumber, reviewBody, nil, event); err != nil {
			slog.Warn("failed to create review entirely, falling back to sticky comment only", "error", err)
		}
	}

	// 6. Sticky verdict comment — single editable conversation comment with
	//    full table, droppped findings, and verdict JSON.
	stickyBody := r.buildStickyBody(v, droppedFindings)
	if err := r.client.PostStickyComment(ctx, owner, repo, prNumber, acigMarker, stickyBody); err != nil {
		slog.Warn("failed to post sticky verdict comment", "error", err)
	}

	// 7. Check run.
	conclusion := "success"
	title := fmt.Sprintf("acig: %s", v.Decision)
	if v.Decision == verdict.DecisionBlock {
		conclusion = "failure"
	} else if v.Decision == verdict.DecisionWarn {
		conclusion = "neutral"
	}
	summary := r.buildCheckRunSummary(v, len(reviewComments), len(droppedFindings))
	if err := r.client.CreateCheckRun(ctx, owner, repo, "acig", conclusion, title, summary, r.headSHA); err != nil {
		slog.Warn("failed to create check run", "error", err)
	}

	return nil
}

func (r *GitHubReporter) cleanupOldReviews(ctx context.Context, owner, repo string, prNumber int) error {
	reviews, err := r.client.ListReviews(ctx, owner, repo, prNumber)
	if err != nil {
		return err
	}

	for _, rev := range reviews {
		if rev.Body == nil || !strings.Contains(*rev.Body, acigMarker) {
			continue
		}
		state := rev.GetState()
		slog.Info("cleaning up old acig review", "id", rev.GetID(), "state", state)

		// Always delete inline review comments regardless of state.
		if err := r.client.DeleteReviewComments(ctx, owner, repo, prNumber, rev.GetID()); err != nil {
			slog.Warn("failed to delete review comments", "id", rev.GetID(), "error", err)
		}

		switch state {
		case "CHANGES_REQUESTED", "APPROVED":
			msg := "acig re-run: replacing with updated review"
			if err := r.client.DismissReview(ctx, owner, repo, prNumber, rev.GetID(), msg); err != nil {
				slog.Warn("failed to dismiss review", "id", rev.GetID(), "error", err)
			}
		case "PENDING":
			if err := r.client.DeletePendingReview(ctx, owner, repo, prNumber, rev.GetID()); err != nil {
				slog.Warn("failed to delete pending review", "id", rev.GetID(), "error", err)
			}
		case "COMMENTED":
			// COMMENTED reviews can't be dismissed or deleted via the API.
			// Edit the body to a small superseded marker so the stale review
			// no longer surfaces a verdict to humans.
			if err := r.client.EditReview(ctx, owner, repo, prNumber, rev.GetID(), supersededMarker); err != nil {
				slog.Warn("failed to edit superseded review", "id", rev.GetID(), "error", err)
			}
		}
	}
	return nil
}

// buildReviewBody returns the short banner used as the body of the GitHub PR
// review itself. Detail lives in the sticky comment.
func (r *GitHubReporter) buildReviewBody(v *verdict.Verdict, inlineCount, droppedCount int) string {
	var body strings.Builder
	body.WriteString(acigMarker)
	body.WriteString("\n")
	body.WriteString(logoHeader(fmt.Sprintf("%s · risk=%s · %d finding(s)", strings.ToUpper(string(v.Decision)), v.Risk, len(v.Findings))))
	body.WriteString("\n\n")
	body.WriteString(severityLegend(v.Findings))
	body.WriteString("\n")
	body.WriteString(fmt.Sprintf("**%d inline comment(s)** posted on the Files changed tab.", inlineCount))
	if droppedCount > 0 {
		body.WriteString(fmt.Sprintf(" %d finding(s) could not be anchored to a diff line — see the sticky comment for the full list.", droppedCount))
	}
	body.WriteString(fmt.Sprintf("\n\n_Cost: $%.4f · %dms_\n", v.TotalCostUSD, v.TotalDurationMS))
	return body.String()
}

// buildStickyBody returns the body of the sticky conversation comment that
// holds the full verdict, table, dropped findings, and JSON.
func (r *GitHubReporter) buildStickyBody(v *verdict.Verdict, droppedFindings []verdict.Finding) string {
	var body strings.Builder
	body.WriteString(acigMarker)
	body.WriteString("\n")
	body.WriteString(logoHeader("Code Review"))
	body.WriteString("\n\n")
	body.WriteString(fmt.Sprintf("## %s · risk=%s · %d finding(s) · $%.4f\n\n",
		strings.ToUpper(string(v.Decision)), v.Risk, len(v.Findings), v.TotalCostUSD))

	body.WriteString(severityLegend(v.Findings))
	body.WriteString("\n")

	if len(v.Findings) == 0 {
		body.WriteString("_No findings. Code looks clean._\n\n")
	} else {
		body.WriteString("| Severity | Critic | Title | File |\n|----------|--------|-------|------|\n")
		for _, f := range v.Findings {
			file := f.File
			if file == "" {
				file = "—"
			}
			loc := file
			if f.LineStart > 0 {
				loc = fmt.Sprintf("%s:%d", file, f.LineStart)
			}
			body.WriteString(fmt.Sprintf("| %s | %s | %s | %s |\n",
				severityBadge(f.Severity), f.Critic, escapeTableCell(f.Title), loc))
		}
		body.WriteString("\n")
	}

	if len(droppedFindings) > 0 {
		body.WriteString("<details>\n<summary>Findings outside the diff</summary>\n\n")
		body.WriteString("These findings were produced by critics but couldn't be anchored to a line in this PR's diff (e.g. references to deleted files or lines outside any hunk).\n\n")
		for _, f := range droppedFindings {
			body.WriteString(fmt.Sprintf("- **[%s] %s** (`%s`)", severityBadge(f.Severity), f.Title, f.Critic))
			if f.File != "" {
				body.WriteString(fmt.Sprintf(" — `%s`", f.File))
				if f.LineStart > 0 {
					body.WriteString(fmt.Sprintf(":%d", f.LineStart))
				}
			}
			body.WriteString("\n")
			if f.Detail != "" {
				body.WriteString(fmt.Sprintf("  %s\n", f.Detail))
			}
		}
		body.WriteString("\n</details>\n\n")
	}

	if notes := collectCriticNotes(v); len(notes) > 0 {
		body.WriteString("<details>\n<summary>Validation notes</summary>\n\n")
		for _, n := range notes {
			body.WriteString(fmt.Sprintf("- %s\n", n))
		}
		body.WriteString("\n</details>\n\n")
	}

	body.WriteString("<details>\n<summary>Verdict JSON</summary>\n\n```json\n")
	if jsonBody, err := verdictJSON(v); err == nil {
		body.WriteString(jsonBody)
	}
	body.WriteString("\n```\n</details>\n")
	return body.String()
}

// buildCheckRunSummary returns the markdown summary attached to the check run.
func (r *GitHubReporter) buildCheckRunSummary(v *verdict.Verdict, inlineCount, droppedCount int) string {
	var s strings.Builder
	s.WriteString(logoHeader(fmt.Sprintf("%s · risk=%s", strings.ToUpper(string(v.Decision)), v.Risk)))
	s.WriteString("\n\n")
	s.WriteString(fmt.Sprintf("**Findings:** %d (%d inline, %d outside diff)\n\n", len(v.Findings), inlineCount, droppedCount))
	s.WriteString(fmt.Sprintf("**Cost:** $%.4f · **Duration:** %dms\n\n", v.TotalCostUSD, v.TotalDurationMS))
	if r.runURL != "" {
		s.WriteString(fmt.Sprintf("[View run logs and artifacts ↗︎](%s)\n", r.runURL))
	}
	return s.String()
}

// buildReviewComments groups findings into inline review comments and returns
// the comments alongside the findings that could not be anchored.
func buildReviewComments(v *verdict.Verdict, prFiles []string, fileDiffs map[string]*diff.FileDiff) ([]githubclient.ReviewComment, []verdict.Finding) {
	prFilesSet := make(map[string]bool, len(prFiles))
	for _, f := range prFiles {
		prFilesSet[f] = true
	}

	type groupKey struct {
		file string
		line int
	}
	groups := map[groupKey][]verdict.Finding{}
	var order []groupKey
	var dropped []verdict.Finding

	for _, f := range v.Findings {
		if f.File == "" {
			dropped = append(dropped, f)
			continue
		}
		if len(prFilesSet) > 0 && !prFilesSet[f.File] {
			dropped = append(dropped, f)
			continue
		}
		k := groupKey{file: f.File, line: f.LineStart}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], f)
	}

	var comments []githubclient.ReviewComment
	for _, k := range order {
		findings := groups[k]
		fd := lookupFileDiff(fileDiffs, k.file)

		anchor, side, ok := anchorLine(fd, findings[0].LineStart, findings[0].LineEnd)
		if !ok {
			dropped = append(dropped, findings...)
			continue
		}

		startLine := 0
		if findings[0].LineEnd > findings[0].LineStart {
			endAnchor, endSide, endOK := anchorLine(fd, findings[0].LineEnd, findings[0].LineEnd)
			if endOK && endSide == side && endAnchor > anchor {
				startLine = anchor
				anchor = endAnchor
			}
		}

		body := buildInlineCommentBody(findings)
		comments = append(comments, githubclient.ReviewComment{
			Path:      k.file,
			Line:      anchor,
			StartLine: startLine,
			Side:      side,
			Body:      body,
		})
	}
	return comments, dropped
}

// anchorLine resolves a model-supplied line number to a real diff position.
// Returns the validated line, the side ("RIGHT"/"LEFT"), and whether anchoring
// succeeded. ±snapWindow snapping absorbs minor LLM line drift.
func anchorLine(fd *diff.FileDiff, lineStart, lineEnd int) (int, string, bool) {
	if fd == nil {
		// No diff data available — only accept positive line numbers and
		// assume RIGHT side. The reporter pipeline always feeds fileDiffs,
		// but tests sometimes don't.
		if lineStart > 0 {
			return lineStart, "RIGHT", true
		}
		return 0, "", false
	}

	// Deleted file: anchor on LEFT side using OrigLines.
	if fd.IsDelete {
		if line, ok := snap(fd.OrigLines, lineStart); ok {
			return line, "LEFT", true
		}
		// Fall back to the first old-side line if the model gave us 0.
		if lineStart <= 0 {
			if line, ok := firstLine(fd.OrigLines); ok {
				return line, "LEFT", true
			}
		}
		return 0, "", false
	}

	// Non-deleted file: prefer RIGHT side (DiffLines).
	if lineStart <= 0 {
		// File-level finding: snap to first visible RIGHT-side line.
		if line, ok := firstLine(fd.DiffLines); ok {
			return line, "RIGHT", true
		}
		// File has no RIGHT-side hunks (e.g. a binary or empty change) — try LEFT.
		if line, ok := firstLine(fd.OrigLines); ok {
			return line, "LEFT", true
		}
		return 0, "", false
	}

	// We have an explicit line. Try RIGHT first with snapping, then LEFT.
	if line, ok := snap(fd.DiffLines, lineStart); ok {
		return line, "RIGHT", true
	}
	if line, ok := snap(fd.OrigLines, lineStart); ok {
		return line, "LEFT", true
	}
	// Legacy fallback for tests that pass FileDiff with Added but no DiffLines.
	if len(fd.DiffLines) == 0 && len(fd.OrigLines) == 0 {
		if len(fd.Added) > 0 && lineStart <= len(fd.Added) {
			return lineStart, "RIGHT", true
		}
	}
	_ = lineEnd
	return 0, "", false
}

// snap returns either the requested line if it's in the set, or the closest
// line in the set within ±snapWindow. Returns (0,false) otherwise.
func snap(set map[int]bool, requested int) (int, bool) {
	if len(set) == 0 || requested <= 0 {
		return 0, false
	}
	if set[requested] {
		return requested, true
	}
	for delta := 1; delta <= snapWindow; delta++ {
		if set[requested-delta] {
			return requested - delta, true
		}
		if set[requested+delta] {
			return requested + delta, true
		}
	}
	return 0, false
}

func firstLine(set map[int]bool) (int, bool) {
	if len(set) == 0 {
		return 0, false
	}
	min := -1
	for l := range set {
		if min < 0 || l < min {
			min = l
		}
	}
	return min, true
}

func lookupFileDiff(diffs map[string]*diff.FileDiff, path string) *diff.FileDiff {
	if diffs == nil {
		return nil
	}
	return diffs[path]
}

// buildInlineCommentBody renders the markdown for a group of findings on the
// same file:line. Includes a logo header, severity badge, suggestion block
// (when SuggestedFix is single-line), and an ignore hint.
func buildInlineCommentBody(findings []verdict.Finding) string {
	var body strings.Builder
	body.WriteString(acigMarker)
	body.WriteString("\n")
	body.WriteString(logoHeader(fmt.Sprintf("%d issue(s)", len(findings))))
	body.WriteString("\n\n")
	for i, f := range findings {
		if i > 0 {
			body.WriteString("\n---\n\n")
		}
		body.WriteString(fmt.Sprintf("**[%s] %s** · `%s`\n\n", severityBadge(f.Severity), f.Title, f.Critic))
		if f.Detail != "" {
			body.WriteString(f.Detail)
			body.WriteString("\n")
		}
		if f.SuggestedFix != "" {
			if isSingleLineSuggestion(f.SuggestedFix) {
				body.WriteString("\n```suggestion\n")
				body.WriteString(strings.TrimRight(f.SuggestedFix, "\n"))
				body.WriteString("\n```\n")
			} else {
				body.WriteString("\n<details>\n<summary>Suggested fix</summary>\n\n")
				body.WriteString(f.SuggestedFix)
				body.WriteString("\n\n</details>\n")
			}
		}
		body.WriteString(fmt.Sprintf("\n_To suppress: add `# acig-ignore: %s` near this line._\n", f.Critic))
	}
	return body.String()
}

// isSingleLineSuggestion returns true when SuggestedFix is plausibly a
// single-line replacement we can render as a GitHub `suggestion` block.
// We require: no newlines, no fenced code, and modest length.
func isSingleLineSuggestion(s string) bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return false
	}
	if strings.ContainsAny(s, "\n\r") {
		return false
	}
	if strings.Contains(s, "```") {
		return false
	}
	if len(s) > 200 {
		return false
	}
	return true
}

func severityBadge(s verdict.Severity) string {
	switch s {
	case verdict.SeverityBlocking:
		return "🚫 blocking"
	case verdict.SeverityHigh:
		return "🔴 high"
	case verdict.SeverityMedium:
		return "🟡 medium"
	case verdict.SeverityLow:
		return "🟢 low"
	case verdict.SeverityInfo:
		return "ℹ️ info"
	default:
		return string(s)
	}
}

func severityLegend(findings []verdict.Finding) string {
	counts := map[verdict.Severity]int{}
	for _, f := range findings {
		counts[f.Severity]++
	}
	if len(counts) == 0 {
		return ""
	}
	var parts []string
	for _, sev := range []verdict.Severity{
		verdict.SeverityBlocking, verdict.SeverityHigh, verdict.SeverityMedium, verdict.SeverityLow, verdict.SeverityInfo,
	} {
		if c := counts[sev]; c > 0 {
			parts = append(parts, fmt.Sprintf("%s &times;%d", severityBadge(sev), c))
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ") + "\n"
}

func escapeTableCell(s string) string {
	s = strings.ReplaceAll(s, "|", "\\|")
	s = strings.ReplaceAll(s, "\n", " ")
	return s
}

// collectCriticNotes pulls Notes from each CriticResult so reviewers can see
// when the validation pass dropped or downgraded findings.
func collectCriticNotes(v *verdict.Verdict) []string {
	var notes []string
	type kv struct {
		critic string
		note   string
	}
	var kvs []kv
	for _, cr := range v.CriticResults {
		for _, n := range cr.Notes {
			kvs = append(kvs, kv{cr.Critic, n})
		}
	}
	if len(kvs) == 0 {
		return nil
	}
	sort.Slice(kvs, func(i, j int) bool { return kvs[i].critic < kvs[j].critic })
	for _, x := range kvs {
		notes = append(notes, fmt.Sprintf("`%s`: %s", x.critic, x.note))
	}
	return notes
}

func verdictJSON(v *verdict.Verdict) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
