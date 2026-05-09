package critics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"

	_ "embed"

	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/models"
	"github.com/helloodokai/acig/internal/verdict"
)

const (
	maxDiffChars          = 10000
	maxCriticOutputTokens = 2048
	// snapWindow mirrors the reporter's anchoring window so prompts and
	// post-parse validation agree on what "close enough" means.
	snapWindow = 3
)

type baseCritic struct {
	id         string
	tier       Tier
	promptTmpl string
}

func (bc *baseCritic) ID() string { return bc.id }
func (bc *baseCritic) Tier() Tier { return bc.tier }

// promptData is what the templates see. Critic-specific fields can be unset
// when not relevant (e.g. CriticResults is only used by the adjudicator).
type promptData struct {
	Patch          string
	NumberedDiff   string
	FileSummary    string
	ValidLines     string
	Stats          string
	CriticalPaths  string
	CriticResults  string
	SeverityRubric string
	OutputContract string
	MaxFindings    int
}

// validationContext is built once per critic call from the diff and used both
// by the prompt (as per-file allowed line lists) and by the post-parse
// validator (to drop hallucinated findings).
type validationContext struct {
	files      map[string]*diff.FileDiff
	allFiles   map[string]bool
	severities map[verdict.Severity]bool
}

func newValidationContext(d *diff.Diff) *validationContext {
	vc := &validationContext{
		files:    make(map[string]*diff.FileDiff, len(d.Files)),
		allFiles: make(map[string]bool, len(d.Files)),
		severities: map[verdict.Severity]bool{
			verdict.SeverityInfo:     true,
			verdict.SeverityLow:      true,
			verdict.SeverityMedium:   true,
			verdict.SeverityHigh:     true,
			verdict.SeverityBlocking: true,
		},
	}
	for i := range d.Files {
		f := &d.Files[i]
		vc.files[f.Path] = f
		vc.allFiles[f.Path] = true
	}
	return vc
}

// validateFinding returns the finding with severity coerced to a known value
// and an empty reason when the finding is acceptable, or a non-empty drop
// reason when it should be filtered out.
func (vc *validationContext) validateFinding(criticID string, tier Tier, f verdict.Finding) (verdict.Finding, string) {
	// 1. Severity coercion (open-weight models love to invent severities).
	if !vc.severities[f.Severity] {
		f.Severity = verdict.SeverityInfo
	}
	// 2. blocking is only allowed from mid/frontier critics.
	if f.Severity == verdict.SeverityBlocking && tier == TierCheap {
		f.Severity = verdict.SeverityHigh
	}
	// 3. If no diff data is available, accept the finding as-is (defensive).
	if len(vc.allFiles) == 0 {
		return f, ""
	}
	// 4. Empty file is fine (truly general findings); reporter routes to the
	//    sticky comment.
	if f.File == "" {
		return f, ""
	}
	// 5. File must be one we know about.
	if !vc.allFiles[f.File] {
		return f, fmt.Sprintf("file %q not in PR", f.File)
	}
	// 6. Critic/file-kind compatibility.
	fd := vc.files[f.File]
	kind := diff.FileKind(f.File)
	if !criticAllowsKind(criticID, kind, fd) {
		return f, fmt.Sprintf("file %q (kind=%s) not eligible for critic %s", f.File, kind, criticID)
	}
	// 7. Line must be in the diff (or within ±snapWindow). Skip the check for
	//    file-level findings (LineStart=0) — the reporter snaps those to the
	//    first hunk line.
	if f.LineStart > 0 && fd != nil {
		set := fd.DiffLines
		if fd.IsDelete {
			set = fd.OrigLines
		}
		if len(set) > 0 && !lineInSet(set, f.LineStart, snapWindow) {
			return f, fmt.Sprintf("line %d in %s outside diff (±%d)", f.LineStart, f.File, snapWindow)
		}
	}
	return f, ""
}

func criticAllowsKind(criticID string, kind diff.Kind, fd *diff.FileDiff) bool {
	switch criticID {
	case "security_smell", "perf_smell":
		// Don't flag runtime issues in docs/generated/test fixtures.
		switch kind {
		case diff.KindDocs, diff.KindGenerated:
			return false
		}
	case "test_coverage_smell":
		// Coverage gaps don't apply to non-code or generated artifacts.
		switch kind {
		case diff.KindDocs, diff.KindConfig, diff.KindGenerated, diff.KindTest:
			return false
		}
		// Coverage findings don't make sense on deleted files either.
		if fd != nil && fd.IsDelete {
			return false
		}
	case "style_conformance":
		if kind == diff.KindGenerated {
			return false
		}
	}
	return true
}

