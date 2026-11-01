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
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/cli"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

func TestNewIAMCommand(t *testing.T) {
	cmd := newIAMCommand()

	assert.NotNil(t, cmd)
	assert.Equal(t, "iam", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify subcommands are registered
	subcommands := cmd.Commands()
	expectedSubcommands := []string{"tenant", "user", "role", "app", "policy", "mfa", "webauthn", "session", "pep", "audit", "admin", "gdpr", "invite", "provider", "identity"}

	assert.Len(t, subcommands, len(expectedSubcommands))

	for _, expected := range expectedSubcommands {
		found := false
		for _, sub := range subcommands {
			if sub.Use == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected subcommand '%s' not found", expected)
	}
}

// TestIAMCommandFlags tests that IAM command has the expected persistent flags.
func TestIAMCommandFlags(t *testing.T) {
	cmd := newIAMCommand()

	// Check for expected flags
	assert.NotNil(t, cmd.PersistentFlags().Lookup("output"))
	assert.NotNil(t, cmd.PersistentFlags().Lookup("yes"))
}

// TestIAMTenantIDRequired tests that tenant ID is required for tenant-scoped commands.
func TestIAMTenantIDRequired(t *testing.T) {
	// Save original factory and restore after test
	originalFactory := iamClientFactory
	defer func() {
		iamClientFactory = originalFactory
	}()

	// Replace factory with mock
	mockClient := newTestMockIAMClient()
	iamClientFactory = func() (iam.Client, error) {
		return mockClient, nil
	}

	tests := []struct {
		name        string
		commandFunc func() *cobra.Command
		args        []string
		expectError bool
	}{
		{
			name:        "user list without tenant",
			commandFunc: newIAMUserListCommand,
			args:        []string{},
			expectError: true,
		},
		{
			name:        "role list without tenant",
			commandFunc: newIAMRoleListCommand,
			args:        []string{},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.commandFunc()
			cmd.SetArgs(tt.args)

			// Don't set the tenant flag
			globalTenantID = ""

			err := cmd.Execute()
			if tt.expectError {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "tenant")
			}
		})
	}
}

// BenchmarkIAMCommandCreation benchmarks IAM command creation.
func BenchmarkIAMCommandCreation(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = newIAMCommand()
	}
}

// BenchmarkIAMSubcommandLookup benchmarks subcommand lookup.
func BenchmarkIAMSubcommandLookup(b *testing.B) {
	cmd := newIAMCommand()
	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = cmd.Commands()
	}
}

// TestIAMErrorHandling tests error handling in IAM commands.
func TestIAMErrorHandling(t *testing.T) {
	// Save original factory and restore after test
	originalFactory := iamClientFactory
	defer func() {
		iamClientFactory = originalFactory
	}()

	// Setup mock client that returns an error
	iamClientFactory = func() (iam.Client, error) {
		return nil, assert.AnError
	}

	cmd := newIAMTenantListCommand()
	err := cmd.Execute()

	require.Error(t, err)
	assert.Equal(t, assert.AnError, err)
}

// =============================================================================
// Session Command Tests
// =============================================================================

// commandHasPrefix checks if a subcommand's Use field starts with the expected name.
func commandHasPrefix(subcommands []*cobra.Command, expected string) bool {
	for _, sub := range subcommands {
		if sub.Use == expected || strings.HasPrefix(sub.Use, expected+" ") {
			return true
		}
	}
	return false
}

// TestNewIAMSessionCommand tests the session command structure.
func TestNewIAMSessionCommand(t *testing.T) {
	cmd := newIAMSessionCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "session", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify subcommands: list, get, revoke, revoke-all
	subcommands := cmd.Commands()
	expectedSubs := []string{"list", "get", "revoke", "revoke-all"}
	assert.GreaterOrEqual(t, len(subcommands), len(expectedSubs))

	for _, expected := range expectedSubs {
		assert.True(t, commandHasPrefix(subcommands, expected),
			"Expected subcommand '%s' not found in session command", expected)
	}
}

// TestIAMSessionListWithMock tests the session list command with a mock client.
func TestIAMSessionListWithMock(t *testing.T) {
	cleanup := testInitLogger(t)
	defer cleanup()

	originalFactory := iamClientFactory
	originalTenantID := globalTenantID
	originalOutputFormat := iamOutputFormat
	defer func() {
		iamClientFactory = originalFactory
		globalTenantID = originalTenantID
		iamOutputFormat = originalOutputFormat
	}()

	now := time.Now()
	mockClient := newTestMockIAMClient()
	mockClient.sessionClient.ListFunc = func(ctx context.Context, req *iam.ListSessionsRequest) (*iam.ListSessionsResponse, error) {
		return &iam.ListSessionsResponse{
			Sessions: []iam.Session{
				{
					ID:          "sess-123",
					UserID:      "user-456",
					TenantID:    req.TenantID,
					Created:     now,
					LastActive:  now,
					ExpiresAt:   now.Add(24 * time.Hour),
					MFAVerified: true,
					IPAddress:   "192.168.1.1",
					UserAgent:   "Mozilla/5.0",
				},
			},
			TotalCount: 1,
		}, nil
	}

	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }
	globalTenantID = "test-tenant"
	iamOutputFormat = "table"
	cmd := newIAMSessionListCommand()
	sessionUserID = "user-456"
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err := cmd.Execute()
	assert.NoError(t, err)
}

// TestIAMSessionRevokeWithMock tests the session revoke command with a mock client.
func TestIAMSessionRevokeWithMock(t *testing.T) {
	cleanup := testInitLogger(t)
	defer cleanup()

	originalFactory := iamClientFactory
	originalTenantID := globalTenantID
	originalForce := iamForce
	defer func() {
		iamClientFactory = originalFactory
		globalTenantID = originalTenantID
		iamForce = originalForce
	}()

	revokedSessionID := ""
	mockClient := newTestMockIAMClient()
	mockClient.sessionClient.RevokeFunc = func(ctx context.Context, sessionID string) error {
		revokedSessionID = sessionID
		return nil
	}

	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }
	globalTenantID = "test-tenant"
	iamForce = true // Skip confirmation

	cmd := newIAMSessionRevokeCommand()
	cmd.SetArgs([]string{"sess-123"})
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err := cmd.Execute()
	assert.NoError(t, err)
	assert.Equal(t, "sess-123", revokedSessionID)
}

// TestIAMSessionRevokeAllWithMock tests the session revoke-all command with a mock client.
func TestIAMSessionRevokeAllWithMock(t *testing.T) {
	cleanup := testInitLogger(t)
	defer cleanup()

	originalFactory := iamClientFactory
	originalTenantID := globalTenantID
	originalForce := iamForce
	originalSessionUserID := sessionUserID
	defer func() {
		iamClientFactory = originalFactory
		globalTenantID = originalTenantID
		iamForce = originalForce
		sessionUserID = originalSessionUserID
	}()

	revokedTenantID := ""
	revokedUserID := ""
	mockClient := newTestMockIAMClient()
	mockClient.sessionClient.RevokeAllFunc = func(ctx context.Context, tenantID, userID string) error {
		revokedTenantID = tenantID
		revokedUserID = userID
		return nil
	}

	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }
	globalTenantID = "test-tenant"
	iamForce = true // Skip confirmation
	cmd := newIAMSessionRevokeAllCommand()
	sessionUserID = "test-user"
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err := cmd.Execute()
	assert.NoError(t, err)
	assert.Equal(t, "test-tenant", revokedTenantID)
	assert.Equal(t, "test-user", revokedUserID)
}

// =============================================================================
// Audit Command Tests
// =============================================================================

// TestNewIAMAuditCommand tests the audit command structure.
func TestNewIAMAuditCommand(t *testing.T) {
	cmd := newIAMAuditCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "audit", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	// Verify subcommands: query, get
	subcommands := cmd.Commands()
	assert.GreaterOrEqual(t, len(subcommands), 2)

	expectedSubs := []string{"query", "get"}
	for _, expected := range expectedSubs {
		assert.True(t, commandHasPrefix(subcommands, expected),
			"Expected subcommand '%s' not found in audit command", expected)
	}
}

// TestIAMAuditQueryJSONFormat tests audit query with JSON output.
func TestIAMAuditQueryJSONFormat(t *testing.T) {
	cleanup := testInitLogger(t)
	defer cleanup()

	originalFactory := iamClientFactory
	originalTenantID := globalTenantID
	originalOutputFormat := iamOutputFormat
	defer func() {
		iamClientFactory = originalFactory
		globalTenantID = originalTenantID
		iamOutputFormat = originalOutputFormat
	}()

	now := time.Now()
	mockClient := newTestMockIAMClient()
	mockClient.auditClient.QueryFunc = func(ctx context.Context, filter *iam.AuditFilter) (*iam.AuditQueryResponse, error) {
		return &iam.AuditQueryResponse{
			Events: []iam.AuditEvent{
				{
					ID:        "evt-123",
					TenantID:  filter.TenantID,
					EventType: iam.AuditEventTypeAuthentication,
					Action:    "user.login",
					Outcome:   iam.AuditOutcomeSuccess,
					Timestamp: now,
				},
			},
			TotalCount: 1,
		}, nil
	}

	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }
	globalTenantID = "test-tenant"
	iamOutputFormat = "json"

	cmd := newIAMAuditQueryCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err := cmd.Execute()
	assert.NoError(t, err)
}

// TestIAMAuditGetJSONFormat tests audit get with JSON output.
func TestIAMAuditGetJSONFormat(t *testing.T) {
	cleanup := testInitLogger(t)
	defer cleanup()

	originalFactory := iamClientFactory
	originalTenantID := globalTenantID
	originalOutputFormat := iamOutputFormat
	defer func() {
		iamClientFactory = originalFactory
		globalTenantID = originalTenantID
		iamOutputFormat = originalOutputFormat
	}()

	now := time.Now()
	mockClient := newTestMockIAMClient()
	mockClient.auditClient.GetFunc = func(ctx context.Context, tenantID, eventID string) (*iam.AuditEvent, error) {
		return &iam.AuditEvent{
			ID:        eventID,
			TenantID:  tenantID,
			EventType: iam.AuditEventTypeAuthentication,
			Action:    "user.login",
			Outcome:   iam.AuditOutcomeSuccess,
			Timestamp: now,
		}, nil
	}

	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }
	globalTenantID = "test-tenant"
	iamOutputFormat = "json"

	cmd := newIAMAuditGetCommand()
	cmd.SetArgs([]string{"evt-123"})
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err := cmd.Execute()
	assert.NoError(t, err)
}

// =============================================================================
// PEP Command Tests
// =============================================================================

// TestNewIAMPEPCommand tests the PEP command structure.
func TestNewIAMPEPCommand(t *testing.T) {
	cmd := newIAMPEPCommand()
	assert.NotNil(t, cmd)
	assert.Equal(t, "pep", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
}

// TestIAMPEPCheckWithMock tests the PEP check command with a mock client.
func TestIAMPEPCheckWithMock(t *testing.T) {
	cleanup := testInitLogger(t)
	defer cleanup()

	originalFactory := iamClientFactory
	originalTenantID := globalTenantID
	originalOutputFormat := iamOutputFormat
	originalPepSubject := pepSubject
	originalPepAction := pepAction
	originalPepResource := pepResource
	defer func() {
		iamClientFactory = originalFactory
		globalTenantID = originalTenantID
		iamOutputFormat = originalOutputFormat
		pepSubject = originalPepSubject
		pepAction = originalPepAction
		pepResource = originalPepResource
	}()

	mockClient := newTestMockIAMClient()
	mockClient.pepClient.CheckFunc = func(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
		return &iam.CheckResponse{
			Allowed:          true,
			MatchedPolicy:    "admin-policy",
			MatchedRule:      "allow-all",
			EvaluationTimeNs: 150000,
		}, nil
	}

	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }
	globalTenantID = "test-tenant"
	iamOutputFormat = "table"

	// Define values as constants BEFORE creating command
	// because Cobra's StringVar sets the global to default when the flag is defined
	testSubject := `{"id":"user1","roles":["admin"]}`
	testAction := "read"
	testResource := "keys/foo"

	cmd := newIAMPEPCheckCommand()
	cmd.SetArgs([]string{"--subject", testSubject, "--action", testAction, "--resource", testResource})
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err := cmd.Execute()
	assert.NoError(t, err)
}

