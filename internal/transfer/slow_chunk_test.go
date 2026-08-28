package transfer

import (
	"context"
	"testing"
	"time"
)

func testSlowMonitor(now *time.Time) *SlowChunkMonitor {
	return NewSlowChunkMonitorWithOptions(SlowChunkOptions{
		Now:           func() time.Time { return *now },
		DisableTicker: true,
	})
}

func registerSlowTestChunk(monitor *SlowChunkMonitor, input SlowChunkRegistration) (SlowChunkSnapshot, context.Context) {
	ctx, cancel := context.WithCancel(context.Background())
	input.AttemptContext = ctx
	input.AttemptCancel = cancel
	return monitor.Register(input), ctx
}

func TestSlowChunkMonitorAbortsStalledChunk(t *testing.T) {
	now := time.Unix(100, 0)
	monitor := testSlowMonitor(&now)
	defer monitor.Close()
	entry, ctx := registerSlowTestChunk(monitor, SlowChunkRegistration{
		FileID:     "file-a",
		ChunkIndex: 0,
		ChunkSize:  1024 * 1024,
	})
	now = now.Add(slowChunkMinObserve + slowChunkStallTimeout)
	monitor.EvaluateNow()
	got, _ := monitor.Get(entry.Key)
	if !got.AbortedSlowChunk || got.Detect != SlowChunkDetectStall || ctx.Err() == nil {
		t.Fatalf("stalled entry = %#v, context error = %v", got, ctx.Err())
	}
}

func TestSlowChunkMonitorAbortsRelativeSlowestAfterTwoTicks(t *testing.T) {
	now := time.Unix(100, 0)
	monitor := testSlowMonitor(&now)
	defer monitor.Close()
	peerA, _ := registerSlowTestChunk(monitor, SlowChunkRegistration{FileID: "file-a", ChunkIndex: 0, ChunkSize: 10 * 1024 * 1024})
	peerB, _ := registerSlowTestChunk(monitor, SlowChunkRegistration{FileID: "file-a", ChunkIndex: 1, ChunkSize: 10 * 1024 * 1024})
	slow, slowCtx := registerSlowTestChunk(monitor, SlowChunkRegistration{FileID: "file-a", ChunkIndex: 2, ChunkSize: 10 * 1024 * 1024})

	now = now.Add(slowChunkMinObserve)
	monitor.RecordSample(peerA.Key, 256*1024)
	monitor.RecordSample(peerB.Key, 256*1024)
	monitor.RecordSample(slow.Key, 4*1024)
	now = now.Add(time.Second)
	monitor.RecordSample(peerA.Key, 512*1024)
	monitor.RecordSample(peerB.Key, 512*1024)
	monitor.RecordSample(slow.Key, 8*1024)
	monitor.EvaluateNow()
	first, _ := monitor.Get(slow.Key)
	if first.AbortedSlowChunk {
		t.Fatal("slow chunk aborted on first tick")
	}
	monitor.EvaluateNow()
	second, _ := monitor.Get(slow.Key)
	if !second.AbortedSlowChunk || second.Detect != SlowChunkDetectRelative || slowCtx.Err() == nil {
		t.Fatalf("slow entry = %#v, context error = %v", second, slowCtx.Err())
	}
}

func TestSlowChunkMonitorRequiresTwoRelativePeers(t *testing.T) {
	now := time.Unix(100, 0)
	monitor := testSlowMonitor(&now)
	defer monitor.Close()
	peer, _ := registerSlowTestChunk(monitor, SlowChunkRegistration{FileID: "file-a", ChunkIndex: 0, ChunkSize: 10 * 1024 * 1024})
	slow, slowCtx := registerSlowTestChunk(monitor, SlowChunkRegistration{FileID: "file-a", ChunkIndex: 1, ChunkSize: 10 * 1024 * 1024})
	now = now.Add(slowChunkMinObserve)
	monitor.RecordSample(peer.Key, 256*1024)
	monitor.RecordSample(slow.Key, 4*1024)
	now = now.Add(time.Second)
	monitor.RecordSample(peer.Key, 512*1024)
	monitor.RecordSample(slow.Key, 8*1024)
	monitor.EvaluateNow()
	monitor.EvaluateNow()
	got, _ := monitor.Get(slow.Key)
	if got.AbortedSlowChunk || slowCtx.Err() != nil {
		t.Fatalf("entry with one peer was aborted: %#v", got)
	}
}

