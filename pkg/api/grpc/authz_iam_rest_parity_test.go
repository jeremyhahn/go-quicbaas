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
	"testing"

	"github.com/stretchr/testify/assert"
)

// iamRESTParityGrants is the grant REST requires for the route each of these
// IAMService RPCs mirrors (pkg/api/rest/routes_permissions_generated.go). By
// owner ruling gRPC takes exactly that grant, nothing wider. Paths under
// .../ are /api/v1/tenants/{tenant_id}/.
var iamRESTParityGrants = map[string]methodPermission{
	// POST .../apps/
	"/quicbaas.iam.v1.IAMService/RegisterApp": {"tenants", "create"},
	// GET .../apps/{client_id}
	"/quicbaas.iam.v1.IAMService/GetApp": {"tenants", "read"},
	// GET .../apps/
	"/quicbaas.iam.v1.IAMService/ListApps": {"tenants", "read"},
	// PUT .../apps/{client_id}
	"/quicbaas.iam.v1.IAMService/UpdateApp": {"tenants", "create"},
	// DELETE .../apps/{client_id}
	"/quicbaas.iam.v1.IAMService/DeleteApp": {"tenants", "delete"},
	// POST .../apps/{client_id}/rotate-secret
	"/quicbaas.iam.v1.IAMService/RotateAppSecret": {"tenants", "create"},
	// POST .../policies/
	"/quicbaas.iam.v1.IAMService/CreatePolicy": {"tenants", "create"},
	// GET .../policies/{policy_name}
	"/quicbaas.iam.v1.IAMService/GetPolicy": {"tenants", "read"},
	// GET .../policies/
	"/quicbaas.iam.v1.IAMService/ListPolicies": {"tenants", "read"},
	// PUT .../policies/{policy_name}
	"/quicbaas.iam.v1.IAMService/UpdatePolicy": {"tenants", "create"},
	// DELETE .../policies/{policy_name}
	"/quicbaas.iam.v1.IAMService/DeletePolicy": {"tenants", "delete"},
	// POST .../policies/test
	"/quicbaas.iam.v1.IAMService/TestPolicy": {"tenants", "create"},
	// POST .../policies/validate
	"/quicbaas.iam.v1.IAMService/ValidatePolicy": {"tenants", "create"},
	// POST .../roles/
	"/quicbaas.iam.v1.IAMService/CreateRole": {"tenants", "create"},
	// GET .../roles/{role_id}
	"/quicbaas.iam.v1.IAMService/GetRole": {"tenants", "read"},
	// GET .../roles/
	"/quicbaas.iam.v1.IAMService/ListRoles": {"tenants", "read"},
	// PUT .../roles/{role_id}
	"/quicbaas.iam.v1.IAMService/UpdateRole": {"tenants", "create"},
	// DELETE .../roles/{role_id}
	"/quicbaas.iam.v1.IAMService/DeleteRole": {"tenants", "delete"},
	// POST .../users/{user_id}/roles
	"/quicbaas.iam.v1.IAMService/AssignRole": {"tenants", "create"},
	// DELETE .../users/{user_id}/roles/{role}
	"/quicbaas.iam.v1.IAMService/RevokeRole": {"tenants", "delete"},
	// POST /api/v1/admin/assignments/
	"/quicbaas.iam.v1.IAMService/CreateAdminAssignment": {"roles", "assign"},
	// GET /api/v1/admin/assignments/{assignment_id}
	"/quicbaas.iam.v1.IAMService/GetAdminAssignment": {"roles", "read"},
	// PUT /api/v1/admin/assignments/{assignment_id}
	"/quicbaas.iam.v1.IAMService/UpdateAdminAssignment": {"roles", "assign"},
	// DELETE /api/v1/admin/assignments/{assignment_id}
	"/quicbaas.iam.v1.IAMService/DeleteAdminAssignment": {"roles", "unassign"},
	// GET /api/v1/admin/assignments/
	"/quicbaas.iam.v1.IAMService/ListAdminAssignments": {"roles", "list"},
	// POST /api/v1/admin/check-permission
	"/quicbaas.iam.v1.IAMService/CheckAdminPermission": {"permissions", "resolve"},
	// POST /api/v1/admin/impersonation/start
	"/quicbaas.iam.v1.IAMService/StartImpersonation": {"users", "update"},
	// POST /api/v1/admin/impersonation/{session_id}/end
	"/quicbaas.iam.v1.IAMService/EndImpersonation": {"users", "update"},
	// GET /api/v1/admin/users/{user_id}/impersonations
	"/quicbaas.iam.v1.IAMService/GetActiveImpersonations": {"users", "read"},
	// POST .../users/{user_id}/gdpr/consent
	"/quicbaas.iam.v1.IAMService/GrantConsent": {"tenants", "create"},
	// PUT .../users/{user_id}/gdpr/consent/{purpose}/withdraw
	"/quicbaas.iam.v1.IAMService/WithdrawConsent": {"tenants", "create"},
	// GET .../users/{user_id}/gdpr/consent/{purpose}
	"/quicbaas.iam.v1.IAMService/GetConsent": {"tenants", "read"},
	// GET .../users/{user_id}/gdpr/consents
	"/quicbaas.iam.v1.IAMService/ListUserConsents": {"tenants", "read"},
	// POST .../users/{user_id}/gdpr/export
	"/quicbaas.iam.v1.IAMService/RequestExport": {"tenants", "create"},
	// GET .../users/{user_id}/gdpr/export/{export_id}
	"/quicbaas.iam.v1.IAMService/GetExportStatus": {"tenants", "read"},
	// POST .../users/{user_id}/gdpr/deletion
	"/quicbaas.iam.v1.IAMService/RequestDeletion": {"tenants", "create"},
	// DELETE .../users/{user_id}/gdpr/deletion/{deletion_id}
	"/quicbaas.iam.v1.IAMService/CancelDeletion": {"tenants", "delete"},
	// GET .../users/{user_id}/gdpr/deletion/{deletion_id}
	"/quicbaas.iam.v1.IAMService/GetDeletionStatus": {"tenants", "read"},
	// PUT .../users/{user_id}
	"/quicbaas.iam.v1.IAMService/UpdateUser": {"tenants", "create"},
}

func TestIAMRESTParityGRPCResourceAndAction(t *testing.T) {
	requireGRPCGrants(t, iamRESTParityGrants)
}

func TestIAMRESTParityGRPCAuthorization(t *testing.T) {
	requireGRPCGrantEnforced(t, iamRESTParityGrants)
}

// TestIAMGRPCErasureVerbsStayUnmapped pins GenerateExport and ProcessDeletion
// as unmapped and not public, so they stay default-denied on gRPC and the Unix
// socket (owner ruling): no REST grant is copied onto them.
func TestIAMGRPCErasureVerbsStayUnmapped(t *testing.T) {
	for _, method := range []string{
		"/quicbaas.iam.v1.IAMService/GenerateExport",
		"/quicbaas.iam.v1.IAMService/ProcessDeletion",
	} {
		_, mapped := grpcMethodPermissions[method]
		assert.False(t, mapped, "%s must carry no grant", method)
		assert.False(t, publicGRPCMethods[method], "%s must not be public", method)
	}
}
