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

// Package main: command-handler coverage for dek.go.
package main

import (
	"bytes"
	"context"
	stderrors "errors"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
)

// =============================================================================
// Mock
// =============================================================================

// mockSDKDEKClient implements quicbaas.DEKClient. Unset hooks return benign
// zero-value responses so each test wires only the call it exercises.
type mockSDKDEKClient struct {
	createReq    *dekclient.CreateDEKRequest
	getPath      string
	listReq      *dekclient.ListDEKsRequest
	rotateReq    *dekclient.RotateDEKRequest
	deleteReq    *dekclient.DeleteDEKRequest
	statusPath   string
	defaultPath  string
	safetyReq    *dekclient.SetDEKSafetyRequest
	clearPath    string
	reencryptReq *dekclient.StartReencryptionRequest
	progressReq  *dekclient.GetReencryptionProgressRequest

	err error
}

func (m *mockSDKDEKClient) CreateDEK(_ context.Context, req *dekclient.CreateDEKRequest) (*dekclient.CreateDEKResponse, error) {
	m.createReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &dekclient.CreateDEKResponse{DEK: &dekclient.DEKInfo{Path: req.Path, Algorithm: req.Algorithm, Version: 1}}, nil
}

func (m *mockSDKDEKClient) GetDEK(_ context.Context, path string) (*dekclient.DEKInfo, error) {
	m.getPath = path
	if m.err != nil {
		return nil, m.err
	}
	return &dekclient.DEKInfo{Path: path, Version: 3, Status: "active"}, nil
}

func (m *mockSDKDEKClient) ListDEKs(_ context.Context, req *dekclient.ListDEKsRequest) (*dekclient.ListDEKsResponse, error) {
	m.listReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &dekclient.ListDEKsResponse{
		DEKs:       []dekclient.DEKInfo{{Path: "/tenants/acme", Version: 1, Status: "active"}},
		TotalCount: 1,
	}, nil
}

func (m *mockSDKDEKClient) RotateDEK(_ context.Context, req *dekclient.RotateDEKRequest) (*dekclient.RotateDEKResponse, error) {
	m.rotateReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &dekclient.RotateDEKResponse{Path: req.Path, OldVersion: 1, NewVersion: 2}, nil
}

func (m *mockSDKDEKClient) DeleteDEK(_ context.Context, req *dekclient.DeleteDEKRequest) (*dekclient.DeleteDEKResponse, error) {
	m.deleteReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &dekclient.DeleteDEKResponse{Path: req.Path, Deleted: true}, nil
}

func (m *mockSDKDEKClient) GetDEKStatus(_ context.Context, path string) (*dekclient.DEKStatusResponse, error) {
	m.statusPath = path
	if m.err != nil {
		return nil, m.err
	}
	return &dekclient.DEKStatusResponse{
		DEK:                  &dekclient.DEKInfo{Path: path, Version: 2},
		ReencryptionProgress: &dekclient.ReencryptionProgress{InProgress: true, TotalKeys: 10, ProcessedKeys: 4, PercentComplete: 40},
	}, nil
}

func (m *mockSDKDEKClient) SetDefaultDEK(_ context.Context, path string) (*dekclient.SetDefaultDEKResponse, error) {
	m.defaultPath = path
	if m.err != nil {
		return nil, m.err
	}
	return &dekclient.SetDefaultDEKResponse{Path: path, PreviousDefault: "/tenants/old"}, nil
}

func (m *mockSDKDEKClient) SetDEKSafety(_ context.Context, req *dekclient.SetDEKSafetyRequest) error {
	m.safetyReq = req
	return m.err
}

func (m *mockSDKDEKClient) ClearDEKSafety(_ context.Context, path string) error {
	m.clearPath = path
	return m.err
}

func (m *mockSDKDEKClient) StartReencryption(_ context.Context, req *dekclient.StartReencryptionRequest) (*dekclient.StartReencryptionResponse, error) {
	m.reencryptReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &dekclient.StartReencryptionResponse{OperationID: "op_1", Path: req.Path, Started: true, EstimatedItems: 7}, nil
}

