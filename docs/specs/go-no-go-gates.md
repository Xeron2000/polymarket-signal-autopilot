# Go/No-Go Gates

## G1 Data Quality

- Missing required field rate `<= 0.5%`
- p99 ingest latency `<= 3000ms`
- WebSocket reconnect success within 10s `>= 99%`
- Unresolved sequence gaps `= 0`

## G2 Causal Robustness

- Lead-lag direction consistent across source and ingest clocks
- Placebo/permutation significance with FDR correction `q < 0.05`
- Anti-lookahead test pass rate `= 100%`

## G3 Economic Viability

- OOS net expectancy (after fee/slippage) `> 0`
- Max drawdown `< 8%`
- Capacity degradation `< 20%`

## G4 Operational Safety

- 30-day paper trading with zero critical risk breaches
- Predicted vs realized slippage deviation `<= 20%`
- Kill-switch and restart drills passed
