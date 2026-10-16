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

package grpc

import (
	"context"
	"errors"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	pt "github.com/jeremyhahn/go-quicbaas/pkg/testutil/prompttest"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// errPromptUnclassified is an error the prompt service does not classify.
var errPromptUnclassified = errors.New("unclassified")

// fixedPromptProvider serves one prompt service, or none when it is nil.
type fixedPromptProvider struct{ svc *service.PromptService }

func (p fixedPromptProvider) Prompts() *service.PromptService { return p.svc }

// promptRig is a PromptServer over a fresh prompt fixture.
func promptRig(t *testing.T) (*PromptServer, *pt.Fixture) {
	t.Helper()
	f := pt.New(t)
	return NewPromptServer(fixedPromptProvider{svc: f.Service}, sdlcTestLogger(t)), f
}

// promptCtx is a call from the fixture's author in tenant.
func promptCtx(tenant string) context.Context {
	return tenantctx.WithTenant(pt.Context(), tenant)
}

func charterAt(scope string) *pb.PromptAddress {
	addr := &pb.PromptAddress{Scope: scope, Set: pt.Set, Name: pt.Charter}
	if scope == "project" {
		addr.ProjectId = pt.Project
	}
	return addr
}

const promptGRPCEdited = "Work the issue carefully.\nEnd with STOP: when done.\n"

func TestPromptServer_SuccessPaths(t *testing.T) {
	srv, _ := promptRig(t)
	ctx := promptCtx(pt.Tenant)

	put, err := srv.PutPrompt(ctx, &pb.PutPromptRequest{
		Address: charterAt("tenant"), Content: promptGRPCEdited, Note: "tighten",
	})
	require.NoError(t, err)
	require.Equal(t, int64(1), put.GetVersion())
	require.Equal(t, pt.Tenant, put.GetTenantId())
	require.NotZero(t, put.GetUpdatedAtUnix())

	list, err := srv.ListPrompts(ctx, &pb.ListPromptsRequest{Scope: "tenant", Set: pt.Set})
	require.NoError(t, err)
	require.Len(t, list.GetPrompts(), len(pt.Spec().Prompts))
	for _, entry := range list.GetPrompts() {
		require.Equal(t, entry.GetName() == pt.Charter, entry.GetOverridden(), entry.GetName())
	}

	got, err := srv.GetPrompt(ctx, &pb.GetPromptRequest{Address: charterAt("tenant"), Version: 1})
	require.NoError(t, err)
	require.Equal(t, promptGRPCEdited, got.GetContent())

	effective, err := srv.GetEffectivePrompt(ctx, charterAt("project"))
	require.NoError(t, err)
	require.Equal(t, promptGRPCEdited, effective.GetContent())
	require.Equal(t, "tenant", effective.GetOrigin().GetScope())
	require.NotEmpty(t, effective.GetChain())

	diff, err := srv.DiffPrompt(ctx, charterAt("tenant"))
	require.NoError(t, err)
	require.False(t, diff.GetSame())
	require.NotEmpty(t, diff.GetLines())

	rolled, err := srv.RollbackPrompt(ctx, &pb.RollbackPromptRequest{
		Address: charterAt("tenant"), ToVersion: 1, Note: "back", ExpectedVersion: 1,
	})
	require.NoError(t, err)
	require.Equal(t, int64(2), rolled.GetVersion())

	reset, err := srv.ResetPrompt(ctx, &pb.ResetPromptRequest{
		Address: charterAt("tenant"), Note: "reset", ExpectedVersion: 2,
	})
	require.NoError(t, err)
	require.True(t, reset.GetFollowParent())

	history, err := srv.GetPromptHistory(ctx, charterAt("tenant"))
	require.NoError(t, err)
	require.Len(t, history.GetRevisions(), 3)
	require.Equal(t, pt.Author, history.GetRevisions()[0].GetAuthor())
}

func TestPromptServer_StatusMapping(t *testing.T) {
	put := func(addr *pb.PromptAddress, content string, expected int64) func(*PromptServer) error {
		return func(srv *PromptServer) error {
			_, err := srv.PutPrompt(promptCtx(pt.Tenant), &pb.PutPromptRequest{
				Address: addr, Content: content, Note: "n", ExpectedVersion: expected,
			})
			return err
		}
	}
	cases := []struct {
		name    string
		call    func(*PromptServer) error
		code    codes.Code
		message string
	}{
		{"get not overridden", func(srv *PromptServer) error {
			_, err := srv.GetPrompt(promptCtx(pt.Tenant), &pb.GetPromptRequest{Address: charterAt("tenant")})
			return err
		}, codes.NotFound, ""},
		{"stale expected_version", put(charterAt("tenant"), promptGRPCEdited, 7), codes.Aborted, ""},
		{"missing marker", put(charterAt("tenant"), "no marker here", 0), codes.InvalidArgument, pt.Marker},
		{"system write", put(charterAt("system"), promptGRPCEdited, 0), codes.InvalidArgument, ""},
		{"unknown set", func(srv *PromptServer) error {
			_, err := srv.ListPrompts(promptCtx(pt.Tenant), &pb.ListPromptsRequest{Scope: "tenant", Set: "ghost"})
			return err
		}, codes.InvalidArgument, ""},
		{"unknown name", func(srv *PromptServer) error {
			_, err := srv.GetEffectivePrompt(promptCtx(pt.Tenant), &pb.PromptAddress{Scope: "tenant", Set: pt.Set, Name: "ghost"})
			return err
		}, codes.InvalidArgument, ""},
		{"diff unknown name", func(srv *PromptServer) error {
			_, err := srv.DiffPrompt(promptCtx(pt.Tenant), &pb.PromptAddress{Scope: "tenant", Set: pt.Set, Name: "ghost"})
			return err
		}, codes.InvalidArgument, ""},
		{"history unknown name", func(srv *PromptServer) error {
			_, err := srv.GetPromptHistory(promptCtx(pt.Tenant), &pb.PromptAddress{Scope: "tenant", Set: pt.Set, Name: "ghost"})
			return err
		}, codes.InvalidArgument, ""},
		{"rollback system", func(srv *PromptServer) error {
			_, err := srv.RollbackPrompt(promptCtx(pt.Tenant), &pb.RollbackPromptRequest{
				Address: charterAt("system"), ToVersion: 1, Note: "n", ExpectedVersion: 1,
			})
			return err
		}, codes.InvalidArgument, ""},
		{"reset system", func(srv *PromptServer) error {
			_, err := srv.ResetPrompt(promptCtx(pt.Tenant), &pb.ResetPromptRequest{
				Address: charterAt("system"), Note: "n", ExpectedVersion: 1,
			})
			return err
		}, codes.InvalidArgument, ""},
		{"other tenant", func(srv *PromptServer) error {
			addr := charterAt("tenant")
			addr.TenantId = "tenant-b"
			_, err := srv.DiffPrompt(promptCtx(pt.Tenant), addr)
			return err
		}, codes.PermissionDenied, ""},
		{"no tenant bound", func(srv *PromptServer) error {
			_, err := srv.GetPromptHistory(pt.Context(), charterAt("tenant"))
			return err
		}, codes.PermissionDenied, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv, _ := promptRig(t)
			err := tc.call(srv)
			require.Equal(t, tc.code, status.Code(err), "%v", err)
			require.Contains(t, status.Convert(err).Message(), tc.message)
		})
	}
}

