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
	"sort"
	"sync"

	qrdbv1 "github.com/jeremyhahn/go-qrdb/pkg/api/grpc/proto/qrdbv1"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"
	"google.golang.org/protobuf/types/descriptorpb"
)

// roleGrantOnce guards the one-time contribution of the module's permission
// set into qrdb's registries.
var roleGrantOnce sync.Once

// restRouteContributor and mcpToolContributor are populated by the
// rest/mcp-tagged sibling files (module_grants_rest.go,
// module_grants_mcp.go) via their init() when those transports are compiled
// in. registerRoleGrants calls whichever are non-nil, so the derived
// permission set always matches the transports actually built into this
// binary rather than assuming both are always present the way xkms's single
// untagged REST/MCP packages allow.
var (
	restRouteContributor func(set map[string]struct{})
	mcpToolContributor   func(set map[string]struct{})
)

// builtInRoleNames is every qrdb built-in role name registerRoleGrants
// checks before deciding a derived permission is an unclaimed gap. The
// Security Officer role is included in the check (a permission it already
// holds by some other author's declaration must not be re-derived here) but
// is never a grant TARGET below — see the rationale in registerRoleGrants.
var builtInRoleNames = []string{
	qrdbsdk.PermissionRoleSO,
	qrdbsdk.PermissionRoleAdmin,
	qrdbsdk.PermissionRoleOperator,
	qrdbsdk.PermissionRoleUser,
	qrdbsdk.PermissionRoleAuditor,
}

// registerRoleGrants contributes the quicbaas module's declared permission
// set — derived from its REST route table, its MCP tool permission map, and
// the qrdb.v1.permission proto options on the gRPC services this module
// registers (module.go's grpcRegistrations) — into go-qrdb's global
// permission registry and built-in role-grant registry.
//
// Unlike go-xkms's module (a genuinely new permission vocabulary qrdb knows
// nothing about, so xkms blanket-grants its whole derived set to Admin and
// Owner), the majority of quicbaas's derived surface is ALREADY registered
// and ALREADY given deliberate, FIPS 140-3 AS10.03-aware built-in role
// grants by go-qrdb itself (pkg/permission/constants.go's core families —
// keys:*, credentials:*, embedding:* — and pkg/permission/
// broker_dataplane_scope.go's Stage 0.5 broker families — objstore:*,
// orchestrator:*, scheduler:*, messaging:*, parameters:*, dynamic_secrets:*,
// project:*, ollama:*, each already placed on Admin/Operator/User/Auditor by
// pkg/permission/builtin_role_grants.go with real SoD care, e.g. Admin holds
// KeysCreate/Rotate/Delete but deliberately NOT KeysSign, and Owner receives
// every registered permission automatically via role.OwnerRole ->
// permission.All(), independent of any role-grant registration at all).
//
// Blanket-granting the module's full derived set to Admin (as xkms's pattern
// does) would therefore silently WIDEN Admin beyond that upstream curation
// (e.g. handing Admin keys:sign, which qrdb deliberately reserves for
// Operator/User) rather than merely filling an omission. registerRoleGrants
// instead:
//
//  1. Registers every derived permission (RegisterPermissions is an
//     idempotent registry insert) — a pure safety net against a genuinely
//     novel permission string this module declares that qrdb's own packages
//     do not already carry, so role creation never fails with
//     "invalid permission" for anything this module's transports reference.
//  2. Grants a derived permission to Admin (and records the Owner grant for
//     parity with the xkms pattern, though Owner already holds every
//     registered permission regardless) ONLY when qrdb's own registry
//     places it on NONE of its five built-in roles — a permission with zero
//     role holders anywhere is an actual gap (an RPC no built-in role can
//     ever reach), not a withheld grant. The Security Officer role is
//     checked but never a grant target here: SoD guards which permissions
//     may reach SO, and quicbaas's broker data-plane surface is
//     overwhelmingly operational, not module governance.
//
// As of go-qrdb v0.5.1-alpha the confirmed gap is the credentials:* family
// (account.proto's AccountService: store/retrieve/list/delete/rotate) and
// the embedding:* family (embedding:embed, embedding:list_models) — both
// registered in pkg/permission/constants.go but held by no built-in role,
// so Admin cannot drive quicbaas's own AccountService or EmbeddingService
// without this contribution.
func registerRoleGrants() {
	roleGrantOnce.Do(func() {
		set := map[string]struct{}{}

		if restRouteContributor != nil {
			restRouteContributor(set)
		}
		if mcpToolContributor != nil {
			mcpToolContributor(set)
		}
		contributeGRPCPermissions(set)

		perms := make([]qrdbsdk.Permission, 0, len(set))
		for p := range set {
			perms = append(perms, qrdbsdk.Permission(p))
		}
		sort.Slice(perms, func(i, j int) bool { return perms[i] < perms[j] })

		// Which of these this module is the FIRST to name has to be
		// answered BEFORE RegisterPermissions, because that call erases the
		// distinction.
		novel := undeclaredPermissions(perms)

		// Register the full derived vocabulary unconditionally: a pure
		// safety net (idempotent for anything qrdb already carries).
		qrdbsdk.RegisterPermissions(perms...)

		// A permission this module is the first to name MUST declare its
		// tenancy reach. go-qrdb#504 removed the implicit default, so one
		// registered without a scope is refused by the authorizer on every
		// call rather than admitted as self-scope — the transports would
		// deny their own routes. Self-scope is the reach these already had
		// implicitly; a module wanting cross-tenant or tenant-only reach
		// declares it deliberately.
		//
		// Only the novel ones. RegisterPermissionScope OVERWRITES, so
		// declaring the whole derived set self-scope would silently WIDEN
		// every permission whose author already decided otherwise —
		// credentials:* is ScopeCross because the credential store has no
		// tenant namespacing at all (go-qrdb constants.go), and the
		// secrets:* / objstore:* broker families are ScopeTenantOnly so a
		// platform operator cannot reach a tenant's material. Self-scope is
		// the weakest of the three and the one the system-tenant rule lifts
		// across every tenant: re-declaring those here would hand the system
		// tenant exactly the reach their authors withheld.
		if len(novel) > 0 {
			qrdbsdk.RegisterPermissionScope(qrdbsdk.PermissionScopeSelf, novel...)
		}

		var gaps []qrdbsdk.Permission
		for _, p := range perms {
			if hasAnyBuiltInGrant(p) {
				continue
			}
			gaps = append(gaps, p)
		}
		if len(gaps) == 0 {
			return
		}
		qrdbsdk.RegisterPermissionRoleGrant(qrdbsdk.PermissionRoleAdmin, gaps...)
		qrdbsdk.RegisterPermissionRoleGrant(qrdbsdk.RoleNameOwner, gaps...)
	})
}