func lineInSet(set map[int]bool, line, window int) bool {
	if set[line] {
		return true
	}
	for d := 1; d <= window; d++ {
		if set[line-d] || set[line+d] {
			return true
		}
	}
	return false
}

// chatRequestForTier returns a ChatRequest configured with conservative
// defaults that work well for open-weight models.
func chatRequestForTier(tier Tier, model, prompt string, schema json.RawMessage) models.ChatRequest {
	temp := 0.15
	switch tier {
	case TierCheap:
		temp = 0.1
	case TierFrontier:
		temp = 0.2
	}
	return models.ChatRequest{
		Model:       model,
		Messages:    []models.ChatMessage{{Role: "user", Content: prompt}},
		MaxTokens:   maxCriticOutputTokens,
		Temperature: temp,
		TopP:        0.9,
		JSONMode:    true,
		JSONSchema:  schema,
		Stop:        []string{"</output>"},
	}
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
	d *diff.Diff,
	schema json.RawMessage,
) (*verdict.CriticResult, error) {
	start := time.Now()

	// Inject shared blocks if the critic didn't supply its own.
	if data.SeverityRubric == "" {
		data.SeverityRubric = severityRubric()
	}
	if data.OutputContract == "" {
		data.OutputContract = outputContract()
	}
	if d != nil {
		if data.NumberedDiff == "" {
			data.NumberedDiff = buildNumberedDiff(d, maxDiffChars)
		}
		if data.FileSummary == "" {
			data.FileSummary = buildFileSummary(d)
		}
		if data.ValidLines == "" {
			data.ValidLines = buildValidLines(d)
		}
	}

	tmpl, err := template.New(criticID).Parse(tmplStr)
	if err != nil {
		return nil, fmt.Errorf("parsing prompt template for %s: %w", criticID, err)
	}

	var buf bytes.Buffer
	if execErr := tmpl.Execute(&buf, data); execErr != nil {
		return nil, fmt.Errorf("executing prompt template for %s: %w", criticID, execErr)
	}

	req := chatRequestForTier(tier, modelName, buf.String(), schema)

	resp, err := client.Chat(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("chat request for %s: %w", criticID, err)
	}

	elapsed := time.Since(start)

	content := strings.TrimSpace(resp.Content)
	content = stripOutputTags(content)
	content = stripMarkdownFence(content)

	result := &verdict.CriticResult{
		Critic:     criticID,
		Model:      resp.Model,
		CostUSD:    costFn(resp.TokensIn, resp.TokensOut),
		DurationMS: elapsed.Milliseconds(),
		TokensIn:   resp.TokensIn,
		TokensOut:  resp.TokensOut,
	}

	// Risk classifier returns ONLY {"risk", "reasoning"} — no findings.
	if criticID == "risk_classifier" {
		var rc struct {
			Risk      string `json:"risk"`
			Reasoning string `json:"reasoning"`
		}
		jsonStr := repairJSON(content)
		if err := json.Unmarshal([]byte(jsonStr), &rc); err != nil {
			slog.Warn("failed to parse risk_classifier JSON output", "error", err, "raw_len", len(content))
			result.Notes = append(result.Notes, "risk_classifier returned malformed JSON; defaulting to risk=low")
			result.Risk = verdict.RiskLow
			return result, nil
		}
		risk := normalizeRisk(rc.Risk)
		if risk == "" {
			result.Notes = append(result.Notes, fmt.Sprintf("risk_classifier returned unknown risk %q; defaulting to low", rc.Risk))
			risk = verdict.RiskLow
		}
		result.Risk = risk
		result.Reasoning = strings.TrimSpace(rc.Reasoning)
		return result, nil
	}

	var parsed struct {
		Findings []verdict.Finding `json:"findings"`
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
		result.Notes = append(result.Notes, "JSON parse failed; raw response truncated")
		return result, nil
	}

	// Stamp critic id and run validation.
	vc := newValidationContext(d)
	dropped := 0
	dropReasons := map[string]int{}
	var kept []verdict.Finding
	for _, f := range parsed.Findings {
		f.Critic = criticID
		validated, reason := vc.validateFinding(criticID, tier, f)
		if reason != "" {
			dropped++
			dropReasons[reason]++
			continue
		}
		kept = append(kept, validated)
	}

	// Cap findings.
	if data.MaxFindings > 0 && len(kept) > data.MaxFindings {
		dropped += len(kept) - data.MaxFindings
		dropReasons[fmt.Sprintf("over MaxFindings (%d)", data.MaxFindings)] += len(kept) - data.MaxFindings
		kept = kept[:data.MaxFindings]
	}

	result.Findings = kept
	if dropped > 0 {
		// Surface a compact summary in Notes for observability.
		var keys []string
		for k := range dropReasons {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%dx %s", dropReasons[k], k))
		}
		result.Notes = append(result.Notes, fmt.Sprintf("dropped %d finding(s) by validation: %s", dropped, strings.Join(parts, "; ")))
	}
	return result, nil
}

