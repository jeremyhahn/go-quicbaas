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

// Package sealedwindow defines the canonical quicbaas-side derivation of the
// sealed-window allowlists for all five quicbaas transports (REST, gRPC,
// Unix-over-gRPC, native QUIC, MCP).
//
// The verb set mirrors go-qrdb's transport-neutral sealed-window table
// (go-qrdb/pkg/api/sealedwindow) verbatim: a verb is present here exactly
// when qrdb defines it, and the per-transport wire names are the QUICBAAS
// wire names for the same operation (quicbaas mounts the qrdb /v1 wire
// routes at its router root AND its own /api/v1 mirrors; it registers the
// qrdb.v1.* gRPC services alongside its own quicbaas.* mirrors; it bridges
// the qrdb-quic ALPN methods next to its native opcode dispatch; and it
// exposes qrdb_* MCP tools). Deriving every transport allowlist from this
// ONE table makes cross-transport drift structurally impossible — the exact
// drift class qrdb eliminated with its own canonical table.
//
// # Strict ceremony model
//
// quicbaas deliberately DIVERGES from qrdb here. go-qrdb keeps a permissive
// "Vault model" in which the ceremony surface — including initialize and
// sign-csr — is reachable pre-authentication while sealed. quicbaas adopts go-xkms's STRICT model instead (owner
// decision), which narrows that surface on two independent axes:
//
//  1. StrictCeremonyExclusions removes the ceremony verbs that MINT or
//     DISTRIBUTE trust from every transport's sealed window. They are
//     driven in-process by `quicbaasd init` against a daemon that is not
//     yet listening, never over the wire by an unauthenticated caller.
//  2. Names.PreOperationalOnly marks the remaining self-authorizing /
//     read-only ceremony routes so their authentication exemption ALSO
//     requires the init ceremony to still be pre-operational. Serving the
//     claim endpoints unauthenticated on an already-initialized daemon
//     would be a live vulnerability, so the exemption stops applying the
//     moment the ceremony reaches operational, and any unrecognized state
//     fails closed (see authbypass.go).
//
// Both axes are properties of THIS table, so every transport inherits them
// from one derivation. There is no per-transport ceremony allowlist to
// drift — the drift class that produced the pre-auth exposure this model
// replaces.
//
// Names.AuthenticatedOnly mirrors qrdb's flag of the same name: the verb is
// admitted by the deny gate while sealed but is never exempt from
// authentication on any transport (cluster unseal is an authorized platform
// act, not a credential-possession one).
//
// Deliberate exclusions (never allowlisted on any transport, matching qrdb):
//
//   - barrier seal and Shamir seal-vote — sealing is only meaningful while
//     the barrier is unsealed;
//   - barrier enable-encryption — the migration requires an unsealed data
//     plane;
//   - Shamir share management (list/delete shares) and custodian
//     enroll/revoke — post-unseal administrative surfaces;
//   - the per-tenant barrier surface (tenant.*) — tenant barriers are
//     delegated to the embedded qrdb node and require the unsealed system
//     data plane;
//   - the credential management surface (credential.*, /v1/credentials) —
//     qrdb deliberately keeps versioned-credential rotation off the sealed
//     window; the ceremony flows deliver boot credentials instead.
package sealedwindow

