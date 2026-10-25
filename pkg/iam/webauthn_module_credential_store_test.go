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

package iam

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

const testModuleTenant = "system"

// newBoundModuleStore returns a late-bound store whose factory is already
// resolvable, wrapping xkms's real in-memory credential store, plus the pieces
// a test asserts against.
func newBoundModuleStore(t *testing.T) (*lateBoundWebAuthnFactorStore, *fakeUserAuthFactorClient, xkmswebauthn.UserStore, xkmssdk.WebAuthnCredentialStore) {
	t.Helper()
	factors := &fakeUserAuthFactorClient{}
	users, inner := newTestWebAuthnStores(t)
	store := &lateBoundWebAuthnFactorStore{
		WebAuthnCredentialStore: inner,
		tenantID:                testModuleTenant,
		build: func(in xkmssdk.WebAuthnCredentialStore) (*webAuthnFactorWriteThrough, error) {
			return newWebAuthnFactorWriteThrough(in, users, factors, recorderOf(factors), testModuleTenant, permissiveTestGate(), slog.Default())
		},
		logger: slog.Default(),
	}
	return store, factors, users, inner
}

// newUnboundModuleStore returns a late-bound store whose factory never
// resolves — the state a composed node is in between qrdb node construction
// and IAM initialisation.
func newUnboundModuleStore(t *testing.T) (*lateBoundWebAuthnFactorStore, xkmssdk.WebAuthnCredentialStore) {
	t.Helper()
	inner := newTestWebAuthnCredentialStore(t)
	store := &lateBoundWebAuthnFactorStore{
		WebAuthnCredentialStore: inner,
		tenantID:                testModuleTenant,
		build: func(xkmssdk.WebAuthnCredentialStore) (*webAuthnFactorWriteThrough, error) {
			return nil, &WebAuthnFactorStoreUnboundError{TenantID: testModuleTenant}
		},
		logger: slog.Default(),
	}
	return store, inner
}

// =============================================================================
// Success path — the defect this file closes
// =============================================================================

// TestModuleStoreSave_EnrollsLoginFactor is the headline regression guard for
// go-quicbaas#484: a registration served by the composed go-xkms module's own
// ceremony routes must enroll the qrdb fido2 factor the unified login ceremony
// resolves, exactly as one served by quicbaas's own surface does.
func TestModuleStoreSave_EnrollsLoginFactor(t *testing.T) {
	store, factors, users, inner := newBoundModuleStore(t)
	seedUser(t, users, 42, "burnin@example.com", "xkey")

	credID := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	if err := store.Save(context.Background(), testCredential(42, credID)); err != nil {
		t.Fatalf("Save: %v", err)
	}

	if len(factors.added) != 1 {
		t.Fatalf("expected exactly one enrolled factor, got %d", len(factors.added))
	}
	added := factors.added[0]
	if added.Type != string(qrdbtransport.FactorTypeFIDO2) {
		t.Errorf("factor type = %q, want %q", added.Type, qrdbtransport.FactorTypeFIDO2)
	}
	if added.UserID != 42 {
		t.Errorf("factor user id = %d, want 42", added.UserID)
	}
	want := webAuthnCredentialLookupKey(credID)
	if string(added.Credential) != want {
		t.Errorf("factor credential = %q, want the base64url credential id %q", added.Credential, want)
	}

	// The credential itself still lands in the store go-xkms built: the
	// decoration adds a side effect, it does not divert the write.
	if _, err := inner.GetByCredentialID(context.Background(), credID); err != nil {
		t.Fatalf("credential not persisted to the wrapped store: %v", err)
	}
}

