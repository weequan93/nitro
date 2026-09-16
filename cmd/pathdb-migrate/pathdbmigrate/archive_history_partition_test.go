// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package pathdbmigrate

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/trie/trienode"
	"github.com/ethereum/go-ethereum/triedb"
)

func TestArchivePartitionBoundaries(t *testing.T) {
	db := rawdb.NewMemoryDatabase()
	defer db.Close()
	tdb := triedb.NewDatabase(db, triedb.HashDefaults)
	defer tdb.Close()
	oldValues, newValues := map[common.Hash][]byte{}, map[common.Hash][]byte{}
	for i := 0; i < 256; i++ {
		// Include keys exactly on every partition boundary and at its upper edge.
		low, high := common.Hash{}, common.Hash{}
		low[0] = byte(i)
		for j := range high {
			high[j] = 255
		}
		high[0] = byte(i)
		oldValues[low] = bytes.Repeat([]byte{1}, 64)
		oldValues[high] = bytes.Repeat([]byte{2}, 64)
		newValues[high] = oldValues[high]
		if i%3 == 0 {
			newValues[low] = bytes.Repeat([]byte{3}, 64)
		} else if i%3 == 1 {
			newValues[low] = oldValues[low]
		}
		insert := low
		insert[31] = 1
		newValues[insert] = bytes.Repeat([]byte{4}, 64)
	}
	build := func(values map[common.Hash][]byte) common.Hash {
		tr := trie.NewEmpty(tdb)
		for k, v := range values {
			if err := tr.Update(k[:], v); err != nil {
				t.Fatal(err)
			}
		}
		root, nodes := tr.Commit(false)
		if err := tdb.Update(root, types.EmptyRootHash, 0, trienode.NewWithNodeSet(nodes), nil); err != nil {
			t.Fatal(err)
		}
		if err := tdb.Commit(root, false); err != nil {
			t.Fatal(err)
		}
		return root
	}
	oldRoot, newRoot := build(oldValues), build(newValues)
	open := func(root common.Hash) *trie.Trie {
		tr, err := trie.New(trie.TrieID(root), tdb)
		if err != nil {
			t.Fatal(err)
		}
		return tr
	}
	for _, roots := range [][2]common.Hash{{oldRoot, newRoot}, {newRoot, oldRoot}, {oldRoot, oldRoot}, {types.EmptyRootHash, newRoot}, {oldRoot, types.EmptyRootHash}} {
		for _, partitions := range []int{1, 2, 4, 8, 16} {
			t.Run(fmt.Sprintf("%s-%s/%d", roots[0].Hex()[:8], roots[1].Hex()[:8], partitions), func(t *testing.T) {
				want := map[common.Hash][]byte{}
				if err := forEachChangedLeaf(open(roots[0]), open(roots[1]), func(k common.Hash, v []byte) error { want[k] = v; return nil }); err != nil {
					t.Fatal(err)
				}
				got := map[common.Hash][]byte{}
				for p := 0; p < partitions; p++ {
					var lower, upper []byte
					if partitions > 1 {
						lower = []byte{byte(p * 256 / partitions)}
						if p+1 < partitions {
							upper = []byte{byte((p + 1) * 256 / partitions)}
						}
					}
					err := forEachChangedLeafRange(context.Background(), open(roots[0]), open(roots[1]), lower, upper, nil, func(k common.Hash, v []byte) error {
						if _, ok := got[k]; ok {
							t.Fatalf("duplicate boundary key %s", k)
						}
						if int(k[0])*partitions/256 != p {
							t.Fatalf("key %s escaped partition %d", k, p)
						}
						got[k] = v
						return nil
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				if len(got) != len(want) {
					t.Fatalf("count %d want %d", len(got), len(want))
				}
				for k, v := range want {
					gv, ok := got[k]
					if !ok || !bytes.Equal(v, gv) {
						t.Fatalf("wrong origin %s", k)
					}
				}
			})
		}
	}
	// Cancellation is checked during traversal, even when no changed leaf emits.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := forEachChangedLeafRange(ctx, open(oldRoot), open(oldRoot), nil, nil, nil, func(common.Hash, []byte) error { t.Fatal("callback on cancelled walk"); return nil }); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
	ctx, cancel = context.WithCancel(context.Background())
	calls := 0
	err := forEachChangedLeafRange(ctx, open(oldRoot), open(newRoot), nil, nil, nil, func(common.Hash, []byte) error { calls++; cancel(); return nil })
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("mid-walk cancellation: calls=%d err=%v", calls, err)
	}
}

func TestArchivePartitionConfig(t *testing.T) {
	for _, n := range []int{0, 3, 17, 32, -1} {
		cfg := DefaultConfig
		cfg.Src.ChainData = "/source"
		cfg.Dst.ChainData = "/destination"
		cfg.ArchiveHistory.Enable = true
		cfg.ArchiveHistory.SpillPartitions = n
		if cfg.Validate() == nil {
			t.Fatalf("accepted partitions=%d", n)
		}
	}
}

func TestArchivePartitionedEncoding(t *testing.T) {
	for _, parts := range []int{1, 2, 4, 8, 16} {
		t.Run(fmt.Sprint(parts), func(t *testing.T) {
			db := rawdb.NewMemoryDatabase()
			defer db.Close()
			tdb := triedb.NewDatabase(db, triedb.HashDefaults)
			defer tdb.Close()
			oldRoot, newRoot := buildArchiveHashStatePair(t, db, tdb)
			accounts, storages, _, _, err := archiveHistoryOrigins(db, tdb, oldRoot, newRoot)
			if err != nil {
				t.Fatal(err)
			}
			ai, si, ad, sd, err := encodeArchiveHistory(accounts, storages)
			if err != nil {
				t.Fatal(err)
			}
			cfg := DefaultConfig.ArchiveHistory
			cfg.SpillPartitions = parts
			cfg.SpillDirectory = t.TempDir()
			got, err := archiveHistoryOriginsSpilled(context.Background(), db, tdb, oldRoot, newRoot, cfg, "l2chaindata")
			if err != nil {
				t.Fatal(err)
			}
			for i, pair := range [][2][]byte{{ai, got.accountIndex}, {si, got.storageIndex}, {ad, got.accountData}, {sd, got.storageData}} {
				if !bytes.Equal(pair[0], pair[1]) {
					t.Fatalf("section %d differs", i)
				}
			}
		})
	}
}
