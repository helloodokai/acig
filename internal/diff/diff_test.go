package diff

import (
	"testing"

	diffParse "github.com/sourcegraph/go-diff/diff"
	"github.com/stretchr/testify/require"
)

const samplePatch = `diff --git a/main.go b/main.go
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
diff --git a/auth/login.go b/auth/login.go
index 1234567..abcdef0 100644
--- a/auth/login.go
+++ b/auth/login.go
@@ -10,6 +10,8 @@ func Login(username string) error {
-	if err != nil {
-		return err
-	}
+	if err != nil {
+		log.Printf("login failed: %v", err)
+		return fmt.Errorf("login: %w", err)
+	}
`

func TestParse(t *testing.T) {
	d, err := Parse(samplePatch)
	require.NoError(t, err)
	require.Len(t, d.Files, 2)
	require.Equal(t, "main.go", d.Files[0].Path)
	require.True(t, d.Files[0].IsNew)
	require.Equal(t, "auth/login.go", d.Files[1].Path)
	require.False(t, d.Files[1].IsNew)
	require.Equal(t, 2, d.Stats.FilesChanged)
}

// TestParse_DiffLinesTracked verifies that Parse populates DiffLines with the
// real new-file line numbers so validateLine can do exact matching.
func TestParse_DiffLinesTracked(t *testing.T) {
	d, err := Parse(samplePatch)
	require.NoError(t, err)
	require.Len(t, d.Files, 2)

	// main.go: new file, hunk @@ -0,0 +1,5 @@ → new-file lines 1-5 (all added)
	mainGo := d.Files[0]
	require.Equal(t, "main.go", mainGo.Path)
	for _, line := range []int{1, 2, 3, 4, 5} {
		require.True(t, mainGo.DiffLines[line], "expected line %d in DiffLines for main.go", line)
	}
	require.False(t, mainGo.DiffLines[6], "line 6 should not be in DiffLines for main.go")

	// auth/login.go: hunk @@ -10,6 +10,8 @@
	// 3 removed lines do not appear in new file; 4 added lines and context lines
	// span new-file lines 10-17.
	loginGo := d.Files[1]
	require.Equal(t, "auth/login.go", loginGo.Path)
	// The first context line in the hunk is new-file line 10.
	require.True(t, loginGo.DiffLines[10], "expected context line 10 in DiffLines for auth/login.go")
	// Added lines follow at 11, 12, 13.
	require.True(t, loginGo.DiffLines[11], "expected added line 11 in DiffLines for auth/login.go")
	// OrigLines should also be populated with the removed-side numbers.
	require.NotEmpty(t, loginGo.OrigLines, "expected OrigLines to be tracked")
}

func TestParseHunkBody_HeaderlessPatch(t *testing.T) {
	// This is the form GitHub's PR ListFiles API returns in `Patch`: hunk
	// bodies with `@@` headers but no `--- a/x +++ b/x` file headers.
	patch := "@@ -10,3 +10,4 @@\n context\n+added\n unchanged\n-removed\n"
	fd := ParseHunkBody("src/a.go", patch, false, false)
	require.NotNil(t, fd)
	require.Equal(t, "src/a.go", fd.Path)
	require.True(t, fd.DiffLines[10], "context line 10")
	require.True(t, fd.DiffLines[11], "added line 11")
	require.True(t, fd.DiffLines[12], "context line 12")
	require.True(t, fd.OrigLines[10], "context orig 10")
	require.True(t, fd.OrigLines[12], "removed orig 12")
}

func TestParseHunkBody_DeletedFileHasOrigLines(t *testing.T) {
	patch := "@@ -1,3 +0,0 @@\n-line one\n-line two\n-line three\n"
	fd := ParseHunkBody("README.md", patch, false, true)
	require.True(t, fd.IsDelete)
	require.True(t, fd.OrigLines[1])
	require.True(t, fd.OrigLines[2])
	require.True(t, fd.OrigLines[3])
}

func TestFileKind(t *testing.T) {
	cases := map[string]Kind{
		"src/foo.go":         KindCode,
		"foo_test.go":        KindTest,
		"src/__tests__/x.ts": KindTest,
		"x.test.ts":          KindTest,
		"README.md":          KindDocs,
		"docs/intro.md":      KindDocs,
		"go.sum":             KindGenerated,
		"package-lock.json":  KindGenerated,
		"node_modules/x.js":  KindGenerated,
		"config.toml":        KindConfig,
		"Dockerfile":         KindConfig,
		"some.unknown":       KindOther,
	}
	for path, want := range cases {
		require.Equal(t, want, FileKind(path), "FileKind(%q)", path)
	}
}

func TestHunkRanges(t *testing.T) {
	fd := &FileDiff{
		DiffLines: map[int]bool{10: true, 11: true, 12: true, 20: true, 21: true},
	}
	require.Equal(t, "[10-12, 20-21]", fd.HunkRanges())

	fd2 := &FileDiff{DiffLines: map[int]bool{}}
	require.Equal(t, "[]", fd2.HunkRanges())

	fd3 := &FileDiff{DiffLines: map[int]bool{42: true}}
	require.Equal(t, "[42]", fd3.HunkRanges())
}

func TestParseEmpty(t *testing.T) {
	d, err := Parse("")
	require.NoError(t, err)
	require.Len(t, d.Files, 0)
}

func TestNewPath(t *testing.T) {
	tests := []struct {
		name     string
		orig     string
		newN     string
		expected string
	}{
		{"new file", "/dev/null", "b/main.go", "main.go"},
		{"modified", "a/foo.go", "b/foo.go", "foo.go"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := &diffParse.FileDiff{OrigName: tt.orig, NewName: tt.newN}
			result := newPath(f)
			require.Equal(t, tt.expected, result)
		})
	}
}
