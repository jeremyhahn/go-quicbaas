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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/platformtoken"
)

// fakeValidatorAuthorizer is a test double for the central platform-token
// Validator seam (GRPCValidatorAuthorizer). It models the offline scp + liveness
// decision the real Validator performs:
//
//   - a token other than the configured one is rejected as invalid (the real
//     Validator fails signature / iss / aud / exp verification);
//   - a granted requiredPerm returns the configured AuthorizedContext;
//   - an ungranted requiredPerm returns *ScopeDeniedError (offline scp deny);
//   - an explicitly seeded denyErr overrides the decision so tests can exercise
//     the inactive / config / generic error -> status mappings.
//
// It records the last call so tests can assert the requiredPerm join and that
// the DPoP args are passed empty over gRPC.
type fakeValidatorAuthorizer struct {
	token       string
	grantedPerm string
	authzCtx    *platformtoken.AuthorizedContext
	denyErr     error

	lastPerm     string
	lastDPoPArgs [3]string
}

func (f *fakeValidatorAuthorizer) Authorize(_ context.Context, token, requiredPerm, dpopProof, htm, htu string) (*platformtoken.AuthorizedContext, error) {
	f.lastPerm = requiredPerm
	f.lastDPoPArgs = [3]string{dpopProof, htm, htu}
	if f.denyErr != nil {
		return nil, f.denyErr
	}
	if token != f.token {
		return nil, &platformtoken.InvalidTokenError{Detail: "signature did not verify"}
	}
	if requiredPerm != f.grantedPerm {
		return nil, &platformtoken.ScopeDeniedError{Required: requiredPerm}
	}
	return f.authzCtx, nil
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
		{"/quicbaas.v1.QuicBaaS/CreateRole", "roles", "create"},
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
		// Embedding mirrors REST /api/v1/embedding and the MCP embedding tools.
		{"/quicbaas.embedding.v1.EmbeddingService/EmbeddingGenerate", "embedding", "embed"},
		{"/quicbaas.embedding.v1.EmbeddingService/EmbeddingBatchGenerate", "embedding", "embed"},
		{"/quicbaas.embedding.v1.EmbeddingService/EmbeddingClearCache", "embedding", "embed"},
		{"/quicbaas.embedding.v1.EmbeddingService/EmbeddingListProviders", "embedding", "list_models"},
		{"/quicbaas.embedding.v1.EmbeddingService/EmbeddingGetProviderInfo", "embedding", "list_models"},
		{"/quicbaas.embedding.v1.EmbeddingService/EmbeddingSetDefaultProvider", "embedding", "list_models"},
		{"/quicbaas.embedding.v1.EmbeddingService/EmbeddingGetCacheStats", "embedding", "list_models"},
		// Per-tenant barrier (quicbaas.tenant.v1.TenantBarrier).
		{"/quicbaas.tenant.v1.TenantBarrier/Init", "tenant_barrier", "init"},
		{"/quicbaas.tenant.v1.TenantBarrier/Unseal", "tenant_barrier", "unseal"},
		{"/quicbaas.tenant.v1.TenantBarrier/UnsealShare", "tenant_barrier", "unseal"},
		{"/quicbaas.tenant.v1.TenantBarrier/Seal", "tenant_barrier", "seal"},
		{"/quicbaas.tenant.v1.TenantBarrier/Status", "tenant_barrier", "status"},
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
	// Agent runtime and RAG remain unregistered after Stage 0.5.
	for _, m := range []string{
		"/quicbaas.fleet.v1.FleetRuntime/DoThing",
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
	// An empty bearer value ("Bearer " with nothing after the scheme) must
	// normalize to the empty token so the interceptor rejects it pre-PDP.
	assert.Equal(t, "", bearerFromContext(ctxWithBearer("")))
	assert.Equal(t, "", bearerFromContext(ctxWithBearer("   ")))
}

// TestUnaryAuthorizationInterceptor_Allow asserts the validator-backed gate
// admits a token whose scp grants the required permission and reconstructs the
// principal (id + username + tenant) from the verified AuthorizedContext.
func TestUnaryAuthorizationInterceptor_Allow(t *testing.T) {
	authorizer := &fakeValidatorAuthorizer{
		token:       "good",
		grantedPerm: "kv:put",
		authzCtx: &platformtoken.AuthorizedContext{
			Subject:  "u1",
			Username: "admin",
			TenantID: "system",
			Roles:    []string{"admin"},
			Scopes:   []string{"kv:put"},
		},
	}
	interceptor := UnaryAuthorizationInterceptor(authorizer)

	var sawSubject bool
	handler := func(ctx context.Context, _ interface{}) (interface{}, error) {
		if subj, ok := auth.SubjectFromContext(ctx); ok &&
			subj.ID == "u1" && subj.Username == "admin" && subj.TenantID == "system" && subj.Authenticated {
			sawSubject = true
		}
		return "ok", nil
	}

	resp, err := interceptor(ctxWithBearer("good"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"}, handler)
	require.NoError(t, err)
	assert.Equal(t, "ok", resp)
	assert.True(t, sawSubject, "authenticated subject must be injected into context")
	// The required permission is the qrdb-canonical colon join.
	assert.Equal(t, "kv:put", authorizer.lastPerm)
	// gRPC carries no DPoP: the validator is always called with empty proof args.
	assert.Equal(t, [3]string{"", "", ""}, authorizer.lastDPoPArgs)
}

// A token's sub is a login name, never an account ID: the bound subject
// classifies it by what it is and carries the verified uid as UserID.
func TestContextWithAuthorizedSubject_AccountIsVerifiedUserID(t *testing.T) {
	cases := map[string]struct {
		ac       platformtoken.AuthorizedContext
		wantKind interfaces.SubjectIDKind
	}{
		"numeric username": {
			ac:       platformtoken.AuthorizedContext{Subject: "12345", UserID: "67890", TenantID: "tenant-a"},
			wantKind: interfaces.SubjectIDUsername,
		},
		"email": {
			ac:       platformtoken.AuthorizedContext{Subject: "alice@example.com", UserID: "67890", TenantID: "tenant-a"},
			wantKind: interfaces.SubjectIDEmail,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			subject, ok := auth.SubjectFromContext(contextWithAuthorizedSubject(context.Background(), &tc.ac))
			require.True(t, ok)
			assert.Equal(t, tc.ac.Subject, subject.ID)
			assert.Equal(t, tc.wantKind, subject.IDKind)
			assert.Equal(t, "67890", subject.UserID)
		})
	}
}

// TestUnaryAuthorizationInterceptor_DenyUnpermitted asserts an offline scp deny
// (*ScopeDeniedError) maps to PermissionDenied and the handler never runs.
func TestUnaryAuthorizationInterceptor_DenyUnpermitted(t *testing.T) {
	authorizer := &fakeValidatorAuthorizer{
		token:       "good",
		grantedPerm: "kv:get", // not put
		authzCtx:    &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "system"},
	}
	interceptor := UnaryAuthorizationInterceptor(authorizer)

	called := false
	handler := func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil }

	_, err := interceptor(ctxWithBearer("good"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"}, handler)
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, called, "handler must not run when authorization denies")
}

func TestUnaryAuthorizationInterceptor_PublicBypass(t *testing.T) {
	authorizer := &fakeValidatorAuthorizer{denyErr: &platformtoken.InvalidTokenError{Detail: "should never be called"}}
	interceptor := UnaryAuthorizationInterceptor(authorizer)

	// No token at all; a public method must still be served without the validator.
	resp, err := interceptor(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Health"},
		func(context.Context, interface{}) (interface{}, error) { return "healthy", nil })
	require.NoError(t, err)
	assert.Equal(t, "healthy", resp)
	assert.Equal(t, "", authorizer.lastPerm, "validator must not be consulted for a public method")
}

func TestUnaryAuthorizationInterceptor_MissingToken(t *testing.T) {
	authorizer := &fakeValidatorAuthorizer{token: "good", grantedPerm: "kv:put"}
	interceptor := UnaryAuthorizationInterceptor(authorizer)

	_, err := interceptor(context.Background(), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"},
		func(context.Context, interface{}) (interface{}, error) { return nil, nil })
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.Equal(t, "", authorizer.lastPerm, "validator must not be consulted without a token")
}

// TestUnaryAuthorizationInterceptor_EmptyBearerToken asserts that an
// "authorization: Bearer " header carrying an EMPTY token is rejected
// Unauthenticated before the validator is ever consulted — identical to a
// missing header. An empty credential must never reach the decision point as
// an anonymous principal (fail-closed, no anonymous data-plane path).
func TestUnaryAuthorizationInterceptor_EmptyBearerToken(t *testing.T) {
	authorizer := &fakeValidatorAuthorizer{token: "good", grantedPerm: "kv:put"}
	interceptor := UnaryAuthorizationInterceptor(authorizer)

	called := false
	_, err := interceptor(ctxWithBearer(""), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"},
		func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil })
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	assert.False(t, called, "handler must not run for an empty bearer token")
	assert.Equal(t, "", authorizer.lastPerm, "validator must not be consulted for an empty bearer token")
}

