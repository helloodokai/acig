package reporters

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/helloodokai/acig/internal/githubclient"
	"github.com/helloodokai/acig/internal/verdict"
)

const stickyMarker = "<!-- acig:sticky -->"

type GitHubReporter struct {
	client *githubclient.Client
}

func NewGitHubReporter(client *githubclient.Client) *GitHubReporter {
	return &GitHubReporter{client: client}
}

func (r *GitHubReporter) Report(ctx context.Context, v *verdict.Verdict, owner, repo string, prNumber int) error {
	md := FormatMarkdown(v)

	var body strings.Builder
	body.WriteString(stickyMarker + "\n")
	body.WriteString(md)
	body.WriteString("\n\n<details>\n<summary>Verdict JSON</summary>\n\n```json\n")

	jsonBody, err := verdictJSON(v)
	if err != nil {
		slog.Warn("failed to marshal verdict for github comment", "error", err)
	} else {
		body.WriteString(jsonBody)
	}

	body.WriteString("\n```\n</details>\n")

	if err := r.client.PostStickyComment(ctx, owner, repo, prNumber, stickyMarker, body.String()); err != nil {
		return fmt.Errorf("posting sticky comment: %w", err)
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

	if err := r.client.CreateCheckRun(ctx, owner, repo, "acig", conclusion, title, summary); err != nil {
		slog.Warn("failed to create check run", "error", err)
	}

	return nil
}

func verdictJSON(v *verdict.Verdict) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}