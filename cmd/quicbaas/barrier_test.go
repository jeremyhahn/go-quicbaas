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
	statusFunc      func(ctx context.Context) (*qrdbtransport.BarrierStatusResponse, error)
	initFunc        func(ctx context.Context, req *qrdbtransport.BarrierInitializeRequest) error
	initShamirFunc  func(ctx context.Context, req *qrdbtransport.BarrierInitializeShamirRequest) (*qrdbtransport.BarrierInitializeShamirResponse, error)
	unsealFunc      func(ctx context.Context, req *qrdbtransport.BarrierUnsealRequest) error
	sealFunc        func(ctx context.Context) error
	genRecoveryFunc func(ctx context.Context, req *qrdbtransport.BarrierGenerateRecoveryKeysRequest) (*qrdbtransport.BarrierRecoveryKeysResponse, error)

	lastInitReq        *qrdbtransport.BarrierInitializeRequest
	lastInitShamirReq  *qrdbtransport.BarrierInitializeShamirRequest
	lastUnsealReq      *qrdbtransport.BarrierUnsealRequest
	lastGenRecoveryReq *qrdbtransport.BarrierGenerateRecoveryKeysRequest

	quorumBeginFunc   func(ctx context.Context, req *qrdbtransport.QuorumBeginRequest) (*qrdbtransport.QuorumChallenge, error)
	quorumSubmitFunc  func(ctx context.Context, req *qrdbtransport.QuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error)
	rekeyStartFunc    func(ctx context.Context, req *qrdbtransport.RekeyStartRequest) (*qrdbtransport.QuorumRound, error)
	officerRevokeFunc func(ctx context.Context, req *qrdbtransport.OfficerRevokeRequest) (*qrdbtransport.QuorumRound, error)
	lastQuorumBegin   *qrdbtransport.QuorumBeginRequest
	lastQuorumSubmit  *qrdbtransport.QuorumSubmitRequest
	lastRekeyStart    *qrdbtransport.RekeyStartRequest
	lastOfficerRevoke *qrdbtransport.OfficerRevokeRequest

	shareClaimBeginFunc    func(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error)
	shareClaimCompleteFunc func(ctx context.Context, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error)
	lastShareClaimBegin    *qrdbtransport.ShareClaimBeginRequest
	lastShareClaimComplete *qrdbtransport.ShareClaimCompleteRequest
	recoveryClaims         int
	recoveryRegisters      int

	enableEncryptionFunc   func(ctx context.Context, req *qrdbtransport.BarrierEnableEncryptionRequest) (*qrdbtransport.BarrierEnableEncryptionResponse, error)
	lastEnableEncReq       *qrdbtransport.BarrierEnableEncryptionRequest
	officerRegisterFunc    func(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error)
	officerListFunc        func(ctx context.Context) ([]*qrdbtransport.OfficerStatus, error)
	lastOfficerRegisterReq *qrdbtransport.OfficerRegisterRequest
}

// BarrierQuorumBegin records the request and delegates to the injected func.
func (f *fakeBarrierClient) BarrierQuorumBegin(ctx context.Context, req *qrdbtransport.QuorumBeginRequest) (*qrdbtransport.QuorumChallenge, error) {
	f.lastQuorumBegin = req
	if f.quorumBeginFunc != nil {
		return f.quorumBeginFunc(ctx, req)
	}
	return &qrdbtransport.QuorumChallenge{Domain: req.Domain, Op: req.Op}, nil
}

// BarrierQuorumSubmit records the request and delegates to the injected func.
func (f *fakeBarrierClient) BarrierQuorumSubmit(ctx context.Context, req *qrdbtransport.QuorumSubmitRequest) (*qrdbtransport.QuorumOutcome, error) {
	f.lastQuorumSubmit = req
	if f.quorumSubmitFunc != nil {
		return f.quorumSubmitFunc(ctx, req)
	}
	return &qrdbtransport.QuorumOutcome{Domain: req.Domain, Op: req.Op}, nil
}

