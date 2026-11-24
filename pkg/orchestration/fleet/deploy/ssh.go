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

package deploy

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
)

// defaultSSHPort is appended to Host addresses that omit an explicit
// port. Port 22 matches the OpenSSH convention.
const defaultSSHPort = "22"

// defaultDialTimeout bounds each dial attempt so an unreachable host
// fails quickly rather than wedging the GUI or CLI.
const defaultDialTimeout = 15 * time.Second

// SSHClient owns a live SSH connection and exposes the small set of
// operations the deploy primitives need: exec, and access to the
// underlying *ssh.Client so sftp.go can negotiate an SFTP subsystem.
//
// An SSHClient must be Closed exactly once; double-close is a no-op.
type SSHClient struct {
	client *ssh.Client
	host   string
}

// DialSSH opens an SSH connection using the credentials in the Spec.
// Password auth is the only spec-embedded credential supported; stored
// keys flow through DialSSHWithAuth / DialSSHWithAuthAndHostKey with
// a signer resolved by the caller.
//
// Host-key verification requires InsecureHostKey=true (explicit
// operator opt-out) or one of the entry points that accepts a
// caller-supplied HostKeyPolicy — DialSSHWithHostKey for these same
// credentials, DialSSHWithAuthAndHostKey for injected auth. No policy +
// InsecureHostKey=false yields an InvalidSpecError at dial
// time so a misconfigured deploy fails loudly.
func DialSSH(ctx context.Context, spec *Spec) (*SSHClient, error) {
	return DialSSHWithHostKey(ctx, spec, nil)
}

// Host returns the dialed host:port string.
func (c *SSHClient) Host() string {
	if c == nil {
		return ""
	}
	return c.host
}

// User returns the login user the connection authenticated as. The
// linger check needs it: `systemctl --user` and `loginctl enable-linger`
// both act on this account, so reading it from the live connection keeps
// the check honest even if a caller's spec says something else.
func (c *SSHClient) User() string {
	if c == nil || c.client == nil {
		return ""
	}
	return c.client.User()
}

// UnderlyingClient exposes the *ssh.Client so adjacent primitives
// (sftp.go) can wrap it. Callers must not Close the returned client;
// the SSHClient owns its lifecycle.
func (c *SSHClient) UnderlyingClient() *ssh.Client {
	if c == nil {
		return nil
	}
	return c.client
}

// Close tears down the SSH connection. Calling Close on a nil or
// already-closed SSHClient is safe.
func (c *SSHClient) Close() error {
	if c == nil || c.client == nil {
		return nil
	}
	err := c.client.Close()
	c.client = nil
	return err
}

// DialTCP opens a TCP connection FROM THE REMOTE HOST to addr, tunnelled
// through this SSH connection (an RFC 4254 direct-tcpip channel). Name
// resolution and routing happen on the remote host, so a successful dial
// proves the remote host can reach addr — which is what preflight needs to
// know about the control plane, and not something a dial from the operator's
// machine could show.
func (c *SSHClient) DialTCP(ctx context.Context, addr string) (net.Conn, error) {
	if c == nil || c.client == nil {
		return nil, &SSHDialError{Host: addr, Cause: errClosed}
	}
	return c.client.DialContext(ctx, "tcp", addr)
}

