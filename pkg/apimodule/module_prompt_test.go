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
	"context"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/service/mocks"
	pt "github.com/jeremyhahn/go-quicbaas/pkg/testutil/prompttest"
)

// TestModule_PromptService_ServedOnTheUnifiedServer proves a prompt RPC on
// the network gRPC listener reaches the prompt server rather than answering
// Unimplemented: with no prompt store mounted it is the prompt server's own
// Unavailable, not the module's "facade not yet composed" one.
func TestModule_PromptService_ServedOnTheUnifiedServer(t *testing.T) {
	composed := &mocks.MockQuicBaaS{}
	m, err := NewModule(func() service.QuicBaaS { return composed }, testLogger(t))
	require.NoError(t, err)

	client := pb.NewPromptServiceClient(startBufconnModule(t, m))
	_, err = client.GetEffectivePrompt(context.Background(), &pb.PromptAddress{Set: "sdlc", Name: "worker_charter"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unavailable, st.Code(), "a prompt RPC must reach the prompt server, got %v", st.Code())
	assert.Equal(t, service.ErrPromptsUnavailable.Error(), st.Message())
}

// TestModule_PromptService_ContributesItsPermissions proves the gRPC pass of
// the grant contribution names every permission the prompt RPCs declare, so
// the unified server's authorizer knows them whichever transports are built.
func TestModule_PromptService_ContributesItsPermissions(t *testing.T) {
	set := map[string]struct{}{}
	contributeGRPCPermissions(set)
	for _, perm := range []string{"prompts:read", "prompts:history", "prompts:write"} {
		_, ok := set[perm]
		assert.True(t, ok, "the gRPC registrations must contribute %s", perm)
	}
}

// promptModule serves a mounted prompt store through the module.
func promptModule(t *testing.T) *Module {
	t.Helper()
	composed := &mocks.MockQuicBaaS{}
	composed.SetPromptService(pt.New(t).Service)
	m, err := NewModule(func() service.QuicBaaS { return composed }, testLogger(t))
	require.NoError(t, err)
	return m
}

// TestModule_PromptService_WritesAuthoredByTheQRDBIdentity proves a write on
// the unified gRPC listener names the caller qrdb authenticated as its
// author. The context carries only the qrdb identity, as go-qrdb's auth
// chain leaves it; no quicbaas Subject is injected.
func TestModule_PromptService_WritesAuthoredByTheQRDBIdentity(t *testing.T) {
	client := pb.NewPromptServiceClient(startAuthenticatedModule(t, promptModule(t), pt.Tenant))
	ctx := context.Background()
	addr := &pb.PromptAddress{Scope: "tenant", Set: pt.Set, Name: pt.Charter}

	_, err := client.PutPrompt(ctx, &pb.PutPromptRequest{Address: addr, Content: "First.\nSTOP:\n", Note: "one"})
	require.NoError(t, err)
	_, err = client.PutPrompt(ctx, &pb.PutPromptRequest{Address: addr, Content: "Second.\nSTOP:\n", Note: "two", ExpectedVersion: 1})
	require.NoError(t, err)
	_, err = client.RollbackPrompt(ctx, &pb.RollbackPromptRequest{Address: addr, ToVersion: 1, Note: "back", ExpectedVersion: 2})
	require.NoError(t, err)
	_, err = client.ResetPrompt(ctx, &pb.ResetPromptRequest{Address: addr, Note: "inherit", ExpectedVersion: 3})
	require.NoError(t, err)

	history, err := client.GetPromptHistory(ctx, addr)
	require.NoError(t, err)
	require.Len(t, history.GetRevisions(), 4)
	for _, rev := range history.GetRevisions() {
		assert.Equal(t, "alice", rev.GetAuthor(), "revision %d", rev.GetVersion())
	}
}

// TestModule_PromptService_RefusesAnAnonymousWrite proves a write reaching the
// module with neither a qrdb identity nor a Subject is refused with the
// store's author error rather than written without an author. The tenant
// alone is bound, so the call gets past tenant resolution to the store.
func TestModule_PromptService_RefusesAnAnonymousWrite(t *testing.T) {
	tenantOnly := grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(qrdbsdk.ContextWithTenant(ctx, pt.Tenant), req)
	})
	client := pb.NewPromptServiceClient(startBufconnModule(t, promptModule(t), tenantOnly))
	addr := &pb.PromptAddress{Scope: "tenant", Set: pt.Set, Name: pt.Charter}

	_, err := client.PutPrompt(context.Background(), &pb.PutPromptRequest{Address: addr, Content: "First.\nSTOP:\n", Note: "one"})
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.InvalidArgument, st.Code())
	assert.Contains(t, st.Message(), "author")
}