// TestIAMPEPCheckJSONFormat tests PEP check with JSON output.
func TestIAMPEPCheckJSONFormat(t *testing.T) {
	cleanup := testInitLogger(t)
	defer cleanup()

	originalFactory := iamClientFactory
	originalTenantID := globalTenantID
	originalOutputFormat := iamOutputFormat
	originalPepSubject := pepSubject
	originalPepAction := pepAction
	originalPepResource := pepResource
	defer func() {
		iamClientFactory = originalFactory
		globalTenantID = originalTenantID
		iamOutputFormat = originalOutputFormat
		pepSubject = originalPepSubject
		pepAction = originalPepAction
		pepResource = originalPepResource
	}()

	mockClient := newTestMockIAMClient()
	mockClient.pepClient.CheckFunc = func(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
		return &iam.CheckResponse{
			Allowed:          false,
			EvaluationTimeNs: 100000,
			Reason:           "No matching policy found",
		}, nil
	}

	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }
	globalTenantID = "test-tenant"
	iamOutputFormat = "json"

	testSubject := `{"id":"user1","roles":["viewer"]}`
	testAction := "write"
	testResource := "keys/bar"

	cmd := newIAMPEPCheckCommand()
	cmd.SetArgs([]string{"--subject", testSubject, "--action", testAction, "--resource", testResource})
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)

	err := cmd.Execute()
	assert.NoError(t, err)
}

// =============================================================================
// Error Handling Tests
// =============================================================================

// TestIAMCommands_ClientError checks that an error from the IAM client is
// returned by the session, audit and PEP commands.
func TestIAMCommands_ClientError(t *testing.T) {
	cleanup := testInitLogger(t)
	defer cleanup()

	originalFactory := iamClientFactory
	originalTenantID := globalTenantID
	originalSessionUserID := sessionUserID
	originalPepSubject := pepSubject
	originalPepAction := pepAction
	originalPepResource := pepResource
	originalPepContext := pepContext
	defer func() {
		iamClientFactory = originalFactory
		globalTenantID = originalTenantID
		sessionUserID = originalSessionUserID
		pepSubject = originalPepSubject
		pepAction = originalPepAction
		pepResource = originalPepResource
		pepContext = originalPepContext
	}()

	tests := []struct {
		name      string
		setupMock func(m *testMockIAMClient)
		cmd       func() *cobra.Command
		args      []string
	}{
		{
			name: "session list",
			setupMock: func(m *testMockIAMClient) {
				m.sessionClient.ListFunc = func(ctx context.Context, req *iam.ListSessionsRequest) (*iam.ListSessionsResponse, error) {
					return nil, assert.AnError
				}
			},
			cmd:  newIAMSessionListCommand,
			args: []string{"--user", "test-user"},
		},
		{
			name: "audit query",
			setupMock: func(m *testMockIAMClient) {
				m.auditClient.QueryFunc = func(ctx context.Context, filter *iam.AuditFilter) (*iam.AuditQueryResponse, error) {
					return nil, assert.AnError
				}
			},
			cmd: newIAMAuditQueryCommand,
		},
		{
			name: "pep check",
			setupMock: func(m *testMockIAMClient) {
				m.pepClient.CheckFunc = func(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
					return nil, assert.AnError
				}
			},
			cmd:  newIAMPEPCheckCommand,
			args: []string{"--subject", `{"id":"user1"}`, "--action", "read", "--resource", "keys/foo"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockClient := newTestMockIAMClient()
			tt.setupMock(mockClient)
			iamClientFactory = func() (iam.Client, error) { return mockClient, nil }
			globalTenantID = "test-tenant"

			cmd := tt.cmd()
			// A non-nil slice keeps cobra from parsing the test binary's own flags.
			cmd.SetArgs(append([]string{}, tt.args...))

			err := cmd.Execute()
			assert.ErrorContains(t, err, assert.AnError.Error())
		})
	}
}

// TestTruncateString tests the truncateString helper function.

// =============================================================================
// Benchmarks
// =============================================================================

// BenchmarkIAMSessionCommandCreation benchmarks session command creation.
func BenchmarkIAMSessionCommandCreation(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = newIAMSessionCommand()
	}
}

// BenchmarkIAMAuditCommandCreation benchmarks audit command creation.
func BenchmarkIAMAuditCommandCreation(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = newIAMAuditCommand()
	}
}

// BenchmarkIAMPEPCommandCreation benchmarks PEP command creation.
func BenchmarkIAMPEPCommandCreation(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = newIAMPEPCommand()
	}
}

func testInitLogger(t *testing.T) func() {
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

// TestIAMTestMain initializes the test environment for IAM tests.
func init() {
	// Initialize logger for tests
	logConfig := logger.LogConfig{
		Level:     "error",
		Output:    "stderr",
		Component: "test",
	}
	testLog, err := logger.New(logConfig)
	if err != nil {
		panic(err)
	}
	log = testLog
}

// =============================================================================
// IAM Command Tests - Factory Error Paths
// =============================================================================

// TestIAMCommands_FactoryError runs every IAM command with a client factory
// that fails and checks that the factory error reaches the caller.
func TestIAMCommands_FactoryError(t *testing.T) {
	cleanup := testInitLogger(t)
	defer cleanup()

	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	tests := []struct {
		name   string
		newCmd func() *cobra.Command
		run    func(*cobra.Command, []string) error
		args   []string
		// setup sets the flag variables after the command is built,
		// because building a command resets them to their defaults.
		setup func()
	}{
		// App
		{"app register", newIAMAppRegisterCommand, runIAMAppRegisterCommand, nil, func() {
			iamAppName = "test-app"
			globalTenantID = "tenant_test"
		}},
		{"app get", newIAMAppGetCommand, runIAMAppGetCommand, []string{"app_test123"}, nil},
		{"app update", newIAMAppUpdateCommand, runIAMAppUpdateCommand, []string{"app_test123"}, nil},
		{"app delete", newIAMAppDeleteCommand, runIAMAppDeleteCommand, []string{"app_test123"}, func() {
			iamForce = true
		}},
		{"app list", newIAMAppListCommand, runIAMAppListCommand, nil, func() {
			globalTenantID = "tenant_test"
		}},
		{"app rotate-secret", newIAMAppRotateSecretCommand, runIAMAppRotateSecretCommand, []string{"app_test123"}, nil},

		// Audit
		{"audit query", newIAMAuditQueryCommand, runIAMAuditQueryCommand, nil, func() {
			globalTenantID = "tenant_test"
		}},
		{"audit get", newIAMAuditGetCommand, runIAMAuditGetCommand, []string{"event_test123"}, nil},

		// GDPR
		{"gdpr consent grant", newIAMGDPRConsentGrantCommand, runIAMGDPRConsentGrantCommand, nil, func() {
			gdprUserID = "user_test123"
			gdprPurpose = "marketing"
			globalTenantID = "tenant_test"
		}},
		{"gdpr consent withdraw", newIAMGDPRConsentWithdrawCommand, runIAMGDPRConsentWithdrawCommand, nil, func() {
			gdprUserID = "user_test123"
			gdprPurpose = "marketing"
			globalTenantID = "tenant_test"
			iamForce = true
		}},
		{"gdpr consent get", newIAMGDPRConsentGetCommand, runIAMGDPRConsentGetCommand, nil, func() {
			gdprUserID = "user_test123"
			gdprPurpose = "marketing"
			globalTenantID = "tenant_test"
		}},
		{"gdpr consent list", newIAMGDPRConsentListCommand, runIAMGDPRConsentListCommand, nil, func() {
			gdprUserID = "user_test123"
			globalTenantID = "tenant_test"
		}},
		{"gdpr export request", newIAMGDPRExportRequestCommand, runIAMGDPRExportRequestCommand, nil, func() {
			gdprUserID = "user_test123"
			globalTenantID = "tenant_test"
		}},
		{"gdpr export status", newIAMGDPRExportStatusCommand, runIAMGDPRExportStatusCommand, nil, func() {
			gdprExportID = "req_test123"
			globalTenantID = "tenant_test"
		}},
		{"gdpr deletion request", newIAMGDPRDeletionRequestCommand, runIAMGDPRDeletionRequestCommand, nil, func() {
			gdprUserID = "user_test123"
			globalTenantID = "tenant_test"
			iamForce = true
		}},
		{"gdpr deletion cancel", newIAMGDPRDeletionCancelCommand, runIAMGDPRDeletionCancelCommand, nil, func() {
			gdprDeletionID = "req_test123"
			globalTenantID = "tenant_test"
		}},
		{"gdpr deletion status", newIAMGDPRDeletionStatusCommand, runIAMGDPRDeletionStatusCommand, nil, func() {
			gdprDeletionID = "req_test123"
			globalTenantID = "tenant_test"
		}},

		// Role
		{"role create", newIAMRoleCreateCommand, runIAMRoleCreateCommand, []string{"role_test"}, func() {
			iamRoleName = "test-role"
			globalTenantID = "tenant_test"
		}},
		{"role get", newIAMRoleGetCommand, runIAMRoleGetCommand, []string{"role_test123"}, func() {
			iamRoleID = "role_test123"
		}},
		{"role update", newIAMRoleUpdateCommand, runIAMRoleUpdateCommand, []string{"role_test123"}, func() {
			iamRoleID = "role_test123"
		}},
		{"role delete", newIAMRoleDeleteCommand, runIAMRoleDeleteCommand, []string{"role_test123"}, func() {
			iamRoleID = "role_test123"
		}},
		{"role list", newIAMRoleListCommand, runIAMRoleListCommand, nil, func() {
			globalTenantID = "tenant_test"
		}},
		{"role assign", newIAMRoleAssignCommand, runIAMRoleAssignCommand, []string{"role_test123", "user_test123"}, func() {
			iamRoleID = "role_test123"
			iamRoleUserID = "user_test123"
		}},
		{"role revoke", newIAMRoleRevokeCommand, runIAMRoleRevokeCommand, []string{"role_test123", "user_test123"}, func() {
			iamRoleID = "role_test123"
			iamRoleUserID = "user_test123"
		}},

		// Policy
		{"policy apply", newIAMPolicyApplyCommand, runIAMPolicyApplyCommand, nil, func() {
			globalTenantID = "tenant_test"
		}},
		{"policy get", newIAMPolicyGetCommand, runIAMPolicyGetCommand, []string{"policy_test123"}, nil},
		{"policy delete", newIAMPolicyDeleteCommand, runIAMPolicyDeleteCommand, []string{"policy_test123"}, nil},
		{"policy list", newIAMPolicyListCommand, runIAMPolicyListCommand, nil, func() {
			globalTenantID = "tenant_test"
		}},
		{"policy validate", newIAMPolicyValidateCommand, runIAMPolicyValidateCommand, []string{`{"effect": "allow"}`}, nil},
		{"policy test", newIAMPolicyTestCommand, runIAMPolicyTestCommand, []string{"policy_test123"}, func() {
			iamPolicySubject = "user_test123"
			iamPolicyAction = "read"
			iamPolicyResource = "document/123"
			globalTenantID = "tenant_test"
		}},

		// Session
		{"session list", newIAMSessionListCommand, runIAMSessionListCommand, nil, func() {
			sessionUserID = "user_test123"
			globalTenantID = "tenant_test"
		}},
		{"session get", newIAMSessionGetCommand, runIAMSessionGetCommand, []string{"session_test123"}, nil},
		{"session revoke", newIAMSessionRevokeCommand, runIAMSessionRevokeCommand, []string{"session_test123"}, nil},
		{"session revoke-all", newIAMSessionRevokeAllCommand, runIAMSessionRevokeAllCommand, nil, func() {
			sessionUserID = "user_test123"
			globalTenantID = "tenant_test"
		}},

		// MFA
		{"mfa status", newMFAStatusCommand, runMFAStatusCommand, nil, func() {
			mfaUserID = "user_test123"
			globalTenantID = "tenant_test"
		}},
		{"mfa enroll", newMFAEnrollCommand, runMFAEnrollCommand, nil, func() {
			mfaUserID = "user_test123"
			mfaMethod = "totp"
			globalTenantID = "tenant_test"
		}},
		{"mfa verify", newMFAVerifyCommand, runMFAVerifyCommand, []string{"enroll_test123"}, func() {
			mfaCode = "123456"
		}},
		{"mfa disable", newMFADisableCommand, runMFADisableCommand, nil, func() {
			mfaUserID = "user_test123"
			globalTenantID = "tenant_test"
			iamForce = true
		}},
		{"mfa backup-codes status", newMFABackupCodesStatusCommand, runMFABackupCodesStatusCommand, nil, func() {
			mfaUserID = "user_test123"
			globalTenantID = "tenant_test"
		}},
		{"mfa backup-codes generate", newMFABackupCodesGenerateCommand, runMFABackupCodesGenerateCommand, nil, func() {
			mfaUserID = "user_test123"
			globalTenantID = "tenant_test"
		}},

		// PEP
		{"pep check", newIAMPEPCheckCommand, runIAMPEPCheckCommand, nil, func() {
			pepSubject = "user_test123"
			pepAction = "read"
			pepResource = "document/123"
			globalTenantID = "tenant_test"
		}},
		{"pep check-batch", newIAMPEPCheckBatchCommand, runIAMPEPCheckBatchCommand, nil, func() {
			pepFile = ""
			globalTenantID = "tenant_test"
		}},

		// Federation
		{"provider create", newIAMProviderCreateCommand, runIAMProviderCreateCommand, nil, func() {
			providerName = "test-provider"
			providerType = "oidc"
			globalTenantID = "tenant_test"
		}},
		{"provider get", newIAMProviderGetCommand, runIAMProviderGetCommand, nil, func() {
			providerID = "provider_test123"
			globalTenantID = "tenant_test"
		}},
		{"provider update", newIAMProviderUpdateCommand, runIAMProviderUpdateCommand, nil, func() {
			providerID = "provider_test123"
			globalTenantID = "tenant_test"
		}},
		{"provider delete", newIAMProviderDeleteCommand, runIAMProviderDeleteCommand, nil, func() {
			providerID = "provider_test123"
			globalTenantID = "tenant_test"
			iamForce = true
		}},
		{"provider list", newIAMProviderListCommand, runIAMProviderListCommand, nil, func() {
			globalTenantID = "tenant_test"
		}},
		{"identity link", newIAMIdentityLinkCommand, runIAMIdentityLinkCommand, nil, func() {
			identityUserID = "user_test123"
			identityProviderID = "provider_test123"
			globalTenantID = "tenant_test"
		}},
		{"identity unlink", newIAMIdentityUnlinkCommand, runIAMIdentityUnlinkCommand, nil, func() {
			identityUserID = "user_test123"
			identityProviderID = "provider_test123"
			globalTenantID = "tenant_test"
			iamForce = true
		}},
		{"identity list", newIAMIdentityListCommand, runIAMIdentityListCommand, nil, func() {
			identityUserID = "user_test123"
			globalTenantID = "tenant_test"
		}},
	}

	// These commands reject their input before they build a client, so the
	// row checks the input error instead of the factory error.
	inputErrors := map[string]string{
		"app update":      "at least one of --name, --redirect-uri, or --redirect-uris must be specified",
		"role update":     "at least one of --name or --description must be specified",
		"policy apply":    "failed to read policy file",
		"policy validate": "failed to read policy file",
		"policy test":     "failed to parse subject JSON",
		"pep check":       "failed to parse subject JSON",
		"pep check-batch": "failed to read batch file",
		"provider create": "either --provider or --provider-id is required",
		"provider update": "at least one update flag must be specified",
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := tt.newCmd()
			if tt.setup != nil {
				tt.setup()
			}
			wantErr, ok := inputErrors[tt.name]
			if !ok {
				wantErr = "connection failed"
			}
			err := tt.run(cmd, tt.args)
			assert.ErrorContains(t, err, wantErr)
		})
	}
}

