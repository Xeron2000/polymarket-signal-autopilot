# Polymarket Signal Bootstrap Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Bootstrap a runnable Go-based Polymarket→News lead-lag signal system with deterministic replay and safety-guarded execution, implemented with strict TDD.

**Architecture:** Build a modular monolith with package boundaries aligned to ingestion, linking, signals, execution, risk, and ops. Use typed domain models and in-memory-first adapters so integration/e2e tests are deterministic and fast. Add SQL migrations and runbooks/specs as production contracts.

**Tech Stack:** Go 1.22+, standard library, goroutines/channels, PostgreSQL migration SQL artifacts, Go test.

---

### Task 1: Foundation and Contracts

**Files:**
- Create: `go.mod`
- Create: `internal/config/contract.go`
- Create: `docs/specs/timestamp-contract.md`
- Create: `docs/specs/go-no-go-gates.md`
- Create: `docs/hypotheses/hypothesis-registry-template.md`
- Test: `tests/unit/config_contract_test.go`

**Step 1: Write failing test**
- Add tests validating timestamp field requirements and gate threshold validation.

**Step 2: Verify RED**
- Run: `go test ./tests/unit -run Contract -v`
- Expected: FAIL (missing config contract implementation).

**Step 3: Minimal implementation**
- Add typed structs + `Validate()` methods for timestamp and gate contracts.

**Step 4: Verify GREEN**
- Run: `go test ./tests/unit -run Contract -v`
- Expected: PASS.

---

### Task 2: Polymarket Ingestion and Raw Ledger

**Files:**
- Create: `internal/connectors/polymarket/{gamma_client.go,clob_client.go,ws_market_client.go}`
- Create: `internal/ingestion/raw_writer.go`
- Create: `migrations/001_raw_events.sql`
- Test: `tests/integration/polymarket_ingestion_test.go`

**TDD Steps:**
1. Write failing tests for immutable raw payload persistence + idempotent event IDs.
2. Run `go test ./tests/integration -run Ingestion -v` and confirm fail.
3. Implement minimal in-memory writer + connector interfaces + reconnect policy primitives.
4. Re-run test until pass.

---

### Task 3: Gap Recovery and Deterministic Replay

**Files:**
- Create: `internal/ingestion/{gap_detector.go,backfill.go}`
- Create: `internal/ops/replay.go`
- Create: `docs/runbooks/replay-backfill.md`
- Test: `tests/e2e/gap_recovery_replay_test.go`

**TDD Steps:**
1. Failing test for sequence gap detection/backfill closure.
2. Failing test for deterministic replay output equality.
3. Minimal implementation.
4. Re-run e2e tests to PASS.

---

### Task 4: News Ingestion and Reliability

**Files:**
- Create: `internal/connectors/news/{base.go,router.go,reliability.go}`
- Create: `migrations/002_news_events.sql`
- Test: `tests/integration/news_ingestion_test.go`

**TDD Steps:**
1. Failing tests for normalization, independent first-seen ingest time, provenance and reliability tier.
2. Implement minimal router/reliability scoring.
3. Verify integration tests pass.

---

### Task 5: Linking and Lead-Lag Engine

**Files:**
- Create: `internal/linking/{entity_linker.go,first_seen_aggregator.go,lead_lag.go}`
- Create: `migrations/003_linked_events.sql`
- Test: `tests/integration/lead_lag_dual_clock_test.go`

**TDD Steps:**
1. Failing tests for entity linking + dual-clock deltas + confidence bounds.
2. Implement minimal deterministic linker and delta calculator.
3. Verify PASS and direction consistency checks.

---

### Task 6: Features, Signals, Risk Filters

**Files:**
- Create: `internal/features/{builder.go,topology_graph.go,contradiction_index.go}`
- Create: `internal/signals/rules.go`
- Create: `internal/risk/filters.go`
- Test: `tests/unit/feature_no_lookahead_test.go`
- Test: `tests/unit/signal_filters_test.go`

**TDD Steps:**
1. Failing tests for no-lookahead and signal reason code requirements.
2. Implement feature builder and signal evaluator.
3. Re-run tests to PASS.

---

### Task 7: Backtest, Validation, Paper Trading

**Files:**
- Create: `internal/execution/{backtest.go,validation.go,paper_router.go}`
- Test: `tests/e2e/{backtest_cost_model_test.go,placebo_permutation_test.go,paper_risk_limit_test.go}`

**TDD Steps:**
1. Failing tests for cost-adjusted expectancy, permutation significance output, risk-limit enforcement in paper router.
2. Minimal implementation and PASS.

---

### Task 8: Guarded Live Execution

**Files:**
- Create: `internal/execution/{live_router.go,kill_switch.go}`
- Create: `docs/runbooks/{incident-response.md,restart-425.md}`
- Test: `tests/e2e/live_guardrails_test.go`

**TDD Steps:**
1. Failing tests for whitelist, exposure caps, stale-data cutoff, idempotent order key, kill switch.
2. Minimal implementation and PASS.

---

### Final Verification

1. `go test ./... -v`
2. `go test -race ./...`
3. `go vet ./...`
4. Ensure docs and migrations present and consistent with package contracts.
