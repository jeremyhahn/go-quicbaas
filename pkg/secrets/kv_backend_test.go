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
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// inMemKV is a simple in-memory KVStoreWriter for testing.
type inMemKV struct {
	mu   sync.RWMutex
	data map[string][]byte
}

func newInMemKV() *inMemKV {
	return &inMemKV{data: make(map[string][]byte)}
}

func (k *inMemKV) Put(_ context.Context, key string, value []byte) error {
	k.mu.Lock()
	k.data[key] = value
	k.mu.Unlock()
	return nil
}

func (k *inMemKV) Get(_ context.Context, key string) ([]byte, error) {
	k.mu.RLock()
	v := k.data[key]
	k.mu.RUnlock()
	return v, nil
}

func (k *inMemKV) Delete(_ context.Context, key string) error {
	k.mu.Lock()
	delete(k.data, key)
	k.mu.Unlock()
	return nil
}

// PutIfAbsent and CompareAndDelete give the test store the REAL guarded
// semantics a qrdb engine arbitrates, so every default-fixture test runs
// the backend's guarded paths; the capability-absent fallback is exercised
// through plainKV, which deliberately hides these.
func (k *inMemKV) PutIfAbsent(_ context.Context, key string, value []byte) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	if _, exists := k.data[key]; exists {
		return false, nil
	}
	cp := make([]byte, len(value))
	copy(cp, value)
	k.data[key] = cp
	return true, nil
}

func (k *inMemKV) CompareAndDelete(_ context.Context, key string, expected []byte) (bool, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	current, ok := k.data[key]
	if !ok || !bytes.Equal(current, expected) {
		return false, nil
	}
	delete(k.data, key)
	return true, nil
}

// plainKV forwards ONLY the base KVStoreWriter verbs — explicitly, never by
// embedding, so the guarded verbs cannot be promoted by accident. It is the
// capability-ABSENT store the fallback paths are proven against.
type plainKV struct {
	base *inMemKV
}

func (p *plainKV) Put(ctx context.Context, key string, value []byte) error {
	return p.base.Put(ctx, key, value)
}
func (p *plainKV) Get(ctx context.Context, key string) ([]byte, error) {
	return p.base.Get(ctx, key)
}
func (p *plainKV) Delete(ctx context.Context, key string) error {
	return p.base.Delete(ctx, key)
}
func (p *plainKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	return p.base.Scan(ctx, prefix, fn)
}
func (p *plainKV) List(ctx context.Context, prefix string) ([]string, error) {
	return p.base.List(ctx, prefix)
}

func (k *inMemKV) Scan(_ context.Context, prefix string, fn func(key string, value []byte) error) error {
	k.mu.RLock()
	defer k.mu.RUnlock()
	for key, val := range k.data {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			if err := fn(key, val); err != nil {
				return err
			}
		}
	}
	return nil
}

func (k *inMemKV) List(_ context.Context, prefix string) ([]string, error) {
	k.mu.RLock()
	defer k.mu.RUnlock()
	var keys []string
	for key := range k.data {
		if len(key) >= len(prefix) && key[:len(prefix)] == prefix {
			keys = append(keys, key)
		}
	}
	return keys, nil
}

func TestKVBackend_StoreAndGet(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	data := map[string][]byte{"key": []byte("value")}
	meta := map[string]string{"env": "test"}

	require.NoError(t, b.Store(ctx, "myapp/db-password", data, meta))

	gotData, gotMeta, err := b.Get(ctx, "myapp/db-password")
	require.NoError(t, err)
	assert.Equal(t, data, gotData)
	assert.Equal(t, meta, gotMeta)
}

func TestKVBackend_StoreVersioning(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	v1 := map[string][]byte{"pass": []byte("v1")}
	v2 := map[string][]byte{"pass": []byte("v2")}

	require.NoError(t, b.Store(ctx, "secret/pass", v1, nil))
	require.NoError(t, b.Store(ctx, "secret/pass", v2, nil))

	// Current should be v2
	cur, _, err := b.Get(ctx, "secret/pass")
	require.NoError(t, err)
	assert.Equal(t, v2, cur)

	// Version 1 should still be readable
	old, _, err := b.GetVersion(ctx, "secret/pass", 1)
	require.NoError(t, err)
	assert.Equal(t, v1, old)
}

func TestKVBackend_Delete(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	require.NoError(t, b.Store(ctx, "to/delete", map[string][]byte{"x": []byte("1")}, nil))
	require.NoError(t, b.Delete(ctx, "to/delete"))

	_, _, err := b.Get(ctx, "to/delete")
	assert.ErrorIs(t, err, ErrSecretDeleted)
}

