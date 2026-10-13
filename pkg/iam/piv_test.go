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

package iam

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"strconv"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// pivTestCA creates a self-signed CA certificate with CertSign usage.
func pivTestCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate CA key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "PIV Test Root"},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create CA cert: %v", err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse CA cert: %v", err)
	}
	return cert, key
}

// pivTestLeaf creates a leaf certificate signed by the given CA.
// When clientAuth is true the ExtKeyUsageClientAuth usage is included.
func pivTestLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, clientAuth bool) (*x509.Certificate, []byte) {
	t.Helper()
	leafKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate leaf key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: "PIV Test Client"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if clientAuth {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &leafKey.PublicKey, caKey)
	if err != nil {
		t.Fatalf("create leaf cert: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("parse leaf cert: %v", err)
	}
	return cert, pemBytes
}

var errPIVTestFactorNotFound = errors.New("piv test: factor not found")

// pivTestFactors is an in-memory qrdb user auth-factor store covering the
// calls CertLoginFactorEnroller and the login lookup make. Like qrdb's, it
// refuses a duplicate credential outright but resolves a lookup only within
// the context tenant.
//
// Remove revokes rather than deletes, and a revoked factor keeps its index
// entry, as qrdb's soft delete does: a lookup then reports ErrFactorRevoked.
type pivTestFactors struct {
	qrdbsdk.UserAuthFactorServicer
	mu        sync.Mutex
	byKey     map[string]*qrdbsdk.AuthFactor
	byID      map[string]*qrdbsdk.AuthFactor
	tenantOf  map[string]string
	nextID    int
	addErr    error
	removeErr error
}

func (f *pivTestFactors) Add(ctx context.Context, userID uint64, factor *qrdbsdk.AuthFactor) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.addErr != nil {
		return f.addErr
	}
	if _, exists := f.byKey[string(factor.Credential)]; exists {
		return qrdbsdk.ErrFactorAlreadyExists
	}
	f.nextID++
	factor.ID = "factor-" + strconv.Itoa(f.nextID)
	factor.UserID = userID
	stored := *factor
	f.byID[stored.ID] = &stored
	tenantID, _ := qrdbsdk.TenantFromContext(ctx)
	for _, key := range []string{string(factor.Credential), string(factor.SPKICredential)} {
		f.byKey[key] = &stored
		f.tenantOf[key] = tenantID
	}
	return nil
}

func (f *pivTestFactors) LookupByCredentialKey(ctx context.Context, _ qrdbsdk.AuthFactorType, key []byte) (*qrdbsdk.AuthFactor, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	tenantID, _ := qrdbsdk.TenantFromContext(ctx)
	factor, ok := f.byKey[string(key)]
	if !ok || f.tenantOf[string(key)] != tenantID {
		return nil, errPIVTestFactorNotFound
	}
	if factor.RevokedAt != nil {
		return nil, qrdbsdk.ErrFactorRevoked
	}
	copied := *factor
	return &copied, nil
}

