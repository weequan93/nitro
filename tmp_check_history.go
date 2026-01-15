package main
import (
  "fmt"
  "log"
  "github.com/erigontech/erigon-lib/kv/mdbx"
  "github.com/erigontech/erigon-lib/kv"
  "github.com/erigontech/erigon/common"
)
func main(){
  dbPath := "/tmp/mdbx-debug10/l2chaindata"
  keyHex := "502ffdafd660aedf4ea7db3d758999e154102a6c"
  key := common.Hex2Bytes(keyHex)
  db, err := mdbx.NewMDBX(log.Default()).Path(dbPath).MustOpen()
  if err!=nil {log.Fatal(err)}
  defer db.Close()
  tx, err := db.BeginRo(context.Background())
  if err!=nil {log.Fatal(err)}
  defer tx.Rollback()
  c, err := tx.Cursor(kv.AccountHistory)
  if err!=nil {log.Fatal(err)}
  defer c.Close()
  for k,v,err:=c.Seek(key); err==nil && k!=nil && string(k[:len(key)])==string(key); k,v,err=c.Next(){
    fmt.Printf("hist key txnum=%x len=%d\n", v, len(v))
  }
}
