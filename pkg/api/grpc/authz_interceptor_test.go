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
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
)

// fakeGRPCValidator validates a fixed token to a fixed identity. Any other token
// errors. It lets the interceptor tests exercise the auth path without a real
// OIDC provider.
type fakeGRPCValidator struct {
	token string
	id    *GRPCIdentity
}

func (f *fakeGRPCValidator) ValidateGRPCToken(_ context.Context, token string) (*GRPCIdentity, error) {
	if token != f.token {
		return nil, errors.New("invalid token")
	}
	return f.id, nil
}

// fakeGRPCAuthorizer permits the configured (resource, action) pair and denies
// everything else. It records the last identity it saw so tests can assert the
// subject/tenant plumbing.
type fakeGRPCAuthorizer struct {
	allowResource string
	allowAction   string
	lastID        *GRPCIdentity
}

func (f *fakeGRPCAuthorizer) AuthorizeGRPC(_ context.Context, id *GRPCIdentity, resource, action string) error {
	f.lastID = id
	if resource == f.allowResource && action == f.allowAction {
		return nil
	}
	return errors.New("permission denied")
}

func ctxWithBearer(token string) context.Context {
	md := metadata.New(map[string]string{"authorization": "Bearer " + token})
	return metadata.NewIncomingContext(context.Background(), md)
}

func TestGRPCResourceAndAction_Mapped(t *testing.T) {
	cases := []struct {
		method   string
		resource string
		action   string
	}{
		{"/quicbaas.v1.QuicBaaS/Put", "kv", "put"},
		{"/quicbaas.v1.QuicBaaS/Get", "kv", "get"},
		{"/quicbaas.v1.QuicBaaS/CreateUser", "users", "create"},
		{"/quicbaas.kms.v1.Crypto/Sign", "keys", "sign"},
		{"/quicbaas.barrier.v1.Barrier/BarrierStatus", "tenant_barrier", "status"},
		{"/quicbaas.vector.v1.VectorDB/SearchSimilar", "vector", "search_similar"},
		// Stage 0.5 broker data-plane surfaces.
		{"/quicbaas.objectstorage.v1.ObjectStorageService/PutObject", "objstore", "put"},
		{"/quicbaas.objectstorage.v1.ObjectStorageService/ListObjects", "objstore", "list"},
		{"/quicbaas.objectstorage.v1.ObjectStorageAdmin/RunGarbageCollection", "objstore", "admin"},
		{"/quicbaas.v1.ObjectTransferService/FetchObject", "objstore", "get"},
		{"/quicbaas.orchestrator.v1.OrchestratorService/SpawnSession", "orchestrator", "spawn"},
		{"/quicbaas.orchestrator.v1.OrchestratorService/ClaimTask", "orchestrator", "task_claim"},
		{"/quicbaas.messaging.v1.MessagingService/Publish", "messaging", "publish"},
		{"/quicbaas.messaging.v1.MessagingService/CreateChannel", "messaging", "channel_create"},
		{"/quicbaas.scheduler.v1.SchedulerService/CreateJob", "scheduler", "create"},
		{"/quicbaas.scheduler.v1.SchedulerService/TriggerJob", "scheduler", "trigger"},
		{"/quicbaas.parameterstore.v1.ParameterStore/GetParameter", "parameters", "read"},
		{"/quicbaas.parameterstore.v1.ParameterStore/SearchByTags", "parameters", "search"},
		{"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/GenerateCredential", "dynamic_secrets", "generate"},
		{"/quicbaas.dynamicsecrets.v1.DynamicSecretsService/CreateRole", "dynamic_secrets", "role_create"},
		{"/quicbaas.project.v1.ProjectService/CreateProject", "project", "create"},
		{"/quicbaas.project.v1.ProjectService/GetProjectByName", "project", "read"},
		{"/quicbaas.ollama.v1.OllamaService/Embed", "ollama", "embed"},
		{"/quicbaas.ollama.v1.OllamaService/GetModelInfo", "ollama", "model_info"},
	}
	for _, c := range cases {
		res, act, ok := grpcResourceAndAction(c.method)
		require.True(t, ok, c.method)
		assert.Equal(t, c.resource, res, c.method)
		assert.Equal(t, c.action, act, c.method)
	}
}

func TestGRPCResourceAndAction_Unmapped(t *testing.T) {
	// Surfaces with no qrdb permission family must be unmapped (default-deny).
	// embedding, agent runtime, and RAG remain unregistered after Stage 0.5.
	for _, m := range []string{
		"/quicbaas.embedding.v1.EmbeddingService/Generate",
		"/quicbaas.agent.v1.AgentRuntime/DoThing",
		"/quicbaas.rag.v1.RAGService/Query",
		"/quicbaas.unknown.v1.Mystery/DoThing",
	} {
		_, _, ok := grpcResourceAndAction(m)
		assert.False(t, ok, m)
	}
}

func TestIsPublicGRPCMethod(t *testing.T) {
	assert.True(t, isPublicGRPCMethod("/quicbaas.v1.QuicBaaS/Health"))
	assert.True(t, isPublicGRPCMethod("/quicbaas.login.v1.Login/BeginLogin"))
	assert.True(t, isPublicGRPCMethod("/quicbaas.login.v1.Login/FinishLogin"))
	assert.False(t, isPublicGRPCMethod("/quicbaas.v1.QuicBaaS/Put"))
}

func TestBearerFromContext(t *testing.T) {
	assert.Equal(t, "abc", bearerFromContext(ctxWithBearer("abc")))
	// Raw token without the Bearer prefix is accepted.
	md := metadata.New(map[string]string{"authorization": "rawtoken"})
	assert.Equal(t, "rawtoken", bearerFromContext(metadata.NewIncomingContext(context.Background(), md)))
	// No metadata -> empty.
	assert.Equal(t, "", bearerFromContext(context.Background()))
}

