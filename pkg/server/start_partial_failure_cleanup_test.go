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

//go:build !integration && raft && local && quic && rest && mcp && vector && timeseries && rag

// start_partial_failure_cleanup_test.go is the unit half of the
// Start()-failure cleanup contract documented on Server.Start and
// Server.Shutdown (go-quicbaas#62):
//
//	a Start that returns an error leaves its partial work standing, and a
//	Shutdown after it releases ALL of it.
//
// The daemon never needed this — it logs the error and exits, and process
// teardown collects everything. An embedded or library caller has no such
// backstop, so the property has to be asserted somewhere or it is only a
// claim.
//
// # The split, and why it is where it is
//
// The contract has two halves, and only one of them can be proven here.
//
// This file owns the RELEASE half: for each resource whose intended owner may
// never be built, Shutdown closes it and drops it. That is a property of the
// teardown path alone, so every test below hands the Server the orphan
// directly and calls Shutdown. None of them calls Start.
//
// That is what keeps this file inside the Unit Test Contract's "ALWAYS run in
// memory" rule (AGENTS.md, "Unit Testing"), and the reason is worth stating
// because it is not obvious: a full Server.Start CANNOT run in memory. Two
// independent parts of the startup path demand a real directory — the
// embedded qrdb node refuses an empty Raft DataDir outright while the vector
// subsystem is compiled in, and buildDataPlaneBarrierService opens a pebble
// seal sub-store for any non-empty Storage.DataDir, ignoring Storage.Backend
// entirely. Setting the backend to memory does not change either. So a test
// that drives a real Start is a test that touches disk, whatever it claims,
// and it belongs in the integration tier.
//
// Which is where the other half lives: the ARRIVAL half — that a mid-Start
// failure really does land in the window where the orphan exists, and that
// the store it leaves behind really does give the operating system's handle
// back. Both need a real server over a real store, and both are asserted in
// test/integration/lifecycle/start_failure_cleanup_test.go, which drives its
// failures from configuration rather than from this package's seams.
//
// The two halves are complementary, not duplicated: without the integration
// tier the orphan windows are unproven, and without this file the release
// path is only exercised for whichever orphans a particular startup failure
// happens to produce.
package server

import (
	"context"
	"log/slog"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/cache"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// withInMemoryServer builds a Server that owns nothing on disk.
//
// It takes the package's standard test server and removes the two settings
// that would make a later Start touch the filesystem: the pebble storage
// backend createTestConfig selects, and the data directory that
// buildDataPlaneBarrierService reads (that function opens a pebble seal
// sub-store for ANY non-empty Storage.DataDir, regardless of the backend --
// an empty DataDir is its own documented "no persistent backend" signal).
//
// No test in this file calls Start, so neither setting is load-bearing for
// what is asserted; clearing them is what makes "runs in memory" checkable
// rather than asserted. A Server that is constructed and shut down without
// ever starting opens nothing at all.
func withInMemoryServer(t *testing.T) *Server {
	t.Helper()
	srv := withTempServer(t)
	srv.config.Storage.Backend = config.StorageBackendMemory
	srv.config.Storage.DataDir = ""
	srv.config.Storage.WALDir = ""
	return srv
}

// cacheCleanupGoroutineFrame is the frame of the goroutine cache.NewManager
// starts for every manager with a positive L1 TTL — which startService's
// manager always has (its config hard-codes 5m when cache is disabled). It is
// the observable that proves a cache manager is still running, as opposed to
// merely still referenced.
const cacheCleanupGoroutineFrame = "cache.(*CacheManager).cleanupExpiredEntries"

// kmsKeyCacheGoroutineFrame is the frame of the goroutine kms.NewKeyCache
// starts for every KMSService built with CacheKeyMaterial — which startKMS
// sets from KMS.Cache.Enabled, on by default. It is the observable that
// proves a KMS manager is still running, as opposed to merely still
// referenced.
const kmsKeyCacheGoroutineFrame = "kms.(*KeyCache).cleanupLoop"

// countGoroutinesWithFrame returns how many goroutines process-wide are
// sitting in frame. Tests compare it against a baseline taken in the same
// test rather than against zero: this binary boots many servers, and a count
// is only meaningful as a delta.
func countGoroutinesWithFrame(frame string) int {
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if n < len(buf) {
			return strings.Count(string(buf[:n]), frame)
		}
		buf = make([]byte, 2*len(buf))
	}
}

