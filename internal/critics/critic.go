package critics

import (
	"context"

	"github.com/helloodokai/acig/internal/budget"
	"github.com/helloodokai/acig/internal/config"
	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/routing"
	"github.com/helloodokai/acig/internal/verdict"
)

type Tier string

const (
	TierCheap    Tier = "cheap"
	TierMid      Tier = "mid"
	TierFrontier Tier = "frontier"
)

type Context struct {
	Config *config.Config
	Router *routing.Router
	Budget *budget.Ledger
	Diff   *diff.Diff
	Risk   verdict.Risk
	Result *verdict.Verdict
}

type Critic interface {
	ID() string
	Tier() Tier
	Run(ctx context.Context, d *diff.Diff, pc *Context) (*verdict.CriticResult, error)
}

var registry = map[string]Critic{}

func Register(c Critic) { registry[c.ID()] = c }
func Get(id string) (Critic, bool) { c, ok := registry[id]; return c, ok }
func All() []Critic {
	var cs []Critic
	for _, c := range registry {
		cs = append(cs, c)
	}
	return cs
}

func NewContext(cfg *config.Config, router *routing.Router, b *budget.Ledger, d *diff.Diff) *Context {
	return &Context{
		Config: cfg,
		Router: router,
		Budget: b,
		Diff:   d,
		Risk:   verdict.RiskLow,
	}
}

func EnabledIDs(enabled []string) []string {
	if len(enabled) == 0 {
		ids := make([]string, 0, len(registry))
		for id := range registry {
			ids = append(ids, id)
		}
		return ids
	}
	return enabled
}