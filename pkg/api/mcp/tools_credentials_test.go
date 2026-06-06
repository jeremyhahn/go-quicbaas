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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// =============================================================================
// Mock CredentialServicer
// =============================================================================

type mockCredentialServicer struct {
	submitFn      func(ctx context.Context, name string, value []byte) error
	deleteFn      func(ctx context.Context, name string) error
	strategyVal   string
	listFn        func(ctx context.Context) ([]string, error)
	rotateFn      func(ctx context.Context, name string, opts qrdbsdk.RotateOptions) (*qrdbsdk.RotateResult, error)
	retrieveVerFn func(ctx context.Context, name string, version uint64) ([]byte, error)
	listVerFn     func(ctx context.Context, name string) ([]qrdbsdk.CredentialVersion, error)
	gcFn          func(ctx context.Context, name string) (int, error)
}

func (m *mockCredentialServicer) CredentialStore(ctx context.Context, name string, value []byte) error {
	return nil
}

func (m *mockCredentialServicer) CredentialRetrieve(ctx context.Context, name string) ([]byte, error) {
	return []byte("v"), nil
}

func (m *mockCredentialServicer) CredentialSubmit(ctx context.Context, name string, value []byte) error {
	if m.submitFn != nil {
		return m.submitFn(ctx, name, value)
	}
	return nil
}

func (m *mockCredentialServicer) CredentialDelete(ctx context.Context, name string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, name)
	}
	return nil
}

func (m *mockCredentialServicer) CredentialStrategy() string {
	if m.strategyVal != "" {
		return m.strategyVal
	}
	return "manual"
}

func (m *mockCredentialServicer) CredentialList(ctx context.Context) ([]string, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return []string{"cred-a"}, nil
}

func (m *mockCredentialServicer) CredentialRotate(ctx context.Context, name string, opts qrdbsdk.RotateOptions) (*qrdbsdk.RotateResult, error) {
	if m.rotateFn != nil {
		return m.rotateFn(ctx, name, opts)
	}
	return &qrdbsdk.RotateResult{
		Name: name, PreviousVersion: 1, NewVersion: 2,
		GraceUntil: time.Now().Add(time.Hour),
	}, nil
}

func (m *mockCredentialServicer) CredentialRetrieveVersion(ctx context.Context, name string, version uint64) ([]byte, error) {
	if m.retrieveVerFn != nil {
		return m.retrieveVerFn(ctx, name, version)
	}
	return []byte("version-value"), nil
}

func (m *mockCredentialServicer) CredentialListVersions(ctx context.Context, name string) ([]qrdbsdk.CredentialVersion, error) {
	if m.listVerFn != nil {
		return m.listVerFn(ctx, name)
	}
	return []qrdbsdk.CredentialVersion{
		{Name: name, Version: 2, IsCurrent: true, CreatedAt: time.Now()},
		{Name: name, Version: 1, IsCurrent: false, CreatedAt: time.Now(), GraceUntil: time.Now().Add(time.Hour)},
	}, nil
}

func (m *mockCredentialServicer) CredentialGCExpired(ctx context.Context, name string) (int, error) {
	if m.gcFn != nil {
		return m.gcFn(ctx, name)
	}
	return 3, nil
}

func newCredentialRegistry(t *testing.T, svc *mockCredentialServicer) *ToolRegistry {
	t.Helper()
	log, _ := logger.New(logger.LogConfig{Level: "error", Output: "stdout"})
	reg := NewToolRegistry(&mockService{}, log)
	if svc != nil {
		reg.SetCredentialService(svc)
	}
	return reg
}

// =============================================================================
// Typed error messages
// =============================================================================

func TestCredentialTypedErrors(t *testing.T) {
	assert.Equal(t, "credential service not configured", (&CredentialServiceUnavailableError{}).Error())
	assert.Equal(t, "name is required", (&CredentialNameRequiredError{}).Error())
	assert.Equal(t, "value is required", (&CredentialValueRequiredError{}).Error())
	assert.Equal(t, "new_value is required", (&CredentialNewValueRequiredError{}).Error())
}

// =============================================================================
// Happy paths
// =============================================================================

