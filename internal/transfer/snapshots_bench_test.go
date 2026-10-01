package transfer

import (
	"testing"
	"time"
)

func BenchmarkUpdateProgress(b *testing.B) {
	now := time.Unix(100, 0)
	service := NewWithOptions(Options{Now: func() time.Time { return now }})
	service.emitStopped = true
	service.entries["fast"] = &entry{record: Record{Snapshot: Snapshot{
		PID: "fast", Status: StatusProgress, TotalSize: 1 << 60,
	}}}
	var transferred int64
	for range 5001 {
		now = now.Add(time.Millisecond)
		transferred += 32 * 1024
		if err := service.Update("fast", Updates{TransferredSize: &transferred}); err != nil {
			b.Fatal(err)
		}
	}
	b.ReportAllocs()
	for b.Loop() {
		now = now.Add(time.Millisecond)
		transferred += 32 * 1024
		if err := service.Update("fast", Updates{TransferredSize: &transferred}); err != nil {
			b.Fatal(err)
		}
	}
}
