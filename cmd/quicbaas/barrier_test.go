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
// 'quicbaas barrier' command tree.
package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Mock BarrierClient
// =============================================================================

// fakeBarrierClient is a hand-rolled BarrierClient mock that records
// the last call and returns canned responses. Each Func field is
// optional; nil fields return zero-valued responses.
type fakeBarrierClient struct {
	statusFunc          func(ctx context.Context) (*qrdbtransport.BarrierStatusResponse, error)
	initFunc            func(ctx context.Context, req *qrdbtransport.BarrierInitializeRequest) error
	initShamirFunc      func(ctx context.Context, req *qrdbtransport.BarrierInitializeShamirRequest) (*qrdbtransport.BarrierInitializeShamirResponse, error)
	unsealFunc          func(ctx context.Context, req *qrdbtransport.BarrierUnsealRequest) error
	unsealSharesFunc    func(ctx context.Context, req *qrdbtransport.BarrierUnsealSharesRequest) error
	sealFunc            func(ctx context.Context) error
	rekeyFunc           func(ctx context.Context, req *qrdbtransport.BarrierRekeyRequest) (*qrdbtransport.BarrierRekeyResponse, error)
	genRecoveryFunc     func(ctx context.Context, req *qrdbtransport.BarrierGenerateRecoveryKeysRequest) (*qrdbtransport.BarrierRecoveryKeysResponse, error)
	recoverFunc         func(ctx context.Context, req *qrdbtransport.BarrierRecoverWithKeysRequest) error
	lastInitReq         *qrdbtransport.BarrierInitializeRequest
	lastInitShamirReq   *qrdbtransport.BarrierInitializeShamirRequest
	lastUnsealReq       *qrdbtransport.BarrierUnsealRequest
	lastUnsealSharesReq *qrdbtransport.BarrierUnsealSharesRequest
	lastRekeyReq        *qrdbtransport.BarrierRekeyRequest
	lastGenRecoveryReq  *qrdbtransport.BarrierGenerateRecoveryKeysRequest
	lastRecoverReq      *qrdbtransport.BarrierRecoverWithKeysRequest

	rootTokenFunc          func(ctx context.Context, req *qrdbtransport.BarrierGenerateRootTokenRequest) (*qrdbtransport.BarrierRootTokenResponse, error)
	enableEncryptionFunc   func(ctx context.Context, req *qrdbtransport.BarrierEnableEncryptionRequest) (*qrdbtransport.BarrierEnableEncryptionResponse, error)
	lastRootTokenReq       *qrdbtransport.BarrierGenerateRootTokenRequest
	lastEnableEncReq       *qrdbtransport.BarrierEnableEncryptionRequest
	officerRegisterFunc    func(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error)
	officerListFunc        func(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error)
	officerClaimStatusFunc func(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error)
	lastOfficerRegisterReq *qrdbtransport.OfficerRegisterRequest
}

// BarrierGenerateRootToken records the request and delegates to the injected func.
func (f *fakeBarrierClient) BarrierGenerateRootToken(ctx context.Context, req *qrdbtransport.BarrierGenerateRootTokenRequest) (*qrdbtransport.BarrierRootTokenResponse, error) {
	f.lastRootTokenReq = req
	if f.rootTokenFunc != nil {
		return f.rootTokenFunc(ctx, req)
	}
	return &qrdbtransport.BarrierRootTokenResponse{}, nil
}

// BarrierEnableEncryption records the request and delegates to the injected func.
func (f *fakeBarrierClient) BarrierEnableEncryption(ctx context.Context, req *qrdbtransport.BarrierEnableEncryptionRequest) (*qrdbtransport.BarrierEnableEncryptionResponse, error) {
	f.lastEnableEncReq = cloneBarrierEnableEncReq(req)
	if f.enableEncryptionFunc != nil {
		return f.enableEncryptionFunc(ctx, req)
	}
	return &qrdbtransport.BarrierEnableEncryptionResponse{}, nil
}

// OfficerRegister records the request and delegates to the injected func.
func (f *fakeBarrierClient) OfficerRegister(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	f.lastOfficerRegisterReq = req
	if f.officerRegisterFunc != nil {
		return f.officerRegisterFunc(ctx, req)
	}
	return &qrdbtransport.OfficerRegisterResponse{}, nil
}

// OfficerList delegates to the injected func.
func (f *fakeBarrierClient) OfficerList(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error) {
	if f.officerListFunc != nil {
		return f.officerListFunc(ctx)
	}
	return nil, nil
}

// OfficerClaimStatus delegates to the injected func.
func (f *fakeBarrierClient) OfficerClaimStatus(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error) {
	if f.officerClaimStatusFunc != nil {
		return f.officerClaimStatusFunc(ctx)
	}
	return nil, nil
}

