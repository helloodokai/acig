package critics

import (
	"context"
	_ "embed"

	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/verdict"
)

//go:embed prompts/risk_classifier.md
var riskClassifierPrompt string

//go:embed prompts/risk_classifier.schema.json
var riskClassifierSchema []byte

type RiskClassifier struct {
	baseCritic
}

func init() {
	Register(&RiskClassifier{
		baseCritic: baseCritic{id: "risk_classifier", tier: TierCheap, promptTmpl: riskClassifierPrompt},
	})
}

func (rc *RiskClassifier) Run(ctx context.Context, d *diff.Diff, pc *Context) (*verdict.CriticResult, error) {
	client, modelName, err := pc.Router.ClientForTier("cheap")
	if err != nil {
		return nil, err
	}

	data := diffToPromptData(d, pc)
	return runCritic(ctx, rc.id, rc.tier, riskClassifierPrompt, data, client, modelName, func(tokensIn, tokensOut int) float64 {
		return pc.Budget.Record("ollama_cloud", modelName, tokensIn, tokensOut)
	}, d, riskClassifierSchema)
}
