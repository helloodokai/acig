package critics

import (
	"context"
	_ "embed"

	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/verdict"
)

//go:embed prompts/test_coverage_smell.md
var testCoverageSmellPrompt string

type TestCoverageSmell struct {
	baseCritic
}

func init() {
	Register(&TestCoverageSmell{
		baseCritic: baseCritic{id: "test_coverage_smell", tier: TierCheap},
	})
}

func (tc *TestCoverageSmell) Run(ctx context.Context, d *diff.Diff, pc *Context) (*verdict.CriticResult, error) {
	client, modelName, err := pc.Router.ClientForTier("cheap")
	if err != nil {
		return nil, err
	}

	data := diffToPromptData(d, pc)
	return runCritic(ctx, tc.id, tc.tier, testCoverageSmellPrompt, data, client, modelName, func(tokensIn, tokensOut int) float64 {
		return pc.Budget.Record("ollama_cloud", modelName, tokensIn, tokensOut)
	})
}