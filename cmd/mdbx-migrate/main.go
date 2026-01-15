package main

import (
	"fmt"
	"os"

	elog "github.com/erigontech/erigon-lib/log/v3"
	flag "github.com/spf13/pflag"

	"github.com/offchainlabs/nitro/cmd/conf"
)

func main() {
	if os.Getenv("MDBX_MIGRATE_DEBUG") != "" || os.Getenv("ERIGON_BAD_ROOT_DEBUG") != "" {
		elog.Root().SetHandler(elog.LvlFilterHandler(elog.LvlInfo, elog.StdoutHandler))
	}

	var opts Options
	opts.Mdbx = conf.MdbxConfigDefault

	verifyModes := map[string]struct{}{
		"none":     {},
		"basic":    {},
		"extended": {},
		"strict":   {},
	}

	flag.StringVar(&opts.Source, "source", "", "path to Pebble chain directory (read-only)")
	flag.StringVar(&opts.Dest, "dest", "", "path to MDBX chain directory")
	flag.StringVar(&opts.Mode, "mode", "full", "migration mode (full, state)")
	flag.StringVar(&opts.Verify, "verify", "extended", "verification level (none, basic, extended, strict)")
	flag.IntVar(&opts.VerifySamples, "verify-samples", 20, "number of sampled blocks for history checks")
	flag.BoolVar(&opts.Resume, "resume", false, "resume from last checkpoint")
	flag.Uint64Var(&opts.StartBlock, "start-block", 0, "start block for replay (testing)")
	flag.Uint64Var(&opts.EndBlock, "end-block", 0, "end block for replay (testing, 0 = head)")
	flag.IntVar(&opts.Workers, "workers", 0, "worker count for execution stages (0 = default)")
	conf.MdbxConfigAddOptions("mdbx", flag.CommandLine, &opts.Mdbx)
	flag.Parse()

	if opts.Source == "" || opts.Dest == "" {
		fmt.Fprintln(os.Stderr, "mdbx-migrate: --source and --dest are required")
		os.Exit(ExitPreflight)
	}
	if opts.Mode != "full" && opts.Mode != "state" {
		fmt.Fprintf(os.Stderr, "mdbx-migrate: invalid --mode %q (expected full or state)\n", opts.Mode)
		os.Exit(ExitPreflight)
	}
	if _, ok := verifyModes[opts.Verify]; !ok {
		fmt.Fprintf(os.Stderr, "mdbx-migrate: invalid --verify %q (expected basic, extended, or strict)\n", opts.Verify)
		os.Exit(ExitPreflight)
	}
	if opts.VerifySamples <= 0 {
		fmt.Fprintf(os.Stderr, "mdbx-migrate: invalid --verify-samples %d (expected > 0)\n", opts.VerifySamples)
		os.Exit(ExitPreflight)
	}
	if opts.Workers < 0 {
		fmt.Fprintf(os.Stderr, "mdbx-migrate: invalid --workers %d (expected >= 0)\n", opts.Workers)
		os.Exit(ExitPreflight)
	}
	if opts.EndBlock != 0 && opts.EndBlock < opts.StartBlock {
		fmt.Fprintf(os.Stderr, "mdbx-migrate: invalid block range %d..%d\n", opts.StartBlock, opts.EndBlock)
		os.Exit(ExitPreflight)
	}
	if err := opts.Mdbx.Validate(); err != nil {
		fmt.Fprintf(os.Stderr, "mdbx-migrate: invalid mdbx options: %v\n", err)
		os.Exit(ExitPreflight)
	}

	if err := run(opts); err != nil {
		if exitErr, ok := err.(ExitError); ok {
			fmt.Fprintln(os.Stderr, exitErr.Err)
			os.Exit(exitErr.Code)
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(ExitMigration)
	}
}