// requireGoroutinesDownTo polls until the count of goroutines in frame is
// back to want. Polling, not sleeping: each pass re-checks the real condition
// (go-style.md "No time.Sleep"). Both Close paths this covers join their
// goroutine before returning — CacheManager.Close through a WaitGroup,
// KeyCache.Close through its cleanupDone channel — so this normally succeeds
// on the first pass; the deadline only covers the instructions between that
// join and the goroutine actually leaving the stack.
func requireGoroutinesDownTo(t *testing.T, frame string, want int, msg string) {
	t.Helper()
	err := testutil.WaitForCondition(10*time.Second, 5*time.Millisecond, func() bool {
		return countGoroutinesWithFrame(frame) <= want
	})
	require.NoError(t, err, "%s (still %d goroutines in %s, want <= %d)",
		msg, countGoroutinesWithFrame(frame), frame, want)
}

// closeRecordingAuditSink is a qrdbsdk.NodeAuditLogger that records whether it
// was closed. Emit/Query are delegated to a real in-memory ring buffer so the
// sink behaves like the production one in every respect but observability of
// Close.
type closeRecordingAuditSink struct {
	qrdbsdk.NodeAuditLogger
	closed atomic.Bool
}

func (s *closeRecordingAuditSink) Close() error {
	s.closed.Store(true)
	return s.NodeAuditLogger.Close()
}

// closeRecordingSealStrategy is a qrdbsdk.SealingStrategy that records whether
// it was closed. Only Close is ever reached on a teardown path, so the
// embedded interface is deliberately left nil: any other call is a bug in the
// code under test and panics loudly rather than passing silently.
type closeRecordingSealStrategy struct {
	qrdbsdk.SealingStrategy
	closed atomic.Bool
}

func (s *closeRecordingSealStrategy) Close() error {
	s.closed.Store(true)
	return nil
}

// closeRecordingStore is an io.Closer standing in for the pebble seal
// sub-store, so the data-plane barrier's teardown order can be observed
// without opening a real store.
type closeRecordingStore struct {
	closed atomic.Bool
}

func (s *closeRecordingStore) Close() error {
	s.closed.Store(true)
	return nil
}

// newOrphanedCacheManager builds the cache manager startService builds, with
// the same config that function hard-codes when cache is disabled. Built
// directly rather than through a Start so the orphan exists without anything
// else existing around it.
func newOrphanedCacheManager(t *testing.T, srv *Server) *cache.CacheManager {
	t.Helper()
	mgr, err := cache.NewManager(cache.Config{
		L1Size:          1000,
		L1TTL:           5 * time.Minute,
		CleanupInterval: time.Minute,
	}, logger.NewAdapter(srv.logger))
	require.NoError(t, err, "building the cache manager under test must succeed")
	return mgr
}

