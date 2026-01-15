package main

import (
	"fmt"
	"os"
	"strings"
)

func logKV(phase string, kv ...interface{}) {
	if len(kv)%2 != 0 {
		fmt.Fprintf(os.Stdout, "phase=%s msg=invalid_log_fields\n", phase)
		return
	}
	var b strings.Builder
	b.WriteString("phase=")
	b.WriteString(phase)
	for i := 0; i < len(kv); i += 2 {
		key, ok := kv[i].(string)
		if !ok {
			key = fmt.Sprint(kv[i])
		}
		b.WriteByte(' ')
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(fmt.Sprint(kv[i+1]))
	}
	b.WriteByte('\n')
	_, _ = os.Stdout.WriteString(b.String())
}
