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

package grpc

import (
	"context"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
)

// =============================================================================
// MaterializeAccountCredential over gRPC (go-quicbaas#734).
//
// The property under test is that TENANCY COMES FROM THE PRINCIPAL the authz
// interceptor injected, not from the request message — which is what makes the
// release tenant-scoped when the caller is a daemon holding one narrow grant.
// =============================================================================

// grpcDaemonContext returns a context carrying the principal an enrolled agent
// daemon presents, injected exactly the way the authz interceptor injects one.
func grpcDaemonContext(tenantID string) context.Context {
	return auth.SubjectIntoContext(context.Background(), interfaces.Subject{
		ID:            "agent-vm-host-4",
		Username:      "agent-vm-host-4",
		TenantID:      tenantID,
		Authenticated: true,
	})
}

// grpcReleasingService returns an AccountService that releases a recognizable
// envelope and records the request it was asked to release.
func grpcReleasingService(captured **service.MaterializeAccountCredentialRequest) *mocks.MockAccountService {
	return &mocks.MockAccountService{
		MaterializeAccountCredentialFunc: func(
			_ context.Context, req *service.MaterializeAccountCredentialRequest,
		) (*service.MaterializeAccountCredentialResponse, error) {
			if captured != nil {
				*captured = req
			}
			return &service.MaterializeAccountCredentialResponse{
				AccountID:     req.AccountID,
				Provider:      orchestration.AccountProviderClaude,
				DisplayName:   "alice-work",
				RefreshPolicy: orchestration.AccountCredentialModeStaticToken,
				Credential: &orchestration.AccountCredential{
					AccessToken:       "RELEASED",
					AccessTokenEnvKey: "CLAUDE_CODE_OAUTH_TOKEN",
					Env:               map[string]string{"EXTRA": "value"},
					RefreshToken:      "MUST-NOT-TRAVEL",
					TokenEndpoint:     "https://example.invalid/token",
					ClientID:          "MUST-NOT-TRAVEL",
				},
				ExpiresAt: time.Now().UTC().Add(time.Hour),
				UpdatedAt: time.Now().UTC(),
				Timestamp: time.Now().UTC(),
			}, nil
		},
	}
}

// TestAccountGRPC_MaterializeReleasesToTheCallersOwnTenant is the happy path.
func TestAccountGRPC_MaterializeReleasesToTheCallersOwnTenant(t *testing.T) {
	var captured *service.MaterializeAccountCredentialRequest
	srv := newAccountServer(grpcReleasingService(&captured))

	resp, err := srv.MaterializeAccountCredential(grpcDaemonContext("tenant-1"),
		&pb.MaterializeAccountCredentialRequest{AccountId: "acct-1", ProjectId: "project-x"})
	if err != nil {
		t.Fatalf("MaterializeAccountCredential() error = %v", err)
	}
	if resp.Credential == nil || resp.Credential.AccessToken == "" {
		t.Fatal("response carried no credential envelope")
	}
	if resp.Credential.AccessTokenEnvKey != "CLAUDE_CODE_OAUTH_TOKEN" {
		t.Errorf("envelope projects into %q, want CLAUDE_CODE_OAUTH_TOKEN", resp.Credential.AccessTokenEnvKey)
	}
	if resp.Credential.Env["EXTRA"] != "value" {
		t.Error("envelope lost its static environment material")
	}
	if resp.UpdatedAtUnix == 0 {
		t.Error("response carried no UpdatedAt; a cache has no invalidation signal")
	}

	if captured == nil {
		t.Fatal("the service was not called")
	}
	if captured.TenantID != "tenant-1" {
		t.Errorf("service asked for tenant %q, want the principal's tenant-1", captured.TenantID)
	}
	if captured.AgentID != "agent-vm-host-4" {
		t.Errorf("release attributed to %q, want the principal", captured.AgentID)
	}
	if captured.ProjectID != "project-x" {
		t.Errorf("service asked for project %q, want project-x", captured.ProjectID)
	}
}

// TestAccountGRPC_MaterializeDoesNotShipTheRefreshGrant is the leak guard on
// the wire message: the proto has no field for a refresh grant, and this proves
// the handler does not smuggle one through another one.
func TestAccountGRPC_MaterializeDoesNotShipTheRefreshGrant(t *testing.T) {
	srv := newAccountServer(grpcReleasingService(nil))

	resp, err := srv.MaterializeAccountCredential(grpcDaemonContext("tenant-1"),
		&pb.MaterializeAccountCredentialRequest{AccountId: "acct-1"})
	if err != nil {
		t.Fatalf("MaterializeAccountCredential() error = %v", err)
	}
	encoded, err := protojson.Marshal(resp)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	if strings.Contains(string(encoded), "MUST-NOT-TRAVEL") {
		t.Errorf("release carried the refresh grant: %s", encoded)
	}
}

