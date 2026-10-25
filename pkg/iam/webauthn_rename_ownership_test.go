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
	"encoding/base64"
	"encoding/hex"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	xkmswebauthn "github.com/jeremyhahn/go-xkms/pkg/webauthn"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// =============================================================================
// go-quicbaas#582 — a rename carries the user's WebAuthn credentials
//
// These tests run against the SAME store pair the server wires:
// xkms's qrdb-backed WebAuthn adapter over a qrdb user client and a qrdb DAO
// factory. That choice is the point of the file. xkms also ships an
// email-KEYED backend store, and reading this product's behaviour off THAT
// one is what produced the bug: the identity store deleted a renamed user's
// passkeys to prevent an orphan the adapter it actually uses cannot produce.
//
// What the adapter does with a rename, measured here rather than assumed:
// a credential is filed under `__xkms:webauthn/users/<qrdb id>/credentials`
// and its login factor under the same uint64, while the email is only the
// index the lookup enters the qrdb user record through. qrdb moves that index
// with the record, so the new address resolves to the same id and the same
// credentials, and the old address resolves to nothing.
// =============================================================================

// renameTestUserClient is an in-memory qrdbtransport.UserClient. It implements
// the two behaviours the adapter depends on: an email lookup scoped to the
// tenant, and an UpdateUser that MOVES that email index with the record, which
// is what qrdb's own store does (pkg/user: TestUpdateUser_MovesTheEmailIndex).
type renameTestUserClient struct {
	mu     sync.Mutex
	nextID uint64
	users  map[uint64]*qrdbtransport.User
}

func newRenameTestUserClient() *renameTestUserClient {
	return &renameTestUserClient{nextID: 1, users: map[uint64]*qrdbtransport.User{}}
}

// renameTestUserNotFoundError is the absent-record signal.
//
// IT IS THE TYPED qrdb ERROR, not a bare one with the right words in it. Until
// xkms v0.12 its isUserNotFound also accepted any message CONTAINING "not
// found", and this fake leaned on that; v0.12 replaced the substring match with
// errors.As on *qrdbtransport.UserNotFoundError and *KeyNotFoundError, keeping
// only a narrow prefix fallback for messages carried inside a *RequestError.
// That is the better contract — a real qrdb returns the typed error — and it
// makes an untyped stand-in unfaithful rather than merely loose.
type renameTestUserNotFoundError struct{ lookup string }

func (e *renameTestUserNotFoundError) Error() string {
	return (&qrdbtransport.UserNotFoundError{Lookup: e.lookup}).Error()
}

// Unwrap hands classifiers the typed error this fake stands in for, so
// errors.As finds it exactly as it would against a real qrdb.
func (e *renameTestUserNotFoundError) Unwrap() error {
	return &qrdbtransport.UserNotFoundError{Lookup: e.lookup}
}

// renameTestUnsupportedError reports a UserClient verb this fake does not
// serve. The adapter under test never calls one; a test that starts to gets a
// named failure instead of a silent zero value.
type renameTestUnsupportedError struct{ Op string }

func (e *renameTestUnsupportedError) Error() string {
	return "renameTestUserClient: unsupported operation: " + e.Op
}

func (c *renameTestUserClient) CreateUser(_ context.Context, req *qrdbtransport.CreateUserRequest) (*qrdbtransport.User, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	id := c.nextID
	c.nextID++
	user := &qrdbtransport.User{
		ID:        id,
		Username:  req.Username,
		Email:     req.Email,
		Name:      req.Name,
		TenantID:  req.TenantID,
		Lifecycle: string(LifecycleActive),
	}
	c.users[id] = user
	return cloneRenameTestUser(user), nil
}

func (c *renameTestUserClient) GetUser(_ context.Context, id uint64) (*qrdbtransport.User, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	user, ok := c.users[id]
	if !ok {
		return nil, &renameTestUserNotFoundError{}
	}
	return cloneRenameTestUser(user), nil
}

func (c *renameTestUserClient) GetUserByUsername(_ context.Context, tenantID, username string) (*qrdbtransport.User, error) {
	return c.lookup(tenantID, func(u *qrdbtransport.User) bool { return u.Username == username })
}

func (c *renameTestUserClient) GetUserByEmail(_ context.Context, tenantID, email string) (*qrdbtransport.User, error) {
	return c.lookup(tenantID, func(u *qrdbtransport.User) bool {
		return strings.EqualFold(u.Email, email)
	})
}

