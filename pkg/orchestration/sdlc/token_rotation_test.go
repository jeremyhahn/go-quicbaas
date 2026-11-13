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

package sdlc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
	"github.com/jeremyhahn/go-quicbaas/pkg/scheduler"
	"github.com/jeremyhahn/go-quicbaas/pkg/secrets"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/secure"
)

// =============================================================================
// Token rotation
// =============================================================================
//
// Rotation has exactly one catastrophic failure: destroying a working
// credential in favour of one that does not work. Everything else is
// recoverable — an orphaned token can be revoked later, a failed mint can be
// retried — but an agent whose only credential is dead cannot fix itself.
//
// So these tests assert ORDER and OMISSION, not just outcomes: that the old
// token is revoked only AFTER the replacement authenticated, and that on any
// verification failure it is not revoked at all.

// fakeLifecycle records calls in order.
type fakeLifecycle struct {
	calls []string

	mintErr   error
	verifyErr error
	revokeErr error

	// verifyNil makes VerifyToken answer (nil, nil) — the adapter that
	// claims success while answering nothing.
	verifyNil bool

	// mintNil makes MintToken answer (nil, nil); mintedName overrides the
	// returned token name; mintEmptySecret returns a response whose secret
	// is empty. Each models a forge adapter answer nothing downstream can
	// trust.
	mintNil         bool
	mintedName      string
	mintEmptySecret bool
	mintEmptyName   bool

	// verifyAs is the account VerifyToken reports.
	verifyAs string

	// mintedSecret is handed back from MintToken; mintNilBuffer answers a
	// response whose Secret buffer is nil outright.
	mintedSecret  string
	mintNilBuffer bool

	// mintedTokens retains every *MintedToken this fake returned — a
	// test-only retention so callers' wipe-on-exit of the owned secret
	// bytes is observable after the fact.
	mintedTokens []*MintedToken

	// revoked names every token revocation was asked to destroy;
	// revokedBy records, in step, the account each revoke acted as.
	revoked   []string
	revokedBy []string

	// verifiedWith records the token verification authenticated as.
	verifiedWith string

	// onRevoke, when set, observes each revocation at the moment it happens,
	// so tests can assert what other state held at that instant.
	onRevoke func(name string)

	// onMint, when set, observes each mint at the moment it happens — the
	// seam a concurrency test uses to hold a minter open while a racer that
	// should have been serialized tries to join it.
	onMint func(name string)
}

func (f *fakeLifecycle) TokenCapabilities() TokenLifecycleCapabilities {
	return TokenLifecycleCapabilities{ScopedTokens: true, RevokeByName: true}
}

func (f *fakeLifecycle) MintToken(_ context.Context, spec TokenSpec) (*MintedToken, error) {
	f.calls = append(f.calls, "mint:"+spec.Name+":"+strings.Join(spec.Scopes, ","))
	if f.onMint != nil {
		f.onMint(spec.Name)
	}
	if f.mintErr != nil {
		return nil, f.mintErr
	}
	if f.mintNil {
		return nil, nil
	}
	secret := f.mintedSecret
	if secret == "" {
		secret = "new-secret"
	}
	if f.mintEmptySecret {
		secret = ""
	}
	minted := &MintedToken{ID: "1"}
	if f.mintNilBuffer {
		secret = ""
	}
	if secret != "" {
		// The fake constructs the enclave exactly as the adapter does; the
		// retained pointer lets tests observe the caller's Destroy.
		buf, err := secure.NewBufferFromBytes([]byte(secret))
		if err != nil {
			panic(err)
		}
		minted.Secret = buf
	}
	f.mintedTokens = append(f.mintedTokens, minted)
	name := spec.Name
	if f.mintedName != "" {
		name = f.mintedName
	}
	if f.mintEmptyName {
		name = ""
	}
	minted.Name = name
	return minted, nil
}

func (f *fakeLifecycle) VerifyToken(_ context.Context, token []byte) (*TokenIdentity, error) {
	f.calls = append(f.calls, "verify")
	// The string copy is the fake RECORDING what it observed for later
	// assertion, made before the caller wipes the borrowed view; production
	// implementations are bound by the no-retain contract instead.
	f.verifiedWith = string(token)
	if f.verifyErr != nil {
		return nil, f.verifyErr
	}
	if f.verifyNil {
		return nil, nil
	}
	return &TokenIdentity{Username: f.verifyAs}, nil
}

func (f *fakeLifecycle) RevokeToken(_ context.Context, username, nameOrID string) error {
	f.calls = append(f.calls, "revoke:"+nameOrID)
	f.revokedBy = append(f.revokedBy, username)
	if f.onRevoke != nil {
		f.onRevoke(nameOrID)
	}
	if f.revokeErr != nil {
		return f.revokeErr
	}
	for _, prior := range f.revoked {
		if prior == nameOrID {
			// The truthful forge: a token already revoked is GONE, and
			// deleting it again is a 404. A fake that answered success here
			// hid the recovery wedge finding 4 was about.
			return &NotFoundError{Entity: "gitea token", ID: nameOrID}
		}
	}
	f.revoked = append(f.revoked, nameOrID)
	return nil
}

// fakeVersion is one stored version of a path, so Rollback can RESTORE rather
// than merely record: recovery tests assert on the store's final state, and a
// rollback that changed nothing would let a broken recovery pass them.
type fakeVersion struct {
	data     map[string][]byte
	metadata map[string]string
}

// fakeSecretStore is an in-memory versioned store.
type fakeSecretStore struct {
	calls []string

	data     map[string]map[string][]byte
	metadata map[string]map[string]string
	versions map[string]int
	history  map[string][]fakeVersion

	storeErr        error
	listVersionsErr error
	rollbackErr     error
	destroyErr      error
	rolledTo        map[string]int

	// onStore observes each write at the moment it happens.
	onStore func(s *fakeSecretStore, path string)

	// storeErrAt fails the NEXT write to a path, once, then disarms.
	storeErrAt map[string]error

	// storeCommitErrAt APPLIES the next write to a path and STILL returns an
	// error, once — the ambiguous outcome a real store produces when a
	// timeout fires after the backend committed. A fake whose errors always
	// meant "nothing happened" accepted a world the real store refuses to
	// promise.
	storeCommitErrAt map[string]error

	// getErr, when set for a path, makes Get fail with a TRANSPORT error
	// rather than reporting absence — the case that used to be indistinguishable.
	getErr map[string]error

	// lastGet retains the exact map the most recent Get handed out, so a
	// test can observe what the CALLER did with it afterwards — namely the
	// readStoredToken wipe (review 959: a metadata read must not leave
	// token bytes live in the heap).
	lastGet map[string][]byte
}

func newFakeSecretStore() *fakeSecretStore {
	return &fakeSecretStore{
		data:     map[string]map[string][]byte{},
		metadata: map[string]map[string]string{},
		versions: map[string]int{},
		history:  map[string][]fakeVersion{},
		rolledTo: map[string]int{},
		getErr:   map[string]error{},
	}
}

func (s *fakeSecretStore) Store(_ context.Context, path string, data map[string][]byte, metadata map[string]string) error {
	s.calls = append(s.calls, "store:"+path)
	if s.onStore != nil {
		s.onStore(s, path)
	}
	if s.storeErr != nil {
		return s.storeErr
	}
	if s.storeErrAt != nil {
		if err, armed := s.storeErrAt[path]; armed {
			delete(s.storeErrAt, path)
			return err
		}
	}
	// DEEP-COPIED ON STORE, the way every real backend commits (memory
	// backend copies, the KV/encrypted backends marshal immediately): the
	// caller may hand a borrowed view into an enclave it will destroy the
	// moment Store returns, and a fake that retained the caller's slices
	// would fault on pages production never touches.
	stored := make(map[string][]byte, len(data))
	for k, v := range data {
		cp := make([]byte, len(v))
		copy(cp, v)
		stored[k] = cp
	}
	if s.storeCommitErrAt != nil {
		if err, armed := s.storeCommitErrAt[path]; armed {
			delete(s.storeCommitErrAt, path)
			s.data[path] = stored
			s.metadata[path] = metadata
			s.versions[path]++
			s.history[path] = append(s.history[path], fakeVersion{data: stored, metadata: metadata})
			return err
		}
	}
	s.data[path] = stored
	s.metadata[path] = metadata
	s.versions[path]++
	s.history[path] = append(s.history[path], fakeVersion{data: stored, metadata: metadata})
	return nil
}

func (s *fakeSecretStore) Get(_ context.Context, path string) (map[string][]byte, map[string]string, error) {
	if err, broken := s.getErr[path]; broken {
		return nil, nil, err
	}
	data, ok := s.data[path]
	if !ok {
		// Absence is reported the way the real store reports it. A fake that
		// invented its own error would let the code under test pass here and
		// fail in production, which is the whole point of this distinction.
		return nil, nil, secrets.ErrSecretNotFound
	}
	// DEEP-COPIED, the way every real backend answers Get. The caller now
	// wipes what Get hands back (readStoredToken's metadata-only read); a
	// fake returning live references would have its own stored state zeroed
	// by that wipe — state-sharing production never exhibits.
	out := make(map[string][]byte, len(data))
	for k, v := range data {
		cp := make([]byte, len(v))
		copy(cp, v)
		out[k] = cp
	}
	s.lastGet = out
	return out, s.metadata[path], nil
}

// UseSecret mirrors the real manager's guarded-use read: the value reaches fn
// as a copy that is wiped when fn returns, absence reports as Get does, and a
// present record without a nonempty value under key refuses with
// ErrSecretKeyAbsent (wrapped, as the real manager wraps) without calling fn.
func (s *fakeSecretStore) UseSecret(_ context.Context, path, key string, fn func(value []byte) error) error {
	s.calls = append(s.calls, "use:"+path)
	if err, broken := s.getErr[path]; broken {
		return err
	}
	stored, ok := s.data[path]
	if !ok {
		return fmt.Errorf("fakeSecretStore: use %q: %w", path, secrets.ErrSecretNotFound)
	}
	value := stored[key]
	if len(value) == 0 {
		return fmt.Errorf("fakeSecretStore: use %q key %q: %w", path, key, secrets.ErrSecretKeyAbsent)
	}
	cp := make([]byte, len(value))
	copy(cp, value)
	defer func() {
		for i := range cp {
			cp[i] = 0
		}
	}()
	return fn(cp)
}

func (s *fakeSecretStore) ListVersions(_ context.Context, path string) ([]*interfaces.SecretVersionMetadata, error) {
	if s.listVersionsErr != nil {
		return nil, s.listVersionsErr
	}
	count := s.versions[path]
	if count == 0 {
		// An absent path is an ERROR from the real store: secrets.Manager
		// wraps ErrSecretNotFound rather than answering an empty list. A fake
		// that answered the empty list here accepted what production refuses,
		// and the absent-path first rotation passed against the fake while
		// failing against the real manager. The error is WRAPPED, as the real
		// manager wraps it, so code comparing with == instead of errors.Is
		// still fails against this fake.
		return nil, fmt.Errorf("fakeSecretStore: list versions %q: %w", path, secrets.ErrSecretNotFound)
	}
	out := make([]*interfaces.SecretVersionMetadata, 0, count)
	for v := 1; v <= count; v++ {
		out = append(out, &interfaces.SecretVersionMetadata{Version: v})
	}
	return out, nil
}

func (s *fakeSecretStore) Destroy(_ context.Context, path string) error {
	s.calls = append(s.calls, "destroy:"+path)
	if s.destroyErr != nil {
		return s.destroyErr
	}
	if _, ok := s.data[path]; !ok {
		// The real manager reports destroying an absent path as NotFound
		// rather than success; a fake that answered success would let a
		// double-destroy path pass here and fail in production.
		return fmt.Errorf("fakeSecretStore: destroy %q: %w", path, secrets.ErrSecretNotFound)
	}
	// Destroy removes ALL versions: the path is absent afterwards, the way
	// the real backend leaves it.
	delete(s.data, path)
	delete(s.metadata, path)
	delete(s.versions, path)
	delete(s.history, path)
	delete(s.rolledTo, path)
	return nil
}

func (s *fakeSecretStore) Rollback(_ context.Context, path string, version int) error {
	s.calls = append(s.calls, "rollback:"+path)
	if s.rollbackErr != nil {
		return s.rollbackErr
	}
	// Restore the target version as the current one, the way the real backend
	// does: rollback WRITES a new version holding the old data.
	if history := s.history[path]; version >= 1 && version <= len(history) {
		target := history[version-1]
		s.data[path] = target.data
		s.metadata[path] = target.metadata
		s.versions[path]++
		s.history[path] = append(s.history[path], target)
	}
	s.rolledTo[path] = version
	return nil
}

const (
	testSecretPath = "forge/agent-quicbaas/token"
	testAccount    = "agent-quicbaas"
)

