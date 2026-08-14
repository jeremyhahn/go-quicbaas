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

package unix

import (
	"context"
	"strings"
	"testing"
	"time"

	projectclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/project"
)

// TestTransport_Project_Delegates proves the Unix transport routes the project
// surface through the gRPC project handler.
//
// Unix is gRPC on a domain socket and is the CLI's default transport, so a
// missing gRPC project handler surfaced as
// "grpc: error while marshaling: ... want proto.Message" on every
// `quicbaas project ...` invocation. With no server listening and a short
// deadline every call here fails on connection — but crucially NOT with a
// marshaling or unsupported-method error, which is the regression this guards.
// The gRPC handler tests assert the full proto round trip.
func TestTransport_Project_Delegates(t *testing.T) {
	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"create", "Create", &projectclient.CreateProjectRequest{TenantID: "t1", Name: "p1"}},
		{"get", "Get", &projectclient.GetProjectRequest{TenantID: "t1", ProjectID: "p1"}},
		{"get-by-name", "GetByName", &projectclient.GetProjectByNameRequest{TenantID: "t1", Name: "p1"}},
		{"update", "Update", &projectclient.UpdateProjectRequest{TenantID: "t1", ProjectID: "p1"}},
		{"delete", "Delete", &projectclient.DeleteProjectRequest{TenantID: "t1", ProjectID: "p1"}},
		{"list", "List", &projectclient.ListProjectsRequest{TenantID: "t1"}},
		{"grant-agent-role", "GrantAgentRole", &projectclient.GrantAgentRoleRequest{
			TenantID: "t1", ProjectID: "p1", AgentID: "a1", Role: projectclient.ProjectRoleLead,
		}},
		{"revoke-agent-role", "RevokeAgentRole", &projectclient.RevokeAgentRoleRequest{
			TenantID: "t1", ProjectID: "p1", AgentID: "a1",
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tr := newWiredTransport(t)
			defer func() { _ = tr.Close() }()

			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()

			_, err := tr.Request(ctx, "quicbaas.Project", tc.method, tc.req)
			if err == nil {
				t.Fatal("expected a connection error: no server is listening")
			}
			msg := err.Error()
			if strings.Contains(msg, "want proto.Message") || strings.Contains(msg, "error while marshaling") {
				t.Fatalf("project request reached the untyped invoke path, dispatch regression: %v", err)
			}
			if strings.Contains(msg, "not supported") || strings.Contains(msg, "not yet supported") {
				t.Fatalf("project request was rejected as unsupported: %v", err)
			}
			if strings.Contains(msg, "unknown project method") {
				t.Fatalf("project method %q is not in the gRPC dispatch table: %v", tc.method, err)
			}
		})
	}
}