func normalizeRisk(s string) verdict.Risk {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "low":
		return verdict.RiskLow
	case "medium", "med":
		return verdict.RiskMedium
	case "high":
		return verdict.RiskHigh
	case "critical", "crit":
		return verdict.RiskCritical
	}
	return ""
}

// severityRubric returns the canonical severity definitions used in every
// prompt. Open-weight models calibrate severities much better when they see
// concrete examples for each level.
func severityRubric() string {
	return strings.TrimSpace(`
SEVERITY RUBRIC (use exactly one of these values):
- "blocking": exploitable security flaw, data loss, or guaranteed runtime crash. Must prevent merge.
- "high":     real bug or security risk likely to cause incidents. Must be fixed before merge in normal flow.
- "medium":   correctness, reliability, or maintainability issue worth fixing in this PR.
- "low":      minor issue or improvement; safe to defer.
- "info":     observation only; no action required.
NEVER use "blocking" for style/test/perf nits or for theoretical risks.
`)
}

// outputContract returns the canonical JSON-only output instructions used by
// every critic prompt.
func outputContract() string {
	return strings.TrimSpace(`
OUTPUT CONTRACT:
- Respond with a single JSON object wrapped in <output> ... </output> tags.
- No prose before or after. No markdown code fences. No commentary.
- All fields below are required unless marked optional.
- "file" MUST appear in the FILES section above.
- "line_start" MUST appear in VALID LINES for that file (or be 0 for file-level findings).
- "severity" MUST be one of: blocking, high, medium, low, info.
`)
}

// buildFileSummary returns a per-file summary block: path, kind, status,
// added/removed counts, and hunk ranges. This is the model's source of truth
// for what files exist and what kind of file they are.
func buildFileSummary(d *diff.Diff) string {
	if d == nil || len(d.Files) == 0 {
		return "(no files)"
	}
	var b strings.Builder
	b.WriteString("FILES:\n")
	for i := range d.Files {
		f := &d.Files[i]
		kind := diff.FileKind(f.Path)
		status := f.Status()
		b.WriteString(fmt.Sprintf("- %s | kind=%s | status=%s | +%d/-%d | hunks=%s\n",
			f.Path, kind, status, len(f.Added), len(f.Removed), f.HunkRanges()))
	}
	return strings.TrimRight(b.String(), "\n")
}

// buildValidLines returns the per-file allowed line list, in the form expected
// by post-parse validation. Models that follow the prompt will only emit
// lines from this set.
func buildValidLines(d *diff.Diff) string {
	if d == nil || len(d.Files) == 0 {
		return "(none)"
	}
	var b strings.Builder
	b.WriteString("VALID LINES (line_start MUST be from this set; LineStart=0 means file-level):\n")
	for i := range d.Files {
		f := &d.Files[i]
		side := "RIGHT"
		if f.IsDelete {
			side = "LEFT"
		}
		b.WriteString(fmt.Sprintf("- %s [%s] %s\n", f.Path, side, f.HunkRanges()))
	}
	return strings.TrimRight(b.String(), "\n")
}

// buildNumberedDiff renders a per-file numbered, hunk-anchored view. Each
// added/removed/context line is prefixed with its line number on the relevant
// side. This is the only line-source critics are allowed to cite.
func buildNumberedDiff(d *diff.Diff, maxChars int) string {
	if d == nil || len(d.Files) == 0 {
		return "(empty diff)"
	}
	var b strings.Builder
	for i := range d.Files {
		f := &d.Files[i]
		// Per-file header.
		header := fmt.Sprintf("--- FILE: %s (kind=%s, status=%s)\n", f.Path, diff.FileKind(f.Path), f.Status())
		if b.Len()+len(header) > maxChars {
			break
		}
		b.WriteString(header)

		// Walk hunks. We re-parse the patch body to recover line numbers
		// because FileDiff doesn't store the per-hunk header order.
		written := writeNumberedHunks(&b, f, maxChars-b.Len())
		if !written {
			break
		}
		b.WriteString("\n")
	}
	out := strings.TrimRight(b.String(), "\n")
	if len(out) >= maxChars {
		out += "\n... (numbered diff truncated)"
	}
	return out
}

