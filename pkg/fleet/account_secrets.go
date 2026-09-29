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

package fleet

import (
	"context"
	"encoding/json"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/secure"
)

// =============================================================================
// The daemon's own credential store: a READ-THROUGH cache over the control
// plane's materialize endpoint.
//
// orchestration.AccountSecretStore — the type AccountTokenManager and therefore
// AccountResolver read every credential through — is a thin wrapper over an
// interfaces.SecretsManager. That interface is the seam, so the daemon supplies
// one whose reads are satisfied from the control plane instead of from a
// barrier-backed vault, and the whole resolver stack above it composes
// unchanged (go-quicbaas#734).
//
// WHERE THE CREDENTIAL LIVES. In this process's memory, and nowhere else.
//
// That is a deliberate narrowing of the issue's "cache it in the daemon's own
// secret store": a worker host has no barrier, no KV and no encrypted-at-rest
// backend to be that store, so the only durable option available is a
// plaintext file under the work directory — a live provider login sitting at
// rest, in the clear, on the least-trusted host in the deployment. The cache's
// whole job (avoid a release per spawn, honour the credential's TTL, notice a
// rotation) is served entirely in memory. The one thing persistence would buy
// is survival across a daemon restart, which costs exactly one mTLS round trip
// to recover and which a restarting daemon has to make anyway to re-enrol.
//
// HOW THE CREDENTIAL IS HELD. In memory is not the same as on the Go heap. A
// released envelope is a live provider OAuth token; ordinary heap bytes are
// swappable to disk and linger until the garbage collector runs. Each cached
// envelope therefore lives in a memguard secure.Buffer (mlocked, guard-paged,
// zeroized on Destroy — go-quicbaas#757), and the plaintext is copied out only
// for the instant a spawn is handed its credential. The buffer is destroyed the
// moment its entry is evicted, replaced or the store is closed, so a retired
// token is wiped rather than left for the GC.
//
// So: nothing here writes a credential to disk, formats one into a log line, or
// puts one in an error. The only egress is the ResolvedAccountCredential the
// resolver hands to a spawn.
// =============================================================================

// accountCacheRenewalMargin is how much life a cached envelope must have left
// to be served from cache.
//
// It is twice AccountTokenManager's own refresh skew, and that relationship is
// the point. The manager renews an oauth_refresh credential when it has less
// than one skew left — but renewal on a daemon is impossible by design (the
// daemon never receives a refresh token) and would fail with
// AccountRefresherNotConfiguredError. Expiring the cache a full skew EARLIER
// means the daemon re-materializes first, the control plane renews on the side
// that owns the refresh token, and the manager is handed a credential that is
// comfortably usable. The renewal path is therefore unreachable rather than
// merely unlikely.
const accountCacheRenewalMargin = 2 * orchestration.DefaultAccountRefreshSkew

// accountCacheMaxTTL bounds how long an envelope with no declared expiry is
// served from cache.
//
// A static token typically reports no expiry at all (see
// orchestration.AccountCredential.ExpiresAt), so without a ceiling it would be
// cached for the daemon's whole lifetime and an operator's rotation would
// reach this host only when it restarted. Fifteen minutes bounds that window
// while still collapsing a burst of spawns onto one release. A rotation is
// normally noticed far sooner than this — the account record's UpdatedAt moves,
// and the resolver reads that record before every credential read — so this is
// the backstop for a rotation that somehow left UpdatedAt alone, not the
// primary mechanism.
const accountCacheMaxTTL = 15 * time.Minute

// =============================================================================
// The cache
// =============================================================================

