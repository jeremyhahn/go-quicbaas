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

// Package main provides flag-parsing and routing tests for the canonical
// 'quicbaas init' command tree.
package main

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock CeremonyClient
// =============================================================================

type fakeCeremonyClient struct {
	statusFunc             func(ctx context.Context) (*qrdbtransport.CeremonyStatusResponse, error)
	claimCertBeginFunc     func(ctx context.Context, req *qrdbtransport.ClaimCertBeginRequest) (*qrdbtransport.ClaimCertBeginResponse, error)
	claimCertCompleteFunc  func(ctx context.Context, req *qrdbtransport.ClaimCertCompleteRequest) (*qrdbtransport.ClaimCertCompleteResponse, error)
	beginClaimShareFunc    func(ctx context.Context, req *qrdbtransport.BeginClaimShareRequest) (*qrdbtransport.BeginClaimShareResponse, error)
	completeClaimShareFunc func(ctx context.Context, req *qrdbtransport.CompleteClaimShareRequest) (*qrdbtransport.CompleteClaimShareResponse, error)
	signCSRFunc            func(ctx context.Context, req *qrdbtransport.SignCSRInitRequest) (*qrdbtransport.SignCSRInitResponse, error)
	enrollFunc             func(ctx context.Context, req *qrdbtransport.EnrollExternalOfficerRequest) (*qrdbtransport.EnrollExternalOfficerResponse, error)

	lastBeginReq         *qrdbtransport.ClaimCertBeginRequest
	lastCompleteReq      *qrdbtransport.ClaimCertCompleteRequest
	lastBeginShareReq    *qrdbtransport.BeginClaimShareRequest
	lastCompleteShareReq *qrdbtransport.CompleteClaimShareRequest
	lastSignReq          *qrdbtransport.SignCSRInitRequest
	lastEnrollReq        *qrdbtransport.EnrollExternalOfficerRequest
}

func (f *fakeCeremonyClient) CeremonyStatus(ctx context.Context) (*qrdbtransport.CeremonyStatusResponse, error) {
	if f.statusFunc != nil {
		return f.statusFunc(ctx)
	}
	return &qrdbtransport.CeremonyStatusResponse{State: "operational"}, nil
}

func (f *fakeCeremonyClient) CeremonyClaimCertBegin(ctx context.Context, req *qrdbtransport.ClaimCertBeginRequest) (*qrdbtransport.ClaimCertBeginResponse, error) {
	f.lastBeginReq = req
	if f.claimCertBeginFunc != nil {
		return f.claimCertBeginFunc(ctx, req)
	}
	return &qrdbtransport.ClaimCertBeginResponse{NonceHex: "deadbeef"}, nil
}

func (f *fakeCeremonyClient) CeremonyClaimCertComplete(ctx context.Context, req *qrdbtransport.ClaimCertCompleteRequest) (*qrdbtransport.ClaimCertCompleteResponse, error) {
	f.lastCompleteReq = req
	if f.claimCertCompleteFunc != nil {
		return f.claimCertCompleteFunc(ctx, req)
	}
	return &qrdbtransport.ClaimCertCompleteResponse{
		CertPEM:   "-----BEGIN CERTIFICATE-----\nstub\n-----END CERTIFICATE-----\n",
		CACertPEM: "-----BEGIN CERTIFICATE-----\nca\n-----END CERTIFICATE-----\n",
	}, nil
}

func (f *fakeCeremonyClient) CeremonyBeginClaimShare(ctx context.Context, req *qrdbtransport.BeginClaimShareRequest) (*qrdbtransport.BeginClaimShareResponse, error) {
	f.lastBeginShareReq = req
	if f.beginClaimShareFunc != nil {
		return f.beginClaimShareFunc(ctx, req)
	}
	return &qrdbtransport.BeginClaimShareResponse{NonceHex: "deadbeef"}, nil
}

