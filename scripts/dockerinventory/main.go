// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
// 1. GNU Affero General Public License v3.0 (AGPL-3.0)
//    See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
// 2. Commercial License
//    Contact licensing@automatethethings.com for commercial licensing options.

// Command dockerinventory prints every Docker image identity this repository
// creates under a name it writes literally, one per line, sorted.
//
// It is the reporting front end for scripts/internal/dockerinv, which is where
// the derivation and the reasoning behind it live. `make verify-clean-docker`
// uses the same package rather than this command's output, so the two cannot
// disagree about what the repository builds.
//
// EXIT: 0 an inventory was derived · 1 it could not be · 2 usage error.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jeremyhahn/go-quicbaas/scripts/internal/dockerinv"
	"github.com/jeremyhahn/go-quicbaas/scripts/internal/gate"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run derives the inventory for the requested repository root and prints it.
// Streams are parameters so a test can read the report this command exists to
// produce without spawning a process.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dockerinventory", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repository root to derive the inventory from")
	if err := fs.Parse(args); err != nil {
		return gate.ExitToolError
	}

	ids, err := dockerinv.Derive(*root)
	if err != nil {
		// A partial inventory is worse than none: a producer that was dropped
		// reads downstream as an image nothing creates. Nothing is printed.
		fmt.Fprintln(stderr, "dockerinventory:", err)
		return gate.ExitFindings
	}
	for _, id := range ids {
		fmt.Fprintln(stdout, id)
	}
	return gate.ExitClean
}
