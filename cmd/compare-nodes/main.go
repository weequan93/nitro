package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
)

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type rpcResponse struct {
	Result json.RawMessage `json:"result"`
	Error  *rpcError       `json:"error,omitempty"`
}

type rpcBlock struct {
	Hash             string `json:"hash"`
	StateRoot        string `json:"stateRoot"`
	ReceiptsRoot     string `json:"receiptsRoot"`
	TransactionsRoot string `json:"transactionsRoot"`
	ParentHash       string `json:"parentHash"`
	GasUsed          string `json:"gasUsed"`
}

type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ",") }
func (m *multiFlag) Set(value string) error {
	*m = append(*m, value)
	return nil
}

func callRPC(client *http.Client, url, method string, params any) (json.RawMessage, error) {
	reqBody, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      1,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest("POST", url, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var out rpcResponse
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	if out.Error != nil {
		return nil, fmt.Errorf("%s error: %s", method, out.Error.Message)
	}
	return out.Result, nil
}

func hexToUint64(hexStr string) (uint64, error) {
	hexStr = strings.TrimSpace(hexStr)
	if hexStr == "" {
		return 0, fmt.Errorf("empty hex string")
	}
	return strconv.ParseUint(strings.TrimPrefix(hexStr, "0x"), 16, 64)
}

func normalizeBlockTag(tag string) (string, error) {
	switch tag {
	case "latest", "earliest", "pending":
		return tag, nil
	}
	if strings.HasPrefix(tag, "0x") {
		return tag, nil
	}
	num, err := strconv.ParseUint(tag, 10, 64)
	if err != nil {
		return "", fmt.Errorf("invalid block tag: %v", err)
	}
	return fmt.Sprintf("0x%x", num), nil
}

func getBlock(client *http.Client, url string, number uint64) (*rpcBlock, error) {
	result, err := callRPC(client, url, "eth_getBlockByNumber", []any{fmt.Sprintf("0x%x", number), false})
	if err != nil {
		return nil, err
	}
	if string(result) == "null" {
		return nil, nil
	}
	var block rpcBlock
	if err := json.Unmarshal(result, &block); err != nil {
		return nil, err
	}
	return &block, nil
}

func compareField(label, a, b string, mismatches *[]string) {
	if a != b {
		*mismatches = append(*mismatches, fmt.Sprintf("%s: %s != %s", label, a, b))
	}
}

func sampleRange(start, end uint64, count int, seed int64) []uint64 {
	if count <= 0 {
		return nil
	}
	rangeLen := end - start + 1
	if uint64(count) >= rangeLen {
		all := make([]uint64, 0, rangeLen)
		for n := start; n <= end; n++ {
			all = append(all, n)
		}
		return all
	}
	rng := rand.New(rand.NewSource(seed))
	// For small sample sizes, random pick with a set is efficient.
	if count <= 10000 {
		picks := make(map[uint64]struct{}, count)
		for len(picks) < count {
			n := start + uint64(rng.Int63n(int64(rangeLen)))
			picks[n] = struct{}{}
		}
		out := make([]uint64, 0, count)
		for n := range picks {
			out = append(out, n)
		}
		return out
	}
	// Reservoir sampling to avoid large memory usage.
	out := make([]uint64, 0, count)
	i := 0
	for n := start; n <= end; n++ {
		if i < count {
			out = append(out, n)
		} else {
			j := rng.Intn(i + 1)
			if j < count {
				out[j] = n
			}
		}
		i++
	}
	return out
}

func main() {
	var (
		urlA   string
		urlB   string
		start  int64
		end    int64
		step   uint64
		samples int
		seed   int64
		addrs  multiFlag
		keys   multiFlag
		acctBlock string
		timeoutSec int
	)
	flag.StringVar(&urlA, "a", "", "RPC URL for the original node")
	flag.StringVar(&urlB, "b", "", "RPC URL for the migrated node")
	flag.Int64Var(&start, "from", -1, "start block number (default 0)")
	flag.Int64Var(&end, "to", -1, "end block number (default min head)")
	flag.Uint64Var(&step, "step", 1000, "step between blocks")
	flag.IntVar(&samples, "samples", 0, "random samples within range")
	flag.Int64Var(&seed, "seed", 1337, "random seed for samples")
	flag.Var(&addrs, "addr", "address to compare (repeatable)")
	flag.Var(&keys, "storage-key", "storage key to compare (repeatable)")
	flag.StringVar(&acctBlock, "account-block", "latest", "block tag/number for account checks (default latest)")
	flag.IntVar(&timeoutSec, "timeout", 30, "RPC timeout seconds")
	flag.Parse()

	if urlA == "" || urlB == "" {
		fmt.Fprintln(os.Stderr, "missing --a or --b")
		os.Exit(2)
	}
	if step == 0 {
		fmt.Fprintln(os.Stderr, "step must be > 0")
		os.Exit(2)
	}

	client := &http.Client{Timeout: time.Duration(timeoutSec) * time.Second}
	mismatches := make([]string, 0)

	chainA, err := callRPC(client, urlA, "eth_chainId", []any{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	chainB, err := callRPC(client, urlB, "eth_chainId", []any{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	compareField("chainId", strings.Trim(string(chainA), `"`), strings.Trim(string(chainB), `"`), &mismatches)

	headAHex, err := callRPC(client, urlA, "eth_blockNumber", []any{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	headBHex, err := callRPC(client, urlB, "eth_blockNumber", []any{})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	headA, err := hexToUint64(strings.Trim(string(headAHex), `"`))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	headB, err := hexToUint64(strings.Trim(string(headBHex), `"`))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	head := headA
	if headB < headA {
		head = headB
	}
	if headA != headB {
		mismatches = append(mismatches, fmt.Sprintf("head mismatch: a=%d b=%d (using min for block compare)", headA, headB))
	}

	startBlock := uint64(0)
	if start >= 0 {
		startBlock = uint64(start)
	}
	endBlock := head
	if end >= 0 {
		endBlock = uint64(end)
	}
	if startBlock > endBlock {
		fmt.Fprintf(os.Stderr, "invalid range: start %d > end %d\n", startBlock, endBlock)
		os.Exit(2)
	}

	checked := map[uint64]struct{}{}
	doCheck := func(num uint64) {
		blockA, err := getBlock(client, urlA, num)
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("block %d: rpc error a=%v", num, err))
			return
		}
		blockB, err := getBlock(client, urlB, num)
		if err != nil {
			mismatches = append(mismatches, fmt.Sprintf("block %d: rpc error b=%v", num, err))
			return
		}
		if blockA == nil || blockB == nil {
			mismatches = append(mismatches, fmt.Sprintf("block %d: missing (a=%v b=%v)", num, blockA == nil, blockB == nil))
			return
		}
		compareField(fmt.Sprintf("block %d hash", num), blockA.Hash, blockB.Hash, &mismatches)
		compareField(fmt.Sprintf("block %d stateRoot", num), blockA.StateRoot, blockB.StateRoot, &mismatches)
		compareField(fmt.Sprintf("block %d receiptsRoot", num), blockA.ReceiptsRoot, blockB.ReceiptsRoot, &mismatches)
		compareField(fmt.Sprintf("block %d transactionsRoot", num), blockA.TransactionsRoot, blockB.TransactionsRoot, &mismatches)
		compareField(fmt.Sprintf("block %d parentHash", num), blockA.ParentHash, blockB.ParentHash, &mismatches)
		compareField(fmt.Sprintf("block %d gasUsed", num), blockA.GasUsed, blockB.GasUsed, &mismatches)
	}

	for num := startBlock; num <= endBlock; num += step {
		doCheck(num)
		if samples > 0 {
			checked[num] = struct{}{}
		}
		if endBlock-num < step {
			break
		}
	}

	if samples > 0 {
		for _, num := range sampleRange(startBlock, endBlock, samples, seed) {
			if _, ok := checked[num]; ok {
				continue
			}
			doCheck(num)
		}
	}

	if len(addrs) > 0 {
		tag, err := normalizeBlockTag(acctBlock)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		for _, addr := range addrs {
			balA, err := callRPC(client, urlA, "eth_getBalance", []any{addr, tag})
			if err != nil {
				mismatches = append(mismatches, fmt.Sprintf("balance %s: a error %v", addr, err))
				continue
			}
			balB, err := callRPC(client, urlB, "eth_getBalance", []any{addr, tag})
			if err != nil {
				mismatches = append(mismatches, fmt.Sprintf("balance %s: b error %v", addr, err))
				continue
			}
			compareField(fmt.Sprintf("balance %s @ %s", addr, tag), strings.Trim(string(balA), `"`), strings.Trim(string(balB), `"`), &mismatches)

			nonceA, err := callRPC(client, urlA, "eth_getTransactionCount", []any{addr, tag})
			if err != nil {
				mismatches = append(mismatches, fmt.Sprintf("nonce %s: a error %v", addr, err))
				continue
			}
			nonceB, err := callRPC(client, urlB, "eth_getTransactionCount", []any{addr, tag})
			if err != nil {
				mismatches = append(mismatches, fmt.Sprintf("nonce %s: b error %v", addr, err))
				continue
			}
			compareField(fmt.Sprintf("nonce %s @ %s", addr, tag), strings.Trim(string(nonceA), `"`), strings.Trim(string(nonceB), `"`), &mismatches)

			codeA, err := callRPC(client, urlA, "eth_getCode", []any{addr, tag})
			if err != nil {
				mismatches = append(mismatches, fmt.Sprintf("code %s: a error %v", addr, err))
				continue
			}
			codeB, err := callRPC(client, urlB, "eth_getCode", []any{addr, tag})
			if err != nil {
				mismatches = append(mismatches, fmt.Sprintf("code %s: b error %v", addr, err))
				continue
			}
			compareField(fmt.Sprintf("code %s @ %s", addr, tag), strings.Trim(string(codeA), `"`), strings.Trim(string(codeB), `"`), &mismatches)

			for _, key := range keys {
				valA, err := callRPC(client, urlA, "eth_getStorageAt", []any{addr, key, tag})
				if err != nil {
					mismatches = append(mismatches, fmt.Sprintf("storage %s %s: a error %v", addr, key, err))
					continue
				}
				valB, err := callRPC(client, urlB, "eth_getStorageAt", []any{addr, key, tag})
				if err != nil {
					mismatches = append(mismatches, fmt.Sprintf("storage %s %s: b error %v", addr, key, err))
					continue
				}
				compareField(fmt.Sprintf("storage %s %s @ %s", addr, key, tag), strings.Trim(string(valA), `"`), strings.Trim(string(valB), `"`), &mismatches)
			}
		}
	}

	if len(mismatches) > 0 {
		fmt.Println("MISMATCHES:")
		for _, m := range mismatches {
			fmt.Printf("- %s\n", m)
		}
		os.Exit(1)
	}

	fmt.Println("OK: nodes match for requested checks")
}
