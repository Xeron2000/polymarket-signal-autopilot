# Replay and Backfill Runbook

## Objective

Recover sequence gaps and verify deterministic replay from append-only raw events.

## Procedure

1. Detect missing sequences from raw stream (`DetectSequenceGaps`).
2. Fill gaps with synthetic closure markers (`FillGaps`) and persist to raw ledger.
3. Execute deterministic replay (`ReplayDeterministic`) on full event set.
4. Compare replay checksum with prior baseline for drift detection.

## Escalation

- If replay checksum drifts for identical input set, stop live routing and switch to ingestion-only mode.
- Open incident and attach raw event IDs used for checksum generation.
