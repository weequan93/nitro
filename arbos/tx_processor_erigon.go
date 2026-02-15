//go:build erigon
// +build erigon

package arbos

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"unsafe"

	"github.com/holiman/uint256"

	ecommon "github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon/arb/osver"
	estate "github.com/erigontech/erigon/core/state"
	evm "github.com/erigontech/erigon/core/vm"
	evmtypes "github.com/erigontech/erigon/core/vm/evmtypes"
	echain "github.com/erigontech/erigon/execution/chain"
	etypes "github.com/erigontech/erigon/execution/types"

	gcommon "github.com/ethereum/go-ethereum/common"
	gcore "github.com/ethereum/go-ethereum/core"
	gtypes "github.com/ethereum/go-ethereum/core/types"
	gvm "github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	gparams "github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/util"
)

type contractEntry struct {
	addr    ecommon.Address
	counted bool
}

type txProcessorIBS struct {
	evm *evm.EVM
	ibs estate.IntraBlockStateArbitrum

	gethState *stateDBAdapter
	gethEVM   *gvm.EVM
	arbosTx   *TxProcessor

	chainConfig *gparams.ChainConfig

	programCounts map[ecommon.Address]uint
	contractStack []contractEntry
}

// GethEVMProvider exposes the geth EVM used for ArbOS hooks in Erigon mode.
// It lets precompiles reuse the exact tx context constructed for execution.
type GethEVMProvider interface {
	GethEVM() *gvm.EVM
}

func NewTxProcessorIBS(evmInstance *evm.EVM, ibs estate.IntraBlockStateArbitrum, msg *etypes.Message) evm.TxProcessingHook {
	p := &txProcessorIBS{
		evm:           evmInstance,
		ibs:           ibs,
		programCounts: make(map[ecommon.Address]uint),
	}
	p.SetMessage(msg, ibs)
	return p
}

func (p *txProcessorIBS) GethEVM() *gvm.EVM {
	return p.gethEVM
}

func (p *txProcessorIBS) GethMessage() *gcore.Message {
	if p.arbosTx == nil {
		return nil
	}
	return p.arbosTx.msg
}

func (p *txProcessorIBS) PosterFee() *big.Int {
	if p.arbosTx == nil || p.arbosTx.PosterFee == nil {
		return nil
	}
	return new(big.Int).Set(p.arbosTx.PosterFee)
}

func (p *txProcessorIBS) CurrentRetryable() *gcommon.Hash {
	if p.arbosTx == nil || p.arbosTx.CurrentRetryable == nil {
		return nil
	}
	hash := *p.arbosTx.CurrentRetryable
	return &hash
}

func (p *txProcessorIBS) CurrentRefundTo() *gcommon.Address {
	if p.arbosTx == nil || p.arbosTx.CurrentRefundTo == nil {
		return nil
	}
	addr := *p.arbosTx.CurrentRefundTo
	return &addr
}

func (p *txProcessorIBS) TopTxType() *byte {
	if p.arbosTx == nil || p.arbosTx.TopTxType == nil {
		return nil
	}
	value := *p.arbosTx.TopTxType
	return &value
}

func (p *txProcessorIBS) Contracts() []*gvm.Contract {
	if p.arbosTx == nil || len(p.arbosTx.Contracts) == 0 {
		return nil
	}
	out := make([]*gvm.Contract, len(p.arbosTx.Contracts))
	copy(out, p.arbosTx.Contracts)
	return out
}

func (p *txProcessorIBS) Programs() map[gcommon.Address]uint {
	if p.arbosTx == nil || len(p.arbosTx.Programs) == 0 {
		return nil
	}
	out := make(map[gcommon.Address]uint, len(p.arbosTx.Programs))
	for addr, count := range p.arbosTx.Programs {
		out[addr] = count
	}
	return out
}

