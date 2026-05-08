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
	createReviewCalls        []createReviewCall
	createReviewFailAt       int
	postStickyCommentErr     error
	listReviewsErr           error
	deleteReviewCommentsErr  error
	dismissReviewErr         error
	deletePendingReviewErr    error
	getPRFileDiffsErr        error
	postedComments           []string
	deletedReviewIDs         []int64
	dismissedReviewIDs       []int64
	deletedPendingReviewIDs  []int64
	reviews                  []*github.PullRequestReview
}

type createReviewCall struct {
	comments []githubclient.ReviewComment
}

func (m *mockGitHubClient) ListReviews(ctx context.Context, owner, repo string, prNumber int) ([]*github.PullRequestReview, error) {
	if m.listReviewsErr != nil {
		return nil, m.listReviewsErr
	}
	return m.reviews, nil
}

func (m *mockGitHubClient) DeleteReviewComments(ctx context.Context, owner, repo string, prNumber int, reviewID int64) error {
	m.deletedReviewIDs = append(m.deletedReviewIDs, reviewID)
	return m.deleteReviewCommentsErr
}

func (m *mockGitHubClient) DismissReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64, message string) error {
	m.dismissedReviewIDs = append(m.dismissedReviewIDs, reviewID)
	return m.dismissReviewErr
}

func (m *mockGitHubClient) DeletePendingReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64) error {
	m.deletedPendingReviewIDs = append(m.deletedPendingReviewIDs, reviewID)
	return m.deletePendingReviewErr
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

// TestReport_TrulyGeneralFindingsPostedAsSeparateComments verifies that
// findings with NO file (File="") are posted as individual PR comments.
func TestReport_TrulyGeneralFindingsPostedAsSeparateComments(t *testing.T) {
	mock := &mockGitHubClient{}
	reporter := &GitHubReporter{client: mock}

	v := &verdict.Verdict{
		Decision: verdict.DecisionPass,
		Risk:     verdict.RiskLow,
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "Inline Issue"},
			{File: "", LineStart: 0, Title: "Truly General Issue", Detail: "No file at all"},
		},
	}

	err := reporter.Report(context.Background(), v, "owner", "repo", 1)
	require.NoError(t, err)

	// Inline finding goes into the review.
	require.Len(t, mock.createReviewCalls[0].comments, 1)
	require.Equal(t, "a.txt", mock.createReviewCalls[0].comments[0].Path)

	// Only File="" findings become separate comments.
	require.Len(t, mock.postedComments, 1)
	require.Contains(t, mock.postedComments[0], "Truly General Issue")
}

// TestReport_FileLevelFindingSnappedToFirstDiffLine verifies the core fix:
// a finding that references a file but has LineStart=0 (e.g. a missing-test
// finding from test_coverage_smell) is snapped to the first visible diff line
// and posted as an inline review comment, NOT as a conversation comment.
func TestReport_FileLevelFindingSnappedToFirstDiffLine(t *testing.T) {
	mock := &mockGitHubClient{}
	reporter := &GitHubReporter{client: mock}

	v := &verdict.Verdict{
		Decision: verdict.DecisionPass,
		Risk:     verdict.RiskLow,
		Findings: []verdict.Finding{
			// File-level finding: LineStart=0, file is in the PR diff.
			{File: "a.txt", LineStart: 0, Title: "Missing Test", Detail: "No unit test"},
		},
	}

	err := reporter.Report(context.Background(), v, "owner", "repo", 1)
	require.NoError(t, err)

	// Must appear as an inline review comment, NOT a conversation comment.
	require.Len(t, mock.createReviewCalls[0].comments, 1)
	c := mock.createReviewCalls[0].comments[0]
	require.Equal(t, "a.txt", c.Path)
	require.Equal(t, 1, c.Line) // snapped to first DiffLine (1)
	require.Contains(t, c.Body, "Missing Test")

	// No separate conversation comments.
	require.Empty(t, mock.postedComments)
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

// TestCleanupOldReviews_DeletesCommentsForAllStates verifies that cleanup
// deletes inline comments for COMMENT-state reviews (not just
// CHANGES_REQUESTED/APPROVED) and also dismisses/disposes reviews correctly.
func TestCleanupOldReviews_DeletesCommentsForAllStates(t *testing.T) {
	commentedReviewBody := "<!-- acig:review -->\n## acig: pass"
	approvedReviewBody := "<!-- acig:review -->\n## acig: block"
	pendingReviewBody := "<!-- acig:review -->\n## acig: pending"

	tests := []struct {
		name           string
		reviews        []*github.PullRequestReview
		wantDeleted    []int64
		wantDismissed  []int64
		wantPendingDel []int64
	}{
		{
			name: "COMMENT review has comments deleted but is not dismissed",
			reviews: []*github.PullRequestReview{
				{
					ID:    github.Int64(101),
					Body:  &commentedReviewBody,
					State: github.String("COMMENTED"),
				},
			},
			wantDeleted:   []int64{101},
			wantDismissed: nil,
		},
		{
			name: "APPROVED review has comments deleted and is dismissed",
			reviews: []*github.PullRequestReview{
				{
					ID:    github.Int64(202),
					Body:  &approvedReviewBody,
					State: github.String("APPROVED"),
				},
			},
			wantDeleted:   []int64{202},
			wantDismissed: []int64{202},
		},
		{
			name: "PENDING review is deleted entirely",
			reviews: []*github.PullRequestReview{
				{
					ID:    github.Int64(303),
					Body:  &pendingReviewBody,
					State: github.String("PENDING"),
				},
			},
			wantDeleted:    []int64{303},
			wantPendingDel: []int64{303},
		},
		{
			name: "non-acig reviews are left untouched",
			reviews: []*github.PullRequestReview{
				{
					ID:    github.Int64(999),
					Body:  github.String("some other review"),
					State: github.String("COMMENTED"),
				},
			},
			wantDeleted:   nil,
			wantDismissed: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockGitHubClient{reviews: tt.reviews}
			reporter := &GitHubReporter{client: mock}
			err := reporter.cleanupOldReviews(context.Background(), "owner", "repo", 1)
			require.NoError(t, err)
			require.Equal(t, tt.wantDeleted, mock.deletedReviewIDs)
			require.Equal(t, tt.wantDismissed, mock.dismissedReviewIDs)
			require.Equal(t, tt.wantPendingDel, mock.deletedPendingReviewIDs)
		})
	}
}