package pipeline

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"

	"github.com/helloodokai/acig/internal/budget"
	"github.com/helloodokai/acig/internal/config"
	"github.com/helloodokai/acig/internal/critics"
	"github.com/helloodokai/acig/internal/diff"
	"github.com/helloodokai/acig/internal/routing"
	"github.com/helloodokai/acig/internal/verdict"
)

const defaultConcurrency = 4

type Pipeline struct {
	cfg    *config.Config
	router *routing.Router
	ledger *budget.Ledger
	d      *diff.Diff
	mu     sync.Mutex
}

func New(cfg *config.Config, router *routing.Router, ledger *budget.Ledger, d *diff.Diff) *Pipeline {
	return &Pipeline{cfg: cfg, router: router, ledger: ledger, d: d}
}

func (p *Pipeline) Execute(ctx context.Context, repo, sha, baseSHA string) (*verdict.Verdict, error) {
	pc := critics.NewContext(p.cfg, p.router, p.ledger, p.d)
	pc.Result = &verdict.Verdict{
		SchemaVersion: "1",
		Repo:          repo,
		SHA:           sha,
		BaseSHA:       baseSHA,
		GeneratedAt:   time.Now().UTC(),
	}

	riskCritic, ok := critics.Get("risk_classifier")
	if !ok {
		return nil, fmt.Errorf("risk_classifier critic not registered")
	}

	slog.Info("running risk classifier", "critic", riskCritic.ID())
	riskResult, err := riskCritic.Run(ctx, p.d, pc)
	if err != nil {
		return nil, fmt.Errorf("risk classifier failed: %w", err)
	}
	pc.Result.CriticResults = append(pc.Result.CriticResults, *riskResult)
	pc.Result.Findings = append(pc.Result.Findings, riskResult.Findings...)

	risk := classifyRisk(riskResult)
	pc.Risk = risk
	if pc.Result.Risk == "" {
		pc.Result.Risk = risk
	}

	enabled := critics.EnabledIDs(p.cfg.Critics.Enabled)
	var criticsToRun []critics.Critic
	for _, id := range enabled {
		if id == "risk_classifier" || id == "adjudicator" {
			continue
		}
		c, ok := critics.Get(id)
		if !ok {
			slog.Warn("unknown critic, skipping", "id", id)
			continue
		}
		if p.ledger.Exhausted() {
			slog.Warn("budget exhausted, skipping critic", "id", id)
			continue
		}
		criticsToRun = append(criticsToRun, c)
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(defaultConcurrency)

	var resultsMu sync.Mutex

	for _, c := range criticsToRun {
		c := c
		g.Go(func() error {
			slog.Info("running critic", "id", c.ID())
			result, err := c.Run(gctx, p.d, pc)
			if err != nil {
				slog.Error("critic error", "id", c.ID(), "error", err)
				resultsMu.Lock()
				pc.Result.CriticResults = append(pc.Result.CriticResults, verdict.CriticResult{
					Critic: c.ID(),
					Error:  err.Error(),
				})
				resultsMu.Unlock()
				return nil
			}
			resultsMu.Lock()
			pc.Result.CriticResults = append(pc.Result.CriticResults, *result)
			pc.Result.Findings = append(pc.Result.Findings, result.Findings...)
			resultsMu.Unlock()
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("critic fan-out: %w", err)
	}

	shouldRunAdjudicator := false
	for _, trigger := range p.cfg.Critics.Adjudicator.TriggerOn {
		switch trigger {
		case "risk:high":
			if risk == verdict.RiskHigh || risk == verdict.RiskCritical {
				shouldRunAdjudicator = true
			}
		case "risk:critical":
			if risk == verdict.RiskCritical {
				shouldRunAdjudicator = true
			}
		case "conflict":
			if hasConflict(pc.Result.CriticResults) {
				shouldRunAdjudicator = true
			}
		}
	}

	if shouldRunAdjudicator && !p.ledger.Exhausted() {
		adj, ok := critics.Get("adjudicator")
		if ok {
			slog.Info("running adjudicator")
			adjResult, err := adj.Run(ctx, p.d, pc)
			if err != nil {
				slog.Error("adjudicator error", "error", err)
			} else {
				pc.Result.CriticResults = append(pc.Result.CriticResults, *adjResult)
				pc.Result.Findings = append(pc.Result.Findings, adjResult.Findings...)
			}
		}
	}

	finalize(pc.Result, p.ledger)
	return pc.Result, nil
}

func NewContext(cfg *config.Config, router *routing.Router, b *budget.Ledger, d *diff.Diff) *critics.Context {
	return critics.NewContext(cfg, router, b, d)
}

func classifyRisk(result *verdict.CriticResult) verdict.Risk {
	for _, f := range result.Findings {
		if f.Severity == verdict.SeverityBlocking || f.Severity == verdict.SeverityHigh {
			return verdict.RiskHigh
		}
	}
	for _, f := range result.Findings {
		if f.Severity == verdict.SeverityMedium {
			return verdict.RiskMedium
		}
	}
	return verdict.RiskLow
}

func hasConflict(results []verdict.CriticResult) bool {
	severityCounts := map[verdict.Severity]int{}
	for _, r := range results {
		if r.Error != "" {
			continue
		}
		for _, f := range r.Findings {
			severityCounts[f.Severity]++
		}
	}
	return len(severityCounts) >= 3
}

func finalize(v *verdict.Verdict, ledger *budget.Ledger) {
	v.TotalCostUSD = ledger.Spent()
	v.BudgetRemainingUSD = ledger.Remaining()
	v.Risk = computeRisk(v.Findings, v.Risk)

	totalMS := int64(0)
	for _, cr := range v.CriticResults {
		totalMS += cr.DurationMS
	}
	v.TotalDurationMS = totalMS

	v.Decision = computeDecision(v.Findings)
	v.Summary = generateSummary(v)
}

func computeRisk(findings []verdict.Finding, currentRisk verdict.Risk) verdict.Risk {
	risk := currentRisk
	if risk == "" {
		risk = verdict.RiskLow
	}
	for _, f := range findings {
		switch f.Severity {
		case verdict.SeverityBlocking:
			return verdict.RiskCritical
		case verdict.SeverityHigh:
			if risk != verdict.RiskCritical {
				risk = verdict.RiskHigh
			}
		case verdict.SeverityMedium:
			if risk == verdict.RiskLow {
				risk = verdict.RiskMedium
			}
		}
	}
	return risk
}

func computeDecision(findings []verdict.Finding) verdict.Decision {
	for _, f := range findings {
		if f.Severity == verdict.SeverityBlocking {
			return verdict.DecisionBlock
		}
	}
	for _, f := range findings {
		if f.Severity == verdict.SeverityHigh {
			return verdict.DecisionWarn
		}
	}
	for _, f := range findings {
		if f.Severity == verdict.SeverityMedium {
			return verdict.DecisionWarn
		}
	}
	return verdict.DecisionPass
}

func generateSummary(v *verdict.Verdict) string {
	blocking := 0
	high := 0
	medium := 0
	low := 0
	info := 0

	for _, f := range v.Findings {
		switch f.Severity {
		case verdict.SeverityBlocking:
			blocking++
		case verdict.SeverityHigh:
			high++
		case verdict.SeverityMedium:
			medium++
		case verdict.SeverityLow:
			low++
		case verdict.SeverityInfo:
			info++
		}
	}

	return fmt.Sprintf("Decision: %s | Risk: %s | Findings: %d blocking, %d high, %d medium, %d low, %d info | Cost: $%.4f",
		v.Decision, v.Risk, blocking, high, medium, low, info, v.TotalCostUSD)
}