func (f *fakeBarrierClient) BarrierStatus(ctx context.Context) (*qrdbtransport.BarrierStatusResponse, error) {
	if f.statusFunc != nil {
		return f.statusFunc(ctx)
	}
	return &qrdbtransport.BarrierStatusResponse{Sealed: true, Strategy: "shamir"}, nil
}

func (f *fakeBarrierClient) BarrierInitialize(ctx context.Context, req *qrdbtransport.BarrierInitializeRequest) error {
	f.lastInitReq = cloneBarrierInitReq(req)
	if f.initFunc != nil {
		return f.initFunc(ctx, req)
	}
	return nil
}

func (f *fakeBarrierClient) BarrierInitializeShamir(ctx context.Context, req *qrdbtransport.BarrierInitializeShamirRequest) (*qrdbtransport.BarrierInitializeShamirResponse, error) {
	f.lastInitShamirReq = cloneBarrierInitShamirReq(req)
	if f.initShamirFunc != nil {
		return f.initShamirFunc(ctx, req)
	}
	return &qrdbtransport.BarrierInitializeShamirResponse{
		Threshold:   req.Threshold,
		TotalShares: req.TotalShares,
		ShareCount:  req.TotalShares,
	}, nil
}

func (f *fakeBarrierClient) BarrierUnseal(ctx context.Context, req *qrdbtransport.BarrierUnsealRequest) error {
	f.lastUnsealReq = cloneBarrierUnsealReq(req)
	if f.unsealFunc != nil {
		return f.unsealFunc(ctx, req)
	}
	return nil
}

func (f *fakeBarrierClient) BarrierUnsealWithShares(ctx context.Context, req *qrdbtransport.BarrierUnsealSharesRequest) error {
	f.lastUnsealSharesReq = req
	if f.unsealSharesFunc != nil {
		return f.unsealSharesFunc(ctx, req)
	}
	return nil
}

func (f *fakeBarrierClient) BarrierSeal(ctx context.Context) error {
	if f.sealFunc != nil {
		return f.sealFunc(ctx)
	}
	return nil
}

func (f *fakeBarrierClient) BarrierRekey(ctx context.Context, req *qrdbtransport.BarrierRekeyRequest) (*qrdbtransport.BarrierRekeyResponse, error) {
	f.lastRekeyReq = req
	if f.rekeyFunc != nil {
		return f.rekeyFunc(ctx, req)
	}
	return &qrdbtransport.BarrierRekeyResponse{
		Threshold:   req.Threshold,
		TotalShares: req.Total,
		ShareCount:  req.Total,
	}, nil
}

func (f *fakeBarrierClient) BarrierGenerateRecoveryKeys(ctx context.Context, req *qrdbtransport.BarrierGenerateRecoveryKeysRequest) (*qrdbtransport.BarrierRecoveryKeysResponse, error) {
	f.lastGenRecoveryReq = req
	if f.genRecoveryFunc != nil {
		return f.genRecoveryFunc(ctx, req)
	}
	return &qrdbtransport.BarrierRecoveryKeysResponse{
		Threshold:  req.Threshold,
		Total:      req.Total,
		ShareCount: req.Total,
	}, nil
}

func (f *fakeBarrierClient) BarrierRecoverWithKeys(ctx context.Context, req *qrdbtransport.BarrierRecoverWithKeysRequest) error {
	f.lastRecoverReq = req
	if f.recoverFunc != nil {
		return f.recoverFunc(ctx, req)
	}
	return nil
}

// withFakeBarrier installs fc as the active barrier factory and returns
// a restore func.
func withFakeBarrier(fc *fakeBarrierClient) func() {
	orig := barrierClientFactory
	barrierClientFactory = func() (BarrierClient, error) { return fc, nil }
	return func() { barrierClientFactory = orig }
}

// resetBarrierFlags zeroes the package-level flag variables so a stale
// value from a prior test does not leak into the next.
func resetBarrierFlags() {
	barrierSecret = ""
	barrierShares = nil
	barrierSharesFile = ""
	barrierRecoverySharesFile = ""
	barrierThreshold = 0
	barrierTotalShares = 0
	barrierOutputFmt = "table"
	barrierAssumeYes = false
	barrierStrategy = ""
	barrierSOPin = ""
	barrierUserPin = ""
	barrierRecoveryShares = nil
}

// =============================================================================
// Command tree shape
// =============================================================================

