package verdict

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFilterFindings(t *testing.T) {
	findings := []Finding{
		{Critic: "security_smell", Title: "Hardcoded secret", File: "auth.go", Severity: SeverityHigh},
		{Critic: "style_conformance", Title: "Missing doc comment", File: "main.go", Severity: SeverityLow},
		{Critic: "security_smell", Title: "SQL injection", File: "db.go", Severity: SeverityBlocking},
	}

	suppressions := []Suppression{
		{Critic: "style_conformance", Title: "Missing doc comment", File: "main.go", Reason: "acceptable"},
		{Critic: "security_smell", Title: "Hardcoded secret", Reason: "test credential"},
	}

	filtered := FilterFindings(findings, suppressions)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 filtered finding, got %d", len(filtered))
	}
	if filtered[0].Title != "SQL injection" {
		t.Fatalf("expected SQL injection to remain, got %s", filtered[0].Title)
	}
}

func TestFilterFindingsEmpty(t *testing.T) {
	findings := []Finding{
		{Critic: "test", Title: "a", Severity: SeverityLow},
	}
	filtered := FilterFindings(findings, nil)
	if len(filtered) != 1 {
		t.Fatalf("expected findings to pass through with no suppressions, got %d", len(filtered))
	}
}

func TestIsSuppressed(t *testing.T) {
	f := Finding{Critic: "security_smell", Title: "Hardcoded secret", File: "auth.go"}

	tests := []struct {
		name string
		s    Suppression
		want bool
	}{
		{"exact match", Suppression{Critic: "security_smell", Title: "Hardcoded secret", File: "auth.go"}, true},
		{"critic+title match", Suppression{Critic: "security_smell", Title: "Hardcoded secret"}, true},
		{"critic only match", Suppression{Critic: "security_smell"}, true},
		{"wrong critic", Suppression{Critic: "style"}, false},
		{"wrong title", Suppression{Title: "Other"}, false},
		{"wrong file", Suppression{Critic: "security_smell", Title: "Hardcoded secret", File: "other.go"}, false},
		{"empty suppression", Suppression{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSuppressed(f, []Suppression{tt.s}); got != tt.want {
				t.Errorf("isSuppressed() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLoadSuppressionsExpiry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "suppressions.toml")

	content := `
[[suppression]]
critic = "test"
title = "expired"
reason = "old"
expires = "2020-01-01"

[[suppression]]
critic = "test"
title = "active"
reason = "current"

[[suppression]]
critic = "test"
title = "future"
reason = "later"
expires = "2099-12-31"
`
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	s, err := LoadSuppressions(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 2 {
		t.Fatalf("expected 2 active suppressions (expired should be skipped), got %d", len(s))
	}
	titles := map[string]bool{}
	for _, supp := range s {
		titles[supp.Title] = true
	}
	if titles["expired"] {
		t.Error("expired suppression should have been skipped")
	}
	if !titles["active"] {
		t.Error("active suppression should be present")
	}
	if !titles["future"] {
		t.Error("future-dated suppression should be present")
	}
}

func TestSaveAndLoadSuppressions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "suppressions.toml")

	supps := []Suppression{
		{Critic: "security_smell", Title: "Hardcoded secret", Reason: "test env var"},
		{Critic: "style", Title: "Long function", File: "main.go", Reason: "known tech debt"},
	}

	if err := SaveSuppressions(path, supps); err != nil {
		t.Fatal(err)
	}

	loaded, err := LoadSuppressions(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) != 2 {
		t.Fatalf("expected 2 suppressions, got %d", len(loaded))
	}
	if loaded[0].Critic != "security_smell" || loaded[0].Title != "Hardcoded secret" {
		t.Errorf("unexpected first suppression: %+v", loaded[0])
	}
	if loaded[1].File != "main.go" {
		t.Errorf("expected file=main.go, got %s", loaded[1].File)
	}
}

func TestLoadSuppressionsNonexistent(t *testing.T) {
	s, err := LoadSuppressions("/nonexistent/path")
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 0 {
		t.Fatalf("expected nil suppressions for nonexistent file, got %d", len(s))
	}
}

func TestLoadSuppressionsEmptyPath(t *testing.T) {
	s, err := LoadSuppressions("")
	if err != nil {
		t.Fatal(err)
	}
	if len(s) != 0 {
		t.Fatalf("expected nil suppressions for empty path, got %d", len(s))
	}
}

func TestIsSuppressedEmptySuppressionMatchesNothing(t *testing.T) {
	f := Finding{Critic: "test", Title: "a", File: "b.go"}
	s := Suppression{}
	if isSuppressed(f, []Suppression{s}) {
		t.Error("empty suppression should not match anything")
	}
}

func TestFilterFindingsWildcardCritic(t *testing.T) {
	findings := []Finding{
		{Critic: "security_smell", Title: "X", Severity: SeverityHigh},
		{Critic: "style", Title: "Y", Severity: SeverityLow},
	}
	suppressions := []Suppression{
		{Title: "X"},
	}
	filtered := FilterFindings(findings, suppressions)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 finding after suppression, got %d", len(filtered))
	}
	if filtered[0].Title != "Y" {
		t.Errorf("expected Y to remain, got %s", filtered[0].Title)
	}
}