func TestExecuteCredentialTool_HappyPaths(t *testing.T) {
	reg := newCredentialRegistry(t, &mockCredentialServicer{})
	ctx := context.Background()

	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{"qrdb_credential_submit", map[string]interface{}{"name": "c1", "value": "v1"}},
		{"qrdb_credential_strategy", map[string]interface{}{}},
		{"qrdb_credential_delete", map[string]interface{}{"name": "c1"}},
		{"qrdb_credential_list", map[string]interface{}{}},
		{"qrdb_credential_rotate", map[string]interface{}{"name": "c1", "new_value": "v2", "grace_period_seconds": int64(60)}},
		{"qrdb_credential_retrieve_version", map[string]interface{}{"name": "c1", "version": uint64(1)}},
		{"qrdb_credential_list_versions", map[string]interface{}{"name": "c1"}},
		{"qrdb_credential_gc_expired", map[string]interface{}{"name": "c1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := reg.executeCredentialTool(ctx, tc.name, tc.args)
			require.NoError(t, err)
			require.NotNil(t, result)
		})
	}
}

// =============================================================================
// Service not configured
// =============================================================================

func TestExecuteCredentialTool_ServiceUnavailable(t *testing.T) {
	reg := newCredentialRegistry(t, nil) // no SetCredentialService
	_, err := reg.executeCredentialTool(context.Background(), "qrdb_credential_list", map[string]interface{}{})
	var unavailable *CredentialServiceUnavailableError
	require.ErrorAs(t, err, &unavailable)
}

func TestExecuteCredentialTool_UnknownTool(t *testing.T) {
	reg := newCredentialRegistry(t, &mockCredentialServicer{})
	_, err := reg.executeCredentialTool(context.Background(), "qrdb_credential_bogus", map[string]interface{}{})
	require.Error(t, err)
}

// =============================================================================
// Required field validation
// =============================================================================

func TestExecuteCredentialTool_MissingRequired(t *testing.T) {
	reg := newCredentialRegistry(t, &mockCredentialServicer{})
	ctx := context.Background()

	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{"qrdb_credential_submit", map[string]interface{}{}},             // name
		{"qrdb_credential_submit", map[string]interface{}{"name": "c1"}}, // value
		{"qrdb_credential_delete", map[string]interface{}{}},             // name
		{"qrdb_credential_rotate", map[string]interface{}{}},             // name
		{"qrdb_credential_rotate", map[string]interface{}{"name": "c1"}}, // new_value
		{"qrdb_credential_retrieve_version", map[string]interface{}{}},   // name
		{"qrdb_credential_list_versions", map[string]interface{}{}},      // name
		{"qrdb_credential_gc_expired", map[string]interface{}{}},         // name
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reg.executeCredentialTool(ctx, tc.name, tc.args)
			require.Error(t, err)
		})
	}
}

// =============================================================================
// Service errors propagate
// =============================================================================

func TestExecuteCredentialTool_ServiceErrors(t *testing.T) {
	sentinel := errors.New("cred boom")
	svc := &mockCredentialServicer{
		submitFn: func(context.Context, string, []byte) error { return sentinel },
		deleteFn: func(context.Context, string) error { return sentinel },
		listFn:   func(context.Context) ([]string, error) { return nil, sentinel },
		rotateFn: func(context.Context, string, qrdbsdk.RotateOptions) (*qrdbsdk.RotateResult, error) {
			return nil, sentinel
		},
		retrieveVerFn: func(context.Context, string, uint64) ([]byte, error) { return nil, sentinel },
		listVerFn:     func(context.Context, string) ([]qrdbsdk.CredentialVersion, error) { return nil, sentinel },
		gcFn:          func(context.Context, string) (int, error) { return 0, sentinel },
	}
	reg := newCredentialRegistry(t, svc)
	ctx := context.Background()

	cases := []struct {
		name string
		args map[string]interface{}
	}{
		{"qrdb_credential_submit", map[string]interface{}{"name": "c1", "value": "v1"}},
		{"qrdb_credential_delete", map[string]interface{}{"name": "c1"}},
		{"qrdb_credential_list", map[string]interface{}{}},
		{"qrdb_credential_rotate", map[string]interface{}{"name": "c1", "new_value": "v2"}},
		{"qrdb_credential_retrieve_version", map[string]interface{}{"name": "c1"}},
		{"qrdb_credential_list_versions", map[string]interface{}{"name": "c1"}},
		{"qrdb_credential_gc_expired", map[string]interface{}{"name": "c1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := reg.executeCredentialTool(ctx, tc.name, tc.args)
			require.ErrorIs(t, err, sentinel)
		})
	}
}

// CredentialList returning nil is normalized to an empty slice.
func TestExecuteCredentialList_NilNamesNormalized(t *testing.T) {
	svc := &mockCredentialServicer{
		listFn: func(context.Context) ([]string, error) { return nil, nil },
	}
	reg := newCredentialRegistry(t, svc)
	result, err := reg.executeCredentialTool(context.Background(), "qrdb_credential_list", map[string]interface{}{})
	require.NoError(t, err)
	m, ok := result.(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, []string{}, m["names"])
}
