package critics

import (
	"context"
	_ "embed"

	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/verdict"
)

//go:embed prompts/security_smell.md
var securitySmellPrompt string

//go:embed prompts/findings.schema.json
var findingsSchema []byte

type SecuritySmell struct {
	baseCritic
}

func init() {
	Register(&SecuritySmell{
		baseCritic: baseCritic{id: "security_smell", tier: TierMid},
	})
}

func (sc *SecuritySmell) Run(ctx context.Context, d *diff.Diff, pc *Context) (*verdict.CriticResult, error) {
	client, modelName, err := pc.Router.ClientForTier("mid")
	if err != nil {
		return nil, err
	}

	data := diffToPromptData(d, pc)
	return runCritic(ctx, sc.id, sc.tier, securitySmellPrompt, data, client, modelName, func(tokensIn, tokensOut int) float64 {
		return pc.Budget.Record("ollama_cloud", modelName, tokensIn, tokensOut)
	}, d, findingsSchema)
}
