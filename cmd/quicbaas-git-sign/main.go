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

// Command quicbaas-git-sign is git's gpg.ssh.program in a quicbaas worker. It
// signs commits with the session's key held in quicbaas, over the session's own
// MCP connection, and never touches private key material. See "Signing through
// quicbaas-git-sign" in docs/sdlc/architecture.md.
package main

import (
	"context"
	"io"
	"os"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration/sdlc/gitsign"
)

func main() {
	os.Exit(run(context.Background(), os.Args[1:], os.Getenv, os.Stdin, os.Stdout, os.Stderr))
}

// run is the command without the process around it: it returns the exit
// status and reports a failure on stderr, where git shows it to the user.
func run(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	err := signWith(ctx, args, getenv, stdin, stdout)
	if err == nil {
		return 0
	}
	if _, writeErr := io.WriteString(stderr, err.Error()+"\n"); writeErr != nil {
		return 2
	}
	return 1
}

// signWith reads the configuration and runs the helper. The invocation is
// checked before the configuration is, so git's own mistakes are reported as
// such even outside a signing session.
func signWith(ctx context.Context, args []string, getenv func(string) string, stdin io.Reader, stdout io.Writer) error {
	if _, err := gitsign.ParseInvocation(args); err != nil {
		return err
	}
	cfg, err := gitsign.ConfigFromEnv(getenv)
	if err != nil {
		return err
	}
	helper := &gitsign.Helper{Stdin: stdin, Stdout: stdout, Open: mcpSigner(cfg)}
	return helper.Run(ctx, args)
}
