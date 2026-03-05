# Polymarket Lead-Lag Signal System Implementation Plan (v3)

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a production-safe Polymarket→News lead-lag signal system that is measurable, replayable, and risk-gated before any auto-execution.

**Architecture:** Start as a modular monolith with append-only raw event logging, deterministic replay, and strict Go/No-Go phase gates. Enforce timestamp discipline (`source_ts`, `ingest_ts`, `normalized_ts`) and causal validation before strategy optimization. Any live execution remains whitelist-only and fail-closed.

**Tech Stack:**
- Primary language: **Go (recommended)**
- Storage: PostgreSQL (+ Timescale extension optional), Parquet/DuckDB for research
- Messaging/runtime: native goroutines/channels + Redis (optional queue/cache)
- Observability: Prometheus + Grafana + OpenTelemetry
- Testing: Go test + integration/e2e harness

---

## 0. Scope and Non-Goals

### In Scope
- Official Polymarket APIs + WebSocket market ingestion
- Multi-source news ingestion with source reliability scoring
- Lead-lag measurement engine with dual-clock validation
- Signal generation with risk filters and contradiction suppression
- Backtest, walk-forward, placebo/permutation, paper trading
- Guarded live execution (optional rollout, mandatory safety controls)

### Out of Scope (v1)
- Cross-exchange high-frequency arbitrage (microsecond/HFT)
- Full market-making engine
- Multi-region active-active deployment

---

## 1. Global Acceptance Gates (Hard Go/No-Go)

### G1 — Data Quality (14-day rolling window)
- Required field missingness `<= 0.5%`
- p99 ingest latency `<= 3s`
- WebSocket reconnect success within 10s `>= 99%`
- Unresolved sequence gaps `= 0`

### G2 — Causal Robustness
- Lead-lag direction consistent across `source_ts` and `ingest_ts`
- Placebo/permutation remains significant after FDR correction (`q < 0.05`)
- Anti-lookahead test suite pass rate `= 100%`

### G3 — Economic Viability (OOS)
- Net expectancy after fees/slippage `> 0`
- Max drawdown below configured limit (default `< 8%`)
- Capacity degradation at target notional within tolerance (default `< 20%` edge decay)

### G4 — Operational Safety (paper trial)
- 30-day paper trading with zero critical risk-limit breach
- Predicted vs realized slippage deviation within tolerance (default `<= 20%`)
- Kill-switch drill and restart runbook drill passed

---

## 2. Target Repository Structure

```text
polymarket-signal/
  cmd/api/main.go
  cmd/worker/main.go
  internal/
    config/
    ingestion/
    connectors/polymarket/
    connectors/news/
    linking/
    features/
    signals/
    execution/
    risk/
    ops/
  migrations/
  tests/{unit,integration,e2e}
  docs/{specs,runbooks,hypotheses}
```

---

## 3. Phase-by-Phase Plan

### Task 1: Foundation and Contracts (Phase 0)

**Files:**
- Create: `docs/specs/timestamp-contract.md`
- Create: `docs/specs/go-no-go-gates.md`
- Create: `docs/hypotheses/hypothesis-registry-template.md`
- Create: `internal/config/contract.go`
- Test: `tests/unit/config_contract_test.go`

**Step 1: Write failing tests**
- Validate required timestamp fields are enforced
- Validate gate config supports strict numeric thresholds

**Step 2: Run tests to confirm failure**
- Run: `go test ./tests/unit -run Contract -v`
- Expected: FAIL due to missing contract implementation

**Step 3: Implement minimal contract objects**
- Add typed config structs for gates and timestamp requirements

**Step 4: Re-run tests**
- Expected: PASS

**Deliverables:**
- Contract spec + typed validation baseline

---

### Task 2: Polymarket Ingestion and Raw Event Ledger (Phase 1)

**Files:**
- Create: `internal/connectors/polymarket/gamma_client.go`
- Create: `internal/connectors/polymarket/clob_client.go`
- Create: `internal/connectors/polymarket/ws_market_client.go`
- Create: `internal/ingestion/raw_writer.go`
- Create: `migrations/001_raw_events.sql`
- Test: `tests/integration/polymarket_ingestion_test.go`

**Step 1: Failing tests**
- Verify all ingested events persist immutable raw payload + timestamp trio
- Verify duplicate event IDs are idempotent

**Step 2: Run tests (expect fail)**
- `go test ./tests/integration -run Ingestion -v`

**Step 3: Implement minimal ingestion**
- REST fetch for markets/events
- WebSocket subscription with heartbeat + reconnect
- 425-aware retry policy for matching engine windows

**Step 4: Run tests (expect pass)**

**Deliverables:**
- Working ingestion path + append-only raw store

---