// BarrierRekeyStart records the request and delegates to the injected func.
func (f *fakeBarrierClient) BarrierRekeyStart(ctx context.Context, req *qrdbtransport.RekeyStartRequest) (*qrdbtransport.QuorumRound, error) {
	f.lastRekeyStart = req
	if f.rekeyStartFunc != nil {
		return f.rekeyStartFunc(ctx, req)
	}
	return &qrdbtransport.QuorumRound{Domain: req.Domain, Op: qrdbtransport.OpRekey}, nil
}

// BarrierOfficerRevoke records the request and delegates to the injected func.
func (f *fakeBarrierClient) BarrierOfficerRevoke(ctx context.Context, req *qrdbtransport.OfficerRevokeRequest) (*qrdbtransport.QuorumRound, error) {
	f.lastOfficerRevoke = req
	if f.officerRevokeFunc != nil {
		return f.officerRevokeFunc(ctx, req)
	}
	return &qrdbtransport.QuorumRound{Domain: req.Domain, Op: qrdbtransport.OpRekey}, nil
}

// RecoveryOfficerRegister counts the recovery-domain call, then shares
// OfficerRegister's recorder so tests assert on the request either way.
func (f *fakeBarrierClient) RecoveryOfficerRegister(ctx context.Context, req *qrdbtransport.OfficerRegisterRequest) (*qrdbtransport.OfficerRegisterResponse, error) {
	f.recoveryRegisters++
	return f.OfficerRegister(ctx, req)
}

// ShareClaimBegin delegates to the injected func.
func (f *fakeBarrierClient) ShareClaimBegin(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	f.lastShareClaimBegin = req
	if f.shareClaimBeginFunc != nil {
		return f.shareClaimBeginFunc(ctx, req)
	}
	return &qrdbtransport.ShareClaimBeginResponse{NonceHex: "deadbeef"}, nil
}

// ShareClaimComplete delegates to the injected func.
func (f *fakeBarrierClient) ShareClaimComplete(ctx context.Context, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	f.lastShareClaimComplete = req
	if f.shareClaimCompleteFunc != nil {
		return f.shareClaimCompleteFunc(ctx, req)
	}
	return &qrdbtransport.ShareClaimCompleteResponse{Envelope: []byte(`{"share":"sealed"}`)}, nil
}

// RecoveryShareClaimBegin counts the recovery-domain call and shares
// ShareClaimBegin's recorder.
func (f *fakeBarrierClient) RecoveryShareClaimBegin(ctx context.Context, req *qrdbtransport.ShareClaimBeginRequest) (*qrdbtransport.ShareClaimBeginResponse, error) {
	f.recoveryClaims++
	return f.ShareClaimBegin(ctx, req)
}

// RecoveryShareClaimComplete counts the recovery-domain call and shares
// ShareClaimComplete's recorder.
func (f *fakeBarrierClient) RecoveryShareClaimComplete(ctx context.Context, req *qrdbtransport.ShareClaimCompleteRequest) (*qrdbtransport.ShareClaimCompleteResponse, error) {
	f.recoveryClaims++
	return f.ShareClaimComplete(ctx, req)
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

func (f *fakeBarrierClient) BarrierSeal(ctx context.Context) error {
	if f.sealFunc != nil {
		return f.sealFunc(ctx)
	}
	return nil
}

func (f *fakeBarrierClient) BarrierGenerateRecoveryKeys(ctx context.Context, req *qrdbtransport.BarrierGenerateRecoveryKeysRequest) (*qrdbtransport.BarrierRecoveryKeysResponse, error) {
	f.lastGenRecoveryReq = req
	if f.genRecoveryFunc != nil {
		return f.genRecoveryFunc(ctx, req)
	}
	return &qrdbtransport.BarrierRecoveryKeysResponse{
		Threshold: req.Threshold,
	}, nil
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
	barrierThreshold = 0
	barrierTotalShares = 0
	barrierOutputFmt = "table"
	barrierAssumeYes = false
	barrierStrategy = ""
	barrierTenantID = ""
	barrierSOPin = ""
	barrierUserPin = ""
	barrierPolicy = ""
	barrierDomain = qrdbtransport.DomainUnseal
	barrierUsername = ""
	barrierQuorumOp = ""
	barrierFactor = ""
	barrierChallengeID = ""
	barrierShareFile = ""
	barrierResponseFile = ""
	barrierResponseMetadata = nil
}

// =============================================================================
// Command tree shape
// =============================================================================

// TestNewBarrierCommand_Shape ensures every canonical verb is wired.
func TestNewBarrierCommand_Shape(t *testing.T) {
	cmd := newBarrierCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "barrier", cmd.Use)

	got := subcommandNames(cmd)
	for _, verb := range canonicalBarrierVerbs {
		assert.True(t, got[verb], "expected 'barrier %s' subcommand", verb)
	}
}

