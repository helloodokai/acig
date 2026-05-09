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
	createReviewCalls       []createReviewCall
	createReviewFailAt      int
	postStickyCommentErr    error
	postStickyBodies        []string
	listReviewsErr          error
	deleteReviewCommentsErr error
	dismissReviewErr        error
	deletePendingReviewErr  error
	editReviewErr           error
	getPRFileDiffsErr       error
	prFiles                 []string
	fileDiffs               map[string]*diff.FileDiff
	postedComments          []string
	deletedReviewIDs        []int64
	dismissedReviewIDs      []int64
	deletedPendingReviewIDs []int64
	editedReviewIDs         []int64
	editedReviewBodies      []string
	reviews                 []*github.PullRequestReview
}

type createReviewCall struct {
	body     string
	comments []githubclient.ReviewComment
	event    string
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

func (m *mockGitHubClient) EditReview(ctx context.Context, owner, repo string, prNumber int, reviewID int64, body string) error {
	m.editedReviewIDs = append(m.editedReviewIDs, reviewID)
	m.editedReviewBodies = append(m.editedReviewBodies, body)
	return m.editReviewErr
}

func (m *mockGitHubClient) ListPRFiles(ctx context.Context, owner, repo string, prNumber int) ([]string, error) {
	if m.prFiles != nil {
		return m.prFiles, nil
	}
	return []string{"a.txt"}, nil
}

func (m *mockGitHubClient) GetPRFileDiffs(ctx context.Context, owner, repo string, prNumber int) (map[string]*diff.FileDiff, error) {
	if m.getPRFileDiffsErr != nil {
		return nil, m.getPRFileDiffsErr
	}
	if m.fileDiffs != nil {
		return m.fileDiffs, nil
	}
	// Default: cover lines 1-10 on a.txt.
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
	m.createReviewCalls = append(m.createReviewCalls, createReviewCall{body: body, comments: comments, event: event})
	if m.createReviewFailAt > 0 && len(m.createReviewCalls) >= m.createReviewFailAt {
		return errors.New("position could not be resolved")
	}
	return nil
}

func (m *mockGitHubClient) PostStickyComment(ctx context.Context, owner, repo string, prNumber int, marker, body string) error {
	m.postStickyBodies = append(m.postStickyBodies, body)
	return m.postStickyCommentErr
}

func (m *mockGitHubClient) PostComment(ctx context.Context, owner, repo string, prNumber int, body string) error {
	m.postedComments = append(m.postedComments, body)
	return nil
}

func (m *mockGitHubClient) RemoveStaleAcigComments(ctx context.Context, owner, repo string, prNumber int, marker string) {
}

func (m *mockGitHubClient) CreateCheckRun(ctx context.Context, owner, repo, name, conclusion, title, summary, headSHA string) error {
	return nil
}

func TestReport_RetryWithoutCommentsOnPositionError(t *testing.T) {
	mock := &mockGitHubClient{createReviewFailAt: 1}
	reporter := &GitHubReporter{client: mock}

	v := &verdict.Verdict{
		Decision: verdict.DecisionPass,
		Risk:     verdict.RiskLow,
		Findings: []verdict.Finding{{File: "a.txt", LineStart: 10, Title: "Issue A"}},
	}

	err := reporter.Report(context.Background(), v, "owner", "repo", 1)
	require.NoError(t, err)
	require.Len(t, mock.createReviewCalls, 2)
	require.NotNil(t, mock.createReviewCalls[0].comments)
	require.Nil(t, mock.createReviewCalls[1].comments)
}

func TestReport_StickyCommentAlwaysPosted(t *testing.T) {
	mock := &mockGitHubClient{}
	reporter := &GitHubReporter{client: mock}

	v := &verdict.Verdict{
		Decision: verdict.DecisionPass,
		Risk:     verdict.RiskLow,
		Findings: []verdict.Finding{{File: "a.txt", LineStart: 10, Title: "Issue A"}},
	}

	err := reporter.Report(context.Background(), v, "owner", "repo", 1)
	require.NoError(t, err)

	require.Len(t, mock.postStickyBodies, 1)
	require.Contains(t, mock.postStickyBodies[0], "Issue A")
	require.Contains(t, mock.postStickyBodies[0], acigMarker)
	// Per-finding "general" comments are no longer posted.
	require.Empty(t, mock.postedComments)
}

func TestReport_DroppedFindingsAppearInSticky(t *testing.T) {
	mock := &mockGitHubClient{}
	reporter := &GitHubReporter{client: mock}

	v := &verdict.Verdict{
		Decision: verdict.DecisionWarn,
		Risk:     verdict.RiskMedium,
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 10, Title: "Inline OK"},
			{File: "", LineStart: 0, Title: "No file finding"},
			{File: "missing.go", LineStart: 5, Title: "Outside PR"},
			{File: "a.txt", LineStart: 99, Title: "Outside diff"},
		},
	}

	err := reporter.Report(context.Background(), v, "owner", "repo", 1)
	require.NoError(t, err)

	// Only the inline-anchored finding becomes a review comment.
	require.Len(t, mock.createReviewCalls[0].comments, 1)
	require.Equal(t, 10, mock.createReviewCalls[0].comments[0].Line)

	// All findings appear in the sticky body (table); the dropped ones also
	// appear under the "Findings outside the diff" details section.
	body := mock.postStickyBodies[0]
	require.Contains(t, body, "Inline OK")
	require.Contains(t, body, "No file finding")
	require.Contains(t, body, "Outside PR")
	require.Contains(t, body, "Outside diff")
	require.Contains(t, body, "Findings outside the diff")
}

