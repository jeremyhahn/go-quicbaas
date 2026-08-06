//go:build mcp

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

package mcp

import (
	"context"
	"encoding/base64"
	stderrors "errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// fakeCeremonyService — deterministic in-process CeremonyServicer for tests.
// =============================================================================

// fakeCeremonyService implements service.CeremonyServicer with scripted
// responses. Production-grade enough for unit tests.
type fakeCeremonyService struct {
	initInfo          *service.CeremonyInitInfo
	initErr           error
	statusInfo        *service.CeremonyStatusInfo
	statusErr         error
	challenge         *service.CeremonyChallenge
	challengeErr      error
	claimResult       *service.CeremonyClaimCertResult
	claimErr          error
	shareChallenge    *service.CeremonyChallenge
	shareChallengeErr error
	shareResult       *service.CeremonyClaimShareResult
	shareResultErr    error
	signResult        *service.CeremonySignCSRResult
	signErr           error
	enrollResp        *service.CeremonyEnrollExternalOfficerResponse
	enrollErr         error

	// observed args
	gotInitReq       *service.CeremonyInitRequest
	gotChallengeUser string
	gotClaimUser     string
	gotClaimNonce    string
	gotClaimSig      string
	gotShareUser     string
	gotShareNonce    string
	gotShareSig      []byte
	gotSignUser      string
	gotSignPin       string
	gotSignCSR       string
	gotSignRole      string
	gotEnrollReq     *service.CeremonyEnrollExternalOfficerRequest
}

func (f *fakeCeremonyService) CeremonyInitialize(_ context.Context, req *service.CeremonyInitRequest) (*service.CeremonyInitInfo, error) {
	f.gotInitReq = req
	return f.initInfo, f.initErr
}

func (f *fakeCeremonyService) CeremonyStatus(_ context.Context) (*service.CeremonyStatusInfo, error) {
	return f.statusInfo, f.statusErr
}

func (f *fakeCeremonyService) CeremonyBeginClaimCert(_ context.Context, username string) (*service.CeremonyChallenge, error) {
	f.gotChallengeUser = username
	return f.challenge, f.challengeErr
}

func (f *fakeCeremonyService) CeremonyCompleteClaimCert(_ context.Context, username, nonceHex, signatureBase64 string) (*service.CeremonyClaimCertResult, error) {
	f.gotClaimUser = username
	f.gotClaimNonce = nonceHex
	f.gotClaimSig = signatureBase64
	return f.claimResult, f.claimErr
}

func (f *fakeCeremonyService) CeremonyBeginClaimShare(_ context.Context, username string) (*service.CeremonyChallenge, error) {
	f.gotShareUser = username
	return f.shareChallenge, f.shareChallengeErr
}

func (f *fakeCeremonyService) CeremonyCompleteClaimShare(_ context.Context, username, nonceHex string, signature []byte) (*service.CeremonyClaimShareResult, error) {
	f.gotShareUser = username
	f.gotShareNonce = nonceHex
	f.gotShareSig = signature
	return f.shareResult, f.shareResultErr
}

func (f *fakeCeremonyService) CeremonySignCSR(_ context.Context, username, soPin, csrPEM, role string) (*service.CeremonySignCSRResult, error) {
	f.gotSignUser = username
	f.gotSignPin = soPin
	f.gotSignCSR = csrPEM
	f.gotSignRole = role
	return f.signResult, f.signErr
}

func (f *fakeCeremonyService) CeremonyEnrollExternalOfficer(_ context.Context, req *service.CeremonyEnrollExternalOfficerRequest) (*service.CeremonyEnrollExternalOfficerResponse, error) {
	f.gotEnrollReq = req
	return f.enrollResp, f.enrollErr
}

// newCeremonyTestRegistry builds a ToolRegistry wired to a fake ceremony
// service.
func newCeremonyTestRegistry(t *testing.T, fake service.CeremonyServicer) *ToolRegistry {
	t.Helper()
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test-ceremony",
	})
	require.NoError(t, err)
	r := NewToolRegistry(&MockQuicBaaS{}, log)
	r.SetCeremonyService(fake)
	return r
}

