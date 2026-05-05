package budget

import (

	_ "embed"
	"fmt"
	"sync"

	"github.com/BurntSushi/toml"
)

//go:embed pricing.toml
var pricingData []byte

type PricingEntry struct {
	InputPerMTok  float64 `toml:"input_per_mtok"`
	OutputPerMTok float64 `toml:"output_per_mtok"`
}

type PricingTable struct {
	Models map[string]PricingEntry `toml:"models"`
}

type Ledger struct {
	mu         sync.Mutex
	spent      float64
	ceiling    float64
	pricing    PricingTable
}

func NewLedger(ceiling float64) (*Ledger, error) {
	var pt PricingTable
	if err := toml.Unmarshal(pricingData, &pt); err != nil {
		return nil, fmt.Errorf("loading pricing table: %w", err)
	}
	return &Ledger{
		ceiling: ceiling,
		pricing: pt,
	}, nil
}

func (l *Ledger) Record(provider, model string, tokensIn, tokensOut int) float64 {
	l.mu.Lock()
	defer l.mu.Unlock()

	key := provider + "/" + model
	entry, ok := l.pricing.Models[key]
	if !ok {
		return 0
	}

	cost := (float64(tokensIn)/1_000_000)*entry.InputPerMTok +
		(float64(tokensOut)/1_000_000)*entry.OutputPerMTok

	l.spent += cost
	return cost
}

func (l *Ledger) Remaining() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.ceiling - l.spent
}

func (l *Ledger) Spent() float64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.spent
}

func (l *Ledger) Exhausted() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.spent >= l.ceiling
}

func (l *Ledger) Ceiling() float64 { return l.ceiling }

func (l *Ledger) EstimateCost(provider, model string, tokensIn, tokensOut int) float64 {
	key := provider + "/" + model
	entry, ok := l.pricing.Models[key]
	if !ok {
		return 0
	}
	return (float64(tokensIn)/1_000_000)*entry.InputPerMTok +
		(float64(tokensOut)/1_000_000)*entry.OutputPerMTok
}

func LoadPricing() (*PricingTable, error) {
	var pt PricingTable
	if err := toml.Unmarshal(pricingData, &pt); err != nil {
		return nil, fmt.Errorf("loading pricing: %w", err)
	}
	return &pt, nil
}