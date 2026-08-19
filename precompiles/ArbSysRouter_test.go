// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package precompiles

import (
	"errors"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/deriwpolicy"
	"github.com/offchainlabs/nitro/cmd/chaininfo"
)

var (
	deriwDevRoutes, _              = deriwpolicy.RouterOnlySendConfigForChainID(new(big.Int).SetUint64(deriwpolicy.DevChainID))
	deriwDevRouter                 = deriwDevRoutes.Router
	deriwDevCanonicalGatewayRouter = deriwDevRoutes.CanonicalGatewayRouter
	deriwDevDefaultERC20Gateway    = deriwDevRoutes.ApprovedTokenGateways[0]
	deriwProdRoutes, _             = deriwpolicy.RouterOnlySendConfigForChainID(new(big.Int).SetUint64(deriwpolicy.ProdChainID))
	deriwProdDefaultERC20Gateway   = deriwProdRoutes.ApprovedTokenGateways[0]
)

type testDeriwSendFrame struct {
	address            common.Address
	delegateOrCallcode bool
}

func (frame testDeriwSendFrame) Address() common.Address {
	return frame.address
}

func (frame testDeriwSendFrame) IsDelegateOrCallcode() bool {
	return frame.delegateOrCallcode
}

func TestNormalizedDeriwSendPath(t *testing.T) {
	helper := common.HexToAddress("0x1001")
	frames := []testDeriwSendFrame{
		{address: helper},
		{address: deriwDevRouter},
		{address: deriwDevRouter, delegateOrCallcode: true},
		{address: deriwDevRouter, delegateOrCallcode: true},
		{address: deriwDevCanonicalGatewayRouter},
		{address: deriwDevCanonicalGatewayRouter, delegateOrCallcode: true},
		{address: deriwDevDefaultERC20Gateway},
		{address: deriwDevDefaultERC20Gateway, delegateOrCallcode: true},
	}

	got := normalizedDeriwSendPath(frames)
	want := []common.Address{
		helper,
		deriwDevRouter,
		deriwDevCanonicalGatewayRouter,
		deriwDevDefaultERC20Gateway,
	}
	if len(got) != len(want) {
		t.Fatalf("normalized path length = %v, want %v: %v", len(got), len(want), got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("normalized path[%v] = %v, want %v", index, got[index], want[index])
		}
	}

	// Equal normal frames are real self-call/reentrancy evidence and must remain.
	repeated := normalizedDeriwSendPath([]testDeriwSendFrame{
		{address: deriwDevRouter},
		{address: deriwDevRouter},
	})
	if len(repeated) != 2 {
		t.Fatalf("normalization deduplicated equal normal frames: %v", repeated)
	}
}