func (f *pivTestFactors) Remove(ctx context.Context, factorID string, _ bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.removeErr != nil {
		return f.removeErr
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	factor, ok := f.byID[factorID]
	if !ok {
		return errPIVTestFactorNotFound
	}
	now := time.Now().UTC()
	factor.RevokedAt = &now
	return nil
}

// activeFactorIDs returns the ids of the factors that are not revoked.
func (f *pivTestFactors) activeFactorIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var ids []string
	for id, factor := range f.byID {
		if factor.RevokedAt == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

// newPIVTestFactorEnroller returns a login factor enroller over an in-memory
// factor store, and the store.
func newPIVTestFactorEnroller(t *testing.T) (*CertLoginFactorEnroller, *pivTestFactors) {
	t.Helper()
	factors := &pivTestFactors{
		byKey:    map[string]*qrdbsdk.AuthFactor{},
		byID:     map[string]*qrdbsdk.AuthFactor{},
		tenantOf: map[string]string{},
	}
	enroller, err := NewCertLoginFactorEnroller(factors)
	if err != nil {
		t.Fatalf("NewCertLoginFactorEnroller: %v", err)
	}
	return enroller, factors
}

// newPIVTestEnroller returns a login factor enroller over an in-memory store.
func newPIVTestEnroller(t *testing.T) *CertLoginFactorEnroller {
	t.Helper()
	enroller, _ := newPIVTestFactorEnroller(t)
	return enroller
}

// setupPIVTest creates a ready-to-use PIVActivator with a pending user and a valid invite token.
func setupPIVTest(t *testing.T) (activator *PIVActivator, token string, userStorage *mockUserStorage, inviteStore *fakeInviteStore) {
	t.Helper()

	userStorage = newMockUserStorage()
	inviteStore = newFakeInviteStore()

	// Create a pending user.
	user := &User{
		ID:        "101",
		TenantID:  "tenant-1",
		Email:     "piv@example.com",
		Lifecycle: LifecyclePending,
		Created:   time.Now(),
		Updated:   time.Now(),
	}
	if err := userStorage.CreateUser(context.Background(), user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	// Issue the invite through the store; the token is minted by the store,
	// never by the caller.
	var genErr error
	token, _, genErr = inviteStore.Create(context.Background(), "tenant-1", "101", time.Hour)
	if genErr != nil {
		t.Fatalf("create invite: %v", genErr)
	}

	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)

	// Store CA/caKey in closure for leaf generation in tests.
	_ = caKey // used by individual tests
	_ = ca

	var pivErr error
	activator, pivErr = NewPIVActivator(PIVActivatorDependencies{
		Roots:            roots,
		UserStorage:      userStorage,
		InviteStore:      inviteStore,
		LoginFactors:     newPIVTestEnroller(t),
		ActivationClaims: newTestActivationClaims(t),
	})
	if pivErr != nil {
		t.Fatalf("NewPIVActivator: %v", pivErr)
	}

	return activator, token, userStorage, inviteStore
}

// TestPIVActivator_Valid verifies a complete successful PIV activation.
func TestPIVActivator_Valid(t *testing.T) {
	ctx := context.Background()

	userStorage := newMockUserStorage()
	inviteStore := newFakeInviteStore()

	user := &User{
		ID:        "1",
		TenantID:  "t1",
		Email:     "piv@example.com",
		Lifecycle: LifecyclePending,
		Created:   time.Now(),
		Updated:   time.Now(),
	}
	_ = userStorage.CreateUser(ctx, user)

	token, _, _ := inviteStore.Create(ctx, "t1", "1", time.Hour)

	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)

	_, certPEM := pivTestLeaf(t, ca, caKey, true)

	act, err := NewPIVActivator(PIVActivatorDependencies{
		Roots:            roots,
		UserStorage:      userStorage,
		InviteStore:      inviteStore,
		LoginFactors:     newPIVTestEnroller(t),
		ActivationClaims: newTestActivationClaims(t),
	})
	if err != nil {
		t.Fatalf("NewPIVActivator: %v", err)
	}

	if err := act.Activate(ctx, "t1", token, certPEM); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	updated, err := userStorage.GetUser(ctx, "t1", "1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if updated.Lifecycle != LifecycleActive {
		t.Errorf("expected state=active, got %q", updated.Lifecycle)
	}
	if len(updated.PIVThumbprints()) == 0 {
		t.Error("expected a PIV-attested cert binding to be set")
	}
}

// newPIVLoginFactorTest builds an activator over factors for a pending user
// "1" in tenant "t1" and returns it with the invite token and a leaf cert.
func newPIVLoginFactorTest(t *testing.T, enroller *CertLoginFactorEnroller) (*PIVActivator, *mockUserStorage, *fakeInviteStore, string, *x509.Certificate, []byte) {
	t.Helper()
	ctx := context.Background()
	userStorage := newMockUserStorage()
	inviteStore := newFakeInviteStore()
	user := &User{ID: "1", TenantID: "t1", Email: "piv@example.com", Lifecycle: LifecyclePending, Created: time.Now(), Updated: time.Now()}
	if err := userStorage.CreateUser(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	token, _, err := inviteStore.Create(ctx, "t1", "1", time.Hour)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	leaf, certPEM := pivTestLeaf(t, ca, caKey, true)
	act, err := NewPIVActivator(PIVActivatorDependencies{
		Roots:            roots,
		UserStorage:      userStorage,
		InviteStore:      inviteStore,
		LoginFactors:     enroller,
		ActivationClaims: newTestActivationClaims(t),
	})
	if err != nil {
		t.Fatalf("NewPIVActivator: %v", err)
	}
	return act, userStorage, inviteStore, token, leaf, certPEM
}

// TestPIVActivator_EnrollsLoginFactor verifies the activated certificate
// resolves to the user through the lookup the unified login ceremony makes.
func TestPIVActivator_EnrollsLoginFactor(t *testing.T) {
	ctx := context.Background()
	enroller, factors := newPIVTestFactorEnroller(t)
	act, _, _, token, leaf, certPEM := newPIVLoginFactorTest(t, enroller)

	if err := act.Activate(ctx, "t1", token, certPEM); err != nil {
		t.Fatalf("Activate: %v", err)
	}

	for _, key := range [][]byte{MTLSCredentialKey(leaf), MTLSSPKICredentialKey(leaf)} {
		factor, err := factors.LookupByCredentialKey(qrdbsdk.ContextWithTenant(ctx, "t1"), qrdbsdk.AuthFactorMTLS, key)
		if err != nil {
			t.Fatalf("login lookup: %v", err)
		}
		if factor.UserID != 1 || factor.Type != qrdbsdk.AuthFactorMTLS {
			t.Errorf("factor = user %d type %q, want user 1 type mtls", factor.UserID, factor.Type)
		}
		if factor.Metadata["tenant_id"] != "t1" || factor.Metadata["binding_source"] != pivInviteBindingSource {
			t.Errorf("factor provenance = %v", factor.Metadata)
		}
	}
}

// TestPIVActivator_ForeignCertificateKeepsActivationRetryable verifies a
// certificate already enrolled for another user is rejected typed before the
// invite is redeemed, so the user stays pending, the invite unspent, and a
// retry with the right certificate activates.
func TestPIVActivator_ForeignCertificateKeepsActivationRetryable(t *testing.T) {
	ctx := context.Background()
	rt := newPIVRollbackTest(t, newMockUserStorage(), nil)
	foreign, foreignPEM := rt.leaf(t)
	if _, err := rt.act.enrollLoginFactor(ctx, "t1", "2", foreign); err != nil {
		t.Fatalf("enroll the certificate for user 2: %v", err)
	}

	err := rt.act.Activate(ctx, "t1", rt.token, foreignPEM)
	var factorErr *PIVActivationLoginFactorError
	var mismatch *CertLoginFactorOwnerMismatchError
	if !errors.As(err, &factorErr) || !errors.As(err, &mismatch) {
		t.Fatalf("expected PIVActivationLoginFactorError wrapping the owner mismatch, got %T: %v", err, err)
	}
	rt.assertNotActivated(t)
	rt.assertInviteUsed(t, false)

	_, ownPEM := rt.leaf(t)
	if err := rt.act.Activate(ctx, "t1", rt.token, ownPEM); err != nil {
		t.Fatalf("retry Activate: %v", err)
	}
}

// TestPIVActivator_EnrollFailureAfterRedeemFailsTyped covers a factor store
// failure after the invite was won: the error is typed, the user stays
// pending, no factor exists, and the spent invite cannot be replayed.
func TestPIVActivator_EnrollFailureAfterRedeemFailsTyped(t *testing.T) {
	ctx := context.Background()
	rt := newPIVRollbackTest(t, newMockUserStorage(), nil)
	rt.factors.addErr = errors.New("factor store down")
	_, certPEM := rt.leaf(t)

	err := rt.act.Activate(ctx, "t1", rt.token, certPEM)
	var factorErr *PIVActivationLoginFactorError
	if !errors.As(err, &factorErr) || !errors.Is(err, rt.factors.addErr) {
		t.Fatalf("expected PIVActivationLoginFactorError wrapping the store error, got %T: %v", err, err)
	}
	if ids := rt.factors.activeFactorIDs(); len(ids) != 0 {
		t.Fatalf("active factors = %v, want none", ids)
	}
	rt.assertNotActivated(t)
	rt.assertInviteUsed(t, true)

	rt.factors.addErr = nil
	if err := rt.act.Activate(ctx, "t1", rt.token, certPEM); !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("retry on the spent invite = %v, want ErrInviteUsed", err)
	}
}

// TestPIVActivator_TenantlessActivateKeepsPreexistingFactor covers an
// activation arriving on a tenantless context (gRPC or embedded, before
// authentication) for a certificate already enrolled as the user's factor: it
// must find that factor as the user's own, complete, and leave it in place.
func TestPIVActivator_TenantlessActivateKeepsPreexistingFactor(t *testing.T) {
	ctx := context.Background()
	rt := newPIVRollbackTest(t, newMockUserStorage(), nil)
	leaf, certPEM := rt.leaf(t)

	// The certificate was enrolled earlier over REST, whose context carries
	// the tenant; the activation arrives without one.
	pre := rt.preEnroll(t, leaf)
	if err := rt.act.Activate(ctx, "t1", rt.token, certPEM); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if ids := rt.factors.activeFactorIDs(); len(ids) != 1 || ids[0] != pre {
		t.Fatalf("active factors = %v, want only the pre-existing %q", ids, pre)
	}
	activated, err := rt.users.GetUser(ctx, "t1", "1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if activated.Lifecycle != LifecycleActive {
		t.Errorf("lifecycle = %q, want active", activated.Lifecycle)
	}
}

// pivRollbackTest is an activator for pending user "1" in tenant "t1" over an
// in-memory factor store, with the CA its test certificates chain to.
type pivRollbackTest struct {
	act     *PIVActivator
	users   UserStorage
	invites *fakeInviteStore
	factors *pivTestFactors
	token   string
	ca      *x509.Certificate
	caKey   *ecdsa.PrivateKey
}

// newPIVRollbackTest builds a pivRollbackTest over users. wrapInvites, when
// non-nil, wraps the invite store the activator uses (to fail or stage Redeem).
func newPIVRollbackTest(t *testing.T, users UserStorage, wrapInvites func(InviteStore) InviteStore) *pivRollbackTest {
	t.Helper()
	ctx := context.Background()
	user := &User{ID: "1", TenantID: "t1", Email: "piv@example.com", Lifecycle: LifecyclePending, Created: time.Now(), Updated: time.Now()}
	if err := users.CreateUser(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}
	invites := newFakeInviteStore()
	token, _, err := invites.Create(ctx, "t1", "1", time.Hour)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}
	var activatorInvites InviteStore = invites
	if wrapInvites != nil {
		activatorInvites = wrapInvites(invites)
	}
	enroller, factors := newPIVTestFactorEnroller(t)
	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	act, err := NewPIVActivator(PIVActivatorDependencies{
		Roots:            roots,
		UserStorage:      users,
		InviteStore:      activatorInvites,
		LoginFactors:     enroller,
		ActivationClaims: newTestActivationClaims(t),
	})
	if err != nil {
		t.Fatalf("NewPIVActivator: %v", err)
	}
	return &pivRollbackTest{act: act, users: users, invites: invites, factors: factors, token: token, ca: ca, caKey: caKey}
}

