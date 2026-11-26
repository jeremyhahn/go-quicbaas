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
// Everything here is dead ON PURPOSE: by owner ruling, or because there is no
// REST or gRPC grant to copy, so choosing a permission would be a guess at a
// security boundary rather than a transport-parity fix.

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
		// The federation provider and account-link tools take their REST
		// route's grant (authz_qrdb.go). The login handshake is public on
		// REST (routes_iam.go) and gRPC (publicGRPCMethods), so there is no
		// grant to copy, and an MCP public entry would be wider than parity.
		reason: "REST and gRPC serve the federated login handshake public, so there is no grant " +
			"to copy, and a public MCP entry would be wider than transport parity",
		resolution: "an owner decision on whether the handshake is served over MCP at all",
		tools: []string{
			"quicbaas_iam_federation_beginlogin",
			"quicbaas_iam_federation_handlecallback",
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
		// The consent and deletion-request tools take their REST route's
		// grant (authz_qrdb.go). By owner ruling, irreversible erasure
		// processing and export generation are refused on every transport;
		// gRPC leaves GenerateExport, ProcessDeletion and RequestExport
		// unmapped. The export-request tool stays dead with them: the request
		// starts export generation in the background with no further check.
		reason: "owner ruling: irreversible erasure processing and export generation are " +
			"refused on every transport; an export request starts export generation in the " +
			"background with no further check",
		resolution: "a new owner ruling",
		tools: []string{
			"quicbaas_iam_gdpr_deletion_process",
			"quicbaas_iam_gdpr_export_generate",
			"quicbaas_iam_gdpr_export_request",
		},
	},
	{
		// The gdpr export-status, deletion-cancel and deletion-status handlers
		// look the export or deletion request up by ID with no tenant check
		// after authorization, so a grant would let a caller read or cancel
		// another tenant's request.
		reason: "the handler looks the export or deletion request up by ID with no tenant " +
			"check, so any grant would reach another tenant's record",
		resolution: "a tenant check in the handler, then the REST route's grant",
		tools: []string{
			"quicbaas_iam_gdpr_export_status",
			"quicbaas_iam_gdpr_deletion_cancel",
			"quicbaas_iam_gdpr_deletion_status",
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