func (m *mockSDKDEKClient) GetReencryptionProgress(_ context.Context, req *dekclient.GetReencryptionProgressRequest) (*dekclient.ReencryptionProgressResponse, error) {
	m.progressReq = req
	if m.err != nil {
		return nil, m.err
	}
	return &dekclient.ReencryptionProgressResponse{
		OperationID: "op_1",
		Path:        req.Path,
		Status:      "running",
		Progress:    &dekclient.ReencryptionProgress{InProgress: true, TotalKeys: 10, ProcessedKeys: 5, PercentComplete: 50},
	}, nil
}

// =============================================================================
// Helpers
// =============================================================================

// withMockDEK installs an sdkDEKClientFactory returning mc and restores the
// previous factory, timeout, and flag state on cleanup.
func withMockDEK(t *testing.T, mc *mockSDKDEKClient) {
	t.Helper()
	origFactory := sdkDEKClientFactory
	origTimeout := globalTimeout
	origOutput := dekOutputFmt

	sdkDEKClientFactory = func() (quicbaas.DEKClient, error) { return mc, nil }
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}
	dekOutputFmt = "table"

	t.Cleanup(func() {
		sdkDEKClientFactory = origFactory
		globalTimeout = origTimeout
		dekOutputFmt = origOutput
		resetDEKFlags()
	})
}

// withFailingDEKFactory installs a factory that always fails.
func withFailingDEKFactory(t *testing.T, err error) {
	t.Helper()
	orig := sdkDEKClientFactory
	sdkDEKClientFactory = func() (quicbaas.DEKClient, error) { return nil, err }
	t.Cleanup(func() { sdkDEKClientFactory = orig })
}

// resetDEKFlags clears every package-level dek flag.
func resetDEKFlags() {
	dekAlgorithm = ""
	dekDescription = ""
	dekMaxOperations = 0
	dekMaxBytes = 0
	dekWarningPercent = 0
	dekListPrefix = ""
	dekListRecursive = false
	dekListLimit = 0
	dekRotateBackground = false
	dekDeleteForce = false
	dekDeleteMigrateTo = ""
	dekFromVersion = 0
	dekToVersion = 0
	dekBatchSize = 0
	dekReencryptForce = false
	dekOperationID = ""
}

// captureCmd returns a cobra command whose stdout is captured in buf.
func captureCmd(buf *bytes.Buffer) *cobra.Command {
	cmd := &cobra.Command{}
	cmd.SetOut(buf)
	return cmd
}

// =============================================================================
// Command construction
// =============================================================================

func TestNewDEKCommand_VerbTree(t *testing.T) {
	cmd := newDEKCommand()
	require.Equal(t, "dek", cmd.Use)

	want := []string{
		"create", "get", "list", "rotate", "delete", "status",
		"set-default", "set-safety", "clear-safety", "reencrypt", "reencrypt-progress",
	}
	got := make(map[string]bool, len(cmd.Commands()))
	for _, c := range cmd.Commands() {
		got[c.Name()] = true
	}
	for _, verb := range want {
		assert.True(t, got[verb], "dek is missing the %q verb", verb)
	}
	assert.Len(t, cmd.Commands(), len(want),
		"the dek verb set must mirror the SDK DEK client one-for-one")

	require.NotNil(t, cmd.PersistentFlags().Lookup("output"))
}

func TestNewDEKCommand_PathVerbsRequireExactlyOneArg(t *testing.T) {
	cmd := newDEKCommand()
	for _, c := range cmd.Commands() {
		switch c.Name() {
		case "list":
			require.NoError(t, c.Args(c, nil), "list must take no positional args")
			assert.Error(t, c.Args(c, []string{"extra"}))
		default:
			assert.Error(t, c.Args(c, nil), "%s must require a path", c.Name())
			assert.NoError(t, c.Args(c, []string{"/tenants/acme"}), c.Name())
		}
	}
}

