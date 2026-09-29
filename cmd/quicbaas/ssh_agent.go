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

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"
	"time"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
	"github.com/spf13/cobra"
	gossh "golang.org/x/crypto/ssh"

	sshSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
)

// sshAgentStopTimeout bounds how long a stopping agent waits for in-flight
// signatures before abandoning them.
const sshAgentStopTimeout = 10 * time.Second

// SSHAgentError reports a failure serving the KMS-backed ssh-agent.
type SSHAgentError struct {
	Op  string
	Err error
}

// Error implements error.
func (e *SSHAgentError) Error() string {
	return "quicbaas/ssh: agent: " + e.Op + ": " + e.Err.Error()
}

// Unwrap returns the underlying failure.
func (e *SSHAgentError) Unwrap() error { return e.Err }

// newSSHAgentCmd builds `quicbaas ssh agent`: a local ssh-agent socket whose
// key lives in the KMS.
//
// THE PRIVATE KEY NEVER LEAVES THE SERVER. The agent holds only the public
// half; every signature request ships its digest to quicbaasd, which signs it
// with the stored key. Git uses the socket for both commit signing
// (gpg.format=ssh) and pushing over SSH, so an operator's identity can live in
// the KMS like every other QuicBaaS key.
func newSSHAgentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agent",
		Short: "Serve an ssh-agent socket for a KMS-held key",
		Long: `Serve an ssh-agent socket whose key lives in the QuicBaaS KMS.

The agent holds only the public key; each signature is made by quicbaasd,
so the private key never leaves the server. The socket path is printed as
SSH_AUTH_SOCK=<path> (and written to --env-file when given) once the agent
is listening. The agent runs until interrupted.

  eval "$(quicbaas ssh agent --scope service --owner agent-go-codec --name agent-go-codec-signing &)"
  git -c gpg.format=ssh -c user.signingkey="key::$(quicbaas ssh keys export ...)" commit -S`,
		RunE: runSSHAgent,
	}
	addKeysSelectorFlags(cmd)
	cmd.Flags().String("socket-dir", "", "Directory to create the socket in (must not exist; default: a private temporary directory)")
	cmd.Flags().String("env-file", "", "Also write SSH_AUTH_SOCK=<path> to this file (mode 0600) once listening")
	return cmd
}

func runSSHAgent(cmd *cobra.Command, _ []string) error {
	scope, owner, name, err := readKeySelector(cmd)
	if err != nil {
		return err
	}
	socketDir, _ := cmd.Flags().GetString("socket-dir")
	envFile, _ := cmd.Flags().GetString("env-file")

	client, closer, err := sshClientWithCloser()
	if err != nil {
		return err
	}
	defer closer()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	signer, err := client.Keys().Signer(ctx, sshSDK.GetKeyRequest{Scope: scope, Owner: owner, Name: name})
	if err != nil {
		return &SSHAgentError{Op: "resolve key " + scope + "/" + owner + "/" + name, Err: err}
	}
	return serveSSHAgent(ctx, signer, socketDir, envFile, cmd.OutOrStdout())
}

// serveSSHAgent serves signer on an ssh-agent socket until ctx ends or the
// agent stops on its own. It announces the socket on out, and in envFile when
// one is named, only after the agent is listening.
func serveSSHAgent(ctx context.Context, signer gossh.Signer, socketDir, envFile string, out io.Writer) error {
	session, err := xkmsssh.StartAgent(ctx, xkmsssh.AgentOptions{
		Signers:   []gossh.Signer{signer},
		SocketDir: socketDir,
	})
	if err != nil {
		return &SSHAgentError{Op: "start", Err: err}
	}
	defer stopSSHAgent(session)

	line := "SSH_AUTH_SOCK=" + session.SockPath + "\n"
	if envFile != "" {
		if err := os.WriteFile(envFile, []byte(line), 0o600); err != nil {
			return &SSHAgentError{Op: "write env file", Err: err}
		}
	}
	if _, err := fmt.Fprint(out, line); err != nil {
		return &SSHAgentError{Op: "announce socket", Err: err}
	}

	select {
	case <-ctx.Done():
		return nil
	case <-session.Done():
		if err := session.Err(); err != nil {
			return &SSHAgentError{Op: "serve", Err: err}
		}
		return nil
	}
}

// stopSSHAgent stops the session within sshAgentStopTimeout, removing its
// socket.
func stopSSHAgent(session *xkmsssh.AgentSession) {
	ctx, cancel := context.WithTimeout(context.Background(), sshAgentStopTimeout)
	defer cancel()
	session.Stop(ctx)
}
