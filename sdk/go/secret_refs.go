// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package quicbaas

import (
	"context"

	cfgsecrets "github.com/jeremyhahn/go-quicbaas/pkg/config/secrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
)

// SecretValue is a resolved secret reference, as returned by
// ResolverRegistry.Resolve. It is an alias for the daemon's
// config-secrets SecretValue.
type SecretValue = cfgsecrets.SecretValue

// ResolverRegistry resolves secret references (such as
// "kms://path?ensure=true&len=N&enc=E") by URI scheme. It is an alias
// for the daemon's config-secrets ResolverRegistry.
type ResolverRegistry = cfgsecrets.ResolverRegistry

// NewSecretResolverRegistry returns a ResolverRegistry with the daemon's
// local "kms://" resolver registered over the SDK secrets client c, so a
// consumer resolves its config references with the same resolver the
// daemon uses and writes none of its own. A value with no scheme resolves
// to itself.
//
// A plain "kms://path" reference reads the whole stored payload through
// c.GetWithBytes; "kms://path?ensure=true&len=N&enc=E" generates the secret
// on first access through c.Ensure.
//
// Known limit: the MCP transport refuses GetWithBytes, so plain "kms://"
// references do not resolve over a client on the MCP transport.
func NewSecretResolverRegistry(c SecretsClient) *ResolverRegistry {
	reg := cfgsecrets.NewResolverRegistry()
	reg.Register(cfgsecrets.NewLocalKMSResolver(sdkSecretsAdapter{c: c}))
	return reg
}

// sdkSecretsAdapter adapts the SDK secrets client to the config-secrets
// SecretsClient and SecretsEnsurer interfaces.
type sdkSecretsAdapter struct {
	c SecretsClient
}

var (
	_ cfgsecrets.SecretsClient  = sdkSecretsAdapter{}
	_ cfgsecrets.SecretsEnsurer = sdkSecretsAdapter{}
)

// GetSecret returns a copy of the whole payload stored at secretID.
func (a sdkSecretsAdapter) GetSecret(ctx context.Context, secretID string) ([]byte, error) {
	var out []byte
	err := a.c.GetWithBytes(ctx, secretID, "", func(b []byte) error {
		out = append([]byte(nil), b...)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// EnsureSecret returns the secret at secretID, generating it with the
// given length and encoding on first access.
func (a sdkSecretsAdapter) EnsureSecret(ctx context.Context, secretID string, length int, encoding string) ([]byte, error) {
	resp, err := a.c.Ensure(ctx, &secrets.EnsureSecretRequest{
		Path:     secretID,
		Length:   length,
		Encoding: encoding,
	})
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}
