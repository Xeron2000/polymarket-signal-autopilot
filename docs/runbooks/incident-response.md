# Incident Response Runbook

## Trigger Conditions

- Critical risk-limit breach
- Replay non-determinism
- Sustained stale data feed
- Unexpected live routing behavior

## Immediate Actions

1. Trigger kill switch (`manual_incident`).
2. Confirm all live submissions are blocked.
3. Keep ingestion path running for forensics.
4. Capture: offending order keys, market IDs, raw event IDs, replay checksums.

## Recovery

1. Identify root cause and validate fix in paper mode.
2. Run kill-switch drill again.
3. Re-enable live route only after sign-off.
