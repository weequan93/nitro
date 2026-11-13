// Copyright 2024-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethexec

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core/types"

	"github.com/offchainlabs/nitro/util/blockdiag"
)

// TestSequencerWrapperMutexReleasedOnPanic verifies that createBlocksMutex is
// properly released even when sequencerFunc panics. Without defer, a panic
// would bypass Unlock() and leave the mutex locked, causing a deadlock on the
// next call (e.g. after createBlock's recover() catches the panic).
func TestSequencerWrapperMutexReleasedOnPanic(t *testing.T) {
	engine := &ExecutionEngine{
		cachedL1PriceData: NewL1PriceData(),
	}

	// Mirrors what createBlock does: call into sequencerWrapper and recover.
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Error("expected a panic but got none")
			}
		}()
		_, _ = engine.sequencerWrapper(func() (*types.Block, error) {
			panic("simulated sequencer panic")
		})
	}()

	// The mutex must be unlocked after the panic is recovered upstream.
	if !engine.createBlocksMutex.TryLock() {
		t.Fatal("createBlocksMutex is still locked after panic recovery; would deadlock on next call")
	}
	engine.createBlocksMutex.Unlock()
}

func TestSequencerObservedMutexContentionAndPanicRelease(t *testing.T) {
	for _, panics := range []bool{false, true} {
		t.Run(map[bool]string{false: "error", true: "panic"}[panics], func(t *testing.T) {
			cfg := blockdiag.DefaultConfig
			cfg.Enable, cfg.Threshold = true, time.Nanosecond
			reports := make(chan blockdiag.Record, 1)
			manager, err := blockdiag.New(cfg, func(r blockdiag.Record) { reports <- r })
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() { manager.Run(ctx); close(done) }()
			defer func() { cancel(); <-done }()
			engine := &ExecutionEngine{cachedL1PriceData: NewL1PriceData()}
			engine.createBlocksMutex.Lock()
			go func() { time.Sleep(20 * time.Millisecond); engine.createBlocksMutex.Unlock() }()
			diag := manager.Begin()
			sentinel := errors.New("sequencing sentinel")
			func() {
				defer func() {
					got := recover()
					if (panics && got != sentinel) || (!panics && got != nil) {
						t.Errorf("panic changed: %v", got)
					}
				}()
				diag.Run(ctx, func() {
					_, err = engine.sequencerWrapperObserved(diag, func() (*types.Block, error) {
						if panics {
							panic(sentinel)
						}
						return nil, sentinel
					})
				})
			}()
			if !panics && err != sentinel {
				t.Fatalf("error changed: %v", err)
			}
			if !engine.createBlocksMutex.TryLock() {
				t.Fatal("observed wrapper leaked mutex")
			}
			engine.createBlocksMutex.Unlock()
			manager.Finish(diag, "test", 0, 0, 0)
			select {
			case r := <-reports:
				if r.Stages["engine_lock_wait"].MS < 10 {
					t.Fatalf("lock contention not observed: %+v", r)
				}
			case <-time.After(time.Second):
				t.Fatal("diagnostic report not emitted")
			}
		})
	}
}

// TestSequencerWrapperMutexReleasedOnSuccess verifies that normal (non-panic)
// returns also leave the mutex unlocked.
func TestSequencerWrapperMutexReleasedOnSuccess(t *testing.T) {
	engine := &ExecutionEngine{
		cachedL1PriceData: NewL1PriceData(),
	}

	sentinel := errors.New("stop retrying")
	_, err := engine.sequencerWrapper(func() (*types.Block, error) {
		return nil, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("unexpected error: %v", err)
	}

	if !engine.createBlocksMutex.TryLock() {
		t.Fatal("createBlocksMutex is still locked after normal return")
	}
	engine.createBlocksMutex.Unlock()
}
