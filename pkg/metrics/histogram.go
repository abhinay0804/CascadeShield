package metrics

import (
	"math"
	"sync"
)

// Histogram is an HDR (High Dynamic Range) inspired log-linear histogram
// for computing latency percentiles in O(1) time.
//
// Concept for beginners:
//   - A histogram divides a value range into "buckets" and counts how many
//     measurements fall in each bucket.
//   - Instead of equal-width buckets (which waste resolution on large values),
//     HDR uses log-scale buckets: each successive bucket covers a range
//     roughly 2x wider than the previous one.
//   - This gives high precision for small values (sub-ms latencies) while
//     still tracking large values (multi-second connections) efficiently.
//
// Implementation:
//   - We use sub-buckets within each power-of-two "major" bucket for
//     configurable significant figures (precision).
//   - P50/P95/P99 are computed by scanning the cumulative count.
type Histogram struct {
	mu sync.Mutex

	// counts[i] is the number of observations that fell in bucket i.
	counts []int64

	// totalCount is the total number of recorded values.
	totalCount int64

	// The bucket boundaries are derived from minMs, maxMs, and sigFigs.
	// bucketBoundaries[i] is the upper bound (inclusive) of bucket i, in nanoseconds.
	bucketBoundaries []int64

	minNs int64
	maxNs int64
}

// NewHistogram creates a log-linear histogram.
//
// Parameters:
//   - minMs: minimum trackable value in milliseconds (values below are clamped)
//   - maxMs: maximum trackable value in milliseconds (values above are clamped)
//   - sigFigs: number of significant figures (precision); 3 means ~0.1% relative error
func NewHistogram(minMs, maxMs int64, sigFigs int) *Histogram {
	if minMs < 0 {
		minMs = 0
	}
	if maxMs <= minMs {
		maxMs = minMs + 1
	}
	if sigFigs < 1 {
		sigFigs = 1
	}
	if sigFigs > 5 {
		sigFigs = 5
	}

	minNs := minMs * 1_000_000
	maxNs := maxMs * 1_000_000

	// Build boundaries: start from minNs, double-and-subdivide to maxNs.
	// Within each power-of-two range, we have 10^sigFigs sub-buckets.
	subBuckets := int64(math.Pow10(sigFigs))

	var boundaries []int64
	value := int64(1) // start from 1ns to avoid log(0)
	if minNs > 1 {
		value = minNs
	}
	for value <= maxNs {
		boundaries = append(boundaries, value)
		// Each step: move to the next sub-bucket
		// Within a power-of-two range [2^k, 2^(k+1)), divide into subBuckets steps
		step := max64(1, value/subBuckets)
		value += step
	}
	boundaries = append(boundaries, maxNs+1) // sentinel upper bound

	return &Histogram{
		counts:           make([]int64, len(boundaries)),
		bucketBoundaries: boundaries,
		minNs:            minNs,
		maxNs:            maxNs,
	}
}

// RecordNs records a single latency observation in nanoseconds.
func (h *Histogram) RecordNs(ns int64) {
	if ns < h.minNs {
		ns = h.minNs
	}
	if ns > h.maxNs {
		ns = h.maxNs
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	idx := h.bucketIndex(ns)
	h.counts[idx]++
	h.totalCount++
}

// Percentile returns the value at the given percentile (0.0–100.0).
// Returns 0 if no observations have been recorded.
func (h *Histogram) Percentile(p float64) float64 {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.totalCount == 0 {
		return 0
	}
	if p <= 0 {
		return float64(h.bucketBoundaries[0]) / 1_000_000.0
	}
	if p >= 100 {
		p = 100
	}

	// Target rank in the sorted list of all observations
	target := int64(math.Ceil(float64(h.totalCount) * p / 100.0))

	cumulative := int64(0)
	for i, count := range h.counts {
		cumulative += count
		if cumulative >= target {
			// Return the upper bound of this bucket converted to ms
			boundNs := h.bucketBoundaries[i]
			return float64(boundNs) / 1_000_000.0
		}
	}
	return float64(h.maxNs) / 1_000_000.0
}

// Reset clears all observation counts. Used when rotating per-window histograms.
func (h *Histogram) Reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := range h.counts {
		h.counts[i] = 0
	}
	h.totalCount = 0
}

// Count returns the total number of recorded observations.
func (h *Histogram) Count() int64 {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.totalCount
}

// bucketIndex finds the correct bucket index for a given nanosecond value
// using binary search over bucket boundaries. Must be called with mu held.
func (h *Histogram) bucketIndex(ns int64) int {
	lo, hi := 0, len(h.bucketBoundaries)-1
	for lo < hi {
		mid := (lo + hi) / 2
		if h.bucketBoundaries[mid] < ns {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo
}

// max64 returns the larger of two int64 values.
func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
