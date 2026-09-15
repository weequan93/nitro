// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
)

// The manifest is synced before any history is appended. Freezer records are
// authoritative on restart; root-to-ID mappings can lag them after a crash.
var archiveMigrationManifestKey = []byte("nitro-pathdb-migrate-archive-v1")

type archiveMigrationManifest struct {
	Version                       int
	Start, End                    uint64
	Genesis, InitialRoot, EndRoot common.Hash
	SkipMissingStates             bool
}

func prepareArchiveResume(ctx context.Context, src, dst ethdb.Database, freezer ethdb.AncientReaderOp, frozen uint64, manifest archiveMigrationManifest, resume bool) (uint64, common.Hash, error) {
	want, err := json.Marshal(manifest)
	if err != nil {
		return 0, common.Hash{}, err
	}
	has, err := dst.Has(archiveMigrationManifestKey)
	if err != nil {
		return 0, common.Hash{}, err
	}
	if resume && has {
		got, err := dst.Get(archiveMigrationManifestKey)
		if err != nil {
			return 0, common.Hash{}, err
		}
		if !bytes.Equal(got, want) {
			return 0, common.Hash{}, fmt.Errorf("archive resume configuration/source mismatch; use the original start-block, end-block and skip-missing-states setting")
		}
	} else if frozen != 0 {
		return 0, common.Hash{}, fmt.Errorf("cannot resume %d history records without a matching migration manifest; this history may predate resume support", frozen)
	}
	if resume && rawdb.ReadPersistentStateID(dst) > frozen {
		return 0, common.Hash{}, fmt.Errorf("destination persistent state ID exceeds retained history; refusing archive resume")
	}
	block, root := manifest.Start, manifest.InitialRoot
	// Validate the entire retained metadata chain before repairing mappings. This
	// deliberately trades a metadata scan for avoiding a second trie-diff run.
	for id := uint64(1); id <= frozen; id++ {
		if err := ctx.Err(); err != nil {
			return 0, common.Hash{}, err
		}
		meta := rawdb.ReadStateHistoryMeta(freezer, id)
		if len(meta) != historyMetaSize || meta[0] != 0 {
			return 0, common.Hash{}, fmt.Errorf("invalid archive history metadata at state ID %d", id)
		}
		parent := common.BytesToHash(meta[1:33])
		nextRoot := common.BytesToHash(meta[33:65])
		nextBlock := binary.BigEndian.Uint64(meta[65:])
		if nextBlock <= block || nextBlock > manifest.End || (id > 1 && parent != root) {
			return 0, common.Hash{}, fmt.Errorf("archive history chain mismatch at state ID %d", id)
		}
		if id == 1 && parent != root {
			// skip-missing-states may have selected a later initial anchor.
			if !manifest.SkipMissingStates {
				return 0, common.Hash{}, fmt.Errorf("archive initial parent mismatch")
			}
			found := false
			for b := manifest.Start; b < nextBlock; b++ {
				if err := ctx.Err(); err != nil {
					return 0, common.Hash{}, err
				}
				_, candidate, err := canonicalHeaderAndRoot(src, b)
				if err != nil {
					return 0, common.Hash{}, err
				}
				if candidate == parent {
					found = true
					break
				}
			}
			if !found {
				return 0, common.Hash{}, fmt.Errorf("archive initial anchor is not canonical in requested range")
			}
		}
		_, canonical, err := canonicalHeaderAndRoot(src, nextBlock)
		if err != nil {
			return 0, common.Hash{}, err
		}
		if canonical != nextRoot {
			return 0, common.Hash{}, fmt.Errorf("archive source root mismatch at block %d", nextBlock)
		}
		block, root = nextBlock, nextRoot
		if id%10000 == 0 {
			log.Info("Validating archive resume history", "stateID", id, "total", frozen, "block", block)
		}
	}
	if frozen != 0 && !archiveTrieRootAvailable(src, root) {
		return 0, common.Hash{}, fmt.Errorf("archive resume anchor at block %d is unavailable in source", block)
	}
	if err := dst.Put(archiveMigrationManifestKey, want); err != nil {
		return 0, common.Hash{}, err
	}
	if resume {
		if err := clearArchiveRootMappings(ctx, dst); err != nil {
			return 0, common.Hash{}, err
		}
	}
	// Replay mappings in bounded batches, including the initial anchor's ID 0.
	batch := dst.NewBatch()
	for id := uint64(1); id <= frozen; id++ {
		if err := ctx.Err(); err != nil {
			return 0, common.Hash{}, err
		}
		meta := rawdb.ReadStateHistoryMeta(freezer, id)
		if len(meta) != historyMetaSize {
			return 0, common.Hash{}, fmt.Errorf("archive metadata changed during resume at ID %d", id)
		}
		if id == 1 {
			rawdb.WriteStateID(batch, common.BytesToHash(meta[1:33]), 0)
		}
		rawdb.WriteStateID(batch, common.BytesToHash(meta[33:65]), id)
		if batch.ValueSize() >= 1024*1024 {
			if err := batch.Write(); err != nil {
				return 0, common.Hash{}, err
			}
			batch.Reset()
		}
		if id%10000 == 0 {
			log.Info("Rebuilding archive root mappings", "stateID", id, "total", frozen)
		}
	}
	if err := batch.Write(); err != nil {
		return 0, common.Hash{}, err
	}
	if err := dst.SyncKeyValue(); err != nil {
		return 0, common.Hash{}, err
	}
	return block, root, nil
}

// rawdb's stateIDPrefix is private: its schema is "L" + 32-byte state root.
// Remove only exact root lookup keys, including mappings to a lost crash tail.
// A crash during this repair is safe: the manifest and freezer permit replay.
func clearArchiveRootMappings(ctx context.Context, dst ethdb.Database) error {
	it := dst.NewIterator([]byte("L"), nil)
	defer it.Release()
	batch := dst.NewBatch()
	for it.Next() {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(it.Key()) != 1+common.HashLength {
			continue
		}
		if err := batch.Delete(it.Key()); err != nil {
			return err
		}
		if batch.ValueSize() >= 1024*1024 {
			if err := batch.Write(); err != nil {
				return err
			}
			batch.Reset()
		}
	}
	if err := it.Error(); err != nil {
		return err
	}
	return batch.Write()
}
