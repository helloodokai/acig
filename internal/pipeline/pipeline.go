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

const defaultConcurrency = 8

type ProgressFunc func(criticID string, status string)

type Pipeline struct {
	cfg          *config.Config
	router       *routing.Router
	ledger       *budget.Ledger
	d            *diff.Diff
	suppressions []verdict.Suppression
	onProgress   ProgressFunc
}

func New(cfg *config.Config, router *routing.Router, ledger *budget.Ledger, d *diff.Diff, suppressions []verdict.Suppression) *Pipeline {
	return &Pipeline{cfg: cfg, router: router, ledger: ledger, d: d, suppressions: suppressions}
}

func (p *Pipeline) OnProgress(fn ProgressFunc) {
	p.onProgress = fn
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

	enabled := critics.EnabledIDs(p.cfg.Critics.Enabled)
	var allCritics []critics.Critic
	for _, id := range enabled {
		if id == "adjudicator" {
			continue
		}
		c, ok := critics.Get(id)
		if !ok {
			slog.Warn("unknown critic, skipping", "id", id)
			continue
		}
		allCritics = append(allCritics, c)
	}

	if p.cfg.Charter.Path != "" {
		if cc, ok := critics.Get("charter_conformance"); ok {
			allCritics = append(allCritics, cc)
		}
	}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(defaultConcurrency)

	var resultsMu sync.Mutex
	var riskResult *verdict.CriticResult

	for _, c := range allCritics {
		c := c
		g.Go(func() error {
			if p.onProgress != nil {
				p.onProgress(c.ID(), "running")
			}
			slog.Info("running critic", "id", c.ID())
			result, err := c.Run(gctx, p.d, pc)
			if err != nil {
				slog.Error("critic error", "id", c.ID(), "error", err)
				if p.onProgress != nil {
					p.onProgress(c.ID(), "error")
				}
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
			if c.ID() == "risk_classifier" {
				riskResult = result
			}
			resultsMu.Unlock()
			if p.onProgress != nil {
				p.onProgress(c.ID(), "done")
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, fmt.Errorf("critic fan-out: %w", err)
	}

	risk := verdict.RiskLow
	if riskResult != nil {
		// Prefer the explicit risk band the new risk_classifier emits.
		if riskResult.Risk != "" {
			risk = riskResult.Risk
		} else {
			risk = classifyRisk(riskResult)
		}
	}
	pc.Risk = risk
	if pc.Result.Risk == "" {
		pc.Result.Risk = risk
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
			if p.onProgress != nil {
				p.onProgress("adjudicator", "running")
			}
			slog.Info("running adjudicator")
			adjResult, err := adj.Run(ctx, p.d, pc)
			if err != nil {
				slog.Error("adjudicator error", "error", err)
				if p.onProgress != nil {
					p.onProgress("adjudicator", "error")
				}
			} else {
				pc.Result.CriticResults = append(pc.Result.CriticResults, *adjResult)
				pc.Result.Findings = append(pc.Result.Findings, adjResult.Findings...)
				if p.onProgress != nil {
					p.onProgress("adjudicator", "done")
				}
			}
		}
	}

	finalize(pc.Result, p.ledger, p.suppressions)
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

func finalize(v *verdict.Verdict, ledger *budget.Ledger, suppressions []verdict.Suppression) {
	v.Findings = verdict.DedupeFindings(v.Findings)
	v.Findings = verdict.FilterFindings(v.Findings, suppressions)
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