// TestServer_Shutdown_OrphanedCacheManager_IsReleased covers the first of the
// two windows that make this contract non-trivial: startService creates the
// cache manager BEFORE the service facade that owns it, and the manager
// starts a cleanup goroutine the moment it exists. Every startService failure
// between the two — and a builder.Build() failure itself — leaves it running
// with its owner never built, so Shutdown is the only call that can reach it.
//
// The facade is left nil here, which is exactly the state such a failure
// leaves behind.
func TestServer_Shutdown_OrphanedCacheManager_IsReleased(t *testing.T) {
	srv := withInMemoryServer(t)

	baseline := countGoroutinesWithFrame(cacheCleanupGoroutineFrame)
	srv.cacheManager = newOrphanedCacheManager(t, srv)

	require.Nil(t, srv.quicBaaSService,
		"precondition: the facade that would own the manager was never built")
	require.Greater(t, countGoroutinesWithFrame(cacheCleanupGoroutineFrame), baseline,
		"precondition: the orphaned manager's cleanup goroutine must be running")

	require.NoError(t, srv.Shutdown(context.Background()),
		"Shutdown after a partial start must complete without error")

	requireGoroutinesDownTo(t, cacheCleanupGoroutineFrame, baseline,
		"Shutdown must stop the cache-cleanup goroutine the partial start orphaned")
	assert.Nil(t, srv.cacheManager,
		"Shutdown must release the orphaned cache manager, not merely stop it")

	require.NoError(t, srv.Shutdown(context.Background()),
		"a second Shutdown must stay a no-op; CacheManager.Close closes an "+
			"unbuffered channel, so a double close would panic rather than leak")
}

// TestServer_Shutdown_OrphanedKMSManager_IsReleased is the KMS half, and a
// harder case than the cache manager: the cache manager at least HAS an
// intended owner that a completed start would have handed it to, whereas the
// KMS manager has none at all downstream — the facade's Close does not close
// it, MemoryCryptoManager.Close is a no-op, and catalog's KMSDEKProvider only
// borrows the reference. Its key-cache cleanup goroutine therefore survived a
// clean Start/Shutdown cycle exactly as readily as a failed one, which is why
// the release below is unconditional rather than guarded on a missing owner.
func TestServer_Shutdown_OrphanedKMSManager_IsReleased(t *testing.T) {
	srv := withInMemoryServer(t)

	baseline := countGoroutinesWithFrame(kmsKeyCacheGoroutineFrame)

	registry := kms.NewBackendRegistry(nil)
	backend := &closeRecordingKMSBackend{}
	require.NoError(t, registry.Register("test-backend", backend, 1),
		"registering a backend must succeed; NewKMSService refuses an empty registry")

	kmsMgr, err := kms.NewKMSService(&kms.KMSServiceOptions{
		Registry: registry,
		Logger:   logger.NewAdapter(srv.logger),
		// The cache is what makes the manager observable: with it on,
		// NewKMSService starts a KeyCache cleanup goroutine. startKMS sets
		// this from KMS.Cache.Enabled, which is on by default.
		CacheKeyMaterial: true,
	})
	require.NoError(t, err, "building the KMS manager under test must succeed")
	srv.kmsManager = kmsMgr

	require.Greater(t, countGoroutinesWithFrame(kmsKeyCacheGoroutineFrame), baseline,
		"precondition: the KMS key-cache cleanup goroutine must be running")

	require.NoError(t, srv.Shutdown(context.Background()))

	requireGoroutinesDownTo(t, kmsKeyCacheGoroutineFrame, baseline,
		"Shutdown must close the KMS manager, joining its key-cache cleanup goroutine")
	assert.Nil(t, srv.kmsManager, "the released KMS manager must not stay referenced")
	assert.True(t, backend.closed.Load(),
		"closing the manager must close its registered backends too")

	require.NoError(t, srv.Shutdown(context.Background()),
		"a second Shutdown must not re-close the cache, which would return ErrCacheClosed")
}

// closeRecordingKMSBackend is the minimum kms.Backend the registry and the
// manager's Close actually touch: Type and SupportedAlgorithms at
// registration, Close at teardown. Every other method is left to the nil
// embedded interface, so a call this test did not anticipate panics loudly
// rather than passing silently.
type closeRecordingKMSBackend struct {
	kms.Backend
	closed atomic.Bool
}

func (b *closeRecordingKMSBackend) Type() config.BackendType { return config.BackendType("test") }

func (b *closeRecordingKMSBackend) SupportedAlgorithms(context.Context) []string { return nil }

func (b *closeRecordingKMSBackend) Close() error {
	b.closed.Store(true)
	return nil
}

