//go:build iam

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

package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

func testIAMSetupLogger(t *testing.T) func() {
	t.Helper()
	origLog := log
	logConfig := logger.LogConfig{
		Level:     "error",
		Output:    "stderr",
		Component: "test-iam",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog
	return func() {
		log = origLog
	}
}

// =============================================================================
// IAM Admin Assignment Create Command Tests
// Note: These tests modify global state and cannot run in parallel
// =============================================================================

func TestIAMAdminAssignmentCreate_InvalidRole(t *testing.T) {
	// Not parallel - modifies global state

	cleanup := testIAMSetupLogger(t)
	defer cleanup()

	// Create command first, then set global values
	cmd := newIAMAdminAssignmentCreateCommand()

	// Set values after command creation (flag bindings are done)
	adminRole = "invalid_role"
	adminUserID = "user_test"
	adminGrantedBy = "admin_test"

	err := runIAMAdminAssignmentCreateCommand(cmd, []string{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid admin role")
}

func TestIAMAdminAssignmentCreate_InvalidExpiration(t *testing.T) {
	// Not parallel - modifies global state

	cleanup := testIAMSetupLogger(t)
	defer cleanup()

	// Create command first, then set global values
	cmd := newIAMAdminAssignmentCreateCommand()

	// Set values after command creation (flag bindings are done)
	adminRole = "tenant_admin"
	adminUserID = "user_test"
	adminGrantedBy = "admin_test"
	adminExpiresAt = "invalid-date-format"

	err := runIAMAdminAssignmentCreateCommand(cmd, []string{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid expires-at format")
}

func TestIAMAdminAssignmentCreate_FactoryError(t *testing.T) {
	// Not parallel - modifies global state

	cleanup := testIAMSetupLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	// Create command first
	cmd := newIAMAdminAssignmentCreateCommand()

	// Set values after command creation
	adminRole = "tenant_admin"
	adminUserID = "user_test"
	adminGrantedBy = "admin_test"
	adminExpiresAt = ""

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	err := runIAMAdminAssignmentCreateCommand(cmd, []string{})

	assert.Error(t, err)
}

// =============================================================================
// IAM Admin Assignment Get Command Tests
// =============================================================================

func TestIAMAdminAssignmentGet_FactoryError(t *testing.T) {
	// Not parallel - modifies global state

	cleanup := testIAMSetupLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	cmd := newIAMAdminAssignmentGetCommand()
	adminAssignmentID = "adm_test123"

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	err := runIAMAdminAssignmentGetCommand(cmd, []string{})

	assert.Error(t, err)
}

// =============================================================================
// IAM Admin Assignment Update Command Tests
// =============================================================================

func TestIAMAdminAssignmentUpdate_InvalidRole(t *testing.T) {
	// Not parallel - modifies global state

	cleanup := testIAMSetupLogger(t)
	defer cleanup()

	cmd := newIAMAdminAssignmentUpdateCommand()
	adminRole = "invalid_role"
	adminAssignmentID = "adm_test123"

	// Mark the role flag as changed
	cmd.Flags().Set("role", "invalid_role")

	err := runIAMAdminAssignmentUpdateCommand(cmd, []string{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid admin role")
}

func TestIAMAdminAssignmentUpdate_InvalidExpiration(t *testing.T) {
	// Not parallel - modifies global state

	cleanup := testIAMSetupLogger(t)
	defer cleanup()

	cmd := newIAMAdminAssignmentUpdateCommand()
	adminExpiresAt = "invalid-date"
	adminAssignmentID = "adm_test123"

	cmd.Flags().Set("expires-at", "invalid-date")

	err := runIAMAdminAssignmentUpdateCommand(cmd, []string{})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid expires-at format")
}

func TestIAMAdminAssignmentUpdate_FactoryError(t *testing.T) {
	// Not parallel - modifies global state

	cleanup := testIAMSetupLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	cmd := newIAMAdminAssignmentUpdateCommand()
	adminAssignmentID = "adm_test123"

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	err := runIAMAdminAssignmentUpdateCommand(cmd, []string{})

	assert.Error(t, err)
}

// =============================================================================
// IAM Admin Assignment Delete Command Tests
// =============================================================================

func TestIAMAdminAssignmentDelete_FactoryError(t *testing.T) {
	// Not parallel - modifies global state

	cleanup := testIAMSetupLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	cmd := newIAMAdminAssignmentDeleteCommand()
	adminAssignmentID = "adm_test123"
	iamForce = true

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	err := runIAMAdminAssignmentDeleteCommand(cmd, []string{})

	assert.Error(t, err)
}

// =============================================================================
// IAM Admin Assignment List Command Tests
// =============================================================================

func TestIAMAdminAssignmentList_FactoryError(t *testing.T) {
	// Not parallel - modifies global state

	cleanup := testIAMSetupLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMAdminAssignmentListCommand()
	err := runIAMAdminAssignmentListCommand(cmd, []string{})

	assert.Error(t, err)
}

// =============================================================================
// IAM Admin Check Permission Command Tests
// =============================================================================

func TestIAMAdminCheckPermission_FactoryError(t *testing.T) {
	// Not parallel - modifies global state

	cleanup := testIAMSetupLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	cmd := newIAMAdminCheckPermissionCommand()
	adminUserID = "user_test123"
	adminCheckPermission = "user:create"

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	err := runIAMAdminCheckPermissionCommand(cmd, []string{})

	assert.Error(t, err)
}

// =============================================================================
// IAM Admin Impersonate Start Command Tests
// =============================================================================

func TestIAMAdminImpersonateStart_FactoryError(t *testing.T) {
	// Not parallel - modifies global state

	cleanup := testIAMSetupLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	cmd := newIAMAdminImpersonateStartCommand()
	impersonateAdminUserID = "admin_test"
	impersonateTargetUserID = "user_target"
	impersonateReason = "test reason"

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	err := runIAMAdminImpersonateStartCommand(cmd, []string{})

	assert.Error(t, err)
}

// =============================================================================
// IAM Admin Impersonate End Command Tests
// =============================================================================

func TestIAMAdminImpersonateEnd_FactoryError(t *testing.T) {
	// Not parallel - modifies global state

	cleanup := testIAMSetupLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	cmd := newIAMAdminImpersonateEndCommand()
	impersonateSessionID = "session_test123"

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	err := runIAMAdminImpersonateEndCommand(cmd, []string{})

	assert.Error(t, err)
}

// =============================================================================
// IAM Admin Impersonate List Command Tests
// =============================================================================

func TestIAMAdminImpersonateList_FactoryError(t *testing.T) {
	// Not parallel - modifies global state

	cleanup := testIAMSetupLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMAdminImpersonateListCommand()
	err := runIAMAdminImpersonateListCommand(cmd, []string{})

	assert.Error(t, err)
}

// =============================================================================
// IAM Admin Command Structure Tests
// Note: These tests modify global state (package-level flag variables) via
// cobra flag bindings in the command constructors (StringVar, BoolVar, etc.),
// so subtests must NOT run in parallel.
// =============================================================================

func TestIAMAdminCommandStructure(t *testing.T) {
	t.Run("admin_command_has_subcommands", func(t *testing.T) {
		cmd := newIAMAdminCommand()
		assert.NotNil(t, cmd)
		assert.Equal(t, "admin", cmd.Use)
		assert.NotEmpty(t, cmd.Commands())
	})

	t.Run("assignment_command_has_subcommands", func(t *testing.T) {
		cmd := newIAMAdminAssignmentCommand()
		assert.NotNil(t, cmd)
		assert.Equal(t, "assignment", cmd.Use)
		assert.Len(t, cmd.Commands(), 5) // create, get, update, delete, list
	})

	t.Run("check_permission_command_structure", func(t *testing.T) {
		cmd := newIAMAdminCheckPermissionCommand()
		assert.NotNil(t, cmd)
		assert.Equal(t, "check-permission", cmd.Use)
	})

	t.Run("impersonate_command_has_subcommands", func(t *testing.T) {
		cmd := newIAMAdminImpersonateCommand()
		assert.NotNil(t, cmd)
		assert.Equal(t, "impersonate", cmd.Use)
		assert.NotEmpty(t, cmd.Commands())
	})
}

// =============================================================================
// Time Duration Parsing Tests
// =============================================================================

func TestTimeDurationParsing(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected time.Duration
		wantErr  bool
	}{
		{"1hour", "1h", 1 * time.Hour, false},
		{"30minutes", "30m", 30 * time.Minute, false},
		{"2hours", "2h", 2 * time.Hour, false},
		{"invalid", "invalid", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d, err := time.ParseDuration(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, tt.expected, d)
			}
		})
	}
}

// =============================================================================
// Context Timeout Integration Tests
// =============================================================================

func TestContextTimeoutWithCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Verify context is not done immediately
	select {
	case <-ctx.Done():
		t.Error("context should not be done yet")
	default:
		// Expected
	}

	// Cancel and verify
	cancel()

	select {
	case <-ctx.Done():
		// Expected
	default:
		t.Error("context should be done after cancel")
	}
}
