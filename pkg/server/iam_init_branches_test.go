//go:build !integration

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

// This file extends iam_init_test.go and iam_bootstrap_test.go to cover the
// branches that the audit identified as 0% coverage under -tags iam:
// adapter passthroughs (interfacesAuthManagerAdapter), the orchestrator
// daemon-client read/write helpers, PIVBootstrapError formatting, the PIV
// cert binding edge cases, and the getter passthroughs that no other test
// file exercises (GetAppStorage / GetFederationManager / GetInviteStore /
// IsPasswordAuthEnabled).

package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam/iamtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil"
)

// ---------------------------------------------------------------------------
// inMemoryAuthManagerAdapter.IsPasswordAuthEnabled
//
// The adapter hard-codes true today because the underlying manager validates
// internally. We still want a test so removing the constant return does not
// silently break the contract.
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// iamComponents getter passthroughs that the older test only covers with
// nil receivers. These tests confirm the non-nil-set branches return the
// instance verbatim.
// ---------------------------------------------------------------------------

func TestIAMComponents_GetAppStorage_ReturnsBackend(t *testing.T) {
	mem := iam.NewMemoryStorage()
	c := &iamComponents{appStorage: mem}
	got := c.GetAppStorage()
	assert.NotNil(t, got)
	assert.Same(t, mem, got)
}

func TestIAMComponents_GetFederationManager_NonNilOrNilByField(t *testing.T) {
	t.Run("nil when unset", func(t *testing.T) {
		c := &iamComponents{}
		assert.Nil(t, c.GetFederationManager())
	})

	t.Run("returns the configured manager", func(t *testing.T) {
		// Use a minimal FederationManager wired with an in-memory identity
		// store so the constructor's validation succeeds without touching
		// network or persistent storage backends.
		mem := iam.NewMemoryStorage()
		ids := iam.NewIdentityStore(iam.IdentityStoreOptions{
			UserStorage:  mem,
			GroupStorage: mem,
			RoleStorage:  mem,
		})
		fedMgr, err := iam.NewFederationManager(
			iam.WithProviderStorage(iam.NewMemoryProviderStorage()),
			iam.WithLinkStorage(iam.NewMemoryFederationStorage()),
			iam.WithStateStorage(iam.NewMemoryStateStorage()),
			iam.WithFederationIdentityStore(ids),
		)
		require.NoError(t, err)
		c := &iamComponents{federationManager: fedMgr}
		assert.Same(t, fedMgr, c.GetFederationManager())
	})
}

func TestIAMComponents_GetInviteStore_ReturnsBackend(t *testing.T) {
	inv := iamtest.NewMemoryInviteStore()
	c := &iamComponents{inviteStore: inv}
	assert.Same(t, inv, c.GetInviteStore())
}

// ---------------------------------------------------------------------------
// PIVBootstrapError formatting / Unwrap
// ---------------------------------------------------------------------------

func TestPIVBootstrapError_Format(t *testing.T) {
	t.Run("formats with cause", func(t *testing.T) {
		cause := errors.New("decode failed")
		err := &PIVBootstrapError{Op: "read PIV cert", Path: "/etc/x.pem", Cause: cause}
		got := err.Error()
		assert.Contains(t, got, "read PIV cert")
		assert.Contains(t, got, "/etc/x.pem")
		assert.Contains(t, got, "decode failed")
		assert.Same(t, cause, err.Unwrap())
	})

	t.Run("formats without cause", func(t *testing.T) {
		err := &PIVBootstrapError{Op: "decode PIV cert PEM", Path: "/tmp/x.pem"}
		got := err.Error()
		assert.Contains(t, got, "decode PIV cert PEM")
		assert.Contains(t, got, "/tmp/x.pem")
		assert.Nil(t, err.Unwrap())
	})
}

// ---------------------------------------------------------------------------
// bindAdminPIVCert direct branch tests
//
// Direct tests of bindAdminPIVCert (the unexported helper inside iam_init.go)
// cover its three typed error paths.
// ---------------------------------------------------------------------------

