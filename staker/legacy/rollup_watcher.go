// Copyright 2021-2026, Offchain Labs, Inc.
// For license information, see https://github.com/OffchainLabs/nitro/blob/master/LICENSE.md

package legacystaker

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"sync/atomic"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"

	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/solgen/go/rollup_legacy_gen"
	"github.com/offchainlabs/nitro/util/headerreader"
)

var rollupInitializedID common.Hash
var nodeCreatedID common.Hash
var challengeCreatedID common.Hash

func init() {
	parsedRollup, err := rollup_legacy_gen.RollupUserLogicMetaData.GetAbi()
	if err != nil {
		panic(err)
	}
	rollupInitializedID = parsedRollup.Events["RollupInitialized"].ID
	nodeCreatedID = parsedRollup.Events["NodeCreated"].ID
	challengeCreatedID = parsedRollup.Events["RollupChallengeStarted"].ID
}

type StakerInfo struct {
	Index            uint64
	LatestStakedNode uint64
	AmountStaked     *big.Int
	CurrentChallenge *uint64
}

type RollupWatcher struct {
	RollUpLogic         RollUpLogic
	address             common.Address
	fromBlock           *big.Int
	client              RollupWatcherL1Interface
	baseCallOpts        bind.CallOpts
	unSupportedL3Method atomic.Bool
	supportedL3Method   atomic.Bool
}

type RollUpLogic struct {
	Erc20RollUpUserLogic *rollup_legacy_gen.ERC20RollupUserLogic
	RollUpUserLogic      *rollup_legacy_gen.RollupUserLogic
}

func (r *RollUpLogic) GetNodeCreationBlockForLogLookup(callOpts *bind.CallOpts, nodeNum uint64) (*big.Int, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.GetNodeCreationBlockForLogLookup(callOpts, nodeNum)
	} else {
		return r.RollUpUserLogic.GetNodeCreationBlockForLogLookup(callOpts, nodeNum)
	}
}

func (r *RollUpLogic) CreateChallenge(opts *bind.TransactOpts, stakers [2]common.Address, nodeNums [2]uint64, machineStatuses [2]uint8, globalStates [2]rollup_legacy_gen.GlobalState, numBlocks uint64, secondExecutionHash [32]byte, proposedBlocks [2]*big.Int, wasmModuleRoots [2][32]byte) (*types.Transaction, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.CreateChallenge(opts, stakers, nodeNums, machineStatuses, globalStates, numBlocks, secondExecutionHash, proposedBlocks, wasmModuleRoots)
	} else {
		return r.RollUpUserLogic.CreateChallenge(opts, stakers, nodeNums, machineStatuses, globalStates, numBlocks, secondExecutionHash, proposedBlocks, wasmModuleRoots)
	}
}

func (r *RollUpLogic) FirstUnresolvedNode(opts *bind.CallOpts) (uint64, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.FirstUnresolvedNode(opts)
	} else {
		return r.RollUpUserLogic.FirstUnresolvedNode(opts)
	}
}

func (r *RollUpLogic) BaseStake(opts *bind.CallOpts) (*big.Int, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.BaseStake(opts)
	} else {
		return r.RollUpUserLogic.BaseStake(opts)
	}
}

func (r *RollUpLogic) ChallengeManager(opts *bind.CallOpts) (common.Address, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.ChallengeManager(opts)
	} else {
		return r.RollUpUserLogic.ChallengeManager(opts)
	}
}

func (r *RollUpLogic) GetNode(callOpts *bind.CallOpts, nodeNum uint64) (rollup_legacy_gen.Node, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.GetNode(callOpts, nodeNum)
	} else {
		return r.RollUpUserLogic.GetNode(callOpts, nodeNum)
	}
}

func (r *RollUpLogic) MinimumAssertionPeriod(copts *bind.CallOpts) (*big.Int, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.MinimumAssertionPeriod(copts)
	} else {
		return r.RollUpUserLogic.MinimumAssertionPeriod(copts)
	}
}

