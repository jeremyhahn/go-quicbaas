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
	"crypto"
	"crypto/elliptic"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"errors"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-jose/go-jose/v4"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/jeremyhahn/go-xkms/pkg/keyversion"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
	"github.com/jeremyhahn/go-xkms/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// SigningAlgorithmES256 is the only OIDC token signing algorithm produced by
// the xkms-backed key manager. ES256 (ECDSA P-256 + SHA-256) keeps tokens
// small, is widely supported by JWT verifiers, and maps cleanly onto
// hardware-backed crypto.Signer implementations (HSM/TPM) where the private
// key never leaves the device.
const SigningAlgorithmES256 = "ES256"

// keyEntry is an immutable description of one OIDC signing key: its derived
// kid, the crypto.Signer that produces signatures, the public key used for
// verification, and the JWS algorithm.
type keyEntry struct {
	kid    string
	signer crypto.Signer
	pub    crypto.PublicKey
	alg    string
	// until is when an adopted key stops verifying; zero for the keys of
	// the manager's own version record.
	until time.Time
}

// keySnapshot is an immutable view of the active key set. Reads load it
// atomically so the hot signing/verification paths never take a lock. Rotation
// builds a new snapshot and swaps the pointer.
type keySnapshot struct {
	current  keyEntry
	previous []keyEntry
	// adopted holds the keys adopted from another backend (the pre-upgrade
	// key AdoptPreviousFromXKMSBackend keeps). They only verify: they are
	// published and resolvable by kid but never sign, and they sit outside
	// the rotation overlap, so no rotation pushes them out; each verifies
	// until its cutoff (keyEntry.until), and a refresh after it drops it.
	adopted []keyEntry
	// version is the current version of the key's version record the
	// snapshot was built from; 0 when it was not built from one.
	version uint32
}

// rotationOverlap is the number of superseded keys kept resolvable after a
// rotation. Keeping one previous key lets tokens minted just before the
// rotation continue to verify until they expire, while the JWKS publishes both
// the current and previous public keys.
const rotationOverlap = 1

// SigningKeyManager owns the OIDC token signing key material backed by xkms
// (and therefore by whatever keystore xkms is configured with: software,
// PKCS#11/HSM, TPM2, or a cloud KMS). It exposes the current signer for
// minting tokens, resolves public keys by kid for kid-aware validation, and
// supports rotation with an overlap window so in-flight tokens stay valid.
//
// Reads (Current, SignerForKID, PublicForKID, JWKS) are lock-free: they load
// an immutable snapshot via an atomic pointer. Only Rotate takes the mutex.
//
// Per-tenant seam: NewSigningKeyManagerFromXKMS already accepts a tenantID so
// keys are namespaced per tenant in the xkms keystore. A future per-tenant
// resolver can map tenantID -> *SigningKeyManager with a global ("") fallback;
// the public API here does not need to change to support that.
type SigningKeyManager struct {
	cn        string
	tenantID  string
	storeType string

	snap atomic.Pointer[keySnapshot]

	rotateMu rotationMutex

	// lastRefresh is when the snapshot was last checked against the key's
	// version record, in Unix nanoseconds.
	lastRefresh atomic.Int64

	// lastUnknownKIDRefresh is when a token naming a kid the manager did
	// not hold last re-read the version record, in Unix nanoseconds.
	lastUnknownKIDRefresh atomic.Int64

	// unknownKIDMu guards unknownKIDFlight, the unknown-kid refresh in
	// flight (nil when none is).
	unknownKIDMu     sync.Mutex
	unknownKIDFlight *unknownKIDFlight

	// routineMu guards routineFlight, the routine refresh in flight (nil
	// when none is).
	routineMu     sync.Mutex
	routineFlight *unknownKIDFlight
}

// RotationLock serializes rotations of one signing key across every node that
// shares the key's backend. It returns once the caller holds the key's
// rotation, with a context derived from ctx that the rotation runs under and
// that is canceled when the hold is lost, and the function that releases it.
type RotationLock func(ctx context.Context, tenantID, cn string) (lockCtx context.Context, unlock func(), err error)

// rotationLocks maps a backend name to the RotationLock its keys rotate under.
var rotationLocks sync.Map

// SetRotationLock makes every rotation of a signing key on backend hold lock,
// so two nodes sharing the backend never rotate one key at once. A nil lock
// removes the backend's lock.
func SetRotationLock(backend string, lock RotationLock) {
	if lock == nil {
		rotationLocks.Delete(backend)
		return
	}
	rotationLocks.Store(backend, lock)
}

// rotationLockFor returns the RotationLock SetRotationLock set for backend,
// and false when the backend has none.
func rotationLockFor(backend string) (RotationLock, bool) {
	v, _ := rotationLocks.Load(backend)
	rl, ok := v.(RotationLock)
	return rl, ok
}

// AdoptionCutoffs is the cluster-wide record of when a key adopted from
// another backend (the pre-upgrade key AdoptPreviousFromXKMSBackend keeps)
// stops verifying. Every node sharing a backend reads the same cutoff, so
// the adopted key leaves verification and the JWKS at one instant across
// the cluster, and a node started after that instant never adopts it again.
type AdoptionCutoffs interface {
	// Cutoff returns the cutoff recorded for the adopted key kid of the
	// (tenantID, cn) signing key. When none is recorded yet it records the
	// adoption time plus the longest lifetime of any token the key signs,
	// create-if-absent, and returns whichever cutoff the store holds.
	Cutoff(ctx context.Context, tenantID, cn, kid string) (time.Time, error)
	// Now is the clock cutoffs are recorded and checked against.
	Now() time.Time
}

// adoptionCutoffs holds the AdoptionCutoffs registered per backend name.
var adoptionCutoffs sync.Map // map[backend]AdoptionCutoffs