func (f *fakeCeremonyClient) CeremonyCompleteClaimShare(ctx context.Context, req *qrdbtransport.CompleteClaimShareRequest) (*qrdbtransport.CompleteClaimShareResponse, error) {
	f.lastCompleteShareReq = req
	if f.completeClaimShareFunc != nil {
		return f.completeClaimShareFunc(ctx, req)
	}
	return &qrdbtransport.CompleteClaimShareResponse{ShareIndex: 1, ShareJSON: `{"share":"x"}`}, nil
}

func (f *fakeCeremonyClient) CeremonySignCSR(ctx context.Context, req *qrdbtransport.SignCSRInitRequest) (*qrdbtransport.SignCSRInitResponse, error) {
	f.lastSignReq = req
	if f.signCSRFunc != nil {
		return f.signCSRFunc(ctx, req)
	}
	return &qrdbtransport.SignCSRInitResponse{
		CertPEM: "-----BEGIN CERTIFICATE-----\nsigned\n-----END CERTIFICATE-----\n",
	}, nil
}

func (f *fakeCeremonyClient) CeremonyEnrollExternalOfficer(ctx context.Context, req *qrdbtransport.EnrollExternalOfficerRequest) (*qrdbtransport.EnrollExternalOfficerResponse, error) {
	f.lastEnrollReq = req
	if f.enrollFunc != nil {
		return f.enrollFunc(ctx, req)
	}
	return &qrdbtransport.EnrollExternalOfficerResponse{}, nil
}

// withFakeCeremony installs fc as the active ceremony factory and returns
// a restore func.
func withFakeCeremony(fc *fakeCeremonyClient) func() {
	orig := ceremonyClientFactory
	ceremonyClientFactory = func() (CeremonyClient, error) { return fc, nil }
	return func() { ceremonyClientFactory = orig }
}

// resetInitFlags zeroes the package-level flag variables so a stale
// value from a prior test does not leak into the next.
func resetInitFlags() {
	initServer = ""
	initSPKIPin = ""
	initUsername = ""
	initCSRKeyPath = ""
	initOutputPath = ""
	initTLSCertPath = ""
	initTLSKeyPath = ""
	initSOPin = ""
	initCSRPath = ""
	initRole = ""
	initCertPath = ""
	initPubKeyPath = ""
	initShareIndex = 0
	initAuthFactor = ""
	initOutputFmt = "table"
}

// writeECPEM writes a freshly generated EC PRIVATE KEY PEM file to path.
// Returned for tests that need to sign challenge nonces.
func writeECPEM(t *testing.T, path string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	out := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	require.NoError(t, os.WriteFile(path, out, 0o600))
}

// =============================================================================
// Command tree shape
// =============================================================================

// TestNewInitCommand_Shape verifies all canonical verbs are wired.
func TestNewInitCommand_Shape(t *testing.T) {
	cmd := newInitCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "init", cmd.Use)

	expected := map[string]bool{
		"status":                  false,
		"claim-cert":              false,
		"claim-share":             false,
		"sign-csr":                false,
		"enroll-external-officer": false,
	}
	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}
	for verb, present := range expected {
		assert.True(t, present, "expected 'init %s' subcommand", verb)
	}
}

// =============================================================================
// status
// =============================================================================

// TestInitStatus_Success verifies the happy path.
func TestInitStatus_Success(t *testing.T) {
	defer resetInitFlags()
	defer withFakeCeremony(&fakeCeremonyClient{
		statusFunc: func(ctx context.Context) (*qrdbtransport.CeremonyStatusResponse, error) {
			return &qrdbtransport.CeremonyStatusResponse{
				State:   "operational",
				SPKIPin: "sha256/abcdef",
			}, nil
		},
	})()

	cmd := newInitStatusCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "CEREMONY STATUS")
	assert.Contains(t, out.String(), "operational")
}

// =============================================================================
// claim-cert
// =============================================================================