func TestSlowChunkMonitorIgnoresNonNetworkPhase(t *testing.T) {
	now := time.Unix(100, 0)
	monitor := testSlowMonitor(&now)
	defer monitor.Close()
	entry, ctx := registerSlowTestChunk(monitor, SlowChunkRegistration{FileID: "file-a", ChunkSize: 1024 * 1024})
	monitor.SetPhase(entry.Key, SlowChunkPhaseDiskWrite)
	now = now.Add(time.Minute)
	monitor.EvaluateNow()
	got, _ := monitor.Get(entry.Key)
	if got.AbortedSlowChunk || ctx.Err() != nil {
		t.Fatalf("disk-write entry was aborted: %#v", got)
	}
	monitor.SetPhase(entry.Key, SlowChunkPhaseNetwork)
	now = now.Add(slowChunkStallTimeout - time.Nanosecond)
	monitor.EvaluateNow()
	got, _ = monitor.Get(entry.Key)
	if got.AbortedSlowChunk {
		t.Fatal("network entry aborted before timeout")
	}
	now = now.Add(time.Nanosecond)
	monitor.EvaluateNow()
	got, _ = monitor.Get(entry.Key)
	if !got.AbortedSlowChunk || got.Detect != SlowChunkDetectStall {
		t.Fatalf("network entry = %#v", got)
	}
}

func TestSlowChunkMonitorKeepsProgressMonotonic(t *testing.T) {
	now := time.Unix(100, 0)
	monitor := testSlowMonitor(&now)
	defer monitor.Close()
	entry, _ := registerSlowTestChunk(monitor, SlowChunkRegistration{FileID: "file-a", ChunkSize: 1024 * 1024})
	monitor.RecordSample(entry.Key, 512*1024)
	now = now.Add(time.Second)
	monitor.RecordSample(entry.Key, 256*1024)
	got, _ := monitor.Get(entry.Key)
	if got.TransferredBytes != 512*1024 {
		t.Fatalf("TransferredBytes = %d", got.TransferredBytes)
	}
}

func TestSlowChunkMonitorDoesNotAbortAfterReconnectLimit(t *testing.T) {
	now := time.Unix(100, 0)
	monitor := testSlowMonitor(&now)
	defer monitor.Close()
	entry, ctx := registerSlowTestChunk(monitor, SlowChunkRegistration{
		FileID:         "file-a",
		ChunkSize:      1024 * 1024,
		SlowReconnects: SlowChunkMaxReconnects,
	})
	now = now.Add(slowChunkMinObserve + slowChunkStallTimeout)
	monitor.EvaluateNow()
	got, _ := monitor.Get(entry.Key)
	if got.AbortedSlowChunk || ctx.Err() != nil {
		t.Fatalf("exhausted entry was aborted: %#v", got)
	}
}

func TestSlowChunkMonitorAbortsSustainedAbsoluteSlowTransfer(t *testing.T) {
	now := time.Unix(100, 0)
	monitor := testSlowMonitor(&now)
	defer monitor.Close()
	entry, ctx := registerSlowTestChunk(monitor, SlowChunkRegistration{
		FileID:     "file-a",
		ChunkIndex: 0,
		ChunkSize:  10 * 1024 * 1024,
	})
	for second := 1; second <= 6; second++ {
		now = now.Add(time.Second)
		monitor.RecordSample(entry.Key, int64(second)*40*1024)
		monitor.EvaluateNow()
	}
	got, _ := monitor.Get(entry.Key)
	if got.AbortedSlowChunk {
		t.Fatalf("aborted before required absolute ticks: %#v", got)
	}
	now = now.Add(time.Second)
	monitor.RecordSample(entry.Key, 7*40*1024)
	monitor.EvaluateNow()
	got, _ = monitor.Get(entry.Key)
	if !got.AbortedSlowChunk || got.Detect != SlowChunkDetectAbsolute || ctx.Err() == nil {
		t.Fatalf("absolute entry = %#v, context error = %v", got, ctx.Err())
	}
}

