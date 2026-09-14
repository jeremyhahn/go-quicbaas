//go:build mcp

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

// boot_oidc_xkms_test.go is the regression guard for BOTH global-xkms boot
// invariants:
//
//  1. KMS-disabled posture: initializeCA must register the CA key universe as
//     the process-global xkms service so IAM's OIDC signing key
//     (NewSigningKeyManagerFromXKMS) — and with it the OIDC provider that
//     backs every RequireAuthz enforcement gate — is available even when the
//     optional KMS service is disabled. Before this guard, a kms.enabled=false
//     boot left the singleton uninitialized, the OIDC provider was nil, and
//     the fail-closed authz gates refused to start the daemon.
//
//  2. KMS-enabled posture: the singleton initializeCA registers must be the
//     UNIFIED software backend (PKCS#8 asymmetric + AES symmetric), because
//     startKMS tolerates the earlier registration via ErrAlreadyInitialized
//     and the KMS service then rides it. A pkcs8-only registration broke
//     every symmetric KMS operation with "backend does not support symmetric
//     operations: software" (the gate-3 regression).

package server

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// productionPostureConfig returns the exact fixture the IAM bootstrap
// integration tests use: RequireAuthz=true, AllowPasswordAuth=false,
// InsecureDevMode=false, embedded qrdb node, all optional data services off.
// KMS is toggled per test.
func productionPostureConfig(t *testing.T, kmsEnabled bool) *config.Config {
	t.Helper()
	tmpDir := t.TempDir()

	cfg := config.DefaultConfig()
	cfg.Storage.DataDir = filepath.Join(tmpDir, "data")
	cfg.Storage.WALDir = filepath.Join(tmpDir, "wal")
	cfg.Raft.NodeID = 1

	// Every listener the embedded node brings up gets a RESERVED port.
	//
	// This used to ask for REST port 0 and leave gRPC and MCP on
	// DefaultConfig's 9443 and 11443. None of the three was collision-free.
	// Port 0 is not "let the kernel pick" once it reaches the node: qrdb_node.go
	// copies Server.REST.Port into qcfg.Server.AdminPort, and go-qrdb's node
	// builder substitutes a literal for a zero port —
	//
	//     go-qrdb/sdk/go@v0.16.3-alpha node/node.go:2660
	//     if serverCfg.Server.AdminPort == 0 {
	//         serverCfg.Server.AdminPort = 63001
	//     }
	//
	// — so "any port" arrived at the bind as the single most contended port in
	// the tree, and the other two were fixed to begin with. Any second gate on
	// the host took one of them and this test failed for a reason that had
	// nothing to do with it (go-quicbaas#713, CI job 61720 on PR #707).
	ports := getFreePorts(t, 3)
	cfg.Server.REST.Port, cfg.Server.GRPC.Port, cfg.Server.MCP.Port = ports[0], ports[1], ports[2]

	cfg.KMS.Enabled = kmsEnabled
	cfg.Secrets.Enabled = false
	cfg.Vector.Enabled = false
	cfg.TimeSeries.Enabled = false
	cfg.Logging.Level = "error"
	cfg.Server.Embedded = true
	cfg.Auth.AllowPasswordAuth = false
	cfg.Auth.InsecureDevMode = false
	return cfg
}

// bootProductionPostureServer starts an embedded server against a fresh
// global xkms singleton and registers shutdown cleanup.
func bootProductionPostureServer(t *testing.T, cfg *config.Config) *Server {
	t.Helper()

	// The xkms singleton is process-global; reset so this boot exercises the
	// real initializeCA/startKMS registration order instead of inheriting
	// whatever an earlier test left behind.
	dbxkms.Reset()
	t.Cleanup(dbxkms.Reset)

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	startErr := srv.Start()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})
	require.NoError(t, startErr,
		"production-posture embedded boot must start (RequireAuthz gates need a live OIDC provider)")
	return srv
}

