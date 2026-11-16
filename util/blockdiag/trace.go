// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package blockdiag

import (
	"context"
	"runtime/pprof"
	"runtime/trace"
	"strconv"
	"sync/atomic"
	"time"
)

type Stage uint8

const (
	EngineLockWait Stage = iota
	InsertRetryWait
	StateOpen
	PreviousBlockRead
	ProduceBlock
	ArbosSetup
	NextTransaction
	TransactionAttempt
	PreFilter
	TransactionPrepare
	ApplyTransaction
	PostFilter
	BlockFilter
	FinalizeBlock
	AssembleBlock
	ConsensusWrite
	BlockWrite
	stageCount
)

var stageNames = [...]string{
	"engine_lock_wait", "insert_retry_wait", "state_open", "previous_block_read", "produce_block",
	"arbos_setup", "next_transaction", "transaction_attempt", "pre_filter", "transaction_prepare",
	"apply_transaction", "post_filter", "block_filter", "finalize_block", "assemble_block", "consensus_write", "block_write",
}

type timing struct {
	calls atomic.Uint64
	ns    atomic.Int64
	maxNS atomic.Int64
}

func (t *timing) add(d time.Duration) {
	if d < 0 {
		return
	}
	t.calls.Add(1)
	t.ns.Add(int64(d))
	for old := t.maxNS.Load(); int64(d) > old; old = t.maxNS.Load() {
		if t.maxNS.CompareAndSwap(old, int64(d)) {
			break
		}
	}
}

type Timing struct {
	Calls uint64  `json:"calls"`
	MS    float64 `json:"ms"`
	MaxMS float64 `json:"max_ms"`
}

func (t *timing) snapshot() Timing {
	return Timing{t.calls.Load(), float64(t.ns.Load()) / float64(time.Millisecond),
		float64(t.maxNS.Load()) / float64(time.Millisecond)}
}

type Record struct {
	Schema         int               `json:"schema"`
	ID             uint64            `json:"seq_id"`
	StartedUTC     string            `json:"started_utc"`
	TotalMS        float64           `json:"total_ms"`
	Outcome        string            `json:"outcome"`
	BlockNumber    uint64            `json:"block"`
	AttemptedBlock uint64            `json:"attempted_block"`
	GasUsed        uint64            `json:"gas_used"`
	Transactions   int               `json:"transactions"`
	Stages         map[string]Timing `json:"stages"`
	Readers        map[string]Timing `json:"state_readers"`
}

// Trace is shared by StateDB copies within one sequencing operation. Its
// counters are safe for concurrent readers; stage/reader work may overlap.
type Trace struct {
	id        uint64
	started   time.Time
	ctx       context.Context
	stages    [stageCount]timing
	reads     [4]timing
	attempted atomic.Uint64
}

func (t *Trace) AttemptBlock(number uint64) {
	if t != nil {
		t.attempted.Store(number)
	}
}

func newTrace(id uint64) *Trace {
	return &Trace{id: id, started: time.Now(), ctx: context.Background()}
}

// Run labels CPU samples and runtime events without binding the goroutine to an
// OS thread. Child goroutines can inherit CPU labels; their CPU is not a wall
// time budget for the foreground goroutine.
func (t *Trace) Run(parent context.Context, fn func()) {
	if t == nil {
		fn()
		return
	}
	pprof.Do(parent, pprof.Labels("component", "sequencer", "seq_id", strconv.FormatUint(t.id, 10)), func(ctx context.Context) {
		ctx, task := trace.NewTask(ctx, "sequencer/block")
		t.ctx = ctx
		defer task.End()
		trace.Log(ctx, "sequencer_id", strconv.FormatUint(t.id, 10))
		fn()
	})
}

type Span struct {
	t      *Trace
	stage  Stage
	start  time.Time
	region *trace.Region
}

func (t *Trace) Begin(stage Stage) Span {
	if t == nil {
		return Span{}
	}
	s := Span{t: t, stage: stage, start: time.Now()}
	if trace.IsEnabled() {
		s.region = trace.StartRegion(t.ctx, "sequencer/"+stageNames[stage])
	}
	return s
}

func (s *Span) End() {
	if s.t == nil {
		return
	}
	duration := time.Since(s.start)
	if s.region != nil {
		s.region.End()
	}
	s.t.stages[s.stage].add(duration)
	s.t = nil
}

func (t *Trace) record(outcome string, block, gas uint64, txs int) Record {
	r := Record{Schema: 1, ID: t.id, StartedUTC: t.started.UTC().Format(time.RFC3339Nano),
		TotalMS: float64(time.Since(t.started)) / float64(time.Millisecond), Outcome: outcome,
		BlockNumber: block, AttemptedBlock: t.attempted.Load(), GasUsed: gas, Transactions: txs, Stages: make(map[string]Timing), Readers: make(map[string]Timing)}
	for i := range t.stages {
		if value := t.stages[i].snapshot(); value.Calls > 0 {
			r.Stages[stageNames[i]] = value
		}
	}
	for i, name := range readerNames {
		if value := t.reads[i].snapshot(); value.Calls > 0 {
			r.Readers[name] = value
		}
	}
	return r
}
