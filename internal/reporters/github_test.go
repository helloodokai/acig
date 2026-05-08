package reporters

import (
	"testing"

	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/verdict"
	"github.com/stretchr/testify/require"
)

func TestGroupFindings(t *testing.T) {
	findings := []verdict.Finding{
		{File: "a.txt", LineStart: 10, LineEnd: 10, Title: "Issue A"},
		{File: "a.txt", LineStart: 10, LineEnd: 10, Title: "Issue A2"},
		{File: "b.txt", LineStart: 20, LineEnd: 20, Title: "Issue B"},
		{File: "a.txt", LineStart: 30, LineEnd: 30, Title: "Issue A3"},
	}

	groups := groupFindings(findings)
	require.Len(t, groups, 3)

	require.Len(t, groups[0], 2)
	require.Equal(t, "a.txt", groups[0][0].File)
	require.Equal(t, 10, groups[0][0].LineStart)

	require.Len(t, groups[1], 1)
	require.Equal(t, "b.txt", groups[1][0].File)

	require.Len(t, groups[2], 1)
	require.Equal(t, 30, groups[2][0].LineStart)
}

func TestGroupFindings_EmptyFile(t *testing.T) {
	findings := []verdict.Finding{
		{File: "", LineStart: 10, Title: "No file"},
		{File: "a.txt", LineStart: 20, Title: "Has file"},
	}

	groups := groupFindings(findings)
	require.Len(t, groups, 1)
	require.Equal(t, "a.txt", groups[0][0].File)
}

func TestGroupFindings_ZeroLineStart(t *testing.T) {
	findings := []verdict.Finding{
		{File: "a.txt", LineStart: 0, Title: "Zero line"},
		{File: "a.txt", LineStart: 0, Title: "Another zero line"},
		{File: "b.txt", LineStart: 10, Title: "Valid line"},
	}

	groups := groupFindings(findings)
	require.Len(t, groups, 2)
	require.Equal(t, "a.txt", groups[0][0].File)
	require.Len(t, groups[0], 2)
	require.Equal(t, "b.txt", groups[1][0].File)
}

// hr is a helper to create HunkRange slices
func hr(ranges ...diff.HunkRange) []diff.HunkRange {
	return ranges
}

func TestBuildReviewComments_FiltersNonPRFiles(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "Issue A"},
			{File: "deleted.txt", LineStart: 20, Title: "Issue in deleted file"},
			{File: "b.txt", LineStart: 30, Title: "Issue B"},
		},
	}

	prFiles := []string{"a.txt", "b.txt", "c.txt"}
	fileDiffs := map[string]*diff.FileDiff{
		"a.txt": {Path: "a.txt", StartLine: 5, HunkRanges: hr(diff.HunkRange{Start: 5, End: 15}), Added: []string{"l5", "l6", "l7", "l8", "l9", "l10"}},
		"b.txt": {Path: "b.txt", StartLine: 25, HunkRanges: hr(diff.HunkRange{Start: 25, End: 35}), Added: []string{"l25", "l26", "l27", "l28", "l29", "l30"}},
	}
	comments := buildReviewComments(v, prFiles, fileDiffs)

	require.Len(t, comments, 2)
	require.Equal(t, "a.txt", comments[0].Path)
	require.Equal(t, "b.txt", comments[1].Path)
}

func TestBuildReviewComments_NoCommentsWhenFileDiffsNil(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "Issue A"},
			{File: "deleted.txt", LineStart: 20, Title: "Issue in deleted file"},
		},
	}

	comments := buildReviewComments(v, nil, nil)

	require.Len(t, comments, 0)
}

func TestBuildReviewComments_EmptyPRFiles(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 5, Title: "Issue A"},
		},
	}

	fileDiffs := map[string]*diff.FileDiff{
		"a.txt": {Path: "a.txt", StartLine: 1, HunkRanges: hr(diff.HunkRange{Start: 1, End: 10}), Added: []string{"l1", "l2", "l3", "l4", "l5"}},
	}
	comments := buildReviewComments(v, []string{}, fileDiffs)

	require.Len(t, comments, 1)
}

func TestBuildReviewComments_MultiLineComment(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, LineEnd: 20, Title: "Multi-line issue"},
		},
	}

	fileDiffs := map[string]*diff.FileDiff{
		"a.txt": {Path: "a.txt", StartLine: 1, HunkRanges: hr(diff.HunkRange{Start: 1, End: 30}), Added: make([]string, 30)},
	}
	comments := buildReviewComments(v, []string{"a.txt"}, fileDiffs)

	require.Len(t, comments, 1)
	require.Equal(t, 20, comments[0].Line)
	require.Equal(t, 10, comments[0].StartLine)
}

