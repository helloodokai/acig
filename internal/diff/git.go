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
			Path:      newPath(f),
			IsNew:     f.OrigName == "/dev/null",
			IsDelete:  f.NewName == "/dev/null",
			Patch:     hunksBody.String(),
			DiffLines: make(map[int]bool),
			OrigLines: make(map[int]bool),
		}

		for _, h := range f.Hunks {
			newLineNum := int(h.NewStartLine)
			origLineNum := int(h.OrigStartLine)
			lines := bytes.Split(h.Body, []byte{'\n'})
			for _, l := range lines {
				line := string(l)
				switch {
				case strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++"):
					fd.Added = append(fd.Added, strings.TrimPrefix(line, "+"))
					fd.DiffLines[newLineNum] = true
					newLineNum++
					d.Stats.LinesAdded++
				case strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---"):
					fd.Removed = append(fd.Removed, strings.TrimPrefix(line, "-"))
					fd.OrigLines[origLineNum] = true
					origLineNum++
					d.Stats.LinesRemoved++
				case strings.HasPrefix(line, "\\"):
					// "No newline at end of file" — skip without advancing.
				default:
					if line != "" {
						fd.DiffLines[newLineNum] = true
						fd.OrigLines[origLineNum] = true
						newLineNum++
						origLineNum++
					}
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

// ParseHunkBody parses a single-file unified-diff hunk body (the form returned
// by GitHub's PR ListFiles API in the `Patch` field). The hunk body has no
// `--- a/` / `+++ b/` headers but does contain `@@` hunk headers. Returns a
// FileDiff populated with Added, Removed, DiffLines, and OrigLines.
//
// This is what the GitHub client uses to translate a PR's per-file patch into
// validated line sets without re-implementing diff parsing.
func ParseHunkBody(path, patch string, isNew, isDelete bool) *FileDiff {
	fd := &FileDiff{
		Path:      path,
		IsNew:     isNew,
		IsDelete:  isDelete,
		Patch:     patch,
		DiffLines: make(map[int]bool),
		OrigLines: make(map[int]bool),
	}
	if patch == "" {
		return fd
	}

	newLineNum := 0
	origLineNum := 0
	inHunk := false

	for _, raw := range strings.Split(patch, "\n") {
		if strings.HasPrefix(raw, "@@") {
			// e.g. @@ -42,5 +42,8 @@ optional context
			origStart, newStart, ok := parseHunkHeader(raw)
			if !ok {
				inHunk = false
				continue
			}
			origLineNum = origStart
			newLineNum = newStart
			inHunk = true
			continue
		}
		if !inHunk {
			continue
		}
		if raw == "" {
			// Empty trailing line in split — treat as context only when both
			// counters are valid.
			continue
		}
		switch raw[0] {
		case '+':
			fd.Added = append(fd.Added, raw[1:])
			fd.DiffLines[newLineNum] = true
			newLineNum++
		case '-':
			fd.Removed = append(fd.Removed, raw[1:])
			fd.OrigLines[origLineNum] = true
			origLineNum++
		case '\\':
			// "No newline at end of file" marker.
		case ' ':
			fd.DiffLines[newLineNum] = true
			fd.OrigLines[origLineNum] = true
			newLineNum++
			origLineNum++
		default:
			// Anything else (e.g. blank line in older formats) — treat as
			// context to stay forgiving.
			fd.DiffLines[newLineNum] = true
			fd.OrigLines[origLineNum] = true
			newLineNum++
			origLineNum++
		}
	}
	return fd
}

// parseHunkHeader extracts old-start and new-start line numbers from a
// `@@ -orig,len +new,len @@ ...` header. Length defaults to 1 when omitted.
func parseHunkHeader(line string) (origStart, newStart int, ok bool) {
	// Must start with "@@".
	if !strings.HasPrefix(line, "@@") {
		return 0, 0, false
	}
	// Find the substring between the first "@@" and the next "@@".
	rest := strings.TrimPrefix(line, "@@")
	end := strings.Index(rest, "@@")
	if end < 0 {
		return 0, 0, false
	}
	header := strings.TrimSpace(rest[:end])
	// header looks like "-42,5 +42,8" or "-42 +42".
	parts := strings.Fields(header)
	if len(parts) < 2 {
		return 0, 0, false
	}
	orig, ok1 := parseHunkSide(parts[0], '-')
	new_, ok2 := parseHunkSide(parts[1], '+')
	if !ok1 || !ok2 {
		return 0, 0, false
	}
	return orig, new_, true
}

func parseHunkSide(s string, sign byte) (int, bool) {
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
		// `@@ -0,0 +N,M @@` is valid for new files (orig start is 0, no orig
		// lines). Returning 1 lets callers treat the first context/added line
		// as line 1, but for new files there is no original side anyway.
		return 1, true
	}
	return n, true
}