func (p *txProcessorIBS) SetMessage(msg *etypes.Message, ibs evmtypes.IntraBlockState) {
	if ibs != nil {
		if arbIBS, ok := ibs.(estate.IntraBlockStateArbitrum); ok {
			p.ibs = arbIBS
		}
	}

	p.programCounts = make(map[ecommon.Address]uint)
	p.contractStack = p.contractStack[:0]

	if p.evm == nil || p.ibs == nil {
		p.arbosTx = nil
		return
	}

	p.gethState = newStateDBAdapter(p.ibs, p.evm.ChainRules())
	chainCfg, err := loadGethChainConfig(p.gethState, p.evm.ChainConfig())
	if err != nil {
		log.Warn("arbos: failed to load chain config, using default", "err", err)
		chainCfg = &gparams.ChainConfig{ChainID: big.NewInt(0)}
	}
	p.chainConfig = chainCfg

	p.gethEVM = buildGethEVM(p.evm, p.gethState, chainCfg)
	gethMsg, err := toGethMessage(msg)
	if err != nil {
		log.Warn("arbos: failed to convert message", "err", err)
		p.arbosTx = nil
		return
	}
	txHash := gcommon.Hash{}
	txType := uint8(0)
	runMode := ""
	if msg != nil {
		runMode = fmt.Sprintf("%v", msg.TxRunMode)
		if msg.Tx != nil {
			txHash = toGethHash(msg.Tx.Hash())
			txType = msg.Tx.Type()
		}
	}
	if p.gethState != nil {
		p.gethState.SetDebugContext(p.evm.Context.BlockNumber, txHash, txType, runMode)
	}
	if gethMsg == nil {
		p.arbosTx = nil
		return
	}
	p.arbosTx = NewTxProcessor(p.gethEVM, gethMsg)
	p.gethEVM.ProcessingHook = p.arbosTx
}

func (p *txProcessorIBS) IsArbitrum() bool { return true }

func (p *txProcessorIBS) StartTxHook() (bool, uint64, error, []byte) {
	if p.arbosTx == nil {
		return false, 0, nil, nil
	}
	return p.arbosTx.StartTxHook()
}

func (p *txProcessorIBS) GasChargingHook(gasRemaining *uint64) (ecommon.Address, error) {
	if p.arbosTx == nil {
		if p.evm != nil {
			return p.evm.Context.Coinbase, nil
		}
		return ecommon.Address{}, nil
	}
	addr, err := p.arbosTx.GasChargingHook(gasRemaining)
	return toErigonAddress(addr), err
}

func (p *txProcessorIBS) ForceRefundGas() uint64 {
	if p.arbosTx == nil {
		return 0
	}
	return p.arbosTx.ForceRefundGas()
}

func (p *txProcessorIBS) NonrefundableGas() uint64 {
	if p.arbosTx == nil {
		return 0
	}
	return p.arbosTx.NonrefundableGas()
}

func (p *txProcessorIBS) DropTip() bool {
	if p.arbosTx == nil {
		return false
	}
	return p.arbosTx.DropTip()
}

func (p *txProcessorIBS) EndTxHook(totalGasUsed uint64, evmSuccess bool) {
	if p.arbosTx == nil {
		return
	}
	p.arbosTx.EndTxHook(totalGasUsed, evmSuccess)

	// Extra debug for bad-root investigation: log key ArbOS pricing fields after each tx.
	if internalTxDebug && p.gethState != nil {
		if arbState, err := arbosState.OpenSystemArbosState(p.gethState, nil, true); err == nil {
			l1p := arbState.L1PricingState()
			l1Fees, _ := l1p.L1FeesAvailable()
			units, _ := l1p.UnitsSinceUpdate()
			lastUpdate, _ := l1p.LastUpdateTime()
			l1BlockNum, _ := arbState.Blockhashes().L1BlockNumber()
			l1PricePerUnit, _ := l1p.PricePerUnit()
			l1PerUnitReward, _ := l1p.PerUnitReward()
			l2p := arbState.L2PricingState()
			l2Backlog, _ := l2p.GasBacklog()
			l2BaseFee, _ := l2p.BaseFeeWei()
			l2MinBaseFee, _ := l2p.MinBaseFeeWei()
			l2Inertia, _ := l2p.PricingInertia()
			arbosStorageRoot := p.gethState.GetStorageRoot(gcommon.HexToAddress("0xA4B05FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"))
			var txHash gcommon.Hash
			if p.arbosTx != nil && p.arbosTx.msg != nil && p.arbosTx.msg.Tx != nil {
				txHash = p.arbosTx.msg.Tx.Hash()
			}
			log.Warn("arbos tx end state",
				"block_number", p.evm.Context.BlockNumber,
				"tx_hash", txHash,
				"arbos_version", arbState.ArbOSVersion(),
				"l1_fees_available", l1Fees,
				"units_since_update", units,
				"last_update_time", lastUpdate,
				"l1_block_number", l1BlockNum,
				"l1_price_per_unit", l1PricePerUnit,
				"l1_per_unit_reward", l1PerUnitReward,
				"l2_backlog", l2Backlog,
				"l2_base_fee", l2BaseFee,
				"l2_min_base_fee", l2MinBaseFee,
				"l2_pricing_inertia", l2Inertia,
				"arbos_storage_root", arbosStorageRoot,
				"gas_used", totalGasUsed,
				"evm_success", evmSuccess,
			)
		}
	}
}

