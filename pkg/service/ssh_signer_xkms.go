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

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"
)

// XKMSSSHSignerResolver adapts an xkmsssh.Client into the narrow
// SSHSignerResolver contract consumed by FleetDeployService.
//
// Spec.SSHKeyName is the only caller-supplied identifier; the
// resolver layers on a (Scope, Owner) pair so multi-tenant deployments
// can route lookups into tenant-scoped SSH stores. Callers supply
// these at construction time — the defaults ("user" / current
// operator) match the xkey CLI convention.
type XKMSSSHSignerResolver struct {
	// Client is the xkms SSH SDK client. Required.
	Client xkmsssh.Client

	// Scope selects the SSH asset scope. Defaults to "user" when empty.
	Scope string

	// Owner is the scope-specific owner identifier. Required — the
	// resolver returns *XKMSSSHSignerMissingOwnerError on an empty
	// owner because we refuse to silently pick a default (e.g. "root")
	// on behalf of a deploy operator.
	Owner string
}

// XKMSSSHSignerMissingOwnerError is returned by Signer when the
// resolver was constructed without an Owner.
type XKMSSSHSignerMissingOwnerError struct{}

// Error implements the error interface.
func (e *XKMSSSHSignerMissingOwnerError) Error() string {
	return "service: xkms ssh signer resolver: owner is required"
}

// XKMSSSHSignerNilClientError is returned when the resolver was
// constructed without a Client. We catch this eagerly so a
// misconfigured deploy fails with a clear message rather than a
// confusing panic.
type XKMSSSHSignerNilClientError struct{}

// Error implements the error interface.
func (e *XKMSSSHSignerNilClientError) Error() string {
	return "service: xkms ssh signer resolver: client is required"
}

// Signer implements SSHSignerResolver by delegating to the xkms SSH
// SDK. The returned ssh.Signer is enclave-backed for the embedded
// transport and forward-signing for the REST transport — in both
// cases the private material never leaves the key store.
func (r *XKMSSSHSignerResolver) Signer(ctx context.Context, ref SSHKeyRef) (ssh.Signer, error) {
	if r.Client == nil {
		return nil, &XKMSSSHSignerNilClientError{}
	}
	scope := strings.TrimSpace(ref.Scope)
	if scope == "" {
		scope = strings.TrimSpace(r.Scope)
	}
	if scope == "" {
		scope = defaultSSHKeyScope
	}
	owner := strings.TrimSpace(ref.Owner)
	if owner == "" {
		owner = strings.TrimSpace(r.Owner)
	}
	if owner == "" {
		return nil, &XKMSSSHSignerMissingOwnerError{}
	}
	return r.Client.Keys().Signer(ctx, xkmsssh.GetKeyRequest{
		Scope: scope,
		Owner: owner,
		Name:  ref.Name,
	})
}

// compile-time contract.
var _ SSHSignerResolver = (*XKMSSSHSignerResolver)(nil)
