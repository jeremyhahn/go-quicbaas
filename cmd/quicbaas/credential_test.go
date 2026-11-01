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

// Package main provides flag-parsing and routing tests for the canonical
// 'quicbaas credential' command tree.
package main

import (
	"bytes"
	"context"
	stderrors "errors"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
)

// =============================================================================
// Mock CredentialClient
// =============================================================================

// fakeCredentialClient mirrors the fakeBarrierClient pattern: each Func
// field is optional; nil fields return canned zero-valued responses, and
// the last-used request is recorded for inspection. Mirrors every method
// on qrdbtransport.CredentialClient so the same fake can drive the
// production SDK surface.
type fakeCredentialClient struct {
	submitFunc          func(ctx context.Context, req *qrdbtransport.CredentialSubmitRequest) error
	strategyFunc        func(ctx context.Context) (*qrdbtransport.CredentialStrategyResponse, error)
	deleteFunc          func(ctx context.Context, name string) error
	listFunc            func(ctx context.Context, req *qrdbtransport.CredentialListRequest) (*qrdbtransport.CredentialListResponse, error)
	rotateFunc          func(ctx context.Context, req *qrdbtransport.CredentialRotateRequest) (*qrdbtransport.CredentialRotateResponse, error)
	retrieveVersionFunc func(ctx context.Context, req *qrdbtransport.CredentialRetrieveVersionRequest) (*qrdbtransport.CredentialRetrieveVersionResponse, error)
	listVersionsFunc    func(ctx context.Context, req *qrdbtransport.CredentialListVersionsRequest) (*qrdbtransport.CredentialListVersionsResponse, error)
	gcExpiredFunc       func(ctx context.Context, req *qrdbtransport.CredentialGCRequest) (*qrdbtransport.CredentialGCResponse, error)
	storeFunc           func(ctx context.Context, name string, value []byte) error
	retrieveFunc        func(ctx context.Context, name string) ([]byte, error)

	lastSubmitReq    *qrdbtransport.CredentialSubmitRequest
	lastListReq      *qrdbtransport.CredentialListRequest
	lastRotateReq    *qrdbtransport.CredentialRotateRequest
	lastRetrieveReq  *qrdbtransport.CredentialRetrieveVersionRequest
	lastVersionsReq  *qrdbtransport.CredentialListVersionsRequest
	lastGCReq        *qrdbtransport.CredentialGCRequest
	lastDeleteName   string
	lastStoreName    string
	lastStoreValue   []byte
	lastRetrieveName string
}

func (f *fakeCredentialClient) CredentialSubmit(ctx context.Context, req *qrdbtransport.CredentialSubmitRequest) error {
	f.lastSubmitReq = req
	if f.submitFunc != nil {
		return f.submitFunc(ctx, req)
	}
	return nil
}

func (f *fakeCredentialClient) CredentialStrategy(ctx context.Context) (*qrdbtransport.CredentialStrategyResponse, error) {
	if f.strategyFunc != nil {
		return f.strategyFunc(ctx)
	}
	return &qrdbtransport.CredentialStrategyResponse{Strategy: "manual"}, nil
}

func (f *fakeCredentialClient) CredentialDelete(ctx context.Context, name string) error {
	f.lastDeleteName = name
	if f.deleteFunc != nil {
		return f.deleteFunc(ctx, name)
	}
	return nil
}

func (f *fakeCredentialClient) CredentialList(ctx context.Context, req *qrdbtransport.CredentialListRequest) (*qrdbtransport.CredentialListResponse, error) {
	f.lastListReq = req
	if f.listFunc != nil {
		return f.listFunc(ctx, req)
	}
	return &qrdbtransport.CredentialListResponse{Names: nil}, nil
}

func (f *fakeCredentialClient) CredentialRotate(ctx context.Context, req *qrdbtransport.CredentialRotateRequest) (*qrdbtransport.CredentialRotateResponse, error) {
	f.lastRotateReq = req
	if f.rotateFunc != nil {
		return f.rotateFunc(ctx, req)
	}
	return &qrdbtransport.CredentialRotateResponse{
		Name:            req.Name,
		PreviousVersion: 1,
		NewVersion:      2,
	}, nil
}

func (f *fakeCredentialClient) CredentialRetrieveVersion(ctx context.Context, req *qrdbtransport.CredentialRetrieveVersionRequest) (*qrdbtransport.CredentialRetrieveVersionResponse, error) {
	f.lastRetrieveReq = req
	if f.retrieveVersionFunc != nil {
		return f.retrieveVersionFunc(ctx, req)
	}
	return &qrdbtransport.CredentialRetrieveVersionResponse{Value: []byte("secret")}, nil
}

