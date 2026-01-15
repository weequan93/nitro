//go:build erigon
// +build erigon

package arbos

import (
	"context"
	"errors"
	"sort"

	"github.com/holiman/uint256"

	ecommon "github.com/erigontech/erigon-lib/common"
	estate "github.com/erigontech/erigon/core/state"
	"github.com/erigontech/erigon/core/vm/evmtypes"
	"github.com/erigontech/erigon/db/kv"
	dbstate "github.com/erigontech/erigon/db/state"
	echain "github.com/erigontech/erigon/execution/chain"

	gcommon "github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/tracing"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbos/burn"
	"github.com/offchainlabs/nitro/arbos/retryables"
	"github.com/offchainlabs/nitro/statetransfer"
	"github.com/offchainlabs/nitro/util/arbmath"
)

func InitializeArbosInDatabase(
	ibs estate.IntraBlockStateArbitrum,
	domains *dbstate.SharedDomains,
	putDel kv.TemporalPutDel,
	initData statetransfer.InitDataReader,
	chainConfig *echain.Config,
	initMessage *arbostypes.ParsedInitMessage,
	timestamp uint64,
	accountsPerSync uint,
) (ecommon.Hash, error) {
	if ibs == nil {
		return ecommon.Hash{}, errors.New("arbos: missing ibs")
	}
	if initData == nil {
		return ecommon.Hash{}, errors.New("arbos: missing init data")
	}
	if chainConfig == nil {
		return ecommon.Hash{}, errors.New("arbos: missing chain config")
	}
	if initMessage == nil {
		return ecommon.Hash{}, errors.New("arbos: missing init message")
	}
	if putDel == nil {
		return ecommon.Hash{}, errors.New("arbos: missing temporal put/del")
	}

	stateDB := newStateDBAdapter(ibs, nil)
	chainCfg, err := loadGethChainConfig(stateDB, chainConfig)
	if err != nil {
		return ecommon.Hash{}, err
	}

	burner := burn.NewSystemBurner(nil, false)
	arbState, err := arbosState.InitializeArbosState(stateDB, burner, chainCfg, initMessage)
	if err != nil {
		return ecommon.Hash{}, err
	}

	chainOwner, err := initData.GetChainOwner()
	if err != nil {
		return ecommon.Hash{}, err
	}
	if chainOwner != (gcommon.Address{}) {
		if err := arbState.ChainOwners().Add(chainOwner); err != nil {
			return ecommon.Hash{}, err
		}
	}

	addrTable := arbState.AddressTable()
	addrTableSize, err := addrTable.Size()
	if err != nil {
		return ecommon.Hash{}, err
	}
	if addrTableSize != 0 {
		return ecommon.Hash{}, errors.New("address table must be empty")
	}
	addressReader, err := initData.GetAddressTableReader()
	if err != nil {
		return ecommon.Hash{}, err
	}
	for i := uint64(0); addressReader.More(); i++ {
		addr, err := addressReader.GetNext()
		if err != nil {
			return ecommon.Hash{}, err
		}
		slot, err := addrTable.Register(*addr)
		if err != nil {
			return ecommon.Hash{}, err
		}
		if i != slot {
			return ecommon.Hash{}, errors.New("address table slot mismatch")
		}
	}
	if err := addressReader.Close(); err != nil {
		return ecommon.Hash{}, err
	}

	retryableReader, err := initData.GetRetryableDataReader()
	if err != nil {
		return ecommon.Hash{}, err
	}
	if err := initializeRetryables(stateDB, arbState.RetryableState(), retryableReader, timestamp); err != nil {
		return ecommon.Hash{}, err
	}

	accountDataReader, err := initData.GetAccountDataReader()
	if err != nil {
		return ecommon.Hash{}, err
	}

	ibsImpl, ok := ibs.(*estate.IntraBlockState)
	if !ok {
		return ecommon.Hash{}, errors.New("arbos: unsupported ibs type")
	}
	txNum := uint64(0)
	if domains != nil {
		txNum = domains.TxNum()
	}
	stateWriter := estate.NewWriter(putDel, nil, txNum)
	blockCtx := evmtypes.BlockContext{
		BlockNumber:  chainConfig.ArbitrumChainParams.GenesisBlockNum,
		Time:         timestamp,
		ArbOSVersion: chainConfig.ArbitrumChainParams.InitialArbOSVersion,
	}
	rules := blockCtx.Rules(chainConfig)
	flush := func() error {
		if err := ibsImpl.CommitBlock(rules, stateWriter); err != nil {
			return err
		}
		ibsImpl.Reset()
		return nil
	}

	accountsRead := uint(0)
	for accountDataReader.More() {
		account, err := accountDataReader.GetNext()
		if err != nil {
			return ecommon.Hash{}, err
		}
		if err := initializeArbosAccount(arbState, *account); err != nil {
			return ecommon.Hash{}, err
		}
		stateDB.AddBalance(account.Addr, uint256.MustFromBig(account.EthBalance), tracing.BalanceChangeUnspecified)
		stateDB.SetNonce(account.Addr, account.Nonce)
		if account.ContractInfo != nil {
			stateDB.SetCode(account.Addr, account.ContractInfo.Code)
			for key, value := range account.ContractInfo.ContractStorage {
				stateDB.SetState(account.Addr, key, value)
			}
		}
		accountsRead++
		if accountsPerSync > 0 && (accountsRead%accountsPerSync == 0) {
			if err := flush(); err != nil {
				return ecommon.Hash{}, err
			}
		}
	}
	if err := accountDataReader.Close(); err != nil {
		return ecommon.Hash{}, err
	}
	if err := flush(); err != nil {
		return ecommon.Hash{}, err
	}

	var root ecommon.Hash
	if domains != nil {
		rootBytes, err := domains.ComputeCommitment(context.Background(), true, blockCtx.BlockNumber, txNum, "arbos init")
		if err != nil {
			return ecommon.Hash{}, err
		}
		if len(rootBytes) > 0 {
			copy(root[:], rootBytes)
		}
	}
	return root, nil
}

