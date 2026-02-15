//go:build erigon
// +build erigon

package arbos

import (
	"errors"
	"fmt"
	"math/big"
	"os"
	"runtime"
	"strings"
	"unsafe"

	"github.com/holiman/uint256"

	ecommon "github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon/arb/ethdb/wasmdb"
	estate "github.com/erigontech/erigon/core/state"
	etracing "github.com/erigontech/erigon/core/tracing"
	"github.com/erigontech/erigon/db/kv"
	echain "github.com/erigontech/erigon/execution/chain"
	etypes "github.com/erigontech/erigon/execution/types"

	gcommon "github.com/ethereum/go-ethereum/common"
	gstate "github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	gtypes "github.com/ethereum/go-ethereum/core/types"
	gvm "github.com/ethereum/go-ethereum/core/vm"
	gethethdb "github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/triedb"

	"github.com/offchainlabs/nitro/execution/erigonexec/kvdb"
)

var errUnsupportedDB = errors.New("arbos: erigon statedb adapter does not support trie operations")

type stateDBAdapter struct {
	ibs        estate.IntraBlockStateArbitrum
	chainRules *echain.Rules
	db         *stateDatabaseAdapter
	debugCtx   setStateDebugContext
}

type setStateDebugContext struct {
	blockNum uint64
	txHash   gcommon.Hash
	txType   uint8
	runMode  string
}

var badRootSetStateTraceEnabled = strings.EqualFold(os.Getenv("ERIGON_BAD_ROOT_SETSTATE_TRACE"), "true") ||
	strings.EqualFold(os.Getenv("ERIGON_BAD_ROOT_DEBUG"), "true")
var badRootSetStateTraceAddr = gcommon.HexToAddress(strings.TrimSpace(defaultEnv(
	"ERIGON_BAD_ROOT_SETSTATE_ADDR",
	"0xA4b05FffffFffFFFFfFFfffFfffFFfffFfFfFFFf",
)))
var badRootSetStateTraceSlot = parseOptionalHash(strings.TrimSpace(os.Getenv("ERIGON_BAD_ROOT_SETSTATE_SLOT")))

func defaultEnv(key, fallback string) string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback
	}
	return raw
}

func parseOptionalHash(raw string) *gcommon.Hash {
	if raw == "" {
		return nil
	}
	if !isHexHash(raw) {
		log.Warn("arbos setstate trace slot ignored: invalid hash", "value", raw)
		return nil
	}
	hash := gcommon.HexToHash(raw)
	return &hash
}

