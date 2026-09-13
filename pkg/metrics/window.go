package metrics

import (
	"sync"
	"time"
)

// Bucket holds aggregated counters for a single tumbling time slice.
//
// For beginners: think of each bucket as a 5-second "time slot" on a clock.
// The window keeps 12 of these slots (= 60 seconds total). Every 5 seconds,
// the oldest slot is discarded and a fresh empty slot is opened.
type Bucket struct {
	// StartTime is when this bucket's time slice begins.
	StartTime time.Time

	// ConnectionCount is the number of CONNECT events in this bucket.
	ConnectionCount int64

	// TotalDurationNs is the sum of tcp_close.duration_ns values in this bucket.
	// Used to compute average (and with the histogram, percentile) latency.
	TotalDurationNs int64

	// TotalBytesSent is the sum of bytes_sent from tcp_close events.
	TotalBytesSent int64

	// TotalBytesRecv is the sum of bytes_received from tcp_close events.
	TotalBytesRecv int64

	// RetransmitCount is the number of RETRANSMIT events in this bucket.
	RetransmitCount int64

	// CloseCount is the number of CLOSE events (completed connections).
	CloseCount int64
}

// Window is a generic tumbling-bucket sliding window.
//
// It maintains a ring of fixed-size buckets. Each bucket covers a time slice
// of duration (WindowDuration / BucketCount). When the current time advances
// past the current bucket's end time, the oldest bucket is evicted and a new
// empty one is opened — this is the "slide" operation.
//
// Go Pattern: sync.Mutex for thread safety (multiple goroutines may record
// events or read aggregates concurrently).
type Window struct {
	mu             sync.Mutex
	buckets        []Bucket
	bucketCount    int
	slideInterval  time.Duration
	windowDuration time.Duration
	currentIdx     int // index of the bucket currently being written to
}

// NewWindow creates a sliding window with the given number of buckets and total duration.
func NewWindow(bucketCount int, windowDuration time.Duration) *Window {
	slideInterval := windowDuration / time.Duration(bucketCount)
	now := time.Now()
	buckets := make([]Bucket, bucketCount)
	for i := range buckets {
		buckets[i].StartTime = now.Add(-windowDuration + time.Duration(i)*slideInterval)
	}
	return &Window{
		buckets:        buckets,
		bucketCount:    bucketCount,
		slideInterval:  slideInterval,
		windowDuration: windowDuration,
		currentIdx:     bucketCount - 1,
	}
}

// advance evicts expired buckets and opens fresh ones up to current time.
// Must be called with mu held.
func (w *Window) advance(now time.Time) {
	currentBucket := &w.buckets[w.currentIdx]
	nextSliceEnd := currentBucket.StartTime.Add(w.slideInterval)

	// Loop: as long as the current slice has expired, evict it and open a new one
	for now.After(nextSliceEnd) {
		// Move to next index (ring buffer: wrap around)
		w.currentIdx = (w.currentIdx + 1) % w.bucketCount
		// Reset the overwritten bucket
		w.buckets[w.currentIdx] = Bucket{
			StartTime: nextSliceEnd,
		}
		nextSliceEnd = nextSliceEnd.Add(w.slideInterval)
	}
}

// RecordConnect records one new TCP connection (CONNECT event).
func (w *Window) RecordConnect() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.advance(time.Now())
	w.buckets[w.currentIdx].ConnectionCount++
}

// RecordClose records a completed connection (CLOSE event) with its metrics.
func (w *Window) RecordClose(durationNs, bytesSent, bytesRecv int64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.advance(time.Now())
	b := &w.buckets[w.currentIdx]
	b.CloseCount++
	b.TotalDurationNs += durationNs
	b.TotalBytesSent += bytesSent
	b.TotalBytesRecv += bytesRecv
}

// RecordRetransmit records one TCP retransmit event.
func (w *Window) RecordRetransmit() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.advance(time.Now())
	w.buckets[w.currentIdx].RetransmitCount++
}

// WindowStats holds the aggregated result computed over all active buckets.
type WindowStats struct {
	// TotalConnections is the connection count across all buckets in the window.
	TotalConnections int64

	// TotalCloses is the number of completed connections.
	TotalCloses int64

	// TotalDurationNs is the sum of all socket lifetimes (for average calculation).
	TotalDurationNs int64

	// TotalBytesSent is total TX bytes across the window.
	TotalBytesSent int64

	// TotalBytesRecv is total RX bytes across the window.
	TotalBytesRecv int64

	// TotalRetransmits is the count of retransmit events in the window.
	TotalRetransmits int64

	// WindowDurationSecs is the actual active window duration in seconds,
	// used to compute per-second rates.
	WindowDurationSecs float64
}

// Aggregate sums all valid (non-expired) buckets and returns a snapshot.
// This is the core "read" operation — safe to call concurrently.
func (w *Window) Aggregate() WindowStats {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.advance(time.Now())

	var stats WindowStats
	stats.WindowDurationSecs = w.windowDuration.Seconds()

	for i := range w.buckets {
		b := &w.buckets[i]
		stats.TotalConnections += b.ConnectionCount
		stats.TotalCloses += b.CloseCount
		stats.TotalDurationNs += b.TotalDurationNs
		stats.TotalBytesSent += b.TotalBytesSent
		stats.TotalBytesRecv += b.TotalBytesRecv
		stats.TotalRetransmits += b.RetransmitCount
	}

	return stats
}