func TestAuthorizedNormalizedDeriwSendPath(t *testing.T) {
	prefix := common.HexToAddress("0x1001")
	helper := common.HexToAddress("0x1002")
	unknownGateway := common.HexToAddress("0x1003")

	tests := []struct {
		name   string
		stack  []common.Address
		caller common.Address
		want   bool
	}{
		{name: "empty", caller: deriwDevRouter},
		{name: "direct", stack: []common.Address{deriwDevRouter}, caller: deriwDevRouter, want: true},
		{name: "direct arbitrary prefix", stack: []common.Address{prefix, deriwDevRouter}, caller: deriwDevRouter, want: true},
		{name: "direct caller mismatch", stack: []common.Address{deriwDevRouter}, caller: helper},
		{name: "direct helper after router", stack: []common.Address{deriwDevRouter, helper}, caller: helper},
		{name: "direct repeated router", stack: []common.Address{deriwDevRouter, deriwDevRouter}, caller: deriwDevRouter},
		{
			name: "erc20",
			stack: []common.Address{
				deriwDevRouter,
				deriwDevCanonicalGatewayRouter,
				deriwDevDefaultERC20Gateway,
			},
			caller: deriwDevDefaultERC20Gateway,
			want:   true,
		},
		{
			name: "erc20 arbitrary prefix",
			stack: []common.Address{
				prefix,
				deriwDevRouter,
				deriwDevCanonicalGatewayRouter,
				deriwDevDefaultERC20Gateway,
			},
			caller: deriwDevDefaultERC20Gateway,
			want:   true,
		},
		{
			name: "erc20 unknown gateway",
			stack: []common.Address{
				deriwDevRouter,
				deriwDevCanonicalGatewayRouter,
				unknownGateway,
			},
			caller: unknownGateway,
		},
		{
			name: "erc20 missing canonical router",
			stack: []common.Address{
				deriwDevRouter,
				deriwDevDefaultERC20Gateway,
			},
			caller: deriwDevDefaultERC20Gateway,
		},
		{
			name: "erc20 helper after router",
			stack: []common.Address{
				deriwDevRouter,
				helper,
				deriwDevCanonicalGatewayRouter,
				deriwDevDefaultERC20Gateway,
			},
			caller: deriwDevDefaultERC20Gateway,
		},
		{
			name: "erc20 helper after canonical router",
			stack: []common.Address{
				deriwDevRouter,
				deriwDevCanonicalGatewayRouter,
				helper,
				deriwDevDefaultERC20Gateway,
			},
			caller: deriwDevDefaultERC20Gateway,
		},
		{
			name: "erc20 repeated router",
			stack: []common.Address{
				deriwDevRouter,
				deriwDevRouter,
				deriwDevCanonicalGatewayRouter,
				deriwDevDefaultERC20Gateway,
			},
			caller: deriwDevDefaultERC20Gateway,
		},
		{
			name: "erc20 repeated canonical router",
			stack: []common.Address{
				deriwDevCanonicalGatewayRouter,
				deriwDevRouter,
				deriwDevCanonicalGatewayRouter,
				deriwDevDefaultERC20Gateway,
			},
			caller: deriwDevDefaultERC20Gateway,
		},
		{
			name: "erc20 repeated gateway",
			stack: []common.Address{
				deriwDevDefaultERC20Gateway,
				deriwDevRouter,
				deriwDevCanonicalGatewayRouter,
				deriwDevDefaultERC20Gateway,
			},
			caller: deriwDevDefaultERC20Gateway,
		},
		{
			name: "erc20 caller mismatch",
			stack: []common.Address{
				deriwDevRouter,
				deriwDevCanonicalGatewayRouter,
				deriwDevDefaultERC20Gateway,
			},
			caller: helper,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := authorizedNormalizedDeriwSendPath(test.stack, test.caller, deriwDevRoutes); got != test.want {
				t.Fatalf("authorized = %v, want %v for %v", got, test.want, test.stack)
			}
		})
	}
}

func TestDeriwSendPathConfigurationsDoNotCrossEnvironments(t *testing.T) {
	devERC20Path := []common.Address{
		deriwDevRoutes.Router,
		deriwDevRoutes.CanonicalGatewayRouter,
		deriwDevDefaultERC20Gateway,
	}
	prodERC20Path := []common.Address{
		deriwProdRoutes.Router,
		deriwProdRoutes.CanonicalGatewayRouter,
		deriwProdDefaultERC20Gateway,
	}

	if !authorizedNormalizedDeriwSendPath(devERC20Path, deriwDevDefaultERC20Gateway, deriwDevRoutes) {
		t.Fatal("development ERC-20 route was rejected by its configuration")
	}
	if authorizedNormalizedDeriwSendPath(devERC20Path, deriwDevDefaultERC20Gateway, deriwProdRoutes) {
		t.Fatal("development ERC-20 route was accepted by production configuration")
	}
	if !authorizedNormalizedDeriwSendPath(prodERC20Path, deriwProdDefaultERC20Gateway, deriwProdRoutes) {
		t.Fatal("production ERC-20 route was rejected by its configuration")
	}
	if authorizedNormalizedDeriwSendPath(prodERC20Path, deriwProdDefaultERC20Gateway, deriwDevRoutes) {
		t.Fatal("production ERC-20 route was accepted by development configuration")
	}
}

