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

// Verb is a transport-neutral identifier for one sealed-window operation.
// The identifiers use the same string values as qrdb's canonical table so
// the two tables can be diffed mechanically.
type Verb string

// Canonical sealed-window verbs (1:1 with go-qrdb/pkg/api/sealedwindow).
const (
	VerbHealth Verb = "health"
	VerbStatus Verb = "status"

	VerbBarrierStatus     Verb = "barrier-status"
	VerbBarrierInitialize Verb = "barrier-initialize"
	VerbBarrierUnseal     Verb = "barrier-unseal"

	VerbShamirInitialize   Verb = "shamir-initialize"
	VerbShamirUnsealShare  Verb = "shamir-unseal-share"
	VerbShamirUnsealShares Verb = "shamir-unseal-shares"
	VerbShamirVerify       Verb = "shamir-verify"

	VerbRekey Verb = "rekey"

	VerbRecoveryGenerate Verb = "recovery-generate"
	VerbRecoveryRecover  Verb = "recovery-recover"
	VerbRecoveryDelete   Verb = "recovery-delete"
	VerbRecoveryStatus   Verb = "recovery-status"

	VerbRootToken Verb = "root-token"

	VerbOfficerRegister    Verb = "officer-register"
	VerbOfficerList        Verb = "officer-list"
	VerbOfficerClaimStatus Verb = "officer-claim-status"
	VerbShareClaimBegin    Verb = "share-claim-begin"
	VerbShareClaimComplete Verb = "share-claim-complete"

	VerbRecoveryOfficerRegister    Verb = "recovery-officer-register"
	VerbRecoveryShareClaimBegin    Verb = "recovery-share-claim-begin"
	VerbRecoveryShareClaimComplete Verb = "recovery-share-claim-complete"

	VerbCeremonyInitialize           Verb = "ceremony-initialize"
	VerbCeremonyStatus               Verb = "ceremony-status"
	VerbCeremonyClaimCertBegin       Verb = "ceremony-claim-cert-begin"
	VerbCeremonyClaimCertComplete    Verb = "ceremony-claim-cert-complete"
	VerbCeremonyOfficerShareBegin    Verb = "ceremony-officer-share-begin"
	VerbCeremonyOfficerShareComplete Verb = "ceremony-officer-share-complete"
	VerbCeremonySignCSR              Verb = "ceremony-sign-csr"
	VerbCeremonyClaimShareBegin      Verb = "ceremony-claim-share-begin"
	VerbCeremonyClaimShareEnd        Verb = "ceremony-claim-share-complete"
	VerbCeremonySubmitUnsealShare    Verb = "ceremony-submit-unseal-share"
	VerbCeremonyEnrollExtOfficer     Verb = "ceremony-enroll-external-officer"
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
	// GRPC method tails — the suffix after the last "/" in FullMethod —
	// matching qrdb's gRPC allowlist convention so the SDK-re-exported
	// SealedWindowUnaryInterceptor / StreamInterceptor consume them
	// directly. One tail covers BOTH the qrdb.v1.* registration and the
	// quicbaas.* mirror service when the method names align (they do by
	// design: quicbaas mirrors qrdb's verb names verbatim).
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
}

// Native quicbaas-QUIC opcode / sub-op values. These mirror the protocol
// constants in pkg/api/quic/protocol.go; the guard test asserts the two
// stay in lock-step (this package must not import the quic transport to
// keep the derivation table dependency-free).
const (
	opHealth   byte = 0x90
	opBarrier  byte = 0xC0
	opCeremony byte = 0xC1

	subBarrierStatus             byte = 0x01
	subBarrierInitialize         byte = 0x02
	subBarrierUnseal             byte = 0x03
	subBarrierShamirInitialize   byte = 0x05
	subBarrierShamirUnsealShare  byte = 0x06
	subBarrierShamirUnsealShares byte = 0x07
	subBarrierRekey              byte = 0x0C
	subBarrierRecoveryGenerate   byte = 0x0D
	subBarrierRecoveryRecover    byte = 0x0E
	subBarrierRecoveryDelete     byte = 0x0F
	subBarrierRecoveryStatus     byte = 0x10
	subBarrierRootToken          byte = 0x11

	subCeremonyInitialize            byte = 0x01
	subCeremonyStatus                byte = 0x02
	subCeremonyClaimCertBegin        byte = 0x03
	subCeremonyClaimCertComplete     byte = 0x04
	subCeremonyBeginClaimShare       byte = 0x05
	subCeremonySignCSR               byte = 0x06
	subCeremonyListOfficers          byte = 0x07
	subCeremonyEnrollExternalOfficer byte = 0x08
	subCeremonyCompleteClaimShare    byte = 0x09
)

