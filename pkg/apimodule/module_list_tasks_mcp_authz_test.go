//go:build rest && mcp

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

package apimodule

import (
	"errors"
	"testing"

	"github.com/jeremyhahn/go-qrdb/pkg/auth/rbac"
	"github.com/jeremyhahn/go-qrdb/pkg/permission"
	"github.com/jeremyhahn/go-qrdb/pkg/user"
)

// listTasksTool is the MCP tool the SDK's ListTasks reaches.
const listTasksTool = "quicbaas_orchestrator_list_tasks"

// TestListTasksTool_IsRefusedWithoutTaskList: a caller not granted
// orchestrator:task_list — the permission the gRPC ListTasks RPC declares —
// cannot list tasks over MCP either.
func TestListTasksTool_IsRefusedWithoutTaskList(t *testing.T) {
	cert := agentSessionPeerCert(t, agentSessionTenant, agentSessionCN)
	am := agentSessionMiddleware(map[string]*user.User{
		agentSessionTenant + "/" + agentSessionCN: enrolledAgentSessionUser(t, cert),
	}, permission.Permission("orchestrator:job_list"))
	registry := agentSessionToolRegistry(t)
	ctx, _ := agentSessionCallContext(t, am, cert)

	err := am.AuthorizeMCP(ctx, registry, listTasksTool, nil)
	var denied *rbac.PermissionDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("AuthorizeMCP = %v, want a *rbac.PermissionDeniedError", err)
	}
}

// TestListTasksTool_IsAdmittedWithTaskList is the other half: the same
// caller granted orchestrator:task_list is admitted.
func TestListTasksTool_IsAdmittedWithTaskList(t *testing.T) {
	cert := agentSessionPeerCert(t, agentSessionTenant, agentSessionCN)
	am := agentSessionMiddleware(map[string]*user.User{
		agentSessionTenant + "/" + agentSessionCN: enrolledAgentSessionUser(t, cert),
	}, permission.Permission("orchestrator:task_list"))
	registry := agentSessionToolRegistry(t)
	ctx, _ := agentSessionCallContext(t, am, cert)

	if _, covered := registry.Lookup(listTasksTool); !covered {
		t.Fatalf("%s is not in the tool registry", listTasksTool)
	}
	if err := am.AuthorizeMCP(ctx, registry, listTasksTool, nil); err != nil {
		t.Errorf("%s must be admitted for a caller granted orchestrator:task_list: %v", listTasksTool, err)
	}
}
