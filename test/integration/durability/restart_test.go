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

// Package durability holds the restart-durability integration suite: entities
// written through the public service API must still be readable after the
// daemon is shut down and brought back up against the SAME data directory.
//
// Regression origin: quicbaasd armed the secure subsystem with
// secure.CatchInterrupt(), whose memguard signal handler wipes mlocked memory
// and then hard os.Exit(1)s from inside the handler. That always beat the
// daemon's own graceful-shutdown path, so Server.Shutdown never ran, the
// embedded qrdb node was never closed, and every PebbleDB write still held in
// the memtable / unsynced WAL block was discarded. API-created IAM users
// vanished across a restart while the first-boot bootstrap admin (whose writes
// had already been flushed) survived — a silent data-loss bug.
//
// These tests pin the storage half of that invariant: a full
// Shutdown -> re-open over the same data dir must round-trip the user record,
// its roles, and its tenant. The signal-handling half is pinned by
// TestSecureSubsystem_DoesNotInstallProcessKillingSignalHandler in
// cmd/quicbaasd.
package durability

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/netport"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// bootTimeout bounds how long a server may take to report started.
const bootTimeout = 60 * time.Second

// freePort reserves a loopback port for the test's server to bind. It is
// reserved through netport rather than drawn from :0 and released, so nothing
// else can be handed it before the bind (go-quicbaas#721), and it is released
// after the test's own cleanups.
func freePort(t *testing.T) int {
	t.Helper()
	return netport.MustReserveOne(t)
}

// durabilityConfig builds an embedded, pebble-backed server configuration
// rooted at dataDir. Pebble is the backend that exhibited the data loss; a
// memory backend cannot express the invariant under test.
func durabilityConfig(t *testing.T, dataDir string) *config.Config {
	t.Helper()

	cfg := config.EmbeddedConfig()
	cfg.IAM.SPIFFETrustDomain = helpers.SPIFFETrustDomain
	cfg.Storage.Backend = config.StorageBackendPebble
	cfg.Storage.DataDir = dataDir
	cfg.Storage.WALDir = filepath.Join(dataDir, "wal")
	cfg.Raft.NodeID = 1
	raftAddr := "127.0.0.1:" + strconv.Itoa(freePort(t))
	cfg.Raft.RaftAddress = raftAddr
	cfg.Raft.ListenAddress = raftAddr
	cfg.Logging.Level = "error"

	return cfg
}

// bootServer starts a server over dataDir and waits for it to report started.
// The caller owns shutdown so a test can control exactly when the store is
// closed (that boundary is the whole point of the suite).
func bootServer(t *testing.T, dataDir string) *server.Server {
	t.Helper()

	srv, err := server.NewServer(durabilityConfig(t, dataDir))
	require.NoError(t, err, "construct server over %s", dataDir)
	require.NoError(t, srv.Start(), "start server over %s", dataDir)

	deadline := time.Now().Add(bootTimeout)
	for !srv.IsStarted() {
		if time.Now().After(deadline) {
			shutdownServer(t, srv)
			t.Fatalf("server over %s did not report started within %s", dataDir, bootTimeout)
		}
		time.Sleep(50 * time.Millisecond)
	}

	require.NotNil(t, srv.Service(), "service must be available after start")
	return srv
}

// shutdownServer performs the graceful shutdown that closes the embedded qrdb
// node (and therefore flushes and closes its PebbleDB).
func shutdownServer(t *testing.T, srv *server.Server) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx), "graceful shutdown must succeed")
}

// iamService resolves the facade's IAM surface. IAM() is a concrete accessor
// on the facade rather than a method on the service.QuicBaaS interface, so the
// test asserts for it explicitly instead of assuming a wider interface.
func iamService(t *testing.T, srv *server.Server) service.IAMService {
	t.Helper()
	svc := srv.Service()
	require.NotNil(t, svc, "service facade must be available")
	accessor, ok := svc.(interface{ IAM() service.IAMService })
	require.True(t, ok, "service facade must expose IAM()")
	iamSvc := accessor.IAM()
	require.NotNil(t, iamSvc, "IAM service must be wired")
	return iamSvc
}

