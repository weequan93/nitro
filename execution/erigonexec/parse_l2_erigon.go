//go:build erigon
// +build erigon

package erigonexec

import (
	"fmt"
	"math/big"

	etypes "github.com/erigontech/erigon/execution/types"
	gtypes "github.com/ethereum/go-ethereum/core/types"

	"github.com/offchainlabs/nitro/arbos"
	"github.com/offchainlabs/nitro/arbos/arbostypes"
)

func parseL2TransactionsErigon(msg *arbostypes.L1IncomingMessage, chainID *big.Int) (etypes.Transactions, error) {
	gethTxs, err := arbos.ParseL2Transactions(msg, chainID)
	if err != nil {
		return nil, err
	}
	return convertGethTransactions(gethTxs)
}

func convertGethTransactions(gethTxs gtypes.Transactions) (etypes.Transactions, error) {
	out := make(etypes.Transactions, 0, len(gethTxs))
	for _, tx := range gethTxs {
		if tx == nil {
			continue
		}
		encoded, err := tx.MarshalBinary()
		if err != nil {
			return nil, fmt.Errorf("encode transaction: %w", err)
		}
		etx, err := etypes.DecodeTransaction(encoded)
		if err != nil {
			return nil, fmt.Errorf("decode transaction: %w", err)
		}
		out = append(out, etx)
	}
	return out, nil
}
