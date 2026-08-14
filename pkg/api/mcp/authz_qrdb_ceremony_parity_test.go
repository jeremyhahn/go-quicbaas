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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// Cross-transport parity guards for the two privileged qrdb-mirror surfaces that
// used to resolve through a prefix catch-all: the initialization ceremony and
// versioned credential rotation.
//
// The absence of a test like this is why the ceremony escalation shipped. The
// analogues to follow are TestBarrierToolsGranularAuthzMapping (authz_qrdb_test.go)
// and TestMCPPermissions_CertificateToolsMatchGRPC (authz_qrdb_coverage_test.go).
// =============================================================================

// wantCeremonyPerms is the gRPC-canonical table for the ceremony surface, copied
// byte-for-byte from pkg/api/grpc/authz_mapping.go
// (quicbaas.ceremony.v1.Ceremony/* and the qrdb.v1.QRDBService Ceremony*
// mirror). It is also the action column of ceremonyGranularOps on REST
// (pkg/api/rest/authz_pep_mapping.go) and ceremonySubOpPermissions on QUIC
// (pkg/api/quic/handler_authz_qrdb.go).
var wantCeremonyPerms = map[string]qrdbToolResource{
	"qrdb_ceremony_initialize":              {"ceremony", "initialize"},
	"qrdb_ceremony_status":                  {"ceremony", "status"},
	"qrdb_ceremony_claim_cert_begin":        {"ceremony", "claim_cert"},
	"qrdb_ceremony_claim_cert_complete":     {"ceremony", "claim_cert"},
	"qrdb_ceremony_begin_claim_share":       {"ceremony", "claim_share_begin"},
	"qrdb_ceremony_complete_claim_share":    {"ceremony", "claim_share_complete"},
	"qrdb_ceremony_sign_csr":                {"ceremony", "sign_csr"},
	"qrdb_ceremony_enroll_external_officer": {"ceremony", "enroll_custodian"},
	"quicbaas_ceremony_list_officers":       {"ceremony", "status"},
}

// TestCeremonyToolsGranularAuthzMapping is the per-tool exhaustive guard for the
// ceremony surface: it walks ceremonyToolNames (the authoritative dispatch map
// in tools_ceremony.go) and asserts getToolQRDBPermission resolves each tool to
// the gRPC-canonical permission.
func TestCeremonyToolsGranularAuthzMapping(t *testing.T) {
	t.Parallel()

	for name := range ceremonyToolNames {
		if _, ok := wantCeremonyPerms[name]; !ok {
			t.Errorf("ceremonyToolNames contains %q which has no entry in wantCeremonyPerms; "+
				"add it to both qrdbToolExactMapping and this table", name)
		}
	}
	for name := range wantCeremonyPerms {
		if _, ok := ceremonyToolNames[name]; !ok {
			t.Errorf("wantCeremonyPerms contains %q which is not in ceremonyToolNames; "+
				"remove the stale expectation", name)
		}
	}

	for name, want := range wantCeremonyPerms {
		t.Run(name, func(t *testing.T) {
			res, act, ok := getToolQRDBPermission(name)
			require.Truef(t, ok, "ceremony tool %q is unmapped; default-deny hides it from tools/list", name)
			assert.Equalf(t, want.resource, res, "tool %q: wrong resource", name)
			assert.Equalf(t, want.action, act, "tool %q: wrong action", name)
		})
	}
}

// TestCeremonyWriteToolsAreNotStatus is the direct regression assertion for the
// escalation this change closes. qrdb grants ceremony:status to Admin, Operator
// AND the read-only Auditor role (go-qrdb/pkg/permission/builtin_role_grants.go),
// while CeremonyInitialize / CeremonySignCSR / CeremonyEnrollExternalOfficer are
// Security-Officer exclusive. Resolving those tools to ceremony:status let an
// Auditor mint the CA, sign arbitrary certificates from the ceremony CA, seal a
// Shamir share to an attacker-supplied key and claim officer shares.
//
// Restoring the "qrdb_ceremony" prefix catch-all fails this test: every tool
// below would resolve to ceremony:status again.
func TestCeremonyWriteToolsAreNotStatus(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"qrdb_ceremony_initialize",
		"qrdb_ceremony_sign_csr",
		"qrdb_ceremony_enroll_external_officer",
		"qrdb_ceremony_claim_cert_begin",
		"qrdb_ceremony_claim_cert_complete",
		"qrdb_ceremony_begin_claim_share",
		"qrdb_ceremony_complete_claim_share",
	} {
		t.Run(name, func(t *testing.T) {
			res, act, ok := getToolQRDBPermission(name)
			require.True(t, ok)
			assert.NotEqualf(t, "ceremony:status", res+":"+act,
				"ceremony write tool %q must not authorize under the read-only "+
					"ceremony:status, which qrdb grants to the Auditor role", name)
		})
	}
}

