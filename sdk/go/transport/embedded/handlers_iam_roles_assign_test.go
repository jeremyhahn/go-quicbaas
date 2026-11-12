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

package embedded

import (
	"context"
	"errors"
	"testing"

	iamsdk "github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// errRoleAssignNotGranted is the refusal roleAssignAuthorizer returns.
var errRoleAssignNotGranted = errors.New("roles:assign not granted")

// roleAssignAuthorizer grants roles:assign to the subjects it names and
// every other permission to everyone, so a test isolates the one check.
type roleAssignAuthorizer struct {
	granted map[string]bool
}

func (a roleAssignAuthorizer) Authorize(ctx context.Context, subject *iam.Subject, resource, action string) error {
	if resource == adminResourceRoles && action == adminActionAssign && !a.granted[subject.ID] {
		return errRoleAssignNotGranted
	}
	if resource == adminResourceRoles && action == adminActionAssign {
		if tenant, ok := tenantctx.Lookup(ctx); !ok || tenant != subject.TenantID {
			return errRoleAssignNotGranted
		}
	}
	return nil
}

// mockQuicBaaSForIAMWithPolicy adds a policy engine to the IAM test service.
type mockQuicBaaSForIAMWithPolicy struct {
	*mockQuicBaaSForIAM
	policy *iam.PolicyEngine
}

func (m *mockQuicBaaSForIAMWithPolicy) GetPolicyEngine(context.Context) (*iam.PolicyEngine, error) {
	return m.policy, nil
}

// createUserWithRoles creates a user in t-1 through the embedded handler,
// carrying a real role when withRoles is set.
func createUserWithRoles(t *testing.T, ctx context.Context, svc service.QuicBaaS, store *iam.IdentityStore, email string, withRoles bool) error {
	t.Helper()
	var roles []string
	if withRoles {
		role, err := store.CreateRole(context.Background(), "t-1", iam.CreateRoleOptions{Name: "auditor-" + email})
		if err != nil {
			t.Fatalf("CreateRole failed: %v", err)
		}
		roles = []string{role.ID}
	}
	_, err := handleIAMUserCreate(ctx, svc, &embeddedCreateUserEnvelope{
		TenantID: "t-1",
		Request: &iamsdk.CreateUserRequest{
			Username: "u" + email[:3],
			Email:    email,
			Roles:    roles,
		},
	})
	return err
}

// TestHandleIAMUserCreate_RolesRequireRolesAssign proves a user create that
// carries roles needs roles:assign from a bound caller, as on REST, gRPC
// and MCP (go-quicbaas#2064), while an unbound context stays the host
// process calling its own service.
func TestHandleIAMUserCreate_RolesRequireRolesAssign(t *testing.T) {
	base, store := newIAMTestService()
	svc := &mockQuicBaaSForIAMWithPolicy{
		mockQuicBaaSForIAM: base,
		policy: iam.NewPolicyEngine(iam.PolicyEngineOptions{
			RBACAuthorizer: roleAssignAuthorizer{granted: map[string]bool{"assigner": true}},
		}),
	}
	bound := func(id string) context.Context {
		return auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: id, TenantID: "t-1"})
	}

	tests := []struct {
		name      string
		ctx       context.Context
		email     string
		withRoles bool
		denied    bool
	}{
		{"bound caller without roles:assign is refused", bound("plain"), "den@example.com", true, true},
		{"bound caller with roles:assign is allowed", bound("assigner"), "all@example.com", true, false},
		{"create without roles skips the check", bound("plain"), "nor@example.com", false, false},
		{"unbound host context is unrestricted", context.Background(), "hst@example.com", true, false},
		{"bound caller of another tenant is refused", auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "assigner", TenantID: "t-2"}), "oth@example.com", true, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := createUserWithRoles(t, tc.ctx, svc, store, tc.email, tc.withRoles)
			if !tc.denied {
				if err != nil {
					t.Fatalf("expected the create to succeed, got %v", err)
				}
				return
			}
			var denied *PermissionDeniedError
			if !errors.As(err, &denied) {
				t.Fatalf("expected *PermissionDeniedError, got %T: %v", err, err)
			}
			if _, lookupErr := store.GetUserByEmail(context.Background(), "t-1", tc.email); lookupErr == nil {
				t.Error("a refused create must not write the user")
			}
		})
	}
}

// TestHandleIAMUserCreate_RolesFailClosedWithoutPolicyEngine proves a bound
// caller's create with roles is refused, not allowed, when no policy engine
// is wired to decide roles:assign.
func TestHandleIAMUserCreate_RolesFailClosedWithoutPolicyEngine(t *testing.T) {
	svc, store := newIAMTestService()
	ctx := auth.SubjectIntoContext(context.Background(), interfaces.Subject{ID: "assigner", TenantID: "t-1"})

	err := createUserWithRoles(t, ctx, svc, store, "fcl@example.com", true)
	var denied *PermissionDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected *PermissionDeniedError (fail closed), got %T: %v", err, err)
	}
}
