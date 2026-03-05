# Timestamp Contract

All normalized events MUST contain all three timestamps:

- `source_ts`: event timestamp from upstream source payload.
- `ingest_ts`: timestamp when this system receives the event.
- `normalized_ts`: timestamp after normalization/validation.

Validation rule: records missing any timestamp are rejected from downstream feature/signal pipelines.