// TestIAMUserSurvivesDaemonRestart is the regression test for the reported
// data loss: a user created through the service API — the same seam every
// transport converges on — must still be readable, with its roles and tenant
// intact, after a graceful shutdown and a fresh boot over the same data dir.
func TestIAMUserSurvivesDaemonRestart(t *testing.T) {
	dataDir := t.TempDir()
	ctx := context.Background()

	tenantID := "durability-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	email := "restart-" + strconv.FormatInt(time.Now().UnixNano(), 36) + "@example.com"

	var userID uint64
	var roleID string

	// ---- boot 1: write ----
	srv := bootServer(t, dataDir)
	iamSvc := iamService(t, srv)

	_, err := iamSvc.CreateTenant(ctx, &service.CreateTenantRequest{
		ID:   tenantID,
		Name: "Durability Tenant",
	})
	require.NoError(t, err, "create tenant")

	roleResp, err := iamSvc.CreateRole(ctx, &service.IAMCreateRoleRequest{
		TenantID:    tenantID,
		Name:        "durability-role",
		Permissions: []string{"kv:get"},
	})
	require.NoError(t, err, "create role")
	require.NotNil(t, roleResp.Role)
	roleID = roleResp.Role.ID

	createResp, err := iamSvc.CreateUser(ctx, &service.IAMCreateUserRequest{
		TenantID: tenantID,
		Email:    email,
		Username: "durableuser",
		Name:     "Durable User",
		Roles:    []string{roleID},
	})
	require.NoError(t, err, "create user")
	require.NotNil(t, createResp.User)
	userID = createResp.User.ID
	require.NotZero(t, userID)
	assert.Equal(t, []string{roleID}, createResp.User.Roles, "roles must round-trip on create")

	// Read back within the same process: proves the write is visible, which
	// was ALSO true of the losing build. Durability is only proven after the
	// restart below.
	preRestart, err := iamSvc.GetUser(ctx, &service.IAMGetUserRequest{TenantID: tenantID, UserID: userID})
	require.NoError(t, err, "read back before restart")
	require.NotNil(t, preRestart.User)

	shutdownServer(t, srv)

	// ---- boot 2: same data dir, must still see the user ----
	srv = bootServer(t, dataDir)
	defer shutdownServer(t, srv)

	iamSvc = iamService(t, srv)

	got, err := iamSvc.GetUser(ctx, &service.IAMGetUserRequest{TenantID: tenantID, UserID: userID})
	require.NoError(t, err, "user must survive the restart")
	require.NotNil(t, got.User)

	assert.Equal(t, userID, got.User.ID, "user ID must be stable across restart")
	assert.Equal(t, email, got.User.Email, "email must survive the restart")
	assert.Equal(t, tenantID, got.User.TenantID, "tenant binding must survive the restart")
	assert.Equal(t, []string{roleID}, got.User.Roles, "role bindings must survive the restart")

	listed, err := iamSvc.ListUsers(ctx, &service.IAMListUsersRequest{TenantID: tenantID})
	require.NoError(t, err, "list users after restart")
	found := false
	for _, u := range listed.Users {
		if u.ID == userID {
			found = true
			break
		}
	}
	assert.True(t, found, "restarted daemon must list the user it persisted")
}

// TestIAMUserRestartDoesNotResurrectDeletedUser is the error-path counterpart:
// durability must be symmetric. A delete that is acknowledged before shutdown
// must also be durable, so the user must NOT reappear after the restart (a
// store that reverted to a stale on-disk snapshot would resurrect it).
func TestIAMUserRestartDoesNotResurrectDeletedUser(t *testing.T) {
	dataDir := t.TempDir()
	ctx := context.Background()

	tenantID := "durability-del-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	email := "deleted-" + strconv.FormatInt(time.Now().UnixNano(), 36) + "@example.com"

	srv := bootServer(t, dataDir)
	iamSvc := iamService(t, srv)

	_, err := iamSvc.CreateTenant(ctx, &service.CreateTenantRequest{ID: tenantID, Name: "Durability Delete Tenant"})
	require.NoError(t, err, "create tenant")

	createResp, err := iamSvc.CreateUser(ctx, &service.IAMCreateUserRequest{
		TenantID: tenantID,
		Email:    email,
		Username: "deleteduser",
		Name:     "Deleted User",
	})
	require.NoError(t, err, "create user")
	userID := createResp.User.ID

	_, err = iamSvc.DeleteUser(ctx, &service.IAMDeleteUserRequest{TenantID: tenantID, UserID: userID})
	require.NoError(t, err, "delete user")

	shutdownServer(t, srv)

	srv = bootServer(t, dataDir)
	defer shutdownServer(t, srv)

	iamSvc = iamService(t, srv)

	_, err = iamSvc.GetUser(ctx, &service.IAMGetUserRequest{TenantID: tenantID, UserID: userID})
	require.Error(t, err, "a deleted user must not be resurrected by a restart")
}

// TestMain keeps the suite honest about leaked temp state: t.TempDir cleans up
// per test, so there is nothing global to tear down, but an explicit hook
// documents that this package intentionally owns no shared server.
func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
