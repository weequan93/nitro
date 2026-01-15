//go:build !erigon
// +build !erigon

package timeboost

import _ "github.com/mattn/go-sqlite3"

const sqliteDriverName = "sqlite3"
