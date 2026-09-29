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
	"context"
	"net"

	"golang.org/x/crypto/ssh"
)

// DialSSHWithAuth opens an SSH connection using a caller-supplied set
// of authentication methods. It mirrors DialSSH but lets higher layers
// inject auth that the spec alone cannot express — notably an
// ssh.Signer backed by a KMS-resident private key. Host key policy,
// port parsing, timeouts, and context handling are identical to DialSSH
// so callers get the same error types.
//
// authMethods must be non-empty; an empty slice is treated as an
// invalid spec and returns a typed error immediately.
func DialSSHWithAuth(ctx context.Context, spec *Spec, authMethods []ssh.AuthMethod) (*SSHClient, error) {
	return DialSSHWithAuthAndHostKey(ctx, spec, authMethods, nil)
}

// HostKeyPolicy is how a dial verifies the target's host key: the callback
// that checks the key the host presents, and the host-key algorithms to
// negotiate so the host presents a key the callback can accept.
//
// Algorithms matters because golang.org/x/crypto/ssh negotiates ECDSA host
// keys before ed25519: a host pinned by its ed25519 key presents its ECDSA
// key unless the client asks for ed25519, and the callback then rejects a
// perfectly legitimate host.
type HostKeyPolicy struct {
	// Callback verifies the presented host key. Required.
	Callback ssh.HostKeyCallback
	// Algorithms lists the host-key signature algorithms to negotiate, in
	// preference order. Empty keeps the library's default negotiation.
	Algorithms []string
}

// DialSSHWithHostKey opens an SSH connection using the spec's own
// credentials plus an explicit HostKeyPolicy. It is the password-auth
// counterpart of DialSSHWithAuthAndHostKey: the service layer resolves
// the target's pinned host key from the SSH known-hosts store and
// injects the resulting policy here, so a password deploy verifies its
// host exactly as a stored-key deploy does.
//
// A nil policy falls back to the spec's own policy (InsecureHostKey),
// which refuses to dial unless the operator opted out explicitly.
func DialSSHWithHostKey(ctx context.Context, spec *Spec, hostKey *HostKeyPolicy) (*SSHClient, error) {
	if spec == nil {
		return nil, &InvalidSpecError{Field: "spec", Reason: "spec is required"}
	}
	authMethods, err := buildAuthMethods(spec)
	if err != nil {
		return nil, err
	}
	return DialSSHWithAuthAndHostKey(ctx, spec, authMethods, hostKey)
}

// DialSSHWithAuthAndHostKey opens an SSH connection using both a
// caller-supplied set of authentication methods and an explicit
// HostKeyPolicy. It is the preferred entry point for the service
// layer, which injects an xkms-backed known-hosts policy so host-key
// pins live in the shared SSH store rather than in deploy.Spec. When
// hostKey is nil the call falls back to buildHostKeyCallback(spec) —
// mirroring DialSSHWithAuth — so callers that only need
// InsecureHostKey can still use this entry point.
func DialSSHWithAuthAndHostKey(ctx context.Context, spec *Spec, authMethods []ssh.AuthMethod, hostKey *HostKeyPolicy) (*SSHClient, error) {
	cfg, err := newClientConfig(spec, authMethods, hostKey)
	if err != nil {
		return nil, err
	}

	addr := normalizeHostPort(spec.Host)

	d := &net.Dialer{Timeout: cfg.Timeout}
	netConn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, &SSHDialError{Host: addr, User: spec.SSHUser, Cause: err}
	}

	sshConn, chans, reqs, err := ssh.NewClientConn(netConn, addr, cfg)
	if err != nil {
		_ = netConn.Close()
		return nil, &SSHDialError{Host: addr, User: spec.SSHUser, Cause: err}
	}

	return &SSHClient{
		client: ssh.NewClient(sshConn, chans, reqs),
		host:   addr,
	}, nil
}

// newClientConfig validates the dial inputs and builds the client
// configuration. A policy with Algorithms restricts host-key negotiation
// to them, so the host presents a key the policy's callback can accept.
func newClientConfig(spec *Spec, authMethods []ssh.AuthMethod, hostKey *HostKeyPolicy) (*ssh.ClientConfig, error) {
	if spec == nil {
		return nil, &InvalidSpecError{Field: "spec", Reason: "spec is required"}
	}
	if spec.Host == "" {
		return nil, &InvalidSpecError{Field: "host", Reason: "host is required"}
	}
	if spec.SSHUser == "" {
		return nil, &InvalidSpecError{Field: "ssh_user", Reason: "ssh user is required"}
	}
	if len(authMethods) == 0 {
		return nil, &InvalidSpecError{Field: "ssh_auth", Reason: "at least one auth method is required"}
	}

	policy := hostKey
	if policy != nil && policy.Callback == nil {
		return nil, &InvalidSpecError{Field: "ssh_host_key", Reason: "host-key policy has no callback"}
	}
	if policy == nil {
		cb, err := buildHostKeyCallback(spec)
		if err != nil {
			return nil, err
		}
		policy = &HostKeyPolicy{Callback: cb}
	}

	cfg := &ssh.ClientConfig{
		User:            spec.SSHUser,
		Auth:            authMethods,
		HostKeyCallback: policy.Callback,
		Timeout:         defaultDialTimeout,
	}
	if len(policy.Algorithms) > 0 {
		cfg.HostKeyAlgorithms = append([]string(nil), policy.Algorithms...)
	}
	applyFIPSAlgorithms(cfg)
	return cfg, nil
}