// TestServer_Shutdown_TenantDatabaseManager_IsReleased covers the widest
// orphan of the set. The tenant database manager is not merely created before
// its owner — it has NO owner: startDatabases hands it to a qrdb
// DatabaseServicer and the servicer to the facade, and the facade's Close
// reaches its cache manager, backup manager, storage, qrdb client and SSH
// client, and never this. Manager.OpenAll meanwhile holds every recovered
// tenant database open.
//
// The database here is memory-backed, so what is asserted is the registry
// emptying and the field dropping. That a released PERSISTENT store really
// returns the OS handle is the integration tier's assertion, by re-recovery.
func TestServer_Shutdown_TenantDatabaseManager_IsReleased(t *testing.T) {
	srv := withInMemoryServer(t)

	mgr := qrdbsdk.NewManager(slog.Default(), t.TempDir())
	_, err := mgr.Create(qrdbsdk.DatabaseConfig{
		Name:    "tenant-db",
		Engine:  qrdbsdk.EngineKV,
		Backend: qrdbsdk.BackendMemory,
	})
	require.NoError(t, err, "creating the tenant database under test must succeed")
	srv.databaseManager = mgr

	require.Len(t, mgr.List(), 1,
		"precondition: the manager must hold an open database for Shutdown to close")

	require.NoError(t, srv.Shutdown(context.Background()))

	assert.Nil(t, srv.databaseManager,
		"Shutdown must release the tenant database manager, not merely close it")
	assert.Empty(t, mgr.List(),
		"Shutdown must close every database the manager had open, emptying its registry")

	require.NoError(t, srv.Shutdown(context.Background()),
		"a second Shutdown must stay a no-op")
}

// TestServer_Shutdown_DataPlaneBarrier_ReleasesSealStrategyAndStore is the
// regression for the leak that had no owner at all until
// dataPlaneBarrierResources existed.
//
// buildDataPlaneBarrierService returned the pebble seal sub-store as its only
// closer, so the sealing strategies it built alongside were released by
// nobody. With a hardware strategy configured and node sealing disabled, that
// strategy holds an open TPM backend — and it is a DIFFERENT instance from
// the node's own Server.sealStrategy that stopQRDBNode releases, so no
// existing teardown path reached it. A Start that failed after startService,
// and equally one that succeeded, left the TPM open for the life of the
// process.
//
// The store is a recorder rather than a real pebble instance: what is under
// test is which resources the closer owns and in what order, not pebble.
func TestServer_Shutdown_DataPlaneBarrier_ReleasesSealStrategyAndStore(t *testing.T) {
	srv := withInMemoryServer(t)

	strategy := &closeRecordingSealStrategy{}
	store := &closeRecordingStore{}
	srv.dataPlaneBarrierCloser = &dataPlaneBarrierResources{
		strategies: []qrdbsdk.SealingStrategy{strategy},
		store:      store,
	}

	require.NoError(t, srv.Shutdown(context.Background()))

	assert.True(t, strategy.closed.Load(),
		"Shutdown must close the data-plane seal strategy; nothing else owns it")
	assert.True(t, store.closed.Load(),
		"Shutdown must still close the pebble seal sub-store it always closed")
	assert.Nil(t, srv.dataPlaneBarrierCloser,
		"the released closer must not stay referenced")

	require.NoError(t, srv.Shutdown(context.Background()),
		"a second Shutdown must stay a no-op")
}

// TestDataPlaneBarrierResources_CloseAccumulatesErrors pins the accumulating
// behaviour of the closer: one failing resource must not strand the others.
// Without this, a strategy whose Close returned an error would silently skip
// the store close and reintroduce the very leak the type exists to prevent.
func TestDataPlaneBarrierResources_CloseAccumulatesErrors(t *testing.T) {
	failing := &closeFailingSealStrategy{}
	store := &closeRecordingStore{}

	res := &dataPlaneBarrierResources{
		strategies: []qrdbsdk.SealingStrategy{failing, nil},
		store:      store,
	}

	err := res.Close()

	require.Error(t, err, "a failing strategy close must be reported, not swallowed")
	assert.True(t, store.closed.Load(),
		"the store must still be closed after a strategy close failed")
}

