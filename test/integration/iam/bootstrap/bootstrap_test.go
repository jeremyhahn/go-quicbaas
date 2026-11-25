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

//go:build integration
// +build integration

// Package iam_test contains integration tests for IAM functionality.
// This file covers the first-boot bootstrap flow: a fresh daemon must print
// an activation URL and create a pending admin user. On the second boot it
// must be silent (no second invite).

package bootstrap_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/server"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// ---------------------------------------------------------------------------
// TestBootstrap_FirstBoot_PrintsAdminCertBanner
//
// Boots a real in-process daemon with AllowPasswordAuth=false and
// InsecureDevMode=false (the production defaults). First boot creates the
// platform admin with an mTLS certificate (docs/bootstrap/engineering.md:
// "there are no invite tokens and no activation URL for the platform admin"),
// so the stderr banner must:
//   1. Be headed FIRST-BOOT COMPLETE.
//   2. Name the admin certificate written to <data-dir>/admin/admin.crt.
//   3. Carry no invite/activation URL and no PEM key or certificate material.
// ---------------------------------------------------------------------------

func TestBootstrap_FirstBoot_PrintsAdminCertBanner(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := minimalBootstrapConfig(tmpDir)
	cfg.Auth.AllowPasswordAuth = false
	cfg.Auth.InsecureDevMode = false

	// Redirect stderr to a pipe so we can inspect the banner.
	rdr, wtr, err := os.Pipe()
	require.NoError(t, err)
	origStderr := os.Stderr
	os.Stderr = wtr
	t.Cleanup(func() { os.Stderr = origStderr })

	srv, startErr := startBootstrapServer(t, cfg)
	require.NoError(t, startErr)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	// Close the write end so the read below terminates.
	_ = wtr.Close()
	os.Stderr = origStderr

	var stderrBuf bytes.Buffer
	_, _ = io.Copy(&stderrBuf, rdr)
	stderrOutput := stderrBuf.String()

	adminCertPath := filepath.Join(cfg.Storage.DataDir, "admin", "admin.crt")
	assert.Contains(t, stderrOutput, "FIRST-BOOT COMPLETE: platform admin created.",
		"first-boot banner must appear on stderr on first boot")
	assert.Contains(t, stderrOutput, "Admin mTLS certificate:",
		"banner must announce the issued admin certificate")
	assert.Contains(t, stderrOutput, adminCertPath,
		"banner must name the admin certificate written under the data dir")
	assert.FileExists(t, adminCertPath, "the certificate the banner names must exist")

	// The banner names file paths only: no invite route, no token, and no
	// PEM-encoded key or certificate material.
	assert.NotContains(t, stderrOutput, "/invite/",
		"the platform admin is never given an invite URL")
	assert.NotContains(t, stderrOutput, "#/activate",
		"the platform admin is never given an activation URL")
	assert.NotContains(t, stderrOutput, "token=",
		"banner must not print a token value")
	assert.NotContains(t, stderrOutput, "-----BEGIN",
		"banner must not print key or certificate material")
}

// ---------------------------------------------------------------------------
// TestBootstrap_SecondBoot_Silent
//
// Boots the daemon twice against the same data directory. On the second boot
// no banner must be printed (admin assignment already exists from first boot).
// ---------------------------------------------------------------------------

func TestBootstrap_SecondBoot_Silent(t *testing.T) {
	tmpDir := t.TempDir()

	// First boot — creates the admin assignment.
	{
		cfg := minimalBootstrapConfig(tmpDir)
		cfg.Auth.AllowPasswordAuth = false
		cfg.Auth.InsecureDevMode = false

		srv, startErr := startBootstrapServer(t, cfg)
		require.NoError(t, startErr)

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = srv.Shutdown(ctx)
		cancel()
	}

	// Second boot — capture stderr, expect no banner.
	rdr, wtr, err := os.Pipe()
	require.NoError(t, err)
	origStderr := os.Stderr
	os.Stderr = wtr

	cfg2 := minimalBootstrapConfig(tmpDir)
	cfg2.Auth.AllowPasswordAuth = false
	cfg2.Auth.InsecureDevMode = false

	srv2, startErr := startBootstrapServer(t, cfg2)
	require.NoError(t, startErr)

	_ = wtr.Close()
	os.Stderr = origStderr
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv2.Shutdown(ctx)
	})

	var stderrBuf bytes.Buffer
	_, _ = io.Copy(&stderrBuf, rdr)
	stderrOutput := stderrBuf.String()

	assert.NotContains(t, stderrOutput, "FIRST-BOOT BOOTSTRAP",
		"no bootstrap banner should appear on second boot")
}

