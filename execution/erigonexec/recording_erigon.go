//go:build erigon
// +build erigon

package erigonexec

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"

	ecommon "github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/common/length"
	"github.com/erigontech/erigon/core"
	estate "github.com/erigontech/erigon/core/state"
	"github.com/erigontech/erigon/core/vm"
	"github.com/erigontech/erigon/core/vm/evmtypes"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/dbutils"
	"github.com/erigontech/erigon/db/kv/membatchwithdb"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	dbstate "github.com/erigontech/erigon/db/state"
	"github.com/erigontech/erigon/execution/consensus"
	"github.com/erigontech/erigon/execution/consensus/ethash"
	"github.com/erigontech/erigon/execution/rlp"
	"github.com/erigontech/erigon/execution/stagedsync"
	witnesstypes "github.com/erigontech/erigon/execution/types/witness"
	etypes "github.com/erigontech/erigon/execution/types"
	"github.com/erigontech/erigon/execution/trie"
	"github.com/erigontech/erigon/rpc/rpchelper"
	"github.com/erigontech/erigon/turbo/services"
	"github.com/ethereum/go-ethereum/common"
	gstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/execution"
)

func (c *Client) recordBlockCreation(ctx context.Context, pos arbutil.MessageIndex, msg *arbostypes.MessageWithMetadata) (*execution.RecordResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if msg == nil || msg.Message == nil {
		return nil, errors.New("erigonexec: missing message for recording")
	}
	if c.chainDB == nil {
		return nil, errors.New("erigonexec: chain db not initialized")
	}
	if c.chainConfig == nil {
		return nil, errors.New("erigonexec: chain config not initialized")
	}

	blockNum := c.MessageIndexToBlockNumber(pos)
	if blockNum == 0 {
		return nil, errors.New("erigonexec: cannot record genesis block")
	}

	temporalDB, ok := c.chainDB.(kv.TemporalRwDB)
	if !ok {
		return nil, errors.New("erigonexec: chain db missing temporal support")
	}
	tx, err := temporalDB.BeginTemporalRw(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	header, err := c.blockReader.HeaderByNumber(ctx, tx, blockNum)
	if err != nil {
		return nil, err
	}
	if header == nil {
		return nil, fmt.Errorf("erigonexec: header not found block=%d", blockNum)
	}
	expectedDelayed := binary.BigEndian.Uint64(header.Nonce[:])
	if msg.DelayedMessagesRead != expectedDelayed {
		return nil, fmt.Errorf("erigonexec: delayed message read mismatch got=%d expected=%d", msg.DelayedMessagesRead, expectedDelayed)
	}

	block, err := c.blockReader.BlockByNumber(ctx, tx, blockNum)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, fmt.Errorf("erigonexec: block not found block=%d", blockNum)
	}

	prevHeader, err := c.blockReader.HeaderByNumber(ctx, tx, blockNum-1)
	if err != nil {
		return nil, err
	}
	if prevHeader == nil {
		return nil, fmt.Errorf("erigonexec: previous header not found block=%d", blockNum-1)
	}

	latestBlock, err := rpchelper.GetLatestBlockNumber(tx)
	if err != nil {
		return nil, err
	}
	if latestBlock < blockNum {
		return nil, fmt.Errorf("erigonexec: block number in future block=%d latest=%d", blockNum, latestBlock)
	}

	dirs, err := buildExecDirs(c.cfg.ChainDir)
	if err != nil {
		return nil, err
	}
	engine := ethash.NewFaker()
	defer engine.Close()

	cfg := stagedsync.StageWitnessCfg(true, 0, c.chainConfig, engine, c.blockReader, dirs)
	txBatch := membatchwithdb.NewMemoryBatch(tx, "", c.logger)
	defer txBatch.Rollback()

	if err := stagedsync.RewindStagesForWitness(txBatch, blockNum, latestBlock, &cfg, false, ctx, c.logger); err != nil {
		return nil, err
	}
	store, err := stagedsync.PrepareForWitness(txBatch, block, prevHeader.Root, &cfg, ctx, c.logger)
	if err != nil {
		return nil, err
	}

	userWasms, err := c.executeBlockForRecord(ctx, block, store, engine)
	if err != nil {
		return nil, err
	}

	touchedPlainKeys, touchedHashedKeys := store.Tds.GetTouchedPlainKeys()
	codeReads := store.Tds.BuildCodeTouches()

	domains, err := dbstate.NewSharedDomains(txBatch, c.logger)
	if err != nil {
		return nil, err
	}
	defer domains.Close()

	sdCtx := domains.GetCommitmentContext()
	for _, key := range touchedPlainKeys {
		sdCtx.TouchKey(kv.AccountsDomain, string(key), nil)
	}

	proofTrie, rootHash, err := sdCtx.Witness(ctx, codeReads, prevHeader.Root[:], "recordBlock")
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(rootHash, prevHeader.Root[:]) {
		return nil, fmt.Errorf("erigonexec: witness root mismatch actual(%x)!=expected(%x)", rootHash, prevHeader.Root[:])
	}

	preimages := make(map[common.Hash][]byte)
	if err := collectTriePreimages(proofTrie, touchedHashedKeys, preimages); err != nil {
		return nil, err
	}
	addCodePreimages(codeReads, preimages)
	if err := addHeaderPreimages(ctx, c.blockReader, tx, blockNum, preimages); err != nil {
		return nil, err
	}

	return &execution.RecordResult{
		Pos:       pos,
		BlockHash: toGethHash(block.Hash()),
		Preimages: preimages,
		UserWasms: convertUserWasms(userWasms),
	}, nil
}

