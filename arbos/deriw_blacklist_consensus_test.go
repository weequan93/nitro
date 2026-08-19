// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package arbos

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/storage"
	arbosutil "github.com/offchainlabs/nitro/arbos/util"
)

func newDeriwBlacklistTestProcessor(t *testing.T, active bool) (*TxProcessor, *arbosState.ArbosState, *state.StateDB) {
	t.Helper()
	state, stateDB := arbosState.NewArbosMemoryBackedArbOSState()
	if active {
		if err := state.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_ConsensusBlacklist); err != nil {
			t.Fatal(err)
		}
	}
	child := common.HexToAddress("0x1001")
	parent := common.HexToAddress("0x2002")
	target := common.HexToAddress("0x3003")
	message := &core.Message{
		TxRunContext: core.NewMessageCommitContext(nil),
		From:         parent,
		To:           &target,
		Value:        big.NewInt(0),
	}
	return &TxProcessor{
		msg:          message,
		originalFrom: child,
		state:        state,
		evm:          &vm.EVM{StateDB: stateDB},
	}, state, stateDB
}

func TestDeriwBlacklistLegacyVersionDoesNotChangeExecution(t *testing.T) {
	processor, arbosState, _ := newDeriwBlacklistTestProcessor(t, false)
	if err := arbosState.Blacklist().TxFromAddrs().Add(processor.originalFrom); err != nil {
		t.Fatal(err)
	}
	gasRemaining := uint64(100_000)
	gas, err := processor.checkTopLevelDeriwBlacklist(&gasRemaining)
	if err != nil || gas.SingleGas() != 0 || gasRemaining != 100_000 {
		t.Fatalf("DeriwOS 0 check = (%v used, %v remaining, %v), want (0, 100000, nil)", gas.SingleGas(), gasRemaining, err)
	}
}

func TestDeriwBlacklistChecksChildParentAndDestinationWithUnionRule(t *testing.T) {
	tests := []struct {
		name    string
		address func(*TxProcessor) common.Address
		add     func(*arbosState.ArbosState, common.Address) error
	}{
		{
			name:    "child listed only as to",
			address: func(processor *TxProcessor) common.Address { return processor.originalFrom },
			add: func(state *arbosState.ArbosState, address common.Address) error {
				return state.Blacklist().TxToAddrs().Add(address)
			},
		},
		{
			name:    "parent listed only as to",
			address: func(processor *TxProcessor) common.Address { return processor.msg.From },
			add: func(state *arbosState.ArbosState, address common.Address) error {
				return state.Blacklist().TxToAddrs().Add(address)
			},
		},
		{
			name:    "destination listed only as from",
			address: func(processor *TxProcessor) common.Address { return *processor.msg.To },
			add: func(state *arbosState.ArbosState, address common.Address) error {
				return state.Blacklist().TxFromAddrs().Add(address)
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor, state, stateDB := newDeriwBlacklistTestProcessor(t, true)
			if err := test.add(state, test.address(processor)); err != nil {
				t.Fatal(err)
			}
			gasRemaining := uint64(100_000)
			_, err := processor.checkTopLevelDeriwBlacklist(&gasRemaining)
			if !errors.Is(err, vm.ErrDeriwBlacklisted) {
				t.Fatalf("error = %v, want ErrDeriwBlacklisted", err)
			}
			if gasRemaining != 0 {
				t.Fatalf("gas remaining = %v, want 0", gasRemaining)
			}
			if stateDB.GetNonce(processor.originalFrom) != 1 || stateDB.GetNonce(processor.msg.From) != 1 {
				t.Fatalf("nonces = child %v parent %v, want 1/1", stateDB.GetNonce(processor.originalFrom), stateDB.GetNonce(processor.msg.From))
			}
		})
	}
}

func TestDeriwBlacklistChargesOnlyTopLevelAddressReads(t *testing.T) {
	processor, _, _ := newDeriwBlacklistTestProcessor(t, true)
	gasRemaining := uint64(100_000)
	gas, err := processor.checkTopLevelDeriwBlacklist(&gasRemaining)
	if err != nil {
		t.Fatal(err)
	}
	// Three unique top-level addresses, with both legacy lists read for each.
	want := uint64(6 * storage.StorageReadCost)
	if gas.SingleGas() != want || gasRemaining != 100_000-want {
		t.Fatalf("gas = %v used/%v remaining, want %v/%v", gas.SingleGas(), gasRemaining, want, 100_000-want)
	}
}

