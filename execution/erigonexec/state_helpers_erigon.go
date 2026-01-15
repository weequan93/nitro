//go:build erigon
// +build erigon

package erigonexec

import (
	"context"
	"errors"

	"github.com/erigontech/erigon/db/kv"
	"github.com/erigontech/erigon/db/kv/rawdbv3"
	erawdb "github.com/erigontech/erigon/db/rawdb"
	estate "github.com/erigontech/erigon/core/state"
	etypes "github.com/erigontech/erigon/execution/types"
)

func (c *Client) latestState(ctx context.Context) (*etypes.Header, estate.IntraBlockStateArbitrum, func(), error) {
	if c.chainDB == nil {
		return nil, nil, func() {}, errors.New("erigonexec: chain db not initialized")
	}
	temporalDB, ok := c.chainDB.(kv.TemporalRoDB)
	if !ok {
		return nil, nil, func() {}, errors.New("erigonexec: chain db missing temporal support")
	}
	tx, err := temporalDB.BeginTemporalRo(ctx)
	if err != nil {
		return nil, nil, func() {}, err
	}
	release := func() {
		tx.Rollback()
	}
	header := erawdb.ReadCurrentHeader(tx)
	if header == nil {
		release()
		return nil, nil, func() {}, errors.New("erigonexec: current header not found")
	}
	maxTxNum, err := rawdbv3.TxNums.Max(tx, header.Number.Uint64())
	if err != nil {
		release()
		return nil, nil, func() {}, c.wrapHistoryError(err)
	}
	stateReader := estate.NewHistoryReaderV3()
	stateReader.SetTx(tx)
	stateReader.SetTxNum(maxTxNum)
	ibs := estate.New(stateReader)
	ibsArb := estate.NewArbitrum(ibs)
	return header, ibsArb, release, nil
}

func (c *Client) stateAtBlockNumber(ctx context.Context, blockNum uint64) (*etypes.Header, estate.IntraBlockStateArbitrum, func(), error) {
	if c.chainDB == nil {
		return nil, nil, func() {}, errors.New("erigonexec: chain db not initialized")
	}
	temporalDB, ok := c.chainDB.(kv.TemporalRoDB)
	if !ok {
		return nil, nil, func() {}, errors.New("erigonexec: chain db missing temporal support")
	}
	tx, err := temporalDB.BeginTemporalRo(ctx)
	if err != nil {
		return nil, nil, func() {}, err
	}
	release := func() {
		tx.Rollback()
	}
	header := erawdb.ReadHeaderByNumber(tx, blockNum)
	if header == nil {
		release()
		return nil, nil, func() {}, errors.New("erigonexec: header not found")
	}
	maxTxNum, err := rawdbv3.TxNums.Max(tx, blockNum)
	if err != nil {
		release()
		return nil, nil, func() {}, c.wrapHistoryError(err)
	}
	stateReader := estate.NewHistoryReaderV3()
	stateReader.SetTx(tx)
	stateReader.SetTxNum(maxTxNum)
	ibs := estate.New(stateReader)
	ibsArb := estate.NewArbitrum(ibs)
	return header, ibsArb, release, nil
}