// leaf issues a fresh client certificate chaining to the test CA.
func (rt *pivRollbackTest) leaf(t *testing.T) (*x509.Certificate, []byte) {
	t.Helper()
	return pivTestLeaf(t, rt.ca, rt.caKey, true)
}

// preEnroll enrolls leaf as user 1's login factor outside any activation and
// returns the factor id.
func (rt *pivRollbackTest) preEnroll(t *testing.T, leaf *x509.Certificate) string {
	t.Helper()
	result, err := rt.act.enrollLoginFactor(qrdbsdk.ContextWithTenant(context.Background(), "t1"), "t1", "1", leaf)
	if err != nil || !result.Created {
		t.Fatalf("pre-enroll = %+v, %v; want a created factor", result, err)
	}
	return result.FactorID
}

// assertNotActivated fails unless user 1 is still pending with no cert binding.
func (rt *pivRollbackTest) assertNotActivated(t *testing.T) {
	t.Helper()
	user, err := rt.users.GetUser(context.Background(), "t1", "1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.Lifecycle != LifecyclePending || len(user.PIVThumbprints()) != 0 {
		t.Fatalf("user must stay pending and unbound, got %q with %v", user.Lifecycle, user.PIVThumbprints())
	}
}

// assertInviteUsed fails unless the invite's spent state is want.
func (rt *pivRollbackTest) assertInviteUsed(t *testing.T, want bool) {
	t.Helper()
	record, err := rt.invites.Lookup(context.Background(), "t1", rt.token)
	if err != nil || record.Used != want {
		t.Fatalf("invite = %+v, %v; want Used=%v", record, err, want)
	}
}