func TestKVBackend_Rollback(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	v1 := map[string][]byte{"pass": []byte("original")}
	v2 := map[string][]byte{"pass": []byte("changed")}

	require.NoError(t, b.Store(ctx, "secret/x", v1, nil))
	require.NoError(t, b.Store(ctx, "secret/x", v2, nil))
	require.NoError(t, b.Rollback(ctx, "secret/x", 1))

	cur, _, err := b.Get(ctx, "secret/x")
	require.NoError(t, err)
	assert.Equal(t, v1, cur, "rollback should restore v1 as a new version")
}

func TestKVBackend_Destroy(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	require.NoError(t, b.Store(ctx, "sec/key", map[string][]byte{"k": []byte("v")}, nil))
	require.NoError(t, b.Destroy(ctx, "sec/key"))

	_, _, err := b.Get(ctx, "sec/key")
	assert.ErrorIs(t, err, ErrSecretNotFound)
}

func TestKVBackend_ListVersions(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	for i := 0; i < 3; i++ {
		require.NoError(t, b.Store(ctx, "multi/ver", map[string][]byte{"n": []byte("x")}, nil))
	}

	versions, err := b.ListVersions(ctx, "multi/ver")
	require.NoError(t, err)
	assert.Len(t, versions, 3)
	for i, v := range versions {
		assert.Equal(t, i+1, v.Version)
	}
}

func TestKVBackend_List(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	require.NoError(t, b.Store(ctx, "app/secret1", map[string][]byte{"k": []byte("1")}, nil))
	require.NoError(t, b.Store(ctx, "app/secret2", map[string][]byte{"k": []byte("2")}, nil))
	require.NoError(t, b.Store(ctx, "other/secret3", map[string][]byte{"k": []byte("3")}, nil))

	paths, err := b.List(ctx, "app/")
	require.NoError(t, err)
	assert.Len(t, paths, 2)
}

func TestKVBackend_Type(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	assert.Equal(t, "internal", b.Type())
}

func TestKVBackend_Close(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	assert.NoError(t, b.Close())
}

func TestKVBackend_ErrCases(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()

	// Empty path
	assert.ErrorIs(t, b.Store(ctx, "", map[string][]byte{"k": []byte("v")}, nil), ErrInvalidPath)
	_, _, err := b.Get(ctx, "")
	assert.ErrorIs(t, err, ErrInvalidPath)

	// Empty data
	assert.ErrorIs(t, b.Store(ctx, "a/b", map[string][]byte{}, nil), ErrEmptyData)

	// Not found
	_, _, err = b.Get(ctx, "nonexistent/path")
	assert.ErrorIs(t, err, ErrSecretNotFound)

	// Version must be positive
	_, _, err = b.GetVersion(ctx, "a/b", 0)
	assert.ErrorIs(t, err, ErrVersionMustBePositive)

	// Delete nonexistent
	assert.ErrorIs(t, b.Delete(ctx, "nonexistent/key"), ErrSecretNotFound)

	// Destroy nonexistent
	assert.ErrorIs(t, b.Destroy(ctx, "nonexistent/key"), ErrSecretNotFound)

	// Rollback nonexistent
	assert.ErrorIs(t, b.Rollback(ctx, "nonexistent/key", 1), ErrSecretNotFound)
}

// TestKVBackend_OverTheCanonicalStore drives the backend over kvstore's
// contract — a missing key is a canonical not-found ERROR, never (nil, nil).
// The production sdkKVStore answers the same way, so this pairing is the
// deployed one: before the classification fix the very first Store on a
// fresh path failed with "load current pointer: kv.get: not_found", which
// meant the daemon's persistent secrets backend could not store a new secret
// at all.
func TestKVBackend_OverTheCanonicalStore(t *testing.T) {
	ctx := context.Background()
	b := NewKVBackend(kvstore.NewMemory())

	require.NoError(t, b.Store(ctx, "canon/path", map[string][]byte{"k": []byte("v1")}, nil),
		"a fresh path must accept its first version")
	require.NoError(t, b.Store(ctx, "canon/path", map[string][]byte{"k": []byte("v2")}, nil))

	data, _, err := b.Get(ctx, "canon/path")
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), data["k"])

	versions, err := b.ListVersions(ctx, "canon/path")
	require.NoError(t, err)
	assert.Len(t, versions, 2)

	require.NoError(t, b.Rollback(ctx, "canon/path", 1))
	data, _, err = b.Get(ctx, "canon/path")
	require.NoError(t, err)
	assert.Equal(t, []byte("v1"), data["k"])

	// An absent VERSION is absence, not a backend failure.
	_, _, err = b.GetVersion(ctx, "canon/path", 99)
	assert.ErrorIs(t, err, ErrVersionNotFound)

	// An absent PATH is absence.
	_, err = b.ListVersions(ctx, "canon/nowhere")
	assert.ErrorIs(t, err, ErrSecretNotFound)
}

