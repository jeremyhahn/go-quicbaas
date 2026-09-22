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

package service

import (
	"context"
	"net"
	"strings"

	"golang.org/x/crypto/ssh"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
)

// SSHKnownHostsLister enumerates the host-key pins stored under a
// (scope, owner) pair. It is the only operation the host-key resolver
// needs from the SSH known-hosts store, and the SDK's KnownHostsClient
// — the same sub-client `quicbaas ssh known-hosts add/list` drives —
// satisfies it as-is.
type SSHKnownHostsLister interface {
	List(ctx context.Context, req xkmsssh.ListKnownHostsRequest) ([]*xkmsssh.Pin, error)
}

// XKMSSSHHostKeyResolver verifies a deploy target's SSH host key against
// the pins held in the xkms-backed known-hosts store.
//
// It is the secure counterpart to Spec.InsecureHostKey: operators pin a
// host once with `quicbaas ssh known-hosts add`, and every subsequent
// deploy accepts only a key whose algorithm and SHA256 fingerprint match
// a pin. Several pins may exist for one host — that is how a host key is
// rotated without a deploy window — and any of them is sufficient.
type XKMSSSHHostKeyResolver struct {
	// KnownHosts is the known-hosts sub-client of the SDK SSH client.
	// Required.
	KnownHosts SSHKnownHostsLister

	// Scope selects the SSH asset scope. Defaults to "user" when empty.
	Scope string

	// Owner is the scope-specific owner identifier. Required — the
	// resolver refuses to guess an owner on a deploy operator's behalf,
	// because guessing wrong reads an empty pin set and would look
	// exactly like an unpinned host.
	Owner string
}

// HostKeyCallback implements SSHHostKeyResolver.
//
// The pin lookup happens here, before the dial: an unpinned host is an
// operator mistake worth reporting immediately, with the command that
// fixes it, rather than mid-handshake. The returned callback performs no
// I/O — it compares the presented key against the pins already read.
func (r *XKMSSSHHostKeyResolver) HostKeyCallback(ctx context.Context, ref SSHHostRef) (ssh.HostKeyCallback, error) {
	if r.KnownHosts == nil {
		return nil, &XKMSSSHHostKeyNilStoreError{}
	}
	host := stripSSHPort(ref.Host)
	if host == "" {
		return nil, &XKMSSSHHostKeyMissingHostError{}
	}
	scope := firstNonEmpty(ref.Scope, r.Scope, "user")
	owner := firstNonEmpty(ref.Owner, r.Owner)
	if owner == "" {
		return nil, &XKMSSSHHostKeyMissingOwnerError{}
	}

	pins, err := r.KnownHosts.List(ctx, xkmsssh.ListKnownHostsRequest{Scope: scope, Owner: owner})
	if err != nil {
		return nil, err
	}

	matched := pinsForHost(pins, host)
	if len(matched) == 0 {
		return nil, &SSHHostKeyNotPinnedError{Host: host, Scope: scope, Owner: owner}
	}

	return func(_ string, _ net.Addr, key ssh.PublicKey) error {
		return verifyPinnedHostKey(host, matched, key)
	}, nil
}

// verifyPinnedHostKey reports whether the presented key matches one of
// the host's pins. A pin matches when the SHA256 fingerprints are equal
// and, when the pin records an algorithm, the algorithms agree too — a
// pin added without --algorithm constrains the fingerprint only.
func verifyPinnedHostKey(host string, pins []*xkmsssh.Pin, key ssh.PublicKey) error {
	presented := ssh.FingerprintSHA256(key)
	algorithm := key.Type()

	expected := make([]string, 0, len(pins))
	for _, pin := range pins {
		expected = append(expected, pin.Fingerprint)
		if pin.Fingerprint != presented {
			continue
		}
		if pin.Algorithm != "" && !strings.EqualFold(pin.Algorithm, algorithm) {
			continue
		}
		return nil
	}
	return &SSHHostKeyMismatchError{
		Host:      host,
		Algorithm: algorithm,
		Presented: presented,
		Pinned:    expected,
	}
}

