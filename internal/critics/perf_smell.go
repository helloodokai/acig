package critics

import (
	"context"
	_ "embed"

	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/verdict"
)

//go:embed prompts/perf_smell.md
var perfSmellPrompt string

type PerfSmell struct {
	baseCritic
}

func init() {
	Register(&PerfSmell{
		baseCritic: baseCritic{id: "perf_smell", tier: TierMid},
	})
}

func (ps *PerfSmell) Run(ctx context.Context, d *diff.Diff, pc *Context) (*verdict.CriticResult, error) {
	client, modelName, err := pc.Router.ClientForTier("mid")
	if err != nil {
		return nil, err
	}

	data := diffToPromptData(d, pc)
	return runCritic(ctx, ps.id, ps.tier, perfSmellPrompt, data, client, modelName, func(tokensIn, tokensOut int) float64 {
		return pc.Budget.Record("ollama_cloud", modelName, tokensIn, tokensOut)
	})
}