// seedCurrentToken puts an in-force credential at the path.
func seedCurrentToken(s *fakeSecretStore, name string, slot string) {
	s.data[testSecretPath] = map[string][]byte{tokenSecretKey: []byte("old-secret")}
	s.metadata[testSecretPath] = map[string]string{
		metadataTokenName:    name,
		metadataRotationSlot: slot,
	}
	s.versions[testSecretPath] = 1
	s.history[testSecretPath] = []fakeVersion{{data: s.data[testSecretPath], metadata: s.metadata[testSecretPath]}}
}

func testSpec() TokenRotationSpec {
	return TokenRotationSpec{
		Purpose:    "agent-quicbaas",
		Username:   testAccount,
		SecretPath: testSecretPath,
	}
}

// TestRotate_RevokesTheOldTokenOnlyAfterVerifying pins the ORDER, which is the
// property, not the outcome.
func TestRotate_RevokesTheOldTokenOnlyAfterVerifying(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")

	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	result, err := rotator.Rotate(context.Background(), testSpec())
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	if result.NewTokenName != "sdlc-agent-quicbaas-v2" {
		t.Errorf("new token name = %q, want the next slot", result.NewTokenName)
	}
	if result.RevokedTokenName != "sdlc-agent-quicbaas-v1" {
		t.Errorf("revoked = %q, want the previous token", result.RevokedTokenName)
	}

	// mint, then verify, then revoke. Any other order either revokes a
	// credential before its replacement is proven, or proves nothing.
	got := strings.Join(lifecycle.calls, " ")
	want := "mint:sdlc-agent-quicbaas-v2:read:user verify revoke:sdlc-agent-quicbaas-v1"
	if got != want {
		t.Errorf("call order:\n got: %s\nwant: %s", got, want)
	}

	if lifecycle.verifiedWith != "new-secret" {
		t.Errorf("verified with %q, want the NEW token", lifecycle.verifiedWith)
	}
}

// TestRotate_WritesTheIntentBeforeMinting is what makes a crash recoverable.
//
// Minting first and recording after leaves a window where a live credential
// exists that nothing knows the name of.
func TestRotate_WritesTheIntentBeforeMinting(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")

	rotator, _ := NewTokenRotator(lifecycle, store)
	if _, err := rotator.Rotate(context.Background(), testSpec()); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	intentPath := testSecretPath + rotationIntentSuffix
	if len(store.calls) == 0 || store.calls[0] != "store:"+intentPath {
		t.Fatalf("first store call was %v, want the intent record", store.calls)
	}
}

// TestRotate_VerificationFailureLeavesTheOldTokenALIVE is the flagship safety
// test.
//
// An unverified replacement plus a destroyed original is total access loss, and
// nothing the agent can recover from on its own. The replacement is the thing
// that gets thrown away.
func TestRotate_VerificationFailureLeavesTheOldTokenALIVE(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyErr: errors.New("401 unauthorized")}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")

	rotator, _ := NewTokenRotator(lifecycle, store)
	_, err := rotator.Rotate(context.Background(), testSpec())
	if err == nil {
		t.Fatal("a replacement that cannot authenticate must fail the rotation")
	}

	var verifyErr *TokenVerificationError
	if !errors.As(err, &verifyErr) {
		t.Fatalf("err = %v, want a TokenVerificationError", err)
	}

	for _, revoked := range lifecycle.revoked {
		if revoked == "sdlc-agent-quicbaas-v1" {
			t.Fatal("THE OLD TOKEN WAS REVOKED after the replacement failed to " +
				"verify; the account is now locked out with no working credential")
		}
	}

	// The useless replacement is what gets destroyed, and the stored version
	// rolls back so the in-force credential is the one that still works.
	if len(lifecycle.revoked) != 1 || lifecycle.revoked[0] != "sdlc-agent-quicbaas-v2" {
		t.Errorf("revoked = %v, want only the unverifiable replacement", lifecycle.revoked)
	}
	if store.rolledTo[testSecretPath] != 1 {
		t.Errorf("rolled back to version %d, want 1", store.rolledTo[testSecretPath])
	}
}

// TestRotate_WrongAccountIsAVerificationFailure covers the subtler case: the
// token works, but it is somebody else's.
//
// A replacement that authenticates as the wrong account would pass any check
// that only asked "did this call succeed".
func TestRotate_WrongAccountIsAVerificationFailure(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: "somebody-else"}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")

	rotator, _ := NewTokenRotator(lifecycle, store)
	_, err := rotator.Rotate(context.Background(), testSpec())
	if err == nil {
		t.Fatal("a token authenticating as another account must fail verification")
	}

	for _, revoked := range lifecycle.revoked {
		if revoked == "sdlc-agent-quicbaas-v1" {
			t.Fatal("the old token was revoked for a replacement belonging to someone else")
		}
	}
}

// TestRotate_FirstRotationRevokesNothing covers the empty-path case.
func TestRotate_FirstRotationRevokesNothing(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()

	rotator, _ := NewTokenRotator(lifecycle, store)
	result, err := rotator.Rotate(context.Background(), testSpec())
	if err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	if result.Slot != 1 || result.NewTokenName != "sdlc-agent-quicbaas-v1" {
		t.Errorf("result = %+v, want the first slot", result)
	}
	if len(lifecycle.revoked) != 0 {
		t.Errorf("revoked %v on a first rotation, when there was nothing to revoke",
			lifecycle.revoked)
	}
}

// TestRotate_AlwaysRequestsAnIdentityScope guards the precondition that makes
// verification possible at all.
func TestRotate_AlwaysRequestsAnIdentityScope(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()

	rotator, _ := NewTokenRotator(lifecycle, store)
	spec := testSpec()
	spec.Scopes = []string{"write:repository"}

	if _, err := rotator.Rotate(context.Background(), spec); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	if !strings.Contains(lifecycle.calls[0], identityScope) {
		t.Errorf("mint scopes = %q, want %s added: without it the replacement "+
			"cannot verify itself and rotation would revoke on faith",
			lifecycle.calls[0], identityScope)
	}
}

// TestRecoverOrphan_RevokesATokenLeftByACrash exercises the reason the intent
// record exists.
func TestRecoverOrphan_RevokesATokenLeftByACrash(t *testing.T) {
	lifecycle := &fakeLifecycle{}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
	// A rotation recorded its intent and died before storing the result.
	store.data[testSecretPath+rotationIntentSuffix] = map[string][]byte{
		metadataTokenName: []byte("sdlc-agent-quicbaas-v2"),
	}

	rotator, _ := NewTokenRotator(lifecycle, store)
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan: %v", err)
	}

	if len(lifecycle.revoked) != 1 || lifecycle.revoked[0] != "sdlc-agent-quicbaas-v2" {
		t.Errorf("revoked = %v, want the orphan", lifecycle.revoked)
	}
}

// TestRecoverOrphan_NeverRevokesTheTokenInForce is the dangerous false positive.
//
// A stale intent naming the credential that is actually in use would, if acted
// on, destroy the working token — recovery causing the exact outage it exists
// to prevent.
func TestRecoverOrphan_NeverRevokesTheTokenInForce(t *testing.T) {
	lifecycle := &fakeLifecycle{}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v2", "2")
	// The rotation COMPLETED; the intent simply was not cleared.
	store.data[testSecretPath+rotationIntentSuffix] = map[string][]byte{
		metadataTokenName: []byte("sdlc-agent-quicbaas-v2"),
	}

	rotator, _ := NewTokenRotator(lifecycle, store)
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan: %v", err)
	}

	if len(lifecycle.revoked) != 0 {
		t.Fatalf("recovery revoked %v — that is the credential currently in use",
			lifecycle.revoked)
	}
}

// TestRotationTokenName_IsDeterministic pins the contract RevokeByName depends
// on: the same inputs always produce the same name, so an orphan can be found
// by recomputing rather than by remembering.
func TestRotationTokenName_IsDeterministic(t *testing.T) {
	first := RotationTokenName("agent-quicbaas", 7)
	second := RotationTokenName("agent-quicbaas", 7)
	if first != second {
		t.Fatalf("%q != %q", first, second)
	}
	if first == RotationTokenName("agent-quicbaas", 8) {
		t.Fatal("different slots must produce different names")
	}
}

// TestNewTokenRotator_RefusesMissingCollaborators keeps construction fail-closed.
func TestNewTokenRotator_RefusesMissingCollaborators(t *testing.T) {
	if _, err := NewTokenRotator(nil, newFakeSecretStore()); err == nil {
		t.Error("a rotator without a forge cannot rotate")
	}
	if _, err := NewTokenRotator(&fakeLifecycle{}, nil); err == nil {
		t.Error("a rotator without a store would mint tokens it cannot keep")
	}
}

// TestTokenSecretStore_IsSatisfiedByTheRealManager proves the port is not a
// shape invented for the fakes.
//
// A narrow port is only honest if the real implementation fits it: this fails
// to compile the moment secrets.Manager and TokenSecretStore drift apart, which
// is the failure a hand-written mock would otherwise hide indefinitely.
func TestTokenSecretStore_IsSatisfiedByTheRealManager(t *testing.T) {
	var _ TokenSecretStore = (*secrets.Manager)(nil)
}

// =============================================================================
// Factory credentials and scheduler registration
// =============================================================================

// TestForgeFactory_CarriesCustodyThrough is the point of widening the factory.
//
// Before this, an adapter built through the registry could never rotate, because
// the signature had nowhere to put a custody credential. The assertion is that a
// registry-built adapter can MINT — the operation that needs the credential the
// old signature could not carry.
func TestForgeFactory_CarriesCustodyThrough(t *testing.T) {
	backend, err := NewForgeBackend(
		ForgeConfig{Type: ForgeGitea, BaseURL: "http://gitea.example/api/v1"},
		ForgeCredentials{
			Token:   "workflow-token",
			Custody: CustodyCredential{Username: "provisioning", Password: "pw"},
		},
		ForgeTransport{})
	if err != nil {
		t.Fatalf("NewForgeBackend: %v", err)
	}

	custodian, ok := backend.(ForgeTokenLifecycle)
	if !ok {
		t.Fatal("the gitea adapter must serve the custody plane")
	}

	// Reaching the network means the credential was accepted and the call was
	// attempted; a missing custody credential refuses before that.
	_, err = custodian.MintToken(context.Background(), TokenSpec{Username: "a", Name: "b"})
	if errors.Is(err, ErrCustodyCredentialAbsent) {
		t.Fatal("custody did not survive the factory: the adapter refused before " +
			"making a call, which is what the old signature forced")
	}
}

// TestForgeFactory_WithoutCustodyStillBuildsAWorkflowAdapter proves custody is
// OPTIONAL within the widened contract.
//
// Most projects never rotate. They must change one signature and nothing else.
func TestForgeFactory_WithoutCustodyStillBuildsAWorkflowAdapter(t *testing.T) {
	backend, err := NewForgeBackend(
		ForgeConfig{Type: ForgeGitea, BaseURL: "http://gitea.example/api/v1"},
		ForgeCredentials{Token: "workflow-token"},
		ForgeTransport{})
	if err != nil {
		t.Fatalf("a project that never rotates must still get an adapter: %v", err)
	}
	if backend.Type() != ForgeGitea {
		t.Errorf("Type() = %v", backend.Type())
	}

	custodian, ok := backend.(ForgeTokenLifecycle)
	if !ok {
		t.Fatal("the adapter still serves the custody plane; it just has no credential")
	}
	_, err = custodian.MintToken(context.Background(), TokenSpec{Username: "a", Name: "b"})
	if !errors.Is(err, ErrCustodyCredentialAbsent) {
		t.Errorf("err = %v, want a loud refusal rather than a silent no-op", err)
	}
}

// fakeHandlerRegistry captures a registration.
type fakeHandlerRegistry struct {
	name    string
	handler scheduler.JobHandler
}

func (f *fakeHandlerRegistry) RegisterHandler(name string, handler scheduler.JobHandler) error {
	f.name = name
	f.handler = handler
	return nil
}

// TestRegisterTokenRotationHandler_RecoversBeforeRotating pins the ordering
// that makes a scheduled run the cleanup point for a previous crash.
func TestRegisterTokenRotationHandler_RecoversBeforeRotating(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
	// A previous rotation died after recording its intent.
	store.data[testSecretPath+rotationIntentSuffix] = map[string][]byte{
		metadataTokenName: []byte("sdlc-agent-quicbaas-v99"),
	}

	rotator, _ := NewTokenRotator(lifecycle, store)
	registry := &fakeHandlerRegistry{}
	if err := RegisterTokenRotationHandler(registry, rotator); err != nil {
		t.Fatalf("RegisterTokenRotationHandler: %v", err)
	}
	if registry.name != RotationHandlerName {
		t.Errorf("registered as %q, want %q", registry.name, RotationHandlerName)
	}

	payload, err := json.Marshal(testSpec())
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	result, err := registry.handler(context.Background(), &scheduler.Job{Payload: payload})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	if !result.Success {
		t.Error("a completed rotation must report success")
	}

	// The orphan is revoked first, then the rotation proceeds.
	if len(lifecycle.revoked) == 0 || lifecycle.revoked[0] != "sdlc-agent-quicbaas-v99" {
		t.Errorf("revoked = %v, want the orphan cleaned up first", lifecycle.revoked)
	}
}

