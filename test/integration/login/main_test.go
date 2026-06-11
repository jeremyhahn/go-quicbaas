// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

//go:build integration && rest

// Package login contains end-to-end integration tests for the unified login
// ceremony (BeginLogin / FinishLogin) that go-qrdb owns and QuicBaaS composes
// and surfaces across every transport (REST, gRPC, QUIC, MCP, Unix, embedded).
//
// The suite drives REAL crypto against the REAL in-container cluster brought
// up by `make integration-test-login`:
//
//   - mTLS  — a client certificate is presented on the network TLS handshake;
//     the server derives the login lookup key from the VERIFIED peer cert
//     (SHA-256 of the certificate DER), NEVER from the request body. The proof
//     is the handshake itself.
//   - PIV   — a software P-256 key + a certificate chained to the cluster CA is
//     enrolled as a piv factor (Credential = SHA-256(certDER), Metadata
//     ["cert_pem"] = the leaf PEM). BeginLogin returns a server nonce; the
//     test signs SHA-256(nonce) with the P-256 key (ECDSA ASN.1) exactly as a
//     real PIV slot would, and FinishLogin verifies it via xkms's
//     PIVHardwareFactorVerifier against the cluster CA.
//
// All factor enrollment writes to go-qrdb's UserAuthFactor store (the SAME
// store the login resolver reads via FactorStore.LookupByCredentialKey), and
// every login mints an OIDC bearer minted by the qrdb LoginClient. The suite
// asserts the resolved (subject, tenant) is IDENTICAL across transports and
// that the issued bearer validates against the cluster's /oauth2/userinfo
// endpoint.
//
// WebAuthn / FIDO2 end-to-end in-container requires a virtual CTAP2
// authenticator, which is not available in the integration image. The
// server-side WebAuthn verification path is covered at the unit level
// (pkg/service/login_service_test.go forwards fido2 proofs unchanged;
// go-xkms pkg/auth_factors/webauthn.go covers the verifier) and the
// vendor-agnostic client-side assertion plumbing is covered in
// cmd/quicbaas/canonical_auth_login_fido2_test.go. See security_test.go
// (TestUnifiedLogin_WebAuthnInContainerDeferred) for the explicit, logged
// statement of what is covered vs deferred — there are NO silent gaps.
package login

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/test/integration/helpers"
)

// =============================================================================
// Environment constants
// =============================================================================

// systemTenantID is go-qrdb's reserved system tenant. The integration cluster
// bootstraps the admin user there; the suite enrolls its test factors against
// users it creates in this same tenant so the qrdb login resolver scopes them
// correctly.
const systemTenantID = "system"

// adminCertEnv / adminKeyEnv / caCertEnv / caKeyEnv override the default
// in-container cert locations. The integration compose mounts ./certs (which
// gen-certs populated) read-only at /etc/quicbaas/certs on every node AND the
// test runner, so the defaults work out of the box inside the devcontainer.
const (
	adminCertPath = "/etc/quicbaas/certs/admin.crt"
	adminKeyPath  = "/etc/quicbaas/certs/admin.key"
	caCertPath    = "/etc/quicbaas/certs/ca.crt"
	caKeyPath     = "/etc/quicbaas/certs/ca.key"
)

// =============================================================================
// Typed errors
// =============================================================================

// CertLoadError is returned when a PEM certificate or key cannot be read or
// parsed from disk.
type CertLoadError struct {
	Path  string
	Cause error
}

func (e *CertLoadError) Error() string {
	return fmt.Sprintf("login-test: load %s: %v", e.Path, e.Cause)
}

func (e *CertLoadError) Unwrap() error { return e.Cause }

// CASignError is returned when signing a test leaf certificate with the
// cluster CA fails.
type CASignError struct {
	Op    string
	Cause error
}

func (e *CASignError) Error() string {
	return fmt.Sprintf("login-test: ca-sign %s: %v", e.Op, e.Cause)
}

func (e *CASignError) Unwrap() error { return e.Cause }

// BearerValidationError is returned when an issued bearer does not validate
// against the cluster's userinfo endpoint or its claims do not match the
// expected identity.
type BearerValidationError struct {
	Reason string
}

func (e *BearerValidationError) Error() string {
	return "login-test: bearer validation: " + e.Reason
}