// cachedAccountCredential is one released envelope plus what decides whether it
// is still good.
//
// The envelope's JSON — a live provider token — is held in a memguard
// secure.Buffer, never on the plain heap. Readers never share that buffer:
// snapshot copies the plaintext into a fresh slice for the spawn path and the
// buffer itself is destroyed once no reader holds it and the entry has been
// evicted (see refs).
type cachedAccountCredential struct {
	// secret holds the released envelope, JSON-encoded in the shape
	// AccountSecretStore expects, in mlocked, guard-paged memory that is
	// zeroized on Destroy. It is never read except through snapshot, and only
	// while a live reference is held.
	secret *secure.Buffer

	// refs is the live-reference count: one for the cache's own hold plus one
	// for each in-flight reader. The secret is destroyed the instant it reaches
	// zero, so a reader that loaded this pointer just before an eviction still
	// reads valid bytes, and the plaintext is wiped as soon as the last reader
	// is done rather than lingering for the garbage collector. A retired entry
	// (refs <= 0) can never be resurrected: acquire refuses it.
	refs atomic.Int32

	// releasedUnder is the account record's LastRefreshedAt as of the release.
	// A later record reporting a newer one means the envelope was replaced and
	// this entry is superseded.
	releasedUnder time.Time

	// freshUntil is when this entry stops being served.
	freshUntil time.Time

	// leaseID is the DELIVERY GRANT this copy was released under. It is not
	// secret material — it is an authorization handle — and it is immutable for
	// the life of the entry, so it needs no synchronization beyond the entry's
	// own publication through the cache's sync.Map.
	leaseID string

	// leaseExpiresAt is when that grant stops being authorised without renewal.
	// It is NOT the credential's expiry, which is inside the envelope: the
	// control plane's OAuth refresher owns the credential's lifetime, and the
	// grant governs only the delivery. Carried alongside leaseID so a spawn's
	// session record can report the grant it was served under in full.
	leaseExpiresAt time.Time
}

// newCachedAccountCredential seals encoded into a memguard buffer and returns a
// cache entry that already holds the cache's own reference. encoded is wiped by
// the buffer constructor, so the caller must not reuse it.
func newCachedAccountCredential(
	encoded []byte,
	releasedUnder, freshUntil time.Time,
	grant accountDeliveryGrant,
) (*cachedAccountCredential, error) {
	buf, err := secure.NewBufferFromBytes(encoded)
	if err != nil {
		return nil, err
	}
	entry := &cachedAccountCredential{
		secret:         buf,
		releasedUnder:  releasedUnder,
		freshUntil:     freshUntil,
		leaseID:        grant.LeaseID,
		leaseExpiresAt: grant.ExpiresAt,
	}
	entry.refs.Store(1)
	return entry, nil
}

// acquire takes a transient reference, returning false if the entry has already
// been retired (its buffer destroyed or about to be). It is lock-free: the CAS
// loop refuses any count that has reached zero, so an acquire can never race a
// Destroy back to life.
func (e *cachedAccountCredential) acquire() bool {
	for {
		n := e.refs.Load()
		if n <= 0 {
			return false
		}
		if e.refs.CompareAndSwap(n, n+1) {
			return true
		}
	}
}

// release drops one reference and destroys the buffer when the last one goes.
func (e *cachedAccountCredential) release() {
	if e.refs.Add(-1) == 0 {
		e.secret.Destroy()
	}
}

// snapshot copies the sealed envelope into a fresh heap slice the caller owns.
// The returned bytes are the plaintext a spawn is handed; they are the one
// unavoidable copy out of secure memory. The caller must hold a live reference.
func (e *cachedAccountCredential) snapshot() []byte {
	b := e.secret.Bytes()
	out := make([]byte, len(b))
	copy(out, b)
	return out
}

// copyOut returns a fresh copy of a published entry's envelope, taking and
// releasing a reference so the buffer cannot be destroyed mid-read. It reports
// false when the entry was retired concurrently, which a caller treats as a
// cache miss.
func (e *cachedAccountCredential) copyOut() ([]byte, bool) {
	if !e.acquire() {
		return nil, false
	}
	defer e.release()
	return e.snapshot(), true
}

// accountCredentialCache holds released envelopes keyed by account ID.
//
// Reads are lock-free (sync.Map over reference-counted entries); the only lock
// is the per-account single-flight that keeps a burst of concurrent spawns for
// one account from producing a burst of releases — each of which would be a
// real credential release with its own audit event on the control plane.
type accountCredentialCache struct {
	entries  sync.Map // accountID -> *cachedAccountCredential
	releases keyedFlight

	// hits / misses are observability only. They count decisions, never
	// content.
	hits   atomic.Int64
	misses atomic.Int64
}

