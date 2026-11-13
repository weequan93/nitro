// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package blockdiag

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
)

type readerStub struct {
	account *types.StateAccount
	value   common.Hash
	code    []byte
	err     error
	wait    func()
}

func (s *readerStub) Account(common.Address) (*types.StateAccount, error) { return s.account, s.err }
func (s *readerStub) Storage(common.Address, common.Hash) (common.Hash, error) {
	if s.wait != nil {
		s.wait()
	}
	return s.value, s.err
}
func (s *readerStub) Code(common.Address, common.Hash) ([]byte, error)  { return s.code, s.err }
func (s *readerStub) CodeSize(common.Address, common.Hash) (int, error) { return len(s.code), s.err }

type readerStatsStub struct{ *readerStub }

func (s *readerStatsStub) GetStats() state.ReaderStats {
	return state.ReaderStats{AccountHit: 7, StorageMiss: 9}
}

func TestDisabledHotPathAndReaderIdentity(t *testing.T) {
	m, err := New(Config{}, nil)
	if err != nil || m != nil {
		t.Fatalf("disabled manager = %v, %v", m, err)
	}
	r := &readerStub{}
	if wrapReader(r, nil) != r {
		t.Fatal("disabled reader was wrapped")
	}
	allocs := testing.AllocsPerRun(1000, func() {
		trace := m.Begin()
		span := trace.Begin(ProduceBlock)
		span.End()
		m.Finish(trace, "success", 1, 2, 3)
	})
	if allocs != 0 {
		t.Fatalf("disabled hot path allocations = %v", allocs)
	}
}

func TestReaderResultsErrorsAndOptionalInterfaceArePreserved(t *testing.T) {
	errSentinel := errors.New("reader sentinel")
	base := &readerStub{account: &types.StateAccount{}, value: common.HexToHash("0x1"), code: []byte{1, 2}, err: errSentinel}
	trace := newTrace(1)
	r := wrapReader(base, trace)
	if _, ok := r.(state.ReaderWithStats); ok {
		t.Fatal("wrapper invented reader statistics")
	}
	account, err := r.Account(common.Address{})
	if account != base.account || err != errSentinel {
		t.Fatal("account result/error changed")
	}
	value, err := r.Storage(common.Address{}, common.Hash{})
	if value != base.value || err != errSentinel {
		t.Fatal("storage result/error changed")
	}
	code, err := r.Code(common.Address{}, common.Hash{})
	if !bytes.Equal(code, base.code) || &code[0] != &base.code[0] || err != errSentinel {
		t.Fatal("code ownership/result/error changed")
	}
	size, err := r.CodeSize(common.Address{}, common.Hash{})
	if size != len(base.code) || err != errSentinel {
		t.Fatal("code size result/error changed")
	}
	stats := wrapReader(&readerStatsStub{base}, trace).(state.ReaderWithStats).GetStats()
	if stats.AccountHit != 7 || stats.StorageMiss != 9 {
		t.Fatal("underlying statistics were not forwarded")
	}
	record := trace.record("error", 0, 0, 0)
	for _, name := range readerNames {
		if record.Readers[name].Calls != 1 {
			t.Fatalf("%s count = %v", name, record.Readers[name])
		}
	}
}

