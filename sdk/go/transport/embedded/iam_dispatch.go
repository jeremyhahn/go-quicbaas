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
	"quicbaas.IAM.Tenant/Create": handleIAMTenantCreate,
	"quicbaas.IAM.Tenant/Get":    handleIAMTenantGet,
	"quicbaas.IAM.Tenant/Update": handleIAMTenantUpdate,
	"quicbaas.IAM.Tenant/Delete": handleIAMTenantDelete,
	"quicbaas.IAM.Tenant/List":   handleIAMTenantList,

	// User
	"quicbaas.IAM.User/Create": handleIAMUserCreate,
	"quicbaas.IAM.User/Get":    handleIAMUserGet,
	"quicbaas.IAM.User/Update": handleIAMUserUpdate,
	"quicbaas.IAM.User/Delete": handleIAMUserDelete,
	"quicbaas.IAM.User/List":   handleIAMUserList,

	// Group
	"quicbaas.IAM.Group/Create": handleIAMGroupCreate,
	"quicbaas.IAM.Group/Get":    handleIAMGroupGet,
	"quicbaas.IAM.Group/Update": handleIAMGroupUpdate,
	"quicbaas.IAM.Group/Delete": handleIAMGroupDelete,
	"quicbaas.IAM.Group/List":   handleIAMGroupList,

	// Role
	"quicbaas.IAM.Role/Create": handleIAMRoleCreate,
	"quicbaas.IAM.Role/Get":    handleIAMRoleGet,
	"quicbaas.IAM.Role/Update": handleIAMRoleUpdate,
	"quicbaas.IAM.Role/Delete": handleIAMRoleDelete,
	"quicbaas.IAM.Role/List":   handleIAMRoleList,

	// Policy
	"quicbaas.IAM.Policy/Create": handleIAMPolicyCreate,
	"quicbaas.IAM.Policy/Get":    handleIAMPolicyGet,
	"quicbaas.IAM.Policy/List":   handleIAMPolicyList,
	"quicbaas.IAM.Policy/Delete": handleIAMPolicyDelete,

	// App
	"quicbaas.IAM.App/Register": handleIAMAppRegister,
	"quicbaas.IAM.App/Get":      handleIAMAppGet,
	"quicbaas.IAM.App/List":     handleIAMAppList,
	"quicbaas.IAM.App/Delete":   handleIAMAppDelete,

	// PEP
	"quicbaas.IAM.PEP/Check": handleIAMPEPCheck,
}

// _ keeps the context import live for handlers that take ctx parameters
// even when the linter would otherwise complain about an unused import in
// some build configurations.
var _ = context.TODO
