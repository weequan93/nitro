// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethhook

import (
	"crypto/ecdsa"
	"errors"
	"math/big"
	"testing"

	"github.com/holiman/uint256"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/blacklist"
)

var deriwConsensusChainConfig = func() *params.ChainConfig {
	config := *testChainConfig
	config.ChainID = big.NewInt(412346)
	return &config
}()

func applyDeriwConsensusTestTx(t *testing.T, stateDB *state.StateDB, arbosState *arbosState.ArbosState, tx *types.Transaction) (*types.Receipt, *core.ExecutionResult) {
	t.Helper()
	header := &types.Header{
		Number:     big.NewInt(1),
		Difficulty: big.NewInt(1),
		GasLimit:   10_000_000,
		BaseFee:    big.NewInt(0),
		Time:       1,
	}
	types.HeaderInfo{ArbOSFormatVersion: arbosState.ArbOSVersion()}.UpdateHeaderWithInfo(header)
	chainContext := &TestChainContext{chainConfig: deriwConsensusChainConfig}
	evm := vm.NewEVM(core.NewEVMBlockContext(header, chainContext, nil), stateDB, deriwConsensusChainConfig, vm.Config{})
	gasPool := core.GasPool(header.GasLimit)
	receipt, result, err := core.ApplyTransaction(evm, &gasPool, stateDB, header, tx, &header.GasUsed)
	if err != nil {
		t.Fatal(err)
	}
	return receipt, result
}

func makeSignedDeriwTx(t *testing.T, key *ecdsa.PrivateKey, arbOSVersion uint64, nonce uint64, to *common.Address, value *big.Int, gas uint64, data []byte) *types.Transaction {
	t.Helper()
	unsigned := types.NewTx(&types.LegacyTx{
		Nonce:    nonce,
		To:       to,
		Value:    value,
		Gas:      gas,
		GasPrice: big.NewInt(0),
		Data:     data,
	})
	signer := types.MakeSigner(deriwConsensusChainConfig, big.NewInt(1), 1, arbOSVersion)
	tx, err := types.SignTx(unsigned, signer, key)
	if err != nil {
		t.Fatal(err)
	}
	return tx
}

func fundedDeriwSender(t *testing.T, stateDB *state.StateDB) (*ecdsa.PrivateKey, common.Address) {
	t.Helper()
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	sender := crypto.PubkeyToAddress(key.PublicKey)
	stateDB.SetBalance(sender, uint256.NewInt(1_000_000), tracing.BalanceChangeUnspecified)
	return key, sender
}

func callThenStoreCode(target common.Address) []byte {
	code := []byte{
		0x60, 0x00, // return size
		0x60, 0x00, // return offset
		0x60, 0x00, // input size
		0x60, 0x00, // input offset
		0x60, 0x00, // value
		0x73,
	}
	code = append(code, target.Bytes()...)
	return append(code,
		0x61, 0xff, 0xff, // gas
		byte(vm.CALL),
		byte(vm.POP),
		0x60, 0x01, // value 1
		0x60, 0x00, // slot 0
		byte(vm.SSTORE),
		byte(vm.STOP),
	)
}

func TestDeriwConsensusBlacklistLegacyVersionPreservesExecution(t *testing.T) {
	state, stateDB := arbosState.NewArbosMemoryBackedArbOSState()
	key, _ := fundedDeriwSender(t, stateDB)
	target := common.HexToAddress("0x1001")
	stateDB.SetCode(target, []byte{0x60, 0x01, 0x60, 0x00, 0x55, 0x00}, tracing.CodeChangeUnspecified)
	if err := state.Blacklist().TxToAddrs().Add(target); err != nil {
		t.Fatal(err)
	}

	tx := makeSignedDeriwTx(t, key, state.ArbOSVersion(), 0, &target, big.NewInt(0), 100_000, nil)
	receipt, result := applyDeriwConsensusTestTx(t, stateDB, state, tx)
	if receipt.Status != types.ReceiptStatusSuccessful || result.Err != nil {
		t.Fatalf("DeriwOS 0 result = (%v, %v), want historical success", receipt.Status, result.Err)
	}
}

func TestDeriwConsensusBlacklistTopLevelFailedNoop(t *testing.T) {
	state, stateDB := arbosState.NewArbosMemoryBackedArbOSState()
	if err := state.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_ConsensusBlacklist); err != nil {
		t.Fatal(err)
	}
	if err := state.L2PricingState().SetMaxPerTxGasLimit(40_000); err != nil {
		t.Fatal(err)
	}
	key, sender := fundedDeriwSender(t, stateDB)
	target := common.HexToAddress("0x2002")
	if err := state.Blacklist().TxFromAddrs().Add(target); err != nil {
		t.Fatal(err)
	}

	const gasLimit = uint64(100_000)
	tx := makeSignedDeriwTx(t, key, state.ArbOSVersion(), 0, &target, big.NewInt(123), gasLimit, nil)
	receipt, result := applyDeriwConsensusTestTx(t, stateDB, state, tx)
	if receipt.Status != types.ReceiptStatusFailed || !errors.Is(result.Err, vm.ErrDeriwBlacklisted) {
		t.Fatalf("result = (%v, %v), want failed blacklist result", receipt.Status, result.Err)
	}
	if receipt.GasUsed != gasLimit || result.UsedMultiGas.SingleGas() != gasLimit {
		t.Fatalf("gas used = %v/%v, want %v", receipt.GasUsed, result.UsedMultiGas.SingleGas(), gasLimit)
	}
	if stateDB.GetNonce(sender) != 1 || stateDB.GetBalance(target).Sign() != 0 {
		t.Fatal("failed no-op nonce or value result is incorrect")
	}
}