// TestKVBackend_OutageIsNotAbsence is #109's property at this seam: a store
// failure that is NOT the canonical not-found must surface as a failure.
// Collapsing it made ListVersions silently SKIP versions and Rollback report
// its target as nonexistent — rotation's rollback target vanishing under a
// transient outage.
func TestKVBackend_OutageIsNotAbsence(t *testing.T) {
	ctx := context.Background()

	seeded := newInMemKV()
	b := NewKVBackend(seeded)
	require.NoError(t, b.Store(ctx, "canon/path", map[string][]byte{"k": []byte("v1")}, nil))
	require.NoError(t, b.Store(ctx, "canon/path", map[string][]byte{"k": []byte("v2")}, nil))

	outage := errors.New("raft: leadership lost")
	failing := &outageKV{inner: seeded, err: outage, failVersionReads: true}
	broken := NewKVBackend(failing)

	_, err := broken.ListVersions(ctx, "canon/path")
	require.Error(t, err, "an outage during version reads was silently skipped")
	assert.ErrorIs(t, err, outage)

	err = broken.Rollback(ctx, "canon/path", 1)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrVersionNotFound,
		"an outage was reported as the rollback target not existing")
	assert.ErrorIs(t, err, outage)

	_, _, err = broken.GetVersion(ctx, "canon/path", 1)
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrVersionNotFound)
	assert.ErrorIs(t, err, outage)
}

// outageKV fails version-entry reads while letting the current-pointer read
// through, so classification is observable per site.
type outageKV struct {
	inner            *inMemKV
	err              error
	failVersionReads bool
}

func (o *outageKV) Put(ctx context.Context, key string, value []byte) error {
	return o.inner.Put(ctx, key, value)
}

func (o *outageKV) Get(ctx context.Context, key string) ([]byte, error) {
	if o.failVersionReads && strings.Contains(key, "/v/") {
		return nil, o.err
	}
	return o.inner.Get(ctx, key)
}

func (o *outageKV) Delete(ctx context.Context, key string) error {
	return o.inner.Delete(ctx, key)
}

func (o *outageKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	return o.inner.Scan(ctx, prefix, fn)
}

func (o *outageKV) List(ctx context.Context, prefix string) ([]string, error) {
	return o.inner.List(ctx, prefix)
}

// =============================================================================
// Destroy is pointer-first and resumable (go-quicbaas#102 review 944)
// =============================================================================

// TestKVBackend_DestroyLeavesAbsenceOnPartialFailure: the deletes inside
// Destroy are individual KV operations, so a failure mid-destroy is a state
// readers will see. With the pointer deleted FIRST, that state is clean
// ABSENCE — never a pointer to a version that no longer exists — and a
// re-entered Destroy finishes the garbage instead of failing on the missing
// pointer.
func TestKVBackend_DestroyLeavesAbsenceOnPartialFailure(t *testing.T) {
	kv := newErrKV()
	b := NewKVBackend(kv)
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v")}, nil))

	// The version delete fails; the pointer delete has already happened.
	kv.deleteErr = errInjected
	kv.deleteErrOn = "/v/"
	require.Error(t, b.Destroy(ctx, "app/secret"))

	// A reader racing the partial destroy sees absence, not a pointer into
	// deleted versions.
	_, _, err := b.Get(ctx, "app/secret")
	assert.ErrorIs(t, err, ErrSecretNotFound,
		"a partially destroyed path must read as absent, never as incoherent")

	// Re-entry finishes the leftovers.
	kv.deleteErr = nil
	require.NoError(t, b.Destroy(ctx, "app/secret"))
	_, err = b.ListVersions(ctx, "app/secret")
	assert.ErrorIs(t, err, ErrSecretNotFound)

	// And only a path with nothing at all left is NotFound.
	assert.ErrorIs(t, b.Destroy(ctx, "app/secret"), ErrSecretNotFound)
}

// TestKVBackend_DestroyResumesAfterPointerCrash: the crash-shaped state —
// pointer gone, versions orphaned — must be finishable. The version sweep
// enumerates by scan, because the pointer's count died with the pointer.
func TestKVBackend_DestroyResumesAfterPointerCrash(t *testing.T) {
	kv := newInMemKV()
	b := NewKVBackend(kv)
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v1")}, nil))
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v2")}, nil))

	// The crash: pointer deleted, versions left behind.
	require.NoError(t, kv.Delete(ctx, currentKey("app/secret")))

	require.NoError(t, b.Destroy(ctx, "app/secret"),
		"a re-entered destroy must finish the orphaned versions, not fail on the absent pointer")
	for v := 1; v <= 2; v++ {
		raw, err := kv.Get(ctx, versionKey("app/secret", v))
		if err == nil && raw != nil {
			t.Fatalf("version %d survived the resumed destroy", v)
		}
	}
	assert.ErrorIs(t, b.Destroy(ctx, "app/secret"), ErrSecretNotFound)
}