// TestCeremonyToolsRegistered asserts every documented ceremony tool is
// present in the registry's tool list.
func TestCeremonyToolsRegistered(t *testing.T) {
	r := newCeremonyTestRegistry(t, &fakeCeremonyService{})
	got := map[string]bool{}
	for _, td := range r.ListTools() {
		got[td.Name] = true
	}
	for name := range ceremonyToolNames {
		assert.Truef(t, got[name], "tool %q not registered", name)
	}
}

// TestCeremonyToolsExecuteSuccess walks every ceremony tool through a happy
// path and verifies arguments + responses.
func TestCeremonyToolsExecuteSuccess(t *testing.T) {
	now := time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)
	fake := &fakeCeremonyService{
		initInfo: &service.CeremonyInitInfo{
			SPKIPin:    "pin1",
			CACertPEM:  []byte("ca-pem"),
			TLSCertPEM: []byte("tls-pem"),
			SOCertPEM:  []byte("so-pem"),
			SOKeyPEM:   []byte("so-key"),
			State:      "READY",
			ShareCount: 3,
		},
		statusInfo:     &service.CeremonyStatusInfo{State: "READY", SPKIPin: "pin1"},
		challenge:      &service.CeremonyChallenge{NonceHex: "deadbeef", ExpiresAt: now},
		claimResult:    &service.CeremonyClaimCertResult{CertPEM: []byte("cert"), CACertPEM: []byte("ca")},
		shareChallenge: &service.CeremonyChallenge{NonceHex: "deadbeef", ExpiresAt: now},
		shareResult:    &service.CeremonyClaimShareResult{ShareIndex: 2, ShareJSON: "share-json"},
		signResult:     &service.CeremonySignCSRResult{CertPEM: []byte("signed-cert")},
		enrollResp:     &service.CeremonyEnrollExternalOfficerResponse{SealedShare: "sealed-share-b64"},
	}
	r := newCeremonyTestRegistry(t, fake)
	ctx := context.Background()

	t.Run("initialize", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_ceremony_initialize", map[string]interface{}{
			"so_pin":                   "so",
			"user_pin":                 "user",
			"credential_seal_strategy": "manual",
			"threshold":                2,
			"hostname":                 "node1.example.com",
			"sans":                     []interface{}{"node1", "alt"},
			"officers": []interface{}{
				map[string]interface{}{"username": "alice", "csr_pem": "csr-a"},
				map[string]interface{}{"username": "bob", "csr_pem": "csr-b"},
			},
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "pin1", m["spki_pin"])
		assert.Equal(t, "READY", m["state"])
		assert.Equal(t, 3, m["share_count"])
		assert.Equal(t, "ca-pem", m["ca_cert_pem"])
		assert.Equal(t, "so-pem", m["so_cert_pem"])
		assert.Equal(t, "so-key", m["so_key_pem"])

		require.NotNil(t, fake.gotInitReq)
		assert.Equal(t, "so", fake.gotInitReq.SOPin)
		assert.Equal(t, "user", fake.gotInitReq.UserPin)
		assert.Equal(t, "manual", fake.gotInitReq.CredentialSealStrategy)
		assert.Equal(t, 2, fake.gotInitReq.Threshold)
		assert.Equal(t, "node1.example.com", fake.gotInitReq.Hostname)
		assert.Equal(t, []string{"node1", "alt"}, fake.gotInitReq.SANs)
		require.Len(t, fake.gotInitReq.Officers, 2)
		assert.Equal(t, "alice", fake.gotInitReq.Officers[0].Username)
		assert.Equal(t, "csr-a", fake.gotInitReq.Officers[0].CSRPEM)
		assert.Equal(t, "bob", fake.gotInitReq.Officers[1].Username)
	})

	t.Run("status", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_ceremony_status", nil)
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "READY", m["state"])
		assert.Equal(t, "pin1", m["spki_pin"])
	})

	t.Run("claim_cert_begin", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_ceremony_claim_cert_begin", map[string]interface{}{"username": "alice"})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "deadbeef", m["nonce_hex"])
		assert.Equal(t, "2026-01-02T00:00:00Z", m["expires_at"])
		assert.Equal(t, "alice", fake.gotChallengeUser)
	})

	t.Run("claim_cert_complete", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_ceremony_claim_cert_complete", map[string]interface{}{
			"username":  "alice",
			"nonce_hex": "deadbeef",
			"signature": "sig-b64",
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "cert", m["cert_pem"])
		assert.Equal(t, "ca", m["ca_cert_pem"])
		assert.Equal(t, "alice", fake.gotClaimUser)
		assert.Equal(t, "deadbeef", fake.gotClaimNonce)
		assert.Equal(t, "sig-b64", fake.gotClaimSig)
	})

	t.Run("begin_claim_share", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_ceremony_begin_claim_share", map[string]interface{}{"username": "alice"})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, "deadbeef", m["nonce_hex"])
		assert.Equal(t, "2026-01-02T00:00:00Z", m["expires_at"])
		assert.Equal(t, "alice", fake.gotShareUser)
	})

	t.Run("complete_claim_share", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_ceremony_complete_claim_share", map[string]interface{}{
			"username":  "alice",
			"nonce_hex": "deadbeef",
			"signature": base64.StdEncoding.EncodeToString([]byte("sig")),
		})
		require.NoError(t, err)
		m := out.(map[string]interface{})
		assert.Equal(t, 2, m["share_index"])
		assert.Equal(t, base64.StdEncoding.EncodeToString([]byte("share-json")), m["share"])
		assert.Equal(t, "alice", fake.gotShareUser)
		assert.Equal(t, "deadbeef", fake.gotShareNonce)
		assert.Equal(t, []byte("sig"), fake.gotShareSig)
	})

	t.Run("sign_csr", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_ceremony_sign_csr", map[string]interface{}{
			"username": "ops",
			"so_pin":   "1234",
			"csr_pem":  "-----BEGIN CSR-----",
			"role":     "node",
		})
		require.NoError(t, err)
		assert.Equal(t, "signed-cert", out.(map[string]interface{})["cert_pem"])
		assert.Equal(t, "ops", fake.gotSignUser)
		assert.Equal(t, "1234", fake.gotSignPin)
		assert.Equal(t, "-----BEGIN CSR-----", fake.gotSignCSR)
		assert.Equal(t, "node", fake.gotSignRole)
	})

	t.Run("list_officers_empty", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "quicbaas_ceremony_list_officers", nil)
		require.NoError(t, err)
		m := out.(map[string]interface{})
		officers, ok := m["officers"].([]interface{})
		require.True(t, ok)
		assert.Len(t, officers, 0)
	})

	t.Run("enroll_external_officer_with_cert", func(t *testing.T) {
		out, err := r.ExecuteTool(ctx, "qrdb_ceremony_enroll_external_officer", map[string]interface{}{
			"username":    "ext",
			"cert_pem":    "-----BEGIN CERT-----",
			"share_index": 4,
			"auth_factor": "mtls",
			"share_bytes": "raw-share",
		})
		require.NoError(t, err)
		assert.Equal(t, "sealed-share-b64", out.(map[string]interface{})["sealed_share"])

		require.NotNil(t, fake.gotEnrollReq)
		assert.Equal(t, "ext", fake.gotEnrollReq.Username)
		assert.Equal(t, "-----BEGIN CERT-----", fake.gotEnrollReq.CertPEM)
		assert.Empty(t, fake.gotEnrollReq.PubKeyPEM)
		assert.Equal(t, 4, fake.gotEnrollReq.ShareIndex)
		assert.Equal(t, "mtls", fake.gotEnrollReq.AuthFactor)
		assert.Equal(t, []byte("raw-share"), fake.gotEnrollReq.ShareBytes)
	})

	t.Run("enroll_external_officer_with_pubkey", func(t *testing.T) {
		fake.gotEnrollReq = nil
		_, err := r.ExecuteTool(ctx, "qrdb_ceremony_enroll_external_officer", map[string]interface{}{
			"username":    "ext2",
			"pubkey_pem":  "-----BEGIN PUBLIC KEY-----",
			"auth_factor": "webauthn",
		})
		require.NoError(t, err)
		require.NotNil(t, fake.gotEnrollReq)
		assert.Empty(t, fake.gotEnrollReq.CertPEM)
		assert.Equal(t, "-----BEGIN PUBLIC KEY-----", fake.gotEnrollReq.PubKeyPEM)
	})
}

