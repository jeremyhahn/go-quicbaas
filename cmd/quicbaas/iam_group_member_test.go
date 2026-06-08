// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package main

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// withGroupMemberState saves and restores the group-member globals.
func withGroupMemberState(t *testing.T) {
	t.Helper()
	cleanup := testInitLogger(t)
	origGroup := iamGroupID
	origUser := iamGroupUserID
	origTenant := globalTenantID
	origForce := iamForce
	origFactory := iamClientFactory
	t.Cleanup(func() {
		iamGroupID = origGroup
		iamGroupUserID = origUser
		globalTenantID = origTenant
		iamForce = origForce
		iamClientFactory = origFactory
		cleanup()
	})
	iamGroupID = ""
	iamGroupUserID = ""
	iamForce = true
}

func TestRunIAMGroupAddMember_MissingGroup(t *testing.T) {
	withGroupMemberState(t)
	cmd := newIAMGroupAddMemberCommand()
	globalTenantID = "acme"

	err := runIAMGroupAddMemberCommand(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "group ID is required")
}

func TestRunIAMGroupAddMember_MissingUser(t *testing.T) {
	withGroupMemberState(t)
	cmd := newIAMGroupAddMemberCommand()
	globalTenantID = "acme"

	// Single positional arg sets only the group ID; user ID stays empty.
	err := runIAMGroupAddMemberCommand(cmd, []string{"engineering"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user ID is required")
}

func TestRunIAMGroupAddMember_MissingTenant(t *testing.T) {
	withGroupMemberState(t)
	cmd := newIAMGroupAddMemberCommand()
	globalTenantID = ""

	err := runIAMGroupAddMemberCommand(cmd, []string{"engineering", "user_1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--tenant")
}

func TestRunIAMGroupAddMember_SDKError(t *testing.T) {
	withGroupMemberState(t)
	mock := newTestMockIAMClient()
	mock.groupClient.AddMemberFunc = func(_ context.Context, _, _, _ string) error {
		return errors.New(errors.ErrInternal, "iam", "add failed")
	}
	iamClientFactory = func() (iam.Client, error) { return mock, nil }

	cmd := newIAMGroupAddMemberCommand()
	globalTenantID = "acme"

	require.Error(t, runIAMGroupAddMemberCommand(cmd, []string{"engineering", "user_1"}))
}

func TestRunIAMGroupAddMember_FactoryError(t *testing.T) {
	withGroupMemberState(t)
	iamClientFactory = func() (iam.Client, error) {
		return nil, errors.New(errors.ErrInternal, "iam", "factory boom")
	}

	cmd := newIAMGroupAddMemberCommand()
	globalTenantID = "acme"

	require.Error(t, runIAMGroupAddMemberCommand(cmd, []string{"engineering", "user_1"}))
}

func TestRunIAMGroupRemoveMember_MissingGroup(t *testing.T) {
	withGroupMemberState(t)
	cmd := newIAMGroupRemoveMemberCommand()
	globalTenantID = "acme"

	err := runIAMGroupRemoveMemberCommand(cmd, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "group ID is required")
}

func TestRunIAMGroupRemoveMember_MissingUser(t *testing.T) {
	withGroupMemberState(t)
	cmd := newIAMGroupRemoveMemberCommand()
	globalTenantID = "acme"

	err := runIAMGroupRemoveMemberCommand(cmd, []string{"engineering"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "user ID is required")
}

func TestRunIAMGroupRemoveMember_SDKError(t *testing.T) {
	withGroupMemberState(t)
	mock := newTestMockIAMClient()
	mock.groupClient.RemoveMemberFunc = func(_ context.Context, _, _, _ string) error {
		return errors.New(errors.ErrInternal, "iam", "remove failed")
	}
	iamClientFactory = func() (iam.Client, error) { return mock, nil }

	cmd := newIAMGroupRemoveMemberCommand()
	globalTenantID = "acme"

	require.Error(t, runIAMGroupRemoveMemberCommand(cmd, []string{"engineering", "user_1"}))
}
