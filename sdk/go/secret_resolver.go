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

package quicbaas

import (
	"context"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"
	qrdbsecure "github.com/jeremyhahn/go-qrdb/sdk/go/secure"
	"github.com/jeremyhahn/go-quicbaas/pkg/config/secrets"
	secretsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
)

// newBoundSecretRegistry builds the secret resolver registry of an embedded
// client. Every resolver reads through the client's own transport, so each
// resolution runs as the client's bound identity and the node authorizes it
// like any other call: a refused delegation resolves nothing, and a
// delegation reads only its own tenant's secrets. The server's registry is
// never handed out, because it reads as the system with no identity check.
func newBoundSecretRegistry(c *client) *qrdbsecret.ResolverRegistry {
	reg := qrdbsecret.NewResolverRegistry()
	reg.Register(qrdbsecret.NewCredentialResolver(c.retrieveCredential))
	reg.Register(secrets.NewLocalKMSResolver(&boundKMSSecrets{c: c}))
	// remote-kms:// has no remote client in embedded mode; registering it
	// keeps such URIs recognised so they fail with the resolver's own error.
	reg.Register(secrets.NewRemoteKMSResolver(nil))
	return reg
}

// retrieveCredential reads a credential:// value through the client's bound
// credential client.
func (c *client) retrieveCredential(ctx context.Context, name string) (*qrdbsecure.Buffer, error) {
	credentials, err := c.Security().Credentials()
	if err != nil {
		return nil, err
	}
	return credentials.CredentialRetrieveBuffer(ctx, name)
}

// boundKMSSecrets serves kms:// resolution through the client's bound
// secrets client, after authorizing a delegation's secrets grants.
type boundKMSSecrets struct {
	c *client
}

// The grants go-qrdb declares for tenant secret material (go-qrdb
// pkg/permission SecretsRead and SecretsWrite). The qrdb SDK re-exports the
// Permission type but not these names. The secrets backend runs as the
// server's service principal, so boundKMSSecrets authorizes a delegation
// against them before dispatch.
const (
	permissionSecretsRead  qrdbsdk.Permission = "secrets:read"
	permissionSecretsWrite qrdbsdk.Permission = "secrets:write"
)

// Compile-time check that boundKMSSecrets supports kms://...?ensure=true.
var _ secrets.SecretsEnsurer = (*boundKMSSecrets)(nil)

// GetSecret returns the stored payload of the secret at secretID.
func (b *boundKMSSecrets) GetSecret(ctx context.Context, secretID string) ([]byte, error) {
	if err := b.c.opts.embeddedIdentity.authorize(ctx, permissionSecretsRead); err != nil {
		return nil, err
	}
	var data []byte
	err := b.c.secrets().GetWithBytes(ctx, secretID, "", func(payload []byte) error {
		data = append([]byte(nil), payload...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// EnsureSecret returns the secret at secretID, generating it on first use.
func (b *boundKMSSecrets) EnsureSecret(ctx context.Context, secretID string, length int, encoding string) ([]byte, error) {
	// Ensure returns the secret and may create it, so it needs both grants.
	if err := b.c.opts.embeddedIdentity.authorize(ctx, permissionSecretsRead, permissionSecretsWrite); err != nil {
		return nil, err
	}
	resp, err := b.c.secrets().Ensure(ctx, &secretsclient.EnsureSecretRequest{
		Path:     secretID,
		Length:   length,
		Encoding: encoding,
	})
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}