// TestPIVActivator_UpdateFailureRevokesCreatedFactor covers a failure after
// the activation won its invite: the factor it created is revoked so the
// certificate cannot log in to an account that never activated, and no later
// call can use the spent invite to enroll another.
func TestPIVActivator_UpdateFailureRevokesCreatedFactor(t *testing.T) {
	ctx := context.Background()
	users := &flakyUpdateUserStorage{mockUserStorage: newMockUserStorage(), failUpdate: true}
	rt := newPIVRollbackTest(t, users, nil)
	leaf, certPEM := rt.leaf(t)

	var updateErr *PIVActivationUpdateError
	if err := rt.act.Activate(ctx, "t1", rt.token, certPEM); !errors.As(err, &updateErr) || !errors.Is(err, errPIVTestUpdate) {
		t.Fatalf("expected PIVActivationUpdateError wrapping the store error, got %T: %v", err, err)
	}
	if ids := rt.factors.activeFactorIDs(); len(ids) != 0 {
		t.Fatalf("active factors = %v, want the failed activation's factor revoked", ids)
	}
	if _, err := rt.factors.LookupByCredentialKey(qrdbsdk.ContextWithTenant(ctx, "t1"), qrdbsdk.AuthFactorMTLS, MTLSCredentialKey(leaf)); !errors.Is(err, qrdbsdk.ErrFactorRevoked) {
		t.Fatalf("login lookup = %v, want ErrFactorRevoked", err)
	}
	rt.assertNotActivated(t)
	rt.assertInviteUsed(t, true)

	users.failUpdate = false
	_, otherPEM := rt.leaf(t)
	if err := rt.act.Activate(ctx, "t1", rt.token, otherPEM); !errors.Is(err, ErrInviteUsed) {
		t.Fatalf("retry on the spent invite = %v, want ErrInviteUsed", err)
	}
	if ids := rt.factors.activeFactorIDs(); len(ids) != 0 {
		t.Fatalf("active factors after retry = %v, want none", ids)
	}
}

// TestPIVActivator_ConcurrentActivationsHaveOneWinner races two activations
// with different certificates past every early-out to the invite redemption.
// Exactly one wins; the loser gets a typed error and never enrolled a factor,
// so the only certificate able to log in is the one the user is bound to.
func TestPIVActivator_ConcurrentActivationsHaveOneWinner(t *testing.T) {
	ctx := context.Background()
	rt := newPIVRaceTest(t, 2)
	leaves := make([]*x509.Certificate, 2)
	pems := make([][]byte, 2)
	for i := range leaves {
		leaves[i], pems[i] = rt.leaf(t)
	}
	winner, loser := rt.race(t, pems)

	tctx := qrdbsdk.ContextWithTenant(ctx, "t1")
	if ids := rt.factors.activeFactorIDs(); len(ids) != 1 {
		t.Fatalf("active factors = %v, want exactly the winner's", ids)
	}
	if factor, err := rt.factors.LookupByCredentialKey(tctx, qrdbsdk.AuthFactorMTLS, MTLSCredentialKey(leaves[winner])); err != nil || factor.UserID != 1 {
		t.Fatalf("winner login lookup = %+v, %v", factor, err)
	}
	if _, err := rt.factors.LookupByCredentialKey(tctx, qrdbsdk.AuthFactorMTLS, MTLSCredentialKey(leaves[loser])); !errors.Is(err, errPIVTestFactorNotFound) {
		t.Fatalf("loser login lookup = %v, want no factor", err)
	}
	user, err := rt.users.GetUser(ctx, "t1", "1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.Lifecycle != LifecycleActive || !user.HasPIVThumbprint(qrdbsdk.ComputeSPKIThumbprint(leaves[winner])) {
		t.Fatalf("user = %q bound to %v, want active and bound to the winner", user.Lifecycle, user.PIVThumbprints())
	}
	if user.HasPIVThumbprint(qrdbsdk.ComputeSPKIThumbprint(leaves[loser])) {
		t.Fatalf("user is bound to the losing certificate: %v", user.PIVThumbprints())
	}
}

// TestPIVActivator_SameCertificateRaceKeepsTheFactor races two activations
// presenting the same certificate, which share one login factor. Whichever
// loses must leave that factor in place: the winner's account is active and
// the certificate still resolves to it at login.
func TestPIVActivator_SameCertificateRaceKeepsTheFactor(t *testing.T) {
	ctx := context.Background()
	rt := newPIVRaceTest(t, 2)
	leaf, certPEM := rt.leaf(t)
	rt.race(t, [][]byte{certPEM, certPEM})

	ids := rt.factors.activeFactorIDs()
	factor, err := rt.factors.LookupByCredentialKey(qrdbsdk.ContextWithTenant(ctx, "t1"), qrdbsdk.AuthFactorMTLS, MTLSCredentialKey(leaf))
	if err != nil || factor.UserID != 1 || len(ids) != 1 || ids[0] != factor.ID {
		t.Fatalf("login lookup = %+v, %v with active factors %v; want the shared factor active", factor, err, ids)
	}
	user, err := rt.users.GetUser(ctx, "t1", "1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if user.Lifecycle != LifecycleActive || !user.HasPIVThumbprint(qrdbsdk.ComputeSPKIThumbprint(leaf)) {
		t.Fatalf("user = %q bound to %v, want active and bound to the certificate", user.Lifecycle, user.PIVThumbprints())
	}
}

// TestPIVActivator_TwoTokensForOneUserHaveOneWinner races two activations of
// the same pending user presenting DIFFERENT invite tokens, as a retried
// provisioning leaves behind. Each token is single-use only on its own, so the
// user's claim is what must pick one winner: exactly one factor is enrolled
// and the user is bound to the winner's certificate alone.
func TestPIVActivator_TwoTokensForOneUserHaveOneWinner(t *testing.T) {
	ctx := context.Background()
	rt := newPIVRaceTest(t, 2)
	second, _, err := rt.invites.Create(ctx, "t1", "1", time.Hour)
	if err != nil {
		t.Fatalf("create second invite: %v", err)
	}
	leaves := make([]*x509.Certificate, 2)
	pems := make([][]byte, 2)
	for i := range leaves {
		leaves[i], pems[i] = rt.leaf(t)
	}
	winner, loser, loserErr := rt.raceTokens(t, []string{rt.token, second}, pems)

	// Neither token is spent for the other, so only the user's claim, or the
	// pending check once the winner has finished, can have stopped the loser.
	if !IsUserNotPending(loserErr) {
		t.Fatalf("losing activation = %T: %v, want the user-not-pending conflict", loserErr, loserErr)
	}

	if ids := rt.factors.activeFactorIDs(); len(ids) != 1 {
		t.Fatalf("active factors = %v, want exactly the winner's", ids)
	}
	tctx := qrdbsdk.ContextWithTenant(ctx, "t1")
	if _, err := rt.factors.LookupByCredentialKey(tctx, qrdbsdk.AuthFactorMTLS, MTLSCredentialKey(leaves[loser])); !errors.Is(err, errPIVTestFactorNotFound) {
		t.Fatalf("loser login lookup = %v, want no factor", err)
	}
	user, err := rt.users.GetUser(ctx, "t1", "1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	if !user.HasPIVThumbprint(qrdbsdk.ComputeSPKIThumbprint(leaves[winner])) || user.HasPIVThumbprint(qrdbsdk.ComputeSPKIThumbprint(leaves[loser])) {
		t.Fatalf("user bound to %v, want only the winner's certificate", user.PIVThumbprints())
	}
}