func (c *renameTestUserClient) GetUserByCertThumbprint(context.Context, string) (*qrdbtransport.User, error) {
	return nil, &renameTestUserNotFoundError{}
}

func (c *renameTestUserClient) AddCertBinding(context.Context, uint64, string, int64) (*qrdbtransport.User, error) {
	return nil, &renameTestUnsupportedError{Op: "AddCertBinding"}
}

func (c *renameTestUserClient) RemoveCertBinding(context.Context, uint64, string) (*qrdbtransport.User, error) {
	return nil, &renameTestUnsupportedError{Op: "RemoveCertBinding"}
}

func (c *renameTestUserClient) UpdateUser(_ context.Context, req *qrdbtransport.UpdateUserRequest) (*qrdbtransport.User, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	user, ok := c.users[req.ID]
	if !ok {
		return nil, &renameTestUserNotFoundError{}
	}
	if req.Username != "" {
		user.Username = req.Username
	}
	if req.Email != "" {
		user.Email = req.Email
	}
	if req.Name != "" {
		user.Name = req.Name
	}
	return cloneRenameTestUser(user), nil
}

func (c *renameTestUserClient) DeleteUser(_ context.Context, id uint64) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.users, id)
	return nil
}

func (c *renameTestUserClient) ListUsers(_ context.Context, req *qrdbtransport.ListUsersRequest) (*qrdbtransport.ListUsersResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]*qrdbtransport.User, 0, len(c.users))
	for _, user := range c.users {
		if req.TenantID == "" || user.TenantID == req.TenantID {
			out = append(out, cloneRenameTestUser(user))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return &qrdbtransport.ListUsersResponse{Users: out, Total: len(out)}, nil
}

func (c *renameTestUserClient) EnableUser(_ context.Context, id uint64) error {
	return c.setLifecycle(id, LifecycleActive)
}

func (c *renameTestUserClient) DisableUser(_ context.Context, id uint64) error {
	return c.setLifecycle(id, LifecycleDisabled)
}

func (c *renameTestUserClient) SuspendUser(_ context.Context, id uint64) error {
	return c.setLifecycle(id, LifecycleSuspended)
}

func (c *renameTestUserClient) UnsuspendUser(_ context.Context, id uint64) error {
	return c.setLifecycle(id, LifecycleActive)
}

func (c *renameTestUserClient) setLifecycle(id uint64, state Lifecycle) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	user, ok := c.users[id]
	if !ok {
		return &renameTestUserNotFoundError{}
	}
	user.Lifecycle = string(state)
	return nil
}

func (c *renameTestUserClient) lookup(tenantID string, match func(*qrdbtransport.User) bool) (*qrdbtransport.User, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, user := range c.users {
		if tenantID != "" && user.TenantID != tenantID {
			continue
		}
		if match(user) {
			return cloneRenameTestUser(user), nil
		}
	}
	return nil, &renameTestUserNotFoundError{}
}

// cloneRenameTestUser hands out a copy, so a caller mutating the record it read
// cannot rewrite the store behind its own back.
func cloneRenameTestUser(u *qrdbtransport.User) *qrdbtransport.User {
	clone := *u
	return &clone
}

var _ qrdbtransport.UserClient = (*renameTestUserClient)(nil)

// newTestWebAuthnDAOFactory returns a DAO factory over a fresh in-process
// qrdb KV store, which gives the credential store the atomic conditional
// writes and secondary indexes it relies on.
func newTestWebAuthnDAOFactory() *qrdbsdk.DAOFactory {
	kv := qrdbsdk.NewMemoryKVStore()
	return qrdbsdk.NewDAOFactory(func(uint64) qrdbsdk.KVStore { return kv })
}

// newTestWebAuthnStores returns an in-memory user store and xkms's qrdb
// credential store over an in-memory DAO factory. xkms v0.55 dropped its
// in-memory stores; the user double below keeps the semantics these tests
// were written against (Save upserts the whole user, lifecycle included),
// while credentials run on the real DAO-backed store.
func newTestWebAuthnStores(t testing.TB) (xkmswebauthn.UserStore, xkmswebauthn.CredentialStore) {
	t.Helper()
	_, creds, err := xkmssdk.NewWebAuthnQRDBUserStoreAdapter(xkmssdk.WebAuthnQRDBUserStoreOptions{
		Users:      newRenameTestUserClient(),
		DAOFactory: newTestWebAuthnDAOFactory(),
		TenantID:   testWriteThroughTenant,
	})
	if err != nil {
		t.Fatalf("NewWebAuthnQRDBUserStoreAdapter: %v", err)
	}
	return newTestWebAuthnUserStore(t), creds
}

