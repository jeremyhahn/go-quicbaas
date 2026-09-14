//go:build mcp

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

package mcp

import (
	"context"
	"sort"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// ToolSpec describes one tool of the quicbaas MCP surface for the
// unified-API-server module (pkg/apimodule): the registry tool it dispatches
// to, the (sanitized) name it registers under on the real mcp-go server, its
// authorization contract from the qrdb permission tables, and the tool's
// declared input schema.
type ToolSpec struct {
	// Name is the registry tool name the bespoke JSON-RPC server serves
	// (e.g. "quicbaas_kms_generate_key", "xkms.generateKey").
	Name string
	// Tool is the MCP tool name: Name sanitized to the MCP tool-name
	// charset ^[A-Za-z0-9_-]+$ (dots become underscores). Names already in
	// the charset are kept verbatim.
	Tool string
	// Resource and Action carry the tool's qrdb-canonical permission from
	// the MCP authorization tables (authz_qrdb.go); empty when the tool is
	// public or unmapped.
	Resource string
	Action   string
	// Public marks the unauthenticated discovery/health tools.
	Public bool
	// Description is the tool's registry description.
	Description string
	// InputSchema is the tool's declared JSON schema (the same schema the
	// bespoke server advertises via tools/list).
	InputSchema map[string]interface{}
}

// ToolName sanitizes a registry tool name into the MCP tool-name charset
// ^[A-Za-z0-9_-]+$: every disallowed rune (the go-xkms compatibility tools
// carry dots) maps deterministically to an underscore.
func ToolName(name string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			return r
		default:
			return '_'
		}
	}, name)
}