func TestStateCopiesPreserveObservationAndStateSemantics(t *testing.T) {
	db := state.NewDatabaseForTesting()
	defer db.TrieDB().Close()
	initial, err := state.New(types.EmptyRootHash, db)
	if err != nil {
		t.Fatal(err)
	}
	addr, slot := common.HexToAddress("0x123"), common.HexToHash("0x5")
	initial.SetBalance(addr, uint256.NewInt(123), tracing.BalanceChangeUnspecified)
	initial.SetCode(addr, []byte{0x60, 0x00}, tracing.CodeChangeUnspecified)
	initial.SetState(addr, slot, common.HexToHash("0x7"))
	root, err := initial.Commit(1, false, false)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := state.New(root, db)
	if err != nil {
		t.Fatal(err)
	}
	trace := newTrace(2)
	observed, err := trace.NewState(root, db)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Database() != db {
		t.Fatal("database/cache ownership changed")
	}
	copyState := observed.Copy()
	if FromState(copyState) != trace {
		t.Fatal("group-copy observation was lost")
	}
	for _, s := range []*state.StateDB{plain, observed, copyState} {
		if s.GetBalance(addr).Uint64() != 123 || !bytes.Equal(s.GetCode(addr), []byte{0x60, 0x00}) ||
			s.GetState(addr, slot) != common.HexToHash("0x7") {
			t.Fatal("state reads changed")
		}
		checkpoint := s.Snapshot()
		s.SetState(addr, slot, common.HexToHash("0xbad"))
		s.RevertToSnapshot(checkpoint)
		s.SetNonce(addr, 2, tracing.NonceChangeUnspecified)
	}
	if plain.IntermediateRoot(false) != observed.IntermediateRoot(false) ||
		plain.IntermediateRoot(false) != copyState.IntermediateRoot(false) {
		t.Fatal("diagnostics changed final state")
	}
	if trace.record("success", 1, 0, 0).Readers["storage"].Calls != 2 {
		t.Fatal("copied-state work was not counted")
	}
}

func TestSharedReaderCountersRaceAndPanicSemantics(t *testing.T) {
	trace := newTrace(3)
	r := wrapReader(&readerStub{}, trace)
	var workers sync.WaitGroup
	for i := 0; i < 8; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for j := 0; j < 100; j++ {
				_, _ = r.Storage(common.Address{}, common.Hash{})
			}
		}()
	}
	workers.Wait()
	if trace.record("success", 0, 0, 0).Readers["storage"].Calls != 800 {
		t.Fatal("concurrent reader counts lost")
	}
	panicking := wrapReader(&readerStub{wait: func() { panic("sentinel") }}, trace)
	func() {
		defer func() {
			if recover() != "sentinel" {
				t.Fatal("reader panic was changed")
			}
		}()
		_, _ = panicking.Storage(common.Address{}, common.Hash{})
	}()
	if trace.record("panic", 0, 0, 0).Readers["storage"].Calls != 801 {
		t.Fatal("panic work was not observed")
	}
}

func TestPhaseEndIsIdempotentAndManagerNeverWaitsForSink(t *testing.T) {
	cfg := DefaultConfig
	cfg.Enable = true
	cfg.Threshold = time.Nanosecond
	m, err := New(cfg, func(Record) { t.Fatal("Finish called sink synchronously") })
	if err != nil {
		t.Fatal(err)
	}
	trace := m.Begin()
	s := trace.Begin(ProduceBlock)
	s.End()
	s.End()
	if trace.record("success", 1, 0, 0).Stages["produce_block"].Calls != 1 {
		t.Fatal("phase recorded twice")
	}
	for i := 0; i < 100; i++ {
		m.Finish(trace, "success", 1, 2, 3)
	}
	if len(m.reports) != cap(m.reports) {
		t.Fatal("bounded report queue did not fill")
	}
}

func TestCaptureConfigAndArtifactBudget(t *testing.T) {
	for _, change := range []func(*Config){
		func(c *Config) { c.Threshold = 0 }, func(c *Config) { c.CaptureDuration = time.Hour },
		func(c *Config) { c.CPUProfile = true },
		func(c *Config) { c.CaptureDuration = time.Minute; c.OutputDir = "" },
	} {
		cfg := DefaultConfig
		cfg.Enable = true
		change(&cfg)
		if cfg.Validate() == nil {
			t.Fatalf("invalid config accepted: %+v", cfg)
		}
	}
	file, err := os.CreateTemp(t.TempDir(), "budget-")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	w := &budgetWriter{ctx: context.Background(), file: file, budget: &fileBudget{max: 2}}
	if _, err := w.Write([]byte("123")); err == nil || !w.failed.Load() {
		t.Fatal("budget overflow accepted")
	}
	if stat, _ := file.Stat(); stat.Size() != 0 {
		t.Fatal("budget overflow wrote bytes")
	}
}