// SetAdoptionCutoffs registers the cutoff record of the keys adopted by
// signing key managers on backend; a nil c removes it. A manager on a
// backend with no cutoff record refuses to adopt a key, since nothing would
// end its trust.
func SetAdoptionCutoffs(backend string, c AdoptionCutoffs) {
	if c == nil {
		adoptionCutoffs.Delete(backend)
		return
	}
	adoptionCutoffs.Store(backend, c)
}

// adoptionCutoffsFor returns the AdoptionCutoffs registered for backend.
func adoptionCutoffsFor(backend string) (AdoptionCutoffs, bool) {
	v, _ := adoptionCutoffs.Load(backend)
	c, ok := v.(AdoptionCutoffs)
	return c, ok
}

// AdoptionCutoffUnavailableError reports an adoption refused because the
// manager's backend has no AdoptionCutoffs registered: an adopted key with
// no cutoff would verify forever.
type AdoptionCutoffUnavailableError struct {
	Backend string
}

// Error implements error.
func (e *AdoptionCutoffUnavailableError) Error() string {
	return "iam: no adoption cutoff record is registered for signing key backend " + e.Backend + "; a key adopted there would never stop verifying"
}

// signingKeyRefreshInterval is how often Current and JWKS re-read the key's
// version record, so a rotation another node made reaches this one.
const signingKeyRefreshInterval = time.Second

// refreshNow is the clock the routine refresh reads; tests replace it.
var refreshNow = time.Now

// unknownKIDRefreshInterval is how often a token naming a key ID this manager
// does not hold may re-read the key's version record.
const unknownKIDRefreshInterval = 100 * time.Millisecond

// unknownKIDRefreshTimeout bounds the unknown-kid refresh: a stalled store or
// a rotation waiting on another node's lease holds the lookups waiting on the
// refresh no longer than this, since the kid is read before the token's
// signature is verified and an unauthenticated request can wait there.
const unknownKIDRefreshTimeout = 3 * time.Second

// unknownKIDRefreshBound is the bound a new unknown-kid refresh runs under;
// tests shorten it.
var unknownKIDRefreshBound = unknownKIDRefreshTimeout

// UnknownKIDRefreshTimeoutError is returned to the lookups waiting on an
// unknown-kid refresh that did not complete within its bound.
type UnknownKIDRefreshTimeoutError struct {
	CN      string
	Timeout time.Duration
}

func (e *UnknownKIDRefreshTimeoutError) Error() string {
	return fmt.Sprintf("iam: refresh of signing key %q for an unknown kid did not complete within %v", e.CN, e.Timeout)
}

// SigningKeyRefreshTimeoutError is returned to a lookup or signer waiting on
// a routine refresh of a key's version record that did not complete within
// unknownKIDRefreshBound.
type SigningKeyRefreshTimeoutError struct {
	CN      string
	Timeout time.Duration
}

func (e *SigningKeyRefreshTimeoutError) Error() string {
	return fmt.Sprintf("iam: routine refresh of signing key %q did not complete within %v", e.CN, e.Timeout)
}

// rotationMutex is a mutex whose LockContext gives up when its context
// ends, so a refresh waiting behind a rotation is bounded. Its zero value is
// an unlocked mutex.
type rotationMutex struct {
	once sync.Once
	sem  chan struct{}
}

func (l *rotationMutex) slot() chan struct{} {
	l.once.Do(func() { l.sem = make(chan struct{}, 1) })
	return l.sem
}

// Lock locks l, waiting as long as it takes.
func (l *rotationMutex) Lock() { l.slot() <- struct{}{} }

