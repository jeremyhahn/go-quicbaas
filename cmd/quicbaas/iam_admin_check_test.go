// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

func TestRunIAMAdminCheckPermission_RichResultDefault(t *testing.T) {
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	mock, cleanup := setupIAMMock(t)
	defer cleanup()
	mock.adminClient.CheckPermissionFunc = func(_ context.Context, _ *iam.AdminCheckRequest) (*iam.AdminCheckResponse, error) {
		return &iam.AdminCheckResponse{
			Allowed:      true,
			AssignmentID: "assign-1",
			Scope:        iam.AdminScopeTenant, // non-global -> prints scope line
			DenialReason: "n/a",
		}, nil
	}

	origFmt := iamOutputFormat
	defer func() { iamOutputFormat = origFmt }()
	iamOutputFormat = "table"

	cmd := newIAMAdminCheckPermissionCommand()
	require.NoError(t, runIAMAdminCheckPermissionCommand(cmd, nil))
}

func TestRunIAMAdminCheckPermission_SDKError(t *testing.T) {
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	mock, cleanup := setupIAMMock(t)
	defer cleanup()
	mock.adminClient.CheckPermissionFunc = func(_ context.Context, _ *iam.AdminCheckRequest) (*iam.AdminCheckResponse, error) {
		return nil, errors.New(errors.ErrInternal, "iam", "check failed")
	}

	cmd := newIAMAdminCheckPermissionCommand()
	require.Error(t, runIAMAdminCheckPermissionCommand(cmd, nil))
}
