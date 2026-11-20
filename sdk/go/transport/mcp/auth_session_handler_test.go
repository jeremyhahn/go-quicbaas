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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/auth"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// sessionViewResult is a hash-free session view as the
// quicbaas_iam_session_* tools return it.
func sessionViewResult(id string) map[string]interface{} {
	return map[string]interface{}{
		"id":           id,
		"user_id":      "10001",
		"tenant_id":    "tenant-1",
		"mfa_verified": true,
		"device_info":  map[string]interface{}{"name": "laptop"},
		"ip_address":   "192.0.2.1",
		"user_agent":   "ua",
		"created":      "2026-10-26T10:00:00Z",
		"last_active":  "2026-10-26T10:05:00Z",
		"expires_at":   "2026-10-26T11:00:00Z",
		"metadata":     map[string]interface{}{"auth_method": "piv"},
	}
}

func TestAuthSessionVerbs_RouteToSelfServiceTools(t *testing.T) {
	for method, want := range map[string]string{
		"GetSession":        "quicbaas_iam_session_get",
		"ListSessions":      "quicbaas_iam_session_list",
		"RevokeSession":     "quicbaas_iam_session_revoke",
		"RevokeAllSessions": "quicbaas_iam_session_revoke_all",
	} {
		if got := ToolName("quicbaas.Auth", method); got != want {
			t.Errorf("ToolName(Auth, %s) = %q, want %q", method, got, want)
		}
		if _, ok := authSessionTransforms[method]; !ok {
			t.Errorf("%s has a tool name but no response transform", method)
		}
	}
}

func TestGetSessionOverMCP_DecodesView(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_iam_session_get",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["session_id"] != "sess-1" {
				t.Errorf("server received %v", args)
			}
		},
		result: sessionViewResult("sess-1"),
	})
	defer cleanup()

	got, err := auth.NewClient(tr).GetSession(context.Background(), "sess-1")
	if err != nil {
		t.Fatalf("GetSession: %v", err)
	}
	if got.ID != "sess-1" || got.UserID != "10001" || got.TenantID != "tenant-1" {
		t.Errorf("identity fields = %+v", got)
	}
	if got.AuthMethod != "piv" || got.DeviceInfo != "laptop" || !got.MFAVerified {
		t.Errorf("detail fields = %+v", got)
	}
	if !got.CreatedAt.Equal(time.Date(2026, 10, 26, 10, 0, 0, 0, time.UTC)) {
		t.Errorf("CreatedAt = %v", got.CreatedAt)
	}
}

func TestGetSessionOverMCP_RefusalIsForbidden(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:      "quicbaas_iam_session_get",
		toolError:     true,
		toolErrorCode: MCPPermissionDenied,
		rawText:       "insufficient permissions",
	})
	defer cleanup()

	_, err := auth.NewClient(tr).GetSession(context.Background(), "someone-elses")
	if !errors.IsForbidden(err) {
		t.Errorf("expected a forbidden error for another user's session, got %v", err)
	}
}

func TestListSessionsOverMCP_DecodesViews(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_iam_session_list",
		result: map[string]interface{}{
			"sessions": []interface{}{sessionViewResult("sess-1"), sessionViewResult("sess-2")},
			"count":    2,
		},
	})
	defer cleanup()

	got, err := auth.NewClient(tr).ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(got.Sessions) != 2 || got.Sessions[0].ID != "sess-1" || got.Sessions[1].ID != "sess-2" {
		t.Fatalf("sessions = %+v", got.Sessions)
	}
	if got.Sessions[1].UserID != "10001" {
		t.Errorf("UserID = %q", got.Sessions[1].UserID)
	}
}

func TestListSessionsOverMCP_EmptyList(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_iam_session_list",
		result:   map[string]interface{}{"sessions": []interface{}{}, "count": 0},
	})
	defer cleanup()

	got, err := auth.NewClient(tr).ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if got == nil || len(got.Sessions) != 0 {
		t.Errorf("expected an empty list, got %+v", got)
	}
}

func TestRevokeSessionOverMCP(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_iam_session_revoke",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["session_id"] != "sess-1" {
				t.Errorf("server received %v", args)
			}
		},
		result: map[string]interface{}{"success": true, "session_id": "sess-1"},
	})
	defer cleanup()

	if err := auth.NewClient(tr).RevokeSession(context.Background(), "sess-1"); err != nil {
		t.Fatalf("RevokeSession: %v", err)
	}
}

func TestRevokeAllSessionsOverMCP(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_iam_session_revoke_all",
		result:   map[string]interface{}{"success": true},
	})
	defer cleanup()

	if err := auth.NewClient(tr).RevokeAllSessions(context.Background()); err != nil {
		t.Fatalf("RevokeAllSessions: %v", err)
	}
}

func TestDecodeMCPAuthSession_MalformedIsInternal(t *testing.T) {
	_, err := decodeMCPAuthSession("GetSession", map[string]interface{}{"user_id": "not-a-number"})
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected internal error for a malformed view, got %v", err)
	}
	_, err = decodeMCPAuthSession("GetSession", nil)
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected internal error for an empty result, got %v", err)
	}
}

func TestDecodeMCPAuthSessionList_MalformedIsInternal(t *testing.T) {
	_, err := decodeMCPAuthSessionList("ListSessions", map[string]interface{}{"sessions": "nope"})
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected internal error for a malformed list, got %v", err)
	}
	_, err = decodeMCPAuthSessionList("ListSessions", nil)
	if errors.Code(err) != errors.CodeInternal {
		t.Errorf("expected internal error for an empty result, got %v", err)
	}
}
