package diff

import (
	"path/filepath"
	"sort"
	"strings"
)

type FileDiff struct {
	Path      string
	Added     []string
	Removed   []string
	Patch     string
	IsNew     bool
	IsDelete  bool
	DiffLines map[int]bool // new-file line numbers visible in this diff's hunks (added + context)
	OrigLines map[int]bool // old-file line numbers visible in this diff's hunks (removed + context)
}

type Diff struct {
	Files    []FileDiff
	RawPatch string
	Stats    Stats
}

type Stats struct {
	FilesChanged int
	LinesAdded   int
	LinesRemoved int
}

// Kind classifies a file path into a coarse category that critic prompts use
// to apply different rules (e.g. don't flag runtime vulnerabilities in docs).
type Kind string

const (
	KindCode      Kind = "code"
	KindTest      Kind = "test"
	KindDocs      Kind = "docs"
	KindConfig    Kind = "config"
	KindGenerated Kind = "generated"
	KindOther     Kind = "other"
)

// FileKind returns a coarse kind for a file path. Path takes precedence over
// extension (so `__tests__/foo.ts` is `test`, not `code`).
func FileKind(path string) Kind {
	if path == "" {
		return KindOther
	}
	p := strings.ToLower(path)
	base := filepath.Base(p)
	ext := filepath.Ext(p)

	// Tests by path or filename.
	if strings.Contains(p, "/__tests__/") ||
		strings.Contains(p, "/tests/") ||
		strings.Contains(p, "/test/") ||
		strings.HasSuffix(p, "_test.go") ||
		strings.Contains(p, ".test.") ||
		strings.Contains(p, ".spec.") ||
		strings.HasSuffix(p, ".test.ts") ||
		strings.HasSuffix(p, ".test.tsx") ||
		strings.HasSuffix(p, ".test.js") ||
		strings.HasSuffix(p, ".test.jsx") ||
		strings.HasSuffix(p, ".spec.ts") ||
		strings.HasSuffix(p, ".spec.tsx") ||
		strings.HasSuffix(p, ".spec.js") {
		return KindTest
	}

	// Generated / lock / build artifacts.
	switch base {
	case "go.sum", "go.mod", "package-lock.json", "yarn.lock", "pnpm-lock.yaml", "bun.lockb", "cargo.lock", "poetry.lock", "gemfile.lock", "composer.lock":
		return KindGenerated
	}
	if strings.HasSuffix(p, ".min.js") || strings.HasSuffix(p, ".min.css") {
		return KindGenerated
	}
	if hasSegment(p, "node_modules") || hasSegment(p, "dist") || hasSegment(p, "build") || hasSegment(p, ".next") || hasSegment(p, "vendor") {
		return KindGenerated
	}

	// Docs.
	switch ext {
	case ".md", ".mdx", ".markdown", ".rst", ".txt", ".adoc":
		return KindDocs
	}
	if base == "license" || base == "license.md" || base == "readme" || base == "readme.md" || base == "changelog" || base == "changelog.md" {
		return KindDocs
	}

	// Config.
	switch ext {
	case ".toml", ".yaml", ".yml", ".json", ".ini", ".env":
		return KindConfig
	}
	if base == "dockerfile" || strings.HasPrefix(base, "dockerfile.") {
		return KindConfig
	}
	if base == "makefile" || strings.HasSuffix(p, ".mk") {
		return KindConfig
	}

	// Code by extension.
	switch ext {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs",
		".py", ".rb", ".java", ".kt", ".scala",
		".rs", ".c", ".cc", ".cpp", ".cxx", ".h", ".hpp", ".hxx",
		".cs", ".php", ".swift", ".m", ".mm",
		".sh", ".bash", ".zsh", ".fish",
		".sql", ".html", ".css", ".scss", ".less":
		return KindCode
	}
	return KindOther
}

// Status describes the file's diff-level status.
type Status string

const (
	StatusModified Status = "modified"
	StatusAdded    Status = "added"
	StatusDeleted  Status = "deleted"
)

// FileStatus returns the Status for a FileDiff.
func (fd *FileDiff) Status() Status {
	switch {
	case fd.IsNew:
		return StatusAdded
	case fd.IsDelete:
		return StatusDeleted
	default:
		return StatusModified
	}
}

// HunkRanges returns the new-file line ranges visible in the diff as
// "[start-end, start-end]" e.g. "[42-46, 78-91]". When the file is a deletion
// the original-side line ranges are returned instead.
func (fd *FileDiff) HunkRanges() string {
	var lines []int
	if fd.IsDelete {
		for l := range fd.OrigLines {
			lines = append(lines, l)
		}
	} else {
		for l := range fd.DiffLines {
			lines = append(lines, l)
		}
	}
	if len(lines) == 0 {
		return "[]"
	}
	sort.Ints(lines)
	var ranges []string
	start := lines[0]
	prev := lines[0]
	for i := 1; i < len(lines); i++ {
		if lines[i] == prev+1 {
			prev = lines[i]
			continue
		}
		if start == prev {
			ranges = append(ranges, itoa(start))
		} else {
			ranges = append(ranges, itoa(start)+"-"+itoa(prev))
		}
		start = lines[i]
		prev = lines[i]
	}
	if start == prev {
		ranges = append(ranges, itoa(start))
	} else {
		ranges = append(ranges, itoa(start)+"-"+itoa(prev))
	}
	return "[" + strings.Join(ranges, ", ") + "]"
}

// hasSegment reports whether the slash-separated path contains the given
// directory segment exactly (so "node_modules/x.js" matches "node_modules"
// but "fnode_modules" does not).
func hasSegment(path, segment string) bool {
	for _, p := range strings.Split(path, "/") {
		if p == segment {
			return true
		}
	}
	return false
}

func itoa(i int) string {
	// avoid pulling strconv into a heavily used helper
	if i == 0 {
		return "0"
	}
	neg := false
	if i < 0 {
		neg = true
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}