func TestReport_FileLevelFindingSnappedToFirstDiffLine(t *testing.T) {
	mock := &mockGitHubClient{}
	reporter := &GitHubReporter{client: mock}

	v := &verdict.Verdict{
		Decision: verdict.DecisionPass,
		Risk:     verdict.RiskLow,
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 0, Title: "Missing Test", Detail: "No unit test"},
		},
	}

	err := reporter.Report(context.Background(), v, "owner", "repo", 1)
	require.NoError(t, err)

	require.Len(t, mock.createReviewCalls[0].comments, 1)
	c := mock.createReviewCalls[0].comments[0]
	require.Equal(t, "a.txt", c.Path)
	require.Equal(t, 1, c.Line)
	require.Equal(t, "RIGHT", c.Side)
}

func TestReport_DeletedFileFindingGoesToLeftSide(t *testing.T) {
	mock := &mockGitHubClient{
		prFiles: []string{"deleted.md"},
		fileDiffs: map[string]*diff.FileDiff{
			"deleted.md": {
				Path:     "deleted.md",
				IsDelete: true,
				OrigLines: map[int]bool{
					1: true, 2: true, 3: true, 4: true, 5: true,
				},
			},
		},
	}
	reporter := &GitHubReporter{client: mock}

	v := &verdict.Verdict{
		Decision: verdict.DecisionPass,
		Risk:     verdict.RiskLow,
		Findings: []verdict.Finding{
			{File: "deleted.md", LineStart: 3, Title: "Comment on deleted code"},
		},
	}

	err := reporter.Report(context.Background(), v, "owner", "repo", 1)
	require.NoError(t, err)

	require.Len(t, mock.createReviewCalls[0].comments, 1)
	c := mock.createReviewCalls[0].comments[0]
	require.Equal(t, "deleted.md", c.Path)
	require.Equal(t, "LEFT", c.Side)
	require.Equal(t, 3, c.Line)
}

func TestReport_NearLineSnapping(t *testing.T) {
	// Diff covers lines 10-15; finding on line 12 should map exactly,
	// finding on line 17 should snap to 15 (within ±3).
	dl := map[int]bool{10: true, 11: true, 12: true, 13: true, 14: true, 15: true}
	mock := &mockGitHubClient{
		prFiles: []string{"a.txt"},
		fileDiffs: map[string]*diff.FileDiff{
			"a.txt": {Path: "a.txt", DiffLines: dl},
		},
	}
	reporter := &GitHubReporter{client: mock}

	v := &verdict.Verdict{
		Decision: verdict.DecisionPass,
		Risk:     verdict.RiskLow,
		Findings: []verdict.Finding{
			{File: "a.txt", LineStart: 17, Title: "Slightly off"},
		},
	}

	err := reporter.Report(context.Background(), v, "owner", "repo", 1)
	require.NoError(t, err)

	require.Len(t, mock.createReviewCalls[0].comments, 1)
	c := mock.createReviewCalls[0].comments[0]
	require.Equal(t, 15, c.Line, "should snap to nearest diff line within ±3")
}

func TestCleanupOldReviews_HandlesAllStates(t *testing.T) {
	commentedReviewBody := "<!-- acig:review -->\n## acig: pass"
	approvedReviewBody := "<!-- acig:review -->\n## acig: block"
	pendingReviewBody := "<!-- acig:review -->\n## acig: pending"

	tests := []struct {
		name           string
		reviews        []*github.PullRequestReview
		wantDeleted    []int64
		wantDismissed  []int64
		wantPendingDel []int64
		wantEdited     []int64
	}{
		{
			name: "COMMENTED review has comments deleted and body replaced",
			reviews: []*github.PullRequestReview{
				{ID: github.Int64(101), Body: &commentedReviewBody, State: github.String("COMMENTED")},
			},
			wantDeleted: []int64{101},
			wantEdited:  []int64{101},
		},
		{
			name: "APPROVED review is dismissed",
			reviews: []*github.PullRequestReview{
				{ID: github.Int64(202), Body: &approvedReviewBody, State: github.String("APPROVED")},
			},
			wantDeleted:   []int64{202},
			wantDismissed: []int64{202},
		},
		{
			name: "PENDING review is deleted",
			reviews: []*github.PullRequestReview{
				{ID: github.Int64(303), Body: &pendingReviewBody, State: github.String("PENDING")},
			},
			wantDeleted:    []int64{303},
			wantPendingDel: []int64{303},
		},
		{
			name: "non-acig review is left alone",
			reviews: []*github.PullRequestReview{
				{ID: github.Int64(999), Body: github.String("some other review"), State: github.String("COMMENTED")},
			},
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
			require.Equal(t, tt.wantEdited, mock.editedReviewIDs)
			if len(tt.wantEdited) > 0 {
				require.Contains(t, mock.editedReviewBodies[0], "Superseded")
			}
		})
	}
}