// TestCeremonyToolsValidation exercises required-field and mutual-exclusion
// validation for every ceremony tool.
func TestCeremonyToolsValidation(t *testing.T) {
	r := newCeremonyTestRegistry(t, &fakeCeremonyService{
		statusInfo: &service.CeremonyStatusInfo{},
	})

	cases := []struct {
		name        string
		tool        string
		args        map[string]interface{}
		expectedErr error
	}{
		{
			name:        "claim_cert_begin missing username",
			tool:        "qrdb_ceremony_claim_cert_begin",
			args:        map[string]interface{}{},
			expectedErr: &CeremonyFieldRequiredError{},
		},
		{
			name:        "claim_cert_complete missing username",
			tool:        "qrdb_ceremony_claim_cert_complete",
			args:        map[string]interface{}{"nonce_hex": "x", "signature": "y"},
			expectedErr: &CeremonyFieldRequiredError{},
		},
		{
			name:        "claim_cert_complete missing nonce",
			tool:        "qrdb_ceremony_claim_cert_complete",
			args:        map[string]interface{}{"username": "a", "signature": "y"},
			expectedErr: &CeremonyFieldRequiredError{},
		},
		{
			name:        "claim_cert_complete missing signature",
			tool:        "qrdb_ceremony_claim_cert_complete",
			args:        map[string]interface{}{"username": "a", "nonce_hex": "x"},
			expectedErr: &CeremonyFieldRequiredError{},
		},
		{
			name:        "begin_claim_share missing username",
			tool:        "qrdb_ceremony_begin_claim_share",
			args:        map[string]interface{}{},
			expectedErr: &CeremonyFieldRequiredError{},
		},
		{
			name:        "complete_claim_share missing username",
			tool:        "qrdb_ceremony_complete_claim_share",
			args:        map[string]interface{}{"nonce_hex": "x", "signature": "c2ln"},
			expectedErr: &CeremonyFieldRequiredError{},
		},
		{
			name:        "complete_claim_share missing nonce",
			tool:        "qrdb_ceremony_complete_claim_share",
			args:        map[string]interface{}{"username": "a", "signature": "c2ln"},
			expectedErr: &CeremonyFieldRequiredError{},
		},
		{
			name:        "complete_claim_share missing signature",
			tool:        "qrdb_ceremony_complete_claim_share",
			args:        map[string]interface{}{"username": "a", "nonce_hex": "x"},
			expectedErr: &CeremonyFieldRequiredError{},
		},
		{
			name:        "complete_claim_share bad signature encoding",
			tool:        "qrdb_ceremony_complete_claim_share",
			args:        map[string]interface{}{"username": "a", "nonce_hex": "x", "signature": "%%%"},
			expectedErr: &CeremonyFieldConflictError{},
		},
		{
			name:        "sign_csr missing username",
			tool:        "qrdb_ceremony_sign_csr",
			args:        map[string]interface{}{"csr_pem": "x"},
			expectedErr: &CeremonyFieldRequiredError{},
		},
		{
			name:        "sign_csr missing csr_pem",
			tool:        "qrdb_ceremony_sign_csr",
			args:        map[string]interface{}{"username": "u"},
			expectedErr: &CeremonyFieldRequiredError{},
		},
		{
			name:        "enroll missing username",
			tool:        "qrdb_ceremony_enroll_external_officer",
			args:        map[string]interface{}{"auth_factor": "mtls", "cert_pem": "c"},
			expectedErr: &CeremonyFieldRequiredError{},
		},
		{
			name:        "enroll missing auth_factor",
			tool:        "qrdb_ceremony_enroll_external_officer",
			args:        map[string]interface{}{"username": "u", "cert_pem": "c"},
			expectedErr: &CeremonyFieldRequiredError{},
		},
		{
			name: "enroll missing both cert_pem and pubkey_pem",
			tool: "qrdb_ceremony_enroll_external_officer",
			args: map[string]interface{}{
				"username":    "u",
				"auth_factor": "mtls",
			},
			expectedErr: &CeremonyFieldConflictError{},
		},
		{
			name: "enroll conflict cert_pem and pubkey_pem",
			tool: "qrdb_ceremony_enroll_external_officer",
			args: map[string]interface{}{
				"username":    "u",
				"auth_factor": "mtls",
				"cert_pem":    "c",
				"pubkey_pem":  "p",
			},
			expectedErr: &CeremonyFieldConflictError{},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := r.ExecuteTool(context.Background(), c.tool, c.args)
			require.Error(t, err)
			assert.IsTypef(t, c.expectedErr, err, "wrong error type for %s: got %T", c.tool, err)
		})
	}
}