func (f *fakeCredentialClient) CredentialListVersions(ctx context.Context, req *qrdbtransport.CredentialListVersionsRequest) (*qrdbtransport.CredentialListVersionsResponse, error) {
	f.lastVersionsReq = req
	if f.listVersionsFunc != nil {
		return f.listVersionsFunc(ctx, req)
	}
	return &qrdbtransport.CredentialListVersionsResponse{Name: req.Name}, nil
}

func (f *fakeCredentialClient) CredentialGCExpired(ctx context.Context, req *qrdbtransport.CredentialGCRequest) (*qrdbtransport.CredentialGCResponse, error) {
	f.lastGCReq = req
	if f.gcExpiredFunc != nil {
		return f.gcExpiredFunc(ctx, req)
	}
	return &qrdbtransport.CredentialGCResponse{Reaped: 0}, nil
}

func (f *fakeCredentialClient) CredentialStore(ctx context.Context, name string, value []byte) error {
	f.lastStoreName = name
	f.lastStoreValue = value
	if f.storeFunc != nil {
		return f.storeFunc(ctx, name, value)
	}
	return nil
}

func (f *fakeCredentialClient) CredentialRetrieve(ctx context.Context, name string) ([]byte, error) {
	f.lastRetrieveName = name
	if f.retrieveFunc != nil {
		return f.retrieveFunc(ctx, name)
	}
	return []byte("secret"), nil
}

// withFakeCredentialFactory installs fc as the active credential factory
// and returns a restore func. Ensures the package-level command timeout
// is set so context.WithTimeout does not panic.
func withFakeCredentialFactory(fc *fakeCredentialClient) func() {
	orig := credentialClientFactory
	credentialClientFactory = func() (qrdbtransport.CredentialClient, error) { return fc, nil }
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}
	return func() { credentialClientFactory = orig }
}

// withFailingCredentialFactory installs a factory that returns the given
// error. Used to test factory-error propagation.
func withFailingCredentialFactory(err error) func() {
	orig := credentialClientFactory
	credentialClientFactory = func() (qrdbtransport.CredentialClient, error) { return nil, err }
	if globalTimeout == 0 {
		globalTimeout = 5 * time.Second
	}
	return func() { credentialClientFactory = orig }
}

// resetCredentialFlags zeroes the package-level flag variables so a stale
// value from a prior test does not leak into the next.
func resetCredentialFlags() {
	credentialValue = ""
	credentialGracePeriod = 0
	credentialVersion = 0
	credentialOutputFmt = "table"
}

// failingCredentialClient returns a fake whose every verb fails with err.
func failingCredentialClient(err error) *fakeCredentialClient {
	return &fakeCredentialClient{
		submitFunc: func(context.Context, *qrdbtransport.CredentialSubmitRequest) error { return err },
		deleteFunc: func(context.Context, string) error { return err },
		listFunc: func(context.Context, *qrdbtransport.CredentialListRequest) (*qrdbtransport.CredentialListResponse, error) {
			return nil, err
		},
		rotateFunc: func(context.Context, *qrdbtransport.CredentialRotateRequest) (*qrdbtransport.CredentialRotateResponse, error) {
			return nil, err
		},
		retrieveVersionFunc: func(context.Context, *qrdbtransport.CredentialRetrieveVersionRequest) (*qrdbtransport.CredentialRetrieveVersionResponse, error) {
			return nil, err
		},
		listVersionsFunc: func(context.Context, *qrdbtransport.CredentialListVersionsRequest) (*qrdbtransport.CredentialListVersionsResponse, error) {
			return nil, err
		},
		gcExpiredFunc: func(context.Context, *qrdbtransport.CredentialGCRequest) (*qrdbtransport.CredentialGCResponse, error) {
			return nil, err
		},
	}
}

// executeCredentialCmd runs cmd with args, discarding its output.
func executeCredentialCmd(cmd *cobra.Command, args []string) error {
	cmd.SetArgs(args)
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	return cmd.Execute()
}

// =============================================================================
// Error paths shared by every verb
// =============================================================================

