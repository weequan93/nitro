package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"flag"
	"fmt"
	"log"
	"math/big"
	"os"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/ethereum/go-ethereum/ethdb/memorydb"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/holiman/uint256"
)

type getProofResponse struct {
	Address      string               `json:"address"`
	AccountProof []string             `json:"accountProof"`
	Balance      string               `json:"balance"`
	CodeHash     string               `json:"codeHash"`
	Nonce        string               `json:"nonce"`
	StorageHash  string               `json:"storageHash"`
	StorageProof []getProofStorageHit `json:"storageProof"`
}

type getProofStorageHit struct {
	Key   string   `json:"key"`
	Value string   `json:"value"`
	Proof []string `json:"proof"`
}

func main() {
	defaultRPC := os.Getenv("L3_RPC_URL")
	if defaultRPC == "" {
		defaultRPC = "http://localhost:8449"
	}

	rpcURL := flag.String("rpc", defaultRPC, "L3 JSON-RPC URL (or set L3_RPC_URL)")
	addrHex := flag.String("address", "0x502FFdAfd660AEDf4Ea7DB3D758999e154102a6c", "Address to check")
	blockNum := flag.Int64("block", 10, "Block number to query")
	method := flag.String("method", "storageAt", "Check method: storageAt | proof")
	slots := flag.Uint64("slots", 1024, "How many storage slots to scan from slot 0 (0 to skip)")
	proofBatch := flag.Uint64("proofBatch", 64, "Batch size for eth_getProof slot keys (only used with -method=proof)")
	requireFound := flag.Bool("require", false, "Exit with code 1 if no non-zero storage is found")
	timeout := flag.Duration("timeout", 20*time.Second, "RPC call timeout")
	flag.Parse()

	if *blockNum < 0 {
		log.Fatalf("invalid -block: %d", *blockNum)
	}

	address := common.HexToAddress(*addrHex)
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	rpcClient, err := rpc.DialContext(ctx, *rpcURL)
	if err != nil {
		log.Fatalf("dial %s: %v", *rpcURL, err)
	}
	defer rpcClient.Close()

	client := ethclient.NewClient(rpcClient)
	defer client.Close()

	block := big.NewInt(*blockNum)
	code, err := client.CodeAt(ctx, address, block)
	if err != nil {
		log.Fatalf("eth_getCode at block %d: %v", *blockNum, err)
	}

	methodNorm := normalizeMethod(*method)
	fmt.Printf("rpc=%s\naddress=%s\nblock=%d\ncode_len=%d\nmethod=%s\n", *rpcURL, address.Hex(), *blockNum, len(code), methodNorm)

	var foundAny bool
	var foundSlot uint64
	var foundKey common.Hash
	var foundValue []byte

	switch methodNorm {
	case "storageat":
		foundAny, foundSlot, foundKey, foundValue = scanWithStorageAt(client, address, block, *slots, *timeout)
	case "proof":
		if *proofBatch == 0 {
			log.Fatalf("invalid -proofBatch: must be > 0")
		}
		acctExists, acct, err := accountProofInfo(ctx, client, rpcClient, address, *blockNum, *timeout)
		if err != nil {
			log.Fatalf("account proof: %v", err)
		}
		fmt.Printf("account_exists=%v\n", acctExists)
		if acctExists {
			fmt.Printf("account_nonce=%d\naccount_balance=%s\naccount_code_hash=0x%x\naccount_storage_root=%s\naccount_empty=%v\n",
				acct.Nonce,
				formatBalance(acct.Balance),
				acct.CodeHash,
				acct.Root.Hex(),
				isEmptyAccount(acct),
			)
		}
		if *slots > 0 {
			foundAny, foundSlot, foundKey, foundValue = scanWithGetProof(rpcClient, address, *blockNum, *slots, *proofBatch, *timeout)
		}
	default:
		log.Fatalf("invalid -method: %q (expected storageAt|proof)", *method)
	}

	if foundAny {
		fmt.Printf("FOUND slot=%d key=%s value=0x%s\n", foundSlot, foundKey.Hex(), hex.EncodeToString(padLeft32(foundValue)))
	}

	if !foundAny {
		fmt.Printf("NOT_FOUND non-zero storage in slots [0..%d)\n", *slots)
		if *requireFound {
			os.Exit(1)
		}
	}
}

func normalizeMethod(raw string) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	switch v {
	case "storage", "getstorage", "getstorageat":
		return "storageat"
	case "proof", "getproof", "eth_getproof", "p":
		return "proof"
	default:
		return v
	}
}