func (r *RollUpLogic) StakeOnNewNode(opts *bind.TransactOpts, assertion rollup_legacy_gen.Assertion, expectedNodeHash [32]byte, prevNodeInboxMaxCount *big.Int) (*types.Transaction, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.StakeOnNewNode(opts, assertion, expectedNodeHash, prevNodeInboxMaxCount)
	} else {
		return r.RollUpUserLogic.StakeOnNewNode(opts, assertion, expectedNodeHash, prevNodeInboxMaxCount)
	}
}

func (r *RollUpLogic) CurrentRequiredStake(opts *bind.CallOpts) (*big.Int, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.CurrentRequiredStake(opts)
	} else {
		return r.RollUpUserLogic.CurrentRequiredStake(opts)
	}
}

func (r *RollUpLogic) StakeToken(opts *bind.CallOpts) (common.Address, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.StakeToken(opts)
	} else {
		return r.RollUpUserLogic.StakeToken(opts)
	}
}

func (r *RollUpLogic) WithdrawableFunds(copts *bind.CallOpts, user common.Address) (*big.Int, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.WithdrawableFunds(copts, user)
	} else {
		return r.RollUpUserLogic.WithdrawableFunds(copts, user)
	}
}

func (r *RollUpLogic) NodeHasStaker(opts *bind.CallOpts, nodeNum uint64, staker common.Address) (bool, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.NodeHasStaker(opts, nodeNum, staker)
	} else {
		return r.RollUpUserLogic.NodeHasStaker(opts, nodeNum, staker)
	}
}

func (r *RollUpLogic) ReturnOldDeposit(opts *bind.TransactOpts, stakerAddress common.Address) (*types.Transaction, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.ReturnOldDeposit(opts, stakerAddress)
	} else {
		return r.RollUpUserLogic.ReturnOldDeposit(opts, stakerAddress)
	}
}

func (r *RollUpLogic) WithdrawStakerFunds(opts *bind.TransactOpts) (*types.Transaction, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.WithdrawStakerFunds(opts)
	} else {
		return r.RollUpUserLogic.WithdrawStakerFunds(opts)
	}
}

func (r *RollUpLogic) LatestConfirmed(opts *bind.CallOpts) (uint64, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.LatestConfirmed(opts)
	} else {
		return r.RollUpUserLogic.LatestConfirmed(opts)
	}
}

func (r *RollUpLogic) IsValidator(opts *bind.CallOpts, address common.Address) (bool, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.IsValidator(opts, address)
	} else {
		return r.RollUpUserLogic.IsValidator(opts, address)
	}
}

func (r *RollUpLogic) StakeOnExistingNode(opts *bind.TransactOpts, nodeNum uint64, nodeHash [32]byte) (*types.Transaction, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.StakeOnExistingNode(opts, nodeNum, nodeHash)
	} else {
		return r.RollUpUserLogic.StakeOnExistingNode(opts, nodeNum, nodeHash)
	}
}

func (r *RollUpLogic) WasmModuleRoot(opts *bind.CallOpts) ([32]byte, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.WasmModuleRoot(opts)
	} else {
		return r.RollUpUserLogic.WasmModuleRoot(opts)
	}
}

func (r *RollUpLogic) ValidatorWhitelistDisabled(opts *bind.CallOpts) (bool, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.ValidatorWhitelistDisabled(opts)
	} else {
		return r.RollUpUserLogic.ValidatorWhitelistDisabled(opts)
	}
}

func (r *RollUpLogic) FastConfirmNextNode(auth *bind.TransactOpts, blockHash [32]byte, sendRoot [32]byte, nodeHash [32]byte) (*types.Transaction, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.FastConfirmNextNode(auth, blockHash, sendRoot, nodeHash)
	} else {
		return r.RollUpUserLogic.FastConfirmNextNode(auth, blockHash, sendRoot, nodeHash)
	}
}

