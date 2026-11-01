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
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

func iamTenantUserTestInitLogger(t *testing.T) func() {
	origLog := log
	logConfig := logger.LogConfig{
		Level:     "error",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	require.NoError(t, err)
	log = testLog
	return func() {
		log = origLog
	}
}

// iamCommandCase is one IAM tenant or user command run with the given
// arguments after setup has set its flags and globals.
type iamCommandCase struct {
	name        string
	newCommand  func() *cobra.Command
	run         func(*cobra.Command, []string) error
	args        []string
	setup       func(t *testing.T, cmd *cobra.Command)
	wantErrText string
}

// withIAMCommandGlobals installs factory as the IAM client factory and
// restores it, the logger and the package globals the commands read when
// the test ends.
func withIAMCommandGlobals(t *testing.T, factory func() (iam.Client, error)) {
	t.Helper()
	logCleanup := iamTenantUserTestInitLogger(t)
	origFactory := iamClientFactory
	origTenantID := globalTenantID
	origTenantName := tenantName
	origUserName := iamUserName
	origForce := iamForce
	origRole := iamUserRole
	origRoleUserID := iamRoleUserID
	t.Cleanup(func() {
		iamClientFactory = origFactory
		globalTenantID = origTenantID
		tenantName = origTenantName
		iamUserName = origUserName
		iamForce = origForce
		iamUserRole = origRole
		iamRoleUserID = origRoleUserID
		newIAMTenantProvisionCommand()
		logCleanup()
	})
	iamClientFactory = factory
}

func setIAMCommandFlag(name, value string) func(t *testing.T, cmd *cobra.Command) {
	return func(t *testing.T, cmd *cobra.Command) {
		require.NoError(t, cmd.Flags().Set(name, value))
	}
}

func setIAMUserTenant(tenantID string) func(t *testing.T, cmd *cobra.Command) {
	return func(t *testing.T, cmd *cobra.Command) {
		globalTenantID = tenantID
	}
}

func setIAMRoleBinding(tenantID string) func(t *testing.T, cmd *cobra.Command) {
	return func(t *testing.T, cmd *cobra.Command) {
		globalTenantID = tenantID
		iamUserRole = "admin"
		iamRoleUserID = "user_abc123"
	}
}

func runIAMCommandCases(t *testing.T, factory func() (iam.Client, error), cases []iamCommandCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withIAMCommandGlobals(t, factory)
			cmd := tc.newCommand()
			if tc.setup != nil {
				tc.setup(t, cmd)
			}

			err := tc.run(cmd, tc.args)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.wantErrText)
		})
	}
}