// TestModuleStoreSave_ReusesOneWriteThrough proves the write-through is built
// once and cached: rebuilding it per registration would re-derive the qrdb
// store adapters on every ceremony.
func TestModuleStoreSave_ReusesOneWriteThrough(t *testing.T) {
	store, _, users, _ := newBoundModuleStore(t)
	seedUser(t, users, 7, "reuse@example.com", "xkey")
	builds := 0
	inner := store.WebAuthnCredentialStore
	prev := store.build
	store.build = func(in xkmssdk.WebAuthnCredentialStore) (*webAuthnFactorWriteThrough, error) {
		builds++
		return prev(in)
	}

	for i := 0; i < 3; i++ {
		if err := store.Save(context.Background(), testCredential(7, []byte{byte(i + 1)})); err != nil {
			t.Fatalf("Save %d: %v", i, err)
		}
	}
	if builds != 1 {
		t.Errorf("write-through built %d times, want 1", builds)
	}
	if _, err := inner.GetByCredentialID(context.Background(), []byte{3}); err != nil {
		t.Fatalf("third credential not persisted: %v", err)
	}
}

// TestModuleStoreConcurrentSave_BindsOnce exercises the lock-free binding under
// concurrent first registrations: every Save must succeed and exactly one
// write-through must win the CAS.
func TestModuleStoreConcurrentSave_BindsOnce(t *testing.T) {
	store, factors, users, _ := newBoundModuleStore(t)
	seedUser(t, users, 9, "race@example.com", "xkey")

	var mu sync.Mutex
	guarded := &serializingFactorClient{inner: factors, mu: &mu}
	prev := store.build
	store.build = func(in xkmssdk.WebAuthnCredentialStore) (*webAuthnFactorWriteThrough, error) {
		wt, err := prev(in)
		if err != nil {
			return nil, err
		}
		wt.factors = guarded
		return wt, nil
	}

	const n = 8
	errs := make(chan error, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs <- store.Save(context.Background(), testCredential(9, []byte{byte(i + 1)}))
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent Save: %v", err)
		}
	}
	if store.resolved.Load() == nil {
		t.Fatal("write-through was never bound")
	}
	mu.Lock()
	got := len(factors.addedRequests())
	mu.Unlock()
	if got != n {
		t.Errorf("enrolled %d factors, want %d", got, n)
	}
}

// serializingFactorClient serialises the fake factor client so the concurrency
// test measures the store's binding, not an unsynchronised test double.
type serializingFactorClient struct {
	inner *fakeUserAuthFactorClient
	mu    *sync.Mutex
}

func (c *serializingFactorClient) AddAttested(ctx context.Context, userID uint64, factor *qrdbsdk.AuthFactor, attestation qrdbsdk.VerifiedAttestation) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inner.AddAttested(ctx, userID, factor, attestation)
}

func (c *serializingFactorClient) RotateAttested(ctx context.Context, oldID string, factor *qrdbsdk.AuthFactor, attestation qrdbsdk.VerifiedAttestation) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inner.RotateAttested(ctx, oldID, factor, attestation)
}

func (c *serializingFactorClient) AddAuthFactor(ctx context.Context, req *qrdbtransport.AuthFactorAddRequest) (*qrdbtransport.AuthFactorAddResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.inner.AddAuthFactor(ctx, req)
}

func (c *serializingFactorClient) ListAuthFactors(ctx context.Context, req *qrdbtransport.AuthFactorListRequest) (*qrdbtransport.AuthFactorListResponse, error) {
	return c.inner.ListAuthFactors(ctx, req)
}

func (c *serializingFactorClient) GetAuthFactor(ctx context.Context, req *qrdbtransport.AuthFactorGetRequest) (*qrdbtransport.AuthFactorGetResponse, error) {
	return c.inner.GetAuthFactor(ctx, req)
}

func (c *serializingFactorClient) RemoveAuthFactor(ctx context.Context, req *qrdbtransport.AuthFactorRemoveRequest) (*qrdbtransport.AuthFactorRemoveResponse, error) {
	return c.inner.RemoveAuthFactor(ctx, req)
}