// qrdbNativeDuplicateTools are quicbaas MCP registry tools whose name and
// behavior duplicate a tool go-qrdb registers NATIVELY on the unified
// server's MCP surface (the SAME "qrdb-proxy plane" family already excluded
// from the gRPC module surface -- pkg/apimodule/module.go's RegisterGRPC doc
// comment: "Barrier, TenantBarrier, Ceremony, VectorDB, VectorAdmin: their
// handlers forward every verb through the embedded go-qrdb node's own SDK
// client... qrdb serves the identical capability natively on the unified
// server; registering quicbaas's wrapper here would duplicate that surface
// under a redundant proto name"). The MCP layer never received the
// equivalent cleanup, so composing quicbaas's module alongside go-qrdb's own
// MCP tool registrations on one node panics at construction time
// ("duplicate permission registration for <tool>").
//
// Two provenances confirmed by direct inspection, not assumption:
//   - qrdb_role_* route through service.rbacService, whose role verbs reach the
//     qrdb-backed iam.IdentityStore (pkg/service/rbac_role_store.go). They are
//     LIVE, and they are listed here for the duplication reason above and for
//     no other reason: qrdb serves the same role capability natively, so a
//     composed node would register the name twice and panic. The user MUTATION
//     trio that used to share this bullet -- qrdb_user_create/delete/list --
//     really was dead (it was gated on an auth manager production never wired)
//     and was deleted outright in issue #905 rather than excluded here; the
//     qrdb_user_get* / qrdb_user_authfactor_* reads below are live passthroughs
//     listed for duplication, like the role tools.
//   - qrdb_credential_*/qrdb_vector_*/qrdb_ts_*/qrdb_barrier_*/
//     qrdb_tenant_barrier_*/qrdb_ceremony_* handlers take a
//     qrdbsdk.Service*Servicer parameter directly (e.g. executeCredentialSubmit
//     (ctx, svc qrdbsdk.ServiceCredentialServicer, ...)) -- genuine live
//     passthroughs to the embedded qrdb node's own SDK-exposed service.
//
// ModuleTools OMITS every one of these; the bespoke JSON-RPC server
// (ToolRegistry.registerTools) is UNCHANGED and keeps serving them on a
// standalone (non-composed) deployment.
var qrdbNativeDuplicateTools = map[string]struct{}{
	"qrdb_barrier_enable_encryption":     {},
	"qrdb_barrier_initialize":            {},
	"qrdb_barrier_recovery_delete":       {},
	"qrdb_barrier_recovery_generate":     {},
	"qrdb_barrier_recovery_recover":      {},
	"qrdb_barrier_recovery_status":       {},
	"qrdb_barrier_rekey":                 {},
	"qrdb_barrier_root_token":            {},
	"qrdb_barrier_seal":                  {},
	"qrdb_barrier_shamir_initialize":     {},
	"qrdb_barrier_shamir_unseal_share":   {},
	"qrdb_barrier_shamir_unseal_shares":  {},
	"qrdb_barrier_status":                {},
	"qrdb_barrier_unseal":                {},
	"qrdb_ceremony_begin_claim_share":    {},
	"qrdb_ceremony_claim_cert_begin":     {},
	"qrdb_ceremony_claim_cert_complete":  {},
	"qrdb_ceremony_complete_claim_share": {},
	"qrdb_ceremony_initialize":           {},
	"qrdb_ceremony_sign_csr":             {},
	"qrdb_ceremony_status":               {},
	"qrdb_credential_delete":             {},
	"qrdb_credential_gc_expired":         {},
	"qrdb_credential_list":               {},
	"qrdb_credential_list_versions":      {},
	"qrdb_credential_retrieve_version":   {},
	"qrdb_credential_rotate":             {},
	"qrdb_credential_strategy":           {},
	"qrdb_credential_submit":             {},
	"qrdb_delete":                        {},
	"qrdb_get":                           {},
	"qrdb_health":                        {},
	"qrdb_list":                          {},
	"qrdb_put":                           {},
	"qrdb_role_assign":                   {},
	"qrdb_role_create":                   {},
	"qrdb_role_delete":                   {},
	"qrdb_role_list":                     {},
	"qrdb_tenant_barrier_init":           {},
	"qrdb_tenant_barrier_seal":           {},
	"qrdb_tenant_barrier_status":         {},
	"qrdb_tenant_barrier_unseal":         {},
	"qrdb_tenant_barrier_unseal_share":   {},
	"qrdb_tenant_update_entitlements":    {},
	"qrdb_tenant_update_quotas":          {},
	"qrdb_ts_delete_series":              {},
	"qrdb_ts_get_retention_policy":       {},
	"qrdb_ts_list_metrics":               {},
	"qrdb_ts_query":                      {},
	"qrdb_ts_set_retention_policy":       {},
	"qrdb_ts_write":                      {},
	"qrdb_ts_write_batch":                {},
	"qrdb_ts_write_point":                {},
	"qrdb_user_authfactor_add":           {},
	"qrdb_user_authfactor_get":           {},
	"qrdb_user_authfactor_list":          {},
	"qrdb_user_authfactor_login":         {},
	"qrdb_user_authfactor_relabel":       {},
	"qrdb_user_authfactor_remove":        {},
	"qrdb_user_authfactor_rotate":        {},
	"qrdb_user_get":                      {},
	"qrdb_user_get_by_cert_thumbprint":   {},
	"qrdb_user_get_by_username":          {},
	"qrdb_vector_batch_delete":           {},
	"qrdb_vector_batch_upsert":           {},
	"qrdb_vector_create_namespace":       {},
	"qrdb_vector_delete":                 {},
	"qrdb_vector_delete_namespace":       {},
	"qrdb_vector_get":                    {},
	"qrdb_vector_get_namespace_info":     {},
	"qrdb_vector_list":                   {},
	"qrdb_vector_list_namespaces":        {},
	"qrdb_vector_rebuild_index":          {},
	"qrdb_vector_search_by_id":           {},
	"qrdb_vector_search_similar":         {},
	"qrdb_vector_update_metadata":        {},
	"qrdb_vector_upsert":                 {},
}