// closeFailingSealStrategy is a strategy whose Close always fails, used to
// prove the closer accumulates rather than short-circuits.
type closeFailingSealStrategy struct {
	qrdbsdk.SealingStrategy
}

func (s *closeFailingSealStrategy) Close() error {
	return errSealStrategyCloseFailed
}

// errSealStrategyCloseFailed is the sentinel the failing strategy returns. A
// declared error rather than an inline one, per the repository's typed-error
// rule.
var errSealStrategyCloseFailed = &DataPlaneBarrierError{Stage: "test strategy close"}

// TestServer_Shutdown_NodeConstructionFailed_ReleasesNodeScopedResources
// covers the other orphan window: the audit sink and the seal strategy are
// built BEFORE the qrdb node they are injected into, so a Start that failed
// during node construction leaves them held by the Server with no node to
// release them. Shutdown must reach them anyway.
func TestServer_Shutdown_NodeConstructionFailed_ReleasesNodeScopedResources(t *testing.T) {
	srv := withInMemoryServer(t)

	sink := &closeRecordingAuditSink{NodeAuditLogger: qrdbsdk.NewAuditRingBuffer(8)}
	strategy := &closeRecordingSealStrategy{}
	srv.qrdbAuditSink = sink
	srv.sealStrategy = strategy

	require.Nil(t, srv.getQRDBNode(),
		"precondition: node construction never completed, so there is no node to own these")

	require.NoError(t, srv.Shutdown(context.Background()))

	assert.True(t, sink.closed.Load(),
		"Shutdown must close the audit sink even though no node was ever built")
	assert.True(t, strategy.closed.Load(),
		"Shutdown must close the seal strategy even though no node was ever built")
	assert.Nil(t, srv.qrdbAuditSink, "the released sink must not stay referenced")
	assert.Nil(t, srv.sealStrategy, "the released strategy must not stay referenced")
	assert.Nil(t, srv.sealTPM2Backend,
		"the TPM backend held inside the strategy must be dropped with it")
}

// memorySealStore is an in-memory sealStore, substituted for the pebble seal
// sub-store so buildDataPlaneBarrierService's assembly can be asserted
// without the filesystem. It is a real store, not a stub: the barrier and the
// credential/ceremony services built over it read and write through it during
// construction, so a store that only pretended would not get them built.
type memorySealStore struct {
	mu     sync.Mutex
	data   map[string][]byte
	closed atomic.Bool
}

func newMemorySealStore() *memorySealStore {
	return &memorySealStore{data: make(map[string][]byte)}
}

func (s *memorySealStore) Get(key []byte) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.data[string(key)]
	if !ok {
		return nil, errSealKeyNotFound
	}
	return append([]byte(nil), value...), nil
}

func (s *memorySealStore) Set(key []byte, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[string(key)] = append([]byte(nil), value...)
	return nil
}

func (s *memorySealStore) Delete(key []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.data, string(key))
	return nil
}

func (s *memorySealStore) List(prefix []byte) ([][]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var keys [][]byte
	for key := range s.data {
		if strings.HasPrefix(key, string(prefix)) {
			keys = append(keys, []byte(key))
		}
	}
	return keys, nil
}

func (s *memorySealStore) Exists(key []byte) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	_, ok := s.data[string(key)]
	return ok, nil
}

func (s *memorySealStore) Close() error {
	s.closed.Store(true)
	return nil
}

// errSealKeyNotFound is the miss returned by the in-memory seal store. A
// declared error rather than an inline one, per the repository's typed-error
// rule.
var errSealKeyNotFound = &DataPlaneBarrierError{Stage: "seal store get: key not found"}

// withMemorySealStore points newSealStoreFn at an in-memory store for the
// duration of the test and hands the store back so the test can observe it.
// Restored on cleanup so no other test in the package inherits it.
func withMemorySealStore(t *testing.T) *memorySealStore {
	t.Helper()
	store := newMemorySealStore()
	original := newSealStoreFn
	newSealStoreFn = func(string) (sealStore, error) { return store, nil }
	t.Cleanup(func() { newSealStoreFn = original })
	return store
}