// TestRegisterTokenRotationHandler_ResultCarriesNoSecret guards what gets
// stored and shown.
//
// A JobResult is persisted, logged and rendered to operators. The credential
// this job just minted must not travel with it.
func TestRegisterTokenRotationHandler_ResultCarriesNoSecret(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount, mintedSecret: "SUPER-SECRET-VALUE"}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")

	rotator, _ := NewTokenRotator(lifecycle, store)
	registry := &fakeHandlerRegistry{}
	if err := RegisterTokenRotationHandler(registry, rotator); err != nil {
		t.Fatalf("RegisterTokenRotationHandler: %v", err)
	}

	payload, _ := json.Marshal(testSpec())
	result, err := registry.handler(context.Background(), &scheduler.Job{Payload: payload})
	if err != nil {
		t.Fatalf("handler: %v", err)
	}

	if strings.Contains(result.Message, "SUPER-SECRET-VALUE") ||
		strings.Contains(string(result.Output), "SUPER-SECRET-VALUE") {
		t.Fatal("the minted credential reached the job result, which is stored and logged")
	}
}

// TestRegisterTokenRotationHandler_RejectsAPayloadThatIsNotASpec keeps a
// misconfigured job loud.
func TestRegisterTokenRotationHandler_RejectsAPayloadThatIsNotASpec(t *testing.T) {
	rotator, _ := NewTokenRotator(&fakeLifecycle{}, newFakeSecretStore())
	registry := &fakeHandlerRegistry{}
	if err := RegisterTokenRotationHandler(registry, rotator); err != nil {
		t.Fatalf("RegisterTokenRotationHandler: %v", err)
	}

	_, err := registry.handler(context.Background(), &scheduler.Job{Payload: []byte("not json")})
	var invalid *InvalidInputError
	if !errors.As(err, &invalid) {
		t.Fatalf("err = %v, want an InvalidInputError naming the payload: a bare "+
			"'some error happened' assertion would pass if the handler failed "+
			"for an entirely different reason", err)
	}
}

// TestSchedulerHandlerRegistry_IsSatisfiedByTheRealEngine is the drift detector
// for the other port.
func TestSchedulerHandlerRegistry_IsSatisfiedByTheRealEngine(t *testing.T) {
	var _ SchedulerHandlerRegistry = (*scheduler.SchedulerEngine)(nil)
}

// =============================================================================
// Absent versus unreadable
// =============================================================================
//
// These cover the finding that a boundary review caught and the original tests
// did not: the guards protecting the in-force credential all sat behind a
// SUCCESSFUL read, so a transient store failure walked straight past them.
// "Nothing is stored" and "I cannot read what is stored" are different facts,
// and only the first is safe to act on.

// TestRecoverOrphan_RefusesWhenTheCurrentTokenCannotBeRead is the regression for
// the critical case.
//
// The intent names a token; the store cannot say whether that token is the live
// one. Revoking on that evidence risks destroying the credential in force, so
// recovery must refuse and leave the orphan for a later pass.
func TestRecoverOrphan_RefusesWhenTheCurrentTokenCannotBeRead(t *testing.T) {
	lifecycle := &fakeLifecycle{}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v2", "2")
	store.data[testSecretPath+rotationIntentSuffix] = map[string][]byte{
		metadataTokenName: []byte("sdlc-agent-quicbaas-v2"),
	}
	// The credential record is unreadable — NOT absent.
	store.getErr[testSecretPath] = errors.New("storage backend unavailable")

	rotator, _ := NewTokenRotator(lifecycle, store)
	err := rotator.RecoverOrphan(context.Background(), testSpec())
	if err == nil {
		t.Fatal("recovery must refuse when it cannot prove the intent names an orphan")
	}

	if len(lifecycle.revoked) != 0 {
		t.Fatalf("REVOKED %v ON AN UNREADABLE STORE — that token may be the "+
			"credential currently in force", lifecycle.revoked)
	}
}

// TestRotate_RefusesWhenTheCurrentTokenCannotBeRead is the sibling case.
//
// Treating an unreadable record as "nothing stored" would restart the slot
// counter at 1, mint a name that already exists on the forge, and revoke
// nothing — losing the rotation lineage silently rather than loudly.
func TestRotate_RefusesWhenTheCurrentTokenCannotBeRead(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v7", "7")
	store.getErr[testSecretPath] = errors.New("storage backend unavailable")

	rotator, _ := NewTokenRotator(lifecycle, store)
	result, err := rotator.Rotate(context.Background(), testSpec())
	if err == nil {
		t.Fatalf("rotation proceeded on an unreadable store and produced %+v", result)
	}

	if len(lifecycle.calls) != 0 {
		t.Errorf("touched the forge before it could read its own state: %v", lifecycle.calls)
	}
}

// TestRotate_AbsentIsStillAnOrdinaryFirstRotation proves the fix did not turn
// the ordinary case into a failure.
//
// Failing closed on unreadable is only correct if ABSENT still works — otherwise
// no credential could ever be rotated for the first time.
func TestRotate_AbsentIsStillAnOrdinaryFirstRotation(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore() // nothing stored at all

	rotator, _ := NewTokenRotator(lifecycle, store)
	result, err := rotator.Rotate(context.Background(), testSpec())
	if err != nil {
		t.Fatalf("a first rotation must succeed against an empty path: %v", err)
	}
	if result.Slot != 1 {
		t.Errorf("slot = %d, want 1", result.Slot)
	}
}

// TestRecoverOrphan_AbsentIntentIsNotAnError is the same guard on the intent
// side: no intent recorded means nothing was in flight, which is the normal
// steady state.
func TestRecoverOrphan_AbsentIntentIsNotAnError(t *testing.T) {
	lifecycle := &fakeLifecycle{}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")

	rotator, _ := NewTokenRotator(lifecycle, store)
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("no intent recorded is the steady state, not a failure: %v", err)
	}
	if len(lifecycle.revoked) != 0 {
		t.Errorf("revoked %v with no intent recorded", lifecycle.revoked)
	}
}

// TestRecoverOrphan_UnreadableIntentIsAnErrorNotSilence distinguishes the two
// failure modes on the intent record itself.
func TestRecoverOrphan_UnreadableIntentIsAnErrorNotSilence(t *testing.T) {
	lifecycle := &fakeLifecycle{}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
	store.getErr[testSecretPath+rotationIntentSuffix] = errors.New("storage backend unavailable")

	rotator, _ := NewTokenRotator(lifecycle, store)
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err == nil {
		t.Fatal("an unreadable intent record must surface, not be read as 'no intent'")
	}
}

// =============================================================================
// The three #87 recovery findings (Codex 837 on #83)
// =============================================================================

// TestRotate_VersionEnumerationFailureAbortsBeforeStoring: a listing failure
// used to become version 0, and a later verification failure then skipped the
// rollback and revoked the replacement — a store pointing at a revoked token.
// The rollback target is now established before the forge is touched, so the
// abort costs NOTHING: no token minted, no orphan to revoke, store untouched.
//
// Mutation evidence: collapsing the listing failure back to (0, nil) stores
// the replacement and fails the store-untouched assertion.
func TestRotate_VersionEnumerationFailureAbortsBeforeStoring(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "agent-quicbaas-r1", "1")
	store.listVersionsErr = errors.New("version index unreachable")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	_, err = rotator.Rotate(context.Background(), testSpec())
	if err == nil {
		t.Fatal("a rotation proceeded with an unknowable rollback target")
	}
	if got := string(store.data[testSecretPath][tokenSecretKey]); got != "old-secret" {
		t.Fatalf("the stored credential moved to %q before the abort", got)
	}
	if len(lifecycle.calls) != 0 {
		t.Fatalf("the forge was touched (%v) for a rotation that could not know its rollback target", lifecycle.calls)
	}
	for _, call := range store.calls {
		if call == "rollback:"+testSecretPath {
			t.Fatal("a rollback ran for a rotation that never stored")
		}
	}
}

// TestRotate_RollbackFailureStopsTheRevokeAndReportsBoth: with the rollback
// lost, the store still points at the replacement — revoking it then would
// destroy the only credential the store holds. The revoke is refused, the
// intent stays for recovery, and both failures come back together.
//
// Mutation evidence: discarding the rollback error revokes the replacement
// and fails the revoked-nothing assertion.
func TestRotate_RollbackFailureStopsTheRevokeAndReportsBoth(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyErr: errors.New("401 unauthorized")}
	store := newFakeSecretStore()
	seedCurrentToken(store, "agent-quicbaas-r1", "1")
	store.rollbackErr = errors.New("rollback plane down")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	_, err = rotator.Rotate(context.Background(), testSpec())
	var recovery *TokenRotationRecoveryError
	if !errors.As(err, &recovery) {
		t.Fatalf("err = %v, want TokenRotationRecoveryError", err)
	}
	var verification *TokenVerificationError
	if !errors.As(err, &verification) {
		t.Fatalf("the compound error does not expose the verification failure: %v", err)
	}
	if !strings.Contains(err.Error(), "rollback plane down") {
		t.Fatalf("the compound error does not carry the rollback failure: %v", err)
	}
	if len(lifecycle.revoked) != 0 {
		t.Fatalf("revoked %v: the store still points at the replacement, and revoking it "+
			"destroys the only credential the store holds", lifecycle.revoked)
	}
	// The intent survives for the next recovery pass.
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != RotationTokenName("agent-quicbaas", 2) {
		t.Fatalf("the intent record was discharged or lost: %q", name)
	}
}

// TestRotate_NilIdentityIsAVerificationFailureNotAPanic: an adapter answering
// (nil, nil) used to crash the rotation worker after the replacement was
// persisted. It is the same class as a failed verification and takes the same
// rollback path.
//
// Mutation evidence: removing the nil-identity guard panics this test.
func TestRotate_NilIdentityIsAVerificationFailureNotAPanic(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyNil: true}
	store := newFakeSecretStore()
	seedCurrentToken(store, "agent-quicbaas-r1", "1")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	_, err = rotator.Rotate(context.Background(), testSpec())
	var verification *TokenVerificationError
	if !errors.As(err, &verification) {
		t.Fatalf("err = %v, want TokenVerificationError", err)
	}
	if store.rolledTo[testSecretPath] != 1 {
		t.Fatalf("rolled back to %d, want the previous version 1", store.rolledTo[testSecretPath])
	}
	if len(lifecycle.revoked) != 1 || lifecycle.revoked[0] != RotationTokenName("agent-quicbaas", 2) {
		t.Fatalf("revoked %v, want the unverified replacement", lifecycle.revoked)
	}
}

// =============================================================================
// The #102 findings (Codex round 1 on the token-rotation PR)
// =============================================================================

