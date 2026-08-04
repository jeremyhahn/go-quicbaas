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

package server

import (
	"context"
	"log/slog"

	qrdbsecret "github.com/jeremyhahn/go-qrdb/sdk/go/secret"

	"github.com/jeremyhahn/go-quicbaas/pkg/config/secrets"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// ErrKMSUnavailableError is returned by the lazy kms:// resolver when the
// SecretsService has not yet been started (i.e. before startSecrets completes).
// This is expected at CA bring-up time: the credential:// and literal resolvers
// are the only usable strategies during that window.
type ErrKMSUnavailableError struct{}

// Error implements the error interface.
func (e *ErrKMSUnavailableError) Error() string {
	return "kms: secrets service is not yet available (startSecrets has not run)"
}

// lazyKMSSecretsClient implements secrets.SecretsClient backed by the Server's
// quicBaaSService.Secrets(). The service may be nil at CA bring-up time
// (before startSecrets runs); when it is nil, GetSecret returns
// ErrKMSUnavailableError so callers receive a clear typed error instead of a
// nil-pointer panic.
type lazyKMSSecretsClient struct {
	s *Server
}

// Compile-time check that lazyKMSSecretsClient satisfies secrets.SecretsClient.
var _ secrets.SecretsClient = (*lazyKMSSecretsClient)(nil)

// GetSecret retrieves a secret by secretID. Returns ErrKMSUnavailableError
// when the SecretsService has not yet been initialized.
func (c *lazyKMSSecretsClient) GetSecret(ctx context.Context, secretID string) ([]byte, error) {
	if c.s.quicBaaSService == nil {
		return nil, &ErrKMSUnavailableError{}
	}
	svc := c.s.quicBaaSService.Secrets()
	if svc == nil {
		return nil, &ErrKMSUnavailableError{}
	}
	resp, err := svc.GetSecret(ctx, &service.GetSecretRequest{SecretID: secretID})
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// buildSecretRegistry constructs the unified secret resolver registry for this
// Server. It is called after the qrdb node is online (so the credential store
// is available) but before startSecrets, giving the kms:// and remote-kms://
// resolvers a lazy initialization path that defers SecretsService access until
// the service has been started.
//
// Registration order:
//  1. credential:// — system-scoped credential store via the embedded qrdb node.
//     Usable immediately after node init. Returns ErrNilContext / ResolutionFailedError.
//  2. kms:// — local KMS via a lazy SecretsClient that returns ErrKMSUnavailableError
//     before startSecrets completes. After startSecrets the full KMS is available.
//  3. remote-kms:// — remote KMS (nil client = returns InvalidSecretURIError on resolve).
//  4. autogen — generates high-entropy secrets for placeholder values.
//
// The returned registry is stored on s.secretRegistry and also accessible via
// GetSecretRegistry() for SDK consumers.
func (s *Server) buildSecretRegistry() *qrdbsecret.ResolverRegistry {
	reg := qrdbsecret.NewResolverRegistry()

	// 1. credential:// — tenant-scoped when a BarrierRegistry is present;
	// falls back to system-scoped when the node has no barrier registry
	// (seal disabled, or sealed state before Unseal completes).
	//
	// Guard against a nil qrdbNode (test-stub / external-CA path where the
	// node is not built): no credential resolver is registered in that case,
	// so credential:// URIs fall through to the literal strategy.
	if s.qrdbNode != nil {
		if svcs := s.qrdbNode.Services(); svcs != nil && svcs.Credential != nil {
			br := s.qrdbNode.BarrierRegistry()
			if br != nil {
				// Tenant-scoped: routes to the per-tenant barrier-backed
				// CredentialService when the request context carries a tenant ID,
				// falls back to the system-scoped Credential service otherwise.
				// iam.TenantFromContext returns a string; the (string,bool) adapter
				// treats a non-empty string as "tenant present".
				reg.Register(qrdbsecret.NewCredentialResolver(
					qrdbsecret.NewTenantScopedCredentialRetrieve(
						br,
						svcs.Credential,
						func(ctx context.Context) (string, bool) {
							tid := iam.TenantFromContext(ctx)
							return tid, tid != ""
						},
						slog.Default(),
					),
				))
			} else {
				// System-scoped fallback: seal disabled or barrier registry not
				// yet built (sealed state). Uses the system credential service
				// directly, matching the pre-tenant-routing behaviour.
				reg.Register(qrdbsecret.CredentialResolverFromServicer(svcs.Credential))
			}
		}
	}

	// 2. kms:// — lazy, backed by the quicbaas SecretsService (available after
	// startSecrets). The LocalKMSResolver is always non-nil because
	// lazyKMSSecretsClient is never nil. secrets.SecretResolver is now an alias
	// for qrdbsecret.SecretResolver so concrete resolvers register directly.
	reg.Register(secrets.NewLocalKMSResolver(&lazyKMSSecretsClient{s: s}))

	// 3. remote-kms:// — remote KMS with a nil client. The RemoteKMSResolver
	// guards against a nil client in Resolve, returning an appropriate error.
	reg.Register(secrets.NewRemoteKMSResolver(nil))

	// 4. autogen — generates crypto-random base64 secrets for placeholder values.
	reg.Register(secrets.NewAutoGenResolver())

	return reg
}

// GetSecretRegistry returns the unified secret resolver registry. It is nil
// until after initializeCA has run (i.e. after the qrdb node is online). SDK
// consumers (e.g. go-trusted-platform) should call this after Server.Start has
// returned to obtain a fully-populated registry.
func (s *Server) GetSecretRegistry() *qrdbsecret.ResolverRegistry {
	return s.secretRegistry
}
