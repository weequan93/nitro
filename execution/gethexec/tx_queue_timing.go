// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethexec

import (
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/rpcdiag"
)

// Copies of a queue item share its timings. The RPC may read them while an
// attempt is still in progress, for example when the request is canceled.
type txQueueTiming struct {
	mu       sync.Mutex
	first    time.Time // assigned before publishing the item to the channel
	dequeued time.Time
	nonce    queueInterval
	sequence queueInterval
	seqID    uint64
}

type queueInterval struct {
	started time.Time
	total   time.Duration
}

func (i *queueInterval) elapsed(now time.Time) time.Duration {
	if i.started.IsZero() {
		return i.total
	}
	return i.total + now.Sub(i.started)
}

func (i *queueInterval) end(now time.Time) {
	i.total = i.elapsed(now)
	i.started = time.Time{}
}

func (t *txQueueTiming) dequeue() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.dequeued.IsZero() {
		t.dequeued = time.Now()
	}
}

func (t *txQueueTiming) startNonceWait() {
	if t != nil {
		t.mu.Lock()
		t.nonce.started = time.Now()
		t.mu.Unlock()
	}
}

func (t *txQueueTiming) endNonceWait() {
	if t != nil {
		t.mu.Lock()
		t.nonce.end(time.Now())
		t.mu.Unlock()
	}
}

func (t *txQueueTiming) beginSequence(id uint64) {
	if t != nil {
		t.mu.Lock()
		t.seqID = id
		t.sequence.started = time.Now()
		t.mu.Unlock()
	}
}

func (t *txQueueTiming) endSequence() {
	if t != nil {
		t.mu.Lock()
		t.sequence.end(time.Now())
		t.mu.Unlock()
	}
}

func (t *txQueueTiming) snapshot() rpcdiag.TxQueueObservation {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	o := rpcdiag.TxQueueObservation{
		NonceWait: t.nonce.elapsed(now), SequenceWait: t.sequence.elapsed(now), LastSeqID: t.seqID,
	}
	if !t.dequeued.IsZero() {
		o.Dequeued = true
		o.FirstDequeue = t.dequeued.Sub(t.first)
	}
	return o
}