func TestBindAdminPIVCert_PathNotExistReturnsTypedError(t *testing.T) {
	c := buildTestIAMComponents(t)

	err := bindAdminPIVCert(context.Background(), c, "default", "admin", "/nonexistent/path/missing.pem")
	require.Error(t, err)
	var piv *PIVBootstrapError
	require.True(t, errors.As(err, &piv), "expected PIVBootstrapError, got %T %v", err, err)
	assert.Equal(t, "read PIV cert", piv.Op)
}

func TestBindAdminPIVCert_MalformedPEMReturnsTypedError(t *testing.T) {
	c := buildTestIAMComponents(t)

	tmp := filepath.Join(t.TempDir(), "bad.pem")
	require.NoError(t, os.WriteFile(tmp, []byte("not a pem block"), 0o600))

	err := bindAdminPIVCert(context.Background(), c, "default", "admin", tmp)
	require.Error(t, err)
	var piv *PIVBootstrapError
	require.True(t, errors.As(err, &piv))
	assert.Equal(t, "decode PIV cert PEM", piv.Op)
}

func TestBindAdminPIVCert_BadX509ReturnsTypedError(t *testing.T) {
	c := buildTestIAMComponents(t)

	// A PEM block whose bytes are not valid DER yields x509.ParseCertificate failure.
	tmp := filepath.Join(t.TempDir(), "bad.pem")
	pemBytes := []byte(`-----BEGIN CERTIFICATE-----
AAAA
-----END CERTIFICATE-----
`)
	require.NoError(t, os.WriteFile(tmp, pemBytes, 0o600))

	err := bindAdminPIVCert(context.Background(), c, "default", "admin", tmp)
	require.Error(t, err)
	var piv *PIVBootstrapError
	require.True(t, errors.As(err, &piv))
	assert.Equal(t, "parse PIV cert", piv.Op)
}

func TestBindAdminPIVCert_HappyPathUpdatesUser(t *testing.T) {
	c, certFactors := buildTestIAMComponentsWithFactors(t)
	adminID := createBootstrapAdmin(t, c)

	// Write a valid self-signed cert into a temp file.
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "admin"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	pemFile := filepath.Join(t.TempDir(), "cert.pem")
	require.NoError(t, os.WriteFile(pemFile, pemFromDER(der), 0o600))

	err = bindAdminPIVCert(context.Background(), c, "default", adminID, pemFile)
	require.NoError(t, err)

	user, err := c.identityStore.GetUser(context.Background(), "default", adminID)
	require.NoError(t, err)
	assert.NotEmpty(t, user.PIVThumbprint(), "PIV-attested binding must be set")
	assert.Equal(t, iam.LifecycleActive, user.Lifecycle)

	// Regression guard for the split-store bug: the bound certificate must ALSO
	// be resolvable as an mtls auth factor, under BOTH keys
	// pkg/service.LoginService.FinishLogin sends. Without this the daemon
	// answered `auth login --method mtls` with 401 "factor not found".
	cert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	assertCertLoginFactorEnrolled(t, certFactors, cert)
}

// assertCertLoginFactorEnrolled asserts the certificate resolves to an mtls
// factor under the renewal-stable SPKI key AND the legacy cert-DER key — the two
// lookup keys the login facade sends in one FinishLogin call.
func assertCertLoginFactorEnrolled(t *testing.T, factors *testutil.MemoryUserAuthFactors, cert *x509.Certificate) {
	t.Helper()
	ctx := context.Background()

	bySPKI, err := factors.LookupByCredentialKey(ctx, qrdbsdk.AuthFactorMTLS, iam.MTLSSPKICredentialKey(cert))
	require.NoError(t, err, "the bound certificate must resolve by its SPKI lookup key")
	require.NotNil(t, bySPKI)

	byCert, err := factors.LookupByCredentialKey(ctx, qrdbsdk.AuthFactorMTLS, iam.MTLSCredentialKey(cert))
	require.NoError(t, err, "the bound certificate must resolve by its cert-DER lookup key")
	require.Equal(t, bySPKI.ID, byCert.ID, "both lookup keys must resolve the same factor")
	require.Equal(t, qrdbsdk.AuthFactorMTLS, bySPKI.Type)
}

