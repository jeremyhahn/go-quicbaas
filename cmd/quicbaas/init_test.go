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
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock CeremonyClient
// =============================================================================

type fakeCeremonyClient struct {
	statusFunc        func(ctx context.Context) (*qrdbtransport.CeremonyStatusResponse, error)
	signCSRFunc       func(ctx context.Context, req *qrdbtransport.SignCSRInitRequest) (*qrdbtransport.SignCSRInitResponse, error)
	initializeFunc    func(ctx context.Context, req *qrdbtransport.CeremonyInitializeRequest) (*qrdbtransport.CeremonyInitializeResponse, error)
	lastInitializeReq *qrdbtransport.CeremonyInitializeRequest

	lastSignReq *qrdbtransport.SignCSRInitRequest
}

// CeremonyInitialize records the request and delegates to the injected func.
func (f *fakeCeremonyClient) CeremonyInitialize(ctx context.Context, req *qrdbtransport.CeremonyInitializeRequest) (*qrdbtransport.CeremonyInitializeResponse, error) {
	f.lastInitializeReq = req
	if f.initializeFunc != nil {
		return f.initializeFunc(ctx, req)
	}
	return &qrdbtransport.CeremonyInitializeResponse{}, nil
}

func (f *fakeCeremonyClient) CeremonyStatus(ctx context.Context) (*qrdbtransport.CeremonyStatusResponse, error) {
	if f.statusFunc != nil {
		return f.statusFunc(ctx)
	}
	return &qrdbtransport.CeremonyStatusResponse{State: "operational"}, nil
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
	initUsername = ""
	initOutputPath = ""
	initTLSCertPath = ""
	initTLSKeyPath = ""
	initSOPin = ""
	initCSRPath = ""
	initRole = ""
	initOutputFmt = "table"
}

// writeECPEM writes a freshly generated EC PRIVATE KEY PEM file to path and
// returns its public key, for tests that verify what the key signed.
func writeECPEM(t *testing.T, path string) *ecdsa.PublicKey {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	der, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)
	out := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
	require.NoError(t, os.WriteFile(path, out, 0o600))
	return &priv.PublicKey
}

// =============================================================================
// Command tree shape
// =============================================================================

// TestNewInitCommand_Shape verifies all canonical verbs are wired.
func TestNewInitCommand_Shape(t *testing.T) {
	cmd := newInitCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "init", cmd.Use)

	got := subcommandNames(cmd)
	for _, verb := range canonicalInitVerbs {
		assert.True(t, got[verb], "expected 'init %s' subcommand", verb)
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
// Errors
// =============================================================================

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

// TestInitClaimShare_Success drives the begin/sign/complete handshake on the
// unseal domain, signs the claim under the system label, and writes the
// sealed envelope to --output verbatim.
func TestInitClaimShare_Success(t *testing.T) {
	defer resetInitFlags()
	defer resetBarrierFlags()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "alice.key")
	outPath := filepath.Join(dir, "alice.share.json")
	pub := writeECPEM(t, keyPath)

	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

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
	require.NotNil(t, fc.lastShareClaimBegin)
	assert.Equal(t, "alice", fc.lastShareClaimBegin.Username)
	require.NotNil(t, fc.lastShareClaimComplete)
	assert.Equal(t, "alice", fc.lastShareClaimComplete.Username)
	assert.Equal(t, "deadbeef", fc.lastShareClaimComplete.NonceHex)
	assertShareClaimSignature(t, pub, qrdbsdk.SystemClaimLabel, "alice", "deadbeef", fc.lastShareClaimComplete.Signature)
	assert.Zero(t, fc.recoveryClaims, "the unseal domain must not claim through the recovery verbs")

	got, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, `{"share":"sealed"}`, string(got))
}

// TestInitClaimShare_RecoveryDomainUsesTheRecoveryVerbs proves shareClaimFor
// routes --domain recovery to the Recovery* handshake and signs the claim
// under the recovery label.
func TestInitClaimShare_RecoveryDomainUsesTheRecoveryVerbs(t *testing.T) {
	defer resetInitFlags()
	defer resetBarrierFlags()
	dir := t.TempDir()
	keyPath := filepath.Join(dir, "alice.key")
	pub := writeECPEM(t, keyPath)

	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newInitClaimShareCommand()
	cmd.SetArgs([]string{
		"--username", "alice",
		"--tls-cert", "/dev/null",
		"--tls-key", keyPath,
		"--output", filepath.Join(dir, "alice.recovery.json"),
		"--domain", "recovery",
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.NoError(t, cmd.Execute())
	assert.Equal(t, 2, fc.recoveryClaims)
	require.NotNil(t, fc.lastShareClaimComplete)
	assertShareClaimSignature(t, pub, qrdbsdk.RecoveryClaimLabel, "alice", "deadbeef", fc.lastShareClaimComplete.Signature)
}

// TestInitClaimShare_RejectsUnknownDomain refuses before dialling.
func TestInitClaimShare_RejectsUnknownDomain(t *testing.T) {
	defer resetInitFlags()
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newInitClaimShareCommand()
	cmd.SetArgs([]string{
		"--username", "alice", "--tls-cert", "c", "--tls-key", "k", "--output", "o", "--domain", "root",
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--domain must be")
	assert.Nil(t, fc.lastShareClaimBegin)
}

// TestInitClaimShare_UnreadableKey rejects a signing key that cannot be
// parsed instead of sending an unsigned complete request.
func TestInitClaimShare_UnreadableKey(t *testing.T) {
	defer resetInitFlags()
	defer resetBarrierFlags()
	outPath := filepath.Join(t.TempDir(), "alice.share.json")

	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newInitClaimShareCommand()
	cmd.SetArgs([]string{
		"--username", "alice",
		"--tls-cert", "/dev/null",
		"--tls-key", "/dev/null",
		"--output", outPath,
	})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})

	require.Error(t, cmd.Execute())
	assert.Nil(t, fc.lastShareClaimComplete, "complete must not be attempted without a valid signing key")
}