// TestRotate_FirstRotationThroughTheRealSecretsManager drives the absent-path
// first rotation, and the ordinary second rotation after it, through the real
// secrets.Manager over the real KVBackend over the canonical kvstore memory
// engine — the deployed composition, fakes nowhere in the storage column.
//
// The property under test is the CONTRACT SEAM: the real manager reports an
// absent path from ListVersions as an error wrapping ErrSecretNotFound, where
// the fake once answered an empty list. Against the unfixed latestVersion this
// test fails exactly as production would: the first rotation mints a token,
// then revokes it without ever storing it.
func TestRotate_FirstRotationThroughTheRealSecretsManager(t *testing.T) {
	manager, err := secrets.NewManager(&secrets.ManagerOptions{
		Backend: secrets.NewKVBackend(kvstore.NewMemory()),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	rotator, err := NewTokenRotator(lifecycle, manager)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	ctx := context.Background()
	first, err := rotator.Rotate(ctx, testSpec())
	if err != nil {
		t.Fatalf("a first rotation against the real manager must succeed: %v", err)
	}
	if first.Slot != 1 || first.RevokedTokenName != "" {
		t.Fatalf("first rotation = %+v, want slot 1 with nothing revoked", first)
	}

	// The stored credential is the minted one — stored, not just returned.
	data, meta, err := manager.Get(ctx, testSecretPath)
	if err != nil {
		t.Fatalf("Get after first rotation: %v", err)
	}
	if got := string(data[tokenSecretKey]); got != "new-secret" {
		t.Fatalf("stored secret = %q, want the minted token", got)
	}
	if meta[metadataTokenName] != first.NewTokenName {
		t.Fatalf("stored token name = %q, want %q", meta[metadataTokenName], first.NewTokenName)
	}

	// The second rotation is the ordinary case on a now-populated path: the
	// real ListVersions answers a real listing, and the first credential is
	// revoked only after the replacement verifies.
	second, err := rotator.Rotate(ctx, testSpec())
	if err != nil {
		t.Fatalf("second rotation: %v", err)
	}
	if second.Slot != 2 || second.RevokedTokenName != first.NewTokenName {
		t.Fatalf("second rotation = %+v, want slot 2 revoking %q", second, first.NewTokenName)
	}
}

// failThenRecoverRotation drives a rotation into the stuck state finding 2 is
// about: verification failed AND the rollback failed, so the store points at
// an unverified replacement while the previous credential is still live on the
// forge, known only to the intent record.
func failThenRecoverRotation(t *testing.T) (*fakeLifecycle, *fakeSecretStore, *TokenRotator) {
	t.Helper()
	lifecycle := &fakeLifecycle{verifyErr: errors.New("401 unauthorized")}
	store := newFakeSecretStore()
	seedCurrentToken(store, "agent-quicbaas-r1", "1")
	store.rollbackErr = errors.New("rollback plane down")

	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}
	if _, err := rotator.Rotate(context.Background(), testSpec()); err == nil {
		t.Fatal("the rotation was supposed to fail with the rollback down")
	}
	return lifecycle, store, rotator
}

// TestRecoverOrphan_RetriesTheRollbackBeforeRevoking is finding 2's damage
// plane: after a rotation whose rollback failed, the intent names the
// replacement and the store POINTS AT the replacement. A recovery that
// compares the two, sees them equal, and declares the rotation complete
// leaves an unverified credential in force and the real one unmanaged; the
// next rotation then revokes the replacement and overwrites the intent,
// losing the live original's identity permanently.
//
// The assertions are on the store's final state, not on return values: the
// previous credential back in force, the unverified replacement revoked, the
// intent discharged.
func TestRecoverOrphan_RetriesTheRollbackBeforeRevoking(t *testing.T) {
	lifecycle, store, rotator := failThenRecoverRotation(t)

	// The rollback plane comes back; the next recovery pass runs.
	store.rollbackErr = nil
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan with the rollback plane restored: %v", err)
	}

	replacement := RotationTokenName("agent-quicbaas", 2)
	if got := string(store.data[testSecretPath][tokenSecretKey]); got != "old-secret" {
		t.Fatalf("stored credential = %q, want the PREVIOUS credential rolled back into force", got)
	}
	if got := store.metadata[testSecretPath][metadataTokenName]; got != "agent-quicbaas-r1" {
		t.Fatalf("stored token name = %q, want the previous credential's", got)
	}
	if len(lifecycle.revoked) != 1 || lifecycle.revoked[0] != replacement {
		t.Fatalf("revoked %v, want exactly the unverified replacement %q", lifecycle.revoked, replacement)
	}
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != "" {
		t.Fatalf("intent record still names %q after a completed recovery", name)
	}

	// And the rotation lineage survives: the NEXT rotation revokes the
	// restored original, not a ghost.
	lifecycle.verifyErr = nil
	lifecycle.verifyAs = testAccount
	result, err := rotator.Rotate(context.Background(), testSpec())
	if err != nil {
		t.Fatalf("rotation after recovery: %v", err)
	}
	if result.RevokedTokenName != "agent-quicbaas-r1" {
		t.Fatalf("post-recovery rotation revoked %q, want the restored original", result.RevokedTokenName)
	}
}

// TestRecoverOrphan_FailedRollbackRetryStopsTheRevoke: recovery that cannot
// restore the previous credential must not revoke anything — the replacement
// is the only credential the store holds — and must leave the recovery state
// standing for the pass after it.
func TestRecoverOrphan_FailedRollbackRetryStopsTheRevoke(t *testing.T) {
	lifecycle, store, rotator := failThenRecoverRotation(t)

	// The rollback plane is STILL down.
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err == nil {
		t.Fatal("recovery claimed success while the rollback plane is still down")
	}
	if len(lifecycle.revoked) != 0 {
		t.Fatalf("revoked %v with the rollback still lost", lifecycle.revoked)
	}
	replacement := RotationTokenName("agent-quicbaas", 2)
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != replacement {
		t.Fatalf("recovery state lost: intent names %q, want %q", name, replacement)
	}

	// The pass after it, with the plane restored, still completes.
	store.rollbackErr = nil
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("recovery after the plane came back: %v", err)
	}
	if got := string(store.data[testSecretPath][tokenSecretKey]); got != "old-secret" {
		t.Fatalf("stored credential = %q, want the previous credential restored", got)
	}
}

// TestRotate_RefusesWhileARollbackIsPending: a new rotation over the stuck
// state would read the replacement as current, mint on top of it, revoke it
// and overwrite the intent — exactly the corruption recovery exists to stop.
// Rotation fails closed until recovery has run.
func TestRotate_RefusesWhileARollbackIsPending(t *testing.T) {
	lifecycle, store, rotator := failThenRecoverRotation(t)
	mintsBefore := len(lifecycle.calls)

	result, err := rotator.Rotate(context.Background(), testSpec())
	if err == nil {
		t.Fatalf("rotation proceeded over a pending rollback and produced %+v", result)
	}
	var pending *RotationRecoveryPendingError
	if !errors.As(err, &pending) {
		t.Fatalf("err = %v, want RotationRecoveryPendingError naming the stuck state", err)
	}
	if pending.Name != RotationTokenName("agent-quicbaas", 2) {
		t.Fatalf("refusal names %q, want the unverified replacement", pending.Name)
	}
	if len(lifecycle.calls) != mintsBefore {
		t.Fatalf("touched the forge over a pending rollback: %v", lifecycle.calls[mintsBefore:])
	}
	replacement := RotationTokenName("agent-quicbaas", 2)
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != replacement {
		t.Fatalf("the refused rotation still overwrote the recovery state: intent names %q", name)
	}
}

// TestRotate_StagesTheIntentBeforeStoringTheReplacement pins the crash-window
// property: at the MOMENT the replacement hits the store, the intent record
// already carries the staging marker with the rollback target. A crash
// between the store and the verification then leaves a record recovery can
// act on; staged-after-failure left that window with an intent
// indistinguishable from a completed rotation.
//
// Mutation evidence: moving the staging write after the store (or dropping
// it) fails the at-store assertion.
func TestRotate_StagesTheIntentBeforeStoringTheReplacement(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "agent-quicbaas-r1", "1")

	var markerAtStore string
	var sawStore bool
	store.onStore = func(s *fakeSecretStore, path string) {
		if path == testSecretPath {
			sawStore = true
			markerAtStore = s.metadata[testSecretPath+rotationIntentSuffix][metadataPendingRollback]
		}
	}

	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}
	if _, err := rotator.Rotate(context.Background(), testSpec()); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	if !sawStore {
		t.Fatal("the replacement was never stored")
	}
	if markerAtStore != "1" {
		t.Fatalf("staging marker at the moment of the store = %q, want the rollback target %q", markerAtStore, "1")
	}
	// And after a CLEAN rotation the marker is gone: the record must not
	// leave a completed rotation looking stuck.
	if got := store.metadata[testSecretPath+rotationIntentSuffix][metadataPendingRollback]; got != "" {
		t.Fatalf("staging marker survived a completed rotation: %q", got)
	}
}

// TestRotate_ClearsTheMarkerBeforeRevokingTheOldToken pins the one safe order
// on the success path: revoke-then-clear leaves a crash window in which
// recovery rolls back to a REVOKED credential — lockout through the safety
// path. At the moment the old token is revoked, the marker must already be
// gone.
//
// Mutation evidence: swapping the clear and the revoke fails the at-revoke
// assertion.
func TestRotate_ClearsTheMarkerBeforeRevokingTheOldToken(t *testing.T) {
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")

	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	var markerAtRevoke string
	var revokedOld bool
	lifecycle.onRevoke = func(name string) {
		if name == "sdlc-agent-quicbaas-v1" {
			revokedOld = true
			markerAtRevoke = store.metadata[testSecretPath+rotationIntentSuffix][metadataPendingRollback]
		}
	}

	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}
	if _, err := rotator.Rotate(context.Background(), testSpec()); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if !revokedOld {
		t.Fatal("the old token was never revoked")
	}
	if markerAtRevoke != "" {
		t.Fatalf("the old token was revoked while the staging marker (%q) still stood", markerAtRevoke)
	}
}

// TestRotate_MarkerClearFailureStopsTheRevoke: when the marker cannot come
// off after a successful verification, the old credential must SURVIVE — a
// standing marker plus a revoked original is the recovery-path lockout — and
// the failure is typed so the caller knows recovery is owed.
//
// Mutation evidence: ignoring the clear failure revokes the old token and
// fails the nothing-revoked assertion.
func TestRotate_MarkerClearFailureStopsTheRevoke(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	// Arm the failure on the THIRD intent write: intent-before-mint and the
	// staging write land; the post-verify clear fails.
	armThirdIntentWrite(store)

	_, err = rotator.Rotate(context.Background(), testSpec())
	var incomplete *RotationIncompleteError
	if !errors.As(err, &incomplete) {
		t.Fatalf("err = %v, want RotationIncompleteError", err)
	}
	if lifecycle.revoked != nil {
		t.Fatalf("revoked %v with the marker still standing", lifecycle.revoked)
	}
	// The stuck state is recoverable: recovery unwinds it and the next
	// rotation runs clean. The store's final state is the property.
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan: %v", err)
	}
	lifecycle.verifyAs = testAccount
	if _, err := rotator.Rotate(context.Background(), testSpec()); err != nil {
		t.Fatalf("rotation after recovery: %v", err)
	}
	if got := store.metadata[testSecretPath+rotationIntentSuffix][metadataPendingRollback]; got != "" {
		t.Fatalf("staging marker still standing at the end: %q", got)
	}
}

// armThirdIntentWrite makes the third write to the intent record fail: the
// first two (intent-before-mint, staging) land, the post-verify clear does
// not.
func armThirdIntentWrite(store *fakeSecretStore) {
	writes := 0
	store.onStore = func(s *fakeSecretStore, path string) {
		if path != testSecretPath+rotationIntentSuffix {
			return
		}
		writes++
		if writes == 3 {
			s.storeErrAt = map[string]error{path: errors.New("intent plane down")}
		}
	}
}

// TestRecoverOrphan_AlreadyRevokedOrphanStillDischarges is finding 4's wedge:
// the ordinary verify-fail path revokes the replacement inline (quietly) and
// leaves the intent for recovery. Recovery then revokes the SAME name, the
// forge answers 404 — and treating that as failure meant the intent was NEVER
// discharged: every scheduled recover-then-rotate pass repeated the loop
// forever. Token absence is the end state revocation exists to reach.
//
// Mutation evidence: dropping the tolerance turns the 404 back into a
// returned error and fails the discharged assertion.
func TestRecoverOrphan_AlreadyRevokedOrphanStillDischarges(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyErr: errors.New("401 unauthorized")}
	store := newFakeSecretStore()
	seedCurrentToken(store, "agent-quicbaas-r1", "1")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	// Ordinary verify-fail: rollback succeeds inline, the replacement is
	// revoked quietly, the intent still names it.
	if _, err := rotator.Rotate(context.Background(), testSpec()); err == nil {
		t.Fatal("the rotation was supposed to fail verification")
	}
	replacement := RotationTokenName("agent-quicbaas", 2)
	if len(lifecycle.revoked) != 1 || lifecycle.revoked[0] != replacement {
		t.Fatalf("revoked %v, want the replacement revoked inline", lifecycle.revoked)
	}

	// The recovery pass revokes the same name again: 404 from the forge.
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("recovery wedged on an already-revoked orphan: %v", err)
	}
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != "" {
		t.Fatalf("intent record still names %q; the wedge is back", name)
	}
}

// TestRotate_AlreadyRevokedOldTokenStillCompletes is the same tolerance at
// the final revoke: an old credential the forge no longer knows must not fail
// the rotation that replaced it.
func TestRotate_AlreadyRevokedOldTokenStillCompletes(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	lifecycle.revoked = []string{"sdlc-agent-quicbaas-v1"} // already gone on the forge
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	result, err := rotator.Rotate(context.Background(), testSpec())
	if err != nil {
		t.Fatalf("rotation failed over an already-dead old credential: %v", err)
	}
	if result.NewTokenName != "sdlc-agent-quicbaas-v2" {
		t.Fatalf("result = %+v", result)
	}
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != "" {
		t.Fatalf("intent record not discharged: %q", name)
	}
}