// TestPIVActivator_ResetToPendingCanActivateAgain covers an administrator
// returning an active user to pending: the claim the first activation
// completed must not block the user's next activation.
func TestPIVActivator_ResetToPendingCanActivateAgain(t *testing.T) {
	ctx := context.Background()
	rt := newPIVRollbackTest(t, newMockUserStorage(), nil)
	_, firstPEM := rt.leaf(t)
	if err := rt.act.Activate(ctx, "t1", rt.token, firstPEM); err != nil {
		t.Fatalf("first Activate: %v", err)
	}

	user, err := rt.users.GetUser(ctx, "t1", "1")
	if err != nil {
		t.Fatalf("GetUser: %v", err)
	}
	user.Lifecycle = LifecyclePending
	user.Updated = user.Updated.Add(time.Second)
	if err := rt.users.UpdateUser(ctx, user); err != nil {
		t.Fatalf("reset to pending: %v", err)
	}
	token, _, err := rt.invites.Create(ctx, "t1", "1", time.Hour)
	if err != nil {
		t.Fatalf("create invite: %v", err)
	}

	_, secondPEM := rt.leaf(t)
	if err := rt.act.Activate(ctx, "t1", token, secondPEM); err != nil {
		t.Fatalf("Activate after reset to pending: %v", err)
	}
}

// pivRaceTest is a pivRollbackTest whose invite Lookup holds every caller
// until all of the expected racing activations have arrived, so they all
// pass the invite checks before any of them claims the user.
type pivRaceTest struct {
	*pivRollbackTest
}

// newPIVRaceTest builds a pivRaceTest for racers concurrent activations.
func newPIVRaceTest(t *testing.T, racers int) *pivRaceTest {
	t.Helper()
	staged := &stagedLookupInviteStore{}
	staged.arrived.Add(racers)
	users := &flakyUpdateUserStorage{mockUserStorage: newMockUserStorage()}
	return &pivRaceTest{newPIVRollbackTest(t, users, func(inner InviteStore) InviteStore {
		staged.InviteStore = inner
		return staged
	})}
}

// race runs one activation per certificate PEM concurrently, all presenting
// the same token. It requires exactly one winner and a loser refused because
// the invite is spent or the user is no longer pending, and returns their
// indexes.
func (rt *pivRaceTest) race(t *testing.T, pems [][]byte) (winner, loser int) {
	t.Helper()
	tokens := make([]string, len(pems))
	for i := range tokens {
		tokens[i] = rt.token
	}
	winner, loser, loserErr := rt.raceTokens(t, tokens, pems)
	if !errors.Is(loserErr, ErrInviteUsed) && !IsUserNotPending(loserErr) {
		t.Fatalf("losing activation = %T: %v, want ErrInviteUsed or the user-not-pending conflict", loserErr, loserErr)
	}
	return winner, loser
}

// raceTokens runs activation i with tokens[i] and pems[i] concurrently. It
// requires exactly one winner and returns its index, the loser's, and the
// loser's error.
func (rt *pivRaceTest) raceTokens(t *testing.T, tokens []string, pems [][]byte) (winner, loser int, loserErr error) {
	t.Helper()
	errs := make([]error, len(pems))
	var wg sync.WaitGroup
	for i := range pems {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs[i] = rt.act.Activate(context.Background(), "t1", tokens[i], pems[i])
		}(i)
	}
	wg.Wait()

	winner, loser = 0, 1
	if errs[0] != nil {
		winner, loser = 1, 0
	}
	if errs[winner] != nil {
		t.Fatalf("no activation won: %v, %v", errs[0], errs[1])
	}
	return winner, loser, errs[loser]
}

// stagedLookupInviteStore holds every Lookup until all expected callers have
// arrived, so racing activations all reach the single-winner step.
type stagedLookupInviteStore struct {
	InviteStore
	arrived sync.WaitGroup
}

func (s *stagedLookupInviteStore) Lookup(ctx context.Context, tenantID, token string) (*InviteRecord, error) {
	s.arrived.Done()
	s.arrived.Wait()
	return s.InviteStore.Lookup(ctx, tenantID, token)
}

