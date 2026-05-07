package reporters

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-github/v66/github"
	"github.com/helloodokai/acig/internal/githubclient"
	"github.com/helloodokai/acig/internal/verdict"
	"github.com/stretchr/testify/require"
)

type mockGitHubClient struct {
	createReviewCalls   []createReviewCall
	createReviewFailAt   int
	postStickyCommentErr  error
	listReviewsErr       error
	deleteReviewCommentsErr error
	dismissReviewErr     error
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