package erigonexec

import "io"

type MdbxOptions struct {
	PageSize   int
	MapSize    int64
	GrowthStep int64
	WriteMap   bool
	NoSync     bool
	MaxReaders int
}

type Databases struct {
	ChainDB io.Closer
	ArbDB   io.Closer
	WasmDB  io.Closer
}

func (d *Databases) Close() error {
	var err error
	if d.ChainDB != nil {
		if closeErr := d.ChainDB.Close(); closeErr != nil {
			err = closeErr
		}
	}
	if d.ArbDB != nil {
		if closeErr := d.ArbDB.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	if d.WasmDB != nil {
		if closeErr := d.WasmDB.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}
	return err
}