func TestDeriwConsensusBlacklistChecksEffectiveSubaccountParent(t *testing.T) {
	state, stateDB := arbosState.NewArbosMemoryBackedArbOSState()
	if err := state.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_ConsensusBlacklist); err != nil {
		t.Fatal(err)
	}
	key, child := fundedDeriwSender(t, stateDB)
	parent := common.HexToAddress("0x2102")
	target := common.HexToAddress("0x3103")
	if err := state.SubAccount().AllowedAddress().Add(target); err != nil {
		t.Fatal(err)
	}
	if err := state.SubAccount().BindRelation(parent, child, big.NewInt(0)); err != nil {
		t.Fatal(err)
	}
	// The union rule must quarantine the effective sender even when the parent
	// appears only in the legacy destination list.
	if err := state.Blacklist().TxToAddrs().Add(parent); err != nil {
		t.Fatal(err)
	}

	const gasLimit = uint64(100_000)
	tx := makeSignedDeriwTx(t, key, state.ArbOSVersion(), 0, &target, big.NewInt(0), gasLimit, nil)
	receipt, result := applyDeriwConsensusTestTx(t, stateDB, state, tx)
	if receipt.Status != types.ReceiptStatusFailed || !errors.Is(result.Err, vm.ErrDeriwBlacklisted) {
		t.Fatalf("result = (%v, %v), want failed parent blacklist result", receipt.Status, result.Err)
	}
	if stateDB.GetNonce(child) != 1 || stateDB.GetNonce(parent) != 1 {
		t.Fatalf("nonces = child %v parent %v, want 1/1", stateDB.GetNonce(child), stateDB.GetNonce(parent))
	}
}

func TestDeriwConsensusBlacklistAllowsExactEmergencyRemoval(t *testing.T) {
	state, stateDB := arbosState.NewArbosMemoryBackedArbOSState()
	key, owner := fundedDeriwSender(t, stateDB)
	target := common.HexToAddress("0x2003")
	if err := state.Blacklist().BlacklistOwner().Add(owner); err != nil {
		t.Fatal(err)
	}
	if err := state.Blacklist().TxFromAddrs().Add(owner); err != nil {
		t.Fatal(err)
	}
	if err := state.Blacklist().TxToAddrs().Add(target); err != nil {
		t.Fatal(err)
	}
	if err := state.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_ConsensusBlacklist); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4+32)
	copy(data[:4], blacklist.RemoveBlacklistTxToSelector[:])
	copy(data[4+12:], target.Bytes())
	precompile := types.DeriwBlacklistAddress
	tx := makeSignedDeriwTx(t, key, state.ArbOSVersion(), 0, &precompile, big.NewInt(0), 200_000, data)

	receipt, result := applyDeriwConsensusTestTx(t, stateDB, state, tx)
	if receipt.Status != types.ReceiptStatusSuccessful || result.Err != nil {
		t.Fatalf("emergency removal result = (%v, %v), want success", receipt.Status, result.Err)
	}
	if state.Blacklist().IsQuarantinedFree(target) {
		t.Fatal("emergency removal left target quarantined")
	}
}

func TestDeriwConsensusBlacklistDoesNotInspectNestedCalls(t *testing.T) {
	state, stateDB := arbosState.NewArbosMemoryBackedArbOSState()
	if err := state.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_ConsensusBlacklist); err != nil {
		t.Fatal(err)
	}
	key, _ := fundedDeriwSender(t, stateDB)
	outer := common.HexToAddress("0x3003")
	blockedInner := common.HexToAddress("0x4004")
	stateDB.SetCode(outer, callThenStoreCode(blockedInner), tracing.CodeChangeUnspecified)
	if err := state.Blacklist().TxToAddrs().Add(blockedInner); err != nil {
		t.Fatal(err)
	}

	tx := makeSignedDeriwTx(t, key, state.ArbOSVersion(), 0, &outer, big.NewInt(0), 200_000, nil)
	receipt, result := applyDeriwConsensusTestTx(t, stateDB, state, tx)
	if receipt.Status != types.ReceiptStatusSuccessful || result.Err != nil {
		t.Fatalf("nested call result = (%v, %v), want success", receipt.Status, result.Err)
	}
	if stored := stateDB.GetState(outer, common.Hash{}); stored != common.BigToHash(big.NewInt(1)) {
		t.Fatalf("outer state = %v, want nested execution to remain allowed", stored)
	}
}