// TestKVBackend_DestroySparesSiblingPaths: the version sweep scans a prefix
// a SIBLING secret's whole keyspace can live under ("<path>/v/..."), so only
// bare-number keys — this secret's versions — may be deleted.
func TestKVBackend_DestroySparesSiblingPaths(t *testing.T) {
	b := NewKVBackend(newInMemKV())
	ctx := context.Background()
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("mine")}, nil))
	// The pathological sibling: its OWN path extends "<path>/v".
	require.NoError(t, b.Store(ctx, "app/secret/v", map[string][]byte{"k": []byte("sibling")}, nil))

	require.NoError(t, b.Destroy(ctx, "app/secret"))

	data, _, err := b.Get(ctx, "app/secret/v")
	require.NoError(t, err, "destroying app/secret took its sibling's records with it")
	assert.Equal(t, []byte("sibling"), data["k"])
}

// hookedKV lets a test interleave a concurrent writer at the exact moment a
// key is deleted — the seam the two-backend destroy race needs.
// hookedKV exposes ONLY the base verbs (explicit forwards, no embedding —
// embedding *inMemKV would silently PROMOTE its guarded verbs and change
// which code path the backend takes; measured on this very file). The
// backend therefore treats it as a capability-ABSENT store, which is what
// the stripe-serialization and GC-bound tests are about.
type hookedKV struct {
	base     *inMemKV
	onDelete func(key string)

	// onScan fires once before the underlying scan — the window between a
	// destroy's pointer observation and its pointer removal, where a
	// concurrent writer's publish lands.
	onScan func()
}

func (h *hookedKV) Put(ctx context.Context, key string, value []byte) error {
	return h.base.Put(ctx, key, value)
}
func (h *hookedKV) Get(ctx context.Context, key string) ([]byte, error) {
	return h.base.Get(ctx, key)
}
func (h *hookedKV) List(ctx context.Context, prefix string) ([]string, error) {
	return h.base.List(ctx, prefix)
}

// guardedHookedKV adds the guarded verbs by explicit forward — the
// capability-PRESENT hooked store the storage-arbitration tests drive.
type guardedHookedKV struct {
	*hookedKV

	// onWriteKey/onWrite fire once before the first write — EITHER verb —
	// whose key contains onWriteKey: the window between a writer's
	// allocation scan and its version write, where a concurrent writer
	// commits the SAME candidate. Hooked on both verbs so the interleaving
	// exists regardless of which path the code under test takes — a seam
	// tied to the guarded verb alone would vanish under the exact mutant
	// (reservation downgraded to a plain put) it exists to catch.
	onWriteKey string
	onWrite    func()
}

func (g *guardedHookedKV) fireOnWrite(key string) {
	if g.onWrite != nil && g.onWriteKey != "" && strings.Contains(key, g.onWriteKey) {
		hook := g.onWrite
		g.onWrite = nil
		hook()
	}
}

func (g *guardedHookedKV) Put(ctx context.Context, key string, value []byte) error {
	g.fireOnWrite(key)
	return g.base.Put(ctx, key, value)
}

func (g *guardedHookedKV) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	g.fireOnWrite(key)
	return g.base.PutIfAbsent(ctx, key, value)
}
func (g *guardedHookedKV) CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error) {
	return g.base.CompareAndDelete(ctx, key, expected)
}

func (h *hookedKV) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	if h.onScan != nil {
		hook := h.onScan
		h.onScan = nil
		hook()
	}
	return h.base.Scan(ctx, prefix, fn)
}

func (h *hookedKV) Delete(ctx context.Context, key string) error {
	err := h.base.Delete(ctx, key)
	// The hook fires AFTER the delete lands: the interleavings under test
	// are writes that observe the world the delete produced.
	if h.onDelete != nil {
		h.onDelete(key)
	}
	return err
}

