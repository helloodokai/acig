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
		{File: "b.txt", LineStart: 10, Title: "Valid line"},
	}

	groups := groupFindings(findings)
	require.Len(t, groups, 1)
	require.Equal(t, "b.txt", groups[0][0].File)
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
		"a.txt": {Path: "a.txt", StartLine: 5, Added: []string{"l5", "l6", "l7", "l8", "l9", "l10"}},
		"b.txt": {Path: "b.txt", StartLine: 25, Added: []string{"l25", "l26", "l27", "l28", "l29", "l30"}},
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
		"a.txt": {Path: "a.txt", StartLine: 1, Added: []string{"l1", "l2", "l3", "l4", "l5"}},
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
		"a.txt": {Path: "a.txt", StartLine: 1, Added: make([]string, 30)},
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
		"a.txt": {Path: "a.txt", StartLine: 1, Added: make([]string, 20)},
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
		"a.txt": {Path: "a.txt", StartLine: 1, Added: make([]string, 50)},
		"b.txt": {Path: "b.txt", StartLine: 1, Added: make([]string, 30)},
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
			name:          "within range",
			fd:            &diff.FileDiff{StartLine: 50, Added: []string{"l1", "l2", "l3"}},
			requestedLine: 51,
			expected:      51,
		},
		{
			name:          "at start",
			fd:            &diff.FileDiff{StartLine: 50, Added: []string{"l1", "l2", "l3"}},
			requestedLine: 50,
			expected:      50,
		},
		{
			name:          "at end",
			fd:            &diff.FileDiff{StartLine: 50, Added: []string{"l1", "l2", "l3"}},
			requestedLine: 52,
			expected:      52,
		},
		{
			name:          "beyond last line clamps",
			fd:            &diff.FileDiff{StartLine: 50, Added: []string{"l1", "l2", "l3"}},
			requestedLine: 100,
			expected:      52,
		},
		{
			name:          "before start line clamps to start",
			fd:            &diff.FileDiff{StartLine: 50, Added: []string{"l1", "l2", "l3"}},
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
			fd:            &diff.FileDiff{StartLine: 1, IsDelete: true, Added: []string{"l1"}},
			requestedLine: 1,
			expected:      0,
		},
		{
			name:          "zero start line returns 0",
			fd:            &diff.FileDiff{StartLine: 0, Added: []string{"l1"}},
			requestedLine: 1,
			expected:      0,
		},
		{
			name:          "empty added lines returns 0",
			fd:            &diff.FileDiff{StartLine: 1, Added: []string{}},
			requestedLine: 1,
			expected:      0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := validateLine(tt.fd, tt.requestedLine)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildReviewComments_LineRemappingWithStartLine(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "app.go", LineStart: 52, LineEnd: 52, Title: "Issue at line 52"},
		},
	}

	fileDiffs := map[string]*diff.FileDiff{
		"app.go": {Path: "app.go", StartLine: 50, Added: []string{"l50", "l51", "l52", "l53", "l54"}},
	}
	comments := buildReviewComments(v, []string{"app.go"}, fileDiffs)

	require.Len(t, comments, 1)
	require.Equal(t, "app.go", comments[0].Path)
	require.Equal(t, 52, comments[0].Line)
}

func TestBuildReviewComments_SkipsFileNotInFileDiffs(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "missing.go", LineStart: 10, Title: "File not in diff"},
			{File: "present.go", LineStart: 10, Title: "File in diff"},
		},
	}

	fileDiffs := map[string]*diff.FileDiff{
		"present.go": {Path: "present.go", StartLine: 1, Added: []string{"l1", "l2", "l3", "l4", "l5", "l6", "l7", "l8", "l9", "l10"}},
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
		"a.txt": {Path: "a.txt", StartLine: 10, Added: []string{"l10", "l11", "l12"}},
	}
	comments := buildReviewComments(v, []string{"a.txt"}, fileDiffs)

	require.Len(t, comments, 1)
	require.Equal(t, 12, comments[0].Line)
}