// Verbs is the canonical verb list in stable derivation order (identical to
// qrdb's order).
var Verbs = []Verb{
	VerbHealth,
	VerbStatus,
	VerbBarrierStatus,
	VerbBarrierInitialize,
	VerbBarrierUnseal,
	VerbShamirInitialize,
	VerbShamirUnsealShare,
	VerbShamirUnsealShares,
	VerbShamirVerify,
	VerbRekey,
	VerbRecoveryGenerate,
	VerbRecoveryRecover,
	VerbRecoveryDelete,
	VerbRecoveryStatus,
	VerbRootToken,
	VerbOfficerRegister,
	VerbOfficerList,
	VerbOfficerClaimStatus,
	VerbShareClaimBegin,
	VerbShareClaimComplete,
	VerbRecoveryOfficerRegister,
	VerbRecoveryShareClaimBegin,
	VerbRecoveryShareClaimComplete,
	VerbCeremonyInitialize,
	VerbCeremonyStatus,
	VerbCeremonyClaimCertBegin,
	VerbCeremonyClaimCertComplete,
	VerbCeremonyOfficerShareBegin,
	VerbCeremonyOfficerShareComplete,
	VerbCeremonySignCSR,
	VerbCeremonyClaimShareBegin,
	VerbCeremonyClaimShareEnd,
	VerbCeremonySubmitUnsealShare,
	VerbCeremonyEnrollExtOfficer,
}