// TestIAMTenantUserCommands_FactoryError checks that every command returns
// the client factory's error once its arguments are valid.
func TestIAMTenantUserCommands_FactoryError(t *testing.T) {
	factory := func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}
	cases := []iamCommandCase{
		{
			name:       "tenant create",
			newCommand: newIAMTenantCreateCommand,
			run:        runIAMTenantCreateCommand,
			args:       []string{"test-tenant"},
			setup:      func(t *testing.T, cmd *cobra.Command) { tenantName = "Test Tenant" },
		},
		{
			name:       "tenant provision",
			newCommand: newIAMTenantProvisionCommand,
			run:        runIAMTenantProvisionCommand,
			args:       []string{"acme"},
			setup:      setIAMCommandFlag("admin-enrollment", "invite"),
		},
		{
			name:       "tenant get",
			newCommand: newIAMTenantGetCommand,
			run:        runIAMTenantGetCommand,
			args:       []string{"test-tenant"},
		},
		{
			name:       "tenant update",
			newCommand: newIAMTenantUpdateCommand,
			run:        runIAMTenantUpdateCommand,
			args:       []string{"test-tenant"},
			setup:      setIAMCommandFlag("name", "New Name"),
		},
		{
			name:       "tenant delete",
			newCommand: newIAMTenantDeleteCommand,
			run:        runIAMTenantDeleteCommand,
			args:       []string{"test-tenant"},
			setup:      func(t *testing.T, cmd *cobra.Command) { iamForce = true },
		},
		{
			name:       "tenant list",
			newCommand: newIAMTenantListCommand,
			run:        runIAMTenantListCommand,
			args:       []string{},
		},
		{
			name:       "user create",
			newCommand: newIAMUserCreateCommand,
			run:        runIAMUserCreateCommand,
			args:       []string{"testuser@example.com"},
			setup: func(t *testing.T, cmd *cobra.Command) {
				globalTenantID = "test-tenant"
				iamUserName = "Test User"
			},
		},
		{
			name:       "user get",
			newCommand: newIAMUserGetCommand,
			run:        runIAMUserGetCommand,
			args:       []string{"user_abc123"},
			setup:      setIAMUserTenant("test-tenant"),
		},
		{
			name:       "user update",
			newCommand: newIAMUserUpdateCommand,
			run:        runIAMUserUpdateCommand,
			args:       []string{"user_abc123"},
			setup: func(t *testing.T, cmd *cobra.Command) {
				setIAMCommandFlag("name", "New Name")(t, cmd)
				globalTenantID = "test-tenant"
			},
		},
		{
			name:       "user delete",
			newCommand: newIAMUserDeleteCommand,
			run:        runIAMUserDeleteCommand,
			args:       []string{"user_abc123"},
			setup: func(t *testing.T, cmd *cobra.Command) {
				globalTenantID = "test-tenant"
				iamForce = true
			},
		},
		{
			name:       "user list",
			newCommand: newIAMUserListCommand,
			run:        runIAMUserListCommand,
			args:       []string{},
			setup:      setIAMUserTenant("test-tenant"),
		},
		{
			name:       "user role assign",
			newCommand: newIAMUserRoleAssignCommand,
			run:        runIAMUserRoleAssignCommand,
			args:       []string{},
			setup:      setIAMRoleBinding("test-tenant"),
		},
		{
			name:       "user role revoke",
			newCommand: newIAMUserRoleRevokeCommand,
			run:        runIAMUserRoleRevokeCommand,
			args:       []string{},
			setup:      setIAMRoleBinding("test-tenant"),
		},
	}
	for i := range cases {
		cases[i].wantErrText = "connection failed"
	}
	runIAMCommandCases(t, factory, cases)
}

// TestIAMTenantUserCommands_InvalidArgs checks that every command rejects
// invalid arguments before it asks the factory for a client.
func TestIAMTenantUserCommands_InvalidArgs(t *testing.T) {
	factory := func() (iam.Client, error) {
		return nil, errors.New(errors.ErrInternal, "test", "factory called before validation")
	}
	runIAMCommandCases(t, factory, []iamCommandCase{
		{
			name:        "tenant create empty tenant ID",
			newCommand:  newIAMTenantCreateCommand,
			run:         runIAMTenantCreateCommand,
			args:        []string{""},
			wantErrText: "tenant ID is required",
		},
		{
			name:        "tenant provision empty tenant ID",
			newCommand:  newIAMTenantProvisionCommand,
			run:         runIAMTenantProvisionCommand,
			args:        []string{""},
			wantErrText: "tenant ID is required",
		},
		{
			name:        "tenant update no flags",
			newCommand:  newIAMTenantUpdateCommand,
			run:         runIAMTenantUpdateCommand,
			args:        []string{"test-tenant"},
			wantErrText: "at least one of",
		},
		{
			name:       "user create missing tenant",
			newCommand: newIAMUserCreateCommand,
			run:        runIAMUserCreateCommand,
			args:       []string{"testuser@example.com"},
			setup: func(t *testing.T, cmd *cobra.Command) {
				globalTenantID = ""
				iamUserName = "Test User"
			},
			wantErrText: "--tenant flag is required",
		},
		{
			name:        "user get missing tenant",
			newCommand:  newIAMUserGetCommand,
			run:         runIAMUserGetCommand,
			args:        []string{"user_abc123"},
			setup:       setIAMUserTenant(""),
			wantErrText: "--tenant flag is required",
		},
		{
			name:       "user update missing tenant",
			newCommand: newIAMUserUpdateCommand,
			run:        runIAMUserUpdateCommand,
			args:       []string{"user_abc123"},
			setup: func(t *testing.T, cmd *cobra.Command) {
				setIAMCommandFlag("name", "New Name")(t, cmd)
				globalTenantID = ""
			},
			wantErrText: "--tenant flag is required",
		},
		{
			name:        "user update no flags",
			newCommand:  newIAMUserUpdateCommand,
			run:         runIAMUserUpdateCommand,
			args:        []string{"user_abc123"},
			setup:       setIAMUserTenant("test-tenant"),
			wantErrText: "at least one of",
		},
		{
			name:       "user delete missing tenant",
			newCommand: newIAMUserDeleteCommand,
			run:        runIAMUserDeleteCommand,
			args:       []string{"user_abc123"},
			setup: func(t *testing.T, cmd *cobra.Command) {
				globalTenantID = ""
				iamForce = true
			},
			wantErrText: "--tenant flag is required",
		},
		{
			name:        "user list missing tenant",
			newCommand:  newIAMUserListCommand,
			run:         runIAMUserListCommand,
			args:        []string{},
			setup:       setIAMUserTenant(""),
			wantErrText: "--tenant flag is required",
		},
		{
			name:        "user role assign missing tenant",
			newCommand:  newIAMUserRoleAssignCommand,
			run:         runIAMUserRoleAssignCommand,
			args:        []string{},
			setup:       setIAMRoleBinding(""),
			wantErrText: "--tenant flag is required",
		},
		{
			name:        "user role revoke missing tenant",
			newCommand:  newIAMUserRoleRevokeCommand,
			run:         runIAMUserRoleRevokeCommand,
			args:        []string{},
			setup:       setIAMRoleBinding(""),
			wantErrText: "--tenant flag is required",
		},
	})
}

