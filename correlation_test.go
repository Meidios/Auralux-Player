//go:build windows

package main

import (
	"math"
	"testing"
)

func TestCorrelationAccumulatorCanonicalSignals(t *testing.T) {
	tests := []struct {
		name         string
		sumL2, sumR2 float64
		sumLR        float64
		want         float32
	}{
		{name: "in phase", sumL2: 100, sumR2: 100, sumLR: 100, want: 1},
		{name: "anti phase", sumL2: 100, sumR2: 100, sumLR: -100, want: -1},
		{name: "uncorrelated", sumL2: 100, sumR2: 100, sumLR: 0, want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var accumulator correlationAccumulator
			got, valid := accumulator.add(test.sumL2, test.sumR2, test.sumLR, 100, 1000)
			if !valid {
				t.Fatal("expected a valid correlation")
			}
			if math.Abs(float64(got-test.want)) > 1e-6 {
				t.Fatalf("got %f, want %f", got, test.want)
			}
		})
	}
}

func TestCorrelationAccumulatorWeightsBuffersByEnergy(t *testing.T) {
	var accumulator correlationAccumulator
	if _, valid := accumulator.add(100, 100, 100, 100, 1000); !valid {
		t.Fatal("expected loud buffer to be valid")
	}
	got, valid := accumulator.add(0.01, 0.01, -0.01, 100, 1000)
	if !valid {
		t.Fatal("expected combined window to be valid")
	}
	want := float32(99.99 / 100.01)
	if math.Abs(float64(got-want)) > 1e-6 {
		t.Fatalf("got %f, want energy-weighted %f", got, want)
	}
}

func TestCorrelationAccumulatorUsesFixedTimeWindow(t *testing.T) {
	var accumulator correlationAccumulator
	if got, valid := accumulator.add(300, 300, 300, 300, 1000); !valid || got != 1 {
		t.Fatalf("initial correlation = %f, valid=%v", got, valid)
	}
	if got, valid := accumulator.add(300, 300, -300, 300, 1000); !valid || got != -1 {
		t.Fatalf("expired-window correlation = %f, valid=%v", got, valid)
	}
}

func TestCorrelationAccumulatorPartiallyTrimsOldestBuffer(t *testing.T) {
	var accumulator correlationAccumulator
	if _, valid := accumulator.add(200, 200, 200, 200, 1000); !valid {
		t.Fatal("expected first buffer to be valid")
	}
	got, valid := accumulator.add(200, 200, -200, 200, 1000)
	if !valid {
		t.Fatal("expected rolling window to be valid")
	}
	want := float32(-1.0 / 3.0) // 100 in-phase frames + 200 anti-phase frames.
	if math.Abs(float64(got-want)) > 1e-6 {
		t.Fatalf("got %f, want %f after partial trim", got, want)
	}
}

func TestCorrelationAccumulatorSilenceExpiresMeasurement(t *testing.T) {
	var accumulator correlationAccumulator
	if _, valid := accumulator.add(300, 300, 300, 300, 1000); !valid {
		t.Fatal("expected signal to be valid")
	}
	if got, valid := accumulator.add(0, 0, 0, 300, 1000); valid || got != 0 {
		t.Fatalf("silence should invalidate the old measurement: got %f, valid=%v", got, valid)
	}
}

func TestCorrelationAccumulatorSingleSilentChannelIsInvalid(t *testing.T) {
	var accumulator correlationAccumulator
	if got, valid := accumulator.add(100, 0, 0, 100, 1000); valid || got != 0 {
		t.Fatalf("single-channel signal should not produce correlation: got %f, valid=%v", got, valid)
	}
}

func TestCorrelationAccumulatorPreservesWindowWhenBucketRingCompacts(t *testing.T) {
	var accumulator correlationAccumulator
	for i := 0; i < correlationBucketCount+20; i++ {
		if got, valid := accumulator.add(1, 1, 1, 1, 10000); !valid || got != 1 {
			t.Fatalf("buffer %d correlation = %f, valid=%v", i, got, valid)
		}
	}
	if accumulator.frames != correlationBucketCount+20 {
		t.Fatalf("compaction shortened the window: got %.0f frames", accumulator.frames)
	}
}