// TestKVBackend_TwoBackendsSerializeStoreAgainstDestroy is review 951's
// third finding: the write mutex was per KVBackend instance, so a Store on
// one backend could interleave with another backend's Destroy — after the
// pointer delete, a concurrent Store writes a fresh version 1, and the
// destroy's enumerated sweep then deletes it, leaving the NEW current
// pointer referencing missing data. The lock registry is process-wide now:
// the racing Store waits out the whole destroy and lands afterwards, so the
// end state is always coherent — readable with the new data, never a
// pointer into nothing. Cross-process writers remain unserialized (the port
// has no conditional write; a property of every write path in this backend,
// tracked as go-quicbaas#120).
func TestKVBackend_TwoBackendsSerializeStoreAgainstDestroy(t *testing.T) {
	base := newInMemKV()
	kv := &hookedKV{base: base}
	destroyer := NewKVBackend(kv)
	writer := NewKVBackend(base) // second instance, same underlying store
	ctx := context.Background()

	require.NoError(t, destroyer.Store(ctx, "app/secret", map[string][]byte{"k": []byte("old")}, nil))

	// At the pointer delete — the first delete of the destroy — launch the
	// racing Store on the OTHER backend, and hold the destroy open long
	// enough for an unserialized writer to land inside it. Under the
	// process-wide stripes the writer blocks until the destroy completes,
	// so the hold closes by its cap; a writer landing inside the window is
	// the defect's own signature.
	storeDone := make(chan error, 1)
	var launched bool
	kv.onDelete = func(key string) {
		if launched || !strings.HasSuffix(key, "/current") {
			return
		}
		launched = true
		go func() {
			storeDone <- writer.Store(ctx, "app/secret", map[string][]byte{"k": []byte("new")}, nil)
		}()
		select {
		case <-storeDone:
			// The writer landed INSIDE the destroy window (unserialized):
			// put its result back for the assertion below and let the
			// destroy finish sweeping — over the writer's fresh version.
			storeDone <- nil
		case <-time.After(100 * time.Millisecond):
		}
	}

	require.NoError(t, destroyer.Destroy(ctx, "app/secret"))
	require.NoError(t, <-storeDone, "the racing store must succeed")

	// THE END STATE MUST BE COHERENT: the racing store committed after (or
	// inside) the destroy, so the path either reads back the new data or is
	// cleanly absent — a pointer referencing deleted data is the corruption
	// under test.
	data, _, err := destroyer.Get(ctx, "app/secret")
	switch {
	case err == nil:
		assert.Equal(t, []byte("new"), data["k"], "the surviving record must be the racing store's")
	case errors.Is(err, ErrSecretNotFound):
		// Also coherent: absence. (Not the expected outcome under the
		// serialized ordering, but never corrupt.)
		t.Fatalf("path absent after a store that succeeded: the destroy swept the racing write")
	default:
		t.Fatalf("Get after the race = %v: the pointer references data the destroy deleted", err)
	}
}

// TestKVBackend_DestroyGCIsBoundedToThePreEnumeratedSet is the semantic half
// of review 951's third finding, and it must hold REGARDLESS of locks: a
// writer the stripes cannot see (another process on the shared store) can
// write while a destroy is mid-flight. The GC deletes only the version set
// enumerated BEFORE the pointer delete, so a concurrent writer's versions
// NUMBERED BEYOND that set — the shape a Store that read the pointer just
// before the delete produces — survive with their pointer, and the path
// reads back coherently as the writer's data. (A writer that lands after
// the pointer delete now allocates from the version KEYSPACE — see
// nextVersionNumber — so any surviving evidence pushes its number past the
// enumerated set; what remains cross-node is pointer-write ordering only,
// the go-quicbaas#120 storage-CAS residual documented at the site.)
// The raw-store writes here model the other node: they bypass every lock
// this process holds.
func TestKVBackend_DestroyGCIsBoundedToThePreEnumeratedSet(t *testing.T) {
	base := newInMemKV()
	kv := &hookedKV{base: base}
	b := NewKVBackend(kv)
	ctx := context.Background()

	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("old-1")}, nil))
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("old-2")}, nil))

	// The other node's Store read the pointer (version 2) an instant before
	// this destroy's pointer delete, and its writes land mid-sweep: a fresh
	// version 3 and a pointer naming it, written RAW — no stripe, no
	// backend, exactly what another process's committed Store looks like
	// from here.
	var interleaved bool
	kv.onDelete = func(key string) {
		if interleaved || !strings.HasSuffix(key, "/current") {
			return
		}
		interleaved = true
		require.NoError(t, base.Put(ctx, versionKey("app/secret", 3),
			[]byte(`{"version":3,"data":{"k":"bmV3"},"created_at":"2026-08-23T00:00:00Z"}`)))
		require.NoError(t, base.Put(ctx, currentKey("app/secret"),
			[]byte(`{"version":3}`)))
	}

	require.NoError(t, b.Destroy(ctx, "app/secret"))

	// The writer's record SURVIVED, coherently: pointer and version both
	// present, and a read answers the writer's data rather than an error
	// about swept versions.
	data, _, err := b.Get(ctx, "app/secret")
	require.NoError(t, err,
		"the concurrent writer's version was swept: the GC was not bounded to the pre-enumerated set")
	assert.Equal(t, []byte("new"), data["k"], "the surviving record must be the writer's")
	// The enumerated generation is fully collected.
	for v := 1; v <= 2; v++ {
		if raw, err := base.Get(ctx, versionKey("app/secret", v)); err == nil && raw != nil {
			t.Fatalf("pre-enumerated version %d survived the destroy", v)
		}
	}
}

// =============================================================================
// Version allocation: numbers are NEVER REUSED while evidence of them remains
// (ruled 2026-08-23). Deriving the next version from the pointer alone let a
// writer that observed a destroy's deleted pointer restart at version 1 — a
// number inside that destroy's pre-enumerated delete set, so the acknowledged
// write was swept and the fresh pointer referenced nothing. Allocation now
// derives from the version KEYSPACE joined with the pointer's count; see
// nextVersionNumber.
// =============================================================================