import (
	"strings"

	ggrpc "google.golang.org/grpc"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// Verb is a transport-neutral identifier for one sealed-window operation.
// The identifiers use the same string values as qrdb's canonical table so
// the two tables can be diffed mechanically.
type Verb string

// Canonical sealed-window verbs (1:1 with go-qrdb/pkg/api/sealedwindow's
// Verbs). Health, node status, ceremony status and the officer list are not
// verbs: a sealed node answers no status question but barrier status, and
// lists no holder to an anonymous caller (go-qrdb v0.50).
const (
	VerbBarrierStatus     Verb = "barrier-status"
	VerbBarrierInitialize Verb = "barrier-initialize"
	VerbBarrierUnseal     Verb = "barrier-unseal"

	VerbShamirInitialize Verb = "shamir-initialize"

	VerbOfficerRegister    Verb = "officer-register"
	VerbShareClaimBegin    Verb = "share-claim-begin"
	VerbShareClaimComplete Verb = "share-claim-complete"

	VerbRecoveryOfficerRegister    Verb = "recovery-officer-register"
	VerbRecoveryShareClaimBegin    Verb = "recovery-share-claim-begin"
	VerbRecoveryShareClaimComplete Verb = "recovery-share-claim-complete"

	VerbQuorumBegin  Verb = "quorum-begin"
	VerbQuorumSubmit Verb = "quorum-submit"

	VerbCeremonyInitialize Verb = "ceremony-initialize"
	VerbCeremonySignCSR    Verb = "ceremony-sign-csr"

	// VerbBarrierClusterUnseal clears a cluster-wide seal of the node
	// barrier. It is AuthenticatedOnly.
	VerbBarrierClusterUnseal Verb = "barrier-cluster-unseal"
)

// QUICOp identifies one native quicbaas-QUIC operation: a wire opcode plus
// its sub-operation byte (payload[0]) for the OpBarrier / OpCeremony
// sub-dispatch pattern. SubOp is zero for opcodes that carry no
// sub-operation (real sub-op codes start at 0x01, so zero is unambiguous).
type QUICOp struct {
	Opcode byte
	SubOp  byte
}

// Names holds the per-transport quicbaas wire names for one canonical verb.
// An empty field means the verb has no surface on that transport.
type Names struct {
	// REST paths. Entries ending in "/" are prefix matches; everything else
	// matches exactly (see qrdb rest.SealedWindowMiddleware, which quicbaas
	// installs directly). Both the qrdb-wire root mounts (/v1/...) and the
	// quicbaas /api/v1 mirrors are listed.
	REST []string
	// GRPC method tails — the bare method name, i.e. the suffix after the
	// last "/" in FullMethod — matching qrdb's canonical table convention so
	// the two tables diff mechanically. A tail is NOT a matchable identity:
	// quicbaas co-registers many services and several of them declare a
	// same-named method (e.g. quicbaas.tenant.v1.TenantBarrier/Status), so
	// live gating MUST use the service-qualified derivation from
	// GRPCFullMethodAllowlist, never a bare tail.
	GRPC []string
	// QUIC lists the native quicbaas-quic ALPN operations (opcode +
	// sub-op) for this verb.
	QUIC []QUICOp
	// QRDBQUIC lists the qrdb-quic ALPN bridge method names (e.g.
	// "barrier.Unseal") served by the same QUIC listener, mirroring qrdb's
	// native QUIC table.
	QRDBQUIC []string
	// MCP lists the MCP tool names (e.g. "qrdb_barrier_unseal").
	MCP []string
	// PreOperationalOnly marks a verb whose sealed-window AUTHENTICATION
	// exemption additionally requires the init ceremony to still be
	// pre-operational (see OperationalProbe in authbypass.go).
	//
	// It is set for the self-authorizing ceremony routes: the claim handshakes
	// authenticate the caller against material submitted at enrollment (a
	// nonce signed by the key in the officer's CSR, or the mTLS peer
	// thumbprint recorded for that officer) rather than against an account
	// that does not exist yet. That is why they may skip ACCOUNT
	// authentication at all — but only for as long as the ceremony they
	// belong to is unfinished.
	//
	// It is ALSO set for the officer registration verbs (officer-register,
	// recovery-officer-register): qrdb's RegisterOfficer CA-signs any
	// caller-supplied CSR and assigns a Shamir share index with no
	// service-layer authentication, so keeping its exemption
	// state-independent would let an unauthenticated caller enroll an
	// officer during any restart-while-sealed after the ceremony completed.
	//
	// It is ALSO set for the first-boot verbs go-qrdb flags
	// PreOperationalOnly (barrier initialize, Shamir initialize, ceremony
	// initialize, ceremony sign-CSR): go-qrdb v0.50 refuses them once the
	// barrier is initialized, so they never skip authentication after that.
	//
	// The other barrier verbs are deliberately NOT marked: a daemon restarted
	// after a completed ceremony is operational AND sealed, and unseal must
	// still be reachable without a token because a sealed IAM plane cannot
	// validate one.
	PreOperationalOnly bool
	// AuthenticatedOnly marks a verb the deny gate admits while sealed but
	// that is never exempt from authentication, in any ceremony state. It
	// mirrors qrdb's AuthenticatedOnly: cluster unseal clears a cluster-wide
	// seal, which is an authorized platform act, not a
	// credential-possession one. Such a verb appears only in ScopeAll.
	AuthenticatedOnly bool
}

// Native quicbaas-QUIC opcode / sub-op values. These mirror the protocol
// constants in pkg/api/quic/protocol.go; the guard test asserts the two
// stay in lock-step (this package must not import the quic transport to
// keep the derivation table dependency-free).
const (
	opHealth   byte = 0x90
	opBarrier  byte = 0xC0
	opCeremony byte = 0xC1

	subBarrierStatus           byte = 0x01
	subBarrierInitialize       byte = 0x02
	subBarrierUnseal           byte = 0x03
	subBarrierShamirInitialize byte = 0x05

	subCeremonyInitialize byte = 0x01
	subCeremonyStatus     byte = 0x02
	subCeremonySignCSR    byte = 0x06
)

// Verbs is the canonical verb list in stable derivation order (identical to
// qrdb's order).
var Verbs = []Verb{
	VerbBarrierStatus,
	VerbBarrierInitialize,
	VerbBarrierUnseal,
	VerbShamirInitialize,
	VerbOfficerRegister,
	VerbShareClaimBegin,
	VerbShareClaimComplete,
	VerbRecoveryOfficerRegister,
	VerbRecoveryShareClaimBegin,
	VerbRecoveryShareClaimComplete,
	VerbQuorumBegin,
	VerbQuorumSubmit,
	VerbCeremonyInitialize,
	VerbCeremonySignCSR,
	VerbBarrierClusterUnseal,
}

// StrictCeremonyExclusions names the canonical verbs quicbaas withholds from
// its sealed window on EVERY transport, mapped to the reason. qrdb allowlists
// ceremony initialize and sign-csr; quicbaas withholds both (owner decision —
// adopt go-xkms's strict model).
//
// The verbs keep their full entries in Table so the quicbaas table still
// diffs mechanically against qrdb's, and every derivation below skips them,
// including the qrdb.v1 methods consumed verbatim from
// qrdbsdk.GRPCSealedWindowAllowlist. Excluding them here means they are
// neither served while sealed (the deny gate rejects them with 503) nor
// exempt from authentication (the bypass never admits them), so the only way
// to reach them is an authenticated, authorized call on an unsealed daemon —
// or, for a first boot, the in-process `quicbaasd init` ceremony, which runs
// before any listener exists.
var StrictCeremonyExclusions = map[Verb]string{
	VerbCeremonyInitialize: "mints the CA, the server TLS identity and the SO credential; " +
		"an unauthenticated caller reaching it on a fresh daemon owns the trust root. " +
		"Runs in-process during `quicbaasd init`.",
	VerbCeremonySignCSR: "issues a certificate from the ceremony CA. Requires the SO PIN and " +
		"an already-enrolled operator, so it stays authenticated and authorized.",
}

// Excluded reports whether v is withheld from the quicbaas sealed window by
// the strict ceremony model. Every derivation consults it, so an exclusion
// cannot apply to some transports and not others.
func Excluded(v Verb) bool {
	_, ok := StrictCeremonyExclusions[v]
	return ok
}

// Table maps every canonical verb to its quicbaas per-transport names. This
// is the single source of truth for the quicbaas sealed-window allowlists.
//
// REST entries include qrdb's canonical /v1 wire paths verbatim (quicbaas
// mounts /v1/barrier, /v1/ceremony, and /v1/init at its router root for
// qrdb-SDK wire compatibility) plus the quicbaas /api/v1 mirrors where a
// mirror exists (/api/v1/barrier/* and the ceremony surface at
// /api/v1/init/*). Every mirror is listed as a FULL PATH next to the verb it
// mirrors; the /api/v1/init/ prefix that used to stand in for the whole
// ceremony surface is gone, because a prefix silently re-admits any route a
// later change mounts beneath it — which is exactly how initialize, sign-csr
// and enroll-external-officer became reachable pre-authentication. Listing a
// mirror beside its verb also keeps the two mounts of one handler in
// lock-step, so /api/v1/barrier/unseal and /v1/barrier/unseal can never be
// gated differently.
//
// Paths that qrdb defines but quicbaas does not currently mount are
// retained verbatim: they are inert against the router (404) and keep the
// table diffable against qrdb's.
var Table = map[Verb]Names{
	VerbBarrierStatus: {
		REST:     []string{"/v1/barrier/status", "/api/v1/barrier/status"},
		GRPC:     []string{"BarrierStatus"},
		QUIC:     []QUICOp{{Opcode: opBarrier, SubOp: subBarrierStatus}},
		QRDBQUIC: []string{"barrier.Status"},
		MCP:      []string{"qrdb_barrier_status"},
	},
	VerbBarrierInitialize: {
		REST:               []string{"/v1/barrier/initialize", "/api/v1/barrier/initialize"},
		GRPC:               []string{"BarrierInitialize"},
		QUIC:               []QUICOp{{Opcode: opBarrier, SubOp: subBarrierInitialize}},
		QRDBQUIC:           []string{"barrier.Initialize"},
		MCP:                []string{"qrdb_barrier_initialize"},
		PreOperationalOnly: true,
	},
	VerbBarrierUnseal: {
		REST:     []string{"/v1/barrier/unseal", "/api/v1/barrier/unseal"},
		GRPC:     []string{"BarrierUnseal"},
		QUIC:     []QUICOp{{Opcode: opBarrier, SubOp: subBarrierUnseal}},
		QRDBQUIC: []string{"barrier.Unseal"},
		MCP:      []string{"qrdb_barrier_unseal"},
	},

	VerbShamirInitialize: {
		REST:               []string{"/v1/barrier/shamir/initialize", "/api/v1/barrier/shamir/initialize"},
		GRPC:               []string{"BarrierShamirInitialize"},
		QUIC:               []QUICOp{{Opcode: opBarrier, SubOp: subBarrierShamirInitialize}},
		QRDBQUIC:           []string{"barrier.ShamirInitialize"},
		MCP:                []string{"qrdb_barrier_shamir_initialize"},
		PreOperationalOnly: true,
	},
	// GET and POST /v1/barrier/officers share one path (method-dispatched).
	// officer-register is PreOperationalOnly: qrdb's RegisterOfficer service
	// body CA-signs any caller-supplied CSR and assigns a Shamir share index
	// with no service-layer authentication, so an exemption that survived
	// ceremony completion would let an unauthenticated caller enroll an
	// officer during any restart-while-sealed. Officer enrollment belongs to
	// the enrollment ceremony; the exemption lapses at operational.
	VerbOfficerRegister: {
		REST:               []string{"POST /v1/barrier/officers", "POST /api/v1/barrier/officers"},
		GRPC:               []string{"BarrierOfficerRegister"},
		QRDBQUIC:           []string{"barrier.OfficerRegister"},
		MCP:                []string{"qrdb_barrier_officer_register"},
		PreOperationalOnly: true,
	},
	// The claim routes carry the holder in a path segment. They are listed
	// as one-segment patterns, never as an /v1/barrier/officers/ prefix: a
	// prefix would also admit POST /v1/barrier/officers/revoke, a rekey that
	// removes a holder, which is not a sealed-window operation (mirrors qrdb).
	VerbShareClaimBegin: {
		REST:     []string{"POST /v1/barrier/officers/{username}/claim/begin", "POST /api/v1/barrier/officers/{username}/claim/begin"},
		GRPC:     []string{"BarrierShareClaimBegin"},
		QRDBQUIC: []string{"barrier.ShareClaimBegin"},
		MCP:      []string{"qrdb_barrier_share_claim_begin"},
	},
	VerbShareClaimComplete: {
		REST:     []string{"POST /v1/barrier/officers/{username}/claim/complete", "POST /api/v1/barrier/officers/{username}/claim/complete"},
		GRPC:     []string{"BarrierShareClaimComplete"},
		QRDBQUIC: []string{"barrier.ShareClaimComplete"},
		MCP:      []string{"qrdb_barrier_share_claim_complete"},
	},

	// recovery-officer-register shares officer-register's PreOperationalOnly
	// rationale: it enrolls a recovery officer via the same
	// CSR-sign-and-assign-share flow with no service-layer auth.
	VerbRecoveryOfficerRegister: {
		REST:               []string{"POST /v1/barrier/recovery/officers", "POST /api/v1/barrier/recovery/officers"},
		GRPC:               []string{"BarrierRecoveryOfficerRegister"},
		QRDBQUIC:           []string{"barrier.RecoveryOfficerRegister"},
		MCP:                []string{"qrdb_barrier_recovery_officer_register"},
		PreOperationalOnly: true,
	},
	VerbRecoveryShareClaimBegin: {
		REST:     []string{"POST /v1/barrier/recovery/officers/{username}/claim/begin", "POST /api/v1/barrier/recovery/officers/{username}/claim/begin"},
		GRPC:     []string{"BarrierRecoveryShareClaimBegin"},
		QRDBQUIC: []string{"barrier.RecoveryShareClaimBegin"},
		MCP:      []string{"qrdb_barrier_recovery_share_claim_begin"},
	},
	VerbRecoveryShareClaimComplete: {
		REST:     []string{"POST /v1/barrier/recovery/officers/{username}/claim/complete", "POST /api/v1/barrier/recovery/officers/{username}/claim/complete"},
		GRPC:     []string{"BarrierRecoveryShareClaimComplete"},
		QRDBQUIC: []string{"barrier.RecoveryShareClaimComplete"},
		MCP:      []string{"qrdb_barrier_recovery_share_claim_complete"},
	},

	// The holder quorum (mirrors qrdb): a holder answers a factor challenge
	// and submits its share. Unseal and recovery happen here, so both must
	// be reachable while sealed; the factor proof, verified against the
	// roster the wrapped root key is sealed under, is the credential.
	VerbQuorumBegin: {
		REST:     []string{"/v1/barrier/quorum/begin", "/api/v1/barrier/quorum/begin"},
		GRPC:     []string{"BarrierQuorumBegin"},
		QRDBQUIC: []string{"barrier.QuorumBegin"},
		MCP:      []string{"qrdb_barrier_quorum_begin"},
	},
	VerbQuorumSubmit: {
		REST:     []string{"/v1/barrier/quorum/submit", "/api/v1/barrier/quorum/submit"},
		GRPC:     []string{"BarrierQuorumSubmit"},
		QRDBQUIC: []string{"barrier.QuorumSubmit"},
		MCP:      []string{"qrdb_barrier_quorum_submit"},
	},

	// EXCLUDED by StrictCeremonyExclusions. The names stay listed so the
	// table remains diffable against qrdb's and so the route-coverage guard
	// test can account for every mounted ceremony route; every derivation
	// below skips the verb.
	VerbCeremonyInitialize: {
		REST:               []string{"/v1/ceremony/initialize", "/api/v1/init/initialize"},
		GRPC:               []string{"CeremonyInitialize"},
		QUIC:               []QUICOp{{Opcode: opCeremony, SubOp: subCeremonyInitialize}},
		QRDBQUIC:           []string{"ceremony.Initialize"},
		MCP:                []string{"qrdb_ceremony_initialize"},
		PreOperationalOnly: true,
	},
	// EXCLUDED by StrictCeremonyExclusions (see VerbCeremonyInitialize).
	VerbCeremonySignCSR: {
		REST:               []string{"/v1/ceremony/sign-csr", "/api/v1/init/sign-csr"},
		GRPC:               []string{"CeremonySignCSR"},
		QUIC:               []QUICOp{{Opcode: opCeremony, SubOp: subCeremonySignCSR}},
		QRDBQUIC:           []string{"ceremony.SignCSR"},
		MCP:                []string{"qrdb_ceremony_sign_csr"},
		PreOperationalOnly: true,
	},

	// AuthenticatedOnly (mirrors qrdb): the deny gate admits cluster unseal
	// while sealed, but no scope exempts it from authentication.
	VerbBarrierClusterUnseal: {
		REST:              []string{"/v1/barrier/cluster-unseal", "/api/v1/barrier/cluster-unseal"},
		GRPC:              []string{"BarrierClusterUnseal"},
		QRDBQUIC:          []string{"barrier.ClusterUnseal"},
		MCP:               []string{"qrdb_barrier_cluster_unseal"},
		AuthenticatedOnly: true,
	},
}

// RESTExtra lists REST-only allowlist entries that have no transport-neutral
// verb: the first-touch trust-bootstrap flow and the cluster peer
// key-transport receiver, both of which must operate in every daemon state.
// Entries ending in "/" are prefix matches.
//
// The "/v1/init/" and "/api/v1/init/" PREFIXES that qrdb carries here are
// deliberately absent. A prefix admits every route mounted beneath it,
// present and future, without review — which is how initialize, sign-csr and
// enroll-external-officer ended up reachable pre-authentication. The init
// ceremony surface is now enumerated as full exact paths beside the verbs it
// mirrors in Table, and the guard test in sealedwindow_test.go fails if a
// prefix covering an init or ceremony route is ever reintroduced here.
var RESTExtra = []string{
	// qrdb-wire mount at the quicbaas router root.
	"/v1/bootstrap/",
	// quicbaas /api/v1 mirror: first-touch trust bootstrap.
	"/api/v1/bootstrap/",
	// Cluster peer key-transport: a joining node fetches its wrapped cluster
	// seed here after enrollment. Must work regardless of sealed state so
	// follower replicas can receive material before unsealing; the handler
	// self-authorises via node cert + attestation.
	"/api/v1/cluster/secret",
}

// Scope selects which slice of the canonical table a derivation covers. The
// sealed window has two independent gates and they cover different verb sets,
// so every derivation is parameterised by scope rather than forked per gate.
type Scope int

const (
	// ScopeAll covers every verb the strict model admits, regardless of
	// ceremony state. It is what the per-transport DENY gates consume: a
	// request outside this set is refused with qrdb's sealed envelope while
	// the barrier is sealed.
	ScopeAll Scope = iota

	// ScopeAlways covers admitted verbs whose authentication exemption holds
	// in ANY ceremony state — the barrier and unseal surface. A daemon
	// restarted after a completed ceremony is operational and sealed, and
	// these verbs are the only way back to a usable data plane.
	ScopeAlways

	// ScopePreOperational covers admitted verbs whose authentication
	// exemption ALSO requires the ceremony to be unfinished: the
	// self-authorizing claim handshakes and the read-only status probe.
	ScopePreOperational
)

// InScope reports whether verb v participates in scope s.
//
// It fails closed on both axes: a verb withheld by StrictCeremonyExclusions
// is in no scope at all, and an unrecognized Scope value matches nothing. All
// five transport derivations route through it, so a verb cannot be scoped one
// way on REST and another on gRPC.
func InScope(v Verb, s Scope) bool {
	if Excluded(v) {
		return false
	}
	switch s {
	case ScopeAll:
		return true
	case ScopeAlways:
		return !Table[v].PreOperationalOnly && !Table[v].AuthenticatedOnly
	case ScopePreOperational:
		return Table[v].PreOperationalOnly && !Table[v].AuthenticatedOnly
	}
	return false
}

// RESTAllowlist derives the full REST sealed-window allowlist (ScopeAll) from
// Table and RESTExtra. This is the list the deny gate consumes.
func RESTAllowlist() []string {
	return RESTAllowlistScoped(ScopeAll)
}

// RESTAllowlistScoped derives the REST sealed-window allowlist for one scope,
// deduplicating shared paths while preserving derivation order. RESTExtra has
// no transport-neutral verb and no ceremony-state dependency, so it
// contributes to every scope except ScopePreOperational.
//
// A REST path can be shared by several verbs — GET and POST
// /v1/barrier/officers are officer-list and officer-register, dispatched by
// method behind ONE path. The REST authentication bypass matches on path
// alone, so a shared path takes the STRICTEST treatment of its contributing
// verbs: if ANY contributor is PreOperationalOnly, the path's exemption is
// pre-operational-gated for every method it serves, and if ANY contributor is
// AuthenticatedOnly, the path has no exemption at all. Splitting the path per
// scope would leave the flagged verb reachable through the unflagged verb's
// state-independent exemption — the exact fail-open
// TestAuthBypass_ScopesDisjoint forbids. The wire-name-keyed transports
// (gRPC, QUIC, MCP) need no such collapse because each verb owns distinct
// names there.
func RESTAllowlistScoped(s Scope) []string {
	// First pass: collect every admitted path in derivation order and OR
	// together the PreOperationalOnly flags of its contributing verbs.
	var order []string
	preOp := make(map[string]bool)
	authOnly := make(map[string]bool)
	seen := make(map[string]struct{})
	for _, v := range Verbs {
		if Excluded(v) {
			continue
		}
		for _, p := range Table[v].REST {
			if _, dup := seen[p]; !dup {
				seen[p] = struct{}{}
				order = append(order, p)
			}
			preOp[p] = preOp[p] || Table[v].PreOperationalOnly
			authOnly[p] = authOnly[p] || Table[v].AuthenticatedOnly
		}
	}

	var out []string
	for _, p := range order {
		switch s {
		case ScopeAll:
			out = append(out, p)
		case ScopeAlways:
			if !preOp[p] && !authOnly[p] {
				out = append(out, p)
			}
		case ScopePreOperational:
			if preOp[p] && !authOnly[p] {
				out = append(out, p)
			}
		}
	}
	if s == ScopeAll || s == ScopeAlways {
		for _, p := range RESTExtra {
			if _, dup := seen[p]; !dup {
				out = append(out, p)
			}
		}
	}
	return out
}

// grpcMirrorServices are the quicbaas-native gRPC services that mirror
// sealed-window verbs onto the quicbaas namespace. Every fully-qualified
// allowlist entry for a quicbaas service is derived by intersecting the
// canonical method tails with the methods these ServiceDescs actually
// declare, so a tail can never be qualified onto a service that does not
// serve it and a proto rename shows up as a missing entry rather than as a
// silently inert string.
//
// The list is deliberately CLOSED. Services omitted here are excluded on
// purpose:
//
//   - quicbaas.tenant.v1.TenantBarrier — the per-tenant barrier surface is a
//     documented sealed-window exclusion, and it declares a method named
//     "Status" whose tail collides with the canonical admin status verb;
//   - quicbaas.fleet.runtime.v1.FleetRuntime ("Status"),
//     quicbaas.orchestrator.v1.OrchestratorService, quicbaas.ollama.v1 and
//     quicbaas.vector.v1 ("Health") — post-unseal data-plane services whose
//     liveness verbs collide by tail only;
//   - quicbaas.v1.QuicBaaS — its Health liveness RPC answers no question on a
//     sealed node (go-qrdb v0.50 excludes health from the sealed window), and
//     its data plane (Get/Put/...) shares no tail with the canonical table.
var grpcMirrorServices = []ggrpc.ServiceDesc{
	pb.Barrier_ServiceDesc,
	pb.Ceremony_ServiceDesc,
}

// GRPCAllowlist derives the gRPC sealed-window allowlist as bare method tails
// from Table in derivation order, deduplicated. It mirrors qrdb's
// sealedwindow.GRPCAllowlist so the two canonical tables diff mechanically.
//
// The entries are NOT suitable for gating live requests: a bare tail such as
// "Status" would match the same-named method on ANY co-registered quicbaas
// service (e.g. "/quicbaas.tenant.v1.TenantBarrier/Status", a deliberate
// sealed-window exclusion). Callers that gate live traffic MUST use
// GRPCFullMethodAllowlist, which pins the service.
func GRPCAllowlist() []string {
	return GRPCAllowlistScoped(ScopeAll)
}

// GRPCAllowlistScoped derives the bare gRPC method tails for one scope. Like
// GRPCAllowlist it is NOT suitable for gating live requests; see
// GRPCFullMethodAllowlistScoped.
func GRPCAllowlistScoped(s Scope) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, v := range Verbs {
		if !InScope(v, s) {
			continue
		}
		for _, m := range Table[v].GRPC {
			if _, dup := seen[m]; dup {
				continue
			}
			seen[m] = struct{}{}
			out = append(out, m)
		}
	}
	return out
}

