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

package server

import (
	"context"
	"crypto"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// platformBearerIssuer is the issuer and audience of the bearer-path tests.
const platformBearerIssuer = "https://issuer.test"

// platformBearerNode is one node's OIDC provider over the replicated signing
// backend and the platform validator its bearer path builds from it.
type platformBearerNode struct {
	global    *iam.SigningKeyManager
	resolver  *iam.TenantSigningKeyResolver
	validator *platformtoken.Validator
}

// newPlatformBearerNode starts a node on the replicated signing backend,
// with tenantID (when not "") holding a dedicated key, and builds its
// platform validator through Server.buildPlatformValidator.
func newPlatformBearerNode(t *testing.T, tenantID string) *platformBearerNode {
	t.Helper()
	global, err := newOIDCSigningKeyManager(oidcSigningBackendName)
	require.NoError(t, err)
	resolver, err := iam.NewTenantSigningKeyResolver(global, dedicatedOIDCSettings{tenantID: tenantID != ""})
	require.NoError(t, err)
	backend := iam.NewBackendStorage(kvstore.NewMemory())
	provider, err := iam.NewOIDCProvider(&iam.OIDCConfig{
		Issuer:            platformBearerIssuer,
		SigningAlgorithm:  iam.SigningAlgorithmES256,
		SigningKeyManager: global,
		TenantSigningKeys: resolver,
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   24 * time.Hour,
		IDTokenTTL:        time.Hour,
		AuthCodeTTL:       10 * time.Minute,
		PKCERequired:      true,
		AllowedMethods:    []string{"S256"},
	}, iam.OIDCDependencies{
		IdentityStore: iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: iam.NewMemoryStorage(), RoleStorage: backend}),
		AppStorage:    backend,
		TokenStorage:  backend,
	})
	require.NoError(t, err)
	s := &Server{config: &config.Config{}, quicBaaSService: &grpcOIDCGetterService{provider: provider}}
	validator, err := s.buildPlatformValidator(context.Background())
	require.NoError(t, err)
	return &platformBearerNode{global: global, resolver: resolver, validator: validator}
}

// manager returns the node's signing key manager for tenantID.
func (n *platformBearerNode) manager(t *testing.T, tenantID string) *iam.SigningKeyManager {
	t.Helper()
	m, err := n.resolver.ManagerFor(context.Background(), tenantID)
	require.NoError(t, err)
	return m
}

// signPlatformAccessToken signs an RFC 9068 access token for the bearer path
// with signer under kid.
func signPlatformAccessToken(t *testing.T, signer crypto.Signer, kid string) string {
	t.Helper()
	tok, err := dbxkms.JWTSignWithSignerKIDTyp(signer, dbxkms.JWTClaims{
		"iss": platformBearerIssuer,
		"aud": platformBearerIssuer,
		"sub": "user-on-the-bearer-path",
		"exp": time.Now().Add(2 * time.Hour).Unix(),
		"iat": time.Now().Unix(),
	}, iam.SigningAlgorithmES256, kid, iam.AccessTokenJWTType)
	require.NoError(t, err)
	return tok
}

// TestPlatformValidator_AdoptedKeyStopsAtCutoff proves the bearer path
// enforces the adopted pre-upgrade key's cutoff at lookup: each node's
// platform validator accepts a token L signed at the cutoff minus epsilon and
// rejects the same token at the cutoff plus epsilon, with no restart and no
// lookup of any other kid in between, for the global and a dedicated tenant
// key.
func TestPlatformValidator_AdoptedKeyStopsAtCutoff(t *testing.T) {
	t.Cleanup(dbxkms.Reset)
	const epsilon = time.Millisecond
	for name, tenantID := range map[string]string{"global key": "", "dedicated tenant key": "tenant-dedicated"} {
		t.Run(name, func(t *testing.T) {
			clock := useAdoptionClock(t)
			cutoff := clock.Now().Add(oidcSignedTokenLifetime(&config.AuthConfig{}))
			initOIDCSigningDefault(t)
			legacy, err := iam.NewSigningKeyManagerFromXKMS(iam.OIDCSigningKeyCN, tenantID)
			require.NoError(t, err)
			lKid, lSigner, _ := legacy.Current()
			registerOIDCSigningBackend(t, xkmssdk.NewStorageMemory())

			a, peer := newPlatformBearerNode(t, tenantID), newPlatformBearerNode(t, tenantID)
			mintTenant := tenantID
			if mintTenant == "" {
				mintTenant = "ordinary-tenant"
			}
			require.NoError(t, a.manager(t, mintTenant).Rotate())
			require.NoError(t, peer.manager(t, mintTenant).Refresh())
			token := signPlatformAccessToken(t, lSigner, lKid)

			nodes := map[string]*platformBearerNode{"a": a, "peer": peer}
			clock.set(cutoff.Add(-epsilon))
			for side, n := range nodes {
				_, err := n.validator.ValidatePlatformToken(context.Background(), token)
				require.NoError(t, err, "%s at the cutoff minus epsilon", side)
			}

			clock.set(cutoff.Add(epsilon))
			for side, n := range nodes {
				_, err := n.validator.ValidatePlatformToken(context.Background(), token)
				var unknown *platformtoken.UnknownKeyIDError
				require.ErrorAs(t, err, &unknown, "%s at the cutoff plus epsilon", side)
				require.Equal(t, lKid, unknown.KID, side)
			}
		})
	}
}