func TestEnforceDeriwSendPathIsVersionedAndFirst(t *testing.T) {
	if err := enforceDeriwSendPath(nil); !errors.Is(err, errUnauthorizedDeriwSendPath) {
		t.Fatalf("nil context error = %v, want %v", err, errUnauthorizedDeriwSendPath)
	}
	if err := enforceDeriwSendPathForOperation(nil, deriwSendOperationETHWithdrawal); !errors.Is(err, errUnauthorizedDeriwSendPath) {
		t.Fatalf("nil ETH withdrawal context error = %v, want %v", err, errUnauthorizedDeriwSendPath)
	}

	chainConfig := chaininfo.ArbitrumDevTestChainConfig()
	chainConfig.ChainID = new(big.Int).SetUint64(arbosState.DeriwDevChainID)
	state, _ := arbosState.NewArbosMemoryBackedArbOSStateWithConfig(chainConfig)
	legacy := &Context{State: state}
	if err := enforceDeriwSendPath(legacy); err != nil {
		t.Fatalf("legacy DeriwOS rejected send path: %v", err)
	}
	if err := enforceDeriwSendPathForOperation(legacy, deriwSendOperationETHWithdrawal); err != nil {
		t.Fatalf("legacy DeriwOS rejected ETH withdrawal: %v", err)
	}

	if err := state.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_RouterOnlySends); err != nil {
		t.Fatal(err)
	}
	unauthorized := &Context{State: state, txProcessor: &arbos.TxProcessor{}}
	if err := enforceDeriwSendPath(unauthorized); !errors.Is(err, errUnauthorizedDeriwSendPath) {
		t.Fatalf("unauthorized error = %v, want %v", err, errUnauthorizedDeriwSendPath)
	}
	if err := enforceDeriwSendPathForOperation(unauthorized, deriwSendOperationETHWithdrawal); !errors.Is(err, errUnauthorizedDeriwSendPath) {
		t.Fatalf("DeriwOS 2 ETH withdrawal error = %v, want %v", err, errUnauthorizedDeriwSendPath)
	}

	// The guard must reject before SendTxToL1 attempts its first existing state
	// lookup. This deliberately incomplete context would otherwise be unusable.
	if _, err := (&ArbSys{}).SendTxToL1(unauthorized, nil, big.NewInt(0), common.Address{}, nil); !errors.Is(err, errUnauthorizedDeriwSendPath) {
		t.Fatalf("SendTxToL1 error = %v, want %v", err, errUnauthorizedDeriwSendPath)
	}

	if err := state.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_DirectETHWithdrawals); err != nil {
		t.Fatal(err)
	}
	if err := enforceDeriwSendPathForOperation(unauthorized, deriwSendOperationETHWithdrawal); err != nil {
		t.Fatalf("DeriwOS 3 rejected direct ETH withdrawal: %v", err)
	}
	if err := enforceDeriwSendPath(unauthorized); !errors.Is(err, errUnauthorizedDeriwSendPath) {
		t.Fatalf("DeriwOS 3 raw send error = %v, want %v", err, errUnauthorizedDeriwSendPath)
	}
	if err := enforceDeriwSendPathForOperation(unauthorized, deriwSendOperation(255)); !errors.Is(err, errUnauthorizedDeriwSendPath) {
		t.Fatalf("DeriwOS 3 unknown operation error = %v, want %v", err, errUnauthorizedDeriwSendPath)
	}
	if _, err := (&ArbSys{}).SendTxToL1(unauthorized, nil, big.NewInt(0), common.Address{}, nil); !errors.Is(err, errUnauthorizedDeriwSendPath) {
		t.Fatalf("DeriwOS 3 SendTxToL1 error = %v, want %v", err, errUnauthorizedDeriwSendPath)
	}

	routerFrame := vm.NewContract(
		common.Address{},
		deriwDevRouter,
		uint256.NewInt(0),
		1_000_000,
		nil,
	)
	authorized := &Context{
		caller: deriwDevRouter,
		State:  state,
		txProcessor: &arbos.TxProcessor{
			Contracts: []*vm.Contract{routerFrame},
		},
	}
	if err := enforceDeriwSendPath(authorized); err != nil {
		t.Fatalf("approved direct route rejected: %v", err)
	}
}