// LockContext locks l, or returns ctx's error if ctx ends first.
func (l *rotationMutex) LockContext(ctx context.Context) error {
	select {
	case l.slot() <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TryLock locks l if it is unlocked and reports whether it did.
func (l *rotationMutex) TryLock() bool {
	select {
	case l.slot() <- struct{}{}:
		return true
	default:
		return false
	}
}

// Unlock unlocks l; unlocking an unlocked rotationMutex is a bug and panics,
// as it is a fatal error for sync.Mutex.
func (l *rotationMutex) Unlock() {
	select {
	case <-l.slot():
	default:
		panic("iam: unlock of unlocked rotationMutex")
	}
}

// unknownKIDWorkerHook is told +1 when an unknown-kid refresh worker starts
// and -1 when it exits, so a test can count the workers alive at once.
var unknownKIDWorkerHook = func(delta int) {}

// unknownKIDFlight is one unknown-kid refresh in flight: done is closed when
// it completes or its bound passes, and err is then its outcome.
type unknownKIDFlight struct {
	done chan struct{}
	err  error
}

// UnknownKeyIDError is returned when a token references a kid that is neither
// the current key nor a retained previous key.
type UnknownKeyIDError struct {
	KID string
}

// Error implements the error interface.
func (e *UnknownKeyIDError) Error() string {
	return "iam: unknown signing key id: " + e.KID
}

// SigningKeyUnavailableError is returned when the key manager has no usable
// signing key (for example before initialization completed).
type SigningKeyUnavailableError struct{}

// Error implements the error interface.
func (e *SigningKeyUnavailableError) Error() string {
	return "iam: no signing key available"
}

// SigningKeyRotationUnsupportedError is returned by Rotate on a key manager
// built from an in-process signer: there is no xkms key to rotate.
type SigningKeyRotationUnsupportedError struct{}

// Error implements the error interface.
func (e *SigningKeyRotationUnsupportedError) Error() string {
	return "iam: signing key is not xkms-backed and cannot be rotated"
}

// PublicKeyMarshalError is returned when a public key cannot be serialized for
// kid derivation.
// SystemRotationAmbiguousError is returned by Rotate for the system key when
// keys of other tenants share its common name in the same backend. xkms
// resolves a rotation by common name alone and would take the first such key,
// which may be a tenant's, so the system key cannot be named exactly and the
// rotation is refused before anything is written.
type SystemRotationAmbiguousError struct {
	CN      string
	Tenants []string
}

func (e *SystemRotationAmbiguousError) Error() string {
	return fmt.Sprintf("iam: system rotation of key %q refused: tenants %v hold keys with the same common name", e.CN, e.Tenants)
}

type PublicKeyMarshalError struct {
	Cause error
}

// Error implements the error interface.
func (e *PublicKeyMarshalError) Error() string {
	return "iam: failed to marshal public key for kid derivation: " + e.Cause.Error()
}

// Unwrap returns the underlying cause.
func (e *PublicKeyMarshalError) Unwrap() error {
	return e.Cause
}

// SigningKeyReloadError is returned when another node created the shared
// signing key first and its key could not be loaded after Attempts tries.
type SigningKeyReloadError struct {
	Attempts int
	Conflict error
	Cause    error
}

// Error implements the error interface.
func (e *SigningKeyReloadError) Error() string {
	return fmt.Sprintf("iam: signing key was created by another node but could not be loaded after %d attempts: %v", e.Attempts, e.Cause)
}

// Unwrap returns the create conflict and the last load error.
func (e *SigningKeyReloadError) Unwrap() []error {
	return []error{e.Conflict, e.Cause}
}

// computeKID derives a stable, deterministic key id from a public key. It is
// the base64url (no padding) encoding of the SHA-256 digest of the key's
// DER-encoded SubjectPublicKeyInfo (SPKI). Because it is a pure function of
// the public key, the same key yields the same kid across process restarts,
// which is exactly what JWKS consumers and cached verifiers expect.
func computeKID(pub crypto.PublicKey) (string, error) {
	der, err := x509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return "", &PublicKeyMarshalError{Cause: err}
	}
	sum := sha256.Sum256(der)
	return base64.RawURLEncoding.EncodeToString(sum[:]), nil
}

// es256KeyAttributes builds the xkms KeyAttributes describing the OIDC signing
// key: an ECDSA P-256 signing key with SHA-256. Backend names the registered
// xkms backend that owns the key (go-xkms resolves the backend by this field,
// not by StoreType), and StoreType carries the same name because some backends
// (the software keystore) require an explicit store type during key generation.
func es256KeyAttributes(cn, tenantID, storeType string) *xkmssdk.KeyAttributes {
	return &xkmssdk.KeyAttributes{
		CN:           cn,
		TenantID:     tenantID,
		StoreType:    keyStoreType(storeType),
		Backend:      storeType,
		KeyType:      xkmssdk.KeyTypeSigning,
		KeyAlgorithm: x509.ECDSA,
		Hash:         crypto.SHA256,
		ECCAttributes: &xkmssdk.ECCAttributes{
			Curve: elliptic.P256(),
		},
	}
}

// keyStoreType returns the store-type keyword for the backend registered as
// name. A registered instance name that is not itself a keyword (such as the
// replicated OIDC store) takes its key provider's type: go-xkms parses the
// store type back out of a listed key's ID, and an instance name there reads
// as "unknown", so rotation could not find the key's current version.
func keyStoreType(name string) xkmssdk.StoreType {
	st := xkmssdk.StoreType(name)
	if st.IsValid() {
		return st
	}
	b, err := xkmssdk.ServiceBackendFor(&xkmssdk.KeyAttributes{Backend: name})
	if err != nil {
		return st
	}
	return xkmssdk.StoreType(b.KeyProvider().Type())
}

// currentSigner returns the signer for the current version of the key attrs
// names. xkms.CurrentSigner reads the key's version record under its store
// type; a key on a registered instance whose name is not its store type has
// its record under the instance name (go-xkms records rotations by backend
// registry name), so it is read there.
func currentSigner(ctx context.Context, attrs *xkmssdk.KeyAttributes) (crypto.Signer, error) {
	if attrs.Backend == "" || attrs.Backend == string(attrs.StoreType) {
		return xkms.CurrentSigner(ctx, attrs)
	}
	svc, err := xkmssdk.Get()
	if err != nil {
		return nil, err
	}
	store := svc.KeyVersionStore()
	if store == nil {
		return xkms.Signer(ctx, attrs)
	}
	rec, found, err := store.Load(ctx, attrs.TenantID, attrs.Backend, attrs.CN)
	if err != nil {
		return nil, err
	}
	if !found || rec.Current <= 1 {
		return xkms.Signer(ctx, attrs)
	}
	current := *attrs
	current.CN = xkmssdk.KeyVersionName(attrs.CN, rec.Current)
	return xkms.Signer(ctx, &current)
}

// signerSource is the xkms surface loadSignerForAttrs drives. Tests replace
// it to simulate two nodes racing to create the shared key.
type signerSource struct {
	current        func(context.Context, *xkmssdk.KeyAttributes) (crypto.Signer, error)
	generate       func(context.Context, *xkmssdk.KeyAttributes) (crypto.PrivateKey, error)
	signer         func(context.Context, *xkmssdk.KeyAttributes) (crypto.Signer, error)
	reloadAttempts int
	reloadBackoff  time.Duration
}

// xkmsSignerSource is the production source: the process xkms service.
var xkmsSignerSource = signerSource{
	current:        currentSigner,
	generate:       xkms.GenerateKey,
	signer:         xkms.Signer,
	reloadAttempts: 10,
	reloadBackoff:  100 * time.Millisecond,
}

// loadSignerForAttrs performs get-or-create against xkms: it returns the
// existing signer for attrs when the key is already present, otherwise it
// generates the key and returns the new signer. This is idempotent across
// restarts because xkms keystores persist key material; the first Signer call
// succeeds for any previously created key. When another node wins the create
// of the shared, replicated key, the loser's create fails with
// ErrBackendKeyAlreadyExists and the winner's key is loaded instead.
func loadSignerForAttrs(ctx context.Context, attrs *xkmssdk.KeyAttributes) (crypto.Signer, error) {
	return xkmsSignerSource.load(ctx, attrs)
}

// load is the get-or-create of loadSignerForAttrs against this source.
func (s signerSource) load(ctx context.Context, attrs *xkmssdk.KeyAttributes) (crypto.Signer, error) {
	// The current version: after a rotation the key's live material is a
	// later version, and a restart must sign with it, not with version 1.
	signer, err := s.current(ctx, attrs)
	if err == nil {
		return signer, nil
	}

	// Generate on first boot (or when the key was removed). GenerateKey
	// surfaces a real error if the backend cannot create the key.
	_, genErr := s.generate(ctx, attrs)
	if genErr == nil {
		return s.signer(ctx, attrs)
	}
	if !errors.Is(genErr, xkmssdk.ErrBackendKeyAlreadyExists) {
		return nil, genErr
	}
	return s.reloadCommitted(ctx, attrs, genErr)
}

// reloadCommitted loads the key another node committed first, retrying while
// the commit becomes visible on this node.
func (s signerSource) reloadCommitted(ctx context.Context, attrs *xkmssdk.KeyAttributes, conflict error) (crypto.Signer, error) {
	var lastErr error
	for attempt := 1; attempt <= s.reloadAttempts; attempt++ {
		signer, err := s.current(ctx, attrs)
		if err == nil {
			return signer, nil
		}
		lastErr = err
		if attempt == s.reloadAttempts {
			break
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(attempt) * s.reloadBackoff):
		}
	}
	return nil, &SigningKeyReloadError{Attempts: s.reloadAttempts, Conflict: conflict, Cause: lastErr}
}

