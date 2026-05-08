package reporters

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-github/v66/github"
	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/githubclient"
	"github.com/helloodokai/acig/internal/verdict"
	"github.com/stretchr/testify/require"
)

type mockGitHubClient struct {
	createReviewCalls      []createReviewCall
	createReviewFailAt     int
	postStickyCommentErr   error
	listReviewsErr         error
	deleteReviewCommentsErr error
	dismissReviewErr       error
	getPRFileDiffsErr      error
	postedComments         []string
}

type createReviewCall struct {
	comments []githubclient.ReviewComment
}

func (m *mockGitHubClient) ListReviews(ctx context.Context, owner, repo string, prNumber int) ([]*github.PullRequestReview, error) {
	if m.listReviewsErr != nil {
		return nil, m.listReviewsErr
	}
	return nil, nil
}

func (m *mockGitHubClient) DeleteReviewComments(ctx context.Context, owner, repo string, prNumber int, reviewID int64) error {
	return m.deleteReviewCommentsErr
}

func (m *mockGitHubClient) DismissReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64, message string) error {
	return m.dismissReviewErr
}

func (m *mockGitHubClient) ListPRFiles(ctx context.Context, owner, repo string, prNumber int) ([]string, error) {
	return []string{"a.txt"}, nil
}

func (m *mockGitHubClient) GetPRFileDiffs(ctx context.Context, owner, repo string, prNumber int) (map[string]*diff.FileDiff, error) {
	if m.getPRFileDiffsErr != nil {
		return nil, m.getPRFileDiffsErr
	}
	// Build DiffLines covering lines 1-10 so that LineStart:10 is valid.
	diffLines := make(map[int]bool, 10)
	for i := 1; i <= 10; i++ {
		diffLines[i] = true
	}
	return map[string]*diff.FileDiff{
		"a.txt": {
			Path:      "a.txt",
			Added:     []string{"line1", "line2", "line3", "line4", "line5", "line6", "line7", "line8", "line9", "line10"},
			DiffLines: diffLines,
		},
	}, nil
}

func (m *mockGitHubClient) CreateReview(ctx context.Context, owner, repo string, prNumber int, body string, comments []githubclient.ReviewComment, event string) error {
	m.createReviewCalls = append(m.createReviewCalls, createReviewCall{comments: comments})
	if m.createReviewFailAt > 0 && len(m.createReviewCalls) >= m.createReviewFailAt {
		return errors.New("position could not be resolved")
	}
	return nil
}

func (m *mockGitHubClient) PostStickyComment(ctx context.Context, owner, repo string, prNumber int, marker, body string) error {
	return m.postStickyCommentErr
}

func (m *mockGitHubClient) PostComment(ctx context.Context, owner, repo string, prNumber int, body string) error {
	m.postedComments = append(m.postedComments, body)
	return nil
}

func (m *mockGitHubClient) RemoveStaleAcigComments(ctx context.Context, owner, repo string, prNumber int, marker string) {}

func (m *mockGitHubClient) CreateCheckRun(ctx context.Context, owner, repo, name, conclusion, title, summary, headSHA string) error {
	return nil
}

func TestReport_RetryWithoutCommentsOnPositionError(t *testing.T) {
	mock := &mockGitHubClient{
		createReviewFailAt: 1,
	}

	reporter := &GitHubReporter{
		client: mock,
	}

	v := &verdict.Verdict{
		Decision: verdict.DecisionPass,
		Risk:     verdict.RiskLow,
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "Issue A"},
		},
	}

	err := reporter.Report(context.Background(), v, "owner", "repo", 1)
	require.NoError(t, err)

	require.Len(t, mock.createReviewCalls, 2)
	require.NotNil(t, mock.createReviewCalls[0].comments)
	require.Nil(t, mock.createReviewCalls[1].comments)
}

func TestReport_FallsBackToStickyCommentWhenBothReviewAttemptsFail(t *testing.T) {
	mock := &mockGitHubClient{
		createReviewFailAt: 1,
		postStickyCommentErr: errors.New("sticky comment failed"),
	}

	reporter := &GitHubReporter{
		client: mock,
	}

	v := &verdict.Verdict{
		Decision: verdict.DecisionPass,
		Risk:     verdict.RiskLow,
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "Issue A"},
		},
	}

	err := reporter.Report(context.Background(), v, "owner", "repo", 1)
	require.Error(t, err)
	require.Len(t, mock.createReviewCalls, 2)
	require.Contains(t, mock.postStickyCommentErr.Error(), "sticky comment failed")
}

