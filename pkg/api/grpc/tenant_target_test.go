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

//go:build !integration

package grpc

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// refusedCallers are the callers that may not act on tenant "acme": another
// tenant, the system tenant (no lift, #1150) and a caller with no tenant.
func refusedCallers() map[string]context.Context {
	return map[string]context.Context{
		"another tenant": callerContext("burnin"),
		"system tenant":  tenantctx.WithSystem(context.Background()),
		"no tenant":      context.Background(),
	}
}

func TestRequireCallerTenant_AllowsTheCallersOwnTenant(t *testing.T) {
	require.NoError(t, requireCallerTenant(callerContext("acme"), "grpc.Test", "acme"))
}

func TestRequireCallerTenant_RefusesEveryOtherCaller(t *testing.T) {
	for name, ctx := range refusedCallers() {
		t.Run(name, func(t *testing.T) {
			err := requireCallerTenant(ctx, "grpc.Test", "acme")
			require.Equal(t, codes.PermissionDenied, status.Code(err), "err = %v", err)
		})
	}
}

// TestTenantScopedServers_RefuseATenantThatIsNotTheCallers proves the IAM,
// WebAuthn, federation, project and session servers honour a request's
// tenant_id only for the caller's own tenant. The service doubles fail on any
// call, so a refusal must happen before the service is reached.
func TestTenantScopedServers_RefuseATenantThatIsNotTheCallers(t *testing.T) {
	iamSrv, _ := newTestIAMServer(t)
	federationSrv, _ := newTestFederationServer(t)
	projectSrv := newProjectServer(t, &grpcMockProjectService{})
	sessionSrv, _ := createTestSessionServer(t)
	orchestratorSrv := newSessionEventsServer(t, seedConversationLog(t, "acme", "s1"))

	calls := map[string]func(context.Context) error{
		"iam.ListUsers": func(ctx context.Context) error {
			_, err := iamSrv.ListUsers(ctx, &iampb.ListUsersRequest{TenantId: "acme"})
			return err
		},
		"iam.DeleteUser": func(ctx context.Context) error {
			_, err := iamSrv.DeleteUser(ctx, &iampb.DeleteUserRequest{TenantId: "acme", UserId: "u1"})
			return err
		},
		"iam.ListWebAuthnCredentials": func(ctx context.Context) error {
			_, err := iamSrv.ListWebAuthnCredentials(ctx, &iampb.ListWebAuthnCredentialsRequest{TenantId: "acme", UserId: "u1"})
			return err
		},
		"federation.ListProviders": func(ctx context.Context) error {
			_, err := federationSrv.ListProviders(ctx, &iampb.ListProvidersRequest{TenantId: "acme"})
			return err
		},
		"project.ListProjects": func(ctx context.Context) error {
			_, err := projectSrv.ListProjects(ctx, &pb.ListProjectsRequest{TenantId: "acme"})
			return err
		},
		"session.ListSessions": func(ctx context.Context) error {
			_, err := sessionSrv.ListSessions(ctx, &iampb.ListSessionsRequest{TenantId: "acme"})
			return err
		},
		"orchestrator.SubscribeSessionEvents": func(ctx context.Context) error {
			return orchestratorSrv.SubscribeSessionEvents(
				&pb.SubscribeSessionRequest{SessionId: "s1", TenantId: "acme"},
				&fakeSubscribeStream{ctx: ctx})
		},
		"orchestrator.GetSessionConversation": func(ctx context.Context) error {
			_, err := orchestratorSrv.GetSessionConversation(ctx,
				&pb.GetSessionConversationRequest{SessionId: "s1", TenantId: "acme"})
			return err
		},
	}
	for method, call := range calls {
		for caller, ctx := range refusedCallers() {
			t.Run(method+"/"+caller, func(t *testing.T) {
				err := call(ctx)
				require.Equal(t, codes.PermissionDenied, status.Code(err), "err = %v", err)
			})
		}
	}
}