// createBootstrapAdmin seeds a minimal admin user under tenant "default" so the
// bindAdminPIVCert helper's UpdateUser call has a target to mutate. It returns
// the id the STORE assigned: a caller no longer chooses one, because the
// identity is the store's Snowflake and nothing correlates a second id space
// against it.
func createBootstrapAdmin(t *testing.T, c *iamComponents) string {
	t.Helper()
	u, err := c.identityStore.CreateUser(context.Background(), "default", iam.CreateUserOptions{
		Username: "admin-bs",
		Name:     "admin",
		Email:    "admin-bs@default.qrdb.local",
	})
	require.NoError(t, err)
	return u.ID
}

// pemFromDER mimics x509.EncodeToMemory(&pem.Block{...}) without taking the
// extra dependency on encoding/pem here — the test only needs a valid block
// header that x509.ParseCertificate's caller (pem.Decode) accepts.
func pemFromDER(der []byte) []byte {
	const begin = "-----BEGIN CERTIFICATE-----\n"
	const end = "\n-----END CERTIFICATE-----\n"
	enc := stdEncode64(der)
	out := make([]byte, 0, len(begin)+len(enc)+len(end))
	out = append(out, begin...)
	out = append(out, enc...)
	out = append(out, end...)
	return out
}

func stdEncode64(b []byte) []byte {
	const tbl = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var pad byte = '='
	n := len(b)
	out := make([]byte, 0, ((n+2)/3)*4)
	for i := 0; i < n; i += 3 {
		var v uint32
		v = uint32(b[i]) << 16
		if i+1 < n {
			v |= uint32(b[i+1]) << 8
		}
		if i+2 < n {
			v |= uint32(b[i+2])
		}
		out = append(out, tbl[(v>>18)&0x3F])
		out = append(out, tbl[(v>>12)&0x3F])
		if i+1 < n {
			out = append(out, tbl[(v>>6)&0x3F])
		} else {
			out = append(out, pad)
		}
		if i+2 < n {
			out = append(out, tbl[v&0x3F])
		} else {
			out = append(out, pad)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// readOrchestratorDaemonClient
// ---------------------------------------------------------------------------

func TestReadOrchestratorDaemonClient_MissingFile(t *testing.T) {
	got, err := readOrchestratorDaemonClient(filepath.Join(t.TempDir(), "no-such-file.json"))
	require.NoError(t, err, "missing file must yield (nil, nil) so caller can re-bootstrap")
	assert.Nil(t, got)
}

func TestReadOrchestratorDaemonClient_MalformedJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	require.NoError(t, os.WriteFile(path, []byte("{not-json"), 0o600))

	got, err := readOrchestratorDaemonClient(path)
	require.Error(t, err)
	assert.Nil(t, got)
}

func TestReadOrchestratorDaemonClient_MissingRequiredFields(t *testing.T) {
	t.Run("missing ClientID", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "partial.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"ClientSecret":"abc"}`), 0o600))

		got, err := readOrchestratorDaemonClient(path)
		require.Error(t, err)
		assert.Nil(t, got)
		assert.Contains(t, err.Error(), "missing required fields")
	})

	t.Run("missing ClientSecret", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "partial.json")
		require.NoError(t, os.WriteFile(path, []byte(`{"ClientID":"abc"}`), 0o600))

		got, err := readOrchestratorDaemonClient(path)
		require.Error(t, err)
		assert.Nil(t, got)
	})
}