func TestPromptServer_UnreachableBackendIsUnavailable(t *testing.T) {
	srv, f := promptRig(t)
	f.Resolver.Fail()
	_, err := srv.GetEffectivePrompt(promptCtx(pt.Tenant), charterAt("tenant"))
	require.Equal(t, codes.Unavailable, status.Code(err), "%v", err)
}

func TestPromptServer_UnmountedIsUnavailable(t *testing.T) {
	servers := map[string]*PromptServer{
		"nil provider": NewPromptServer(nil, sdlcTestLogger(t)),
		"no store":     NewPromptServer(fixedPromptProvider{}, sdlcTestLogger(t)),
	}
	ctx := promptCtx(pt.Tenant)
	addr := charterAt("tenant")
	for name, srv := range servers {
		calls := map[string]func() error{
			"ListPrompts": func() error { _, err := srv.ListPrompts(ctx, &pb.ListPromptsRequest{Set: pt.Set}); return err },
			"GetPrompt": func() error {
				_, err := srv.GetPrompt(ctx, &pb.GetPromptRequest{Address: addr})
				return err
			},
			"GetEffectivePrompt": func() error { _, err := srv.GetEffectivePrompt(ctx, addr); return err },
			"DiffPrompt":         func() error { _, err := srv.DiffPrompt(ctx, addr); return err },
			"GetPromptHistory":   func() error { _, err := srv.GetPromptHistory(ctx, addr); return err },
			"PutPrompt": func() error {
				_, err := srv.PutPrompt(ctx, &pb.PutPromptRequest{Address: addr})
				return err
			},
			"RollbackPrompt": func() error {
				_, err := srv.RollbackPrompt(ctx, &pb.RollbackPromptRequest{Address: addr})
				return err
			},
			"ResetPrompt": func() error {
				_, err := srv.ResetPrompt(ctx, &pb.ResetPromptRequest{Address: addr})
				return err
			},
		}
		for method, call := range calls {
			err := call()
			require.Equal(t, codes.Unavailable, status.Code(err), "%s %s: %v", name, method, err)
		}
	}
}