func TestUnaryAuthorizationInterceptor_Allow(t *testing.T) {
	validator := &fakeGRPCValidator{token: "good", id: &GRPCIdentity{ID: "u1", TenantID: "system", Roles: []string{"admin"}}}
	authorizer := &fakeGRPCAuthorizer{allowResource: "kv", allowAction: "put"}
	interceptor := UnaryAuthorizationInterceptor(validator, authorizer)

	var sawSubject bool
	handler := func(ctx context.Context, _ interface{}) (interface{}, error) {
		if subj, ok := auth.SubjectFromContext(ctx); ok && subj.ID == "u1" && subj.TenantID == "system" {
			sawSubject = true
		}
		return "ok", nil
	}

	resp, err := interceptor(ctxWithBearer("good"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"}, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.True(t, sawSubject, "authenticated subject must be injected into context")
	assert.Equal(t, "u1", authorizer.lastID.ID)
}

func TestUnaryAuthorizationInterceptor_DenyUnpermitted(t *testing.T) {
	validator := &fakeGRPCValidator{token: "good", id: &GRPCIdentity{ID: "u1", TenantID: "system", Roles: []string{"reader"}}}
	authorizer := &fakeGRPCAuthorizer{allowResource: "kv", allowAction: "get"} // not put
	interceptor := UnaryAuthorizationInterceptor(validator, authorizer)

	called := false
	handler := func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil }

	_, err := interceptor(ctxWithBearer("good"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"}, handler)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, called, "handler must not run when authorization denies")
}

func TestUnaryAuthorizationInterceptor_PublicBypass(t *testing.T) {
	validator := &fakeGRPCValidator{token: "good", id: &GRPCIdentity{ID: "u1", TenantID: "system"}}
	authorizer := &fakeGRPCAuthorizer{} // denies everything
	interceptor := UnaryAuthorizationInterceptor(validator, authorizer)

	// No token at all; a public method must still be served.
	resp, err := interceptor(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Health"},
		func(context.Context, interface{}) (interface{}, error) { return "healthy", nil })
	require.NoError(t, err)
	assert.Equal(t, "healthy", resp)
}

func TestUnaryAuthorizationInterceptor_MissingToken(t *testing.T) {
	validator := &fakeGRPCValidator{token: "good", id: &GRPCIdentity{ID: "u1", TenantID: "system"}}
	authorizer := &fakeGRPCAuthorizer{allowResource: "kv", allowAction: "put"}
	interceptor := UnaryAuthorizationInterceptor(validator, authorizer)

	_, err := interceptor(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"},
		func(context.Context, interface{}) (interface{}, error) { return nil, nil })
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestUnaryAuthorizationInterceptor_InvalidToken(t *testing.T) {
	validator := &fakeGRPCValidator{token: "good", id: &GRPCIdentity{ID: "u1", TenantID: "system"}}
	authorizer := &fakeGRPCAuthorizer{allowResource: "kv", allowAction: "put"}
	interceptor := UnaryAuthorizationInterceptor(validator, authorizer)

	_, err := interceptor(ctxWithBearer("bad"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"},
		func(context.Context, interface{}) (interface{}, error) { return nil, nil })
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestUnaryAuthorizationInterceptor_UnmappedDenied(t *testing.T) {
	validator := &fakeGRPCValidator{token: "good", id: &GRPCIdentity{ID: "u1", TenantID: "system"}}
	authorizer := &fakeGRPCAuthorizer{} // would deny anyway
	interceptor := UnaryAuthorizationInterceptor(validator, authorizer)

	_, err := interceptor(ctxWithBearer("good"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.orchestrator.v1.OrchestratorService/Spawn"},
		func(context.Context, interface{}) (interface{}, error) { return nil, nil })
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestAuthorizeGRPC_TenantFallbackFromContext(t *testing.T) {
	// When the identity carries no tenant, the bridge in the server layer falls
	// back to the cert-derived tenant. Here we assert the interceptor still passes
	// the identity through to the authorizer unchanged so the bridge can do that.
	validator := &fakeGRPCValidator{token: "good", id: &GRPCIdentity{ID: "u1", TenantID: "", Roles: []string{"admin"}}}
	authorizer := &fakeGRPCAuthorizer{allowResource: "kv", allowAction: "put"}
	interceptor := UnaryAuthorizationInterceptor(validator, authorizer)

	_, err := interceptor(ctxWithBearer("good"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"},
		func(context.Context, interface{}) (interface{}, error) { return nil, nil })
	require.NoError(t, err)
	require.NotNil(t, authorizer.lastID)
	assert.Equal(t, "", authorizer.lastID.TenantID)
}

func TestStreamAuthorizationInterceptor_AllowAndDeny(t *testing.T) {
	validator := &fakeGRPCValidator{token: "good", id: &GRPCIdentity{ID: "u1", TenantID: "system", Roles: []string{"admin"}}}
	authorizer := &fakeGRPCAuthorizer{allowResource: "watch", allowAction: "watch"}
	interceptor := StreamAuthorizationInterceptor(validator, authorizer)

	// Allow: Watch maps to watch:watch.
	served := false
	err := interceptor(nil, &dummyStream{ctx: ctxWithBearer("good")},
		&grpc.StreamServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Watch"},
		func(interface{}, grpc.ServerStream) error { served = true; return nil })
	require.NoError(t, err)
	assert.True(t, served)

	// Deny: a method whose permission is not allowed by the authorizer.
	served = false
	err = interceptor(nil, &dummyStream{ctx: ctxWithBearer("good")},
		&grpc.StreamServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/StreamBatchPut"},
		func(interface{}, grpc.ServerStream) error { served = true; return nil })
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, served)
}
