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
	target := common.HexToHash("0xcbc64323f89fef3d29be3a5d098994b791e3edd15a731ee8f562dc8d5d446771")

	addrs := []common.Address{
		common.HexToAddress("0x31c5a1C83265113bd089385d76dfe4D8A2577204"),
		common.HexToAddress("0x28c18bc63069e3581870904f32Dd34D9e3332cce"),
		common.HexToAddress("0xA4b000000000000000000073657175656e636572"),
		common.HexToAddress("0xA4B00000000000000000000000000000000000f6"),
		common.HexToAddress("0x00000000000000000000000000000000000A4B05"),
		common.HexToAddress("0xA4B05FFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFFF"),
	}

	rootKey := []byte{}
	subspaces := []struct {
		name string
		key  []byte
	}{
		{"addressTable", crypto.Keccak256(rootKey, []byte{3})},
		{"chainOwners", crypto.Keccak256(rootKey, []byte{4})},
		{"gasless", crypto.Keccak256(rootKey, []byte{9})},
	}

	for _, ss := range subspaces {
		// addressTable byAddress uses OpenSubStorage([]byte{})
		addrTableByAddr := crypto.Keccak256(ss.key, []byte{})
		// addressSet byAddress uses OpenCachedSubStorage([]byte{0}) then OpenSubStorage([]byte{0})
		addrSetRoot := crypto.Keccak256(ss.key, []byte{0})
		addrSetByAddr := crypto.Keccak256(addrSetRoot, []byte{0})

		for _, addr := range addrs {
			key := common.BytesToHash(addr.Bytes())
			slot1 := mapAddress(addrTableByAddr, key)
			slot2 := mapAddress(addrSetByAddr, key)
			if slot1 == target {
				fmt.Printf("match: subspace=%s addrTableByAddr=%s addr=%s key=%s slot=%s\n",
					ss.name, hexBytes(addrTableByAddr), addr.Hex(), key.Hex(), slot1.Hex())
			}
			if slot2 == target {
				fmt.Printf("match: subspace=%s addrSetByAddr=%s addr=%s key=%s slot=%s\n",
					ss.name, hexBytes(addrSetByAddr), addr.Hex(), key.Hex(), slot2.Hex())
			}
		}
	}
}
