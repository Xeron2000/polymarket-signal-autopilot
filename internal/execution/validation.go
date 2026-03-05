package execution

import "sort"

type PermutationResult struct {
	Observed    float64
	PValue      float64
	QValue      float64
	Significant bool
}

func EvaluatePermutation(observed float64, placebo []float64, fdrQMax float64) PermutationResult {
	p := empiricalPValue(observed, placebo)
	q := p
	return PermutationResult{
		Observed:    observed,
		PValue:      p,
		QValue:      q,
		Significant: q < fdrQMax,
	}
}

func EvaluatePermutationSet(observed []float64, placebo [][]float64, fdrQMax float64) []PermutationResult {
	results := make([]PermutationResult, len(observed))
	pValues := make([]float64, len(observed))

	for i, metric := range observed {
		var permutations []float64
		if i < len(placebo) {
			permutations = placebo[i]
		}
		pValues[i] = empiricalPValue(metric, permutations)
	}

	qValues := benjaminiHochberg(pValues)
	for i, metric := range observed {
		results[i] = PermutationResult{
			Observed:    metric,
			PValue:      pValues[i],
			QValue:      qValues[i],
			Significant: qValues[i] < fdrQMax,
		}
	}

	return results
}

func empiricalPValue(observed float64, placebo []float64) float64 {
	if len(placebo) == 0 {
		return 1
	}

	extreme := 0
	for _, metric := range placebo {
		if metric >= observed {
			extreme++
		}
	}

	return float64(extreme+1) / float64(len(placebo)+1)
}

func benjaminiHochberg(pValues []float64) []float64 {
	if len(pValues) == 0 {
		return nil
	}

	type indexed struct {
		index int
		value float64
	}

	ordered := make([]indexed, len(pValues))
	for i, p := range pValues {
		ordered[i] = indexed{index: i, value: p}
	}
	sort.Slice(ordered, func(i, j int) bool {
		return ordered[i].value < ordered[j].value
	})

	m := float64(len(ordered))
	adjusted := make([]float64, len(ordered))
	for i := range ordered {
		rank := float64(i + 1)
		q := ordered[i].value * m / rank
		if q > 1 {
			q = 1
		}
		adjusted[i] = q
	}

	for i := len(adjusted) - 2; i >= 0; i-- {
		if adjusted[i] > adjusted[i+1] {
			adjusted[i] = adjusted[i+1]
		}
	}

	out := make([]float64, len(ordered))
	for i := range ordered {
		out[ordered[i].index] = adjusted[i]
	}
	return out
}