func TestReadOrchestratorDaemonClient_HappyPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ok.json")
	payload := `{"ClientID":"id-1","ClientSecret":"sec-1","Scopes":["a","b"],"TenantID":"default"}`
	require.NoError(t, os.WriteFile(path, []byte(payload), 0o600))

	got, err := readOrchestratorDaemonClient(path)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "id-1", got.ClientID)
	assert.Equal(t, "sec-1", got.ClientSecret)
	assert.Equal(t, "default", got.TenantID)
	assert.Equal(t, []string{"a", "b"}, got.Scopes)
}

// ---------------------------------------------------------------------------
// writeOrchestratorDaemonClient
// ---------------------------------------------------------------------------

func TestWriteOrchestratorDaemonClient_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "orchestrator_daemon_client.json")
	want := &OrchestratorDaemonClient{
		ClientID:     "id-2",
		ClientSecret: "sec-2",
		Scopes:       []string{"x"},
		TenantID:     "system",
	}
	require.NoError(t, writeOrchestratorDaemonClient(path, want))

	// Verify file mode bits are 0600 on the final file (helps regression
	// against accidental loosening of the credential file's permissions).
	info, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	// Round-trip read.
	got, err := readOrchestratorDaemonClient(path)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, want.ClientID, got.ClientID)
	assert.Equal(t, want.ClientSecret, got.ClientSecret)
	assert.Equal(t, want.TenantID, got.TenantID)
	assert.Equal(t, want.Scopes, got.Scopes)

	// And the on-disk JSON is well-formed.
	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	var decoded OrchestratorDaemonClient
	require.NoError(t, json.Unmarshal(raw, &decoded))
}

func TestWriteOrchestratorDaemonClient_MkdirAllFails(t *testing.T) {
	// Make the parent path a regular file so MkdirAll fails with "not a
	// directory". This exercises the MkdirAll error branch without
	// touching system mount points.
	dir := t.TempDir()
	blockerFile := filepath.Join(dir, "blocker")
	require.NoError(t, os.WriteFile(blockerFile, []byte("x"), 0o600))

	target := filepath.Join(blockerFile, "child", "orchestrator_daemon_client.json")
	err := writeOrchestratorDaemonClient(target, &OrchestratorDaemonClient{
		ClientID:     "x",
		ClientSecret: "y",
	})
	require.Error(t, err, "MkdirAll over a regular file must fail")
}

// ---------------------------------------------------------------------------
// firstBootBootstrap branch coverage: PIV bootstrap path
// ---------------------------------------------------------------------------

func TestFirstBootBootstrap_PIVBootstrapPath_SkipsBanner(t *testing.T) {
	srv := withTempServer(t)
	c := buildTestIAMComponents(t)

	// Write a valid PEM cert to the configured PIV path so the bootstrap
	// can complete the non-interactive binding instead of printing the
	// banner.
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "admin"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	pemFile := filepath.Join(t.TempDir(), "admin.pem")
	require.NoError(t, os.WriteFile(pemFile, pemFromDER(der), 0o600))

	srv.config.Auth.BootstrapAdminPIVCertPath = pemFile

	var bannerCalled bool
	origPrint := writeFirstBootBannerFn
	writeFirstBootBannerFn = func(_ interface{ WriteString(string) (int, error) }, _ adminEnrollmentBanner) {
		bannerCalled = true
	}
	defer func() { writeFirstBootBannerFn = origPrint }()

	require.NoError(t, srv.firstBootBootstrap(context.Background(), c))
	assert.False(t, bannerCalled, "PIV bootstrap path must NOT print the banner")

	// Bootstrap admin must be active (PIV thumbprint bound) and have a
	// thumbprint persisted.
	user, err := c.identityStore.GetUser(context.Background(), "system", srv.bootstrapAdminUserID)
	require.NoError(t, err)
	assert.Equal(t, iam.LifecycleActive, user.Lifecycle)
	assert.NotEmpty(t, user.PIVThumbprint())
}

