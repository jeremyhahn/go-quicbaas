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
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qrdbsealedwindow "github.com/jeremyhahn/go-qrdb/pkg/api/sealedwindow"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// TestTable_EveryVerbPresent asserts every canonical verb has a Table entry
// and the verb set is go-qrdb's canonical table (18 verbs at v0.47) plus the
// one documented quicbaas extra, the ceremony officer list.
func TestTable_EveryVerbPresent(t *testing.T) {
	want := make(map[Verb]struct{}, len(qrdbsealedwindow.Verbs))
	for _, v := range qrdbsealedwindow.Verbs {
		want[Verb(v)] = struct{}{}
	}
	require.Len(t, Verbs, len(want), "verb set must be exactly qrdb's sealed surface")
	seen := make(map[Verb]struct{}, len(Verbs))
	for _, v := range Verbs {
		_, canonical := want[v]
		assert.True(t, canonical, "verb %q is not in qrdb's sealed surface", v)
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

	// quicbaas withholds the ceremony verbs that mint or sign credential
	// material (StrictCeremonyExclusions) plus qrdb's catch-all "/v1/init/"
	// PREFIX, which is what admitted them wholesale. Everything else qrdb
	// carries must still be present, so real drift is still caught.
	excludedPaths := make(map[string]struct{})
	for v := range StrictCeremonyExclusions {
		for _, p := range Table[v].REST {
			excludedPaths[p] = struct{}{}
		}
	}
	// qrdb's prefix entries have no strict-model equivalent: the exclusions
	// above are enumerated as full paths precisely so a prefix can never
	// re-admit them.
	excludedPaths["/v1/init/"] = struct{}{}
	excludedPaths["/api/v1/init/"] = struct{}{}

	for _, canonical := range qrdbsdk.SealedWindowRESTAllowlist() {
		if canonical == "/v1/cluster/" {
			_, ok := ours["/api/v1/cluster/secret"]
			assert.True(t, ok, "quicbaas substitution for qrdb %q missing", canonical)
			continue
		}
		if _, strict := excludedPaths[canonical]; strict {
			_, present := ours[canonical]
			assert.False(t, present,
				"strictly excluded REST entry %q must NOT appear in the quicbaas sealed-window derivation", canonical)
			continue
		}
		_, ok := ours[canonical]
		assert.True(t, ok, "qrdb canonical REST allowlist entry %q missing from quicbaas derivation", canonical)
	}

	// The narrowing is only meaningful if no surviving entry is a PREFIX that
	// would re-admit an excluded path. Non-ceremony prefixes (trust bootstrap,
	// cluster key-transport) are deliberately retained — see RESTExtra — so
	// this asserts the property that actually matters: no allowlist prefix may
	// cover any strictly excluded ceremony path.
	for p := range ours {
		if !strings.HasSuffix(p, "/") {
			continue
		}
		for excluded := range excludedPaths {
			require.False(t, strings.HasPrefix(excluded, p),
				"REST allowlist prefix %q re-admits strictly excluded ceremony path %q", p, excluded)
		}
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
// derivation, and that quicbaas adds no gRPC tail of its own (the
// CeremonyListOfficers mirror left with the go-qrdb v0.47 ceremony cut).
//
// qrdb's re-exported allowlist is service-qualified
// ("/qrdb.v1.QRDBService/BarrierUnseal"); the tail is the unit both canonical
// tables share, so the comparison strips the service prefix.
func TestGRPCAllowlist_NoDriftFromQRDB(t *testing.T) {
	ours := make(map[string]struct{})
	for _, m := range GRPCAllowlist() {
		require.Equal(t, m, methodTail(m), "GRPCAllowlist must yield bare method tails")
		ours[m] = struct{}{}
	}
	// quicbaas deliberately diverges from qrdb here: qrdb keeps its permissive
	// "Vault model" surface, while quicbaas withholds the ceremony verbs that
	// mint or sign credential material (StrictCeremonyExclusions). The
	// invariant is therefore "qrdb's table MINUS the strict exclusions", not
	// verbatim parity — but every non-excluded verb must still be carried, so
	// genuine drift is still caught.
	excludedTails := excludedGRPCTails()
	canonical := make(map[string]struct{})
	for _, m := range qrdbsdk.GRPCSealedWindowAllowlist {
		tail := methodTail(m)
		canonical[tail] = struct{}{}
		if _, strict := excludedTails[tail]; strict {
			_, present := ours[tail]
			assert.False(t, present,
				"strictly excluded ceremony tail %q must NOT appear in the quicbaas sealed-window derivation", tail)
			continue
		}
		_, ok := ours[tail]
		assert.True(t, ok, "qrdb canonical gRPC allowlist tail %q (from %q) missing from quicbaas derivation", tail, m)
	}

	for m := range ours {
		_, ok := canonical[m]
		assert.True(t, ok, "unexpected quicbaas-only gRPC allowlist tail %q", m)
	}
}

// TestGRPCFullMethodAllowlist_ServicePinned asserts the derivation quicbaas
// actually gates on is fully service-qualified: it carries qrdb's canonical
// qrdb.v1.QRDBService surface verbatim, qualifies the quicbaas mirror verbs
// onto the services that really declare them, and admits no bare tail.
func TestGRPCFullMethodAllowlist_ServicePinned(t *testing.T) {
	full := GRPCFullMethodAllowlist()
	require.NotEmpty(t, full)

	set := make(map[string]struct{}, len(full))
	for _, m := range full {
		require.True(t, strings.HasPrefix(m, "/"), "entry %q must be a fully-qualified method path", m)
		require.Equal(t, 2, strings.Count(m, "/"), "entry %q must be /{service}/{method}", m)
		_, dup := set[m]
		require.False(t, dup, "duplicate full-method entry %q", m)
		set[m] = struct{}{}
	}

	// 1. qrdb's canonical qrdb.v1 surface is carried, MINUS the strict ceremony
	// exclusions quicbaas deliberately withholds (qrdb keeps them; quicbaas
	// does not — see StrictCeremonyExclusions).
	excludedTails := excludedGRPCTails()
	for _, m := range qrdbsdk.GRPCSealedWindowAllowlist {
		_, ok := set[m]
		if _, strict := excludedTails[methodTail(m)]; strict {
			assert.False(t, ok,
				"strictly excluded ceremony method %q must NOT be reachable through the sealed window", m)
			continue
		}
		assert.True(t, ok, "qrdb canonical full method %q missing from quicbaas derivation", m)
	}

	// 2. The quicbaas mirror surfaces are qualified onto their own services.
	// The ceremony verbs that mint or sign credential material are absent by
	// design.
	for _, m := range []string{
		"/quicbaas.barrier.v1.Barrier/BarrierUnseal",
		"/quicbaas.barrier.v1.Barrier/BarrierStatus",
	} {
		_, ok := set[m]
		assert.True(t, ok, "quicbaas mirror full method %q missing from derivation", m)
	}

	// A sealed node answers no status question but barrier status (go-qrdb
	// v0.50): health and ceremony status stay sealed on the mirrors too.
	for _, m := range []string{
		"/quicbaas.ceremony.v1.Ceremony/CeremonyStatus",
		"/quicbaas.v1.QuicBaaS/Health",
	} {
		_, ok := set[m]
		assert.False(t, ok, "status mirror %q must NOT be reachable through the sealed window", m)
	}

	// 3. The excluded ceremony mirrors are unreachable on the quicbaas
	// services too — narrowing qrdb's surface would be pointless if the
	// mirror stayed open.
	for _, m := range []string{
		"/quicbaas.ceremony.v1.Ceremony/CeremonyInitialize",
		"/quicbaas.ceremony.v1.Ceremony/CeremonySignCSR",
	} {
		_, ok := set[m]
		assert.False(t, ok, "strictly excluded ceremony mirror %q must NOT be reachable through the sealed window", m)
	}

	// 3. Bare tails are never admitted — the whole point of service pinning.
	for _, tail := range GRPCAllowlist() {
		_, ok := set[tail]
		assert.False(t, ok, "bare tail %q must not appear in the full-method allowlist", tail)
	}
}

// TestGRPCFullMethodAllowlist_NoTailCollisionAdmitted pins the service-pinning
// guarantee against the real collisions in the quicbaas proto surface: several
// co-registered services declare a method whose TAIL is on the canonical
// allowlist ("Status" on the per-tenant barrier — itself a deliberate
// sealed-window exclusion — and "Health" on the post-unseal data-plane
// services). None of them may be admitted through the sealed window.
func TestGRPCFullMethodAllowlist_NoTailCollisionAdmitted(t *testing.T) {
	set := make(map[string]struct{})
	for _, m := range GRPCFullMethodAllowlist() {
		set[m] = struct{}{}
	}
	for _, m := range []string{
		"/quicbaas.tenant.v1.TenantBarrier/Status",
		"/quicbaas.tenant.v1.TenantBarrier/Unseal",
		"/quicbaas.fleet.runtime.v1.FleetRuntime/Status",
		"/quicbaas.orchestrator.v1.OrchestratorService/Health",
		"/quicbaas.ollama.v1.OllamaService/Health",
		"/quicbaas.vector.v1.VectorDB/Health",
		"/quicbaas.barrier.v1.Barrier/BarrierSeal",
		"/quicbaas.v1.QuicBaaS/Get",
	} {
		_, ok := set[m]
		assert.False(t, ok, "tail-colliding method %q must stay sealed", m)
	}
}

// TestQRDBQUICAllowlist_NoDriftFromQRDB asserts the qrdb-quic ALPN bridge
// allowlist covers qrdb's canonical native-QUIC sealed-window allowlist
// verbatim (the bridge speaks qrdb's wire method names) minus the strict
// exclusions, with no quicbaas extra.
func TestQRDBQUICAllowlist_NoDriftFromQRDB(t *testing.T) {
	ours := QRDBQUICAllowlist()

	// As on REST and gRPC, quicbaas withholds the trust-minting ceremony verbs
	// (StrictCeremonyExclusions). Everything else qrdb carries must still be
	// present, so real drift is still caught.
	excluded := make(map[string]struct{})
	for v := range StrictCeremonyExclusions {
		for _, m := range Table[v].QRDBQUIC {
			excluded[m] = struct{}{}
		}
	}

	for m := range qrdbsdk.QUICSealedWindowAllowlist {
		if _, strict := excluded[m]; strict {
			_, present := ours[m]
			assert.False(t, present,
				"strictly excluded qrdb-quic method %q must NOT appear in the quicbaas bridge allowlist", m)
			continue
		}
		_, ok := ours[m]
		assert.True(t, ok, "qrdb canonical QUIC method %q missing from quicbaas bridge allowlist", m)
	}
	for m := range ours {
		_, ok := qrdbsdk.QUICSealedWindowAllowlist[m]
		assert.True(t, ok, "unexpected quicbaas-only qrdb-quic bridge allowlist method %q", m)
	}
}

// TestMCPAllowlist_NoDriftFromQRDB asserts every tool in qrdb's canonical MCP
// sealed-window allowlist has its quicbaas counterpart, and the quicbaas
// extras are exactly the documented quicbaas-native tool names.
func TestMCPAllowlist_NoDriftFromQRDB(t *testing.T) {
	ours := MCPAllowlist()
	canonical := qrdbsdk.MCPSealedWindowAllowlist

	// As on every other transport, the trust-minting ceremony verbs are
	// withheld (StrictCeremonyExclusions).
	excluded := make(map[string]struct{})
	for v := range StrictCeremonyExclusions {
		for _, tool := range Table[v].MCP {
			excluded[tool] = struct{}{}
		}
	}

	for tool := range canonical {
		if _, strict := excluded[tool]; strict {
			_, present := ours[tool]
			assert.False(t, present,
				"strictly excluded MCP tool %q must NOT appear in the quicbaas derivation", tool)
			continue
		}
		_, ok := ours[tool]
		assert.True(t, ok, "qrdb canonical MCP tool %q missing from quicbaas derivation", tool)
	}

	for tool := range ours {
		_, ok := canonical[tool]
		assert.True(t, ok, "unexpected quicbaas-only MCP allowlist tool %q", tool)
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

// TestRESTAllowlist_ExactSet pins the REST sealed-window allowlist to its
// exact, ordered contents.
//
// The other REST tests assert PROPERTIES (no drift from qrdb, no duplicates,
// no prefix re-admits an excluded path). This one pins the SET, so any change
// to the surface reachable while the barrier is sealed — an addition as much
// as a removal — has to be made deliberately here and reviewed as a change to
// the daemon's entire pre-unseal control surface.
//
// Entries ending in "/" are prefix matches (see AuthBypass.BypassesRESTPath
// and qrdb's SealedWindowMiddleware); there are exactly four, all
// non-ceremony, and TestRESTAllowlist_NoDriftFromQRDB proves none of them
// covers a strictly excluded ceremony path.
func TestRESTAllowlist_ExactSet(t *testing.T) {
	want := []string{
		// Barrier lifecycle: status, first initialization, and every route
		// back to an unsealed data plane.
		"/v1/barrier/status",
		"/api/v1/barrier/status",
		"/v1/barrier/initialize",
		"/api/v1/barrier/initialize",
		"/v1/barrier/unseal",
		"/api/v1/barrier/unseal",
		"/v1/barrier/shamir/initialize",
		"/api/v1/barrier/shamir/initialize",

		// Officer registration and the one-segment share-claim patterns.
		"POST /v1/barrier/officers",
		"POST /api/v1/barrier/officers",
		"POST /v1/barrier/officers/{username}/claim/begin",
		"POST /api/v1/barrier/officers/{username}/claim/begin",
		"POST /v1/barrier/officers/{username}/claim/complete",
		"POST /api/v1/barrier/officers/{username}/claim/complete",
		"POST /v1/barrier/recovery/officers",
		"POST /api/v1/barrier/recovery/officers",
		"POST /v1/barrier/recovery/officers/{username}/claim/begin",
		"POST /api/v1/barrier/recovery/officers/{username}/claim/begin",
		"POST /v1/barrier/recovery/officers/{username}/claim/complete",
		"POST /api/v1/barrier/recovery/officers/{username}/claim/complete",

		// The holder quorum.
		"/v1/barrier/quorum/begin",
		"/api/v1/barrier/quorum/begin",
		"/v1/barrier/quorum/submit",
		"/api/v1/barrier/quorum/submit",

		// AuthenticatedOnly: admitted by the deny gate, never auth-exempt.
		"/v1/barrier/cluster-unseal",
		"/api/v1/barrier/cluster-unseal",

		// RESTExtra: first-touch trust bootstrap and the cluster peer
		// key-transport receiver.
		"/v1/bootstrap/",
		"/api/v1/bootstrap/",
		"/api/v1/cluster/secret",
	}
	assert.Equal(t, want, RESTAllowlist())
}

// TestAllowlists_StrictExclusionsAppliedOnEveryTransport is the cross-transport
// guard for the strict ceremony model.
//
// StrictCeremonyExclusions withholds the ceremony verbs that MINT trust —
// initialize (mints the CA, the server TLS identity and the SO credential) and
// sign-csr (issues from the ceremony CA).
// The package contract is that the exclusion is a property of the TABLE, so
// all five derivations inherit it from one place.
//
// Enforcing that per-transport is not enough: the derivation for native QUIC,
// the qrdb-quic bridge and MCP used to read Table directly, so those three
// transports served the excluded verbs while sealed AND exempted them from
// authentication, even though REST and gRPC withheld them. This test walks the
// exclusion set against every derivation so the asymmetry cannot come back.
func TestAllowlists_StrictExclusionsAppliedOnEveryTransport(t *testing.T) {
	require.NotEmpty(t, StrictCeremonyExclusions)

	rest := make(map[string]struct{})
	for _, p := range RESTAllowlist() {
		rest[p] = struct{}{}
	}
	grpcTails := make(map[string]struct{})
	for _, m := range GRPCAllowlist() {
		grpcTails[m] = struct{}{}
	}
	grpcFull := make(map[string]struct{})
	for _, m := range GRPCFullMethodAllowlist() {
		grpcFull[m] = struct{}{}
	}
	quic := QUICAllowlist()
	bridge := QRDBQUICAllowlist()
	mcp := MCPAllowlist()

	for v, reason := range StrictCeremonyExclusions {
		names := Table[v]

		for _, p := range names.REST {
			_, ok := rest[p]
			assert.Falsef(t, ok, "REST allowlist admits excluded path %q (%s)", p, reason)
		}
		for _, m := range names.GRPC {
			_, ok := grpcTails[m]
			assert.Falsef(t, ok, "gRPC tail allowlist admits excluded method %q (%s)", m, reason)
			for full := range grpcFull {
				assert.NotEqualf(t, m, methodTail(full),
					"gRPC full-method allowlist admits excluded method %q via %q (%s)", m, full, reason)
			}
		}
		for _, op := range names.QUIC {
			_, ok := quic[op]
			assert.Falsef(t, ok, "native QUIC allowlist admits excluded op 0x%02X/0x%02X (%s)",
				op.Opcode, op.SubOp, reason)
		}
		for _, m := range names.QRDBQUIC {
			_, ok := bridge[m]
			assert.Falsef(t, ok, "qrdb-quic bridge allowlist admits excluded method %q (%s)", m, reason)
		}
		for _, tool := range names.MCP {
			_, ok := mcp[tool]
			assert.Falsef(t, ok, "MCP allowlist admits excluded tool %q (%s)", tool, reason)
		}

		// The exclusion must also hold at scope granularity, since a future
		// caller of the scoped derivations must not be able to reach the verb
		// through a narrower scope.
		for _, scope := range []Scope{ScopeAll, ScopeAlways, ScopePreOperational} {
			assert.Falsef(t, InScope(v, scope),
				"excluded verb %v is in scope %d (%s)", v, scope, reason)
		}
	}
}

// TestQUICAllowlist_UnsealFlow asserts the native QUIC allowlist admits the
// core barrier unseal-flow sub-ops derived from the table, and neither the
// health opcode nor ceremony status: a sealed node answers no status question
// but barrier status (go-qrdb v0.50).
func TestQUICAllowlist_UnsealFlow(t *testing.T) {
	quic := QUICAllowlist()
	expected := []QUICOp{
		{Opcode: opBarrier, SubOp: subBarrierStatus},
		{Opcode: opBarrier, SubOp: subBarrierInitialize},
		{Opcode: opBarrier, SubOp: subBarrierUnseal},
		{Opcode: opBarrier, SubOp: subBarrierShamirInitialize},
	}
	for _, op := range expected {
		_, ok := quic[op]
		assert.True(t, ok, "QUIC allowlist must admit opcode 0x%02X sub 0x%02X", op.Opcode, op.SubOp)
	}

	// The trust-minting ceremony sub-ops are withheld on native QUIC exactly
	// as they are on REST and gRPC: they are driven in-process by
	// `quicbaasd init`, never over the wire by an unauthenticated caller.
	for _, op := range []QUICOp{
		{Opcode: opCeremony, SubOp: subCeremonyInitialize},
		{Opcode: opCeremony, SubOp: subCeremonySignCSR},
	} {
		_, ok := quic[op]
		assert.False(t, ok,
			"strictly excluded ceremony sub-op 0x%02X must NOT be on the native QUIC allowlist", op.SubOp)
	}

	for _, op := range []QUICOp{
		{Opcode: opHealth},
		{Opcode: opCeremony, SubOp: subCeremonyStatus},
	} {
		_, ok := quic[op]
		assert.False(t, ok,
			"status opcode 0x%02X sub 0x%02X must NOT be on the native QUIC allowlist", op.Opcode, op.SubOp)
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

// TestPreOperationalOnlyFlags locks the exact set of verbs whose sealed-window
// authentication exemption is gated on a pre-operational ceremony. The officer
// registration verbs are flagged because qrdb's RegisterOfficer CA-signs any
// caller-supplied CSR and assigns a Shamir share index with no service-layer
// auth: a state-independent exemption would let an unauthenticated caller
// enroll an officer during any restart-while-sealed after the ceremony
// completed. Flagging a NEW verb here is a security-posture change reviewed
// as such; UN-flagging either officer verb reopens that hole and must never
// happen.
func TestPreOperationalOnlyFlags(t *testing.T) {
	// go-qrdb v0.50's PreOperationalOnly set, exactly.
	want := map[Verb]bool{
		VerbBarrierInitialize:       true,
		VerbShamirInitialize:        true,
		VerbOfficerRegister:         true,
		VerbRecoveryOfficerRegister: true,
		VerbCeremonyInitialize:      true,
		VerbCeremonySignCSR:         true,
	}
	for _, v := range Verbs {
		assert.Equalf(t, want[v], Table[v].PreOperationalOnly,
			"PreOperationalOnly(%q) drifted from the locked set", v)
	}
}

// TestAuthenticatedOnly_NeverAuthExempt pins the AuthenticatedOnly contract
// mirrored from qrdb on every transport: cluster unseal is in the deny gate's
// ScopeAll allowlist but in neither authentication-bypass scope. The qrdb.v1
// gRPC method arrives from qrdbsdk.GRPCSealedWindowAllowlist, which carries no
// flag, so its exclusion from the bypass scopes is checked on the full-method
// derivation the bypass actually consumes.
func TestAuthenticatedOnly_NeverAuthExempt(t *testing.T) {
	var authOnly []Verb
	for _, v := range Verbs {
		if Table[v].AuthenticatedOnly {
			authOnly = append(authOnly, v)
		}
	}
	require.Equal(t, []Verb{VerbBarrierClusterUnseal}, authOnly, "the AuthenticatedOnly set is locked")

	names := Table[VerbBarrierClusterUnseal]
	assert.False(t, names.PreOperationalOnly, "AuthenticatedOnly and PreOperationalOnly are exclusive")
	assert.True(t, InScope(VerbBarrierClusterUnseal, ScopeAll))
	assert.False(t, InScope(VerbBarrierClusterUnseal, ScopeAlways))
	assert.False(t, InScope(VerbBarrierClusterUnseal, ScopePreOperational))

	const grpcMethod = "/qrdb.v1.QRDBService/BarrierClusterUnseal"
	require.Contains(t, qrdbsdk.GRPCSealedWindowAllowlist, grpcMethod, "qrdb still allowlists cluster unseal")
	assert.Contains(t, GRPCFullMethodAllowlistScoped(ScopeAll), grpcMethod)
	for _, scope := range []Scope{ScopeAlways, ScopePreOperational} {
		assert.NotContains(t, GRPCFullMethodAllowlistScoped(scope), grpcMethod)
		for _, p := range names.REST {
			assert.NotContains(t, RESTAllowlistScoped(scope), p)
		}
		for _, m := range names.QRDBQUIC {
			assert.NotContains(t, QRDBQUICAllowlistScoped(scope), m)
		}
		for _, tool := range names.MCP {
			assert.NotContains(t, MCPAllowlistScoped(scope), tool)
		}
	}
	for _, p := range names.REST {
		assert.Contains(t, RESTAllowlist(), p)
	}
}

// TestRESTAllowlistScoped_OfficerRegistrationIsMethodQualified pins how the
// officer path is admitted since go-qrdb v0.25: only POST (registration, a
// PreOperationalOnly verb) is on the allowlist, as a method-qualified entry, so
// a GET of the same path (the barrier officer list, withdrawn from the sealed
// window) is never admitted, and registration is exempt only while the
// ceremony is unfinished.
func TestRESTAllowlistScoped_OfficerRegistrationIsMethodQualified(t *testing.T) {
	for _, entry := range []string{"POST /v1/barrier/officers", "POST /v1/barrier/recovery/officers"} {
		assert.NotContains(t, RESTAllowlistScoped(ScopeAlways), entry, "registration must not be state-independently exempt")
		assert.Contains(t, RESTAllowlistScoped(ScopePreOperational), entry, "registration stays exempt while the ceremony is unfinished")
	}
	for _, path := range []string{"/v1/barrier/officers", "/v1/barrier/recovery/officers"} {
		for _, entry := range RESTAllowlist() {
			assert.Falsef(t, qrdbsealedwindow.RESTEntryMatches(entry, http.MethodGet, path),
				"entry %q admits GET %s, which left the sealed window", entry, path)
		}
	}
}

// TestOfficerRegister_PreOperationalOnEveryTransport asserts the officer
// registration wire names land in the pre-operational scope — and only that
// scope — on every transport that carries them.
func TestOfficerRegister_PreOperationalOnEveryTransport(t *testing.T) {
	for _, v := range []Verb{VerbOfficerRegister, VerbRecoveryOfficerRegister} {
		names := Table[v]

		always := GRPCAllowlistScoped(ScopeAlways)
		preOp := GRPCAllowlistScoped(ScopePreOperational)
		for _, m := range names.GRPC {
			assert.NotContainsf(t, always, m, "gRPC tail %q must not be state-independent", m)
			assert.Containsf(t, preOp, m, "gRPC tail %q must be pre-operational", m)
		}

		bridgeAlways := QRDBQUICAllowlistScoped(ScopeAlways)
		bridgePreOp := QRDBQUICAllowlistScoped(ScopePreOperational)
		for _, m := range names.QRDBQUIC {
			_, inAlways := bridgeAlways[m]
			assert.Falsef(t, inAlways, "qrdb-quic method %q must not be state-independent", m)
			_, inPreOp := bridgePreOp[m]
			assert.Truef(t, inPreOp, "qrdb-quic method %q must be pre-operational", m)
		}

		mcpAlways := MCPAllowlistScoped(ScopeAlways)
		mcpPreOp := MCPAllowlistScoped(ScopePreOperational)
		for _, tool := range names.MCP {
			_, inAlways := mcpAlways[tool]
			assert.Falsef(t, inAlways, "MCP tool %q must not be state-independent", tool)
			_, inPreOp := mcpPreOp[tool]
			assert.Truef(t, inPreOp, "MCP tool %q must be pre-operational", tool)
		}
	}
}