func TestConvertPromptError(t *testing.T) {
	require.Equal(t, codes.Unavailable, status.Code(convertPromptError(service.ErrPromptsUnavailable)))
	require.Equal(t, codes.Internal, status.Code(convertPromptError(errPromptUnclassified)))
}

// TestPromptServiceDescIsGuarded proves the prompt descriptor is in the
// exhaustive list, so every method must carry a permission mapping.
func TestPromptServiceDescIsGuarded(t *testing.T) {
	for _, desc := range brokerServiceDescs {
		if desc.ServiceName == pb.PromptService_ServiceDesc.ServiceName {
			return
		}
	}
	t.Fatalf("brokerServiceDescs omits %s, so its methods escape the exhaustive permission guard",
		pb.PromptService_ServiceDesc.ServiceName)
}

// TestPromptServer_PermissionIsEnforced runs each method through the
// authorization interceptor: a principal holding only another prompt
// permission is refused, and one holding the method's own is let through.
func TestPromptServer_PermissionIsEnforced(t *testing.T) {
	want := map[string]string{
		"ListPrompts":        "prompts:read",
		"GetPrompt":          "prompts:read",
		"GetEffectivePrompt": "prompts:read",
		"DiffPrompt":         "prompts:read",
		"GetPromptHistory":   "prompts:history",
		"PutPrompt":          "prompts:write",
		"RollbackPrompt":     "prompts:write",
		"ResetPrompt":        "prompts:write",
	}
	other := map[string]string{
		"prompts:read":    "prompts:write",
		"prompts:history": "prompts:read",
		"prompts:write":   "prompts:read",
	}
	require.ElementsMatch(t, methodNamesOf(pb.PromptService_ServiceDesc), slices.Collect(maps.Keys(want)))

	for method, perm := range want {
		info := &grpc.UnaryServerInfo{FullMethod: "/" + pb.PromptService_ServiceDesc.ServiceName + "/" + method}
		for granted, allowed := range map[string]bool{perm: true, other[perm]: false} {
			called := false
			handler := func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil }
			interceptor := UnaryAuthorizationInterceptor(&fakeValidatorAuthorizer{
				token:       "good",
				grantedPerm: granted,
				authzCtx:    &platformtoken.AuthorizedContext{Subject: pt.Author, TenantID: pt.Tenant},
			})
			_, err := interceptor(ctxWithBearer("good"), nil, info, handler)
			if allowed {
				require.NoError(t, err, "%s with %s", method, granted)
			} else {
				require.Equal(t, codes.PermissionDenied, status.Code(err), "%s with %s", method, granted)
			}
			require.Equal(t, allowed, called, "%s with %s", method, granted)
		}
	}
}