func TestFirstBootBootstrap_PIVBootstrapPath_InvalidCertReturnsTypedError(t *testing.T) {
	srv := withTempServer(t)
	c := buildTestIAMComponents(t)
	srv.config.Auth.BootstrapAdminPIVCertPath = "/no/such/file.pem"

	origPrint := writeFirstBootBannerFn
	writeFirstBootBannerFn = func(_ interface{ WriteString(string) (int, error) }, _ adminEnrollmentBanner) {}
	defer func() { writeFirstBootBannerFn = origPrint }()

	err := srv.firstBootBootstrap(context.Background(), c)
	require.Error(t, err)
	var fb *FirstBootBootstrapError
	require.True(t, errors.As(err, &fb))
	assert.Contains(t, fb.Op, "bind bootstrap admin PIV cert")
}

// ---------------------------------------------------------------------------
// getIssuerURL: explicit per-branch tests for the host/port/scheme branches.
// This file extends TestServer_GetIssuerURL in iam_components_test.go which
// already covers most permutations; we add the empty-host and
// custom-host branches with assertions on the precise emitted string.
// ---------------------------------------------------------------------------

func TestServer_GetIssuerURL_TLSDisabledEmitsHTTP(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Security.TLSEnabled = false
	srv.config.Server.Host = "10.0.0.1"
	srv.config.Server.REST.Port = 7000
	url := srv.getIssuerURL()
	assert.Equal(t, "http://10.0.0.1:7000", url)
}

func TestServer_GetIssuerURL_EmptyHostFallsBackToLocalhost(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Security.TLSEnabled = false
	srv.config.Server.Host = ""
	srv.config.Server.REST.Port = 7001
	url := srv.getIssuerURL()
	assert.Equal(t, "http://localhost:7001", url)
}

func TestServer_GetIssuerURL_ZeroPortFallsBack(t *testing.T) {
	srv := withTempServer(t)
	srv.config.Security.TLSEnabled = true
	srv.config.Server.Host = "host1"
	srv.config.Server.REST.Port = 0
	url := srv.getIssuerURL()
	assert.Equal(t, "https://host1:8443", url)
}

// TestReadOrchestratorDaemonClient_ReadErrorNotMissing covers the
// read-error-that-is-not-os.ErrNotExist branch: pointing the loader at a
// directory makes os.ReadFile fail with a non-ENOENT error, which must be
// surfaced (not swallowed as a missing-file (nil,nil)).
func TestReadOrchestratorDaemonClient_ReadErrorNotMissing(t *testing.T) {
	dir := t.TempDir() // a directory, not a regular file
	got, err := readOrchestratorDaemonClient(dir)
	require.Error(t, err, "reading a directory must surface a non-ENOENT error")
	assert.Nil(t, got)
}

// TestBindAdminPIVCert_UpdateUserError covers the UpdateUser error branch of
// bindAdminPIVCert: when the target user does not exist, the identity store's
// UpdateUser fails and bindAdminPIVCert wraps it in a typed PIVBootstrapError.
func TestBindAdminPIVCert_UpdateUserError(t *testing.T) {
	c := buildTestIAMComponents(t)
	// Deliberately do NOT create the bootstrap admin, so UpdateUser fails.

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "admin"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	pemFile := filepath.Join(t.TempDir(), "cert.pem")
	require.NoError(t, os.WriteFile(pemFile, pemFromDER(der), 0o600))

	// A MALFORMED id is refused before anything is enrolled. The user id is
	// qrdb's Snowflake now, so "does-not-exist" never named a user in any
	// tenant, and the factor enrollment that converts it is the first step to
	// say so — which is the right place, because it runs before any write.
	err = bindAdminPIVCert(context.Background(), c, "default", "does-not-exist", pemFile)
	require.Error(t, err, "a malformed user id must fail")
	var malformed *PIVBootstrapError
	require.ErrorAs(t, err, &malformed)
	require.Equal(t, "enroll bootstrap admin mtls login factor", malformed.Op)

	// A WELL-FORMED id that names no user gets as far as the update, which is
	// the step that actually consults the record.
	err = bindAdminPIVCert(context.Background(), c, "default", "999999999999999999", pemFile)
	require.Error(t, err, "updating a nonexistent user must fail")
	var missing *PIVBootstrapError
	require.ErrorAs(t, err, &missing)
	require.Equal(t, "update bootstrap admin with PIV thumbprint", missing.Op)
}