// TestPIVActivator_RedeemFailureEnrollsNothing covers an activation that does
// not win its invite because the store failed: nothing is committed, no
// factor is enrolled, and the invite stays unspent for a retry.
func TestPIVActivator_RedeemFailureEnrollsNothing(t *testing.T) {
	ctx := context.Background()
	failing := &switchableRedeemInviteStore{fail: true}
	rt := newPIVRollbackTest(t, newMockUserStorage(), func(inner InviteStore) InviteStore {
		failing.InviteStore = inner
		return failing
	})
	leaf, certPEM := rt.leaf(t)

	var redeemErr *PIVActivationRedeemError
	if err := rt.act.Activate(ctx, "t1", rt.token, certPEM); !errors.As(err, &redeemErr) {
		t.Fatalf("expected PIVActivationRedeemError, got %T: %v", err, err)
	}
	if ids := rt.factors.activeFactorIDs(); len(ids) != 0 {
		t.Fatalf("active factors = %v, want none", ids)
	}
	rt.assertNotActivated(t)
	rt.assertInviteUsed(t, false)

	failing.fail = false
	if err := rt.act.Activate(ctx, "t1", rt.token, certPEM); err != nil {
		t.Fatalf("retry Activate: %v", err)
	}
	ids := rt.factors.activeFactorIDs()
	factor, err := rt.factors.LookupByCredentialKey(qrdbsdk.ContextWithTenant(ctx, "t1"), qrdbsdk.AuthFactorMTLS, MTLSCredentialKey(leaf))
	if err != nil || len(ids) != 1 || ids[0] != factor.ID {
		t.Fatalf("active factors = %v (factor %+v, %v), want only the retry's", ids, factor, err)
	}
}

// TestPIVActivator_CancellationAfterEnrollStillRevokes covers a request
// canceled after the factor was enrolled: the update fails with the
// cancellation, and the factor is revoked anyway, because the compensation
// does not run on the canceled request context.
func TestPIVActivator_CancellationAfterEnrollStillRevokes(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	users := &flakyUpdateUserStorage{mockUserStorage: newMockUserStorage(), cancelUpdate: cancel}
	rt := newPIVRollbackTest(t, users, nil)
	_, certPEM := rt.leaf(t)

	err := rt.act.Activate(ctx, "t1", rt.token, certPEM)
	var updateErr *PIVActivationUpdateError
	if !errors.As(err, &updateErr) || !errors.Is(err, context.Canceled) {
		t.Fatalf("expected PIVActivationUpdateError wrapping the cancellation, got %T: %v", err, err)
	}
	if ids := rt.factors.activeFactorIDs(); len(ids) != 0 {
		t.Fatalf("active factors = %v, want the canceled activation's factor revoked", ids)
	}
	rt.assertNotActivated(t)
}

// switchableRedeemInviteStore fails Redeem with a store error while fail is set.
type switchableRedeemInviteStore struct {
	InviteStore
	fail bool
}

func (s *switchableRedeemInviteStore) Redeem(ctx context.Context, tenantID, token string) (*InviteRecord, error) {
	if s.fail {
		return nil, &dberrors.MessageError{Message: "simulated Redeem failure"}
	}
	return s.InviteStore.Redeem(ctx, tenantID, token)
}

// TestPIVActivator_FailureKeepsPreexistingFactor ensures a failing activation
// revokes only a factor it created: the same certificate enrolled before the
// call keeps its factor.
func TestPIVActivator_FailureKeepsPreexistingFactor(t *testing.T) {
	ctx := context.Background()
	users := &flakyUpdateUserStorage{mockUserStorage: newMockUserStorage(), failUpdate: true}
	rt := newPIVRollbackTest(t, users, nil)
	leaf, certPEM := rt.leaf(t)
	pre := rt.preEnroll(t, leaf)

	var updateErr *PIVActivationUpdateError
	if err := rt.act.Activate(ctx, "t1", rt.token, certPEM); !errors.As(err, &updateErr) {
		t.Fatalf("expected PIVActivationUpdateError, got %T: %v", err, err)
	}
	if ids := rt.factors.activeFactorIDs(); len(ids) != 1 || ids[0] != pre {
		t.Fatalf("active factors = %v, want the pre-existing %q kept", ids, pre)
	}
}

// TestPIVActivator_CleanupFailureSurfacesResidualFactor ensures a failed
// revocation is neither swallowed nor reported as the plain activation
// failure: the caller learns a residual factor may exist, and the audit log
// names it.
func TestPIVActivator_CleanupFailureSurfacesResidualFactor(t *testing.T) {
	ctx := context.Background()
	users := &flakyUpdateUserStorage{mockUserStorage: newMockUserStorage(), failUpdate: true}
	rt := newPIVRollbackTest(t, users, nil)
	audit := &mockAuditLogger{}
	rt.act.auditLogger = audit
	rt.factors.removeErr = errors.New("factor store down")
	_, certPEM := rt.leaf(t)

	err := rt.act.Activate(ctx, "t1", rt.token, certPEM)
	var cleanupErr *PIVActivationFactorCleanupError
	if !errors.As(err, &cleanupErr) {
		t.Fatalf("expected PIVActivationFactorCleanupError, got %T: %v", err, err)
	}
	ids := rt.factors.activeFactorIDs()
	if len(ids) != 1 || cleanupErr.FactorID != ids[0] {
		t.Fatalf("cleanup error names factor %q, residual factors %v", cleanupErr.FactorID, ids)
	}
	var updateErr *PIVActivationUpdateError
	if !errors.Is(err, rt.factors.removeErr) || !errors.As(err, &updateErr) {
		t.Fatalf("cleanup error must wrap the revocation and the activation failures, got %v", err)
	}
	if len(audit.events) != 1 || audit.events[0].Outcome != AuditOutcomeFailure || audit.events[0].Details["factor_id"] != ids[0] {
		t.Fatalf("audit events = %+v, want one failure naming factor %q", audit.events, ids[0])
	}
}

// errPIVTestUpdate is flakyUpdateUserStorage's update failure.
var errPIVTestUpdate = errors.New("user store update failed")

// flakyUpdateUserStorage fails UpdateUser while failUpdate is set. When
// cancelUpdate is set, UpdateUser instead cancels the request context and
// fails with its error, as a store observing the cancellation would. It hands
// out copies, so a failed update leaves the stored user untouched as a real
// store would.
type flakyUpdateUserStorage struct {
	*mockUserStorage
	failUpdate   bool
	cancelUpdate context.CancelFunc
}

