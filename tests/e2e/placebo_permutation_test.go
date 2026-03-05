package e2e

import (
	"math"
	"testing"

	"polymarket-signal/internal/execution"
)

func TestPlaceboPermutationSignificance(t *testing.T) {
	observed := 2.1
	placebo := make([]float64, 100)
	for i := range placebo {
		placebo[i] = 0.3 + float64(i%5)*0.01
	}

	result := execution.EvaluatePermutation(observed, placebo, 0.05)
	if !result.Significant {
		t.Fatalf("expected observed metric to be significant, got q=%.6f", result.QValue)
	}
	if result.QValue >= 0.05 {
		t.Fatalf("expected q-value below threshold, got %.6f", result.QValue)
	}
}

func TestPermutationSetAppliesBHCorrection(t *testing.T) {
	observed := []float64{2.1, 0.9}
	placebo := [][]float64{
		make([]float64, 100),
		make([]float64, 100),
	}

	for i := 0; i < 100; i++ {
		placebo[0][i] = 0.2 + float64(i%3)*0.01
		if i < 40 {
			placebo[1][i] = 1.0
		} else {
			placebo[1][i] = 0.2
		}
	}

	results := execution.EvaluatePermutationSet(observed, placebo, 0.05)
	if len(results) != 2 {
		t.Fatalf("expected 2 permutation set results, got %d", len(results))
	}
	if !results[0].Significant {
		t.Fatalf("expected first hypothesis significant, got q=%.6f", results[0].QValue)
	}
	if results[1].Significant {
		t.Fatalf("expected second hypothesis non-significant, got q=%.6f", results[1].QValue)
	}
	if results[0].QValue >= results[1].QValue {
		t.Fatalf("expected first q-value to be smaller than second, got %.6f >= %.6f", results[0].QValue, results[1].QValue)
	}
	if math.IsNaN(results[0].QValue) || math.IsNaN(results[1].QValue) {
		t.Fatal("expected finite BH-corrected q-values")
	}
}