// pinsForHost returns every pin recorded for host. Host comparison is
// case-insensitive and port-insensitive so "Omega" and "omega:22" both
// resolve the pin stored as "omega".
func pinsForHost(pins []*xkmsssh.Pin, host string) []*xkmsssh.Pin {
	matched := make([]*xkmsssh.Pin, 0, 2)
	for _, pin := range pins {
		if pin == nil {
			continue
		}
		if strings.EqualFold(stripSSHPort(pin.Host), host) {
			matched = append(matched, pin)
		}
	}
	return matched
}

// stripSSHPort trims a ":port" suffix and surrounding whitespace, leaving
// the bare host. Values without a port are returned unchanged.
func stripSSHPort(host string) string {
	host = strings.TrimSpace(host)
	if h, _, err := net.SplitHostPort(host); err == nil {
		return h
	}
	return host
}

// firstNonEmpty returns the first value that is non-empty after trimming.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

// =============================================================================
// Typed errors
// =============================================================================

// SSHHostKeyNotPinnedError reports that the deploy target has no host-key
// pin in the known-hosts store. Its message carries the exact command
// that fixes it, because the operator reading this error is mid-deploy.
type SSHHostKeyNotPinnedError struct {
	// Host is the SSH target with no pin.
	Host string
	// Scope and Owner name the known-hosts store that was searched.
	Scope string
	Owner string
}

// Error implements the error interface.
func (e *SSHHostKeyNotPinnedError) Error() string {
	return "service: no ssh host-key pin for " + e.Host +
		" in scope=" + e.Scope + " owner=" + e.Owner +
		"; pin it first: quicbaas ssh known-hosts add --host " + e.Host +
		" --algorithm <a> --fingerprint <f>" +
		" (or pass --ssh-insecure-host-key to deploy without verification)"
}

// SSHHostKeyMismatchError reports that the host presented a key that no
// pin matches. It is distinct from SSHHostKeyNotPinnedError because the
// two demand opposite responses: pin the host, versus stop and find out
// why the host's key changed.
type SSHHostKeyMismatchError struct {
	// Host is the SSH target whose key failed verification.
	Host string
	// Algorithm is the presented key's algorithm (e.g. ssh-ed25519).
	Algorithm string
	// Presented is the SHA256 fingerprint the host offered.
	Presented string
	// Pinned lists every fingerprint pinned for the host.
	Pinned []string
}

// Error implements the error interface.
func (e *SSHHostKeyMismatchError) Error() string {
	return "service: ssh host-key mismatch for " + e.Host +
		": presented " + e.Algorithm + " " + e.Presented +
		", pinned " + strings.Join(e.Pinned, ", ")
}

// XKMSSSHHostKeyNilStoreError is returned when the resolver was built
// without a known-hosts client. Caught eagerly so a misconfigured deploy
// fails with a clear message rather than a nil-pointer panic.
type XKMSSSHHostKeyNilStoreError struct{}

// Error implements the error interface.
func (e *XKMSSSHHostKeyNilStoreError) Error() string {
	return "service: xkms ssh host-key resolver: known-hosts client is required"
}

// XKMSSSHHostKeyMissingOwnerError is returned when neither the ref nor
// the resolver supplies an owner.
type XKMSSSHHostKeyMissingOwnerError struct{}

// Error implements the error interface.
func (e *XKMSSSHHostKeyMissingOwnerError) Error() string {
	return "service: xkms ssh host-key resolver: owner is required"
}

// XKMSSSHHostKeyMissingHostError is returned when the ref carries no
// host to look up.
type XKMSSSHHostKeyMissingHostError struct{}

// Error implements the error interface.
func (e *XKMSSSHHostKeyMissingHostError) Error() string {
	return "service: xkms ssh host-key resolver: host is required"
}

// compile-time contract.
var _ SSHHostKeyResolver = (*XKMSSSHHostKeyResolver)(nil)
