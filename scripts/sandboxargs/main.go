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

// Command sandboxargs prints the `docker run` flags that bound a sandboxed
// agent container — --pids-limit, --cap-add and --cap-drop — exactly as the
// spawn renders them from the default orchestration.SandboxSecurity policy.
//
// IT EXISTS SO `make test-worker-image` PROBES THE BOUNDARY PRODUCTION USES.
// Writing the flags into the Makefile by hand would give the gate its own copy
// of the policy, and a copy drifts: the probe would then certify a capability
// set no worker runs with. The flags come from the same two calls the spawn
// makes (SandboxSecurity.Apply, ContainerSpec.PrivilegeRunArgs).
//
// -pids-limit overrides only the process bound, so the fork probe can prove the
// bound holds without launching thousands of processes; the capability flags
// are always the production ones.
//
// EXIT: 0 flags printed · 2 bad invocation or a policy that cannot be applied.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run parses args, renders the policy's run flags onto stdout as one
// space-separated line, and returns the exit code.
func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("sandboxargs", flag.ContinueOnError)
	fs.SetOutput(stderr)
	pidsLimit := fs.Int64("pids-limit", 0, "override the process bound; 0 keeps the production default")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "sandboxargs: unexpected arguments %q\n", fs.Args())
		return 2
	}

	flags, err := runFlags(orchestration.SandboxSecurity{PidsLimit: *pidsLimit})
	if err != nil {
		fmt.Fprintf(stderr, "sandboxargs: %v\n", err)
		return 2
	}
	fmt.Fprintln(stdout, strings.Join(flags, " "))
	return 0
}

// runFlags applies policy to a spec and renders the flags the runtime would
// launch that spec with.
func runFlags(policy orchestration.SandboxSecurity) ([]string, error) {
	spec := &orchestration.ContainerSpec{}
	if err := policy.Apply(spec); err != nil {
		return nil, err
	}
	return spec.PrivilegeRunArgs(), nil
}