func isHexHash(raw string) bool {
	if len(raw) != 66 || !(strings.HasPrefix(raw, "0x") || strings.HasPrefix(raw, "0X")) {
		return false
	}
	for _, c := range raw[2:] {
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

func traceSetStateCallers(skip, max int) string {
	if max <= 0 {
		return ""
	}
	pcs := make([]uintptr, max+skip+2)
	n := runtime.Callers(skip, pcs)
	if n <= 0 {
		return ""
	}
	frames := runtime.CallersFrames(pcs[:n])
	parts := make([]string, 0, max)
	for {
		frame, more := frames.Next()
		if frame.Function != "" {
			parts = append(parts, fmt.Sprintf("%s:%d", frame.Function, frame.Line))
			if len(parts) >= max {
				break
			}
		}
		if !more {
			break
		}
	}
	return strings.Join(parts, " <- ")
}

func newStateDBAdapter(ibs estate.IntraBlockStateArbitrum, rules *echain.Rules) *stateDBAdapter {
	return &stateDBAdapter{
		ibs:        ibs,
		chainRules: rules,
	}
}

// NewStateDBAdapter exposes a geth-compatible StateDB backed by an Erigon IBS.
func NewStateDBAdapter(ibs estate.IntraBlockStateArbitrum, rules *echain.Rules) gvm.StateDB {
	return newStateDBAdapter(ibs, rules)
}

func (s *stateDBAdapter) ActivateWasm(moduleHash gcommon.Hash, asmMap map[gethethdb.WasmTarget][]byte) {
	if s.ibs == nil {
		return
	}
	converted := make(map[wasmdb.WasmTarget][]byte, len(asmMap))
	for target, asm := range asmMap {
		converted[wasmdb.WasmTarget(target)] = asm
	}
	s.ibs.ActivateWasm(toErigonHash(moduleHash), converted)
}

func (s *stateDBAdapter) TryGetActivatedAsm(target gethethdb.WasmTarget, moduleHash gcommon.Hash) ([]byte, error) {
	if s.ibs == nil {
		return nil, errors.New("arbos: missing ibs")
	}
	return s.ibs.TryGetActivatedAsm(wasmdb.WasmTarget(target), toErigonHash(moduleHash))
}

func (s *stateDBAdapter) TryGetActivatedAsmMap(targets []gethethdb.WasmTarget, moduleHash gcommon.Hash) (map[gethethdb.WasmTarget][]byte, error) {
	if s.ibs == nil {
		return nil, errors.New("arbos: missing ibs")
	}
	convertedTargets := make([]wasmdb.WasmTarget, 0, len(targets))
	for _, target := range targets {
		convertedTargets = append(convertedTargets, wasmdb.WasmTarget(target))
	}
	asmMap, err := s.ibs.TryGetActivatedAsmMap(convertedTargets, toErigonHash(moduleHash))
	if err != nil {
		return nil, err
	}
	out := make(map[gethethdb.WasmTarget][]byte, len(asmMap))
	for target, asm := range asmMap {
		out[gethethdb.WasmTarget(target)] = asm
	}
	return out, nil
}

func (s *stateDBAdapter) RecordCacheWasm(wasm gstate.CacheWasm) {
	if s.ibs == nil {
		return
	}
	s.ibs.RecordCacheWasm(estate.CacheWasm{
		ModuleHash: toErigonHash(wasm.ModuleHash),
		Version:    wasm.Version,
		Tag:        wasm.Tag,
		Debug:      wasm.Debug,
	})
}

func (s *stateDBAdapter) RecordEvictWasm(wasm gstate.EvictWasm) {
	if s.ibs == nil {
		return
	}
	s.ibs.RecordEvictWasm(estate.EvictWasm{
		ModuleHash: toErigonHash(wasm.ModuleHash),
		Version:    wasm.Version,
		Tag:        wasm.Tag,
		Debug:      wasm.Debug,
	})
}

func (s *stateDBAdapter) GetRecentWasms() gstate.RecentWasms {
	if s.ibs == nil {
		return gstate.NewRecentWasms()
	}
	erigonRecent := s.ibs.GetRecentWasms()
	return *(*gstate.RecentWasms)(unsafe.Pointer(&erigonRecent))
}

func (s *stateDBAdapter) GetStylusPages() (uint16, uint16) {
	if s.ibs == nil {
		return 0, 0
	}
	return s.ibs.GetStylusPages()
}

func (s *stateDBAdapter) GetStylusPagesOpen() uint16 {
	if s.ibs == nil {
		return 0
	}
	return s.ibs.GetStylusPagesOpen()
}

func (s *stateDBAdapter) SetStylusPagesOpen(open uint16) {
	if s.ibs == nil {
		return
	}
	s.ibs.SetStylusPagesOpen(open)
}

func (s *stateDBAdapter) AddStylusPages(new uint16) (uint16, uint16) {
	if s.ibs == nil {
		return 0, 0
	}
	return s.ibs.AddStylusPages(new)
}

func (s *stateDBAdapter) AddStylusPagesEver(new uint16) {
	if s.ibs == nil {
		return
	}
	s.ibs.AddStylusPagesEver(new)
}

func (s *stateDBAdapter) CreateZombieIfDeleted(addr gcommon.Address) {
	if s.ibs == nil {
		return
	}
	erigonAddr := toErigonAddress(addr)
	exists, err := s.ibs.Exist(erigonAddr)
	if err != nil {
		panic(err)
	}
	if exists {
		return
	}
	destructed, err := s.ibs.HasSelfdestructed(erigonAddr)
	if err != nil {
		panic(err)
	}
	if !destructed {
		return
	}
	// Mark the account as a zombie touch so empty-removal does not delete it.
	if err := s.ibs.CreateZombieAccount(erigonAddr); err != nil {
		panic(err)
	}
	if mdbxMigrateDebug {
		log.Info("arbos marked zombie account",
			"addr", addr.Hex(),
		)
	}
}

func (s *stateDBAdapter) RemoveEscrowProtection(addr gcommon.Address) {
	if s.ibs == nil {
		return
	}
	s.ibs.RemoveEscrowProtection(toErigonAddress(addr))
}

func (s *stateDBAdapter) FilterTx()      {}
func (s *stateDBAdapter) ClearTxFilter() {}

func (s *stateDBAdapter) IsTxFiltered() bool {
	if s.ibs == nil {
		return false
	}
	return s.ibs.IsTxFiltered()
}

func (s *stateDBAdapter) Deterministic() bool { return false }

func (s *stateDBAdapter) Database() gstate.Database {
	if s.db == nil {
		s.db = &stateDatabaseAdapter{state: s}
	}
	return s.db
}

func (s *stateDBAdapter) CreateAccount(addr gcommon.Address) {
	if s.ibs == nil {
		return
	}
	if err := s.ibs.CreateAccount(toErigonAddress(addr), false); err != nil {
		panic(err)
	}
}

func (s *stateDBAdapter) CreateContract(addr gcommon.Address) {
	if s.ibs == nil {
		return
	}
	if err := s.ibs.CreateAccount(toErigonAddress(addr), true); err != nil {
		panic(err)
	}
}

func (s *stateDBAdapter) SetDebugContext(blockNum uint64, txHash gcommon.Hash, txType uint8, runMode string) {
	s.debugCtx = setStateDebugContext{
		blockNum: blockNum,
		txHash:   txHash,
		txType:   txType,
		runMode:  runMode,
	}
}

func (s *stateDBAdapter) SubBalance(addr gcommon.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) {
	if s.ibs == nil {
		return
	}
	val := uint256.NewInt(0)
	if amount != nil {
		val.Set(amount)
	}
	if val.IsZero() {
		if s.chainRules == nil || s.chainRules.ArbOSVersion < params.ArbosVersion_Stylus {
			s.CreateZombieIfDeleted(addr)
		}
	}
	if err := s.ibs.SubBalance(toErigonAddress(addr), *val, etracing.BalanceChangeReason(reason)); err != nil {
		panic(err)
	}
}

func (s *stateDBAdapter) AddBalance(addr gcommon.Address, amount *uint256.Int, reason tracing.BalanceChangeReason) {
	if s.ibs == nil {
		return
	}
	val := uint256.NewInt(0)
	if amount != nil {
		val.Set(amount)
	}
	if err := s.ibs.AddBalance(toErigonAddress(addr), *val, etracing.BalanceChangeReason(reason)); err != nil {
		panic(err)
	}
}

func (s *stateDBAdapter) GetBalance(addr gcommon.Address) *uint256.Int {
	if s.ibs == nil {
		return new(uint256.Int)
	}
	bal, err := s.ibs.GetBalance(toErigonAddress(addr))
	if err != nil {
		panic(err)
	}
	out := new(uint256.Int)
	out.Set(&bal)
	return out
}

func (s *stateDBAdapter) ExpectBalanceBurn(amount *big.Int) {
	if s.ibs == nil {
		return
	}
	s.ibs.ExpectBalanceBurn(toUint256(amount))
}

func (s *stateDBAdapter) GetNonce(addr gcommon.Address) uint64 {
	if s.ibs == nil {
		return 0
	}
	nonce, err := s.ibs.GetNonce(toErigonAddress(addr))
	if err != nil {
		panic(err)
	}
	return nonce
}

func (s *stateDBAdapter) SetNonce(addr gcommon.Address, nonce uint64) {
	if s.ibs == nil {
		return
	}
	if err := s.ibs.SetNonce(toErigonAddress(addr), nonce); err != nil {
		panic(err)
	}
}

func (s *stateDBAdapter) GetCodeHash(addr gcommon.Address) gcommon.Hash {
	if s.ibs == nil {
		return gcommon.Hash{}
	}
	hash, err := s.ibs.GetCodeHash(toErigonAddress(addr))
	if err != nil {
		panic(err)
	}
	return toGethHash(hash)
}

func (s *stateDBAdapter) GetCode(addr gcommon.Address) []byte {
	if s.ibs == nil {
		return nil
	}
	code, err := s.ibs.GetCode(toErigonAddress(addr))
	if err != nil {
		panic(err)
	}
	return code
}

func (s *stateDBAdapter) SetCode(addr gcommon.Address, code []byte) {
	if s.ibs == nil {
		return
	}
	if err := s.ibs.SetCode(toErigonAddress(addr), code); err != nil {
		panic(err)
	}
}

func (s *stateDBAdapter) GetCodeSize(addr gcommon.Address) int {
	if s.ibs == nil {
		return 0
	}
	size, err := s.ibs.GetCodeSize(toErigonAddress(addr))
	if err != nil {
		panic(err)
	}
	return size
}

func (s *stateDBAdapter) AddRefund(gas uint64) {
	if s.ibs == nil {
		return
	}
	s.ibs.AddRefund(gas)
}

func (s *stateDBAdapter) SubRefund(gas uint64) {
	if s.ibs == nil {
		return
	}
	if err := s.ibs.SubRefund(gas); err != nil {
		panic(err)
	}
}

func (s *stateDBAdapter) GetRefund() uint64 {
	if s.ibs == nil {
		return 0
	}
	return s.ibs.GetRefund()
}

func (s *stateDBAdapter) GetCommittedState(addr gcommon.Address, slot gcommon.Hash) gcommon.Hash {
	if s.ibs == nil {
		return gcommon.Hash{}
	}
	var out uint256.Int
	if err := s.ibs.GetCommittedState(toErigonAddress(addr), toErigonHash(slot), &out); err != nil {
		panic(err)
	}
	return gcommon.Hash(out.Bytes32())
}

func (s *stateDBAdapter) GetState(addr gcommon.Address, slot gcommon.Hash) gcommon.Hash {
	if s.ibs == nil {
		return gcommon.Hash{}
	}
	var out uint256.Int
	if err := s.ibs.GetState(toErigonAddress(addr), toErigonHash(slot), &out); err != nil {
		panic(err)
	}
	return gcommon.Hash(out.Bytes32())
}

func (s *stateDBAdapter) SetState(addr gcommon.Address, key gcommon.Hash, value gcommon.Hash) {
	if s.ibs == nil {
		return
	}
	traceThisWrite := badRootSetStateTraceEnabled && addr == badRootSetStateTraceAddr
	if traceThisWrite && badRootSetStateTraceSlot != nil && key != *badRootSetStateTraceSlot {
		traceThisWrite = false
	}

	var (
		prevVal uint256.Int
		prevErr error
	)
	if traceThisWrite {
		prevErr = s.ibs.GetState(toErigonAddress(addr), toErigonHash(key), &prevVal)
	}

	val := uint256.MustFromBig(value.Big())
	if err := s.ibs.SetState(toErigonAddress(addr), toErigonHash(key), *val); err != nil {
		panic(err)
	}

	if traceThisWrite {
		var (
			afterVal uint256.Int
			afterErr error
			txIndex  int
		)
		afterErr = s.ibs.GetState(toErigonAddress(addr), toErigonHash(key), &afterVal)
		txIndex = s.ibs.TxnIndex()
		log.Warn(
			"arbos setstate trace",
			"block", s.debugCtx.blockNum,
			"tx_index", txIndex,
			"tx_hash", s.debugCtx.txHash,
			"tx_type", s.debugCtx.txType,
			"run_mode", s.debugCtx.runMode,
			"addr", addr.Hex(),
			"slot", key.Hex(),
			"prev", gcommon.Hash(prevVal.Bytes32()).Hex(),
			"new", value.Hex(),
			"after", gcommon.Hash(afterVal.Bytes32()).Hex(),
			"prev_err", prevErr,
			"after_err", afterErr,
			"callers", traceSetStateCallers(3, 6),
		)
	}
}

func (s *stateDBAdapter) GetStorageRoot(addr gcommon.Address) gcommon.Hash {
	if s.ibs == nil {
		return gcommon.Hash{}
	}
	root := s.ibs.GetStorageRoot(toErigonAddress(addr))
	return toGethHash(root)
}

func (s *stateDBAdapter) GetTransientState(addr gcommon.Address, key gcommon.Hash) gcommon.Hash {
	if s.ibs == nil {
		return gcommon.Hash{}
	}
	val := s.ibs.GetTransientState(toErigonAddress(addr), toErigonHash(key))
	return gcommon.Hash(val.Bytes32())
}

func (s *stateDBAdapter) SetTransientState(addr gcommon.Address, key, value gcommon.Hash) {
	if s.ibs == nil {
		return
	}
	val := uint256.MustFromBig(value.Big())
	s.ibs.SetTransientState(toErigonAddress(addr), toErigonHash(key), *val)
}

func (s *stateDBAdapter) SelfDestruct(addr gcommon.Address) {
	if s.ibs == nil {
		return
	}
	if _, err := s.ibs.Selfdestruct(toErigonAddress(addr)); err != nil {
		panic(err)
	}
}

func (s *stateDBAdapter) HasSelfDestructed(addr gcommon.Address) bool {
	if s.ibs == nil {
		return false
	}
	ok, err := s.ibs.HasSelfdestructed(toErigonAddress(addr))
	if err != nil {
		panic(err)
	}
	return ok
}

func (s *stateDBAdapter) GetSelfDestructs() []gcommon.Address {
	if s.ibs == nil {
		return nil
	}
	getter, ok := s.ibs.(interface{ GetSelfDestructs() []ecommon.Address })
	if !ok {
		return nil
	}
	addrs := getter.GetSelfDestructs()
	out := make([]gcommon.Address, len(addrs))
	for i, addr := range addrs {
		out[i] = toGethAddress(addr)
	}
	return out
}

func (s *stateDBAdapter) Selfdestruct6780(addr gcommon.Address) {
	if s.ibs == nil {
		return
	}
	if err := s.ibs.Selfdestruct6780(toErigonAddress(addr)); err != nil {
		panic(err)
	}
}

func (s *stateDBAdapter) Exist(addr gcommon.Address) bool {
	if s.ibs == nil {
		return false
	}
	ok, err := s.ibs.Exist(toErigonAddress(addr))
	if err != nil {
		panic(err)
	}
	return ok
}

func (s *stateDBAdapter) Empty(addr gcommon.Address) bool {
	if s.ibs == nil {
		return true
	}
	ok, err := s.ibs.Empty(toErigonAddress(addr))
	if err != nil {
		panic(err)
	}
	return ok
}

func (s *stateDBAdapter) AddressInAccessList(addr gcommon.Address) bool {
	if s.ibs == nil {
		return false
	}
	return s.ibs.AddressInAccessList(toErigonAddress(addr))
}

func (s *stateDBAdapter) SlotInAccessList(addr gcommon.Address, slot gcommon.Hash) (bool, bool) {
	if s.ibs == nil {
		return false, false
	}
	type slotChecker interface {
		SlotInAccessList(ecommon.Address, ecommon.Hash) (bool, bool)
	}
	if checker, ok := s.ibs.(slotChecker); ok {
		return checker.SlotInAccessList(toErigonAddress(addr), toErigonHash(slot))
	}
	return false, false
}

func (s *stateDBAdapter) AddAddressToAccessList(addr gcommon.Address) {
	if s.ibs == nil {
		return
	}
	s.ibs.AddAddressToAccessList(toErigonAddress(addr))
}

func (s *stateDBAdapter) AddSlotToAccessList(addr gcommon.Address, slot gcommon.Hash) {
	if s.ibs == nil {
		return
	}
	s.ibs.AddSlotToAccessList(toErigonAddress(addr), toErigonHash(slot))
}

func (s *stateDBAdapter) Prepare(rules params.Rules, sender, coinbase gcommon.Address, dest *gcommon.Address, precompiles []gcommon.Address, txAccesses gtypes.AccessList) {
	if s.ibs == nil || s.chainRules == nil {
		return
	}
	ergDest := toErigonAddressPtr(dest)
	ergPrecompiles := make([]ecommon.Address, len(precompiles))
	for i, addr := range precompiles {
		ergPrecompiles[i] = toErigonAddress(addr)
	}
	if err := s.ibs.Prepare(s.chainRules, toErigonAddress(sender), toErigonAddress(coinbase), ergDest, ergPrecompiles, toErigonAccessList(txAccesses), nil); err != nil {
		panic(err)
	}
	_ = rules
}

func (s *stateDBAdapter) RevertToSnapshot(revision int) {
	if s.ibs == nil {
		return
	}
	s.ibs.RevertToSnapshot(revision, nil)
}

func (s *stateDBAdapter) Snapshot() int {
	if s.ibs == nil {
		return 0
	}
	return s.ibs.Snapshot()
}

func (s *stateDBAdapter) AddLog(log *gtypes.Log) {
	if s.ibs == nil || log == nil {
		return
	}
	s.ibs.AddLog(toErigonLog(log))
}

func (s *stateDBAdapter) AddPreimage(_ gcommon.Hash, _ []byte) {}

func (s *stateDBAdapter) GetCurrentTxLogs() []*gtypes.Log {
	if s.ibs == nil {
		return nil
	}
	txIndex := s.ibs.TxnIndex()
	logs := s.ibs.GetLogs(txIndex, ecommon.Hash{}, 0, ecommon.Hash{})
	if len(logs) == 0 {
		return nil
	}
	out := make([]*gtypes.Log, 0, len(logs))
	for _, log := range logs {
		out = append(out, toGethLog(log))
	}
	return out
}

type stateDatabaseAdapter struct {
	state     *stateDBAdapter
	wasmStore gethethdb.KeyValueStore
}

func (d *stateDatabaseAdapter) ActivatedAsm(target gethethdb.WasmTarget, moduleHash gcommon.Hash) ([]byte, error) {
	if d.state == nil || d.state.ibs == nil {
		return nil, errors.New("arbos: missing ibs")
	}
	return d.state.ibs.ActivatedAsm(wasmdb.WasmTarget(target), toErigonHash(moduleHash))
}

func (d *stateDatabaseAdapter) WasmStore() gethethdb.KeyValueStore {
	if d.wasmStore != nil {
		return d.wasmStore
	}
	if d.state == nil || d.state.ibs == nil {
		return nil
	}
	d.wasmStore = kvdb.New(d.state.ibs.WasmStore(), kv.ArbWasmActivationBucket)
	return d.wasmStore
}

func (d *stateDatabaseAdapter) WasmCacheTag() uint32 {
	if d.state == nil || d.state.ibs == nil {
		return 0
	}
	return d.state.ibs.WasmCacheTag()
}

func (d *stateDatabaseAdapter) WasmTargets() []gethethdb.WasmTarget {
	if d.state == nil || d.state.ibs == nil {
		return nil
	}
	targets := d.state.ibs.WasmTargets()
	out := make([]gethethdb.WasmTarget, len(targets))
	for i, target := range targets {
		out[i] = gethethdb.WasmTarget(target)
	}
	return out
}

func (d *stateDatabaseAdapter) OpenTrie(root gcommon.Hash) (gstate.Trie, error) {
	_ = root
	return nil, errUnsupportedDB
}

func (d *stateDatabaseAdapter) OpenStorageTrie(stateRoot gcommon.Hash, address gcommon.Address, root gcommon.Hash, trie gstate.Trie) (gstate.Trie, error) {
	_ = stateRoot
	_ = address
	_ = root
	_ = trie
	return nil, errUnsupportedDB
}

func (d *stateDatabaseAdapter) CopyTrie(trie gstate.Trie) gstate.Trie {
	_ = trie
	return nil
}

func (d *stateDatabaseAdapter) ContractCode(addr gcommon.Address, codeHash gcommon.Hash) ([]byte, error) {
	if d.state == nil || d.state.ibs == nil {
		return nil, errors.New("arbos: missing ibs")
	}
	_ = codeHash
	return d.state.ibs.GetCode(toErigonAddress(addr))
}

func (d *stateDatabaseAdapter) ContractCodeSize(addr gcommon.Address, codeHash gcommon.Hash) (int, error) {
	if d.state == nil || d.state.ibs == nil {
		return 0, errors.New("arbos: missing ibs")
	}
	_ = codeHash
	return d.state.ibs.GetCodeSize(toErigonAddress(addr))
}

func (d *stateDatabaseAdapter) DiskDB() gethethdb.KeyValueStore {
	return nil
}

func (d *stateDatabaseAdapter) TrieDB() *triedb.Database {
	return nil
}

func toGethLog(log *etypes.Log) *gtypes.Log {
	if log == nil {
		return nil
	}
	out := &gtypes.Log{
		Address:     toGethAddress(log.Address),
		Topics:      toGethHashes(log.Topics),
		Data:        append([]byte(nil), log.Data...),
		BlockNumber: log.BlockNumber,
		TxHash:      toGethHash(log.TxHash),
		TxIndex:     log.TxIndex,
		BlockHash:   toGethHash(log.BlockHash),
		Index:       log.Index,
		Removed:     log.Removed,
	}
	return out
}

func toErigonLog(log *gtypes.Log) *etypes.Log {
	if log == nil {
		return nil
	}
	return &etypes.Log{
		Address:     toErigonAddress(log.Address),
		Topics:      toErigonHashes(log.Topics),
		Data:        append([]byte(nil), log.Data...),
		BlockNumber: log.BlockNumber,
		TxHash:      toErigonHash(log.TxHash),
		TxIndex:     log.TxIndex,
		BlockHash:   toErigonHash(log.BlockHash),
		Index:       log.Index,
		Removed:     log.Removed,
	}
}

func toErigonHashes(hashes []gcommon.Hash) []ecommon.Hash {
	if len(hashes) == 0 {
		return nil
	}
	out := make([]ecommon.Hash, len(hashes))
	for i, hash := range hashes {
		out[i] = toErigonHash(hash)
	}
	return out
}

func toErigonAddressPtr(addr *gcommon.Address) *ecommon.Address {
	if addr == nil {
		return nil
	}
	out := toErigonAddress(*addr)
	return &out
}

func toErigonAccessList(list gtypes.AccessList) etypes.AccessList {
	if len(list) == 0 {
		return nil
	}
	out := make(etypes.AccessList, len(list))
	for i, entry := range list {
		out[i] = etypes.AccessTuple{
			Address:     toErigonAddress(entry.Address),
			StorageKeys: toErigonHashes(entry.StorageKeys),
		}
	}
	return out
}
