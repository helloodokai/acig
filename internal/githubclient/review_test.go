package githubclient

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReviewCommentStruct(t *testing.T) {
	rc := ReviewComment{
		Path:      "test.txt",
		Line:      10,
		StartLine: 5,
		Body:      "test body",
	}
	require.Equal(t, "test.txt", rc.Path)
	require.Equal(t, 10, rc.Line)
	require.Equal(t, 5, rc.StartLine)
	require.Equal(t, "test body", rc.Body)
}

func TestReviewCommentStruct_NoStartLine(t *testing.T) {
	rc := ReviewComment{
		Path: "test.txt",
		Line: 10,
		Body: "test body",
	}
	require.Equal(t, 0, rc.StartLine)
}

func TestReviewCommentStruct_MultiLine(t *testing.T) {
	rc := ReviewComment{
		Path:      "test.txt",
		Line:      20,
		StartLine: 10,
		Body:     "multi-line comment",
	}
	require.True(t, rc.StartLine > 0)
	require.Greater(t, rc.Line, rc.StartLine)
}