// TestKVBackend_StoreAllocatesPastOrphanedVersions is the ruled fix's core
// property: after a destroy that crashed between the pointer delete and the
// version sweep, a new Store must NOT re-issue the orphans' numbers.
func TestKVBackend_StoreAllocatesPastOrphanedVersions(t *testing.T) {
	kv := newInMemKV()
	b := NewKVBackend(kv)
	ctx := context.Background()

	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("gen1-v1")}, nil))
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("gen1-v2")}, nil))

	// The crashed destroy: pointer deleted first (pointer-first order),
	// version keys 1 and 2 still on disk. A sibling path whose name extends
	// "<path>/v/" must not count as a version of THIS path.
	require.NoError(t, kv.Delete(ctx, currentKey("app/secret")))
	require.NoError(t, kv.Put(ctx, kvBackendKeyPrefix+"app/secret/v/extra/current", []byte(`{"version":7}`)))

	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("gen2")}, nil))

	// The new write took the number PAST the orphans: whatever a resumed
	// sweep of the old generation deletes, it cannot be this record.
	raw, err := kv.Get(ctx, versionKey("app/secret", 3))
	require.NoError(t, err)
	require.NotNil(t, raw, "the post-destroy write must be version 3, past orphans 1 and 2")
	var cur kvCurrentEntry
	require.NoError(t, json.Unmarshal(mustRawGet(t, kv, currentKey("app/secret")), &cur))
	assert.Equal(t, 3, cur.Version, "the pointer must name the keyspace-derived version")
	for v := 1; v <= 2; v++ {
		orphan, err := kv.Get(ctx, versionKey("app/secret", v))
		require.NoError(t, err)
		assert.NotNil(t, orphan, "orphan version %d must be untouched by allocation", v)
	}
}

// TestKVBackend_StoreKeepsThePointerFloor covers the other half of the join:
// legacy data whose version KEYS are gone but whose pointer survives must
// still advance from the pointer's count, not restart below it.
func TestKVBackend_StoreKeepsThePointerFloor(t *testing.T) {
	kv := newInMemKV()
	b := NewKVBackend(kv)
	ctx := context.Background()

	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v1")}, nil))
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v2")}, nil))
	require.NoError(t, kv.Delete(ctx, versionKey("app/secret", 1)))
	require.NoError(t, kv.Delete(ctx, versionKey("app/secret", 2)))

	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v3")}, nil))

	var cur kvCurrentEntry
	require.NoError(t, json.Unmarshal(mustRawGet(t, kv, currentKey("app/secret")), &cur))
	assert.Equal(t, 3, cur.Version,
		"with an empty keyspace the pointer's count is the floor: 2 -> 3, never back to 1")
}

// TestKVBackend_RollbackAllocatesByTheSameRule: Rollback writes the restored
// data AS A NEW VERSION, and that write follows the same never-reuse rule —
// an orphan beyond the pointer (a crashed destroy's residue from an older,
// larger generation) pushes the allocation past itself.
func TestKVBackend_RollbackAllocatesByTheSameRule(t *testing.T) {
	kv := newInMemKV()
	b := NewKVBackend(kv)
	ctx := context.Background()

	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v1")}, nil))
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v2")}, nil))
	// Orphan evidence beyond the pointer: version 9 exists in the keyspace.
	require.NoError(t, kv.Put(ctx, versionKey("app/secret", 9),
		[]byte(`{"path":"app/secret","data":{"k":"b3JwaGFu"},"version":9}`)))

	require.NoError(t, b.Rollback(ctx, "app/secret", 1))

	var cur kvCurrentEntry
	require.NoError(t, json.Unmarshal(mustRawGet(t, kv, currentKey("app/secret")), &cur))
	assert.Equal(t, 10, cur.Version,
		"rollback's new version must clear the whole keyspace (9), not just the pointer (2)")
	data, _, err := b.Get(ctx, "app/secret")
	require.NoError(t, err)
	assert.Equal(t, []byte("v1"), data["k"], "the restored content is version 1's")
}

// TestKVBackend_StoreFailsClosedWhenAllocationCannotEnumerate: an allocation
// that cannot SEE the keyspace must not guess a number — a guess is exactly
// the reuse the rule forbids.
func TestKVBackend_StoreFailsClosedWhenAllocationCannotEnumerate(t *testing.T) {
	base := newInMemKV()
	b := NewKVBackend(&scanErrKV{inMemKV: base, err: errStoreScan})
	ctx := context.Background()

	err := b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v1")}, nil)
	require.Error(t, err)
	require.ErrorIs(t, err, errStoreScan, "the enumeration failure must surface, wrapped")
}

