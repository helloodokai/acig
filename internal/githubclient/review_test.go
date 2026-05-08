package githubclient

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/google/go-github/v66/github"
	"github.com/helloodokai/acig/internal/diff"
	"github.com/stretchr/testify/require"
)

func newTestClient(server *httptest.Server) *Client {
	httpClient := server.Client()
	ghClient := github.NewClient(httpClient).WithAuthToken("fake-token")
	ghClient.BaseURL, _ = url.Parse(server.URL + "/")
	return &Client{client: ghClient}
}

func TestGetPRFileDiffs(t *testing.T) {
	patchAuthLogin := `diff --git a/auth/login.go b/auth/login.go
index 1234567..abcdef0 100644
--- a/auth/login.go
+++ b/auth/login.go
@@ -10,6 +10,8 @@ func Login(username string) error {
 	if err != nil {
-		return err
-	}
+	if err != nil {
+		log.Printf("login failed: %v", err)
+		return fmt.Errorf("login: %w", err)
+	}
`

	patchMultiHunk := `diff --git a/bar.go b/bar.go
index 1111111..2222222 100644
--- a/bar.go
+++ b/bar.go
@@ -10,3 +10,5 @@ func first() {
 	context1
+	added1
 	context2
@@ -50,3 +50,4 @@ func second() {
 	context3
+	added2
 	context4
}
`

	patchDeletedFile := `diff --git a/old_file.go b/old_file.go
deleted file mode 100644
index abc1234..0000000
--- a/old_file.go
+++ /dev/null
@@ -1,5 +0,0 @@
-package old
-
-func removed() {
-}
`

	type ghFile struct {
		Filename  string `json:"filename"`
		Patch     string `json:"patch,omitempty"`
		Status    string `json:"status,omitempty"`
		Additions int    `json:"additions,omitempty"`
		Deletions int    `json:"deletions,omitempty"`
		Changes   int    `json:"changes,omitempty"`
	}

	tests := []struct {
		name        string
		files       []ghFile
		expectLen   int
		expectPaths []string
		expectHunks map[string][]diff.HunkRange
		expectNew   map[string]bool
		expectStart map[string]int
	}{
		{
			name: "single file with patch",
			files: []ghFile{
				{
					Filename:  "auth/login.go",
					Patch:     patchAuthLogin,
					Status:    "modified",
					Additions: 4,
					Deletions: 2,
					Changes:   6,
				},
			},
			expectLen:   1,
			expectPaths: []string{"auth/login.go"},
			expectHunks: map[string][]diff.HunkRange{
				"auth/login.go": {{Start: 10, End: 17}},
			},
			expectNew: map[string]bool{
				"auth/login.go": false,
			},
			expectStart: map[string]int{
				"auth/login.go": 10,
			},
		},
		{
			name: "multi-hunk file",
			files: []ghFile{
				{
					Filename:  "bar.go",
					Patch:     patchMultiHunk,
					Status:    "modified",
					Additions: 2,
					Deletions: 0,
					Changes:   2,
				},
			},
			expectLen:   1,
			expectPaths: []string{"bar.go"},
			expectHunks: map[string][]diff.HunkRange{
				"bar.go": {{Start: 10, End: 14}, {Start: 50, End: 53}},
			},
			expectStart: map[string]int{
				"bar.go": 10,
			},
		},
		{
			name: "filters out deleted files",
			files: []ghFile{
				{
					Filename:  "auth/login.go",
					Patch:     patchAuthLogin,
					Status:    "modified",
					Additions: 4,
					Deletions: 2,
					Changes:   6,
				},
				{
					Filename:  "old_file.go",
					Patch:     patchDeletedFile,
					Status:    "removed",
					Additions: 0,
					Deletions: 5,
					Changes:   5,
				},
			},
			expectLen:   1,
			expectPaths: []string{"auth/login.go"},
			expectHunks: map[string][]diff.HunkRange{
				"auth/login.go": {{Start: 10, End: 17}},
			},
		},
		{
			name: "filters out files without patch",
			files: []ghFile{
				{
					Filename: "binary_file.png",
					Status:   "modified",
				},
				{
					Filename:  "auth/login.go",
					Patch:     patchAuthLogin,
					Status:    "modified",
					Additions: 4,
					Deletions: 2,
					Changes:   6,
				},
			},
			expectLen:   1,
			expectPaths: []string{"auth/login.go"},
		},
		{
			name: "filters out file with empty patch",
			files: []ghFile{
				{
					Filename: "empty_patch.go",
					Patch:    "",
					Status:   "modified",
				},
				{
					Filename:  "auth/login.go",
					Patch:     patchAuthLogin,
					Status:    "modified",
					Additions: 4,
					Deletions: 2,
					Changes:   6,
				},
			},
			expectLen:   1,
			expectPaths: []string{"auth/login.go"},
		},
		{
			name:      "empty response",
			files:     []ghFile{},
			expectLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/repos/owner/repo/pulls/42/files" {
					body, _ := json.Marshal(tt.files)
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write(body)
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()

			client := newTestClient(server)

			result, err := client.GetPRFileDiffs(context.Background(), "owner", "repo", 42)
			require.NoError(t, err)
			require.Len(t, result, tt.expectLen)

			for _, path := range tt.expectPaths {
				fd, ok := result[path]
				require.True(t, ok, "expected file %q in result", path)
				if hunks, expected := tt.expectHunks[path]; expected {
					require.Equal(t, hunks, fd.HunkRanges, "hunk ranges for %q", path)
				}
				if isNew, expected := tt.expectNew[path]; expected {
					require.Equal(t, isNew, fd.IsNew, "IsNew for %q", path)
				}
				if startLine, expected := tt.expectStart[path]; expected {
					require.Equal(t, startLine, fd.StartLine, "StartLine for %q", path)
				}
			}
		})
	}
}

