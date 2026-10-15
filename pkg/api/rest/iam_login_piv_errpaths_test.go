//go:build rest

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

package rest

import (
	"context"
	cryptorand "crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// pivStoreErrService implements GetIdentityStore but returns a (nil, error)
// pair, driving PIVLogin's "identity store not configured" fail-closed branch
// that fires before any tenant derivation or user lookup.
type pivStoreErrService struct {
	*testutil.MockQuicBaaS
}

func (m *pivStoreErrService) GetIdentityStore(_ context.Context) (*iam.IdentityStore, error) {
	return nil, errors.New("store backend down")
}

// A PIV login attempt fails closed with a 500 and mints no token when the
// identity store getter returns an error. Guards the store-resolution failure
// branch: a broken backend must never fall through to token issuance.
func TestPIVLogin_IdentityStoreError(t *testing.T) {
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	mockSvc := &pivStoreErrService{MockQuicBaaS: testutil.NewMockQuicBaaS()}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	cfg.PIVClientCert.Enabled = true
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	defer server.Close()

	ca, caKey := pivLoginTestCA(t, "PIV Root CA")
	leaf := pivLoginTestLeaf(t, ca, caKey)
	w := httptest.NewRecorder()
	handler.PIVLogin(w, buildRequestWithPeerCert(leaf))

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "identity store not configured")
	assert.NotContains(t, w.Body.String(), "access_token")
}

// pivNoOIDCGetterService resolves a real identity store (so a user can be
// looked up by PIV thumbprint) but deliberately omits GetOIDCProvider so the
// type-assertion in PIVLogin fails, exercising the "OIDC provider not
// available" branch. GetAuditLogger is provided so the failure-audit path is
// non-nil.
type pivNoOIDCGetterService struct {
	*testutil.MockQuicBaaS
	store       *iam.IdentityStore
	auditLogger iam.AuditLogger
}

func (m *pivNoOIDCGetterService) GetIdentityStore(_ context.Context) (*iam.IdentityStore, error) {
	return m.store, nil
}

func (m *pivNoOIDCGetterService) GetAuditLogger(_ context.Context) (iam.AuditLogger, error) {
	return m.auditLogger, nil
}

// A resolvable PIV user whose service exposes no OIDC provider getter fails
// closed with a 500 and mints no token. Guards the OIDC-getter type-assertion
// branch, distinct from the nil-provider branch covered elsewhere.
func TestPIVLogin_OIDCGetterUnavailable(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "PIV Root CA")
	leaf := pivSpiffeLeaf(t, ca, caKey, "tenant-piv")

	memStorage := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: memStorage,
		Tenants:     &testTenantManager{storage: memStorage},
	})
	thumb := sha256.Sum256(leaf.Raw)
	pivUser := &iam.User{ID: 78, TenantID: "tenant-piv", Email: "piv@example.com"}
	pivUser.SetPIVThumbprint(hex.EncodeToString(thumb[:]))
	require.NoError(t, memStorage.CreateUser(context.Background(), pivUser))

	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	mockSvc := &pivNoOIDCGetterService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		store:        store,
		auditLogger:  &captureAuditLogger{},
	}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	cfg.SPIFFETrustDomain = pivTrustDomain
	cfg.PIVClientCert.Enabled = true
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	defer server.Close()

	w := httptest.NewRecorder()
	handler.PIVLogin(w, pivRequestWithSpiffe(leaf))

	require.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "OIDC provider not available")
	assert.NotContains(t, w.Body.String(), "access_token")
}

// pivFailingTokenStorage embeds a real TokenStorage but fails every StoreToken
// call, simulating a token-store write outage during hardware-token issuance.
type pivFailingTokenStorage struct {
	iam.TokenStorage
}

func (s *pivFailingTokenStorage) StoreToken(_ context.Context, _ *iam.Token) error {
	return errors.New("token store unavailable")
}

// A resolvable PIV user whose OIDC token issuance fails (token-store write
// error) fails closed with a 500, mints no token, and emits a failure audit
// event. Guards the post-lookup token-issuance error branch — a storage outage
// must never leak a partially-issued credential.
func TestPIVLogin_TokenIssuanceFailure(t *testing.T) {
	ca, caKey := pivLoginTestCA(t, "PIV Root CA")
	leaf := pivSpiffeLeaf(t, ca, caKey, "tenant-piv")
	vendorCA, vendorKey := pivLoginTestCA(t, "Yubico PIV Attestation Root")
	attestation := pivSlotAttestationFor(t, leaf, vendorCA, vendorKey)

	memStorage := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{
		UserStorage: memStorage,
		RoleStorage: memStorage,
		Tenants:     &testTenantManager{storage: memStorage},
	})
	thumb := sha256.Sum256(leaf.Raw)
	pivUser := &iam.User{ID: 78, TenantID: "tenant-piv", Email: "piv@example.com"}
	pivUser.SetPIVThumbprint(hex.EncodeToString(thumb[:]))
	require.NoError(t, memStorage.CreateUser(context.Background(), pivUser))

	privateKey, err := rsa.GenerateKey(cryptorand.Reader, 2048)
	require.NoError(t, err)
	provider, err := iam.NewOIDCProvider(&iam.OIDCConfig{
		Issuer:            "https://" + pivTrustDomain,
		SigningAlgorithm:  "RS256",
		SigningKeyManager: testOIDCKeyManager(t, privateKey),
		AccessTokenTTL:    15 * time.Minute,
		RefreshTokenTTL:   24 * time.Hour,
		IDTokenTTL:        15 * time.Minute,
		AuthCodeTTL:       10 * time.Minute,
	}, iam.OIDCDependencies{
		IdentityStore: store,
		AppStorage:    iam.NewBackendStorage(newIntrospectMemKV()),
		TokenStorage:  &pivFailingTokenStorage{TokenStorage: iam.NewBackendStorage(newIntrospectMemKV())},
	})
	require.NoError(t, err)

	audit := &captureAuditLogger{}
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	mockSvc := &pivFullMockService{
		MockQuicBaaS: testutil.NewMockQuicBaaS(),
		store:        store,
		provider:     provider,
		auditLogger:  audit,
	}
	cfg := DefaultConfig()
	cfg.Address = ":0"
	cfg.SPIFFETrustDomain = pivTrustDomain
	cfg.PIVClientCert.Enabled = true
	server, err := NewServer(mockSvc.MockQuicBaaS, cfg, log)
	require.NoError(t, err)
	server.service = mockSvc
	handler := NewIAMHandler(IAMHandlerDependencies{Server: server})
	defer server.Close()
	// The certificate must clear the attestation gate — with a real slot
	// attestation, since the login certificate is never its own evidence — so
	// the failure under test is the token-store outage rather than an unproven
	// credential.
	trustPIVAttestationCA(t, handler, vendorCA)

	w := httptest.NewRecorder()
	handler.PIVLogin(w, pivRequestWithChain(leaf, attestation))

	// A token-store write outage surfaces as a 503 (qrdb storage error maps to
	// Service Unavailable); the contract is fail-closed, so assert a server
	// error and that no credential leaked.
	assert.GreaterOrEqual(t, w.Code, 500)
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
	assert.NotContains(t, w.Body.String(), "access_token")

	require.NotEmpty(t, audit.events)
	last := audit.events[len(audit.events)-1]
	assert.Equal(t, "auth.login.piv.failure", last.Action)
}
