//go:build rest

package rest

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

// roleAssignDecision is one decision the node authorizer was asked to make.
type roleAssignDecision struct {
	resource string
	action   string
	tenant   string
}

// roleAssignAuthorizer records every decision with the tenant bound to its
// context, and denies roles:assign unless grantRolesAssign is set. Any other
// permission is allowed.
type roleAssignAuthorizer struct {
	mu               sync.Mutex
	grantRolesAssign bool
	decisions        []roleAssignDecision
}

func (a *roleAssignAuthorizer) Authorize(ctx context.Context, _ *qrdbsdk.Identity, resource, action string) error {
	tenant, _ := qrdbsdk.TenantFromContext(ctx)
	a.mu.Lock()
	a.decisions = append(a.decisions, roleAssignDecision{resource: resource, action: action, tenant: tenant})
	a.mu.Unlock()
	if resource == "roles" && action == "assign" && !a.grantRolesAssign {
		return errors.New("roles:assign denied")
	}
	return nil
}

func (a *roleAssignAuthorizer) made() []roleAssignDecision {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]roleAssignDecision(nil), a.decisions...)
}

// roleAssignCreateRequest builds a tenant1 user create carrying body, made by
// an authenticated tenant1 caller.
func roleAssignCreateRequest(body string) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/tenant1/users", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(qrdbsdk.WithIdentity(req.Context(),
		&qrdbsdk.Identity{Subject: "creator", TenantID: "tenant1"}))
	return withChiParams(req, map[string]string{"tenant_id": "tenant1"})
}

// TestIAMHandler_CreateUserWithRolesRequiresRolesAssign pins go-qrdb's native
// create rule (AuthorizeRoleAssignmentWriteOnCreate) on REST: a create that
// names roles also needs roles:assign in the tenant the user is created in,
// and a create with no roles asks for nothing more.
// withRoleAssignCaller gives a role-carrying create the production
// precondition of an authorized caller: server's node authorizer grants
// roles:assign, and req carries an authenticated tenant1 identity.
func withRoleAssignCaller(server *Server, req *http.Request) *http.Request {
	return withRoleAssignCallerIn(server, req, "tenant1")
}

// withRoleAssignCallerIn is withRoleAssignCaller for a caller in tenantID.
func withRoleAssignCallerIn(server *Server, req *http.Request, tenantID string) *http.Request {
	server.SetBarrierScopeAuthorizer(&roleAssignAuthorizer{grantRolesAssign: true})
	return req.WithContext(qrdbsdk.WithIdentity(req.Context(),
		&qrdbsdk.Identity{Subject: "creator", TenantID: tenantID}))
}

func TestIAMHandler_CreateUserWithRolesRequiresRolesAssign(t *testing.T) {
	const withRoles = `{"email": "assign@example.com", "username": "assign", "roles": ["role-auditor"]}`

	setup := func(t *testing.T) (*Server, *iam.MemoryStorage, *IAMHandler) {
		t.Helper()
		server, _, memStorage, handler, cleanup := setupIAMTestServer(t)
		t.Cleanup(cleanup)
		ctx := context.Background()
		_ = memStorage.CreateTenant(ctx, &iam.Tenant{ID: "tenant1", Name: "Tenant 1"})
		require.NoError(t, memStorage.CreateRole(ctx, &iam.Role{ID: "role-auditor", TenantID: "tenant1", Name: "auditor"}))
		return server, memStorage, handler
	}

	t.Run("roles:assign denied", func(t *testing.T) {
		server, memStorage, handler := setup(t)
		authz := &roleAssignAuthorizer{}
		server.SetBarrierScopeAuthorizer(authz)
		w := httptest.NewRecorder()

		handler.CreateUser(w, roleAssignCreateRequest(withRoles))

		assert.Equal(t, http.StatusForbidden, w.Code, "body: %s", w.Body.String())
		_, err := memStorage.GetUserByEmail(context.Background(), "tenant1", "assign@example.com")
		require.Error(t, err, "no user may be stored without roles:assign")
		assert.Contains(t, authz.made(), roleAssignDecision{resource: "roles", action: "assign", tenant: "tenant1"})
	})

	t.Run("roles:assign granted", func(t *testing.T) {
		server, memStorage, handler := setup(t)
		authz := &roleAssignAuthorizer{grantRolesAssign: true}
		server.SetBarrierScopeAuthorizer(authz)
		w := httptest.NewRecorder()

		handler.CreateUser(w, roleAssignCreateRequest(withRoles))

		require.Equal(t, http.StatusCreated, w.Code, "body: %s", w.Body.String())
		stored, err := memStorage.GetUserByEmail(context.Background(), "tenant1", "assign@example.com")
		require.NoError(t, err)
		assert.Equal(t, []string{"role-auditor"}, stored.Roles)
		assert.Contains(t, authz.made(), roleAssignDecision{resource: "roles", action: "assign", tenant: "tenant1"})
	})

	t.Run("no authorizer wired", func(t *testing.T) {
		_, memStorage, handler := setup(t)
		w := httptest.NewRecorder()

		handler.CreateUser(w, roleAssignCreateRequest(withRoles))

		assert.Equal(t, http.StatusServiceUnavailable, w.Code, "body: %s", w.Body.String())
		_, err := memStorage.GetUserByEmail(context.Background(), "tenant1", "assign@example.com")
		require.Error(t, err, "no user may be stored when roles:assign cannot be decided")
	})

	t.Run("no roles named", func(t *testing.T) {
		server, memStorage, handler := setup(t)
		authz := &roleAssignAuthorizer{}
		server.SetBarrierScopeAuthorizer(authz)
		w := httptest.NewRecorder()

		handler.CreateUser(w, roleAssignCreateRequest(`{"email": "plain@example.com", "username": "plain"}`))

		// A role is mandatory, so the create is refused before roles:assign.
		require.Equal(t, http.StatusBadRequest, w.Code, "body: %s", w.Body.String())
		_, err := memStorage.GetUserByEmail(context.Background(), "tenant1", "plain@example.com")
		require.Error(t, err, "a create naming no roles must write no user")
		for _, d := range authz.made() {
			assert.False(t, d.resource == "roles" && d.action == "assign",
				"a create naming no roles must not ask roles:assign")
		}
	})
}