// TestInitClaimCert_FlagValidation checks the required-flag matrix.
func TestInitClaimCert_FlagValidation(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "missing-username",
			args:    []string{"--csr-key", "k.pem", "--output", "c.pem"},
			wantErr: "--username is required",
		},
		{
			name:    "missing-csr-key",
			args:    []string{"--username", "alice", "--output", "c.pem"},
			wantErr: "--csr-key is required",
		},
		{
			name:    "missing-output",
			args:    []string{"--username", "alice", "--csr-key", "k.pem"},
			wantErr: "--output is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer resetInitFlags()
			cmd := newInitClaimCertCommand()
			cmd.SetArgs(tc.args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestInitClaimCert_HappyPath drives the begin/sign/complete pipeline.
func TestInitClaimCert_HappyPath(t *testing.T) {
	defer resetInitFlags()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "alice.key")
	certPath := filepath.Join(dir, "alice.crt")
	writeECPEM(t, keyPath)

	fc := &fakeCeremonyClient{}
	defer withFakeCeremony(fc)()

	cmd := newInitClaimCertCommand()
	cmd.SetArgs([]string{
		"--username", "alice",
		"--csr-key", keyPath,
		"--output", certPath,
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())

	require.NotNil(t, fc.lastBeginReq)
	assert.Equal(t, "alice", fc.lastBeginReq.Username)
	require.NotNil(t, fc.lastCompleteReq)
	assert.Equal(t, "alice", fc.lastCompleteReq.Username)
	assert.Equal(t, "deadbeef", fc.lastCompleteReq.NonceHex)
	_, err := hex.DecodeString(fc.lastCompleteReq.Signature)
	assert.NoError(t, err, "signature must be hex-encoded")

	bytesOut, err := os.ReadFile(certPath)
	require.NoError(t, err)
	assert.Contains(t, string(bytesOut), "BEGIN CERTIFICATE")
}

// =============================================================================
// claim-share
// =============================================================================

// TestInitClaimShare_FlagValidation checks the required-flag matrix.
func TestInitClaimShare_FlagValidation(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "missing-username",
			args:    []string{"--tls-cert", "c", "--tls-key", "k", "--output", "o"},
			wantErr: "--username is required",
		},
		{
			name:    "missing-tls-cert",
			args:    []string{"--username", "alice", "--tls-key", "k", "--output", "o"},
			wantErr: "--tls-cert is required",
		},
		{
			name:    "missing-tls-key",
			args:    []string{"--username", "alice", "--tls-cert", "c", "--output", "o"},
			wantErr: "--tls-key is required",
		},
		{
			name:    "missing-output",
			args:    []string{"--username", "alice", "--tls-cert", "c", "--tls-key", "k"},
			wantErr: "--output is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer resetInitFlags()
			cmd := newInitClaimShareCommand()
			cmd.SetArgs(tc.args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestInitClaimShare_Success drives the begin/sign/complete handshake and
// writes the share JSON to --output.
func TestInitClaimShare_Success(t *testing.T) {
	defer resetInitFlags()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "alice.key")
	outPath := filepath.Join(dir, "alice.share.json")
	writeECPEM(t, keyPath)

	fc := &fakeCeremonyClient{}
	defer withFakeCeremony(fc)()

	cmd := newInitClaimShareCommand()
	cmd.SetArgs([]string{
		"--username", "alice",
		"--tls-cert", "/dev/null",
		"--tls-key", keyPath,
		"--output", outPath,
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastBeginShareReq)
	assert.Equal(t, "alice", fc.lastBeginShareReq.Username)
	require.NotNil(t, fc.lastCompleteShareReq)
	assert.Equal(t, "alice", fc.lastCompleteShareReq.Username)
	assert.Equal(t, "deadbeef", fc.lastCompleteShareReq.NonceHex)
	assert.NotEmpty(t, fc.lastCompleteShareReq.Signature, "signature must be sent as raw bytes")

	got, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Contains(t, string(got), `"share"`)
}

// TestInitClaimShare_UnreadableKey rejects a signing key that cannot be
// parsed instead of sending an unsigned complete request.
func TestInitClaimShare_UnreadableKey(t *testing.T) {
	defer resetInitFlags()
	dir := t.TempDir()
	outPath := filepath.Join(dir, "alice.share.json")

	fc := &fakeCeremonyClient{}
	defer withFakeCeremony(fc)()

	cmd := newInitClaimShareCommand()
	cmd.SetArgs([]string{
		"--username", "alice",
		"--tls-cert", "/dev/null",
		"--tls-key", "/dev/null",
		"--output", outPath,
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.Error(t, cmd.Execute())
	assert.Nil(t, fc.lastCompleteShareReq, "complete must not be attempted without a valid signing key")
}

// =============================================================================
// sign-csr
// =============================================================================

// TestInitSignCSR_FlagValidation checks the required-flag matrix.
func TestInitSignCSR_FlagValidation(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "missing-username",
			args:    []string{"--so-pin", "p", "--csr", "c.csr", "--role", "r"},
			wantErr: "--username is required",
		},
		{
			name:    "missing-so-pin",
			args:    []string{"--username", "u", "--csr", "c.csr", "--role", "r"},
			wantErr: "--so-pin is required",
		},
		{
			name:    "missing-csr",
			args:    []string{"--username", "u", "--so-pin", "p", "--role", "r"},
			wantErr: "--csr is required",
		},
		{
			name:    "missing-role",
			args:    []string{"--username", "u", "--so-pin", "p", "--csr", "c.csr"},
			wantErr: "--role is required",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer resetInitFlags()
			cmd := newInitSignCSRCommand()
			cmd.SetArgs(tc.args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			err := cmd.Execute()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestInitSignCSR_HappyPath drives the SDK call.
func TestInitSignCSR_HappyPath(t *testing.T) {
	defer resetInitFlags()
	dir := t.TempDir()
	csrPath := filepath.Join(dir, "alice.csr")
	require.NoError(t, os.WriteFile(csrPath, []byte("-----BEGIN CERTIFICATE REQUEST-----\nx\n-----END CERTIFICATE REQUEST-----\n"), 0o600))

	fc := &fakeCeremonyClient{}
	defer withFakeCeremony(fc)()

	cmd := newInitSignCSRCommand()
	cmd.SetArgs([]string{
		"--username", "alice",
		"--so-pin", "secret",
		"--csr", csrPath,
		"--role", "officer",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastSignReq)
	assert.Equal(t, "alice", fc.lastSignReq.Username)
	assert.Equal(t, "secret", fc.lastSignReq.SOPin)
	assert.Equal(t, "officer", fc.lastSignReq.Role)
	assert.Contains(t, fc.lastSignReq.CSRPEM, "BEGIN CERTIFICATE REQUEST")
	assert.Contains(t, out.String(), "BEGIN CERTIFICATE")
}

// =============================================================================
// enroll-external-officer
// =============================================================================

// TestInitEnrollExternalOfficer_RequiresUsername rejects empty --username.
func TestInitEnrollExternalOfficer_RequiresUsername(t *testing.T) {
	defer resetInitFlags()
	cmd := newInitEnrollExternalOfficerCommand()
	cmd.SetArgs([]string{
		"--cert", "c.pem",
		"--share-index", "1",
		"--auth-factor", "mtls",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--username is required")
}

// TestInitEnrollExternalOfficer_CertPubKeyXOR rejects missing/duplicated trust input.
func TestInitEnrollExternalOfficer_CertPubKeyXOR(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{
			name: "neither",
			args: []string{
				"--username", "u",
				"--share-index", "1",
				"--auth-factor", "mtls",
			},
		},
		{
			name: "both",
			args: []string{
				"--username", "u",
				"--cert", "c.pem",
				"--pubkey", "p.pem",
				"--share-index", "1",
				"--auth-factor", "mtls",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer resetInitFlags()
			cmd := newInitEnrollExternalOfficerCommand()
			cmd.SetArgs(tc.args)
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			err := cmd.Execute()
			require.Error(t, err)
			var typed *InitMutualExclusiveCertPubKeyError
			assert.ErrorAs(t, err, &typed)
		})
	}
}

// TestInitEnrollExternalOfficer_RejectsBadAuthFactor verifies validation.
func TestInitEnrollExternalOfficer_RejectsBadAuthFactor(t *testing.T) {
	defer resetInitFlags()
	dir := t.TempDir()
	certPath := filepath.Join(dir, "c.pem")
	require.NoError(t, os.WriteFile(certPath, []byte("x"), 0o600))

	cmd := newInitEnrollExternalOfficerCommand()
	cmd.SetArgs([]string{
		"--username", "u",
		"--cert", certPath,
		"--share-index", "1",
		"--auth-factor", "totp",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	var typed *InitInvalidAuthFactorError
	assert.ErrorAs(t, err, &typed)
}

// TestInitEnrollExternalOfficer_AuthFactorAccepted accepts each canonical factor.
func TestInitEnrollExternalOfficer_AuthFactorAccepted(t *testing.T) {
	for _, af := range []string{"mtls", "webauthn", "piv"} {
		t.Run(af, func(t *testing.T) {
			defer resetInitFlags()
			dir := t.TempDir()
			certPath := filepath.Join(dir, "c.pem")
			require.NoError(t, os.WriteFile(certPath, []byte("x"), 0o600))

			fc := &fakeCeremonyClient{}
			defer withFakeCeremony(fc)()

			cmd := newInitEnrollExternalOfficerCommand()
			cmd.SetArgs([]string{
				"--username", "alice",
				"--cert", certPath,
				"--share-index", "2",
				"--auth-factor", af,
			})
			var out bytes.Buffer
			cmd.SetOut(&out)
			cmd.SetErr(&out)
			require.NoError(t, cmd.Execute())
			require.NotNil(t, fc.lastEnrollReq)
			assert.Equal(t, af, fc.lastEnrollReq.AuthFactor)
			assert.Equal(t, 2, fc.lastEnrollReq.ShareIndex)
		})
	}
}

// TestInitEnrollExternalOfficer_PubKeyPath verifies the --pubkey route.
func TestInitEnrollExternalOfficer_PubKeyPath(t *testing.T) {
	defer resetInitFlags()
	dir := t.TempDir()
	pubPath := filepath.Join(dir, "p.pem")
	require.NoError(t, os.WriteFile(pubPath, []byte("public-key-data"), 0o600))

	fc := &fakeCeremonyClient{}
	defer withFakeCeremony(fc)()

	cmd := newInitEnrollExternalOfficerCommand()
	cmd.SetArgs([]string{
		"--username", "alice",
		"--pubkey", pubPath,
		"--share-index", "0",
		"--auth-factor", "webauthn",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastEnrollReq)
	assert.Equal(t, "public-key-data", fc.lastEnrollReq.PubKeyPEM)
	assert.Empty(t, fc.lastEnrollReq.CertPEM)
}

// =============================================================================
// Errors
// =============================================================================

// TestInitMutualExclusiveCertPubKeyError_Message verifies the error string.
func TestInitMutualExclusiveCertPubKeyError_Message(t *testing.T) {
	err := &InitMutualExclusiveCertPubKeyError{}
	assert.Contains(t, err.Error(), "--cert or --pubkey")
}

// TestInitInvalidAuthFactorError_Message verifies the error string.
func TestInitInvalidAuthFactorError_Message(t *testing.T) {
	err := &InitInvalidAuthFactorError{Got: "bogus"}
	assert.Contains(t, err.Error(), "bogus")
}

// TestInitWriteFileError_UnwrapAndMessage verifies wrap semantics.
func TestInitWriteFileError_UnwrapAndMessage(t *testing.T) {
	inner := errors.New("disk full")
	err := &InitWriteFileError{Path: "/x", Err: inner}
	assert.Contains(t, err.Error(), "/x")
	assert.Contains(t, err.Error(), "disk full")
	assert.ErrorIs(t, err, inner)
}

// TestInitCSRKeySignError_UnwrapAndMessage verifies wrap semantics.
func TestInitCSRKeySignError_UnwrapAndMessage(t *testing.T) {
	inner := errors.New("not a key")
	err := &InitCSRKeySignError{Path: "/k", Err: inner}
	assert.Contains(t, err.Error(), "/k")
	assert.Contains(t, err.Error(), "not a key")
	assert.ErrorIs(t, err, inner)
}