// writeNumberedHunks writes one file's hunks with line numbers. Returns true
// when at least the file's header could fit; false if we ran out of budget.
func writeNumberedHunks(b *strings.Builder, f *diff.FileDiff, budget int) bool {
	if budget <= 0 {
		return false
	}
	// We need to re-derive new/old line numbers. The simplest reliable path
	// is to walk the FileDiff.Patch which contains hunk bodies *with* their
	// `@@ -a,b +c,d @@` headers when the patch came from `git diff`. When the
	// FileDiff was produced by ParseHunkBody we also have headers in Patch.
	patch := f.Patch
	if patch == "" {
		return true
	}
	newLine := 0
	oldLine := 0
	inHunk := false
	for _, raw := range strings.Split(patch, "\n") {
		if budget <= 0 {
			return true
		}
		if strings.HasPrefix(raw, "@@") {
			ol, nl, ok := parseHunkHeaderForPrompt(raw)
			if !ok {
				inHunk = false
				continue
			}
			oldLine, newLine = ol, nl
			inHunk = true
			line := raw + "\n"
			if budget-len(line) < 0 {
				return true
			}
			b.WriteString(line)
			budget -= len(line)
			continue
		}
		if !inHunk || raw == "" {
			continue
		}
		var line string
		switch raw[0] {
		case '+':
			line = fmt.Sprintf("+ %5d | %s\n", newLine, raw[1:])
			newLine++
		case '-':
			line = fmt.Sprintf("- %5d | %s\n", oldLine, raw[1:])
			oldLine++
		case '\\':
			continue
		case ' ':
			line = fmt.Sprintf("  %5d | %s\n", newLine, raw[1:])
			newLine++
			oldLine++
		default:
			line = fmt.Sprintf("  %5d | %s\n", newLine, raw)
			newLine++
			oldLine++
		}
		if budget-len(line) < 0 {
			b.WriteString("  ... (file truncated)\n")
			return true
		}
		b.WriteString(line)
		budget -= len(line)
	}
	return true
}

// parseHunkHeaderForPrompt extracts old-start/new-start from a hunk header.
func parseHunkHeaderForPrompt(line string) (origStart, newStart int, ok bool) {
	if !strings.HasPrefix(line, "@@") {
		return 0, 0, false
	}
	rest := strings.TrimPrefix(line, "@@")
	end := strings.Index(rest, "@@")
	if end < 0 {
		return 0, 0, false
	}
	header := strings.TrimSpace(rest[:end])
	parts := strings.Fields(header)
	if len(parts) < 2 {
		return 0, 0, false
	}
	orig, ok1 := parseSidedNum(parts[0], '-')
	new_, ok2 := parseSidedNum(parts[1], '+')
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	return orig, new_, true
}

func parseSidedNum(s string, sign byte) (int, bool) {
	if len(s) == 0 || s[0] != sign {
		return 0, false
	}
	s = s[1:]
	if i := strings.IndexByte(s, ','); i >= 0 {
		s = s[:i]
	}
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int(c-'0')
	}
	if n == 0 {
		return 1, true
	}
	return n, true
}

func repairJSON(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "{}"
	}

	if strings.HasPrefix(s, "```") {
		s = stripMarkdownFence(s)
	}

	lastClose := strings.LastIndex(s, "}")
	lastBracket := strings.LastIndex(s, "]")
	endIdx := lastClose
	if lastBracket > endIdx {
		endIdx = lastBracket
	}
	if endIdx > 0 {
		s = s[:endIdx+1]
	}

	s = regexp.MustCompile(`,\s*([}\]])`).ReplaceAllString(s, "$1")

	if strings.HasPrefix(s, "{") && !strings.HasSuffix(s, "}") {
		lastComma := strings.LastIndex(s, ",")
		if lastComma > 0 {
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

	if strings.HasPrefix(s, "{") {
		openBraces := strings.Count(s, "{") - strings.Count(s, "}")
		if openBraces > 0 && strings.HasSuffix(s, "]") {
			s = s[:len(s)-1] + "}"
		}
	}

	return s
}

func stripOutputTags(s string) string {
	if i := strings.Index(s, "<output>"); i >= 0 {
		s = s[i+len("<output>"):]
	}
	if i := strings.Index(s, "</output>"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
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
	if cfg != nil && cfg.Config != nil && len(cfg.Config.Paths.Critical) > 0 {
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
		Patch:          patch,
		NumberedDiff:   buildNumberedDiff(d, maxDiffChars),
		FileSummary:    buildFileSummary(d),
		ValidLines:     buildValidLines(d),
		Stats:          stats,
		CriticalPaths:  criticalPaths,
		SeverityRubric: severityRubric(),
		OutputContract: outputContract(),
		MaxFindings:    8,
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