// errStoreScan is the planted enumeration failure.
var errStoreScan = errors.New("kv: scan unavailable")

// scanErrKV fails every Scan, modelling a store whose range reads are down
// while point reads still answer.
type scanErrKV struct {
	*inMemKV
	err error
}

func (s *scanErrKV) Scan(_ context.Context, _ string, _ func(key string, value []byte) error) error {
	return s.err
}

// mustRawGet reads a raw key that must exist.
func mustRawGet(t *testing.T, kv *inMemKV, key string) []byte {
	t.Helper()
	raw, err := kv.Get(context.Background(), key)
	require.NoError(t, err)
	require.NotNil(t, raw, "raw key %q absent", key)
	return raw
}

// TestKVBackend_AllocationRefusesToWrap is review 977's overflow finding: a
// version at the integer ceiling must fail allocation with a typed error,
// never wrap negative — a negative version key is invisible to every scan,
// so the acknowledged write would vanish into a hole in the history.
func TestKVBackend_AllocationRefusesToWrap(t *testing.T) {
	t.Run("CeilingInTheKeyspace", func(t *testing.T) {
		kv := newInMemKV()
		b := NewKVBackend(kv)
		ctx := context.Background()
		require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v1")}, nil))
		require.NoError(t, kv.Put(ctx, versionKey("app/secret", math.MaxInt),
			[]byte(`{"path":"app/secret","data":{"k":"eA=="}}`)))

		err := b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v2")}, nil)
		require.ErrorIs(t, err, ErrVersionSpaceExhausted)
	})

	t.Run("CeilingInThePointer", func(t *testing.T) {
		kv := newInMemKV()
		b := NewKVBackend(kv)
		ctx := context.Background()
		require.NoError(t, kv.Put(ctx, currentKey("app/secret"),
			[]byte(`{"version":9223372036854775807}`)))

		err := b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v")}, nil)
		require.ErrorIs(t, err, ErrVersionSpaceExhausted)
	})
}

// =============================================================================
// Storage-arbitrated pointer writes (go-quicbaas#120, owner-ruled): with a
// guarded store, the cross-process interleavings reviews 957/975/980 named
// resolve cleanly — the acknowledged Store survives, or the Destroy loses
// closed. Without the capability, the documented fallback behaves as before.
// =============================================================================

// TestKVBackend_GuardedDestroyLosesCleanlyToAConcurrentRepublish is review
// 980's exact ordering: a concurrent Store (another process — raw writes,
// beneath every stripe) publishes a fresh version AND its pointer between
// Destroy's observation and its pointer delete. The conditional delete
// presents the OBSERVED generation, the store refuses it, and the destroy
// fails CLOSED with nothing deleted — the acknowledged write survives and
// reads back.
func TestKVBackend_GuardedDestroyLosesCleanlyToAConcurrentRepublish(t *testing.T) {
	base := newInMemKV()
	kv := &guardedHookedKV{hookedKV: &hookedKV{base: base}}
	b := NewKVBackend(kv)
	ctx := context.Background()

	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("old")}, nil))

	// The concurrent writer lands INSIDE destroy's window: after the pointer
	// observation (loadCurrentRaw), before the conditional delete — the scan
	// hook is exactly that window.
	kv.onScan = func() {
		require.NoError(t, base.Put(ctx, versionKey("app/secret", 2),
			[]byte(`{"path":"app/secret","data":{"k":"bmV3"},"version":2}`)))
		require.NoError(t, base.Put(ctx, currentKey("app/secret"),
			[]byte(`{"version":2}`)))
	}

	err := b.Destroy(ctx, "app/secret")
	require.ErrorIs(t, err, ErrWriteConflict,
		"the re-published pointer must refuse the destroy, not be deleted by it")

	// The acknowledged write SURVIVES, coherently readable.
	data, _, err := b.Get(ctx, "app/secret")
	require.NoError(t, err, "the concurrent writer's publish was destroyed")
	assert.Equal(t, []byte("new"), data["k"])

	// A LATER destroy — re-observing the new world — succeeds.
	require.NoError(t, b.Destroy(ctx, "app/secret"))
	_, _, err = b.Get(ctx, "app/secret")
	require.ErrorIs(t, err, ErrSecretNotFound)
}