func initializeRetryables(stateDB *stateDBAdapter, rs *retryables.RetryableState, initData statetransfer.RetryableDataReader, currentTimestamp uint64) error {
	var retryablesList []*statetransfer.InitializationDataForRetryable
	for initData.More() {
		retryable, err := initData.GetNext()
		if err != nil {
			return err
		}
		if retryable.Timeout <= currentTimestamp {
			stateDB.AddBalance(retryable.Beneficiary, uint256.MustFromBig(retryable.Callvalue), tracing.BalanceChangeUnspecified)
			continue
		}
		retryablesList = append(retryablesList, retryable)
	}
	sort.Slice(retryablesList, func(i, j int) bool {
		a := retryablesList[i]
		b := retryablesList[j]
		if a.Timeout == b.Timeout {
			return arbmath.BigLessThan(a.Id.Big(), b.Id.Big())
		}
		return a.Timeout < b.Timeout
	})
	for _, retryable := range retryablesList {
		var to *gcommon.Address
		if retryable.To != (gcommon.Address{}) {
			addr := retryable.To
			to = &addr
		}
		stateDB.AddBalance(retryables.RetryableEscrowAddress(retryable.Id), uint256.MustFromBig(retryable.Callvalue), tracing.BalanceChangeUnspecified)
		if _, err := rs.CreateRetryable(retryable.Id, retryable.Timeout, retryable.From, to, retryable.Callvalue, retryable.Beneficiary, retryable.Calldata); err != nil {
			return err
		}
	}
	return initData.Close()
}

func initializeArbosAccount(arbState *arbosState.ArbosState, account statetransfer.AccountInitializationInfo) error {
	l1pState := arbState.L1PricingState()
	posterTable := l1pState.BatchPosterTable()
	if account.AggregatorInfo != nil {
		isPoster, err := posterTable.ContainsPoster(account.Addr)
		if err != nil {
			return err
		}
		if isPoster {
			poster, err := posterTable.OpenPoster(account.Addr, false)
			if err != nil {
				return err
			}
			if err := poster.SetPayTo(account.AggregatorInfo.FeeCollector); err != nil {
				return err
			}
		}
	}
	return nil
}
