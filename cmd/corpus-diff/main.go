// Command corpus-diff checks Starknet JSON-RPC responses against recorded
// responses or against another node, with order-aware comparison rules.
//
//	corpus-diff validate <corpus>...
//	corpus-diff record  --corpus <file|dir> --node <url> --out <file|dir>
//	corpus-diff compare --corpus <file|dir> (--records <file|dir> | --node <url>)...
//
// Exit codes: 0 all pass, 1 a check failed, 2 usage or setup error.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"runtime"
)

const (
	exitOK    = 0
	exitFail  = 1
	exitSetup = 2
)

// errFailed means the run completed and found one or more failures.
var errFailed = errors.New("one or more checks failed")

const usage = `usage:
  corpus-diff validate <corpus file|dir>...
  corpus-diff record  --corpus <file|dir> --node <url> --out <file|dir> [flags]
  corpus-diff compare --corpus <file|dir> (--records <file|dir> | --node <url>)... [flags]

Run "corpus-diff <command> -h" for the flags of a command.
`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, os.Args[1:], os.Stdout, os.Stderr)
	stop()
	os.Exit(code)
}

func run(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitSetup
	}
	var err error
	switch args[0] {
	case "validate":
		err = runValidate(args[1:], stdout, stderr)
	case "record":
		err = runRecord(ctx, args[1:], stdout, stderr)
	case "compare":
		err = runCompare(ctx, args[1:], stdout, stderr)
	case "-h", "--help", "help":
		fmt.Fprint(stdout, usage)
		return exitOK
	default:
		fmt.Fprintf(stderr, "unknown command %q\n%s", args[0], usage)
		return exitSetup
	}
	switch {
	case err == nil:
		return exitOK
	case errors.Is(err, errFailed):
		fmt.Fprintln(stderr, err)
		return exitFail
	case errors.Is(err, flag.ErrHelp):
		return exitOK
	default:
		fmt.Fprintln(stderr, "error:", err)
		return exitSetup
	}
}

func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

func defaultConcurrency() int {
	// The work waits on the network, so use more workers than CPUs.
	return runtime.GOMAXPROCS(0) * 4
}
