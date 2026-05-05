package budget

import "testing"

func TestLedgerRecord(t *testing.T) {
	ledger, err := NewLedger(0.25)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	cost := ledger.Record("ollama_cloud", "gpt-oss:20b", 1000, 500)
	if cost <= 0 {
		t.Errorf("expected positive cost for cloud model, got %f", cost)
	}

	if ledger.Spent() != cost {
		t.Errorf("Spent() = %f, want %f", ledger.Spent(), cost)
	}

	if ledger.Remaining() >= 0.25 {
		t.Errorf("Remaining() = %f, should be less than ceiling", ledger.Remaining())
	}
}

func TestLedgerExhausted(t *testing.T) {
	ledger, err := NewLedger(0.001)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	if ledger.Exhausted() {
		t.Error("should not be exhausted initially")
	}

	ledger.Record("anthropic", "claude-sonnet-4-6", 1_000_000, 100_000)

	if !ledger.Exhausted() {
		t.Error("should be exhausted after large cost")
	}
}

func TestLedgerLocalCostsZero(t *testing.T) {
	ledger, err := NewLedger(0.25)
	if err != nil {
		t.Fatalf("NewLedger: %v", err)
	}

	cost := ledger.Record("ollama_local", "qwen2.5-coder:7b", 1000, 500)
	if cost != 0 {
		t.Errorf("local model should cost 0, got %f", cost)
	}
}