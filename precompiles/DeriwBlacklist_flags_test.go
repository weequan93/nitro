package precompiles

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/stretchr/testify/require"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/blacklist"
	"github.com/offchainlabs/nitro/cmd/chaininfo"
	"github.com/offchainlabs/nitro/solgen/go/precompilesgen"
)

func TestBlacklistBanTypeReplacesBothDirections(t *testing.T) {
	for _, direction := range []string{"from", "to"} {
		t.Run(direction, func(t *testing.T) {
			chainConfig := chaininfo.ArbitrumDevTestChainConfig()
			chainConfig.ChainID = new(big.Int).SetUint64(arbosState.DeriwDevChainID)
			evm := newMockEVMForTestingWithConfigs(chainConfig, chainConfig)
			c := testContext(common.HexToAddress("0x1001"), evm)
			owner := DeriwBlacklist{}
			public := DeriwBlacklistPublic{}
			address := common.HexToAddress("0x2002")
			// Historical entries become type 1 without a migration.
			require.NoError(t, owner.AddBlacklistTxFrom(c, evm, address))
			require.NoError(t, owner.AddBlacklistTxTo(c, evm, address))
			require.NoError(t, c.State.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_BlacklistBanTypes))
			flag, err := public.GetBlacklistBanFlag(c, evm, address)
			require.NoError(t, err)
			require.Equal(t, blacklist.BanFlagAll, flag)

			add := owner.AddBlacklistTxFromWithFlag
			legacyAdd := owner.AddBlacklistTxFrom
			if direction == "to" {
				add = owner.AddBlacklistTxToWithFlag
				legacyAdd = owner.AddBlacklistTxTo
			}
			require.NoError(t, add(c, evm, address, blacklist.BanFlagERC20Transfer))
			require.NoError(t, add(c, evm, address, blacklist.BanFlagERC20Transfer)) // idempotent
			flag, err = public.GetBlacklistBanFlag(c, evm, address)
			require.NoError(t, err)
			require.Equal(t, blacklist.BanFlagERC20Transfer, flag)
			require.Equal(t, blacklist.BanFlagERC20Transfer, c.State.Blacklist().BanTypeFree(address))
			for _, get := range []func(ctx, mech, uint64) ([]common.Address, error){public.GetBlacklistTxFromWithFlag, public.GetBlacklistTxToWithFlag} {
				members, err := get(c, evm, blacklist.BanFlagAll)
				require.NoError(t, err)
				require.Empty(t, members)
				members, err = get(c, evm, blacklist.BanFlagERC20Transfer)
				require.NoError(t, err)
				require.Equal(t, []common.Address{address}, members)
			}
			// Both the old API and the new API must replace transfer metadata.
			for _, restore := range []func() error{
				func() error { return legacyAdd(c, evm, address) },
				func() error { return add(c, evm, address, blacklist.BanFlagAll) },
			} {
				require.NoError(t, restore())
				require.Equal(t, blacklist.BanFlagAll, c.State.Blacklist().BanTypeFree(address))
				for _, get := range []func(ctx, mech, uint64) ([]common.Address, error){public.GetBlacklistTxFromWithFlag, public.GetBlacklistTxToWithFlag} {
					members, err := get(c, evm, blacklist.BanFlagERC20Transfer)
					require.NoError(t, err)
					require.Empty(t, members)
					members, err = get(c, evm, blacklist.BanFlagAll)
					require.NoError(t, err)
					require.Equal(t, []common.Address{address}, members)
				}
				require.NoError(t, add(c, evm, address, blacklist.BanFlagERC20Transfer))
			}
			// Removal affects only its direction; removing the final entry clears the type.
			require.Error(t, owner.RemoveBlacklistTxFromWithFlag(c, evm, address, blacklist.BanFlagAll))
			require.NoError(t, owner.RemoveBlacklistTxFromWithFlag(c, evm, address, blacklist.BanFlagERC20Transfer))
			flag, err = public.GetBlacklistBanFlag(c, evm, address)
			require.NoError(t, err)
			require.Equal(t, blacklist.BanFlagERC20Transfer, flag)
			require.NoError(t, owner.RemoveBlacklistTxToWithFlag(c, evm, address, blacklist.BanFlagERC20Transfer))
			flag, err = public.GetBlacklistBanFlag(c, evm, address)
			require.NoError(t, err)
			require.Zero(t, flag)
			require.Error(t, owner.RemoveBlacklistTxToWithFlag(c, evm, address, blacklist.BanFlagERC20Transfer))
		})
	}
}

