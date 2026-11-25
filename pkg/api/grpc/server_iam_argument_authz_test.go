package grpc

import (
	"context"
	"sync"
	"testing"
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	iampb "github.com/jeremyhahn/go-quicbaas/api/proto/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// argumentIAMService is an IAMService whose user writes record that they
// ran; every other method is the nil embedded interface.
type argumentIAMService struct {
	IAMService
	created, updated bool
}

func (s *argumentIAMService) CreateUser(_ context.Context, user *iam.User, _ time.Duration) (*iam.User, string, *iam.InviteRecord, error) {
	s.created = true
	return &iam.User{ID: 7, TenantID: user.TenantID, Email: user.Email}, "", nil, nil
}

func (s *argumentIAMService) UpdateUser(_ context.Context, user *iam.User) (*iam.User, error) {
	s.updated = true
	return &iam.User{ID: user.ID, TenantID: user.TenantID}, nil
}

// recordingAuthorizer is a go-qrdb authorizer that grants the permissions in
// granted and records each resource:action it is asked, the tenant bound in
// the context it was asked under, and the identity it was asked about.
type recordingAuthorizer struct {
	mu         sync.Mutex
	granted    map[string]bool
	asked      []string
	tenants    []string
	identities []*qrdbsdk.Identity
}

func newRecordingAuthorizer(granted ...string) *recordingAuthorizer {
	a := &recordingAuthorizer{granted: map[string]bool{}}
	for _, p := range granted {
		a.granted[p] = true
	}
	return a
}

func (a *recordingAuthorizer) Authorize(ctx context.Context, identity *qrdbsdk.Identity, resource, action string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	perm := resource + ":" + action
	a.asked = append(a.asked, perm)
	tenant, _ := qrdbsdk.TenantFromContext(ctx)
	a.tenants = append(a.tenants, tenant)
	a.identities = append(a.identities, identity)
	if a.granted[perm] {
		return nil
	}
	return &qrdbsdk.PermissionDeniedError{}
}

const argumentTenant = "tenant-a"

// argumentAuthzContext is the context the unified module hands the handler:
// go-qrdb's identity and tenant, projected into the quicbaas subject.
func argumentAuthzContext() context.Context {
	ctx := qrdbsdk.WithIdentity(tenantctx.WithTenant(context.Background(), argumentTenant),
		&qrdbsdk.Identity{Subject: "alice", TenantID: argumentTenant})
	return auth.ContextWithIdentitySubject(ctx)
}

func newArgumentIAMServer(t *testing.T, authz qrdbsdk.Authorizer) (*IAMServer, *argumentIAMService) {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	svc := &argumentIAMService{}
	srv := NewIAMServer(svc, log)
	if authz != nil {
		srv.SetArgumentAuthorizer(func() qrdbsdk.Authorizer { return authz })
	}
	return srv, svc
}

// allowArgumentWrites installs an allow-all argument authorizer, for a
// fixture whose subject is not authorization.
func allowArgumentWrites(srv *IAMServer) {
	srv.SetArgumentAuthorizer(func() qrdbsdk.Authorizer { return &qrdbsdk.NoopAuthorizer{} })
}

// authenticatedCallerContext is callerContext with an authenticated subject
// of the tenant bound, for a user write whose arguments are authorized.
func authenticatedCallerContext(tenantID string) context.Context {
	return auth.SubjectIntoContext(callerContext(tenantID), interfaces.Subject{
		ID: "caller", TenantID: tenantID, Authenticated: true,
	})
}

func requireDenied(t *testing.T, err error) {
	t.Helper()
	require.Error(t, err)
	assert.Equal(t, codes.PermissionDenied, status.Code(err), "got %v", err)
}

func TestIAMServer_UpdateUserWithRolesNeedsRolesAssign(t *testing.T) {
	srv, svc := newArgumentIAMServer(t, newRecordingAuthorizer("users:update"))
	_, err := srv.UpdateUser(argumentAuthzContext(), &iampb.UpdateUserRequest{
		TenantId: argumentTenant, UserId: "7", Roles: []string{"admin"},
	})
	requireDenied(t, err)
	assert.False(t, svc.updated, "the service must not run without roles:assign")
}

func TestIAMServer_UpdateUserAttributesNeedRolesAssign(t *testing.T) {
	srv, svc := newArgumentIAMServer(t, newRecordingAuthorizer("users:update"))
	_, err := srv.UpdateUser(argumentAuthzContext(), &iampb.UpdateUserRequest{
		TenantId: argumentTenant, UserId: "7", Attributes: map[string]string{"department": "finance"},
	})
	requireDenied(t, err)
	assert.False(t, svc.updated)
}

func TestIAMServer_UpdateUserLifecycleNeedsEnableOrDisable(t *testing.T) {
	t.Run("active without users:enable", func(t *testing.T) {
		srv, svc := newArgumentIAMServer(t, newRecordingAuthorizer("users:update", "users:disable"))
		_, err := srv.UpdateUser(argumentAuthzContext(), &iampb.UpdateUserRequest{
			TenantId: argumentTenant, UserId: "7", Lifecycle: string(iam.LifecycleActive),
		})
		requireDenied(t, err)
		assert.False(t, svc.updated)
	})
	t.Run("suspended without users:disable", func(t *testing.T) {
		srv, svc := newArgumentIAMServer(t, newRecordingAuthorizer("users:update", "users:enable"))
		_, err := srv.UpdateUser(argumentAuthzContext(), &iampb.UpdateUserRequest{
			TenantId: argumentTenant, UserId: "7", Lifecycle: "suspended",
		})
		requireDenied(t, err)
		assert.False(t, svc.updated)
	})
}

func TestIAMServer_CreateUserWithRolesNeedsRolesAssign(t *testing.T) {
	srv, svc := newArgumentIAMServer(t, newRecordingAuthorizer("users:create"))
	_, err := srv.CreateUser(argumentAuthzContext(), &iampb.CreateUserRequest{
		TenantId: argumentTenant, Email: "bob@example.com", Roles: []string{"admin"},
	})
	requireDenied(t, err)
	assert.False(t, svc.created, "the service must not run without roles:assign")
}

func TestIAMServer_ArgumentChecksFailClosed(t *testing.T) {
	req := &iampb.UpdateUserRequest{TenantId: argumentTenant, UserId: "7", Roles: []string{"admin"}}

	t.Run("no authorizer installed", func(t *testing.T) {
		srv, svc := newArgumentIAMServer(t, nil)
		_, err := srv.UpdateUser(argumentAuthzContext(), req)
		requireDenied(t, err)
		assert.False(t, svc.updated)
	})
	t.Run("resolver returns nil", func(t *testing.T) {
		srv, svc := newArgumentIAMServer(t, nil)
		srv.SetArgumentAuthorizer(func() qrdbsdk.Authorizer { return nil })
		_, err := srv.UpdateUser(argumentAuthzContext(), req)
		requireDenied(t, err)
		assert.False(t, svc.updated)
	})
	t.Run("no identity and no authenticated subject", func(t *testing.T) {
		authz := newRecordingAuthorizer("users:update", "roles:assign")
		srv, svc := newArgumentIAMServer(t, authz)
		ctx := tenantctx.WithTenant(context.Background(), argumentTenant)
		_, err := srv.UpdateUser(ctx, req)
		require.Error(t, err)
		assert.False(t, svc.updated)
		assert.Empty(t, authz.asked, "nobody to ask about")
	})
}

func TestIAMServer_ArgumentChecksAskForTheTargetTenant(t *testing.T) {
	authz := newRecordingAuthorizer("users:update", "roles:assign", "users:enable", "users:disable")
	srv, svc := newArgumentIAMServer(t, authz)
	req := &iampb.UpdateUserRequest{
		TenantId: argumentTenant, UserId: "7", Roles: []string{"admin"},
		Attributes: map[string]string{"department": "finance"}, Lifecycle: string(iam.LifecycleActive),
	}
	_, err := srv.UpdateUser(argumentAuthzContext(), req)
	require.NoError(t, err)
	assert.True(t, svc.updated)

	want, err := grpcArgumentPermissionsFor(iampb.IAMService_UpdateUser_FullMethodName, req)
	require.NoError(t, err)
	assert.Equal(t, want, authz.asked)
	for _, tenant := range authz.tenants {
		assert.Equal(t, argumentTenant, tenant)
	}
}

func TestIAMServer_LegacySubjectIsAskedToo(t *testing.T) {
	authz := newRecordingAuthorizer()
	srv, svc := newArgumentIAMServer(t, authz)
	ctx := auth.SubjectIntoContext(tenantctx.WithTenant(context.Background(), argumentTenant), interfaces.Subject{
		ID: "alice", TenantID: argumentTenant, Roles: []string{"member"}, Authenticated: true,
	})
	_, err := srv.UpdateUser(ctx, &iampb.UpdateUserRequest{
		TenantId: argumentTenant, UserId: "7", Roles: []string{"admin"},
	})
	requireDenied(t, err)
	assert.False(t, svc.updated)
	require.NotEmpty(t, authz.identities)
	identity := authz.identities[0]
	assert.Equal(t, "alice", identity.Subject)
	assert.Equal(t, argumentTenant, identity.TenantID)
	assert.Equal(t, []string{"member"}, identity.Roles)
}

func TestIAMServer_PlainUserWritesAskNothing(t *testing.T) {
	srv, svc := newArgumentIAMServer(t, nil)
	_, err := srv.UpdateUser(argumentAuthzContext(), &iampb.UpdateUserRequest{
		TenantId: argumentTenant, UserId: "7", Name: "Alice",
	})
	require.NoError(t, err)
	assert.True(t, svc.updated)

	_, err = srv.CreateUser(argumentAuthzContext(), &iampb.CreateUserRequest{
		TenantId: argumentTenant, Email: "bob@example.com",
	})
	require.NoError(t, err)
	assert.True(t, svc.created)
}
