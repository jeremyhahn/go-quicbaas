package kms

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kvstore"
)

// raceRegistry is a key registry whose next Get, PutIfAbsent or Delete of one
// key can be paused or failed, so a test orders a registry step against a
// concurrent rotation deterministically. PutIfAbsent is guarded by mu, as the
// production store's guarded insert is atomic.
type raceRegistry struct {
	interfaces.KVStore
	mu sync.Mutex

	key        string
	pauseGet   bool
	pausePut   bool
	failDelete bool
	paused     chan struct{}
	release    chan struct{}
}

func newRaceRegistry() *raceRegistry {
	return &raceRegistry{KVStore: kvstore.NewMemory()}
}

// armPause makes the next Get (get) or PutIfAbsent (!get) of key pause: a
// Get reads the record first and returns that read once released.
func (r *raceRegistry) armPause(key string, get bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.key = key
	r.pauseGet, r.pausePut = get, !get
	r.paused = make(chan struct{})
	r.release = make(chan struct{})
}

// waitPaused waits for the armed step to pause, failing the test when it
// never does.
func (r *raceRegistry) waitPaused(t *testing.T) {
	t.Helper()
	select {
	case <-r.paused:
	case <-time.After(10 * time.Second):
		t.Fatal("the armed registry step never ran")
	}
}

func (r *raceRegistry) takePause(key string, get bool) (chan struct{}, chan struct{}) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if key != r.key || (get && !r.pauseGet) || (!get && !r.pausePut) {
		return nil, nil
	}
	r.pauseGet, r.pausePut = false, false
	return r.paused, r.release
}

func (r *raceRegistry) Get(ctx context.Context, key string) ([]byte, error) {
	data, err := r.KVStore.Get(ctx, key)
	if paused, release := r.takePause(key, true); paused != nil {
		close(paused)
		<-release
	}
	return data, err
}

func (r *raceRegistry) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if paused, release := r.takePause(key, false); paused != nil {
		close(paused)
		<-release
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if data, err := r.KVStore.Get(ctx, key); err == nil && len(data) > 0 {
		return false, nil
	}
	return true, r.KVStore.Put(ctx, key, value)
}

func (r *raceRegistry) Delete(ctx context.Context, key string) error {
	r.mu.Lock()
	fail := r.failDelete && key == r.key
	if fail {
		r.failDelete = false
	}
	r.mu.Unlock()
	if fail {
		return errors.New("registry unavailable")
	}
	return r.KVStore.Delete(ctx, key)
}

// TestXKMSBackend_StaleVersionRefreshDoesNotRollBackRotation pauses a version
// refresh's durable read across a rotation: the refresh read version 1, the
// rotation recorded version 2, and the refresh must neither restore version 1
// in the cache nor report it.
func TestXKMSBackend_StaleVersionRefreshDoesNotRollBackRotation(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	registry := newRaceRegistry()
	b := newRegistryTestBackend(t, registry)
	createKeyNameTestKey(t, b, "ledger")
	current, err := b.CurrentVersion(ctx, "ledger")
	require.NoError(t, err)
	require.Equal(t, 1, current)

	registry.armPause(b.versionRecordKey("ledger"), true)
	type result struct {
		version int
		err     error
	}
	refreshed := make(chan result, 1)
	go func() {
		v, err := b.CurrentVersion(ctx, "ledger")
		refreshed <- result{v, err}
	}()
	registry.waitPaused(t)

	rotated, err := b.RotateKey(ctx, "ledger")
	require.NoError(t, err)
	require.Equal(t, 2, rotated.Version)

	close(registry.release)
	got := <-refreshed
	require.NoError(t, got.err)
	assert.Equal(t, 2, got.version, "a refresh read before the rotation reports the rotated version")

	meta, err := b.GetKeyMetadata(ctx, "ledger")
	require.NoError(t, err)
	assert.Equal(t, 2, meta.Version, "the stale refresh does not roll the cache back")

	ciphertext, err := b.Encrypt(ctx, "ledger", []byte("after rotation"), nil)
	require.NoError(t, err)
	v, err := CiphertextKeyVersion(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, uint64(2), v, "encryption uses the rotated key")
}