// TestRotator_ConcurrentRotateAndRecoverArePerPathSingleFlight exercises the
// stripe lock under the race detector: a rotation and a recovery pass
// hammering one path concurrently must serialize, and the final state must be
// a clean, unstuck record. Without the lock the two interleave their
// read-then-act sequences and the race detector (and the invariants) catch it.
func TestRotator_ConcurrentRotateAndRecoverArePerPathSingleFlight(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	done := make(chan struct{}, 2)
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 25; i++ {
			_, _ = rotator.Rotate(context.Background(), testSpec())
		}
	}()
	go func() {
		defer func() { done <- struct{}{} }()
		for i := 0; i < 25; i++ {
			_ = rotator.RecoverOrphan(context.Background(), testSpec())
		}
	}()
	<-done
	<-done

	if got := store.metadata[testSecretPath+rotationIntentSuffix][metadataPendingRollback]; got != "" {
		t.Fatalf("a staging marker was left standing by serialized operations: %q", got)
	}
	// The stored credential is a real, verified one: its name matches its
	// metadata and the slot lineage is intact.
	meta := store.metadata[testSecretPath]
	if meta[metadataTokenName] == "" {
		t.Fatal("the store lost its credential")
	}
}

// TestRecoverOrphan_RealRevokeFailureStillSurfaces guards the tolerance from
// overreach: only NotFound is "already done" — a forge outage on the revoke
// must return, with the intent intact for the next pass. Swallowing every
// error would discharge the record over a still-live orphan.
func TestRecoverOrphan_RealRevokeFailureStillSurfaces(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyErr: errors.New("401 unauthorized")}
	store := newFakeSecretStore()
	seedCurrentToken(store, "agent-quicbaas-r1", "1")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}
	if _, err := rotator.Rotate(context.Background(), testSpec()); err == nil {
		t.Fatal("the rotation was supposed to fail verification")
	}

	forgeDown := errors.New("forge unreachable")
	lifecycle.revokeErr = forgeDown
	err = rotator.RecoverOrphan(context.Background(), testSpec())
	if !errors.Is(err, forgeDown) {
		t.Fatalf("err = %v, want the forge failure surfaced", err)
	}
	replacement := RotationTokenName("agent-quicbaas", 2)
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != replacement {
		t.Fatalf("intent = %q, want it intact for the next pass", name)
	}
}

// TestRotation_CorruptStagingMarkerFailsClosed: an unparseable pending
// version proves nothing about the stored state — guessing a rollback target
// risks the in-force credential — so BOTH operations refuse, typed.
func TestRotation_CorruptStagingMarkerFailsClosed(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
	store.data[testSecretPath+rotationIntentSuffix] = map[string][]byte{metadataTokenName: []byte("sdlc-agent-quicbaas-v2")}
	store.metadata[testSecretPath+rotationIntentSuffix] = map[string]string{metadataPendingRollback: "garbage"}
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	var stateErr *RotationRecoveryStateError
	if _, err := rotator.Rotate(context.Background(), testSpec()); !errors.As(err, &stateErr) {
		t.Fatalf("Rotate err = %v, want RotationRecoveryStateError", err)
	}
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); !errors.As(err, &stateErr) {
		t.Fatalf("RecoverOrphan err = %v, want RotationRecoveryStateError", err)
	}
	if len(lifecycle.calls) != 0 {
		t.Fatalf("the forge was touched over an unreadable recovery record: %v", lifecycle.calls)
	}
	if len(store.rolledTo) != 0 {
		t.Fatalf("a rollback ran to a guessed target: %v", store.rolledTo)
	}
}

// TestRotate_RevokeFailureKeepsTheOldTokenNamed is review 905's finding, on
// the damage plane: after the post-verify record update, the OLD token's
// identity must survive until its revoke actually lands. A record that named
// only the replacement made a revoke failure (or a crash before the revoke)
// permanent: the old privileged credential stayed live on the forge with
// nothing naming it — recovery read name==stored as "completed" and walked
// away, and the next rotation overwrote the record. The recovery pass after
// the failure must revoke the old token exactly once and only then discharge.
func TestRotate_RevokeFailureKeepsTheOldTokenNamed(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	// The rotation verifies, then the forge drops the revoke.
	forgeDown := errors.New("forge unreachable")
	lifecycle.revokeErr = forgeDown
	if _, err := rotator.Rotate(context.Background(), testSpec()); !errors.Is(err, forgeDown) {
		t.Fatalf("Rotate err = %v, want the revoke failure surfaced", err)
	}
	if got := string(store.data[testSecretPath][tokenSecretKey]); got != "new-secret" {
		t.Fatalf("stored credential = %q, want the verified replacement in force", got)
	}

	// A recovery pass while the forge is STILL down keeps the record: a
	// discharge before the revoke lands would erase the old token's only
	// remaining identity.
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); !errors.Is(err, forgeDown) {
		t.Fatalf("RecoverOrphan with the forge down: %v", err)
	}
	if got := store.metadata[testSecretPath+rotationIntentSuffix][metadataRevokePending]; got != "sdlc-agent-quicbaas-v1" {
		t.Fatalf("a failed recovery lost the pending revoke: %q", got)
	}

	// The next recovery pass finishes the job: the old token dies exactly
	// once, then the record is discharged.
	lifecycle.revokeErr = nil
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan: %v", err)
	}
	attempts := 0
	for _, call := range lifecycle.calls {
		if call == "revoke:sdlc-agent-quicbaas-v1" {
			attempts++
		}
	}
	if attempts != 3 { // inline failure, failed recovery retry, landed retry
		t.Fatalf("the old token saw %d revoke attempts, want 3", attempts)
	}
	revokedOld := false
	for _, name := range lifecycle.revoked {
		if name == "sdlc-agent-quicbaas-v1" {
			revokedOld = true
		}
	}
	if !revokedOld {
		t.Fatal("the old token is still live on the forge with nothing naming it")
	}
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != "" {
		t.Fatalf("intent record not discharged after the revoke landed: %q", name)
	}
	// The verified replacement stayed in force throughout.
	if got := store.metadata[testSecretPath][metadataTokenName]; got != "sdlc-agent-quicbaas-v2" {
		t.Fatalf("stored token name = %q after recovery", got)
	}
}

// TestRecoverOrphan_CrashBeforeTheRevokeStillKillsTheOldToken is the crash
// variant: the process died between the post-verify record update and the
// revoke. The record on disk is the verified phase — replacement named as
// current, old token named as revoke-pending — and recovery alone must finish
// it.
func TestRecoverOrphan_CrashBeforeTheRevokeStillKillsTheOldToken(t *testing.T) {
	lifecycle := &fakeLifecycle{}
	store := newFakeSecretStore()
	// The store: verified replacement in force.
	store.data[testSecretPath] = map[string][]byte{tokenSecretKey: []byte("new-secret")}
	store.metadata[testSecretPath] = map[string]string{
		metadataTokenName:    "sdlc-agent-quicbaas-v2",
		metadataRotationSlot: "2",
	}
	store.versions[testSecretPath] = 2
	// The intent: verified phase, old token revoke-pending.
	store.data[testSecretPath+rotationIntentSuffix] = map[string][]byte{
		metadataTokenName: []byte("sdlc-agent-quicbaas-v2"),
	}
	store.metadata[testSecretPath+rotationIntentSuffix] = map[string]string{
		metadataRotationSlot:  "2",
		metadataRevokePending: "sdlc-agent-quicbaas-v1",
	}

	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan: %v", err)
	}
	if len(lifecycle.revoked) != 1 || lifecycle.revoked[0] != "sdlc-agent-quicbaas-v1" {
		t.Fatalf("revoked %v, want exactly the old token", lifecycle.revoked)
	}
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != "" {
		t.Fatalf("intent record not discharged: %q", name)
	}
	// A second pass finds nothing to do — the revoke happened exactly once.
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if len(lifecycle.revoked) != 1 {
		t.Fatalf("a second pass revoked again: %v", lifecycle.revoked)
	}
}

// TestRotate_RefusesWhileARevokeIsPending: rotating over the verified phase
// would overwrite the record that names the old token — the intent-before-mint
// write erases it — sealing the leak the phase exists to prevent. Same
// fail-closed shape as the staged refusal; recovery clears it.
func TestRotate_RefusesWhileARevokeIsPending(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}
	forgeDown := errors.New("forge unreachable")
	lifecycle.revokeErr = forgeDown
	if _, err := rotator.Rotate(context.Background(), testSpec()); !errors.Is(err, forgeDown) {
		t.Fatalf("Rotate err = %v", err)
	}
	callsBefore := len(lifecycle.calls)

	_, err = rotator.Rotate(context.Background(), testSpec())
	var pending *RotationRevokePendingError
	if !errors.As(err, &pending) {
		t.Fatalf("err = %v, want RotationRevokePendingError", err)
	}
	if pending.OldToken != "sdlc-agent-quicbaas-v1" {
		t.Fatalf("refusal names %q, want the old token", pending.OldToken)
	}
	if len(lifecycle.calls) != callsBefore {
		t.Fatalf("the forge was touched over a pending revoke: %v", lifecycle.calls[callsBefore:])
	}
	if got := store.metadata[testSecretPath+rotationIntentSuffix][metadataRevokePending]; got != "sdlc-agent-quicbaas-v1" {
		t.Fatalf("the refused rotation erased the pending revoke: %q", got)
	}
}

// TestRotate_CommittedButErroredStoreIsResolvedByObservation is review 914's
// finding: an error return from the replacement's store write does NOT prove
// the write failed to commit — a timeout can fire after the backend applied
// it. Clearing the staging marker (and revoking the replacement) on the
// strength of that error left the store holding a REVOKED, unverified
// credential with a record claiming nothing was in flight: recovery read
// name==stored as "completed" and walked away. Lockout, minted on the error
// path.
//
// Recovery state is cleared only on an OBSERVED store state: the rotation
// leaves the marker standing and refuses further rotations; the recovery pass
// reads the store, finds the replacement IN FORCE — the write committed — and
// unwinds it exactly as any staged state: rollback, revoke, discharge.
func TestRotate_CommittedButErroredStoreIsResolvedByObservation(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	// The write COMMITS and still errors.
	timeout := errors.New("store: request timed out")
	store.storeCommitErrAt = map[string]error{testSecretPath: timeout}

	if _, err := rotator.Rotate(context.Background(), testSpec()); !errors.Is(err, timeout) {
		t.Fatalf("Rotate err = %v, want the store failure surfaced", err)
	}
	// Nothing was revoked on the ambiguous outcome: the replacement may be —
	// and here IS — the credential the store holds.
	if len(lifecycle.revoked) != 0 {
		t.Fatalf("revoked %v on an outcome the error return could not prove", lifecycle.revoked)
	}
	if got := store.metadata[testSecretPath+rotationIntentSuffix][metadataPendingRollback]; got != "1" {
		t.Fatalf("staging marker = %q after the ambiguous write, want it standing", got)
	}

	// Rotation refuses until the ambiguity is resolved.
	var pending *RotationRecoveryPendingError
	if _, err := rotator.Rotate(context.Background(), testSpec()); !errors.As(err, &pending) {
		t.Fatalf("rotation over the unresolved state: %v", err)
	}

	// Recovery OBSERVES the committed write and unwinds it.
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan: %v", err)
	}
	if got := string(store.data[testSecretPath][tokenSecretKey]); got != "old-secret" {
		t.Fatalf("stored credential = %q, want the previous credential rolled back into force", got)
	}
	replacement := RotationTokenName("agent-quicbaas", 2)
	if len(lifecycle.revoked) != 1 || lifecycle.revoked[0] != replacement {
		t.Fatalf("revoked %v, want exactly the unverified replacement", lifecycle.revoked)
	}
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != "" {
		t.Fatalf("intent not discharged: %q", name)
	}

	// And the pipeline is live again.
	if _, err := rotator.Rotate(context.Background(), testSpec()); err != nil {
		t.Fatalf("rotation after recovery: %v", err)
	}
}

// TestRotate_UncommittedStoreFailureResolvesToACleanClear is the other world
// the same error return can mean: nothing was committed. The observation
// finds the store still holding the previous credential, so recovery skips
// the rollback — the store never moved — revokes the mint-only orphan, and
// discharges.
func TestRotate_UncommittedStoreFailureResolvesToACleanClear(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	down := errors.New("store: unreachable")
	store.storeErrAt = map[string]error{testSecretPath: down}

	if _, err := rotator.Rotate(context.Background(), testSpec()); !errors.Is(err, down) {
		t.Fatalf("Rotate err = %v", err)
	}
	if got := store.metadata[testSecretPath+rotationIntentSuffix][metadataPendingRollback]; got != "1" {
		t.Fatalf("staging marker = %q, want it standing until an observation clears it", got)
	}

	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan: %v", err)
	}
	// The store never moved, so nothing was rolled back.
	for _, call := range store.calls {
		if call == "rollback:"+testSecretPath {
			t.Fatal("a rollback ran for a write the observation shows never committed")
		}
	}
	replacement := RotationTokenName("agent-quicbaas", 2)
	if len(lifecycle.revoked) != 1 || lifecycle.revoked[0] != replacement {
		t.Fatalf("revoked %v, want exactly the mint-only orphan", lifecycle.revoked)
	}
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != "" {
		t.Fatalf("intent not discharged: %q", name)
	}
	if got := string(store.data[testSecretPath][tokenSecretKey]); got != "old-secret" {
		t.Fatalf("stored credential = %q, want untouched", got)
	}
}