// TestCredentialVerbs_ClientErrors verifies every verb surfaces both a
// client-factory error and a transport error unchanged.
func TestCredentialVerbs_ClientErrors(t *testing.T) {
	verbs := []struct {
		verb   string
		newCmd func() *cobra.Command
		args   []string
	}{
		{"submit", newCredentialSubmitCmd, []string{"my-key", "--value", "v"}},
		{"list", newCredentialListCmd, nil},
		{"rotate", newCredentialRotateCmd, []string{"my-key", "--value", "v"}},
		{"retrieve", newCredentialRetrieveCmd, []string{"my-key"}},
		{"versions", newCredentialVersionsCmd, []string{"my-key"}},
		{"delete", newCredentialDeleteCmd, []string{"my-key"}},
		{"gc", newCredentialGCCmd, []string{"my-key"}},
	}
	factoryErr := stderrors.New("factory boom")
	transportErr := stderrors.New("transport failed")
	failures := []struct {
		name    string
		install func() func()
		wantErr error
	}{
		{"factory", func() func() { return withFailingCredentialFactory(factoryErr) }, factoryErr},
		{"transport", func() func() { return withFakeCredentialFactory(failingCredentialClient(transportErr)) }, transportErr},
	}
	for _, v := range verbs {
		for _, f := range failures {
			t.Run(v.verb+"/"+f.name, func(t *testing.T) {
				defer resetCredentialFlags()
				defer f.install()()

				err := executeCredentialCmd(v.newCmd(), v.args)
				assert.ErrorIs(t, err, f.wantErr)
			})
		}
	}
}

// TestCredentialVerbs_MissingName verifies every named verb rejects a
// whitespace-only name before it reaches the client.
func TestCredentialVerbs_MissingName(t *testing.T) {
	tests := []struct {
		verb   string
		newCmd func() *cobra.Command
		args   []string
	}{
		{"submit", newCredentialSubmitCmd, []string{"  ", "--value", "x"}},
		{"rotate", newCredentialRotateCmd, []string{"   ", "--value", "v"}},
		{"retrieve", newCredentialRetrieveCmd, []string{"  "}},
		{"versions", newCredentialVersionsCmd, []string{" "}},
		{"delete", newCredentialDeleteCmd, []string{"  "}},
		{"gc", newCredentialGCCmd, []string{"  "}},
	}
	for _, tt := range tests {
		t.Run(tt.verb, func(t *testing.T) {
			defer resetCredentialFlags()

			err := executeCredentialCmd(tt.newCmd(), tt.args)
			var typed *CredentialNameRequiredError
			require.ErrorAs(t, err, &typed)
			assert.Equal(t, tt.verb, typed.Verb)
			assert.Contains(t, err.Error(), tt.verb)
			assert.Contains(t, err.Error(), "<name>")
		})
	}
}

// TestCredentialVerbs_MissingValue verifies the writing verbs reject an
// empty --value before they reach the client.
func TestCredentialVerbs_MissingValue(t *testing.T) {
	tests := []struct {
		verb   string
		newCmd func() *cobra.Command
	}{
		{"submit", newCredentialSubmitCmd},
		{"rotate", newCredentialRotateCmd},
	}
	for _, tt := range tests {
		t.Run(tt.verb, func(t *testing.T) {
			defer resetCredentialFlags()

			err := executeCredentialCmd(tt.newCmd(), []string{"my-key"})
			var typed *CredentialValueRequiredError
			require.ErrorAs(t, err, &typed)
			assert.Equal(t, tt.verb, typed.Verb)
			assert.Contains(t, err.Error(), tt.verb)
			assert.Contains(t, err.Error(), "--value")
		})
	}
}

// =============================================================================
// submit
// =============================================================================

