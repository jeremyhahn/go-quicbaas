package iam

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// sharedSigningBackend is the registered backend instance the two nodes of
// these tests share; its versions are recorded, as on a cluster node.
const sharedSigningBackend = "shared-signing"

// countingStorage counts the reads of the key-version records and, while a
// gate is set, holds each read until the gate is closed.
type countingStorage struct {
	xkmssdk.StorageBackend
	gets atomic.Int64
	gate atomic.Pointer[chan struct{}]
}

// Get counts the read, waits on any gate, and delegates it.
func (s *countingStorage) Get(ctx context.Context, key string) ([]byte, error) {
	s.gets.Add(1)
	if gate := s.gate.Load(); gate != nil {
		<-*gate
	}
	return s.StorageBackend.Get(ctx, key)
}

// setupSharedSigningXKMS initializes xkms with a default backend and a
// second, shared backend instance whose key versions are recorded in a
// store whose reads are counted.
func setupSharedSigningXKMS(t *testing.T) *countingStorage {
	t.Helper()
	xkms.Reset()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	if err != nil {
		t.Fatalf("logger: %v", err)
	}
	backend := func() xkmssdk.ServiceBackend {
		keyStorage, err := xkmssdk.NewMemoryBackend()
		if err != nil {
			t.Fatalf("key storage: %v", err)
		}
		software, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{KeyStorage: keyStorage})
		if err != nil {
			t.Fatalf("software backend: %v", err)
		}
		ks, err := xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{
			Backend:     software,
			CertStorage: xkmssdk.NewStorageMemory(),
		})
		if err != nil {
			t.Fatalf("service backend: %v", err)
		}
		return ks
	}
	records := &countingStorage{StorageBackend: xkmssdk.NewStorageMemory()}
	keyVersions, err := xkmssdk.NewKeyVersionStore(records)
	if err != nil {
		t.Fatalf("key version store: %v", err)
	}
	if err := xkms.Initialize(&xkms.Config{
		DefaultBackend: "software",
		Backends: map[string]xkmssdk.ServiceBackend{
			"software":           backend(),
			sharedSigningBackend: backend(),
		},
		KeyVersions: keyVersions,
		Logger:      logger.NewAdapter(log),
	}); err != nil {
		t.Fatalf("xkms: %v", err)
	}
	t.Cleanup(xkms.Reset)
	return records
}

// sharedSigningNode is one node's OIDC provider over the shared backend.
type sharedSigningNode struct {
	global   *SigningKeyManager
	resolver *TenantSigningKeyResolver
	provider *OIDCProvider
}

// newSharedSigningNode builds a node's signing key managers and provider on
// the shared backend, with tenantID (when not "") holding a dedicated key.
func newSharedSigningNode(t *testing.T, tenantID string) *sharedSigningNode {
	t.Helper()
	global, err := NewSigningKeyManagerFromXKMSBackend(OIDCSigningKeyCN, "", sharedSigningBackend)
	if err != nil {
		t.Fatalf("global signing key manager: %v", err)
	}
	resolver, err := NewTenantSigningKeyResolver(global, &fakeOIDCSettings{dedicated: map[string]bool{tenantID: tenantID != ""}})
	if err != nil {
		t.Fatalf("resolver: %v", err)
	}
	memStorage := NewMemoryStorage()
	provider, err := NewOIDCProvider(&OIDCConfig{
		Issuer:            "https://issuer.test",
		SigningAlgorithm:  SigningAlgorithmES256,
		SigningKeyManager: global,
		TenantSigningKeys: resolver,
		AccessTokenTTL:    time.Hour,
		RefreshTokenTTL:   time.Hour,
		IDTokenTTL:        time.Hour,
		AuthCodeTTL:       10 * time.Minute,
		PKCERequired:      true,
		AllowedMethods:    []string{CodeChallengeMethodS256},
	}, OIDCDependencies{
		IdentityStore: NewIdentityStore(IdentityStoreOptions{UserStorage: memStorage, RoleStorage: memStorage}),
		AppStorage:    newMockAppStorage(),
		TokenStorage:  newMockTokenStorage(),
	})
	if err != nil {
		t.Fatalf("NewOIDCProvider: %v", err)
	}
	return &sharedSigningNode{global: global, resolver: resolver, provider: provider}
}

// manager returns the node's signing key manager for tenantID.
func (n *sharedSigningNode) manager(t *testing.T, tenantID string) *SigningKeyManager {
	t.Helper()
	m, err := n.resolver.ManagerFor(context.Background(), tenantID)
	if err != nil {
		t.Fatalf("ManagerFor(%q): %v", tenantID, err)
	}
	return m
}

