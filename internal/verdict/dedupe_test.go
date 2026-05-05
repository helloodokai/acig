package verdict

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDedupeFindingsIdentical(t *testing.T) {
	findings := []Finding{
		{Critic: "a", Title: "Hardcoded secret", File: "foo.go", Severity: SeverityHigh},
		{Critic: "b", Title: "Hardcoded secret", File: "foo.go", Severity: SeverityHigh},
	}
	result := DedupeFindings(findings)
	require.Len(t, result, 1)
	require.Equal(t, "a", result[0].Critic)
}

func TestDedupeFindingsDifferentSeverity(t *testing.T) {
	findings := []Finding{
		{Critic: "a", Title: "Hardcoded secret", File: "foo.go", Severity: SeverityBlocking},
		{Critic: "b", Title: "Hardcoded secret", File: "foo.go", Severity: SeverityMedium},
	}
	result := DedupeFindings(findings)
	require.Len(t, result, 2)
}

func TestDedupeFindingsDifferentFile(t *testing.T) {
	findings := []Finding{
		{Critic: "a", Title: "Hardcoded secret", File: "foo.go", Severity: SeverityHigh},
		{Critic: "b", Title: "Hardcoded secret", File: "bar.go", Severity: SeverityHigh},
	}
	result := DedupeFindings(findings)
	require.Len(t, result, 2)
}

func TestDedupeFindingsDifferentTitle(t *testing.T) {
	findings := []Finding{
		{Critic: "a", Title: "SQL injection", File: "foo.go", Severity: SeverityBlocking},
		{Critic: "b", Title: "Hardcoded secret", File: "foo.go", Severity: SeverityBlocking},
	}
	result := DedupeFindings(findings)
	require.Len(t, result, 2)
}

func TestDedupeFindingsCaseInsensitive(t *testing.T) {
	findings := []Finding{
		{Critic: "a", Title: "Hardcoded Secret", File: "foo.go", Severity: SeverityHigh},
		{Critic: "b", Title: "hardcoded secret", File: "foo.go", Severity: SeverityHigh},
	}
	result := DedupeFindings(findings)
	require.Len(t, result, 1)
}

func TestDedupeFindingsDetailTruncation(t *testing.T) {
	longDetail := strings.Repeat("x", 200)
	findings := []Finding{
		{Critic: "a", Title: "Issue", File: "foo.go", Severity: SeverityHigh, Detail: longDetail},
		{Critic: "b", Title: "Issue", File: "foo.go", Severity: SeverityHigh, Detail: longDetail[:100] + longDetail[150:]},
	}
	result := DedupeFindings(findings)
	require.Len(t, result, 1)
}

func TestDedupeFindingsNoFile(t *testing.T) {
	findings := []Finding{
		{Critic: "a", Title: "No SQL injection patterns", Severity: SeverityInfo},
		{Critic: "b", Title: "No SQL injection patterns", Severity: SeverityInfo},
	}
	result := DedupeFindings(findings)
	require.Len(t, result, 1)
}

func TestWorstSeverity(t *testing.T) {
	require.Equal(t, SeverityBlocking, WorstSeverity(SeverityBlocking, SeverityInfo))
	require.Equal(t, SeverityHigh, WorstSeverity(SeverityHigh, SeverityMedium))
	require.Equal(t, SeverityMedium, WorstSeverity(SeverityLow, SeverityMedium))
	require.Equal(t, SeverityInfo, WorstSeverity(SeverityInfo, SeverityInfo))
}