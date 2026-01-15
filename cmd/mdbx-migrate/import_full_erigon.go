//go:build erigon
// +build erigon

package main

import (
	"context"
	"encoding/binary"
	"fmt"

	ecommon "github.com/erigontech/erigon-lib/common"
	"github.com/erigontech/erigon/db/kv"
	erawdb "github.com/erigontech/erigon/db/rawdb"
	"github.com/erigontech/erigon/execution/rlp"
	"github.com/erigontech/erigon/execution/stagedsync/stages"
	etypes "github.com/erigontech/erigon/execution/types"
	gcommon "github.com/ethereum/go-ethereum/common"
	gethrawdb "github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/ethdb"
)

const importBatchBlocks = 1000

type importStats struct {
	Blocks uint64
}

func resolveImportHead(src ethdb.Database, opts Options) (uint64, error) {
	headHash := gethrawdb.ReadHeadBlockHash(src)
	if headHash == (gcommon.Hash{}) {
		return 0, fmt.Errorf("source head hash missing")
	}
	headNum := gethrawdb.ReadHeaderNumber(src, headHash)
	if headNum == nil {
		return 0, fmt.Errorf("source head number missing")
	}
	if opts.EndBlock != 0 {
		if opts.EndBlock < opts.StartBlock {
			return 0, fmt.Errorf("invalid block range %d..%d", opts.StartBlock, opts.EndBlock)
		}
		if opts.EndBlock > *headNum {
			return 0, fmt.Errorf("end block %d exceeds head %d", opts.EndBlock, *headNum)
		}
		return opts.EndBlock, nil
	}
	if *headNum < opts.StartBlock {
		return 0, fmt.Errorf("start block %d exceeds head %d", opts.StartBlock, *headNum)
	}
	return *headNum, nil
}

func importHeadersAndBodies(ctx context.Context, src ethdb.Database, dst kv.RwDB, start, end uint64) (importStats, ecommon.Hash, error) {
	stats := importStats{}
	var lastHash ecommon.Hash
	if end < start {
		return stats, lastHash, fmt.Errorf("invalid import range %d..%d", start, end)
	}
	logKV("import", "from", start, "to", end, "status", "start")
	for cur := start; cur <= end; {
		tx, err := dst.BeginRw(ctx)
		if err != nil {
			return stats, lastHash, err
		}
		for i := 0; i < importBatchBlocks && cur <= end; i++ {
			if err := ctx.Err(); err != nil {
				tx.Rollback()
				return stats, lastHash, err
			}
			hash := gethrawdb.ReadCanonicalHash(src, cur)
			if hash == (gcommon.Hash{}) {
				tx.Rollback()
				return stats, lastHash, fmt.Errorf("missing canonical hash at block %d", cur)
			}
			headerRLP := gethrawdb.ReadHeaderRLP(src, hash, cur)
			if len(headerRLP) == 0 {
				tx.Rollback()
				return stats, lastHash, fmt.Errorf("missing header at block %d", cur)
			}
			var header etypes.Header
			if err := rlp.DecodeBytes(headerRLP, &header); err != nil {
				tx.Rollback()
				return stats, lastHash, fmt.Errorf("decode header %d: %w", cur, err)
			}
			if header.Number == nil || header.Number.Uint64() != cur {
				tx.Rollback()
				return stats, lastHash, fmt.Errorf("header number mismatch at block %d", cur)
			}
			bodyRLP := gethrawdb.ReadBodyRLP(src, hash, cur)
			if len(bodyRLP) == 0 {
				tx.Rollback()
				return stats, lastHash, fmt.Errorf("missing body at block %d", cur)
			}
			var body etypes.Body
			if err := rlp.DecodeBytes(bodyRLP, &body); err != nil {
				tx.Rollback()
				return stats, lastHash, fmt.Errorf("decode body %d: %w", cur, err)
			}
			if err := erawdb.WriteHeader(tx, &header); err != nil {
				tx.Rollback()
				return stats, lastHash, fmt.Errorf("write header %d: %w", cur, err)
			}
			headerHash := header.Hash()
			expectedHash := ecommon.BytesToHash(hash.Bytes())
			if headerHash != expectedHash {
				tx.Rollback()
				return stats, lastHash, fmt.Errorf("header hash mismatch at block %d", cur)
			}
			if err := erawdb.WriteCanonicalHash(tx, headerHash, cur); err != nil {
				tx.Rollback()
				return stats, lastHash, fmt.Errorf("write canonical hash %d: %w", cur, err)
			}
			if err := erawdb.WriteBody(tx, headerHash, cur, &body); err != nil {
				tx.Rollback()
				return stats, lastHash, fmt.Errorf("write body %d: %w", cur, err)
			}
			td := gethrawdb.ReadTd(src, hash, cur)
			if td != nil {
				if err := erawdb.WriteTd(tx, headerHash, cur, td); err != nil {
					tx.Rollback()
					return stats, lastHash, fmt.Errorf("write td %d: %w", cur, err)
				}
			}
			stats.Blocks++
			lastHash = headerHash
			cur++
		}
		if err := tx.Commit(); err != nil {
			tx.Rollback()
			return stats, lastHash, err
		}
		tx.Rollback()
		logKV("import", "status", "progress", "blocks", stats.Blocks, "last", cur-1)
	}
	return stats, lastHash, nil
}

func finalizeImportProgress(ctx context.Context, dst kv.RwDB, head uint64, headHash ecommon.Hash) error {
	return dst.Update(ctx, func(tx kv.RwTx) error {
		if err := erawdb.WriteHeadHeaderHash(tx, headHash); err != nil {
			return err
		}
		erawdb.WriteHeadBlockHash(tx, headHash)
		if err := stages.SaveStageProgress(tx, stages.Headers, head); err != nil {
			return err
		}
		if err := stages.SaveStageProgress(tx, stages.BlockHashes, head); err != nil {
			return err
		}
		if err := stages.SaveStageProgress(tx, stages.Bodies, head); err != nil {
			return err
		}
		return nil
	})
}

func appendCanonicalTxNums(ctx context.Context, dst kv.RwDB, start uint64) error {
	var from uint64
	err := dst.Update(ctx, func(tx kv.RwTx) error {
		from = start
		c, err := tx.Cursor(kv.MaxTxNum)
		if err != nil {
			return err
		}
		defer c.Close()
		lastKey, _, err := c.Last()
		if err != nil {
			return err
		}
		if lastKey != nil {
			lastBlock := binary.BigEndian.Uint64(lastKey)
			if lastBlock+1 > from {
				from = lastBlock + 1
			}
		}
		return erawdb.AppendCanonicalTxNums(tx, from)
	})
	if err != nil {
		return err
	}
	logKV("txnums", "status", "done", "from", from)
	return nil
}