// =============================================================================
// Argument / flag validation
// =============================================================================

func TestDEKPathArg(t *testing.T) {
	path, err := dekPathArg("get", []string{"  /tenants/acme  "})
	require.NoError(t, err)
	assert.Equal(t, "/tenants/acme", path)
}

func TestDEKPathArg_Invalid(t *testing.T) {
	for name, args := range map[string][]string{
		"no args":     nil,
		"blank arg":   {"   "},
		"empty arg":   {""},
		"only spaces": {"\t"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := dekPathArg("get", args)
			require.Error(t, err)
			var typed *DEKArgRequiredError
			require.True(t, stderrors.As(err, &typed))
			assert.Contains(t, err.Error(), "<path>")
		})
	}
}

func TestRunDEKCreate_RejectsOutOfRangeWarningPercent(t *testing.T) {
	withMockDEK(t, &mockSDKDEKClient{})
	dekWarningPercent = 101

	err := runDEKCreate(captureCmd(&bytes.Buffer{}), []string{"/tenants/acme"})
	require.Error(t, err)
	var typed *DEKFlagInvalidError
	require.True(t, stderrors.As(err, &typed))
	assert.Equal(t, "warning-percent", typed.Flag)
}

func TestRunDEKList_RejectsNegativeLimit(t *testing.T) {
	withMockDEK(t, &mockSDKDEKClient{})
	dekListLimit = -1

	err := runDEKList(captureCmd(&bytes.Buffer{}), nil)
	require.Error(t, err)
	var typed *DEKFlagInvalidError
	require.True(t, stderrors.As(err, &typed))
	assert.Equal(t, "limit", typed.Flag)
}

func TestRunDEKSetSafety_RequiresALimit(t *testing.T) {
	withMockDEK(t, &mockSDKDEKClient{})

	err := runDEKSetSafety(captureCmd(&bytes.Buffer{}), []string{"/tenants/acme"})
	require.Error(t, err)
	var typed *DEKFlagInvalidError
	require.True(t, stderrors.As(err, &typed))
	assert.Contains(t, err.Error(), "at least one limit")
}

func TestRunDEKReencrypt_VersionValidation(t *testing.T) {
	tests := []struct {
		name     string
		from, to int
		batch    int
		wantFlag string
	}{
		{"missing from", 0, 2, 0, "from-version"},
		{"missing to", 1, 0, 0, "to-version"},
		{"identical versions", 2, 2, 0, "to-version"},
		{"negative batch", 1, 2, -5, "batch-size"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			withMockDEK(t, &mockSDKDEKClient{})
			dekFromVersion, dekToVersion, dekBatchSize = tt.from, tt.to, tt.batch

			err := runDEKReencrypt(captureCmd(&bytes.Buffer{}), []string{"/tenants/acme"})
			require.Error(t, err)
			var typed *DEKFlagInvalidError
			require.True(t, stderrors.As(err, &typed))
			assert.Equal(t, tt.wantFlag, typed.Flag)
		})
	}
}

// =============================================================================
// Success paths
// =============================================================================

func TestRunDEKCreate_SendsEveryFlag(t *testing.T) {
	mc := &mockSDKDEKClient{}
	withMockDEK(t, mc)
	dekAlgorithm = "AES-256-GCM"
	dekDescription = "customer db"
	dekMaxOperations = 4096
	dekMaxBytes = 8192
	dekWarningPercent = 75

	var buf bytes.Buffer
	require.NoError(t, runDEKCreate(captureCmd(&buf), []string{"/tenants/acme"}))

	require.NotNil(t, mc.createReq)
	assert.Equal(t, "/tenants/acme", mc.createReq.Path)
	assert.Equal(t, "AES-256-GCM", mc.createReq.Algorithm)
	assert.Equal(t, "customer db", mc.createReq.Description)
	assert.Equal(t, uint64(4096), mc.createReq.MaxOperations)
	assert.Equal(t, uint64(8192), mc.createReq.MaxBytes)
	assert.Equal(t, 75, mc.createReq.WarningPercent)
	assert.Contains(t, buf.String(), "/tenants/acme")
}