// syncBuffer is a byte buffer that may be READ WHILE IT IS STILL BEING
// WRITTEN, which is exactly what capturing a cancelled command's output
// requires.
//
// ssh.Session copies the remote's stdout and stderr into these on goroutines
// of its own. Those goroutines are started by Run and are not finished until
// Run returns — so Exec's cancel path, which deliberately returns WITHOUT
// waiting for Run, reads the buffers while the copiers are still appending to
// them. With a plain bytes.Buffer that is a data race on every field of the
// buffer at once, and it is a race whatever the captured bytes happen to look
// like afterwards.
//
// LOCKING RATHER THAN WAITING, because the alternative spends the one thing
// the cancel path exists to provide. Awaiting Run before reading would also
// remove the race — Wait does not return until the copiers have — but it would
// make a caller who has already given up wait on a remote command that may
// never end, which is the hang the cancel branch was written to avoid. A
// bounded wait only trades that for a new timeout to tune and a capture
// discarded when it expires. A lock costs nothing on either path and makes the
// PARTIAL capture — the honest answer for a command that was cancelled — safe
// to read at any moment.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// String returns what has been captured so far.
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// Exec runs cmd on the remote host and returns the captured stdout and
// stderr. A non-zero exit is surfaced as a RemoteExecError.
//
// On cancellation the returned output is whatever had been captured by then,
// which may be partial — see syncBuffer for why that is read under a lock
// rather than after waiting for the command to finish.
func (c *SSHClient) Exec(ctx context.Context, cmd string) (string, string, error) {
	if c == nil || c.client == nil {
		return "", "", &RemoteExecError{Command: cmd, Cause: &SSHDialError{Cause: errClosed}}
	}
	session, err := c.client.NewSession()
	if err != nil {
		return "", "", &RemoteExecError{Command: cmd, Cause: err}
	}
	defer session.Close()

	var stdout, stderr syncBuffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	done := make(chan error, 1)
	go func() { done <- session.Run(cmd) }()

	select {
	case <-ctx.Done():
		// Best effort abort; ssh.Session has no context hook.
		_ = session.Signal(ssh.SIGTERM)
		return stdout.String(), stderr.String(), &RemoteExecError{Command: cmd, Cause: ctx.Err()}
	case err := <-done:
		if err != nil {
			return stdout.String(), stderr.String(), &RemoteExecError{
				Command: cmd,
				Stderr:  strings.TrimSpace(stderr.String()),
				Cause:   err,
			}
		}
		return stdout.String(), stderr.String(), nil
	}
}

// WriteFile writes data to the remote path using cat > path; for
// small text payloads (units, scripts) this is simpler than opening a
// second SFTP session. The file mode is encoded with an explicit
// chmod after the write.
func (c *SSHClient) WriteFile(ctx context.Context, remotePath string, data []byte, mode os.FileMode) error {
	if c == nil || c.client == nil {
		return &RemoteExecError{Command: "write " + remotePath, Cause: errClosed}
	}

	// mkdir -p the parent directory so writes into fresh install/data
	// dirs succeed. shellQuotePath preserves $HOME expansion on the
	// remote shell; directories like "$HOME/.quicbaas/agent" are
	// common when the operator hasn't pre-provisioned paths.
	if dir := parentDir(remotePath); dir != "" {
		mkdirCmd := "mkdir -p " + shellQuotePath(dir)
		if _, _, mkdirErr := c.Exec(ctx, mkdirCmd); mkdirErr != nil {
			return &RemoteExecError{Command: mkdirCmd, Cause: mkdirErr}
		}
	}

	session, err := c.client.NewSession()
	if err != nil {
		return &RemoteExecError{Command: "write " + remotePath, Cause: err}
	}
	defer session.Close()

	stdin, err := session.StdinPipe()
	if err != nil {
		return &RemoteExecError{Command: "write " + remotePath, Cause: err}
	}

	var stderr bytes.Buffer
	session.Stderr = &stderr

	if err := session.Start("cat > " + shellQuotePath(remotePath)); err != nil {
		return &RemoteExecError{Command: "write " + remotePath, Cause: err}
	}

	_, copyErr := io.Copy(stdin, bytes.NewReader(data))
	closeErr := stdin.Close()

	done := make(chan error, 1)
	go func() { done <- session.Wait() }()

	select {
	case <-ctx.Done():
		_ = session.Signal(ssh.SIGTERM)
		return &RemoteExecError{Command: "write " + remotePath, Cause: ctx.Err()}
	case waitErr := <-done:
		if copyErr != nil {
			return &RemoteExecError{Command: "write " + remotePath, Cause: copyErr}
		}
		if closeErr != nil {
			return &RemoteExecError{Command: "write " + remotePath, Cause: closeErr}
		}
		if waitErr != nil {
			return &RemoteExecError{
				Command: "write " + remotePath,
				Stderr:  strings.TrimSpace(stderr.String()),
				Cause:   waitErr,
			}
		}
	}

	// chmod to the requested mode.
	if _, _, err := c.Exec(ctx, "chmod "+modeOctal(mode)+" "+shellQuotePath(remotePath)); err != nil {
		return err
	}
	return nil
}