// newTestWebAuthnUserStore returns an empty in-memory user store.
func newTestWebAuthnUserStore(t testing.TB) xkmswebauthn.UserStore {
	t.Helper()
	return &testWebAuthnUserStore{byID: map[string]xkmswebauthn.User{}, byEmail: map[string]string{}}
}

// testWebAuthnUserStore is an in-memory xkmswebauthn.UserStore keyed by the
// hex WebAuthn handle, with an email index.
type testWebAuthnUserStore struct {
	mu      sync.Mutex
	byID    map[string]xkmswebauthn.User
	byEmail map[string]string
}

func (s *testWebAuthnUserStore) GetByID(_ context.Context, userID []byte) (xkmswebauthn.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.byID[hex.EncodeToString(userID)]
	if !ok {
		return nil, xkmswebauthn.ErrUserNotFound
	}
	return user, nil
}

func (s *testWebAuthnUserStore) GetByEmail(_ context.Context, email string) (xkmswebauthn.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	user, ok := s.byID[s.byEmail[email]]
	if !ok {
		return nil, xkmswebauthn.ErrUserNotFound
	}
	return user, nil
}

func (s *testWebAuthnUserStore) Create(_ context.Context, email, displayName string) (xkmswebauthn.User, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.byEmail[email]; ok {
		return nil, xkmswebauthn.ErrUserAlreadyExists
	}
	user := xkmswebauthn.NewDefaultUserFromEmail(email, displayName)
	s.storeLocked(user)
	return user, nil
}

func (s *testWebAuthnUserStore) Save(_ context.Context, user xkmswebauthn.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storeLocked(user)
	return nil
}

func (s *testWebAuthnUserStore) Delete(_ context.Context, userID []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := hex.EncodeToString(userID)
	user, ok := s.byID[key]
	if !ok {
		return xkmswebauthn.ErrUserNotFound
	}
	delete(s.byID, key)
	delete(s.byEmail, user.Email())
	return nil
}

func (s *testWebAuthnUserStore) storeLocked(user xkmswebauthn.User) {
	key := hex.EncodeToString(user.WebAuthnID())
	s.byID[key] = user
	s.byEmail[user.Email()] = key
}

var _ xkmswebauthn.UserStore = (*testWebAuthnUserStore)(nil)

// newTestWebAuthnCredentialStore returns the credential half of a fresh
// store pair.
func newTestWebAuthnCredentialStore(t testing.TB) xkmswebauthn.CredentialStore {
	t.Helper()
	_, creds := newTestWebAuthnStores(t)
	return creds
}

// =============================================================================
// Fixture
// =============================================================================

// renameFixture is the production wiring over in-memory qrdb clients: the xkms
// qrdb store pair, the fido2 write-through decorating its credential half, a
// relying party over both, and the manager the admin verbs run through.
type renameFixture struct {
	mgr     *XKMSWebAuthnManager
	creds   xkmssdk.WebAuthnCredentialStore
	users   *renameTestUserClient
	factors *reapableFactorClient
	userID  uint64
}

// newRenameFixture creates one enrolled account and returns the wiring around
// it. displayName and email are the record's own fields, exactly as the
// adapter's Create writes them.
func newRenameFixture(t *testing.T, email, displayName string) *renameFixture {
	t.Helper()

	userClient := newRenameTestUserClient()
	users, creds, err := xkmssdk.NewWebAuthnQRDBUserStoreAdapter(xkmssdk.WebAuthnQRDBUserStoreOptions{
		Users:      userClient,
		DAOFactory: newTestWebAuthnDAOFactory(),
		TenantID:   testWriteThroughTenant,
	})
	if err != nil {
		t.Fatalf("NewWebAuthnQRDBUserStoreAdapter: %v", err)
	}

	created, err := users.Create(context.Background(), email, displayName)
	if err != nil {
		t.Fatalf("create webauthn user: %v", err)
	}

	factors := &reapableFactorClient{}
	writeThrough, err := newWebAuthnFactorWriteThrough(
		creds, users, factors, recorderOf(factors), testWriteThroughTenant, permissiveTestGate(), nil)
	if err != nil {
		t.Fatalf("newWebAuthnFactorWriteThrough: %v", err)
	}

	svc, err := xkmssdk.NewWebAuthnService(xkmssdk.WebAuthnServiceParams{
		Config: &xkmssdk.WebAuthnConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		UserStore:       users,
		SessionStore:    xkmssdk.NewWebAuthnMemorySessionStore(),
		CredentialStore: writeThrough,
	})
	if err != nil {
		t.Fatalf("NewWebAuthnService: %v", err)
	}

	factory, err := NewWebAuthnServiceFactory(WebAuthnFactoryParams{
		Config: WebAuthnFactoryConfig{
			RPID:          "example.com",
			RPDisplayName: "Example",
			RPOrigins:     []string{"https://example.com"},
		},
		QRDB:                dummyQRDBClient,
		AttestationRecorder: recorderOf(factors),
	})
	if err != nil {
		t.Fatalf("NewWebAuthnServiceFactory: %v", err)
	}
	factory.services.Store(testWriteThroughTenant, svc)

	mgr, err := NewXKMSWebAuthnManager(factory)
	if err != nil {
		t.Fatalf("NewXKMSWebAuthnManager: %v", err)
	}

	userID, err := xkmswebauthn.UserIDFromHandle(created.WebAuthnID())
	if err != nil {
		t.Fatalf("decode webauthn handle: %v", err)
	}

	fixture := &renameFixture{mgr: mgr, creds: writeThrough, users: userClient, factors: factors, userID: userID}
	return fixture
}

