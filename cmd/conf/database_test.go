// Copyright 2021-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package conf

import (
	"fmt"
	"testing"

	"github.com/spf13/pflag"

	"github.com/ethereum/go-ethereum/core/rawdb"
)

func TestPersistentFreezerBatchConfig(t *testing.T) {
	for _, limit := range []uint64{0, 1, 256, rawdb.DefaultFreezerMaxBatchBlocks, rawdb.DefaultFreezerMaxBatchBlocks + 1} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			config := PersistentConfigDefault
			config.FreezerMaxBatchBlocks = limit
			err := config.Validate()
			if (err != nil) != (limit > rawdb.DefaultFreezerMaxBatchBlocks) {
				t.Fatalf("Validate(%d) = %v", limit, err)
			}
		})
	}
	flags := pflag.NewFlagSet("freezer-test", pflag.ContinueOnError)
	PersistentConfigAddOptions("persistent", flags)
	if err := flags.Parse([]string{"--persistent.freezer-max-batch-blocks=256"}); err != nil {
		t.Fatal(err)
	}
	if got, err := flags.GetUint64("persistent.freezer-max-batch-blocks"); err != nil || got != 256 {
		t.Fatalf("parsed freezer limit = %d, %v", got, err)
	}
}

func TestCompactionOutputPacingConfig(t *testing.T) {
	for _, value := range []int64{-1, 0, 1, 64 << 20} {
		config := PebbleConfigDefault
		config.CompactionOutputBytesPerSecond = value
		if err := config.Validate(); (err != nil) != (value < 0) {
			t.Fatalf("Validate(%d) = %v", value, err)
		}
		if value >= 0 && config.ExtraOptions("l2chaindata").CompactionOutputBytesPerSecond != value {
			t.Fatalf("pacing config was not forwarded: %d", value)
		}
		for _, namespace := range []string{"wasm", "arbitrumdata", "other", ""} {
			if config.ExtraOptions(namespace).CompactionOutputBytesPerSecond != 0 {
				t.Fatalf("pacing applied to %q", namespace)
			}
		}
	}
	flags := pflag.NewFlagSet("pacing-test", pflag.ContinueOnError)
	PebbleConfigAddOptions("persistent.pebble", flags, &PebbleConfigDefault)
	if err := flags.Parse([]string{"--persistent.pebble.compaction-output-bytes-per-second=67108864"}); err != nil {
		t.Fatal(err)
	}
	if value, err := flags.GetInt64("persistent.pebble.compaction-output-bytes-per-second"); err != nil || value != 64<<20 {
		t.Fatalf("parsed pacing rate = %d, %v", value, err)
	}
}
