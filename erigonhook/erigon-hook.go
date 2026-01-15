//go:build erigon
// +build erigon

package erigonhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"

	gcommon "github.com/ethereum/go-ethereum/common"
	gcore "github.com/ethereum/go-ethereum/core"
	gvm "github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	gparams "github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"

	ecommon "github.com/erigontech/erigon-lib/common"
	estate "github.com/erigontech/erigon/core/state"
	evm "github.com/erigontech/erigon/core/vm"
	echain "github.com/erigontech/erigon/execution/chain"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/precompiles"
)

var (
	mdbxMigrateDebug   = os.Getenv("MDBX_MIGRATE_DEBUG") != ""
	arbOwnerErigonAddr = ecommon.HexToAddress("0x0000000000000000000000000000000000000070")
)

func logMdbxMigrateDebug(msg string, ctx ...interface{}) {
	if !mdbxMigrateDebug {
		return
	}
	log.Info(msg, ctx...)
}

type arbitrumPrecompileWrapper struct {
	inner precompiles.ArbosPrecompile
	name  string
}

func (p *arbitrumPrecompileWrapper) RequiredGas([]byte) uint64 {
	panic("non-advanced precompile method called")
}

func (p *arbitrumPrecompileWrapper) Run([]byte) ([]byte, error) {
	panic("non-advanced precompile method called")
}

func (p *arbitrumPrecompileWrapper) Name() string {
	return p.name
}

func (p *arbitrumPrecompileWrapper) RunAdvanced(
	input []byte,
	gasSupplied uint64,
	info *evm.AdvancedPrecompileCall,
) ([]byte, uint64, error) {
	if info == nil || info.Evm == nil {
		return nil, 0, errors.New("missing EVM for precompile")
	}
	gethEVM, restore, err := resolveGethEVM(info, input, gasSupplied)
	if err != nil {
		return nil, 0, err
	}
	if mdbxMigrateDebug && info.PrecompileAddress == arbOwnerErigonAddr {
		method := "unknown"
		if len(input) >= 4 {
			method = fmt.Sprintf("0x%x", input[:4])
		}
		var (
			topTxType interface{} = nil
			contracts interface{} = nil
		)
		if hook, ok := gethEVM.ProcessingHook.(*arbos.TxProcessor); ok {
			if hook.TopTxType != nil {
				topTxType = int(*hook.TopTxType)
			}
			contracts = len(hook.Contracts)
		}
		ergDepth := -1
		if info.Evm != nil && info.Evm.Interpreter() != nil {
			ergDepth = info.Evm.Interpreter().Depth()
		}
		logMdbxMigrateDebug(
			"erigon precompile context",
			"precompile", info.PrecompileAddress,
			"method", method,
			"caller", info.Caller,
			"acting_as", info.ActingAsAddress,
			"read_only", info.ReadOnly,
			"gas_supplied", gasSupplied,
			"erigon_depth", ergDepth,
			"geth_depth", gethEVM.Depth(),
			"hook", fmt.Sprintf("%T", gethEVM.ProcessingHook),
			"top_tx_type", topTxType,
			"contracts", contracts,
		)
	}
	origDepth := gethEVM.Depth()
	targetDepth := origDepth
	if info != nil && info.Evm != nil && info.Evm.Interpreter() != nil {
		targetDepth = info.Evm.Interpreter().Depth()
	}
	adjustDepth := func(from, to int) {
		switch {
		case to > from:
			for i := from; i < to; i++ {
				gethEVM.IncrementDepth()
			}
		case to < from:
			for i := from; i > to; i-- {
				gethEVM.DecrementDepth()
			}
		}
	}
	adjustDepth(origDepth, targetDepth)
	defer adjustDepth(targetDepth, origDepth)
	defer restore()
	return p.inner.Call(
		input,
		toGethAddress(info.PrecompileAddress),
		toGethAddress(info.ActingAsAddress),
		toGethAddress(info.Caller),
		info.Value,
		info.ReadOnly,
		gasSupplied,
		gethEVM,
	)
}

