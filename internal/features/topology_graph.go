package features

func ClusterConfirmationScore(series []MarketPoint) float64 {
	if len(series) < 2 {
		return 0
	}

	ups := 0
	downs := 0
	for i := 1; i < len(series); i++ {
		if series[i].Price > series[i-1].Price {
			ups++
		} else if series[i].Price < series[i-1].Price {
			downs++
		}
	}
	total := ups + downs
	if total == 0 {
		return 0
	}
	if ups > downs {
		return float64(ups) / float64(total)
	}
	return float64(downs) / float64(total)
}