func TestBuildReviewComments_SingleLineComment(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, LineEnd: 10, Title: "Single line issue"},
		},
	}

	fileDiffs := map[string]*diff.FileDiff{
		"a.txt": {Path: "a.txt", StartLine: 1, HunkRanges: hr(diff.HunkRange{Start: 1, End: 20}), Added: make([]string, 20)},
	}
	comments := buildReviewComments(v, []string{"a.txt"}, fileDiffs)

	require.Len(t, comments, 1)
	require.Equal(t, 10, comments[0].Line)
	require.Equal(t, 0, comments[0].StartLine)
}

func TestBuildReviewComments_GroupsFindingsByFileAndLine(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "Issue 1"},
			{File: "a.txt", LineStart: 10, Title: "Issue 2"},
			{File: "a.txt", LineStart: 30, Title: "Issue 3"},
			{File: "b.txt", LineStart: 20, Title: "Issue 4"},
		},
	}

	fileDiffs := map[string]*diff.FileDiff{
		"a.txt": {Path: "a.txt", StartLine: 1, HunkRanges: hr(diff.HunkRange{Start: 1, End: 50}), Added: make([]string, 50)},
		"b.txt": {Path: "b.txt", StartLine: 1, HunkRanges: hr(diff.HunkRange{Start: 1, End: 30}), Added: make([]string, 30)},
	}
	comments := buildReviewComments(v, []string{"a.txt", "b.txt"}, fileDiffs)

	require.Len(t, comments, 3)
	require.Contains(t, comments[0].Body, "Issue 1")
	require.Contains(t, comments[0].Body, "Issue 2")
}

func TestValidateLine(t *testing.T) {
	tests := []struct {
		name          string
		fd            *diff.FileDiff
		requestedLine int
		expected      int
	}{
		{
			name:          "within hunk range",
			fd:            &diff.FileDiff{StartLine: 50, HunkRanges: hr(diff.HunkRange{Start: 50, End: 55}), Added: []string{"l1", "l2"}},
			requestedLine: 51,
			expected:      51,
		},
		{
			name:          "at hunk start",
			fd:            &diff.FileDiff{StartLine: 50, HunkRanges: hr(diff.HunkRange{Start: 50, End: 55}), Added: []string{"l1", "l2"}},
			requestedLine: 50,
			expected:      50,
		},
		{
			name:          "context line within hunk range",
			fd:            &diff.FileDiff{StartLine: 50, HunkRanges: hr(diff.HunkRange{Start: 50, End: 55}), Added: []string{"l1", "l2"}},
			requestedLine: 53,
			expected:      53,
		},
		{
			name:          "at hunk end",
			fd:            &diff.FileDiff{StartLine: 50, HunkRanges: hr(diff.HunkRange{Start: 50, End: 55}), Added: []string{"l1", "l2"}},
			requestedLine: 55,
			expected:      55,
		},
		{
			name:          "beyond hunk range clamps to nearest start",
			fd:            &diff.FileDiff{StartLine: 50, HunkRanges: hr(diff.HunkRange{Start: 50, End: 55}), Added: []string{"l1", "l2"}},
			requestedLine: 100,
			expected:      50,
		},
		{
			name:          "before hunk start clamps to start",
			fd:            &diff.FileDiff{StartLine: 50, HunkRanges: hr(diff.HunkRange{Start: 50, End: 55}), Added: []string{"l1", "l2"}},
			requestedLine: 10,
			expected:      50,
		},
		{
			name:          "nil file diff returns 0",
			fd:            nil,
			requestedLine: 10,
			expected:      0,
		},
		{
			name:          "deleted file returns 0",
			fd:            &diff.FileDiff{StartLine: 1, IsDelete: true, HunkRanges: hr(diff.HunkRange{Start: 1, End: 10}), Added: []string{"l1"}},
			requestedLine: 1,
			expected:      0,
		},
		{
			name:          "empty hunk ranges returns 0",
			fd:            &diff.FileDiff{StartLine: 1, HunkRanges: nil, Added: []string{"l1"}},
			requestedLine: 1,
			expected:      0,
		},
		{
			name:          "line in second hunk range",
			fd:            &diff.FileDiff{StartLine: 10, HunkRanges: hr(diff.HunkRange{Start: 10, End: 20}, diff.HunkRange{Start: 50, End: 60}), Added: []string{"l1"}},
			requestedLine: 55,
			expected:      55,
		},
		{
			name:          "line between hunk ranges clamps to nearest",
			fd:            &diff.FileDiff{StartLine: 10, HunkRanges: hr(diff.HunkRange{Start: 10, End: 20}, diff.HunkRange{Start: 50, End: 60}), Added: []string{"l1"}},
			requestedLine: 35,
			expected:      50,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.fd != nil && tt.fd.HunkRanges != nil {
				result := validateLine(tt.fd, tt.requestedLine)
				require.Equal(t, tt.expected, result)
			} else {
				result := validateLine(tt.fd, tt.requestedLine)
				require.Equal(t, tt.expected, result)
			}
		})
	}
}