// newKeyEntry derives the kid for a signer's public key and packages it as a
// keyEntry.
func newKeyEntry(signer crypto.Signer, alg string) (keyEntry, error) {
	pub := signer.Public()
	kid, err := computeKID(pub)
	if err != nil {
		return keyEntry{}, err
	}
	return keyEntry{
		kid:    kid,
		signer: signer,
		pub:    pub,
		alg:    alg,
	}, nil
}

// NewSigningKeyManagerFromXKMS returns a key manager whose current signing key
// is the ES256 (ECDSA P-256) key identified by cn within tenantID, creating it
// in the configured xkms backend if it does not already exist. The kid is
// derived from the key's public SPKI so it is stable across restarts.
//
// A tenantID of "" selects the global/single-tenant namespace.
func NewSigningKeyManagerFromXKMS(cn string, tenantID string) (*SigningKeyManager, error) {
	if cn == "" {
		return nil, &OIDCConfigRequiredError{Field: "signing key common name"}
	}

	storeType, err := xkms.DefaultBackendName()
	if err != nil {
		return nil, err
	}
	return NewSigningKeyManagerFromXKMSBackend(cn, tenantID, storeType)
}

// NewSigningKeyManagerFromXKMSBackend builds a SigningKeyManager whose ES256
// key is get-or-created in the named xkms backend (storeType) instead of the
// process default. The server passes the replicated OIDC signing backend so
// every node loads the same key, and therefore publishes and verifies the
// same kid. Rotation stays on the same backend.
func NewSigningKeyManagerFromXKMSBackend(cn, tenantID, storeType string) (*SigningKeyManager, error) {
	if cn == "" {
		return nil, &OIDCConfigRequiredError{Field: "signing key common name"}
	}
	if storeType == "" {
		return nil, &OIDCConfigRequiredError{Field: "signing key store type"}
	}

	attrs := es256KeyAttributes(cn, tenantID, storeType)
	signer, err := loadSignerForAttrs(context.Background(), attrs)
	if err != nil {
		return nil, err
	}

	entry, err := newKeyEntry(signer, SigningAlgorithmES256)
	if err != nil {
		return nil, err
	}

	m := &SigningKeyManager{
		cn:        cn,
		tenantID:  tenantID,
		storeType: storeType,
	}
	m.snap.Store(&keySnapshot{current: entry})
	// A key rotated before this node started keeps its earlier version
	// verifiable: the snapshot takes every version the record holds.
	if err := m.Refresh(); err != nil {
		return nil, err
	}
	return m, nil
}

// versioned reports whether the manager's key lives on a registered backend
// instance whose versions the xkms service records, so the snapshot can be
// rebuilt from the key's version record.
func (m *SigningKeyManager) versioned() bool {
	if m.cn == "" {
		return false
	}
	attrs := es256KeyAttributes(m.cn, m.tenantID, m.storeType)
	return attrs.Backend != "" && attrs.Backend != string(attrs.StoreType)
}

// Refresh rebuilds the manager's keys from the key's version record when the
// record's current version differs from the one the manager holds: another
// node sharing the backend may have rotated the key. The record's current
// version becomes current and its newest other enabled versions, then the
// keys the manager held, fill the verification overlap.
func (m *SigningKeyManager) Refresh() error {
	if !m.versioned() {
		return nil
	}
	m.rotateMu.Lock()
	defer m.rotateMu.Unlock()
	return m.refreshLocked(context.Background())
}