// TestCeremonyToolsFailClosedOnUnknownTool asserts a hypothetical future ceremony
// tool that has not been added to qrdbToolExactMapping is default-denied rather
// than admitted via a catch-all. This is the invariant the deleted
// "qrdb_ceremony" prefix entry violated.
func TestCeremonyToolsFailClosedOnUnknownTool(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"qrdb_ceremony_export_ca_key",
		"qrdb_ceremony",
	} {
		_, _, ok := getToolQRDBPermission(name)
		assert.Falsef(t, ok,
			"%q must be default-denied, not admitted via a ceremony catch-all", name)
	}
}

// wantCredentialPerms is the gRPC-canonical table for the versioned
// named-credential rotation surface, copied byte-for-byte from
// pkg/api/grpc/authz_mapping.go (qrdb.v1.QRDBService/Credential* entries).
var wantCredentialPerms = map[string]qrdbToolResource{
	"qrdb_credential_submit":           {"credentials", "submit"},
	"qrdb_credential_strategy":         {"credentials", "strategy"},
	"qrdb_credential_delete":           {"credentials", "delete"},
	"qrdb_credential_list":             {"credentials", "list"},
	"qrdb_credential_rotate":           {"credentials", "rotate"},
	"qrdb_credential_retrieve_version": {"credentials", "retrieve_version"},
	"qrdb_credential_list_versions":    {"credentials", "list_versions"},
	"qrdb_credential_gc_expired":       {"credentials", "gc_expired"},
}

// TestCredentialToolsGranularAuthzMapping is the per-tool exhaustive guard for
// the credential surface. The prefix catch-all it replaces collapsed submit /
// rotate / gc_expired onto credentials:store, and the shorter sibling prefixes
// swallowed list_versions and retrieve_version, so five verbs authorized under a
// permission that was not theirs.
func TestCredentialToolsGranularAuthzMapping(t *testing.T) {
	t.Parallel()

	for name := range credentialToolNames {
		if _, ok := wantCredentialPerms[name]; !ok {
			t.Errorf("credentialToolNames contains %q which has no entry in "+
				"wantCredentialPerms; add it to both qrdbToolExactMapping and this table", name)
		}
	}
	for name := range wantCredentialPerms {
		if _, ok := credentialToolNames[name]; !ok {
			t.Errorf("wantCredentialPerms contains %q which is not in "+
				"credentialToolNames; remove the stale expectation", name)
		}
	}

	for name, want := range wantCredentialPerms {
		t.Run(name, func(t *testing.T) {
			res, act, ok := getToolQRDBPermission(name)
			require.Truef(t, ok, "credential tool %q is unmapped; default-deny hides it from tools/list", name)
			assert.Equalf(t, want.resource, res, "tool %q: wrong resource", name)
			assert.Equalf(t, want.action, act, "tool %q: wrong action", name)
		})
	}
}

// TestCredentialToolsEachVerbIsDistinct asserts no two credential verbs collapse
// onto the same action, which is precisely what the catch-all did. Only then does
// a grant of one verb stop implying the others.
func TestCredentialToolsEachVerbIsDistinct(t *testing.T) {
	t.Parallel()

	seen := make(map[string]string, len(wantCredentialPerms))
	for name := range wantCredentialPerms {
		res, act, ok := getToolQRDBPermission(name)
		require.True(t, ok, name)
		perm := res + ":" + act
		if other, dup := seen[perm]; dup {
			t.Errorf("credential tools %q and %q both resolve to %s; each verb must "+
				"carry its own permission", other, name, perm)
		}
		seen[perm] = name
	}
}

// TestCredentialToolsFailClosedOnUnknownTool asserts a future credential tool
// fails closed instead of inheriting credentials:store from a catch-all.
func TestCredentialToolsFailClosedOnUnknownTool(t *testing.T) {
	t.Parallel()

	for _, name := range []string{
		"qrdb_credential_export_all",
		"qrdb_credential",
	} {
		_, _, ok := getToolQRDBPermission(name)
		assert.Falsef(t, ok,
			"%q must be default-denied, not admitted via a credential catch-all", name)
	}
}
