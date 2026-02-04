package main

import (
	"encoding/hex"
	"fmt"
	"math/big"

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
	l2Subspace := []byte{1}
	l2SubKey := crypto.Keccak256(rootKey, l2Subspace)

	fmt.Printf("l2_subspace_key=%s\n", hexBytes(l2SubKey))
	fmt.Println("L2Pricing offsets (mapAddress raw slots):")
	for off := uint64(0); off <= 6; off++ {
		key := common.BigToHash(new(big.Int).SetUint64(off))
		slot := mapAddress(l2SubKey, key)
		fmt.Printf("  off=%d key=%s slot=%s\n", off, key.Hex(), slot.Hex())
	}
}