func TestDeriwBlacklistChecksOriginalAndAliasedL1Sender(t *testing.T) {
	tests := []struct {
		name string
		tx   func(common.Address, *common.Address) *types.Transaction
	}{
		{
			name: "unsigned L1 execution",
			tx: func(from common.Address, to *common.Address) *types.Transaction {
				return types.NewTx(&types.ArbitrumUnsignedTx{
					ChainId: big.NewInt(1), From: from, GasFeeCap: big.NewInt(0), Gas: 100_000, To: to, Value: big.NewInt(0),
				})
			},
		},
		{
			name: "contract L1 execution",
			tx: func(from common.Address, to *common.Address) *types.Transaction {
				return types.NewTx(&types.ArbitrumContractTx{
					ChainId: big.NewInt(1), From: from, GasFeeCap: big.NewInt(0), Gas: 100_000, To: to, Value: big.NewInt(0),
				})
			},
		},
		{
			name: "retryable execution",
			tx: func(from common.Address, to *common.Address) *types.Transaction {
				return types.NewTx(&types.ArbitrumRetryTx{
					ChainId: big.NewInt(1), From: from, GasFeeCap: big.NewInt(0), Gas: 100_000, To: to, Value: big.NewInt(0),
					MaxRefund: big.NewInt(0), SubmissionFeeRefund: big.NewInt(0),
				})
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			processor, state, _ := newDeriwBlacklistTestProcessor(t, true)
			originalL1Sender := common.HexToAddress("0x4101")
			aliasedSender := arbosutil.RemapL1Address(originalL1Sender)
			processor.originalFrom = aliasedSender
			processor.msg.From = aliasedSender
			processor.msg.Tx = test.tx(aliasedSender, processor.msg.To)
			if err := state.Blacklist().TxFromAddrs().Add(originalL1Sender); err != nil {
				t.Fatal(err)
			}

			gasRemaining := uint64(100_000)
			if _, err := processor.checkTopLevelDeriwBlacklist(&gasRemaining); !errors.Is(err, vm.ErrDeriwBlacklisted) {
				t.Fatalf("original L1 sender check = %v, want ErrDeriwBlacklisted", err)
			}
		})
	}
}

func TestDeriwBlacklistChecksActualRetryDestination(t *testing.T) {
	processor, state, _ := newDeriwBlacklistTestProcessor(t, true)
	retryTo := *processor.msg.To
	processor.msg.Tx = types.NewTx(&types.ArbitrumRetryTx{
		ChainId: big.NewInt(1), From: processor.msg.From, GasFeeCap: big.NewInt(0), Gas: 100_000, To: &retryTo, Value: big.NewInt(0),
		MaxRefund: big.NewInt(0), SubmissionFeeRefund: big.NewInt(0),
	})
	if err := state.Blacklist().TxToAddrs().Add(retryTo); err != nil {
		t.Fatal(err)
	}

	gasRemaining := uint64(100_000)
	if _, err := processor.checkTopLevelDeriwBlacklist(&gasRemaining); !errors.Is(err, vm.ErrDeriwBlacklisted) {
		t.Fatalf("retry destination check = %v, want ErrDeriwBlacklisted", err)
	}
}

func TestDeriwBlacklistDoesNotInspectCalldataAddresses(t *testing.T) {
	processor, state, _ := newDeriwBlacklistTestProcessor(t, true)
	embedded := common.HexToAddress("0x4004")
	if err := state.Blacklist().TxFromAddrs().Add(embedded); err != nil {
		t.Fatal(err)
	}
	processor.msg.Data = make([]byte, 4+32)
	copy(processor.msg.Data[4+12:], embedded.Bytes())

	gasRemaining := uint64(100_000)
	if _, err := processor.checkTopLevelDeriwBlacklist(&gasRemaining); err != nil {
		t.Fatalf("embedded calldata address was checked: %v", err)
	}
}

func TestDeriwBlacklistDoesNotCheckProtocolInternalTransactions(t *testing.T) {
	processor, state, _ := newDeriwBlacklistTestProcessor(t, true)
	if err := state.Blacklist().TxFromAddrs().Add(processor.originalFrom); err != nil {
		t.Fatal(err)
	}
	processor.msg.Tx = types.NewTx(&types.ArbitrumInternalTx{ChainId: big.NewInt(1)})

	gasRemaining := uint64(100_000)
	gas, err := processor.checkTopLevelDeriwBlacklist(&gasRemaining)
	if err != nil || gas.SingleGas() != 0 || gasRemaining != 100_000 {
		t.Fatalf("internal transaction check = (%v used, %v remaining, %v), want skipped", gas.SingleGas(), gasRemaining, err)
	}
}

func TestDeriwBlacklistNonMutatingCallsRemainAllowed(t *testing.T) {
	processor, state, _ := newDeriwBlacklistTestProcessor(t, true)
	if err := state.Blacklist().TxFromAddrs().Add(*processor.msg.To); err != nil {
		t.Fatal(err)
	}
	processor.msg.TxRunContext = core.NewMessageEthcallContext()
	gasRemaining := uint64(100_000)
	gas, err := processor.checkTopLevelDeriwBlacklist(&gasRemaining)
	if err != nil || gas.SingleGas() != 0 || gasRemaining != 100_000 {
		t.Fatalf("eth_call check = (%v used, %v remaining, %v), want allowed", gas.SingleGas(), gasRemaining, err)
	}
}

func TestDeriwBlacklistLookupOutOfGasIsFailedNoop(t *testing.T) {
	processor, _, stateDB := newDeriwBlacklistTestProcessor(t, true)
	gasRemaining := uint64(1)
	gas, err := processor.checkTopLevelDeriwBlacklist(&gasRemaining)
	if !errors.Is(err, vm.ErrOutOfGas) || gas.SingleGas() != 1 || gasRemaining != 0 {
		t.Fatalf("lookup = (%v used, %v remaining, %v), want (1, 0, out of gas)", gas.SingleGas(), gasRemaining, err)
	}
	if stateDB.GetNonce(processor.originalFrom) != 1 || stateDB.GetNonce(processor.msg.From) != 1 {
		t.Fatal("lookup out of gas did not advance child and parent nonces")
	}
}
