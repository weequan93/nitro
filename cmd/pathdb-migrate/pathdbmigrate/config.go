// Copyright 2023-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/spf13/pflag"

	"github.com/offchainlabs/nitro/cmd/conf"
	"github.com/offchainlabs/nitro/cmd/genericconf"
)

type DBConfig struct {
	ChainData string            `koanf:"chain-data"`
	Ancient   string            `koanf:"ancient"`
	DBEngine  string            `koanf:"db-engine"`
	Handles   int               `koanf:"handles"`
	Cache     int               `koanf:"cache"`
	Namespace string            `koanf:"namespace"`
	Pebble    conf.PebbleConfig `koanf:"pebble"`
}

var DBConfigDefaultSrc = DBConfig{
	DBEngine:  "",
	Handles:   conf.PersistentConfigDefault.Handles,
	Cache:     2048,
	Namespace: "pathdb-migrate-src/",
	Pebble:    conf.PebbleConfigDefault,
}

var DBConfigDefaultDst = DBConfig{
	DBEngine:  "",
	Handles:   conf.PersistentConfigDefault.Handles,
	Cache:     2048,
	Namespace: "pathdb-migrate-dst/",
	Pebble:    conf.PebbleConfigDefault,
}

func DBConfigAddOptions(prefix string, f *pflag.FlagSet, defaultConfig *DBConfig) {
	f.String(prefix+".chain-data", defaultConfig.ChainData, "path to l2chaindata database directory")
	f.String(prefix+".ancient", defaultConfig.Ancient, "path to ancient directory; defaults to <chain-data>/ancient")
	f.String(prefix+".db-engine", defaultConfig.DBEngine, "backing database implementation ('leveldb', 'pebble', or '' = auto-detect)")
	f.Int(prefix+".handles", defaultConfig.Handles, "number of files to be open simultaneously")
	f.Int(prefix+".cache", defaultConfig.Cache, "the capacity in megabytes of the data cache")
	f.String(prefix+".namespace", defaultConfig.Namespace, "metrics namespace")
	conf.PebbleConfigAddOptions(prefix+".pebble", f, &defaultConfig.Pebble)
}

func (c DBConfig) ancientPath() string {
	if c.Ancient != "" {
		return filepath.Clean(c.Ancient)
	}
	if c.ChainData == "" {
		return ""
	}
	return filepath.Join(c.ChainData, "ancient")
}

type Config struct {
	Src              DBConfig                        `koanf:"src"`
	Dst              DBConfig                        `koanf:"dst"`
	Block            string                          `koanf:"block"`
	AccountHistory   AccountHistoryConfig            `koanf:"account-history"`
	ArchiveHistory   ArchiveHistoryConfig            `koanf:"archive-history"`
	Migrate          bool                            `koanf:"migrate"`
	Verify           bool                            `koanf:"verify"`
	VerifyOnly       bool                            `koanf:"verify-only"`
	IgnoreUnfinished bool                            `koanf:"ignore-unfinished-conversion"`
	CleanupLegacy    bool                            `koanf:"cleanup-legacy-hash-state"`
	StrictCleanup    bool                            `koanf:"strict-cleanup"`
	Compact          bool                            `koanf:"compact"`
	DiscardSnapshot  bool                            `koanf:"discard-snapshot"`
	IdealBatchSize   int                             `koanf:"ideal-batch-size"`
	LogLevel         string                          `koanf:"log-level"`
	LogType          string                          `koanf:"log-type"`
	Metrics          bool                            `koanf:"metrics"`
	MetricsServer    genericconf.MetricsServerConfig `koanf:"metrics-server"`
}

type AccountHistoryConfig struct {
	Enable       bool     `koanf:"enable"`
	Addresses    []string `koanf:"addresses"`
	StartBlock   string   `koanf:"start-block"`
	EndBlock     string   `koanf:"end-block"`
	ResetHistory bool     `koanf:"reset-history"`
}