// =============================================================================
// Cert / key helpers
// =============================================================================

// adminCertPaths resolves the admin client cert + key + CA paths, honoring env
// overrides so the suite can run outside the default in-container layout.
func adminCertPaths() (cert, key, ca string) {
	cert = adminCertPath
	if v := os.Getenv("QUICBAAS_TEST_PIV_CERT"); v != "" {
		cert = v
	}
	key = adminKeyPath
	if v := os.Getenv("QUICBAAS_TEST_PIV_KEY"); v != "" {
		key = v
	}
	ca = caCertPath
	if v := os.Getenv("QUICBAAS_TEST_PIV_CA"); v != "" {
		ca = v
	}
	return cert, key, ca
}

// caKeyFile resolves the cluster CA private key path. The integration compose
// mounts the whole certs dir (including ca.key) read-only, so the test can
// mint piv leaf certificates that chain to the cluster CA — exactly what the
// server-side PIVHardwareFactorVerifier requires for chain validation.
func caKeyFile() string {
	if v := os.Getenv("QUICBAAS_TEST_CA_KEY"); v != "" {
		return v
	}
	return caKeyPath
}

// loadClientCert loads the admin client certificate keypair for mTLS.
func loadClientCert(t *testing.T) tls.Certificate {
	t.Helper()
	certPath, keyPath, _ := adminCertPaths()
	cert, err := tls.LoadX509KeyPair(certPath, keyPath)
	if err != nil {
		t.Fatalf("%v", &CertLoadError{Path: certPath, Cause: err})
	}
	return cert
}

// loadAdminLeaf parses the admin client certificate (DER) so the test can
// compute its canonical mTLS lookup key (SHA-256 of the DER).
func loadAdminLeaf(t *testing.T) *x509.Certificate {
	t.Helper()
	certPath, _, _ := adminCertPaths()
	data, err := os.ReadFile(certPath) // #nosec G304 — path is test config
	if err != nil {
		t.Fatalf("%v", &CertLoadError{Path: certPath, Cause: err})
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		t.Fatalf("%v", &CertLoadError{Path: certPath, Cause: fmt.Errorf("no CERTIFICATE PEM block")})
	}
	leaf, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatalf("%v", &CertLoadError{Path: certPath, Cause: err})
	}
	return leaf
}

// certSHA256 returns SHA-256 over the certificate DER — the canonical mTLS /
// PIV login lookup key (matches go-qrdb pkg/auth.CredentialKey and the
// server-side mtlsLookupKey).
func certSHA256(cert *x509.Certificate) []byte {
	sum := sha256.Sum256(cert.Raw)
	return sum[:]
}

// loadCA reads and parses the cluster CA certificate + private key from the
// mounted certs dir so the suite can sign piv leaf certificates that chain to
// it. The CA key is mounted read-only alongside the public certs.
func loadCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	_, _, caPath := adminCertPaths()

	caPEM, err := os.ReadFile(caPath) // #nosec G304 — path is test config
	if err != nil {
		t.Fatalf("%v", &CertLoadError{Path: caPath, Cause: err})
	}
	caBlock, _ := pem.Decode(caPEM)
	if caBlock == nil {
		t.Fatalf("%v", &CertLoadError{Path: caPath, Cause: fmt.Errorf("no CA PEM block")})
	}
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	if err != nil {
		t.Fatalf("%v", &CertLoadError{Path: caPath, Cause: err})
	}

	keyPath := caKeyFile()
	keyPEM, err := os.ReadFile(keyPath) // #nosec G304 — path is test config
	if err != nil {
		t.Fatalf("%v", &CertLoadError{Path: keyPath, Cause: err})
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		t.Fatalf("%v", &CertLoadError{Path: keyPath, Cause: fmt.Errorf("no CA key PEM block")})
	}
	caKey, err := x509.ParseECPrivateKey(keyBlock.Bytes)
	if err != nil {
		t.Fatalf("%v", &CertLoadError{Path: keyPath, Cause: err})
	}
	return caCert, caKey
}

