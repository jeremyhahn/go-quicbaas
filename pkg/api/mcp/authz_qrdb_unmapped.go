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

// This file is the declared, reviewed inventory of MCP tools that are
// registered but deliberately carry NO qrdb permission mapping.
//
// The MCP authz gate is default-deny: an unmapped, non-public tool is denied
// AND filtered out of tools/list, so it is invisible to every MCP client.
// Registering a tool without a mapping therefore ships a DEAD tool rather than
// a working one, and nothing used to notice — that is how the six base
// certificate tools stayed invisible.
//
// TestMCPPermissions_EveryToolMappedOrPublic walks every registered tool and
// requires it to be in exactly one of three buckets: public (publicTools),
// mapped (qrdbToolExactMapping / qrdbToolPrefixMapping), or listed HERE with a
// reason. A newly registered tool matches none of the three and fails the
// build, which is the point: shipping a tool dead must be an explicit,
// reviewed decision, never an accident.
//
// Everything here is dead ON PURPOSE, pending an owner decision. None of these
// surfaces has a REST or gRPC PEP rule to copy, so choosing a permission would
// be a guess at a security boundary rather than a transport-parity fix.

// unmappedToolGroup declares one family of registered-but-unmapped tools
// together with the reason no permission was assigned and what has to happen
// for the family to become reachable.
type unmappedToolGroup struct {
	// reason states why no mapping exists.
	reason string

	// resolution states what has to happen for the tools to become reachable.
	resolution string

	// tools are the exact registered tool names the group covers.
	tools []string
}

