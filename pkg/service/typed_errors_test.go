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

package service_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Agent admin typed-error message contracts.
//
// Handlers surface these messages to clients (they encode tenant/agent IDs),
// so the exact format is part of the public contract and must not drift.
// =============================================================================

func TestAgentAdminTypedErrors_Messages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "AgentAdminNotFoundError",
			err:  &service.AgentAdminNotFoundError{TenantID: "t1", AgentID: "a1"},
			want: "agent not found: tenant=t1 agent=a1",
		},
		{
			name: "AgentAlreadyDisabledError",
			err:  &service.AgentAlreadyDisabledError{TenantID: "t1", AgentID: "a1"},
			want: "agent already disabled: tenant=t1 agent=a1",
		},
		{
			name: "AgentNotDisabledError",
			err:  &service.AgentNotDisabledError{TenantID: "t1", AgentID: "a1"},
			want: "agent not disabled: tenant=t1 agent=a1",
		},
		{
			name: "AgentDisabledError",
			err:  &service.AgentDisabledError{TenantID: "t1", AgentID: "a1"},
			want: "agent is disabled: tenant=t1 agent=a1",
		},
		{
			name: "ErrAgentStreamCloseNotImplemented",
			err:  &service.ErrAgentStreamCloseNotImplemented{AgentID: "a1"},
			want: "agent stream close not implemented: no stream registry wired for agent=a1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.EqualError(t, tc.err, tc.want)
		})
	}
}

// =============================================================================
// Project typed-error message contracts, including the conditional branches
// in ProjectNotFoundError and ProjectInvalidError.
// =============================================================================

func TestProjectTypedErrors_Messages(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "ProjectNotFoundError by name",
			err:  &service.ProjectNotFoundError{TenantID: "t1", Name: "proj"},
			want: "project not found: tenant=t1 name=proj",
		},
		{
			name: "ProjectNotFoundError by id",
			err:  &service.ProjectNotFoundError{TenantID: "t1", ProjectID: "p1"},
			want: "project not found: tenant=t1 id=p1",
		},
		{
			name: "ProjectAlreadyExistsError",
			err:  &service.ProjectAlreadyExistsError{TenantID: "t1", Name: "proj"},
			want: "project already exists: tenant=t1 name=proj",
		},
		{
			name: "ProjectInvalidError with field",
			err:  &service.ProjectInvalidError{Field: "name", Message: "required"},
			want: "project invalid: name: required",
		},
		{
			name: "ProjectInvalidError without field",
			err:  &service.ProjectInvalidError{Message: "bad request"},
			want: "project invalid: bad request",
		},
		{
			name: "ProjectAccessDeniedError",
			err: &service.ProjectAccessDeniedError{
				ProjectID:    "p1",
				AgentID:      "a1",
				RequiredRole: orchestration.ProjectRoleLead,
				ActualRole:   orchestration.ProjectRoleObserver,
			},
			want: "project access denied: project=p1 agent=a1 required=lead actual=observer",
		},
		{
			name: "ProjectRoleInvalidError",
			err:  &service.ProjectRoleInvalidError{Role: "bogus"},
			want: "project role invalid: bogus",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.EqualError(t, tc.err, tc.want)
		})
	}
}

// =============================================================================
// Agent certificate issuer typed errors, including the Unwrap contract used
// by errors.Is / errors.As at the handler layer.
// =============================================================================

func TestAgentCertIssuerTypedErrors_Messages(t *testing.T) {
	assert.EqualError(t,
		&service.AgentCertRequestFieldError{Field: "tenant_id"},
		"agent cert issuer: field tenant_id is required",
	)

	cause := errors.New("ca offline")
	issueErr := &service.AgentCertIssueError{Op: "issue agent certificate", Cause: cause}
	assert.EqualError(t, issueErr, "agent cert issuer: issue agent certificate: ca offline")

	// Unwrap must expose the wrapped cause so callers can match it.
	assert.ErrorIs(t, issueErr, cause)
	assert.Equal(t, cause, errors.Unwrap(issueErr))
}

// =============================================================================
// Agent deploy typed errors, including the Unwrap contract.
// =============================================================================

func TestAgentDeployTypedErrors_Messages(t *testing.T) {
	cause := errors.New("sftp closed")
	uploadErr := &service.AgentIdentityUploadError{File: "agent.crt", Cause: cause}
	assert.EqualError(t, uploadErr, "agent deploy: upload agent.crt: sftp closed")

	// Unwrap must expose the wrapped cause for errors.Is matching.
	assert.ErrorIs(t, uploadErr, cause)
	assert.Equal(t, cause, errors.Unwrap(uploadErr))
}