// ModuleTools enumerates the full quicbaas MCP tool surface for the
// unified-API-server module, sorted by registry tool name, EXCLUDING
// qrdbNativeDuplicateTools (see its doc comment). The enumeration source is
// the SAME registration path the bespoke server uses
// (ToolRegistry.registerTools), so the module surface cannot drift from the
// served tool set beyond that deliberate exclusion. Authorization contracts
// come from the canonical qrdb permission tables: IsPublicTool marks the
// discovery/health tools, and getToolQRDBPermission yields the
// qrdb-canonical resource:action. A tool with neither is enumerated with an
// empty permission -- the module OMITS it from MCPToolPermissions so it
// stays fail-closed on the unified server, mirroring the bespoke gate's
// default-deny posture for unmapped tools.
func ModuleTools() []ToolSpec {
	reg := &ToolRegistry{tools: make([]ToolDefinition, 0, 512)}
	reg.registerTools()

	specs := make([]ToolSpec, 0, len(reg.tools))
	for _, def := range reg.tools {
		if _, excluded := qrdbNativeDuplicateTools[def.Name]; excluded {
			continue
		}
		// go-xkms's own ServiceModule registers its complete JSON-RPC method
		// surface as MCP tools natively (sdk/go/apimodule/module_mcp.go);
		// quicbaas's "xkms." registry family (tools_xkms.go) predates that
		// and is a compatibility wrapper around the exact same capability
		// (cert/key/seal/sign/verify/encrypt/decrypt), the MCP-surface twin
		// of TestModule_RegisterGRPC_ExcludesXKMSCanonicalSurface
		// (pkg/apimodule/module_test.go).
		if strings.HasPrefix(def.Name, "xkms.") {
			continue
		}
		spec := ToolSpec{
			Name:        def.Name,
			Tool:        ToolName(def.Name),
			Description: def.Description,
			InputSchema: def.InputSchema,
		}
		if IsPublicTool(def.Name) {
			spec.Public = true
		} else if resource, action, ok := getToolQRDBPermission(def.Name); ok {
			spec.Resource = resource
			spec.Action = action
		}
		specs = append(specs, spec)
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].Name < specs[j].Name })
	return specs
}

// InvokeTool dispatches one tool call against s, running the connectionless
// core of the bespoke tools/call path: the sealed-window gate, the upstream
// identity projection, and the registry dispatcher. It backs the
// unified-API-server module's real MCP tools (pkg/apimodule RegisterMCP).
//
// Authorization is deliberately NOT re-checked here: the bespoke gate is a
// platform-token check (CheckToolAuthorizationWithContext), and unified-
// server callers authenticate with qrdb identities — go-qrdb's MCP tool
// registry authorizes each call against the permissions the module declares
// via MCPToolPermissions before this dispatch runs. The qrdb identity is
// projected into the quicbaas principal contract so service-layer per-key
// authorization and tenant isolation observe the caller the unified server
// authorized, exactly as the token path injects its validated Subject.
func (s *Server) InvokeTool(ctx context.Context, name string, args map[string]interface{}) (interface{}, error) {
	if s.sealedWindowDeniesTool(name) {
		return nil, &SealedWindowError{Tool: name}
	}
	return s.tools.ExecuteTool(upstreamToolContext(ctx), name, args)
}

// upstreamToolContext projects a qrdb identity already authenticated by the
// unified server into the quicbaas principal contract (Subject + tenant
// binding), matching the injection the token-based gate performs. A context
// without a qrdb identity is returned unchanged.
func upstreamToolContext(ctx context.Context) context.Context {
	identity := qrdbsdk.IdentityFromContext(ctx)
	if identity == nil {
		return ctx
	}
	ctx = auth.SubjectIntoContext(ctx, interfaces.Subject{
		ID:            identity.Subject,
		Username:      identity.Subject,
		TenantID:      identity.TenantID,
		Roles:         identity.Roles,
		Authenticated: true,
	})
	if identity.TenantID != "" {
		ctx = iam.ContextWithTenant(ctx, identity.TenantID)
	}
	return ctx
}
