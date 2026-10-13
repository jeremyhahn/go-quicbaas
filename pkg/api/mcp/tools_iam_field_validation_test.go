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
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// TestMCP_UserCreateMalformedEmailNamesTheField pins that the MCP tool hands
// back the identity store's field validation error with the field intact. MCP
// has no status code to get wrong (go-quicbaas#335); the field name is what a
// caller has to act on.
func TestMCP_UserCreateMalformedEmailNamesTheField(t *testing.T) {
	mem := iam.NewMemoryStorage()
	store := iam.NewIdentityStore(iam.IdentityStoreOptions{UserStorage: mem, RoleStorage: mem})
	mock := &mockIAMService{
		// The service layer's path: a pending create (no lifecycle given)
		// through CreateInvitedUser, here with no invite store wired. The
		// request is judged before the wiring, so the email is what is
		// refused, not the missing store (#335).
		createUserFunc: func(ctx context.Context, req *service.IAMCreateUserRequest) (*service.IAMCreateUserResponse, error) {
			user, _, _, err := store.CreateInvitedUser(ctx, req.TenantID, iam.CreateUserOptions{
				Email:    req.Email,
				Username: req.Username,
				Name:     req.Name,
			}, nil, 0)
			if err != nil {
				return nil, err
			}
			return &service.IAMCreateUserResponse{User: user, Timestamp: time.Now()}, nil
		},
	}
	reg := newIAMTestRegistry(mock)

	_, err := reg.executeIAMUserCreate(context.Background(), map[string]interface{}{
		"tenant_id": "acme",
		"email":     "not-an-email",
		"username":  "bob",
	})
	require.Error(t, err)
	var verr *iam.ValidationError
	require.True(t, errors.As(err, &verr), "want *iam.ValidationError in the chain, got %T: %v", err, err)
	assert.Equal(t, "email", verr.Field)
}
