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
	require.Equal(t, 1, d.Files[0].StartLine)
	require.Equal(t, []HunkRange{{Start: 1, End: 5}}, d.Files[0].HunkRanges)
	require.Equal(t, "auth/login.go", d.Files[1].Path)
	require.False(t, d.Files[1].IsNew)
	require.Equal(t, 10, d.Files[1].StartLine)
	require.Equal(t, []HunkRange{{Start: 10, End: 17}}, d.Files[1].HunkRanges)
	require.Equal(t, 2, d.Stats.FilesChanged)
}

func TestParseEmpty(t *testing.T) {
	d, err := Parse("")
	require.NoError(t, err)
	require.Len(t, d.Files, 0)
}

func TestParseStartLine(t *testing.T) {
	patch := `diff --git a/foo.go b/foo.go
index abc1234..def5678 100644
--- a/foo.go
+++ b/foo.go
@@ -50,3 +50,5 @@ func existing() {
+	added line 1
+	added line 2
 }
`
	d, err := Parse(patch)
	require.NoError(t, err)
	require.Len(t, d.Files, 1)
	require.Equal(t, "foo.go", d.Files[0].Path)
	require.Equal(t, 50, d.Files[0].StartLine)
	require.Equal(t, []HunkRange{{Start: 50, End: 54}}, d.Files[0].HunkRanges)
	require.Equal(t, []string{"\tadded line 1", "\tadded line 2"}, d.Files[0].Added)
}

func TestParseMultiHunkRanges(t *testing.T) {
	patch := `diff --git a/bar.go b/bar.go
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
	d, err := Parse(patch)
	require.NoError(t, err)
	require.Len(t, d.Files, 1)
	require.Equal(t, 10, d.Files[0].StartLine)
	require.Equal(t, []HunkRange{{Start: 10, End: 14}, {Start: 50, End: 53}}, d.Files[0].HunkRanges)
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