func TestDeriwConsensusBlacklistDoesNotInspectERC20Calldata(t *testing.T) {
	state, stateDB := arbosState.NewArbosMemoryBackedArbOSState()
	if err := state.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_ConsensusBlacklist); err != nil {
		t.Fatal(err)
	}
	key, _ := fundedDeriwSender(t, stateDB)
	token := common.HexToAddress("0x5005")
	recipient := common.HexToAddress("0x6006")
	stateDB.SetCode(token, []byte{0x60, 0x01, 0x60, 0x00, 0x55, 0x00}, tracing.CodeChangeUnspecified)
	if err := state.Blacklist().TxFromAddrs().Add(recipient); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 4+32+32)
	copy(data[:4], []byte{0xa9, 0x05, 0x9c, 0xbb})
	copy(data[4+12:4+32], recipient.Bytes())

	tx := makeSignedDeriwTx(t, key, state.ArbOSVersion(), 0, &token, big.NewInt(0), 150_000, data)
	receipt, result := applyDeriwConsensusTestTx(t, stateDB, state, tx)
	if receipt.Status != types.ReceiptStatusSuccessful || result.Err != nil {
		t.Fatalf("ERC20-shaped calldata result = (%v, %v), want success", receipt.Status, result.Err)
	}
}

func TestDeriwConsensusBlacklistDoesNotCheckDerivedCreationAddress(t *testing.T) {
	state, stateDB := arbosState.NewArbosMemoryBackedArbOSState()
	if err := state.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_ConsensusBlacklist); err != nil {
		t.Fatal(err)
	}
	key, sender := fundedDeriwSender(t, stateDB)
	created := crypto.CreateAddress(sender, 0)
	if err := state.Blacklist().TxToAddrs().Add(created); err != nil {
		t.Fatal(err)
	}
	tx := makeSignedDeriwTx(t, key, state.ArbOSVersion(), 0, nil, big.NewInt(0), 150_000, []byte{byte(vm.STOP)})

	receipt, result := applyDeriwConsensusTestTx(t, stateDB, state, tx)
	if receipt.Status != types.ReceiptStatusSuccessful || result.Err != nil {
		t.Fatalf("creation result = (%v, %v), want success", receipt.Status, result.Err)
	}
	if stateDB.GetNonce(sender) != 1 {
		t.Fatalf("sender nonce = %v, want 1", stateDB.GetNonce(sender))
	}
}

func TestDeriwConsensusBlacklistAllowsDepositFunding(t *testing.T) {
	state, stateDB := arbosState.NewArbosMemoryBackedArbOSState()
	if err := state.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_ConsensusBlacklist); err != nil {
		t.Fatal(err)
	}
	from := common.HexToAddress("0x7007")
	to := common.HexToAddress("0x8008")
	if err := state.Blacklist().TxToAddrs().Add(to); err != nil {
		t.Fatal(err)
	}
	tx := types.NewTx(&types.ArbitrumDepositTx{
		ChainId:     deriwConsensusChainConfig.ChainID,
		L1RequestId: common.HexToHash("0x9009"),
		From:        from,
		To:          to,
		Value:       big.NewInt(123),
	})

	receipt, result := applyDeriwConsensusTestTx(t, stateDB, state, tx)
	if receipt.Status != types.ReceiptStatusSuccessful || result.Err != nil {
		t.Fatalf("deposit result = (%v, %v), want successful funding", receipt.Status, result.Err)
	}
	if stateDB.GetBalance(from).Sign() != 0 || stateDB.GetBalance(to).Cmp(uint256.NewInt(123)) != 0 {
		t.Fatal("deposit did not credit the blacklisted destination")
	}
}

func TestDeriwConsensusBlacklistAllowsRetryableTicketCreation(t *testing.T) {
	state, stateDB := arbosState.NewArbosMemoryBackedArbOSState()
	if err := state.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_ConsensusBlacklist); err != nil {
		t.Fatal(err)
	}
	from := common.HexToAddress("0x7107")
	retryTo := common.HexToAddress("0x8108")
	if err := state.Blacklist().TxToAddrs().Add(retryTo); err != nil {
		t.Fatal(err)
	}
	tx := types.NewTx(&types.ArbitrumSubmitRetryableTx{
		ChainId:          deriwConsensusChainConfig.ChainID,
		RequestId:        common.HexToHash("0x9109"),
		From:             from,
		L1BaseFee:        big.NewInt(0),
		DepositValue:     big.NewInt(123),
		GasFeeCap:        big.NewInt(0),
		Gas:              100_000,
		RetryTo:          &retryTo,
		RetryValue:       big.NewInt(0),
		Beneficiary:      common.HexToAddress("0xb001"),
		MaxSubmissionFee: big.NewInt(0),
		FeeRefundAddr:    common.HexToAddress("0xf001"),
	})

	receipt, result := applyDeriwConsensusTestTx(t, stateDB, state, tx)
	if receipt.Status != types.ReceiptStatusSuccessful || result.Err != nil {
		t.Fatalf("retryable submission result = (%v, %v), want successful ticket creation", receipt.Status, result.Err)
	}
	retryable, err := state.RetryableState().OpenRetryable(tx.Hash(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if retryable == nil {
		t.Fatal("blacklisted retry destination prevented ticket creation")
	}
}