// softwarePIVCredential is a software stand-in for a PIV slot: a freshly
// generated P-256 key and a leaf certificate chained to the cluster CA. The
// in-container integration image has no PC/SC reader or SoftHSM PIV applet, so
// the suite uses a software credential whose verification path is byte-for-byte
// identical to a hardware slot's (the server's PIVHardwareFactorVerifier only
// sees the certificate + an ECDSA signature over SHA-256(nonce); it cannot tell
// software from hardware). The crypto is REAL — no verification is mocked.
type softwarePIVCredential struct {
	key      *ecdsa.PrivateKey
	leaf     *x509.Certificate
	leafPEM  string
	leafHash []byte // SHA-256(leaf.Raw) — the piv factor lookup key
}

// newSoftwarePIVCredential mints a P-256 key + a leaf cert signed by the
// cluster CA. The leaf carries ClientAuth EKU and the supplied CN so it looks
// like a real PIV slot certificate.
func newSoftwarePIVCredential(t *testing.T, commonName string) *softwarePIVCredential {
	t.Helper()
	caCert, caKey := loadCA(t)

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("%v", &CASignError{Op: "generate P-256 key", Cause: err})
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		t.Fatalf("%v", &CASignError{Op: "serial", Cause: err})
	}

	now := time.Now()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"QuicBaaS Integration Tests"},
			CommonName:   commonName,
		},
		NotBefore:   now.Add(-time.Minute),
		NotAfter:    now.AddDate(0, 0, 30),
		KeyUsage:    x509.KeyUsageDigitalSignature,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, caCert, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("%v", &CASignError{Op: "create leaf", Cause: err})
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatalf("%v", &CASignError{Op: "parse leaf", Cause: err})
	}
	leafPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	sum := sha256.Sum256(der)

	return &softwarePIVCredential{
		key:      key,
		leaf:     leaf,
		leafPEM:  leafPEM,
		leafHash: sum[:],
	}
}

// signNonce signs SHA-256(nonce) with the credential's P-256 key, producing the
// ASN.1 DER ECDSA signature that PIVHardwareFactorVerifier.Verify expects.
func (c *softwarePIVCredential) signNonce(nonce []byte) ([]byte, error) {
	digest := sha256.Sum256(nonce)
	return ecdsa.SignASN1(rand.Reader, c.key, digest[:])
}

// =============================================================================
// SDK client builders (per transport, presenting the admin mTLS client cert)
// =============================================================================

// loginTransport names a wire transport for the table-driven login matrix.
type loginTransport string

const (
	transportREST loginTransport = "rest"
	transportGRPC loginTransport = "grpc"
	transportQUIC loginTransport = "quic"
	transportUnix loginTransport = "unix"
	transportMCP  loginTransport = "mcp"
)

// allLoginTransports is the network-transport matrix the login ceremony is
// exercised over. MCP is now in the matrix: it is HTTP+TLS, so the SDK MCP
// transport presents the admin client certificate on the same TLS handshake
// the server verifies and threads onto the request context
// (pkg/api/mcp/server.go r.TLS.VerifiedChains[0][0] -> service.WithVerifiedPeerCert),
// exactly like REST. The embedded transport is covered by the in-process
// suites; this set is the network parity matrix where a real TLS peer cert is
// presented.
func allLoginTransports() []loginTransport {
	return []loginTransport{transportREST, transportGRPC, transportQUIC, transportUnix, transportMCP}
}

// mtlsTLSConfig builds a tls.Config that trusts the cluster CA AND presents the
// admin client certificate. This is what makes the network mTLS login work: the
// server verifies the presented client cert against its client-CA and places
// the verified leaf on the request context.
func mtlsTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	base, err := helpers.IntegrationTLSConfig()
	if err != nil {
		t.Fatalf("login-test: integration TLS config: %v", err)
	}
	base.Certificates = []tls.Certificate{loadClientCert(t)}
	return base
}

// noCertTLSConfig trusts the cluster CA but presents NO client certificate.
// Used by the fail-closed security assertion.
func noCertTLSConfig(t *testing.T) *tls.Config {
	t.Helper()
	base, err := helpers.IntegrationTLSConfig()
	if err != nil {
		t.Fatalf("login-test: integration TLS config: %v", err)
	}
	return base
}

