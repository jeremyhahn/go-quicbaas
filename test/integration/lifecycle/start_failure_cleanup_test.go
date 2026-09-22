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

//go:build integration

// Package lifecycle holds the integration half of the Server start/shutdown
// resource-release contract (go-quicbaas#62).
//
// The unit half lives in pkg/server/start_partial_failure_cleanup_test.go. It
// owns the RELEASE property -- that Shutdown closes and drops each orphan it
// is handed -- and it proves that without calling Start at all, which is what
// lets it stay in memory as the Unit Test Contract requires (AGENTS.md, "Unit
// Testing": ALWAYS run in memory).
//
// Two things cannot be proven that way, and they are what this package exists
// for. Both need a real Start over a real store, and a real Start cannot run
// in memory: the embedded qrdb node refuses an empty Raft DataDir while the
// vector subsystem is compiled in, and buildDataPlaneBarrierService opens a
// pebble seal sub-store for any non-empty Storage.DataDir regardless of the
// configured storage backend.
//
//  1. That a mid-Start failure really lands in the window where the orphan
//     exists, and that Shutdown afterwards joins the background goroutines it
//     left running. Goroutine stack frames carry that assertion because they
//     cross the package boundary that Server's unexported fields do not.
//
//  2. That a released PERSISTENT store really let go of the operating
//     system's handle, rather than merely being dropped from a field. A
//     memory-backed database releases nothing observable when it is closed.
//     The proof is re-recovery: a database a previous run left on disk is
//     recovered by the server at boot, and after Shutdown a FRESH manager
//     must be able to recover the very same directory. See
//     requireDatabasesRecoverable for why the recovered COUNT, not the
//     returned error, is what carries it.
//
// Every failure here is driven from CONFIGURATION rather than a test seam,
// since this package sits outside pkg/server -- which is also the truer test,
// because an embedded caller's Start fails that way rather than by a swapped
// function pointer.
package lifecycle

import (
	"context"
	"log/slog"
	"net"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
)

// seededDatabaseName is the tenant database every test here leaves on disk
// for the server to recover.
const seededDatabaseName = "recovered-tenant-db"

// freePort reserves a kernel-assigned port and hands it back. The listener is
// closed before the port is used, which is the standard race every server in
// this suite already accepts; a fixed port would be the worse trade, because
// two packages running in parallel would collide on it deterministically
// rather than improbably.
func freePort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err, "reserving a free port must succeed")
	defer listener.Close()

	return listener.Addr().(*net.TCPAddr).Port
}

// embeddedConfig builds an embedded-mode server configuration whose tenant
// databases live under dbRoot. Embedded mode is the case the contract is
// about: the daemon survives a failed Start by exiting, and an embedded
// caller has no such backstop.
func embeddedConfig(t *testing.T, dbRoot string) *config.Config {
	t.Helper()

	tempDir := t.TempDir()
	cfg := config.EmbeddedConfig()
	cfg.Storage.DataDir = filepath.Join(tempDir, "data")
	cfg.Storage.WALDir = filepath.Join(tempDir, "wal")
	cfg.Logging.Level = "error"
	cfg.Server.Embedded = true

	port := freePort(t)
	cfg.Raft.NodeID = 1
	cfg.Raft.RaftAddress = "127.0.0.1:" + strconv.Itoa(port)
	cfg.Raft.ListenAddress = "127.0.0.1:" + strconv.Itoa(port)

	// The subsystem under test: startDatabases builds the tenant database
	// manager and recovers every database it finds under this directory.
	cfg.Databases.Enabled = true
	cfg.Databases.DataDir = dbRoot

	return cfg
}

// seedPersistentDatabase leaves under dbRoot exactly what a previous run of
// the daemon would have left: a meta.json plus a pebble store that the next
// startDatabases recovers through Manager.OpenAll.
//
// BackendPebble, not BackendMemory, is the whole point of running this in the
// integration tier. A memory-backed database releases nothing observable when
// it is closed, so "the handle was released" could only be inferred from a
// nil field; a pebble one is held by the process that opened it, so the
// release can be proved from the outside by recovering the same directory
// again.
func seedPersistentDatabase(t *testing.T, dbRoot string) {
	t.Helper()

	seed := qrdbsdk.NewManager(slog.Default(), dbRoot)
	_, err := seed.Create(qrdbsdk.DatabaseConfig{
		Name:    seededDatabaseName,
		Engine:  qrdbsdk.EngineKV,
		Backend: qrdbsdk.BackendPebble,
	})
	require.NoError(t, err, "seeding a persistent tenant database must succeed")
	require.NoError(t, seed.Close(), "the seeding manager must release the database again")
}

