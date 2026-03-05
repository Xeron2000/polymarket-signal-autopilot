# Restart Procedure for HTTP 425 Bursts

## Symptom

Order/matching endpoints repeatedly return HTTP 425 (Too Early), typically around matching-engine windows.

## Procedure

1. Keep ingestion running; pause live order submissions.
2. Apply exponential backoff with cap (base 250ms, max 5s).
3. Resume after sustained healthy responses over 3 consecutive attempts.
4. Reconcile missed windows from raw ledger via replay/backfill.

## Exit Criteria

- 425 error rate returns below threshold
- Reconnect success SLO restored
- Replay checksum stable for affected period