// Table maps every canonical verb to its quicbaas per-transport names. This
// is the single source of truth for the quicbaas sealed-window allowlists.
//
// REST entries include qrdb's canonical /v1 wire paths verbatim (quicbaas
// mounts /v1/barrier, /v1/ceremony, and /v1/init at its router root for
// qrdb-SDK wire compatibility) plus the quicbaas /api/v1 mirrors where a
// mirror exists (/api/v1/barrier/*; the ceremony surface is mounted at
// /api/v1/init/* and covered by the RESTExtra prefix). Paths that qrdb
// defines but quicbaas does not currently mount (e.g. the officer claim
// routes) are retained verbatim: they are inert against the router (404)
// and keep the table diffable against qrdb's.
var Table = map[Verb]Names{
	VerbHealth: {
		REST:     []string{"/health", "/v1/health", "/api/v1/health", "/api/v1/health/live", "/api/v1/health/ready"},
		GRPC:     []string{"Health"},
		QUIC:     []QUICOp{{Opcode: opHealth}},
		QRDBQUIC: []string{"admin.Health"},
	},
	VerbStatus: {
		REST:     []string{"/status"},
		GRPC:     []string{"Status"},
		QRDBQUIC: []string{"admin.Status"},
	},

	VerbBarrierStatus: {
		REST:     []string{"/v1/barrier/status", "/api/v1/barrier/status"},
		GRPC:     []string{"BarrierStatus"},
		QUIC:     []QUICOp{{Opcode: opBarrier, SubOp: subBarrierStatus}},
		QRDBQUIC: []string{"barrier.Status"},
		MCP:      []string{"qrdb_barrier_status"},
	},
	VerbBarrierInitialize: {
		REST:     []string{"/v1/barrier/initialize", "/api/v1/barrier/initialize"},
		GRPC:     []string{"BarrierInitialize"},
		QUIC:     []QUICOp{{Opcode: opBarrier, SubOp: subBarrierInitialize}},
		QRDBQUIC: []string{"barrier.Initialize"},
		MCP:      []string{"qrdb_barrier_initialize"},
	},
	VerbBarrierUnseal: {
		REST:     []string{"/v1/barrier/unseal", "/api/v1/barrier/unseal"},
		GRPC:     []string{"BarrierUnseal"},
		QUIC:     []QUICOp{{Opcode: opBarrier, SubOp: subBarrierUnseal}},
		QRDBQUIC: []string{"barrier.Unseal"},
		MCP:      []string{"qrdb_barrier_unseal"},
	},

	VerbShamirInitialize: {
		REST:     []string{"/v1/barrier/shamir/initialize", "/api/v1/barrier/shamir/initialize"},
		GRPC:     []string{"BarrierShamirInitialize"},
		QUIC:     []QUICOp{{Opcode: opBarrier, SubOp: subBarrierShamirInitialize}},
		QRDBQUIC: []string{"barrier.ShamirInitialize"},
		MCP:      []string{"qrdb_barrier_shamir_initialize"},
	},
	VerbShamirUnsealShare: {
		REST:     []string{"/v1/barrier/shamir/unseal-share", "/api/v1/barrier/shamir/unseal-share"},
		GRPC:     []string{"BarrierShamirUnsealShare"},
		QUIC:     []QUICOp{{Opcode: opBarrier, SubOp: subBarrierShamirUnsealShare}},
		QRDBQUIC: []string{"barrier.ShamirUnsealShare"},
		MCP:      []string{"qrdb_barrier_shamir_unseal_share"},
	},
	VerbShamirUnsealShares: {
		REST:     []string{"/v1/barrier/shamir/unseal-shares", "/api/v1/barrier/shamir/unseal-shares"},
		GRPC:     []string{"BarrierShamirUnsealShares"},
		QUIC:     []QUICOp{{Opcode: opBarrier, SubOp: subBarrierShamirUnsealShares}},
		QRDBQUIC: []string{"barrier.ShamirUnsealShares"},
		MCP:      []string{"qrdb_barrier_shamir_unseal_shares"},
	},
	// shamir-verify has no qrdb gRPC RPC or MCP tool; quicbaas additionally
	// exposes it on its own quicbaas.barrier.v1 mirror (BarrierShamirVerify).
	VerbShamirVerify: {
		REST:     []string{"/v1/barrier/shamir/verify"},
		GRPC:     []string{"BarrierShamirVerify"},
		QRDBQUIC: []string{"barrier.ShamirVerify"},
	},

	VerbRekey: {
		REST:     []string{"/v1/barrier/rekey", "/api/v1/barrier/rekey"},
		GRPC:     []string{"BarrierRekey"},
		QUIC:     []QUICOp{{Opcode: opBarrier, SubOp: subBarrierRekey}},
		QRDBQUIC: []string{"barrier.Rekey"},
		MCP:      []string{"qrdb_barrier_rekey"},
	},

	VerbRecoveryGenerate: {
		REST:     []string{"/v1/barrier/recovery/generate", "/api/v1/barrier/recovery/generate"},
		GRPC:     []string{"BarrierRecoveryGenerate"},
		QUIC:     []QUICOp{{Opcode: opBarrier, SubOp: subBarrierRecoveryGenerate}},
		QRDBQUIC: []string{"barrier.RecoveryGenerate"},
		MCP:      []string{"qrdb_barrier_recovery_generate"},
	},
	VerbRecoveryRecover: {
		REST:     []string{"/v1/barrier/recovery/recover", "/api/v1/barrier/recovery/recover"},
		GRPC:     []string{"BarrierRecoveryRecover"},
		QUIC:     []QUICOp{{Opcode: opBarrier, SubOp: subBarrierRecoveryRecover}},
		QRDBQUIC: []string{"barrier.RecoveryRecover"},
		MCP:      []string{"qrdb_barrier_recovery_recover"},
	},
	// REST recovery-delete is DELETE /v1/barrier/recovery (exact path).
	VerbRecoveryDelete: {
		REST:     []string{"/v1/barrier/recovery", "/api/v1/barrier/recovery"},
		GRPC:     []string{"BarrierRecoveryDelete"},
		QUIC:     []QUICOp{{Opcode: opBarrier, SubOp: subBarrierRecoveryDelete}},
		QRDBQUIC: []string{"barrier.RecoveryDelete"},
		MCP:      []string{"qrdb_barrier_recovery_delete"},
	},
	VerbRecoveryStatus: {
		REST:     []string{"/v1/barrier/recovery/status", "/api/v1/barrier/recovery/status"},
		GRPC:     []string{"BarrierRecoveryStatus"},
		QUIC:     []QUICOp{{Opcode: opBarrier, SubOp: subBarrierRecoveryStatus}},
		QRDBQUIC: []string{"barrier.RecoveryStatus"},
		MCP:      []string{"qrdb_barrier_recovery_status"},
	},

	VerbRootToken: {
		REST:     []string{"/v1/barrier/root-token", "/api/v1/barrier/root-token"},
		GRPC:     []string{"BarrierRootToken"},
		QUIC:     []QUICOp{{Opcode: opBarrier, SubOp: subBarrierRootToken}},
		QRDBQUIC: []string{"barrier.RootToken"},
		MCP:      []string{"qrdb_barrier_root_token"},
	},

	// GET and POST /v1/barrier/officers share one path (method-dispatched).
	VerbOfficerRegister: {
		REST:     []string{"/v1/barrier/officers"},
		GRPC:     []string{"BarrierOfficerRegister"},
		QRDBQUIC: []string{"barrier.OfficerRegister"},
		MCP:      []string{"qrdb_barrier_officer_register"},
	},
	VerbOfficerList: {
		REST:     []string{"/v1/barrier/officers", "/v1/ceremony/officers"},
		GRPC:     []string{"BarrierOfficerList", "CeremonyListOfficers"},
		QUIC:     []QUICOp{{Opcode: opCeremony, SubOp: subCeremonyListOfficers}},
		QRDBQUIC: []string{"barrier.OfficerList"},
		MCP:      []string{"qrdb_barrier_officer_list", "quicbaas_ceremony_list_officers"},
	},
	VerbOfficerClaimStatus: {
		REST:     []string{"/v1/barrier/officers/claim-status"},
		GRPC:     []string{"BarrierOfficerClaimStatus"},
		QRDBQUIC: []string{"barrier.OfficerClaimStatus"},
		MCP:      []string{"qrdb_barrier_officer_claim_status"},
	},
	// The path-param claim routes (/{username}/claim/begin|complete) are
	// covered by the /v1/barrier/officers/ prefix, shared by both verbs.
	VerbShareClaimBegin: {
		REST:     []string{"/v1/barrier/officers/"},
		GRPC:     []string{"BarrierShareClaimBegin"},
		QRDBQUIC: []string{"barrier.ShareClaimBegin"},
		MCP:      []string{"qrdb_barrier_share_claim_begin"},
	},
	VerbShareClaimComplete: {
		REST:     []string{"/v1/barrier/officers/"},
		GRPC:     []string{"BarrierShareClaimComplete"},
		QRDBQUIC: []string{"barrier.ShareClaimComplete"},
		MCP:      []string{"qrdb_barrier_share_claim_complete"},
	},

	VerbRecoveryOfficerRegister: {
		REST:     []string{"/v1/barrier/recovery/officers"},
		GRPC:     []string{"BarrierRecoveryOfficerRegister"},
		QRDBQUIC: []string{"barrier.RecoveryOfficerRegister"},
		MCP:      []string{"qrdb_barrier_recovery_officer_register"},
	},
	VerbRecoveryShareClaimBegin: {
		REST:     []string{"/v1/barrier/recovery/officers/"},
		GRPC:     []string{"BarrierRecoveryShareClaimBegin"},
		QRDBQUIC: []string{"barrier.RecoveryShareClaimBegin"},
		MCP:      []string{"qrdb_barrier_recovery_share_claim_begin"},
	},
	VerbRecoveryShareClaimComplete: {
		REST:     []string{"/v1/barrier/recovery/officers/"},
		GRPC:     []string{"BarrierRecoveryShareClaimComplete"},
		QRDBQUIC: []string{"barrier.RecoveryShareClaimComplete"},
		MCP:      []string{"qrdb_barrier_recovery_share_claim_complete"},
	},

	VerbCeremonyInitialize: {
		REST:     []string{"/v1/ceremony/initialize"},
		GRPC:     []string{"CeremonyInitialize"},
		QUIC:     []QUICOp{{Opcode: opCeremony, SubOp: subCeremonyInitialize}},
		QRDBQUIC: []string{"ceremony.Initialize"},
		MCP:      []string{"qrdb_ceremony_initialize"},
	},
	VerbCeremonyStatus: {
		REST:     []string{"/v1/ceremony/status"},
		GRPC:     []string{"CeremonyStatus"},
		QUIC:     []QUICOp{{Opcode: opCeremony, SubOp: subCeremonyStatus}},
		QRDBQUIC: []string{"ceremony.Status"},
		MCP:      []string{"qrdb_ceremony_status"},
	},
	VerbCeremonyClaimCertBegin: {
		REST:     []string{"/v1/ceremony/claim-cert/begin"},
		GRPC:     []string{"CeremonyClaimCertBegin"},
		QUIC:     []QUICOp{{Opcode: opCeremony, SubOp: subCeremonyClaimCertBegin}},
		QRDBQUIC: []string{"ceremony.ClaimCertBegin"},
		MCP:      []string{"qrdb_ceremony_claim_cert_begin"},
	},
	VerbCeremonyClaimCertComplete: {
		REST:     []string{"/v1/ceremony/claim-cert/complete"},
		GRPC:     []string{"CeremonyClaimCertComplete"},
		QUIC:     []QUICOp{{Opcode: opCeremony, SubOp: subCeremonyClaimCertComplete}},
		QRDBQUIC: []string{"ceremony.ClaimCertComplete"},
		MCP:      []string{"qrdb_ceremony_claim_cert_complete"},
	},
	// The officer Shamir-share claim handshake (qrdb's officer-share verbs):
	// begin issues the challenge nonce, complete verifies the signature and
	// releases the share exactly once. Mirrors qrdb's officer-share wire
	// names verbatim across every transport.
	VerbCeremonyOfficerShareBegin: {
		REST:     []string{"/v1/ceremony/officer-share/begin"},
		GRPC:     []string{"CeremonyBeginClaimShare"},
		QUIC:     []QUICOp{{Opcode: opCeremony, SubOp: subCeremonyBeginClaimShare}},
		QRDBQUIC: []string{"ceremony.BeginClaimShare"},
		MCP:      []string{"qrdb_ceremony_begin_claim_share"},
	},
	VerbCeremonyOfficerShareComplete: {
		REST:     []string{"/v1/ceremony/officer-share/complete"},
		GRPC:     []string{"CeremonyCompleteClaimShare"},
		QUIC:     []QUICOp{{Opcode: opCeremony, SubOp: subCeremonyCompleteClaimShare}},
		QRDBQUIC: []string{"ceremony.CompleteClaimShare"},
		MCP:      []string{"qrdb_ceremony_complete_claim_share"},
	},
	VerbCeremonySignCSR: {
		REST:     []string{"/v1/ceremony/sign-csr"},
		GRPC:     []string{"CeremonySignCSR"},
		QUIC:     []QUICOp{{Opcode: opCeremony, SubOp: subCeremonySignCSR}},
		QRDBQUIC: []string{"ceremony.SignCSR"},
		MCP:      []string{"qrdb_ceremony_sign_csr"},
	},
	VerbCeremonyClaimShareBegin: {
		REST:     []string{"/v1/ceremony/claim-share/begin"},
		GRPC:     []string{"CeremonyClaimShareBegin"},
		QRDBQUIC: []string{"ceremony.ClaimShareBegin"},
		MCP:      []string{"qrdb_ceremony_claim_share_begin"},
	},
	VerbCeremonyClaimShareEnd: {
		REST:     []string{"/v1/ceremony/claim-share/complete"},
		GRPC:     []string{"CeremonyClaimShareComplete"},
		QRDBQUIC: []string{"ceremony.ClaimShareComplete"},
		MCP:      []string{"qrdb_ceremony_claim_share_complete"},
	},
	VerbCeremonySubmitUnsealShare: {
		REST:     []string{"/v1/barrier/unseal-share"},
		GRPC:     []string{"CeremonySubmitUnsealShare"},
		QRDBQUIC: []string{"ceremony.SubmitUnsealShare"},
		MCP:      []string{"qrdb_ceremony_submit_unseal_share"},
	},
	// REST serves this verb at /v1/init/enroll-external-officer (mounted at
	// the quicbaas router root) plus the /api/v1/init/ mirror covered by the
	// RESTExtra prefix. quicbaas also carries it on its native QUIC opcode
	// surface and the qrdb-quic bridge.
	VerbCeremonyEnrollExtOfficer: {
		GRPC:     []string{"CeremonyEnrollExternalOfficer"},
		QUIC:     []QUICOp{{Opcode: opCeremony, SubOp: subCeremonyEnrollExternalOfficer}},
		QRDBQUIC: []string{"ceremony.EnrollExternalOfficer"},
	},
}

