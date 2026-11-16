// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package blockdiag

import (
	"runtime/trace"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
)

var readerNames = [...]string{"account", "storage", "code", "code_size"}

type observedReader struct {
	state.Reader
	t *Trace
}

func (r *observedReader) BlockTrace() *Trace { return r.t }

type readerSpan struct {
	r      *observedReader
	op     int
	start  time.Time
	region *trace.Region
}

func (r *observedReader) begin(op int) readerSpan {
	s := readerSpan{r: r, op: op, start: time.Now()}
	if trace.IsEnabled() {
		s.region = trace.StartRegion(r.t.ctx, "sequencer/state_reader/"+readerNames[op])
	}
	return s
}

func (s *readerSpan) end() {
	s.r.t.reads[s.op].add(time.Since(s.start))
	if s.region != nil {
		s.region.End()
	}
}

func (r *observedReader) Account(addr common.Address) (*types.StateAccount, error) {
	s := r.begin(0)
	defer s.end()
	return r.Reader.Account(addr)
}

func (r *observedReader) Storage(addr common.Address, key common.Hash) (common.Hash, error) {
	s := r.begin(1)
	defer s.end()
	return r.Reader.Storage(addr, key)
}

func (r *observedReader) Code(addr common.Address, hash common.Hash) ([]byte, error) {
	s := r.begin(2)
	defer s.end()
	return r.Reader.Code(addr, hash)
}

func (r *observedReader) CodeSize(addr common.Address, hash common.Hash) (int, error) {
	s := r.begin(3)
	defer s.end()
	return r.Reader.CodeSize(addr, hash)
}

type observedReaderWithStats struct {
	*observedReader
	stats state.ReaderWithStats
}

func (r *observedReaderWithStats) GetStats() state.ReaderStats { return r.stats.GetStats() }

func wrapReader(reader state.Reader, t *Trace) state.Reader {
	if t == nil {
		return reader
	}
	r := &observedReader{Reader: reader, t: t}
	if stats, ok := reader.(state.ReaderWithStats); ok {
		return &observedReaderWithStats{observedReader: r, stats: stats}
	}
	return r
}

// NewState follows state.New's Reader/NewWithReader construction using the
// original database, caches, trie, witness and config. Only Reader is wrapped.
// StateDB.Copy shares that reader, preserving work across group rollback.
func (t *Trace) NewState(root common.Hash, db state.Database) (*state.StateDB, error) {
	if t == nil {
		return state.New(root, db)
	}
	reader, err := db.Reader(root)
	if err != nil {
		return nil, err
	}
	return state.NewWithReader(root, db, wrapReader(reader, t))
}

func FromState(db *state.StateDB) *Trace {
	if provider, ok := db.Reader().(interface{ BlockTrace() *Trace }); ok {
		return provider.BlockTrace()
	}
	return nil
}