func (r *RollUpLogic) RejectNextNode(opts *bind.TransactOpts, stakerAddress common.Address) (*types.Transaction, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.RejectNextNode(opts, stakerAddress)
	} else {
		return r.RollUpUserLogic.RejectNextNode(opts, stakerAddress)
	}
}

func (r *RollUpLogic) ConfirmNextNode(opts *bind.TransactOpts, blockHash [32]byte, sendRoot [32]byte) (*types.Transaction, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.ConfirmNextNode(opts, blockHash, sendRoot)
	} else {
		return r.RollUpUserLogic.ConfirmNextNode(opts, blockHash, sendRoot)
	}
}

func (r *RollUpLogic) StakerMap(opts *bind.CallOpts, arg0 common.Address) (struct {
	AmountStaked     *big.Int
	Index            uint64
	LatestStakedNode uint64
	CurrentChallenge uint64
	IsStaked         bool
}, error) {
	if r.Erc20RollUpUserLogic != nil {
		return r.Erc20RollUpUserLogic.StakerMap(opts, arg0)
	} else {
		return r.RollUpUserLogic.StakerMap(opts, arg0)
	}
}

func (r *RollUpLogic) ParseRollupChallengeStarted(log types.Log) (uint64, error) {
	if r.Erc20RollUpUserLogic != nil {
		challenge, err := r.Erc20RollUpUserLogic.ParseRollupChallengeStarted(log)
		if err != nil {
			return 0, err
		}
		return challenge.ChallengedNode, nil
	} else {
		challenge, err := r.RollUpUserLogic.ParseRollupChallengeStarted(log)
		if err != nil {
			return 0, err
		}
		return challenge.ChallengedNode, nil
	}

}

func (r *RollUpLogic) ParseNodeCreated(ethLog types.Log) (uint64, rollup_legacy_gen.Assertion, *big.Int, [32]byte, [32]byte, [32]byte, [32]byte, error) {
	if r.Erc20RollUpUserLogic != nil {
		parsedLog, err := r.Erc20RollUpUserLogic.ParseNodeCreated(ethLog)
		if err != nil {
			return 0, rollup_legacy_gen.Assertion{}, nil, [32]byte{}, [32]byte{}, [32]byte{}, [32]byte{}, err
		}
		return parsedLog.NodeNum, parsedLog.Assertion, parsedLog.InboxMaxCount, parsedLog.AfterInboxBatchAcc, parsedLog.NodeHash, parsedLog.WasmModuleRoot, parsedLog.ExecutionHash, nil
	} else {
		parsedLog, err := r.RollUpUserLogic.ParseNodeCreated(ethLog)
		if err != nil {
			return 0, rollup_legacy_gen.Assertion{}, nil, [32]byte{}, [32]byte{}, [32]byte{}, [32]byte{}, err
		}
		return parsedLog.NodeNum, parsedLog.Assertion, parsedLog.InboxMaxCount, parsedLog.AfterInboxBatchAcc, parsedLog.NodeHash, parsedLog.WasmModuleRoot, parsedLog.ExecutionHash, nil
	}

}

type RollupWatcherL1Interface interface {
	bind.ContractBackend
	HeaderByNumber(ctx context.Context, number *big.Int) (*types.Header, error)
	FilterLogs(ctx context.Context, q ethereum.FilterQuery) ([]types.Log, error)
}

func NewRollupWatcher(address common.Address, client RollupWatcherL1Interface, callOpts bind.CallOpts) (*RollupWatcher, error) {
	var rollUpLogic RollUpLogic
	dummyRollUp, err := rollup_legacy_gen.NewRollupUserLogic(address, client)
	if err != nil {
		return nil, err
	}
	stakeToken, err := dummyRollUp.StakeToken(&callOpts)

	if stakeToken == (common.Address{}) {
		// eth
		con, err := rollup_legacy_gen.NewRollupUserLogic(address, client)
		if err != nil {
			return nil, err
		}
		rollUpLogic.RollUpUserLogic = con
	} else {
		con, err := rollup_legacy_gen.NewERC20RollupUserLogic(address, client)
		if err != nil {
			return nil, err
		}
		rollUpLogic.Erc20RollUpUserLogic = con
	}
	return &RollupWatcher{
		address:      address,
		client:       client,
		baseCallOpts: callOpts,
		RollUpLogic:  rollUpLogic,
	}, nil
}

