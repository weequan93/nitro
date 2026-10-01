// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethexec

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/ethereum/go-ethereum/triedb"
	"github.com/holiman/uint256"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbos/burn"
	"github.com/offchainlabs/nitro/cmd/chaininfo"
)

// Capture hash-addressed reads using a cold state database, as the block
// recorder does. Proof verification below uses only these captured bytes.
type feeWitnessDB struct {
	ethdb.Database
	proof ethdb.Database
}

func (db *feeWitnessDB) Get(key []byte) ([]byte, error) {
	value, err := db.Database.Get(key)
	if err == nil && len(key) == common.HashLength && crypto.Keccak256Hash(value) == common.BytesToHash(key) {
		if err := db.proof.Put(key, value); err != nil {
			return nil, err
		}
	}
	return value, err
}

func TestLegacyFeeAccountPreimages(t *testing.T) {
	for _, name := range []string{"absent", "funded", "empty"} {
		funded := name == "funded"
		exists := name != "absent"
		t.Run(name, func(t *testing.T) {
			disk := rawdb.NewMemoryDatabase()
			defer disk.Close()
			trieDB := triedb.NewDatabase(disk, nil)
			defer trieDB.Close()
			sdb, err := state.New(types.EmptyRootHash, state.NewDatabase(trieDB, nil))
			if err != nil {
				t.Fatal(err)
			}
			arbState, err := arbosState.InitializeArbosState(sdb, burn.NewSystemBurner(nil, false), chaininfo.ArbitrumDevTestChainConfig(), nil, arbostypes.TestInitMessage)
			if err != nil {
				t.Fatal(err)
			}
			infra := common.HexToAddress("0x1234")
			network := common.HexToAddress("0x5678")
			if err := arbState.SetInfraFeeAccount(infra); err != nil {
				t.Fatal(err)
			}
			if err := arbState.SetNetworkFeeAccount(network); err != nil {
				t.Fatal(err)
			}
			accounts := []common.Address{infra, network, types.L1PricerFundsPoolAddress}
			if exists {
				for _, account := range accounts {
					sdb.CreateAccount(account)
					if funded {
						sdb.AddBalance(account, uint256.NewInt(123), tracing.BalanceChangeUnspecified)
					}
				}
			}
			// Retain empty accounts to detect accidental touches during recording.
			root, err := sdb.Commit(0, false, false)
			if err != nil {
				t.Fatal(err)
			}
			if err := trieDB.Commit(root, false); err != nil {
				t.Fatal(err)
			}
			proof := rawdb.NewMemoryDatabase()
			defer proof.Close()
			coldTrieDB := triedb.NewDatabase(&feeWitnessDB{Database: disk, proof: proof}, nil)
			defer coldTrieDB.Close()
			recorded, err := state.NewRecording(root, state.NewDatabase(coldTrieDB, nil))
			if err != nil {
				t.Fatal(err)
			}
			recorded.StartRecording()
			initial, err := arbosState.OpenSystemArbosState(recorded, nil, true)
			if err != nil {
				t.Fatal(err)
			}
			if funded {
				missing := false
				for _, account := range accounts {
					if _, err := trie.VerifyProof(root, crypto.Keccak256(account.Bytes()), proof); err != nil {
						missing = true
					}
				}
				if !missing {
					t.Fatal("fixture already contains all fee account proofs before supplementation")
				}
			}
			if err := recordLegacyFeeAccountPreimages(recorded, initial); err != nil {
				t.Fatal(err)
			}
			// Verify before computing IntermediateRoot or doing further account
			// reads, so those operations cannot fill gaps in the witness.
			for _, account := range accounts {
				value, err := trie.VerifyProof(root, crypto.Keccak256(account.Bytes()), proof)
				if err != nil {
					t.Fatalf("incomplete witness for %s: %v", account, err)
				}
				if (len(value) != 0) != exists {
					t.Fatalf("unexpected account existence for %s", account)
				}
			}
			for _, account := range accounts {
				if recorded.Exist(account) != exists {
					t.Fatalf("supplement created or removed account %s", account)
				}
			}
			if got := recorded.IntermediateRoot(true); got != root {
				t.Fatalf("supplement changed state root: got %s, want %s", got, root)
			}
		})
	}
}