// ---------------------------------------------------------------------------
// TestBootstrap_InviteEndpoint_IsPublic
//
// The invite metadata endpoint GET /api/v1/iam/{tenant_id}/invites/{token}
// is on the default-deny public allowlist (pkg/api/rest/authz_default_deny.go):
// an invitee has no credentials yet, so activation MUST be reachable without
// a bearer. Under the default-deny gate any non-public path fails 401/403
// BEFORE routing, so this test sends an unauthenticated request for a random,
// unknown token and asserts the invite handler itself answered: its opaque
// not-found (404, pkg/api/rest/iam_invite.go respondInviteUnusable), and in
// particular NOT 401 or 403, which would mean an allowlist regression.
//
// First boot creates the platform admin with an mTLS certificate and issues
// no invite, so the token is generated here rather than read from the banner.
//
// Unlike the stderr-only tests above, this case needs a real network
// listener, so it runs with Embedded=false (REST enabled) and discovers the
// OS-assigned ephemeral port via srv.RESTAddr() rather than reading the
// configured REST.Port (which stays 0 once the OS picks the real port).
// ---------------------------------------------------------------------------

func TestBootstrap_InviteEndpoint_IsPublic(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := minimalBootstrapConfig(tmpDir)
	cfg.Auth.AllowPasswordAuth = false
	cfg.Auth.InsecureDevMode = false

	// Start a real REST listener (not the in-process embedded transport) so
	// the invite endpoint is reachable over HTTP(S). REST.Port stays 0 so
	// the OS assigns a free ephemeral port, avoiding collisions with siblings.
	cfg.Server.Embedded = false
	cfg.Server.REST.Enabled = true

	srv, startErr := startBootstrapServer(t, cfg)
	require.NoError(t, startErr)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	// A random token no invite was ever issued for.
	raw := make([]byte, 32)
	_, err := rand.Read(raw)
	require.NoError(t, err)
	token := hex.EncodeToString(raw)

	// Resolve the REAL bound address. With REST.Port=0 the kernel picks an
	// ephemeral port; cfg.Server.REST.Port still reads 0, so we must ask the
	// running listener for its actual address.
	restAddr := srv.RESTAddr()
	require.NotNil(t, restAddr, "REST listener must be bound when Embedded=false and REST is enabled")

	// The daemon enables TLS by default (Security.TLSEnabled), so the REST
	// transport speaks HTTPS. daemonCAHTTPClient pins the daemon's own
	// auto-issued CA (it is not a cluster node) and verifies its 127.0.0.1 leaf.
	scheme := "https"
	if !cfg.Security.TLSEnabled {
		scheme = "http"
	}
	baseURL := fmt.Sprintf("%s://%s", scheme, restAddr.String())
	inviteURL := baseURL + "/api/v1/iam/" + iam.SystemTenantID + "/invites/" + token

	// No Authorization header: the invitee has no credentials.
	resp, httpErr := daemonCAHTTPClient(t, srv).Get(inviteURL)
	require.NoError(t, httpErr)
	defer resp.Body.Close()

	assert.NotEqual(t, http.StatusUnauthorized, resp.StatusCode,
		"the public invite endpoint must not demand authentication (allowlist regression)")
	assert.NotEqual(t, http.StatusForbidden, resp.StatusCode,
		"the public invite endpoint must not be forbidden (allowlist regression)")
	assert.Equalf(t, http.StatusNotFound, resp.StatusCode,
		"an unknown token must reach the invite handler and get its not-found answer")

	// The router also answers 404 for an unmatched route, so the status alone
	// does not prove the invite handler ran. Its not-found answer is a JSON
	// IAM error with a fixed code and description; a 404 without this body
	// means the route never reached the invite handler.
	const notInviteHandler = "a 404 without the invite handler's JSON not-found body means the route never reached the invite handler"
	assert.Contains(t, resp.Header.Get("Content-Type"), "application/json", notInviteHandler)
	var body struct {
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body), notInviteHandler)
	// "not_found" is rest.ErrNotFound (pkg/api/rest/iam_errors.go), which is
	// built only under the rest tag and so cannot be imported here.
	assert.Equal(t, "not_found", body.Error, notInviteHandler)
	assert.Equal(t, "invite link is not valid", body.ErrorDescription, notInviteHandler)
}