func (c *serializingFactorClient) DeleteAuthFactor(ctx context.Context, req *qrdbtransport.AuthFactorDeleteRequest) (*qrdbtransport.AuthFactorDeleteResponse, error) {
	return c.inner.DeleteAuthFactor(ctx, req)
}

func (c *serializingFactorClient) RotateAuthFactor(ctx context.Context, req *qrdbtransport.AuthFactorRotateRequest) (*qrdbtransport.AuthFactorRotateResponse, error) {
	return c.inner.RotateAuthFactor(ctx, req)
}

func (c *serializingFactorClient) RelabelAuthFactor(ctx context.Context, req *qrdbtransport.AuthFactorRelabelRequest) (*qrdbtransport.AuthFactorRelabelResponse, error) {
	return c.inner.RelabelAuthFactor(ctx, req)
}

func (c *serializingFactorClient) LoginAuthFactor(ctx context.Context, req *qrdbtransport.AuthFactorLoginRequest) (*qrdbtransport.AuthFactorLoginResponse, error) {
	return c.inner.LoginAuthFactor(ctx, req)
}

// =============================================================================
// Fail-closed path — the thing that must never degrade silently
// =============================================================================

// TestModuleStoreSave_UnboundFactoryPersistsNothing is the invariant that makes
// the whole decoration safe: a registration that cannot enroll its login factor
// must be REFUSED, not silently written to the credential store. Persisting it
// would reproduce go-quicbaas#484 exactly — a passkey that registers and can
// never complete a login.
func TestModuleStoreSave_UnboundFactoryPersistsNothing(t *testing.T) {
	store, inner := newUnboundModuleStore(t)
	credID := []byte{9, 9, 9, 9}

	err := store.Save(context.Background(), testCredential(1, credID))
	if err == nil {
		t.Fatal("Save succeeded with an unbound relying-party factory; it must fail closed")
	}
	var unbound *WebAuthnFactorStoreUnboundError
	if !errors.As(err, &unbound) {
		t.Fatalf("error = %T (%v), want *WebAuthnFactorStoreUnboundError", err, err)
	}
	if unbound.TenantID != testModuleTenant {
		t.Errorf("error tenant = %q, want %q", unbound.TenantID, testModuleTenant)
	}
	if _, getErr := inner.GetByCredentialID(context.Background(), credID); !errors.Is(getErr, xkmswebauthn.ErrCredentialNotFound) {
		t.Fatalf("credential was persisted despite the refusal (lookup err = %v)", getErr)
	}
}

// TestModuleStoreDelete_UnboundFactoryDeletesNothing covers the mirror
// invariant: a credential must not be stripped while its login factor cannot be
// revoked, which would strand a factor resolving at login to a passkey that no
// longer exists.
func TestModuleStoreDelete_UnboundFactoryDeletesNothing(t *testing.T) {
	store, inner := newUnboundModuleStore(t)
	credID := []byte{4, 5, 6}
	if err := inner.Save(context.Background(), testCredential(3, credID)); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	var unbound *WebAuthnFactorStoreUnboundError
	if err := store.Delete(context.Background(), credID); !errors.As(err, &unbound) {
		t.Fatalf("Delete error = %v, want *WebAuthnFactorStoreUnboundError", err)
	}
	if err := store.DeleteByUserID(context.Background(), testUserHandle(3)); !errors.As(err, &unbound) {
		t.Fatalf("DeleteByUserID error = %v, want *WebAuthnFactorStoreUnboundError", err)
	}
	if _, err := inner.GetByCredentialID(context.Background(), credID); err != nil {
		t.Fatalf("credential was deleted despite the refusal: %v", err)
	}
}

