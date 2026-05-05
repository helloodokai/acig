package fix

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"github.com/helloodokai/acig/internal/budget"
	"github.com/helloodokai/acig/internal/config"
	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/models"
	"github.com/helloodokai/acig/internal/pipeline"
	"github.com/helloodokai/acig/internal/routing"
	"github.com/helloodokai/acig/internal/verdict"
)

//go:embed prompt_fix.md
var fixPromptTmpl string

type Group struct {
	File     string
	Findings []verdict.Finding
}

type Result struct {
	Group    Group
	Patch    string
	Applied  bool
	CommitSHA string
	Error    string
}

type Options struct {
	Profile    string
	BudgetUSD  float64
	DiffRange  string
	MaxIter    int
	DryRun     bool
	NoPush     bool
	BranchPrefix string
}

func Run(ctx context.Context, cfg *config.Config, opts Options) ([]Result, error) {
	if opts.MaxIter <= 0 {
		opts.MaxIter = 10
	}
	if opts.BranchPrefix == "" {
		opts.BranchPrefix = "acig-fix"
	}

	d, err := resolveDiff(opts.DiffRange)
	if err != nil {
		return nil, fmt.Errorf("getting diff: %w", err)
	}
	if d.Stats.FilesChanged == 0 {
		slog.Info("no changes to fix")
		return nil, nil
	}

	ledger, err := budget.NewLedger(cfg.Budget.PerRunUSD)
	if err != nil {
		return nil, fmt.Errorf("initializing budget: %w", err)
	}

	repo := detectRepo()
	sha := detectSHA()

	router := routing.NewRouter(cfg)
	if opts.Profile != "" {
		cfg.Models.DefaultProfile = opts.Profile
	}

	pipe := pipeline.New(cfg, router, ledger, d, nil)
	v, err := pipe.Execute(ctx, repo, sha, "")
	if err != nil {
		return nil, fmt.Errorf("pipeline execution: %w", err)
	}

	if len(v.Findings) == 0 {
		slog.Info("no findings to fix")
		return nil, nil
	}

	groups := groupByFile(v.Findings)
	slog.Info("grouped findings for fix", "files", len(groups), "findings", len(v.Findings))

	branchName := fmt.Sprintf("%s/%s", opts.BranchPrefix, sha[:8])
	if err := createBranch(branchName); err != nil {
		return nil, fmt.Errorf("creating branch: %w", err)
	}
	slog.Info("created fix branch", "branch", branchName)

	repoRoot, err := repoRootDir()
	if err != nil {
		return nil, fmt.Errorf("detecting repo root: %w", err)
	}

	var results []Result
	applied := 0

	for i, group := range groups {
		if i >= opts.MaxIter {
			slog.Warn("max iterations reached, stopping", "max", opts.MaxIter)
			break
		}
		if ledger.Exhausted() {
			slog.Warn("budget exhausted, stopping")
			break
		}

		if !isFixableFile(group.File) {
			slog.Info("skipping non-fixable group", "file", group.File, "findings", len(group.Findings))
			results = append(results, Result{Group: group, Error: "findings not associated with a fixable file"})
			continue
		}

		slog.Info("fixing group", "file", group.File, "findings", len(group.Findings), "iteration", i+1)
		patch, err := generatePatch(ctx, cfg, router, ledger, group, d)
		if err != nil {
			results = append(results, Result{Group: group, Error: err.Error()})
			continue
		}

		if opts.DryRun {
			slog.Info("dry run: would apply patch", "file", group.File, "patch_len", len(patch))
			results = append(results, Result{Group: group, Patch: patch})
			continue
		}

		appliedOk, err := applyPatch(patch, group.File, repoRoot)
		if err != nil || !appliedOk {
			msg := "patch apply failed"
			if err != nil {
				msg = err.Error()
			}
			slog.Warn("patch apply failed", "file", group.File, "error", msg)
			results = append(results, Result{Group: group, Patch: patch, Error: msg})
			continue
		}

		commitSHA, err := commitFix(group, repoRoot)
		if err != nil {
			slog.Warn("commit failed", "error", err)
			results = append(results, Result{Group: group, Patch: patch, Error: err.Error()})
			continue
		}

		slog.Info("fix committed", "file", group.File, "sha", commitSHA)
		results = append(results, Result{
			Group:     group,
			Patch:     patch,
			Applied:   true,
			CommitSHA: commitSHA,
		})
		applied++
	}

	if applied > 0 && !opts.NoPush && !opts.DryRun {
		if err := pushBranch(branchName); err != nil {
			slog.Error("push failed", "error", err)
			return results, fmt.Errorf("push failed: %w", err)
		}
		slog.Info("pushed branch", "branch", branchName)
	}

	return results, nil
}

