// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

// Package deriwpolicy contains deterministic, chain-specific Deriw consensus
// configuration shared by ArbOS state validation and precompile execution.
package deriwpolicy

import (
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
)

const MaxApprovedTokenGateways = 32

const (
	DevChainID  uint64 = 18417507517
	TestChainID uint64 = 2885
	ProdChainID uint64 = 2886

	DevRouterAddress                 = "0x32068069f13191B57c03Eee8531a8C82b26d12B9"
	DevCanonicalGatewayRouterAddress = "0x9fF6747040212f6C21fCe2E8ED0B7B05bA5B4a5d"
	DevDefaultERC20GatewayAddress    = "0x3fc1626EE794Aa6CdE8d8987F4B67BC1bC217679"

	ProdRouterAddress                 = "0x8fb358679749FD952Ea5f090b0eA3675722B08F5"
	ProdCanonicalGatewayRouterAddress = "0xb85b91A9362e296243360e83Cb0792a87Dc32712"
	ProdDefaultERC20GatewayAddress    = "0x6121117fCcEcdD6dFa7B3230Eacd4f53e12905Db"
)

// RouterOnlySendConfig is the complete approved route set for one Deriw chain.
// The resolver returns a new ApprovedTokenGateways slice on every call so no
// mutable global consensus configuration is exposed.
type RouterOnlySendConfig struct {
	Environment            string
	Router                 common.Address
	CanonicalGatewayRouter common.Address
	ApprovedTokenGateways  []common.Address
}

// ValidateRouterOnlySendConfig validates the complete route before it can be
// committed to consensus state. Protected route addresses must be distinct so
// the call-stack uniqueness checks cannot become ambiguous.
func ValidateRouterOnlySendConfig(config RouterOnlySendConfig) error {
	if config.Router == (common.Address{}) {
		return fmt.Errorf("Deriw router must not be the zero address")
	}
	if config.CanonicalGatewayRouter == (common.Address{}) {
		return fmt.Errorf("canonical gateway router must not be the zero address")
	}
	if config.Router == config.CanonicalGatewayRouter {
		return fmt.Errorf("Deriw router and canonical gateway router must be distinct")
	}
	if len(config.ApprovedTokenGateways) == 0 {
		return fmt.Errorf("at least one token gateway must be approved")
	}
	if len(config.ApprovedTokenGateways) > MaxApprovedTokenGateways {
		return fmt.Errorf(
			"too many approved token gateways: got %v, maximum is %v",
			len(config.ApprovedTokenGateways),
			MaxApprovedTokenGateways,
		)
	}

	seen := map[common.Address]struct{}{
		config.Router:                 {},
		config.CanonicalGatewayRouter: {},
	}
	for _, gateway := range config.ApprovedTokenGateways {
		if gateway == (common.Address{}) {
			return fmt.Errorf("approved token gateway must not be the zero address")
		}
		if _, duplicate := seen[gateway]; duplicate {
			return fmt.Errorf("protected route address %v appears more than once", gateway)
		}
		seen[gateway] = struct{}{}
	}
	return nil
}

// RouterOnlySendConfigForChainID resolves the consensus route from the on-chain
// chain ID. Unknown or incompletely audited environments fail closed.
func RouterOnlySendConfigForChainID(chainID *big.Int) (RouterOnlySendConfig, bool) {
	if chainID == nil || !chainID.IsUint64() {
		return RouterOnlySendConfig{}, false
	}

	switch chainID.Uint64() {
	case DevChainID:
		return RouterOnlySendConfig{
			Environment:            "dev",
			Router:                 common.HexToAddress(DevRouterAddress),
			CanonicalGatewayRouter: common.HexToAddress(DevCanonicalGatewayRouterAddress),
			ApprovedTokenGateways: []common.Address{
				common.HexToAddress(DevDefaultERC20GatewayAddress),
			},
		}, true
	case ProdChainID:
		return RouterOnlySendConfig{
			Environment:            "prod",
			Router:                 common.HexToAddress(ProdRouterAddress),
			CanonicalGatewayRouter: common.HexToAddress(ProdCanonicalGatewayRouterAddress),
			ApprovedTokenGateways: []common.Address{
				common.HexToAddress(ProdDefaultERC20GatewayAddress),
			},
		}, true
	case TestChainID:
		// Test uses different deployments. Keep activation disabled until its
		// router, canonical router, and complete gateway set are verified.
		return RouterOnlySendConfig{}, false
	default:
		return RouterOnlySendConfig{}, false
	}
}