// TestRecoverOrphan_UnreadableObservationFailsClosed: the staged arm acts on
// what it OBSERVES, and an unreadable store observes nothing — recovery
// returns with the state intact rather than guessing which world it is in.
func TestRecoverOrphan_UnreadableObservationFailsClosed(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}
	store.storeCommitErrAt = map[string]error{testSecretPath: errors.New("timeout")}
	if _, err := rotator.Rotate(context.Background(), testSpec()); err == nil {
		t.Fatal("the rotation was supposed to fail")
	}

	outage := errors.New("storage backend unavailable")
	store.getErr[testSecretPath] = outage
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); !errors.Is(err, outage) {
		t.Fatalf("err = %v, want the observation failure surfaced", err)
	}
	if len(lifecycle.revoked) != 0 {
		t.Fatalf("revoked %v on an observation that could not be made", lifecycle.revoked)
	}
	if got := store.metadata[testSecretPath+rotationIntentSuffix][metadataPendingRollback]; got != "1" {
		t.Fatalf("recovery state = %q, want intact for the pass after", got)
	}
}

// =============================================================================
// Review 922 — the three fail-open / lockout paths
// =============================================================================

// commitThenDieFirstRotation drives a FIRST rotation into the committed-staged
// stuck state review 922's finding 3 is about: the store write COMMITTED (the
// ambiguous error the fake's commit-then-error injection produces), so the
// path's only credential is an unverified replacement, the intent is staged
// with PendingRollback 0, and verification never ran.
func commitThenDieFirstRotation(t *testing.T) (*fakeLifecycle, *fakeSecretStore, *TokenRotator) {
	t.Helper()
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	store.storeCommitErrAt = map[string]error{testSecretPath: errors.New("timeout after commit")}

	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}
	if _, err := rotator.Rotate(context.Background(), testSpec()); err == nil {
		t.Fatal("the first rotation was supposed to die on the ambiguous store error")
	}
	if got := store.metadata[testSecretPath+rotationIntentSuffix][metadataPendingRollback]; got != "0" {
		t.Fatalf("fixture: pending rollback = %q, want the staged first-rotation marker \"0\"", got)
	}
	return lifecycle, store, rotator
}

// TestRecoverOrphan_AdoptsACommittedVerifiedFirstRotation: the committed
// replacement was merely UNVERIFIED, not proven bad. Recovery verifies it the
// way Rotate would have, and a replacement that authenticates as the right
// account is adopted — nothing revoked, the store keeps a working credential,
// the record discharged. The old behaviour revoked it and reported success
// with the store still presenting the revoked token as current.
func TestRecoverOrphan_AdoptsACommittedVerifiedFirstRotation(t *testing.T) {
	lifecycle, store, rotator := commitThenDieFirstRotation(t)

	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan: %v", err)
	}

	if len(lifecycle.revoked) != 0 {
		t.Fatalf("recovery revoked %v — that credential verified as the right account", lifecycle.revoked)
	}
	replacement := RotationTokenName("agent-quicbaas", 1)
	if got := store.metadata[testSecretPath][metadataTokenName]; got != replacement {
		t.Fatalf("stored token name = %q, want the adopted replacement %q", got, replacement)
	}
	if got := string(store.data[testSecretPath][tokenSecretKey]); got != "new-secret" {
		t.Fatalf("stored credential = %q, want the adopted replacement's secret", got)
	}
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != "" {
		t.Fatalf("intent record not discharged after adoption: %q", name)
	}

	// The adopted credential is ordinary lineage: the next rotation rotates
	// it out normally.
	result, err := rotator.Rotate(context.Background(), testSpec())
	if err != nil {
		t.Fatalf("rotation after adoption: %v", err)
	}
	if result.RevokedTokenName != replacement {
		t.Fatalf("post-adoption rotation revoked %q, want the adopted %q", result.RevokedTokenName, replacement)
	}
}

// TestRecoverOrphan_FailedFirstRotationResolvesToAbsence: a committed first
// rotation whose replacement does NOT verify ends as absence — the state a
// first boot starts from — never as a revoked credential posing as current.
// Then review 922's exact sequence: the NEXT rotation's verification also
// fails, and it must not roll back to (or report preserving) the dead token.
func TestRecoverOrphan_FailedFirstRotationResolvesToAbsence(t *testing.T) {
	lifecycle, store, rotator := commitThenDieFirstRotation(t)
	lifecycle.verifyErr = errors.New("401 unauthorized")

	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan: %v", err)
	}

	replacement := RotationTokenName("agent-quicbaas", 1)
	if len(lifecycle.revoked) != 1 || lifecycle.revoked[0] != replacement {
		t.Fatalf("revoked %v, want exactly the unverifiable replacement %q", lifecycle.revoked, replacement)
	}
	if _, held := store.data[testSecretPath]; held {
		t.Fatalf("store still holds %q as current after its token was revoked; want absence",
			store.metadata[testSecretPath][metadataTokenName])
	}
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != "" {
		t.Fatalf("intent record not discharged: %q", name)
	}

	// The sequence from the review: another rotation, another verification
	// failure. With absence restored it runs as an ordinary first rotation —
	// no rollback to a revoked ghost, and no claim that a predecessor
	// survived.
	if _, err := rotator.Rotate(context.Background(), testSpec()); err == nil {
		t.Fatal("the follow-up rotation was supposed to fail verification")
	}
	if _, rolled := store.rolledTo[testSecretPath]; rolled {
		t.Fatal("the follow-up rotation rolled back — its only possible target was the revoked token")
	}
	if _, held := store.data[testSecretPath]; held {
		t.Fatal("the follow-up failed first rotation left its dead credential stored as current")
	}
}

// TestRotate_FailedFirstRotationLeavesAbsenceNotADeadCredential covers the
// INLINE producer of the same incoherence: a first rotation that stores its
// replacement and then fails verification in the same call. The rollback arm
// has nothing to restore, so the truthful end state is absence — the old
// behaviour left the revoked token stored as current, which the next rotation
// adopted as rotation lineage and rollback target.
func TestRotate_FailedFirstRotationLeavesAbsenceNotADeadCredential(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyErr: errors.New("401 unauthorized")}
	store := newFakeSecretStore()
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	if _, err := rotator.Rotate(context.Background(), testSpec()); err == nil {
		t.Fatal("the rotation was supposed to fail verification")
	}

	replacement := RotationTokenName("agent-quicbaas", 1)
	if _, held := store.data[testSecretPath]; held {
		t.Fatalf("store holds %q as current after the inline revoke; want absence",
			store.metadata[testSecretPath][metadataTokenName])
	}
	if len(lifecycle.revoked) != 1 || lifecycle.revoked[0] != replacement {
		t.Fatalf("revoked %v, want exactly the failed replacement %q", lifecycle.revoked, replacement)
	}
	// The unstaged intent still names the replacement so a lost quiet revoke
	// stays recoverable; recovery resolves it against the absent path and a
	// repaired verifier then rotates cleanly from first-boot state.
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan over the residue: %v", err)
	}
	lifecycle.verifyErr = nil
	lifecycle.verifyAs = testAccount
	result, err := rotator.Rotate(context.Background(), testSpec())
	if err != nil {
		t.Fatalf("rotation after the residue was resolved: %v", err)
	}
	if result.RevokedTokenName != "" {
		t.Fatalf("clean first rotation reported revoking %q; nothing usable preceded it", result.RevokedTokenName)
	}
}

// TestRecoverOrphan_PendingRevokeRefusesWhenTheWorldChanged is review 922's
// finding 1: the pending-revoke record says "the replacement is in force and
// OldToken is the predecessor". If the store no longer agrees — here an
// operator rolled the secret back to the old version — OldToken IS the
// credential in force, and revoking it on the record's word is lockout with
// the recovery record discharged behind it. The revoke is destructive, so it
// requires a fresh observation of the state it assumes.
func TestRecoverOrphan_PendingRevokeRefusesWhenTheWorldChanged(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	// The real producer: a verified rotation whose revoke the forge dropped.
	// The record now names v2 current with v1 revoke-pending.
	forgeDown := errors.New("forge unreachable")
	lifecycle.revokeErr = forgeDown
	if _, err := rotator.Rotate(context.Background(), testSpec()); !errors.Is(err, forgeDown) {
		t.Fatalf("Rotate err = %v, want the revoke failure surfaced", err)
	}
	lifecycle.revokeErr = nil

	// The world changes under the record: the old version is restored.
	if err := store.Rollback(context.Background(), testSecretPath, 1); err != nil {
		t.Fatalf("operator rollback: %v", err)
	}

	err = rotator.RecoverOrphan(context.Background(), testSpec())
	var diverged *RotationStateDivergedError
	if !errors.As(err, &diverged) {
		t.Fatalf("err = %v, want *RotationStateDivergedError", err)
	}
	if len(lifecycle.revoked) != 0 {
		t.Fatalf("recovery revoked %v — the store says that credential is back in force", lifecycle.revoked)
	}
	if got := store.metadata[testSecretPath+rotationIntentSuffix][metadataRevokePending]; got != "sdlc-agent-quicbaas-v1" {
		t.Fatalf("the divergence refusal lost the pending revoke record: %q", got)
	}

	// The permitting direction, same record: once the store names the
	// replacement again, the observation holds and the revoke proceeds.
	if err := store.Rollback(context.Background(), testSecretPath, 2); err != nil {
		t.Fatalf("restore replacement: %v", err)
	}
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan with the world consistent again: %v", err)
	}
	if len(lifecycle.revoked) != 1 || lifecycle.revoked[0] != "sdlc-agent-quicbaas-v1" {
		t.Fatalf("revoked %v, want exactly the old token once the observation held", lifecycle.revoked)
	}
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != "" {
		t.Fatalf("intent record not discharged after the revoke landed: %q", name)
	}
}

// TestRotate_RefusesOverAnUnstagedOrphanIntent is review 922's finding 2: an
// unstaged intent with a name is a mint-window orphan's ONLY durable
// identity, and Rotate — an exported method that cannot assume its caller
// recovered first — must refuse rather than overwrite it.
func TestRotate_RefusesOverAnUnstagedOrphanIntent(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	// The real producer: intent-before-mint written, then the mint dies.
	mintDown := errors.New("forge mint unavailable")
	lifecycle.mintErr = mintDown
	if _, err := rotator.Rotate(context.Background(), testSpec()); !errors.Is(err, mintDown) {
		t.Fatalf("Rotate err = %v, want the mint failure surfaced", err)
	}
	lifecycle.mintErr = nil
	orphan := RotationTokenName("agent-quicbaas", 2)
	mintsBefore := len(lifecycle.calls)

	// A direct Rotate over the standing record refuses, typed, and touches
	// nothing: no mint, and the orphan's identity survives.
	_, err = rotator.Rotate(context.Background(), testSpec())
	var unresolved *RotationUnresolvedIntentError
	if !errors.As(err, &unresolved) {
		t.Fatalf("err = %v, want *RotationUnresolvedIntentError", err)
	}
	if unresolved.Name != orphan {
		t.Fatalf("refusal names %q, want the orphan %q", unresolved.Name, orphan)
	}
	if len(lifecycle.calls) != mintsBefore {
		t.Fatalf("the refused rotation still called the forge: %v", lifecycle.calls[mintsBefore:])
	}
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != orphan {
		t.Fatalf("intent record = %q, want the orphan's identity %q preserved", name, orphan)
	}

	// The refusal is TRANSIENT: recovery resolves the record (revoke by
	// name against a forge that may hold it, discharge), and the same call
	// then succeeds — the recover-then-rotate pairing the scheduler runs.
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan: %v", err)
	}
	if _, err := rotator.Rotate(context.Background(), testSpec()); err != nil {
		t.Fatalf("Rotate after recovery: %v", err)
	}
}

// TestRecoverOrphan_DischargesAStaleCompletedIntent: with Rotate refusing
// over ANY unresolved intent, a stale record naming the credential in force
// must be discharged by recovery, or a completed rotation's lost discharge
// would refuse every future rotation. Same seeding as
// TestRecoverOrphan_NeverRevokesTheTokenInForce, which pins the other half:
// nothing is revoked either way.
func TestRecoverOrphan_DischargesAStaleCompletedIntent(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v2", "2")
	store.data[testSecretPath+rotationIntentSuffix] = map[string][]byte{
		metadataTokenName: []byte("sdlc-agent-quicbaas-v2"),
	}

	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan: %v", err)
	}

	if len(lifecycle.revoked) != 0 {
		t.Fatalf("recovery revoked %v discharging a stale record", lifecycle.revoked)
	}
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != "" {
		t.Fatalf("stale record still names %q; Rotate would refuse forever", name)
	}
	// And rotation proceeds where it would have been refused.
	if _, err := rotator.Rotate(context.Background(), testSpec()); err != nil {
		t.Fatalf("Rotate after the stale record was discharged: %v", err)
	}
}