// newLoginClient builds an SDK Client for the given transport that presents the
// supplied tls.Config (with or without a client cert). The address is resolved
// from the standard integration helpers.
func newLoginClient(t *testing.T, tr loginTransport, tlsCfg *tls.Config) (quicbaas.Client, error) {
	t.Helper()
	switch tr {
	case transportREST:
		endpoints := helpers.GetRESTEndpoints()
		if len(endpoints) == 0 {
			return nil, fmt.Errorf("login-test: no REST endpoints")
		}
		return quicbaas.NewClient(endpoints[0],
			quicbaas.WithREST(),
			quicbaas.WithTLSConfig(tlsCfg),
			quicbaas.WithTimeout(30*time.Second))
	case transportGRPC:
		endpoints := helpers.GetClusterEndpoints()
		if len(endpoints) == 0 {
			return nil, fmt.Errorf("login-test: no gRPC endpoints")
		}
		return quicbaas.NewClient(endpoints[0],
			quicbaas.WithTLSConfig(tlsCfg),
			quicbaas.WithTimeout(30*time.Second))
	case transportQUIC:
		endpoints := helpers.GetQUICEndpoints()
		if len(endpoints) == 0 {
			return nil, fmt.Errorf("login-test: no QUIC endpoints")
		}
		quicCfg := tlsCfg.Clone()
		quicCfg.NextProtos = []string{"quicbaas-quic"}
		return quicbaas.NewClient(endpoints[0],
			quicbaas.WithQUIC(),
			quicbaas.WithTLSConfig(quicCfg),
			quicbaas.WithTimeout(30*time.Second))
	case transportUnix:
		socket := helpers.GetUnixSocketPath()
		if socket == "" {
			return nil, fmt.Errorf("login-test: no Unix socket path")
		}
		unixCfg := tlsCfg.Clone()
		unixCfg.ServerName = "localhost"
		return quicbaas.NewClient("unix://"+socket,
			quicbaas.WithUnix(socket),
			quicbaas.WithTLSConfig(unixCfg),
			quicbaas.WithTimeout(30*time.Second))
	case transportMCP:
		endpoints := helpers.GetMCPEndpoints()
		if len(endpoints) == 0 {
			return nil, fmt.Errorf("login-test: no MCP endpoints")
		}
		// MCP is JSON-RPC 2.0 over HTTPS; WithTLSConfig wires the supplied
		// tls.Config (admin client cert) into the MCP transport's HTTP client
		// so the server verifies the same peer cert it does over REST. The
		// login tools (quicbaas_login_begin/finish) are public, so no bearer
		// is required to drive the ceremony.
		return quicbaas.NewClient(endpoints[0],
			quicbaas.WithMCP(),
			quicbaas.WithTLSConfig(tlsCfg),
			quicbaas.WithTimeout(30*time.Second))
	default:
		return nil, fmt.Errorf("login-test: unknown transport %q", tr)
	}
}

// =============================================================================
// Factor enrollment (writes to the qrdb UserAuthFactor store the login
// resolver reads)
// =============================================================================

// enrollMTLSFactor enrolls an mtls factor (Credential = SHA-256(certDER)) for
// the user via the SDK Factors surface. The lookup key matches the canonical
// mtls key the server derives from the verified peer certificate.
func enrollMTLSFactor(ctx context.Context, c quicbaas.Client, username string, leaf *x509.Certificate) error {
	hexKey := hex.EncodeToString(certSHA256(leaf))
	_, err := c.Security().Auth().Factors().Add(ctx, &quicbaas.AuthFactorEnrollRequest{
		FactorName: "mtls",
		Subject:    username,
		TenantID:   systemTenantID,
		MethodParams: map[string]any{
			"label":             "login-test-mtls",
			"credential_sha256": hexKey,
			"cert_cn":           leaf.Subject.CommonName,
		},
	})
	return err
}

// enrollPIVFactor enrolls a piv factor for the user. The verifier reads the
// certificate from Metadata["cert_pem"] and validates the chain against the
// cluster CA, then verifies the signature over the nonce — so cert_pem MUST be
// the leaf PEM and the lookup key MUST be SHA-256(leaf DER).
func enrollPIVFactor(ctx context.Context, c quicbaas.Client, username string, cred *softwarePIVCredential) error {
	hexKey := hex.EncodeToString(cred.leafHash)
	_, err := c.Security().Auth().Factors().Add(ctx, &quicbaas.AuthFactorEnrollRequest{
		FactorName: "piv",
		Subject:    username,
		TenantID:   systemTenantID,
		MethodParams: map[string]any{
			"label":             "login-test-piv",
			"credential_sha256": hexKey,
			"cert_pem":          cred.leafPEM,
			"slot":              "9a",
		},
	})
	return err
}

