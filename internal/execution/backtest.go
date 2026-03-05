package execution

type TradeOutcome struct {
	GrossReturnPct float64
}

type ExpectancyReport struct {
	TradesCount        int
	CostPerTradePct    float64
	GrossExpectancyPct float64
	NetExpectancyPct   float64
}

func ComputeExpectancy(trades []TradeOutcome, feeBps float64, slippageBps float64) ExpectancyReport {
	if len(trades) == 0 {
		return ExpectancyReport{}
	}

	costPerTrade := (feeBps + slippageBps) / 10000
	grossSum := 0.0
	netSum := 0.0
	for _, trade := range trades {
		grossSum += trade.GrossReturnPct
		netSum += trade.GrossReturnPct - costPerTrade
	}

	count := float64(len(trades))
	return ExpectancyReport{
		TradesCount:        len(trades),
		CostPerTradePct:    costPerTrade,
		GrossExpectancyPct: grossSum / count,
		NetExpectancyPct:   netSum / count,
	}
}