// TestNewBarrierRecoveryCommand_Shape verifies the recovery subgroup.
func TestNewBarrierRecoveryCommand_Shape(t *testing.T) {
	cmd := newBarrierRecoveryCommand()
	require.NotNil(t, cmd)
	expected := map[string]bool{"generate": false}
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
			name:    "missing-threshold",
			args:    []string{"--total", "3"},
			wantErr: "--threshold must be > 0",
		},
		{
			name:    "missing-total",
			args:    []string{"--threshold", "2"},
			wantErr: "--total must be > 0",
		},
		{
			name:    "threshold-exceeds-total",
			args:    []string{"--threshold", "5", "--total", "3"},
			wantErr: "--threshold cannot exceed --total",
		},
		{
			name:    "user-pin-without-so-pin",
			args:    []string{"--threshold", "2", "--total", "3", "--user-pin", "u"},
			wantErr: "--user-pin requires --so-pin",
		},
		{
			name:    "so-pin-without-user-pin",
			args:    []string{"--threshold", "2", "--total", "3", "--so-pin", "s"},
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
		"--threshold", "2",
		"--total", "3",
		"--policy", "designated",
		"--so-pin", "SO",
		"--user-pin", "U",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastInitShamirReq)
	assert.Equal(t, "designated", fc.lastInitShamirReq.Policy)
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
// the unseal command and that its help points a Shamir holder at the quorum
// commands that carry each share from a file.
func TestBarrierUnseal_DoesNotAcceptShareFlag(t *testing.T) {
	cmd := newBarrierUnsealCommand()
	assert.Nil(t, cmd.Flags().Lookup("share"),
		"--share must not be on 'barrier unseal'; holders use 'barrier quorum submit --share-file'")
	assert.Contains(t, cmd.Long, "'barrier quorum begin --op unseal'")
	assert.Contains(t, cmd.Long, "'barrier quorum submit --op unseal'")
	assert.NotContains(t, cmd.Long, "unseal-shares")
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

// TestBarrierSeal_AcceptsSOPin verifies the canonical --so-pin flag stays
// accepted for surface parity with the other barrier verbs. The help steers
// operators to --so-pin-stdin instead (#491); the flag is the last-precedence
// source and is forwarded like the others (#946). If this test is ever
// deleted, the flag is being dropped by accident.
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
			args:    []string{"--threshold", "1"},
			wantErr: "--threshold must be >= 2",
		},
		{
			name:    "total-is-not-a-recovery-flag",
			args:    []string{"--threshold", "2", "--total", "3"},
			wantErr: "unknown flag: --total",
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
		"--policy", "any-enrolled",
		"--so-pin", "SO123",
	})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastGenRecoveryReq)
	assert.Equal(t, 3, fc.lastGenRecoveryReq.Threshold)
	assert.Equal(t, "any-enrolled", fc.lastGenRecoveryReq.Policy)
	assert.Equal(t, "SO123", fc.lastGenRecoveryReq.SOPin)
	assert.Contains(t, out.String(), "RECOVERY KEYS GENERATED")
}