func groupByFile(findings []verdict.Finding) []Group {
	groups := map[string][]verdict.Finding{}
	for _, f := range findings {
		file := f.File
		if file == "" {
			file = "unknown"
		}
		groups[file] = append(groups[file], f)
	}

	var result []Group
	for file, fs := range groups {
		sort.Slice(fs, func(i, j int) bool {
			return fs[i].Severity > fs[j].Severity
		})
		result = append(result, Group{File: file, Findings: fs})
	}
	sort.Slice(result, func(i, j int) bool {
		return len(result[i].Findings) > len(result[j].Findings)
	})
	return result
}

func generatePatch(ctx context.Context, cfg *config.Config, router *routing.Router, ledger *budget.Ledger, group Group, d *diff.Diff) (string, error) {
	client, modelName, err := router.ClientForTier("frontier")
	if err != nil {
		return "", fmt.Errorf("getting frontier client: %w", err)
	}

	fileContent, err := readFileContent(group.File)
	if err != nil {
		slog.Warn("could not read file for fix context", "file", group.File, "error", err)
	}

	findingDescs := make([]string, len(group.Findings))
	for i, f := range group.Findings {
		findingDescs[i] = fmt.Sprintf("- [%s] %s: %s", f.Severity, f.Title, f.Detail)
		if f.SuggestedFix != "" {
			findingDescs[i] += fmt.Sprintf("\n  Suggested fix: %s", f.SuggestedFix)
		}
	}

	data := map[string]interface{}{
		"File":          group.File,
		"FileContent":   fileContent,
		"Findings":      strings.Join(findingDescs, "\n"),
		"FindingCount":  len(group.Findings),
	}

	tmpl, err := template.New("fix").Parse(fixPromptTmpl)
	if err != nil {
		return "", fmt.Errorf("parsing fix template: %w", err)
	}

	var buf strings.Builder
	if execErr := tmpl.Execute(&buf, data); execErr != nil {
		return "", fmt.Errorf("executing fix template: %w", execErr)
	}

	start := ledger.Remaining()
	resp, err := client.Chat(ctx, models.ChatRequest{
		Model:       modelName,
		Messages:    []models.ChatMessage{{Role: "user", Content: buf.String()}},
		MaxTokens:   8096,
		Temperature: 0.1,
	})
	if err != nil {
		return "", fmt.Errorf("frontier chat: %w", err)
	}

	cost := 0.0
	if start > 0 {
		cost = start - ledger.Remaining()
	}
	_ = cost

	patch := extractPatch(resp.Content)
	if patch == "" {
		return "", fmt.Errorf("model returned no patch")
	}
	return patch, nil
}

var patchRe = regexp.MustCompile("(?s)```diff\n(.*?)```")

func extractPatch(content string) string {
	matches := patchRe.FindStringSubmatch(content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	if strings.Contains(content, "---") && strings.Contains(content, "@@") {
		lines := strings.Split(content, "\n")
		var inPatch bool
		var patchLines []string
		for _, line := range lines {
			if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "diff --git") {
				inPatch = true
			}
			if inPatch {
				patchLines = append(patchLines, line)
			}
		}
		if len(patchLines) > 0 {
			return strings.Join(patchLines, "\n")
		}
	}
	return strings.TrimSpace(content)
}

