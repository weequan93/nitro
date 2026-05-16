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
	fmt.Printf("Progress:\n")
	fmt.Printf("\taccount nodes:\t%d\n", stats.AccountNodes())
	fmt.Printf("\taccount leaves:\t%d\n", stats.AccountLeaves())
	fmt.Printf("\tstorage tries:\t%d\n", stats.StorageTries())
	fmt.Printf("\tstorage nodes:\t%d\n", stats.StorageNodes())
	fmt.Printf("\tstorage leaves:\t%d\n", stats.StorageLeaves())
	fmt.Printf("\tprocessed MB:\t%d\n", stats.Bytes()/1024/1024)
	fmt.Printf("\tbatches:\t%d\n", stats.Batches())
	fmt.Printf("\tlegacy cleanup:\t%d nodes / %d MB\n", stats.LegacyNodes(), stats.LegacyBytes()/1024/1024)
	fmt.Printf("\tsnapshot cleanup:\t%d entries / %d MB\n", stats.SnapshotNodes(), stats.SnapshotBytes()/1024/1024)
	fmt.Printf("\telapsed:\t%v\n", stats.Elapsed())
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
