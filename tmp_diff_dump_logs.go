package main

import (
    "bufio"
    "encoding/hex"
    "fmt"
    "log"
    "os"
    "strings"

    "github.com/ethereum/go-ethereum/crypto"
)

type destEntry struct {
    rawSlot string
    val     string
}

type srcEntry struct {
    preimage string
    val      string
}

func parseField(parts []string, prefix string) string {
	for _, p := range parts {
		if strings.HasPrefix(p, prefix) {
			return strings.TrimPrefix(p, prefix)
		}
	}
	return ""
}

func trimHex(s string) string {
	s = strings.TrimPrefix(s, "0x")
	s = strings.TrimLeft(s, "0")
	if s == "" {
		return "0"
	}
	return s
}

func keccakHex(slot string) (string, error) {
    b, err := hex.DecodeString(strings.TrimPrefix(slot, "0x"))
    if err != nil {
        return "", err
    }
    h := crypto.Keccak256Hash(b)
    return strings.TrimPrefix(h.Hex(), "0x"), nil
}

func readDest(path string) (map[string]destEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := make(map[string]destEntry)
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if !strings.HasPrefix(line, "slot=") {
			continue
		}
		parts := strings.Split(line, " ")
		if len(parts) < 2 {
			continue
		}
        slot := strings.TrimPrefix(parts[0], "slot=")
        val := ""
		for _, p := range parts[1:] {
			if strings.HasPrefix(p, "value=") {
				val = strings.TrimPrefix(p, "value=")
				break
			}
		}
		if slot == "" {
			continue
		}
        slot = strings.TrimPrefix(slot, "0x")
        if len(slot) != 64 {
            continue
        }
        hashed, err := keccakHex(slot)
        if err != nil {
            return nil, err
        }
        m[strings.ToLower(hashed)] = destEntry{rawSlot: slot, val: trimHex(val)}
    }
	if err := s.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

func readSrc(path string) (map[string]srcEntry, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	m := make(map[string]srcEntry)
	s := bufio.NewScanner(f)
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if !strings.HasPrefix(line, "slot=") {
			continue
		}
		parts := strings.Split(line, " ")
		if len(parts) < 2 {
			continue
		}
        slotHash := strings.TrimPrefix(parts[0], "slot=")
        preimage := parseField(parts[1:], "preimage=")
        val := parseField(parts[1:], "val=")
        if slotHash == "" {
            continue
        }
        slotHash = strings.TrimPrefix(slotHash, "0x")
        if len(slotHash) != 64 {
            continue
        }
        m[strings.ToLower(slotHash)] = srcEntry{preimage: strings.TrimPrefix(preimage, "0x"), val: trimHex(val)}
    }
	if err := s.Err(); err != nil {
		return nil, err
	}
	return m, nil
}

func main() {
	destPath := "dumpstorage.log"
	srcPath := "dumpstorage_src.log"
	if v := os.Getenv("DEST_LOG"); v != "" {
		destPath = v
	}
	if v := os.Getenv("SRC_LOG"); v != "" {
		srcPath = v
	}
	dest, err := readDest(destPath)
	if err != nil {
		log.Fatalf("read dest: %v", err)
	}
	src, err := readSrc(srcPath)
	if err != nil {
		log.Fatalf("read src: %v", err)
	}

	fmt.Printf("dest_hashed=%d src_hashed=%d\n", len(dest), len(src))

	missing := 0
	for h := range src {
		if _, ok := dest[h]; !ok {
			missing++
		}
	}
	extra := 0
	for h := range dest {
		if _, ok := src[h]; !ok {
			extra++
		}
	}
	fmt.Printf("missing_in_dest=%d extra_in_dest=%d\n", missing, extra)

	if missing > 0 {
		fmt.Println("missing_in_dest:")
        for h, se := range src {
            if _, ok := dest[h]; ok {
                continue
            }
            if se.preimage != "" {
                fmt.Printf("  hash=%s preimage=%s val=%s\n", h, se.preimage, se.val)
            } else {
                fmt.Printf("  hash=%s val=%s\n", h, se.val)
            }
        }
    }
	if extra > 0 {
		fmt.Println("extra_in_dest:")
        for h, de := range dest {
            if _, ok := src[h]; ok {
                continue
            }
            fmt.Printf("  hash=%s preimage=%s val=%s\n", h, de.rawSlot, de.val)
        }
    }
}