// TestNewBarrierCommand_Shape ensures every canonical verb is wired.
func TestNewBarrierCommand_Shape(t *testing.T) {
	cmd := newBarrierCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "barrier", cmd.Use)

	expected := map[string]bool{
		"status":        false,
		"init":          false,
		"init-shamir":   false,
		"unseal":        false,
		"unseal-shares": false,
		"seal":          false,
		"rekey":         false,
		"recovery":      false,
	}
	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}
	for verb, present := range expected {
		assert.True(t, present, "expected 'barrier %s' subcommand", verb)
	}
}

// TestNewBarrierRecoveryCommand_Shape verifies the recovery subgroup.
func TestNewBarrierRecoveryCommand_Shape(t *testing.T) {
	cmd := newBarrierRecoveryCommand()
	require.NotNil(t, cmd)
	expected := map[string]bool{"generate": false, "recover": false}
	for _, sub := range cmd.Commands() {
		if _, ok := expected[sub.Name()]; ok {
			expected[sub.Name()] = true
		}
	}
	for verb, present := range expected {
		assert.True(t, present, "expected 'recovery %s' subcommand", verb)
	}
}

// =============================================================================
// status
// =============================================================================

// TestBarrierStatus_Success verifies the happy path.
func TestBarrierStatus_Success(t *testing.T) {
	defer resetBarrierFlags()
	defer withFakeBarrier(&fakeBarrierClient{
		statusFunc: func(ctx context.Context) (*qrdbtransport.BarrierStatusResponse, error) {
			return &qrdbtransport.BarrierStatusResponse{
				Sealed: false, Strategy: "shamir", HardwareBacked: true,
				InitializedAt: "2026-04-30T00:00:00Z",
			}, nil
		},
	})()

	cmd := newBarrierStatusCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "BARRIER STATUS")
	assert.Contains(t, out.String(), "shamir")
}

// TestBarrierStatus_FactoryError surfaces typed errors.
func TestBarrierStatus_FactoryError(t *testing.T) {
	defer resetBarrierFlags()
	orig := barrierClientFactory
	defer func() { barrierClientFactory = orig }()

	wantErr := errors.New("factory failure")
	barrierClientFactory = func() (BarrierClient, error) { return nil, wantErr }

	cmd := newBarrierStatusCommand()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "factory failure")
}

// =============================================================================
// init
// =============================================================================

