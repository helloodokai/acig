package critics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"text/template"
	"time"

	_ "embed"

	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/models"
	"github.com/helloodokai/acig/internal/verdict"
)

const maxDiffChars = 12000
const maxCriticOutputTokens = 2048

type baseCritic struct {
	id         string
	tier       Tier
	promptTmpl string
}

func (bc *baseCritic) ID() string { return bc.id }
func (bc *baseCritic) Tier() Tier  { return bc.tier }

type promptData struct {
	Patch         string
	Stats         string
	CriticalPaths string
	CriticResults string
	MaxFindings   int
}

func runCritic(
	ctx context.Context,
	criticID string,
	tier Tier,
	tmplStr string,
	data promptData,
	client models.Client,
	modelName string,
	costFn func(tokensIn, tokensOut int) float64,
) (*verdict.CriticResult, error) {
	start := time.Now()

	tmpl, err := template.New(criticID).Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("parsing prompt template for %s: %w", criticID, err)
	}

	var buf bytes.Buffer
	if execErr := tmpl.Execute(&buf, data); execErr != nil {
		return nil, fmt.Errorf("executing prompt template for %s: %w", criticID, execErr)
	}

	req := models.ChatRequest{
		Model:       modelName,
		Messages:    []models.ChatMessage{{Role: "user", Content: buf.String()}},
		MaxTokens:   maxCriticOutputTokens,
		Temperature: 0.2,
		JSONMode:    true,
	}

	resp, err := client.Chat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("chat request for %s: %w", criticID, err)
	}

	elapsed := time.Since(start)

	content := strings.TrimSpace(resp.Content)
	content = stripMarkdownFence(content)

	result := &verdict.CriticResult{
		Critic:     criticID,
		Model:      resp.Model,
		CostUSD:    costFn(resp.TokensIn, resp.TokensOut),
		DurationMS: elapsed.Milliseconds(),
		TokensIn:   resp.TokensIn,
		TokensOut:  resp.TokensOut,
	}

	var parsed struct {
		Findings []verdict.Finding `json:"findings"`
		Risk     verdict.Risk      `json:"risk"`
	}

	jsonStr := repairJSON(content)
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		slog.Warn("failed to parse critic JSON output", "critic", criticID, "error", err, "raw_len", len(content))
		result.Findings = []verdict.Finding{
			{
				Critic:   criticID,
				Severity: verdict.SeverityInfo,
				Title:    "Failed to parse critic output",
				Detail:   fmt.Sprintf("Model returned invalid JSON (%d bytes). This usually means the output was truncated. Raw: %s", len(content), truncate(content, 300)),
			},
		}
		return result, nil
	}

	for i := range parsed.Findings {
		parsed.Findings[i].Critic = criticID
	}

	result.Findings = parsed.Findings
	return result, nil
}

func repairJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "{}"
	}

	if strings.HasPrefix(s, "```") {
		s = stripMarkdownFence(s)
	}

	// Remove trailing content after the last valid JSON closing brace/bracket.
	// Models sometimes append prose after the JSON block.
	lastClose := strings.LastIndex(s, "}")
	lastBracket := strings.LastIndex(s, "]")
	endIdx := lastClose
	if lastBracket > endIdx {
		endIdx = lastBracket
	}
	if endIdx > 0 {
		// Keep content up to and including the last } or ]
		s = s[:endIdx+1]
	}

	// Try to fix truncated JSON:
	// 1. Remove trailing commas before ] or }
	s = regexp.MustCompile(`,\s*([}\]])`).ReplaceAllString(s, "$1")

	// 2. If the string starts with { but doesn't end with }, close unclosed containers
	if strings.HasPrefix(s, "{") && !strings.HasSuffix(s, "}") {
		// Remove any trailing incomplete key:value pair after the last comma
		// inside the deepest open structure.
		lastComma := strings.LastIndex(s, ",")
		if lastComma > 0 {
			// Check if everything after the last comma looks like a complete pair
			after := strings.TrimSpace(s[lastComma+1:])
			hasColon := strings.Contains(after, ":")
			if !hasColon {
				s = s[:lastComma]
			}
		}

		openBraces := strings.Count(s, "{") - strings.Count(s, "}")
		openBrackets := strings.Count(s, "[") - strings.Count(s, "]")
		for i := 0; i < openBrackets; i++ {
			s += "]"
		}
		for i := 0; i < openBraces; i++ {
			s += "}"
		}
	}

	// 3. Handle case where model outputs a ] where } is expected
	// e.g. {"findings": [...], "risk": "low"]}
	// becomes {"findings": [...], "risk": "low"}}
	// This regex finds ] immediately followed by } or end-of-string when we
	// actually need }. We handle this by replacing trailing ] with } when
	// braces are still open.
	if strings.HasPrefix(s, "{") {
		openBraces := strings.Count(s, "{") - strings.Count(s, "}")
		if openBraces > 0 && strings.HasSuffix(s, "]") {
			s = s[:len(s)-1] + "}"
		}
	}

	return s
}