// withTenantProvisionState resets the provision flags to their defaults,
// installs the IAM mock client, and restores everything when the test ends.
func withTenantProvisionState(t *testing.T) (*testMockIAMClient, *cobra.Command) {
	t.Helper()
	logCleanup := iamTenantUserTestInitLogger(t)
	mockClient, mockCleanup := setupIAMMock(t)
	origFormat := iamOutputFormat
	t.Cleanup(func() {
		iamOutputFormat = origFormat
		tenantName = ""
		newIAMTenantProvisionCommand()
		mockCleanup()
		logCleanup()
	})
	iamOutputFormat = "table"
	tenantName = ""
	return mockClient, newIAMTenantProvisionCommand()
}

// writeTestCSR writes a CSR file the provision command can read.
func writeTestCSR(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "admin.csr")
	require.NoError(t, os.WriteFile(path, []byte(testAdminCSRPEM), 0o600))
	return path
}

const (
	testAdminCSRPEM  = "-----BEGIN CERTIFICATE REQUEST-----\nMIIB\n-----END CERTIFICATE REQUEST-----\n"
	testAdminCertPEM = "-----BEGIN CERTIFICATE-----\nMIIC\n-----END CERTIFICATE-----\n"
)

func TestIAMTenantProvisionCommand_CertModeMapsFlagsAndWritesCert(t *testing.T) {
	mockClient, cmd := withTenantProvisionState(t)
	certOut := filepath.Join(t.TempDir(), "admin.crt")
	require.NoError(t, cmd.Flags().Set("name", "Acme Corp"))
	require.NoError(t, cmd.Flags().Set("isolation-type", "volume"))
	require.NoError(t, cmd.Flags().Set("admin-username", "root"))
	require.NoError(t, cmd.Flags().Set("admin-csr", writeTestCSR(t)))
	require.NoError(t, cmd.Flags().Set("admin-cert-out", certOut))

	var got *iam.ProvisionTenantRequest
	mockClient.tenantClient.ProvisionFunc = func(_ context.Context, req *iam.ProvisionTenantRequest) (*iam.ProvisionTenantResponse, error) {
		got = req
		return &iam.ProvisionTenantResponse{
			TenantID:            req.TenantID,
			AdminUsername:       req.AdminUsername,
			AdminCertPEM:        testAdminCertPEM,
			AdminCertThumbprint: "thumb",
		}, nil
	}

	out, err := captureRoleStdout(t, func() error {
		return runIAMTenantProvisionCommand(cmd, []string{"acme"})
	})
	require.NoError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, &iam.ProvisionTenantRequest{
		TenantID:        "acme",
		Name:            "Acme Corp",
		IsolationType:   iam.TenantIsolationVolume,
		AdminUsername:   "root",
		AdminEnrollment: iam.TenantAdminEnrollCert,
		AdminCSRPEM:     testAdminCSRPEM,
	}, got)

	written, err := os.ReadFile(certOut)
	require.NoError(t, err)
	assert.Equal(t, testAdminCertPEM, string(written))
	info, err := os.Stat(certOut)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())

	assert.Contains(t, out, "written to "+certOut)
	assert.Contains(t, out, "thumb")
	assert.NotContains(t, out, "BEGIN CERTIFICATE-----", "the certificate goes to the file, not stdout")
}