func (s *flakyUpdateUserStorage) GetUser(ctx context.Context, tenantID, userID string) (*User, error) {
	user, err := s.mockUserStorage.GetUser(ctx, tenantID, userID)
	if err != nil {
		return nil, err
	}
	clone := *user
	return &clone, nil
}

func (s *flakyUpdateUserStorage) UpdateUser(ctx context.Context, user *User) error {
	if s.failUpdate {
		return errPIVTestUpdate
	}
	if s.cancelUpdate != nil {
		s.cancelUpdate()
		return ctx.Err()
	}
	return s.mockUserStorage.UpdateUser(ctx, user)
}

// TestPIVActivator_ExpiredInvite verifies that an expired invite is rejected.
func TestPIVActivator_ExpiredInvite(t *testing.T) {
	ctx := context.Background()

	userStorage := newMockUserStorage()
	inviteStore := newFakeInviteStore()

	user := &User{ID: "2", TenantID: "t1", Email: "e@x.com", Lifecycle: LifecyclePending, Created: time.Now(), Updated: time.Now()}
	_ = userStorage.CreateUser(ctx, user)

	token, _, _ := inviteStore.Create(ctx, "t1", "2", -time.Minute) // already expired

	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)

	_, certPEM := pivTestLeaf(t, ca, caKey, true)

	act, _ := NewPIVActivator(PIVActivatorDependencies{
		Roots:            roots,
		UserStorage:      userStorage,
		InviteStore:      inviteStore,
		LoginFactors:     newPIVTestEnroller(t),
		ActivationClaims: newTestActivationClaims(t),
	})

	err := act.Activate(ctx, "t1", token, certPEM)
	if !errors.Is(err, ErrInviteExpired) {
		t.Fatalf("expected ErrInviteExpired, got %v", err)
	}
}

// TestPIVActivator_UntrustedCert verifies that a certificate from an unknown CA is rejected.
func TestPIVActivator_UntrustedCert(t *testing.T) {
	ctx := context.Background()

	userStorage := newMockUserStorage()
	inviteStore := newFakeInviteStore()

	user := &User{ID: "3", TenantID: "t1", Email: "f@x.com", Lifecycle: LifecyclePending, Created: time.Now(), Updated: time.Now()}
	_ = userStorage.CreateUser(ctx, user)

	token, _, _ := inviteStore.Create(ctx, "t1", "3", time.Hour)

	// Trust only ca2, but present a cert signed by ca1.
	ca1, ca1Key := pivTestCA(t)
	ca2, _ := pivTestCA(t)

	roots := x509.NewCertPool()
	roots.AddCert(ca2)

	_, certPEM := pivTestLeaf(t, ca1, ca1Key, true)

	act, _ := NewPIVActivator(PIVActivatorDependencies{
		Roots:            roots,
		UserStorage:      userStorage,
		InviteStore:      inviteStore,
		LoginFactors:     newPIVTestEnroller(t),
		ActivationClaims: newTestActivationClaims(t),
	})

	err := act.Activate(ctx, "t1", token, certPEM)
	if !errors.Is(err, ErrPIVCertRejected) {
		t.Fatalf("expected ErrPIVCertRejected, got %v", err)
	}
}

// TestPIVActivate_RejectsUntrustedIssuer verifies that a certificate whose issuer DN is not
// in the TrustedIssuers allowlist is rejected even when chain verification succeeds.
func TestPIVActivate_RejectsUntrustedIssuer(t *testing.T) {
	ctx := context.Background()

	userStorage := newMockUserStorage()
	inviteStore := newFakeInviteStore()

	user := &User{ID: "5", TenantID: "t1", Email: "h@x.com", Lifecycle: LifecyclePending, Created: time.Now(), Updated: time.Now()}
	_ = userStorage.CreateUser(ctx, user)

	token, _, _ := inviteStore.Create(ctx, "t1", "5", time.Hour)

	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	_, certPEM := pivTestLeaf(t, ca, caKey, true)

	// The cert's issuer will be "CN=PIV Test Root" but we allow only "CN=Trusted Agency CA".
	act, err := NewPIVActivator(PIVActivatorDependencies{
		Roots:            roots,
		UserStorage:      userStorage,
		InviteStore:      inviteStore,
		LoginFactors:     newPIVTestEnroller(t),
		ActivationClaims: newTestActivationClaims(t),
		TrustedIssuers:   []string{"CN=Trusted Agency CA"},
	})
	if err != nil {
		t.Fatalf("NewPIVActivator: %v", err)
	}

	activateErr := act.Activate(ctx, "t1", token, certPEM)
	if activateErr == nil {
		t.Fatal("expected error but got nil")
	}
	var issuerErr *PIVCertIssuerRejectedError
	if !errors.As(activateErr, &issuerErr) {
		t.Fatalf("expected PIVCertIssuerRejectedError, got %T: %v", activateErr, activateErr)
	}
	if issuerErr.Issuer == "" {
		t.Error("expected non-empty Issuer in error")
	}
}

// TestPIVActivate_AcceptsTrustedIssuer verifies that a certificate whose issuer DN matches
// an entry in TrustedIssuers passes the second-gate check.
func TestPIVActivate_AcceptsTrustedIssuer(t *testing.T) {
	ctx := context.Background()

	userStorage := newMockUserStorage()
	inviteStore := newFakeInviteStore()

	user := &User{ID: "6", TenantID: "t1", Email: "i@x.com", Lifecycle: LifecyclePending, Created: time.Now(), Updated: time.Now()}
	_ = userStorage.CreateUser(ctx, user)

	token, _, _ := inviteStore.Create(ctx, "t1", "6", time.Hour)

	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	_, certPEM := pivTestLeaf(t, ca, caKey, true)

	// Use the actual issuer DN of the test CA (CN=PIV Test Root).
	act, err := NewPIVActivator(PIVActivatorDependencies{
		Roots:            roots,
		UserStorage:      userStorage,
		InviteStore:      inviteStore,
		LoginFactors:     newPIVTestEnroller(t),
		ActivationClaims: newTestActivationClaims(t),
		TrustedIssuers:   []string{"CN=PIV Test Root"},
	})
	if err != nil {
		t.Fatalf("NewPIVActivator: %v", err)
	}

	if err := act.Activate(ctx, "t1", token, certPEM); err != nil {
		t.Fatalf("expected success but got: %v", err)
	}
}