// =============================================================================
// Bearer validation
// =============================================================================

// jwtSubject decodes a JWT's payload (without verifying the signature — the
// server already verified it when minting it) and returns the sub + tenant_id
// claims so the test can assert the bearer encodes the resolved identity.
func jwtSubject(bearer string) (sub, tenant string, err error) {
	parts := strings.Split(bearer, ".")
	if len(parts) != 3 {
		return "", "", &BearerValidationError{Reason: "bearer is not a 3-part JWT"}
	}
	payload, derr := base64URLDecode(parts[1])
	if derr != nil {
		return "", "", &BearerValidationError{Reason: "decode payload: " + derr.Error()}
	}
	var claims struct {
		Sub      string `json:"sub"`
		TenantID string `json:"tenant_id"`
		Tid      string `json:"tid"`
	}
	if jerr := json.Unmarshal(payload, &claims); jerr != nil {
		return "", "", &BearerValidationError{Reason: "unmarshal claims: " + jerr.Error()}
	}
	tenant = claims.TenantID
	if tenant == "" {
		tenant = claims.Tid
	}
	return claims.Sub, tenant, nil
}

// base64URLDecode decodes a base64url segment, tolerating missing padding.
func base64URLDecode(s string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(strings.TrimRight(s, "="))
}

// validateBearerOverREST presents the bearer to the cluster's /oauth2/userinfo
// endpoint over REST and asserts it is accepted (HTTP 200). This proves the
// issued token is a live, server-recognized credential, not merely a
// well-formed JWT. Returns a typed error on any non-200 or transport failure.
func validateBearerOverREST(ctx context.Context, bearer string) error {
	endpoints := helpers.GetRESTEndpoints()
	if len(endpoints) == 0 {
		return &BearerValidationError{Reason: "no REST endpoints for userinfo"}
	}
	url := strings.TrimRight(endpoints[0], "/") + "/oauth2/userinfo"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return &BearerValidationError{Reason: "build userinfo request: " + err.Error()}
	}
	req.Header.Set("Authorization", "Bearer "+bearer)

	client := helpers.NewHTTPClient(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return &BearerValidationError{Reason: "userinfo request: " + err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return &BearerValidationError{Reason: fmt.Sprintf("userinfo returned %d: %s", resp.StatusCode, string(body))}
	}
	return nil
}

// =============================================================================
// Direct-wire login ceremony (REST) — used for the security-critical crypto
// assertions where the test must control the exact TLS handshake (client cert
// present / absent) and the exact request body (spoofed lookup key).
// =============================================================================

// wireBeginLoginResponse mirrors ceremonyBeginLoginResponse.
type wireBeginLoginResponse struct {
	SessionID         string            `json:"session_id"`
	Nonce             []byte            `json:"nonce"`
	ChallengeMetadata map[string]string `json:"challenge_metadata,omitempty"`
	ExpiresAt         string            `json:"expires_at"`
}

// wireFinishLoginResponse mirrors ceremonyFinishLoginResponse.
type wireFinishLoginResponse struct {
	Bearer   string `json:"bearer"`
	Identity *struct {
		Subject  string   `json:"subject"`
		TenantID string   `json:"tenant_id"`
		Roles    []string `json:"roles,omitempty"`
	} `json:"identity,omitempty"`
	ExpiresAt string `json:"expires_at"`
	FactorID  string `json:"factor_id,omitempty"`
}

// restCeremonyClient drives /api/v1/auth/login/begin|finish directly over a
// caller-supplied tls.Config so the test controls whether a client cert is
// presented. This is the canonical wire surface every SDK transport converges
// to; using it directly keeps the security assertions explicit.
type restCeremonyClient struct {
	baseURL string
	http    *http.Client
}