func (p *txProcessorIBS) ScheduledTxes() etypes.Transactions {
	if p.arbosTx == nil {
		return nil
	}
	gethTxs := p.arbosTx.ScheduledTxes()
	if len(gethTxs) == 0 {
		return nil
	}
	out := make(etypes.Transactions, 0, len(gethTxs))
	for _, tx := range gethTxs {
		ergTx, err := toErigonTx(tx)
		if err != nil {
			log.Warn("arbos: failed to convert scheduled tx", "err", err)
			continue
		}
		out = append(out, ergTx)
	}
	return out
}

func (p *txProcessorIBS) L1BlockNumber(_ evmtypes.BlockContext) (uint64, error) {
	if p.arbosTx == nil || p.gethEVM == nil {
		return 0, nil
	}
	return p.arbosTx.L1BlockNumber(p.gethEVM.Context)
}

func (p *txProcessorIBS) L1BlockHash(_ evmtypes.BlockContext, l1BlockNumber uint64) (ecommon.Hash, error) {
	if p.arbosTx == nil || p.gethEVM == nil {
		return ecommon.Hash{}, nil
	}
	hash, err := p.arbosTx.L1BlockHash(p.gethEVM.Context, l1BlockNumber)
	return toErigonHash(hash), err
}

func (p *txProcessorIBS) GasPriceOp(_ *evm.EVM) *uint256.Int {
	if p.arbosTx == nil || p.gethEVM == nil {
		return new(uint256.Int)
	}
	return toUint256(p.arbosTx.GasPriceOp(p.gethEVM))
}

func (p *txProcessorIBS) FillReceiptInfo(receipt *etypes.Receipt) {
	if p.arbosTx == nil || receipt == nil {
		return
	}
	gethReceipt := &gtypes.Receipt{}
	p.arbosTx.FillReceiptInfo(gethReceipt)
	receipt.GasUsedForL1 = gethReceipt.GasUsedForL1
}

func (p *txProcessorIBS) MsgIsNonMutating() bool {
	if p.arbosTx == nil {
		return false
	}
	return p.arbosTx.MsgIsNonMutating()
}

func (p *txProcessorIBS) ExecuteWASM(scope *evm.ScopeContext, input []byte, interpreter *evm.EVMInterpreter) ([]byte, error) {
	if p.arbosTx == nil || p.gethEVM == nil || p.gethState == nil {
		return nil, errors.New("arbos: missing geth context for wasm execution")
	}
	if scope == nil || scope.Contract == nil {
		return nil, errors.New("arbos: missing scope contract for wasm execution")
	}

	var tracingInfo *util.TracingInfo
	if p.gethEVM.Config.Tracer != nil {
		tracingInfo = util.NewTracingInfo(p.gethEVM, toGethAddress(scope.Contract.Caller()), toGethAddress(scope.Contract.Address()), util.TracingDuringEVM)
	}

	gethContract := toGethContract(scope.Contract, input)
	gethScope := &gvm.ScopeContext{Contract: gethContract}
	gethInterpreter := gvm.NewEVMInterpreter(p.gethEVM)
	setGethInterpreterReadOnly(gethInterpreter, interpreter != nil && interpreter.ReadOnly())

	arbState, err := arbosState.OpenSystemArbosState(p.gethState, tracingInfo, false)
	if err != nil {
		return nil, err
	}

	reentrant := p.programCounts[scope.Contract.Address()] > 1
	ret, err := arbState.Programs().CallProgram(
		gethScope,
		p.gethState,
		arbState.ArbOSVersion(),
		gethInterpreter,
		tracingInfo,
		input,
		reentrant,
		p.arbosTx.RunMode(),
	)

	scope.Contract.Gas = gethContract.Gas
	if interpreter != nil {
		interpreter.SetReturnData(ret)
	}
	return ret, err
}

func (p *txProcessorIBS) PushContract(contract *evm.Contract) {
	if contract == nil {
		return
	}
	if p.arbosTx != nil && p.gethEVM != nil {
		gethContract := toGethContract(contract, contract.Input)
		p.arbosTx.PushContract(gethContract)
	}
	counted := !contract.IsDelegateOrCallcode()
	entry := contractEntry{
		addr:    contract.Address(),
		counted: counted,
	}
	p.contractStack = append(p.contractStack, entry)
	if counted {
		p.programCounts[entry.addr]++
	}
}

