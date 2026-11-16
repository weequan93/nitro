// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package blockdiag

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime/pprof"
	"runtime/trace"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
)

var dropped = metrics.NewRegisteredCounter("arb/blockdiag/reports_dropped", nil)
var sequenceIDs atomic.Uint64

type report struct {
	Record
	started time.Time
}

type Manager struct {
	cfg     Config
	reports chan report
	sink    func(Record)
}

func New(cfg Config, sink func(Record)) (*Manager, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if !cfg.Enable {
		return nil, nil
	}
	if sink == nil {
		sink = func(r Record) {
			encoded, _ := json.Marshal(r)
			log.Warn("Sequencer block diagnostics", "seq", r.ID, "block", r.BlockNumber,
				"total_ms", r.TotalMS, "timings", string(encoded))
		}
	}
	return &Manager{cfg: cfg, reports: make(chan report, 32), sink: sink}, nil
}

func (m *Manager) Begin() *Trace {
	if m == nil {
		return nil
	}
	return newTrace(sequenceIDs.Add(1))
}

// Finish never waits for logging, tracing or disk IO, and is called after the
// sequencing mutex has been released. Full queues drop observations, not work.
func (m *Manager) Finish(t *Trace, outcome string, block, gas uint64, txs int) {
	if m == nil || t == nil || time.Since(t.started) < m.cfg.Threshold {
		return
	}
	r := report{Record: t.record(outcome, block, gas, txs), started: t.started}
	select {
	case m.reports <- r:
	default:
		dropped.Inc(1)
	}
}

// Run owns flight-recorder lifecycle and artifact IO. It must run once as an
// execution-engine child, and stop with the node's normal shutdown context.
func (m *Manager) Run(ctx context.Context) {
	if m == nil {
		return
	}
	var start, end *time.Timer
	var startC, endC <-chan time.Time
	if m.cfg.CaptureDuration > 0 {
		start = time.NewTimer(m.cfg.CaptureDelay)
		defer start.Stop()
		startC = start.C
	}
	var capture *captureSession
	defer func() {
		if end != nil {
			end.Stop()
		}
		if capture != nil {
			capture.close("node_shutdown")
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-startC:
			startC = nil
			var err error
			capture, err = startCapture(ctx, m.cfg)
			if err != nil {
				log.Warn("Sequencer diagnostics capture unavailable", "err", err)
				continue
			}
			end = time.NewTimer(m.cfg.CaptureDuration)
			endC = end.C
		case <-endC:
			endC = nil
			capture.close("duration_elapsed")
			capture = nil
		case r := <-m.reports:
			m.sink(r.Record)
			if capture == nil || r.started.Before(capture.started) ||
				time.Since(capture.lastSnapshot) < m.cfg.SnapshotInterval {
				continue
			}
			err := capture.snapshot(r.Record)
			result := map[string]any{"seq_id": r.ID, "saved": err == nil}
			if err != nil {
				result["error"] = err.Error()
				log.Warn("Sequencer diagnostics snapshot failed", "seq", r.ID, "err", err)
			} else {
				result["trace"] = fmt.Sprintf("seq-%d.trace", r.ID)
			}
			capture.results = append(capture.results, result)
			if capture.snapshots >= m.cfg.MaxSnapshots {
				capture.close("snapshot_limit")
				capture = nil
				endC = nil
				end.Stop()
			}
		}
	}
}

type fileBudget struct {
	mu   sync.Mutex
	used int64
	max  int64
}

type budgetWriter struct {
	mu      sync.Mutex
	ctx     context.Context
	file    *os.File
	budget  *fileBudget
	failed  atomic.Bool
	written int64
}

func (w *budgetWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.ctx.Err(); err != nil {
		w.failed.Store(true)
		return 0, err
	}
	if int64(len(p)) > (64<<20)-w.written {
		w.failed.Store(true)
		return 0, errors.New("diagnostics single artifact exceeds 64 MiB")
	}
	w.budget.mu.Lock()
	if int64(len(p)) > w.budget.max-w.budget.used {
		w.budget.mu.Unlock()
		w.failed.Store(true)
		return 0, errors.New("diagnostics artifact payload budget exceeded")
	}
	w.budget.used += int64(len(p))
	w.budget.mu.Unlock()
	n, err := w.file.Write(p)
	w.written += int64(n)
	if err != nil || n != len(p) {
		w.failed.Store(true)
	}
	return n, err
}