type ArchiveHistoryConfig struct {
	Enable           bool   `koanf:"enable"`
	StartBlock       string `koanf:"start-block"`
	EndBlock         string `koanf:"end-block"`
	ResetHistory     bool   `koanf:"reset-history"`
	RequirePreimages bool   `koanf:"require-preimages"`
	ProgressEvery    uint64 `koanf:"progress-every"`
}

var DefaultConfig = Config{
	Src:   DBConfigDefaultSrc,
	Dst:   DBConfigDefaultDst,
	Block: "latest",
	AccountHistory: AccountHistoryConfig{
		Enable:     false,
		StartBlock: "0",
		EndBlock:   "latest",
	},
	ArchiveHistory: ArchiveHistoryConfig{
		Enable:           false,
		StartBlock:       "0",
		EndBlock:         "latest",
		RequirePreimages: true,
		ProgressEvery:    1000,
	},
	Migrate:          false,
	Verify:           true,
	VerifyOnly:       false,
	IgnoreUnfinished: false,
	CleanupLegacy:    false,
	StrictCleanup:    false,
	Compact:          false,
	DiscardSnapshot:  true,
	IdealBatchSize:   100 * 1024 * 1024,
	LogLevel:         "INFO",
	LogType:          "plaintext",
	Metrics:          false,
	MetricsServer:    genericconf.MetricsServerConfigDefault,
}

func ConfigAddOptions(f *pflag.FlagSet) {
	DBConfigAddOptions("src", f, &DefaultConfig.Src)
	DBConfigAddOptions("dst", f, &DefaultConfig.Dst)
	f.String("block", DefaultConfig.Block, "state block to migrate ('latest' or block number)")
	f.Bool("account-history.enable", DefaultConfig.AccountHistory.Enable, "write targeted PathDB account history for known addresses without replaying blocks")
	f.StringSlice("account-history.addresses", DefaultConfig.AccountHistory.Addresses, "addresses to include in generated account history")
	f.String("account-history.start-block", DefaultConfig.AccountHistory.StartBlock, "first block for targeted account history")
	f.String("account-history.end-block", DefaultConfig.AccountHistory.EndBlock, "last block for targeted account history ('latest' or block number)")
	f.Bool("account-history.reset-history", DefaultConfig.AccountHistory.ResetHistory, "DANGEROUS: reset existing destination PathDB state history before writing account history")
	f.Bool("archive-history.enable", DefaultConfig.ArchiveHistory.Enable, "write full PathDB archive state history from hashdb without replaying blocks")
	f.String("archive-history.start-block", DefaultConfig.ArchiveHistory.StartBlock, "first block for full archive history")
	f.String("archive-history.end-block", DefaultConfig.ArchiveHistory.EndBlock, "last block for full archive history ('latest' or block number)")
	f.Bool("archive-history.reset-history", DefaultConfig.ArchiveHistory.ResetHistory, "DANGEROUS: reset existing destination PathDB state history before writing archive history")
	f.Bool("archive-history.require-preimages", DefaultConfig.ArchiveHistory.RequirePreimages, "require account key preimages when writing full archive history")
	f.Uint64("archive-history.progress-every", DefaultConfig.ArchiveHistory.ProgressEvery, "log archive-history progress every N blocks")
	f.Bool("migrate", DefaultConfig.Migrate, "write pathdb trie nodes and metadata into destination database")
	f.Bool("verify", DefaultConfig.Verify, "verify destination pathdb after migration")
	f.Bool("verify-only", DefaultConfig.VerifyOnly, "verify an existing pathdb destination without running migration")
	f.Bool("ignore-unfinished-conversion", DefaultConfig.IgnoreUnfinished, "allow --verify-only to open a destination with an unfinished conversion canary")
	f.Bool("cleanup-legacy-hash-state", DefaultConfig.CleanupLegacy, "after successful pathdb verification, delete legacy hash-scheme trie nodes from the destination")
	f.Bool("strict-cleanup", DefaultConfig.StrictCleanup, "after successful pathdb verification, delete legacy hash trie nodes and stale hashdb snapshot flat-state entries")
	f.Bool("compact", DefaultConfig.Compact, "compact the destination key-value database after migration or cleanup")
	f.Bool("discard-snapshot", DefaultConfig.DiscardSnapshot, "discard inherited snapshot root/generator metadata so pathdb rebuilds flat snapshots")
	f.Int("ideal-batch-size", DefaultConfig.IdealBatchSize, "ideal write batch size in bytes")
	f.String("log-level", DefaultConfig.LogLevel, "log level, valid values are CRIT, ERROR, WARN, INFO, DEBUG, TRACE")
	f.String("log-type", DefaultConfig.LogType, "log type (plaintext or json)")
	f.Bool("metrics", DefaultConfig.Metrics, "enable metrics")
	genericconf.MetricsServerAddOptions("metrics-server", f)
}

