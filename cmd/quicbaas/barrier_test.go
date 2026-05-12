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
	unsealShareFunc     func(ctx context.Context, req *qrdbtransport.BarrierUnsealShareRequest) (*qrdbtransport.BarrierUnsealShareResponse, error)
	unsealSharesFunc    func(ctx context.Context, req *qrdbtransport.BarrierUnsealSharesRequest) error
	sealFunc            func(ctx context.Context) error
	rekeyFunc           func(ctx context.Context, req *qrdbtransport.BarrierRekeyRequest) (*qrdbtransport.BarrierRekeyResponse, error)
	lastInitReq         *qrdbtransport.BarrierInitializeRequest
	lastInitShamirReq   *qrdbtransport.BarrierInitializeShamirRequest
	lastUnsealReq       *qrdbtransport.BarrierUnsealRequest
	lastUnsealShareReq  *qrdbtransport.BarrierUnsealShareRequest
	lastUnsealSharesReq *qrdbtransport.BarrierUnsealSharesRequest
	lastRekeyReq        *qrdbtransport.BarrierRekeyRequest
}

func (f *fakeBarrierClient) BarrierStatus(ctx context.Context) (*qrdbtransport.BarrierStatusResponse, error) {
	if f.statusFunc != nil {
		return f.statusFunc(ctx)
	}
	return &qrdbtransport.BarrierStatusResponse{Sealed: true, Strategy: "shamir"}, nil
}

func (f *fakeBarrierClient) BarrierInitialize(ctx context.Context, req *qrdbtransport.BarrierInitializeRequest) error {
	f.lastInitReq = req
	if f.initFunc != nil {
		return f.initFunc(ctx, req)
	}
	return nil
}

func (f *fakeBarrierClient) BarrierInitializeShamir(ctx context.Context, req *qrdbtransport.BarrierInitializeShamirRequest) (*qrdbtransport.BarrierInitializeShamirResponse, error) {
	f.lastInitShamirReq = req
	if f.initShamirFunc != nil {
		return f.initShamirFunc(ctx, req)
	}
	return &qrdbtransport.BarrierInitializeShamirResponse{
		Shares:      []string{"s1", "s2", "s3"},
		Threshold:   req.Threshold,
		TotalShares: req.TotalShares,
	}, nil
}

func (f *fakeBarrierClient) BarrierUnseal(ctx context.Context, req *qrdbtransport.BarrierUnsealRequest) error {
	f.lastUnsealReq = req
	if f.unsealFunc != nil {
		return f.unsealFunc(ctx, req)
	}
	return nil
}

