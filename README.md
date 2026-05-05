# acig — Agentic CI Gateway

**Tiered code review via cheap critics + frontier adjudication.** `acig` sits in front of normal CI, fanning out lightweight "critic" models in parallel and escalating to a frontier model only when needed. It emits a machine-readable JSON verdict that coding agents (and humans) can consume.

## Install

**Homebrew (macOS / Linux):**

```bash
brew tap helloodokai/tap
brew install acig
```

**Binary from GitHub Releases:**

```bash
# macOS (arm64)
curl -sSL https://github.com/helloodokai/acig/releases/latest/download/acig_darwin_arm64.tar.gz \
  | tar -xz -C /usr/local/bin acig

# Linux (amd64)
curl -sSL https://github.com/helloodokai/acig/releases/latest/download/acig_linux_amd64.tar.gz \
  | tar -xz -C /usr/local/bin acig

# Windows — download the .zip from the releases page
```

## 60-Second Quick Start (Cloud)

1. **Set your Ollama Cloud API key:**

```bash
export OLLAMA_API_KEY="your-key-here"
```

> Get a key at [ollama.com](https://ollama.com). Ollama Cloud runs open-weight models at a fraction of frontier-API prices — no local GPU needed.

2. **Run against your current diff:**

```bash
acig run --profile cloud
```

3. **Done.** You'll get a JSON verdict on stdout (or markdown with `--format md`).

## Commands

| Command | Description |
|---------|-------------|
| `acig run` | Run the critic pipeline on a diff |
| `acig install-hook` | Install the acig pre-push hook |
| `acig explain <verdict.json>` | Pretty-print a verdict for humans |
| `acig doctor` | Check that backends are reachable and API keys are set |
| `acig schema` | Print the Verdict JSON schema |

### `acig run` flags

| Flag | Default | Description |
|------|---------|-------------|
| `--diff` | auto-detect | Git diff range (e.g. `HEAD~3..HEAD`) |
| `--config` | `.acig.toml` | Config file path |
| `--format` | `json` | Output format: `json`, `md`, `both` |
| `--out` | stdout | Output file base path (extensions added automatically) |
| `--budget` | from config | Per-run budget in USD |
| `--profile` | from config | `cloud` or `local` |

### Exit codes

| Code | Meaning |
|------|---------|
| 0 | `pass` — no issues |
| 1 | `warn` — findings but none blocking |
| 2 | `block` — blocking findings present |
| ≥10 | Tool error (config, network, etc.) |

## OLLAMA_API_KEY Setup

1. Sign up at [ollama.com](https://ollama.com).
2. Navigate to your account settings and generate an API key.
3. Set it in your environment:

```bash
export OLLAMA_API_KEY="ollama-your-key-here"
```

4. Verify with `acig doctor`.

Ollama Cloud is the default backend for `cheap` and `mid` critic tiers. It runs models like `gpt-oss:20b` and `qwen3-coder:480b` — fast, cheap, and capable enough for most code review tasks.

## GitHub Actions Integration

Add this reusable workflow to your repo at `.github/workflows/acig.yml` (or reference the one in this repo):

```yaml
name: Acig

on:
  pull_request:

jobs:
  acig:
    runs-on: ubuntu-latest
    permissions:
      contents: read
      pull-requests: write
      checks: write
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - name: Install acig
        run: |
          curl -sSL https://github.com/helloodokai/acig/releases/latest/download/acig_linux_amd64.tar.gz \
            | tar -xz -C /usr/local/bin acig
      - name: Run acig
        env:
          OLLAMA_API_KEY: ${{ secrets.OLLAMA_API_KEY }}
          ANTHROPIC_API_KEY: ${{ secrets.ANTHROPIC_API_KEY }}
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: acig run --format both --out acig-verdict --profile cloud
      - uses: actions/upload-artifact@v4
        with:
          name: acig-verdict
          path: acig-verdict.json
```

**Required secrets:**
- `OLLAMA_API_KEY` — for cloud critics (required)
- `ANTHROPIC_API_KEY` — for frontier adjudicator (optional; only triggered on high-risk diffs)
- `GITHUB_TOKEN` — automatically provided; needs `pull-requests: write` and `checks: write` permissions

`acig` will post a sticky PR comment (updated on each push) and create a check run.

## Pre-Push Hook

```bash
acig install-hook
```

This writes `.git/hooks/pre-push` that:
- Runs `acig run --format md --profile cloud` on the commits about to be pushed
- Blocks the push if the verdict is `block`
- Respects `ACIG_SKIP=1` to bypass (with a logged warning)

The hook targets the `cloud` profile by default for speed. On metered networks, switch to `--profile local`.

## Configuration (`.acig.toml`)

Create `.acig.toml` in your repo root. Everything has sensible defaults — it works without one.

```toml
[budget]
per_run_usd = 0.25

[models]
default_profile   = "cloud"
fallback_to_local = true

[models.profiles.cloud]
cheap    = { provider = "ollama_cloud", name = "gpt-oss:20b" }
mid      = { provider = "ollama_cloud", name = "qwen3-coder:480b" }
frontier = { provider = "anthropic",    name = "claude-sonnet-4-6" }

[models.profiles.local]
cheap    = { provider = "ollama_local", name = "qwen2.5-coder:7b",  host = "http://localhost:11434" }
mid      = { provider = "ollama_local", name = "qwen2.5-coder:32b", host = "http://localhost:11434" }
frontier = { provider = "anthropic",    name = "claude-sonnet-4-6" }

[models.ollama_cloud]
host    = "https://ollama.com"
api_key = "${OLLAMA_API_KEY}"

[critics]
enabled = ["risk_classifier", "style_conformance", "test_coverage_smell", "security_smell", "perf_smell"]

[critics.adjudicator]
trigger_on = ["risk:high", "risk:critical", "conflict"]

[paths]
critical = ["src/auth/**", "src/payments/**", "migrations/**"]
```

**Key options:**
- `budget.per_run_usd` — maximum spend per `acig run` invocation (default: $0.25)
- `models.default_profile` — `cloud` or `local`
- `models.fallback_to_local` — if Ollama Cloud fails, fall back to local Ollama
- `critics.enabled` — which critics to run
- `critics.adjudicator.trigger_on` — when to run the frontier adjudicator
- `paths.critical` — glob patterns; changes here auto-bump risk to `high`

### Environment variable interpolation

Use `${VAR_NAME}` in config values to interpolate from environment:

```toml
[models.ollama_cloud]
api_key = "${OLLAMA_API_KEY}"
```

## Critics

| Critic | Tier | What it checks |
|--------|------|----------------|
| `risk_classifier` | cheap | Overall risk (runs first; gates other critics) |
| `style_conformance` | cheap | Naming, formatting, docs, dead code, magic numbers |
| `test_coverage_smell` | cheap | Missing tests for new logic, untested error paths |
| `security_smell` | mid | SQL injection, XSS, hardcoded secrets, auth issues |
| `perf_smell` | mid | N+1 queries, unbounded allocations, missing timeouts |
| `adjudicator` | frontier | Resolves conflicts between critics; adds missed findings |

The **risk_classifier** always runs first and synchronously. Its output determines which other critics are needed. Low-risk diffs skip the expensive adjudicator entirely.

## Verdict JSON Schema

Run `acig schema` to output the full JSON schema. The verdict always includes:

```json
{
  "schema_version": "1",
  "repo": "github.com/owner/repo",
  "sha": "abc123...",
  "base_sha": "def456...",
  "risk": "low|medium|high|critical",
  "decision": "pass|warn|block",
  "summary": "Decision: pass | Risk: low | Findings: 0 blocking, 0 high, ...",
  "findings": [...],
  "critic_results": [...],
  "total_cost_usd": 0.0042,
  "total_duration_ms": 3200,
  "budget_remaining_usd": 0.2458,
  "generated_at": "2025-01-15T10:30:00Z"
}
```

The schema is stable across versions. Use it to validate automated consumers.

## For AI Agents Consuming This Output

If you are a coding agent reading this, here's what you need:

1. **Read the verdict JSON.** It's the single source of truth. Find the `acig-verdict.json` artifact or look for the `<details>` block in the PR comment.
2. **`decision` tells you the gate.** `"pass"` → you're done. `"warn"` → optional fixes. `"block"` → you must address findings before merge.
3. **Each `finding` has a `severity`, `file`, `line_start`, and `suggested_fix`.** Use these to target your edits.
4. **`critic_results` shows which models ran and what they cost.** The `adjudicator` critic is authoritative when present.
5. **Validate against the schema** (`acig schema`) before relying on fields. New fields may be added; existing fields will not be removed or renamed within a `schema_version`.
6. **Budget is tracked.** If `budget_remaining_usd <= 0`, some critics were skipped. Re-run with a higher budget if needed.

## Cost & Latency Reality Check

Approximate cost and latency on a representative ~200-line diff (5 files changed):

| Tier | Provider | Model | Cost | Latency |
|------|----------|-------|------|---------|
| cheap | Ollama Cloud | `gpt-oss:20b` | ~$0.001 | 1-3s |
| mid | Ollama Cloud | `qwen3-coder:480b` | ~$0.005 | 2-5s |
| cheap | Local Ollama | `qwen2.5-coder:7b` | $0 | 1-4s |
| mid | Local Ollama | `qwen2.5-coder:32b` | $0 | 3-8s |
| frontier | Anthropic | `claude-sonnet-4-6` | ~$0.03 | 2-6s |
| | **Full pipeline (cloud)** | | **~$0.02** | **5-15s** |
| | **Full pipeline (local)** | | **$0** | **8-20s** |

*Latency varies by diff size, network, and model load. These are observed averages.*

A full cloud run on a typical diff costs well under $0.01 per run — ~40x cheaper than running a single frontier model on the same input.

## How It Works

```
┌─────────────┐
│  acig run   │
└─────┬───────┘
      │
      ▼
┌──────────────────┐     cheap tier
│  risk_classifier │◄──────────────── Ollama Cloud
└─────┬────────────┘
      │ risk = low/medium/high/critical
      ▼
┌──────────────────────────────────────────────┐
│                 Fan-out (4 concurrent)       │
│                                              │
│  style_conformance  ──► cheap  ──► Cloud     │
│  test_coverage_smell ──► cheap  ──► Cloud     │
│  security_smell     ──► mid    ──► Cloud     │
│  perf_smell         ──► mid    ──► Cloud     │
└──────────────────────┬───────────────────────┘
                       │
                       ▼ (if high-risk or critic conflict)
              ┌────────────────┐
              │  adjudicator   │◄── frontier ──► Anthropic
              └────────────────┘
                       │
                       ▼
              ┌────────────────┐
              │  Verdict JSON  │──► stdout / file / GitHub
              └────────────────┘
```

## Building from Source

```bash
git clone https://github.com/helloodokai/acig.git
cd acig
make build        # builds to dist/acig
make test         # runs tests
make lint         # golangci-lint
make release-snapshot  # goreleaser snapshot
```

Requires Go 1.22+, golangci-lint, and goreleaser.

## License

MIT