func init() {
	// Base Ethereum precompiles for Arbitrum and ArbOS30.
	for addr, precompile := range evm.PrecompiledContractsBerlin {
		evm.PrecompiledContractsArbitrum[addr] = precompile
		evm.PrecompiledAddressesArbitrum = append(evm.PrecompiledAddressesArbitrum, addr)
	}
	for addr, precompile := range evm.PrecompiledContractsCancun {
		evm.PrecompiledContractsArbOS30[addr] = precompile
		evm.PrecompiledAddressesArbOS30 = append(evm.PrecompiledAddressesArbOS30, addr)
	}

	// Arbitrum precompiles.
	for addr, precompile := range precompiles.Precompiles() {
		wrapped := &arbitrumPrecompileWrapper{
			inner: precompile,
			name:  "arb_precompile_" + addr.Hex(),
		}
		ergAddr := toErigonAddress(addr)
		evm.PrecompiledContractsArbOS30[ergAddr] = wrapped
		evm.PrecompiledAddressesArbOS30 = append(evm.PrecompiledAddressesArbOS30, ergAddr)
		if precompile.Precompile().ArbosVersion() < gparams.ArbosVersion_Stylus {
			evm.PrecompiledContractsArbitrum[ergAddr] = wrapped
			evm.PrecompiledAddressesArbitrum = append(evm.PrecompiledAddressesArbitrum, ergAddr)
		}
	}

	for addr, precompile := range evm.PrecompiledContractsArbitrum {
		evm.PrecompiledContractsArbOS30[addr] = precompile
		evm.PrecompiledAddressesArbOS30 = append(evm.PrecompiledAddressesArbOS30, addr)
	}
	for addr, precompile := range evm.PrecompiledContractsP256Verify {
		evm.PrecompiledContractsArbOS30[addr] = precompile
		evm.PrecompiledAddressesArbOS30 = append(evm.PrecompiledAddressesArbOS30, addr)
	}
}

// RequireHookedErigon does nothing, but forces an import to let init() run.
func RequireHookedErigon() {}

func buildGethEVM(
	evmInstance *evm.EVM,
	info *evm.AdvancedPrecompileCall,
	input []byte,
	gasSupplied uint64,
) (*gvm.EVM, error) {
	if evmInstance == nil {
		return nil, errors.New("missing erigon evm")
	}

	ibs := evmInstance.IntraBlockState()
	if ibs == nil {
		return nil, errors.New("missing intra-block state")
	}
	ibsArb := estate.NewArbitrum(ibs)
	stateDB := arbos.NewStateDBAdapter(ibsArb, evmInstance.ChainRules())

	chainCfg, err := toGethChainConfig(evmInstance.ChainConfig())
	if err != nil {
		return nil, err
	}

	baseFee := toBig(evmInstance.Context.BaseFee)
	baseFeeInBlock := toBig(evmInstance.Context.BaseFeeInBlock)
	if evmInstance.Config().NoBaseFee &&
		evmInstance.TxContext.GasPrice != nil &&
		evmInstance.TxContext.GasPrice.IsZero() &&
		baseFee.Sign() == 0 &&
		baseFeeInBlock.Sign() != 0 {
		baseFee = new(big.Int).Set(baseFeeInBlock)
	}

	blockCtx := gvm.BlockContext{
		CanTransfer: gcore.CanTransfer,
		Transfer:    gcore.Transfer,
		GetHash: func(n uint64) gcommon.Hash {
			hash, err := evmInstance.Context.GetHash(n)
			if err != nil {
				return gcommon.Hash{}
			}
			return toGethHash(hash)
		},
		Coinbase:       toGethAddress(evmInstance.Context.Coinbase),
		GasLimit:       evmInstance.Context.GasLimit,
		BlockNumber:    big.NewInt(int64(evmInstance.Context.BlockNumber)),
		Time:           evmInstance.Context.Time,
		BaseFee:        baseFee,
		BlobBaseFee:    toBig(evmInstance.Context.BlobBaseFee),
		Random:         toGethHashPtr(evmInstance.Context.PrevRanDao),
		ArbOSVersion:   evmInstance.Context.ArbOSVersion,
		BaseFeeInBlock: baseFeeInBlock,
	}
	if evmInstance.Context.Difficulty != nil {
		blockCtx.Difficulty = new(big.Int).Set(evmInstance.Context.Difficulty)
	}

	txCtx := gvm.TxContext{
		Origin:     toGethAddress(evmInstance.TxContext.Origin),
		GasPrice:   toBig(evmInstance.TxContext.GasPrice),
		BlobHashes: toGethHashes(evmInstance.TxContext.BlobHashes),
		BlobFeeCap: toBig(evmInstance.TxContext.BlobFee),
	}

	cfg := gvm.Config{
		NoBaseFee: evmInstance.Config().NoBaseFee,
		ExtraEips: evmInstance.Config().ExtraEips,
	}

	gethEVM := gvm.NewEVM(blockCtx, txCtx, stateDB, chainCfg, cfg)
	if info != nil {
		msg := buildArbosMessage(info, input, gasSupplied, txCtx)
		gethEVM.ProcessingHook = arbos.NewTxProcessor(gethEVM, msg)
	}
	return gethEVM, nil
}