// maybeRefresh re-reads the key's version record when it was last read more
// than every ago and no rotation is under way (a rotation re-reads the
// record itself). A failed refresh keeps the keys the manager holds.
//
// One routine refresh runs at a time; a caller arriving while it runs
// waits for it. It runs detached from any one caller, under a context
// bounded by unknownKIDRefreshBound, and holds rotateMu only while it runs.
// Each caller waits only while its own ctx is live, returning ctx's error
// once it is done, and no longer than the bound, returning
// *SigningKeyRefreshTimeoutError after it.
func (m *SigningKeyManager) maybeRefresh(ctx context.Context, every time.Duration) error {
	if !m.versioned() {
		return nil
	}
	if refreshNow().Sub(time.Unix(0, m.lastRefresh.Load())) < every {
		return nil
	}
	bound := unknownKIDRefreshBound
	m.routineMu.Lock()
	flight := m.routineFlight
	if flight == nil {
		if !m.rotateMu.TryLock() {
			m.routineMu.Unlock()
			return nil
		}
		flight = &unknownKIDFlight{done: make(chan struct{})}
		m.routineFlight = flight
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bound)
		go m.runRoutineRefresh(rctx, cancel, bound, flight)
	}
	m.routineMu.Unlock()
	timer := time.NewTimer(bound)
	defer timer.Stop()
	select {
	case <-flight.done:
		return flight.err
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return &SigningKeyRefreshTimeoutError{CN: m.cn, Timeout: bound}
	}
}

// runRoutineRefresh re-reads the version record under ctx with rotateMu,
// which its starter took for it, then releases rotateMu, retires flight and
// releases flight's waiters, with *SigningKeyRefreshTimeoutError when ctx's
// bound passed before the re-read completed.
func (m *SigningKeyManager) runRoutineRefresh(ctx context.Context, cancel context.CancelFunc, bound time.Duration, flight *unknownKIDFlight) {
	defer func() {
		m.routineMu.Lock()
		m.routineFlight = nil
		m.routineMu.Unlock()
		close(flight.done)
	}()
	defer m.rotateMu.Unlock()
	defer cancel()
	_ = m.refreshLocked(ctx)
	if ctx.Err() != nil {
		flight.err = &SigningKeyRefreshTimeoutError{CN: m.cn, Timeout: bound}
	}
}

// refreshLocked is Refresh with rotateMu held.
func (m *SigningKeyManager) refreshLocked(ctx context.Context) error {
	m.lastRefresh.Store(refreshNow().UnixNano())
	prev := m.load()
	if prev == nil {
		return &SigningKeyUnavailableError{}
	}
	// An adopted key past its cutoff leaves the snapshot.
	if live := m.liveAdopted(prev.adopted); len(live) != len(prev.adopted) {
		prev = &keySnapshot{current: prev.current, previous: prev.previous, adopted: live, version: prev.version}
		m.snap.Store(prev)
	}
	svc, err := xkmssdk.Get()
	if err != nil {
		return err
	}
	store := svc.KeyVersionStore()
	if store == nil {
		return nil
	}
	attrs := es256KeyAttributes(m.cn, m.tenantID, m.storeType)
	rec, found, err := store.Load(ctx, attrs.TenantID, attrs.Backend, attrs.CN)
	if err != nil {
		return err
	}
	if !found || rec.Current == prev.version {
		return nil
	}
	current, err := versionEntry(ctx, attrs, rec.Current)
	if err != nil {
		return err
	}
	candidates := make([]keyEntry, 0, rotationOverlap+1+len(prev.previous))
	for _, n := range rec.Enabled() {
		if len(candidates) == rotationOverlap {
			break
		}
		if n == rec.Current {
			continue
		}
		e, err := versionEntry(ctx, attrs, n)
		if err != nil {
			return err
		}
		candidates = append(candidates, e)
	}
	candidates = append(candidates, prev.current)
	candidates = append(candidates, prev.previous...)
	previous := make([]keyEntry, 0, rotationOverlap)
	for _, e := range candidates {
		if len(previous) == rotationOverlap {
			break
		}
		if e.kid == current.kid || slices.ContainsFunc(previous, func(p keyEntry) bool { return p.kid == e.kid }) {
			continue
		}
		previous = append(previous, e)
	}
	m.snap.Store(&keySnapshot{current: current, previous: previous, adopted: prev.adopted, version: rec.Current})
	return nil
}

// versionEntry loads version n of the key attrs name.
func versionEntry(ctx context.Context, attrs *xkmssdk.KeyAttributes, n uint32) (keyEntry, error) {
	a := *attrs
	a.CN = xkmssdk.KeyVersionName(attrs.CN, n)
	signer, err := xkms.Signer(ctx, &a)
	if err != nil {
		return keyEntry{}, err
	}
	return newKeyEntry(signer, SigningAlgorithmES256)
}

// NewSigningKeyManagerFromSigner returns a key manager whose only key is the
// given in-process signer, signing with alg (for example RS256 or ES256). The
// kid is derived from the signer's public key exactly as for xkms-backed keys,
// so tokens carry a kid that the manager's JWKS publishes. The key is not held
// by xkms, so Rotate returns SigningKeyRotationUnsupportedError.
func NewSigningKeyManagerFromSigner(signer crypto.Signer, alg string) (*SigningKeyManager, error) {
	if signer == nil {
		return nil, &OIDCConfigRequiredError{Field: "signer"}
	}
	if alg == "" {
		return nil, &OIDCConfigRequiredError{Field: "signing algorithm"}
	}

	entry, err := newKeyEntry(signer, alg)
	if err != nil {
		return nil, err
	}

	m := &SigningKeyManager{}
	m.snap.Store(&keySnapshot{current: entry})
	return m, nil
}

// load returns the current immutable snapshot.
func (m *SigningKeyManager) load() *keySnapshot {
	return m.snap.Load()
}

// Current returns the kid, signer, and algorithm of the key tokens are
// currently signed with.
func (m *SigningKeyManager) Current() (kid string, signer crypto.Signer, alg string) {
	// A refresh that times out leaves the keys the manager holds.
	_ = m.maybeRefresh(context.Background(), signingKeyRefreshInterval)
	snap := m.load()
	if snap == nil {
		return "", nil, ""
	}
	return snap.current.kid, snap.current.signer, snap.current.alg
}

// Algorithm returns the JWS algorithm used for signing (always ES256).
func (m *SigningKeyManager) Algorithm() string {
	snap := m.load()
	if snap == nil {
		return SigningAlgorithmES256
	}
	return snap.current.alg
}