// lookup returns the cached entry for accountID when it is still fresh at now.
// It does not take a reference; a caller that intends to read the envelope must
// go through the entry's copyOut, which does.
//
// A STALE ENTRY IS EVICTED HERE, NOT LEFT TO SIT. "Ages out" has to mean the
// entry is gone, because a copy that has stopped being served while still
// CARRYING its delivery grant is what made a revoked grant unrecoverable: the
// next release presented that dead id, the control plane refused it, and
// nothing ever removed the entry, so every spawn from then on failed until the
// process restarted. A routine session teardown revokes the grant, so one
// teardown was enough to stop the daemon for good.
//
// Owner decision, 2026-09-14. It is a real trade and worth naming: while the
// copy is fresh, revocation still cannot reach it (it is served from memory and
// no release is attempted), but once the window closes the daemon asks again
// with no grant and the control plane decides afresh. Revocation of a single
// delivery therefore stops that delivery rather than the host; cutting a host
// off is done by disabling or fencing the ACCOUNT, which GrantDelivery re-checks
// on every first release.
func (c *accountCredentialCache) lookup(accountID string, now time.Time) (*cachedAccountCredential, bool) {
	value, ok := c.entries.Load(accountID)
	if !ok {
		return nil, false
	}
	entry, ok := value.(*cachedAccountCredential)
	if !ok {
		return nil, false
	}
	if !now.Before(entry.freshUntil) {
		c.drop(accountID)
		return nil, false
	}
	return entry, true
}

// recordGrant writes the grant this copy is held under into the calling
// resolution's slot, so the grantor reports this copy's grant and not whatever
// a concurrent spawn later stored for the account.
func (e *cachedAccountCredential) recordGrant(ctx context.Context) {
	orchestration.DeliveryGrantSlotFrom(ctx).Record(e.leaseID, e.leaseExpiresAt)
}

// accountDeliveryGrant is the non-secret authorization handle a released copy
// is held under: which grant, and until when it stays authorised without
// renewal. Its zero value means the control plane leases no deliveries.
type accountDeliveryGrant struct {
	// LeaseID identifies the grant. Empty means none.
	LeaseID string

	// ExpiresAt is when the grant lapses without renewal. It is not the
	// credential's expiry.
	ExpiresAt time.Time
}

// grantFor returns the delivery grant a cached copy of accountID is held under,
// zero when nothing is cached.
//
// It reads the entry WITHOUT the freshness test lookup applies, and it has two
// callers that both need that:
//
//   - a refresh about to be made for a copy whose cache window has closed must
//     present the grant it was released under; dropping it would turn every
//     refresh into a first release and make revocation unenforceable;
//   - a spawn's resolution must report the grant its release was served under
//     so the control plane can record it on the session record
//     (account_grant.go).
func (c *accountCredentialCache) grantFor(accountID string) accountDeliveryGrant {
	value, ok := c.entries.Load(accountID)
	if !ok {
		return accountDeliveryGrant{}
	}
	entry, ok := value.(*cachedAccountCredential)
	if !ok {
		return accountDeliveryGrant{}
	}
	return accountDeliveryGrant{LeaseID: entry.leaseID, ExpiresAt: entry.leaseExpiresAt}
}

// store records a freshly released envelope, releasing whatever it replaces.
// A superseded entry's buffer is wiped once its own readers have finished with
// it (release drops the cache's reference, which is the last one unless a
// reader is mid-copy).
func (c *accountCredentialCache) store(accountID string, entry *cachedAccountCredential) {
	if previous, loaded := c.entries.Swap(accountID, entry); loaded {
		if old, ok := previous.(*cachedAccountCredential); ok {
			old.release()
		}
	}
}

// drop removes one entry and releases the cache's reference to it, wiping the
// buffer once no reader still holds it. It is the single eviction primitive.
func (c *accountCredentialCache) drop(key any) {
	if previous, loaded := c.entries.LoadAndDelete(key); loaded {
		if old, ok := previous.(*cachedAccountCredential); ok {
			old.release()
		}
	}
}

// Observe drops any cached credential released before lastRefreshedAt. It
// implements accountObserver.
//
// THIS IS THE ROTATION SIGNAL. An operator rotating a credential on the control
// plane stamps MarkRefreshed on the account record, which is also what an
// oauth renewal there stamps; the resolver reads that record before it reads
// any credential, so the retired envelope is evicted on the very next spawn
// rather than lingering until its own expiry.
//
// It is deliberately NOT the record's UpdatedAt, which was the obvious choice
// and the wrong one: UpdatedAt moves on any write, including the usage stamp
// the control plane makes as part of the release itself. Keyed on that, every
// release invalidated the entry it had just created and the cache never
// returned a single hit.
//
// A zero lastRefreshedAt observes nothing: an account whose envelope has never
// been replaced has nothing to invalidate against, and evicting on it would
// force a release per spawn.
func (c *accountCredentialCache) Observe(accountID string, lastRefreshedAt time.Time) {
	if accountID == "" || lastRefreshedAt.IsZero() {
		return
	}
	value, ok := c.entries.Load(accountID)
	if !ok {
		return
	}
	entry, ok := value.(*cachedAccountCredential)
	if !ok || lastRefreshedAt.After(entry.releasedUnder) {
		c.drop(accountID)
	}
}

