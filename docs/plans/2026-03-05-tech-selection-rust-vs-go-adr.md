# ADR: Rust vs Go for Polymarket Lead-Lag System

## Status
Accepted (initial decision)

## Date
2026-03-05

## Context
We are building a greenfield Polymarket→News lead-lag signal platform with:
- WebSocket + REST ingestion
- timestamp normalization and causal validation
- backtest + paper trading + guarded live execution
- strict replayability and operational safety

This is a low-ms to sub-second decisioning system, not microsecond colocation HFT.

## Options

### Option A — Rust as primary language
**Pros**
- Excellent memory safety and low-level performance
- Tight latency control and predictable runtime behavior

**Cons**
- Higher development complexity and slower onboarding for most teams
- Async/concurrency ergonomics increase implementation time
- Longer iteration loops for research-heavy strategy stages

### Option B — Go as primary language
**Pros**
- Fast delivery for concurrent I/O pipelines (WSS + REST + workers)
- Simpler concurrency model and stronger team productivity
- Mature operational ergonomics (single binary, profiling, deploy simplicity)

**Cons**
- GC/alloc behavior can affect tail latency if not profiled
- Absolute performance ceiling below hand-optimized Rust in some hotspots

### Option C — Hybrid (Go orchestration + Rust hotspot modules)
**Pros**
- Keeps delivery speed while allowing targeted optimization
- Avoids premature optimization and language-wide complexity

**Cons**
- Additional interface/operational complexity
- Requires clean boundaries and stable contracts

## Decision
Choose **Go as primary implementation language** for v1.

Use a **metrics-gated hybrid escalation**:
- Keep ingestion, orchestration, risk controls, and ops in Go.
- Extract only proven hotspots to Rust if SLOs are not met after Go-level optimization.

## Why
1. Primary bottlenecks are likely data quality, external I/O jitter, and execution/risk logic—not language runtime in early phases.
2. System value depends on fast iteration of hypotheses, validation, and risk controls; Go improves cycle time.
3. The architecture needs replayability and safety more than micro-optimizing compute paths at start.

## Escalation Triggers to Rust
Migrate a component to Rust only if all are true:
1. p99 decision latency repeatedly violates SLO after profiling and Go optimization.
2. Hotspot concentration is clear (single stage dominates CPU/latency).
3. Expected gain exceeds migration/maintenance cost.

Candidate extraction targets:
- signal scoring core
- execution pre-trade check core

## Guardrails
- No full rewrite to Rust without benchmark-backed RFC.
- Preserve deterministic replay contract across language boundaries.
- Keep process boundaries explicit (versioned API/proto contracts).

## Consequences
- Faster initial delivery and safer rollout cadence.
- Clear path for targeted performance upgrades without architecture reset.