func TestBlacklistBanTypeValidationAndProtection(t *testing.T) {
	chainConfig := chaininfo.ArbitrumDevTestChainConfig()
	chainConfig.ChainID = new(big.Int).SetUint64(arbosState.DeriwDevChainID)
	evm := newMockEVMForTestingWithConfigs(chainConfig, chainConfig)
	c := testContext(common.HexToAddress("0x1001"), evm)
	owner := DeriwBlacklist{}
	public := DeriwBlacklistPublic{}
	require.NoError(t, c.State.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_BlacklistBanTypes))
	address := common.HexToAddress("0x2002")
	require.NoError(t, owner.AddBlacklistTxFromWithFlag(c, evm, address, blacklist.BanFlagERC20Transfer))
	for _, invalid := range []uint64{0, 3, 255, 256, ^uint64(0)} {
		require.ErrorIs(t, owner.AddBlacklistTxFromWithFlag(c, evm, address, invalid), blacklist.ErrInvalidBanFlag)
		require.ErrorIs(t, owner.AddBlacklistTxToWithFlag(c, evm, address, invalid), blacklist.ErrInvalidBanFlag)
		require.ErrorIs(t, owner.RemoveBlacklistTxFromWithFlag(c, evm, address, invalid), blacklist.ErrInvalidBanFlag)
		require.ErrorIs(t, owner.RemoveBlacklistTxToWithFlag(c, evm, address, invalid), blacklist.ErrInvalidBanFlag)
		_, err := public.IsBlacklistTxFromWithFlag(c, evm, address, invalid)
		require.ErrorIs(t, err, blacklist.ErrInvalidBanFlag)
		_, err = public.GetBlacklistTxToWithFlag(c, evm, invalid)
		require.ErrorIs(t, err, blacklist.ErrInvalidBanFlag)
	}
	flag, err := public.GetBlacklistBanFlag(c, evm, address)
	require.NoError(t, err)
	require.Equal(t, blacklist.BanFlagERC20Transfer, flag)

	// Metadata may name a protected address; promoting it to an enforced ban cannot.
	protected := types.DeriwBlacklistAddress
	require.NoError(t, owner.AddBlacklistTxFromWithFlag(c, evm, protected, blacklist.BanFlagERC20Transfer))
	require.Error(t, owner.AddBlacklistTxToWithFlag(c, evm, protected, blacklist.BanFlagAll))
	require.Error(t, owner.AddBlacklistTxFrom(c, evm, protected))
	flag, err = public.GetBlacklistBanFlag(c, evm, protected)
	require.NoError(t, err)
	require.Equal(t, blacklist.BanFlagERC20Transfer, flag)
	require.Equal(t, blacklist.BanFlagERC20Transfer, c.State.Blacklist().BanTypeFree(protected))
}

func TestBlacklistBanTypeABIAuthorizationAndActivation(t *testing.T) {
	chainConfig := chaininfo.ArbitrumDevTestChainConfig()
	chainConfig.ChainID = new(big.Int).SetUint64(arbosState.DeriwDevChainID)
	evm := newMockEVMForTestingWithConfigs(chainConfig, chainConfig)
	owner := common.HexToAddress("0x1001")
	stranger := common.HexToAddress("0x2002")
	address := common.HexToAddress("0x3003")
	c := testContext(owner, evm)
	require.NoError(t, c.State.Blacklist().BlacklistOwner().Add(owner))
	ownerABI, err := precompilesgen.DeriwBlacklistMetaData.GetAbi()
	require.NoError(t, err)
	publicABI, err := precompilesgen.DeriwBlacklistPublicMetaData.GetAbi()
	require.NoError(t, err)
	contracts := Precompiles()
	call := func(contract common.Address, caller common.Address, readOnly bool, data []byte) ([]byte, error) {
		result, _, _, err := contracts[contract].Call(data, contract, caller, big.NewInt(0), readOnly, 10_000_000, evm)
		return result, err
	}
	add, err := ownerABI.Pack("addBlacklistTxFromWithFlag", address, blacklist.BanFlagERC20Transfer)
	require.NoError(t, err)
	get, err := publicABI.Pack("getBlacklistBanFlag", address)
	require.NoError(t, err)
	// New selectors must not become callable when replaying older versions.
	for version := uint64(0); version < arbosState.DeriwOSVersion_BlacklistBanTypes; version++ {
		require.NoError(t, c.State.UpgradeDeriwOSVersion(version))
		_, err = call(types.DeriwBlacklistAddress, owner, false, add)
		require.Error(t, err)
		_, err = call(types.DeriwBlacklistPublicAddress, stranger, true, get)
		require.Error(t, err)
	}
	require.NoError(t, c.State.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_BlacklistBanTypes))
	_, err = call(types.DeriwBlacklistAddress, stranger, false, add)
	require.Error(t, err)
	_, err = call(types.DeriwBlacklistAddress, owner, true, add)
	require.ErrorIs(t, err, vm.ErrExecutionReverted)
	flag, err := c.State.Blacklist().BanType(address)
	require.NoError(t, err)
	require.Zero(t, flag, "rejected calls must not store a ban")
	_, err = call(types.DeriwBlacklistAddress, owner, false, add)
	require.NoError(t, err)
	output, err := call(types.DeriwBlacklistPublicAddress, stranger, true, get)
	require.NoError(t, err)
	values, err := publicABI.Unpack("getBlacklistBanFlag", output)
	require.NoError(t, err)
	require.Equal(t, []interface{}{blacklist.BanFlagERC20Transfer}, values)
}

