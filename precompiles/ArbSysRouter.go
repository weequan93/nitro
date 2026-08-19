// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package precompiles

import (
	"errors"
	"fmt"

	"github.com/ethereum/go-ethereum/common"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/deriwpolicy"
)

var errUnauthorizedDeriwSendPath = errors.New("unauthorized L3-to-parent send path")

type deriwSendOperation uint8

const (
	deriwSendOperationRaw deriwSendOperation = iota
	deriwSendOperationETHWithdrawal
)

// deriwSendPathFrame is the subset of an EVM contract frame used to normalize
// the route to ArbSys. Generic input keeps the consensus code tied to the real
// vm.Contract API while allowing focused tests of delegate/callcode handling.
type deriwSendPathFrame interface {
	Address() common.Address
	IsDelegateOrCallcode() bool
}

func normalizedDeriwSendPath[T deriwSendPathFrame](frames []T) []common.Address {
	normalized := make([]common.Address, 0, len(frames))
	for _, frame := range frames {
		if frame.IsDelegateOrCallcode() {
			continue
		}
		normalized = append(normalized, frame.Address())
	}
	return normalized
}

func hasExactDeriwSendSuffix(actual []common.Address, expected ...common.Address) bool {
	if len(actual) < len(expected) {
		return false
	}
	start := len(actual) - len(expected)
	for index := range expected {
		if actual[start+index] != expected[index] {
			return false
		}
	}
	return true
}

func occursExactlyOnce(stack []common.Address, target common.Address) bool {
	count := 0
	for _, current := range stack {
		if current == target {
			count++
		}
	}
	return count == 1
}

func authorizedNormalizedDeriwSendPath(
	stack []common.Address,
	caller common.Address,
	routes deriwpolicy.RouterOnlySendConfig,
) bool {
	if len(stack) == 0 || stack[len(stack)-1] != caller {
		return false
	}

	// Direct ETH withdrawal or an authenticated arbitrary-message wrapper.
	if hasExactDeriwSendSuffix(stack, routes.Router) && occursExactlyOnce(stack, routes.Router) {
		return true
	}

	// Canonical ERC-20 routes. Unknown and newly configured gateways fail closed.
	for _, gateway := range routes.ApprovedTokenGateways {
		if hasExactDeriwSendSuffix(
			stack,
			routes.Router,
			routes.CanonicalGatewayRouter,
			gateway,
		) && occursExactlyOnce(stack, routes.Router) &&
			occursExactlyOnce(stack, routes.CanonicalGatewayRouter) &&
			occursExactlyOnce(stack, gateway) {
			return true
		}
	}

	return false
}

func authorizedDeriwSendPath(c ctx, routes deriwpolicy.RouterOnlySendConfig) bool {
	if c == nil || c.txProcessor == nil {
		return false
	}
	stack := normalizedDeriwSendPath(c.txProcessor.Contracts)
	return authorizedNormalizedDeriwSendPath(stack, c.caller, routes)
}

func enforceDeriwSendPathForOperation(c ctx, operation deriwSendOperation) error {
	if c == nil || c.State == nil {
		return errUnauthorizedDeriwSendPath
	}
	deriwOSVersion := c.State.DeriwOSVersion()
	if deriwOSVersion < arbosState.DeriwOSVersion_RouterOnlySends {
		return nil
	}
	if operation == deriwSendOperationETHWithdrawal &&
		deriwOSVersion >= arbosState.DeriwOSVersion_DirectETHWithdrawals {
		return nil
	}
	routes, _, configured, err := c.State.ActiveDeriwRouterConfig()
	if err != nil {
		return fmt.Errorf("failed to read active Deriw router configuration: %w", err)
	}
	if !configured || !authorizedDeriwSendPath(c, routes) {
		return errUnauthorizedDeriwSendPath
	}
	return nil
}

func enforceDeriwSendPath(c ctx) error {
	return enforceDeriwSendPathForOperation(c, deriwSendOperationRaw)
}