// SignerForKID returns the signer for the given kid, searching the current key
// and any retained previous keys. It returns UnknownKeyIDError when no key
// matches.
func (m *SigningKeyManager) SignerForKID(kid string) (crypto.Signer, error) {
	snap := m.load()
	if snap == nil {
		return nil, &SigningKeyUnavailableError{}
	}
	if snap.current.kid == kid {
		return snap.current.signer, nil
	}
	for i := range snap.previous {
		if snap.previous[i].kid == kid {
			return snap.previous[i].signer, nil
		}
	}
	return nil, &UnknownKeyIDError{KID: kid}
}

// PublicForKID returns the public key for the given kid, searching the current
// key and any retained previous keys. It returns UnknownKeyIDError when no key
// matches. A kid it does not hold waits on the unknown-kid refresh no longer
// than ctx allows, and no longer than the refresh's own bound; so does a
// kid it holds on the routine refresh.
func (m *SigningKeyManager) PublicForKID(ctx context.Context, kid string) (crypto.PublicKey, error) {
	pub, err := m.refreshedPublicForKID(ctx, kid)
	if errors.As(err, new(*UnknownKeyIDError)) {
		// The key may have been rotated on another node sharing the backend.
		if err := m.refreshForUnknownKID(ctx); err != nil {
			return nil, err
		}
		return m.publicForKID(kid)
	}
	return pub, err
}

// refreshedPublicForKID looks kid up after the routine, rate-limited
// refresh, so a held key a peer has since rotated out of the overlap stops
// verifying within signingKeyRefreshInterval even on a node that only
// verifies. It returns the refresh's error when ctx ends or the refresh's
// bound passes first.
func (m *SigningKeyManager) refreshedPublicForKID(ctx context.Context, kid string) (crypto.PublicKey, error) {
	if err := m.maybeRefresh(ctx, signingKeyRefreshInterval); err != nil {
		return nil, err
	}
	return m.publicForKID(kid)
}

// refreshForUnknownKID re-reads the key's version record for a token naming
// a kid the manager does not hold: a peer may have just rotated the key and
// signed with its new version. It runs at most once per
// unknownKIDRefreshInterval however recently the routine refresh ran, so a
// token from a fresh rotation verifies at once and a flood of unknown kids
// costs one read per interval. A miss arriving while that refresh is in
// flight waits for it to complete, so concurrent tokens under the same new
// kid all see the re-read version record.
//
// Each waiter, the one that started the refresh included, waits only while
// its own ctx is live: a canceled request returns ctx's error at once. The
// refresh runs detached from any one request, so a canceled request does not
// abort it for the rest, and under unknownKIDRefreshBound; a waiter still
// waiting at the bound gets *UnknownKIDRefreshTimeoutError. The refresh
// stays in flight until its worker exits, so however long a stalled store
// keeps the worker, no second worker starts beside it.
func (m *SigningKeyManager) refreshForUnknownKID(ctx context.Context) error {
	if !m.versioned() {
		return nil
	}
	bound := unknownKIDRefreshBound
	m.unknownKIDMu.Lock()
	flight := m.unknownKIDFlight
	if flight == nil {
		now := time.Now().UnixNano()
		if time.Duration(now-m.lastUnknownKIDRefresh.Load()) < unknownKIDRefreshInterval {
			m.unknownKIDMu.Unlock()
			return nil
		}
		m.lastUnknownKIDRefresh.Store(now)
		flight = &unknownKIDFlight{done: make(chan struct{})}
		m.unknownKIDFlight = flight
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), bound)
		go m.runUnknownKIDRefresh(rctx, cancel, bound, flight)
	}
	m.unknownKIDMu.Unlock()
	timer := time.NewTimer(bound)
	defer timer.Stop()
	select {
	case <-flight.done:
		return flight.err
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return &UnknownKIDRefreshTimeoutError{CN: m.cn, Timeout: bound}
	}
}

// runUnknownKIDRefresh is flight's worker: it re-reads the version record
// under ctx, which ends at bound, then retires flight and releases its
// waiters, with *UnknownKIDRefreshTimeoutError when the bound passed first.
// It waits for rotateMu no longer than ctx allows, so a rotation holding
// rotateMu while it waits on a lease holds the worker no longer than the
// bound.
func (m *SigningKeyManager) runUnknownKIDRefresh(ctx context.Context, cancel context.CancelFunc, bound time.Duration, flight *unknownKIDFlight) {
	unknownKIDWorkerHook(1)
	defer func() {
		unknownKIDWorkerHook(-1)
		m.unknownKIDMu.Lock()
		m.unknownKIDFlight = nil
		m.unknownKIDMu.Unlock()
		close(flight.done)
	}()
	defer cancel()
	// A rotation or refresh holding rotateMu re-reads the record itself;
	// waiting for it keeps this miss from failing while it runs.
	if err := m.rotateMu.LockContext(ctx); err == nil {
		_ = m.refreshLocked(ctx)
		m.rotateMu.Unlock()
	}
	if ctx.Err() != nil {
		flight.err = &UnknownKIDRefreshTimeoutError{CN: m.cn, Timeout: bound}
	}
}

// cnAliasKID returns the kid the xkms module writes into the tokens it
// signs with this key -- the key's CN -- when this manager is the global
// OIDC signing key, and "" otherwise. The xkms module names a key by CN,
// not by the derived kid, so the global key is published under both:
// a login token the module signed then verifies against this JWKS.
func (m *SigningKeyManager) cnAliasKID() string {
	if m.tenantID == "" && m.cn == OIDCSigningKeyCN {
		return m.cn
	}
	return ""
}

