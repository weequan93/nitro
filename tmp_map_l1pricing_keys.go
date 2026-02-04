package main

import (
	"encoding/hex"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"

	"github.com/offchainlabs/nitro/arbos/arbosState"
	"github.com/offchainlabs/nitro/arbos/l1pricing"
	"github.com/offchainlabs/nitro/arbos/storage"
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
	posterInfoKey := crypto.Keccak256(batchPosterKey, []byte{1})

	fmt.Printf("l1_subspace_key=%s\n", hexBytes(l1SubKey))
	fmt.Printf("batchposter_key=%s\n", hexBytes(batchPosterKey))
	fmt.Printf("poster_addrs_key=%s\n", hexBytes(posterAddrsKey))
	fmt.Printf("poster_info_key=%s\n", hexBytes(posterInfoKey))

	fmt.Println("L1Pricing offsets (mapAddress raw slots):")
	for off := uint64(0); off <= 11; off++ {
		key := common.BigToHash(new(big.Int).SetUint64(off))
		slot := mapAddress(l1SubKey, key)
		fmt.Printf("  off=%d key=%s slot=%s\n", off, key.Hex(), slot.Hex())
	}

	fmt.Println("BatchPoster table offsets (mapAddress raw slots):")
	for off := uint64(0); off <= 1; off++ {
		key := common.BigToHash(new(big.Int).SetUint64(off))
		slot := mapAddress(batchPosterKey, key)
		fmt.Printf("  off=%d key=%s slot=%s\n", off, key.Hex(), slot.Hex())
	}

	fmt.Println("BatchPoster posterInfo substorage slots:")
	poster := l1pricing.BatchPosterAddress
	for off := uint64(0); off <= 1; off++ {
		key := common.BigToHash(new(big.Int).SetUint64(off))
		subKey := crypto.Keccak256(posterInfoKey, poster.Bytes())
		slot := mapAddress(subKey, key)
		fmt.Printf("  poster=%s off=%d slot=%s\n", poster.Hex(), off, slot.Hex())
	}

	fmt.Println("ArbOS core offsets (mapAddress raw slots):")
	for off := uint64(0); off <= 7; off++ {
		key := common.BigToHash(new(big.Int).SetUint64(off))
		slot := mapAddress(rootKey, key)
		fmt.Printf("  off=%d key=%s slot=%s\n", off, key.Hex(), slot.Hex())
	}

	_ = storage.NewGeth // avoid unused import in case build tags change
	_ = arbosState.ArbOSVersion
}
