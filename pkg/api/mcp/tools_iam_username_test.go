//go:build mcp

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

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestMCP_UserCreateCarriesTheUsername proves the MCP tool forwards a username
// distinct from the display name. The tool accepted only `name`, so an agent
// asking for one got a user addressable by the other.
func TestMCP_UserCreateCarriesTheUsername(t *testing.T) {
	var captured *service.IAMCreateUserRequest
	mock := &mockIAMService{
		createUserFunc: func(_ context.Context, req *service.IAMCreateUserRequest) (*service.IAMCreateUserResponse, error) {
			captured = req
			return &service.IAMCreateUserResponse{
				User: &iam.User{
					ID: 1, TenantID: req.TenantID, Email: req.Email,
					Username: req.Username, Name: req.Name,
				},
				Timestamp: time.Now(),
			}, nil
		},
	}
	reg := newIAMTestRegistry(mock)

	_, err := reg.executeIAMUserCreate(context.Background(), map[string]interface{}{
		"tenant_id": "acme",
		"email":     "jules@acme.test",
		"username":  "jules",
		"name":      "Jules Verne",
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	assert.Equal(t, "jules", captured.Username, "the MCP create dropped the username")
	assert.Equal(t, "Jules Verne", captured.Name, "the MCP create dropped the display name")
}

// TestMCP_UserUpdateCarriesTheUsername is the same for the update tool, where
// the two names are applied independently.
func TestMCP_UserUpdateCarriesTheUsername(t *testing.T) {
	var captured *service.IAMUpdateUserRequest
	mock := &mockIAMService{
		updateUserFunc: func(_ context.Context, req *service.IAMUpdateUserRequest) (*service.IAMUpdateUserResponse, error) {
			captured = req
			return &service.IAMUpdateUserResponse{
				User:      &iam.User{ID: req.UserID, TenantID: req.TenantID},
				Timestamp: time.Now(),
			}, nil
		},
	}
	reg := newIAMTestRegistry(mock)

	_, err := reg.executeIAMUserUpdate(context.Background(), map[string]interface{}{
		"tenant_id": "acme",
		"user_id":   "1",
		"username":  "jverne",
		"name":      "Jules Two",
	})
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.NotNil(t, captured.Username, "the MCP update dropped the username")
	assert.Equal(t, "jverne", *captured.Username)
	require.NotNil(t, captured.Name)
	assert.Equal(t, "Jules Two", *captured.Name)
}