// TestXKMSBackend_MigrationRacingRotationKeepsRotatedRecord pauses node A's
// version-record migration before its insert while node B migrates the same
// key and rotates it to version 2: A's migration must not overwrite B's
// record, and A reads the version that won.
func TestXKMSBackend_MigrationRacingRotationKeepsRotatedRecord(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	// A key created before the registry: it has no version record.
	legacy := newKeyNameTestBackend(t)
	createKeyNameTestKey(t, legacy, "ledger")

	registry := newRaceRegistry()
	nodeA := newRegistryTestBackend(t, registry)
	nodeB := newRegistryTestBackend(t, registry)
	recordKey := nodeA.versionRecordKey("ledger")

	registry.armPause(recordKey, false)
	type result struct {
		version int
		err     error
	}
	migrated := make(chan result, 1)
	go func() {
		v, err := nodeA.CurrentVersion(ctx, "ledger")
		migrated <- result{v, err}
	}()
	registry.waitPaused(t)

	_, err := nodeB.GetKeyMetadata(ctx, "ledger")
	require.NoError(t, err)
	rotated, err := nodeB.RotateKey(ctx, "ledger")
	require.NoError(t, err)
	require.Equal(t, 2, rotated.Version)

	close(registry.release)
	got := <-migrated
	require.NoError(t, got.err)
	assert.Equal(t, 2, got.version, "the lost migration reads the record that won")

	record, err := registry.KVStore.Get(ctx, recordKey)
	require.NoError(t, err)
	assert.Equal(t, "2", string(record), "the migration does not overwrite the rotation's record")

	ciphertext, err := nodeB.Encrypt(ctx, "ledger", []byte("sealed under v2"), nil)
	require.NoError(t, err)
	fresh := newRegistryTestBackend(t, registry)
	plaintext, err := fresh.Decrypt(ctx, "ledger", ciphertext, nil)
	require.NoError(t, err, "a fresh reader accepts version-2 ciphertext")
	assert.Equal(t, []byte("sealed under v2"), plaintext)
}

// TestXKMSBackend_DeleteResumesRegistryCleanupOnRetry fails the registry
// Delete after a rotated key's material is deleted: the cache is dropped all
// the same, a retried DeleteKey completes the registry cleanup, and a key
// recreated under the ID starts from version 1.
func TestXKMSBackend_DeleteResumesRegistryCleanupOnRetry(t *testing.T) {
	cleanup := initXKMSForTest(t)
	defer cleanup()
	ctx := context.Background()

	registry := newRaceRegistry()
	b := newRegistryTestBackend(t, registry)
	createKeyNameTestKey(t, b, "ledger")
	_, err := b.RotateKey(ctx, "ledger")
	require.NoError(t, err)
	recordKey := b.versionRecordKey("ledger")

	registry.mu.Lock()
	registry.key, registry.failDelete = recordKey, true
	registry.mu.Unlock()
	require.Error(t, b.DeleteKey(ctx, "ledger"), "the registry Delete fails")

	b.mu.RLock()
	_, cached := b.metadata["ledger"]
	b.mu.RUnlock()
	assert.False(t, cached, "the cache does not outlive the key material")
	stored, err := b.listStoredKeyIDs(ctx, "ledger")
	require.NoError(t, err)
	require.Empty(t, stored, "the key material is gone")

	require.NoError(t, b.DeleteKey(ctx, "ledger"), "a retry completes the deletion")
	_, err = registry.KVStore.Get(ctx, recordKey)
	require.Error(t, err, "the retry drops the version record")
	require.ErrorIs(t, b.DeleteKey(ctx, "ledger"), ErrKeyNotFound, "a deleted key is not found")

	createKeyNameTestKey(t, b, "ledger")
	current, err := b.CurrentVersion(ctx, "ledger")
	require.NoError(t, err)
	assert.Equal(t, 1, current, "a recreated key starts from version 1")
	ciphertext, err := b.Encrypt(ctx, "ledger", []byte("recreated"), nil)
	require.NoError(t, err)
	v, err := CiphertextKeyVersion(ciphertext)
	require.NoError(t, err)
	assert.Equal(t, uint64(1), v)
}