// unmappedToolGroups is the source of truth for deliberately dead tools.
var unmappedToolGroups = []unmappedToolGroup{
	{
		// Neither pkg/api/rest/authz_pep_mapping.go nor
		// pkg/api/grpc/authz_mapping.go carries a rule for the federation
		// surface, and the qrdb registry defines no federation:* family. The
		// verbs span provider CRUD (an administrative trust decision: whose
		// assertions this tenant accepts) and the login/callback handshake
		// (pre-auth by construction). One borrowed family would be wrong for
		// half of them.
		reason: "no REST/gRPC PEP rule and no qrdb federation:* family; provider CRUD " +
			"and the login handshake need different boundaries",
		resolution: "register federation:* in go-qrdb, then map all five transports together",
		tools: []string{
			"quicbaas_iam_federation_createprovider",
			"quicbaas_iam_federation_getprovider",
			"quicbaas_iam_federation_listproviders",
			"quicbaas_iam_federation_updateprovider",
			"quicbaas_iam_federation_deleteprovider",
			"quicbaas_iam_federation_beginlogin",
			"quicbaas_iam_federation_handlecallback",
			"quicbaas_iam_federation_linkaccount",
			"quicbaas_iam_federation_unlinkaccount",
			"quicbaas_iam_federation_getuserlinks",
		},
	},
	{
		// Same gap as federation. users:update is the nearest verb the REST
		// table borrows for session mutations, but binding a foreign identity
		// to a local user is strictly stronger than touching a session, so it
		// is not copied here.
		reason: "no REST/gRPC PEP rule; binding a foreign identity to a local user is " +
			"stronger than the users:update the session rules borrow",
		resolution: "register an identity-binding permission in go-qrdb, then map all transports",
		tools: []string{
			"quicbaas_iam_identity_linkidentity",
			"quicbaas_iam_identity_unlinkidentity",
			"quicbaas_iam_identity_listuseridentities",
		},
	},
	{
		// No sibling rule and no qrdb gdpr:* / consent:* family. Erasure
		// processing destroys tenant data irreversibly, so it must not be
		// folded into users:delete without an explicit decision.
		reason: "no REST/gRPC PEP rule and no qrdb gdpr:*/consent:* family; irreversible " +
			"erasure must not silently borrow users:delete",
		resolution: "register the GDPR permission family in go-qrdb, then map all transports",
		tools: []string{
			"quicbaas_iam_gdpr_consent_grant",
			"quicbaas_iam_gdpr_consent_withdraw",
			"quicbaas_iam_gdpr_consent_get",
			"quicbaas_iam_gdpr_consent_list",
			"quicbaas_iam_gdpr_deletion_request",
			"quicbaas_iam_gdpr_deletion_status",
			"quicbaas_iam_gdpr_deletion_cancel",
			"quicbaas_iam_gdpr_deletion_process",
			"quicbaas_iam_gdpr_export_request",
			"quicbaas_iam_gdpr_export_status",
			"quicbaas_iam_gdpr_export_generate",
		},
	},
	{
		// No sibling rule. qrdb registers permissions:list and
		// permissions:resolve, but those READ a subject's effective
		// permissions; they are not a write capability over the policy
		// documents that produce them. Reusing them for policy CRUD would let
		// any principal that can run an access check rewrite the rules the
		// check evaluates.
		reason: "no REST/gRPC PEP rule; permissions:list/resolve are READ verbs over " +
			"effective permissions and must not be reused to authorize policy writes",
		resolution: "register a policy write family in go-qrdb, then map all transports",
		tools: []string{
			"quicbaas_iam_policy_create",
			"quicbaas_iam_policy_get",
			"quicbaas_iam_policy_list",
			"quicbaas_iam_policy_update",
			"quicbaas_iam_policy_delete",
			"quicbaas_iam_policy_test",
			"quicbaas_iam_policy_validate",
		},
	},
	{
		// No REST or gRPC PEP rule for the MCP MFA surface, and the qrdb
		// registry offers TWO candidate families for the same concept:
		// authfactor:* (challenge/enroll/list/revoke/verify) and
		// user_authfactors:* (add/get/list/remove/rotate/relabel/login, which
		// the REST WebAuthn mount and the /v1/users/factors wire mount already
		// use). Picking one here would fork the factor vocabulary a third way.
		reason: "no REST/gRPC PEP rule, and qrdb registers TWO candidate factor families " +
			"(authfactor:* and user_authfactors:*)",
		resolution: "pick one factor vocabulary for the whole stack, then map MCP onto it",
		tools: []string{
			"quicbaas_mfa_enroll_begin",
			"quicbaas_mfa_enroll_complete",
			"quicbaas_mfa_enroll_cancel",
			"quicbaas_mfa_challenge_begin",
			"quicbaas_mfa_challenge_verify",
			"quicbaas_mfa_status",
			"quicbaas_mfa_disable",
			"quicbaas_mfa_backup_codes_generate",
			"quicbaas_mfa_backup_codes_status",
			"quicbaas_mfa_backup_codes_verify",
		},
	},
	{
		// Login is pre-authentication on every sibling, so the MCP equivalent
		// would be membership in publicTools. login_begin stays dead instead:
		// every call allocates a login attempt in the pinned go-qrdb/sdk/go
		// (transport/embedded BeginLogin), which keeps the caller's hint and
		// tenant strings until FinishLogin removes the attempt. Nothing
		// evicts an expired attempt and nothing caps their number, and
		// login_finish is refused on MCP (the next group), so an attempt
		// begun here would never be removed: repeated calls would grow the
		// daemon's memory without bound.
		reason: "login_begin allocates a login attempt that only login_finish removes; the pinned " +
			"go-qrdb/sdk/go neither expires nor caps abandoned attempts, and login_finish is refused on MCP",
		resolution: "pin a go-qrdb/sdk/go that expires and caps abandoned login attempts, cover the " +
			"expiry with a test, then add to publicTools",
		tools: []string{
			"quicbaas_login_begin",
		},
	},
	{
		// login_finish binds the session to the caller's VERIFIED mTLS leaf
		// (service.WithVerifiedPeerCert), which REST and gRPC read from their
		// own handshakes. The MCP tools are served on go-qrdb's MCP listener,
		// whose verified chain reaches a tool handler only through go-qrdb's
		// SDK verified-chain seam (go-qrdb#1181); reading PeerCertificates[0]
		// or verifying in go-quicbaas is ruled out (ruling 172). Public
		// without the leaf, every mTLS login would answer
		// MTLSPeerCertRequiredError, so the tool stays dead.
		reason: "login_finish needs the verified mTLS leaf, which the MCP listener exposes only " +
			"through go-qrdb's SDK verified-chain seam (go-qrdb#1181), absent from the pinned go-qrdb/sdk/go",
		resolution: "pin the go-qrdb alpha carrying #1181, put its verified leaf on the context, then add to publicTools",
		tools: []string{
			"quicbaas_login_finish",
		},
	},
}

// unmappedTools indexes unmappedToolGroups by exact tool name.
var unmappedTools = buildUnmappedTools()

// buildUnmappedTools flattens unmappedToolGroups into a name -> group lookup.
func buildUnmappedTools() map[string]unmappedToolGroup {
	out := make(map[string]unmappedToolGroup)
	for _, group := range unmappedToolGroups {
		for _, name := range group.tools {
			out[name] = group
		}
	}
	return out
}

// isDeclaredUnmappedTool reports whether the tool is a reviewed, deliberately
// dead tool rather than an accidentally unmapped one.
func isDeclaredUnmappedTool(name string) bool {
	_, ok := unmappedTools[name]
	return ok
}