// newRESTCeremonyClient builds a direct REST ceremony client with the given
// TLS config (client-cert-bearing or not).
func newRESTCeremonyClient(t *testing.T, tlsCfg *tls.Config) *restCeremonyClient {
	t.Helper()
	endpoints := helpers.GetRESTEndpoints()
	if len(endpoints) == 0 {
		t.Fatalf("login-test: no REST endpoints")
	}
	return &restCeremonyClient{
		baseURL: strings.TrimRight(endpoints[0], "/"),
		http: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig:     tlsCfg,
				TLSHandshakeTimeout: 10 * time.Second,
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 30 * time.Second,
				}).DialContext,
			},
		},
	}
}

// begin POSTs /api/v1/auth/login/begin and returns the decoded response or a
// typed error carrying the HTTP status for non-200 responses.
func (c *restCeremonyClient) begin(ctx context.Context, factorType, hint string) (*wireBeginLoginResponse, int, error) {
	body, _ := json.Marshal(map[string]string{
		"type":      factorType,
		"hint":      hint,
		"tenant_id": systemTenantID,
	})
	respBody, status, err := c.post(ctx, "/api/v1/auth/login/begin", body)
	if err != nil {
		return nil, status, err
	}
	if status != http.StatusOK {
		return nil, status, fmt.Errorf("begin returned %d: %s", status, string(respBody))
	}
	var out wireBeginLoginResponse
	if jerr := json.Unmarshal(respBody, &out); jerr != nil {
		return nil, status, fmt.Errorf("decode begin response: %w", jerr)
	}
	return &out, status, nil
}

// finish POSTs /api/v1/auth/login/finish with the supplied body and returns the
// decoded response + HTTP status. A non-200 status is returned without error so
// callers can assert fail-closed behavior on the status code.
func (c *restCeremonyClient) finish(ctx context.Context, reqBody map[string]any) (*wireFinishLoginResponse, int, error) {
	body, _ := json.Marshal(reqBody)
	respBody, status, err := c.post(ctx, "/api/v1/auth/login/finish", body)
	if err != nil {
		return nil, status, err
	}
	if status != http.StatusOK {
		return &wireFinishLoginResponse{}, status, nil
	}
	var out wireFinishLoginResponse
	if jerr := json.Unmarshal(respBody, &out); jerr != nil {
		return nil, status, fmt.Errorf("decode finish response: %w", jerr)
	}
	return &out, status, nil
}

// post performs a POST and returns the body + status.
func (c *restCeremonyClient) post(ctx context.Context, path string, body []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, strings.NewReader(string(body)))
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, resp.StatusCode, err
	}
	return respBody, resp.StatusCode, nil
}

// =============================================================================
// Shared SDK login round-trip
// =============================================================================

// sdkLoginResult captures the outcome of a unified login over the SDK.
type sdkLoginResult struct {
	bearer   string
	subject  string
	tenant   string
	factorID string
}

// runSDKLogin runs BeginLogin + FinishLogin over the SDK client for the given
// factor type, signing the challenge as needed (sign==nil for mtls). It returns
// the issued bearer and resolved identity.
func runSDKLogin(
	ctx context.Context,
	c quicbaas.Client,
	factorType qrdbtransport.FactorType,
	hint string,
	lookupKey []byte,
	sign func(nonce []byte) ([]byte, error),
	meta map[string]string,
) (*sdkLoginResult, error) {
	auth := c.Security().Auth()
	begin, err := auth.BeginLogin(ctx, qrdbtransport.BeginLoginRequest{
		Type:     factorType,
		Hint:     hint,
		TenantID: systemTenantID,
	})
	if err != nil {
		return nil, err
	}

	var signature []byte
	if sign != nil {
		signature, err = sign(begin.Nonce)
		if err != nil {
			return nil, err
		}
	}

	finish, err := auth.FinishLogin(ctx, qrdbtransport.FinishLoginRequest{
		SessionID:      begin.SessionID,
		Type:           factorType,
		LookupKey:      lookupKey,
		Signature:      signature,
		MethodMetadata: meta,
	})
	if err != nil {
		return nil, err
	}
	res := &sdkLoginResult{bearer: finish.Bearer, factorID: finish.FactorID}
	if finish.Identity != nil {
		res.subject = finish.Identity.Subject
		res.tenant = finish.Identity.TenantID
	}
	return res, nil
}
