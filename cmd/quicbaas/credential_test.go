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
	"strings"
	"testing"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"
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
	if timeout == 0 {
		timeout = 5 * time.Second
	}
	return func() { credentialClientFactory = orig }
}

// withFailingCredentialFactory installs a factory that returns the given
// error. Used to test factory-error propagation.
func withFailingCredentialFactory(err error) func() {
	orig := credentialClientFactory
	credentialClientFactory = func() (qrdbtransport.CredentialClient, error) { return nil, err }
	if timeout == 0 {
		timeout = 5 * time.Second
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

// =============================================================================
// Error types
// =============================================================================

func TestCredentialNameRequiredError_Message(t *testing.T) {
	err := &CredentialNameRequiredError{Verb: "submit"}
	assert.Contains(t, err.Error(), "submit")
	assert.Contains(t, err.Error(), "<name>")
}

func TestCredentialValueRequiredError_Message(t *testing.T) {
	err := &CredentialValueRequiredError{Verb: "rotate"}
	assert.Contains(t, err.Error(), "rotate")
	assert.Contains(t, err.Error(), "--value")
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

// TestCredentialSubmit_MissingName rejects whitespace-only positional arg.
func TestCredentialSubmit_MissingName(t *testing.T) {
	defer resetCredentialFlags()
	credentialValue = "x"
	cmd := newCredentialSubmitCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runCredentialSubmit(cmd, []string{"  "})
	require.Error(t, err)
	var typed *CredentialNameRequiredError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "submit", typed.Verb)
}

// TestCredentialSubmit_MissingValue rejects empty --value flag.
func TestCredentialSubmit_MissingValue(t *testing.T) {
	defer resetCredentialFlags()
	cmd := newCredentialSubmitCmd()
	cmd.SetArgs([]string{"my-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	var typed *CredentialValueRequiredError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "submit", typed.Verb)
}

// TestCredentialSubmit_FactoryError surfaces typed factory errors.
func TestCredentialSubmit_FactoryError(t *testing.T) {
	defer resetCredentialFlags()
	wantErr := stderrors.New("factory boom")
	defer withFailingCredentialFactory(wantErr)()

	cmd := newCredentialSubmitCmd()
	cmd.SetArgs([]string{"my-key", "--value", "v"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

// TestCredentialSubmit_TransportError surfaces SDK errors.
func TestCredentialSubmit_TransportError(t *testing.T) {
	defer resetCredentialFlags()
	wantErr := stderrors.New("submit failed")
	fc := &fakeCredentialClient{
		submitFunc: func(_ context.Context, _ *qrdbtransport.CredentialSubmitRequest) error {
			return wantErr
		},
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialSubmitCmd()
	cmd.SetArgs([]string{"my-key", "--value", "v"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
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
	got := out.String()
	assert.Contains(t, got, "alpha")
	assert.Contains(t, got, "beta")
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
	assert.Contains(t, out.String(), "(no credentials)")
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

// TestCredentialList_FactoryError surfaces factory errors.
func TestCredentialList_FactoryError(t *testing.T) {
	defer resetCredentialFlags()
	wantErr := stderrors.New("factory boom")
	defer withFailingCredentialFactory(wantErr)()

	cmd := newCredentialListCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

// TestCredentialList_TransportError surfaces SDK errors.
func TestCredentialList_TransportError(t *testing.T) {
	defer resetCredentialFlags()
	wantErr := stderrors.New("list failed")
	fc := &fakeCredentialClient{
		listFunc: func(_ context.Context, _ *qrdbtransport.CredentialListRequest) (*qrdbtransport.CredentialListResponse, error) {
			return nil, wantErr
		},
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialListCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
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

// TestCredentialRotate_MissingName rejects whitespace-only positional.
func TestCredentialRotate_MissingName(t *testing.T) {
	defer resetCredentialFlags()
	credentialValue = "v"
	cmd := newCredentialRotateCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runCredentialRotate(cmd, []string{"   "})
	require.Error(t, err)
	var typed *CredentialNameRequiredError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "rotate", typed.Verb)
}

// TestCredentialRotate_MissingValue rejects empty --value.
func TestCredentialRotate_MissingValue(t *testing.T) {
	defer resetCredentialFlags()
	cmd := newCredentialRotateCmd()
	cmd.SetArgs([]string{"my-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	var typed *CredentialValueRequiredError
	require.ErrorAs(t, err, &typed)
}

// TestCredentialRotate_FactoryError surfaces factory errors.
func TestCredentialRotate_FactoryError(t *testing.T) {
	defer resetCredentialFlags()
	wantErr := stderrors.New("factory boom")
	defer withFailingCredentialFactory(wantErr)()

	cmd := newCredentialRotateCmd()
	cmd.SetArgs([]string{"my-key", "--value", "v"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

// TestCredentialRotate_TransportError surfaces SDK errors.
func TestCredentialRotate_TransportError(t *testing.T) {
	defer resetCredentialFlags()
	wantErr := stderrors.New("rotate failed")
	fc := &fakeCredentialClient{
		rotateFunc: func(_ context.Context, _ *qrdbtransport.CredentialRotateRequest) (*qrdbtransport.CredentialRotateResponse, error) {
			return nil, wantErr
		},
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialRotateCmd()
	cmd.SetArgs([]string{"my-key", "--value", "v"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
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

// TestCredentialRetrieve_MissingName rejects whitespace-only positional.
func TestCredentialRetrieve_MissingName(t *testing.T) {
	defer resetCredentialFlags()
	cmd := newCredentialRetrieveCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runCredentialRetrieve(cmd, []string{"  "})
	require.Error(t, err)
	var typed *CredentialNameRequiredError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "retrieve", typed.Verb)
}

// TestCredentialRetrieve_FactoryError surfaces factory errors.
func TestCredentialRetrieve_FactoryError(t *testing.T) {
	defer resetCredentialFlags()
	wantErr := stderrors.New("factory boom")
	defer withFailingCredentialFactory(wantErr)()

	cmd := newCredentialRetrieveCmd()
	cmd.SetArgs([]string{"my-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

// TestCredentialRetrieve_TransportError surfaces SDK errors.
func TestCredentialRetrieve_TransportError(t *testing.T) {
	defer resetCredentialFlags()
	wantErr := stderrors.New("retrieve failed")
	fc := &fakeCredentialClient{
		retrieveVersionFunc: func(_ context.Context, _ *qrdbtransport.CredentialRetrieveVersionRequest) (*qrdbtransport.CredentialRetrieveVersionResponse, error) {
			return nil, wantErr
		},
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialRetrieveCmd()
	cmd.SetArgs([]string{"my-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
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
					{Version: 2, IsCurrent: true, CreatedAtUnix: now},
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

// TestCredentialVersions_MissingName rejects whitespace-only positional.
func TestCredentialVersions_MissingName(t *testing.T) {
	defer resetCredentialFlags()
	cmd := newCredentialVersionsCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runCredentialVersions(cmd, []string{" "})
	require.Error(t, err)
	var typed *CredentialNameRequiredError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "versions", typed.Verb)
}

// TestCredentialVersions_FactoryError surfaces factory errors.
func TestCredentialVersions_FactoryError(t *testing.T) {
	defer resetCredentialFlags()
	wantErr := stderrors.New("factory boom")
	defer withFailingCredentialFactory(wantErr)()

	cmd := newCredentialVersionsCmd()
	cmd.SetArgs([]string{"my-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

// TestCredentialVersions_TransportError surfaces SDK errors.
func TestCredentialVersions_TransportError(t *testing.T) {
	defer resetCredentialFlags()
	wantErr := stderrors.New("versions failed")
	fc := &fakeCredentialClient{
		listVersionsFunc: func(_ context.Context, _ *qrdbtransport.CredentialListVersionsRequest) (*qrdbtransport.CredentialListVersionsResponse, error) {
			return nil, wantErr
		},
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialVersionsCmd()
	cmd.SetArgs([]string{"my-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
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

// TestCredentialDelete_MissingName rejects whitespace-only positional.
func TestCredentialDelete_MissingName(t *testing.T) {
	defer resetCredentialFlags()
	cmd := newCredentialDeleteCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runCredentialDelete(cmd, []string{"  "})
	require.Error(t, err)
	var typed *CredentialNameRequiredError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "delete", typed.Verb)
}

// TestCredentialDelete_FactoryError surfaces factory errors.
func TestCredentialDelete_FactoryError(t *testing.T) {
	defer resetCredentialFlags()
	wantErr := stderrors.New("factory boom")
	defer withFailingCredentialFactory(wantErr)()

	cmd := newCredentialDeleteCmd()
	cmd.SetArgs([]string{"my-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

// TestCredentialDelete_TransportError surfaces SDK errors.
func TestCredentialDelete_TransportError(t *testing.T) {
	defer resetCredentialFlags()
	wantErr := stderrors.New("delete failed")
	fc := &fakeCredentialClient{
		deleteFunc: func(_ context.Context, _ string) error { return wantErr },
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialDeleteCmd()
	cmd.SetArgs([]string{"my-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
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

// TestCredentialGC_MissingName rejects whitespace-only positional.
func TestCredentialGC_MissingName(t *testing.T) {
	defer resetCredentialFlags()
	cmd := newCredentialGCCmd()
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := runCredentialGC(cmd, []string{"  "})
	require.Error(t, err)
	var typed *CredentialNameRequiredError
	require.ErrorAs(t, err, &typed)
	assert.Equal(t, "gc", typed.Verb)
}

// TestCredentialGC_FactoryError surfaces factory errors.
func TestCredentialGC_FactoryError(t *testing.T) {
	defer resetCredentialFlags()
	wantErr := stderrors.New("factory boom")
	defer withFailingCredentialFactory(wantErr)()

	cmd := newCredentialGCCmd()
	cmd.SetArgs([]string{"my-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

// TestCredentialGC_TransportError surfaces SDK errors.
func TestCredentialGC_TransportError(t *testing.T) {
	defer resetCredentialFlags()
	wantErr := stderrors.New("gc failed")
	fc := &fakeCredentialClient{
		gcExpiredFunc: func(_ context.Context, _ *qrdbtransport.CredentialGCRequest) (*qrdbtransport.CredentialGCResponse, error) {
			return nil, wantErr
		},
	}
	defer withFakeCredentialFactory(fc)()

	cmd := newCredentialGCCmd()
	cmd.SetArgs([]string{"my-key"})
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)

	err := cmd.Execute()
	require.Error(t, err)
	assert.ErrorIs(t, err, wantErr)
}

// =============================================================================
// Golden-output formatters (writeCredentialNames / writeCredentialRotateResult /
// writeCredentialVersions). These exercise the format helpers directly so the
// json + table branches are covered even when callers swap formats.
// =============================================================================

// TestWriteCredentialNames_TableNonEmpty prints one name per line.
func TestWriteCredentialNames_TableNonEmpty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeCredentialNames(&buf, []string{"a", "b"}, "table"))
	got := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	assert.Equal(t, []string{"a", "b"}, got)
}

// TestWriteCredentialNames_TableEmpty prints the friendly empty marker.
func TestWriteCredentialNames_TableEmpty(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeCredentialNames(&buf, nil, "table"))
	assert.Equal(t, "(no credentials)\n", buf.String())
}

// TestWriteCredentialNames_JSON wraps the names in a map envelope.
func TestWriteCredentialNames_JSON(t *testing.T) {
	var buf bytes.Buffer
	require.NoError(t, writeCredentialNames(&buf, []string{"a"}, "json"))
	got := buf.String()
	assert.Contains(t, got, `"names"`)
	assert.Contains(t, got, `"a"`)
}

// TestWriteCredentialRotateResult_TableNoGrace omits the grace tail.
func TestWriteCredentialRotateResult_TableNoGrace(t *testing.T) {
	var buf bytes.Buffer
	resp := &qrdbtransport.CredentialRotateResponse{
		Name: "k", PreviousVersion: 1, NewVersion: 2,
	}
	require.NoError(t, writeCredentialRotateResult(&buf, resp, "table"))
	got := buf.String()
	assert.Contains(t, got, "rotated k")
	assert.Contains(t, got, "previous v1 -> new v2")
	assert.NotContains(t, got, "retrievable until")
}

// TestWriteCredentialRotateResult_TableWithGrace includes the grace tail.
func TestWriteCredentialRotateResult_TableWithGrace(t *testing.T) {
	var buf bytes.Buffer
	resp := &qrdbtransport.CredentialRotateResponse{
		Name: "k", PreviousVersion: 1, NewVersion: 2,
		GraceUntilUnix: time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC).Unix(),
	}
	require.NoError(t, writeCredentialRotateResult(&buf, resp, "table"))
	assert.Contains(t, buf.String(), "retrievable until")
}

// TestWriteCredentialRotateResult_JSON wraps the response in JSON.
func TestWriteCredentialRotateResult_JSON(t *testing.T) {
	var buf bytes.Buffer
	resp := &qrdbtransport.CredentialRotateResponse{Name: "k", PreviousVersion: 1, NewVersion: 2}
	require.NoError(t, writeCredentialRotateResult(&buf, resp, "json"))
	got := buf.String()
	assert.Contains(t, got, `"name": "k"`)
	assert.Contains(t, got, `"new_version": 2`)
}

// TestWriteCredentialVersions_TableEmpty prints the friendly empty marker.
func TestWriteCredentialVersions_TableEmpty(t *testing.T) {
	var buf bytes.Buffer
	resp := &qrdbtransport.CredentialListVersionsResponse{Name: "k"}
	require.NoError(t, writeCredentialVersions(&buf, resp, "table"))
	assert.Contains(t, buf.String(), "(no versions)")
}

// TestWriteCredentialVersions_TableRendered includes header + rows, with
// "-" placeholders for missing timestamps.
func TestWriteCredentialVersions_TableRendered(t *testing.T) {
	var buf bytes.Buffer
	resp := &qrdbtransport.CredentialListVersionsResponse{
		Name: "k",
		Versions: []qrdbtransport.CredentialVersionInfo{
			{Version: 1, IsCurrent: true},
			{Version: 2, IsCurrent: false, CreatedAtUnix: 100, GraceUntilUnix: 200},
		},
	}
	require.NoError(t, writeCredentialVersions(&buf, resp, "table"))
	got := buf.String()
	assert.Contains(t, got, "VERSION")
	assert.Contains(t, got, "yes")
	assert.Contains(t, got, "no")
}

// TestWriteCredentialVersions_JSON wraps the response in JSON.
func TestWriteCredentialVersions_JSON(t *testing.T) {
	var buf bytes.Buffer
	resp := &qrdbtransport.CredentialListVersionsResponse{
		Name: "k",
		Versions: []qrdbtransport.CredentialVersionInfo{
			{Version: 1, IsCurrent: true},
		},
	}
	require.NoError(t, writeCredentialVersions(&buf, resp, "json"))
	got := buf.String()
	assert.Contains(t, got, `"name": "k"`)
	assert.Contains(t, got, `"versions"`)
}

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
