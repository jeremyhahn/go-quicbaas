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

package embedded

import "context"

// iam_dispatch.go registers the embedded IAM service handlers into the
// global unaryHandlers map at package init time. The handlers themselves
// live in handlers_iam.go (also iam-tagged); this file only wires the
// service.method → handler map.

// init registers IAM dispatch entries when the package is built with
// -tags iam. Without the tag the IAM REST/CLI surface returns
// "unknown method" and the caller is expected to switch to gRPC or
// REST transport.
func init() {
	for k, v := range iamUnaryHandlers {
		unaryHandlers[k] = v
	}
}

// iamUnaryHandlers maps quicbaas.IAM.* service methods onto handler
// functions. The map is collected here (rather than appended directly to
// the global table) so the iam-tagged build is the only thing that adds
// IAM entries.
var iamUnaryHandlers = map[string]handlerFunc{
	// Tenant
	"quicbaas.IAM.Tenant/Create":    handleIAMTenantCreate,
	"quicbaas.IAM.Tenant/Get":       handleIAMTenantGet,
	"quicbaas.IAM.Tenant/Update":    handleIAMTenantUpdate,
	"quicbaas.IAM.Tenant/Delete":    handleIAMTenantDelete,
	"quicbaas.IAM.Tenant/List":      handleIAMTenantList,
	"quicbaas.IAM.Tenant/Provision": handleIAMTenantProvision,

	// User
	"quicbaas.IAM.User/Create": handleIAMUserCreate,
	"quicbaas.IAM.User/Get":    handleIAMUserGet,
	"quicbaas.IAM.User/Update": handleIAMUserUpdate,
	"quicbaas.IAM.User/Delete": handleIAMUserDelete,
	"quicbaas.IAM.User/List":   handleIAMUserList,

	// User lookup, role and approval verbs. Suspend and Unsuspend are absent:
	// no other transport serves them either.
	"quicbaas.IAM.User/GetByEmail":     handleIAMUserGetByEmail,
	"quicbaas.IAM.User/FindByUsername": handleIAMUserFindByUsername,
	"quicbaas.IAM.User/AssignRole":     handleIAMUserAssignRole,
	"quicbaas.IAM.User/RevokeRole":     handleIAMUserRevokeRole,
	"quicbaas.IAM.User/Approve":        handleIAMUserApprove,
	"quicbaas.IAM.User/Reject":         handleIAMUserReject,

	// Role
	"quicbaas.IAM.Role/Create": handleIAMRoleCreate,
	"quicbaas.IAM.Role/Get":    handleIAMRoleGet,
	"quicbaas.IAM.Role/Update": handleIAMRoleUpdate,
	"quicbaas.IAM.Role/Delete": handleIAMRoleDelete,
	"quicbaas.IAM.Role/List":   handleIAMRoleList,

	// Reconcile converges a tenant's built-in roles onto the running build's
	// definitions; there is no ordinary update path for a system role.
	"quicbaas.IAM.Role/Reconcile": handleIAMRoleReconcile,

	// Policy
	"quicbaas.IAM.Policy/Create":   handleIAMPolicyCreate,
	"quicbaas.IAM.Policy/Get":      handleIAMPolicyGet,
	"quicbaas.IAM.Policy/List":     handleIAMPolicyList,
	"quicbaas.IAM.Policy/Delete":   handleIAMPolicyDelete,
	"quicbaas.IAM.Policy/Update":   handleIAMPolicyUpdate,
	"quicbaas.IAM.Policy/Validate": handleIAMPolicyValidate,
	"quicbaas.IAM.Policy/Test":     handleIAMPolicyTest,

	// Policy Simulate (go-quicbaas#1564): the TestPolicy evaluation the gRPC
	// and REST transports map Simulate onto.
	"quicbaas.IAM.Policy/Simulate": handleIAMPolicySimulate,

	// App
	"quicbaas.IAM.App/Register":     handleIAMAppRegister,
	"quicbaas.IAM.App/Get":          handleIAMAppGet,
	"quicbaas.IAM.App/List":         handleIAMAppList,
	"quicbaas.IAM.App/Update":       handleIAMAppUpdate,
	"quicbaas.IAM.App/Delete":       handleIAMAppDelete,
	"quicbaas.IAM.App/RotateSecret": handleIAMAppRotateSecret,

	// Session. RevokeOther is the spelling the gRPC transport also accepts.
	// Touch stamps server time through SessionManager.UpdateActivity, as the
	// MCP transport does; the client's Timestamp field is not carried.
	"quicbaas.IAM.Session/Get":          handleIAMSessionGet,
	"quicbaas.IAM.Session/Touch":        handleIAMSessionTouch,
	"quicbaas.IAM.Session/List":         handleIAMSessionList,
	"quicbaas.IAM.Session/Revoke":       handleIAMSessionRevoke,
	"quicbaas.IAM.Session/RevokeAll":    handleIAMSessionRevokeAll,
	"quicbaas.IAM.Session/RevokeOthers": handleIAMSessionRevokeOthers,
	"quicbaas.IAM.Session/RevokeOther":  handleIAMSessionRevokeOthers,

	// Admin (delegated administration). GetActiveImpersonations is the name
	// the gRPC transport accepts alongside ListActiveImpersonations; both are
	// registered so a caller's method name resolves identically on either
	// transport.
	"quicbaas.IAM.Admin/CreateAssignment":         handleIAMAdminCreateAssignment,
	"quicbaas.IAM.Admin/GetAssignment":            handleIAMAdminGetAssignment,
	"quicbaas.IAM.Admin/UpdateAssignment":         handleIAMAdminUpdateAssignment,
	"quicbaas.IAM.Admin/DeleteAssignment":         handleIAMAdminDeleteAssignment,
	"quicbaas.IAM.Admin/ListAssignments":          handleIAMAdminListAssignments,
	"quicbaas.IAM.Admin/CheckPermission":          handleIAMAdminCheckPermission,
	"quicbaas.IAM.Admin/StartImpersonation":       handleIAMAdminStartImpersonation,
	"quicbaas.IAM.Admin/EndImpersonation":         handleIAMAdminEndImpersonation,
	"quicbaas.IAM.Admin/ListActiveImpersonations": handleIAMAdminListActiveImpersonations,
	"quicbaas.IAM.Admin/GetActiveImpersonations":  handleIAMAdminListActiveImpersonations,

	// PEP
	"quicbaas.IAM.PEP/Check": handleIAMPEPCheck,

	// PEP CheckBatch (go-quicbaas#1564): evaluated through the policy engine,
	// as the gRPC PEP server's CheckBatch does.
	"quicbaas.IAM.PEP/CheckBatch": handleIAMPEPCheckBatch,

	// WebAuthn credential administration. Registered under the same
	// quicbaas.IAM.WebAuthn service the SDK client dispatches on, so the
	// admin recovery verbs work on the embedded transport too rather than
	// failing with unsupported-service.
	"quicbaas.IAM.WebAuthn/ListWebAuthnCredentials":   handleIAMWebAuthnCredentialsList,
	"quicbaas.IAM.WebAuthn/DeleteWebAuthnCredential":  handleIAMWebAuthnCredentialDelete,
	"quicbaas.IAM.WebAuthn/DeleteWebAuthnCredentials": handleIAMWebAuthnCredentialsDeleteAll,

	// Invite activation (public: the invite token is the credential)
	"quicbaas.IAM.Invite/GetInvite":           handleIAMInviteGet,
	"quicbaas.IAM.Invite/ActivateFIDO2Begin":  handleIAMInviteFIDO2Begin,
	"quicbaas.IAM.Invite/ActivateFIDO2Finish": handleIAMInviteFIDO2Finish,
	"quicbaas.IAM.Invite/ActivatePIV":         handleIAMInvitePIV,
}

// _ keeps the context import live for handlers that take ctx parameters
// even when the linter would otherwise complain about an unused import in
// some build configurations.
var _ = context.TODO
