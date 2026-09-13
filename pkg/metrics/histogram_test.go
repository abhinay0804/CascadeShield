package metrics

import (
	"math"
	"testing"
)

func TestHistogram_Percentiles(t *testing.T) {
	h := NewHistogram(0, 10000, 3) // 0–10000ms, 3 sig figs

	// Record 100 values: 1ms, 2ms, ..., 100ms
	for ms := int64(1); ms <= 100; ms++ {
		h.RecordNs(ms * 1_000_000)
	}

	p50 := h.Percentile(50)
	p95 := h.Percentile(95)
	p99 := h.Percentile(99)

	t.Logf("P50=%.2fms P95=%.2fms P99=%.2fms", p50, p95, p99)

	// With 100 values 1..100ms:
	// P50 should be ~50ms (±5% = ±2.5ms)
	// P95 should be ~95ms (±5% = ±4.75ms)
	// P99 should be ~99ms (±5% = ±4.95ms)
	if math.Abs(p50-50) > 5 {
		t.Errorf("P50 %.2fms too far from 50ms (expected within 5ms)", p50)
	}
	if math.Abs(p95-95) > 5 {
		t.Errorf("P95 %.2fms too far from 95ms (expected within 5ms)", p95)
	}
	if math.Abs(p99-99) > 5 {
		t.Errorf("P99 %.2fms too far from 99ms (expected within 5ms)", p99)
	}
}

func TestHistogram_EmptyReturnsZero(t *testing.T) {
	h := NewHistogram(0, 10000, 3)
	if h.Percentile(99) != 0 {
		t.Error("expected 0 for empty histogram")
	}
	if h.Count() != 0 {
		t.Error("expected 0 count for empty histogram")
	}
}

func TestHistogram_Clamp(t *testing.T) {
	h := NewHistogram(0, 100, 3) // max = 100ms

	// Record a value beyond the max — should be clamped
	h.RecordNs(500 * 1_000_000) // 500ms > 100ms max
	h.RecordNs(1 * 1_000_000)   // 1ms

	if h.Count() != 2 {
		t.Errorf("expected 2 observations, got %d", h.Count())
	}
	// P100 should be max (100ms)
	p100 := h.Percentile(100)
	if p100 > 105 { // allow 5ms tolerance
		t.Errorf("P100 %.2fms exceeded max bound significantly", p100)
	}
}

func TestHistogram_Reset(t *testing.T) {
	h := NewHistogram(0, 10000, 3)
	for i := 0; i < 100; i++ {
		h.RecordNs(int64(i) * 1_000_000)
	}
	if h.Count() == 0 {
		t.Fatal("expected non-zero count before reset")
	}
	h.Reset()
	if h.Count() != 0 {
		t.Errorf("expected 0 count after reset, got %d", h.Count())
	}
	if h.Percentile(99) != 0 {
		t.Errorf("expected 0 percentile after reset")
	}
}

func TestHistogram_SingleValue(t *testing.T) {
	h := NewHistogram(0, 10000, 3)
	h.RecordNs(50 * 1_000_000) // 50ms

	// Every percentile should return approximately 50ms
	for _, p := range []float64{1, 50, 95, 99, 100} {
		v := h.Percentile(p)
		if math.Abs(v-50) > 5 {
			t.Errorf("P%.0f = %.2fms, expected ~50ms for single observation", p, v)
		}
	}
}