// TestBarrierInit_StrategyValidation covers --barrier-strategy validation.
func TestBarrierInit_StrategyValidation(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "invalid-strategy",
			args:    []string{"--barrier-strategy", "bogus", "--passphrase", "x"},
			wantErr: "must be one of none, passphrase, shamir",
		},
		{
			name:    "missing-secret",
			args:    []string{"--barrier-strategy", "passphrase"},
			wantErr: "--passphrase is required",
		},
		{
			name:    "none-with-secret",
			args:    []string{"--barrier-strategy", "none", "--passphrase", "x"},
			wantErr: "--passphrase is not allowed when --barrier-strategy=none",
		},
		{
			name:    "user-pin-without-so-pin",
			args:    []string{"--barrier-strategy", "passphrase", "--passphrase", "x", "--user-pin", "u"},
			wantErr: "--user-pin requires --so-pin",
		},
		{
			name:    "so-pin-without-user-pin",
			args:    []string{"--barrier-strategy", "passphrase", "--passphrase", "x", "--so-pin", "s"},
			wantErr: "--so-pin requires --user-pin",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer resetBarrierFlags()
			cmd := newBarrierInitCommand()
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

// TestBarrierInit_NoneRejectsPINs verifies that --barrier-strategy=none
// rejects PINs with the typed PINsNotAllowedError.
func TestBarrierInit_NoneRejectsPINs(t *testing.T) {
	defer resetBarrierFlags()
	cmd := newBarrierInitCommand()
	cmd.SetArgs([]string{"--barrier-strategy", "none", "--so-pin", "s", "--user-pin", "u"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	var typed *PINsNotAllowedError
	require.ErrorAs(t, err, &typed, "want typed PINsNotAllowedError, got %T", err)
	assert.Equal(t, "none", typed.Strategy)
	assert.Equal(t, "init", typed.Op)
}

// TestBarrierInit_Success forwards every field to the SDK.
func TestBarrierInit_Success(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierInitCommand()
	cmd.SetArgs([]string{
		"--barrier-strategy", "passphrase",
		"--passphrase", "topsecret",
		"--so-pin", "SO123",
		"--user-pin", "USR123",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastInitReq)
	assert.Equal(t, "topsecret", string(fc.lastInitReq.Secret))
	assert.Equal(t, "passphrase", fc.lastInitReq.BarrierStrategy)
	assert.Equal(t, "SO123", fc.lastInitReq.SOPin)
	assert.Equal(t, "USR123", fc.lastInitReq.UserPin)
}

// TestBarrierInit_NoneStrategy succeeds without --secret or PINs.
func TestBarrierInit_NoneStrategy(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierInitCommand()
	cmd.SetArgs([]string{"--barrier-strategy", "none"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastInitReq)
	assert.Equal(t, "none", fc.lastInitReq.BarrierStrategy)
	assert.Empty(t, fc.lastInitReq.Secret)
	assert.Empty(t, fc.lastInitReq.SOPin)
	assert.Empty(t, fc.lastInitReq.UserPin)
}

// =============================================================================
// init-shamir
// =============================================================================

// TestBarrierInitShamir_FlagValidation covers all input-validation branches.
func TestBarrierInitShamir_FlagValidation(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "missing-secret",
			args:    []string{"--threshold", "2", "--total", "3"},
			wantErr: "--passphrase is required",
		},
		{
			name:    "missing-threshold",
			args:    []string{"--passphrase", "x", "--total", "3"},
			wantErr: "--threshold must be > 0",
		},
		{
			name:    "missing-total",
			args:    []string{"--passphrase", "x", "--threshold", "2"},
			wantErr: "--total must be > 0",
		},
		{
			name:    "threshold-exceeds-total",
			args:    []string{"--passphrase", "x", "--threshold", "5", "--total", "3"},
			wantErr: "--threshold cannot exceed --total",
		},
		{
			name:    "user-pin-without-so-pin",
			args:    []string{"--passphrase", "x", "--threshold", "2", "--total", "3", "--user-pin", "u"},
			wantErr: "--user-pin requires --so-pin",
		},
		{
			name:    "so-pin-without-user-pin",
			args:    []string{"--passphrase", "x", "--threshold", "2", "--total", "3", "--so-pin", "s"},
			wantErr: "--so-pin requires --user-pin",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer resetBarrierFlags()
			cmd := newBarrierInitShamirCommand()
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

// TestBarrierInitShamir_Success verifies the SDK dispatch, including PINs.
func TestBarrierInitShamir_Success(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierInitShamirCommand()
	cmd.SetArgs([]string{
		"--passphrase", "x",
		"--threshold", "2",
		"--total", "3",
		"--so-pin", "SO",
		"--user-pin", "U",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastInitShamirReq)
	assert.Equal(t, "x", string(fc.lastInitShamirReq.Secret))
	assert.Equal(t, 2, fc.lastInitShamirReq.Threshold)
	assert.Equal(t, 3, fc.lastInitShamirReq.TotalShares)
	assert.Equal(t, "SO", fc.lastInitShamirReq.SOPin)
	assert.Equal(t, "U", fc.lastInitShamirReq.UserPin)
}

// =============================================================================
// unseal (passphrase only)
// =============================================================================

// TestBarrierUnseal_RequiresSecret rejects empty --secret.
func TestBarrierUnseal_RequiresSecret(t *testing.T) {
	defer resetBarrierFlags()
	cmd := newBarrierUnsealCommand()
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--passphrase is required")
}

// TestBarrierUnseal_Success forwards the secret to the SDK.
func TestBarrierUnseal_Success(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierUnsealCommand()
	cmd.SetArgs([]string{"--passphrase", "pass"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastUnsealReq)
	assert.Equal(t, "pass", string(fc.lastUnsealReq.Secret))
}

// TestBarrierUnseal_DoesNotAcceptShareFlag verifies --share is NOT on
// the unseal command (canonical surface moved it to unseal-shares).
func TestBarrierUnseal_DoesNotAcceptShareFlag(t *testing.T) {
	cmd := newBarrierUnsealCommand()
	assert.Nil(t, cmd.Flags().Lookup("share"),
		"--share must not be on 'barrier unseal'; canonical surface uses 'unseal-shares'")
}

// =============================================================================
// unseal-shares
// =============================================================================

// TestBarrierUnsealShares_RequiresInput rejects empty input.
func TestBarrierUnsealShares_RequiresInput(t *testing.T) {
	defer resetBarrierFlags()
	cmd := newBarrierUnsealSharesCommand()
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	var typed *BarrierShareInputError
	require.ErrorAs(t, err, &typed)
	assert.Contains(t, typed.Error(), "one of --share")
}

// TestBarrierUnsealShares_MutuallyExclusive rejects --share + --shares-file.
func TestBarrierUnsealShares_MutuallyExclusive(t *testing.T) {
	defer resetBarrierFlags()
	dir := t.TempDir()
	path := filepath.Join(dir, "shares.txt")
	require.NoError(t, os.WriteFile(path, []byte("a\n"), 0o600))

	cmd := newBarrierUnsealSharesCommand()
	cmd.SetArgs([]string{"--share", "a", "--shares-file", path})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	var typed *BarrierShareInputError
	require.ErrorAs(t, err, &typed)
	assert.Contains(t, typed.Error(), "exactly one")
}

// TestBarrierUnsealShares_RepeatableShareFlag passes a repeatable
// --share through to the SDK.
func TestBarrierUnsealShares_RepeatableShareFlag(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierUnsealSharesCommand()
	cmd.SetArgs([]string{"--share", "a", "--share", "b", "--share", "c"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastUnsealSharesReq)
	assert.Equal(t, []string{"a", "b", "c"}, fc.lastUnsealSharesReq.Shares)
}

// TestBarrierUnsealShares_ReadsLines reads one share per line, ignoring
// blanks/comments.
func TestBarrierUnsealShares_ReadsLines(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	dir := t.TempDir()
	path := filepath.Join(dir, "shares.txt")
	body := "share-a\n\n# a comment\nshare-b\n   share-c   \n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	cmd := newBarrierUnsealSharesCommand()
	cmd.SetArgs([]string{"--shares-file", path})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastUnsealSharesReq)
	assert.Equal(t, []string{"share-a", "share-b", "share-c"}, fc.lastUnsealSharesReq.Shares)
}

// TestBarrierUnsealShares_EmptyFile rejects files with no shares.
func TestBarrierUnsealShares_EmptyFile(t *testing.T) {
	defer resetBarrierFlags()
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.txt")
	require.NoError(t, os.WriteFile(path, []byte("# only comments\n\n"), 0o600))

	cmd := newBarrierUnsealSharesCommand()
	cmd.SetArgs([]string{"--shares-file", path})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	var typed *BarrierSharesFileEmptyError
	require.ErrorAs(t, err, &typed)
}

// TestBarrierUnsealShares_FileMissing rejects non-existent paths.
func TestBarrierUnsealShares_FileMissing(t *testing.T) {
	defer resetBarrierFlags()
	cmd := newBarrierUnsealSharesCommand()
	cmd.SetArgs([]string{"--shares-file", "/nonexistent/shares.txt"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	var typed *BarrierSharesFileError
	require.ErrorAs(t, err, &typed)
}

// =============================================================================
// seal
// =============================================================================

// TestBarrierSeal_AssumeYes skips the prompt with --assume-yes.
func TestBarrierSeal_AssumeYes(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierSealCommand()
	cmd.SetArgs([]string{"--assume-yes"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "Barrier sealed successfully")
}

// TestBarrierSeal_AcceptsSOPin verifies the canonical --so-pin flag is
// accepted (parsed for surface parity).
//
// This is the half of the design #491 deliberately did NOT change: it chose
// option A (soften the help that demanded a PIN the verb discards) over
// option C (drop the flag). Removing the flag would give up the four-CLI
// lock-step newBarrierSealCommand's doc comment buys, and would have to be
// undone when the SDK gains a request-bearing BarrierSeal. If this test is
// ever deleted, that is option C being taken by accident.
func TestBarrierSeal_AcceptsSOPin(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierSealCommand()
	cmd.SetArgs([]string{"--so-pin", "SO123", "--assume-yes"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Equal(t, "SO123", barrierSOPin)
}

// =============================================================================
// rekey
// =============================================================================

// TestBarrierRekey_FlagValidation covers all input-validation branches.
func TestBarrierRekey_FlagValidation(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "passphrase-flag-removed",
			args:    []string{"--passphrase", "x", "--threshold", "3", "--total", "5"},
			wantErr: "unknown flag: --passphrase",
		},
		{
			name:    "unpaired-threshold",
			args:    []string{"--threshold", "3"},
			wantErr: "must be supplied together",
		},
		{
			name:    "unpaired-total",
			args:    []string{"--total", "5"},
			wantErr: "must be supplied together",
		},
		{
			name:    "threshold-too-low",
			args:    []string{"--threshold", "1", "--total", "3"},
			wantErr: "--threshold must be >= 2",
		},
		{
			name:    "total-below-threshold",
			args:    []string{"--threshold", "5", "--total", "3"},
			wantErr: "--total must be >= --threshold",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer resetBarrierFlags()
			cmd := newBarrierRekeyCommand()
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

// TestBarrierRekey_SuccessWithShamir forwards Shamir params and renders.
func TestBarrierRekey_SuccessWithShamir(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierRekeyCommand()
	cmd.SetArgs([]string{
		"--so-pin", "SO",
		"--threshold", "3",
		"--total", "5",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastRekeyReq)
	assert.Equal(t, "SO", fc.lastRekeyReq.SOPin)
	assert.Equal(t, 3, fc.lastRekeyReq.Threshold)
	assert.Equal(t, 5, fc.lastRekeyReq.Total)
	assert.Contains(t, out.String(), "BARRIER REKEY COMPLETE")
}

// TestBarrierRekey_SuccessWithoutShamir omits threshold/total cleanly.
func TestBarrierRekey_SuccessWithoutShamir(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierRekeyCommand()
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastRekeyReq)
	assert.Equal(t, 0, fc.lastRekeyReq.Threshold)
	assert.Equal(t, 0, fc.lastRekeyReq.Total)
}

// =============================================================================
// recovery generate
// =============================================================================

// TestBarrierRecoveryGenerate_FlagValidation covers validation paths.
func TestBarrierRecoveryGenerate_FlagValidation(t *testing.T) {
	cases := []struct {
		name    string
		args    []string
		wantErr string
	}{
		{
			name:    "threshold-too-low",
			args:    []string{"--threshold", "1", "--total", "3"},
			wantErr: "--threshold must be >= 2",
		},
		{
			name:    "total-below-threshold",
			args:    []string{"--threshold", "5", "--total", "3"},
			wantErr: "--total must be >= --threshold",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer resetBarrierFlags()
			cmd := newBarrierRecoveryGenerateCommand()
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

// TestBarrierRecoveryGenerate_Success forwards params + SO PIN.
func TestBarrierRecoveryGenerate_Success(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierRecoveryGenerateCommand()
	cmd.SetArgs([]string{
		"--threshold", "3",
		"--total", "5",
		"--so-pin", "SO123",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastGenRecoveryReq)
	assert.Equal(t, 3, fc.lastGenRecoveryReq.Threshold)
	assert.Equal(t, 5, fc.lastGenRecoveryReq.Total)
	assert.Equal(t, "SO123", fc.lastGenRecoveryReq.SOPin)
	assert.Contains(t, out.String(), "RECOVERY KEYS GENERATED")
}

// =============================================================================
// recovery recover
// =============================================================================

// TestBarrierRecoveryRecover_RequiresShare rejects empty input. The verb
// shares resolveUnsealShares with its siblings now, so it reports the same
// typed error and names both input modes rather than only --share (#227).
func TestBarrierRecoveryRecover_RequiresShare(t *testing.T) {
	defer resetBarrierFlags()
	cmd := newBarrierRecoveryRecoverCommand()
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	var typed *BarrierShareInputError
	require.ErrorAs(t, err, &typed)
	assert.Contains(t, typed.Error(), "one of --share")
	assert.Contains(t, typed.Error(), "--shares-file")
	assert.Contains(t, typed.Error(), "barrier recovery recover")
}

// TestBarrierRecoveryRecover_Success forwards repeatable shares.
func TestBarrierRecoveryRecover_Success(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierRecoveryRecoverCommand()
	cmd.SetArgs([]string{"--share", "k1", "--share", "k2"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastRecoverReq)
	assert.Equal(t, []string{"k1", "k2"}, fc.lastRecoverReq.Keys)
}

// TestBarrierRecoveryRecover_ReadsSharesFile is the fix for #227: a recovery
// quorum can be supplied off argv.
//
// The assertion is the KEYS THAT REACHED THE SDK, not that the command ran.
// A test that only checked for a nil error would pass against a build where
// --shares-file parses and is then ignored, which is the failure mode worth
// guarding: the flag existing is not the same as the flag being read.
func TestBarrierRecoveryRecover_ReadsSharesFile(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	dir := t.TempDir()
	path := filepath.Join(dir, "recovery.txt")
	body := "key-a\n\n# an officer's note\nkey-b\n   key-c   \n"
	require.NoError(t, os.WriteFile(path, []byte(body), 0o600))

	cmd := newBarrierRecoveryRecoverCommand()
	cmd.SetArgs([]string{"--shares-file", path})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastRecoverReq)
	assert.Equal(t, []string{"key-a", "key-b", "key-c"}, fc.lastRecoverReq.Keys,
		"--shares-file was accepted but its keys did not reach the SDK")
}

// TestBarrierRecoveryRecover_MutuallyExclusive rejects --share + --shares-file,
// matching unseal-shares and root-token.
func TestBarrierRecoveryRecover_MutuallyExclusive(t *testing.T) {
	defer resetBarrierFlags()
	dir := t.TempDir()
	path := filepath.Join(dir, "recovery.txt")
	require.NoError(t, os.WriteFile(path, []byte("key-a\n"), 0o600))

	cmd := newBarrierRecoveryRecoverCommand()
	cmd.SetArgs([]string{"--share", "key-a", "--shares-file", path})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	var typed *BarrierShareInputError
	require.ErrorAs(t, err, &typed)
	assert.Contains(t, typed.Error(), "exactly one")
}

// TestBarrierRecoveryRecover_UsesItsOwnSharesFileVariable proves the two
// quorum verbs do not share flag state. barrierSharesFile belongs to
// unseal-shares and root-token; a recovery recover that read it would take a
// path the operator supplied to a different verb.
func TestBarrierRecoveryRecover_UsesItsOwnSharesFileVariable(t *testing.T) {
	defer resetBarrierFlags()
	dir := t.TempDir()
	path := filepath.Join(dir, "recovery.txt")
	require.NoError(t, os.WriteFile(path, []byte("key-a\n"), 0o600))

	cmd := newBarrierRecoveryRecoverCommand()
	require.NoError(t, cmd.Flags().Parse([]string{"--shares-file", path}))

	assert.Equal(t, path, barrierRecoverySharesFile)
	assert.Empty(t, barrierSharesFile,
		"recovery recover wrote the unseal-shares/root-token variable")
}

// TestSharesFileErrorsNameTheInvokingVerb covers the attribution defect the
// #227 work surfaced. readSharesFile is reached by three verbs, and both of
// its error types hard-coded "barrier unseal-shares" — so an operator whose
// `barrier root-token --shares-file` pointed at a bad path was sent off to
// debug a verb they had not run. BarrierShareInputError already carried an Op
// field for exactly this reason; these two did not.
func TestSharesFileErrorsNameTheInvokingVerb(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.txt")
	require.NoError(t, os.WriteFile(empty, []byte("# only comments\n"), 0o600))

	for _, op := range []string{
		"barrier unseal-shares",
		"barrier root-token",
		"barrier recovery recover",
	} {
		t.Run(op, func(t *testing.T) {
			_, err := readSharesFile("/nonexistent/shares.txt", op)
			require.Error(t, err)
			var missing *BarrierSharesFileError
			require.ErrorAs(t, err, &missing)
			assert.True(t, strings.HasPrefix(missing.Error(), op+": "),
				"unreadable-file error names the wrong verb: %q", missing.Error())

			_, err = readSharesFile(empty, op)
			require.Error(t, err)
			var blank *BarrierSharesFileEmptyError
			require.ErrorAs(t, err, &blank)
			assert.True(t, strings.HasPrefix(blank.Error(), op+": "),
				"empty-file error names the wrong verb: %q", blank.Error())
		})
	}
}

// =============================================================================
// Errors
// =============================================================================

// TestBarrierSharesFileError_UnwrapAndMessage verifies wrap semantics.
func TestBarrierSharesFileError_UnwrapAndMessage(t *testing.T) {
	inner := errors.New("permission denied")
	err := &BarrierSharesFileError{Path: "/x", Err: inner}
	assert.Contains(t, err.Error(), "/x")
	assert.Contains(t, err.Error(), "permission denied")
	assert.ErrorIs(t, err, inner)
}

// TestBarrierSharesFileEmptyError_Message verifies the error string.
func TestBarrierSharesFileEmptyError_Message(t *testing.T) {
	err := &BarrierSharesFileEmptyError{Path: "/empty"}
	assert.True(t, strings.Contains(err.Error(), "/empty"))
}

// TestPINsNotAllowedError_Message verifies the error string.
func TestPINsNotAllowedError_Message(t *testing.T) {
	err := &PINsNotAllowedError{Strategy: "none", Op: "init"}
	assert.Contains(t, err.Error(), "none")
	assert.Contains(t, err.Error(), "init")
	assert.Contains(t, err.Error(), "--so-pin")
}

// TestBarrierShareInputError_Message verifies the error string.
func TestBarrierShareInputError_Message(t *testing.T) {
	err := &BarrierShareInputError{Reason: "test reason"}
	assert.Contains(t, err.Error(), "test reason")
	assert.Contains(t, err.Error(), "barrier unseal-shares")
}

// =============================================================================
// root-token
// =============================================================================

// TestRunBarrierRootToken_SendsTheQuorum verifies the shares reach the server
// and the minted token is surfaced with its one-time warning.
func TestRunBarrierRootToken_SendsTheQuorum(t *testing.T) {
	resetBarrierFlags()
	fb := &fakeBarrierClient{
		rootTokenFunc: func(_ context.Context, _ *qrdbtransport.BarrierGenerateRootTokenRequest) (*qrdbtransport.BarrierRootTokenResponse, error) {
			return &qrdbtransport.BarrierRootTokenResponse{
				Token:     "root-tok-abc",
				CreatedAt: "2026-08-11T00:00:00Z",
			}, nil
		},
	}
	restore := withFakeBarrier(fb)
	defer restore()

	barrierShares = []string{"share-a", "share-b"}

	cmd := &cobra.Command{Use: "root-token"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runBarrierRootTokenCommand(cmd, nil))

	require.NotNil(t, fb.lastRootTokenReq)
	assert.Equal(t, []string{"share-a", "share-b"}, fb.lastRootTokenReq.Shares)

	got := out.String()
	assert.Contains(t, got, "root-tok-abc")
	assert.Contains(t, got, "ONCE",
		"the operator must be told the token cannot be retrieved again")
}

// TestRunBarrierRootToken_RequiresAQuorum verifies no request is sent without
// shares — the server would reject it, and failing locally is clearer.
func TestRunBarrierRootToken_RequiresAQuorum(t *testing.T) {
	resetBarrierFlags()
	fb := &fakeBarrierClient{}
	restore := withFakeBarrier(fb)
	defer restore()

	err := runBarrierRootTokenCommand(&cobra.Command{Use: "root-token"}, nil)
	require.Error(t, err)
	assert.Nil(t, fb.lastRootTokenReq)
}

// =============================================================================
// enable-encryption
// =============================================================================

// TestRunBarrierEnableEncryption_SendsTheMigration verifies the tenant scope
// and strategy parameters reach the server.
func TestRunBarrierEnableEncryption_SendsTheMigration(t *testing.T) {
	resetBarrierFlags()
	fb := &fakeBarrierClient{
		enableEncryptionFunc: func(_ context.Context, _ *qrdbtransport.BarrierEnableEncryptionRequest) (*qrdbtransport.BarrierEnableEncryptionResponse, error) {
			return &qrdbtransport.BarrierEnableEncryptionResponse{Status: "started", Scope: "tenant:acme"}, nil
		},
	}
	restore := withFakeBarrier(fb)
	defer restore()

	barrierTenantID = "acme"
	barrierStrategy = "shamir"
	barrierThreshold = 2
	barrierTotalShares = 3

	cmd := &cobra.Command{Use: "enable-encryption"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runBarrierEnableEncryptionCommand(cmd, nil))

	req := fb.lastEnableEncReq
	require.NotNil(t, req)
	assert.Equal(t, "acme", req.TenantID)
	assert.Equal(t, "shamir", req.Strategy)
	assert.Equal(t, 2, req.ShamirThreshold)
	assert.Equal(t, 3, req.ShamirTotal)
	assert.Contains(t, out.String(), "started")
}

// TestRunBarrierEnableEncryption_ValidatesLocally verifies combinations the
// server would reject are refused before a migration is attempted against
// live data.
func TestRunBarrierEnableEncryption_ValidatesLocally(t *testing.T) {
	cases := []struct {
		name      string
		strategy  string
		secret    string
		threshold int
		total     int
	}{
		{"no strategy", "", "", 0, 0},
		{"passphrase without passphrase", "passphrase", "", 0, 0},
		{"shamir threshold below 2", "shamir", "", 1, 3},
		{"shamir total below threshold", "shamir", "", 3, 2},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resetBarrierFlags()
			fb := &fakeBarrierClient{}
			restore := withFakeBarrier(fb)
			defer restore()

			barrierStrategy = tc.strategy
			barrierSecret = tc.secret
			barrierThreshold = tc.threshold
			barrierTotalShares = tc.total

			err := runBarrierEnableEncryptionCommand(&cobra.Command{Use: "enable-encryption"}, nil)
			require.Error(t, err)
			assert.Nil(t, fb.lastEnableEncReq,
				"an invalid migration must not reach the server")
		})
	}
}

// The credential fields on these requests are a BORROW of the CLI's protected
// buffer, which is mlocked and guard-paged and is destroyed the moment the
// command returns. A double that kept the slice header would not read zeros,
// it would fault reading an unmapped page, so everything asserted on after the
// call has to be copied out here. go-qrdb#184 calls for exactly this.
func cloneBarrierInitReq(req *qrdbtransport.BarrierInitializeRequest) *qrdbtransport.BarrierInitializeRequest {
	if req == nil {
		return nil
	}
	out := *req
	out.Secret = bytes.Clone(req.Secret)
	return &out
}

func cloneBarrierInitShamirReq(req *qrdbtransport.BarrierInitializeShamirRequest) *qrdbtransport.BarrierInitializeShamirRequest {
	if req == nil {
		return nil
	}
	out := *req
	out.Secret = bytes.Clone(req.Secret)
	return &out
}

func cloneBarrierUnsealReq(req *qrdbtransport.BarrierUnsealRequest) *qrdbtransport.BarrierUnsealRequest {
	if req == nil {
		return nil
	}
	out := *req
	out.Secret = bytes.Clone(req.Secret)
	return &out
}

func cloneBarrierEnableEncReq(req *qrdbtransport.BarrierEnableEncryptionRequest) *qrdbtransport.BarrierEnableEncryptionRequest {
	if req == nil {
		return nil
	}
	out := *req
	out.Passphrase = bytes.Clone(req.Passphrase)
	return &out
}
