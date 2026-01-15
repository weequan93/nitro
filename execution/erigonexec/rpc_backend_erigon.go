//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"errors"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	ecommon "github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/gointerfaces"
	"github.com/erigontech/erigon-lib/gointerfaces/remoteproto"
	"github.com/erigontech/erigon-lib/gointerfaces/typesproto"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/rawdb"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	"github.com/erigontech/erigon/execution/rlp"
	etypes "github.com/erigontech/erigon/execution/types"
	"github.com/erigontech/erigon/p2p"
	"github.com/erigontech/erigon/rpc/rpchelper"
)

var errRPCBackendUnsupported = errors.New("erigonexec: rpc backend not supported")

type rpcBackend struct {
	client *Client
}

func newRPCBackend(client *Client) *rpcBackend {
	return &rpcBackend{client: client}
}

func (b *rpcBackend) Syncing(ctx context.Context) (*remoteproto.SyncingReply, error) {
	if b.client == nil {
		return nil, errRPCBackendUnsupported
	}
	header, err := b.client.readCurrentHeader()
	if err != nil {
		return nil, err
	}
	reply := &remoteproto.SyncingReply{
		CurrentBlock:     header.Number.Uint64(),
		LastNewBlockSeen: header.Number.Uint64(),
		Syncing:          !b.client.Synced(),
	}
	if !reply.Syncing {
		return reply, nil
	}

	progress := b.client.FullSyncProgressMap()
	stages := make([]*remoteproto.SyncingReply_StageProgress, 0, len(progress))
	for name, value := range progress {
		v, ok := value.(uint64)
		if !ok {
			continue
		}
		stages = append(stages, &remoteproto.SyncingReply_StageProgress{
			StageName:   name,
			BlockNumber: v,
		})
	}
	sort.Slice(stages, func(i, j int) bool { return stages[i].StageName < stages[j].StageName })
	reply.Stages = stages
	return reply, nil
}

func (b *rpcBackend) Etherbase(ctx context.Context) (ecommon.Address, error) {
	_ = ctx
	return ecommon.Address{}, nil
}

func (b *rpcBackend) NetVersion(ctx context.Context) (uint64, error) {
	_ = ctx
	if b.client == nil || b.client.chainConfig == nil || b.client.chainConfig.ChainID == nil {
		return 0, errRPCBackendUnsupported
	}
	return b.client.chainConfig.ChainID.Uint64(), nil
}

func (b *rpcBackend) NetPeerCount(ctx context.Context) (uint64, error) {
	_ = ctx
	return 0, nil
}

func (b *rpcBackend) ProtocolVersion(ctx context.Context) (uint64, error) {
	_ = ctx
	return 0, nil
}

func (b *rpcBackend) ClientVersion(ctx context.Context) (string, error) {
	_ = ctx
	return "nitro-erigon", nil
}

func (b *rpcBackend) Subscribe(ctx context.Context, cb func(*remoteproto.SubscribeReply)) error {
	if b.client == nil || b.client.chainDB == nil || b.client.blockReader == nil {
		return errRPCBackendUnsupported
	}
	header, err := b.client.readCurrentHeader()
	if err != nil {
		return err
	}
	cb(&remoteproto.SubscribeReply{Type: remoteproto.Event_NEW_SNAPSHOT})
	lastHead := header.Number.Uint64()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		header, err := b.client.readCurrentHeader()
		if err != nil {
			return err
		}
		headNum := header.Number.Uint64()
		if headNum <= lastHead {
			continue
		}
		for blockNum := lastHead + 1; blockNum <= headNum; blockNum++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			data, err := b.headerRlp(ctx, blockNum)
			if err != nil {
				return err
			}
			if len(data) == 0 {
				continue
			}
			cb(&remoteproto.SubscribeReply{Type: remoteproto.Event_HEADER, Data: data})
		}
		lastHead = headNum
	}
}

