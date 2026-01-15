//go:build !erigon
// +build !erigon

package erigonexec

import (
	"context"
	"errors"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/params"

	"github.com/offchainlabs/nitro/arbos/arbostypes"
	"github.com/offchainlabs/nitro/arbutil"
	"github.com/offchainlabs/nitro/execution"
	"github.com/offchainlabs/nitro/execution/gethexec"
)

var errNotImplemented = errors.New("erigon execution backend not implemented")

type Client struct{}

func New(cfg Config) (execution.FullExecutionClient, error) {
	_ = cfg
	return &Client{}, errNotImplemented
}

func (c *Client) Start(ctx context.Context) error {
	_ = ctx
	return errNotImplemented
}

func (c *Client) StopAndWait() {}

func (c *Client) Maintenance() error {
	return errNotImplemented
}

func (c *Client) ArbDB() ethdb.Database {
	return nil
}

func (c *Client) ArbOSVersionForMessageNumber(messageNum arbutil.MessageIndex) (uint64, error) {
	_ = messageNum
	return 0, errNotImplemented
}

func (c *Client) DigestMessage(num arbutil.MessageIndex, msg *arbostypes.MessageWithMetadata, msgForPrefetch *arbostypes.MessageWithMetadata) (*execution.MessageResult, error) {
	_ = num
	_ = msg
	_ = msgForPrefetch
	return nil, errNotImplemented
}

func (c *Client) Reorg(count arbutil.MessageIndex, newMessages []arbostypes.MessageWithMetadataAndBlockInfo, oldMessages []*arbostypes.MessageWithMetadata) ([]*execution.MessageResult, error) {
	_ = count
	_ = newMessages
	_ = oldMessages
	return nil, errNotImplemented
}

func (c *Client) HeadMessageNumber() (arbutil.MessageIndex, error) {
	return 0, errNotImplemented
}

func (c *Client) HeadMessageNumberSync(t *testing.T) (arbutil.MessageIndex, error) {
	_ = t
	return 0, errNotImplemented
}

func (c *Client) ResultAtPos(pos arbutil.MessageIndex) (*execution.MessageResult, error) {
	_ = pos
	return nil, errNotImplemented
}

func (c *Client) MessageIndexToBlockNumber(messageNum arbutil.MessageIndex) uint64 {
	_ = messageNum
	return 0
}

func (c *Client) BlockNumberToMessageIndex(blockNum uint64) (arbutil.MessageIndex, error) {
	_ = blockNum
	return 0, errNotImplemented
}

func (c *Client) RecordBlockCreation(ctx context.Context, pos arbutil.MessageIndex, msg *arbostypes.MessageWithMetadata) (*execution.RecordResult, error) {
	_ = ctx
	_ = pos
	_ = msg
	return nil, errNotImplemented
}

func (c *Client) MarkValid(pos arbutil.MessageIndex, resultHash common.Hash) {
	_ = pos
	_ = resultHash
}

func (c *Client) PrepareForRecord(ctx context.Context, start, end arbutil.MessageIndex) error {
	_ = ctx
	_ = start
	_ = end
	return errNotImplemented
}

func (c *Client) Pause() {}

func (c *Client) Activate() {}

func (c *Client) ForwardTo(url string) error {
	_ = url
	return errNotImplemented
}

func (c *Client) SequenceDelayedMessage(message *arbostypes.L1IncomingMessage, delayedSeqNum uint64) error {
	_ = message
	_ = delayedSeqNum
	return errNotImplemented
}

func (c *Client) NextDelayedMessageNumber() (uint64, error) {
	return 0, errNotImplemented
}

func (c *Client) MarkFeedStart(to arbutil.MessageIndex) {
	_ = to
}

func (c *Client) Synced() bool {
	return false
}

func (c *Client) FullSyncProgressMap() map[string]interface{} {
	return map[string]interface{}{}
}

func (c *Client) ValidateChainConfig(ctx context.Context, chainConfig *params.ChainConfig) error {
	_ = ctx
	_ = chainConfig
	return errNotImplemented
}

func (c *Client) ArbOSChainConfig(ctx context.Context) (*params.ChainConfig, error) {
	_ = ctx
	return nil, errNotImplemented
}

func (c *Client) SetTxPublisher(publisher gethexec.TransactionPublisher) {
	_ = publisher
}