// TestModuleStoreReads_DelegateWhileUnbound confirms the refusal is scoped to
// mutations: an unbound factory must not break the assertion ceremony, which
// only reads. A login/begin that could not list credentials would turn a
// registration-plane defect into a login-plane outage.
func TestModuleStoreReads_DelegateWhileUnbound(t *testing.T) {
	store, inner := newUnboundModuleStore(t)
	credID := []byte{7, 7}
	cred := testCredential(11, credID)
	if err := inner.Save(context.Background(), cred); err != nil {
		t.Fatalf("seed credential: %v", err)
	}

	got, err := store.GetByCredentialID(context.Background(), credID)
	if err != nil {
		t.Fatalf("GetByCredentialID: %v", err)
	}
	if string(got.ID) != string(credID) {
		t.Errorf("read credential id = %x, want %x", got.ID, credID)
	}
	list, err := store.GetByUserID(context.Background(), testUserHandle(11))
	if err != nil {
		t.Fatalf("GetByUserID: %v", err)
	}
	if len(list) != 1 {
		t.Errorf("GetByUserID returned %d credentials, want 1", len(list))
	}
}

// =============================================================================
// Constructor
// =============================================================================

// TestNewWebAuthnModuleCredentialStoreDecorator_Decorates confirms the
// constructor returns a decorator that actually wraps — the composed node's
// whole fix depends on the returned store not being the one passed in.
func TestNewWebAuthnModuleCredentialStoreDecorator_Decorates(t *testing.T) {
	decorate, err := NewWebAuthnModuleCredentialStoreDecorator(
		testModuleTenant,
		func() *WebAuthnServiceFactory { return nil },
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("NewWebAuthnModuleCredentialStoreDecorator: %v", err)
	}
	inner := newTestWebAuthnCredentialStore(t)
	decorated := decorate(inner)
	if decorated == nil {
		t.Fatal("decorator returned nil for a valid store")
	}
	store, ok := decorated.(*lateBoundWebAuthnFactorStore)
	if !ok {
		t.Fatalf("decorated store = %T, want *lateBoundWebAuthnFactorStore", decorated)
	}
	if store.tenantID != testModuleTenant {
		t.Errorf("tenant = %q, want %q", store.tenantID, testModuleTenant)
	}

	// A nil resolver stands in for IAM never initialising a WebAuthn
	// surface: the ceremony is refused rather than served undecorated.
	var unbound *WebAuthnFactorStoreUnboundError
	if err := store.Save(context.Background(), testCredential(1, []byte{1})); !errors.As(err, &unbound) {
		t.Fatalf("Save error = %v, want *WebAuthnFactorStoreUnboundError", err)
	}
}

// TestNewWebAuthnModuleCredentialStoreDecorator_RejectsInvalidConfig covers the
// failures that ARE knowable at composition time, which must stop the daemon
// rather than yield a decorator that refuses every ceremony at runtime.
func TestNewWebAuthnModuleCredentialStoreDecorator_RejectsInvalidConfig(t *testing.T) {
	resolve := func() *WebAuthnServiceFactory { return nil }
	tests := []struct {
		name     string
		tenantID string
		resolve  WebAuthnServiceFactoryResolver
		field    string
	}{
		{"empty tenant", "", resolve, "tenantID"},
		{"nil resolver", testModuleTenant, nil, "WebAuthnServiceFactoryResolver"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewWebAuthnModuleCredentialStoreDecorator(tc.tenantID, tc.resolve, slog.Default())
			var cfgErr *WebAuthnFactorStoreConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("error = %v, want *WebAuthnFactorStoreConfigError", err)
			}
			if cfgErr.Field != tc.field {
				t.Errorf("field = %q, want %q", cfgErr.Field, tc.field)
			}
		})
	}
}

