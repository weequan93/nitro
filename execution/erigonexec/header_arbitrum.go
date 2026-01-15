//go:build erigon
// +build erigon

package erigonexec

import (
	"errors"
	"math/big"

	ecommon "github.com/erigontech/erigon-lib/common"
	estate "github.com/erigontech/erigon/core/state"
	"github.com/erigontech/erigon/execution/chain"
	etypes "github.com/erigontech/erigon/execution/types"
	gcommon "github.com/ethereum/go-ethereum/common"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/l2pricing"
)

type l1Info struct {
	poster        ecommon.Address
	l1BlockNumber uint64
	l1Timestamp   uint64
}

func buildArbitrumHeader(prev *etypes.Header, l1 l1Info, chainCfg *chain.Config, ibs estate.IntraBlockStateArbitrum) (*etypes.Header, *arbosState.ArbosState, error) {
	if chainCfg == nil {
		return nil, nil, errors.New("erigonexec: missing chain config")
	}
	if ibs == nil {
		return nil, nil, errors.New("erigonexec: missing state")
	}

	stateDB := arbos.NewStateDBAdapter(ibs, nil)
	arbState, err := arbosState.OpenSystemArbosState(stateDB, nil, true)
	if err != nil {
		return nil, nil, err
	}
	baseFee, err := arbState.L2PricingState().BaseFeeWei()
	if err != nil {
		return nil, nil, err
	}

	var parentHash ecommon.Hash
	blockNumber := big.NewInt(0)
	timestamp := l1.l1Timestamp
	coinbase := l1.poster
	extra := make([]byte, 32)
	mixDigest := ecommon.Hash{}

	if prev != nil {
		parentHash = prev.Hash()
		blockNumber.Add(prev.Number, big.NewInt(1))
		if timestamp < prev.Time {
			timestamp = prev.Time
		}
		extra = append([]byte(nil), prev.Extra...)
		mixDigest = prev.MixDigest
	}

	header := &etypes.Header{
		ParentHash:  parentHash,
		UncleHash:   etypes.EmptyUncleHash,
		Coinbase:    coinbase,
		Root:        ecommon.Hash{},
		TxHash:      ecommon.Hash{},
		ReceiptHash: ecommon.Hash{},
		Bloom:       etypes.Bloom{},
		Difficulty:  big.NewInt(1),
		Number:      blockNumber,
		GasLimit:    l2pricing.GethBlockGasLimit,
		GasUsed:     0,
		Time:        timestamp,
		Extra:       extra,
		MixDigest:   mixDigest,
		Nonce:       etypes.BlockNonce{},
		BaseFee:     baseFee,
	}
	return header, arbState, nil
}

func updateArbitrumHeaderInfo(header *etypes.Header, chainCfg *chain.Config, arbState *arbosState.ArbosState) error {
	if header == nil || chainCfg == nil || arbState == nil {
		return errors.New("erigonexec: missing header, chain config, or arbos state")
	}

	var sendRoot ecommon.Hash
	var sendCount uint64
	var nextL1BlockNumber uint64
	var arbosVersion uint64

	if header.Number.Uint64() == chainCfg.ArbitrumChainParams.GenesisBlockNum {
		arbosVersion = chainCfg.ArbitrumChainParams.InitialArbOSVersion
	} else {
		acc := arbState.SendMerkleAccumulator()
		root, err := acc.Root()
		if err != nil {
			return err
		}
		size, err := acc.Size()
		if err != nil {
			return err
		}
		l1Num, err := arbState.Blockhashes().L1BlockNumber()
		if err != nil {
			return err
		}
		sendRoot = toErigonHash(root)
		sendCount = size
		nextL1BlockNumber = l1Num
		arbosVersion = arbState.ArbOSVersion()
	}

	info := etypes.HeaderInfo{
		SendRoot:           sendRoot,
		SendCount:          sendCount,
		L1BlockNumber:      nextL1BlockNumber,
		ArbOSFormatVersion: arbosVersion,
	}
	info.UpdateHeaderWithInfo(header)
	return nil
}

func toErigonHash(hash gcommon.Hash) ecommon.Hash { return ecommon.Hash(hash) }