// ---------------------------------------------------------------------------
// TestBootstrap_PIVCertPath_BindsAdmin_NoInviteBanner
//
// QuicBaaS is passwordless: the legacy InsecureDevMode password-admin
// bootstrap was retired with the hardening that removed password login
// (Auth.BootstrapAdminUsername/Password are no longer consumed anywhere).
// The current NO-BANNER bootstrap contract is path (d): when
// Auth.BootstrapAdminPIVCertPath is configured, firstBootBootstrap binds the
// certificate's thumbprint to the bootstrap admin non-interactively and
// SKIPS the three-option ceremony banner entirely. This is the exact path
// the integration harness uses (test/integration/quicbaas.yaml
// auth.bootstrap_admin_piv_cert_path).
// ---------------------------------------------------------------------------

func TestBootstrap_PIVCertPath_BindsAdmin_NoInviteBanner(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := minimalBootstrapConfig(tmpDir)
	cfg.Auth.AllowPasswordAuth = false
	cfg.Auth.InsecureDevMode = false
	cfg.Auth.BootstrapAdminPIVCertPath = writeSelfSignedAdminCert(t, tmpDir)

	rdr, wtr, err := os.Pipe()
	require.NoError(t, err)
	origStderr := os.Stderr
	os.Stderr = wtr

	srv, startErr := startBootstrapServer(t, cfg)
	// Restore stderr before asserting so a failure message is visible.
	_ = wtr.Close()
	os.Stderr = origStderr
	require.NoError(t, startErr,
		"server must boot and bind the bootstrap admin PIV cert non-interactively")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	})

	var stderrBuf bytes.Buffer
	_, _ = io.Copy(&stderrBuf, rdr)
	stderrOutput := stderrBuf.String()

	assert.NotContains(t, stderrOutput, "FIRST-BOOT BOOTSTRAP",
		"ceremony banner must NOT appear when the bootstrap admin is bound to a PIV cert")
	assert.NotContains(t, stderrOutput, "/ui/#/invite/",
		"no activation URL may be printed on the PIV cert-bind bootstrap path")
}

// writeSelfSignedAdminCert mints a throwaway self-signed P-256 client
// certificate and writes it as PEM into dir, returning the file path. Only
// the certificate's SPKI/DER thumbprint is consumed by bindAdminPIVCert, so a
// self-signed leaf is sufficient — no CA chain is validated on this path.
func writeSelfSignedAdminCert(t *testing.T, dir string) string {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err, "generate P-256 key")

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	require.NoError(t, err, "generate serial")

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"QuicBaaS Integration Tests"},
			CommonName:   "bootstrap-admin",
		},
		NotBefore:   now.Add(-time.Minute),
		NotAfter:    now.AddDate(0, 0, 1),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	require.NoError(t, err, "create self-signed cert")

	path := filepath.Join(dir, "bootstrap-admin.crt")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	require.NoError(t, os.WriteFile(path, pemBytes, 0o600), "write cert PEM")
	return path
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// minimalBootstrapConfig creates a minimal server config for bootstrap tests.
// IAM is enabled; all other optional services are disabled to keep startup fast.
func minimalBootstrapConfig(dataDir string) *config.Config {
	cfg := config.DefaultConfig()
	cfg.IAM.SPIFFETrustDomain = helpers.SPIFFETrustDomain
	cfg.Storage.DataDir = filepath.Join(dataDir, "data")
	cfg.Storage.WALDir = filepath.Join(dataDir, "wal")
	cfg.Raft.NodeID = 1

	// Use a random-ish port to avoid collisions between parallel tests.
	cfg.Server.REST.Port = 0 // Let the OS pick a free port when port is 0.

	cfg.KMS.Enabled = false
	cfg.Secrets.Enabled = false
	cfg.Vector.Enabled = false
	cfg.TimeSeries.Enabled = false
	cfg.Logging.Level = "error"
	cfg.Server.Embedded = true

	return cfg
}