func stripMarkdownFence(s string) string {
	if !strings.HasPrefix(s, "```") {
		return s
	}
	lines := strings.SplitN(s, "\n", 2)
	if len(lines) < 2 {
		return s
	}
	content := lines[1]
	if idx := strings.LastIndex(content, "```"); idx != -1 {
		content = content[:idx]
	}
	return strings.TrimSpace(content)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func diffToPromptData(d *diff.Diff, cfg *Context) promptData {
	criticalPaths := "none configured"
	if len(cfg.Config.Paths.Critical) > 0 {
		criticalPaths = strings.Join(cfg.Config.Paths.Critical, ", ")
	}

	patch := d.RawPatch
	truncated := false
	if len(patch) > maxDiffChars {
		patch = truncatePatch(d, maxDiffChars)
		truncated = true
	}

	stats := fmt.Sprintf("%d files, +%d/-%d lines", d.Stats.FilesChanged, d.Stats.LinesAdded, d.Stats.LinesRemoved)
	if truncated {
		stats += fmt.Sprintf(" (showing first ~%d chars)", maxDiffChars)
	}

	return promptData{
		Patch:         patch,
		Stats:         stats,
		CriticalPaths: criticalPaths,
		MaxFindings:   8,
	}
}

func truncatePatch(d *diff.Diff, maxChars int) string {
	var buf strings.Builder
	buf.WriteString(fmt.Sprintf("--- Diff truncated: %d files, +%d/-%d lines total ---\n\n",
		d.Stats.FilesChanged, d.Stats.LinesAdded, d.Stats.LinesRemoved))

	remaining := maxChars - buf.Len()
	filesIncluded := 0

	for _, f := range d.Files {
		entry := f.Path
		if f.IsNew {
			entry += " (new file)"
		}
		entry += "\n"

		if buf.Len()+len(entry) > remaining {
			remainingFiles := len(d.Files) - filesIncluded
			if remainingFiles > 0 {
				buf.WriteString(fmt.Sprintf("\n... and %d more files omitted\n", remainingFiles))
			}
			break
		}
		buf.WriteString(entry)

		if buf.Len()+len(f.Patch) <= remaining {
			buf.WriteString(f.Patch)
			buf.WriteString("\n")
		} else {
			chunk := f.Patch
			if len(chunk) > remaining-buf.Len() {
				chunk = truncate(chunk, maxChars-buf.Len())
				chunk += "\n... (file diff truncated)\n"
			}
			buf.WriteString(chunk)
			remainingFiles := len(d.Files) - filesIncluded - 1
			if remainingFiles > 0 {
				buf.WriteString(fmt.Sprintf("\n... and %d more files omitted\n", remainingFiles))
			}
			break
		}
		filesIncluded++
	}

	return buf.String()
}

func truncateIfLong(s string) string {
	if len(s) <= maxDiffChars {
		return s
	}
	return truncate(s, maxDiffChars) + "\n\n... (diff truncated)"
}