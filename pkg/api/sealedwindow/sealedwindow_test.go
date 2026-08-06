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

package sealedwindow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// TestTable_EveryVerbPresent asserts every canonical verb has a Table entry
// and the verb count matches qrdb's canonical 34-verb table.
func TestTable_EveryVerbPresent(t *testing.T) {
	require.Len(t, Verbs, 34, "verb set must mirror qrdb's canonical 34-verb table")
	seen := make(map[Verb]struct{}, len(Verbs))
	for _, v := range Verbs {
		_, dup := seen[v]
		require.False(t, dup, "duplicate verb %q", v)
		seen[v] = struct{}{}
		_, ok := Table[v]
		assert.True(t, ok, "verb %q missing from Table", v)
	}
	assert.Len(t, Table, len(Verbs), "Table must not carry entries outside Verbs")
}

// TestRESTAllowlist_NoDriftFromQRDB asserts the quicbaas REST allowlist is a
// superset of qrdb's canonical REST sealed-window allowlist, modulo the ONE
// documented substitution: qrdb's /v1/cluster/ peer key-transport prefix maps
// onto quicbaas's /api/v1/cluster/secret delivery endpoint.
func TestRESTAllowlist_NoDriftFromQRDB(t *testing.T) {
	ours := make(map[string]struct{})
	for _, p := range RESTAllowlist() {
		ours[p] = struct{}{}
	}

	for _, canonical := range qrdbsdk.SealedWindowRESTAllowlist() {
		if canonical == "/v1/cluster/" {
			_, ok := ours["/api/v1/cluster/secret"]
			assert.True(t, ok, "quicbaas substitution for qrdb %q missing", canonical)
			continue
		}
		_, ok := ours[canonical]
		assert.True(t, ok, "qrdb canonical REST allowlist entry %q missing from quicbaas derivation", canonical)
	}
}

// TestRESTAllowlist_Deduplicated asserts shared paths (e.g. the officers
// GET/POST pair) appear exactly once.
func TestRESTAllowlist_Deduplicated(t *testing.T) {
	seen := make(map[string]int)
	for _, p := range RESTAllowlist() {
		seen[p]++
	}
	for p, n := range seen {
		assert.Equal(t, 1, n, "REST allowlist entry %q duplicated", p)
	}
}

// TestGRPCAllowlist_NoDriftFromQRDB asserts every method tail in qrdb's
// canonical gRPC sealed-window allowlist is present in the quicbaas
// derivation, and the quicbaas extras are exactly the two documented
// quicbaas-only mirror surfaces (BarrierShamirVerify on quicbaas.barrier.v1
// and CeremonyListOfficers on quicbaas.ceremony.v1).
func TestGRPCAllowlist_NoDriftFromQRDB(t *testing.T) {
	ours := make(map[string]struct{})
	for _, m := range GRPCAllowlist() {
		ours[m] = struct{}{}
	}
	canonical := make(map[string]struct{})
	for _, m := range qrdbsdk.GRPCSealedWindowAllowlist {
		canonical[m] = struct{}{}
		_, ok := ours[m]
		assert.True(t, ok, "qrdb canonical gRPC allowlist tail %q missing from quicbaas derivation", m)
	}

	extras := map[string]struct{}{
		"BarrierShamirVerify":  {},
		"CeremonyListOfficers": {},
	}
	for m := range ours {
		if _, ok := canonical[m]; ok {
			continue
		}
		_, allowed := extras[m]
		assert.True(t, allowed, "unexpected quicbaas-only gRPC allowlist tail %q (extend the documented extras deliberately)", m)
	}
}

// TestQRDBQUICAllowlist_NoDriftFromQRDB asserts the qrdb-quic ALPN bridge
// allowlist covers qrdb's canonical native-QUIC sealed-window allowlist
// verbatim (the bridge speaks qrdb's wire method names), plus exactly one
// documented quicbaas extra: the bridge serves ceremony.EnrollExternalOfficer
// as a real handler while qrdb's own native QUIC surface does not carry the
// verb (it is REST/gRPC-only there).
func TestQRDBQUICAllowlist_NoDriftFromQRDB(t *testing.T) {
	ours := QRDBQUICAllowlist()
	for m := range qrdbsdk.QUICSealedWindowAllowlist {
		_, ok := ours[m]
		assert.True(t, ok, "qrdb canonical QUIC method %q missing from quicbaas bridge allowlist", m)
	}
	extras := map[string]struct{}{
		"ceremony.EnrollExternalOfficer": {},
	}
	for m := range ours {
		if _, ok := qrdbsdk.QUICSealedWindowAllowlist[m]; ok {
			continue
		}
		_, allowed := extras[m]
		assert.True(t, allowed, "unexpected quicbaas-only qrdb-quic bridge allowlist method %q", m)
	}
}

// TestMCPAllowlist_NoDriftFromQRDB asserts every tool in qrdb's canonical MCP
// sealed-window allowlist has its quicbaas counterpart, and the quicbaas
// extras are exactly the documented quicbaas-native tool names.
func TestMCPAllowlist_NoDriftFromQRDB(t *testing.T) {
	ours := MCPAllowlist()
	canonical := qrdbsdk.MCPSealedWindowAllowlist
	for tool := range canonical {
		// qrdb_health / qrdb_status have no quicbaas MCP tool (MCP health is
		// an HTTP endpoint outside the tool dispatcher, always reachable).
		if tool == "qrdb_health" || tool == "qrdb_status" {
			continue
		}
		_, ok := ours[tool]
		assert.True(t, ok, "qrdb canonical MCP tool %q missing from quicbaas derivation", tool)
	}

	extras := map[string]struct{}{
		"quicbaas_ceremony_list_officers": {},
	}
	for tool := range ours {
		if _, ok := canonical[tool]; ok {
			continue
		}
		_, allowed := extras[tool]
		assert.True(t, allowed, "unexpected quicbaas-only MCP allowlist tool %q", tool)
	}
}

