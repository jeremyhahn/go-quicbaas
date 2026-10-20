// Command depscheck is `make deps-check`: it fails when a direct requirement
// of a go.mod, first-party or third-party, is behind its latest release and
// the go.mod gives no reason for holding it (handbook devops.md §9,
// ci-standard.md §1).
//
// Usage:
//
//	go list -C <module dir> -m -u -json all | go run ./scripts/depscheck <module dir>/go.mod
package main

import (
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go list -m -u -json all | depscheck <go.mod>")
		os.Exit(2)
	}
	modPath := os.Args[1]
	data, err := os.ReadFile(modPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "depscheck:", err)
		os.Exit(1)
	}
	if err := Check(modPath, data, os.Stdin); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "depscheck: %s: every direct requirement is at its latest release or documented\n", modPath)
}
