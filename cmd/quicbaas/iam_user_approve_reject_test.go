package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// withIAMUserTestState saves and restores the globals touched by the
// approve/reject command run-funcs so tests don't leak state.
func withIAMUserTestState(t *testing.T) {
	t.Helper()
	cleanup := testInitLogger(t)
	originalFactory := iamClientFactory
	originalTenantID := globalTenantID
	originalReason := iamUserRejectReason
	t.Cleanup(func() {
		iamClientFactory = originalFactory
		globalTenantID = originalTenantID
		iamUserRejectReason = originalReason
		cleanup()
	})
}

func TestRunIAMUserApproveCommand_RequiresTenant(t *testing.T) {
	withIAMUserTestState(t)
	globalTenantID = ""

	err := runIAMUserApproveCommand(newIAMUserApproveCommand(), []string{"user_abc"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant")
}

func TestRunIAMUserApproveCommand_FactoryError(t *testing.T) {
	withIAMUserTestState(t)
	globalTenantID = "acme"
	wantErr := errors.Newf(errors.ErrInternal, "iam", "factory boom")
	iamClientFactory = func() (iam.Client, error) { return nil, wantErr }

	err := runIAMUserApproveCommand(newIAMUserApproveCommand(), []string{"user_abc"})
	require.Error(t, err)
	assert.Equal(t, wantErr, err)
}

func TestRunIAMUserApproveCommand_Success(t *testing.T) {
	withIAMUserTestState(t)
	globalTenantID = "acme"

	var gotTenant, gotUser string
	mockClient := newTestMockIAMClient()
	mockClient.userClient.ApproveFunc = func(_ context.Context, tenantID, userID string) error {
		gotTenant, gotUser = tenantID, userID
		return nil
	}
	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }

	err := runIAMUserApproveCommand(newIAMUserApproveCommand(), []string{"user_abc"})
	require.NoError(t, err)
	assert.Equal(t, "acme", gotTenant)
	assert.Equal(t, "user_abc", gotUser)
}

func TestRunIAMUserApproveCommand_SDKError(t *testing.T) {
	withIAMUserTestState(t)
	globalTenantID = "acme"

	mockClient := newTestMockIAMClient()
	mockClient.userClient.ApproveFunc = func(_ context.Context, _, _ string) error {
		return errors.Newf(errors.ErrNotFound, "iam", "no such user")
	}
	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }

	err := runIAMUserApproveCommand(newIAMUserApproveCommand(), []string{"missing"})
	require.Error(t, err)
}

func TestRunIAMUserRejectCommand_RequiresTenant(t *testing.T) {
	withIAMUserTestState(t)
	globalTenantID = ""

	err := runIAMUserRejectCommand(newIAMUserRejectCommand(), []string{"user_abc"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant")
}

func TestRunIAMUserRejectCommand_FactoryError(t *testing.T) {
	withIAMUserTestState(t)
	globalTenantID = "acme"
	wantErr := errors.Newf(errors.ErrInternal, "iam", "factory boom")
	iamClientFactory = func() (iam.Client, error) { return nil, wantErr }

	err := runIAMUserRejectCommand(newIAMUserRejectCommand(), []string{"user_abc"})
	require.Error(t, err)
	assert.Equal(t, wantErr, err)
}

func TestRunIAMUserRejectCommand_SuccessPassesReason(t *testing.T) {
	withIAMUserTestState(t)
	globalTenantID = "acme"

	var gotTenant, gotUser, gotReason string
	mockClient := newTestMockIAMClient()
	mockClient.userClient.RejectFunc = func(_ context.Context, tenantID, userID, reason string) error {
		gotTenant, gotUser, gotReason = tenantID, userID, reason
		return nil
	}
	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }

	// newIAMUserRejectCommand binds the --reason flag to the global, resetting
	// it to "". Set the reason after construction to mirror flag parsing.
	cmd := newIAMUserRejectCommand()
	iamUserRejectReason = "policy violation"

	err := runIAMUserRejectCommand(cmd, []string{"user_abc"})
	require.NoError(t, err)
	assert.Equal(t, "acme", gotTenant)
	assert.Equal(t, "user_abc", gotUser)
	assert.Equal(t, "policy violation", gotReason)
}

func TestRunIAMUserRejectCommand_SDKError(t *testing.T) {
	withIAMUserTestState(t)
	globalTenantID = "acme"

	mockClient := newTestMockIAMClient()
	mockClient.userClient.RejectFunc = func(_ context.Context, _, _, _ string) error {
		return errors.Newf(errors.ErrNotFound, "iam", "no such user")
	}
	iamClientFactory = func() (iam.Client, error) { return mockClient, nil }

	err := runIAMUserRejectCommand(newIAMUserRejectCommand(), []string{"missing"})
	require.Error(t, err)
}