func TestRunDEKGet_And_Status(t *testing.T) {
	mc := &mockSDKDEKClient{}
	withMockDEK(t, mc)

	var buf bytes.Buffer
	require.NoError(t, runDEKGet(captureCmd(&buf), []string{"/tenants/acme"}))
	assert.Equal(t, "/tenants/acme", mc.getPath)
	assert.Contains(t, buf.String(), "Version:")

	buf.Reset()
	require.NoError(t, runDEKStatus(captureCmd(&buf), []string{"/tenants/acme"}))
	assert.Equal(t, "/tenants/acme", mc.statusPath)
	assert.Contains(t, buf.String(), "Percent Complete:")
}

func TestRunDEKList_PassesFilters(t *testing.T) {
	mc := &mockSDKDEKClient{}
	withMockDEK(t, mc)
	dekListPrefix = "/tenants/"
	dekListRecursive = true
	dekListLimit = 25

	var buf bytes.Buffer
	require.NoError(t, runDEKList(captureCmd(&buf), nil))

	require.NotNil(t, mc.listReq)
	assert.Equal(t, "/tenants/", mc.listReq.Prefix)
	assert.True(t, mc.listReq.Recursive)
	assert.Equal(t, 25, mc.listReq.Limit)
	assert.Contains(t, buf.String(), "showing 1 of 1 total")
}

func TestRunDEKRotateDeleteDefaultSafety(t *testing.T) {
	mc := &mockSDKDEKClient{}
	withMockDEK(t, mc)

	var buf bytes.Buffer
	dekRotateBackground = true
	require.NoError(t, runDEKRotate(captureCmd(&buf), []string{"/tenants/acme"}))
	require.NotNil(t, mc.rotateReq)
	assert.True(t, mc.rotateReq.Background)
	assert.Contains(t, buf.String(), "v1 -> v2")

	buf.Reset()
	dekDeleteForce = true
	dekDeleteMigrateTo = "/tenants/acme-v2"
	require.NoError(t, runDEKDelete(captureCmd(&buf), []string{"/tenants/acme"}))
	require.NotNil(t, mc.deleteReq)
	assert.True(t, mc.deleteReq.Force)
	assert.Equal(t, "/tenants/acme-v2", mc.deleteReq.MigrateTo)
	assert.Contains(t, buf.String(), "deleted")

	buf.Reset()
	require.NoError(t, runDEKSetDefault(captureCmd(&buf), []string{"/tenants/acme"}))
	assert.Equal(t, "/tenants/acme", mc.defaultPath)
	assert.Contains(t, buf.String(), "was /tenants/old")

	buf.Reset()
	dekMaxBytes = 1024
	require.NoError(t, runDEKSetSafety(captureCmd(&buf), []string{"/tenants/acme"}))
	require.NotNil(t, mc.safetyReq)
	assert.Equal(t, uint64(1024), mc.safetyReq.MaxBytes)

	buf.Reset()
	require.NoError(t, runDEKClearSafety(captureCmd(&buf), []string{"/tenants/acme"}))
	assert.Equal(t, "/tenants/acme", mc.clearPath)
}

func TestRunDEKReencrypt_AndProgress(t *testing.T) {
	mc := &mockSDKDEKClient{}
	withMockDEK(t, mc)
	dekFromVersion, dekToVersion, dekBatchSize, dekReencryptForce = 1, 2, 100, true

	var buf bytes.Buffer
	require.NoError(t, runDEKReencrypt(captureCmd(&buf), []string{"/tenants/acme"}))
	require.NotNil(t, mc.reencryptReq)
	assert.Equal(t, 1, mc.reencryptReq.FromVersion)
	assert.Equal(t, 2, mc.reencryptReq.ToVersion)
	assert.Equal(t, 100, mc.reencryptReq.BatchSize)
	assert.True(t, mc.reencryptReq.Force)
	assert.Contains(t, buf.String(), "op_1")

	buf.Reset()
	dekOperationID = "op_1"
	require.NoError(t, runDEKReencryptProgress(captureCmd(&buf), []string{"/tenants/acme"}))
	require.NotNil(t, mc.progressReq)
	assert.Equal(t, "op_1", mc.progressReq.OperationID)
	assert.Contains(t, buf.String(), "running")
}