// RESTExtra lists REST-only allowlist entries that have no transport-neutral
// verb: the first-boot / bootstrap flows and the cluster peer key-transport
// receiver, all of which must operate in every daemon state. Entries ending
// in "/" are prefix matches. Mirrors qrdb's RESTExtra with the quicbaas
// /api/v1 mirror surfaces added.
var RESTExtra = []string{
	// qrdb-wire mounts at the quicbaas router root.
	"/v1/init/",
	"/v1/bootstrap/",
	// quicbaas /api/v1 mirrors: the init-ceremony surface lives under
	// /api/v1/init/, first-touch trust bootstrap under /api/v1/bootstrap/.
	"/api/v1/init/",
	"/api/v1/bootstrap/",
	// Cluster peer key-transport: a joining node fetches its wrapped cluster
	// seed here after enrollment. Must work regardless of sealed state so
	// follower replicas can receive material before unsealing; the handler
	// self-authorises via node cert + attestation.
	"/api/v1/cluster/secret",
}

// RESTAllowlist derives the REST sealed-window allowlist from Table and
// RESTExtra, deduplicating shared paths while preserving derivation order.
func RESTAllowlist() []string {
	seen := make(map[string]struct{})
	var out []string
	appendPath := func(p string) {
		if _, dup := seen[p]; dup {
			return
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	for _, v := range Verbs {
		for _, p := range Table[v].REST {
			appendPath(p)
		}
	}
	for _, p := range RESTExtra {
		appendPath(p)
	}
	return out
}

// GRPCAllowlist derives the gRPC sealed-window allowlist (method tails) from
// Table in derivation order, deduplicated. The tails are consumed by the
// qrdb-SDK-re-exported SealedWindowUnaryInterceptor / StreamInterceptor,
// which match the suffix after the last "/" in FullMethod — one tail
// therefore covers both the qrdb.v1.* registration and the quicbaas.*
// mirror service for the same verb.
func GRPCAllowlist() []string {
	seen := make(map[string]struct{})
	var out []string
	for _, v := range Verbs {
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

// QUICAllowlist derives the native quicbaas-QUIC sealed-window allowlist
// (opcode + sub-op pairs) from Table.
func QUICAllowlist() map[QUICOp]struct{} {
	out := make(map[QUICOp]struct{})
	for _, v := range Verbs {
		for _, op := range Table[v].QUIC {
			out[op] = struct{}{}
		}
	}
	return out
}

// QRDBQUICAllowlist derives the qrdb-quic ALPN bridge sealed-window
// allowlist (method names) from Table.
func QRDBQUICAllowlist() map[string]struct{} {
	out := make(map[string]struct{})
	for _, v := range Verbs {
		for _, m := range Table[v].QRDBQUIC {
			out[m] = struct{}{}
		}
	}
	return out
}

// MCPAllowlist derives the MCP tool-name sealed-window allowlist from Table.
func MCPAllowlist() map[string]struct{} {
	out := make(map[string]struct{})
	for _, v := range Verbs {
		for _, m := range Table[v].MCP {
			out[m] = struct{}{}
		}
	}
	return out
}