// Forget drops one account's cached envelope unconditionally.
func (c *accountCredentialCache) Forget(accountID string) {
	c.drop(accountID)
}

// Stats reports cache decisions for diagnostics. It never reports content.
func (c *accountCredentialCache) Stats() (hits, misses int64) {
	return c.hits.Load(), c.misses.Load()
}

// keyedFlight serializes work per key so N concurrent callers for one key
// produce one unit of work, not N.
type keyedFlight struct {
	mu     sync.Mutex
	locks  map[string]*sync.Mutex
	inited bool
}

// lock acquires the mutex guarding key and returns its release function.
func (k *keyedFlight) lock(key string) func() {
	k.mu.Lock()
	if !k.inited {
		k.locks = make(map[string]*sync.Mutex)
		k.inited = true
	}
	mu, ok := k.locks[key]
	if !ok {
		mu = &sync.Mutex{}
		k.locks[key] = mu
	}
	k.mu.Unlock()

	mu.Lock()
	return mu.Unlock
}

// =============================================================================
// The SecretsManager
// =============================================================================

// materializingSecrets is the interfaces.SecretsManager a fleet node daemon backs
// its orchestration.AccountSecretStore with.
//
// Exactly one method does anything: Get, which resolves a cached envelope or
// releases a fresh one from the control plane. Every other method is REFUSED
// with a typed error rather than quietly succeeding, because each one would
// otherwise mean the daemon believes it owns credential material it does not:
// a Store that appeared to work would leave a renewed token on this host and
// nowhere else, and a Destroy that appeared to work would tell a caller a
// credential was gone while the control plane still held it.
type materializingSecrets struct {
	client *AccountAPIClient
	cache  *accountCredentialCache
	logger interfaces.Logger

	// projectID scopes every release this daemon makes. It is empty on a
	// daemon serving no particular project, which is the ordinary case: the
	// project a spawn belongs to reaches the control plane through the
	// resolver's own project filter, and an account fenced to a project is
	// refused release to a request that does not name it.
	projectID string
}

// newMaterializingSecrets returns the daemon's read-through credential store.
func newMaterializingSecrets(client *AccountAPIClient, cache *accountCredentialCache, logger interfaces.Logger) *materializingSecrets {
	return &materializingSecrets{client: client, cache: cache, logger: logger}
}

// Get returns the credential envelope for the account the path addresses,
// releasing it from the control plane when nothing usable is cached.
func (m *materializingSecrets) Get(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
	tenantID, accountID, ok := orchestration.ParseAccountSecretPath(path)
	if !ok {
		return nil, nil, &AccountSecretsUnsupportedPathError{Path: path}
	}
	if tenantID != m.client.TenantID() {
		return nil, nil, &AccountAPITenantMismatchError{Requested: tenantID, Bound: m.client.TenantID()}
	}

	body, err := m.resolve(ctx, accountID)
	if err != nil {
		return nil, nil, err
	}
	// body is a fresh copy taken out of secure memory; it is the credential a
	// spawn is handed and travels no further than the container env file.
	return map[string][]byte{orchestration.AccountSecretDataKey: body},
		map[string]string{"kind": "agent_account_credential", "source": "control_plane"}, nil
}

