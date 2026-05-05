package critics

import (
	"context"
	"encoding/json"
	_ "embed"
	"strings"

	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/verdict"
)

//go:embed prompts/adjudicator.md
var adjudicatorPrompt string

type Adjudicator struct {
	baseCritic
}

func init() {
	Register(&Adjudicator{
		baseCritic: baseCritic{id: "adjudicator", tier: TierFrontier},
	})
}

func (a *Adjudicator) Run(ctx context.Context, d *diff.Diff, pc *Context) (*verdict.CriticResult, error) {
	client, modelName, err := pc.Router.ClientForTier("frontier")
	if err != nil {
		return nil, err
	}

	var results []string
	for _, cr := range pc.Result.CriticResults {
		b, _ := json.MarshalIndent(cr, "", "  ")
		results = append(results, string(b))
	}

	data := promptData{
		Patch:         truncateIfLong(d.RawPatch),
		CriticResults: strings.Join(results, "\n\n"),
		MaxFindings:   15,
	}

	return runCritic(ctx, a.id, a.tier, adjudicatorPrompt, data, client, modelName, func(tokensIn, tokensOut int) float64 {
		return pc.Budget.Record("anthropic", modelName, tokensIn, tokensOut)
	})
}