func TestSlowChunkMonitorDoesNotAbsoluteAbortWhenDisabled(t *testing.T) {
	now := time.Unix(100, 0)
	monitor := testSlowMonitor(&now)
	defer monitor.Close()
	allow := false
	entry, ctx := registerSlowTestChunk(monitor, SlowChunkRegistration{
		FileID:             "file-a",
		ChunkIndex:         0,
		ChunkSize:          10 * 1024 * 1024,
		AllowAbsoluteAbort: &allow,
	})
	for second := 1; second <= 7; second++ {
		now = now.Add(time.Second)
		monitor.RecordSample(entry.Key, int64(second)*40*1024)
		monitor.EvaluateNow()
	}
	got, _ := monitor.Get(entry.Key)
	if got.AbortedSlowChunk || ctx.Err() != nil {
		t.Fatalf("disabled absolute abort still fired: %#v", got)
	}
}

func TestSlowChunkMonitorLetsAbsoluteSlowFinishWhenLessThanThirtySecondsRemain(t *testing.T) {
	now := time.Unix(100, 0)
	monitor := testSlowMonitor(&now)
	defer monitor.Close()
	initial := int64(1024 * 1024)
	entry, ctx := registerSlowTestChunk(monitor, SlowChunkRegistration{
		FileID:                  "file-a",
		ChunkIndex:              0,
		ChunkSize:               2 * 1024 * 1024,
		InitialTransferredBytes: initial,
	})
	for second := 1; second <= 10; second++ {
		now = now.Add(time.Second)
		monitor.RecordSample(entry.Key, initial+int64(second)*40*1024)
		monitor.EvaluateNow()
	}
	got, _ := monitor.Get(entry.Key)
	if got.AbortedSlowChunk || ctx.Err() != nil {
		t.Fatalf("short remaining transfer was aborted: %#v", got)
	}
}

func TestSlowChunkMonitorSkipsRelativeAbortWhenNearComplete(t *testing.T) {
	now := time.Unix(100, 0)
	monitor := testSlowMonitor(&now)
	defer monitor.Close()
	chunkSize := int64(1024 * 1024)
	peer, _ := registerSlowTestChunk(monitor, SlowChunkRegistration{FileID: "file-a", ChunkIndex: 0, ChunkSize: chunkSize})
	near, nearCtx := registerSlowTestChunk(monitor, SlowChunkRegistration{FileID: "file-a", ChunkIndex: 1, ChunkSize: chunkSize})
	now = now.Add(slowChunkMinObserve)
	monitor.RecordSample(peer.Key, 256*1024)
	monitor.RecordSample(near.Key, chunkSize*9/10)
	now = now.Add(time.Second)
	monitor.RecordSample(peer.Key, 512*1024)
	monitor.RecordSample(near.Key, chunkSize*9/10+1000)
	monitor.EvaluateNow()
	monitor.EvaluateNow()
	got, _ := monitor.Get(near.Key)
	if got.AbortedSlowChunk || nearCtx.Err() != nil {
		t.Fatalf("near-complete chunk was relatively aborted: %#v", got)
	}
}

func TestSlowChunkMonitorExtendsStallTimeoutForNearComplete(t *testing.T) {
	now := time.Unix(100, 0)
	monitor := testSlowMonitor(&now)
	defer monitor.Close()
	chunkSize := int64(1024 * 1024)
	entry, ctx := registerSlowTestChunk(monitor, SlowChunkRegistration{
		FileID:     "file-a",
		ChunkIndex: 0,
		ChunkSize:  chunkSize,
	})
	monitor.RecordSample(entry.Key, chunkSize*99/100)
	now = now.Add(slowChunkStallTimeout)
	monitor.EvaluateNow()
	got, _ := monitor.Get(entry.Key)
	if got.AbortedSlowChunk || ctx.Err() != nil {
		t.Fatalf("near-complete aborted at the short stall timeout: %#v", got)
	}
	now = now.Add(slowChunkNearCompleteStallTimeout - slowChunkStallTimeout)
	monitor.EvaluateNow()
	got, _ = monitor.Get(entry.Key)
	if !got.AbortedSlowChunk || got.Detect != SlowChunkDetectStall || ctx.Err() == nil {
		t.Fatalf("near-complete stall = %#v, context error = %v", got, ctx.Err())
	}
}