// TestKVBackend_GuardedFirstPublishRefusesTheRaceLoser: two writers race a
// path's FIRST publish; the loser's PutIfAbsent is refused by the store and
// Store fails closed with ErrWriteConflict — never two acknowledgements onto
// one pointer.
func TestKVBackend_GuardedFirstPublishRefusesTheRaceLoser(t *testing.T) {
	base := newInMemKV()
	kv := &guardedHookedKV{hookedKV: &hookedKV{base: base}}
	b := NewKVBackend(kv)
	ctx := context.Background()

	// The other writer's first publish lands during our allocation scan —
	// after our absent observation, before our pointer write.
	kv.onScan = func() {
		require.NoError(t, base.Put(ctx, versionKey("app/secret", 7),
			[]byte(`{"path":"app/secret","data":{"k":"d2lubmVy"},"version":7}`)))
		require.NoError(t, base.Put(ctx, currentKey("app/secret"),
			[]byte(`{"version":7}`)))
	}

	err := b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("loser")}, nil)
	require.ErrorIs(t, err, ErrWriteConflict,
		"the racing first publish must fail closed, not overwrite the winner's pointer")

	data, _, err := b.Get(ctx, "app/secret")
	require.NoError(t, err)
	assert.Equal(t, []byte("winner"), data["k"], "the winner's acknowledged write must stand")

	// THE LOSER LEFT NOTHING BEHIND (review 995): its reserved version
	// entry — credential material — was unreserved on the failed
	// publication, not stranded durable for a later sweep to find. The
	// WHOLE keyspace is enumerated rather than a guessed version range: a
	// range derived from the test author's mental replay of allocation is
	// a yardstick that moves with the code under test (measured — the
	// first version of this check scanned v2..v3 while the loser strands
	// at v8, and the unwind-dropped mutant walked straight past it).
	require.NoError(t, base.Scan(ctx, kvBackendKeyPrefix+"app/secret/v/", func(key string, raw []byte) error {
		var entry kvEntry
		if json.Unmarshal(raw, &entry) == nil {
			require.NotEqual(t, []byte("loser"), entry.Data["k"],
				"the losing writer's reserved entry at %s survived its failed publication", key)
		}
		return nil
	}))
}

// TestKVBackend_FallbackWithoutGuardedVerbsStillDestroysAndStores pins the
// capability-absent path: a store exposing only the base verbs keeps
// today's unconditional behavior (with the residual stated on the type's
// concurrency contract), rather than failing or silently emulating.
func TestKVBackend_FallbackWithoutGuardedVerbsStillDestroysAndStores(t *testing.T) {
	b := NewKVBackend(&plainKV{base: newInMemKV()})
	ctx := context.Background()

	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v1")}, nil))
	require.NoError(t, b.Store(ctx, "app/secret", map[string][]byte{"k": []byte("v2")}, nil))
	data, _, err := b.Get(ctx, "app/secret")
	require.NoError(t, err)
	assert.Equal(t, []byte("v2"), data["k"])
	require.NoError(t, b.Destroy(ctx, "app/secret"))
	_, _, err = b.Get(ctx, "app/secret")
	require.ErrorIs(t, err, ErrSecretNotFound)
}

// TestKVBackend_GuardedVersionReservationKeepsBothWriters is review 994's
// fresh-path collision: two processes both observe an empty path and both
// choose version 1. The loser's reservation is REFUSED (PutIfAbsent on the
// version key), it re-allocates past the winner, and the pointer race then
// resolves at the pointer's own guard — the winner's acknowledged bytes are
// never overwritten and are exactly what a read serves.
func TestKVBackend_GuardedVersionReservationKeepsBothWriters(t *testing.T) {
	base := newInMemKV()
	kv := &guardedHookedKV{hookedKV: &hookedKV{base: base}}
	a := NewKVBackend(kv)
	ctx := context.Background()

	// Writer B (another process, raw) commits its full first Store — version
	// 1 and the pointer — in the exact window review 994 constructed: AFTER
	// A's allocation scan chose the same candidate, BEFORE A's reservation
	// write. Both writers hold candidate 1 at this instant.
	kv.onWriteKey = "/v/1"
	kv.onWrite = func() {
		require.NoError(t, base.Put(ctx, versionKey("app/secret", 1),
			[]byte(`{"path":"app/secret","data":{"k":"Yi1kYXRh"},"version":1}`)))
		require.NoError(t, base.Put(ctx, currentKey("app/secret"),
			[]byte(`{"version":1}`)))
	}

	err := a.Store(ctx, "app/secret", map[string][]byte{"k": []byte("a-data")}, nil)
	require.ErrorIs(t, err, ErrWriteConflict,
		"A's first publish must lose the pointer race closed, never overwrite")

	// B's acknowledged bytes are intact AND are what the path serves — the
	// exact loss 994 constructed was A's re-used version key overwriting
	// B's data while B's pointer still named it.
	data, _, err := a.Get(ctx, "app/secret")
	require.NoError(t, err)
	assert.Equal(t, []byte("b-data"), data["k"],
		"the winner's acknowledged version was overwritten by the loser's allocation")
	// The colliding version key itself still holds B's bytes.
	raw := mustRawGet(t, base, versionKey("app/secret", 1))
	var entry kvEntry
	require.NoError(t, json.Unmarshal(raw, &entry))
	assert.Equal(t, []byte("b-data"), entry.Data["k"],
		"version 1 was clobbered despite the reservation guard")
}