// =============================================================================
// Command Structure Tests
// =============================================================================

func TestNewIAMAppCommand_StructureComplete(t *testing.T) {
	cmd := newIAMAppCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "app", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.Greater(t, len(cmd.Commands()), 0)
}

func TestNewIAMAuditCommand_StructureComplete(t *testing.T) {
	cmd := newIAMAuditCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "audit", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.Greater(t, len(cmd.Commands()), 0)
}

func TestNewIAMGDPRCommand_StructureComplete(t *testing.T) {
	cmd := newIAMGDPRCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "gdpr", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.Greater(t, len(cmd.Commands()), 0)
}

func TestNewIAMRoleCommand_StructureComplete(t *testing.T) {
	cmd := newIAMRoleCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "role", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.Greater(t, len(cmd.Commands()), 0)
}

func TestNewIAMPolicyCommand_StructureComplete(t *testing.T) {
	cmd := newIAMPolicyCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "policy", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.Greater(t, len(cmd.Commands()), 0)
}

func TestNewIAMSessionCommand_StructureComplete(t *testing.T) {
	cmd := newIAMSessionCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "session", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.Greater(t, len(cmd.Commands()), 0)
}

func TestNewIAMMFACommand_StructureComplete(t *testing.T) {
	cmd := newIAMMFACommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "mfa", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.Greater(t, len(cmd.Commands()), 0)
}

func TestNewIAMPEPCommand_StructureComplete(t *testing.T) {
	cmd := newIAMPEPCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "pep", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.Greater(t, len(cmd.Commands()), 0)
}

func TestNewIAMProviderCommand_StructureComplete(t *testing.T) {
	cmd := newIAMProviderCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "provider", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.Greater(t, len(cmd.Commands()), 0)
}

func TestNewIAMIdentityCommand_StructureComplete(t *testing.T) {
	cmd := newIAMIdentityCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "identity", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.Greater(t, len(cmd.Commands()), 0)
}

func TestIAMAdminAssignmentCreateCommand_InvalidRole(t *testing.T) {
	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	cmd := newIAMAdminAssignmentCreateCommand()
	adminUserID = "user_abc123"
	adminRole = "invalid_role"
	adminGrantedBy = "admin_xyz"
	adminExpiresAt = ""

	err := runIAMAdminAssignmentCreateCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid admin role")
}

func TestIAMAdminAssignmentCreateCommand_InvalidExpiresAt(t *testing.T) {
	cmd := newIAMAdminAssignmentCreateCommand()
	adminUserID = "user_abc123"
	adminRole = "tenant_admin"
	adminGrantedBy = "admin_xyz"
	adminExpiresAt = "not-a-date"

	err := runIAMAdminAssignmentCreateCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expires-at")
}

func TestIAMAdminAssignmentCreateCommand_FactoryError(t *testing.T) {
	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMAdminAssignmentCreateCommand()
	adminUserID = "user_abc123"
	adminRole = "tenant_admin"
	adminGrantedBy = "admin_xyz"
	adminExpiresAt = ""

	err := runIAMAdminAssignmentCreateCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "connection failed")
}

func TestIAMAdminAssignmentGetCommand_FactoryError(t *testing.T) {
	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMAdminAssignmentGetCommand()
	adminAssignmentID = "adm_test123"

	err := runIAMAdminAssignmentGetCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestIAMAdminAssignmentUpdateCommand_InvalidRole(t *testing.T) {
	cmd := newIAMAdminAssignmentUpdateCommand()
	cmd.Flags().Set("role", "invalid_role")
	adminAssignmentID = "adm_test123"
	adminRole = "invalid_role"

	err := runIAMAdminAssignmentUpdateCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid admin role")
}

func TestIAMAdminAssignmentUpdateCommand_InvalidExpiresAt(t *testing.T) {
	cmd := newIAMAdminAssignmentUpdateCommand()
	cmd.Flags().Set("expires-at", "not-valid-date")
	adminAssignmentID = "adm_test123"
	adminExpiresAt = "not-valid-date"
	adminRole = ""

	err := runIAMAdminAssignmentUpdateCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expires-at")
}

func TestIAMAdminAssignmentUpdateCommand_FactoryError(t *testing.T) {
	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMAdminAssignmentUpdateCommand()
	adminAssignmentID = "adm_test123"
	adminRole = ""
	adminExpiresAt = ""

	err := runIAMAdminAssignmentUpdateCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestIAMAdminAssignmentDeleteCommand_FactoryError(t *testing.T) {
	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMAdminAssignmentDeleteCommand()
	adminAssignmentID = "adm_test123"
	iamForce = true

	err := runIAMAdminAssignmentDeleteCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestIAMAdminAssignmentListCommand_FactoryError(t *testing.T) {
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

func TestIAMAdminCheckPermissionCommand_FactoryError(t *testing.T) {
	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMAdminCheckPermissionCommand()
	adminUserID = "user_abc123"
	adminCheckPermission = "user:create"

	err := runIAMAdminCheckPermissionCommand(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// IAM Admin Impersonation Command Tests
// =============================================================================

func TestIAMAdminImpersonateStartCommand_FactoryError(t *testing.T) {
	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMAdminImpersonateStartCommand()
	impersonateAdminUserID = "admin_xyz"
	impersonateTargetUserID = "user_abc123"

	err := runIAMAdminImpersonateStartCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestIAMAdminImpersonateEndCommand_FactoryError(t *testing.T) {
	origFactory := iamClientFactory
	defer func() { iamClientFactory = origFactory }()

	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrUnavailable, "test", "connection failed")
	}

	cmd := newIAMAdminImpersonateEndCommand()
	impersonateSessionID = "imp_test123"

	err := runIAMAdminImpersonateEndCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestIAMAdminImpersonateListCommand_FactoryError(t *testing.T) {
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
// Command Structure Tests
// =============================================================================

func TestNewIAMAdminAssignmentCommand_StructureComplete(t *testing.T) {
	cmd := newIAMAdminAssignmentCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "assignment", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)

	expectedSubcmds := []string{"create", "get", "update", "delete", "list"}
	for _, expected := range expectedSubcmds {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Use == expected || sub.Name() == expected {
				found = true
				break
			}
		}
		assert.True(t, found, "Expected subcommand %s not found", expected)
	}
}

func TestNewIAMAdminImpersonateCommand_StructureComplete(t *testing.T) {
	cmd := newIAMAdminImpersonateCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "impersonate", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.Greater(t, len(cmd.Commands()), 0)
}

func TestNewIAMAdminCheckPermissionCommand_StructureComplete(t *testing.T) {
	cmd := newIAMAdminCheckPermissionCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "check-permission", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
	assert.NotNil(t, cmd.Flags().Lookup("user-id"))
	assert.NotNil(t, cmd.Flags().Lookup("permission"))
}

func TestNewIAMAdminAssignmentCreateCommand_Flags(t *testing.T) {
	cmd := newIAMAdminAssignmentCreateCommand()
	require.NotNil(t, cmd)
	assert.NotNil(t, cmd.Flags().Lookup("user"))
	assert.NotNil(t, cmd.Flags().Lookup("role"))
	assert.NotNil(t, cmd.Flags().Lookup("tenant-ids"))
	assert.NotNil(t, cmd.Flags().Lookup("permissions"))
	assert.NotNil(t, cmd.Flags().Lookup("denied-permissions"))
	assert.NotNil(t, cmd.Flags().Lookup("expires-at"))
	assert.NotNil(t, cmd.Flags().Lookup("granted-by"))
}

func TestNewIAMAdminAssignmentGetCommand_Flags(t *testing.T) {
	cmd := newIAMAdminAssignmentGetCommand()
	require.NotNil(t, cmd)
	assert.NotNil(t, cmd.Flags().Lookup("assignment-id"))
}

func TestNewIAMAdminAssignmentUpdateCommand_Flags(t *testing.T) {
	cmd := newIAMAdminAssignmentUpdateCommand()
	require.NotNil(t, cmd)
	assert.NotNil(t, cmd.Flags().Lookup("assignment-id"))
	assert.NotNil(t, cmd.Flags().Lookup("role"))
	assert.NotNil(t, cmd.Flags().Lookup("tenant-ids"))
	assert.NotNil(t, cmd.Flags().Lookup("permissions"))
	assert.NotNil(t, cmd.Flags().Lookup("expires-at"))
	assert.NotNil(t, cmd.Flags().Lookup("active"))
}

func TestNewIAMAdminAssignmentDeleteCommand_Flags(t *testing.T) {
	cmd := newIAMAdminAssignmentDeleteCommand()
	require.NotNil(t, cmd)
	assert.NotNil(t, cmd.Flags().Lookup("assignment-id"))
}

func TestNewIAMAdminAssignmentListCommand_Flags(t *testing.T) {
	cmd := newIAMAdminAssignmentListCommand()
	require.NotNil(t, cmd)
	assert.NotNil(t, cmd)
}

func TestNewIAMAdminImpersonateStartCommand_Flags(t *testing.T) {
	cmd := newIAMAdminImpersonateStartCommand()
	require.NotNil(t, cmd)
	// No --admin-user-id: the acting admin is the authenticated caller,
	// never named on the command line (go-quicbaas#383).
	assert.Nil(t, cmd.Flags().Lookup("admin-user-id"))
	assert.NotNil(t, cmd.Flags().Lookup("target-user-id"))
	assert.NotNil(t, cmd.Flags().Lookup("tenant-id"))
	assert.NotNil(t, cmd.Flags().Lookup("reason"))
}

func TestNewIAMAdminImpersonateEndCommand_Flags(t *testing.T) {
	cmd := newIAMAdminImpersonateEndCommand()
	require.NotNil(t, cmd)
	assert.NotNil(t, cmd.Flags().Lookup("session-id"))
}

func TestNewIAMAdminImpersonateListCommand_Structure(t *testing.T) {
	cmd := newIAMAdminImpersonateListCommand()
	require.NotNil(t, cmd)
	assert.Equal(t, "list", cmd.Use)
}

// =============================================================================
// Policy Apply - Success & Error Paths (covers YAML parsing, metadata)
// =============================================================================

func TestIAMPolicyApply_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "policy.yaml")
	policyYAML := `apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: test-policy
  tenant: tenant_test
  description: A test policy
spec:
  rules:
    - name: allow-read
      effect: allow
      actions: [read]
    - name: deny-delete
      effect: deny
      actions: [delete]
`
	err := os.WriteFile(policyPath, []byte(policyYAML), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPolicyApplyCommand()
	iamPolicyFile = policyPath
	iamOutputFormat = "table"

	err = runIAMPolicyApplyCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMPolicyApply_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "policy.yaml")
	policyYAML := `apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: json-policy
  tenant: tenant_test
spec:
  rules:
    - name: allow-all
      effect: allow
`
	err := os.WriteFile(policyPath, []byte(policyYAML), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPolicyApplyCommand()
	iamPolicyFile = policyPath
	iamOutputFormat = "json"

	err = runIAMPolicyApplyCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMPolicyApply_MissingFile(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPolicyApplyCommand()
	iamPolicyFile = "/nonexistent/path/policy.yaml"

	err := runIAMPolicyApplyCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read policy file")
}

func TestIAMPolicyApply_InvalidYAML(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "bad.yaml")
	err := os.WriteFile(policyPath, []byte("{{invalid yaml content"), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPolicyApplyCommand()
	iamPolicyFile = policyPath

	err = runIAMPolicyApplyCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse policy YAML")
}

func TestIAMPolicyApply_MissingMetadata(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "no-meta.yaml")
	err := os.WriteFile(policyPath, []byte("apiVersion: v1\nkind: Policy\n"), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPolicyApplyCommand()
	iamPolicyFile = policyPath

	err = runIAMPolicyApplyCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "policy must have metadata section")
}

func TestIAMPolicyApply_MissingName(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "no-name.yaml")
	err := os.WriteFile(policyPath, []byte("metadata:\n  tenant: acme\n"), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPolicyApplyCommand()
	iamPolicyFile = policyPath

	err = runIAMPolicyApplyCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "policy must have metadata.name")
}

func TestIAMPolicyApply_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "no-tenant.yaml")
	err := os.WriteFile(policyPath, []byte("metadata:\n  name: test-policy\n"), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPolicyApplyCommand()
	iamPolicyFile = policyPath

	err = runIAMPolicyApplyCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "policy must have metadata.tenant")
}

// =============================================================================
// Policy Validate - Success & Error Paths (covers 8.8% -> ~90%)
// =============================================================================

func TestIAMPolicyValidate_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "valid-policy.yaml")
	policyYAML := `apiVersion: abac.quicbaas.io/v1
kind: Policy
metadata:
  name: valid-policy
  tenant: tenant_test
spec:
  rules:
    - name: allow-read
      effect: allow
`
	err := os.WriteFile(policyPath, []byte(policyYAML), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPolicyValidateCommand()
	iamPolicyFile = policyPath

	err = runIAMPolicyValidateCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMPolicyValidate_ValidationFailed(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.policyClient.ValidateFunc = func(ctx context.Context, tenantID string, policy *iam.Policy) (*iam.ValidationResult, error) {
		return &iam.ValidationResult{
			Valid: false,
			Errors: []iam.ValidationError{
				{Field: "rules[0].effect", Message: "effect must be 'allow' or 'deny'"},
			},
		}, nil
	}

	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "invalid-policy.yaml")
	policyYAML := `metadata:
  name: bad-policy
  tenant: tenant_test
spec:
  rules:
    - name: bad-rule
      effect: invalid
`
	err := os.WriteFile(policyPath, []byte(policyYAML), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPolicyValidateCommand()
	iamPolicyFile = policyPath

	err = runIAMPolicyValidateCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "policy validation failed")
}