// TestWebAuthnModuleDecorator_NilStoreLeavesLaneUnserved pins the disposition
// for a store that cannot be wrapped: returning nil makes go-xkms fail its
// WebAuthn construction and leave the routes unmounted. Returning the
// undecorated store instead would serve registrations that enroll no login
// factor — the defect, restored.
func TestWebAuthnModuleDecorator_NilStoreLeavesLaneUnserved(t *testing.T) {
	decorate, err := NewWebAuthnModuleCredentialStoreDecorator(
		testModuleTenant,
		func() *WebAuthnServiceFactory { return nil },
		slog.Default(),
	)
	if err != nil {
		t.Fatalf("NewWebAuthnModuleCredentialStoreDecorator: %v", err)
	}
	if got := decorate(nil); got != nil {
		t.Fatalf("decorator returned %T for a nil store, want nil so the lane stays unserved", got)
	}
}

// TestWebAuthnFactorStoreUnboundError_Message covers the typed error's own
// rendering, including the nil receiver a caller can reach through errors.As.
func TestWebAuthnFactorStoreUnboundError_Message(t *testing.T) {
	var nilErr *WebAuthnFactorStoreUnboundError
	if msg := nilErr.Error(); msg == "" {
		t.Error("nil receiver produced an empty message")
	}
	withTenant := (&WebAuthnFactorStoreUnboundError{TenantID: "acme"}).Error()
	if !strings.Contains(withTenant, "acme") {
		t.Errorf("message %q does not name the tenant", withTenant)
	}
	withoutTenant := (&WebAuthnFactorStoreUnboundError{}).Error()
	if strings.Contains(withoutTenant, "(tenant") {
		t.Errorf("message %q names a tenant it does not have", withoutTenant)
	}
}

// TestDecorateCredentialStore_RejectsANilStoreBeforeBuilding pins the
// argument check AND the shape of the failure. It must be refused before any
// qrdb adapter is derived — a doomed call should not construct stores it will
// throw away, and on this qrdb SDK version Client.Users() panics on a
// half-built client, so a late refusal is a panic mid-ceremony rather than an
// error. The returned store must also be a TRUE nil: DecorateCredentialStore's
// success value is a concrete pointer widened to an interface, and a nil
// pointer inside a non-nil interface reads as a usable store.
func TestDecorateCredentialStore_RejectsANilStoreBeforeBuilding(t *testing.T) {
	factory, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "localhost",
			RPDisplayName: "QuicBaaS",
			RPOrigins:     []string{"https://localhost:18443"},
		},
		QRDB:                dummyQRDBClient,
		AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
	})
	if err != nil {
		t.Fatalf("NewWebAuthnServiceFactory: %v", err)
	}

	store, err := factory.DecorateCredentialStore(nil, testModuleTenant)
	var cfgErr *WebAuthnFactorStoreConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("error = %v, want *WebAuthnFactorStoreConfigError", err)
	}
	if cfgErr.Field != "CredentialStore" {
		t.Errorf("field = %q, want %q", cfgErr.Field, "CredentialStore")
	}
	if store != nil {
		t.Fatalf("error return carried a non-nil interface (%T); it must be a true nil", store)
	}
}

// TestDecorateCredentialStore_RejectsAnEmptyTenant covers the other
// composition-time refusal: a tenant-less decorator would resolve the
// registrant in no tenant at all.
func TestDecorateCredentialStore_RejectsAnEmptyTenant(t *testing.T) {
	factory, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "localhost",
			RPDisplayName: "QuicBaaS",
			RPOrigins:     []string{"https://localhost:18443"},
		},
		QRDB:                dummyQRDBClient,
		AttestationRecorder: recorderOf(&fakeUserAuthFactorClient{}),
	})
	if err != nil {
		t.Fatalf("NewWebAuthnServiceFactory: %v", err)
	}

	store, err := factory.DecorateCredentialStore(newTestWebAuthnCredentialStore(t), "")
	var cfgErr *WebAuthnFactorStoreConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("error = %v, want *WebAuthnFactorStoreConfigError", err)
	}
	if cfgErr.Field != "tenantID" {
		t.Errorf("field = %q, want %q", cfgErr.Field, "tenantID")
	}
	if store != nil {
		t.Fatalf("error return carried a non-nil interface (%T)", store)
	}
}