func (f *fakeBarrierClient) BarrierUnsealWithShare(ctx context.Context, req *qrdbtransport.BarrierUnsealShareRequest) (*qrdbtransport.BarrierUnsealShareResponse, error) {
	f.lastUnsealShareReq = req
	if f.unsealShareFunc != nil {
		return f.unsealShareFunc(ctx, req)
	}
	return &qrdbtransport.BarrierUnsealShareResponse{Required: 3, Submitted: 1}, nil
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
		Shares:      []string{"n1", "n2"},
		Threshold:   req.Threshold,
		TotalShares: req.Total,
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
	barrierShare = ""
	barrierSharesFile = ""
	barrierThreshold = 0
	barrierTotalShares = 0
	barrierOutputFmt = "table"
	barrierAssumeYes = false
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

// TestBarrierInit_RequiresSecret rejects empty --secret.
func TestBarrierInit_RequiresSecret(t *testing.T) {
	defer resetBarrierFlags()
	cmd := newBarrierInitCommand()
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--secret is required")
}

// TestBarrierInit_Success forwards the secret to the SDK.
func TestBarrierInit_Success(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierInitCommand()
	cmd.SetArgs([]string{"--secret", "topsecret"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastInitReq)
	assert.Equal(t, "topsecret", fc.lastInitReq.Secret)
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
			wantErr: "--secret is required",
		},
		{
			name:    "missing-threshold",
			args:    []string{"--secret", "x", "--total", "3"},
			wantErr: "--threshold must be > 0",
		},
		{
			name:    "missing-total",
			args:    []string{"--secret", "x", "--threshold", "2"},
			wantErr: "--total must be > 0",
		},
		{
			name:    "threshold-exceeds-total",
			args:    []string{"--secret", "x", "--threshold", "5", "--total", "3"},
			wantErr: "--threshold cannot exceed --total",
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

// TestBarrierInitShamir_Success verifies the SDK dispatch.
func TestBarrierInitShamir_Success(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierInitShamirCommand()
	cmd.SetArgs([]string{"--secret", "x", "--threshold", "2", "--total", "3"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastInitShamirReq)
	assert.Equal(t, "x", fc.lastInitShamirReq.Secret)
	assert.Equal(t, 2, fc.lastInitShamirReq.Threshold)
	assert.Equal(t, 3, fc.lastInitShamirReq.TotalShares)
}

// =============================================================================
// unseal
// =============================================================================

// TestBarrierUnseal_RequiresInput rejects empty input.
func TestBarrierUnseal_RequiresInput(t *testing.T) {
	defer resetBarrierFlags()
	cmd := newBarrierUnsealCommand()
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	var typed *BarrierUnsealNoInputError
	assert.ErrorAs(t, err, &typed, "want typed BarrierUnsealNoInputError, got %T", err)
}

// TestBarrierUnseal_Secret routes secret unseals through BarrierUnseal.
func TestBarrierUnseal_Secret(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierUnsealCommand()
	cmd.SetArgs([]string{"--secret", "pass"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastUnsealReq)
	assert.Equal(t, "pass", fc.lastUnsealReq.Secret)
	assert.Nil(t, fc.lastUnsealShareReq, "should not call BarrierUnsealWithShare")
}

// TestBarrierUnseal_Share routes share unseals through BarrierUnsealWithShare.
func TestBarrierUnseal_Share(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierUnsealCommand()
	cmd.SetArgs([]string{"--share", "abc-share"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastUnsealShareReq)
	assert.Equal(t, "abc-share", fc.lastUnsealShareReq.Share)
}

// =============================================================================
// unseal-shares
// =============================================================================

// TestBarrierUnsealShares_RequiresFile rejects empty --shares-file.
func TestBarrierUnsealShares_RequiresFile(t *testing.T) {
	defer resetBarrierFlags()
	cmd := newBarrierUnsealSharesCommand()
	cmd.SetArgs([]string{})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--shares-file is required")
}

// TestBarrierUnsealShares_ReadsLines reads one share per line, ignoring blanks/comments.
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
	assert.ErrorAs(t, err, &typed)
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
	assert.ErrorAs(t, err, &typed)
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
			args:    []string{"--threshold", "9", "--total", "3"},
			wantErr: "--threshold cannot exceed --total",
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

// TestBarrierRekey_Success forwards the parameters and renders the response.
func TestBarrierRekey_Success(t *testing.T) {
	defer resetBarrierFlags()
	fc := &fakeBarrierClient{}
	defer withFakeBarrier(fc)()

	cmd := newBarrierRekeyCommand()
	cmd.SetArgs([]string{"--threshold", "3", "--total", "5"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastRekeyReq)
	assert.Equal(t, 3, fc.lastRekeyReq.Threshold)
	assert.Equal(t, 5, fc.lastRekeyReq.Total)
	assert.Contains(t, out.String(), "BARRIER REKEY COMPLETE")
}

// =============================================================================
// Errors
// =============================================================================

// TestBarrierUnsealNoInputError_Message verifies the error string.
func TestBarrierUnsealNoInputError_Message(t *testing.T) {
	err := &BarrierUnsealNoInputError{}
	assert.Contains(t, err.Error(), "--secret or --share")
}

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