// enroll registers a credential under the fixture's account through the same
// write-through the ceremony's FinishRegistration drives.
func (f *renameFixture) enroll(t *testing.T, seed byte) string {
	t.Helper()
	rawID := rawCredentialID(seed)
	if err := f.creds.Save(context.Background(), testCredential(f.userID, rawID)); err != nil {
		t.Fatalf("enroll credential %x: %v", rawID, err)
	}
	return base64.RawURLEncoding.EncodeToString(rawID)
}

// rename applies an identity edit to the qrdb record, exactly as the identity
// store's qrdb adapter does: non-empty fields replace, the record keeps its id.
func (f *renameFixture) rename(t *testing.T, req *qrdbtransport.UpdateUserRequest) {
	t.Helper()
	req.ID = f.userID
	if _, err := f.users.UpdateUser(context.Background(), req); err != nil {
		t.Fatalf("rename user: %v", err)
	}
}

// listedCredentialIDs returns the credential ids the admin listing reports for
// the address, sorted so an assertion does not depend on store iteration order.
func (f *renameFixture) listedCredentialIDs(t *testing.T, email string) []string {
	t.Helper()
	creds, err := f.mgr.ListCredentials(context.Background(), testWriteThroughTenant, email)
	if err != nil {
		t.Fatalf("ListCredentials(%q): %v", email, err)
	}
	ids := make([]string, 0, len(creds))
	for _, cred := range creds {
		ids = append(ids, cred.ID)
	}
	sort.Strings(ids)
	return ids
}