type captureSession struct {
	ctx          context.Context
	cfg          Config
	dir          string
	started      time.Time
	lastSnapshot time.Time
	snapshots    uint64
	results      []map[string]any
	recorder     *trace.FlightRecorder
	cpu          *budgetWriter
	budget       *fileBudget
}

func saveJSON(path string, value any) error {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	partial := path + ".partial"
	if err := os.WriteFile(partial, append(encoded, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(partial, path)
}

func startCapture(ctx context.Context, cfg Config) (*captureSession, error) {
	if err := os.MkdirAll(cfg.OutputDir, 0o700); err != nil {
		return nil, err
	}
	if err := spaceAvailable(cfg.OutputDir); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(cfg.OutputDir, "sequencer-diagnostics-")
	if err != nil {
		return nil, err
	}
	s := &captureSession{ctx: ctx, cfg: cfg, dir: dir, budget: &fileBudget{max: int64(cfg.MaxOutputMiB << 20)},
		recorder: trace.NewFlightRecorder(trace.FlightRecorderConfig{MinAge: cfg.TraceWindow, MaxBytes: cfg.TraceMaxMiB << 20})}
	if err := s.recorder.Start(); err != nil {
		return nil, err
	}
	if cfg.CPUProfile {
		file, err := os.OpenFile(filepath.Join(dir, "cpu.pprof"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			s.recorder.Stop()
			return nil, err
		}
		// StopCPUProfile must be allowed to flush its final buffers on shutdown.
		s.cpu = &budgetWriter{ctx: context.WithoutCancel(ctx), file: file, budget: s.budget}
		if err := pprof.StartCPUProfile(s.cpu); err != nil {
			s.cpu.file.Close()
			s.recorder.Stop()
			return nil, err
		}
	}
	s.started = time.Now()
	if err := saveJSON(filepath.Join(dir, "session.json"), map[string]any{
		"state": "capturing", "started_utc": s.started.UTC().Format(time.RFC3339Nano), "config": cfg,
		"note": "Capture changes observation overhead. Trace memory/age are runtime hints. File payloads are budgeted."}); err != nil {
		s.close("metadata_error")
		return nil, err
	}
	log.Info("Sequencer diagnostics capture started", "dir", dir, "duration", cfg.CaptureDuration)
	return s, nil
}

func (s *captureSession) snapshot(r Record) error {
	// Rate-limit attempts too, so filesystem failures cannot cause repeated flushes.
	s.lastSnapshot = time.Now()
	s.snapshots++
	if err := spaceAvailable(s.dir); err != nil {
		return err
	}
	name := filepath.Join(s.dir, fmt.Sprintf("seq-%d.trace", r.ID))
	file, err := os.OpenFile(name+".partial", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	writer := &budgetWriter{ctx: s.ctx, file: file, budget: s.budget}
	_, writeErr := s.recorder.WriteTo(writer)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil || writer.failed.Load() {
		os.Remove(name + ".partial")
		if err == nil {
			err = io.ErrShortWrite
		}
		return err
	}
	if err := os.Rename(name+".partial", name); err != nil {
		return err
	}
	if err := saveJSON(name+".json", r); err != nil {
		return err
	}
	log.Info("Sequencer diagnostic trace saved", "seq", r.ID, "trace", name)
	return nil
}

func (s *captureSession) close(reason string) {
	if s.cpu != nil {
		pprof.StopCPUProfile()
		if err := s.cpu.file.Close(); err != nil {
			s.cpu.failed.Store(true)
		}
	}
	s.recorder.Stop()
	s.budget.mu.Lock()
	used := s.budget.used
	s.budget.mu.Unlock()
	err := saveJSON(filepath.Join(s.dir, "session.json"), map[string]any{
		"state": "complete", "reason": reason, "started_utc": s.started.UTC().Format(time.RFC3339Nano),
		"finished_utc": time.Now().UTC().Format(time.RFC3339Nano), "snapshot_attempts": s.snapshots,
		"snapshots":              s.results,
		"payload_bytes_reserved": used, "cpu_profile_requested": s.cpu != nil,
		"cpu_profile_write_failed": s.cpu != nil && s.cpu.failed.Load(), "config": s.cfg,
		"note": "Check complete task boundaries in each trace. Running time is not exact CPU time; use CPU samples too."})
	if err != nil {
		log.Warn("Sequencer diagnostics session metadata failed", "err", err)
	}
	log.Info("Sequencer diagnostics capture finished", "dir", s.dir, "reason", reason)
}
