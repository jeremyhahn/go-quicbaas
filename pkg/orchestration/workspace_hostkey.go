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

package orchestration

import (
	"errors"
	"net/url"
	"strings"

	"golang.org/x/crypto/ssh"
)

// =============================================================================
// The forge's SSH host key — pinned by the operator, never learned
// =============================================================================
//
// A worker container clones over ssh, and ssh will not talk to a host it
// cannot recognise. There are exactly two ways to give it one: TELL IT, or
// let it LEARN whatever answers on the first connection. This file exists so
// the second one is never an option.
//
// `StrictHostKeyChecking=accept-new` would make the burn-in green in one line
// and make every clone a trust-on-first-use: a factory that pushes SIGNED
// commits would be handing its signing agent's socket to whatever machine
// held the forge's address at that moment. So the operator PINS the forge's
// host public key when binding the project (ForgeConfig.SSHHostKey), the
// daemon writes it into a known_hosts the container mounts read-only, and the
// container's ssh runs with StrictHostKeyChecking=yes against that file only.
//
// NOTHING HERE KNOWS ANY HOST. The host pattern is derived from the
// repository URL the project already carries and the key comes from the
// binding; no address and no key is compiled in, defaulted, or baked into the
// worker image.

// sshDefaultPort is the port ssh connects to when a repository URL names
// none. A known_hosts entry for it is the bare hostname, never "[host]:22" —
// ssh writes and looks up the default port unbracketed.
const sshDefaultPort = "22"

// ValidateSSHHostKey checks that line is one OpenSSH public-key line the
// daemon could pin — "<type> <base64> [comment]", as `ssh-keyscan` prints
// after the host field. It is what a binding is validated against, so an
// operator learns a mistyped key when they bind rather than when the first
// job's clone dies.
func ValidateSSHHostKey(line string) error {
	_, err := ParseSSHHostKey(line)
	return err
}

// ParseSSHHostKey parses one pinned OpenSSH public-key line.
//
// It refuses anything that is not exactly one key: a known_hosts line with a
// host pattern in front, a file with several lines, and an empty string are
// all rejected by name, because each is a plausible thing to paste and none
// of them is what this field holds.
func ParseSSHHostKey(line string) (ssh.PublicKey, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil, &SSHHostKeyError{Message: "is empty; the forge's host key is pinned, never learned"}
	}
	if strings.ContainsAny(trimmed, "\r\n") {
		return nil, &SSHHostKeyError{Message: "must be a single line holding a single key"}
	}
	key, _, options, rest, err := ssh.ParseAuthorizedKey([]byte(trimmed))
	if err != nil {
		return nil, &SSHHostKeyError{
			Message: "is not an OpenSSH public-key line (\"<type> <base64> [comment]\"): " + err.Error(),
		}
	}
	if len(options) > 0 {
		// ParseAuthorizedKey reads anything before the key type as an
		// authorized_keys OPTION LIST, which is how a whole known_hosts line
		// — "forge.invalid ssh-ed25519 AAAA…" — slips through as a valid
		// key with a host pattern for an option. The host belongs to the
		// repository URL, never to this field, so say so instead.
		return nil, &SSHHostKeyError{
			Message: "must start with the key type; drop the host pattern in front of \"" + options[0] + "\", " +
				"it is taken from the project's repository URL",
		}
	}
	if len(strings.TrimSpace(string(rest))) != 0 {
		return nil, &SSHHostKeyError{Message: "must be a single line holding a single key"}
	}
	return key, nil
}

// SSHRepoHost returns the known_hosts host pattern a git clone of repoURL
// would connect to, or the empty string when repoURL names no ssh transport.
//
// Both spellings git accepts are recognised: the URL form
// "ssh://git@host:2222/owner/repo.git" and the scp form
// "git@host:owner/repo.git". A non-default port is bracketed, which is the
// form ssh looks entries up under.
func SSHRepoHost(repoURL string) string {
	raw := strings.TrimSpace(repoURL)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		return scpFormHost(raw)
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "ssh" {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	if host == "" {
		return ""
	}
	if port := u.Port(); port != "" && port != sshDefaultPort {
		return "[" + host + "]:" + port
	}
	return host
}

// scpFormHost returns the host of a scp-form repository ("git@host:path"), or
// the empty string when raw is not one. The scp form carries no port, so the
// pattern is always the bare hostname.
func scpFormHost(raw string) string {
	at := strings.Index(raw, "@")
	if at <= 0 {
		return ""
	}
	host, path, found := strings.Cut(raw[at+1:], ":")
	if !found || strings.TrimSpace(path) == "" {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(host))
}

// KnownHostsFor renders the known_hosts content pinning hostKey for the host
// repoURL is cloned from.
//
// It returns empty content and no error when repoURL is not an ssh URL: an
// https checkout authenticates with the forge token and needs no host key,
// and a binding that carries one anyway is not made wrong by it.
//
// THE LINE IS RE-RENDERED FROM THE PARSED KEY, never echoed from the
// operator's text. A pinned value is tenant data that becomes a file ssh
// obeys; rendering it back out of ssh.PublicKey is what guarantees the file
// holds one host, one key and no options, whatever was pasted into the
// binding.
func KnownHostsFor(repoURL, hostKey string) (string, error) {
	host := SSHRepoHost(repoURL)
	if host == "" {
		return "", nil
	}
	key, err := ParseSSHHostKey(hostKey)
	if err != nil {
		return "", err
	}
	// MarshalAuthorizedKey terminates its output with a newline.
	return host + " " + string(ssh.MarshalAuthorizedKey(key)), nil
}

// ErrSSHHostKeyInvalid indicates a pinned SSH host key the daemon will not
// write into a known_hosts.
var ErrSSHHostKeyInvalid = errors.New("forge.ssh_host_key: invalid")

// SSHHostKeyError reports a pinned host key that is missing or unusable. It
// names the binding field the operator has to fix.
type SSHHostKeyError struct {
	// Message says what is wrong with the pinned line.
	Message string
}

// Field names the binding field this error is about. It is a method rather
// than a field because there is exactly one field a host key can come from,
// and an error that could name another would be reporting a different fault.
func (e *SSHHostKeyError) Field() string { return "forge.ssh_host_key" }

// Error implements the error interface.
func (e *SSHHostKeyError) Error() string {
	return ErrSSHHostKeyInvalid.Error() + ": " + e.Message
}

// Unwrap returns the sentinel error for errors.Is support.
func (e *SSHHostKeyError) Unwrap() error { return ErrSSHHostKeyInvalid }
