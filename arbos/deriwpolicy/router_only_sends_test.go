// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package deriwpolicy

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestRouterOnlySendConfigForChainID(t *testing.T) {
	tests := []struct {
		name            string
		chainID         *big.Int
		configured      bool
		environment     string
		router          string
		canonicalRouter string
		gateway         string
	}{
		{
			name:            "dev",
			chainID:         new(big.Int).SetUint64(DevChainID),
			configured:      true,
			environment:     "dev",
			router:          DevRouterAddress,
			canonicalRouter: DevCanonicalGatewayRouterAddress,
			gateway:         DevDefaultERC20GatewayAddress,
		},
		{name: "test remains fail closed", chainID: new(big.Int).SetUint64(TestChainID)},
		{
			name:            "prod",
			chainID:         new(big.Int).SetUint64(ProdChainID),
			configured:      true,
			environment:     "prod",
			router:          ProdRouterAddress,
			canonicalRouter: ProdCanonicalGatewayRouterAddress,
			gateway:         ProdDefaultERC20GatewayAddress,
		},
		{name: "unknown", chainID: big.NewInt(1337)},
		{name: "nil"},
		{name: "larger than uint64", chainID: new(big.Int).Lsh(big.NewInt(1), 65)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			config, configured := RouterOnlySendConfigForChainID(test.chainID)
			if configured != test.configured {
				t.Fatalf("configured = %v, want %v", configured, test.configured)
			}
			if !configured {
				return
			}
			if config.Environment != test.environment ||
				config.Router != common.HexToAddress(test.router) ||
				config.CanonicalGatewayRouter != common.HexToAddress(test.canonicalRouter) ||
				len(config.ApprovedTokenGateways) != 1 ||
				config.ApprovedTokenGateways[0] != common.HexToAddress(test.gateway) {
				t.Fatalf("unexpected route configuration: %+v", config)
			}
		})
	}
}

func TestRouterOnlySendConfigReturnsIndependentGatewaySlices(t *testing.T) {
	chainID := new(big.Int).SetUint64(DevChainID)
	first, configured := RouterOnlySendConfigForChainID(chainID)
	if !configured {
		t.Fatal("development route is not configured")
	}
	first.ApprovedTokenGateways[0] = common.Address{}

	second, configured := RouterOnlySendConfigForChainID(chainID)
	if !configured || second.ApprovedTokenGateways[0] != common.HexToAddress(DevDefaultERC20GatewayAddress) {
		t.Fatal("caller mutated shared consensus route configuration")
	}
}

func TestValidateRouterOnlySendConfigRejectsAmbiguousRoutes(t *testing.T) {
	router := common.HexToAddress("0x1001")
	canonicalRouter := common.HexToAddress("0x1002")
	gateway := common.HexToAddress("0x1003")
	valid := RouterOnlySendConfig{
		Router:                 router,
		CanonicalGatewayRouter: canonicalRouter,
		ApprovedTokenGateways:  []common.Address{gateway},
	}
	if err := ValidateRouterOnlySendConfig(valid); err != nil {
		t.Fatalf("valid route rejected: %v", err)
	}

	invalid := []RouterOnlySendConfig{
		{},
		{Router: router, CanonicalGatewayRouter: canonicalRouter},
		{Router: router, CanonicalGatewayRouter: router, ApprovedTokenGateways: []common.Address{gateway}},
		{Router: router, CanonicalGatewayRouter: canonicalRouter, ApprovedTokenGateways: []common.Address{common.Address{}}},
		{Router: router, CanonicalGatewayRouter: canonicalRouter, ApprovedTokenGateways: []common.Address{gateway, gateway}},
		{Router: router, CanonicalGatewayRouter: canonicalRouter, ApprovedTokenGateways: []common.Address{router}},
	}
	for index, config := range invalid {
		if err := ValidateRouterOnlySendConfig(config); err == nil {
			t.Fatalf("invalid route %v was accepted: %+v", index, config)
		}
	}
}
