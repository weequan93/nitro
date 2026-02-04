package main

import (
	"encoding/hex"
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func mapAddress(storageKey []byte, key common.Hash) common.Hash {
	keyBytes := key.Bytes()
	boundary := common.HashLength - 1
	mapped := make([]byte, 0, common.HashLength)
	mapped = append(mapped, crypto.Keccak256(storageKey, keyBytes[:boundary])[:boundary]...)
	mapped = append(mapped, keyBytes[boundary])
	return common.BytesToHash(mapped)
}

func hexBytes(b []byte) string {
	return "0x" + hex.EncodeToString(b)
}

func main() {
	rootKey := []byte{}
	l1Subspace := []byte{0}
	l1SubKey := crypto.Keccak256(rootKey, l1Subspace)
	batchPosterKey := crypto.Keccak256(l1SubKey, []byte{0})
	posterAddrsKey := crypto.Keccak256(batchPosterKey, []byte{0})
	posterAddrsByAddrKey := crypto.Keccak256(posterAddrsKey, []byte{0})

	poster := common.HexToAddress("0x31c5a1C83265113bd089385d76dfe4D8A2577204")
	key := common.BytesToHash(poster.Bytes())
	slot := mapAddress(posterAddrsByAddrKey, key)

	fmt.Printf("poster_addrs_key=%s\n", hexBytes(posterAddrsKey))
	fmt.Printf("poster_addrs_by_addr_key=%s\n", hexBytes(posterAddrsByAddrKey))
	fmt.Printf("poster=%s key=%s slot=%s\n", poster.Hex(), key.Hex(), slot.Hex())
}