func (c *Config) Validate() error {
	if c.AccountHistory.Enable && c.ArchiveHistory.Enable {
		return errors.New("account-history.enable and archive-history.enable cannot both be set")
	}
	if c.AccountHistory.Enable {
		if c.Src.ChainData == "" {
			return errors.New("src.chain-data is required")
		}
		if c.Dst.ChainData == "" {
			return errors.New("dst.chain-data is required")
		}
		if len(c.AccountHistory.Addresses) == 0 {
			return errors.New("account-history.addresses is required")
		}
		if c.Migrate || c.VerifyOnly || c.CleanupLegacy || c.StrictCleanup || c.Compact {
			return errors.New("account-history.enable cannot be combined with migrate, verify-only, cleanup, or compact modes")
		}
	}
	if c.ArchiveHistory.Enable {
		if c.Src.ChainData == "" {
			return errors.New("src.chain-data is required")
		}
		if c.Dst.ChainData == "" {
			return errors.New("dst.chain-data is required")
		}
		if c.Migrate || c.VerifyOnly || c.CleanupLegacy || c.StrictCleanup || c.Compact {
			return errors.New("archive-history.enable cannot be combined with migrate, verify-only, cleanup, or compact modes")
		}
		if c.ArchiveHistory.ProgressEvery == 0 {
			return errors.New("archive-history.progress-every must be greater than 0")
		}
		if !c.ArchiveHistory.RequirePreimages {
			return errors.New("archive-history.require-preimages=false is not supported for full archive-compatible migration")
		}
	}
	if !c.VerifyOnly && c.Src.ChainData == "" {
		return errors.New("src.chain-data is required")
	}
	if (c.Migrate || c.VerifyOnly) && c.Dst.ChainData == "" {
		return errors.New("dst.chain-data is required when --migrate or --verify-only is set")
	}
	if c.CleanupLegacy && !c.Migrate && !c.VerifyOnly {
		return errors.New("cleanup-legacy-hash-state requires --migrate or --verify-only")
	}
	if c.StrictCleanup && !c.Migrate && !c.VerifyOnly {
		return errors.New("strict-cleanup requires --migrate or --verify-only")
	}
	if c.Compact && !c.Migrate && !c.VerifyOnly {
		return errors.New("compact requires --migrate or --verify-only")
	}
	if c.CleanupLegacy && c.Migrate && !c.Verify {
		return errors.New("cleanup-legacy-hash-state with --migrate requires --verify")
	}
	if c.StrictCleanup && c.Migrate && !c.Verify {
		return errors.New("strict-cleanup with --migrate requires --verify")
	}
	if c.IgnoreUnfinished && !c.VerifyOnly {
		return errors.New("ignore-unfinished-conversion is only allowed with --verify-only")
	}
	if c.Migrate {
		src, err := filepath.Abs(c.Src.ChainData)
		if err != nil {
			return fmt.Errorf("resolve src.chain-data: %w", err)
		}
		dst, err := filepath.Abs(c.Dst.ChainData)
		if err != nil {
			return fmt.Errorf("resolve dst.chain-data: %w", err)
		}
		if src == dst {
			return errors.New("src.chain-data and dst.chain-data must be different; migrate only into a copied destination")
		}
	}
	if c.IdealBatchSize <= 0 {
		return fmt.Errorf("invalid ideal-batch-size %d", c.IdealBatchSize)
	}
	return nil
}