func TestReport_SuccessfulReviewOnFirstTry(t *testing.T) {
	mock := &mockGitHubClient{
		createReviewFailAt: 0,
	}

	reporter := &GitHubReporter{
		client: mock,
	}

	v := &verdict.Verdict{
		Decision: verdict.DecisionPass,
		Risk:     verdict.RiskLow,
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "Issue A"},
		},
	}

	err := reporter.Report(context.Background(), v, "owner", "repo", 1)
	require.NoError(t, err)

	require.Len(t, mock.createReviewCalls, 1)
	require.NotNil(t, mock.createReviewCalls[0].comments)
}

func TestReport_UsesPRFilesFilter(t *testing.T) {
	mock := &mockGitHubClient{}

	reporter := &GitHubReporter{
		client: mock,
	}

	v := &verdict.Verdict{
		Decision: verdict.DecisionPass,
		Risk:     verdict.RiskLow,
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "Issue A"},
			{File: "deleted.txt", LineStart: 20, Title: "Should be filtered"},
		},
	}

	err := reporter.Report(context.Background(), v, "owner", "repo", 1)
	require.NoError(t, err)

	require.Len(t, mock.createReviewCalls, 1)
	require.Len(t, mock.createReviewCalls[0].comments, 1)
	require.Equal(t, "a.txt", mock.createReviewCalls[0].comments[0].Path)
}

// TestReport_GeneralFindingsPostedAsSeparateComments verifies that findings
// without a file are posted as individual PR comments (not buried in the
// review body) so they are visible and actionable.
func TestReport_GeneralFindingsPostedAsSeparateComments(t *testing.T) {
	mock := &mockGitHubClient{}
	reporter := &GitHubReporter{client: mock}

	v := &verdict.Verdict{
		Decision: verdict.DecisionPass,
		Risk:     verdict.RiskLow,
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "Inline Issue"},
			{File: "", LineStart: 0, Title: "General Issue", Detail: "Missing tests"},
		},
	}

	err := reporter.Report(context.Background(), v, "owner", "repo", 1)
	require.NoError(t, err)

	// Inline finding goes into the review.
	require.Len(t, mock.createReviewCalls[0].comments, 1)
	require.Equal(t, "a.txt", mock.createReviewCalls[0].comments[0].Path)

	// General finding is posted as a separate comment.
	require.Len(t, mock.postedComments, 1)
	require.Contains(t, mock.postedComments[0], "General Issue")
	require.Contains(t, mock.postedComments[0], "Missing tests")
}

// TestReport_MissingFileFindingsPostedAsSeparateComments verifies that
// findings referencing files not in the PR are posted as individual PR
// comments rather than silently dropped.
func TestReport_MissingFileFindingsPostedAsSeparateComments(t *testing.T) {
	mock := &mockGitHubClient{}
	reporter := &GitHubReporter{client: mock}

	v := &verdict.Verdict{
		Decision: verdict.DecisionPass,
		Risk:     verdict.RiskLow,
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "Inline Issue"},
			{File: "missing.go", LineStart: 5, Title: "Missing File Issue", Detail: "Tests absent"},
		},
	}

	err := reporter.Report(context.Background(), v, "owner", "repo", 1)
	require.NoError(t, err)

	// Only the in-PR file gets an inline comment.
	require.Len(t, mock.createReviewCalls[0].comments, 1)
	require.Equal(t, "a.txt", mock.createReviewCalls[0].comments[0].Path)

	// The missing-file finding is posted as a separate comment.
	require.Len(t, mock.postedComments, 1)
	require.Contains(t, mock.postedComments[0], "missing.go")
	require.Contains(t, mock.postedComments[0], "Missing File Issue")
}

// TestReport_OutOfDiffLineFindingsPostedAsSeparateComments verifies that
// findings whose line number is not visible in the diff are posted as
// separate PR comments rather than being silently dropped or mapped to the
// wrong diff line.
func TestReport_OutOfDiffLineFindingsPostedAsSeparateComments(t *testing.T) {
	mock := &mockGitHubClient{}
	reporter := &GitHubReporter{client: mock}

	v := &verdict.Verdict{
		Decision: verdict.DecisionPass,
		Risk:     verdict.RiskLow,
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "In-diff Issue"},
			// Line 99 is outside the mocked diff which only covers lines 1-10.
			{File: "a.txt", LineStart: 99, Title: "Out-of-diff Issue", Detail: "Not in hunk"},
		},
	}

	err := reporter.Report(context.Background(), v, "owner", "repo", 1)
	require.NoError(t, err)

	// Only the in-diff finding gets an inline comment.
	require.Len(t, mock.createReviewCalls[0].comments, 1)
	require.Equal(t, 10, mock.createReviewCalls[0].comments[0].Line)

	// The out-of-diff finding is posted as a separate comment.
	require.Len(t, mock.postedComments, 1)
	require.Contains(t, mock.postedComments[0], "Out-of-diff Issue")
	require.Contains(t, mock.postedComments[0], "outside diff")
}