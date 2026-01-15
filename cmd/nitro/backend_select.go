package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/ethereum/go-ethereum/node"
	execbackend "github.com/offchainlabs/nitro/execution/backend"
	"github.com/offchainlabs/nitro/util/dbutil"
)

type dbPresence struct {
	mdbxDirs   []string
	pebbleDirs []string
	canaryDirs []string
}

func resolveBackendAndDBApply(cfg *NodeConfig, stackConf *node.Config) (execbackend.Kind, error) {
	backendKind, err := execbackend.ParseKind(cfg.Execution.Backend)
	if err != nil {
		return "", err
	}
	presence := detectDBPresence(cfg.Persistent.Chain)
	if len(presence.canaryDirs) > 0 {
		return "", fmt.Errorf("unfinished MDBX conversion detected in %s; rerun mdbx-migrate --resume or remove the canary", strings.Join(presence.canaryDirs, ", "))
	}

	if backendKind == execbackend.KindAuto {
		switch {
		case len(presence.mdbxDirs) > 0 && len(presence.pebbleDirs) == 0:
			if missing := missingMdbxDirs(cfg.Persistent.Chain); len(missing) > 0 {
				return "", fmt.Errorf("partial MDBX data found (missing: %s); run mdbx-migrate or set execution.backend=geth", strings.Join(missing, ", "))
			}
			backendKind = execbackend.KindErigon
		case len(presence.pebbleDirs) > 0 && len(presence.mdbxDirs) == 0:
			return "", fmt.Errorf("Pebble data found in %s; run mdbx-migrate or set execution.backend=geth", strings.Join(presence.pebbleDirs, ", "))
		case len(presence.mdbxDirs) > 0 && len(presence.pebbleDirs) > 0:
			return "", fmt.Errorf("mixed MDBX and Pebble data found (mdbx: %s, pebble: %s)", strings.Join(presence.mdbxDirs, ", "), strings.Join(presence.pebbleDirs, ", "))
		default:
			backendKind = execbackend.KindGeth
		}
	}

	stackDBEngine := cfg.Persistent.DBEngine
	if stackDBEngine == "auto" {
		stackDBEngine = ""
	}

	switch backendKind {
	case execbackend.KindGeth:
		if len(presence.mdbxDirs) > 0 {
			return "", fmt.Errorf("MDBX data found in %s; use execution.backend=erigon", strings.Join(presence.mdbxDirs, ", "))
		}
		if cfg.Persistent.DBEngine == "mdbx" {
			return "", fmt.Errorf("persistent.db-engine=mdbx is not supported with execution.backend=geth")
		}
	case execbackend.KindErigon:
		if cfg.Persistent.Ancient != "" {
			return "", fmt.Errorf("persistent.ancient is not supported with execution.backend=erigon")
		}
		if cfg.Persistent.DBEngine == "pebble" || cfg.Persistent.DBEngine == "leveldb" {
			return "", fmt.Errorf("persistent.db-engine=%s is not supported with execution.backend=erigon; use mdbx or auto", cfg.Persistent.DBEngine)
		}
		if len(presence.pebbleDirs) > 0 && len(presence.mdbxDirs) > 0 {
			return "", fmt.Errorf("mixed MDBX and Pebble data found (mdbx: %s, pebble: %s)", strings.Join(presence.mdbxDirs, ", "), strings.Join(presence.pebbleDirs, ", "))
		}
		if len(presence.pebbleDirs) > 0 {
			return "", fmt.Errorf("Pebble data found in %s; run mdbx-migrate before using execution.backend=erigon", strings.Join(presence.pebbleDirs, ", "))
		}
		if missing := missingMdbxDirs(cfg.Persistent.Chain); len(missing) > 0 {
			return "", fmt.Errorf("missing MDBX data in %s; run mdbx-migrate or use execution.backend=geth", strings.Join(missing, ", "))
		}
		if cfg.Persistent.DBEngine == "" || cfg.Persistent.DBEngine == "auto" {
			cfg.Persistent.DBEngine = "mdbx"
		}
		// Avoid passing an unsupported engine to the geth stack when selecting the erigon backend.
		if stackDBEngine == "mdbx" {
			stackDBEngine = ""
		}
	}

	stackConf.DBEngine = stackDBEngine

	return backendKind, nil
}

func detectDBPresence(chainDir string) dbPresence {
	subDirs := []string{"l2chaindata", "arbitrumdata", "wasm"}
	presence := dbPresence{}
	for _, sub := range subDirs {
		dir := filepath.Join(chainDir, sub)
		if dbutil.HasUnfinishedConversionCanaryFile(dir) {
			presence.canaryDirs = append(presence.canaryDirs, dir)
		}
		if hasFile(filepath.Join(dir, "mdbx.dat")) {
			presence.mdbxDirs = append(presence.mdbxDirs, dir)
		}
		if hasFile(filepath.Join(dir, "CURRENT")) {
			presence.pebbleDirs = append(presence.pebbleDirs, dir)
		}
	}
	return presence
}

func missingMdbxDirs(chainDir string) []string {
	subDirs := []string{"l2chaindata", "arbitrumdata", "wasm"}
	var missing []string
	for _, sub := range subDirs {
		dir := filepath.Join(chainDir, sub)
		if !hasFile(filepath.Join(dir, "mdbx.dat")) {
			missing = append(missing, dir)
		}
	}
	return missing
}

func hasFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}