func TestBlacklistBanTypeSelectorsPreserveHistoricalGas(t *testing.T) {
	chainConfig := chaininfo.ArbitrumDevTestChainConfig()
	chainConfig.ChainID = new(big.Int).SetUint64(arbosState.DeriwDevChainID)
	evm := newMockEVMForTestingWithConfigs(chainConfig, chainConfig)
	caller := common.HexToAddress("0x1001")
	c := testContext(caller, evm)
	contracts := Precompiles()
	for version := uint64(0); version < arbosState.DeriwOSVersion_BlacklistBanTypes; version++ {
		require.NoError(t, c.State.UpgradeDeriwOSVersion(version))
		gated := 0
		for _, address := range []common.Address{types.DeriwBlacklistAddress, types.DeriwBlacklistPublicAddress} {
			precompile := contracts[address].Precompile()
			for _, method := range precompile.methods {
				if method.deriwOSVersion == 0 {
					continue
				}
				require.Equal(t, arbosState.DeriwOSVersion_BlacklistBanTypes, method.deriwOSVersion)
				gated++
				args := make([]interface{}, len(method.template.Inputs))
				for i, arg := range method.template.Inputs {
					if arg.Type.String() == "address" {
						args[i] = caller
					} else {
						args[i] = blacklist.BanFlagERC20Transfer
					}
				}
				payload, err := method.template.Inputs.Pack(args...)
				require.NoError(t, err)
				input := append(append([]byte{}, method.template.ID...), payload...)
				output, remaining, used, err := precompile.Call(input, address, caller, big.NewInt(0), false, 100_000, evm)
				require.ErrorIs(t, err, vm.ErrExecutionReverted, method.name)
				require.Empty(t, output, method.name)
				require.Zero(t, remaining, method.name)
				require.Equal(t, uint64(100_000), used.SingleGas(), method.name)
			}
		}
		require.Equal(t, 14, gated)
	}
}

func TestBlacklistFeeAccountRejectionRequiresBanFlagAll(t *testing.T) {
	chainConfig := chaininfo.ArbitrumDevTestChainConfig()
	chainConfig.ChainID = new(big.Int).SetUint64(arbosState.DeriwDevChainID)
	evm := newMockEVMForTestingWithConfigs(chainConfig, chainConfig)
	c := testContext(common.HexToAddress("0x1001"), evm)
	require.NoError(t, c.State.UpgradeDeriwOSVersion(arbosState.DeriwOSVersion_BlacklistBanTypes))
	metadata := common.HexToAddress("0x2002")
	blocked := common.HexToAddress("0x3003")
	blacklistOwner := DeriwBlacklist{}
	require.NoError(t, blacklistOwner.AddBlacklistTxFromWithFlag(c, evm, metadata, blacklist.BanFlagERC20Transfer))
	require.NoError(t, blacklistOwner.AddBlacklistTxToWithFlag(c, evm, blocked, blacklist.BanFlagAll))
	require.True(t, c.State.Blacklist().TxFromAddrs().IsMemberFree(metadata))
	owner := ArbOwner{}
	for _, set := range []func(ctx, mech, addr) error{owner.SetNetworkFeeAccount, owner.SetInfraFeeAccount} {
		require.NoError(t, set(c, evm, metadata), "other flags must not trigger fee-account rejection")
		require.Error(t, set(c, evm, blocked), "BanFlagAll must still reject fee accounts")
	}
	// Legacy removal also clears the stored type after the final membership.
	require.NoError(t, blacklistOwner.RemoveBlacklistTxFrom(c, evm, metadata))
	flag, err := c.State.Blacklist().BanType(metadata)
	require.NoError(t, err)
	require.Zero(t, flag)
}