// publicForKID looks kid up in the manager's snapshot.
func (m *SigningKeyManager) publicForKID(kid string) (crypto.PublicKey, error) {
	snap := m.load()
	if snap == nil {
		return nil, &SigningKeyUnavailableError{}
	}
	if snap.current.kid == kid {
		return snap.current.pub, nil
	}
	if alias := m.cnAliasKID(); alias != "" && kid == alias {
		return snap.current.pub, nil
	}
	for i := range snap.previous {
		if snap.previous[i].kid == kid {
			return snap.previous[i].pub, nil
		}
	}
	for _, e := range m.liveAdopted(snap.adopted) {
		if e.kid == kid {
			return e.pub, nil
		}
	}
	return nil, &UnknownKeyIDError{KID: kid}
}

// liveAdopted returns the adopted keys still before their cutoff on the
// backend's AdoptionCutoffs clock. With no cutoff record registered no
// adopted key is live.
func (m *SigningKeyManager) liveAdopted(adopted []keyEntry) []keyEntry {
	if len(adopted) == 0 {
		return adopted
	}
	cutoffs, ok := adoptionCutoffsFor(m.storeType)
	if !ok {
		return nil
	}
	now := cutoffs.Now()
	live := make([]keyEntry, 0, len(adopted))
	for _, e := range adopted {
		if now.Before(e.until) {
			live = append(live, e)
		}
	}
	return live
}

// JWKS returns the JSON Web Key Set the OIDC provider publishes: one entry per
// active kid (current first, then retained previous keys), each with Use "sig",
// its algorithm, and KeyID set to the derived kid. The global OIDC signing key
// manager also publishes its current key a second time, right after the
// current entry, under the alias kid cnAliasKID returns; the alias always
// names the current key.
func (m *SigningKeyManager) JWKS() *jose.JSONWebKeySet {
	// A refresh that times out leaves the keys the manager holds.
	_ = m.maybeRefresh(context.Background(), signingKeyRefreshInterval)
	snap := m.load()
	if snap == nil {
		return &jose.JSONWebKeySet{}
	}

	keys := make([]jose.JSONWebKey, 0, 2+len(snap.previous)+len(snap.adopted))
	keys = append(keys, jose.JSONWebKey{
		Key:       snap.current.pub,
		KeyID:     snap.current.kid,
		Algorithm: snap.current.alg,
		Use:       "sig",
	})
	if alias := m.cnAliasKID(); alias != "" {
		keys = append(keys, jose.JSONWebKey{
			Key:       snap.current.pub,
			KeyID:     alias,
			Algorithm: snap.current.alg,
			Use:       "sig",
		})
	}
	for _, e := range slices.Concat(snap.previous, m.liveAdopted(snap.adopted)) {
		keys = append(keys, jose.JSONWebKey{
			Key:       e.pub,
			KeyID:     e.kid,
			Algorithm: e.alg,
			Use:       "sig",
		})
	}
	return &jose.JSONWebKeySet{Keys: keys}
}

// Rotate generates a fresh ES256 key in the xkms backend, promotes it to the
// current key, and demotes the previously current key into the overlap window
// (pruned to rotationOverlap entries). Tokens already signed with the demoted
// key remain verifiable until they expire because both keys stay in the JWKS
// and resolvable by kid.
func (m *SigningKeyManager) Rotate() error {
	m.rotateMu.Lock()
	defer m.rotateMu.Unlock()

	prev := m.load()
	if prev == nil {
		return &SigningKeyUnavailableError{}
	}
	// Only NewSigningKeyManagerFromXKMS sets cn; a manager without one holds
	// an in-process signer that xkms cannot rotate.
	if m.cn == "" {
		return &SigningKeyRotationUnsupportedError{}
	}

	attrs := es256KeyAttributes(m.cn, m.tenantID, m.storeType)

	// RotateKey creates the key's next version -- a new key with a new
	// public key, hence a new derived kid -- and makes it current; earlier
	// versions are kept. We then fetch the signer for the current version;
	// backends may return a handle whose private key never leaves the device.
	// The service API resolves registered backend instance names (such as
	// the replicated OIDC store), which a parsed key ID string does not.
	ctx := context.Background()

	// A system rotation must name the system-owned key exactly; it is
	// checked before the lease is taken, so a refusal writes nothing, and
	// again once the lease is held.
	if isSystemTenant(m.tenantID) {
		if err := m.requireExactSystemKey(ctx); err != nil {
			return err
		}
	}

	// Nodes sharing the backend rotate the key one at a time, each from the
	// version record the last rotation left: a node rotating from a stale
	// record would take another node's version for an orphan and replace it.
	if lock, ok := rotationLockFor(m.storeType); ok {
		lockCtx, unlock, err := lock(ctx, m.tenantID, m.cn)
		if err != nil {
			return err
		}
		defer unlock()
		// The rotation runs under the lease: a lost lease cancels it.
		ctx = lockCtx
	}
	// A tenant's dedicated key created while this rotation waited for
	// its lease holds no lease this one excludes: the check is made again
	// under the lease, before anything is written.
	if isSystemTenant(m.tenantID) {
		if err := m.requireExactSystemKey(ctx); err != nil {
			return err
		}
	}
	if m.versioned() {
		if err := m.refreshLocked(ctx); err != nil {
			return err
		}
		prev = m.load()
	}

	svc, err := xkmssdk.Get()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := svc.RotateKey(ctx, &transport.RotateKeyRequest{
		Backend:  m.storeType,
		KeyID:    m.cn,
		TenantID: m.tenantID,
	}); err != nil {
		return err
	}

	if m.versioned() {
		if err := m.refreshLocked(ctx); err != nil {
			return err
		}
		if m.load() != prev {
			return nil
		}
	}

	signer, err := currentSigner(ctx, attrs)
	if err != nil {
		return err
	}

	newEntry, err := newKeyEntry(signer, SigningAlgorithmES256)
	if err != nil {
		return err
	}

	// If rotation produced the same key (no-op backend), do not duplicate it.
	if newEntry.kid == prev.current.kid {
		return nil
	}

	previous := make([]keyEntry, 0, rotationOverlap)
	previous = append(previous, prev.current)
	for i := 0; i < len(prev.previous) && len(previous) < rotationOverlap; i++ {
		previous = append(previous, prev.previous[i])
	}

	m.snap.Store(&keySnapshot{
		current:  newEntry,
		previous: previous,
		adopted:  prev.adopted,
	})
	return nil
}