// TestAccountGRPC_MaterializeRefusesUnauthenticatedAndCrossTenantCallers walks
// the identity refusals.
func TestAccountGRPC_MaterializeRefusesUnauthenticatedAndCrossTenantCallers(t *testing.T) {
	cases := []struct {
		name string
		ctx  context.Context
		req  *pb.MaterializeAccountCredentialRequest
		want codes.Code
	}{
		{
			name: "no principal",
			ctx:  context.Background(),
			req:  &pb.MaterializeAccountCredentialRequest{AccountId: "acct-1"},
			want: codes.Unauthenticated,
		},
		{
			name: "principal with no tenant binding",
			ctx:  grpcDaemonContext(""),
			req:  &pb.MaterializeAccountCredentialRequest{AccountId: "acct-1"},
			want: codes.InvalidArgument,
		},
		{
			name: "request names another tenant",
			ctx:  grpcDaemonContext("tenant-1"),
			req:  &pb.MaterializeAccountCredentialRequest{TenantId: "tenant-2", AccountId: "acct-1"},
			want: codes.PermissionDenied,
		},
		{
			name: "no account named",
			ctx:  grpcDaemonContext("tenant-1"),
			req:  &pb.MaterializeAccountCredentialRequest{},
			want: codes.InvalidArgument,
		},
		{
			name: "nil request",
			ctx:  grpcDaemonContext("tenant-1"),
			req:  nil,
			want: codes.InvalidArgument,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var captured *service.MaterializeAccountCredentialRequest
			srv := newAccountServer(grpcReleasingService(&captured))

			_, err := srv.MaterializeAccountCredential(tc.ctx, tc.req)
			if status.Code(err) != tc.want {
				t.Fatalf("code = %s, want %s (err = %v)", status.Code(err), tc.want, err)
			}
			if captured != nil {
				t.Fatal("a refused request reached the service")
			}
		})
	}
}

// TestAccountGRPC_MaterializeMapsRefusalsToCodes pins the code each refusal
// takes. The distinction matters to a daemon: FailedPrecondition clears itself,
// PermissionDenied needs a configuration change, Unavailable is the control
// plane's fault and not the caller's.
func TestAccountGRPC_MaterializeMapsRefusalsToCodes(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want codes.Code
	}{
		{
			name: "revoked account",
			err:  &service.AccountNotReleasableError{AccountID: "acct-1", Status: orchestration.AccountStatusRevoked},
			want: codes.FailedPrecondition,
		},
		{
			name: "project fence",
			err:  &service.AccountProjectScopeError{AccountID: "acct-1", AccountProjectID: "project-x"},
			want: codes.PermissionDenied,
		},
		{
			name: "no such account",
			err:  &service.AccountNotFoundError{TenantID: "tenant-1", AccountID: "acct-1"},
			want: codes.NotFound,
		},
		{
			name: "pool not wired",
			err:  &service.AccountTokenManagerNotConfiguredError{},
			want: codes.Unavailable,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newAccountServer(&mocks.MockAccountService{
				MaterializeAccountCredentialFunc: func(
					_ context.Context, _ *service.MaterializeAccountCredentialRequest,
				) (*service.MaterializeAccountCredentialResponse, error) {
					return nil, tc.err
				},
			})
			_, err := srv.MaterializeAccountCredential(grpcDaemonContext("tenant-1"),
				&pb.MaterializeAccountCredentialRequest{AccountId: "acct-1"})
			if status.Code(err) != tc.want {
				t.Fatalf("code = %s, want %s (err = %v)", status.Code(err), tc.want, err)
			}
		})
	}
}

// TestMaterializedCredentialToProto_DropsTheRefreshGrant is the unit-level
// guard on the converter, so the property survives a handler refactor.
func TestMaterializedCredentialToProto_DropsTheRefreshGrant(t *testing.T) {
	out := materializedCredentialToProto(&orchestration.AccountCredential{
		AccessToken:       "RELEASED",
		RefreshToken:      "MUST-NOT-TRAVEL",
		TokenEndpoint:     "https://example.invalid/token",
		ClientID:          "MUST-NOT-TRAVEL",
		AccessTokenEnvKey: "K",
		Env:               map[string]string{"A": "b"},
		Files:             map[string][]byte{"c": []byte("d")},
	})
	if out == nil {
		t.Fatal("converter returned nil for a populated envelope")
	}
	encoded, err := protojson.Marshal(out)
	if err != nil {
		t.Fatalf("protojson.Marshal: %v", err)
	}
	if strings.Contains(string(encoded), "MUST-NOT-TRAVEL") {
		t.Errorf("converted envelope carries the refresh grant: %s", encoded)
	}
	if out.AccessToken == "" || out.AccessTokenEnvKey != "K" ||
		out.Env["A"] != "b" || string(out.Files["c"]) != "d" {
		t.Error("converted envelope dropped the material a spawn needs")
	}
	if materializedCredentialToProto(nil) != nil {
		t.Error("converter fabricated an envelope from nil")
	}
}