func TestCaptureWritesCompleteTaskTraceAndCPU(t *testing.T) {
	output := t.TempDir()
	if keep := os.Getenv("BLOCKDIAG_FIXTURE_DIR"); keep != "" {
		if err := os.MkdirAll(keep, 0o700); err != nil {
			t.Fatal(err)
		}
		var err error
		output, err = os.MkdirTemp(keep, "test-")
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(output, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := spaceAvailable(output); err != nil {
		t.Skip(err)
	}
	cfg := DefaultConfig
	cfg.Enable, cfg.Threshold, cfg.OutputDir = true, time.Nanosecond, output
	cfg.CaptureDuration, cfg.SnapshotInterval, cfg.MaxSnapshots, cfg.CPUProfile = 5*time.Second, 0, 1, true
	m, err := New(cfg, func(Record) {})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { m.Run(ctx); close(done) }()
	defer func() { cancel(); <-done }()
	var session string
	waitFor := func(state string) map[string]any {
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			paths, _ := filepath.Glob(filepath.Join(output, "sequencer-diagnostics-*", "session.json"))
			for _, path := range paths {
				data, _ := os.ReadFile(path)
				var value map[string]any
				if json.Unmarshal(data, &value) == nil && value["state"] == state {
					session = path
					return value
				}
			}
			time.Sleep(time.Millisecond)
		}
		t.Fatalf("capture did not reach %s", state)
		return nil
	}
	waitFor("capturing")
	var lock sync.Mutex
	lock.Lock()
	go func() { time.Sleep(20 * time.Millisecond); lock.Unlock() }()
	trace := m.Begin()
	trace.Run(context.Background(), func() {
		span := trace.Begin(ApplyTransaction)
		defer span.End()
		r := wrapReader(&readerStub{wait: func() { lock.Lock(); lock.Unlock() }}, trace)
		_, _ = r.Storage(common.Address{}, common.Hash{})
		until := time.Now().Add(80 * time.Millisecond)
		for time.Now().Before(until) {
		}
	})
	m.Finish(trace, "success", 100, 21000, 1)
	value := waitFor("complete")
	if value["reason"] != "snapshot_limit" || value["cpu_profile_write_failed"] != false {
		t.Fatalf("capture incomplete: %v", value)
	}
	dir := filepath.Dir(session)
	traces, _ := filepath.Glob(filepath.Join(dir, "seq-*.trace"))
	if len(traces) != 1 {
		t.Fatalf("trace count = %d", len(traces))
	}
	data, err := os.ReadFile(traces[0])
	if err != nil || !bytes.HasPrefix(data, []byte("go 1.")) {
		t.Fatal("invalid runtime trace header", err)
	}
	cpu, err := os.Open(filepath.Join(dir, "cpu.pprof"))
	if err != nil {
		t.Fatal(err)
	}
	defer cpu.Close()
	gz, err := gzip.NewReader(cpu)
	if err != nil {
		t.Fatal(err)
	}
	if n, err := io.Copy(io.Discard, gz); err != nil || n == 0 {
		t.Fatal("CPU profile gzip is incomplete", err)
	}
	if keep := os.Getenv("BLOCKDIAG_FIXTURE_DIR"); keep != "" {
		t.Log("retained fixture", dir)
	}
}

func BenchmarkStateReaderObservation(b *testing.B) {
	for _, enabled := range []bool{false, true} {
		name := "disabled"
		var trace *Trace
		if enabled {
			name = "enabled"
			trace = newTrace(1)
		}
		b.Run(name, func(b *testing.B) {
			r := wrapReader(&readerStub{}, trace)
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = r.Storage(common.Address{}, common.Hash{})
			}
		})
	}
}