func TestIAMPolicyValidate_MissingFile(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPolicyValidateCommand()
	iamPolicyFile = "/nonexistent/file.yaml"

	err := runIAMPolicyValidateCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read policy file")
}

func TestIAMPolicyValidate_InvalidYAML(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "bad.yaml")
	err := os.WriteFile(policyPath, []byte("{{bad yaml"), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPolicyValidateCommand()
	iamPolicyFile = policyPath

	err = runIAMPolicyValidateCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse policy YAML")
}

func TestIAMPolicyValidate_MissingMetadata(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "no-meta.yaml")
	err := os.WriteFile(policyPath, []byte("kind: Policy\n"), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPolicyValidateCommand()
	iamPolicyFile = policyPath

	err = runIAMPolicyValidateCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "policy must have metadata section")
}

func TestIAMPolicyValidate_MissingName(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "no-name.yaml")
	err := os.WriteFile(policyPath, []byte("metadata:\n  tenant: acme\n"), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPolicyValidateCommand()
	iamPolicyFile = policyPath

	err = runIAMPolicyValidateCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "policy must have metadata.name")
}

func TestIAMPolicyValidate_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	tmpDir := t.TempDir()
	policyPath := filepath.Join(tmpDir, "no-tenant.yaml")
	err := os.WriteFile(policyPath, []byte("metadata:\n  name: test\n"), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPolicyValidateCommand()
	iamPolicyFile = policyPath

	err = runIAMPolicyValidateCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "policy must have metadata.tenant")
}

// =============================================================================
// Policy Test - Success & Error Paths (covers 10.5% -> ~90%)
// =============================================================================

func TestIAMPolicyTest_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPolicyTestCommand()
	globalTenantID = "tenant_test"
	iamPolicySubject = `{"id":"user1","roles":["admin"]}`
	iamPolicyAction = "read"
	iamPolicyResource = "keys/foo"
	iamOutputFormat = "table"

	err := runIAMPolicyTestCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMPolicyTest_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPolicyTestCommand()
	globalTenantID = "tenant_test"
	iamPolicySubject = `{"id":"user1","roles":["admin","developer"]}`
	iamPolicyAction = "write"
	iamPolicyResource = "orders://orders/123"
	iamOutputFormat = "json"

	err := runIAMPolicyTestCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMPolicyTest_WithMatchedPoliciesAndRules(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.policyClient.SimulateFunc = func(ctx context.Context, tenantID string, req *iam.SimulateRequest) (*iam.SimulateResponse, error) {
		return &iam.SimulateResponse{
			Allowed:          true,
			MatchedPolicies:  []string{"order-access", "admin-override"},
			MatchedRules:     []string{"read-own", "admin-all"},
			EvaluationTimeNs: 12345,
		}, nil
	}

	cmd := newIAMPolicyTestCommand()
	globalTenantID = "tenant_test"
	iamPolicySubject = `{"id":"user1","roles":["admin"]}`
	iamPolicyAction = "read"
	iamPolicyResource = "orders://orders/123"
	iamOutputFormat = "table"

	err := runIAMPolicyTestCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMPolicyTest_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPolicyTestCommand()
	globalTenantID = ""
	iamPolicySubject = `{"id":"user1"}`
	iamPolicyAction = "read"
	iamPolicyResource = "keys/foo"

	err := runIAMPolicyTestCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMPolicyTest_InvalidSubjectJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPolicyTestCommand()
	globalTenantID = "tenant_test"
	iamPolicySubject = "not valid json"
	iamPolicyAction = "read"
	iamPolicyResource = "keys/foo"

	err := runIAMPolicyTestCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse subject JSON")
}

// =============================================================================
// PEP Check Batch - Success & Error Paths (covers 35% -> ~90%)
// =============================================================================