// excludedGRPCTails returns the bare method tails belonging to verbs the
// strict model withholds. It is the filter applied to qrdb's own
// fully-qualified allowlist, which quicbaas otherwise consumes verbatim:
// quicbaas registers qrdb.v1.QRDBService on its gRPC server, so without this
// filter the excluded ceremony verbs would remain reachable pre-authentication
// on the qrdb service even though quicbaas removed them from its own mirrors.
// That asymmetry — narrowed on one service, wide open on another serving the
// same operation — is precisely the drift the single table exists to prevent.
func excludedGRPCTails() map[string]struct{} {
	out := make(map[string]struct{})
	for _, v := range Verbs {
		if !Excluded(v) {
			continue
		}
		for _, m := range Table[v].GRPC {
			out[m] = struct{}{}
		}
	}
	return out
}

// preOperationalGRPCTails returns the bare method tails whose exemption is
// gated on a pre-operational ceremony.
func preOperationalGRPCTails() map[string]struct{} {
	return grpcTailsWhere(func(n Names) bool { return n.PreOperationalOnly })
}

// authenticatedOnlyGRPCTails returns the gRPC method tails of every
// AuthenticatedOnly verb. GRPCFullMethodAllowlistScoped keeps the matching
// qrdb.v1 methods out of both authentication-bypass scopes, because they are
// consumed from qrdbsdk.GRPCSealedWindowAllowlist, which carries no flag.
func authenticatedOnlyGRPCTails() map[string]struct{} {
	return grpcTailsWhere(func(n Names) bool { return n.AuthenticatedOnly })
}

