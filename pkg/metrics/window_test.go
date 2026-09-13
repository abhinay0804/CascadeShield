package metrics

import (
	"sync"
	"testing"
	"time"
)

func TestWindow_RecordAndAggregate(t *testing.T) {
	w := NewWindow(12, 60*time.Second)

	w.RecordConnect()
	w.RecordConnect()
	w.RecordClose(1_000_000, 512, 1024) // 1ms duration
	w.RecordRetransmit()

	stats := w.Aggregate()

	if stats.TotalConnections != 2 {
		t.Errorf("expected 2 connections, got %d", stats.TotalConnections)
	}
	if stats.TotalCloses != 1 {
		t.Errorf("expected 1 close, got %d", stats.TotalCloses)
	}
	if stats.TotalRetransmits != 1 {
		t.Errorf("expected 1 retransmit, got %d", stats.TotalRetransmits)
	}
	if stats.TotalBytesSent != 512 {
		t.Errorf("expected 512 bytes sent, got %d", stats.TotalBytesSent)
	}
	if stats.WindowDurationSecs != 60.0 {
		t.Errorf("expected 60s window, got %f", stats.WindowDurationSecs)
	}
}

func TestWindow_SlideEviction(t *testing.T) {
	// 2 buckets, 100ms total window → each bucket = 50ms
	w := NewWindow(2, 100*time.Millisecond)

	// Record in the current bucket
	w.RecordConnect()
	stats := w.Aggregate()
	if stats.TotalConnections != 1 {
		t.Fatalf("expected 1 connection before slide, got %d", stats.TotalConnections)
	}

	// Wait until the bucket expires
	time.Sleep(120 * time.Millisecond)

	// After slide, old bucket should be evicted
	stats = w.Aggregate()
	if stats.TotalConnections != 0 {
		t.Errorf("expected 0 connections after expiry, got %d", stats.TotalConnections)
	}
}

func TestWindow_ConcurrentRace(t *testing.T) {
	w := NewWindow(12, 60*time.Second)
	var wg sync.WaitGroup

	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				w.RecordConnect()
				w.RecordClose(500_000, 64, 128)
				w.RecordRetransmit()
				_ = w.Aggregate()
			}
		}()
	}
	wg.Wait()
	stats := w.Aggregate()
	if stats.TotalConnections <= 0 {
		t.Errorf("expected positive connection count after concurrent writes")
	}
}

// BenchmarkWindow_RecordConnect measures how fast we can record CONNECT events.
// Target: > 1M ops/sec (100K events/s requirement with headroom).
func BenchmarkWindow_RecordConnect(b *testing.B) {
	w := NewWindow(12, 60*time.Second)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			w.RecordConnect()
		}
	})
}

// BenchmarkWindow_Aggregate measures sliding window aggregate computation.
func BenchmarkWindow_Aggregate(b *testing.B) {
	w := NewWindow(12, 60*time.Second)
	for i := 0; i < 1000; i++ {
		w.RecordConnect()
		w.RecordClose(int64(i)*1_000_000, 512, 1024)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = w.Aggregate()
	}
}

// BenchmarkHistogram_RecordNs measures histogram recording throughput.
func BenchmarkHistogram_RecordNs(b *testing.B) {
	h := NewHistogram(0, 30000, 3)
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		ns := int64(50_000_000) // 50ms
		for pb.Next() {
			h.RecordNs(ns)
		}
	})
}