func (b *rpcBackend) SubscribeLogs(ctx context.Context, cb func(*remoteproto.SubscribeLogsReply), requestor *atomic.Value) error {
	if b.client == nil || b.client.chainDB == nil || b.client.blockReader == nil {
		return errRPCBackendUnsupported
	}
	temporalDB, ok := b.client.chainDB.(kv.TemporalRoDB)
	if !ok {
		return errRPCBackendUnsupported
	}
	filter := &logsFilter{}
	var filterMu sync.RWMutex
	updateFilter := func(req *remoteproto.LogsFilterRequest) error {
		filterMu.Lock()
		defer filterMu.Unlock()
		filter.apply(req)
		return nil
	}
	if requestor != nil {
		requestor.Store(updateFilter)
	}

	header, err := b.client.readCurrentHeader()
	if err != nil {
		return err
	}
	lastHead := header.Number.Uint64()
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	txNumReader := b.client.blockReader.TxnumReader(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		header, err := b.client.readCurrentHeader()
		if err != nil {
			return err
		}
		headNum := header.Number.Uint64()
		if headNum < lastHead {
			lastHead = headNum
			continue
		}
		if headNum == lastHead {
			continue
		}
		for blockNum := lastHead + 1; blockNum <= headNum; blockNum++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			logs, err := b.collectBlockLogs(ctx, temporalDB, txNumReader, blockNum)
			if err != nil {
				return err
			}
			if len(logs) == 0 {
				continue
			}
			filterMu.RLock()
			current := filter.clone()
			filterMu.RUnlock()
			for _, log := range logs {
				if current.matches(log) {
					cb(log)
				}
			}
		}
		lastHead = headNum
	}
}

func (b *rpcBackend) BlockWithSenders(ctx context.Context, tx kv.Getter, hash ecommon.Hash, blockHeight uint64) (*etypes.Block, []ecommon.Address, error) {
	if b.client == nil || b.client.blockReader == nil {
		return nil, nil, errRPCBackendUnsupported
	}
	return b.client.blockReader.BlockWithSenders(ctx, tx, hash, blockHeight)
}

func (b *rpcBackend) NodeInfo(ctx context.Context, limit uint32) ([]p2p.NodeInfo, error) {
	_ = ctx
	_ = limit
	return []p2p.NodeInfo{}, nil
}

func (b *rpcBackend) Peers(ctx context.Context) ([]*p2p.PeerInfo, error) {
	_ = ctx
	return []*p2p.PeerInfo{}, nil
}

func (b *rpcBackend) AddPeer(ctx context.Context, url *remoteproto.AddPeerRequest) (*remoteproto.AddPeerReply, error) {
	_ = ctx
	_ = url
	return &remoteproto.AddPeerReply{Success: false}, errRPCBackendUnsupported
}

func (b *rpcBackend) RemovePeer(ctx context.Context, url *remoteproto.RemovePeerRequest) (*remoteproto.RemovePeerReply, error) {
	_ = ctx
	_ = url
	return &remoteproto.RemovePeerReply{Success: false}, errRPCBackendUnsupported
}

func (b *rpcBackend) PendingBlock(ctx context.Context) (*etypes.Block, error) {
	if b.client == nil || b.client.chainDB == nil || b.client.blockReader == nil {
		return nil, errRPCBackendUnsupported
	}
	var block *etypes.Block
	err := b.client.chainDB.View(ctx, func(tx kv.Tx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		var err error
		block, err = b.client.blockReader.CurrentBlock(tx)
		return err
	})
	if err != nil {
		return nil, err
	}
	return block, nil
}

var _ rpchelper.ApiBackend = (*rpcBackend)(nil)

func (b *rpcBackend) headerRlp(ctx context.Context, blockNum uint64) ([]byte, error) {
	if b.client == nil || b.client.chainDB == nil || b.client.blockReader == nil {
		return nil, errRPCBackendUnsupported
	}
	var header *etypes.Header
	err := b.client.chainDB.View(ctx, func(tx kv.Tx) error {
		var err error
		header, err = b.client.blockReader.HeaderByNumber(ctx, tx, blockNum)
		return err
	})
	if err != nil {
		return nil, err
	}
	if header == nil {
		return nil, nil
	}
	return rlp.EncodeToBytes(header)
}

