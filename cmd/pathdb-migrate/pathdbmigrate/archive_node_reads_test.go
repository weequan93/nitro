// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"bytes"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
)

type blockedArchiveReadDB struct {
	ethdb.Database
	gate  chan struct{}
	calls atomic.Uint64
}

func (db *blockedArchiveReadDB) Get(key []byte) ([]byte, error) {
	db.calls.Add(1)
	<-db.gate
	return db.Database.Get(key)
}

func TestArchiveNodeReadCoalescing(t *testing.T) {
	base := rawdb.NewMemoryDatabase()
	defer base.Close()
	key, value := bytes.Repeat([]byte{1}, 32), []byte{1, 2, 3}
	if err := base.Put(key, value); err != nil {
		t.Fatal(err)
	}
	blocked := &blockedArchiveReadDB{Database: base, gate: make(chan struct{})}
	db := &archiveNodeReads{Database: blocked, enabled: true}
	const workers = 32
	results := make([][]byte, workers)
	errs := make([]error, workers)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) { defer wg.Done(); results[i], errs[i] = db.Get(key) }(i)
	}
	deadline := time.Now().Add(5 * time.Second)
	for db.requests.Load() < workers && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	// Allow callers that incremented requests to enter singleflight. This test
	// asserts a reduction, not an exact scheduling-dependent backend count.
	time.Sleep(20 * time.Millisecond)
	close(blocked.gate)
	wg.Wait()
	if blocked.calls.Load() >= workers {
		t.Fatalf("no reads coalesced: %d", blocked.calls.Load())
	}
	for i := range results {
		if errs[i] != nil || !bytes.Equal(results[i], value) {
			t.Fatalf("result %d: %x %v", i, results[i], errs[i])
		}
	}
	results[0][0] = 99
	for i := 1; i < workers; i++ {
		if results[i][0] != 1 {
			t.Fatal("shared mutable buffer")
		}
	}
	before := blocked.calls.Load()
	if _, err := db.Get(key); err != nil {
		t.Fatal(err)
	}
	if blocked.calls.Load() != before+1 {
		t.Fatal("in-flight result retained as cache")
	}
	if db.reads.Load() != blocked.calls.Load() {
		t.Fatal("incorrect read count")
	}
}

type failedArchiveReadDB struct {
	ethdb.Database
	err error
}

func (db failedArchiveReadDB) Get([]byte) ([]byte, error) { return nil, db.err }

func TestArchiveNodeReadErrorsAndBypass(t *testing.T) {
	base := rawdb.NewMemoryDatabase()
	defer base.Close()
	want := errors.New("read failed")
	db := &archiveNodeReads{Database: failedArchiveReadDB{base, want}, enabled: true}
	for i := 0; i < 2; i++ {
		if _, err := db.Get(make([]byte, 32)); !errors.Is(err, want) {
			t.Fatalf("lost error: %v", err)
		}
	}
	if db.reads.Load() != 2 {
		t.Fatal("cached failed read")
	}
	if _, err := db.Get([]byte("metadata")); !errors.Is(err, want) {
		t.Fatal(err)
	}
	if db.requests.Load() != 2 {
		t.Fatal("metadata should bypass coalescing")
	}
}
