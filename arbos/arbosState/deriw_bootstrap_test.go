// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md
package arbosState

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/offchainlabs/nitro/arbos/burn"
	"github.com/offchainlabs/nitro/cmd/chaininfo"
)

func TestDeriwProductionBootstrapBoundary(t *testing.T) {
	cases := []struct {
		name       string
		version    uint64
		raw        string
		before, at bool
	}{
		{"absent", 60, `{}`, false, false},
		{"zero", 60, `{"deriwOSBootstrapTime":0}`, false, false},
		{"future", 60, `{"deriwOSBootstrapTime":200}`, false, true},
		{"requiresArbOS60", 32, `{"deriwOSBootstrapTime":200}`, false, false},
		{"negative", 60, `{"deriwOSBootstrapTime":-1}`, false, false},
		{"string", 60, `{"deriwOSBootstrapTime":"200"}`, false, false},
		{"overflow", 60, `{"deriwOSBootstrapTime":18446744073709551616}`, false, false},
		{"invalidJson", 60, `{`, false, false},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			cfg := chaininfo.ArbitrumDevTestChainConfig()
			cfg.ChainID = new(big.Int).SetUint64(DeriwProdChainID)
			cfg.ArbitrumChainParams.InitialArbOSVersion = test.version
			st, db := NewArbosMemoryBackedArbOSStateWithConfig(cfg)
			if err := st.SetChainConfig([]byte(test.raw)); err != nil {
				t.Fatal(err)
			}
			for _, pair := range []struct {
				timestamp uint64
				want      bool
			}{{199, test.before}, {200, test.at}} {
				if got, err := DeriwBootstrapAPIsActive(db, pair.timestamp, burn.NewSystemBurner(nil, true)); err != nil || got != pair.want {
					t.Fatalf("at %d: got %v want %v", pair.timestamp, got, pair.want)
				}
			}
		})
	}
	for _, version := range []uint64{1, 5, 6} {
		t.Run(fmt.Sprintf("activeVersion%d", version), func(t *testing.T) {
			cfg := chaininfo.ArbitrumDevTestChainConfig()
			cfg.ChainID = new(big.Int).SetUint64(DeriwProdChainID)
			cfg.ArbitrumChainParams.InitialArbOSVersion = 60
			st, db := NewArbosMemoryBackedArbOSStateWithConfig(cfg)
			if err := st.UpgradeDeriwOSVersion(version); err != nil {
				t.Fatal(err)
			}
			if err := st.SetChainConfig([]byte(`{}`)); err != nil {
				t.Fatal(err)
			}
			if active, err := DeriwBootstrapAPIsActive(db, 0, burn.NewSystemBurner(nil, true)); err != nil || !active {
				t.Fatalf("active DeriwOS %d history was disabled", version)
			}
		})
	}
}