func resolveGethEVM(
	info *evm.AdvancedPrecompileCall,
	input []byte,
	gasSupplied uint64,
) (*gvm.EVM, func(), error) {
	if info == nil || info.Evm == nil {
		return nil, nil, errors.New("missing EVM for precompile")
	}
	type gethContextProvider interface {
		GethEVM() *gvm.EVM
		GethMessage() *gcore.Message
	}
	if provider, ok := info.Evm.ProcessingHook.(gethContextProvider); ok {
		if gethEVM := provider.GethEVM(); gethEVM != nil {
			msg := provider.GethMessage()
			if mdbxMigrateDebug && info.PrecompileAddress == arbOwnerErigonAddr {
				logMdbxMigrateDebug(
					"erigon precompile uses geth evm",
					"precompile", info.PrecompileAddress,
					"caller", info.Caller,
					"acting_as", info.ActingAsAddress,
					"gas_supplied", gasSupplied,
					"has_msg", msg != nil,
					"hook", fmt.Sprintf("%T", gethEVM.ProcessingHook),
				)
			}
			if msg == nil {
				return gethEVM, func() {}, nil
			}
			prevHook := gethEVM.ProcessingHook
			if _, ok := prevHook.(*arbos.TxProcessor); ok {
				return gethEVM, func() {}, nil
			}
			gethEVM.ProcessingHook = arbos.NewTxProcessor(gethEVM, msg)
			restore := func() {
				gethEVM.ProcessingHook = prevHook
			}
			return gethEVM, restore, nil
		}
	}
	if mdbxMigrateDebug && info.PrecompileAddress == arbOwnerErigonAddr {
		logMdbxMigrateDebug(
			"erigon precompile building geth evm",
			"precompile", info.PrecompileAddress,
			"caller", info.Caller,
			"acting_as", info.ActingAsAddress,
			"gas_supplied", gasSupplied,
			"hook", fmt.Sprintf("%T", info.Evm.ProcessingHook),
		)
	}
	gethEVM, err := buildGethEVM(info.Evm, info, input, gasSupplied)
	if err != nil {
		return nil, nil, err
	}
	return gethEVM, func() {}, nil
}

func toGethChainConfig(cfg *echain.Config) (*gparams.ChainConfig, error) {
	if cfg == nil {
		return &gparams.ChainConfig{ChainID: big.NewInt(0)}, nil
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var out gparams.ChainConfig
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func toErigonAddress(addr gcommon.Address) ecommon.Address {
	return ecommon.BytesToAddress(addr.Bytes())
}

func toGethAddress(addr ecommon.Address) gcommon.Address {
	return gcommon.BytesToAddress(addr.Bytes())
}

func toGethHash(hash ecommon.Hash) gcommon.Hash {
	return gcommon.BytesToHash(hash.Bytes())
}

func toGethHashPtr(hash *ecommon.Hash) *gcommon.Hash {
	if hash == nil {
		return nil
	}
	out := toGethHash(*hash)
	return &out
}

func toGethHashes(hashes []ecommon.Hash) []gcommon.Hash {
	if len(hashes) == 0 {
		return nil
	}
	out := make([]gcommon.Hash, len(hashes))
	for i, hash := range hashes {
		out[i] = toGethHash(hash)
	}
	return out
}

func toBig(val *uint256.Int) *big.Int {
	if val == nil {
		return new(big.Int)
	}
	return val.ToBig()
}

func buildArbosMessage(
	info *evm.AdvancedPrecompileCall,
	input []byte,
	gasSupplied uint64,
	txCtx gvm.TxContext,
) *gcore.Message {
	gasPrice := new(big.Int)
	if txCtx.GasPrice != nil {
		gasPrice.Set(txCtx.GasPrice)
	}
	value := new(big.Int)
	if info.Value != nil {
		value.Set(info.Value)
	}
	to := toGethAddress(info.PrecompileAddress)
	return &gcore.Message{
		TxRunMode:  gcore.MessageReplayMode,
		To:         &to,
		From:       toGethAddress(info.Caller),
		Value:      value,
		GasLimit:   gasSupplied,
		GasPrice:   gasPrice,
		GasFeeCap:  new(big.Int).Set(gasPrice),
		GasTipCap:  new(big.Int).Set(gasPrice),
		Data:       append([]byte(nil), input...),
		AccessList: nil,
		BlobHashes: append([]gcommon.Hash(nil), txCtx.BlobHashes...),
		BlobGasFeeCap: func() *big.Int {
			if txCtx.BlobFeeCap == nil {
				return nil
			}
			return new(big.Int).Set(txCtx.BlobFeeCap)
		}(),
	}
}
