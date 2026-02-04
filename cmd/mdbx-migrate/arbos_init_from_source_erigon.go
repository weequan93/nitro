//go:build erigon
// +build erigon

package main

import (
	"fmt"
	"math/big"
	"sort"

	gethcommon "github.com/ethereum/go-ethereum/common"
	gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"
	gethstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/ethdb"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/burn"
	"github.com/offchainlabs/nitro/statetransfer"
)

func maybeFillAddressTableFromSource(initData *statetransfer.ArbosInitializationInfo, srcChain ethdb.Database, blockNum uint64) error {
	if initData == nil || srcChain == nil {
		return nil
	}
	if len(initData.AddressTableContents) != 0 {
		return nil
	}
	if blockNum == 0 {
		if head, ok := sourceHeadNumber(srcChain); ok {
			blockNum = head
		}
	}
	addrs, err := loadAddressTableFromSource(srcChain, blockNum)
	if err != nil {
		logKV("arbos_init_address_table", "status", "error", "block", blockNum, "err", err)
		return err
	}
	initData.AddressTableContents = addrs
	logKV("arbos_init_address_table", "status", "loaded", "block", blockNum, "count", len(addrs))
	return nil
}

func maybeFillAccountsFromSource(initData *statetransfer.ArbosInitializationInfo, srcChain ethdb.Database, blockNum uint64) error {
	if initData == nil || srcChain == nil {
		return nil
	}
	if len(initData.Accounts) != 0 {
		return nil
	}
	accounts, err := loadAccountsFromSource(srcChain, blockNum)
	if err != nil {
		logKV("arbos_init_accounts", "status", "error", "block", blockNum, "err", err)
		return err
	}
	initData.Accounts = accounts
	logKV("arbos_init_accounts", "status", "loaded", "block", blockNum, "count", len(accounts))
	return nil
}

func sourceHeadNumber(srcChain ethdb.Database) (uint64, bool) {
	headHash := gethrawdb.ReadHeadHeaderHash(srcChain)
	if headHash == (gethcommon.Hash{}) {
		headHash = gethrawdb.ReadHeadBlockHash(srcChain)
	}
	if headHash == (gethcommon.Hash{}) {
		return 0, false
	}
	headNum := gethrawdb.ReadHeaderNumber(srcChain, headHash)
	if headNum == nil {
		return 0, false
	}
	return *headNum, true
}

func loadAddressTableFromSource(srcChain ethdb.Database, blockNum uint64) ([]gethcommon.Address, error) {
	hash := gethrawdb.ReadCanonicalHash(srcChain, blockNum)
	if hash == (gethcommon.Hash{}) {
		return nil, fmt.Errorf("missing canonical hash for block %d", blockNum)
	}
	header := gethrawdb.ReadHeader(srcChain, hash, blockNum)
	if header == nil {
		return nil, fmt.Errorf("missing header for block %d", blockNum)
	}

	stateDB := gethstate.NewDatabase(srcChain)
	statedb, err := gethstate.New(header.Root, stateDB, nil)
	if err != nil {
		return nil, fmt.Errorf("open state at block %d: %w", blockNum, err)
	}

	arbState, err := arbosState.OpenArbosState(statedb, burn.NewSystemBurner(nil, false))
	if err != nil {
		return nil, fmt.Errorf("open arbos state at block %d: %w", blockNum, err)
	}

	addrTable := arbState.AddressTable()
	size, err := addrTable.Size()
	if err != nil {
		return nil, fmt.Errorf("address table size at block %d: %w", blockNum, err)
	}

	addrs := make([]gethcommon.Address, 0, size)
	for i := uint64(0); i < size; i++ {
		addr, ok, err := addrTable.LookupIndex(i)
		if err != nil {
			return nil, fmt.Errorf("address table lookup index %d: %w", i, err)
		}
		if !ok {
			return nil, fmt.Errorf("address table missing index %d", i)
		}
		addrs = append(addrs, addr)
	}

	return addrs, nil
}

func loadAccountsFromSource(srcChain ethdb.Database, blockNum uint64) ([]statetransfer.AccountInitializationInfo, error) {
	hash := gethrawdb.ReadCanonicalHash(srcChain, blockNum)
	if hash == (gethcommon.Hash{}) {
		return nil, fmt.Errorf("missing canonical hash for block %d", blockNum)
	}
	header := gethrawdb.ReadHeader(srcChain, hash, blockNum)
	if header == nil {
		return nil, fmt.Errorf("missing header for block %d", blockNum)
	}

	stateDB := gethstate.NewDatabase(srcChain)
	statedb, err := gethstate.New(header.Root, stateDB, nil)
	if err != nil {
		return nil, fmt.Errorf("open state at block %d: %w", blockNum, err)
	}

	dump := statedb.RawDump(&gethstate.DumpConfig{
		SkipCode:          false,
		SkipStorage:       false,
		OnlyWithAddresses: true,
	})
	if dump.Next != nil {
		return nil, fmt.Errorf("state dump incomplete at block %d", blockNum)
	}

	addrs := make([]string, 0, len(dump.Accounts))
	for addrStr := range dump.Accounts {
		addrs = append(addrs, addrStr)
	}
	sort.Strings(addrs)

	accounts := make([]statetransfer.AccountInitializationInfo, 0, len(addrs))
	for _, addrStr := range addrs {
		dumpAcc := dump.Accounts[addrStr]
		addr := gethcommon.HexToAddress(addrStr)

		balance := new(big.Int)
		if dumpAcc.Balance != "" {
			if _, ok := balance.SetString(dumpAcc.Balance, 10); !ok {
				return nil, fmt.Errorf("parse balance %q for %s", dumpAcc.Balance, addrStr)
			}
		}

		account := statetransfer.AccountInitializationInfo{
			Addr:       addr,
			Nonce:      dumpAcc.Nonce,
			EthBalance: balance,
		}

		if len(dumpAcc.Code) > 0 || len(dumpAcc.Storage) > 0 {
			contractStorage := make(map[gethcommon.Hash]gethcommon.Hash, len(dumpAcc.Storage))
			for key, value := range dumpAcc.Storage {
				contractStorage[key] = gethcommon.HexToHash(value)
			}
			account.ContractInfo = &statetransfer.AccountInitContractInfo{
				Code:            dumpAcc.Code,
				ContractStorage: contractStorage,
			}
		}

		accounts = append(accounts, account)
	}

	return accounts, nil
}
