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