// mint issues an access token for tenantID on the node.
func (n *sharedSigningNode) mint(t *testing.T, tenantID string) string {
	t.Helper()
	now := time.Now()
	tok, err := n.provider.tokenService.GenerateAccessToken(context.Background(), &Claims{
		Issuer:    "https://issuer.test",
		Subject:   "user-" + tenantID,
		Audience:  []string{"https://issuer.test"},
		ExpiresAt: NewUnixTime(now.Add(time.Hour)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       mustGenerateJTI(t),
		TenantID:  tenantID,
	})
	if err != nil {
		t.Fatalf("GenerateAccessToken(%q): %v", tenantID, err)
	}
	return tok
}

// TestOIDCProvider_VerifiesPeerRotationAtOnce proves a kid the per-tenant
// aggregate does not publish goes through the refresh-aware lookup before
// it is unknown: node B has just refreshed, node A rotates and issues a
// token with the new key, and B verifies it at once, for the global key and
// for a dedicated tenant key.
func TestOIDCProvider_VerifiesPeerRotationAtOnce(t *testing.T) {
	for name, tenantID := range map[string]string{"global key": "", "dedicated tenant key": "dedicated-tenant"} {
		t.Run(name, func(t *testing.T) {
			setupSharedSigningXKMS(t)
			a := newSharedSigningNode(t, tenantID)
			b := newSharedSigningNode(t, tenantID)
			mintTenant := tenantID
			if mintTenant == "" {
				mintTenant = "ordinary-tenant"
			}
			mA, mB := a.manager(t, mintTenant), b.manager(t, mintTenant)
			if err := mB.Refresh(); err != nil {
				t.Fatalf("B refresh: %v", err)
			}
			b.resolver.JWKS(context.Background())

			if err := mA.Rotate(); err != nil {
				t.Fatalf("A rotate: %v", err)
			}
			tok := a.mint(t, mintTenant)
			kid, _ := xkms.JWTExtractKID(tok)
			if aKid, _, _ := mA.Current(); kid != aKid {
				t.Fatalf("A signed with %q, want its rotated key %q", kid, aKid)
			}
			claims, err := b.provider.parseVerifiedAccessTokenClaims(tok)
			if err != nil {
				t.Fatalf("B verifying A's freshly rotated token: %v", err)
			}
			if claims.TenantID != mintTenant {
				t.Fatalf("tenant %q, want %q", claims.TenantID, mintTenant)
			}
		})
	}
}

// TestOIDCProvider_UnknownKIDFloodIsRateLimited proves a flood of tokens
// naming kids no key holds re-reads the key-version records only at the
// rate-limited pace, and each still fails with UnknownKeyIDError.
func TestOIDCProvider_UnknownKIDFloodIsRateLimited(t *testing.T) {
	records := setupSharedSigningXKMS(t)
	node := newSharedSigningNode(t, "dedicated-tenant")
	node.manager(t, "dedicated-tenant")
	signer := node.global
	_, globalSigner, _ := signer.Current()

	// One refresh of every manager is the unit a rate-limited refresh costs.
	records.gets.Store(0)
	for _, m := range []*SigningKeyManager{node.global, node.manager(t, "dedicated-tenant")} {
		if err := m.Refresh(); err != nil {
			t.Fatalf("refresh: %v", err)
		}
	}
	perRefresh := records.gets.Load()
	if perRefresh == 0 {
		t.Fatal("a refresh reads no version record")
	}

	const flood = 500
	records.gets.Store(0)
	start := time.Now()
	for i := range flood {
		kid := "unknown-kid-" + time.Duration(i).String()
		tok, err := xkms.JWTSignWithSignerKID(globalSigner, xkms.JWTClaims{
			"sub": "flood",
			"exp": time.Now().Add(time.Hour).Unix(),
		}, SigningAlgorithmES256, kid)
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		_, err = node.provider.resolveVerificationKey(tok)
		var unknown *UnknownKeyIDError
		if !errors.As(err, &unknown) || unknown.KID != kid {
			t.Fatalf("kid %q: got %v, want UnknownKeyIDError", kid, err)
		}
	}
	elapsed := time.Since(start)
	// Each manager refreshes at most once per unknown-kid interval and once
	// per routine interval.
	windows := int64(elapsed/unknownKIDRefreshInterval) + 1 + int64(elapsed/signingKeyRefreshInterval) + 1
	if got, limit := records.gets.Load(), perRefresh*windows; got > limit {
		t.Fatalf("%d unknown kids in %v read the version records %d times, want at most %d", flood, elapsed, got, limit)
	}
}

// TestSigningKeyManager_ConcurrentMissesShareTheInFlightRefresh proves
// concurrent lookups of a peer's freshly rotated kid wait on the one
// in-flight unknown-kid refresh rather than failing while it runs: with the
// version-record read held, every lookup succeeds once it completes, the
// records are read for one refresh only, and a later miss stays
// rate-limited.
func TestSigningKeyManager_ConcurrentMissesShareTheInFlightRefresh(t *testing.T) {
	records := setupSharedSigningXKMS(t)
	a, b := newSharedSigningNode(t, ""), newSharedSigningNode(t, "")
	records.gets.Store(0)
	if err := b.global.Refresh(); err != nil {
		t.Fatalf("B refresh: %v", err)
	}
	perRefresh := records.gets.Load()
	if perRefresh == 0 {
		t.Fatal("a refresh reads no version record")
	}
	if err := a.global.Rotate(); err != nil {
		t.Fatalf("A rotate: %v", err)
	}
	kid, _, _ := a.global.Current()

	gate := make(chan struct{})
	records.gets.Store(0)
	records.gate.Store(&gate)
	const lookups = 16
	var started sync.WaitGroup
	started.Add(lookups)
	errs := make(chan error, lookups)
	for range lookups {
		go func() {
			started.Done()
			_, err := b.global.PublicForKID(kid)
			errs <- err
		}()
	}
	started.Wait()
	for records.gets.Load() == 0 {
		runtime.Gosched()
	}
	// Give the other lookups time to reach the in-flight refresh.
	time.Sleep(20 * time.Millisecond)
	records.gate.Store(nil)
	close(gate)
	for range lookups {
		if err := <-errs; err != nil {
			t.Errorf("lookup during the in-flight refresh: %v", err)
		}
	}
	if got := records.gets.Load(); got != perRefresh {
		t.Fatalf("%d concurrent lookups read the version records %d times, want one refresh's %d", lookups, got, perRefresh)
	}

	_, err := b.global.PublicForKID("never-issued-kid")
	var unknown *UnknownKeyIDError
	if !errors.As(err, &unknown) {
		t.Fatalf("later miss: got %v, want UnknownKeyIDError", err)
	}
	if got := records.gets.Load(); got != perRefresh {
		t.Fatalf("a miss right after the refresh read the version records again (%d reads, want %d)", got, perRefresh)
	}
}