// isSystemTenant reports whether tenantID names the system tenant, however
// spelled.
func isSystemTenant(tenantID string) bool {
	return tenantID == "" || qrdbsdk.IsSystemTenant(tenantID)
}

// requireExactSystemKey refuses a system rotation the xkms common-name lookup
// could resolve to another tenant's key: RotateKey takes the first key in the
// backend with the common name, so the system key is named exactly only when
// no other tenant holds a key, or a version of one, with that name. There is
// no fallback to the ambiguous lookup.
func (m *SigningKeyManager) requireExactSystemKey(ctx context.Context) error {
	svc, err := xkmssdk.Get()
	if err != nil {
		return err
	}
	name := m.storeType
	if name == "" {
		name = svc.DefaultBackendName()
	}
	b, err := svc.LookupBackend(name)
	if err != nil {
		return err
	}
	keys, err := b.ListKeys(ctx)
	if err != nil {
		return err
	}
	var tenants []string
	for _, attrs := range keys {
		if attrs == nil || keyversion.BaseKeyID(attrs.CN) != m.cn || isSystemTenant(attrs.TenantID) {
			continue
		}
		if !slices.Contains(tenants, attrs.TenantID) {
			tenants = append(tenants, attrs.TenantID)
		}
	}
	if len(tenants) > 0 {
		slices.Sort(tenants)
		return &SystemRotationAmbiguousError{CN: m.cn, Tenants: tenants}
	}
	return nil
}

// AdoptPreviousFromXKMSBackend keeps the manager's ES256 key from another
// xkms backend (storeType) as a verification-only key: it is published in
// JWKS and resolvable by PublicForKID but never signs, and it is kept beside
// the rotation overlap, so later rotations and refreshes neither push it out
// nor lose the versions the overlap holds. It verifies until the backend's
// AdoptionCutoffs cutoff for it, and is not adopted after it. An upgrade that moves the
// signing key to a new backend uses it so tokens signed with the old key keep
// verifying. It only reads the existing key and never creates one; when
// storeType holds no such key it returns false and changes nothing.
func (m *SigningKeyManager) AdoptPreviousFromXKMSBackend(storeType string) (bool, error) {
	if storeType == "" {
		return false, &OIDCConfigRequiredError{Field: "signing key store type"}
	}
	if m.cn == "" {
		return false, &SigningKeyRotationUnsupportedError{}
	}

	m.rotateMu.Lock()
	defer m.rotateMu.Unlock()

	prev := m.load()
	if prev == nil {
		return false, &SigningKeyUnavailableError{}
	}

	signer, err := currentSigner(context.Background(), es256KeyAttributes(m.cn, m.tenantID, storeType))
	if err != nil {
		if errors.Is(err, xkmssdk.ErrKeyNotFound) || errors.Is(err, xkmssdk.ErrBackendKeyNotFound) {
			return false, nil
		}
		return false, err
	}
	entry, err := newKeyEntry(signer, SigningAlgorithmES256)
	if err != nil {
		return false, err
	}
	if entry.kid == prev.current.kid {
		return false, nil
	}
	sameKID := func(p keyEntry) bool { return p.kid == entry.kid }
	if slices.ContainsFunc(prev.previous, sameKID) || slices.ContainsFunc(prev.adopted, sameKID) {
		return false, nil
	}
	// The adopted key verifies only until the cluster's cutoff for it: a
	// node starting after the cutoff does not adopt it again.
	cutoffs, ok := adoptionCutoffsFor(m.storeType)
	if !ok {
		return false, &AdoptionCutoffUnavailableError{Backend: m.storeType}
	}
	until, err := cutoffs.Cutoff(context.Background(), m.tenantID, m.cn, entry.kid)
	if err != nil {
		return false, err
	}
	if !cutoffs.Now().Before(until) {
		return false, nil
	}
	entry.until = until
	// The adopted key verifies beside the rotation overlap rather than in
	// it: the versions the record keeps stay in the overlap, and the
	// snapshot's version is unchanged, so a later refresh still rebuilds
	// the overlap from the record.
	adopted := append(slices.Clone(prev.adopted), entry)
	m.snap.Store(&keySnapshot{current: prev.current, previous: prev.previous, adopted: adopted, version: prev.version})
	return true, nil
}

// AdoptDefaultBackendKey keeps the manager's pre-upgrade key from the process
// default xkms backend as a verification-only key (AdoptPreviousFromXKMSBackend) when
// the manager's own backend is another one, such as the replicated store. It
// only reads the default backend; on the default backend itself, or when
// there is no default backend, it does nothing.
func (m *SigningKeyManager) AdoptDefaultBackendKey() error {
	defaultName, err := xkms.DefaultBackendName()
	if err != nil || defaultName == m.storeType {
		return nil
	}
	_, err = m.AdoptPreviousFromXKMSBackend(defaultName)
	return err
}

// rotationKID builds the fully-qualified xkms key reference used to address the
// signing key for in-place rotation. The xkms key id format is
// "backend:type:algo:keyname"; the backend (store type) segment must be present
// so the resolved KeyAttributes carry the StoreType that backends require for
// key deletion/regeneration during rotation.
func (m *SigningKeyManager) rotationKID() string {
	return m.storeType + ":signing:ecdsa-p256:" + m.cn
}
