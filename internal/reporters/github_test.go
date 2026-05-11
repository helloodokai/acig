package reporters

import (
	"testing"

	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/verdict"
	"github.com/stretchr/testify/require"
)

func diffWithLines(path string, lines ...int) *diff.FileDiff {
	dl := make(map[int]bool, len(lines))
	for _, l := range lines {
		dl[l] = true
	}
	return &diff.FileDiff{Path: path, DiffLines: dl}
}

func TestBuildReviewComments_GroupsByFileAndLine(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "Issue 1"},
			{File: "a.txt", LineStart: 10, Title: "Issue 2"},
			{File: "a.txt", LineStart: 30, Title: "Issue 3"},
			{File: "b.txt", LineStart: 20, Title: "Issue 4"},
		},
	}
	fileDiffs := map[string]*diff.FileDiff{
		"a.txt": diffWithLines("a.txt", 10, 30),
		"b.txt": diffWithLines("b.txt", 20),
	}
	comments, dropped := buildReviewComments(v, []string{"a.txt", "b.txt"}, fileDiffs)

	require.Empty(t, dropped)
	require.Len(t, comments, 3)
	require.Contains(t, comments[0].Body, "Issue 1")
	require.Contains(t, comments[0].Body, "Issue 2")
}

func TestBuildReviewComments_FiltersFilesNotInPR(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "Inline"},
			{File: "missing.go", LineStart: 5, Title: "Outside PR"},
		},
	}
	fileDiffs := map[string]*diff.FileDiff{"a.txt": diffWithLines("a.txt", 10)}

	comments, dropped := buildReviewComments(v, []string{"a.txt"}, fileDiffs)
	require.Len(t, comments, 1)
	require.Equal(t, "a.txt", comments[0].Path)
	require.Len(t, dropped, 1)
	require.Equal(t, "missing.go", dropped[0].File)
}

func TestBuildReviewComments_FindingWithoutFileIsDropped(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "", LineStart: 0, Title: "Truly general"},
			{File: "a.txt", LineStart: 10, Title: "Inline"},
		},
	}
	fileDiffs := map[string]*diff.FileDiff{"a.txt": diffWithLines("a.txt", 10)}

	comments, dropped := buildReviewComments(v, []string{"a.txt"}, fileDiffs)
	require.Len(t, comments, 1)
	require.Len(t, dropped, 1)
	require.Equal(t, "Truly general", dropped[0].Title)
}

func TestBuildReviewComments_MultiLineComment(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, LineEnd: 12, Title: "Multi-line"},
		},
	}
	fileDiffs := map[string]*diff.FileDiff{
		"a.txt": diffWithLines("a.txt", 10, 11, 12),
	}
	comments, _ := buildReviewComments(v, []string{"a.txt"}, fileDiffs)

	require.Len(t, comments, 1)
	require.Equal(t, 12, comments[0].Line)
	require.Equal(t, 10, comments[0].StartLine)
}

func TestBuildReviewComments_LegacyAddedFallback(t *testing.T) {
	// FileDiffs with Added but no DiffLines (used by some legacy callers).
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 5, Title: "Issue"},
		},
	}
	fileDiffs := map[string]*diff.FileDiff{
		"a.txt": {Path: "a.txt", Added: []string{"a", "b", "c", "d", "e", "f"}},
	}
	comments, _ := buildReviewComments(v, []string{"a.txt"}, fileDiffs)
	require.Len(t, comments, 1)
	require.Equal(t, 5, comments[0].Line)
}

func TestSnap_WithinWindow(t *testing.T) {
	set := map[int]bool{10: true, 11: true, 12: true}
	cases := []struct {
		req  int
		want int
		ok   bool
	}{
		{10, 10, true},
		{11, 11, true},
		{13, 12, true}, // snap down
		{9, 10, true},  // snap up
		{15, 12, true}, // 15→12 is within ±3
		{20, 0, false}, // outside window
	}
	for _, c := range cases {
		got, ok := snap(set, c.req)
		require.Equal(t, c.ok, ok, "snap(%d).ok", c.req)
		require.Equal(t, c.want, got, "snap(%d)", c.req)
	}
}