func TestGetPRFileDiffs_PathNormalization(t *testing.T) {
	patchNewFile := `diff --git a/main.go b/main.go
new file mode 100644
index 0000000..abc1234
--- /dev/null
+++ b/main.go
@@ -0,0 +1,5 @@
+package main
+
+func main() {
+	println("hello")
+}
`

	files := []struct {
		Filename  string `json:"filename"`
		Patch     string `json:"patch"`
		Status    string `json:"status"`
		Additions int    `json:"additions"`
		Deletions int    `json:"deletions"`
		Changes   int    `json:"changes"`
	}{
		{
			Filename:  "main.go",
			Patch:     patchNewFile,
			Status:    "added",
			Additions: 5,
			Changes:   5,
		},
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/repos/owner/repo/pulls/1/files" {
			body, _ := json.Marshal(files)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := newTestClient(server)

	result, err := client.GetPRFileDiffs(context.Background(), "owner", "repo", 1)
	require.NoError(t, err)
	require.Len(t, result, 1)

	fd, ok := result["main.go"]
	require.True(t, ok, "expected 'main.go' in result, got keys: %v", mapKeys(result))
	require.True(t, fd.IsNew, "expected IsNew=true for new file")
	require.Equal(t, 1, fd.StartLine)
	require.Equal(t, []diff.HunkRange{{Start: 1, End: 5}}, fd.HunkRanges)
}

func TestGetPRFileDiffs_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"message":"Internal Server Error"}`))
	}))
	defer server.Close()

	client := newTestClient(server)

	_, err := client.GetPRFileDiffs(context.Background(), "owner", "repo", 1)
	require.Error(t, err)
}

func TestCreateReview(t *testing.T) {
	type createReviewRequest struct {
		Body     string `json:"body"`
		Event    string `json:"event"`
		Comments []struct {
			Path      string `json:"path"`
			Line      int    `json:"line"`
			StartLine int    `json:"start_line,omitempty"`
			Side      string `json:"side"`
			StartSide string `json:"start_side,omitempty"`
			Body      string `json:"body"`
		} `json:"comments"`
	}

	tests := []struct {
		name     string
		comments []ReviewComment
		event    string
		body     string
		want     createReviewRequest
	}{
		{
			name:  "single-line comment",
			event: "COMMENT",
			body:  "LGTM",
			comments: []ReviewComment{
				{Path: "main.go", Line: 10, StartLine: 0, Body: "nit: fix this"},
			},
			want: createReviewRequest{
				Body:  "LGTM",
				Event: "COMMENT",
				Comments: []struct {
					Path      string `json:"path"`
					Line      int    `json:"line"`
					StartLine int    `json:"start_line,omitempty"`
					Side      string `json:"side"`
					StartSide string `json:"start_side,omitempty"`
					Body      string `json:"body"`
				}{
					{Path: "main.go", Line: 10, Side: "RIGHT", Body: "nit: fix this"},
				},
			},
		},
		{
			name:  "multi-line comment",
			event: "REQUEST_CHANGES",
			body:  "Please fix",
			comments: []ReviewComment{
				{Path: "auth/login.go", Line: 25, StartLine: 20, Body: "refactor this block"},
			},
			want: createReviewRequest{
				Body:  "Please fix",
				Event: "REQUEST_CHANGES",
				Comments: []struct {
					Path      string `json:"path"`
					Line      int    `json:"line"`
					StartLine int    `json:"start_line,omitempty"`
					Side      string `json:"side"`
					StartSide string `json:"start_side,omitempty"`
					Body      string `json:"body"`
				}{
					{Path: "auth/login.go", Line: 25, StartLine: 20, Side: "RIGHT", StartSide: "RIGHT", Body: "refactor this block"},
				},
			},
		},
		{
			name:  "skips comments with line <= 0",
			event: "COMMENT",
			body:  "review",
			comments: []ReviewComment{
				{Path: "main.go", Line: 0, Body: "skip me"},
				{Path: "other.go", Line: 5, Body: "keep me"},
			},
			want: createReviewRequest{
				Body:  "review",
				Event: "COMMENT",
				Comments: []struct {
					Path      string `json:"path"`
					Line      int    `json:"line"`
					StartLine int    `json:"start_line,omitempty"`
					Side      string `json:"side"`
					StartSide string `json:"start_side,omitempty"`
					Body      string `json:"body"`
				}{
					{Path: "other.go", Line: 5, Side: "RIGHT", Body: "keep me"},
				},
			},
		},
		{
			name:     "no comments",
			event:    "APPROVE",
			body:     "Looks good",
			comments: nil,
			want: createReviewRequest{
				Body:     "Looks good",
				Event:    "APPROVE",
				Comments: nil,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var received createReviewRequest

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/pulls/42/reviews" {
					body, _ := io.ReadAll(r.Body)
					defer r.Body.Close()
					_ = json.Unmarshal(body, &received)

					resp := `{"id":1,"user":{"login":"test"},"body":"","state":"APPROVED"}`
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(resp))
					return
				}
				http.NotFound(w, r)
			}))
			defer server.Close()

			client := newTestClient(server)

			err := client.CreateReview(context.Background(), "owner", "repo", 42, tt.body, tt.comments, tt.event)
			require.NoError(t, err)

			require.Equal(t, tt.want.Body, received.Body)
			require.Equal(t, tt.want.Event, received.Event)
			require.Len(t, received.Comments, len(tt.want.Comments))

			for i, want := range tt.want.Comments {
				got := received.Comments[i]
				require.Equal(t, want.Path, got.Path, "comment %d: path", i)
				require.Equal(t, want.Line, got.Line, "comment %d: line", i)
				require.Equal(t, want.Side, got.Side, "comment %d: side", i)
				require.Equal(t, want.Body, got.Body, "comment %d: body", i)
				if want.StartLine > 0 {
					require.Equal(t, want.StartLine, got.StartLine, "comment %d: start_line", i)
					require.Equal(t, want.StartSide, got.StartSide, "comment %d: start_side", i)
				}
			}
		})
	}
}

func mapKeys(m map[string]*diff.FileDiff) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}