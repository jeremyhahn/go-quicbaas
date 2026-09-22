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
	"strings"

	"golang.org/x/crypto/ssh"
)

// SSHSignerResolver resolves a named SSH key into an ssh.Signer. The
// service layer uses this narrow interface to keep AgentDeployService
// independent of any specific SSH key-management implementation — it
// only needs the one operation "give me a signer for this key name".
//
// Callers typically wire an xkms-backed resolver here so the private
// key material never leaves the xkms server; the returned ssh.Signer
// proxies Sign operations over xkms's remote-signing path.
type SSHSignerResolver interface {
	// Signer returns an ssh.Signer backed by the named stored key.
	// The ref carries the triple (scope, owner, name) that uniquely
	// identifies the key in the xkms store. Empty scope/owner let the
	// resolver apply its default policy (typically scope="user",
	// owner=current operator). The signer's lifetime is owned by the
	// caller; Close semantics are implementation-specific.
	Signer(ctx context.Context, ref SSHKeyRef) (ssh.Signer, error)
}

// SSHKeyRef is the (scope, owner, name) triple used to look up a
// stored SSH key in the xkms-backed key store.
type SSHKeyRef struct {
	// Name is the key identifier within the scope. Required.
	Name string
	// Scope is one of "user", "tenant", "service". Empty → "user".
	Scope string
	// Owner is the scope-specific owner id. Empty → resolver default.
	Owner string
}

// SSHAuthConflictError reports that a deploy.Spec set more than one of
// the mutually-exclusive SSH auth fields (ssh_key_path, ssh_key_name,
// ssh_password). Callers must pick exactly one.
type SSHAuthConflictError struct {
	// Fields lists the auth field names that were set simultaneously.
	Fields []string
}

// Error implements the error interface.
func (e *SSHAuthConflictError) Error() string {
	if e == nil || len(e.Fields) == 0 {
		return "service: ssh auth fields conflict"
	}
	return "service: conflicting ssh auth fields: " + strings.Join(e.Fields, ", ")
}

// SSHHostKeyResolver resolves the host-key verification policy for an
// SSH target. The service layer uses this narrow interface so
// AgentDeployService stays independent of where host-key pins live — it
// only needs the one operation "give me the callback that decides
// whether this host's key is trusted".
//
// Callers wire an xkms-backed resolver here so the pins consulted are
// exactly the ones `quicbaas ssh known-hosts add/list` manage.
type SSHHostKeyResolver interface {
	// HostKeyCallback returns the ssh.HostKeyCallback that verifies a
	// target's presented host key against its stored pins. Resolution is
	// eager: a host with no pin fails here, before the dial, with a typed
	// error that tells the operator how to pin it.
	HostKeyCallback(ctx context.Context, ref SSHHostRef) (ssh.HostKeyCallback, error)
}

// SSHHostRef identifies the host whose pins to consult. Scope and Owner
// address the known-hosts store exactly as SSHKeyRef addresses the key
// store — both live behind the same xkms-backed SSH surface, under the
// same (scope, owner) pair.
type SSHHostRef struct {
	// Host is the SSH destination in "host" or "host:port" form. The
	// port, when present, is ignored: pins are stored per host.
	Host string
	// Scope is one of "user", "tenant", "service". Empty → "user".
	Scope string
	// Owner is the scope-specific owner id. Empty → resolver default.
	Owner string
}
