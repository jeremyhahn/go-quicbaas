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

	"github.com/jeremyhahn/go-quicbaas/pkg/fleet/deploy"
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

// HostKeyPolicy implements SSHHostKeyResolver.
//
// The pin lookup happens here, before the dial: an unpinned host is an
// operator mistake worth reporting immediately, with the command that
// fixes it, rather than mid-handshake. The returned callback performs no
// I/O — it compares the presented key against the pins already read.
//
// The policy also restricts host-key negotiation to the pinned
// algorithms. golang.org/x/crypto/ssh prefers ECDSA host keys, so without
// that restriction a host pinned by its ed25519 key presents its ECDSA
// key and the callback rejects it.
func (r *XKMSSSHHostKeyResolver) HostKeyPolicy(ctx context.Context, ref SSHHostRef) (*deploy.HostKeyPolicy, error) {
	if r.KnownHosts == nil {
		return nil, &XKMSSSHHostKeyNilStoreError{}
	}
	host := stripSSHPort(ref.Host)
	if host == "" {
		return nil, &XKMSSSHHostKeyMissingHostError{}
	}
	scope := firstNonEmpty(ref.Scope, r.Scope, defaultSSHKeyScope)
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

	return &deploy.HostKeyPolicy{
		Callback: func(_ string, _ net.Addr, key ssh.PublicKey) error {
			return verifyPinnedHostKey(host, matched, key)
		},
		Algorithms: hostKeyAlgorithmsForPins(matched),
	}, nil
}

// hostKeyAlgorithmsForPins returns the host-key signature algorithms to
// negotiate so the host presents a key one of pins covers, de-duplicated
// in pin order. A pin with no algorithm constrains the fingerprint only
// and so accepts a key of any algorithm; when one is present the result
// is empty and the library's default negotiation applies.
func hostKeyAlgorithmsForPins(pins []*xkmsssh.Pin) []string {
	algorithms := make([]string, 0, len(pins))
	seen := make(map[string]bool, len(pins))
	for _, pin := range pins {
		if pin.Algorithm == "" {
			return nil
		}
		for _, algorithm := range hostKeySignatureAlgorithms(pin.Algorithm) {
			if seen[algorithm] {
				continue
			}
			seen[algorithm] = true
			algorithms = append(algorithms, algorithm)
		}
	}
	return algorithms
}

// pinSignatureAlgorithms maps a pin's key algorithm — the key type, as
// ssh.PublicKey.Type reports it, lower-cased — to the host-key signature
// algorithms that make a host present a key of that type. An RSA key is
// presented under any of three signature algorithms, strongest first.
var pinSignatureAlgorithms = map[string][]string{
	ssh.KeyAlgoED25519:  {ssh.KeyAlgoED25519},
	ssh.KeyAlgoECDSA256: {ssh.KeyAlgoECDSA256},
	ssh.KeyAlgoECDSA384: {ssh.KeyAlgoECDSA384},
	ssh.KeyAlgoECDSA521: {ssh.KeyAlgoECDSA521},
	ssh.KeyAlgoRSA:      {ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, ssh.KeyAlgoRSA},
}

// hostKeySignatureAlgorithms returns the host-key signature algorithms
// for a pin's key algorithm. Pins compare algorithms case-insensitively,
// so the lookup does too; an algorithm the table does not know is passed
// through as-is.
func hostKeySignatureAlgorithms(keyAlgorithm string) []string {
	if algorithms, ok := pinSignatureAlgorithms[strings.ToLower(keyAlgorithm)]; ok {
		return algorithms
	}
	return []string{keyAlgorithm}
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
	// No pin names the algorithm the host negotiated. That is not a rotated
	// key, it is a key the pins never covered: golang.org/x/crypto/ssh
	// prefers ECDSA host keys, so a host pinned by its ed25519 key presents
	// its ECDSA one unless the dial asks for ed25519 — which is how the remote fleet node
	// failed. HostKeyPolicy's Algorithms now prevent that; this check stays
	// as defence in depth. Say so, rather than reporting a fingerprint
	// mismatch that reads like an attack.
	mismatch := &SSHHostKeyMismatchError{
		Host:      host,
		Algorithm: algorithm,
		Presented: presented,
		Pinned:    expected,
	}
	if !pinsCoverAlgorithm(pins, algorithm) {
		algorithms := make([]string, 0, len(pins))
		for _, pin := range pins {
			algorithms = append(algorithms, pin.Algorithm)
		}
		return &SSHHostKeyAlgorithmMismatchError{
			Host:      host,
			Presented: algorithm,
			Offered:   presented,
			Pinned:    algorithms,
			Mismatch:  mismatch,
		}
	}
	return mismatch
}

