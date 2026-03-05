# Production Persistence + Private Trading + TG Autopilot Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Upgrade the project from in-memory pipeline to production-ready persistence, private signed CLOB execution, and Telegram-notified lead-lag autopilot.

**Architecture:** Keep modular monolith boundaries. Add DB-backed sink/repository for durable raw/signal/order/notification events, add private signed trading client for `/order` family, and add autopilot runtime that converts Polymarket rank-shift signals into notifications and (optionally) live order submissions through risk-gated execution.

**Tech Stack:** Go 1.22, `database/sql` + `pgx/v5/stdlib`, Gorilla WebSocket, Telegram Bot API over HTTPS.

---

### Task 1: Private Signed CLOB Client (TDD)

**Files:**
- Create: `tests/unit/polymarket_private_client_test.go`
- Create: `internal/connectors/polymarket/private_client.go`

**Steps:**
1. Write failing tests for HMAC signature parity with official client and mandatory POLY_* headers.
2. Run targeted tests to confirm RED.
3. Implement minimal `BuildPolyHMACSignature`, `PrivateCLOBClient.PostOrder/GetOrder/CancelOrder`.
4. Run tests to verify GREEN.

### Task 2: Database Persistence (TDD)

**Files:**
- Create: `tests/unit/persistence_store_test.go`
- Create: `internal/persistence/store.go`
- Create: `migrations/004_execution_pipeline.sql`

**Steps:**
1. Write failing tests for idempotent raw event insert + signal/order/notification persistence.
2. Run targeted tests to confirm RED.
3. Implement SQL store with `ON CONFLICT DO NOTHING` for raw events and append-only tables for signal/order/notification.
4. Run tests to verify GREEN.

### Task 3: Strategy + Telegram Notifier (TDD)

**Files:**
- Create: `tests/unit/telegram_notifier_test.go`
- Create: `tests/unit/rank_shift_strategy_test.go`
- Create: `internal/notify/telegram.go`
- Create: `internal/strategy/rank_shift.go`

**Steps:**
1. Write failing tests for Telegram sendMessage success + retry_after handling.
2. Write failing tests for rank-shift signal emission and dedup window.
3. Implement notifier and strategy with minimal robust behavior.
4. Run tests to verify GREEN.

### Task 4: Autopilot Execution Chain (TDD)

**Files:**
- Create: `tests/integration/autopilot_pipeline_test.go`
- Create: `internal/runtime/autopilot.go`
- Modify: `cmd/worker/main.go`

**Steps:**
1. Write failing integration test for stream update -> persist raw -> generate signal -> notify -> execute private order.
2. Implement autopilot engine with risk-gated execution path and storage hooks.
3. Upgrade worker entrypoint to initialize DB store, strategy, notifier, private client from env.
4. Run tests to verify GREEN.

### Task 5: API + Docs + Verification

**Files:**
- Modify: `cmd/api/main.go`
- Modify: `docs/runbooks/real-data-quickstart.md`

**Steps:**
1. Add private execution endpoint surface in API where needed.
2. Document env vars and E2E runbook for arbitrage signal + execution + TG notify.
3. Run full verification: `go build ./...`, `go test ./... -v`, `go test -race ./...`, `go vet ./...`.