// requireDatabasesRecoverable proves that no store handle survives under
// dbRoot, by recovering the directory with a manager that has nothing to do
// with the server.
//
// The RECOVERED COUNT is what carries this assertion, not the error.
// Manager.OpenAll treats a database it cannot open as non-fatal -- it logs
// and moves on, so that one broken tenant cannot keep a node from booting --
// and a store another process still holds is exactly such a database. A
// leaked handle therefore shows up here as a manager that returns no error
// and has recovered NOTHING, which is why asserting only on the error would
// pass against the very leak this file exists to catch.
func requireDatabasesRecoverable(t *testing.T, dbRoot string) {
	t.Helper()

	reopened := qrdbsdk.NewManager(slog.Default(), dbRoot)
	require.NoError(t, reopened.OpenAll(),
		"a fresh manager must be able to recover the databases the server released")
	t.Cleanup(func() {
		require.NoError(t, reopened.Close(), "the verifying manager must release the database again")
	})

	recovered := reopened.List()
	require.Len(t, recovered, 1,
		"the seeded database must be recoverable after shutdown; recovering none is how a "+
			"surviving store handle manifests, because OpenAll skips what it cannot open")
	require.Equal(t, seededDatabaseName, recovered[0].Name,
		"the recovered database must be the one that was seeded")
}

// TestServerStartFailure_ShutdownReleasesRecoveredDatabaseHandles is the
// verification go-quicbaas#62 asks for, in the tier that can make it: start a
// server against a real persistent store, force a mid-Start failure AFTER the
// databases have been recovered, and prove that the Shutdown the contract
// requires of the caller leaves no store handle behind.
//
// The failure is driven entirely by configuration rather than by a test seam,
// because this package is outside pkg/server and has no access to its
// internals -- which is the more honest test anyway: an embedded caller's
// Start fails for reasons like this one, not because a function pointer was
// swapped. An idle threshold naming a job state that does not exist is
// refused by startOrchestration, which initialize reaches several steps after
// startDatabases, so by the time the error fires the seeded database is open
// and owned by a manager whose only closer is Shutdown.
func TestServerStartFailure_ShutdownReleasesRecoveredDatabaseHandles(t *testing.T) {
	dbRoot := t.TempDir()
	seedPersistentDatabase(t, dbRoot)

	cfg := embeddedConfig(t, dbRoot)
	cfg.Orchestration.Enabled = true
	cfg.Orchestration.IdleThresholds = map[string]time.Duration{
		"not-a-watched-job-state": time.Hour,
	}

	srv, err := server.NewServer(cfg)
	require.NoError(t, err, "constructing the server must succeed; the failure under test belongs to Start")

	startErr := srv.Start()
	require.Error(t, startErr, "the configured idle threshold must abort Start")

	// Precondition, not decoration. Without it this test would also pass on a
	// Start that failed BEFORE any database was recovered -- which is not the
	// window the contract is about, and would make the assertion below vacuous.
	var thresholdErr *orchestration.JobIdleThresholdError
	require.ErrorAs(t, startErr, &thresholdErr,
		"precondition: Start must fail at orchestration, which initialize reaches after startDatabases")

	// The contract: Shutdown after a failed Start releases the partial start.
	require.NoError(t, srv.Shutdown(context.Background()),
		"Shutdown after a failed Start must complete without error")

	requireDatabasesRecoverable(t, dbRoot)

	// And it stays safe to call again, which is what lets an embedded caller
	// defer Shutdown immediately after NewServer.
	require.NoError(t, srv.Shutdown(context.Background()),
		"a second Shutdown after a failed Start must stay a no-op")
}