// undeclaredPermissions returns the members of perms that no layer has
// registered yet, in input order — the permissions this module is the first
// to name and therefore the only ones whose tenancy scope is its to declare.
//
// Registration is the right question to ask because in go-qrdb the two are
// inseparable: every built-in permission is declared in self_scope.go,
// tenant_scope.go, cross_scope.go or self_strict_scope.go beside its
// Register call, the gate test TestEveryPermissionDeclaresItsScope refuses a
// build that adds one without a scope, and the SDK's own permission
// projection states the invariant outright — "a registered permission always
// has a declared scope (the build gate enforces it)". So an already-registered
// permission arrives with its author's decision attached, and this module's
// job is to leave it alone.
//
// It runs once, inside roleGrantOnce, so the per-permission registry lookup
// costs nothing worth batching.
func undeclaredPermissions(perms []qrdbsdk.Permission) []qrdbsdk.Permission {
	var out []qrdbsdk.Permission
	for _, p := range perms {
		if qrdbsdk.IsPermissionRegistered(p) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// hasAnyBuiltInGrant reports whether p is already placed on at least one of
// qrdb's five built-in roles.
func hasAnyBuiltInGrant(p qrdbsdk.Permission) bool {
	for _, role := range builtInRoleNames {
		if qrdbsdk.HasPermissionRoleGrant(role, p) {
			return true
		}
	}
	return false
}

// contributeGRPCPermissions reads the qrdb.v1.permission option off every
// method of every gRPC service this module registers (grpcRegistrations,
// module.go) and adds each declared permission to set. It resolves each
// service's proto FileDescriptor from the process-global registry by the
// path grpc.ServiceDesc.Metadata carries (protoc-gen-go-grpc always stamps
// the source .proto path there), rather than importing one generated
// File_..._proto variable per broker proto file: the module's gRPC surface
// spans seven separate proto files (scheduler, orchestrator, project,
// account, ssh, messaging, embedding) and a new one must not require an
// edit here to be picked up.
func contributeGRPCPermissions(set map[string]struct{}) {
	seen := map[string]struct{}{}
	for _, entry := range grpcRegistrations {
		path, ok := entry.desc.Metadata.(string)
		if !ok || path == "" {
			continue
		}
		if _, done := seen[path]; done {
			continue
		}
		seen[path] = struct{}{}

		fd, err := protoregistry.GlobalFiles.FindFileByPath(path)
		if err != nil {
			continue
		}
		contributeFilePermissions(fd, set)
	}
}

// contributeFilePermissions adds every qrdb.v1.permission method option
// declared in fd's services to set.
func contributeFilePermissions(fd protoreflect.FileDescriptor, set map[string]struct{}) {
	services := fd.Services()
	for i := 0; i < services.Len(); i++ {
		methods := services.Get(i).Methods()
		for j := 0; j < methods.Len(); j++ {
			perm := methodPermission(methods.Get(j))
			if perm == "" || qrdbsdk.Permission(perm) == qrdbsdk.PermissionIdentityGated {
				continue
			}
			set[perm] = struct{}{}
		}
	}
}

// methodPermission returns the (qrdb.v1.permission) option a method
// declares, or "" when it declares none.
func methodPermission(method protoreflect.MethodDescriptor) string {
	opts, ok := method.Options().(*descriptorpb.MethodOptions)
	if !ok || !proto.HasExtension(opts, qrdbv1.E_Permission) {
		return ""
	}
	perm, ok := proto.GetExtension(opts, qrdbv1.E_Permission).(string)
	if !ok {
		return ""
	}
	return perm
}
