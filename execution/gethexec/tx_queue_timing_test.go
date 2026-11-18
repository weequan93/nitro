package gethexec

import (
	"testing"
	"time"
)

func TestTxQueueTimingIncludesPendingWaitAndKeepsFirstDequeue(t *testing.T) {
	now := time.Now()
	timer := &txQueueTiming{first: now.Add(-time.Second)}
	timer.dequeue()
	first := timer.snapshot().FirstDequeue
	timer.startNonceWait()
	if got := timer.snapshot(); !got.Dequeued || got.NonceWait <= 0 {
		t.Fatalf("missing in-progress nonce wait: %+v", got)
	}
	timer.endNonceWait()
	nonceWait := timer.snapshot().NonceWait
	timer.dequeue()
	timer.beginSequence(41)
	if got := timer.snapshot(); got.FirstDequeue != first || got.NonceWait != nonceWait ||
		got.LastSeqID != 41 || got.SequenceWait <= 0 {
		t.Fatalf("unexpected timing during sequence: %+v", got)
	}
	timer.endSequence()
	sequenceWait := timer.snapshot().SequenceWait
	timer.beginSequence(42)
	timer.endSequence()
	if got := timer.snapshot(); got.LastSeqID != 42 || got.SequenceWait <= sequenceWait {
		t.Fatalf("retry timing lost: %+v", got)
	}
}