func (b *rpcBackend) collectBlockLogs(ctx context.Context, db kv.TemporalRoDB, txNumReader rawdbv3.TxNumsReader, blockNum uint64) ([]*remoteproto.SubscribeLogsReply, error) {
	tx, err := db.BeginTemporalRo(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	block, err := b.client.blockReader.BlockByNumber(ctx, tx, blockNum)
	if err != nil {
		return nil, err
	}
	if block == nil {
		return nil, nil
	}
	receipts, err := rawdb.ReadReceiptsCacheV2(tx, block, txNumReader)
	if err != nil {
		return nil, err
	}
	if len(receipts) == 0 {
		return nil, nil
	}
	replies := make([]*remoteproto.SubscribeLogsReply, 0, len(receipts))
	for _, receipt := range receipts {
		if receipt == nil {
			continue
		}
		blockNumber := blockNum
		if receipt.BlockNumber != nil {
			blockNumber = receipt.BlockNumber.Uint64()
		}
		for _, logEntry := range receipt.Logs {
			reply := &remoteproto.SubscribeLogsReply{
				Address:          gointerfaces.ConvertAddressToH160(logEntry.Address),
				BlockHash:        gointerfaces.ConvertHashToH256(receipt.BlockHash),
				BlockNumber:      blockNumber,
				Data:             logEntry.Data,
				LogIndex:         uint64(logEntry.Index),
				Topics:           make([]*typesproto.H256, 0, len(logEntry.Topics)),
				TransactionHash:  gointerfaces.ConvertHashToH256(receipt.TxHash),
				TransactionIndex: uint64(logEntry.TxIndex),
				Removed:          false,
			}
			for _, topic := range logEntry.Topics {
				reply.Topics = append(reply.Topics, gointerfaces.ConvertHashToH256(topic))
			}
			replies = append(replies, reply)
		}
	}
	return replies, nil
}

type logsFilter struct {
	ready       bool
	allAddrs    bool
	allTopics   bool
	addresses   map[ecommon.Address]struct{}
	topics      map[ecommon.Hash]struct{}
}

func (f *logsFilter) apply(req *remoteproto.LogsFilterRequest) {
	f.ready = true
	if req == nil {
		f.allAddrs = false
		f.allTopics = false
		f.addresses = nil
		f.topics = nil
		return
	}
	f.allAddrs = req.AllAddresses
	f.allTopics = req.AllTopics
	if len(req.Addresses) > 0 {
		addrs := make(map[ecommon.Address]struct{}, len(req.Addresses))
		for _, addr := range req.Addresses {
			addrs[gointerfaces.ConvertH160toAddress(addr)] = struct{}{}
		}
		f.addresses = addrs
	} else {
		f.addresses = nil
	}
	if len(req.Topics) > 0 {
		topics := make(map[ecommon.Hash]struct{}, len(req.Topics))
		for _, topic := range req.Topics {
			topics[gointerfaces.ConvertH256ToHash(topic)] = struct{}{}
		}
		f.topics = topics
	} else {
		f.topics = nil
	}
}

func (f *logsFilter) clone() logsFilter {
	clone := logsFilter{
		ready:     f.ready,
		allAddrs:  f.allAddrs,
		allTopics: f.allTopics,
	}
	if len(f.addresses) > 0 {
		clone.addresses = make(map[ecommon.Address]struct{}, len(f.addresses))
		for addr := range f.addresses {
			clone.addresses[addr] = struct{}{}
		}
	}
	if len(f.topics) > 0 {
		clone.topics = make(map[ecommon.Hash]struct{}, len(f.topics))
		for topic := range f.topics {
			clone.topics[topic] = struct{}{}
		}
	}
	return clone
}

func (f logsFilter) matches(log *remoteproto.SubscribeLogsReply) bool {
	if !f.ready {
		return true
	}
	if !f.allAddrs {
		if len(f.addresses) == 0 {
			return false
		}
		addr := gointerfaces.ConvertH160toAddress(log.Address)
		if _, ok := f.addresses[addr]; !ok {
			return false
		}
	}
	if !f.allTopics {
		if len(f.topics) == 0 {
			return false
		}
		for _, topic := range log.Topics {
			if _, ok := f.topics[gointerfaces.ConvertH256ToHash(topic)]; ok {
				return true
			}
		}
		return false
	}
	return true
}