// =============================================================================
// Errors
// =============================================================================

// TestPINsNotAllowedError_Message verifies the error string.
func TestPINsNotAllowedError_Message(t *testing.T) {
	err := &PINsNotAllowedError{Strategy: "none", Op: "init"}
	assert.Contains(t, err.Error(), "none")
	assert.Contains(t, err.Error(), "init")
	assert.Contains(t, err.Error(), "--so-pin")
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
			return &qrdbtransport.BarrierEnableEncryptionResponse{Status: "migration_started", Scope: "tenant"}, nil
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

// TestRunBarrierEnableEncryption_RefusesAWrongScope pins the client half of
// go-quicbaas#990: a server that drops the body tenant_id migrates the SYSTEM
// barrier and says so in `scope`. The verb must not report that as the tenant
// migration the operator asked for.
func TestRunBarrierEnableEncryption_RefusesAWrongScope(t *testing.T) {
	resetBarrierFlags()
	fb := &fakeBarrierClient{
		enableEncryptionFunc: func(_ context.Context, _ *qrdbtransport.BarrierEnableEncryptionRequest) (*qrdbtransport.BarrierEnableEncryptionResponse, error) {
			return &qrdbtransport.BarrierEnableEncryptionResponse{Status: "migration_started", Scope: "system"}, nil
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
	err := runBarrierEnableEncryptionCommand(cmd, nil)

	var mismatch *BarrierScopeMismatchError
	require.ErrorAs(t, err, &mismatch)
	assert.Equal(t, "acme", mismatch.TenantID)
	assert.Equal(t, "system", mismatch.Scope)
	assert.NotContains(t, out.String(), "migration_started",
		"a migration of the wrong barrier must not be rendered as success")
}

// TestRunBarrierEnableEncryption_SystemScopeNeedsNoTenant asserts the scope
// check only applies when --tenant was given.
func TestRunBarrierEnableEncryption_SystemScopeNeedsNoTenant(t *testing.T) {
	resetBarrierFlags()
	fb := &fakeBarrierClient{
		enableEncryptionFunc: func(_ context.Context, _ *qrdbtransport.BarrierEnableEncryptionRequest) (*qrdbtransport.BarrierEnableEncryptionResponse, error) {
			return &qrdbtransport.BarrierEnableEncryptionResponse{Status: "migration_started", Scope: "system"}, nil
		},
	}
	restore := withFakeBarrier(fb)
	defer restore()

	barrierStrategy = "shamir"
	barrierThreshold = 2
	barrierTotalShares = 3

	cmd := &cobra.Command{Use: "enable-encryption"}
	var out bytes.Buffer
	cmd.SetOut(&out)
	require.NoError(t, runBarrierEnableEncryptionCommand(cmd, nil))
	assert.Empty(t, fb.lastEnableEncReq.TenantID)
	assert.Contains(t, out.String(), "system")
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

// The enable-encryption help must send each operator to the status command
// that can observe THEIR migration: `barrier status` reads the system barrier
// only, so a tenant migration is watched with `iam tenant barrier status`.
// Both commands must exist where the help says they are.
func TestBarrierEnableEncryptionHelp_NamesTheStatusCommandPerScope(t *testing.T) {
	help := newBarrierEnableEncryptionCommand().Long

	for _, path := range [][]string{
		{"barrier", "status"},
		{"iam", "tenant", "barrier", "status"},
	} {
		invocation := "quicbaas " + strings.Join(path, " ")
		assert.Contains(t, help, invocation, "the help does not name %q", invocation)

		cmd, _, err := rootCmd.Find(path)
		require.NoError(t, err, "%q does not resolve", invocation)
		assert.Equal(t, path[len(path)-1], cmd.Name(), "%q resolved to %q", invocation, cmd.CommandPath())
	}
	assert.Contains(t, help, "quicbaas iam tenant barrier status --tenant",
		"a tenant migration's status command must name the tenant")
}