// A production-posture embedded boot with KMS disabled must come up with a
// working OIDC provider sourced from the CA-registered global xkms backend —
// and that registration must be the unified composition (symmetric-capable),
// not a pkcs8-only fork.
func TestEmbeddedBoot_KMSDisabled_OIDCProviderAvailable(t *testing.T) {
	srv := bootProductionPostureServer(t, productionPostureConfig(t, false))

	require.True(t, dbxkms.IsInitialized(),
		"global xkms must be initialized by initializeCA even with kms.enabled=false")

	svc := srv.Service()
	require.NotNil(t, svc)
	getter, ok := svc.(interface {
		GetOIDCProvider(ctx context.Context) (*iam.OIDCProvider, error)
	})
	require.True(t, ok, "service must expose GetOIDCProvider")
	provider, provErr := getter.GetOIDCProvider(context.Background())
	require.NoError(t, provErr)
	require.NotNil(t, provider,
		"OIDC provider must be available for the RequireAuthz enforcement gates")

	// Pin the single-composition mechanism: the CA-registered "software"
	// backend must be the unified backend so a later kms.enabled=true boot
	// (which reuses this registration via ErrAlreadyInitialized) gets
	// symmetric operations instead of "backend does not support symmetric
	// operations: software".
	backend, beErr := dbxkms.GetBackend(string(xkmssdk.StoreSoftware))
	require.NoError(t, beErr)
	_, symmetric := backend.KeyProvider().(xkmssdk.SymmetricKeyProvider)
	require.True(t, symmetric,
		"the CA-registered software backend must support symmetric operations (unified composition)")
}

// A production-posture embedded boot with KMS ENABLED must serve symmetric
// KMS operations end-to-end through the service layer. This is the gate-3
// regression guard: initializeCA registers the global xkms singleton first,
// startKMS tolerates ErrAlreadyInitialized and rides that registration, so
// the registration must support symmetric key generation and AES-GCM
// encrypt/decrypt.
func TestEmbeddedBoot_KMSEnabled_SymmetricKeyOperations(t *testing.T) {
	srv := bootProductionPostureServer(t, productionPostureConfig(t, true))

	require.NotNil(t, srv.kmsManager, "kms.enabled=true boot must initialize the KMS manager")

	ctx := context.Background()
	meta, keyErr := srv.kmsManager.GenerateKey(ctx, &xkmssdk.KeyAttributes{
		CN:                 "boot-kms-symmetric-regression",
		KeyType:            xkmssdk.KeyTypeEncryption,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	})
	require.NoError(t, keyErr,
		"symmetric key generation through the KMS service layer must succeed when kms.enabled=true")
	require.NotNil(t, meta)

	plaintext := []byte("gate-3 symmetric roundtrip")
	ciphertext, encErr := srv.kmsManager.Encrypt(ctx, "boot-kms-symmetric-regression", plaintext, nil)
	require.NoError(t, encErr, "symmetric encrypt through the KMS service layer must succeed")

	decrypted, decErr := srv.kmsManager.Decrypt(ctx, "boot-kms-symmetric-regression", ciphertext, nil)
	require.NoError(t, decErr, "symmetric decrypt through the KMS service layer must succeed")
	require.True(t, bytes.Equal(plaintext, decrypted), "decrypted plaintext must match")
}

// initializeCA must abort startup with a typed operation-failed error when
// the global xkms registration fails with anything other than the benign
// ErrAlreadyInitialized.
func TestInitializeCA_XKMSInitFailure(t *testing.T) {
	origInitXKMS := initXKMS
	t.Cleanup(func() { initXKMS = origInitXKMS })

	sentinel := errors.New("boom: xkms init rejected")
	initXKMS = func(*dbxkms.Config) error { return sentinel }

	cfg := config.DefaultConfig()
	cfg.Storage.DataDir = t.TempDir()
	cfg.Security.TLSEnabled = true

	srv, err := NewServer(cfg)
	require.NoError(t, err)

	initErr := srv.initializeCA()
	require.Error(t, initErr, "non-benign xkms init failure must abort CA initialization")
	require.Contains(t, initErr.Error(), "initialize XKMS from CA backend")
}
