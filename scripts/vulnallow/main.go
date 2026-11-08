// Command vulnallow is the allowlist step of `make vuln`: it reads
// govulncheck's -format json stream and fails on a reachable advisory unless
// the allowlist names its ID with a reason. An allowed advisory is printed
// with that reason, and an entry that matches no reachable finding fails as
// stale, so it is removed once a fixed release exists (go-quicbaas#2211).
//
// Usage:
//
//	govulncheck -format json ./... | go run ./scripts/vulnallow scripts/vuln-allowlist.txt
package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
)

func main() {
	os.Exit(run(os.Args, os.Stdin, os.Stdout, os.Stderr))
}

// run is main with its inputs and outputs injected; it returns the exit code:
// 0 when no reachable advisory is unlisted and no entry is stale, 1 when one
// is, or on a read or parse error, 2 on bad usage.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) != 2 {
		fmt.Fprintln(stderr, "usage: govulncheck -format json ./... | vulnallow <allowlist>")
		return 2
	}
	listPath := args[1]
	data, err := os.ReadFile(listPath)
	if err != nil {
		fmt.Fprintln(stderr, "vulnallow:", err)
		return 1
	}
	entries, err := ParseAllowlist(listPath, bytes.NewReader(data))
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	result, err := Check(entries, stdin)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	for _, a := range result.Allowed {
		fmt.Fprintf(stdout, "vulnallow: %s is allowed: %s\n", a.ID, a.Reason)
	}
	for _, f := range result.Unlisted {
		fmt.Fprintf(stdout, "vulnallow: %s is reachable and not on %s\n", f.ID, listPath)
		fmt.Fprintf(stdout, "  summary:  %s\n", f.Summary)
		fmt.Fprintf(stdout, "  found in: %s@%s\n", f.Module, f.Version)
		fmt.Fprintf(stdout, "  fixed in: %s\n", f.FixedVersion)
		fmt.Fprintf(stdout, "  trace:    %s\n", f.Position)
	}
	for _, e := range result.Stale {
		fmt.Fprintf(stdout, "vulnallow: %s on %s:%d matches no reachable finding; remove it\n", e.ID, e.File, e.Line)
	}
	if result.Failed() {
		return 1
	}
	return 0
}