// resolve returns a fresh plaintext copy of accountID's envelope, releasing one
// from the control plane if nothing usable is cached.
func (m *materializingSecrets) resolve(ctx context.Context, accountID string) ([]byte, error) {
	now := time.Now().UTC()
	if entry, ok := m.cache.lookup(accountID, now); ok {
		if body, ok := entry.copyOut(); ok {
			m.cache.hits.Add(1)
			entry.recordGrant(ctx)
			return body, nil
		}
	}

	// Single-flight: a burst of concurrent spawns against one account must
	// produce ONE release, not one per spawn. Each release is a real
	// credential handout with its own audit event on the control plane.
	unlock := m.cache.releases.lock(accountID)
	defer unlock()

	// Double-check: a racing caller may have released while we waited.
	if entry, ok := m.cache.lookup(accountID, time.Now().UTC()); ok {
		if body, ok := entry.copyOut(); ok {
			m.cache.hits.Add(1)
			entry.recordGrant(ctx)
			return body, nil
		}
	}
	m.cache.misses.Add(1)

	// THE GRANT THIS DAEMON ALREADY HOLDS IS PRESENTED, and that is what makes
	// the control plane able to cut this host off. A refresh under a revoked
	// grant is refused there (pkg/service/account_materialize.go), so the copy
	// in this process ages out of the cache with nothing able to replace it —
	// which is the whole of "a credential is unusable after its lease is
	// revoked" as far as a credential issued by somebody else's identity
	// provider can be made unusable.
	released, err := m.client.MaterializeCredential(
		ctx, accountID, m.projectID, m.cache.grantFor(accountID).LeaseID)
	if err != nil {
		// The control plane's refusal is the answer, unchanged. It already
		// says which account and why (revoked, fenced to another project, no
		// such account), and wrapping it again would bury that.
		return nil, err
	}

	encoded, marshalErr := json.Marshal(released.Credential)
	if marshalErr != nil {
		return nil, &AccountAPIError{Op: "encode released credential", AccountID: accountID, Cause: marshalErr}
	}

	now = time.Now().UTC()
	freshUntil := accountCacheFreshUntil(released.Credential.ExpiresAt, now)
	if released.SingleUse {
		// A REFERENCED account's credential is a tenant secret, and writing
		// that secret changes no account timestamp — the cache's only
		// staleness signal (Observe, on last_refreshed_at) cannot see it. So
		// the copy is not reused: it is stored already expired, which serves
		// THIS spawn and keeps its delivery grant reportable, and the next
		// spawn re-releases and gets whatever the secret holds then. Checking
		// the secret's revision instead would cost the same round trip to the
		// control plane that a release does, so there is nothing cheaper to
		// key on.
		freshUntil = now
	}
	entry, sealErr := newCachedAccountCredential(
		encoded,
		released.LastRefreshedAt,
		freshUntil,
		accountDeliveryGrant{LeaseID: released.LeaseID, ExpiresAt: released.LeaseExpiresAt},
	)
	if sealErr != nil {
		return nil, &AccountAPIError{Op: "seal released credential", AccountID: accountID, Cause: sealErr}
	}

	// Copy the plaintext out for THIS spawn before publishing the entry: the
	// buffer is still privately owned here, so the read needs no reference and
	// cannot race a concurrent eviction.
	body := entry.snapshot()
	entry.recordGrant(ctx)
	m.cache.store(accountID, entry)

	if m.logger != nil {
		// Account ID and expiry only. Never the envelope.
		m.logger.Debugf("agent account pool: released credential for account %s (cached until %s)",
			accountID, entry.freshUntil.Format(time.RFC3339))
	}
	return body, nil
}

// accountCacheFreshUntil decides how long a released envelope may be served.
//
// An envelope that declares an expiry is cached until a renewal margin before
// it, capped by accountCacheMaxTTL; one that declares none is cached for the
// cap alone. An envelope already inside its margin is not cached at all — it is
// served once and re-released next time, which is the honest handling of a
// credential the control plane could not renew.
func accountCacheFreshUntil(expiresAt, now time.Time) time.Time {
	ceiling := now.Add(accountCacheMaxTTL)
	if expiresAt.IsZero() {
		return ceiling
	}
	usableUntil := expiresAt.Add(-accountCacheRenewalMargin)
	if !usableUntil.After(now) {
		return now
	}
	if usableUntil.Before(ceiling) {
		return usableUntil
	}
	return ceiling
}

// =============================================================================
// Refused surface
//
// The remaining SecretsManager methods exist so this type satisfies the
// interface AccountSecretStore composes over. Each refuses loudly.
// =============================================================================

// Store refuses: a daemon does not own credential material.
func (m *materializingSecrets) Store(_ context.Context, path string, _ map[string][]byte, _ map[string]string) error {
	return &AccountSecretsReadOnlyError{Method: "Store", Path: path}
}