func applyPatch(patch, targetFile, repoRoot string) (bool, error) {
	tmpFile, err := os.CreateTemp("", "acig-fix-*.patch")
	if err != nil {
		return false, fmt.Errorf("creating temp patch file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, writeErr := tmpFile.WriteString(patch); writeErr != nil {
		tmpFile.Close()
		return false, fmt.Errorf("writing patch: %w", writeErr)
	}
	tmpFile.Close()

	checkCmd := exec.Command("git", "apply", "--check", "-p1", tmpFile.Name())
	checkCmd.Dir = repoRoot
	out, err := checkCmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("patch does not apply cleanly: %s", strings.TrimSpace(string(out)))
	}

	applyCmd := exec.Command("git", "apply", "-p1", tmpFile.Name())
	applyCmd.Dir = repoRoot
	if out, err := applyCmd.CombinedOutput(); err != nil {
		return false, fmt.Errorf("applying patch: %s: %w", strings.TrimSpace(string(out)), err)
	}

	return true, nil
}

func commitFix(group Group, repoRoot string) (string, error) {
	addCmd := exec.Command("git", "add", group.File)
	addCmd.Dir = repoRoot
	if out, err := addCmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git add: %s: %w", strings.TrimSpace(string(out)), err)
	}

	titles := make([]string, 0, 3)
	for _, f := range group.Findings {
		titles = append(titles, f.Title)
		if len(titles) == 3 {
			titles = append(titles, fmt.Sprintf("and %d more", len(group.Findings)-3))
			break
		}
	}
	msg := fmt.Sprintf("fix(acig): %s", strings.Join(titles, ", "))

	cmd := exec.Command("git", "commit", "-m", msg)
	cmd.Dir = repoRoot
	if out, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("git commit: %s: %w", strings.TrimSpace(string(out)), err)
	}

	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func createBranch(name string) error {
	cmd := exec.Command("git", "checkout", "-b", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(out), "already exists") {
			cmd = exec.Command("git", "checkout", name)
			if out2, branchErr := cmd.CombinedOutput(); branchErr != nil {
				return fmt.Errorf("checkout existing branch: %s: %w", strings.TrimSpace(string(out2)), branchErr)
			}
			return nil
		}
		return fmt.Errorf("create branch: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func pushBranch(name string) error {
	cmd := exec.Command("git", "push", "-u", "origin", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("push: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return nil
}

func readFileContent(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	content := string(data)
	if len(content) > 16000 {
		content = content[:16000] + "\n... (truncated)"
	}
	return content, nil
}

func repoRootDir() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --show-toplevel: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func isFixableFile(file string) bool {
	if file == "" || file == "unknown" || file == "diff" {
		return false
	}
	if strings.HasPrefix(file, "/dev/") {
		return false
	}
	return true
}

func detectRepo() string {
	out, err := exec.Command("git", "remote", "get-url", "origin").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func detectSHA() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func CreatePR(ctx context.Context, results []Result, opts Options) error {
	if len(results) == 0 {
		return nil
	}

	applied := 0
	for _, r := range results {
		if r.Applied {
			applied++
		}
	}
	if applied == 0 {
		return nil
	}

	sha := detectSHA()
	branchName := fmt.Sprintf("%s/%s", opts.BranchPrefix, sha[:8])

	var body strings.Builder
	body.WriteString("## acig auto-fix\n\n")
	body.WriteString(fmt.Sprintf("Fixes %d finding(s) across %d file(s).\n\n", applied, len(results)))
	body.WriteString("| File | Findings | Status |\n|------|----------|--------|\n")
	for _, r := range results {
		status := "skipped"
		if r.Applied {
			status = "fixed"
		} else if r.Error != "" {
			status = "error: " + r.Error
		}
		body.WriteString(fmt.Sprintf("| %s | %d | %s |\n", r.Group.File, len(r.Group.Findings), status))
	}

	title := fmt.Sprintf("fix(acig): auto-fix %d finding(s)", applied)

	cmd := exec.Command("gh", "pr", "create",
		"--title", title,
		"--body", body.String(),
		"--head", branchName,
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("creating PR: %s: %w", strings.TrimSpace(string(out)), err)
	}
	slog.Info("created PR", "url", strings.TrimSpace(string(out)))
	return nil
}

func resolveDiff(diffRange string) (*diff.Diff, error) {
	if strings.HasPrefix(diffRange, "pr:") {
		prRef := strings.TrimPrefix(diffRange, "pr:")
		d, _, err := diff.FromPR(prRef)
		return d, err
	}
	return diff.FromGitRange(diffRange)
}