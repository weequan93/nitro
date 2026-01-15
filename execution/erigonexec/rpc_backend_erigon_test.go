//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"math/big"
	"testing"

	"github.com/holiman/uint256"
	"github.com/stretchr/testify/require"

	ecommon "github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon-lib/common/empty"
	elog "github.com/erigontech/erigon-lib/log/v3"
	"github.com/erigontech/erigon-lib/gointerfaces"
	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/temporal/temporaltest"
	"github.com/erigontech/erigon/db/rawdb"
	dbstate "github.com/erigontech/erigon/db/state"
	"github.com/erigontech/erigon/db/state/statecfg"
	"github.com/erigontech/erigon/execution/rlp"
	etypes "github.com/erigontech/erigon/execution/types"
)

type testChain struct {
	client   *Client
	db       kv.TemporalRwDB
	block    *etypes.Block
	logEntry *etypes.Log
	tx       etypes.Transaction
}

func setupTestChain(t *testing.T) *testChain {
	t.Helper()
	statecfg.EnableHistoricalRCache()
	chainDir := t.TempDir()
	dirs, err := buildExecDirs(chainDir)
	require.NoError(t, err)

	db := temporaltest.NewTestDB(t, dirs)
	logger := elog.New("component", "test")
	blockReader, err := newBlockReader(chainDir, "test", logger)
	require.NoError(t, err)

	client := &Client{
		chainDB:     db,
		blockReader: blockReader,
		logger:      logger,
	}

	genesisHeader := &etypes.Header{
		ParentHash: empty.RootHash,
		UncleHash:  empty.UncleHash,
		Root:       empty.RootHash,
		Number:     big.NewInt(0),
		GasLimit:   1_000_000,
		Time:       1,
		Difficulty: big.NewInt(1),
		Extra:      []byte{},
	}
	genesisBlock := etypes.NewBlock(genesisHeader, nil, nil, nil, nil)

	tx := etypes.NewTransaction(
		0,
		ecommon.Address{},
		uint256.NewInt(1),
		21_000,
		uint256.NewInt(1),
		nil,
	)
	logEntry := &etypes.Log{
		Address: ecommon.HexToAddress("0x1111111111111111111111111111111111111111"),
		Topics:  []ecommon.Hash{ecommon.HexToHash("0x02")},
		Data:    []byte{0x01, 0x02},
		TxIndex: 0,
		Index:   0,
	}
	receipt := &etypes.Receipt{
		Status:                   etypes.ReceiptStatusSuccessful,
		CumulativeGasUsed:        21_000,
		Logs:                     etypes.Logs{logEntry},
		BlockNumber:              big.NewInt(1),
		TransactionIndex:         0,
		FirstLogIndexWithinBlock: 0,
	}
	blockHeader := &etypes.Header{
		ParentHash: genesisBlock.Hash(),
		UncleHash:  empty.UncleHash,
		Root:       empty.RootHash,
		Number:     big.NewInt(1),
		GasLimit:   1_000_000,
		Time:       2,
		Difficulty: big.NewInt(1),
		Extra:      []byte{},
	}
	block := etypes.NewBlock(blockHeader, []etypes.Transaction{tx}, nil, []*etypes.Receipt{receipt}, nil)
	receipt.BlockHash = block.Hash()
	receipt.TxHash = tx.Hash()

	txrw, err := db.BeginTemporalRw(context.Background())
	require.NoError(t, err)
	defer txrw.Rollback()

	domains, err := dbstate.NewSharedDomains(txrw, logger)
	require.NoError(t, err)
	defer domains.Close()

	require.NoError(t, rawdb.WriteBlock(txrw, genesisBlock))
	require.NoError(t, rawdb.WriteCanonicalHash(txrw, genesisBlock.Hash(), genesisBlock.NumberU64()))

	require.NoError(t, rawdb.WriteBlock(txrw, block))
	require.NoError(t, rawdb.WriteCanonicalHash(txrw, block.Hash(), block.NumberU64()))
	require.NoError(t, rawdb.WriteHeadHeaderHash(txrw, block.Hash()))
	rawdb.WriteHeadBlockHash(txrw, block.Hash())

	require.NoError(t, rawdb.AppendCanonicalTxNums(txrw, 0))

	txNumReader := blockReader.TxnumReader(context.Background())
	baseTxNum, err := txNumReader.Min(txrw, block.NumberU64())
	require.NoError(t, err)

	require.NoError(t, rawdb.WriteReceiptCacheV2(domains.AsPutDel(txrw), nil, baseTxNum))
	for i := range block.Transactions() {
		require.NoError(t, rawdb.WriteReceiptCacheV2(domains.AsPutDel(txrw), receipt, baseTxNum+1+uint64(i)))
	}
	require.NoError(t, rawdb.WriteReceiptCacheV2(domains.AsPutDel(txrw), nil, baseTxNum+uint64(len(block.Transactions()))+1))
	require.NoError(t, domains.Flush(context.Background(), txrw))

	require.NoError(t, txrw.Commit())

	return &testChain{
		client:   client,
		db:       db,
		block:    block,
		logEntry: logEntry,
		tx:       tx,
	}
}

func TestHeaderRlp(t *testing.T) {
	chain := setupTestChain(t)
	backend := newRPCBackend(chain.client)

	got, err := backend.headerRlp(context.Background(), chain.block.NumberU64())
	require.NoError(t, err)
	want, err := rlp.EncodeToBytes(chain.block.Header())
	require.NoError(t, err)
	require.Equal(t, want, got)
}

func TestCollectBlockLogs(t *testing.T) {
	chain := setupTestChain(t)
	backend := newRPCBackend(chain.client)

	ctx := context.Background()
	logs, err := backend.collectBlockLogs(ctx, chain.db, chain.client.blockReader.TxnumReader(ctx), chain.block.NumberU64())
	require.NoError(t, err)
	require.Len(t, logs, 1)

	reply := logs[0]
	require.Equal(t, gointerfaces.ConvertAddressToH160(chain.logEntry.Address), reply.Address)
	require.Equal(t, chain.block.NumberU64(), reply.BlockNumber)
	require.Equal(t, gointerfaces.ConvertHashToH256(chain.block.Hash()), reply.BlockHash)
	require.Equal(t, gointerfaces.ConvertHashToH256(chain.tx.Hash()), reply.TransactionHash)
	require.Equal(t, uint64(chain.logEntry.TxIndex), reply.TransactionIndex)
	require.Equal(t, uint64(chain.logEntry.Index), reply.LogIndex)
	require.Equal(t, chain.logEntry.Data, reply.Data)
	require.False(t, reply.Removed)
	require.Len(t, reply.Topics, len(chain.logEntry.Topics))
	for i, topic := range chain.logEntry.Topics {
		require.Equal(t, gointerfaces.ConvertHashToH256(topic), reply.Topics[i])
	}
}