func TestIAMTenantProvisionCommand_CertOutTightensExistingFileMode(t *testing.T) {
	mockClient, cmd := withTenantProvisionState(t)
	certOut := filepath.Join(t.TempDir(), "admin.crt")
	require.NoError(t, os.WriteFile(certOut, []byte("stale"), 0o644))
	require.NoError(t, cmd.Flags().Set("admin-csr", writeTestCSR(t)))
	require.NoError(t, cmd.Flags().Set("admin-cert-out", certOut))
	mockClient.tenantClient.ProvisionFunc = func(_ context.Context, req *iam.ProvisionTenantRequest) (*iam.ProvisionTenantResponse, error) {
		return &iam.ProvisionTenantResponse{TenantID: req.TenantID, AdminCertPEM: testAdminCertPEM}, nil
	}

	_, err := captureRoleStdout(t, func() error {
		return runIAMTenantProvisionCommand(cmd, []string{"acme"})
	})
	require.NoError(t, err)

	info, err := os.Stat(certOut)
	require.NoError(t, err)
	assert.Equal(t, os.FileMode(0o600), info.Mode().Perm())
}

func TestIAMTenantProvisionCommand_CertModePrintsCertWithoutOutPath(t *testing.T) {
	mockClient, cmd := withTenantProvisionState(t)
	require.NoError(t, cmd.Flags().Set("admin-csr", writeTestCSR(t)))
	mockClient.tenantClient.ProvisionFunc = func(_ context.Context, req *iam.ProvisionTenantRequest) (*iam.ProvisionTenantResponse, error) {
		return &iam.ProvisionTenantResponse{TenantID: req.TenantID, AdminCertPEM: testAdminCertPEM}, nil
	}

	out, err := captureRoleStdout(t, func() error {
		return runIAMTenantProvisionCommand(cmd, []string{"acme"})
	})
	require.NoError(t, err)
	assert.Contains(t, out, testAdminCertPEM)
}