func TestBuildReviewComments_LineInHunkRange(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "app.go", LineStart: 52, LineEnd: 52, Title: "Issue at line 52"},
		},
	}

	fileDiffs := map[string]*diff.FileDiff{
		"app.go": {Path: "app.go", StartLine: 50, HunkRanges: hr(diff.HunkRange{Start: 50, End: 55}), Added: []string{"l50", "l51", "l52", "l53", "l54"}},
	}
	comments := buildReviewComments(v, []string{"app.go"}, fileDiffs)

	require.Len(t, comments, 1)
	require.Equal(t, "app.go", comments[0].Path)
	require.Equal(t, 52, comments[0].Line)
}

func TestBuildReviewComments_ContextLineInRange(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "app.go", LineStart: 51, LineEnd: 51, Title: "Issue on context line"},
		},
	}

	fileDiffs := map[string]*diff.FileDiff{
		"app.go": {Path: "app.go", StartLine: 50, HunkRanges: hr(diff.HunkRange{Start: 50, End: 55}), Added: []string{"l52", "l53"}},
	}
	comments := buildReviewComments(v, []string{"app.go"}, fileDiffs)

	require.Len(t, comments, 1)
	require.Equal(t, 51, comments[0].Line)
}

func TestBuildReviewComments_SkipsFileNotInFileDiffs(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "missing.go", LineStart: 10, Title: "File not in diff"},
			{File: "present.go", LineStart: 10, Title: "File in diff"},
		},
	}

	fileDiffs := map[string]*diff.FileDiff{
		"present.go": {Path: "present.go", StartLine: 1, HunkRanges: hr(diff.HunkRange{Start: 1, End: 20}), Added: []string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9", "l10"}},
	}
	comments := buildReviewComments(v, []string{"missing.go", "present.go"}, fileDiffs)

	require.Len(t, comments, 1)
	require.Equal(t, "present.go", comments[0].Path)
}

func TestBuildReviewComments_ClampsOutOfRangeLine(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 500, Title: "Way out of range"},
		},
	}

	fileDiffs := map[string]*diff.FileDiff{
		"a.txt": {Path: "a.txt", StartLine: 10, HunkRanges: hr(diff.HunkRange{Start: 10, End: 15}), Added: []string{"l10", "l11", "l12"}},
	}
	comments := buildReviewComments(v, []string{"a.txt"}, fileDiffs)

	require.Len(t, comments, 1)
	require.Equal(t, 10, comments[0].Line)
}

func TestBuildReviewComments_LineInSecondHunkRange(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "multi.go", LineStart: 55, Title: "Issue in second hunk"},
		},
	}

	fileDiffs := map[string]*diff.FileDiff{
		"multi.go": {Path: "multi.go", StartLine: 10, HunkRanges: hr(diff.HunkRange{Start: 10, End: 20}, diff.HunkRange{Start: 50, End: 60}), Added: []string{"l1", "l2"}},
	}
	comments := buildReviewComments(v, []string{"multi.go"}, fileDiffs)

	require.Len(t, comments, 1)
	require.Equal(t, "multi.go", comments[0].Path)
	require.Equal(t, 55, comments[0].Line)
}

func TestBuildReviewComments_ZeroLineStartClampsToHunkStart(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "app.ts", LineStart: 0, Title: "Issue with no line"},
			{File: "app.ts", LineStart: 0, Title: "Another issue with no line"},
		},
	}

	fileDiffs := map[string]*diff.FileDiff{
		"app.ts": {Path: "app.ts", StartLine: 5, HunkRanges: hr(diff.HunkRange{Start: 5, End: 15}), Added: []string{"l5", "l6"}},
	}
	comments := buildReviewComments(v, []string{"app.ts"}, fileDiffs)

	require.Len(t, comments, 1)
	require.Equal(t, "app.ts", comments[0].Path)
	require.Equal(t, 5, comments[0].Line)
	require.Contains(t, comments[0].Body, "2 issue(s)")
}