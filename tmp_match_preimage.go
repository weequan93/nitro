package main

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"os"
	"strings"

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
	target := common.HexToHash("0xcbc64323f89fef3d29be3a5d098994b791e3edd15a731ee8f562dc8d5d446771")
	addrFile := "/tmp/debug_addrs.txt"

	f, err := os.Open(addrFile)
	if err != nil {
		panic(err)
	}
	defer f.Close()

	var addrs []common.Address
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		addrs = append(addrs, common.HexToAddress(line))
	}

	rootKey := []byte{}
	l1SubKey := crypto.Keccak256(rootKey, []byte{0})
	batchPosterKey := crypto.Keccak256(l1SubKey, []byte{0})
	posterAddrsKey := crypto.Keccak256(batchPosterKey, []byte{0})
	posterAddrsByAddrKey := crypto.Keccak256(posterAddrsKey, []byte{0})

	type storageCandidate struct {
		name string
		key  []byte
	}

	candidates := []storageCandidate{
		{"addressTable.byAddress", crypto.Keccak256(crypto.Keccak256(rootKey, []byte{3}), []byte{})},
		{"chainOwners.byAddress", crypto.Keccak256(crypto.Keccak256(crypto.Keccak256(rootKey, []byte{4}), []byte{0}), []byte{0})},
		{"gasless.byAddress", crypto.Keccak256(crypto.Keccak256(crypto.Keccak256(rootKey, []byte{9}), []byte{0}), []byte{0})},
		{"blacklist.owner.byAddress", crypto.Keccak256(crypto.Keccak256(crypto.Keccak256(rootKey, []byte{12}), []byte{0}), []byte{0})},
		{"blacklist.from.byAddress", crypto.Keccak256(crypto.Keccak256(crypto.Keccak256(rootKey, []byte{12}), []byte{1}), []byte{0})},
		{"blacklist.to.byAddress", crypto.Keccak256(crypto.Keccak256(crypto.Keccak256(rootKey, []byte{12}), []byte{2}), []byte{0})},
		{"batchPoster.byAddress", posterAddrsByAddrKey},
	}

	for _, c := range candidates {
		for _, addr := range addrs {
			key := common.BytesToHash(addr.Bytes())
			slot := mapAddress(c.key, key)
			if slot == target {
				fmt.Printf("match: name=%s storageKey=%s addr=%s key=%s slot=%s\n",
					c.name, hexBytes(c.key), addr.Hex(), key.Hex(), slot.Hex())
			}
		}
	}
}