// TestServerStartSuccess_ShutdownReleasesRecoveredDatabaseHandles is the same
// guarantee on the clean path, and it is not redundant with the test above.
//
// The tenant database manager had no downstream owner on ANY path: the facade
// it is handed to closes its own cache, backup, storage and client resources
// and never this. So a fully successful Start/Shutdown cycle stranded the
// recovered stores exactly as a failed one did, and a fix that only ran on
// the partial-start branch would have left the commoner case leaking.
func TestServerStartSuccess_ShutdownReleasesRecoveredDatabaseHandles(t *testing.T) {
	dbRoot := t.TempDir()
	seedPersistentDatabase(t, dbRoot)

	srv, err := server.NewServer(embeddedConfig(t, dbRoot))
	require.NoError(t, err, "constructing the server must succeed")

	require.NoError(t, srv.Start(), "the clean embedded boot must succeed")
	require.NoError(t, srv.Shutdown(context.Background()), "the clean shutdown must succeed")

	requireDatabasesRecoverable(t, dbRoot)
}

// cacheCleanupGoroutineFrame and kmsKeyCacheGoroutineFrame are the frames of
// the two background goroutines that a partially-started server used to
// strand: the cache manager's expiry sweeper, started by cache.NewManager the
// moment startService builds the manager, and the KMS key-material cache's
// cleanup loop, started by kms.NewKMSService whenever kms.cache.enabled is on.
//
// Counting stack frames is how this package observes them at all. The
// resources themselves are unexported fields of server.Server and cannot be
// reached from outside the package -- but a running goroutine is visible
// process-wide, which makes it the one observable that survives the package
// boundary. It is also the observable that matters: the defect was never "a
// field stayed non-nil", it was "a goroutine kept running after the process
// asked for shutdown".
const (
	cacheCleanupGoroutineFrame = "cache.(*CacheManager).cleanupExpiredEntries"
	kmsKeyCacheGoroutineFrame  = "kms.(*KeyCache).cleanupLoop"
)

// countGoroutinesWithFrame returns how many goroutines process-wide are
// currently in frame. Compared against a baseline taken in the same test
// rather than against zero: this binary boots several servers, so only a
// delta is meaningful.
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

// requireGoroutinesDownTo polls until the count of goroutines in frame is back
// to want. Polling rather than sleeping: each pass re-checks the real
// condition (go-style.md, "No time.Sleep"). Both Close paths this covers join
// their goroutine before returning -- CacheManager.Close through a WaitGroup,
// KeyCache.Close through its cleanupDone channel -- so this normally succeeds
// on the first pass; the deadline only covers the instructions between that
// join and the goroutine leaving the stack.
func requireGoroutinesDownTo(t *testing.T, frame string, want int, msg string) {
	t.Helper()

	deadline := time.After(30 * time.Second)
	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()

	for {
		if countGoroutinesWithFrame(frame) <= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("%s (still %d goroutines in %s, want <= %d)",
				msg, countGoroutinesWithFrame(frame), frame, want)
		case <-tick.C:
		}
	}
}

// failingEmbeddedConfig is embeddedConfig plus the configuration that aborts
// Start well after startService has built the cache and KMS managers. The
// same idle-threshold refusal the database test uses: startOrchestration
// rejects a threshold naming a job state that does not exist, and initialize
// reaches orchestration several steps after both managers exist.
func failingEmbeddedConfig(t *testing.T, dbRoot string) *config.Config {
	t.Helper()

	cfg := embeddedConfig(t, dbRoot)
	cfg.Orchestration.Enabled = true
	cfg.Orchestration.IdleThresholds = map[string]time.Duration{
		"not-a-watched-job-state": time.Hour,
	}
	return cfg
}

