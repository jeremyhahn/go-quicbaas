// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package main

import (
	"os"
	"strings"

	sshSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
)

// CLISSHClientBuildError wraps a failure to obtain the SSH SDK client for
// CLI use. The Op field names the stage that failed so operators see a
// self-describing error message.
type CLISSHClientBuildError struct {
	Op  string
	Err error
}

// Error implements the error interface.
func (e *CLISSHClientBuildError) Error() string {
	return "quicbaas/ssh: cli client: " + e.Op + ": " + e.Err.Error()
}

// Unwrap returns the underlying construction failure.
func (e *CLISSHClientBuildError) Unwrap() error { return e.Err }

// buildCLISSHClient returns the QuicBaaS SDK SSH client.
//
// Transport selection is the SDK's job: the global --server / --transport /
// --tls-* flags pick embedded, REST, gRPC, QUIC, MCP, or Unix, exactly as
// they do for every other `quicbaas` subcommand. The CLI no longer builds a
// go-xkms client of its own — SSH reaches the same quicbaasd surface as
// every other verb, under the same authentication and authorization.
//
// The helper is shared by the `quicbaas ssh` subcommands, the agent deploy
// path, and the desktop bindings so all three observe one client.
func buildCLISSHClient() (sshSDK.Client, error) {
	return sshClientFactory()
}

// createSDKSSHClient resolves the SSH sub-service from the SDK client. It is
// the default sshClientFactory; tests substitute a fake.
func createSDKSSHClient() (sshSDK.Client, error) {
	sdk, err := sdkClientFactory()
	if err != nil {
		return nil, &CLISSHClientBuildError{Op: "sdk_client", Err: err}
	}
	return sdk.Security().SSH(), nil
}

// cliSSHOwner returns the operator identifier used as the "owner"
// component of SSH keys / pins / authorized entries. Prefers $USER,
// falls back to $LOGNAME, then "default".
func cliSSHOwner() string {
	if v := strings.TrimSpace(os.Getenv("USER")); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("LOGNAME")); v != "" {
		return v
	}
	return "default"
}