// TestBindAdminPIVCert_EnrollFailureLeavesNoBinding pins the transactional
// contract: the mtls login factor is enrolled BEFORE the user-side binding, so a
// failed enrollment leaves the user completely untouched. The inverse order
// would persist a binding whose certificate can never complete a login — exactly
// the defect the enrollment closes.
func TestBindAdminPIVCert_EnrollFailureLeavesNoBinding(t *testing.T) {
	c, certFactors := buildTestIAMComponentsWithFactors(t)
	adminID := createBootstrapAdmin(t, c)
	certFactors.AddErr = errors.New("factor store unavailable")

	pemFile := writeTestCertPEM(t)

	err := bindAdminPIVCert(context.Background(), c, "default", adminID, pemFile)
	require.Error(t, err, "a factor-enrollment failure must fail the binding closed")
	var pivErr *PIVBootstrapError
	require.ErrorAs(t, err, &pivErr)
	require.Equal(t, "enroll bootstrap admin mtls login factor", pivErr.Op)

	user, err := c.identityStore.GetUser(context.Background(), "default", adminID)
	require.NoError(t, err)
	require.Empty(t, user.PIVThumbprint(), "no binding may be persisted when the login factor could not be enrolled")
	require.NotEqual(t, iam.LifecycleActive, user.Lifecycle,
		"the user must not be activated by a binding that failed")
}

// TestBindAdminPIVCert_RebindingSameCertIsIdempotent covers the repeat path: the
// bootstrap binding re-runs across ceremonies and restarts, so binding the same
// certificate twice must succeed and leave exactly one login factor.
func TestBindAdminPIVCert_RebindingSameCertIsIdempotent(t *testing.T) {
	c, certFactors := buildTestIAMComponentsWithFactors(t)
	adminID := createBootstrapAdmin(t, c)

	pemFile := writeTestCertPEM(t)

	require.NoError(t, bindAdminPIVCert(context.Background(), c, "default", adminID, pemFile))
	require.NoError(t, bindAdminPIVCert(context.Background(), c, "default", adminID, pemFile),
		"re-binding the same certificate must be idempotent")

	// The user id IS qrdb's numeric id, so this is a conversion rather than a
	// lookup in a correlation table that could disagree with the record.
	qrdbUserID, err := iam.ParseUserID(adminID)
	require.NoError(t, err)
	list, err := certFactors.List(context.Background(), qrdbUserID)
	require.NoError(t, err)
	require.Len(t, list, 1, "re-binding must not enroll a duplicate factor")
}

// TestBindAdminPIVCert_MissingEnrollerFailsClosed ensures a server wired without
// the factor enroller refuses to bind rather than silently writing a binding
// that cannot log in.
func TestBindAdminPIVCert_MissingEnrollerFailsClosed(t *testing.T) {
	c := buildTestIAMComponents(t)
	adminID := createBootstrapAdmin(t, c)
	c.certLoginFactors = nil

	err := bindAdminPIVCert(context.Background(), c, "default", "admin-bs", writeTestCertPEM(t))
	require.Error(t, err)
	var unavailable *AdminCertFactorUnavailableError
	require.ErrorAs(t, err, &unavailable)

	user, err := c.identityStore.GetUser(context.Background(), "default", adminID)
	require.NoError(t, err)
	require.Empty(t, user.PIVThumbprint(), "an unwired enroller must not produce a binding")
}

// writeTestCertPEM issues a self-signed certificate and returns the path of its
// PEM file.
func writeTestCertPEM(t *testing.T) string {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: "admin"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "cert.pem")
	require.NoError(t, os.WriteFile(path, pemFromDER(der), 0o600))
	return path
}