// =============================================================================
// Review 944 — cross-instance serialization and destroy-before-revoke
// =============================================================================

// TestRotator_TwoInstancesSerializeOnePath is review 944's first finding: the
// path lock was per rotator instance, so two rotators over the same store
// held disjoint locks, both passed the current/intent checks, derived the
// same slot and name, and overwrote the single intent record. The lock
// registry is process-wide now; the racers here rotate the same path from
// BOTH instances, with the forge holding its first minter open long enough
// for an unserialized racer to join it (the cap is the correct exit for
// serialized code; a second concurrent minter is the defect's signature).
// Serialized, the two rotations land as consecutive slots with the first
// token revoked by the second — the ordinary lineage two sequential
// rotations produce.
func TestRotator_TwoInstancesSerializeOnePath(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	rotatorA, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator A: %v", err)
	}
	rotatorB, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator B: %v", err)
	}

	var minters atomic.Int32
	release := make(chan struct{})
	var releaseOnce sync.Once
	lifecycle.onMint = func(string) {
		if minters.Add(1) >= 2 {
			releaseOnce.Do(func() { close(release) })
			return
		}
		select {
		case <-release:
		case <-time.After(100 * time.Millisecond):
		}
	}

	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make([]*RotationResult, 2)
	errs := make([]error, 2)
	for i, r := range []*TokenRotator{rotatorA, rotatorB} {
		wg.Add(1)
		go func(n int, rot *TokenRotator) {
			defer wg.Done()
			<-start
			results[n], errs[n] = rot.Rotate(context.Background(), testSpec())
		}(i, r)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("rotator %d: %v", i, err)
		}
	}
	slots := map[uint64]bool{results[0].Slot: true, results[1].Slot: true}
	if !slots[1] || !slots[2] {
		t.Fatalf("slots = {%d, %d}, want {1, 2}: both instances derived the same "+
			"slot, so the second overwrote the first's intent and lineage",
			results[0].Slot, results[1].Slot)
	}
	// The durable end state is one coherent lineage: slot 2's token stored as
	// current, slot 1's revoked.
	if got := store.metadata[testSecretPath][metadataTokenName]; got != RotationTokenName("agent-quicbaas", 2) {
		t.Fatalf("stored token name = %q, want slot 2's", got)
	}
	if len(lifecycle.revoked) != 1 || lifecycle.revoked[0] != RotationTokenName("agent-quicbaas", 1) {
		t.Fatalf("revoked %v, want exactly slot 1's token", lifecycle.revoked)
	}
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != "" {
		t.Fatalf("intent record still names %q after both rotations completed", name)
	}
}

// TestRecoverOrphan_DestroysBeforeRevoking is review 944's second finding:
// the real backend's Destroy is not atomic, so a partial destroy AFTER the
// revoke leaves an unreadable path whose only forge token is already gone —
// permanently incoherent. The order is pinned at the damage plane: at the
// moment the revoke fires, the stored path must already be absent.
func TestRecoverOrphan_DestroysBeforeRevoking(t *testing.T) {
	lifecycle, store, rotator := commitThenDieFirstRotation(t)
	lifecycle.verifyErr = errors.New("401 unauthorized")

	lifecycle.onRevoke = func(name string) {
		if _, held := store.data[testSecretPath]; held {
			t.Errorf("revoke of %q fired while the store still holds the path: "+
				"a partial destroy after this revoke would strand an unreadable "+
				"path whose only token is already dead", name)
		}
	}

	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan: %v", err)
	}
	if len(lifecycle.revoked) != 1 {
		t.Fatalf("revoked %v, want exactly the replacement", lifecycle.revoked)
	}
}

// TestRecoverOrphan_FailedDestroyKeepsTheTokenAndTheRecord is the recoverable
// half of the same finding: a destroy that fails leaves the forge token
// UNREVOKED and the staged intent standing — an operator-visible stall with
// nothing lost — and the pass after a restored store completes the
// resolution.
func TestRecoverOrphan_FailedDestroyKeepsTheTokenAndTheRecord(t *testing.T) {
	lifecycle, store, rotator := commitThenDieFirstRotation(t)
	lifecycle.verifyErr = errors.New("401 unauthorized")

	store.destroyErr = errors.New("storage backend unavailable")
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err == nil {
		t.Fatal("recovery claimed success while the destroy failed")
	}
	if len(lifecycle.revoked) != 0 {
		t.Fatalf("revoked %v with the destroy failed — that is the incoherence "+
			"the ordering exists to prevent", lifecycle.revoked)
	}
	if got := store.metadata[testSecretPath+rotationIntentSuffix][metadataPendingRollback]; got != "0" {
		t.Fatalf("staged intent = %q, want intact for the pass after", got)
	}

	// The store comes back; the next pass completes: destroy, then revoke,
	// then discharge.
	store.destroyErr = nil
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan after the store recovered: %v", err)
	}
	replacement := RotationTokenName("agent-quicbaas", 1)
	if len(lifecycle.revoked) != 1 || lifecycle.revoked[0] != replacement {
		t.Fatalf("revoked %v, want exactly %q", lifecycle.revoked, replacement)
	}
	if _, held := store.data[testSecretPath]; held {
		t.Fatal("store still holds the path after a completed resolution")
	}
	if name := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); name != "" {
		t.Fatalf("intent record not discharged: %q", name)
	}
}

// =============================================================================
// Review 951 — untrusted forge answers and malformed stored records
// =============================================================================

// TestRotate_RefusesAMintResponseNothingCanTrust is review 951's first
// finding, refusal half: a nil mint response or an empty secret is a claim
// of success carrying nothing usable, refused before anything depends on
// it, with the standing unstaged intent kept for recovery.
func TestRotate_RefusesAMintResponseNothingCanTrust(t *testing.T) {
	cases := map[string]func(l *fakeLifecycle){
		"nil_response": func(l *fakeLifecycle) { l.mintNil = true },
		// review 995: a nil secure buffer is refused explicitly BEFORE any
		// dereference — never left to the buffer's nil-safe method set.
		"nil_buffer":   func(l *fakeLifecycle) { l.mintNilBuffer = true },
		"empty_secret": func(l *fakeLifecycle) { l.mintEmptySecret = true },
		// review 956: a usable secret under an EMPTY name would record an
		// identity no recovery arm can revoke or classify — refused
		// independently of the equality below, so the check can never hold
		// vacuously.
		"empty_name": func(l *fakeLifecycle) { l.mintEmptyName = true },
		// ruled 2026-08-23: a DIVERGENT name is a contract violation, not
		// an input — RotationTokenName is deterministic so every party can
		// recompute it, and absorbing a rename silently would change the
		// recovery identity model. Refused, with the returned token revoked
		// at the one moment its actual name is in hand.
		"divergent_name": func(l *fakeLifecycle) { l.mintedName = "sdlc-agent-quicbaas-v2-normalized" },
	}
	for name, arm := range cases {
		t.Run(name, func(t *testing.T) {
			lifecycle := &fakeLifecycle{verifyAs: testAccount}
			store := newFakeSecretStore()
			seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
			arm(lifecycle)
			rotator, err := NewTokenRotator(lifecycle, store)
			if err != nil {
				t.Fatalf("NewTokenRotator: %v", err)
			}

			_, err = rotator.Rotate(context.Background(), testSpec())
			var mintErr *MintResponseError
			if !errors.As(err, &mintErr) {
				t.Fatalf("err = %v, want *MintResponseError", err)
			}
			if got := store.metadata[testSecretPath][metadataTokenName]; got != "sdlc-agent-quicbaas-v1" {
				t.Fatalf("stored token name = %q; the unusable mint response reached the store", got)
			}
			requested := RotationTokenName("agent-quicbaas", 2)
			intentName := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName])
			if name == "divergent_name" {
				// The actual returned credential was revoked at the only
				// moment its name was known — and with the revoke LANDED,
				// the intent is discharged (review 974: an intent may only
				// die after the token it names does).
				found := false
				for _, r := range lifecycle.revoked {
					if r == "sdlc-agent-quicbaas-v2-normalized" {
						found = true
					}
				}
				if !found {
					t.Fatalf("revoked %v; the divergent-name credential is live and untracked", lifecycle.revoked)
				}
				if intentName != "" {
					t.Fatalf("intent still names %q after the divergent token was revoked", intentName)
				}
			} else if intentName != requested {
				t.Fatalf("intent names %q, want the requested %q kept for recovery", intentName, requested)
			}
			if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
				t.Fatalf("RecoverOrphan: %v", err)
			}
		})
	}
}

// TestRotate_DivergentMintRevokeFailureKeepsCustody is review 974's first
// finding: the divergent token's ACTUAL name must be durable before cleanup
// is attempted, so a failed revoke leaves recovery hunting the name the
// forge actually knows — never the requested name the forge ignored.
func TestRotate_DivergentMintRevokeFailureKeepsCustody(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount, mintedName: "renamed-by-forge"}
	lifecycle.revokeErr = errors.New("forge: 502")
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	_, err = rotator.Rotate(context.Background(), testSpec())
	var mintErr *MintResponseError
	if !errors.As(err, &mintErr) {
		t.Fatalf("err = %v, want *MintResponseError joined with the revoke failure", err)
	}
	if mintErr.Returned != "renamed-by-forge" {
		t.Fatalf("sanitized error names %q, want the returned name", mintErr.Returned)
	}

	// THE DURABLE RECORD NAMES THE LIVE TOKEN. This is the custody fact the
	// finding was about: with the revoke failed, the intent must carry the
	// forge's actual name, not the requested one.
	if got := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); got != "renamed-by-forge" {
		t.Fatalf("intent names %q; a failed revoke stranded the live token untracked", got)
	}

	// Rotation refuses over the unresolved record — the orphan must die
	// before anything new is minted over its identity.
	if _, err := rotator.Rotate(context.Background(), testSpec()); err == nil {
		t.Fatal("a rotation ran over an unresolved divergent-mint record")
	}

	// The forge heals; the NEXT recovery pass revokes the actual name and
	// only then discharges.
	lifecycle.revokeErr = nil
	if err := rotator.RecoverOrphan(context.Background(), testSpec()); err != nil {
		t.Fatalf("RecoverOrphan: %v", err)
	}
	found := false
	for _, r := range lifecycle.revoked {
		if r == "renamed-by-forge" {
			found = true
		}
	}
	if !found {
		t.Fatalf("revoked %v; recovery never revoked the actual divergent name", lifecycle.revoked)
	}
	if got := string(store.data[testSecretPath+rotationIntentSuffix][metadataTokenName]); got != "" {
		t.Fatalf("intent still names %q after recovery revoked the token", got)
	}

	// The path is coherent: the old credential is still current and the next
	// rotation runs normally.
	lifecycle.mintedName = ""
	result, err := rotator.Rotate(context.Background(), testSpec())
	if err != nil {
		t.Fatalf("rotation after recovery: %v", err)
	}
	if result.RevokedTokenName != "sdlc-agent-quicbaas-v1" {
		t.Fatalf("post-recovery rotation revoked %q, want the surviving original", result.RevokedTokenName)
	}
}

// TestRecoverOrphan_RefusesARecordWithNoTokenValue is review 951's second
// finding: a stored record whose metadata matches the staged intent but
// which carries no (or an empty) token value must not be verified — an
// adapter answering the expected identity for an empty string would get an
// unusable record ADOPTED as current. Fail closed: typed error, nothing
// revoked, intent kept.
func TestRecoverOrphan_RefusesARecordWithNoTokenValue(t *testing.T) {
	corruptions := map[string]func(s *fakeSecretStore){
		"missing_key": func(s *fakeSecretStore) {
			delete(s.data[testSecretPath], tokenSecretKey)
		},
		"empty_value": func(s *fakeSecretStore) {
			s.data[testSecretPath][tokenSecretKey] = nil
		},
	}
	for name, corrupt := range corruptions {
		t.Run(name, func(t *testing.T) {
			// The treacherous adapter: it would report the expected account
			// for ANY input, the empty string included.
			lifecycle, store, rotator := commitThenDieFirstRotation(t)
			corrupt(store)

			err := rotator.RecoverOrphan(context.Background(), testSpec())
			var missing *RotationCredentialMissingError
			if !errors.As(err, &missing) {
				t.Fatalf("err = %v, want *RotationCredentialMissingError", err)
			}
			if len(lifecycle.revoked) != 0 {
				t.Fatalf("revoked %v on a record that proved nothing", lifecycle.revoked)
			}
			if got := store.metadata[testSecretPath+rotationIntentSuffix][metadataPendingRollback]; got != "0" {
				t.Fatalf("staged intent = %q, want intact for the pass after", got)
			}
			if calls := len(lifecycle.calls); calls != 1 { // the fixture's own mint only
				t.Fatalf("lifecycle saw %d calls, want 1: an empty credential was verified", calls)
			}
		})
	}
}

