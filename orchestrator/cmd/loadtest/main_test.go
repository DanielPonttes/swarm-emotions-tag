package main

import (
	"testing"
	"time"
)

func TestAverageLatencyMs(t *testing.T) {
	latencies := []time.Duration{
		500 * time.Microsecond,
		1500 * time.Microsecond,
		2500 * time.Microsecond,
	}

	got := averageLatencyMs(latencies)
	want := 1.5

	if got != want {
		t.Fatalf("averageLatencyMs() = %v, want %v", got, want)
	}
}

func TestPercentileLatencyMs(t *testing.T) {
	latencies := []time.Duration{
		1 * time.Millisecond,
		2 * time.Millisecond,
		3 * time.Millisecond,
		4 * time.Millisecond,
		5 * time.Millisecond,
	}

	if got := percentileLatencyMs(latencies, 0.95); got != 5 {
		t.Fatalf("percentileLatencyMs(..., 0.95) = %v, want 5", got)
	}
	if got := percentileLatencyMs(latencies, 0.99); got != 5 {
		t.Fatalf("percentileLatencyMs(..., 0.99) = %v, want 5", got)
	}
	if got := percentileLatencyMs(latencies, 0.50); got != 3 {
		t.Fatalf("percentileLatencyMs(..., 0.50) = %v, want 3", got)
	}
}
