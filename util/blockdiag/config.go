// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

// Package blockdiag implements optional sequencer observation. Reader timings
// include caches, decoding, locks and storage access; they are not disk timings.
package blockdiag

import (
	"fmt"
	"time"

	"github.com/spf13/pflag"
)

type Config struct {
	Enable           bool          `koanf:"enable"`
	Threshold        time.Duration `koanf:"threshold"`
	CaptureDelay     time.Duration `koanf:"capture-delay"`
	CaptureDuration  time.Duration `koanf:"capture-duration"`
	OutputDir        string        `koanf:"output-dir"`
	CPUProfile       bool          `koanf:"cpu-profile"`
	TraceWindow      time.Duration `koanf:"trace-window"`
	TraceMaxMiB      uint64        `koanf:"trace-max-mib"`
	MaxSnapshots     uint64        `koanf:"max-snapshots"`
	SnapshotInterval time.Duration `koanf:"snapshot-interval"`
	MaxOutputMiB     uint64        `koanf:"max-output-mib"`
}

var DefaultConfig = Config{
	Threshold: 500 * time.Millisecond, TraceWindow: 10 * time.Second,
	TraceMaxMiB: 32, MaxSnapshots: 3, SnapshotInterval: time.Minute, MaxOutputMiB: 256,
}

func (c Config) Validate() error {
	if !c.Enable {
		return nil
	}
	if c.Threshold <= 0 || c.CaptureDelay < 0 || c.CaptureDuration < 0 || c.CaptureDuration > 30*time.Minute {
		return fmt.Errorf("invalid block diagnostics threshold or capture duration (maximum 30m)")
	}
	if c.CaptureDuration == 0 {
		if c.CPUProfile {
			return fmt.Errorf("block diagnostics CPU profile requires a capture duration")
		}
		return nil
	}
	if c.OutputDir == "" || c.TraceWindow <= 0 || c.TraceWindow > time.Minute || c.TraceMaxMiB < 1 || c.TraceMaxMiB > 128 ||
		c.MaxSnapshots < 1 || c.MaxSnapshots > 10 || c.SnapshotInterval < 0 || c.MaxOutputMiB < 16 || c.MaxOutputMiB > 1024 {
		return fmt.Errorf("invalid block diagnostics capture directory or bounds")
	}
	return nil
}

func ConfigAddOptions(prefix string, f *pflag.FlagSet) {
	c := DefaultConfig
	f.Bool(prefix+".enable", c.Enable, "observe sequencer block phases and state-reader work")
	f.Duration(prefix+".threshold", c.Threshold, "minimum total sequencing duration for a block diagnostic record")
	f.Duration(prefix+".capture-delay", c.CaptureDelay, "delay before the optional bounded runtime capture")
	f.Duration(prefix+".capture-duration", c.CaptureDuration, "runtime capture window; 0 disables it, maximum 30m")
	f.String(prefix+".output-dir", c.OutputDir, "private directory for runtime capture artifacts")
	f.Bool(prefix+".cpu-profile", c.CPUProfile, "collect a labeled CPU profile during the runtime capture window")
	f.Duration(prefix+".trace-window", c.TraceWindow, "target age of runtime events retained for slow blocks")
	f.Uint64(prefix+".trace-max-mib", c.TraceMaxMiB, "runtime flight recorder size hint in MiB (not a hard memory bound)")
	f.Uint64(prefix+".max-snapshots", c.MaxSnapshots, "maximum slow-block trace snapshot attempts per process capture window")
	f.Duration(prefix+".snapshot-interval", c.SnapshotInterval, "minimum interval between runtime snapshots")
	f.Uint64(prefix+".max-output-mib", c.MaxOutputMiB, "combined file payload budget for the runtime capture")
}
