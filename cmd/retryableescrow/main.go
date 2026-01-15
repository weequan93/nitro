package main

import (
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/ethereum/go-ethereum/common"
	"github.com/offchainlabs/nitro/arbos/retryables"
)

func main() {
	ticketHex := flag.String("ticket", "", "Retryable ticket id (32-byte hash, usually the tx hash)")
	txHex := flag.String("tx", "", "Alias for -ticket")
	flag.Parse()

	input := strings.TrimSpace(*ticketHex)
	if input == "" {
		input = strings.TrimSpace(*txHex)
	}
	if input == "" && flag.NArg() > 0 {
		input = strings.TrimSpace(flag.Arg(0))
	}
	if input == "" {
		log.Fatal("missing ticket id: use -ticket 0x<hash>")
	}
	if !strings.HasPrefix(input, "0x") {
		input = "0x" + input
	}
	if len(input) != 66 {
		log.Fatalf("ticket id must be 32-byte hex (got len=%d): %s", len(input), input)
	}

	ticket := common.HexToHash(input)
	escrow := retryables.RetryableEscrowAddress(ticket)

	fmt.Printf("ticket_id=%s\nescrow=%s\n", ticket.Hex(), escrow.Hex())
}