func accountProofInfo(ctx context.Context, client *ethclient.Client, rpcClient *rpc.Client, address common.Address, blockNum int64, timeout time.Duration) (bool, *types.StateAccount, error) {
	header, err := client.HeaderByNumber(ctx, big.NewInt(blockNum))
	if err != nil {
		return false, nil, fmt.Errorf("get header: %w", err)
	}
	resp, err := getProof(rpcClient, address, nil, blockNum, timeout)
	if err != nil {
		return false, nil, err
	}
	acct, exists, err := verifyAccountProof(header.Root, address, resp.AccountProof)
	if err != nil {
		return false, nil, err
	}
	return exists, acct, nil
}

func getProof(rpcClient *rpc.Client, address common.Address, keys []string, blockNum int64, timeout time.Duration) (getProofResponse, error) {
	blockArg := fmt.Sprintf("0x%x", blockNum)
	if keys == nil {
		keys = []string{}
	}
	callCtx, callCancel := context.WithTimeout(context.Background(), timeout)
	defer callCancel()
	var resp getProofResponse
	if err := rpcClient.CallContext(callCtx, &resp, "eth_getProof", address.Hex(), keys, blockArg); err != nil {
		return getProofResponse{}, fmt.Errorf("eth_getProof failed (node may not support it): %w", err)
	}
	return resp, nil
}

func verifyAccountProof(root common.Hash, addr common.Address, proof []string) (*types.StateAccount, bool, error) {
	db := memorydb.New()
	for _, nodeHex := range proof {
		node := common.FromHex(nodeHex)
		if len(node) == 0 {
			continue
		}
		hash := crypto.Keccak256(node)
		if err := db.Put(hash, node); err != nil {
			return nil, false, fmt.Errorf("proof db put: %w", err)
		}
	}
	key := crypto.Keccak256(addr.Bytes())
	value, err := trie.VerifyProof(root, key, db)
	if err != nil {
		return nil, false, fmt.Errorf("verify proof: %w", err)
	}
	if value == nil {
		return nil, false, nil
	}
	var acct types.StateAccount
	if err := rlp.DecodeBytes(value, &acct); err != nil {
		return nil, false, fmt.Errorf("decode account: %w", err)
	}
	return &acct, true, nil
}

func scanWithStorageAt(client *ethclient.Client, address common.Address, block *big.Int, slots uint64, timeout time.Duration) (bool, uint64, common.Hash, []byte) {
	if slots == 0 {
		return false, 0, common.Hash{}, nil
	}
	for i := uint64(0); i < slots; i++ {
		keyInt := new(big.Int).SetUint64(i)
		slotKey := common.BigToHash(keyInt)

		callCtx, callCancel := context.WithTimeout(context.Background(), timeout)
		value, err := client.StorageAt(callCtx, address, slotKey, block)
		callCancel()
		if err != nil {
			log.Fatalf("eth_getStorageAt slot %d: %v", i, err)
		}

		if hasNonZeroByte(value) {
			return true, i, slotKey, value
		}
	}
	return false, 0, common.Hash{}, nil
}

func scanWithGetProof(rpcClient *rpc.Client, address common.Address, blockNum int64, slots uint64, batch uint64, timeout time.Duration) (bool, uint64, common.Hash, []byte) {
	if slots == 0 {
		return false, 0, common.Hash{}, nil
	}
	for start := uint64(0); start < slots; start += batch {
		end := start + batch
		if end > slots {
			end = slots
		}

		keys := make([]string, 0, end-start)
		for i := start; i < end; i++ {
			keyInt := new(big.Int).SetUint64(i)
			slotKey := common.BigToHash(keyInt)
			keys = append(keys, slotKey.Hex())
		}

		resp, err := getProof(rpcClient, address, keys, blockNum, timeout)
		if err != nil {
			log.Fatalf("eth_getProof failed (node may not support it): %v", err)
		}

		for idx, hit := range resp.StorageProof {
			valueBytes := common.FromHex(hit.Value)
			if hasNonZeroByte(valueBytes) {
				foundSlot := start + uint64(idx)
				foundKey := common.HexToHash(hit.Key)
				return true, foundSlot, foundKey, valueBytes
			}
		}
	}
	return false, 0, common.Hash{}, nil
}

func isEmptyAccount(acct *types.StateAccount) bool {
	if acct == nil {
		return true
	}
	balanceZero := acct.Balance == nil || acct.Balance.IsZero()
	codeEmpty := len(acct.CodeHash) == 0 || bytes.Equal(acct.CodeHash, types.EmptyCodeHash[:])
	return acct.Nonce == 0 && balanceZero && codeEmpty
}

func formatBalance(balance *uint256.Int) string {
	if balance == nil {
		return "0"
	}
	return balance.String()
}

func hasNonZeroByte(b []byte) bool {
	for _, v := range b {
		if v != 0 {
			return true
		}
	}
	return false
}

func padLeft32(b []byte) []byte {
	if len(b) >= 32 {
		return b[len(b)-32:]
	}
	out := make([]byte, 32)
	copy(out[32-len(b):], b)
	return out
}
