package reporters

import (
	"testing"

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
	// LineStart=0 findings with a file are now included in groupFindings so
	// that buildReviewComments can snap them to the first diff line and post
	// them as inline review comments instead of general conversation comments.
	findings := []verdict.Finding{
		{File: "a.txt", LineStart: 0, Title: "Zero line"},
		{File: "b.txt", LineStart: 10, Title: "Valid line"},
	}

	groups := groupFindings(findings)
	require.Len(t, groups, 2)
	require.Equal(t, "a.txt", groups[0][0].File)
	require.Equal(t, "b.txt", groups[1][0].File)
}

func TestGroupFindings_EmptyFileExcluded(t *testing.T) {
	// Findings with no file at all are excluded — they go to general comments.
	findings := []verdict.Finding{
		{File: "", LineStart: 0, Title: "Truly general"},
		{File: "a.txt", LineStart: 0, Title: "File-level"},
	}

	groups := groupFindings(findings)
	require.Len(t, groups, 1)
	require.Equal(t, "a.txt", groups[0][0].File)
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
	comments := buildReviewComments(v, prFiles, nil)

	require.Len(t, comments, 2)
	require.Equal(t, "a.txt", comments[0].Path)
	require.Equal(t, "b.txt", comments[1].Path)
}

func TestBuildReviewComments_AllFilesIfPRFilesNil(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "Issue A"},
			{File: "deleted.txt", LineStart: 20, Title: "Issue in deleted file"},
		},
	}

	comments := buildReviewComments(v, nil, nil)

	require.Len(t, comments, 2)
}

func TestBuildReviewComments_EmptyPRFiles(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "Issue A"},
		},
	}

	comments := buildReviewComments(v, []string{}, nil)

	require.Len(t, comments, 1)
}

func TestBuildReviewComments_MultiLineComment(t *testing.T) {
	v := &verdict.Verdict{
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, LineEnd: 20, Title: "Multi-line issue"},
		},
	}

	comments := buildReviewComments(v, []string{"a.txt"}, nil)

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

	comments := buildReviewComments(v, []string{"a.txt"}, nil)

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

	comments := buildReviewComments(v, []string{"a.txt", "b.txt"}, nil)

	require.Len(t, comments, 3)
	require.Contains(t, comments[0].Body, "Issue 1")
	require.Contains(t, comments[0].Body, "Issue 2")
}