// TestUnaryAuthorizationInterceptor_InvalidToken asserts a token-validation
// failure (generic platform-token error) maps to Unauthenticated.
func TestUnaryAuthorizationInterceptor_InvalidToken(t *testing.T) {
	authorizer := &fakeValidatorAuthorizer{token: "good", grantedPerm: "kv:put"}
	interceptor := UnaryAuthorizationInterceptor(authorizer)

	_, err := interceptor(ctxWithBearer("bad"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"},
		func(context.Context, interface{}) (interface{}, error) { return nil, nil })
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

// TestUnaryAuthorizationInterceptor_InactiveToken asserts a revoked / disabled
// token (*TokenInactiveError from the liveness check) maps to Unauthenticated.
func TestUnaryAuthorizationInterceptor_InactiveToken(t *testing.T) {
	authorizer := &fakeValidatorAuthorizer{
		token:       "good",
		grantedPerm: "kv:put",
		denyErr:     &platformtoken.TokenInactiveError{Subject: "u1", JTI: "j1"},
	}
	interceptor := UnaryAuthorizationInterceptor(authorizer)

	_, err := interceptor(ctxWithBearer("good"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"},
		func(context.Context, interface{}) (interface{}, error) { return nil, nil })
	require.Error(t, err)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

// TestUnaryAuthorizationInterceptor_Misconfigured asserts a server-side
// misconfiguration (*ConfigError) fails closed with Internal — the request is
// denied, not admitted.
func TestUnaryAuthorizationInterceptor_Misconfigured(t *testing.T) {
	authorizer := &fakeValidatorAuthorizer{
		token:       "good",
		grantedPerm: "kv:put",
		denyErr:     &platformtoken.ConfigError{Detail: "liveness checker required but absent"},
	}
	interceptor := UnaryAuthorizationInterceptor(authorizer)

	called := false
	_, err := interceptor(ctxWithBearer("good"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Put"},
		func(context.Context, interface{}) (interface{}, error) { called = true; return nil, nil })
	require.Error(t, err)
	assert.Equal(t, codes.Internal, status.Code(err))
	assert.False(t, called, "handler must not run on a misconfiguration deny")
}

func TestUnaryAuthorizationInterceptor_UnmappedDenied(t *testing.T) {
	authorizer := &fakeValidatorAuthorizer{denyErr: &platformtoken.InvalidTokenError{Detail: "should never be called"}}
	interceptor := UnaryAuthorizationInterceptor(authorizer)

	_, err := interceptor(ctxWithBearer("good"), nil,
		&grpc.UnaryServerInfo{FullMethod: "/quicbaas.orchestrator.v1.OrchestratorService/Spawn"},
		func(context.Context, interface{}) (interface{}, error) { return nil, nil })
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.Equal(t, "", authorizer.lastPerm, "validator must not be consulted for an unmapped method")
}

func TestStreamAuthorizationInterceptor_AllowAndDeny(t *testing.T) {
	authorizer := &fakeValidatorAuthorizer{
		token:       "good",
		grantedPerm: "watch:watch",
		authzCtx:    &platformtoken.AuthorizedContext{Subject: "u1", TenantID: "system", Roles: []string{"admin"}},
	}
	interceptor := StreamAuthorizationInterceptor(authorizer)

	// Allow: Watch maps to watch:watch.
	served := false
	err := interceptor(nil, &dummyStream{ctx: ctxWithBearer("good")},
		&grpc.StreamServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/Watch"},
		func(interface{}, grpc.ServerStream) error { served = true; return nil })
	require.NoError(t, err)
	assert.True(t, served)

	// Deny: a method whose permission is not granted by the token's scp set.
	served = false
	err = interceptor(nil, &dummyStream{ctx: ctxWithBearer("good")},
		&grpc.StreamServerInfo{FullMethod: "/quicbaas.v1.QuicBaaS/StreamBatchPut"},
		func(interface{}, grpc.ServerStream) error { served = true; return nil })
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	assert.False(t, served)
}

// TestGRPCPermissions_EveryMethodMapped is a descriptor-exhaustive guard that
// enumerates every method in every registered broker service descriptor and
// asserts it is either mapped in grpcMethodPermissions or declared public in
// publicGRPCMethods. Any future RPC added to a registered service without a
// corresponding authz mapping will fail this test, preventing silent
// default-deny regressions from going unnoticed.
func TestGRPCPermissions_EveryMethodMapped(t *testing.T) {
	for _, desc := range brokerServiceDescs {
		for _, method := range desc.Methods {
			fullMethod := "/" + desc.ServiceName + "/" + method.MethodName
			_, _, mapped := grpcResourceAndAction(fullMethod)
			if !mapped && !isPublicGRPCMethod(fullMethod) {
				t.Errorf("gRPC method %q is neither mapped in grpcMethodPermissions nor declared public; add a permission mapping or declare it public", fullMethod)
			}
		}
		for _, stream := range desc.Streams {
			fullMethod := "/" + desc.ServiceName + "/" + stream.StreamName
			_, _, mapped := grpcResourceAndAction(fullMethod)
			if !mapped && !isPublicGRPCMethod(fullMethod) {
				t.Errorf("gRPC stream method %q is neither mapped in grpcMethodPermissions nor declared public; add a permission mapping or declare it public", fullMethod)
			}
		}
	}
}

// TestValidatorGRPCStatus_Mapping pins the error -> code mapping fail-closed.
func TestValidatorGRPCStatus_Mapping(t *testing.T) {
	assert.Equal(t, codes.PermissionDenied,
		status.Code(validatorGRPCStatus(&platformtoken.ScopeDeniedError{Required: "kv:put"})))
	assert.Equal(t, codes.Unauthenticated,
		status.Code(validatorGRPCStatus(&platformtoken.TokenInactiveError{Subject: "u1"})))
	assert.Equal(t, codes.Internal,
		status.Code(validatorGRPCStatus(&platformtoken.ConfigError{Detail: "x"})))
	assert.Equal(t, codes.Unauthenticated,
		status.Code(validatorGRPCStatus(&platformtoken.InvalidTokenError{Detail: "bad sig"})))
}
