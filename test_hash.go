package main

import (
	"fmt"
	"math/big"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func main() {
	sk := crypto.Keccak256([]byte{0}) // Subspace 0
	key := uint64(11)                 // Offset 11
	keyBytes := common.BigToHash(new(big.Int).SetUint64(key)).Bytes()
	boundary := 31
	mapped := append(crypto.Keccak256(sk, keyBytes[:boundary])[:boundary], keyBytes[boundary])
	fmt.Printf("%x\n", mapped)
}