// TestAllowlists_DeliberateExclusions pins the never-allowlisted operations:
// seal, enable-encryption, share management, the per-tenant barrier surface,
// and the credential rotation surface must not appear on ANY transport
// allowlist while sealed.
func TestAllowlists_DeliberateExclusions(t *testing.T) {
	// REST: no seal / enable-encryption paths.
	for _, p := range RESTAllowlist() {
		assert.False(t, strings.HasSuffix(p, "/seal"), "REST allowlist must not carry a seal path: %q", p)
		assert.False(t, strings.Contains(p, "enable-encryption"), "REST allowlist must not carry enable-encryption: %q", p)
		assert.False(t, strings.HasPrefix(p, "/v1/credentials"), "REST allowlist must not carry the credential surface: %q", p)
	}

	// gRPC tails.
	grpcDenied := []string{
		"BarrierSeal", "BarrierShamirSealVote", "BarrierEnableEncryption",
		"BarrierShamirListShares", "BarrierShamirDeleteShare", "BarrierShamirDeleteAllShares",
		"CredentialSubmit", "CredentialRetrieve", "CredentialRotate",
	}
	grpcAllow := make(map[string]struct{})
	for _, m := range GRPCAllowlist() {
		grpcAllow[m] = struct{}{}
	}
	for _, m := range grpcDenied {
		_, ok := grpcAllow[m]
		assert.False(t, ok, "gRPC allowlist must exclude %q", m)
	}

	// qrdb-quic bridge methods.
	qrdbQUICDenied := []string{
		"barrier.Seal", "barrier.EnableEncryption",
		"credential.Submit", "credential.Retrieve", "credential.Rotate",
		"tenant.BarrierInit", "tenant.BarrierUnseal", "tenant.BarrierStatus",
		"user.Get", "userauthfactor.Login",
	}
	bridge := QRDBQUICAllowlist()
	for _, m := range qrdbQUICDenied {
		_, ok := bridge[m]
		assert.False(t, ok, "qrdb-quic bridge allowlist must exclude %q", m)
	}

	// MCP tools.
	mcpDenied := []string{
		"qrdb_barrier_seal", "qrdb_barrier_enable_encryption",
		"qrdb_credential_submit", "qrdb_credential_list",
		"qrdb_tenant_barrier_enable_encryption",
	}
	mcp := MCPAllowlist()
	for _, tool := range mcpDenied {
		_, ok := mcp[tool]
		assert.False(t, ok, "MCP allowlist must exclude %q", tool)
	}

	// Native QUIC: no seal / enable-encryption / tenant governance sub-ops.
	quicDenied := []QUICOp{
		{Opcode: opBarrier, SubOp: 0x04}, // SubOpBarrierSeal
		{Opcode: opBarrier, SubOp: 0x12}, // SubOpBarrierEnableEncryption
		{Opcode: opBarrier, SubOp: 0x13}, // SubOpBarrierUpdateTenantQuotas
		{Opcode: opBarrier, SubOp: 0x14}, // SubOpBarrierUpdateTenantEntitlements
	}
	quic := QUICAllowlist()
	for _, op := range quicDenied {
		_, ok := quic[op]
		assert.False(t, ok, "QUIC allowlist must exclude opcode 0x%02X sub 0x%02X", op.Opcode, op.SubOp)
	}
}

// TestQUICAllowlist_HealthAndUnsealFlow asserts the native QUIC allowlist
// admits the health opcode and the core barrier / ceremony unseal-flow
// sub-ops derived from the table.
func TestQUICAllowlist_HealthAndUnsealFlow(t *testing.T) {
	quic := QUICAllowlist()
	expected := []QUICOp{
		{Opcode: opHealth},
		{Opcode: opBarrier, SubOp: subBarrierStatus},
		{Opcode: opBarrier, SubOp: subBarrierInitialize},
		{Opcode: opBarrier, SubOp: subBarrierUnseal},
		{Opcode: opBarrier, SubOp: subBarrierShamirUnsealShare},
		{Opcode: opBarrier, SubOp: subBarrierShamirUnsealShares},
		{Opcode: opBarrier, SubOp: subBarrierRootToken},
		{Opcode: opCeremony, SubOp: subCeremonyInitialize},
		{Opcode: opCeremony, SubOp: subCeremonyStatus},
		{Opcode: opCeremony, SubOp: subCeremonySignCSR},
		{Opcode: opCeremony, SubOp: subCeremonyEnrollExternalOfficer},
	}
	for _, op := range expected {
		_, ok := quic[op]
		assert.True(t, ok, "QUIC allowlist must admit opcode 0x%02X sub 0x%02X", op.Opcode, op.SubOp)
	}
}

// TestGRPCAllowlist_Deduplicated asserts no tail appears twice (shared verbs
// like officer list/register dedupe cleanly).
func TestGRPCAllowlist_Deduplicated(t *testing.T) {
	seen := make(map[string]int)
	for _, m := range GRPCAllowlist() {
		seen[m]++
	}
	for m, n := range seen {
		assert.Equal(t, 1, n, "gRPC allowlist tail %q duplicated", m)
	}
}