// TestRecoverOrphan_SweepsAPartiallyDestroyedPath is review 957's third
// finding, driven through the REAL secrets manager and KV backend because
// the fake's atomic destroy cannot represent the state: the backend's
// pointer-first destroy crashed after the pointer delete, so the path reads
// ABSENT while version material remains on disk. Recovery's never-committed
// arm used to revoke and discharge on that read, stranding
// destroyed-credential material indefinitely against the
// permanent-destruction contract. The absent observation now triggers a
// sweep before the record dies: the resumable Destroy finishes the
// leftovers, and only then is the intent discharged.
func TestRecoverOrphan_SweepsAPartiallyDestroyedPath(t *testing.T) {
	raw := kvstore.NewMemory()
	manager, err := secrets.NewManager(&secrets.ManagerOptions{
		Backend: secrets.NewKVBackend(raw),
	})
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	lifecycle := &fakeLifecycle{verifyErr: errors.New("401 unauthorized")}
	rotator, err := NewTokenRotator(lifecycle, manager)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}
	ctx := context.Background()

	// The crash state, built through the real plane: a committed staged
	// first rotation...
	name := RotationTokenName("agent-quicbaas", 1)
	if err := manager.Store(ctx, testSecretPath,
		map[string][]byte{tokenSecretKey: []byte("unverified-secret")},
		map[string]string{metadataTokenName: name, metadataRotationSlot: "1"}); err != nil {
		t.Fatalf("seed store: %v", err)
	}
	if err := saveRotationIntent(ctx, manager, testSecretPath,
		rotationIntent{Name: name, Slot: 1, Staged: true, PendingRollback: 0}); err != nil {
		t.Fatalf("seed staged intent: %v", err)
	}
	// ...whose destroy died after the pointer delete: the pointer is gone,
	// the version entry is not.
	if err := raw.Delete(ctx, "_qb/secrets/"+testSecretPath+"/current"); err != nil {
		t.Fatalf("simulate pointer-delete crash: %v", err)
	}
	if _, _, err := readStoredToken(ctx, manager, testSecretPath); err != nil {
		t.Fatalf("fixture: the crashed path must read cleanly as absent, got %v", err)
	}

	if err := rotator.RecoverOrphan(ctx, testSpec()); err != nil {
		t.Fatalf("RecoverOrphan over the crashed destroy: %v", err)
	}

	// The residue is GONE — swept before the record died.
	if raw2, err := raw.Get(ctx, "_qb/secrets/"+testSecretPath+"/v/1"); err == nil && raw2 != nil {
		t.Fatal("destroyed-credential material survived recovery: the absent read " +
			"was treated as never-committed and the sweep never ran")
	}
	// The minted token died, and the record after it.
	if len(lifecycle.revoked) != 1 || lifecycle.revoked[0] != name {
		t.Fatalf("revoked %v, want exactly %q", lifecycle.revoked, name)
	}
	intent, _, err := loadRotationIntent(ctx, manager, testSecretPath)
	if err != nil {
		t.Fatalf("loadRotationIntent: %v", err)
	}
	if intent.Name != "" {
		t.Fatalf("intent still names %q after a completed recovery", intent.Name)
	}
	// And the path is honestly absent for the next first rotation.
	lifecycle.verifyErr = nil
	lifecycle.verifyAs = testAccount
	result, err := rotator.Rotate(ctx, testSpec())
	if err != nil {
		t.Fatalf("rotation after the sweep: %v", err)
	}
	if result.RevokedTokenName != "" {
		t.Fatalf("clean first rotation reported revoking %q", result.RevokedTokenName)
	}
}

// TestRotate_VerificationSeesTheSecretOnlyDuringTheCall is the caller's
// half of VerifyToken's no-retain contract under the #97 enclave: the view
// handed to the adapter points INTO the mlocked buffer and is valid only
// during the call — the fake records what it OBSERVED (a copy made in the
// window), and after Rotate returns the enclave is destroyed, which
// TestRotate_DestroysTheMintedSecretOnEveryPath pins. Retaining the raw
// view past the call — the old test's seam — is now a fault by
// construction: destroyed enclave pages are unmapped, so a violator
// crashes rather than leaking, which is the design working.
func TestRotate_VerificationSeesTheSecretOnlyDuringTheCall(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}

	if _, err := rotator.Rotate(context.Background(), testSpec()); err != nil {
		t.Fatalf("Rotate: %v", err)
	}
	if lifecycle.verifiedWith != "new-secret" {
		t.Fatalf("verified with %q: the adapter must see the real secret DURING the call", lifecycle.verifiedWith)
	}
	if lifecycle.mintedTokens[0].Secret.Bytes() != nil {
		t.Fatal("the enclave outlived the rotation")
	}
}

// TestReadStoredToken_WipesTheDataItFetched: a metadata read must not leave
// credential bytes live in the heap (review 959). readStoredToken returns
// only metadata, and the data map Get materialized is loop-zeroed before it
// returns; the fake retains the exact map it handed out so the wipe is
// observable.
func TestReadStoredToken_WipesTheDataItFetched(t *testing.T) {
	store := newFakeSecretStore()
	seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")

	meta, absent, err := readStoredToken(context.Background(), store, testSecretPath)
	if err != nil || absent {
		t.Fatalf("readStoredToken: err=%v absent=%v", err, absent)
	}
	if meta[metadataTokenName] != "sdlc-agent-quicbaas-v1" {
		t.Fatalf("metadata lost: %v", meta)
	}
	if store.lastGet == nil {
		t.Fatal("fixture: the fake recorded no Get")
	}
	for key, v := range store.lastGet {
		for i, b := range v {
			if b != 0 {
				t.Fatalf("byte %d of fetched field %q survived readStoredToken: "+
					"the metadata read leaked credential bytes into the heap", i, key)
			}
		}
	}
	// The wipe hit the fetched COPY, never the stored record.
	if got := string(store.data[testSecretPath][tokenSecretKey]); got != "old-secret" {
		t.Fatalf("the stored credential was corrupted by the wipe: %q", got)
	}
}

// TestRecoverOrphan_RevokesAsTheRecordedOwner is review 977's third finding:
// the intent record carries the account that OWNS the token it names, and
// recovery revokes as that recorded owner — never as whatever account a
// later caller's spec supplies. Without this, a recovery run after an
// account reconfiguration turned the forge's account-scoped NotFound into
// "already revoked" and discharged the only record naming a still-live
// credential under the original owner.
func TestRecoverOrphan_RevokesAsTheRecordedOwner(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: testAccount}
	store := newFakeSecretStore()
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}
	ctx := context.Background()

	// A mint-window orphan crashed under alice: the unstaged intent records
	// the token name AND the owning account, exactly as Rotate writes it.
	if err := saveRotationIntent(ctx, store, testSecretPath,
		rotationIntent{Name: "alice-orphan-v3", Slot: 3, Username: "alice"}); err != nil {
		t.Fatalf("seed intent: %v", err)
	}

	// Recovery runs under TODAY'S configuration — a different account.
	if err := rotator.RecoverOrphan(ctx, testSpec()); err != nil {
		t.Fatalf("RecoverOrphan: %v", err)
	}

	if len(lifecycle.revoked) != 1 || lifecycle.revoked[0] != "alice-orphan-v3" {
		t.Fatalf("revoked %v, want exactly the recorded orphan", lifecycle.revoked)
	}
	if lifecycle.revokedBy[0] != "alice" {
		t.Fatalf("revoked as %q, want the RECORDED owner alice — an account-scoped "+
			"NotFound under the wrong account is not a revocation", lifecycle.revokedBy[0])
	}
	intent, _, err := loadRotationIntent(ctx, store, testSecretPath)
	if err != nil {
		t.Fatalf("loadRotationIntent: %v", err)
	}
	if intent.Name != "" {
		t.Fatalf("intent still names %q after the owner-scoped revoke landed", intent.Name)
	}
}

// TestRecoverOrphan_AdoptsUnderTheRecordedOwner is review 979's first
// finding: the staged-first-rotation arm decides ADOPT-or-DESTROY, and it
// must judge the verified identity against the intent's RECORDED owner —
// judging an alice-owned rotation against a later caller's account would
// classify a valid credential as wrong and destroy its only stored copy.
func TestRecoverOrphan_AdoptsUnderTheRecordedOwner(t *testing.T) {
	lifecycle := &fakeLifecycle{verifyAs: "alice"}
	store := newFakeSecretStore()
	rotator, err := NewTokenRotator(lifecycle, store)
	if err != nil {
		t.Fatalf("NewTokenRotator: %v", err)
	}
	ctx := context.Background()

	// A committed, unverified FIRST rotation crashed under alice: the store
	// holds the replacement, the staged intent records name and owner.
	name := RotationTokenName("agent-quicbaas", 1)
	store.data[testSecretPath] = map[string][]byte{tokenSecretKey: []byte("alice-secret")}
	store.metadata[testSecretPath] = map[string]string{
		metadataTokenName: name, metadataRotationSlot: "1",
	}
	store.versions[testSecretPath] = 1
	store.history[testSecretPath] = []fakeVersion{{data: store.data[testSecretPath], metadata: store.metadata[testSecretPath]}}
	if err := saveRotationIntent(ctx, store, testSecretPath,
		rotationIntent{Name: name, Slot: 1, Staged: true, PendingRollback: 0, Username: "alice"}); err != nil {
		t.Fatalf("seed staged intent: %v", err)
	}

	// Recovery runs under TODAY'S configuration — a different account. The
	// token verifies as alice, alice is the recorded owner: ADOPTED.
	if err := rotator.RecoverOrphan(ctx, testSpec()); err != nil {
		t.Fatalf("RecoverOrphan: %v", err)
	}
	if len(lifecycle.revoked) != 0 {
		t.Fatalf("recovery revoked %v — a valid recorded-owner credential was destroyed", lifecycle.revoked)
	}
	if got := string(store.data[testSecretPath][tokenSecretKey]); got != "alice-secret" {
		t.Fatalf("stored credential = %q; adoption must keep the only stored copy", got)
	}
	intent, _, err := loadRotationIntent(ctx, store, testSecretPath)
	if err != nil {
		t.Fatalf("loadRotationIntent: %v", err)
	}
	if intent.Name != "" {
		t.Fatalf("intent still names %q after adoption", intent.Name)
	}
}

// TestRotate_DestroysTheMintedSecretOnEveryPath is the caller's half of the
// #97 rotation-surface slice (reviews 982/984): the adapter hands Rotate the
// secret inside the stack-owned secure buffer, and Rotate DESTROYS the
// enclave on every exit — success and the divergent refusal alike. The fake
// retains the returned *MintedToken so the destruction is observable
// (Bytes() answers nil and Size() answers 0 only after Destroy).
func TestRotate_DestroysTheMintedSecretOnEveryPath(t *testing.T) {
	t.Run("Success", func(t *testing.T) {
		lifecycle := &fakeLifecycle{verifyAs: testAccount}
		store := newFakeSecretStore()
		seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
		rotator, _ := NewTokenRotator(lifecycle, store)
		if _, err := rotator.Rotate(context.Background(), testSpec()); err != nil {
			t.Fatalf("Rotate: %v", err)
		}
		if len(lifecycle.mintedTokens) != 1 {
			t.Fatalf("fixture: %d minted tokens retained", len(lifecycle.mintedTokens))
		}
		if lifecycle.mintedTokens[0].Secret.Bytes() != nil {
			t.Fatal("the minted secret's enclave survived a successful rotation")
		}
		// The STORE holds a usable copy — the destruction reached only the
		// rotation's enclave.
		if got := string(store.data[testSecretPath][tokenSecretKey]); got != "new-secret" {
			t.Fatalf("stored credential = %q; the destroy corrupted the store's copy", got)
		}
	})
	t.Run("DivergentRefusal", func(t *testing.T) {
		lifecycle := &fakeLifecycle{verifyAs: testAccount, mintedName: "renamed-by-forge"}
		store := newFakeSecretStore()
		seedCurrentToken(store, "sdlc-agent-quicbaas-v1", "1")
		rotator, _ := NewTokenRotator(lifecycle, store)
		if _, err := rotator.Rotate(context.Background(), testSpec()); err == nil {
			t.Fatal("a divergent mint was accepted")
		}
		if lifecycle.mintedTokens[0].Secret.Bytes() != nil {
			t.Fatal("the refused divergent secret's enclave survived the refusal path")
		}
	})
}
