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

//go:build desktop

package main

import (
	"context"

	xkmsssh "github.com/jeremyhahn/go-xkms/sdk/go/client/ssh"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// =============================================================================
// SSHKeysBinding — Wails binding for SSH key management
// =============================================================================
//
// Exposes four methods that mirror the existing TypeScript
// SSHKeysClient surface (sdk/typescript/src/wails.ts): List, Create,
// Get, Delete. Under the hood every call delegates to an xkms SSH
// SDK client so the binding and the CLI share a single code path.
//
// The binding intentionally defaults scope/owner to user/$USER so the
// existing Svelte views, which treat SSH keys as flat (name, algorithm,
// fingerprint) tuples, keep working unchanged. Phase 2b rewrites the
// TypeScript surface to expose scope/owner/name directly.

// SSHKeysBinding is exported so Wails can bind it on startup.
type SSHKeysBinding struct {
	logger *logger.Logger
	ctx    context.Context

	// newClient is the factory for the xkms SSH SDK client. Stored so
	// tests can substitute a fake. Defaults to buildCLISSHClient so the
	// binding picks the same transport (REST or embedded file store)
	// as the `quicbaas ssh` CLI and the agent deploy path.
	newClient func() (xkmsssh.Client, error)

	// defaultScope / defaultOwner are applied to every SDK request.
	// The binding is constructed with the operator's identity baked in
	// so the Svelte surface doesn't have to carry scope/owner through
	// its UI — the flat-by-name contract holds.
	defaultScope string
	defaultOwner string
}

// NewSSHKeysBinding constructs a binding that uses buildCLISSHClient
// for transport selection and cliSSHOwner() for owner defaults.
func NewSSHKeysBinding(log *logger.Logger) *SSHKeysBinding {
	return &SSHKeysBinding{
		logger:       log,
		newClient:    buildCLISSHClient,
		defaultScope: "user",
		defaultOwner: cliSSHOwner(),
	}
}

// startup stores the Wails context for downstream calls.
func (b *SSHKeysBinding) startup(ctx context.Context) {
	b.ctx = ctx
}

// shutdown is a no-op — the binding does not hold persistent
// connections; each call opens a fresh client and closes it.
func (b *SSHKeysBinding) shutdown(_ context.Context) {}

// =============================================================================
// Wire-level DTOs
// =============================================================================
//
// These mirror sdk/typescript/src/types.ts so the binding's output
// shape matches what the Svelte frontend already consumes.

// SSHKeyDTO is the flat-by-name view returned to the frontend. It
// deliberately avoids exposing scope/owner so the existing Svelte
// components don't need a schema change during Phase 2a.
type SSHKeyDTO struct {
	Name             string `json:"name"`
	Algorithm        string `json:"algorithm"`
	Fingerprint      string `json:"fingerprint"`
	PublicKeyOpenSSH string `json:"public_key_openssh"`
	CreatedAt        string `json:"created_at"`
}

// CreateSSHKeyRequest matches the frontend-supplied shape for create.
type CreateSSHKeyRequest struct {
	Name      string `json:"name"`
	Algorithm string `json:"algorithm"`
}

// ListSSHKeysResponse matches the frontend-expected envelope.
type ListSSHKeysResponse struct {
	Keys []SSHKeyDTO `json:"keys"`
}

// =============================================================================
// Binding methods — exposed to Wails
// =============================================================================

// List returns all SSH keys for the binding's (scope, owner) pair.
func (b *SSHKeysBinding) List() (*ListSSHKeysResponse, error) {
	client, err := b.newClient()
	if err != nil {
		return nil, NewGUITransportError("SSHKeysBinding.List", err)
	}
	defer client.Close()

	items, err := client.Keys().List(b.context(), xkmsssh.ListKeysRequest{
		Scope: b.defaultScope,
		Owner: b.defaultOwner,
	})
	if err != nil {
		return nil, NewGUITransportError("SSHKeysBinding.List", err)
	}
	out := &ListSSHKeysResponse{Keys: make([]SSHKeyDTO, 0, len(items))}
	for _, k := range items {
		out.Keys = append(out.Keys, sshKeyToDTO(k))
	}
	return out, nil
}

// Create generates a new SSH key. Algorithm defaults to ed25519 when
// empty.
func (b *SSHKeysBinding) Create(req *CreateSSHKeyRequest) (*SSHKeyDTO, error) {
	if req == nil || req.Name == "" {
		return nil, NewGUIRequestRequiredError("SSHKeysBinding.Create")
	}
	algo := req.Algorithm
	if algo == "" {
		algo = "ed25519"
	}
	client, err := b.newClient()
	if err != nil {
		return nil, NewGUITransportError("SSHKeysBinding.Create", err)
	}
	defer client.Close()

	out, err := client.Keys().Generate(b.context(), xkmsssh.GenerateRequest{
		Scope: b.defaultScope, Owner: b.defaultOwner,
		Name: req.Name, Algorithm: algo,
	})
	if err != nil {
		return nil, NewGUITransportError("SSHKeysBinding.Create", err)
	}
	dto := sshKeyToDTO(out)
	return &dto, nil
}

// Get returns the metadata for a single key by name.
func (b *SSHKeysBinding) Get(name string) (*SSHKeyDTO, error) {
	if name == "" {
		return nil, NewGUIRequestRequiredError("SSHKeysBinding.Get")
	}
	client, err := b.newClient()
	if err != nil {
		return nil, NewGUITransportError("SSHKeysBinding.Get", err)
	}
	defer client.Close()

	out, err := client.Keys().Get(b.context(), xkmsssh.GetKeyRequest{
		Scope: b.defaultScope, Owner: b.defaultOwner, Name: name,
	})
	if err != nil {
		return nil, NewGUITransportError("SSHKeysBinding.Get", err)
	}
	dto := sshKeyToDTO(out)
	return &dto, nil
}

// Delete removes a key by name.
func (b *SSHKeysBinding) Delete(name string) error {
	if name == "" {
		return NewGUIRequestRequiredError("SSHKeysBinding.Delete")
	}
	client, err := b.newClient()
	if err != nil {
		return NewGUITransportError("SSHKeysBinding.Delete", err)
	}
	defer client.Close()

	if err := client.Keys().Delete(b.context(), xkmsssh.DeleteKeyRequest{
		Scope: b.defaultScope, Owner: b.defaultOwner, Name: name,
	}); err != nil {
		return NewGUITransportError("SSHKeysBinding.Delete", err)
	}
	return nil
}

// =============================================================================
// Helpers
// =============================================================================

// context returns the Wails-provided context, falling back to
// context.Background when the binding has not yet been started up.
func (b *SSHKeysBinding) context() context.Context {
	if b.ctx != nil {
		return b.ctx
	}
	return context.Background()
}

// sshKeyToDTO mirrors xkmsssh.Key onto the flat-by-name DTO the Svelte
// frontend expects.
func sshKeyToDTO(k *xkmsssh.Key) SSHKeyDTO {
	if k == nil {
		return SSHKeyDTO{}
	}
	return SSHKeyDTO{
		Name:             k.Name,
		Algorithm:        k.Algorithm,
		Fingerprint:      k.Fingerprint,
		PublicKeyOpenSSH: k.PublicKey,
		CreatedAt:        k.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
	}
}
