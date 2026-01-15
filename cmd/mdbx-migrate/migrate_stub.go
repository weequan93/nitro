//go:build !erigon
// +build !erigon

package main

import "errors"

func migrate(opts Options) error {
	_ = opts
	return ExitError{Code: ExitMigration, Err: errors.New("mdbx-migrate: migrate not implemented")}
}

func verify(opts Options) error {
	_ = opts
	return ExitError{Code: ExitVerification, Err: errors.New("mdbx-migrate: verify not implemented")}
}