func (p *txProcessorIBS) PopContract() {
	if len(p.contractStack) == 0 {
		return
	}
	if p.arbosTx != nil {
		p.arbosTx.PopContract()
	}
	last := p.contractStack[len(p.contractStack)-1]
	p.contractStack = p.contractStack[:len(p.contractStack)-1]
	if last.counted {
		if count := p.programCounts[last.addr]; count > 0 {
			p.programCounts[last.addr] = count - 1
		}
	}
}

func (p *txProcessorIBS) IsCalldataPricingIncreaseEnabled() bool {
	if p.gethState == nil {
		return true
	}
	version := arbosState.ArbOSVersion(p.gethState)
	return version >= osver.ArbosVersion_40
}

func buildGethEVM(evmInstance *evm.EVM, stateDB gvm.StateDB, chainCfg *gparams.ChainConfig) *gvm.EVM {
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
			if evmInstance == nil {
				return gcommon.Hash{}
			}
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
		Difficulty:     evmInstance.Context.Difficulty,
		BaseFee:        baseFee,
		BlobBaseFee:    toBig(evmInstance.Context.BlobBaseFee),
		Random:         toGethHashPtr(evmInstance.Context.PrevRanDao),
		ArbOSVersion:   evmInstance.Context.ArbOSVersion,
		BaseFeeInBlock: baseFeeInBlock,
	}
	txCtx := gvm.TxContext{
		Origin:     toGethAddress(evmInstance.TxContext.Origin),
		GasPrice:   toBig(evmInstance.TxContext.GasPrice),
		BlobHashes: toGethHashes(evmInstance.TxContext.BlobHashes),
		BlobFeeCap: toBig(evmInstance.TxContext.BlobFee),
	}
	// Keep geth hook execution on canonical base-fee semantics so ArbOS system
	// tx state updates (e.g. blockhash/l1 pricing) match geth-based execution.
	cfg := gvm.Config{
		NoBaseFee: false,
		ExtraEips: evmInstance.Config().ExtraEips,
	}
	return gvm.NewEVM(blockCtx, txCtx, stateDB, chainCfg, cfg)
}

func loadGethChainConfig(stateDB gvm.StateDB, erigonCfg *echain.Config) (*gparams.ChainConfig, error) {
	if stateDB != nil {
		arbState, err := arbosState.OpenSystemArbosState(stateDB, nil, true)
		if err == nil {
			raw, err := arbState.ChainConfig()
			if err == nil && len(raw) > 0 {
				var cfg gparams.ChainConfig
				if err := json.Unmarshal(raw, &cfg); err == nil {
					return &cfg, nil
				}
			}
		}
	}
	if erigonCfg != nil {
		raw, err := json.Marshal(erigonCfg)
		if err != nil {
			return nil, err
		}
		var cfg gparams.ChainConfig
		if err := json.Unmarshal(raw, &cfg); err != nil {
			return nil, err
		}
		return &cfg, nil
	}
	return nil, errors.New("arbos: chain config unavailable")
}

func toGethMessage(msg *etypes.Message) (*gcore.Message, error) {
	if msg == nil {
		return nil, nil
	}
	var gethTx *gtypes.Transaction
	if msg.Tx != nil {
		// Avoid marshal/unmarshal round‑trip only for submit‑retryable; other
		// tx types keep the existing binary path to minimize risk.
		switch t := msg.Tx.(type) {
		case *etypes.ArbitrumSubmitRetryableTx:
			gethTx = gtypes.NewTx(&gtypes.ArbitrumSubmitRetryableTx{
				ChainId:          t.ChainId,
				RequestId:        toGethHash(t.RequestId),
				From:             toGethAddress(t.From),
				L1BaseFee:        t.L1BaseFee,
				DepositValue:     t.DepositValue,
				GasFeeCap:        t.GasFeeCap,
				Gas:              t.Gas,
				RetryTo:          toGethAddressPtr(t.RetryTo),
				RetryValue:       t.RetryValue,
				Beneficiary:      toGethAddress(t.Beneficiary),
				MaxSubmissionFee: t.MaxSubmissionFee,
				FeeRefundAddr:    toGethAddress(t.FeeRefundAddr),
				RetryData:        append([]byte(nil), t.RetryData...),
			})
		default:
			txBytes, err := marshalErigonTx(msg.Tx)
			if err != nil {
				return nil, err
			}
			gethTx = new(gtypes.Transaction)
			if err := gethTx.UnmarshalBinary(txBytes); err != nil {
				return nil, fmt.Errorf("decode geth tx: %w", err)
			}
		}
	}
	return &gcore.Message{
		TxRunMode:         gcore.MessageRunMode(msg.TxRunMode),
		Tx:                gethTx,
		To:                toGethAddressPtr(msg.To()),
		From:              toGethAddress(msg.From()),
		Nonce:             msg.Nonce(),
		Value:             toBig(msg.Value()),
		GasLimit:          msg.Gas(),
		GasPrice:          toBig(msg.GasPrice()),
		GasFeeCap:         toBig(msg.FeeCap()),
		GasTipCap:         toBig(msg.TipCap()),
		Data:              append([]byte(nil), msg.Data()...),
		AccessList:        toGethAccessList(msg.AccessList()),
		BlobGasFeeCap:     toBig(msg.MaxFeePerBlobGas()),
		BlobHashes:        toGethHashes(msg.BlobHashes()),
		SkipAccountChecks: msg.SkipAccountChecks,
		SkipL1Charging:    msg.SkipL1Charging,
	}, nil
}