func TestIAMPEPCheckBatch_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	tmpDir := t.TempDir()
	batchPath := filepath.Join(tmpDir, "requests.json")
	batchJSON := `{
  "requests": [
    {"subject": {"id": "user1", "roles": ["admin"]}, "action": "read", "resource": "keys/foo"},
    {"subject": {"id": "user2", "roles": ["viewer"], "groups": ["engineering"]}, "action": "write", "resource": "keys/bar"}
  ]
}`
	err := os.WriteFile(batchPath, []byte(batchJSON), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPEPCheckBatchCommand()
	pepFile = batchPath
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err = runIAMPEPCheckBatchCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMPEPCheckBatch_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	tmpDir := t.TempDir()
	batchPath := filepath.Join(tmpDir, "requests.json")
	batchJSON := `{
  "requests": [
    {"subject": {"id": "user1", "roles": ["admin"]}, "action": "read", "resource": "keys/foo"}
  ]
}`
	err := os.WriteFile(batchPath, []byte(batchJSON), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPEPCheckBatchCommand()
	pepFile = batchPath
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err = runIAMPEPCheckBatchCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMPEPCheckBatch_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPEPCheckBatchCommand()
	pepFile = "/tmp/any.json"
	globalTenantID = ""

	err := runIAMPEPCheckBatchCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMPEPCheckBatch_MissingFile(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPEPCheckBatchCommand()
	pepFile = "/nonexistent/batch.json"
	globalTenantID = "tenant_test"

	err := runIAMPEPCheckBatchCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read batch file")
}

func TestIAMPEPCheckBatch_InvalidJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	tmpDir := t.TempDir()
	batchPath := filepath.Join(tmpDir, "bad.json")
	err := os.WriteFile(batchPath, []byte("not valid json"), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPEPCheckBatchCommand()
	pepFile = batchPath
	globalTenantID = "tenant_test"

	err = runIAMPEPCheckBatchCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse batch file JSON")
}

func TestIAMPEPCheckBatch_EmptyRequests(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	tmpDir := t.TempDir()
	batchPath := filepath.Join(tmpDir, "empty.json")
	err := os.WriteFile(batchPath, []byte(`{"requests": []}`), 0o600)
	assert.NoError(t, err)

	cmd := newIAMPEPCheckBatchCommand()
	pepFile = batchPath
	globalTenantID = "tenant_test"

	err = runIAMPEPCheckBatchCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "batch file contains no requests")
}

// =============================================================================
// PEP Check - Additional Paths (context parsing, groups, denial)
// =============================================================================

func TestIAMPEPCheck_WithContext(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPEPCheckCommand()
	pepSubject = `{"id":"user_001","roles":["developer"],"groups":["engineering"]}`
	pepAction = "write"
	pepResource = "orders://orders/123"
	pepContext = `{"ip":"192.168.1.1","time":"2025-01-01T12:00:00Z"}`
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMPEPCheckCommand(cmd, []string{})
	assert.NoError(t, err)
	pepContext = ""
}

func TestIAMPEPCheck_InvalidContext(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPEPCheckCommand()
	pepSubject = `{"id":"user1"}`
	pepAction = "read"
	pepResource = "keys/foo"
	pepContext = "invalid json"
	globalTenantID = "tenant_test"

	err := runIAMPEPCheckCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse context JSON")
	pepContext = ""
}

func TestIAMPEPCheck_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPEPCheckCommand()
	pepSubject = `{"id":"user1"}`
	pepAction = "read"
	pepResource = "keys/foo"
	pepContext = ""
	globalTenantID = ""

	err := runIAMPEPCheckCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMPEPCheck_InvalidSubject(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPEPCheckCommand()
	pepSubject = "not json"
	pepAction = "read"
	pepResource = "keys/foo"
	pepContext = ""
	globalTenantID = "tenant_test"

	err := runIAMPEPCheckCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse subject JSON")
}

func TestIAMPEPCheck_WithMatchedPolicyAndRule(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.pepClient.CheckFunc = func(ctx context.Context, req *iam.CheckRequest) (*iam.CheckResponse, error) {
		return &iam.CheckResponse{
			Allowed:          false,
			MatchedPolicy:    "deny-all-policy",
			MatchedRule:      "deny-external",
			Reason:           "Access denied by policy",
			EvaluationTimeNs: 5678,
		}, nil
	}

	cmd := newIAMPEPCheckCommand()
	pepSubject = `{"id":"user1","roles":["viewer"]}`
	pepAction = "delete"
	pepResource = "keys/important"
	pepContext = ""
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMPEPCheckCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Audit Query - Filter Paths (covers 56.9% -> ~90%)
// =============================================================================

func TestIAMAuditQuery_WithAllFilters(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAuditQueryCommand()
	globalTenantID = "tenant_test"
	iamAuditEventType = "user.login"
	iamAuditCategory = "authentication"
	iamAuditOutcome = "success"
	iamAuditActor = "user_abc"
	iamAuditResource = "session_001"
	iamAuditStartTime = "2025-01-01T00:00:00Z"
	iamAuditEndTime = "2025-12-31T23:59:59Z"
	iamAuditLimit = 50
	iamAuditOffset = 10
	iamOutputFormat = "table"

	err := runIAMAuditQueryCommand(cmd, []string{})
	assert.NoError(t, err)

	iamAuditEventType = ""
	iamAuditCategory = ""
	iamAuditOutcome = ""
	iamAuditActor = ""
	iamAuditResource = ""
	iamAuditStartTime = ""
	iamAuditEndTime = ""
	iamAuditLimit = 100
	iamAuditOffset = 0
}

func TestIAMAuditQuery_WithFailureOutcome(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAuditQueryCommand()
	globalTenantID = "tenant_test"
	iamAuditOutcome = "failure"
	iamOutputFormat = "table"

	err := runIAMAuditQueryCommand(cmd, []string{})
	assert.NoError(t, err)
	iamAuditOutcome = ""
}

func TestIAMAuditQuery_InvalidOutcome(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAuditQueryCommand()
	globalTenantID = "tenant_test"
	iamAuditOutcome = "invalid"

	err := runIAMAuditQueryCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--outcome must be 'success' or 'failure'")
	iamAuditOutcome = ""
}

func TestIAMAuditQuery_InvalidStartTime(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAuditQueryCommand()
	globalTenantID = "tenant_test"
	iamAuditStartTime = "not-a-date"

	err := runIAMAuditQueryCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid start-time format")
	iamAuditStartTime = ""
}

func TestIAMAuditQuery_InvalidEndTime(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAuditQueryCommand()
	globalTenantID = "tenant_test"
	iamAuditEndTime = "not-a-date"

	err := runIAMAuditQueryCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid end-time format")
	iamAuditEndTime = ""
}

func TestIAMAuditQuery_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAuditQueryCommand()
	globalTenantID = ""

	err := runIAMAuditQueryCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMAuditQuery_EmptyResults(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.auditClient.QueryFunc = func(ctx context.Context, filter *iam.AuditFilter) (*iam.AuditQueryResponse, error) {
		return &iam.AuditQueryResponse{Events: []iam.AuditEvent{}, TotalCount: 0}, nil
	}

	cmd := newIAMAuditQueryCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMAuditQueryCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAuditQuery_WithActorAndResource(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.auditClient.QueryFunc = func(ctx context.Context, filter *iam.AuditFilter) (*iam.AuditQueryResponse, error) {
		return &iam.AuditQueryResponse{
			Events: []iam.AuditEvent{
				{
					ID: "evt_001", TenantID: "tenant_test", Action: "user.login",
					Outcome: iam.AuditOutcomeSuccess, Timestamp: time.Now(),
					Actor:    &iam.AuditActor{ID: "user_abc", Type: "user", IPAddress: "10.0.0.1"},
					Resource: &iam.AuditResource{ID: "session_001", Type: "session"},
				},
			},
			TotalCount: 1,
		}, nil
	}

	cmd := newIAMAuditQueryCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMAuditQueryCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Audit Get - Additional Paths
// =============================================================================

func TestIAMAuditGet_WithFullDetails(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.auditClient.GetFunc = func(ctx context.Context, tenantID, eventID string) (*iam.AuditEvent, error) {
		return &iam.AuditEvent{
			ID: eventID, TenantID: tenantID, Action: "user.login",
			EventType: iam.AuditEventTypeAuthentication, Outcome: iam.AuditOutcomeSuccess,
			Severity: iam.AuditSeverityInfo, Timestamp: time.Now(),
			Actor:    &iam.AuditActor{ID: "user_abc", Type: "user", IPAddress: "10.0.0.1", UserAgent: "test-agent/1.0"},
			Resource: &iam.AuditResource{ID: "session_001", Type: "session"},
			Details:  map[string]interface{}{"method": "password", "mfa": true},
		}, nil
	}

	cmd := newIAMAuditGetCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMAuditGetCommand(cmd, []string{"evt_001"})
	assert.NoError(t, err)
}

func TestIAMAuditGet_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAuditGetCommand()
	globalTenantID = ""

	err := runIAMAuditGetCommand(cmd, []string{"evt_001"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

// =============================================================================
// Session Commands - Additional Paths
// =============================================================================

func TestIAMSessionList_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMSessionListCommand()
	sessionUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMSessionListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMSessionList_SuccessYAML(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMSessionListCommand()
	sessionUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "yaml"

	err := runIAMSessionListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMSessionList_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMSessionListCommand()
	globalTenantID = ""

	err := runIAMSessionListCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMSessionList_EmptyResults(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.sessionClient.ListFunc = func(ctx context.Context, req *iam.ListSessionsRequest) (*iam.ListSessionsResponse, error) {
		return &iam.ListSessionsResponse{Sessions: []iam.Session{}, TotalCount: 0}, nil
	}

	cmd := newIAMSessionListCommand()
	sessionUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMSessionListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMSessionList_LongUserAgent(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.sessionClient.ListFunc = func(ctx context.Context, req *iam.ListSessionsRequest) (*iam.ListSessionsResponse, error) {
		return &iam.ListSessionsResponse{
			Sessions: []iam.Session{{
				ID: "sess_001", UserID: "user_001", TenantID: "tenant_test",
				IPAddress: "127.0.0.1",
				UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36",
				Created:   time.Now(), LastActive: time.Now(),
			}},
			TotalCount: 1,
		}, nil
	}

	cmd := newIAMSessionListCommand()
	sessionUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMSessionListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMSessionRevoke_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMSessionRevokeCommand()
	globalTenantID = ""
	iamForce = true

	err := runIAMSessionRevokeCommand(cmd, []string{"sess_001"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMSessionRevokeAll_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMSessionRevokeAllCommand()
	globalTenantID = ""
	iamForce = true

	err := runIAMSessionRevokeAllCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMSessionGet_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMSessionGetCommand()
	globalTenantID = ""

	err := runIAMSessionGetCommand(cmd, []string{"sess_001"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMSessionGet_WithDeviceInfo(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.sessionClient.GetFunc = func(ctx context.Context, sessionID string) (*iam.Session, error) {
		return &iam.Session{
			ID: sessionID, UserID: "user_001", TenantID: "tenant_test",
			IPAddress: "10.0.0.1", UserAgent: "test-agent",
			DeviceInfo: "Windows 10 / Chrome 120", MFAVerified: true,
			Created: time.Now(), LastActive: time.Now(), ExpiresAt: time.Now().Add(time.Hour),
		}, nil
	}

	cmd := newIAMSessionGetCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMSessionGetCommand(cmd, []string{"sess_001"})
	assert.NoError(t, err)
}

// =============================================================================
// GDPR Commands - JSON Output & Error Paths
// =============================================================================

func TestIAMGDPRConsentGrant_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRConsentGrantCommand()
	gdprUserID = "user_001"
	gdprPurpose = "marketing"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMGDPRConsentGrantCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRConsentGrant_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRConsentGrantCommand()
	gdprUserID = "user_001"
	gdprPurpose = "marketing"
	globalTenantID = ""

	err := runIAMGDPRConsentGrantCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMGDPRConsentGrant_InvalidPurpose(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRConsentGrantCommand()
	gdprUserID = "user_001"
	gdprPurpose = "invalid_purpose"
	globalTenantID = "tenant_test"

	err := runIAMGDPRConsentGrantCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid purpose")
}

func TestIAMGDPRConsentWithdraw_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRConsentWithdrawCommand()
	gdprUserID = "user_001"
	gdprPurpose = "marketing"
	iamForce = true
	globalTenantID = ""

	err := runIAMGDPRConsentWithdrawCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMGDPRConsentGet_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRConsentGetCommand()
	gdprUserID = "user_001"
	gdprPurpose = "analytics"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMGDPRConsentGetCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRConsentGet_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRConsentGetCommand()
	gdprUserID = "user_001"
	gdprPurpose = "analytics"
	globalTenantID = ""

	err := runIAMGDPRConsentGetCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMGDPRConsentGet_WithTimestamps(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.gdprClient.GetConsentFunc = func(ctx context.Context, tenantID, userID, purpose string) (*iam.Consent, error) {
		return &iam.Consent{
			ID: "consent_001", UserID: userID, TenantID: tenantID, Purpose: purpose,
			Status:    iam.ConsentStatusWithdrawn,
			GrantedAt: time.Now().Add(-24 * time.Hour), WithdrawnAt: time.Now(),
			ExpiresAt: time.Now().Add(30 * 24 * time.Hour),
		}, nil
	}

	cmd := newIAMGDPRConsentGetCommand()
	gdprUserID = "user_001"
	gdprPurpose = "analytics"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRConsentGetCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRConsentList_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRConsentListCommand()
	gdprUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMGDPRConsentListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRConsentList_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRConsentListCommand()
	gdprUserID = "user_001"
	globalTenantID = ""

	err := runIAMGDPRConsentListCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMGDPRConsentList_EmptyResults(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.gdprClient.ListConsentsFunc = func(ctx context.Context, tenantID, userID string) (*iam.ListConsentsResponse, error) {
		return &iam.ListConsentsResponse{Consents: []iam.Consent{}, TotalCount: 0}, nil
	}

	cmd := newIAMGDPRConsentListCommand()
	gdprUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRConsentListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRConsentList_WithWithdrawnTimestamp(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.gdprClient.ListConsentsFunc = func(ctx context.Context, tenantID, userID string) (*iam.ListConsentsResponse, error) {
		return &iam.ListConsentsResponse{
			Consents: []iam.Consent{
				{ID: "c1", Purpose: "marketing", Status: iam.ConsentStatusGranted, GrantedAt: time.Now()},
				{ID: "c2", Purpose: "analytics", Status: iam.ConsentStatusWithdrawn, GrantedAt: time.Now().Add(-24 * time.Hour), WithdrawnAt: time.Now()},
			},
			TotalCount: 2,
		}, nil
	}

	cmd := newIAMGDPRConsentListCommand()
	gdprUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRConsentListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRExportRequest_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRExportRequestCommand()
	gdprUserID = "user_001"
	gdprExportFormat = "json"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMGDPRExportRequestCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRExportRequest_CSVFormat(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRExportRequestCommand()
	gdprUserID = "user_001"
	gdprExportFormat = "csv"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRExportRequestCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRExportRequest_XMLFormat(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRExportRequestCommand()
	gdprUserID = "user_001"
	gdprExportFormat = "xml"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRExportRequestCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRExportRequest_InvalidFormat(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRExportRequestCommand()
	gdprUserID = "user_001"
	gdprExportFormat = "pdf"
	globalTenantID = "tenant_test"

	err := runIAMGDPRExportRequestCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid format")
}

func TestIAMGDPRExportRequest_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRExportRequestCommand()
	gdprUserID = "user_001"
	gdprExportFormat = "json"
	globalTenantID = ""

	err := runIAMGDPRExportRequestCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMGDPRExportStatus_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRExportStatusCommand()
	gdprUserID = "user_001"
	gdprExportID = "export_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMGDPRExportStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRExportStatus_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRExportStatusCommand()
	gdprUserID = "user_001"
	gdprExportID = "export_001"
	globalTenantID = ""

	err := runIAMGDPRExportStatusCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMGDPRExportStatus_WithFullDetails(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.gdprClient.GetExportStatusFunc = func(ctx context.Context, tenantID, userID, exportID string) (*iam.DataExportRequest, error) {
		return &iam.DataExportRequest{
			ID: exportID, UserID: userID, TenantID: tenantID,
			Format: iam.ExportFormatJSON, Status: iam.ExportStatusCompleted,
			RequestedAt: time.Now().Add(-time.Hour), CompletedAt: time.Now(),
			DownloadURL: "https://example.com/export/file.json",
			ExpiresAt:   time.Now().Add(24 * time.Hour), SizeBytes: 1048576,
		}, nil
	}

	cmd := newIAMGDPRExportStatusCommand()
	gdprUserID = "user_001"
	gdprExportID = "export_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRExportStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRExportStatus_WithError(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.gdprClient.GetExportStatusFunc = func(ctx context.Context, tenantID, userID, exportID string) (*iam.DataExportRequest, error) {
		return &iam.DataExportRequest{
			ID: exportID, UserID: userID, TenantID: tenantID,
			Status: iam.ExportStatusFailed, RequestedAt: time.Now(),
			ErrorMessage: "export generation failed",
		}, nil
	}

	cmd := newIAMGDPRExportStatusCommand()
	gdprUserID = "user_001"
	gdprExportID = "export_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRExportStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRDeletionRequest_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRDeletionRequestCommand()
	gdprUserID = "user_001"
	gdprDeletionReason = "user request"
	globalTenantID = "tenant_test"
	iamForce = true
	iamOutputFormat = "json"

	err := runIAMGDPRDeletionRequestCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRDeletionRequest_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRDeletionRequestCommand()
	gdprUserID = "user_001"
	gdprDeletionReason = "reason"
	iamForce = true
	globalTenantID = ""

	err := runIAMGDPRDeletionRequestCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMGDPRDeletionRequest_WithScheduledFor(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.gdprClient.RequestDeletionFunc = func(ctx context.Context, tenantID, userID string, req *iam.DeletionRequest) (*iam.DataDeletionRequest, error) {
		return &iam.DataDeletionRequest{
			ID: "del_001", UserID: userID, TenantID: tenantID,
			Status: iam.DeletionStatusPending, Reason: req.Reason,
			RequestedAt: time.Now(), ScheduledFor: req.ScheduledFor,
		}, nil
	}

	cmd := newIAMGDPRDeletionRequestCommand()
	gdprUserID = "user_001"
	gdprDeletionReason = "GDPR request"
	gdprGracePeriodDays = 14
	globalTenantID = "tenant_test"
	iamForce = true
	iamOutputFormat = "table"

	err := runIAMGDPRDeletionRequestCommand(cmd, []string{})
	assert.NoError(t, err)
	gdprGracePeriodDays = 30
}

func TestIAMGDPRDeletionCancel_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRDeletionCancelCommand()
	gdprUserID = "user_001"
	gdprDeletionID = "del_001"
	globalTenantID = ""

	err := runIAMGDPRDeletionCancelCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMGDPRDeletionStatus_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRDeletionStatusCommand()
	gdprUserID = "user_001"
	gdprDeletionID = "del_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMGDPRDeletionStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRDeletionStatus_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRDeletionStatusCommand()
	gdprUserID = "user_001"
	gdprDeletionID = "del_001"
	globalTenantID = ""

	err := runIAMGDPRDeletionStatusCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMGDPRDeletionStatus_WithFullDetails(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.gdprClient.GetDeletionStatusFunc = func(ctx context.Context, tenantID, userID, deletionID string) (*iam.DataDeletionRequest, error) {
		return &iam.DataDeletionRequest{
			ID: deletionID, UserID: userID, TenantID: tenantID,
			Status: iam.DeletionStatusCompleted, Reason: "GDPR Article 17",
			RequestedAt:         time.Now().Add(-72 * time.Hour),
			ScheduledFor:        time.Now().Add(-48 * time.Hour),
			ProcessingStartedAt: time.Now().Add(-24 * time.Hour),
			CompletedAt:         time.Now(),
		}, nil
	}

	cmd := newIAMGDPRDeletionStatusCommand()
	gdprUserID = "user_001"
	gdprDeletionID = "del_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRDeletionStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRDeletionStatus_Cancelled(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.gdprClient.GetDeletionStatusFunc = func(ctx context.Context, tenantID, userID, deletionID string) (*iam.DataDeletionRequest, error) {
		return &iam.DataDeletionRequest{
			ID: deletionID, UserID: userID, TenantID: tenantID,
			Status: iam.DeletionStatusCancelled, Reason: "User changed mind",
			RequestedAt: time.Now().Add(-24 * time.Hour), CancelledAt: time.Now(),
		}, nil
	}

	cmd := newIAMGDPRDeletionStatusCommand()
	gdprUserID = "user_001"
	gdprDeletionID = "del_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRDeletionStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRDeletionStatus_WithError(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.gdprClient.GetDeletionStatusFunc = func(ctx context.Context, tenantID, userID, deletionID string) (*iam.DataDeletionRequest, error) {
		return &iam.DataDeletionRequest{
			ID: deletionID, UserID: userID, TenantID: tenantID,
			Status: iam.DeletionStatusFailed, Reason: "User request",
			RequestedAt: time.Now(), ErrorMessage: "deletion processing failed",
		}, nil
	}

	cmd := newIAMGDPRDeletionStatusCommand()
	gdprUserID = "user_001"
	gdprDeletionID = "del_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRDeletionStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Policy Get/Delete/List - Additional Paths
// =============================================================================

func TestIAMPolicyGet_SuccessYAML(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPolicyGetCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "yaml"

	err := runIAMPolicyGetCommand(cmd, []string{"test-policy"})
	assert.NoError(t, err)
}

func TestIAMPolicyGet_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPolicyGetCommand()
	globalTenantID = ""

	err := runIAMPolicyGetCommand(cmd, []string{"test-policy"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMPolicyDelete_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPolicyDeleteCommand()
	globalTenantID = ""
	iamForce = true

	err := runIAMPolicyDeleteCommand(cmd, []string{"test-policy"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMPolicyList_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMPolicyListCommand()
	globalTenantID = ""

	err := runIAMPolicyListCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMPolicyList_EmptyResults(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.policyClient.ListFunc = func(ctx context.Context, tenantID string, req *iam.ListPoliciesRequest) (*iam.ListPoliciesResponse, error) {
		return &iam.ListPoliciesResponse{Policies: []iam.Policy{}}, nil
	}

	cmd := newIAMPolicyListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMPolicyListCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Admin Assignment Update - Additional Paths
// =============================================================================

func TestIAMAdminAssignmentUpdate_WithExpiresAt(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminAssignmentUpdateCommand()
	adminAssignmentID = "adm_test123"
	cmd.Flags().Set("expires-at", time.Now().Add(48*time.Hour).Format(time.RFC3339))
	iamOutputFormat = "table"

	err := runIAMAdminAssignmentUpdateCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminAssignmentUpdate_WithPermissions(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminAssignmentUpdateCommand()
	adminAssignmentID = "adm_test123"
	cmd.Flags().Set("permissions", "user:read,user:write")
	iamOutputFormat = "json"

	err := runIAMAdminAssignmentUpdateCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminAssignmentUpdate_WithActive(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminAssignmentUpdateCommand()
	adminAssignmentID = "adm_test123"
	cmd.Flags().Set("active", "false")
	iamOutputFormat = "table"

	err := runIAMAdminAssignmentUpdateCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// App Commands - Missing Tenant & JSON
// =============================================================================

func TestIAMAppRegister_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppRegisterCommand()
	iamAppName = "test-app"
	globalTenantID = ""

	err := runIAMAppRegisterCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestIAMAppGet_WithRedirectURIs(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.appClient.GetFunc = func(ctx context.Context, tenantID, clientID string) (*iam.Application, error) {
		return &iam.Application{ClientID: clientID, Name: "App With URIs", TenantID: tenantID, RedirectURIs: []string{"https://app.com/cb1", "https://app.com/cb2"}, CreatedAt: time.Now(), UpdatedAt: time.Now()}, nil
	}
	cmd := newIAMAppGetCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"
	err := runIAMAppGetCommand(cmd, []string{"app_test"})
	assert.NoError(t, err)
}

func TestIAMAppGet_SuccessJSON_WithRedirectURIs(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppGetCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMAppGetCommand(cmd, []string{"app_test"})
	assert.NoError(t, err)
}

func TestIAMAppDelete_SuccessForce(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppDeleteCommand()
	globalTenantID = "tenant_test"
	iamForce = true

	err := runIAMAppDeleteCommand(cmd, []string{"app_test"})
	assert.NoError(t, err)
}

func TestIAMAppList_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppListCommand()
	globalTenantID = ""

	err := runIAMAppListCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestIAMAppRotateSecret_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppRotateSecretCommand()
	globalTenantID = "tenant_test"
	iamForce = true
	iamOutputFormat = "json"

	err := runIAMAppRotateSecretCommand(cmd, []string{"app_test123"})
	assert.NoError(t, err)
}

// =============================================================================
// MFA Missing Tenant Tests
// =============================================================================

func TestMFADisable_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newMFADisableCommand()
	mfaUserID = "user_001"
	globalTenantID = ""
	iamForce = true

	err := runMFADisableCommand(cmd, []string{})
	assert.Error(t, err)
}

func TestMFABackupCodesGenerate_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newMFABackupCodesGenerateCommand()
	mfaUserID = "user_001"
	globalTenantID = ""
	iamForce = true

	err := runMFABackupCodesGenerateCommand(cmd, []string{})
	assert.Error(t, err)
}

// =============================================================================
// Utility function: truncateString
// =============================================================================

func TestTruncateString_ShortString(t *testing.T) {
	result := truncateString("hello", 10)
	assert.Equal(t, "hello", result)
}

func TestTruncateString_ExactLength(t *testing.T) {
	result := truncateString("hello", 5)
	assert.Equal(t, "hello", result)
}

func TestTruncateString_Truncated(t *testing.T) {
	result := truncateString("hello world", 8)
	assert.Equal(t, "hello...", result)
}

func TestTruncateString_VeryShortMax(t *testing.T) {
	result := truncateString("hello", 3)
	assert.Equal(t, "hel", result)
}

func withStdinInput(t *testing.T, input string) func() {
	t.Helper()
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed to create pipe: %v", err)
	}
	os.Stdin = r
	go func() {
		w.Write([]byte(input))
		w.Close()
	}()
	return func() {
		os.Stdin = oldStdin
	}
}

// =============================================================================
// App Delete - Confirmation Prompt Paths
// =============================================================================

func TestIAMAppDelete_ConfirmYes(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "y\n")
	defer restoreStdin()

	cmd := newIAMAppDeleteCommand()
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMAppDeleteCommand(cmd, []string{"app_test123"})
	assert.NoError(t, err)
}

func TestIAMAppDelete_ConfirmNo(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "n\n")
	defer restoreStdin()

	cmd := newIAMAppDeleteCommand()
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMAppDeleteCommand(cmd, []string{"app_test123"})
	assert.NoError(t, err)
}

// =============================================================================
// Policy Delete - Confirmation Prompt Paths
// =============================================================================

func TestIAMPolicyDelete_ConfirmYes(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "yes\n")
	defer restoreStdin()

	cmd := newIAMPolicyDeleteCommand()
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMPolicyDeleteCommand(cmd, []string{"test-policy"})
	assert.NoError(t, err)
}

func TestIAMPolicyDelete_ConfirmNo(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "n\n")
	defer restoreStdin()

	cmd := newIAMPolicyDeleteCommand()
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMPolicyDeleteCommand(cmd, []string{"test-policy"})
	assert.NoError(t, err)
}

// =============================================================================
// Session Revoke - Confirmation Prompt Paths
// =============================================================================

func TestIAMSessionRevoke_ConfirmYes(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "y\n")
	defer restoreStdin()

	cmd := newIAMSessionRevokeCommand()
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMSessionRevokeCommand(cmd, []string{"sess_001"})
	assert.NoError(t, err)
}

func TestIAMSessionRevoke_ConfirmNo(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "no\n")
	defer restoreStdin()

	cmd := newIAMSessionRevokeCommand()
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMSessionRevokeCommand(cmd, []string{"sess_001"})
	assert.NoError(t, err)
}

// =============================================================================
// Session RevokeAll - Confirmation + User ID Paths
// =============================================================================

func TestIAMSessionRevokeAll_ConfirmNo(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "n\n")
	defer restoreStdin()

	cmd := newIAMSessionRevokeAllCommand()
	globalTenantID = "tenant_test"
	iamForce = false
	sessionKeepCurrent = false

	err := runIAMSessionRevokeAllCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMSessionRevokeAll_KeepCurrentConfirmNo(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "n\n")
	defer restoreStdin()

	cmd := newIAMSessionRevokeAllCommand()
	globalTenantID = "tenant_test"
	iamForce = false
	sessionKeepCurrent = true

	err := runIAMSessionRevokeAllCommand(cmd, []string{})
	assert.NoError(t, err)
	sessionKeepCurrent = false
}

func TestIAMSessionRevokeAll_WithExplicitUserID(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMSessionRevokeAllCommand()
	globalTenantID = "tenant_test"
	iamForce = true
	sessionKeepCurrent = false
	sessionUserID = "user_explicit"

	err := runIAMSessionRevokeAllCommand(cmd, []string{})
	assert.NoError(t, err)
	sessionUserID = ""
}

// =============================================================================
// GDPR Consent Withdraw - Confirmation Prompt Paths
// =============================================================================

func TestIAMGDPRConsentWithdraw_ConfirmYes(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "y\n")
	defer restoreStdin()

	cmd := newIAMGDPRConsentWithdrawCommand()
	gdprUserID = "user_001"
	gdprPurpose = "marketing"
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMGDPRConsentWithdrawCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRConsentWithdraw_ConfirmNo(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "n\n")
	defer restoreStdin()

	cmd := newIAMGDPRConsentWithdrawCommand()
	gdprUserID = "user_001"
	gdprPurpose = "marketing"
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMGDPRConsentWithdrawCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// GDPR Deletion Request - Confirmation Prompt Paths
// =============================================================================

func TestIAMGDPRDeletionRequest_ConfirmYes(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "yes\n")
	defer restoreStdin()

	cmd := newIAMGDPRDeletionRequestCommand()
	gdprUserID = "user_001"
	gdprDeletionReason = "user request"
	globalTenantID = "tenant_test"
	iamForce = false
	iamOutputFormat = "table"

	err := runIAMGDPRDeletionRequestCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRDeletionRequest_ConfirmNo(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "n\n")
	defer restoreStdin()

	cmd := newIAMGDPRDeletionRequestCommand()
	gdprUserID = "user_001"
	gdprDeletionReason = "user request"
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMGDPRDeletionRequestCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// GDPR Consent Grant - Additional Purpose Paths
// =============================================================================

func TestIAMGDPRConsentGrant_DataProcessing(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRConsentGrantCommand()
	gdprUserID = "user_001"
	gdprPurpose = "data_processing"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRConsentGrantCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMGDPRConsentGrant_Personalization(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMGDPRConsentGrantCommand()
	gdprUserID = "user_001"
	gdprPurpose = "personalization"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMGDPRConsentGrantCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Admin Assignment Delete - Confirmation Prompt Paths
// =============================================================================

func TestIAMAdminAssignmentDelete_ConfirmYes(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "y\n")
	defer restoreStdin()

	cmd := newIAMAdminAssignmentDeleteCommand()
	adminAssignmentID = "adm_test123"
	iamForce = false

	err := runIAMAdminAssignmentDeleteCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminAssignmentDelete_ConfirmNo(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "n\n")
	defer restoreStdin()

	cmd := newIAMAdminAssignmentDeleteCommand()
	adminAssignmentID = "adm_test123"
	iamForce = false

	err := runIAMAdminAssignmentDeleteCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Admin Assignment List - JSON & Empty
// =============================================================================

func TestIAMAdminAssignmentList_JSONOutput(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminAssignmentListCommand()
	iamOutputFormat = "json"

	err := runIAMAdminAssignmentListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminAssignmentList_EmptyResults(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.adminClient.ListAssignmentsFunc = func(ctx context.Context, req *iam.ListAdminAssignmentsRequest) (*iam.ListAdminAssignmentsResponse, error) {
		return &iam.ListAdminAssignmentsResponse{Assignments: []iam.AdminAssignment{}}, nil
	}

	cmd := newIAMAdminAssignmentListCommand()
	iamOutputFormat = "table"

	err := runIAMAdminAssignmentListCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Admin Check Permission - JSON Output
// =============================================================================

func TestIAMAdminCheckPermission_JSONOutput(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminCheckPermissionCommand()
	adminUserID = "user_abc123"
	adminCheckPermission = "user:create"
	iamOutputFormat = "json"

	err := runIAMAdminCheckPermissionCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Admin Impersonation - JSON Output & Empty
// =============================================================================

func TestIAMAdminImpersonateList_JSONOutput(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAdminImpersonateListCommand()
	iamOutputFormat = "json"

	err := runIAMAdminImpersonateListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAdminImpersonateList_EmptyResults(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.adminClient.ListActiveImpersonationsFunc = func(ctx context.Context) (*iam.ListImpersonationsResponse, error) {
		return &iam.ListImpersonationsResponse{Sessions: []iam.ImpersonationSession{}}, nil
	}

	cmd := newIAMAdminImpersonateListCommand()
	iamOutputFormat = "table"

	err := runIAMAdminImpersonateListCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// App Update - Redirect URI Paths
// =============================================================================

func TestIAMAppUpdate_WithRedirectURI(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.appClient.UpdateFunc = func(ctx context.Context, tenantID, clientID string, req *iam.UpdateAppRequest) (*iam.Application, error) {
		return &iam.Application{
			ClientID: clientID, Name: "Updated", TenantID: tenantID,
			RedirectURIs: req.RedirectURIs, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}, nil
	}

	cmd := newIAMAppUpdateCommand()
	cmd.Flags().Set("redirect-uri", "https://newapp.com/callback")
	iamAppRedirectURI = "https://newapp.com/callback"
	globalTenantID = "tenant_test"

	err := runIAMAppUpdateCommand(cmd, []string{"app_test123"})
	assert.NoError(t, err)
	iamAppRedirectURI = ""
}

func TestIAMAppUpdate_WithRedirectURIs(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.appClient.UpdateFunc = func(ctx context.Context, tenantID, clientID string, req *iam.UpdateAppRequest) (*iam.Application, error) {
		return &iam.Application{
			ClientID: clientID, Name: "Updated", TenantID: tenantID,
			RedirectURIs: req.RedirectURIs, CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}, nil
	}

	cmd := newIAMAppUpdateCommand()
	cmd.Flags().Set("redirect-uris", "https://app.com/cb1,https://app.com/cb2")
	globalTenantID = "tenant_test"

	err := runIAMAppUpdateCommand(cmd, []string{"app_test123"})
	assert.NoError(t, err)
}

func TestIAMAppUpdate_NoFlagsChanged(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppUpdateCommand()
	globalTenantID = "tenant_test"

	err := runIAMAppUpdateCommand(cmd, []string{"app_test123"})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "at least one of --name, --redirect-uri, or --redirect-uris must be specified")
}

// =============================================================================
// App Register - JSON & With Redirect URIs
// =============================================================================

func TestIAMAppRegister_JSONOutput(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppRegisterCommand()
	iamAppName = "test-app"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMAppRegisterCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAppRegister_WithRedirectURIs(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.appClient.RegisterFunc = func(ctx context.Context, tenantID string, req *iam.RegisterAppRequest) (*iam.Application, error) {
		return &iam.Application{
			ClientID: "app_new", ClientSecret: "secret123",
			Name: req.Name, TenantID: tenantID,
			RedirectURIs: req.RedirectURIs,
			CreatedAt:    time.Now(),
		}, nil
	}

	cmd := newIAMAppRegisterCommand()
	iamAppName = "test-app-uris"
	globalTenantID = "tenant_test"
	iamAppRedirectURIs = []string{"https://app.com/cb1", "https://app.com/cb2"}
	iamOutputFormat = "table"

	err := runIAMAppRegisterCommand(cmd, []string{})
	assert.NoError(t, err)
	iamAppRedirectURIs = nil
}

// =============================================================================
// App List - JSON & Empty
// =============================================================================

func TestIAMAppList_JSONOutput(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMAppListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMAppListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMAppList_EmptyResults(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.appClient.ListFunc = func(ctx context.Context, tenantID string) (*iam.ListAppsResponse, error) {
		return &iam.ListAppsResponse{Applications: []iam.Application{}}, nil
	}

	cmd := newIAMAppListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMAppListCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// MFA Disable - Confirmation Prompt Paths
// =============================================================================

func TestMFADisable_ConfirmYes(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "y\n")
	defer restoreStdin()

	cmd := newMFADisableCommand()
	mfaUserID = "user_001"
	globalTenantID = "tenant_test"
	iamForce = false

	err := runMFADisableCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestMFADisable_ConfirmNo(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "n\n")
	defer restoreStdin()

	cmd := newMFADisableCommand()
	mfaUserID = "user_001"
	globalTenantID = "tenant_test"
	iamForce = false

	err := runMFADisableCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// MFA Backup Codes Generate - Confirmation Prompt
// =============================================================================

func TestMFABackupCodesGenerate_ConfirmYes(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "y\n")
	defer restoreStdin()

	cmd := newMFABackupCodesGenerateCommand()
	mfaUserID = "user_001"
	globalTenantID = "tenant_test"
	iamForce = false

	err := runMFABackupCodesGenerateCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestMFABackupCodesGenerate_ConfirmNo(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "n\n")
	defer restoreStdin()

	cmd := newMFABackupCodesGenerateCommand()
	mfaUserID = "user_001"
	globalTenantID = "tenant_test"
	iamForce = false

	err := runMFABackupCodesGenerateCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// MFA Status - JSON Output
// =============================================================================

func TestMFAStatus_JSONOutput(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newMFAStatusCommand()
	mfaUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runMFAStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestMFABackupCodesStatus_JSONOutput(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newMFABackupCodesStatusCommand()
	mfaUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runMFABackupCodesStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Federation Provider Delete - Confirmation Prompt Paths
// =============================================================================

func TestIAMProviderDelete_ConfirmYes(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "y\n")
	defer restoreStdin()

	cmd := newIAMProviderDeleteCommand()
	providerID = "provider_test"
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMProviderDeleteCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMProviderDelete_ConfirmNo(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "n\n")
	defer restoreStdin()

	cmd := newIAMProviderDeleteCommand()
	providerID = "provider_test"
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMProviderDeleteCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMProviderDelete_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMProviderDeleteCommand()
	providerID = "provider_test"
	globalTenantID = ""
	iamForce = true

	err := runIAMProviderDeleteCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

// =============================================================================
// Federation Identity Unlink - Confirmation Prompt
// =============================================================================

func TestIAMIdentityUnlink_ConfirmYes(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "y\n")
	defer restoreStdin()

	cmd := newIAMIdentityUnlinkCommand()
	identityUserID = "user_001"
	identityProviderID = "provider_test"
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMIdentityUnlinkCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMIdentityUnlink_ConfirmNo(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "n\n")
	defer restoreStdin()

	cmd := newIAMIdentityUnlinkCommand()
	identityUserID = "user_001"
	identityProviderID = "provider_test"
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMIdentityUnlinkCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMIdentityUnlink_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMIdentityUnlinkCommand()
	identityUserID = "user_001"
	identityProviderID = "provider_test"
	globalTenantID = ""
	iamForce = true

	err := runIAMIdentityUnlinkCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

// =============================================================================
// Federation Identity List - JSON & Missing Tenant
// =============================================================================

func TestIAMIdentityList_JSONOutput(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMIdentityListCommand()
	identityUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMIdentityListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMIdentityList_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMIdentityListCommand()
	identityUserID = "user_001"
	globalTenantID = ""

	err := runIAMIdentityListCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

// =============================================================================
// Federation Provider List - JSON, Empty, Missing Tenant
// =============================================================================

func TestIAMProviderList_JSONOutput(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMProviderListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMProviderListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMProviderList_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMProviderListCommand()
	globalTenantID = ""

	err := runIAMProviderListCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

func TestIAMProviderList_EmptyResults(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.ListProvidersFunc = func(ctx context.Context, tenantID string) ([]*cli.Provider, error) {
		return []*cli.Provider{}, nil
	}

	cmd := newIAMProviderListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMProviderListCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Federation Provider Get - JSON & Missing Tenant
// =============================================================================

func TestIAMProviderGet_JSONOutput(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMProviderGetCommand()
	providerID = "provider_test"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMProviderGetCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMProviderGet_MissingTenant(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMProviderGetCommand()
	providerID = "provider_test"
	globalTenantID = ""

	err := runIAMProviderGetCommand(cmd, []string{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant flag is required")
}

// =============================================================================
// Group Delete - Confirmation Prompt Paths
// =============================================================================

// =============================================================================
// Group List - JSON & Empty
// =============================================================================

// =============================================================================
// Role Delete - Confirmation Prompt Paths
// =============================================================================

func TestIAMRoleDelete_ConfirmYes(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "y\n")
	defer restoreStdin()

	cmd := newIAMRoleDeleteCommand()
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMRoleDeleteCommand(cmd, []string{"role_test"})
	assert.NoError(t, err)
}

func TestIAMRoleDelete_ConfirmNo(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "n\n")
	defer restoreStdin()

	cmd := newIAMRoleDeleteCommand()
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMRoleDeleteCommand(cmd, []string{"role_test"})
	assert.NoError(t, err)
}

// =============================================================================
// Role List - JSON & Empty
// =============================================================================

func TestIAMRoleList_JSONOutput(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMRoleListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMRoleListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMRoleList_EmptyResults(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.roleClient.ListFunc = func(ctx context.Context, tenantID string, req *iam.ListRolesRequest) (*iam.ListRolesResponse, error) {
		return &iam.ListRolesResponse{Roles: []iam.Role{}}, nil
	}

	cmd := newIAMRoleListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMRoleListCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Tenant Delete - Confirmation Prompt Paths
// =============================================================================

func TestIAMTenantDelete_ConfirmYes(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "y\n")
	defer restoreStdin()

	cmd := newIAMTenantDeleteCommand()
	iamForce = false

	err := runIAMTenantDeleteCommand(cmd, []string{"tenant_test"})
	assert.NoError(t, err)
}

func TestIAMTenantDelete_ConfirmNo(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "n\n")
	defer restoreStdin()

	cmd := newIAMTenantDeleteCommand()
	iamForce = false

	err := runIAMTenantDeleteCommand(cmd, []string{"tenant_test"})
	assert.NoError(t, err)
}

// =============================================================================
// Tenant List - JSON & Empty
// =============================================================================

func TestIAMTenantList_JSONOutput(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMTenantListCommand()
	iamOutputFormat = "json"

	err := runIAMTenantListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMTenantList_EmptyResults(t *testing.T) {
	mockClient, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	mockClient.tenantClient.ListFunc = func(ctx context.Context, req *iam.ListTenantsRequest) (*iam.ListTenantsResponse, error) {
		return &iam.ListTenantsResponse{Tenants: []iam.Tenant{}}, nil
	}

	cmd := newIAMTenantListCommand()
	iamOutputFormat = "table"

	err := runIAMTenantListCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Tenant Get - JSON Output
// =============================================================================

func TestIAMTenantGet_JSONOutput(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMTenantGetCommand()
	iamOutputFormat = "json"

	err := runIAMTenantGetCommand(cmd, []string{"tenant_test"})
	assert.NoError(t, err)
}

// =============================================================================
// User Delete - Confirmation Prompt Paths
// =============================================================================

func TestIAMUserDelete_ConfirmYes(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "y\n")
	defer restoreStdin()

	cmd := newIAMUserDeleteCommand()
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMUserDeleteCommand(cmd, []string{"user_test"})
	assert.NoError(t, err)
}

func TestIAMUserDelete_ConfirmNo(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()
	restoreStdin := withStdinInput(t, "n\n")
	defer restoreStdin()

	cmd := newIAMUserDeleteCommand()
	globalTenantID = "tenant_test"
	iamForce = false

	err := runIAMUserDeleteCommand(cmd, []string{"user_test"})
	assert.NoError(t, err)
}

// =============================================================================
// User List - JSON Output
// =============================================================================

func TestIAMUserList_JSONOutput(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMUserListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMUserListCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Tenant Commands - Success Paths
// =============================================================================

func TestIAMTenantCreate_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMTenantCreateCommand()
	tenantName = "Acme Corporation"
	tenantMaxUsers = 100
	tenantMFARequired = false
	iamOutputFormat = "table"

	err := runIAMTenantCreateCommand(cmd, []string{"acme-corp"})
	assert.NoError(t, err)
}

func TestIAMTenantCreate_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMTenantCreateCommand()
	tenantName = "Acme Corporation"
	tenantMaxUsers = 0
	tenantMFARequired = true
	iamOutputFormat = "json"

	err := runIAMTenantCreateCommand(cmd, []string{"acme-corp"})
	assert.NoError(t, err)
}

func TestIAMTenantGet_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMTenantGetCommand()
	iamOutputFormat = "table"

	err := runIAMTenantGetCommand(cmd, []string{"acme-corp"})
	assert.NoError(t, err)
}

func TestIAMTenantGet_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMTenantGetCommand()
	iamOutputFormat = "json"

	err := runIAMTenantGetCommand(cmd, []string{"acme-corp"})
	assert.NoError(t, err)
}

func TestIAMTenantUpdate_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMTenantUpdateCommand()
	cmd.Flags().Set("name", "Updated Acme Corp")
	iamOutputFormat = "table"

	err := runIAMTenantUpdateCommand(cmd, []string{"acme-corp"})
	assert.NoError(t, err)
}

func TestIAMTenantUpdate_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMTenantUpdateCommand()
	cmd.Flags().Set("name", "Updated Acme")
	cmd.Flags().Set("mfa-required", "true")
	cmd.Flags().Set("max-users", "500")
	iamOutputFormat = "json"

	err := runIAMTenantUpdateCommand(cmd, []string{"acme-corp"})
	assert.NoError(t, err)
}

func TestIAMTenantDelete_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMTenantDeleteCommand()
	iamForce = true

	err := runIAMTenantDeleteCommand(cmd, []string{"acme-corp"})
	assert.NoError(t, err)
}

func TestIAMTenantList_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMTenantListCommand()
	iamOutputFormat = "table"

	err := runIAMTenantListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMTenantList_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMTenantListCommand()
	iamOutputFormat = "json"

	err := runIAMTenantListCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// User Commands - Success Paths
// =============================================================================

func TestIAMUserCreate_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMUserCreateCommand()
	iamUserName = "Test User"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"
	iamUserRoles = nil

	err := runIAMUserCreateCommand(cmd, []string{"user@example.com"})
	assert.NoError(t, err)
}

func TestIAMUserCreate_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMUserCreateCommand()
	iamUserName = "Test User JSON"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"
	iamUserRoles = []string{"admin"}

	err := runIAMUserCreateCommand(cmd, []string{"user@example.com"})
	assert.NoError(t, err)
}

func TestIAMUserGet_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMUserGetCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMUserGetCommand(cmd, []string{"user_001"})
	assert.NoError(t, err)
}

func TestIAMUserGet_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMUserGetCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMUserGetCommand(cmd, []string{"user_001"})
	assert.NoError(t, err)
}

func TestIAMUserUpdate_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMUserUpdateCommand()
	cmd.Flags().Set("name", "Updated Name")
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMUserUpdateCommand(cmd, []string{"user_001"})
	assert.NoError(t, err)
}

func TestIAMUserUpdate_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMUserUpdateCommand()
	cmd.Flags().Set("name", "Updated Name JSON")
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMUserUpdateCommand(cmd, []string{"user_001"})
	assert.NoError(t, err)
}

func TestIAMUserDelete_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMUserDeleteCommand()
	globalTenantID = "tenant_test"
	iamForce = true

	err := runIAMUserDeleteCommand(cmd, []string{"user_001"})
	assert.NoError(t, err)
}

func TestIAMUserList_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMUserListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMUserListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMUserList_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMUserListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMUserListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMUserRoleAssign_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMUserRoleAssignCommand()
	iamUserRole = "admin"
	iamUserName = "user_001"
	globalTenantID = "tenant_test"

	err := runIAMUserRoleAssignCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMUserRoleRevoke_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMUserRoleRevokeCommand()
	iamUserRole = "admin"
	iamUserName = "user_001"
	globalTenantID = "tenant_test"

	err := runIAMUserRoleRevokeCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Group Commands - Success Paths
// =============================================================================

// =============================================================================
// Role Commands - Success Paths
// =============================================================================

func TestIAMRoleCreate_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMRoleCreateCommand()
	iamRoleName = "Team Lead"
	iamRoleDescription = "Can manage team resources"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMRoleCreateCommand(cmd, []string{"team-lead"})
	assert.NoError(t, err)
}

func TestIAMRoleCreate_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMRoleCreateCommand()
	iamRoleName = "Team Lead"
	iamRoleDescription = ""
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMRoleCreateCommand(cmd, []string{"team-lead"})
	assert.NoError(t, err)
}

func TestIAMRoleGet_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMRoleGetCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMRoleGetCommand(cmd, []string{"admin"})
	assert.NoError(t, err)
}

func TestIAMRoleGet_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMRoleGetCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMRoleGetCommand(cmd, []string{"admin"})
	assert.NoError(t, err)
}

func TestIAMRoleUpdate_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMRoleUpdateCommand()
	cmd.Flags().Set("name", "Updated Role")
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMRoleUpdateCommand(cmd, []string{"admin"})
	assert.NoError(t, err)
}

func TestIAMRoleUpdate_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMRoleUpdateCommand()
	cmd.Flags().Set("description", "Updated description")
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMRoleUpdateCommand(cmd, []string{"admin"})
	assert.NoError(t, err)
}

func TestIAMRoleDelete_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMRoleDeleteCommand()
	globalTenantID = "tenant_test"
	iamForce = true

	err := runIAMRoleDeleteCommand(cmd, []string{"admin"})
	assert.NoError(t, err)
}

func TestIAMRoleList_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMRoleListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMRoleListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMRoleList_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMRoleListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMRoleListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMRoleAssign_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMRoleAssignCommand()
	iamRoleID = "admin"
	iamRoleUserID = "user_001"
	globalTenantID = "tenant_test"

	err := runIAMRoleAssignCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMRoleAssign_SuccessWithArgs(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMRoleAssignCommand()
	iamRoleID = ""
	iamRoleUserID = ""
	globalTenantID = "tenant_test"

	err := runIAMRoleAssignCommand(cmd, []string{"user_001", "admin"})
	assert.NoError(t, err)
}

func TestIAMRoleRevoke_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMRoleRevokeCommand()
	iamRoleID = "admin"
	iamRoleUserID = "user_001"
	globalTenantID = "tenant_test"
	iamForce = true

	err := runIAMRoleRevokeCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Federation - Provider Commands - Success Paths
// =============================================================================

func TestIAMProviderCreate_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMProviderCreateCommand()
	providerName = "Google OIDC"
	providerID = "google-oidc"
	providerType = "oidc"
	providerClientID = "client_abc"
	providerSecret = "secret_xyz"
	providerIssuerURL = "https://accounts.google.com"
	providerScopes = []string{"openid", "profile", "email"}
	providerPreset = ""
	providerDomain = ""
	providerEnabled = true
	autoProvision = true
	allowLinking = true
	defaultRoles = []string{"user"}
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMProviderCreateCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMProviderCreate_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMProviderCreateCommand()
	providerName = "GitHub"
	providerID = "github-oauth"
	providerType = "oauth2"
	providerClientID = "client_github"
	providerSecret = "secret_github"
	providerIssuerURL = "https://github.com"
	providerScopes = []string{"read:user"}
	providerPreset = ""
	providerDomain = ""
	providerEnabled = true
	autoProvision = false
	allowLinking = false
	defaultRoles = nil
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMProviderCreateCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMProviderGet_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMProviderGetCommand()
	providerID = "provider_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMProviderGetCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMProviderGet_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMProviderGetCommand()
	providerID = "provider_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMProviderGetCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMProviderUpdate_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMProviderUpdateCommand()
	cmd.Flags().Set("name", "Updated Provider")
	providerID = "provider_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMProviderUpdateCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMProviderDelete_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMProviderDeleteCommand()
	providerID = "provider_001"
	globalTenantID = "tenant_test"
	iamForce = true

	err := runIAMProviderDeleteCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMProviderList_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMProviderListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMProviderListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMProviderList_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMProviderListCommand()
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMProviderListCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// Federation - Identity Commands - Success Paths
// =============================================================================

func TestIAMIdentityLink_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMIdentityLinkCommand()
	identityUserID = "user_001"
	identityProviderID = "provider_001"
	identityAuthCode = "auth_code_abc"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMIdentityLinkCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMIdentityLink_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMIdentityLinkCommand()
	identityUserID = "user_001"
	identityProviderID = "provider_001"
	identityAuthCode = "auth_code_abc"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMIdentityLinkCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMIdentityUnlink_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMIdentityUnlinkCommand()
	identityUserID = "user_001"
	identityProviderID = "provider_001"
	globalTenantID = "tenant_test"
	iamForce = true

	err := runIAMIdentityUnlinkCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMIdentityList_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMIdentityListCommand()
	identityUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runIAMIdentityListCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestIAMIdentityList_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newIAMIdentityListCommand()
	identityUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runIAMIdentityListCommand(cmd, []string{})
	assert.NoError(t, err)
}

// =============================================================================
// MFA Commands - Success Paths
// =============================================================================

func TestMFAStatus_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newMFAStatusCommand()
	mfaUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runMFAStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestMFAStatus_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newMFAStatusCommand()
	mfaUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runMFAStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestMFAEnroll_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newMFAEnrollCommand()
	mfaUserID = "user_001"
	mfaMethod = "totp"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runMFAEnrollCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestMFAEnroll_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newMFAEnrollCommand()
	mfaUserID = "user_001"
	mfaMethod = "totp"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runMFAEnrollCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestMFAVerify_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newMFAVerifyCommand()
	mfaCode = "123456"

	err := runMFAVerifyCommand(cmd, []string{"enroll_001"})
	assert.NoError(t, err)
}

func TestMFADisable_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newMFADisableCommand()
	mfaUserID = "user_001"
	globalTenantID = "tenant_test"
	iamForce = true

	err := runMFADisableCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestMFABackupCodesStatus_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newMFABackupCodesStatusCommand()
	mfaUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "table"

	err := runMFABackupCodesStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestMFABackupCodesStatus_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newMFABackupCodesStatusCommand()
	mfaUserID = "user_001"
	globalTenantID = "tenant_test"
	iamOutputFormat = "json"

	err := runMFABackupCodesStatusCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestMFABackupCodesGenerate_Success(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newMFABackupCodesGenerateCommand()
	mfaUserID = "user_001"
	globalTenantID = "tenant_test"
	iamForce = true
	iamOutputFormat = "table"

	err := runMFABackupCodesGenerateCommand(cmd, []string{})
	assert.NoError(t, err)
}

func TestMFABackupCodesGenerate_SuccessJSON(t *testing.T) {
	_, cleanup := setupIAMMock(t)
	defer cleanup()
	cleanupLog := testInitLogger(t)
	defer cleanupLog()

	cmd := newMFABackupCodesGenerateCommand()
	mfaUserID = "user_001"
	globalTenantID = "tenant_test"
	iamForce = true
	iamOutputFormat = "json"

	err := runMFABackupCodesGenerateCommand(cmd, []string{})
	assert.NoError(t, err)
}
