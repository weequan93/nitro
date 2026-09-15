// Copyright 2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package gethexec

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/tracing"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/params"
	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/blacklist"
	"github.com/offchainlabs/nitro/cmd/chaininfo"
)

type blacklistConsistencyChain struct{ config *params.ChainConfig }

func (c blacklistConsistencyChain) Config() *params.ChainConfig               { return c.config }
func (c blacklistConsistencyChain) Engine() consensus.Engine                  { return arbos.Engine{} }
func (c blacklistConsistencyChain) CurrentHeader() *types.Header              { return &types.Header{} }
func (c blacklistConsistencyChain) GetHeaderByNumber(uint64) *types.Header    { return &types.Header{} }
func (c blacklistConsistencyChain) GetHeaderByHash(common.Hash) *types.Header { return &types.Header{} }
func (c blacklistConsistencyChain) GetHeader(common.Hash, uint64) *types.Header {
	return &types.Header{}
}

// Compare admission with real committed EVM execution using identical state and
// the same signed transaction, including historical self-bound relationships.
func TestBlacklistAdmissionMatchesCommittedExecution(t *testing.T) {
	for _, version := range []uint64{1, arbosState.DeriwOSVersion_BlacklistBanTypes} {
		for _, scenario := range []string{"sender", "parent", "recipient", "recovery", "self-bound recovery", "delegated recovery", "unauthorized recovery"} {
			for _, flag := range []uint64{0, blacklist.BanFlagAll, blacklist.BanFlagERC20Transfer} {
				if version < arbosState.DeriwOSVersion_BlacklistBanTypes && flag == blacklist.BanFlagERC20Transfer {
					continue
				}
				t.Run(fmt.Sprintf("v%d/%s/flag%d", version, scenario, flag), func(t *testing.T) {
					config := chaininfo.ArbitrumDevTestChainConfig()
					config.ChainID = new(big.Int).SetUint64(arbosState.DeriwDevChainID)
					state, stateDB := arbosState.NewArbosMemoryBackedArbOSStateWithConfig(config)
					require.NoError(t, state.UpgradeDeriwOSVersion(version))
					key, err := crypto.GenerateKey()
					require.NoError(t, err)
					sender := crypto.PubkeyToAddress(key.PublicKey)
					parent := common.HexToAddress("0x2002")
					target := common.HexToAddress("0x3003")
					listed := sender
					var data []byte
					recovery := scenario == "recovery" || scenario == "self-bound recovery" || scenario == "delegated recovery" || scenario == "unauthorized recovery"
					if recovery {
						// Remove a separate existing entry so successful recovery has an
						// observable state change, even when the sender is not banned.
						data = emergencyRemovalTestTx(target).Data()
						require.NoError(t, state.Blacklist().TxFromAddrs().Add(target))
						target = types.DeriwBlacklistAddress
						if scenario != "unauthorized recovery" {
							require.NoError(t, state.Blacklist().BlacklistOwner().Add(sender))
							require.NoError(t, state.Blacklist().BlacklistOwner().Add(parent))
						}
					}
					if scenario == "self-bound recovery" {
						parent = sender
					}
					if scenario == "parent" || scenario == "self-bound recovery" || scenario == "delegated recovery" {
						require.NoError(t, state.SubAccount().AllowedAddress().Add(target))
						require.NoError(t, state.SubAccount().BindRelationLegacy(parent, sender, big.NewInt(0)))
					}
					if scenario == "parent" {
						listed = parent
					}
					if scenario == "recipient" {
						listed = target
					}
					if flag != 0 {
						// Deliberately use the opposite historical direction to exercise
						// the union rule as well as the exact flag comparison.
						if scenario == "recipient" {
							require.NoError(t, state.Blacklist().TxFromAddrs().Add(listed))
						} else {
							require.NoError(t, state.Blacklist().TxToAddrs().Add(listed))
						}
						if version >= arbosState.DeriwOSVersion_BlacklistBanTypes {
							require.NoError(t, state.Blacklist().SetBanType(listed, flag))
						}
					}
					stateDB.SetBalance(sender, uint256.NewInt(1_000_000), tracing.BalanceChangeUnspecified)
					header := &types.Header{Number: big.NewInt(1), Time: 1, Difficulty: big.NewInt(1), BaseFee: big.NewInt(0), GasLimit: 10_000_000}
					types.HeaderInfo{ArbOSFormatVersion: state.ArbOSVersion()}.UpdateHeaderWithInfo(header)
					tx, err := types.SignTx(types.NewTx(&types.LegacyTx{To: &target, Gas: 500_000, GasPrice: big.NewInt(0), Data: data}), types.MakeSigner(config, header.Number, header.Time, state.ArbOSVersion()), key)
					require.NoError(t, err)
					admissionErr := PreCheckTx(nil, config, header, stateDB, state, tx, nil, &TxPreCheckerConfig{Strictness: TxPreCheckerStrictnessNone})
					require.Zero(t, stateDB.GetNonce(sender), "admission mutated the sender nonce")
					require.Equal(t, uint64(1_000_000), stateDB.GetBalance(sender).Uint64(), "admission charged gas")
					evm := vm.NewEVM(core.NewEVMBlockContext(header, blacklistConsistencyChain{config}, nil), stateDB, config, vm.Config{})
					gasPool := core.GasPool(header.GasLimit)
					receipt, result, err := core.ApplyTransaction(evm, &gasPool, stateDB, header, tx, &header.GasUsed)
					require.NoError(t, err)
					blocked := flag == blacklist.BanFlagAll && scenario != "recovery" && scenario != "self-bound recovery"
					if blocked {
						require.ErrorIs(t, admissionErr, ErrTxBlacklist)
						require.ErrorIs(t, result.Err, vm.ErrDeriwBlacklisted)
						require.Equal(t, uint64(types.ReceiptStatusFailed), receipt.Status)
						require.Equal(t, tx.Gas(), receipt.GasUsed)
					} else {
						require.NoError(t, admissionErr)
						if scenario == "unauthorized recovery" {
							require.Error(t, result.Err, "ordinary precompile authorization must still reject strangers")
							require.NotErrorIs(t, result.Err, vm.ErrDeriwBlacklisted)
						} else {
							require.NoError(t, result.Err)
							require.Equal(t, uint64(types.ReceiptStatusSuccessful), receipt.Status)
						}
					}
				})
			}
		}
	}
}
