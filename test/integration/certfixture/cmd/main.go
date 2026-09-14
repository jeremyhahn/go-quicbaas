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

// Command certfixture writes the integration suite's TLS and PIV fixtures,
// issued by QuicBaaS's own certificate authority.
//
// Usage:
//
//	go run ./test/integration/certfixture/cmd [-topology PATH] [DIR]
//
// DIR defaults to test/integration/certs. The topology — the identities and the
// names they are reached by — is a JSON declaration, not a constant in this
// program; see test/integration/certfixture/topology.json.
//
// It replaces test/integration/gen-certs, which built a parallel certificate
// authority out of crypto/ecdsa and crypto/x509 and hardcoded the cluster's
// hostnames and addresses inside the signing function (go-quicbaas#943). This
// program signs nothing: every certificate comes from the CA adapter the daemon
// serves certificate operations through.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/jeremyhahn/go-quicbaas/test/integration/certfixture"
)

// Exit codes. A generation failure and a usage failure are distinguished so a
// harness can tell "the fixtures could not be issued" from "this command was
// invoked wrongly".
const (
	exitOK      = 0
	exitFailure = 1
	exitUsage   = 2
)

// Defaults for an invocation that names neither a directory nor a topology.
const (
	defaultDir      = "test/integration/certs"
	defaultTopology = "test/integration/certfixture/topology.json"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the whole command, with its streams as parameters so a test can read
// what it reports without spawning a process.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("certfixture", flag.ContinueOnError)
	fs.SetOutput(stderr)
	topologyPath := fs.String("topology", defaultTopology, "path to the topology declaration")
	force := fs.Bool("force", false, "reissue even when a complete fixture set is already present")
	if err := fs.Parse(args); err != nil {
		return exitUsage
	}

	dir := defaultDir
	switch fs.NArg() {
	case 0:
	case 1:
		dir = fs.Arg(0)
	default:
		fmt.Fprintln(stderr, "certfixture: expected at most one output directory")
		return exitUsage
	}

	topo, err := certfixture.LoadTopology(*topologyPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	ctx := context.Background()
	if !*force {
		if err := certfixture.Ensure(ctx, dir, topo); err != nil {
			fmt.Fprintln(stderr, err)
			return exitFailure
		}
	} else if err := certfixture.Write(ctx, dir, topo); err != nil {
		fmt.Fprintln(stderr, err)
		return exitFailure
	}

	// forbidigo forbids fmt.Print*; this is a CLI whose report is its product,
	// so it writes to the stream it was handed.
	fmt.Fprintf(stdout, "certfixture: %d node(s), %d client(s) in %s\n", len(topo.Nodes), len(topo.Clients), dir)
	for _, f := range topo.Files(dir) {
		fmt.Fprintf(stdout, "  %s\n", f)
	}
	return exitOK
}