// TestBuildDataPlaneBarrierService_CloserOwnsSealStrategies is the wiring half
// of the data-plane leak regression, and the half that would still fail if the
// strategies were built and then dropped on the floor.
//
// buildDataPlaneBarrierService used to return the seal sub-store as its ONLY
// closer, so the sealing strategies it built alongside were released by
// nobody: not the caller, which stores the closer and nothing else; not the
// barrier, which does not own its strategies; and not stopQRDBNode, whose
// Server.sealStrategy is a different instance from a separate build call. With
// a hardware strategy configured that orphan holds an open TPM backend, and
// the SealingStrategy contract is that closing the strategy is what releases
// it -- so a Start that failed after this point, and equally one that
// succeeded, left the TPM open for the life of the process.
//
// The strategy built here is the software "none" strategy, not tpm2: the
// ownership wiring is identical for both, and a regression that needed real
// hardware could not run anywhere it matters.
func TestBuildDataPlaneBarrierService_CloserOwnsSealStrategies(t *testing.T) {
	store := withMemorySealStore(t)

	cfg := config.Default()
	// Non-empty so the builder proceeds; never touched, because the seam
	// above has replaced everything that would read it.
	cfg.Storage.DataDir = "/quicbaas-test-never-created"
	cfg.Seal.Strategy = string(qrdbsdk.StrategyNone)

	svc, barrier, ceremony, closer, err := buildDataPlaneBarrierService(cfg, slog.Default())
	require.NoError(t, err, "constructing the data-plane barrier must succeed")
	require.NotNil(t, svc, "the barrier service must be built")
	require.NotNil(t, barrier, "the raw StorageBarrier must be built")
	require.NotNil(t, ceremony, "the ceremony core must be built")
	require.NotNil(t, closer, "a successful build must hand back a closer")

	// The assertion. A closer holding only the store would satisfy every
	// other check here and still leak the strategies.
	owned, ok := closer.(*dataPlaneBarrierResources)
	require.True(t, ok,
		"the builder must return the owning closer, not a bare store handle")
	require.NotEmpty(t, owned.strategies,
		"the sealing strategies must travel out with the store; nothing else owns them")
	require.NotNil(t, owned.store, "the seal sub-store must still be owned, as it always was")

	require.NoError(t, closer.Close(), "closing must release strategies and store together")
	assert.True(t, store.closed.Load(),
		"closing the returned closer must close the seal sub-store")
}

// TestBuildDataPlaneBarrierService_ConstructionErrorReleasesStore is the
// error-path half of the same ask: a build that fails must leave nothing
// open and hand back no closer for the caller to have to guess about.
func TestBuildDataPlaneBarrierService_ConstructionErrorReleasesStore(t *testing.T) {
	store := newMemorySealStore()
	original := newSealStoreFn
	newSealStoreFn = func(string) (sealStore, error) { return store, nil }
	t.Cleanup(func() { newSealStoreFn = original })

	cfg := config.Default()
	cfg.Storage.DataDir = "/quicbaas-test-never-created"
	// A shamir threshold larger than the total share count cannot produce a
	// valid strategy, so the build fails AFTER the store is open -- which is
	// the window where a leak would happen.
	cfg.Seal.Strategy = string(qrdbsdk.StrategyShamir)
	cfg.Seal.Threshold = 5
	cfg.Seal.TotalShares = 2

	svc, barrier, ceremony, closer, err := buildDataPlaneBarrierService(cfg, slog.Default())

	require.Error(t, err, "an impossible shamir quorum must fail the build")
	assert.Nil(t, svc, "a failed build must return no service")
	assert.Nil(t, barrier, "a failed build must return no barrier")
	assert.Nil(t, ceremony, "a failed build must return no ceremony core")
	assert.Nil(t, closer, "a failed build must return no closer")
	assert.True(t, store.closed.Load(),
		"a failed build must close the seal sub-store it had already opened")
}