// startBootstrapServer creates and starts a Server from cfg, waits for it to
// be ready, and returns. The caller is responsible for shutting it down.
//
// In embedded mode Server.Start returns as soon as initialization (including
// the first-boot bootstrap banner) finishes, so we start synchronously. In
// non-embedded mode Start blocks until shutdown, so we run it in a goroutine
// and wait for the REST listener to come up — which the daemon binds at the
// tail of the same synchronous initialization that prints the banner, so a
// bound listener implies the banner has already been emitted.
func startBootstrapServer(t *testing.T, cfg *config.Config) (*server.Server, error) {
	t.Helper()

	srv, err := server.NewServer(cfg)
	if err != nil {
		return nil, fmt.Errorf("NewServer: %w", err)
	}

	if cfg.Server.Embedded {
		if err := srv.Start(); err != nil {
			return nil, fmt.Errorf("Start: %w", err)
		}
		if err := waitForStarted(t, srv, nil, false); err != nil {
			return nil, err
		}
		return srv, nil
	}

	// Non-embedded: Start blocks until shutdown, so run it in a goroutine and
	// surface an early startup failure over a channel.
	startErrCh := make(chan error, 1)
	go func() {
		if err := srv.Start(); err != nil {
			startErrCh <- err
		}
	}()

	if err := waitForStarted(t, srv, startErrCh, true); err != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		return nil, err
	}
	return srv, nil
}

// waitForStarted blocks until the server reports started and, when requireREST
// is set (non-embedded mode), until the REST listener has bound a real address.
// It fails fast if the goroutine-driven Start reports an error on startErrCh
// (which is nil in embedded mode, where Start returns synchronously).
func waitForStarted(t *testing.T, srv *server.Server, startErrCh <-chan error, requireREST bool) error {
	t.Helper()

	deadline := time.Now().Add(15 * time.Second)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()

	ready := func() bool {
		if !srv.IsStarted() {
			return false
		}
		if requireREST {
			return srv.RESTAddr() != nil
		}
		return true
	}

	for {
		if ready() {
			return nil
		}
		select {
		case err := <-startErrCh:
			return fmt.Errorf("Start: %w", err)
		case <-tick.C:
			if time.Now().After(deadline) {
				return fmt.Errorf("server did not start within timeout")
			}
		}
	}
}

// daemonCAHTTPClient returns an http.Client that trusts the in-process
// bootstrap daemon's OWN auto-issued CA (srv.CA()), pinned via RootCAs.
//
// The bootstrap daemon is NOT a cluster node — it boots from config.DefaultConfig
// with a self-bootstrapped PKI, so the integration cluster CA (helpers.NewHTTPClient,
// which pins /etc/quicbaas/certs/ca.crt) cannot verify it. The daemon binds
// 127.0.0.1 (config default) and its serving leaf carries the 127.0.0.1 SAN —
// the same loopback trust path the daemon uses to call itself — so RootCAs +
// ServerName "127.0.0.1" verify cleanly without InsecureSkipVerify.
func daemonCAHTTPClient(t *testing.T, srv *server.Server) *http.Client {
	t.Helper()
	pool, err := srv.CA().CABundleCertPool()
	require.NoError(t, err, "load in-process daemon CA bundle pool")
	return &http.Client{
		Timeout: 10 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs:    pool,
				ServerName: "127.0.0.1",
				MinVersion: tls.VersionTLS13,
			},
		},
	}
}

// Ensure the iam package is used (invite types are referenced in doc comments).
// The in-memory store now lives in pkg/iam/iamtest: quicbaas keeps no invite
// store implementation in production code, since go-qrdb owns token
// generation, digesting, single-use redemption, and tenant routing.
var _ iam.InviteStore = (*iamtest.MemoryInviteStore)(nil)