func (r *RollupWatcher) getCallOpts(ctx context.Context) *bind.CallOpts {
	opts := r.baseCallOpts
	opts.Context = ctx
	return &opts
}

const noNodeErr string = "NO_NODE"

func looksLikeNoNodeError(err error) bool {
	if err == nil {
		return false
	}
	if strings.Contains(err.Error(), noNodeErr) {
		return true
	}
	var errWithData rpc.DataError
	ok := errors.As(err, &errWithData)
	if !ok {
		return false
	}
	dataString, ok := errWithData.ErrorData().(string)
	if !ok {
		return false
	}
	data := common.FromHex(dataString)
	return bytes.Contains(data, []byte(noNodeErr))
}

func (r *RollupWatcher) getNodeCreationBlock(ctx context.Context, nodeNum uint64) (*big.Int, error) {
	callOpts := r.getCallOpts(ctx)
	if !r.unSupportedL3Method.Load() {
		createdAtBlock, err := r.RollUpLogic.GetNodeCreationBlockForLogLookup(callOpts, nodeNum)
		if err == nil {
			r.supportedL3Method.Store(true)
			return createdAtBlock, nil
		}
		if headerreader.IsExecutionReverted(err) && !looksLikeNoNodeError(err) {
			if r.supportedL3Method.Load() {
				return nil, fmt.Errorf("getNodeCreationBlockForLogLookup failed despite previously succeeding: %w", err)
			}
			log.Info("getNodeCreationBlockForLogLookup does not seem to exist, falling back on node CreatedAtBlock field", "err", err)
			r.unSupportedL3Method.Store(true)
		} else {
			return nil, err
		}
	}

	node, err := r.RollUpLogic.GetNode(callOpts, nodeNum)
	if err != nil {
		return nil, err
	}
	createdAtBlock := new(big.Int).SetUint64(node.CreatedAtBlock)
	return createdAtBlock, nil
}

func (r *RollupWatcher) Initialize(ctx context.Context) error {
	var err error
	r.fromBlock, err = r.getNodeCreationBlock(ctx, 0)
	return err
}

func (r *RollupWatcher) Client() RollupWatcherL1Interface {
	return r.client
}