// TestPIVActivate_EmptyTrustedIssuers verifies that an empty TrustedIssuers list
// is backward-compatible (any valid issuer is accepted).
func TestPIVActivate_EmptyTrustedIssuers(t *testing.T) {
	ctx := context.Background()

	userStorage := newMockUserStorage()
	inviteStore := newFakeInviteStore()

	user := &User{ID: "7", TenantID: "t1", Email: "j@x.com", Lifecycle: LifecyclePending, Created: time.Now(), Updated: time.Now()}
	_ = userStorage.CreateUser(ctx, user)

	token, _, _ := inviteStore.Create(ctx, "t1", "7", time.Hour)

	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	_, certPEM := pivTestLeaf(t, ca, caKey, true)

	// Empty TrustedIssuers = permissive (any issuer that validates is accepted).
	act, err := NewPIVActivator(PIVActivatorDependencies{
		Roots:            roots,
		UserStorage:      userStorage,
		InviteStore:      inviteStore,
		LoginFactors:     newPIVTestEnroller(t),
		ActivationClaims: newTestActivationClaims(t),
	})
	if err != nil {
		t.Fatalf("NewPIVActivator: %v", err)
	}

	if err := act.Activate(ctx, "t1", token, certPEM); err != nil {
		t.Fatalf("expected success but got: %v", err)
	}
}

// TestNewPIVActivator_MissingDependencies covers each fail-closed configuration
// guard in the constructor: a nil root pool, nil user storage, and nil invite
// store must all return a typed PIVActivatorConfigError.
func TestNewPIVActivator_MissingDependencies(t *testing.T) {
	roots := x509.NewCertPool()
	us := newMockUserStorage()
	is := newFakeInviteStore()
	lf := newPIVTestEnroller(t)
	ac := newTestActivationClaims(t)

	cases := []struct {
		name string
		deps PIVActivatorDependencies
	}{
		{"nil roots", PIVActivatorDependencies{UserStorage: us, InviteStore: is, LoginFactors: lf, ActivationClaims: ac}},
		{"nil user storage", PIVActivatorDependencies{Roots: roots, InviteStore: is, LoginFactors: lf, ActivationClaims: ac}},
		{"nil invite store", PIVActivatorDependencies{Roots: roots, UserStorage: us, LoginFactors: lf, ActivationClaims: ac}},
		{"nil login factors", PIVActivatorDependencies{Roots: roots, UserStorage: us, InviteStore: is, ActivationClaims: ac}},
		{"nil activation claims", PIVActivatorDependencies{Roots: roots, UserStorage: us, InviteStore: is, LoginFactors: lf}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			act, err := NewPIVActivator(tc.deps)
			if act != nil {
				t.Fatalf("%s: expected nil activator", tc.name)
			}
			var cfgErr *PIVActivatorConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("%s: expected PIVActivatorConfigError, got %T: %v", tc.name, err, err)
			}
		})
	}
}

// TestPIVActivator_AuditOnSuccess verifies a successful activation emits a
// success-outcome audit event through the configured logger.
func TestPIVActivator_AuditOnSuccess(t *testing.T) {
	ctx := context.Background()

	userStorage := newMockUserStorage()
	inviteStore := newFakeInviteStore()
	audit := &mockAuditLogger{}

	_ = userStorage.CreateUser(ctx, &User{ID: "10", TenantID: "t1", Email: "a@x.com", Lifecycle: LifecyclePending, Created: time.Now(), Updated: time.Now()})
	token, _, _ := inviteStore.Create(ctx, "t1", "10", time.Hour)

	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)
	_, certPEM := pivTestLeaf(t, ca, caKey, true)

	act, err := NewPIVActivator(PIVActivatorDependencies{
		Roots:            roots,
		UserStorage:      userStorage,
		InviteStore:      inviteStore,
		LoginFactors:     newPIVTestEnroller(t),
		ActivationClaims: newTestActivationClaims(t),
		AuditLogger:      audit,
	})
	if err != nil {
		t.Fatalf("NewPIVActivator: %v", err)
	}

	if err := act.Activate(ctx, "t1", token, certPEM); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if len(audit.events) != 1 {
		t.Fatalf("expected 1 audit event, got %d", len(audit.events))
	}
	if audit.events[0].Outcome != AuditOutcomeSuccess {
		t.Fatalf("expected success outcome, got %v", audit.events[0].Outcome)
	}
}

func TestPIVActivator_UserNotPending(t *testing.T) {
	ctx := context.Background()

	userStorage := newMockUserStorage()
	inviteStore := newFakeInviteStore()

	user := &User{ID: "4", TenantID: "t1", Email: "g@x.com", Lifecycle: LifecycleActive, Created: time.Now(), Updated: time.Now()}
	_ = userStorage.CreateUser(ctx, user)

	token, _, _ := inviteStore.Create(ctx, "t1", "4", time.Hour)

	ca, caKey := pivTestCA(t)
	roots := x509.NewCertPool()
	roots.AddCert(ca)

	_, certPEM := pivTestLeaf(t, ca, caKey, true)

	act, _ := NewPIVActivator(PIVActivatorDependencies{
		Roots:            roots,
		UserStorage:      userStorage,
		InviteStore:      inviteStore,
		LoginFactors:     newPIVTestEnroller(t),
		ActivationClaims: newTestActivationClaims(t),
	})

	err := act.Activate(ctx, "t1", token, certPEM)
	if !errors.Is(err, ErrUserNotPending) {
		t.Fatalf("expected ErrUserNotPending, got %v", err)
	}
}