// StoreWithOptions refuses, for the same reason as Store. In practice it is
// unreachable: the only caller is AccountTokenManager's renewal path, which
// this store's freshness margin keeps out of reach (see
// accountCacheRenewalMargin). It refuses rather than being omitted so a future
// caller that does reach it fails visibly instead of writing a credential this
// host would then be the only holder of.
func (m *materializingSecrets) StoreWithOptions(
	_ context.Context, path string, _ map[string][]byte, _ map[string]string, _ *interfaces.StoreOptions,
) error {
	return &AccountSecretsReadOnlyError{Method: "StoreWithOptions", Path: path}
}

// GetVersion refuses: the daemon holds one released envelope per account and
// no history of them.
func (m *materializingSecrets) GetVersion(_ context.Context, path string, _ int) (map[string][]byte, map[string]string, error) {
	return nil, nil, &AccountSecretsReadOnlyError{Method: "GetVersion", Path: path}
}

// List refuses: enumerating credentials is the control plane's metadata
// surface (ListAccounts), not this store's.
func (m *materializingSecrets) List(_ context.Context, prefix string) ([]string, error) {
	return nil, &AccountSecretsReadOnlyError{Method: "List", Path: prefix}
}

// Delete drops the daemon's cached copy and reports success.
//
// It is the one refusal-adjacent method that DOES something, and the something
// is local: forgetting a cached envelope is exactly what a daemon may do with
// it, and the durable record is untouched on the control plane. A caller asking
// this store to forget a credential gets what it asked for.
func (m *materializingSecrets) Delete(_ context.Context, path string) error {
	_, accountID, ok := orchestration.ParseAccountSecretPath(path)
	if !ok {
		return &AccountSecretsUnsupportedPathError{Path: path}
	}
	m.cache.Forget(accountID)
	return nil
}

// Destroy refuses: permanently removing a credential is a control-plane verb,
// and answering "done" here would tell a caller a credential is gone while the
// pool still holds it.
func (m *materializingSecrets) Destroy(_ context.Context, path string) error {
	return &AccountSecretsReadOnlyError{Method: "Destroy", Path: path}
}

// ListVersions refuses: see GetVersion.
func (m *materializingSecrets) ListVersions(_ context.Context, path string) ([]*interfaces.SecretVersionMetadata, error) {
	return nil, &AccountSecretsReadOnlyError{Method: "ListVersions", Path: path}
}

// Rollback refuses: see GetVersion.
func (m *materializingSecrets) Rollback(_ context.Context, path string, _ int) error {
	return &AccountSecretsReadOnlyError{Method: "Rollback", Path: path}
}

// GetBackendType identifies this store in diagnostics.
func (m *materializingSecrets) GetBackendType() string { return "agent-materializing" }

// Close releases the cached envelopes, wiping every secure buffer. A stopped
// daemon holds no credentials.
func (m *materializingSecrets) Close() error {
	m.cache.entries.Range(func(key, _ any) bool {
		m.cache.drop(key)
		return true
	})
	return nil
}

// =============================================================================
// Typed errors
// =============================================================================

// AccountSecretsReadOnlyError reports a credential-store mutation attempted on
// a fleet node daemon. The daemon holds released copies of the tenant's pooled
// credentials; the durable records are the control plane's.
type AccountSecretsReadOnlyError struct {
	// Method is the refused SecretsManager method.
	Method string

	// Path is the secret path the call addressed.
	Path string
}

// Error implements the error interface.
func (e *AccountSecretsReadOnlyError) Error() string {
	return "agent account secrets: " + e.Method +
		" is not available on a fleet node daemon: pooled credentials are owned by the control plane (path " +
		strconv.Quote(e.Path) + ")"
}

// AccountSecretsUnsupportedPathError reports a secret path this store does not
// address. It refuses rather than guessing: a misparsed path would read one
// account's credential under another account's name.
type AccountSecretsUnsupportedPathError struct {
	// Path is the unrecognized path.
	Path string
}

// Error implements the error interface.
func (e *AccountSecretsUnsupportedPathError) Error() string {
	return "agent account secrets: path " + strconv.Quote(e.Path) +
		" is not a pooled account credential path"
}

// Compile-time proof the daemon's credential store satisfies the interface
// orchestration.AccountSecretStore composes over.
var _ interfaces.SecretsManager = (*materializingSecrets)(nil)