// pinsCoverAlgorithm reports whether any pin applies to keys of algorithm. A
// pin with no algorithm applies to every key.
func pinsCoverAlgorithm(pins []*xkmsssh.Pin, algorithm string) bool {
	for _, pin := range pins {
		if pin.Algorithm == "" || strings.EqualFold(pin.Algorithm, algorithm) {
			return true
		}
	}
	return false
}

// pinsForHost returns every pin recorded for host. Host comparison is
// case-insensitive and port-insensitive so "Agent-09" and "agent-09:22" both
// resolve the pin stored as "agent-09".
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
		" --key '<authorized_keys line>'" +
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

// Fix implements deploy.Fixer.
func (e *SSHHostKeyNotPinnedError) Fix() string {
	return "read the node's key fingerprint on the node itself (ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub), then: " +
		"quicbaas ssh known-hosts add --host " + e.Host + " --algorithm ssh-ed25519 --fingerprint <SHA256:...>"
}

// Fix implements deploy.Fixer.
func (e *SSHHostKeyMismatchError) Fix() string {
	return "the node presented " + e.Algorithm + " " + e.Presented + ", which is not pinned. Confirm on the node " +
		"(ssh-keygen -lf /etc/ssh/ssh_host_*_key.pub) that it is the node's key before trusting it; if it is, " +
		"re-pin: quicbaas ssh known-hosts add --host " + e.Host + " --algorithm " + e.Algorithm + " --fingerprint " + e.Presented
}

// SSHHostKeyAlgorithmMismatchError reports a host whose negotiated host-key
// algorithm no pin covers. Every pin names a different algorithm than the key
// the host presented, so the pins could not have matched whatever the key.
type SSHHostKeyAlgorithmMismatchError struct {
	// Host is the SSH target.
	Host string
	// Presented is the algorithm of the key the host presented.
	Presented string
	// Offered is the presented key's SHA256 fingerprint.
	Offered string
	// Pinned lists the algorithms the host's pins name.
	Pinned []string
	// Mismatch is the underlying fingerprint mismatch. An algorithm
	// mismatch IS a host-key mismatch, so callers matching on
	// *SSHHostKeyMismatchError keep matching it.
	Mismatch *SSHHostKeyMismatchError
}

// Unwrap exposes the underlying *SSHHostKeyMismatchError.
func (e *SSHHostKeyAlgorithmMismatchError) Unwrap() error {
	if e.Mismatch == nil {
		return nil
	}
	return e.Mismatch
}

// Error implements the error interface.
func (e *SSHHostKeyAlgorithmMismatchError) Error() string {
	return "service: ssh host-key algorithm mismatch for " + e.Host + ": the host presented its " + e.Presented +
		" key, but its pins are for " + strings.Join(e.Pinned, ", ") + " (Go's ssh client negotiates ECDSA before ed25519)"
}

// Fix implements deploy.Fixer.
func (e *SSHHostKeyAlgorithmMismatchError) Fix() string {
	return "pin the node's " + e.Presented + " key as well — confirm on the node that its fingerprint is " + e.Offered +
		" (ssh-keygen -lf /etc/ssh/ssh_host_*_key.pub), then: quicbaas ssh known-hosts add --host " + e.Host +
		" --algorithm " + e.Presented + " --fingerprint " + e.Offered
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
