package verdict

import "time"

type Severity string

const (
	SeverityInfo     Severity = "info"
	SeverityLow      Severity = "low"
	SeverityMedium   Severity = "medium"
	SeverityHigh     Severity = "high"
	SeverityBlocking Severity = "blocking"
)

type Risk string

const (
	RiskLow      Risk = "low"
	RiskMedium   Risk = "medium"
	RiskHigh     Risk = "high"
	RiskCritical Risk = "critical"
)

type Decision string

const (
	DecisionPass  Decision = "pass"
	DecisionWarn  Decision = "warn"
	DecisionBlock Decision = "block"
)

type Finding struct {
	Critic       string   `json:"critic"`
	Severity     Severity `json:"severity"`
	Title        string   `json:"title"`
	Detail       string   `json:"detail"`
	File         string   `json:"file,omitempty"`
	LineStart    int      `json:"line_start,omitempty"`
	LineEnd      int      `json:"line_end,omitempty"`
	SuggestedFix string   `json:"suggested_fix,omitempty"`
}

type CriticResult struct {
	Critic     string    `json:"critic"`
	Model      string    `json:"model"`
	Findings   []Finding `json:"findings"`
	CostUSD    float64   `json:"cost_usd"`
	DurationMS int64     `json:"duration_ms"`
	TokensIn   int       `json:"tokens_in"`
	TokensOut  int       `json:"tokens_out"`
	Error      string    `json:"error,omitempty"`
	// Notes carries observability info from the post-parse validator.
	// Examples: "dropped 2 findings outside diff", "downgraded 1 blocking from cheap tier".
	Notes []string `json:"notes,omitempty"`
	// Reasoning is populated by the risk_classifier critic to explain the chosen risk band.
	Reasoning string `json:"reasoning,omitempty"`
	// Risk is populated by the risk_classifier critic.
	Risk Risk `json:"risk,omitempty"`
}

type Verdict struct {
	SchemaVersion      string         `json:"schema_version"`
	Repo               string         `json:"repo"`
	SHA                string         `json:"sha"`
	BaseSHA            string         `json:"base_sha"`
	Risk               Risk           `json:"risk"`
	Decision           Decision       `json:"decision"`
	Summary            string         `json:"summary"`
	Findings           []Finding      `json:"findings"`
	CriticResults      []CriticResult `json:"critic_results"`
	TotalCostUSD       float64        `json:"total_cost_usd"`
	TotalDurationMS    int64          `json:"total_duration_ms"`
	BudgetRemainingUSD float64        `json:"budget_remaining_usd"`
	GeneratedAt        time.Time      `json:"generated_at"`
}
