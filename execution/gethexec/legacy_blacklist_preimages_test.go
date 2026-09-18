// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethexec

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbos/burn"
	"github.com/offchainlabs/nitro/arbos/storage"
	"github.com/offchainlabs/nitro/cmd/chaininfo"
	"testing"
)

func TestLegacyBlacklistPreimages(t *testing.T) {
	for _, tc := range []struct{ scheduled, blocked bool }{{false, false}, {false, true}, {true, false}, {true, true}} {
		scheduled, blocked := tc.scheduled, tc.blocked
		name := "absent"
		if blocked {
			name = "blocked"
		}
		if scheduled {
			name = "scheduled/" + name
		}
		t.Run(name, func(t *testing.T) {
			disk := rawdb.NewMemoryDatabase()
			defer disk.Close()
			td := triedb.NewDatabase(disk, nil)
			defer td.Close()
			db, err := state.New(types.EmptyRootHash, state.NewDatabase(td, nil))
			if err != nil {
				t.Fatal(err)
			}
			a, err := arbosState.InitializeArbosState(db, burn.NewSystemBurner(nil, false), chaininfo.ArbitrumDevTestChainConfig(), nil, arbostypes.TestInitMessage)
			if err != nil {
				t.Fatal(err)
			}
			sender, to := common.HexToAddress("0x123456"), common.HexToAddress("0x987654")
			// Populate unrelated entries so absence proofs need nontrivial trie paths.
			for i := 0; i < 100; i++ {
				addr := common.BigToAddress(common.Big1)
				addr[18] = byte(i)
				if err := a.Blacklist().TxFromAddrs().Add(addr); err != nil {
					t.Fatal(err)
				}
				if err := a.Blacklist().TxToAddrs().Add(addr); err != nil {
					t.Fatal(err)
				}
			}
			if blocked {
				if err := a.Blacklist().TxFromAddrs().Add(sender); err != nil {
					t.Fatal(err)
				}
				if err := a.Blacklist().TxToAddrs().Add(to); err != nil {
					t.Fatal(err)
				}
			}
			slots := []common.Hash{}
			for i, addr := range []common.Address{sender, to} {
				sto := storage.NewGeth(db, burn.NewSystemBurner(nil, true)).OpenSubStorage([]byte{12}).OpenSubStorage([]byte{byte(i + 1)}).OpenSubStorage([]byte{0})
				slots = append(slots, sto.GetStorageSlot(common.BytesToHash(addr.Bytes())))
			}
			db.IntermediateRoot(true)
			storageRoot := db.GetStorageRoot(types.ArbosStateAddress)
			root, err := db.Commit(0, true, false)
			if err != nil {
				t.Fatal(err)
			}
			if err := td.Commit(root, false); err != nil {
				t.Fatal(err)
			}
			proof := rawdb.NewMemoryDatabase()
			defer proof.Close()
			cold := triedb.NewDatabase(&feeWitnessDB{Database: disk, proof: proof}, nil)
			defer cold.Close()
			recorded, err := state.NewRecording(root, state.NewDatabase(cold, nil))
			if err != nil {
				t.Fatal(err)
			}
			recorded.StartRecording()
			if _, err := arbosState.OpenSystemArbosState(recorded, nil, true); err != nil {
				t.Fatal(err)
			}
			gap := false
			for _, slot := range slots {
				if _, err := trie.VerifyProof(storageRoot, crypto.Keccak256(slot.Bytes()), proof); err != nil {
					gap = true
				}
			}
			if scheduled {
				if _, err := trie.VerifyProof(storageRoot, crypto.Keccak256(slots[1].Bytes()), proof); err == nil {
					t.Fatal("fixture already contains the scheduled recipient proof")
				}
			}
			if !gap {
				t.Fatal("fixture does not expose missing blacklist paths")
			}
			tx := types.NewTx(&types.LegacyTx{To: &to})
			// Even a blocked address must only be recorded, never reject the tx here.
			if scheduled {
				// Redeems never reach PreTxFilter. Their destination differs
				// from the scheduling transaction, and their sender must come
				// from the redeem, not the parent (manual redeem can differ).
				hooks := &legacyRecordingHooks{}
				redeem := types.NewTx(&types.ArbitrumRetryTx{From: sender, To: &to})
				result := &core.ExecutionResult{ScheduledTxes: types.Transactions{redeem}}
				parentTo := common.HexToAddress("0x6e")
				parent := types.NewTx(&types.LegacyTx{To: &parentTo})
				if err := hooks.PostTxFilter(nil, recorded, nil, parent, common.Address{}, 0, result); err != nil {
					t.Fatal(err)
				}
				if hooks.recordingError != nil {
					t.Fatal(hooks.recordingError)
				}
			} else if err := recordLegacyBlacklistPreimages(recorded, tx, sender); err != nil {
				t.Fatal(err)
			}
			for _, slot := range slots {
				value, err := trie.VerifyProof(storageRoot, crypto.Keccak256(slot.Bytes()), proof)
				if err != nil {
					t.Fatal(err)
				}
				if (len(value) != 0) != blocked {
					t.Fatalf("unexpected membership proof %x", value)
				}
			}
			// Contract creation has no recipient but still needs the sender read.
			if err := recordLegacyBlacklistPreimages(recorded, types.NewTx(&types.LegacyTx{}), sender); err != nil {
				t.Fatal(err)
			}
			if got := recorded.IntermediateRoot(true); got != root {
				t.Fatalf("state changed: %s != %s", got, root)
			}
		})
	}
}
