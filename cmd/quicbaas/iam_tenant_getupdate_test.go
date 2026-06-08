// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

func TestRunIAMTenantGet_WithSettingsAndQuotas(t *testing.T) {
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	mock, cleanup := setupIAMMock(t)
	defer cleanup()
	mock.tenantClient.GetFunc = func(_ context.Context, tenantID string) (*iam.Tenant, error) {
		return &iam.Tenant{
			ID:   tenantID,
			Name: tenantID,
			Settings: map[string]string{
				"max_users":    "42",
				"mfa_required": "true",
			},
			Quotas:    &iam.TenantQuotas{MaxUsers: 99},
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}, nil
	}

	origFmt := iamOutputFormat
	defer func() { iamOutputFormat = origFmt }()
	iamOutputFormat = "table"

	cmd := newIAMTenantGetCommand()
	require.NoError(t, runIAMTenantGetCommand(cmd, []string{"acme"}))
}

func TestRunIAMTenantGet_SDKError(t *testing.T) {
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	mock, cleanup := setupIAMMock(t)
	defer cleanup()
	mock.tenantClient.GetFunc = func(_ context.Context, _ string) (*iam.Tenant, error) {
		return nil, errors.New(errors.ErrNotFound, "iam", "tenant not found")
	}

	cmd := newIAMTenantGetCommand()
	require.Error(t, runIAMTenantGetCommand(cmd, []string{"missing"}))
}

func TestRunIAMTenantGet_FactoryError(t *testing.T) {
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	orig := iamClientFactory
	defer func() { iamClientFactory = orig }()
	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrInternal, "iam", "factory boom")
	}

	cmd := newIAMTenantGetCommand()
	require.Error(t, runIAMTenantGetCommand(cmd, []string{"acme"}))
}

func TestRunIAMTenantUpdate_NoFlags(t *testing.T) {
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	_, cleanup := setupIAMMock(t)
	defer cleanup()

	cmd := newIAMTenantUpdateCommand()
	err := runIAMTenantUpdateCommand(cmd, []string{"acme"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one of")
}

func TestRunIAMTenantUpdate_FetchesNameWhenNotSpecified(t *testing.T) {
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	mock, cleanup := setupIAMMock(t)
	defer cleanup()

	fetched := false
	mock.tenantClient.GetFunc = func(_ context.Context, tenantID string) (*iam.Tenant, error) {
		fetched = true
		return &iam.Tenant{ID: tenantID, Name: tenantID, DisplayName: "Existing Name", CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
	}
	var gotName string
	mock.tenantClient.UpdateFunc = func(_ context.Context, tenantID string, req *iam.UpdateTenantRequest) (*iam.Tenant, error) {
		gotName = req.DisplayName
		return &iam.Tenant{ID: tenantID, Name: tenantID, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
	}

	cmd := newIAMTenantUpdateCommand()
	require.NoError(t, cmd.Flags().Set("mfa-required", "true"))

	require.NoError(t, runIAMTenantUpdateCommand(cmd, []string{"acme"}))
	assert.True(t, fetched, "Get should be called to fill display name")
	assert.Equal(t, "Existing Name", gotName)
}

func TestRunIAMTenantUpdate_FetchNameFallsBackToName(t *testing.T) {
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	mock, cleanup := setupIAMMock(t)
	defer cleanup()

	mock.tenantClient.GetFunc = func(_ context.Context, tenantID string) (*iam.Tenant, error) {
		// No DisplayName -> command falls back to Name.
		return &iam.Tenant{ID: tenantID, Name: "fallback-name", CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
	}
	var gotName string
	mock.tenantClient.UpdateFunc = func(_ context.Context, tenantID string, req *iam.UpdateTenantRequest) (*iam.Tenant, error) {
		gotName = req.DisplayName
		return &iam.Tenant{ID: tenantID, Name: tenantID, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
	}

	cmd := newIAMTenantUpdateCommand()
	require.NoError(t, cmd.Flags().Set("max-users", "10"))

	require.NoError(t, runIAMTenantUpdateCommand(cmd, []string{"acme"}))
	assert.Equal(t, "fallback-name", gotName)
}

func TestRunIAMTenantUpdate_FetchNameSDKError(t *testing.T) {
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	mock, cleanup := setupIAMMock(t)
	defer cleanup()

	mock.tenantClient.GetFunc = func(_ context.Context, _ string) (*iam.Tenant, error) {
		return nil, errors.New(errors.ErrNotFound, "iam", "tenant not found")
	}

	cmd := newIAMTenantUpdateCommand()
	require.NoError(t, cmd.Flags().Set("mfa-required", "true"))

	require.Error(t, runIAMTenantUpdateCommand(cmd, []string{"acme"}))
}

func TestRunIAMTenantUpdate_SDKError(t *testing.T) {
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	mock, cleanup := setupIAMMock(t)
	defer cleanup()

	mock.tenantClient.UpdateFunc = func(_ context.Context, _ string, _ *iam.UpdateTenantRequest) (*iam.Tenant, error) {
		return nil, errors.New(errors.ErrInternal, "iam", "update failed")
	}

	cmd := newIAMTenantUpdateCommand()
	require.NoError(t, cmd.Flags().Set("name", "New Name"))

	require.Error(t, runIAMTenantUpdateCommand(cmd, []string{"acme"}))
}
