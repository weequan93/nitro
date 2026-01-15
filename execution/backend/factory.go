package backend

import (
	"fmt"

	"github.com/offchainlabs/nitro/execution"
)

type Kind string

const (
	KindAuto   Kind = "auto"
	KindGeth   Kind = "geth"
	KindErigon Kind = "erigon"
)

func ParseKind(value string) (Kind, error) {
	switch value {
	case "", string(KindAuto):
		return KindAuto, nil
	case string(KindGeth):
		return KindGeth, nil
	case string(KindErigon):
		return KindErigon, nil
	default:
		return Kind(""), fmt.Errorf("unknown execution backend: %q", value)
	}
}

func Select(kind Kind, gethFactory, erigonFactory func() (execution.FullExecutionClient, error)) (execution.FullExecutionClient, error) {
	switch kind {
	case KindAuto, KindGeth:
		if gethFactory == nil {
			return nil, fmt.Errorf("geth backend factory not set")
		}
		return gethFactory()
	case KindErigon:
		if erigonFactory == nil {
			return nil, fmt.Errorf("erigon backend factory not set")
		}
		return erigonFactory()
	default:
		return nil, fmt.Errorf("invalid backend kind: %q", kind)
	}
}