// offeredCredentialIDs returns the ids login/begin puts in allowCredentials for
// the address — the list the browser's get() is restricted to, and therefore
// the operational answer to "can this user still sign in with their passkey?".
func (f *renameFixture) offeredCredentialIDs(t *testing.T, email string) []string {
	t.Helper()
	options, err := f.mgr.BeginLogin(context.Background(), testWriteThroughTenant, email)
	if err != nil {
		t.Fatalf("BeginLogin(%q): %v", email, err)
	}
	publicKey, ok := options["publicKey"].(map[string]any)
	if !ok {
		t.Fatalf("BeginLogin(%q) returned no publicKey object: %#v", email, options)
	}
	allowed, ok := publicKey["allowCredentials"].([]any)
	if !ok {
		t.Fatalf("BeginLogin(%q) returned no allowCredentials: %#v", email, publicKey)
	}
	ids := make([]string, 0, len(allowed))
	for _, entry := range allowed {
		descriptor, ok := entry.(map[string]any)
		if !ok {
			t.Fatalf("allowCredentials entry is not an object: %#v", entry)
		}
		id, ok := descriptor["id"].(string)
		if !ok {
			t.Fatalf("allowCredentials entry has no string id: %#v", descriptor)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// factorCredentialIDs returns the credential ids of the qrdb fido2 login
// factors enrolled for the account's USER ID — the records the unified login
// ceremony resolves an assertion against.
func (f *renameFixture) factorCredentialIDs(t *testing.T) []string {
	t.Helper()
	resp, err := f.factors.ListAuthFactors(context.Background(), &qrdbtransport.AuthFactorListRequest{UserID: f.userID})
	if err != nil {
		t.Fatalf("ListAuthFactors: %v", err)
	}
	ids := make([]string, 0, len(resp.Factors))
	for _, factor := range resp.Factors {
		ids = append(ids, string(factor.Credential))
	}
	sort.Strings(ids)
	return ids
}

func sortedStrings(in ...string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// =============================================================================
// Tests
// =============================================================================

// TestRename_NameOnlyKeepsBothCredentials is the first regression the issue
// asks for: enroll two authenticators, change only the display name, and both
// must still be listed AND still be offered by login/begin. A display-name edit
// touches no lookup key at all, so anything it costs the user is a defect in
// the update path rather than in the store.
func TestRename_NameOnlyKeepsBothCredentials(t *testing.T) {
	f := newRenameFixture(t, "alice@example.com", "Alice")
	first := f.enroll(t, 0x10)
	second := f.enroll(t, 0x40)
	want := sortedStrings(first, second)

	f.rename(t, &qrdbtransport.UpdateUserRequest{Name: "Alice Smith"})

	if got := f.listedCredentialIDs(t, "alice@example.com"); !slices.Equal(got, want) {
		t.Errorf("listed credentials after a name change = %v, want %v", got, want)
	}
	if got := f.offeredCredentialIDs(t, "alice@example.com"); !slices.Equal(got, want) {
		t.Errorf("login/begin offered %v after a name change, want %v", got, want)
	}
	if got := f.factorCredentialIDs(t); !slices.Equal(got, want) {
		t.Errorf("qrdb fido2 factors after a name change = %v, want %v", got, want)
	}
}

// TestRename_EmailChangeCarriesCredentialOwnership is the measurement that
// settles go-quicbaas#582. The credentials are filed under the qrdb user id, so
// changing the address the user answers to re-addresses them and re-keys
// nothing: the new address resolves the same two credentials, login/begin still
// offers them, the fido2 factors are untouched, and the OLD address resolves to
// nothing — there is no orphan for a revocation to prevent.
func TestRename_EmailChangeCarriesCredentialOwnership(t *testing.T) {
	f := newRenameFixture(t, "old@example.com", "Alice")
	first := f.enroll(t, 0x10)
	second := f.enroll(t, 0x40)
	want := sortedStrings(first, second)

	f.rename(t, &qrdbtransport.UpdateUserRequest{
		Email:    "new@example.com",
		Username: "alice-smith",
		Name:     "Alice Smith",
	})

	if got := f.listedCredentialIDs(t, "new@example.com"); !slices.Equal(got, want) {
		t.Errorf("listed credentials under the new address = %v, want %v", got, want)
	}
	if got := f.offeredCredentialIDs(t, "new@example.com"); !slices.Equal(got, want) {
		t.Errorf("login/begin under the new address offered %v, want %v", got, want)
	}
	if got := f.factorCredentialIDs(t); !slices.Equal(got, want) {
		t.Errorf("qrdb fido2 factors after an email change = %v, want %v", got, want)
	}
	if got := f.listedCredentialIDs(t, "old@example.com"); len(got) != 0 {
		t.Errorf("the old address still resolves %v; a rename must leave nothing behind", got)
	}
}

// TestRename_OldAddressIsFreeForANewAccount closes the reason the revocation
// was introduced. The concern was that a credential left under the old address
// would land in the next registrant's excludeCredentials and make that address
// permanently unenrollable. It cannot: a new account under the old address gets
// a new qrdb id, and the credential index is keyed by id, so the newcomer
// starts with none.
func TestRename_OldAddressIsFreeForANewAccount(t *testing.T) {
	f := newRenameFixture(t, "old@example.com", "Alice")
	f.enroll(t, 0x10)
	f.rename(t, &qrdbtransport.UpdateUserRequest{Email: "new@example.com"})

	successor, err := f.users.CreateUser(context.Background(), &qrdbtransport.CreateUserRequest{
		Username: "old@example.com",
		Email:    "old@example.com",
		Name:     "Bob",
		TenantID: testWriteThroughTenant,
	})
	if err != nil {
		t.Fatalf("create the successor account: %v", err)
	}
	if successor.ID == f.userID {
		t.Fatal("the successor must be a distinct qrdb record")
	}

	if got := f.listedCredentialIDs(t, "old@example.com"); len(got) != 0 {
		t.Errorf("the successor account inherited %v; the old address must enroll clean", got)
	}
	if _, err := f.mgr.BeginLogin(context.Background(), testWriteThroughTenant, "old@example.com"); err == nil {
		t.Error("login/begin for an account with no credentials must fail closed")
	}
}