func TestSlowChunkMonitorAppliesNearCompleteProtectionOnResume(t *testing.T) {
	now := time.Unix(100, 0)
	monitor := testSlowMonitor(&now)
	defer monitor.Close()
	chunkSize := int64(1024 * 1024)
	initial := chunkSize * 99 / 100
	entry, ctx := registerSlowTestChunk(monitor, SlowChunkRegistration{
		FileID:                  "file-a",
		ChunkIndex:              0,
		ChunkSize:               chunkSize,
		InitialTransferredBytes: initial,
	})
	now = now.Add(slowChunkStallTimeout)
	monitor.EvaluateNow()
	got, _ := monitor.Get(entry.Key)
	if got.TransferredBytes != initial || got.AbortedSlowChunk || ctx.Err() != nil {
		t.Fatalf("resumed near-complete aborted too early: %#v", got)
	}
	now = now.Add(slowChunkNearCompleteStallTimeout - slowChunkStallTimeout)
	monitor.EvaluateNow()
	got, _ = monitor.Get(entry.Key)
	if !got.AbortedSlowChunk || got.Detect != SlowChunkDetectStall || ctx.Err() == nil {
		t.Fatalf("resumed near-complete stall = %#v, context error = %v", got, ctx.Err())
	}
}

func TestSlowChunkMonitorDoesNotCountNonNetworkPhasesTowardStall(t *testing.T) {
	phases := []SlowChunkPhase{SlowChunkPhaseBandwidthWait, SlowChunkPhaseDiskWrite, SlowChunkPhaseProcessing}
	for _, phase := range phases {
		t.Run(string(phase), func(t *testing.T) {
			now := time.Unix(100, 0)
			monitor := testSlowMonitor(&now)
			defer monitor.Close()
			entry, ctx := registerSlowTestChunk(monitor, SlowChunkRegistration{FileID: "file-a", ChunkSize: 1024 * 1024})
			monitor.SetPhase(entry.Key, phase)
			now = now.Add(time.Minute)
			monitor.EvaluateNow()
			got, _ := monitor.Get(entry.Key)
			if got.AbortedSlowChunk || ctx.Err() != nil {
				t.Fatalf("%s entry was aborted: %#v", phase, got)
			}
			monitor.SetPhase(entry.Key, SlowChunkPhaseNetwork)
			now = now.Add(slowChunkStallTimeout - time.Nanosecond)
			monitor.EvaluateNow()
			got, _ = monitor.Get(entry.Key)
			if got.AbortedSlowChunk {
				t.Fatal("network entry aborted before timeout")
			}
			now = now.Add(time.Nanosecond)
			monitor.EvaluateNow()
			got, _ = monitor.Get(entry.Key)
			if !got.AbortedSlowChunk || got.Detect != SlowChunkDetectStall {
				t.Fatalf("network entry = %#v", got)
			}
		})
	}
}

func TestSlowChunkMonitorKeepsSimultaneousRegistrationsIndependent(t *testing.T) {
	now := time.Unix(100, 0)
	monitor := testSlowMonitor(&now)
	defer monitor.Close()
	first, _ := registerSlowTestChunk(monitor, SlowChunkRegistration{FileID: "file-a", ChunkIndex: 0, ChunkSize: 1024})
	second, _ := registerSlowTestChunk(monitor, SlowChunkRegistration{FileID: "file-a", ChunkIndex: 0, ChunkSize: 1024})
	if first.Key == second.Key {
		t.Fatal("same-file same-chunk registrations shared a key")
	}
	monitor.RecordSample(first.Key, 256)
	firstGot, _ := monitor.Get(first.Key)
	secondGot, _ := monitor.Get(second.Key)
	if firstGot.TransferredBytes != 256 || secondGot.TransferredBytes != 0 {
		t.Fatalf("first = %#v, second = %#v", firstGot, secondGot)
	}
	monitor.Unregister(first.Key)
	monitor.RecordSample(second.Key, 512)
	secondGot, _ = monitor.Get(second.Key)
	if secondGot.TransferredBytes != 512 {
		t.Fatalf("second after first unregister = %#v", secondGot)
	}
}
