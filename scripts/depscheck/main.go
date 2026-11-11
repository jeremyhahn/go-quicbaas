// Command depscheck is `make deps-check`: it fails when a direct requirement
// of a go.mod, first-party or third-party, is behind its latest release and
// the go.mod gives no reason for holding it (handbook devops.md §9,
// ci-standard.md §1). A requirement behind a release younger than 7 days is
// printed as reported and does not fail; the listing must carry Update.Time
// for that, and an update with no time fails.
//
// Usage:
//
//	go list -C <module dir> -m -u -json all | go run ./scripts/depscheck <module dir>/go.mod
package main

import (
	"fmt"
	"io"
	"os"
	"time"
)

func main() {
	os.Exit(run(os.Args, os.Stdin, os.Stdout, os.Stderr, time.Now()))
}

// run is main with its inputs, outputs and current time injected; it returns
// the exit code: 0 when nothing fails (reported entries alone do not), 1 when
// the check fails, 2 on bad usage.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer, now time.Time) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: go list -m -u -json all | depscheck <go.mod>")
		return 2
	}
	modPath := args[1]
	data, err := os.ReadFile(modPath)
	if err != nil {
		fmt.Fprintln(stderr, "depscheck:", err)
		return 1
	}
	reported, err := Check(modPath, data, stdin, now)
	for _, b := range reported {
		fmt.Fprintln(stdout, b.ReportedLine(now))
	}
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	fmt.Fprintf(stdout, "depscheck: %s: every direct requirement is at its latest release, documented, or reported\n", modPath)
	return 0
}