// TestCeremonyToolsUnavailable asserts every ceremony tool returns the typed
// unavailable error when no CeremonyServicer is wired.
func TestCeremonyToolsUnavailable(t *testing.T) {
	log, err := logger.New(logger.LogConfig{
		Level:     "error",
		Format:    "json",
		Output:    "stdout",
		Component: "mcp-test-ceremony",
	})
	require.NoError(t, err)
	r := NewToolRegistry(&MockQuicBaaS{}, log)
	// Intentionally do NOT call SetCeremonyService.

	for name := range ceremonyToolNames {
		t.Run(name, func(t *testing.T) {
			_, err := r.ExecuteTool(context.Background(), name, map[string]interface{}{
				"username":    "u",
				"nonce_hex":   "x",
				"signature":   "y",
				"csr_pem":     "z",
				"auth_factor": "mtls",
				"cert_pem":    "c",
			})
			require.Error(t, err)
			var notReady *CeremonyServiceUnavailableError
			assert.Truef(t, stderrors.As(err, &notReady), "expected CeremonyServiceUnavailableError, got %T", err)
		})
	}
}

// TestCeremonyToolsPropagatesServiceError ensures each executor surfaces the
// underlying service error unchanged.
func TestCeremonyToolsPropagatesServiceError(t *testing.T) {
	sentinel := stderrors.New("scripted-failure")
	fake := &fakeCeremonyService{
		initInfo:          &service.CeremonyInitInfo{},
		initErr:           sentinel,
		statusInfo:        &service.CeremonyStatusInfo{},
		statusErr:         sentinel,
		challenge:         &service.CeremonyChallenge{},
		challengeErr:      sentinel,
		claimResult:       &service.CeremonyClaimCertResult{},
		claimErr:          sentinel,
		shareChallengeErr: sentinel,
		shareResultErr:    sentinel,
		signResult:        &service.CeremonySignCSRResult{},
		signErr:           sentinel,
		enrollResp:        &service.CeremonyEnrollExternalOfficerResponse{},
		enrollErr:         sentinel,
	}
	r := newCeremonyTestRegistry(t, fake)

	// Note: list_officers returns the empty roster locally without
	// invoking the service, so it is intentionally omitted here.
	calls := []struct {
		name string
		args map[string]interface{}
	}{
		{"qrdb_ceremony_initialize", map[string]interface{}{}},
		{"qrdb_ceremony_status", nil},
		{"qrdb_ceremony_claim_cert_begin", map[string]interface{}{"username": "u"}},
		{"qrdb_ceremony_claim_cert_complete", map[string]interface{}{
			"username": "u", "nonce_hex": "n", "signature": "s",
		}},
		{"qrdb_ceremony_begin_claim_share", map[string]interface{}{"username": "u"}},
		{"qrdb_ceremony_complete_claim_share", map[string]interface{}{
			"username": "u", "nonce_hex": "n", "signature": "c2ln",
		}},
		{"qrdb_ceremony_sign_csr", map[string]interface{}{
			"username": "u", "csr_pem": "c", "so_pin": "p", "role": "node",
		}},
		{"qrdb_ceremony_enroll_external_officer", map[string]interface{}{
			"username": "u", "auth_factor": "mtls", "cert_pem": "c",
		}},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			_, err := r.ExecuteTool(context.Background(), c.name, c.args)
			require.Error(t, err)
			assert.ErrorIs(t, err, sentinel)
		})
	}
}

// TestCeremonyToolsErrorMessages locks in the human-readable error messages
// surfaced by each typed error.
func TestCeremonyToolsErrorMessages(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&CeremonyServiceUnavailableError{}, "ceremony service unavailable: not configured"},
		{&CeremonyFieldRequiredError{Field: "username"}, "ceremony: username is required"},
		{&CeremonyFieldConflictError{Reason: "x"}, "ceremony: x"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, c.err.Error())
	}
}