// TestCredentialSubmit_Success verifies the happy path and request shape.
func TestCredentialSubmit_Success(t *testing.T) {
	defer resetCredentialFlags()
	fc := &fakeCredentialClient{}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialSubmitCmd()
	cmd.SetArgs([]string{"my-key", "--value", "topsecret"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastSubmitReq)
	assert.Equal(t, "my-key", fc.lastSubmitReq.Name)
	assert.Equal(t, []byte("topsecret"), fc.lastSubmitReq.Value)
	assert.Contains(t, out.String(), `credential "my-key" submitted`)
}

// =============================================================================
// list
// =============================================================================

// TestCredentialList_Success verifies enumeration of names (table format).
func TestCredentialList_Success(t *testing.T) {
	defer resetCredentialFlags()
	fc := &fakeCredentialClient{
		listFunc: func(_ context.Context, _ *qrdbtransport.CredentialListRequest) (*qrdbtransport.CredentialListResponse, error) {
			return &qrdbtransport.CredentialListResponse{Names: []string{"alpha", "beta"}}, nil
		},
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialListCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastListReq)
	assert.Equal(t, "alpha\nbeta\n", out.String())
}

// TestCredentialList_EmptyTable shows the friendly empty marker.
func TestCredentialList_EmptyTable(t *testing.T) {
	defer resetCredentialFlags()
	fc := &fakeCredentialClient{}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialListCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Equal(t, "(no credentials)\n", out.String())
}

// TestCredentialList_JSON outputs a JSON envelope.
func TestCredentialList_JSON(t *testing.T) {
	defer resetCredentialFlags()
	fc := &fakeCredentialClient{
		listFunc: func(_ context.Context, _ *qrdbtransport.CredentialListRequest) (*qrdbtransport.CredentialListResponse, error) {
			return &qrdbtransport.CredentialListResponse{Names: []string{"alpha"}}, nil
		},
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialListCmd()
	cmd.SetArgs([]string{"--output", "json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	got := out.String()
	assert.Contains(t, got, `"names"`)
	assert.Contains(t, got, `"alpha"`)
}

// =============================================================================
// rotate
// =============================================================================

// TestCredentialRotate_Success verifies the happy path and grace forwarding.
func TestCredentialRotate_Success(t *testing.T) {
	defer resetCredentialFlags()
	fc := &fakeCredentialClient{
		rotateFunc: func(_ context.Context, req *qrdbtransport.CredentialRotateRequest) (*qrdbtransport.CredentialRotateResponse, error) {
			return &qrdbtransport.CredentialRotateResponse{
				Name:            req.Name,
				PreviousVersion: 3,
				NewVersion:      4,
				GraceUntilUnix:  time.Now().Add(24 * time.Hour).Unix(),
			}, nil
		},
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialRotateCmd()
	cmd.SetArgs([]string{"my-key", "--value", "newSecret", "--grace", "24h"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastRotateReq)
	assert.Equal(t, "my-key", fc.lastRotateReq.Name)
	assert.Equal(t, []byte("newSecret"), fc.lastRotateReq.NewValue)
	assert.Equal(t, int64(24*60*60), fc.lastRotateReq.GracePeriodSeconds)
	assert.Contains(t, out.String(), "rotated my-key")
	assert.Contains(t, out.String(), "previous v3 -> new v4")
	assert.Contains(t, out.String(), "retrievable until")
}

// TestCredentialRotate_NoGrace verifies the no-grace branch in formatting.
func TestCredentialRotate_NoGrace(t *testing.T) {
	defer resetCredentialFlags()
	fc := &fakeCredentialClient{
		rotateFunc: func(_ context.Context, req *qrdbtransport.CredentialRotateRequest) (*qrdbtransport.CredentialRotateResponse, error) {
			return &qrdbtransport.CredentialRotateResponse{
				Name:            req.Name,
				PreviousVersion: 1,
				NewVersion:      2,
			}, nil
		},
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialRotateCmd()
	cmd.SetArgs([]string{"my-key", "--value", "v"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.NotContains(t, out.String(), "retrievable until")
}

// TestCredentialRotate_JSON exercises the JSON output branch.
func TestCredentialRotate_JSON(t *testing.T) {
	defer resetCredentialFlags()
	fc := &fakeCredentialClient{
		rotateFunc: func(_ context.Context, req *qrdbtransport.CredentialRotateRequest) (*qrdbtransport.CredentialRotateResponse, error) {
			return &qrdbtransport.CredentialRotateResponse{
				Name:            req.Name,
				PreviousVersion: 5,
				NewVersion:      6,
				GraceUntilUnix:  100,
			}, nil
		},
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialRotateCmd()
	cmd.SetArgs([]string{"my-key", "--value", "v", "--output", "json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	got := out.String()
	assert.Contains(t, got, `"name": "my-key"`)
	assert.Contains(t, got, `"previous_version": 5`)
	assert.Contains(t, got, `"new_version": 6`)
}

// =============================================================================
// retrieve
// =============================================================================

// TestCredentialRetrieve_Current returns the current-version value.
func TestCredentialRetrieve_Current(t *testing.T) {
	defer resetCredentialFlags()
	fc := &fakeCredentialClient{
		retrieveVersionFunc: func(_ context.Context, req *qrdbtransport.CredentialRetrieveVersionRequest) (*qrdbtransport.CredentialRetrieveVersionResponse, error) {
			return &qrdbtransport.CredentialRetrieveVersionResponse{Value: []byte("current-value")}, nil
		},
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialRetrieveCmd()
	cmd.SetArgs([]string{"my-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastRetrieveReq)
	assert.Equal(t, uint64(0), fc.lastRetrieveReq.Version)
	assert.Contains(t, out.String(), "current-value")
}

// TestCredentialRetrieve_SpecificVersion forwards --version.
func TestCredentialRetrieve_SpecificVersion(t *testing.T) {
	defer resetCredentialFlags()
	fc := &fakeCredentialClient{}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialRetrieveCmd()
	cmd.SetArgs([]string{"my-key", "--version", "3"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastRetrieveReq)
	assert.Equal(t, uint64(3), fc.lastRetrieveReq.Version)
}

// =============================================================================
// versions
// =============================================================================

// TestCredentialVersions_Success renders the table format with rich metadata.
func TestCredentialVersions_Success(t *testing.T) {
	defer resetCredentialFlags()
	now := time.Now().Unix()
	fc := &fakeCredentialClient{
		listVersionsFunc: func(_ context.Context, req *qrdbtransport.CredentialListVersionsRequest) (*qrdbtransport.CredentialListVersionsResponse, error) {
			return &qrdbtransport.CredentialListVersionsResponse{
				Name: req.Name,
				Versions: []qrdbtransport.CredentialVersionInfo{
					{Version: 1, IsCurrent: false, CreatedAtUnix: now - 86400, GraceUntilUnix: now + 3600},
					{Version: 2, IsCurrent: true},
				},
			}, nil
		},
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialVersionsCmd()
	cmd.SetArgs([]string{"my-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	got := out.String()
	assert.Contains(t, got, "VERSION")
	assert.Contains(t, got, "CURRENT")
	assert.Contains(t, got, "no")
	assert.Contains(t, got, "yes")
}

// TestCredentialVersions_Empty renders the friendly empty marker.
func TestCredentialVersions_Empty(t *testing.T) {
	defer resetCredentialFlags()
	fc := &fakeCredentialClient{}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialVersionsCmd()
	cmd.SetArgs([]string{"my-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Contains(t, out.String(), "(no versions)")
}

// TestCredentialVersions_JSON outputs a JSON envelope.
func TestCredentialVersions_JSON(t *testing.T) {
	defer resetCredentialFlags()
	fc := &fakeCredentialClient{
		listVersionsFunc: func(_ context.Context, req *qrdbtransport.CredentialListVersionsRequest) (*qrdbtransport.CredentialListVersionsResponse, error) {
			return &qrdbtransport.CredentialListVersionsResponse{
				Name: req.Name,
				Versions: []qrdbtransport.CredentialVersionInfo{
					{Version: 1, IsCurrent: true},
				},
			}, nil
		},
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialVersionsCmd()
	cmd.SetArgs([]string{"my-key", "--output", "json"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	got := out.String()
	assert.Contains(t, got, `"name": "my-key"`)
	assert.Contains(t, got, `"versions"`)
}

// =============================================================================
// delete
// =============================================================================

// TestCredentialDelete_Success verifies the SDK delete dispatch.
func TestCredentialDelete_Success(t *testing.T) {
	defer resetCredentialFlags()
	fc := &fakeCredentialClient{}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialDeleteCmd()
	cmd.SetArgs([]string{"my-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	assert.Equal(t, "my-key", fc.lastDeleteName)
	assert.Contains(t, out.String(), `credential "my-key" deleted`)
}

// =============================================================================
// gc
// =============================================================================

// TestCredentialGC_Success verifies the reap count is reported.
func TestCredentialGC_Success(t *testing.T) {
	defer resetCredentialFlags()
	fc := &fakeCredentialClient{
		gcExpiredFunc: func(_ context.Context, req *qrdbtransport.CredentialGCRequest) (*qrdbtransport.CredentialGCResponse, error) {
			return &qrdbtransport.CredentialGCResponse{Reaped: 7}, nil
		},
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialGCCmd()
	cmd.SetArgs([]string{"my-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	require.NoError(t, cmd.Execute())
	require.NotNil(t, fc.lastGCReq)
	assert.Equal(t, "my-key", fc.lastGCReq.Name)
	assert.Contains(t, out.String(), "reaped 7 expired version(s)")
}

// =============================================================================
// Golden-output formatters (writeCredentialNames / writeCredentialRotateResult /
// writeCredentialVersions). These exercise the format helpers directly so the
// json + table branches are covered even when callers swap formats.
// =============================================================================

// =============================================================================
// SDK factory error path
// =============================================================================

// TestCreateSDKCredentialClient_PropagatesSDKError exercises the
// production factory: when sdkClientFactory fails, the credential
// factory must surface the same error rather than panicking.
func TestCreateSDKCredentialClient_PropagatesSDKError(t *testing.T) {
	orig := sdkClientFactory
	wantErr := stderrors.New("sdk init failed")
	sdkClientFactory = func() (quicbaas.Client, error) {
		return nil, wantErr
	}
	t.Cleanup(func() { sdkClientFactory = orig })

	_, err := createSDKCredentialClient()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}