// grpcTailsWhere returns the gRPC method tails of every non-excluded verb
// whose Names satisfy keep.
func grpcTailsWhere(keep func(Names) bool) map[string]struct{} {
	out := make(map[string]struct{})
	for _, v := range Verbs {
		if Excluded(v) || !keep(Table[v]) {
			continue
		}
		for _, m := range Table[v].GRPC {
			out[m] = struct{}{}
		}
	}
	return out
}

// methodTail returns the bare method name of a fully-qualified gRPC method
// path ("/qrdb.v1.QRDBService/BarrierUnseal" -> "BarrierUnseal"). A path with
// no "/" is returned unchanged.
func methodTail(fullMethod string) string {
	if i := strings.LastIndex(fullMethod, "/"); i >= 0 {
		return fullMethod[i+1:]
	}
	return fullMethod
}

// GRPCFullMethodAllowlist derives the gRPC sealed-window allowlist as
// fully-qualified "/{service}/{method}" paths — the exact form of
// grpc.UnaryServerInfo.FullMethod — for every gRPC service quicbaas registers
// that serves sealed-window verbs. This is the list the sealed-window
// interceptors and the authentication bypass consume; matching is exact, so a
// same-named method on any other co-registered service stays sealed.
//
// Two sources compose it, in derivation order:
//
//  1. The qrdb.v1.QRDBService composition surface, taken verbatim from qrdb's
//     own canonical derivation (qrdbsdk.GRPCSealedWindowAllowlist). quicbaas
//     registers that service through qrdbsdk.RegisterGRPCService, so consuming
//     qrdb's list directly makes drift on the qrdb.v1 surface impossible.
//  2. The quicbaas mirror services in grpcMirrorServices, qualified with the
//     canonical tails from Table that each service actually declares.
func GRPCFullMethodAllowlist() []string {
	return GRPCFullMethodAllowlistScoped(ScopeAll)
}

