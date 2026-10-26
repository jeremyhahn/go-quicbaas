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

	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"
	qrdbsecure "github.com/jeremyhahn/go-qrdb/sdk/go/secure"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
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
	// Version 0 is the current version. qrdb authorizes this verb as
	// credentials:retrieve_version, which the service principal holds;
	// CredentialRetrieveBuffer needs credentials:retrieve, which it does not.
	return credentials.CredentialRetrieveVersionBuffer(ctx, &qrdbtransport.CredentialRetrieveVersionRequest{
		Name: name,
	})
}

// boundKMSSecrets serves kms:// resolution through the client's bound
// secrets client. The embedded transport authorizes each dispatch against
// the bound identity's grants (quicbaas.Secrets/GetData needs secrets:read,
// quicbaas.Secrets/Ensure secrets:read and secrets:write), so this adapter
// adds no check of its own: one logical request is authorized, and spends
// a rate-limit token, exactly once.
type boundKMSSecrets struct {
	c *client
}

// Compile-time check that boundKMSSecrets supports kms://...?ensure=true.
var _ secrets.SecretsEnsurer = (*boundKMSSecrets)(nil)

// GetSecret returns the stored payload of the secret at secretID.
func (b *boundKMSSecrets) GetSecret(ctx context.Context, secretID string) ([]byte, error) {
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
