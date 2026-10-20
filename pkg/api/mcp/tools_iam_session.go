//go:build mcp

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

package mcp

import (
	"context"
	"time"

	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Session Manager Access
// =============================================================================

// sessionManagerAccessor provides access to the session manager from the QuicBaaS service.
type sessionManagerAccessor interface {
	GetSessionManager(ctx context.Context) (service.SessionManager, error)
}

// getSessionManager retrieves the session manager from the QuicBaaS service.
func (r *ToolRegistry) getSessionManager(ctx context.Context) (service.SessionManager, error) {
	accessor, ok := r.service.(sessionManagerAccessor)
	if !ok {
		return nil, api.NewServiceUnavailableError("session")
	}
	mgr, err := accessor.GetSessionManager(ctx)
	if err != nil {
		return nil, api.NewOperationFailedError("get session manager", err)
	}
	if mgr == nil {
		return nil, api.NewServiceUnavailableError("session")
	}
	return mgr, nil
}

// =============================================================================
// Session Tools Registration
// =============================================================================

func (r *ToolRegistry) registerSessionTools() {
	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_session_get",
		Description: "Retrieve a session by ID",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Session ID to retrieve",
				},
			},
			"required": []string{"session_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_session_list",
		Description: "List sessions for a user in a tenant",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User ID to list sessions for",
				},
			},
			"required": []string{"tenant_id", "user_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_session_revoke",
		Description: "Revoke a specific session",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Session ID to revoke",
				},
			},
			"required": []string{"session_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_session_revoke_all",
		Description: "Revoke all sessions for a user in a tenant",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User ID whose sessions to revoke",
				},
			},
			"required": []string{"tenant_id", "user_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_session_revoke_others",
		Description: "Revoke all sessions except the specified one",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"tenant_id": map[string]interface{}{
					"type":        "string",
					"description": "Tenant ID",
				},
				"user_id": map[string]interface{}{
					"type":        "string",
					"description": "User ID",
				},
				"keep_session_id": map[string]interface{}{
					"type":        "string",
					"description": "Session ID to keep active",
				},
			},
			"required": []string{"tenant_id", "user_id", "keep_session_id"},
		},
	})

	r.addTool(ToolDefinition{
		Name:        "quicbaas_iam_session_touch",
		Description: "Update the last active timestamp for a session",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"session_id": map[string]interface{}{
					"type":        "string",
					"description": "Session ID to touch",
				},
			},
			"required": []string{"session_id"},
		},
	})
}

// =============================================================================
// Session Execution Handlers
// =============================================================================

func (r *ToolRegistry) executeIAMSessionGet(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionMgr, err := r.getSessionManager(ctx)
	if err != nil {
		return nil, err
	}

	session, err := sessionMgr.Get(ctx, getString(args, "session_id"))
	if err != nil {
		return nil, err
	}
	return marshalResult(session)
}

func (r *ToolRegistry) executeIAMSessionList(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionMgr, err := r.getSessionManager(ctx)
	if err != nil {
		return nil, err
	}

	userID, err := getUserID(args)
	if err != nil {
		return nil, err
	}
	sessions, err := sessionMgr.List(ctx, userID, getString(args, "tenant_id"))
	if err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{
		"sessions":  sessions,
		"timestamp": time.Now(),
	})
}

func (r *ToolRegistry) executeIAMSessionRevoke(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionMgr, err := r.getSessionManager(ctx)
	if err != nil {
		return nil, err
	}

	sessionID := getString(args, "session_id")

	// Verify session exists before attempting revocation.
	_, getErr := sessionMgr.Get(ctx, sessionID)
	if getErr != nil {
		return nil, getErr
	}

	err = sessionMgr.Revoke(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{
		"revoked":   true,
		"timestamp": time.Now(),
	})
}

func (r *ToolRegistry) executeIAMSessionRevokeAll(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionMgr, err := r.getSessionManager(ctx)
	if err != nil {
		return nil, err
	}

	userID, err := getUserID(args)
	if err != nil {
		return nil, err
	}
	err = sessionMgr.RevokeAllForUser(ctx, userID, getString(args, "tenant_id"))
	if err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{
		"revoked":   true,
		"timestamp": time.Now(),
	})
}

func (r *ToolRegistry) executeIAMSessionRevokeOthers(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionMgr, err := r.getSessionManager(ctx)
	if err != nil {
		return nil, err
	}

	userID, err := getUserID(args)
	if err != nil {
		return nil, err
	}
	err = sessionMgr.RevokeAllExcept(ctx,
		userID,
		getString(args, "tenant_id"),
		getString(args, "keep_session_id"),
	)
	if err != nil {
		return nil, err
	}
	return marshalResult(map[string]interface{}{
		"revoked":   true,
		"timestamp": time.Now(),
	})
}

func (r *ToolRegistry) executeIAMSessionTouch(ctx context.Context, args map[string]interface{}) (interface{}, error) {
	sessionID := getString(args, "session_id")
	if sessionID == "" {
		return nil, api.NewRequiredFieldError("session_id")
	}

	sessionMgr, err := r.getSessionManager(ctx)
	if err != nil {
		return nil, err
	}

	// UpdateActivity stamps server time; it refuses and removes an expired
	// session, and its error is returned unchanged.
	if err := sessionMgr.UpdateActivity(ctx, sessionID); err != nil {
		return nil, err
	}

	return marshalResult(map[string]interface{}{
		"touched":   true,
		"timestamp": time.Now(),
	})
}
