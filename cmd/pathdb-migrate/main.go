// Copyright 2023-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/spf13/pflag"

	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/metrics"
	"github.com/ethereum/go-ethereum/metrics/exp"

	"github.com/offchainlabs/nitro/cmd/genericconf"
	"github.com/offchainlabs/nitro/cmd/pathdb-migrate/pathdbmigrate"
	"github.com/offchainlabs/nitro/cmd/util/confighelpers"
)

func parseConfig(args []string) (*pathdbmigrate.Config, error) {
	f := pflag.NewFlagSet("pathdb-migrate", pflag.ContinueOnError)
	pathdbmigrate.ConfigAddOptions(f)
	k, err := confighelpers.BeginCommonParse(f, args)
	if err != nil {
		return nil, err
	}
	var config pathdbmigrate.Config
	if err := confighelpers.EndCommonParse(k, &config); err != nil {
		return nil, err
	}
	return &config, config.Validate()
}

func printSampleUsage(name string) {
	fmt.Printf("Sample usage: %s --src.chain-data /data/node/l2chaindata --dst.chain-data /data/node-path/l2chaindata --migrate\n\n", name)
}

func printProgress(m *pathdbmigrate.Migrator) {
	stats := m.Stats()
	if archive, ok := stats.ArchiveHistoryProgress(); ok {
		elapsed := stats.Elapsed()
		percent := float64(100)
		if archive.EndBlock > archive.StartBlock {
			percent = float64(archive.ProcessedBlocks) * 100 / float64(archive.EndBlock-archive.StartBlock)
		}
		coverage := float64(100)
		if checked := archive.AvailableBlocks + archive.SkippedBlocks; checked != 0 {
			coverage = float64(archive.AvailableBlocks) * 100 / float64(checked)
		}
		rate := float64(0)
		if elapsed > 0 {
			rate = float64(archive.ProcessedBlocks) / elapsed.Seconds()
		}
		eta := time.Duration(0)
		if rate > 0 && archive.EndBlock >= archive.CurrentBlock {
			eta = time.Duration(float64(archive.EndBlock-archive.CurrentBlock)/rate) * time.Second
		}
		fmt.Printf("Archive history progress:\n")
		fmt.Printf("\trange:\t%d -> %d\n", archive.StartBlock, archive.EndBlock)
		fmt.Printf("\tcurrent block:\t%d (%.2f%%)\n", archive.CurrentBlock, percent)
		fmt.Printf("\tprocessed blocks:\t%d\n", archive.ProcessedBlocks)
		fmt.Printf("\tavailable / skipped:\t%d / %d (coverage %.2f%%)\n", archive.AvailableBlocks, archive.SkippedBlocks, coverage)
		fmt.Printf("\thistory records / state ID:\t%d / %d\n", archive.Transitions, archive.StateID)
		fmt.Printf("\taccounts / storage slots:\t%d / %d\n", archive.Accounts, archive.StorageSlots)
		fmt.Printf("\tmissing preimages:\t%d\n", archive.MissingPreimages)
		fmt.Printf("\trate:\t%.2f blocks/s\n", rate)
		fmt.Printf("\telapsed / eta:\t%v / %v\n", elapsed, eta)
		return
	}
	elapsed := stats.Elapsed()
	totalNodes := stats.AccountNodes() + stats.StorageNodes()
	nodesPerSecond := float64(0)
	mbPerSecond := float64(0)
	if elapsed > 0 {
		nodesPerSecond = float64(totalNodes) / elapsed.Seconds()
		mbPerSecond = float64(stats.Bytes()) / (1024 * 1024) / elapsed.Seconds()
	}
	scheduled := stats.StorageJobsScheduled()
	completed := stats.StorageJobsCompleted()
	inFlight := uint64(0)
	if scheduled >= completed {
		inFlight = scheduled - completed
	}
	fmt.Printf("Progress:\n")
	fmt.Printf("\taccount nodes:\t%d\n", stats.AccountNodes())
	fmt.Printf("\taccount leaves:\t%d\n", stats.AccountLeaves())
	fmt.Printf("\tstorage tries:\t%d\n", stats.StorageTries())
	fmt.Printf("\tstorage nodes:\t%d\n", stats.StorageNodes())
	fmt.Printf("\tstorage leaves:\t%d\n", stats.StorageLeaves())
	fmt.Printf("\tstorage jobs scheduled / completed / in-flight:\t%d / %d / %d\n", scheduled, completed, inFlight)
	fmt.Printf("\tactive storage workers:\t%d\n", stats.ActiveStorageWorkers())
	fmt.Printf("\tprocessed MB:\t%d\n", stats.Bytes()/1024/1024)
	fmt.Printf("\trate:\t%.0f nodes/s / %.2f MB/s\n", nodesPerSecond, mbPerSecond)
	fmt.Printf("\tbatches:\t%d\n", stats.Batches())
	fmt.Printf("\tlegacy cleanup:\t%d nodes / %d MB\n", stats.LegacyNodes(), stats.LegacyBytes()/1024/1024)
	fmt.Printf("\tsnapshot cleanup:\t%d entries / %d MB\n", stats.SnapshotNodes(), stats.SnapshotBytes()/1024/1024)
	fmt.Printf("\telapsed:\t%v\n", elapsed)
}

func main() {
	config, err := parseConfig(os.Args[1:])
	if err != nil {
		confighelpers.PrintErrorAndExit(err, printSampleUsage)
	}
	if err = genericconf.InitLog(config.LogType, config.LogLevel, &genericconf.FileLoggingConfig{Enable: false}, nil); err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing logging: %v\n", err)
		os.Exit(1)
	}
	if config.Metrics {
		log.Info("Enabling metrics collection")
		metrics.Enable()
		go metrics.CollectProcessMetrics(config.MetricsServer.UpdateInterval)
		exp.Setup(fmt.Sprintf("%v:%v", config.MetricsServer.Addr, config.MetricsServer.Port))
	}

	migrator := pathdbmigrate.NewMigrator(config)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := time.NewTicker(10 * time.Second)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				printProgress(migrator)
			case <-ctx.Done():
				return
			}
		}
	}()

	if err := migrator.Run(ctx); err != nil {
		log.Error("Pathdb migration failed", "err", err)
		os.Exit(1)
	}
	printProgress(migrator)
}
