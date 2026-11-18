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

package workerimage

import (
	"bytes"
	"context"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// SecretCredential is a registry account whose token is a QuicBaaS tenant
// secret.
//
// THE SECRET IS READ AT EACH PUSH, and only then: rotating the token is a new
// version of the secret and nothing else, and a pass that never publishes
// never touches it. The value goes from the secrets service to the engine's
// login on stdin, under a private client configuration the push removes
// (orchestration.ExecContainerRuntime.PushImage); it is never written to the
// node-wide engine configuration, an argument or a file of its own.
type SecretCredential struct {
	resolver orchestration.AccountSecretResolver
	tenantID string
	ref      string
	username string
}

// NewSecretCredential validates the reference: ref is a short,
// tenant-relative secret name in tenantID's keyspace — the same names an SDLC
// binding's forge.token_secret_ref uses — and username is the registry
// account the token belongs to.
func NewSecretCredential(resolver orchestration.AccountSecretResolver, tenantID, ref, username string) (*SecretCredential, error) {
	if resolver == nil {
		return nil, &InvalidConfigError{Field: "secrets", Message: "a secret-backed registry credential needs the secrets service"}
	}
	if strings.TrimSpace(tenantID) == "" {
		return nil, &InvalidConfigError{Field: "registry_token_tenant", Message: "required: the tenant whose secrets hold the registry token"}
	}
	if err := orchestration.ValidateAccountSecretRef(ref); err != nil {
		return nil, &InvalidConfigError{Field: "registry_token_secret", Message: err.Error()}
	}
	if strings.TrimSpace(username) == "" {
		return nil, &InvalidConfigError{Field: "registry_username", Message: "required: the registry account the token belongs to"}
	}
	return &SecretCredential{resolver: resolver, tenantID: tenantID, ref: ref, username: username}, nil
}

// Credential reads the token from the tenant's secrets.
func (c *SecretCredential) Credential(ctx context.Context) (*orchestration.RegistryAuth, error) {
	source := c.tenantID + ":" + c.ref
	value, err := c.resolver.ResolveAccountSecret(ctx, c.tenantID, c.ref)
	if err != nil {
		return nil, &CredentialError{Source: source, Message: "cannot be read", Cause: err}
	}
	if value == nil {
		return nil, &CredentialError{Source: source, Message: "does not exist"}
	}
	token := bytes.TrimSpace(value.Data)
	if len(token) == 0 {
		return nil, &CredentialError{Source: source, Message: "is empty"}
	}
	return &orchestration.RegistryAuth{Username: c.username, Secret: bytes.Clone(token)}, nil
}
