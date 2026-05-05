package diff

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"

	diffParse "github.com/sourcegraph/go-diff/diff"
)

func FromGitRange(refRange string) (*Diff, error) {
	cmd := exec.Command("git", "diff", "--unified=5", refRange)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git diff %s: %w", refRange, err)
	}
	return Parse(string(out))
}

func FromFiles(patch string) (*Diff, error) {
	return Parse(patch)
}

func FromPR(prRef string) (*Diff, string, error) {
	cmd := exec.Command("gh", "pr", "diff", prRef)
	out, err := cmd.Output()
	if err != nil {
		return nil, "", fmt.Errorf("gh pr diff %s: %w", prRef, err)
	}

	d, parseErr := Parse(string(out))
	if parseErr != nil {
		return nil, "", parseErr
	}

	baseRef := detectPRBase(prRef)
	return d, baseRef, nil
}

func detectPRBase(prRef string) string {
	out, err := exec.Command("gh", "pr", "view", prRef, "--json", "baseRefName", "--jq", ".baseRefName").Output()
	if err != nil {
		return "main"
	}
	return strings.TrimSpace(string(out))
}

func AutoDetectRange() (string, error) {
	out, err := exec.Command("git", "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}").Output()
	if err != nil {
		out, err = exec.Command("git", "rev-parse", "HEAD~1").Output()
		if err != nil {
			return "", fmt.Errorf("cannot determine upstream ref: %w", err)
		}
		return strings.TrimSpace(string(out)) + "..HEAD", nil
	}
	return strings.TrimSpace(string(out)) + "..HEAD", nil
}

func Parse(patch string) (*Diff, error) {
	if patch == "" {
		return &Diff{}, nil
	}

	files, err := diffParse.ParseMultiFileDiff([]byte(patch))
	if err != nil {
		return nil, fmt.Errorf("parsing diff: %w", err)
	}

	d := &Diff{RawPatch: patch}
	for _, f := range files {
		var hunksBody strings.Builder
		for _, h := range f.Hunks {
			hunksBody.Write(h.Body)
		}

		fd := FileDiff{
			Path:     newPath(f),
			IsNew:    f.OrigName == "/dev/null",
			IsDelete: f.NewName == "/dev/null",
			Patch:    hunksBody.String(),
		}

		for _, h := range f.Hunks {
			lines := bytes.Split(h.Body, []byte{'\n'})
			for _, l := range lines {
				line := string(l)
				switch {
				case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
					fd.Added = append(fd.Added, strings.TrimPrefix(line, "+"))
					d.Stats.LinesAdded++
				case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
					fd.Removed = append(fd.Removed, strings.TrimPrefix(line, "-"))
					d.Stats.LinesRemoved++
				}
			}
		}

		d.Files = append(d.Files, fd)
		d.Stats.FilesChanged++
	}

	return d, nil
}

func newPath(f *diffParse.FileDiff) string {
	switch {
	case f.NewName != "" && f.NewName != "/dev/null":
		return strings.TrimPrefix(f.NewName, "b/")
	case f.OrigName != "" && f.OrigName != "/dev/null":
		return strings.TrimPrefix(f.OrigName, "a/")
	default:
		return "unknown"
	}
}