// errClosed is returned when an operation is attempted on a closed SSH
// connection. It is unexported on purpose — callers should check for
// RemoteExecError or SSHDialError, not this sentinel.
var errClosed = &stringError{msg: "ssh client is closed"}

// stringError is a tiny internal helper so we can return errors without
// pulling in fmt.Errorf. It satisfies error.
type stringError struct{ msg string }

func (e *stringError) Error() string { return e.msg }

// buildAuthMethods selects an ssh.AuthMethod from the Spec. The
// only spec-embedded credential supported here is SSHPassword; stored
// SSH keys are resolved through the xkms-backed SDK client and
// injected via DialSSHWithAuth by the service layer so the private
// material never leaves the key store.
func buildAuthMethods(spec *Spec) ([]ssh.AuthMethod, error) {
	if spec.SSHPassword != "" {
		return []ssh.AuthMethod{ssh.Password(spec.SSHPassword)}, nil
	}
	return nil, &InvalidSpecError{
		Field:  "ssh_password",
		Reason: "ssh_password must be set or the caller must inject a signer via DialSSHWithAuth",
	}
}

// buildHostKeyCallback wires the Spec's host key policy into an
// ssh.HostKeyCallback. The only spec-embedded policy supported here
// is InsecureHostKey; pinned host-key verification flows through the
// xkms SSH known-hosts store, which the service layer injects into
// DialSSHWithAuthAndHostKey as an explicit HostKeyPolicy.
func buildHostKeyCallback(spec *Spec) (ssh.HostKeyCallback, error) {
	if spec.InsecureHostKey {
		// Deliberately trust-on-first-use. Documented on the Spec.
		return ssh.InsecureIgnoreHostKey(), nil // #nosec G106 -- host-key checking is opt-out per Spec.InsecureHostKey; the default path refuses to dial without a pinned callback.
	}
	return nil, &InvalidSpecError{
		Field:  "ssh_host_key",
		Reason: "host-key verification is required; pass a HostKeyPolicy to DialSSHWithAuthAndHostKey or set InsecureHostKey",
	}
}

// normalizeHostPort returns host with a default port appended when the
// caller didn't supply one.
func normalizeHostPort(host string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return net.JoinHostPort(host, defaultSSHPort)
}

// shellQuote wraps s in single quotes safe for sh -c. Embedded single
// quotes are emitted as '\”. This keeps commands auditable since we
// never pass user data through /bin/sh expansions.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// shellQuotePath quotes a filesystem path for use in an sh command,
// while still allowing the shell to expand a leading "$HOME" (or
// other "$VAR/" prefix). Paths like "$HOME/.quicbaas/agent/agent.crt"
// become `"$HOME"'/.quicbaas/agent/agent.crt'` so the remote shell
// substitutes the home dir but the remainder is literal.
func shellQuotePath(p string) string {
	if strings.HasPrefix(p, "$HOME/") {
		return `"$HOME"` + shellQuote(strings.TrimPrefix(p, "$HOME"))
	}
	if p == "$HOME" {
		return `"$HOME"`
	}
	return shellQuote(p)
}

// parentDir returns the directory portion of remotePath. Paths that
// don't contain a separator yield "".
func parentDir(p string) string {
	idx := strings.LastIndex(p, "/")
	if idx <= 0 {
		return ""
	}
	return p[:idx]
}

// modeOctal formats a os.FileMode as the three-digit octal string
// accepted by the remote `chmod` command.
func modeOctal(m os.FileMode) string {
	m &= os.ModePerm
	// Build three digits explicitly to avoid pulling in fmt here.
	digits := []byte{'0', '0', '0'}
	digits[0] = '0' + byte((m>>6)&7)
	digits[1] = '0' + byte((m>>3)&7)
	digits[2] = '0' + byte(m&7)
	return string(digits)
}
