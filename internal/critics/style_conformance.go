package critics

import (
	"context"
	_ "embed"

	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/verdict"
)

//go:embed prompts/style_conformance.md
var styleConformancePrompt string

type StyleConformance struct {
	baseCritic
}

func init() {
	Register(&StyleConformance{
		baseCritic: baseCritic{id: "style_conformance", tier: TierCheap},
	})
}

func (sc *StyleConformance) Run(ctx context.Context, d *diff.Diff, pc *Context) (*verdict.CriticResult, error) {
	client, modelName, err := pc.Router.ClientForTier("cheap")
	if err != nil {
		return nil, err
	}

	data := diffToPromptData(d, pc)
	return runCritic(ctx, sc.id, sc.tier, styleConformancePrompt, data, client, modelName, func(tokensIn, tokensOut int) float64 {
		return pc.Budget.Record("ollama_cloud", modelName, tokensIn, tokensOut)
	}, d, findingsSchema)
}