// =============================================================================
// Error propagation
// =============================================================================

func TestRunDEK_FactoryErrorPropagates(t *testing.T) {
	sentinel := stderrors.New("no transport")
	withFailingDEKFactory(t, sentinel)
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}

	handlers := map[string]func(*cobra.Command, []string) error{
		"get":                runDEKGet,
		"status":             runDEKStatus,
		"set-default":        runDEKSetDefault,
		"clear-safety":       runDEKClearSafety,
		"rotate":             runDEKRotate,
		"delete":             runDEKDelete,
		"reencrypt-progress": runDEKReencryptProgress,
	}
	for name, run := range handlers {
		t.Run(name, func(t *testing.T) {
			err := run(captureCmd(&bytes.Buffer{}), []string{"/tenants/acme"})
			require.ErrorIs(t, err, sentinel)
		})
	}
}

func TestRunDEK_ServerErrorPropagates(t *testing.T) {
	sentinel := stderrors.New("dek boom")
	withMockDEK(t, &mockSDKDEKClient{err: sentinel})

	require.ErrorIs(t, runDEKGet(captureCmd(&bytes.Buffer{}), []string{"/tenants/acme"}), sentinel)
	require.ErrorIs(t, runDEKList(captureCmd(&bytes.Buffer{}), nil), sentinel)
	require.ErrorIs(t, runDEKClearSafety(captureCmd(&bytes.Buffer{}), []string{"/tenants/acme"}), sentinel)
}

// =============================================================================
// Formatting
// =============================================================================

func TestFormatDEKDetail_Formats(t *testing.T) {
	info := &dekclient.DEKInfo{
		Path:      "/tenants/acme",
		Algorithm: "AES-256-GCM",
		Version:   2,
		Status:    "active",
		IsDefault: true,
		CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}

	for _, format := range []string{"table", "json", "yaml", ""} {
		t.Run("format="+format, func(t *testing.T) {
			var buf bytes.Buffer
			require.NoError(t, formatDEKDetail(info, format, &buf))
			assert.Contains(t, buf.String(), "/tenants/acme")
		})
	}
}

func TestFormatDEKDetail_NilAndBadFormat(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, formatDEKDetail(nil, "table", &buf))
	assert.Contains(t, buf.String(), "(no DEK)")

	buf.Reset()
	require.Error(t, formatDEKDetail(&dekclient.DEKInfo{}, "toml", &buf))
}

func TestFormatDEKList_Empty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, formatDEKList(&dekclient.ListDEKsResponse{}, "table", &buf))
	assert.Contains(t, buf.String(), "No DEKs found")

	buf.Reset()
	require.NoError(t, formatDEKList(nil, "table", &buf))
	assert.Contains(t, buf.String(), "No DEKs found")
}

func TestFormatDEKReencryptionProgress_NilAndBadFormat(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, formatDEKReencryptionProgress(nil, "table", &buf))
	assert.Contains(t, buf.String(), "(no progress)")

	buf.Reset()
	require.Error(t, formatDEKReencryptionProgress(&dekclient.ReencryptionProgressResponse{}, "toml", &buf))
}

func TestFormatDEKStatus_NilAndBadFormat(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, formatDEKStatus(nil, "table", &buf))
	assert.Contains(t, buf.String(), "(no status)")

	buf.Reset()
	require.Error(t, formatDEKStatus(&dekclient.DEKStatusResponse{}, "toml", &buf))
}