// ToGethMessage converts an Erigon message into a geth-compatible message.
func ToGethMessage(msg *etypes.Message) (*gcore.Message, error) {
	return toGethMessage(msg)
}

func toErigonTx(tx *gtypes.Transaction) (etypes.Transaction, error) {
	if tx == nil {
		return nil, nil
	}
	raw, err := tx.MarshalBinary()
	if err != nil {
		return nil, err
	}
	return etypes.DecodeTransaction(raw)
}

func marshalErigonTx(tx etypes.Transaction) ([]byte, error) {
	var buf bytes.Buffer
	if err := tx.MarshalBinary(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func toGethContract(contract *evm.Contract, input []byte) *gvm.Contract {
	caller := gvm.AccountRef(toGethAddress(contract.Caller()))
	self := gvm.AccountRef(toGethAddress(contract.Address()))
	gethContract := gvm.NewContract(caller, self, contract.Value(), contract.Gas)
	gethContract.Input = append([]byte(nil), input...)
	gethContract.Code = contract.Code
	gethContract.CodeHash = toGethHash(contract.CodeHash)
	if contract.CodeAddr != nil {
		addr := toGethAddress(*contract.CodeAddr)
		gethContract.CodeAddr = &addr
	}
	return gethContract
}

func setGethInterpreterReadOnly(interpreter *gvm.EVMInterpreter, readOnly bool) {
	if interpreter == nil {
		return
	}
	// Keep this in sync with go-ethereum/core/vm/EVMInterpreter layout.
	type interpState struct {
		evm        *gvm.EVM
		table      *gvm.JumpTable
		hasher     crypto.KeccakState
		hasherBuf  gcommon.Hash
		readOnly   bool
		returnData []byte
	}
	state := (*interpState)(unsafe.Pointer(interpreter))
	state.readOnly = readOnly
}

func toGethAddress(addr ecommon.Address) gcommon.Address { return gcommon.Address(addr) }

func toErigonAddress(addr gcommon.Address) ecommon.Address { return ecommon.Address(addr) }

func toGethHash(hash ecommon.Hash) gcommon.Hash { return gcommon.Hash(hash) }

func toErigonHash(hash gcommon.Hash) ecommon.Hash { return ecommon.Hash(hash) }

func toGethHashPtr(hash *ecommon.Hash) *gcommon.Hash {
	if hash == nil {
		return nil
	}
	h := toGethHash(*hash)
	return &h
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

func toGethAddressPtr(addr *ecommon.Address) *gcommon.Address {
	if addr == nil {
		return nil
	}
	out := toGethAddress(*addr)
	return &out
}

func toBig(val *uint256.Int) *big.Int {
	if val == nil {
		return new(big.Int)
	}
	return val.ToBig()
}

func toUint256(val *big.Int) *uint256.Int {
	if val == nil {
		return new(uint256.Int)
	}
	return uint256.MustFromBig(val)
}

func toGethAccessList(list etypes.AccessList) gtypes.AccessList {
	if len(list) == 0 {
		return nil
	}
	out := make(gtypes.AccessList, len(list))
	for i, entry := range list {
		out[i] = gtypes.AccessTuple{
			Address:     toGethAddress(entry.Address),
			StorageKeys: toGethHashes(entry.StorageKeys),
		}
	}
	return out
}