func (c *Client) prepareForRecord(ctx context.Context, start, end arbutil.MessageIndex) error {
	if end < start {
		return fmt.Errorf("erigonexec: invalid range start=%d end=%d", start, end)
	}
	if c.chainDB == nil {
		return errors.New("erigonexec: chain db not initialized")
	}
	temporalDB, ok := c.chainDB.(kv.TemporalRoDB)
	if !ok {
		return errors.New("erigonexec: chain db missing temporal support")
	}
	tx, err := temporalDB.BeginTemporalRo(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for pos := start; pos <= end; pos++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		blockNum := c.MessageIndexToBlockNumber(pos)
		header, err := c.blockReader.HeaderByNumber(ctx, tx, blockNum)
		if err != nil {
			return err
		}
		if header == nil {
			return fmt.Errorf("erigonexec: header not found block=%d", blockNum)
		}
		if _, err := rpchelper.CreateHistoryStateReader(tx, blockNum, 0, rawdbv3.TxNums); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) executeBlockForRecord(ctx context.Context, block *etypes.Block, store *stagedsync.WitnessStore, engine consensus.EngineReader) (estate.UserWasms, error) {
	if block == nil {
		return nil, errors.New("erigonexec: missing block for recording")
	}
	if store == nil || store.TrieStateWriter == nil || store.Statedb == nil {
		return nil, errors.New("erigonexec: witness store not initialized")
	}
	header := block.Header()
	headerCopy := *header
	headerCopy.GasUsed = 0
	if header.BlobGasUsed != nil {
		zero := uint64(0)
		headerCopy.BlobGasUsed = &zero
	}

	ibs := store.Statedb
	ibs.SetWasmDB(c.wasmDBForCtx(ctx))
	ibsArb := estate.NewArbitrum(ibs)
	ibsArb.StartRecording()

	blockCtx := core.NewEVMBlockContext(&headerCopy, store.GetHashFn, engine, &headerCopy.Coinbase, c.chainConfig)
	rules := blockCtx.Rules(c.chainConfig)
	evm := vm.NewEVM(blockCtx, evmtypes.TxContext{}, ibs, c.chainConfig, vm.Config{})
	signer := *etypes.MakeSigner(c.chainConfig, headerCopy.Number.Uint64(), headerCopy.Time)
	gasPool := new(core.GasPool).AddGas(headerCopy.GasLimit)

	for i, tx := range block.Transactions() {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		ibs.SetTxContext(headerCopy.Number.Uint64(), i)
		msgForHook, err := tx.AsMessage(signer, headerCopy.BaseFee, rules)
		if err != nil {
			return nil, err
		}
		if evm.ProcessingHookSet.CompareAndSwap(false, true) {
			evm.ProcessingHook = arbos.NewTxProcessorIBS(evm, ibsArb, msgForHook)
		} else {
			evm.ProcessingHook.SetMessage(msgForHook, ibsArb)
		}
		_, _, err = core.ApplyArbTransactionVmenv(
			c.chainConfig,
			engine,
			gasPool,
			ibsArb,
			store.TrieStateWriter,
			&headerCopy,
			tx,
			&headerCopy.GasUsed,
			headerCopy.BlobGasUsed,
			vm.Config{},
			evm,
		)
		if err != nil {
			return nil, err
		}
	}

	if headerCopy.GasUsed != header.GasUsed {
		return nil, fmt.Errorf("erigonexec: gas used mismatch got=%d expected=%d", headerCopy.GasUsed, header.GasUsed)
	}
	if header.BlobGasUsed != nil && headerCopy.BlobGasUsed != nil && *headerCopy.BlobGasUsed != *header.BlobGasUsed {
		return nil, fmt.Errorf("erigonexec: blob gas used mismatch got=%d expected=%d", *headerCopy.BlobGasUsed, *header.BlobGasUsed)
	}

	return ibsArb.UserWasms(), nil
}

func collectTriePreimages(proofTrie *trie.Trie, touchedHashedKeys [][]byte, preimages map[common.Hash][]byte) error {
	if proofTrie == nil {
		return errors.New("erigonexec: witness trie is nil")
	}
	seen := make(map[string]struct{}, len(touchedHashedKeys))
	for _, key := range touchedHashedKeys {
		if len(key) == 0 {
			continue
		}
		keyStr := string(key)
		if _, ok := seen[keyStr]; ok {
			continue
		}
		seen[keyStr] = struct{}{}

		var proofKey []byte
		storage := false
		switch len(key) {
		case length.Hash:
			proofKey = key
		case length.Hash + length.Incarnation + length.Hash:
			addrHash, _, storageHash := dbutils.ParseCompositeStorageKey(key)
			proofKey = dbutils.GenerateCompositeTrieKey(addrHash, storageHash)
			storage = true
		default:
			return fmt.Errorf("erigonexec: unexpected hashed key length %d", len(key))
		}

		nodes, err := proofTrie.Prove(proofKey, 0, storage)
		if err != nil {
			return err
		}
		for _, node := range nodes {
			hash := crypto.Keccak256Hash(node)
			if _, ok := preimages[hash]; !ok {
				preimages[hash] = node
			}
		}
	}
	return nil
}

func addCodePreimages(codeReads map[ecommon.Hash]witnesstypes.CodeWithHash, preimages map[common.Hash][]byte) {
	for _, code := range codeReads {
		if len(code.Code) == 0 {
			continue
		}
		hash := toGethHash(code.CodeHash)
		if _, ok := preimages[hash]; !ok {
			preimages[hash] = code.Code
		}
	}
}

func addHeaderPreimages(ctx context.Context, reader services.FullBlockReader, tx kv.Getter, blockNum uint64, preimages map[common.Hash][]byte) error {
	if blockNum == 0 {
		return nil
	}
	start := uint64(0)
	if blockNum > 256 {
		start = blockNum - 256
	}
	for num := start; num < blockNum; num++ {
		header, err := reader.HeaderByNumber(ctx, tx, num)
		if err != nil {
			return err
		}
		if header == nil {
			return fmt.Errorf("erigonexec: header not found block=%d", num)
		}
		encoded, err := rlp.EncodeToBytes(header)
		if err != nil {
			return err
		}
		hash := toGethHash(header.Hash())
		if _, ok := preimages[hash]; !ok {
			preimages[hash] = encoded
		}
	}
	return nil
}

func convertUserWasms(userWasms estate.UserWasms) gstate.UserWasms {
	if len(userWasms) == 0 {
		return nil
	}
	out := make(gstate.UserWasms, len(userWasms))
	for hash, asmMap := range userWasms {
		converted := make(gstate.ActivatedWasm, len(asmMap))
		for target, asm := range asmMap {
			converted[ethdb.WasmTarget(target)] = asm
		}
		out[toGethHash(hash)] = converted
	}
	return out
}
