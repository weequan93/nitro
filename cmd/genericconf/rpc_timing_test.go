package genericconf

import (
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/node"
	"github.com/spf13/pflag"
)

func TestRPCSlowCallConfig(t *testing.T) {
	config := DefaultRpcConfig
	if config.SlowCallThreshold != 0 {
		t.Fatal("slow logging enabled by default")
	}
	config.SlowCallThreshold = 5 * time.Second
	var stack node.Config
	config.Apply(&stack)
	if stack.RPCSlowCallThreshold != config.SlowCallThreshold {
		t.Fatal("threshold not applied")
	}
	flags := pflag.NewFlagSet("rpc-timing", pflag.ContinueOnError)
	RpcConfigAddOptions("rpc", flags)
	if err := flags.Parse([]string{"--rpc.slow-call-threshold=5s"}); err != nil {
		t.Fatal(err)
	}
	if value, err := flags.GetDuration("rpc.slow-call-threshold"); err != nil || value != 5*time.Second {
		t.Fatalf("threshold=%v err=%v", value, err)
	}
}

func TestRPCStageDiagnosticsConfig(t *testing.T) {
	config := DefaultRpcConfig
	if config.StageDiagnosticsThreshold != 0 {
		t.Fatal("stage diagnostics enabled by default")
	}
	config.StageDiagnosticsThreshold = time.Second
	var stack node.Config
	config.Apply(&stack)
	if stack.RPCStageDiagnosticsThreshold != time.Second {
		t.Fatal("stage setting not applied")
	}
	flags := pflag.NewFlagSet("rpc-stage", pflag.ContinueOnError)
	RpcConfigAddOptions("rpc", flags)
	if err := flags.Parse([]string{"--rpc.stage-diagnostics-threshold=1s"}); err != nil {
		t.Fatal(err)
	}
	if value, err := flags.GetDuration("rpc.stage-diagnostics-threshold"); err != nil || value != time.Second {
		t.Fatal("stage flag not registered")
	}
	if stack.RPCSlowCallThreshold != 0 {
		t.Fatal("existing independent slow-log setting changed")
	}
}

func TestRPCMapWriteDiagnosticsExplicitOptIn(t *testing.T) {
	config := DefaultRpcConfig
	if config.MapWriteDiagnostics {
		t.Fatal("write diagnostics enabled by default")
	}
	var stack node.Config
	config.Apply(&stack)
	if stack.RPCMapWriteDiagnostics {
		t.Fatal("default application enabled write diagnostics")
	}
	flags := pflag.NewFlagSet("rpc-map-write", pflag.ContinueOnError)
	RpcConfigAddOptions("rpc", flags)
	if value, err := flags.GetBool("rpc.map-write-diagnostics"); err != nil || value {
		t.Fatal("write diagnostics flag must default off")
	}
	if err := flags.Parse([]string{"--rpc.map-write-diagnostics"}); err != nil {
		t.Fatal(err)
	}
	config.MapWriteDiagnostics, _ = flags.GetBool("rpc.map-write-diagnostics")
	config.Apply(&stack)
	if !stack.RPCMapWriteDiagnostics || stack.RPCStageDiagnosticsThreshold != 0 || stack.RPCSlowCallThreshold != 0 {
		t.Fatal("independent write opt-in not applied")
	}
	config.MapWriteDiagnostics = false
	config.Apply(&stack)
	if stack.RPCMapWriteDiagnostics {
		t.Fatal("false setting retained previous write opt-in")
	}
}