### Task 3: Gap Recovery, Replay, and Determinism (Phase 1)

**Files:**
- Create: `internal/ingestion/gap_detector.go`
- Create: `internal/ingestion/backfill.go`
- Create: `internal/ops/replay.go`
- Create: `docs/runbooks/replay-backfill.md`
- Test: `tests/e2e/gap_recovery_replay_test.go`

**Steps:**
1. Write failing test for sequence gap simulation
2. Implement REST backfill and gap closure markers
3. Add deterministic replay mode
4. Re-run e2e tests

**Acceptance:**
- Same input log produces identical linked/signal outputs

---

### Task 4: News Ingestion and Source Reliability Layer (Phase 1/2)

**Files:**
- Create: `internal/connectors/news/base.go`
- Create: `internal/connectors/news/router.go`
- Create: `internal/connectors/news/reliability.go`
- Create: `migrations/002_news_events.sql`
- Test: `tests/integration/news_ingestion_test.go`

**Requirements:**
- Normalize source fields and timezone
- Keep `first_seen_ingest_ts` independent from publisher-declared time
- Assign reliability tier and preserve provenance

---

### Task 5: Entity Linking and Lead-Lag Engine (Phase 2)

**Files:**
- Create: `internal/linking/entity_linker.go`
- Create: `internal/linking/first_seen_aggregator.go`
- Create: `internal/linking/lead_lag.go`
- Create: `migrations/003_linked_events.sql`
- Test: `tests/integration/lead_lag_dual_clock_test.go`

**Steps:**
1. Build event/market/news linking graph
2. Compute dual-clock deltas (`delta_source`, `delta_ingest`)
3. Add confidence scoring to each link
4. Validate consistency thresholds and false-link controls

**Acceptance:**
- Link precision target met on labeled sample
- Dual-clock direction consistency reported per category

---

### Task 6: Feature and Signal Layer (Phase 3)

**Files:**
- Create: `internal/features/builder.go`
- Create: `internal/features/topology_graph.go`
- Create: `internal/features/contradiction_index.go`
- Create: `internal/signals/rules.go`
- Create: `internal/risk/filters.go`
- Test: `tests/unit/feature_no_lookahead_test.go`
- Test: `tests/unit/signal_filters_test.go`

**Feature set (minimum):**
- Price magnitude/velocity/acceleration
- Spread/depth/volume stress
- Novelty score
- Contradiction index
- Cluster confirmation

**Acceptance:**
- No-lookahead test hard-fails on future data usage
- Signals always emit reason codes for auditability

---

### Task 7: Backtest + Statistical Validation + Paper Trading (Phase 4)

**Files:**
- Create: `internal/execution/backtest.go`
- Create: `internal/execution/validation.go`
- Create: `internal/execution/paper_router.go`
- Test: `tests/e2e/backtest_cost_model_test.go`
- Test: `tests/e2e/placebo_permutation_test.go`
- Test: `tests/e2e/paper_risk_limit_test.go`

**Validation requirements:**
- Walk-forward splits (predefined)
- Placebo/permutation tests
- FDR multiple-comparison correction
- Slippage/depth/capacity-aware PnL

**Acceptance:**
- G2 and G3 pass with explicit reports

---

### Task 8: Guarded Live Execution (Phase 5, only after G1-G4)

**Files:**
- Create: `internal/execution/live_router.go`
- Create: `internal/execution/kill_switch.go`
- Create: `docs/runbooks/incident-response.md`
- Create: `docs/runbooks/restart-425.md`
- Test: `tests/e2e/live_guardrails_test.go`

**Mandatory controls:**
- Whitelist markets only
- Per-market and per-cluster exposure caps
- Daily loss cap and stale-data cutoff
- Idempotent order keys
- Geoblock and compliance checks before order placement

**Acceptance:**
- Dry-run and paper/live parity checks passed
- Kill-switch and restart drills passed

---

## 4. Recommended Timeline

- Phase 0: 1-2 days
- Phase 1: 3-5 days
- Phase 2: 2-4 days
- Phase 3: 3-5 days
- Phase 4: 5-10 days
- Phase 5: 3-7 days (only if all gates pass)

Total: ~17-33 days depending on data quality and validation outcomes.

---

## 5. Operational KPIs to Report Daily

- Ingestion completeness rate
- p95/p99 stage latency
- Gap recovery count and mean closure time
- Lead-lag sample count per category
- Signal pass/block ratio by risk rule
- Paper PnL, drawdown, slippage drift
- Risk-limit breach count

---

## 6. Stop Conditions (Immediate Rollback)

- Any critical risk-limit breach
- Data staleness beyond configured threshold
- Replay non-determinism detected
- Gate regression below minimum thresholds

If triggered: disable live route, continue ingestion-only mode, run incident runbook.