// GRPCFullMethodAllowlistScoped derives the fully-qualified gRPC allowlist for
// one scope. qrdb's own entries are classified by method tail against the same
// canonical table, so the strict exclusions and the pre-operational gate apply
// identically to the qrdb.v1 surface and to the quicbaas mirrors.
//
// A qrdb tail the quicbaas table does not name is treated as unexcluded and
// state-independent, preserving qrdb's behavior for any verb quicbaas has not
// modelled. TestGRPCTailsCoverQRDBAllowlist asserts that set is empty, so the
// fallback is provably unreachable rather than a silent widening.
func GRPCFullMethodAllowlistScoped(s Scope) []string {
	tails := GRPCAllowlistScoped(s)
	excluded := excludedGRPCTails()
	preOp := preOperationalGRPCTails()
	authOnly := authenticatedOnlyGRPCTails()

	// qualifies reports whether a qrdb-supplied fully-qualified method
	// belongs in this scope. Exclusion wins over every scope (fail closed).
	qualifies := func(fullMethod string) bool {
		tail := methodTail(fullMethod)
		if _, bad := excluded[tail]; bad {
			return false
		}
		_, isPreOp := preOp[tail]
		_, isAuthOnly := authOnly[tail]
		switch s {
		case ScopeAll:
			return true
		case ScopeAlways:
			return !isPreOp && !isAuthOnly
		case ScopePreOperational:
			return isPreOp && !isAuthOnly
		}
		return false
	}

	size := len(qrdbsdk.GRPCSealedWindowAllowlist) + len(tails)*len(grpcMirrorServices)
	seen := make(map[string]struct{}, size)
	out := make([]string, 0, size)
	appendMethod := func(m string) {
		if _, dup := seen[m]; dup {
			return
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	for _, m := range qrdbsdk.GRPCSealedWindowAllowlist {
		if !qualifies(m) {
			continue
		}
		appendMethod(m)
	}
	for _, desc := range grpcMirrorServices {
		declared := declaredMethods(desc)
		for _, tail := range tails {
			if _, ok := declared[tail]; !ok {
				continue
			}
			appendMethod("/" + desc.ServiceName + "/" + tail)
		}
	}
	return out
}

// declaredMethods returns the set of method names a gRPC service declares,
// covering both unary methods and streams.
func declaredMethods(desc ggrpc.ServiceDesc) map[string]struct{} {
	out := make(map[string]struct{}, len(desc.Methods)+len(desc.Streams))
	for _, m := range desc.Methods {
		out[m.MethodName] = struct{}{}
	}
	for _, s := range desc.Streams {
		out[s.StreamName] = struct{}{}
	}
	return out
}

// QUICAllowlist derives the native quicbaas-QUIC sealed-window allowlist
// (opcode + sub-op pairs) from Table. This is the list the deny gate
// consumes.
//
// Like every other derivation it filters through InScope, so the strict
// ceremony model's exclusions apply here too: the trust-minting verbs
// (initialize / sign-csr / enroll-external-officer) are withheld from the
// native QUIC window exactly as they are from REST and gRPC. Skipping that
// filter on one transport would leave those verbs servable AND
// authentication-exempt while sealed on that transport alone — the
// per-transport drift the single table exists to prevent.
func QUICAllowlist() map[QUICOp]struct{} {
	return QUICAllowlistScoped(ScopeAll)
}

// QUICAllowlistScoped derives the native quicbaas-QUIC allowlist for one
// scope.
func QUICAllowlistScoped(s Scope) map[QUICOp]struct{} {
	out := make(map[QUICOp]struct{})
	for _, v := range Verbs {
		if !InScope(v, s) {
			continue
		}
		for _, op := range Table[v].QUIC {
			out[op] = struct{}{}
		}
	}
	return out
}

// QRDBQUICAllowlist derives the qrdb-quic ALPN bridge sealed-window
// allowlist (method names) from Table, filtered through InScope so the strict
// ceremony exclusions apply here as they do on every other transport. This is
// the list the deny gate consumes.
func QRDBQUICAllowlist() map[string]struct{} {
	return QRDBQUICAllowlistScoped(ScopeAll)
}

// QRDBQUICAllowlistScoped derives the qrdb-quic ALPN bridge allowlist for one
// scope.
func QRDBQUICAllowlistScoped(s Scope) map[string]struct{} {
	out := make(map[string]struct{})
	for _, v := range Verbs {
		if !InScope(v, s) {
			continue
		}
		for _, m := range Table[v].QRDBQUIC {
			out[m] = struct{}{}
		}
	}
	return out
}

// MCPAllowlist derives the MCP tool-name sealed-window allowlist from Table,
// filtered through InScope so the strict ceremony exclusions apply here as
// they do on every other transport. This is the list the deny gate consumes.
func MCPAllowlist() map[string]struct{} {
	return MCPAllowlistScoped(ScopeAll)
}

// MCPAllowlistScoped derives the MCP tool-name allowlist for one scope.
func MCPAllowlistScoped(s Scope) map[string]struct{} {
	out := make(map[string]struct{})
	for _, v := range Verbs {
		if !InScope(v, s) {
			continue
		}
		for _, m := range Table[v].MCP {
			out[m] = struct{}{}
		}
	}
	return out
}