func (r *RollupWatcher) LookupCreation(ctx context.Context) (*rollup_legacy_gen.RollupUserLogicRollupInitialized, error) {
	var query = ethereum.FilterQuery{
		FromBlock: r.fromBlock,
		ToBlock:   r.fromBlock,
		Addresses: []common.Address{r.address},
		Topics:    [][]common.Hash{{rollupInitializedID}},
	}
	logs, err := r.client.FilterLogs(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(logs) == 0 {
		return nil, errors.New("rollup not created")
	}
	if len(logs) > 1 {
		return nil, errors.New("rollup created multiple times")
	}
	ev, err := r.RollUpLogic.RollUpUserLogic.ParseRollupInitialized(logs[0])
	return ev, err
}

func (r *RollupWatcher) Erc20LookupCreation(ctx context.Context) (*rollup_legacy_gen.ERC20RollupUserLogicRollupInitialized, error) {
	var query = ethereum.FilterQuery{
		FromBlock: r.fromBlock,
		ToBlock:   r.fromBlock,
		Addresses: []common.Address{r.address},
		Topics:    [][]common.Hash{{rollupInitializedID}},
	}
	logs, err := r.client.FilterLogs(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(logs) == 0 {
		return nil, errors.New("rollup not created")
	}
	if len(logs) > 1 {
		return nil, errors.New("rollup created multiple times")
	}
	ev, err := r.RollUpLogic.Erc20RollUpUserLogic.ParseRollupInitialized(logs[0])
	return ev, err
}

func (r *RollupWatcher) LookupNode(ctx context.Context, number uint64) (*NodeInfo, error) {
	createdAtBlock, err := r.getNodeCreationBlock(ctx, number)
	if err != nil {
		return nil, err
	}
	var numberAsHash common.Hash
	binary.BigEndian.PutUint64(numberAsHash[(32-8):], number)
	var query = ethereum.FilterQuery{
		FromBlock: createdAtBlock,
		ToBlock:   createdAtBlock,
		Addresses: []common.Address{r.address},
		Topics:    [][]common.Hash{{nodeCreatedID}, {numberAsHash}},
	}
	logs, err := r.client.FilterLogs(ctx, query)
	if err != nil {
		return nil, err
	}
	if len(logs) == 0 {
		return nil, fmt.Errorf("couldn't find requested node %v", number)
	}
	if len(logs) > 1 {
		return nil, fmt.Errorf("found multiple instances of requested node %v", number)
	}
	ethLog := logs[0]

	nodeNum, assertion, inboxMaxCount, afterInboxBatchAcc, nodeHash, wasmModuleRoot, _, err := r.RollUpLogic.ParseNodeCreated(ethLog)
	if err != nil {
		return nil, err
	}

	l1BlockProposed, err := arbutil.CorrespondingL1BlockNumber(ctx, r.client, ethLog.BlockNumber)
	if err != nil {
		return nil, err
	}
	return &NodeInfo{
		NodeNum:                  nodeNum,
		L1BlockProposed:          l1BlockProposed,
		ParentChainBlockProposed: ethLog.BlockNumber,
		Assertion:                NewAssertionFromLegacySolidity(assertion),
		InboxMaxCount:            inboxMaxCount,
		AfterInboxBatchAcc:       afterInboxBatchAcc,
		NodeHash:                 nodeHash,
		WasmModuleRoot:           wasmModuleRoot,
	}, nil

}

func (r *RollupWatcher) LookupNodeChildren(ctx context.Context, nodeNum uint64, logQueryRangeSize uint64, nodeHash common.Hash) ([]*NodeInfo, error) {
	node, err := r.RollUpLogic.GetNode(r.getCallOpts(ctx), nodeNum)
	if err != nil {
		return nil, err
	}
	if node.LatestChildNumber == 0 {
		return nil, nil
	}
	if node.NodeHash != nodeHash {
		return nil, fmt.Errorf("got unexpected node hash %v looking for node number %v with expected hash %v (reorg?)", node.NodeHash, nodeNum, nodeHash)
	}
	var query = ethereum.FilterQuery{
		Addresses: []common.Address{r.address},
		Topics:    [][]common.Hash{{nodeCreatedID}, nil, {nodeHash}},
	}
	fromBlock, err := r.getNodeCreationBlock(ctx, nodeNum)
	if err != nil {
		return nil, err
	}
	toBlock, err := r.getNodeCreationBlock(ctx, node.LatestChildNumber)
	if err != nil {
		return nil, err
	}
	var logs []types.Log
	// break down the query to avoid eth_getLogs query limit
	for toBlock.Cmp(fromBlock) > 0 {
		query.FromBlock = fromBlock
		if logQueryRangeSize == 0 {
			query.ToBlock = toBlock
		} else {
			query.ToBlock = new(big.Int).Add(fromBlock, new(big.Int).SetUint64(logQueryRangeSize))
		}
		if query.ToBlock.Cmp(toBlock) > 0 {
			query.ToBlock = toBlock
		}
		segment, err := r.client.FilterLogs(ctx, query)
		if err != nil {
			return nil, err
		}
		logs = append(logs, segment...)
		fromBlock = new(big.Int).Add(query.ToBlock, big.NewInt(1))
	}
	infos := make([]*NodeInfo, 0, len(logs))
	lastHash := nodeHash
	for i, ethLog := range logs {

		nodeNum, assertion, inboxMaxCount, afterInboxBatchAcc, _, wasmModuleRoot, executionHash, err := r.RollUpLogic.ParseNodeCreated(ethLog)
		if err != nil {
			return nil, err
		}
		lastHashIsSibling := [1]byte{0}
		if i > 0 {
			lastHashIsSibling[0] = 1
		}
		lastHash = crypto.Keccak256Hash(lastHashIsSibling[:], lastHash[:], executionHash[:], afterInboxBatchAcc[:], wasmModuleRoot[:])
		l1BlockProposed, err := arbutil.CorrespondingL1BlockNumber(ctx, r.client, ethLog.BlockNumber)
		if err != nil {
			return nil, err
		}
		infos = append(infos, &NodeInfo{
			NodeNum:                  nodeNum,
			L1BlockProposed:          l1BlockProposed,
			ParentChainBlockProposed: ethLog.BlockNumber,
			Assertion:                NewAssertionFromLegacySolidity(assertion),
			InboxMaxCount:            inboxMaxCount,
			AfterInboxBatchAcc:       afterInboxBatchAcc,
			NodeHash:                 lastHash,
			WasmModuleRoot:           wasmModuleRoot,
		})

	}
	return infos, nil
}

func (r *RollupWatcher) LatestConfirmedCreationBlock(ctx context.Context) (uint64, error) {
	latestConfirmed, err := r.RollUpLogic.LatestConfirmed(r.getCallOpts(ctx))
	if err != nil {
		return 0, err
	}
	creation, err := r.getNodeCreationBlock(ctx, latestConfirmed)
	if err != nil {
		return 0, err
	}
	if !creation.IsUint64() {
		return 0, fmt.Errorf("node %v creation block %v is not a uint64", latestConfirmed, creation)
	}
	return creation.Uint64(), nil
}

func (r *RollupWatcher) LookupChallengedNode(ctx context.Context, address common.Address) (uint64, error) {
	// TODO: This function is currently unused

	// Assuming this function is only used to find information about an active challenge, it
	// must be a challenge over an unconfirmed node and thus must have been created after the
	// latest confirmed node was created
	latestConfirmedCreated, err := r.LatestConfirmedCreationBlock(ctx)
	if err != nil {
		return 0, err
	}

	addressQuery := common.Hash{}
	copy(addressQuery[12:], address.Bytes())

	query := ethereum.FilterQuery{
		FromBlock: new(big.Int).SetUint64(latestConfirmedCreated),
		ToBlock:   nil,
		Addresses: []common.Address{r.address},
		Topics:    [][]common.Hash{{challengeCreatedID}, {addressQuery}},
	}
	logs, err := r.client.FilterLogs(ctx, query)
	if err != nil {
		return 0, err
	}

	if len(logs) == 0 {
		return 0, errors.New("no matching challenge")
	}

	if len(logs) > 1 {
		return 0, errors.New("too many matching challenges")
	}

	challengedNode, err := r.RollUpLogic.ParseRollupChallengeStarted(logs[0])
	if err != nil {
		return 0, err
	}
	return challengedNode, err
}

func (r *RollupWatcher) StakerInfo(ctx context.Context, staker common.Address) (*StakerInfo, error) {

	info, err := r.RollUpLogic.StakerMap(r.getCallOpts(ctx), staker)
	if err != nil {
		return nil, err
	}
	if !info.IsStaked {
		return nil, nil
	}
	stakerInfo := &StakerInfo{
		Index:            info.Index,
		LatestStakedNode: info.LatestStakedNode,
		AmountStaked:     info.AmountStaked,
	}
	if info.CurrentChallenge != 0 {
		chal := info.CurrentChallenge
		stakerInfo.CurrentChallenge = &chal
	}
	return stakerInfo, nil
}