// TestServerStartFailure_ShutdownJoinsBackgroundGoroutines is the goroutine
// half of the verification go-quicbaas#62 asks for: start a server, force a
// mid-Start failure, and assert no background worker survives the Shutdown
// the contract requires of the caller.
//
// It runs here rather than in the unit tier because it needs a REAL Start,
// and a real Start cannot run in memory: the embedded qrdb node refuses an
// empty Raft DataDir while the vector subsystem is compiled in, and the
// data-plane barrier opens a pebble seal sub-store for any non-empty
// Storage.DataDir regardless of the storage backend. The unit tier asserts
// the release path itself -- that Shutdown closes and drops each orphan it is
// handed -- entirely in memory and without ever calling Start.
//
// Both goroutines are covered in one test deliberately. They are orphaned by
// the same failure at the same moment, and asserting them together is what
// proves Shutdown reaches ALL of a partial start rather than whichever piece
// a particular test remembered to check.
func TestServerStartFailure_ShutdownJoinsBackgroundGoroutines(t *testing.T) {
	dbRoot := t.TempDir()
	seedPersistentDatabase(t, dbRoot)

	cfg := failingEmbeddedConfig(t, dbRoot)
	// The KMS key cache is what makes the KMS manager observable: with it on,
	// NewKMSService starts a cleanup goroutine the moment the manager exists.
	cfg.KMS.Enabled = true
	cfg.KMS.Cache.Enabled = true

	cacheBaseline := countGoroutinesWithFrame(cacheCleanupGoroutineFrame)
	kmsBaseline := countGoroutinesWithFrame(kmsKeyCacheGoroutineFrame)

	srv, err := server.NewServer(cfg)
	require.NoError(t, err, "constructing the server must succeed; the failure under test belongs to Start")

	startErr := srv.Start()
	require.Error(t, startErr, "the configured idle threshold must abort Start")

	var thresholdErr *orchestration.JobIdleThresholdError
	require.ErrorAs(t, startErr, &thresholdErr,
		"precondition: Start must fail at orchestration, which initialize reaches after startService")

	// Preconditions. Without these the assertions below would pass just as
	// happily on a Start that failed before either manager was ever built,
	// which is not the window the contract is about.
	require.Greater(t, countGoroutinesWithFrame(cacheCleanupGoroutineFrame), cacheBaseline,
		"precondition: the cache manager orphaned by the failed Start must still be running")
	require.Greater(t, countGoroutinesWithFrame(kmsKeyCacheGoroutineFrame), kmsBaseline,
		"precondition: the KMS manager's key-cache goroutine must still be running")

	// The contract: Shutdown after a failed Start releases the partial start.
	require.NoError(t, srv.Shutdown(context.Background()),
		"Shutdown after a failed Start must complete without error")

	requireGoroutinesDownTo(t, cacheCleanupGoroutineFrame, cacheBaseline,
		"Shutdown must stop the cache-cleanup goroutine the failed Start orphaned")
	requireGoroutinesDownTo(t, kmsKeyCacheGoroutineFrame, kmsBaseline,
		"Shutdown must stop the KMS key-cache goroutine the failed Start orphaned")

	require.NoError(t, srv.Shutdown(context.Background()),
		"a second Shutdown after a failed Start must stay a no-op")
}

// TestServerStartSuccess_ShutdownJoinsBackgroundGoroutines is the same
// guarantee on the clean path, and it is not redundant. The KMS manager has
// no downstream owner on ANY path -- the service facade's Close does not
// close it, MemoryCryptoManager.Close is a no-op, and catalog's KMSDEKProvider
// only borrows the reference -- so its key-cache goroutine survived a fully
// successful Start/Shutdown cycle exactly as readily as a failed one. A fix
// that only ran on the partial-start branch would have left the commoner case
// leaking.
func TestServerStartSuccess_ShutdownJoinsBackgroundGoroutines(t *testing.T) {
	dbRoot := t.TempDir()
	seedPersistentDatabase(t, dbRoot)

	cfg := embeddedConfig(t, dbRoot)
	cfg.KMS.Enabled = true
	cfg.KMS.Cache.Enabled = true

	cacheBaseline := countGoroutinesWithFrame(cacheCleanupGoroutineFrame)
	kmsBaseline := countGoroutinesWithFrame(kmsKeyCacheGoroutineFrame)

	srv, err := server.NewServer(cfg)
	require.NoError(t, err, "constructing the server must succeed")

	require.NoError(t, srv.Start(), "the clean embedded boot must succeed")
	require.NoError(t, srv.Shutdown(context.Background()), "the clean shutdown must succeed")

	requireGoroutinesDownTo(t, cacheCleanupGoroutineFrame, cacheBaseline,
		"a clean Shutdown must stop the cache-cleanup goroutine too")
	requireGoroutinesDownTo(t, kmsKeyCacheGoroutineFrame, kmsBaseline,
		"a clean Shutdown must stop the KMS key-cache goroutine too")
}
