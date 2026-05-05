package verdict

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestVerdictJSONRoundTrip(t *testing.T) {
	v := Verdict{
		SchemaVersion:      "1",
		Repo:               "github.com/helloodokai/acig",
		SHA:                "abc123",
		BaseSHA:            "def456",
		Risk:               RiskLow,
		Decision:           DecisionPass,
		Summary:            "No issues found",
		Findings:           []Finding{},
		CriticResults:      []CriticResult{},
		TotalCostUSD:       0.001,
		BudgetRemainingUSD: 0.249,
		GeneratedAt:        time.Now().UTC(),
	}

	b, err := json.Marshal(v)
	require.NoError(t, err)

	var decoded Verdict
	require.NoError(t, json.Unmarshal(b, &decoded))
	require.Equal(t, "1", decoded.SchemaVersion)
	require.Equal(t, RiskLow, decoded.Risk)
	require.Equal(t, DecisionPass, decoded.Decision)
}

func TestSeverityConstants(t *testing.T) {
	require.Equal(t, Severity("info"), SeverityInfo)
	require.Equal(t, Severity("low"), SeverityLow)
	require.Equal(t, Severity("medium"), SeverityMedium)
	require.Equal(t, Severity("high"), SeverityHigh)
	require.Equal(t, Severity("blocking"), SeverityBlocking)
}

func TestRiskConstants(t *testing.T) {
	require.Equal(t, Risk("low"), RiskLow)
	require.Equal(t, Risk("medium"), RiskMedium)
	require.Equal(t, Risk("high"), RiskHigh)
	require.Equal(t, Risk("critical"), RiskCritical)
}

func TestDecisionConstants(t *testing.T) {
	require.Equal(t, Decision("pass"), DecisionPass)
	require.Equal(t, Decision("warn"), DecisionWarn)
	require.Equal(t, Decision("block"), DecisionBlock)
}