func TestIAMTenantProvisionCommand_CertOutMissingCertFromServer(t *testing.T) {
	mockClient, cmd := withTenantProvisionState(t)
	certOut := filepath.Join(t.TempDir(), "admin.crt")
	require.NoError(t, cmd.Flags().Set("admin-csr", writeTestCSR(t)))
	require.NoError(t, cmd.Flags().Set("admin-cert-out", certOut))
	mockClient.tenantClient.ProvisionFunc = func(_ context.Context, req *iam.ProvisionTenantRequest) (*iam.ProvisionTenantResponse, error) {
		return &iam.ProvisionTenantResponse{TenantID: req.TenantID}, nil
	}

	err := runIAMTenantProvisionCommand(cmd, []string{"acme"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no admin certificate")
	_, statErr := os.Stat(certOut)
	assert.True(t, os.IsNotExist(statErr))
}

func TestIAMTenantProvisionCommand_CertOutWriteError(t *testing.T) {
	mockClient, cmd := withTenantProvisionState(t)
	certOut := filepath.Join(t.TempDir(), "missing-dir", "admin.crt")
	require.NoError(t, cmd.Flags().Set("admin-csr", writeTestCSR(t)))
	require.NoError(t, cmd.Flags().Set("admin-cert-out", certOut))
	mockClient.tenantClient.ProvisionFunc = func(_ context.Context, req *iam.ProvisionTenantRequest) (*iam.ProvisionTenantResponse, error) {
		return &iam.ProvisionTenantResponse{TenantID: req.TenantID, AdminCertPEM: testAdminCertPEM}, nil
	}

	err := runIAMTenantProvisionCommand(cmd, []string{"acme"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to write admin certificate")
}

func TestIAMTenantProvisionCommand_InviteModeMapsFlagsAndPrintsToken(t *testing.T) {
	mockClient, cmd := withTenantProvisionState(t)
	require.NoError(t, cmd.Flags().Set("admin-enrollment", "invite"))
	require.NoError(t, cmd.Flags().Set("admin-username", "alice"))
	require.NoError(t, cmd.Flags().Set("invite-expiry-hours", "24"))

	expires := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	var got *iam.ProvisionTenantRequest
	mockClient.tenantClient.ProvisionFunc = func(_ context.Context, req *iam.ProvisionTenantRequest) (*iam.ProvisionTenantResponse, error) {
		got = req
		return &iam.ProvisionTenantResponse{
			TenantID:        req.TenantID,
			AdminUsername:   req.AdminUsername,
			AdminPending:    true,
			InviteToken:     "tok-123",
			InviteExpiresAt: &expires,
		}, nil
	}

	out, err := captureRoleStdout(t, func() error {
		return runIAMTenantProvisionCommand(cmd, []string{"acme"})
	})
	require.NoError(t, err)

	require.NotNil(t, got)
	assert.Equal(t, &iam.ProvisionTenantRequest{
		TenantID:          "acme",
		AdminUsername:     "alice",
		AdminEnrollment:   iam.TenantAdminEnrollInvite,
		InviteExpiryHours: 24,
	}, got)
	assert.Contains(t, out, "Invite Token: tok-123")
	assert.Contains(t, out, "2026-10-13 12:00:00")
	assert.Contains(t, out, "single-use")
	assert.Contains(t, out, "Admin Pending: true")
}

func TestIAMTenantProvisionCommand_JSONOutput(t *testing.T) {
	mockClient, cmd := withTenantProvisionState(t)
	iamOutputFormat = "json"
	require.NoError(t, cmd.Flags().Set("admin-enrollment", "invite"))
	mockClient.tenantClient.ProvisionFunc = func(_ context.Context, req *iam.ProvisionTenantRequest) (*iam.ProvisionTenantResponse, error) {
		return &iam.ProvisionTenantResponse{TenantID: req.TenantID, InviteToken: "tok-123"}, nil
	}

	out, err := captureRoleStdout(t, func() error {
		return runIAMTenantProvisionCommand(cmd, []string{"acme"})
	})
	require.NoError(t, err)
	assert.Contains(t, out, `"tenant_id": "acme"`)
	assert.Contains(t, out, `"invite_token": "tok-123"`)
}

func TestIAMTenantProvisionCommand_CSRReadError(t *testing.T) {
	mockClient, cmd := withTenantProvisionState(t)
	require.NoError(t, cmd.Flags().Set("admin-csr", filepath.Join(t.TempDir(), "absent.csr")))
	called := false
	mockClient.tenantClient.ProvisionFunc = func(_ context.Context, _ *iam.ProvisionTenantRequest) (*iam.ProvisionTenantResponse, error) {
		called = true
		return nil, nil
	}

	err := runIAMTenantProvisionCommand(cmd, []string{"acme"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read admin CSR file")
	assert.False(t, called, "a CSR read failure must not reach the server")
}

func TestIAMTenantProvisionCommand_FlagConflicts(t *testing.T) {
	tests := []struct {
		name    string
		flags   map[string]string
		withCSR bool
		wantMsg string
	}{
		{"cert without csr", nil, false, "--admin-csr is required"},
		{"cert with invite expiry", map[string]string{"invite-expiry-hours": "24"}, true, "--invite-expiry-hours applies to invite"},
		{"invite with csr", map[string]string{"admin-enrollment": "invite"}, true, "--admin-csr applies to cert"},
		{"invite with cert out", map[string]string{"admin-enrollment": "invite", "admin-cert-out": "admin.crt"}, false, "--admin-cert-out applies to cert"},
		{"unknown enrollment", map[string]string{"admin-enrollment": "password"}, false, "--admin-enrollment must be"},
		{"negative invite expiry", map[string]string{"admin-enrollment": "invite", "invite-expiry-hours": "-1"}, false, "must not be negative"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient, cmd := withTenantProvisionState(t)
			for flag, value := range tt.flags {
				require.NoError(t, cmd.Flags().Set(flag, value))
			}
			if tt.withCSR {
				require.NoError(t, cmd.Flags().Set("admin-csr", writeTestCSR(t)))
			}
			called := false
			mockClient.tenantClient.ProvisionFunc = func(_ context.Context, _ *iam.ProvisionTenantRequest) (*iam.ProvisionTenantResponse, error) {
				called = true
				return nil, nil
			}

			err := runIAMTenantProvisionCommand(cmd, []string{"acme"})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantMsg)
			assert.False(t, called, "a flag conflict must not reach the server")
		})
	}
}

func TestIAMTenantProvisionCommand_ServerError(t *testing.T) {
	mockClient, cmd := withTenantProvisionState(t)
	require.NoError(t, cmd.Flags().Set("admin-csr", writeTestCSR(t)))
	mockClient.tenantClient.ProvisionFunc = func(_ context.Context, _ *iam.ProvisionTenantRequest) (*iam.ProvisionTenantResponse, error) {
		return nil, errors.New(errors.ErrConflict, "iam", "tenant already exists")
	}

	err := runIAMTenantProvisionCommand(cmd, []string{"acme"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "tenant already exists")
}

// =============================================================================
// IAM Tenant Command Structure Tests
// =============================================================================

func TestNewIAMTenantCommand_FullStructure(t *testing.T) {
	cmd := newIAMTenantCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "tenant", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify subcommands exist
	expectedSubcmds := []string{"create", "provision", "get", "update", "delete", "list"}
	for _, expected := range expectedSubcmds {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Name() == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected subcommand %s not found", expected)
	}
}

func TestNewIAMTenantCreateCommand_Flags(t *testing.T) {
	cmd := newIAMTenantCreateCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "create")

	// Check required flags exist
	assert.NotNil(t, cmd.Flags().Lookup("name"))
	assert.NotNil(t, cmd.Flags().Lookup("max-users"))
	assert.NotNil(t, cmd.Flags().Lookup("mfa-required"))
}

func TestNewIAMTenantUpdateCommand_Flags(t *testing.T) {
	cmd := newIAMTenantUpdateCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "update")

	// Check flags exist
	assert.NotNil(t, cmd.Flags().Lookup("name"))
	assert.NotNil(t, cmd.Flags().Lookup("max-users"))
	assert.NotNil(t, cmd.Flags().Lookup("mfa-required"))
}

// =============================================================================
// IAM User Command Structure Tests
// =============================================================================

func TestNewIAMUserCommand_FullStructure(t *testing.T) {
	cmd := newIAMUserCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "user", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify subcommands exist
	expectedSubcmds := []string{"create", "get", "update", "delete", "list", "role"}
	for _, expected := range expectedSubcmds {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Name() == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected subcommand %s not found", expected)
	}
}

func TestNewIAMUserCreateCommand_Flags(t *testing.T) {
	cmd := newIAMUserCreateCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "create")

	// Check flags exist. QuicBaaS is passwordless — no --password flag.
	assert.NotNil(t, cmd.Flags().Lookup("name"))
	assert.NotNil(t, cmd.Flags().Lookup("roles"))
	assert.NotNil(t, cmd.Flags().Lookup("role"))
	assert.Nil(t, cmd.Flags().Lookup("groups"), "--groups must not exist; an account carries no group membership (go-quicbaas#1027)")
	assert.Nil(t, cmd.Flags().Lookup("password"), "--password flag must not exist; QuicBaaS uses invite-based FIDO2/PIV activation")
}

func TestNewIAMUserUpdateCommand_Flags(t *testing.T) {
	cmd := newIAMUserUpdateCommand()
	require.NotNil(t, cmd)
	assert.Contains(t, cmd.Use, "update")

	// Check flags exist
	assert.NotNil(t, cmd.Flags().Lookup("name"))
	assert.NotNil(t, cmd.Flags().Lookup("roles"))
}

func TestNewIAMUserListCommand_Flags(t *testing.T) {
	cmd := newIAMUserListCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "list", cmd.Use)

	// Check filter flags exist
	assert.NotNil(t, cmd.Flags().Lookup("role"))
	// quicbaas keeps no groups, so there is no group filter.
	assert.Nil(t, cmd.Flags().Lookup("group"))
}

func TestNewIAMUserRoleCommand_Structure(t *testing.T) {
	cmd := newIAMUserRoleCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "role", cmd.Use)
	assert.NotEmpty(t, cmd.Short)

	// Verify subcommands exist
	expectedSubcmds := []string{"assign", "revoke"}
	for _, expected := range expectedSubcmds {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Name() == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected subcommand %s not found", expected)
	}
}

func TestNewIAMUserRoleAssignCommand_Flags(t *testing.T) {
	cmd := newIAMUserRoleAssignCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "assign", cmd.Use)

	// Check required flags exist
	assert.NotNil(t, cmd.Flags().Lookup("role"))
	assert.NotNil(t, cmd.Flags().Lookup("user"))
}

func TestNewIAMUserRoleRevokeCommand_Flags(t *testing.T) {
	cmd := newIAMUserRoleRevokeCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "revoke", cmd.Use)

	// Check required flags exist
	assert.NotNil(t, cmd.Flags().Lookup("role"))
	assert.NotNil(t, cmd.Flags().Lookup("user"))
}
