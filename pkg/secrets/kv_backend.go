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

package secrets

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/id"
)

// kvEntry is the on-disk representation of a single secret version.
type kvEntry struct {
	Path      string            `json:"path"`
	Data      map[string][]byte `json:"data"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	Version   int               `json:"version"`
	Deleted   bool              `json:"deleted"`
	Destroyed bool              `json:"destroyed"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
	DeletedAt *time.Time        `json:"deleted_at,omitempty"`
	// CreatedBy is the authenticated principal that minted this version.
	CreatedBy string `json:"created_by,omitempty"`
}

// kvCurrentEntry tracks the current version and soft-delete state of a path.
type kvCurrentEntry struct {
	Version   int        `json:"version"`
	Deleted   bool       `json:"deleted"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

// KVStoreWriter is the subset of the qrdb KVStore interface needed by KVBackend.
// This avoids importing the kvstore package directly from the secrets package.
// A missing key is reported either as (nil, nil) or as an error satisfying
// pkg/errors.IsNotFound — the backend treats both as ABSENCE. Every other
// error is a store failure and is never read as absence.
type KVStoreWriter interface {
	Put(ctx context.Context, key string, value []byte) error
	Get(ctx context.Context, key string) ([]byte, error)
	Delete(ctx context.Context, key string) error
	Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error
	List(ctx context.Context, prefix string) ([]string, error)
}

// guardedKVStore is the OPTIONAL storage-arbitration capability
// (go-quicbaas#120, owner-ruled 2026-08-24): stores whose guarded verbs
// evaluate their condition INSIDE the storage engine — across every process
// sharing the store — let the backend make its pointer writes conditional.
// Discovered by type assertion on the injected KVStoreWriter, the
// established capability pattern; a wrapper that wants to carry the
// capability must FORWARD these verbs explicitly to a store that truly
// arbitrates them — emulating them wrapper-side (check-then-act over the
// base verbs) would claim an atomicity the store does not provide.
type guardedKVStore interface {
	// PutIfAbsent stores value only when key is absent, atomically.
	PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error)
	// CompareAndDelete removes key only while it still holds expected.
	CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error)
}

const (
	// kvBackendKeyPrefix is the root prefix for all secrets in the KV store.
	kvBackendKeyPrefix = "_qb/secrets/"

	// kvBackendType is the type identifier for this backend.
	kvBackendType = "internal"
)

// KVBackend implements secrets.Backend over a qrdb KVStore. Secrets are
// stored as versioned JSON entries so that version history, soft-delete,
// and rollback all work without an external database.
//
// CONCURRENCY CONTRACT (stated, not assumed — reviews 980/#120 ruling):
// within one process, process-global path-striped locks serialize every
// write path. ACROSS processes the backend discovers the store's guarded
// verbs (guardedKVStore) and arbitrates the decisive pointer writes at the
// storage layer: Destroy's pointer delete is conditional on the exact
// observed generation (a re-published pointer refuses the destroy and the
// acknowledged write survives), and a path's FIRST publish is
// conditional-on-absent (two racing first Stores cannot both be
// acknowledged onto one pointer). The stated remainder, tracked on
// go-quicbaas#120: a pointer UPDATE between two concurrent Stores is
// last-write-wins (no compare-and-swap verb exists; both versions persist
// under never-reused numbers and the pointer names one acknowledged
// write), and the soft-delete pointer write shares that shape. On a store
// WITHOUT the guarded verbs, deploy exactly one writer process per shared
// secrets store.
//
// Key layout:
//
//	_qb/secrets/<path>/current   → kvCurrentEntry (current version pointer)
//	_qb/secrets/<path>/v/<N>     → kvEntry (version N data)
type KVBackend struct {
	store KVStoreWriter
}

// kvWriteLocks serialises writes per secret path, PROCESS-WIDE. Package-level
// rather than a field, because a per-instance mutex only coordinates callers
// holding the same *KVBackend: two backends over one shared store held
// disjoint locks, so a Store racing a Destroy could interleave with its
// version/pointer sequence and leave a current pointer referencing deleted
// data. One process, one registry — every KVBackend instance contends on the
// same stripes, keyed by path. IN-PROCESS ARBITRATION IS COMPLETE HERE:
// process-global stripes cover all four write paths. CROSS-NODE arbitration
// for the secrets plane has never existed on any write path — the
// KVStoreWriter port carries no conditional write, and clustered quicbaasd
// deployments using this persistent backend are exposed today independent
// of the SDLC module — and is tracked as go-quicbaas#120 (storage-level write
// arbitration; guarded-verb CAS, tombstones and leases are the open design
// space there).
var kvWriteLocks [32]sync.Mutex

// lockKVPath returns the process-wide stripe lock for a secret path. Striped:
// two paths may share a stripe and serialise harmlessly; one path never spans
// two stripes.
func lockKVPath(path string) *sync.Mutex {
	return &kvWriteLocks[id.HashString(path)%uint64(len(kvWriteLocks))]
}

// nextVersionNumber allocates the next version for a path: one past the
// highest version visible ANYWHERE — the version keyspace (orphans from a
// crashed destroy included) joined with the pointer's count (the floor for
// legacy data whose keys may already be gone).
//
// THE ALLOCATION RULE IS THE CONCURRENCY FIX (ruled 2026-08-23): version
// numbers are NEVER REUSED while any evidence of them remains. Deriving from
// the pointer alone let a writer that observed a destroy's deleted pointer
// restart at version 1 — a number sitting in that destroy's pre-enumerated
// delete set, so the acknowledged write was swept and the fresh pointer
// referenced nothing. Keyspace-derived allocation makes the enumerated set
// and any concurrent acknowledged write DISJOINT BY CONSTRUCTION, across
// processes, with no lock: whatever survives to be scanned pushes the next
// number past it. The stripes still order writers in-process; what remains
// cross-node is pointer-write ordering only (go-quicbaas#120).
func (b *KVBackend) nextVersionNumber(ctx context.Context, path string, pointerCount int) (int, error) {
	highest := pointerCount
	prefix := kvBackendKeyPrefix + path + "/v/"
	if err := b.store.Scan(ctx, prefix, func(key string, _ []byte) error {
		rest := strings.TrimPrefix(key, prefix)
		if rest == "" || strings.ContainsFunc(rest, func(r rune) bool { return r < '0' || r > '9' }) {
			return nil
		}
		if n, convErr := strconv.Atoi(rest); convErr == nil && n > highest {
			highest = n
		}
		return nil
	}); err != nil {
		return 0, &SecretsCauseError{Message: "allocate version: enumerate keyspace", Cause: err}
	}
	// REFUSE TO WRAP (review 977): highest+1 past the integer ceiling goes
	// negative, and a negative version key is invisible to every scan —
	// the write would be acknowledged into a hole in the history.
	if highest == math.MaxInt {
		return 0, ErrVersionSpaceExhausted
	}
	return highest + 1, nil
}

// NewKVBackend creates a KVBackend backed by the given KVStoreWriter.
func NewKVBackend(store KVStoreWriter) *KVBackend {
	return &KVBackend{store: store}
}

// Store saves a secret at the specified path, creating a new version.
func (b *KVBackend) Store(ctx context.Context, path string, data map[string][]byte, metadata map[string]string) error {
	if path == "" {
		return ErrInvalidPath
	}
	if len(data) == 0 {
		return ErrEmptyData
	}

	lock := lockKVPath(path)
	lock.Lock()
	defer lock.Unlock()

	cur, err := b.loadCurrent(ctx, path)
	if err != nil {
		return err
	}
	observed := cur.Version

	now := time.Now()
	createdBy := auth.PrincipalFromContext(ctx)
	nextVer, err := b.allocateAndReserve(ctx, path, cur.Version, func(version int) *kvEntry {
		return &kvEntry{
			Path:      path,
			Data:      data,
			Metadata:  metadata,
			Version:   version,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: createdBy,
		}
	})
	if err != nil {
		return err
	}

	// Update the current pointer — and UNRESERVE ON FAILURE (review 995):
	// a publication the pointer guard refuses (or a failed write) must not
	// strand the reserved version entry, which holds credential material a
	// resumable destroy would otherwise be the only thing to collect. The
	// unwind is best-effort-with-record: a failed delete returns BOTH
	// errors, and the entry's identity is in them for the operator.
	cur.Version = nextVer
	cur.Deleted = false
	cur.DeletedAt = nil
	if err := b.writeCurrent(ctx, path, cur, observed); err != nil {
		if delErr := b.store.Delete(ctx, versionKey(path, nextVer)); delErr != nil && !qberrors.IsNotFound(delErr) {
			return errors.Join(err, &SecretsCauseError{
				Message: "unreserve version " + strconv.Itoa(nextVer) + " after failed publication", Cause: delErr})
		}
		return err
	}
	return nil
}

// allocateAndReserve allocates the next version number and reserves its key,
// retrying past concurrent winners on a guarded store (review 994): each
// refused reservation means another process took the number after our scan,
// so the scan runs again — the winner's surviving key pushes the next
// allocation past it. Bounded; exhaustion fails closed with ErrWriteConflict
// rather than guessing.
func (b *KVBackend) allocateAndReserve(ctx context.Context, path string, pointerCount int, build func(version int) *kvEntry) (int, error) {
	for attempt := 0; attempt < versionReserveAttempts; attempt++ {
		nextVer, err := b.nextVersionNumber(ctx, path, pointerCount)
		if err != nil {
			return 0, err
		}
		taken, err := b.reserveEntry(ctx, path, nextVer, build(nextVer))
		if err != nil {
			return 0, err
		}
		if !taken {
			return nextVer, nil
		}
	}
	return 0, &SecretsSentinelCauseError{Sentinel: ErrWriteConflict,
		Cause: errors.New("version allocation lost every reservation race")}
}

// versionReserveAttempts bounds allocateAndReserve: each retry means a
// concurrent writer won a number after our scan, and more than a few in one
// call is pathological contention better answered with the conflict error.
const versionReserveAttempts = 4

// Get retrieves the current version of a secret.
func (b *KVBackend) Get(ctx context.Context, path string) (map[string][]byte, map[string]string, error) {
	if path == "" {
		return nil, nil, ErrInvalidPath
	}
	cur, err := b.loadCurrent(ctx, path)
	if err != nil {
		return nil, nil, err
	}
	if cur.Version == 0 {
		return nil, nil, ErrSecretNotFound
	}
	if cur.Deleted {
		return nil, nil, ErrSecretDeleted
	}
	return b.getVersion(ctx, path, cur.Version)
}

// GetVersion retrieves a specific version of a secret.
func (b *KVBackend) GetVersion(ctx context.Context, path string, version int) (map[string][]byte, map[string]string, error) {
	if path == "" {
		return nil, nil, ErrInvalidPath
	}
	if version <= 0 {
		return nil, nil, ErrVersionMustBePositive
	}
	return b.getVersion(ctx, path, version)
}

// List returns all secret paths matching the prefix.
func (b *KVBackend) List(ctx context.Context, prefix string) ([]string, error) {
	fullPrefix := kvBackendKeyPrefix + prefix
	var paths []string
	seen := make(map[string]struct{})
	err := b.store.Scan(ctx, fullPrefix, func(key string, _ []byte) error {
		// Keys are: _qb/secrets/<path>/current or _qb/secrets/<path>/v/<N>
		rel := strings.TrimPrefix(key, kvBackendKeyPrefix)
		// Extract the path (everything before the last slash component that is
		// either "current" or starts with "v/").
		parts := strings.Split(rel, "/")
		if len(parts) < 2 {
			return nil
		}
		var secretPath string
		switch {
		case parts[len(parts)-1] == "current":
			secretPath = strings.Join(parts[:len(parts)-1], "/")
		case len(parts) >= 3 && parts[len(parts)-2] == "v":
			secretPath = strings.Join(parts[:len(parts)-2], "/")
		default:
			return nil
		}
		if _, ok := seen[secretPath]; !ok {
			seen[secretPath] = struct{}{}
			paths = append(paths, secretPath)
		}
		return nil
	})
	return paths, err
}

// Delete soft-deletes a secret (preserves versions).
func (b *KVBackend) Delete(ctx context.Context, path string) error {
	if path == "" {
		return ErrInvalidPath
	}
	lock := lockKVPath(path)
	lock.Lock()
	defer lock.Unlock()

	cur, err := b.loadCurrent(ctx, path)
	if err != nil {
		return err
	}
	if cur.Version == 0 {
		return ErrSecretNotFound
	}
	now := time.Now()
	cur.Deleted = true
	cur.DeletedAt = &now
	return b.writeCurrent(ctx, path, cur, cur.Version)
}

// Destroy permanently removes all versions of a secret.
//
// THE CURRENT POINTER GOES FIRST, and the order is the contract: the deletes
// below are individual KV operations, so a crash mid-destroy is always
// possible. Deleting the pointer first makes ABSENCE truthful from the very
// first delete — a reader racing a partial destroy sees a missing secret,
// never a pointer to a version that no longer exists (which presented as an
// unreadable, unrecoverable path to the one caller that destroys credentials,
// go-quicbaas#102 review 944). Everything after the pointer is idempotent
// GARBAGE COLLECTION: surviving version entries are enumerated by SCAN rather
// than by the pointer's count — after a crash the pointer is gone and its
// count with it — so a re-entered Destroy finds and finishes the leftovers
// instead of failing on the absent pointer, and tolerates entries a previous
// attempt already removed. Only a path with neither pointer nor version
// entries is NotFound.
//
// THE SWEEP IS BOUNDED TO THE PRE-ENUMERATED SET — the scan runs before the
// pointer delete, and only those keys are deleted. That bound is what a
// concurrent writer's survival rests on when locks cannot see it (another
// process on the shared store): a Store that read the pointer just before
// the delete lands its next-numbered version and pointer untouched, and the
// path reads back coherently as that writer's data. The residual is stated
// honestly: a cross-process writer's POINTER landing inside the delete's own
// window is lost (absence, never corruption) — the version-number half of
// the old residual is closed by keyspace-derived allocation
// (nextVersionNumber), which keeps any concurrent write's numbers disjoint
// from the enumerated set. The pointer ordering needs a storage-level
// conditional write to close — go-quicbaas#120,
// which tracks cross-node arbitration for every write path of this backend
// — and are out of scope for a lock this process holds.
func (b *KVBackend) Destroy(ctx context.Context, path string) error {
	if path == "" {
		return ErrInvalidPath
	}
	lock := lockKVPath(path)
	lock.Lock()
	defer lock.Unlock()

	cur, observedRaw, err := b.loadCurrentRaw(ctx, path)
	if err != nil {
		return err
	}

	// Surviving version entries, by scan. The prefix is shared with any
	// SIBLING secret whose own path begins "<path>/v/", so only keys whose
	// remainder is a bare version number are this secret's — deleting on the
	// raw prefix would destroy that sibling's records.
	versionPrefix := kvBackendKeyPrefix + path + "/v/"
	var versionKeys []string
	if scanErr := b.store.Scan(ctx, versionPrefix, func(key string, _ []byte) error {
		rest := strings.TrimPrefix(key, versionPrefix)
		if rest == "" || strings.ContainsFunc(rest, func(r rune) bool { return r < '0' || r > '9' }) {
			return nil
		}
		versionKeys = append(versionKeys, key)
		return nil
	}); scanErr != nil {
		return &SecretsCauseError{Message: "destroy: enumerate versions", Cause: scanErr}
	}

	if cur.Version == 0 && len(versionKeys) == 0 {
		return ErrSecretNotFound
	}

	if cur.Version != 0 {
		// THE POINTER DELETE IS CONDITIONAL ON THE OBSERVED GENERATION where
		// the store can arbitrate it (go-quicbaas#120, owner-ruled
		// 2026-08-24): CompareAndDelete presents the exact raw bytes this
		// destroy READ, so a pointer a concurrent Store re-published — in
		// another process, beneath every stripe — refuses the delete and the
		// acknowledged write SURVIVES. The refusal fails this destroy closed
		// (ErrWriteConflict): nothing was deleted, the next attempt
		// re-observes the world. On a store without the capability the
		// unconditional delete remains, with the residual stated on the
		// type's concurrency contract.
		if g, ok := b.store.(guardedKVStore); ok && len(observedRaw) > 0 {
			deleted, delErr := g.CompareAndDelete(ctx, currentKey(path), observedRaw)
			if delErr != nil {
				return &SecretsCauseError{Message: "destroy: failed to delete current pointer", Cause: delErr}
			}
			if !deleted {
				return &SecretsSentinelCauseError{Sentinel: ErrWriteConflict,
					Cause: errors.New("a concurrent writer re-published the pointer; the destroy was refused")}
			}
		} else if delErr := b.store.Delete(ctx, currentKey(path)); delErr != nil && !qberrors.IsNotFound(delErr) {
			return &SecretsCauseError{Message: "destroy: failed to delete current pointer", Cause: delErr}
		}
	}
	for _, key := range versionKeys {
		if delErr := b.store.Delete(ctx, key); delErr != nil && !qberrors.IsNotFound(delErr) {
			return &SecretsCauseError{Message: "destroy: failed to delete version", Cause: delErr}
		}
	}
	return nil
}

// ListVersions returns metadata for all versions of a secret.
func (b *KVBackend) ListVersions(ctx context.Context, path string) ([]*VersionMetadata, error) {
	if path == "" {
		return nil, ErrInvalidPath
	}
	cur, err := b.loadCurrent(ctx, path)
	if err != nil {
		return nil, err
	}
	if cur.Version == 0 {
		return nil, ErrSecretNotFound
	}
	var versions []*VersionMetadata
	for v := 1; v <= cur.Version; v++ {
		raw, err := b.store.Get(ctx, versionKey(path, v))
		if err != nil {
			if qberrors.IsNotFound(err) {
				continue // destroyed versions leave gaps; absence is ordinary here
			}
			// An OUTAGE is not a gap: skipping it silently hands the caller a
			// truncated history presented as complete — the #109 class, at
			// this seam.
			return nil, &SecretsCauseError{Message: "list version entry", Cause: err}
		}
		if raw == nil {
			continue
		}
		var entry kvEntry
		if err := json.Unmarshal(raw, &entry); err != nil {
			continue
		}
		vm := &VersionMetadata{
			Version:   entry.Version,
			CreatedAt: entry.CreatedAt,
			CreatedBy: entry.CreatedBy,
			Deleted:   entry.Destroyed,
		}
		if entry.DeletedAt != nil {
			vm.DeletedAt = entry.DeletedAt
		}
		versions = append(versions, vm)
	}
	return versions, nil
}

// Rollback restores a previous version as the current version.
func (b *KVBackend) Rollback(ctx context.Context, path string, version int) error {
	if path == "" {
		return ErrInvalidPath
	}
	if version <= 0 {
		return ErrVersionMustBePositive
	}
	lock := lockKVPath(path)
	lock.Lock()
	defer lock.Unlock()

	cur, err := b.loadCurrent(ctx, path)
	if err != nil {
		return err
	}
	if cur.Version == 0 {
		return ErrSecretNotFound
	}
	if version > cur.Version {
		return ErrVersionNotFound
	}

	// Load the target version to get its data. Only ABSENCE is
	// ErrVersionNotFound; an outage must not make the rollback target seem
	// nonexistent — for rotation that read as "nothing to restore" exactly
	// when restoring mattered most.
	raw, err := b.store.Get(ctx, versionKey(path, version))
	if err != nil {
		if qberrors.IsNotFound(err) {
			return ErrVersionNotFound
		}
		return &SecretsCauseError{Message: "load rollback target", Cause: err}
	}
	if raw == nil {
		return ErrVersionNotFound
	}
	var oldEntry kvEntry
	if err := json.Unmarshal(raw, &oldEntry); err != nil {
		return &SecretsSentinelCauseError{Sentinel: ErrDeserializationFailed, Cause: err}
	}
	if oldEntry.Destroyed {
		return ErrVersionDestroyed
	}

	// Write as a new version, allocated by the same never-reuse rule Store
	// follows (see nextVersionNumber), reserved against concurrent winners
	// exactly as Store reserves (review 994).
	now := time.Now()
	createdBy := auth.PrincipalFromContext(ctx)
	nextVer, err := b.allocateAndReserve(ctx, path, cur.Version, func(version int) *kvEntry {
		return &kvEntry{
			Path:      path,
			Data:      oldEntry.Data,
			Metadata:  oldEntry.Metadata,
			Version:   version,
			CreatedAt: now,
			UpdatedAt: now,
			CreatedBy: createdBy,
		}
	})
	if err != nil {
		return err
	}
	cur.Version = nextVer
	cur.Deleted = false
	cur.DeletedAt = nil
	// Rollback demands an existing history, so the pointer write is always
	// an UPDATE (observed non-zero) — never the guarded first publish. The
	// same unreserve-on-failure applies (review 995).
	if err := b.writeCurrent(ctx, path, cur, 1); err != nil {
		if delErr := b.store.Delete(ctx, versionKey(path, nextVer)); delErr != nil && !qberrors.IsNotFound(delErr) {
			return errors.Join(err, &SecretsCauseError{
				Message: "unreserve version " + strconv.Itoa(nextVer) + " after failed publication", Cause: delErr})
		}
		return err
	}
	return nil
}

// Type returns the backend type identifier.
func (b *KVBackend) Type() string { return kvBackendType }

// Close is a no-op; the underlying KV store's lifecycle is owned by the caller.
func (b *KVBackend) Close() error { return nil }

// ---------------------------------------------------------------------------
// internal helpers
// ---------------------------------------------------------------------------

func currentKey(path string) string {
	return kvBackendKeyPrefix + path + "/current"
}

func versionKey(path string, version int) string {
	return fmt.Sprintf("%s%s/v/%d", kvBackendKeyPrefix, path, version)
}

func (b *KVBackend) loadCurrent(ctx context.Context, path string) (*kvCurrentEntry, error) {
	cur, _, err := b.loadCurrentRaw(ctx, path)
	return cur, err
}

// loadCurrentRaw additionally returns the pointer's RAW stored bytes — the
// exact value a guarded CompareAndDelete must present to delete only the
// generation it observed. raw is nil when the pointer is absent.
func (b *KVBackend) loadCurrentRaw(ctx context.Context, path string) (*kvCurrentEntry, []byte, error) {
	raw, err := b.store.Get(ctx, currentKey(path))
	if err != nil {
		// A missing current pointer is an ordinary fresh path. The canonical
		// stores (pkg/kvstore, the daemon's sdkKVStore) report it as a
		// not-found ERROR, not (nil, nil); treating that error as fatal meant
		// the persistent secrets backend could not store its FIRST version.
		if qberrors.IsNotFound(err) {
			return &kvCurrentEntry{}, nil, nil
		}
		return nil, nil, &SecretsCauseError{Message: "load current pointer", Cause: err}
	}
	if raw == nil {
		return &kvCurrentEntry{}, nil, nil
	}
	var cur kvCurrentEntry
	if err := json.Unmarshal(raw, &cur); err != nil {
		return nil, nil, &SecretsSentinelCauseError{Sentinel: ErrDeserializationFailed, Cause: err}
	}
	return &cur, raw, nil
}

// writeCurrent publishes the pointer. observedVersion is what the caller's
// read saw before mutating: 0 means the path was ABSENT, and on a guarded
// store the first publish rides PutIfAbsent — two cross-process writers
// racing a first Store cannot both be acknowledged onto one pointer; the
// loser fails closed with ErrWriteConflict and re-derives on retry
// (go-quicbaas#120). A non-zero observedVersion is a pointer UPDATE, which
// the available guarded verbs cannot make conditional (no compare-and-swap);
// two concurrent Stores to one path remain last-write-wins there — the
// stated #120 remainder, distinct from data loss: both versions exist under
// never-reused numbers and the pointer names one of the acknowledged writes.
func (b *KVBackend) writeCurrent(ctx context.Context, path string, cur *kvCurrentEntry, observedVersion int) error {
	raw, err := json.Marshal(cur)
	if err != nil {
		return &SecretsSentinelCauseError{Sentinel: ErrSerializationFailed, Cause: err}
	}
	if g, ok := b.store.(guardedKVStore); ok && observedVersion == 0 {
		stored, err := g.PutIfAbsent(ctx, currentKey(path), raw)
		if err != nil {
			return &SecretsCauseError{Message: "write current pointer", Cause: err}
		}
		if !stored {
			return &SecretsSentinelCauseError{Sentinel: ErrWriteConflict,
				Cause: errors.New("another writer published this path's first pointer")}
		}
		return nil
	}
	if err := b.store.Put(ctx, currentKey(path), raw); err != nil {
		return &SecretsCauseError{Message: "write current pointer", Cause: err}
	}
	return nil
}

// reserveEntry writes a version entry. On a guarded store the key is
// RESERVED via PutIfAbsent (review 994): version numbers derive from a scan,
// so two processes can both allocate N+1 — an unconditional Put would let
// the second overwrite the first's acknowledged bytes while the pointer
// arbitration elects the loser's data. A refused reservation reports
// taken=true and the caller re-allocates past the winner; on an unguarded
// store the unconditional write remains, under the type's stated fallback
// contract (one writer process).
func (b *KVBackend) reserveEntry(ctx context.Context, path string, version int, entry *kvEntry) (taken bool, err error) {
	raw, err := json.Marshal(entry)
	if err != nil {
		return false, &SecretsSentinelCauseError{Sentinel: ErrSerializationFailed, Cause: err}
	}
	if g, ok := b.store.(guardedKVStore); ok {
		stored, err := g.PutIfAbsent(ctx, versionKey(path, version), raw)
		if err != nil {
			return false, &SecretsCauseError{Message: "reserve version entry", Cause: err}
		}
		return !stored, nil
	}
	if err := b.store.Put(ctx, versionKey(path, version), raw); err != nil {
		return false, &SecretsCauseError{Message: "write version entry", Cause: err}
	}
	return false, nil
}

func (b *KVBackend) getVersion(ctx context.Context, path string, version int) (map[string][]byte, map[string]string, error) {
	raw, err := b.store.Get(ctx, versionKey(path, version))
	if err != nil {
		// The inverse misclassification: an absent version reported as a
		// backend failure. Absence is ErrVersionNotFound whichever way the
		// store spells it.
		if qberrors.IsNotFound(err) {
			return nil, nil, ErrVersionNotFound
		}
		return nil, nil, &SecretsCauseError{Message: "get version entry", Cause: err}
	}
	if raw == nil {
		return nil, nil, ErrVersionNotFound
	}
	var entry kvEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, nil, &SecretsSentinelCauseError{Sentinel: ErrDeserializationFailed, Cause: err}
	}
	if entry.Destroyed {
		return nil, nil, ErrVersionDestroyed
	}
	return entry.Data, entry.Metadata, nil
}
