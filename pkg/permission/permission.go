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

// Package permission declares the permissions QuicBaaS contributes to the
// single qrdb RBAC vocabulary, the built-in roles that hold them by default,
// the separated duty each belongs to, and registers all three in qrdb's global
// registries.
//
// go-qrdb owns the policy decision point and the canonical "resource:action"
// permission vocabulary, but it deliberately does not know about higher-layer
// domain concepts. It exposes the extension points for exactly that:
// qrdbsdk.RegisterPermissions (the vocabulary),
// qrdbsdk.RegisterPermissionRoleGrant (the default role placement) and
// qrdbsdk.RegisterPermissionDuty (the FIPS 140-3 AS10.03 separated duty). This
// package is the ONE place QuicBaaS uses them, so there is exactly one list of
// broker contributions rather than scattered init() calls.
//
// Why registration matters: a permission that is not in qrdb's registry cannot
// be held by any role. go-qrdb's role store validates every permission on role
// create/update (pkg/role.validatePermissions) and rejects an unregistered one
// with InvalidPermissionError. A transport authz table that maps an endpoint
// onto an unregistered permission therefore ships a DEAD endpoint that merely
// looks wired: the PEP resolves a permission no token can ever carry. The
// per-transport guard tests (see Register below) make that a build failure.
//
// Why the role grant matters: registration alone only makes a permission
// grantable. A registered permission with no default role grant lands in none
// of qrdb's five built-in roles and is reachable only through a hand-authored
// custom role — a deployment-time workaround for a declaration-time omission.
// Role placement is metadata carried by the permission (qrdb's
// pkg/permission.RegisterRoleGrant), and pkg/role.BuiltInRoles is a live
// projection of that registry evaluated at role-seed time, so a grant declared
// here reaches the seeded roles as long as it is registered first. It is: the
// production call site is server.NewServer, and role seeding happens strictly
// downstream of it, inside the embedded qrdb node's bootstrap / first-boot.
//
// Why the duty matters: FIPS 140-3 AS10.03 separates module GOVERNANCE (the
// Security Officer's own duties) from cryptographic USE, and qrdb decides
// Security-Officer grantability on the duty each permission's AUTHOR declares
// (qrdb's pkg/permission.RegisterDuty). qrdb also applies an unoverridable
// backstop over unambiguously cryptographic action verbs — sign, encrypt,
// decrypt, wrap, unwrap, derive, kdf, mac, hmac, attest, quote — in EVERY
// resource family, including one a higher layer contributed. Declaring the duty
// here anyway means the classification is stated by the author rather than
// inferred from a verb's spelling, and it is stated in the same literal as the
// permission and its roles. Omission is fail-safe in qrdb (an undeclared
// permission is not SO-grantable), but an omission at THIS layer must still be
// deliberate, so a contribution distinguishes "outside the Security Officer's
// duties" from "nobody has decided yet" — see the duty type below.
//
// Scope: every permission declared here takes qrdb's default ScopeSelf — the
// permission acts on the identity's own tenant, and the system-tenant rule
// lifts it across tenants for a system-tenant identity. qrdb re-exports
// RegisterPermissionScope, so ScopeCross and ScopeTenantOnly are reachable
// from this layer, but neither is right for attestation: ScopeTenantOnly would
// deny the system tenant the host's own attestation surface, and ScopeCross
// would deny it to every tenant-local identity. ScopeSelf plus the
// system-tenant rule already yields both. Permissions absent from qrdb's scope
// registry resolve to ScopeSelf (permission.LookupScope), so the default is
// taken by omission rather than by a no-op registration call.
package permission

import (
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// Attestation permissions.
//
// Attestation has its own resource vocabulary rather than borrowing certs:* or
// trust:*: the verbs answer "is this host and this workload in the state we
// approved?", and a principal allowed to read the certificate store has no
// business driving a TPM quote or reading a host's PCR values, which
// fingerprint its boot state.
//
// The read/write split below is the one all five transports already encode
// (pkg/api/grpc/authz_mapping.go, pkg/api/rest/authz_pep_mapping.go,
// pkg/api/quic/handler_authz_qrdb.go, pkg/api/mcp/authz_qrdb.go).
const (
	// AttestationVerify ("attestation:verify") authorizes verification of an
	// attestation statement or of an attestation certificate chain. Both are
	// pure computations over caller-supplied material: they mutate nothing and
	// touch no hardware, so this is the read-strength verb of the family.
	AttestationVerify qrdbsdk.Permission = "attestation:verify"

	// AttestationAttest ("attestation:attest") authorizes driving the
	// registered attestor — extending a PCR and asking the host TPM for a
	// signed quote over the resulting boot state. It is a host-level mutation
	// and the write-strength verb of the family; it must never be conflated
	// with AttestationVerify.
	AttestationAttest qrdbsdk.Permission = "attestation:attest"

	// AttestationProvision ("attestation:provision") authorizes managing the
	// Verifier's trust: storing and deleting endorsements (the anchors that
	// attestation-key certificates and signed reference values must chain or
	// verify to) and the signed reference values appraisal compares against.
	// It decides what every later appraisal will accept, so it is the
	// administrative verb of the family. Reading those records and appraising
	// evidence against them carry AttestationVerify.
	AttestationProvision qrdbsdk.Permission = "attestation:provision"

	// AuditRecord ("audit_events:record") authorizes emitting (writing) a
	// new event onto the audit chain — the remote-write counterpart to
	// go-qrdb's own audit:read / audit:query (pkg/permission.AuditRead /
	// AuditQuery in go-qrdb, the read-strength verbs of go-qrdb's OWN
	// "audit" resource family). go-qrdb owns "audit" outright (it ships no
	// write verb of its own — the chain is written in-process by the node's
	// own subsystems, never over the wire) and
	// TestAll_DoesNotShadowAQRDBPermission forbids QuicBaaS from adding any
	// action to a resource family qrdb already owns (the single-PDP,
	// no-forked-vocabulary rule), so this deliberately lives in its own
	// "audit_events" resource — adjacent and discoverable, never a
	// same-named extension of qrdb's family — for the verb a REMOTE caller
	// (e.g. a downstream platform composing this IAM plane) needs to record
	// an event via RecordAuditEvent (api/proto/iam/iam.proto).
	AuditRecord qrdbsdk.Permission = "audit_events:record"

	// The SDLC module's verbs. A binding attaches a forge repository and
	// its review policy to an orchestration Project; the module then holds
	// forge credentials and drives autonomous work against that repository,
	// so binding and unbinding are administrative configuration changes —
	// Admin only. Reading a binding or a task's delivery is a read verb
	// Operator holds too, mirroring the read/write split every other family
	// here takes. Readmitting or blocking a task is orchestrator:*.
	SDLCRead   qrdbsdk.Permission = "sdlc:read"
	SDLCBind   qrdbsdk.Permission = "sdlc:bind"
	SDLCUnbind qrdbsdk.Permission = "sdlc:unbind"

	// SDLCIssueCreate ("sdlc:issue_create") authorizes filing an issue on a
	// bound project's forge through the binding's own token — how the lead
	// splits an issue into follow-ups. It is an operator verb, not an
	// administrative one: it changes no binding and no policy, and every
	// filing is audited.
	SDLCIssueCreate qrdbsdk.Permission = "sdlc:issue_create"

	// AgentsRead and AgentsWrite govern a tenant's AI agent catalog: the named
	// Agents (runtime + model + provider) every role slot references, and the
	// ordered pools a slot fails over along. Reading is Admin and Operator;
	// changing an Agent changes what every slot bound to it runs and what it
	// costs, so it is Admin alone. The family is not "orchestrator", which
	// go-qrdb owns outright.
	AgentsRead  qrdbsdk.Permission = "agents:read"
	AgentsWrite qrdbsdk.Permission = "agents:write"

	// The prompt store (pkg/orchestration/prompts): a tenant's own and its
	// projects' edits to the prompt sets every factory job resolves at launch
	// (go-quicbaas#804). Reading and writing are Admin and Operator — tuning
	// what an agent is told is day-to-day operation, and the store refuses a
	// body that would break the engine's parsing. History is also the
	// Auditor's: who changed which words, and when, is oversight evidence.
	// The system set is written only by the boot reconciler, never by a verb.
	PromptsRead    qrdbsdk.Permission = "prompts:read"
	PromptsWrite   qrdbsdk.Permission = "prompts:write"
	PromptsHistory qrdbsdk.Permission = "prompts:history"

	// CostSettingsWrite ("cost_settings:write") authorizes REPLACING a
	// tenant's runtime cost settings: the electricity rate and per-engine
	// device draws its local inference is costed from (go-quicbaas#1058).
	// Reading them is orchestrator:status, like the spend report. It is its
	// own family because go-qrdb owns the "orchestrator" family outright
	// (TestAll_DoesNotShadowAQRDBPermission).
	CostSettingsWrite qrdbsdk.Permission = "cost_settings:write"

	// The Software Factory worker image refresh (pkg/sdlc/workerimage). It
	// is a HOST operation, not a tenant one: a pass builds on the node's own
	// container engine and rebinds the managed image in EVERY tenant's
	// bindings, so both verbs are cross-tenant — held only by the system
	// tenant's operators. Reading the status is Admin and Operator; asking
	// for a pass, which builds, smokes on a pooled credential and rebinds
	// projects, is Admin alone.
	WorkerImageRead    qrdbsdk.Permission = "worker_images:read"
	WorkerImageRefresh qrdbsdk.Permission = "worker_images:refresh"

	// AgentAccountMaterialize ("agent_accounts:materialize") authorizes
	// RELEASING one pooled provider account's credential envelope to the
	// caller — the single verb in this stack whose response body is secret
	// material. An enrolled fleet node daemon holds it so it can resolve a pooled
	// account on the worker host that actually runs the agent, which is where
	// the credential has to be (go-quicbaas#734).
	//
	// It is deliberately NOT an action on go-qrdb's "orchestrator" or
	// "credentials" families, for two separate reasons:
	//
	//   - go-qrdb owns both families outright, and
	//     TestAll_DoesNotShadowAQRDBPermission forbids QuicBaaS from adding an
	//     action to a family qrdb already owns (the single-PDP, no-forked-
	//     vocabulary rule). "audit_events:record" lives in its own family for
	//     exactly this reason; so does this.
	//   - credentials:* is registered ScopeCross — system tenant only — because
	//     it guards qrdb's un-namespaced named-credential store. A daemon is a
	//     TENANT identity, so a credentials:* grant would be denied by the RBAC
	//     cross-scope rule on every call. This permission takes qrdb's default
	//     ScopeSelf, which is the daemon's own tenant and nothing else.
	//
	// It is a separate verb from the rest of the account surface, not a reuse
	// of the read verb: an identity that may LIST the pool (metadata) must not
	// thereby be able to extract every login in it.
	AgentAccountMaterialize qrdbsdk.Permission = "agent_accounts:materialize"

	// SpendPolicyWrite ("spend_policy:write") authorizes REPLACING a tenant's
	// own spend limits: the sliding token and dollar windows over every turn
	// charged to the tenant, which defer the tenant's new work at admission
	// once spent (go-quicbaas#1058). Reading them is orchestrator:status,
	// like the spend report. It is its own family because go-qrdb owns the
	// "orchestrator" family outright (TestAll_DoesNotShadowAQRDBPermission).
	SpendPolicyWrite qrdbsdk.Permission = "spend_policy:write"

	// AgentAccountRead ("agent_accounts:read") and AgentAccountList
	// ("agent_accounts:list") authorize the METADATA half of the pooled
	// provider account surface: one account's non-secret record, its usage
	// report, and the tenant's pool listing. No response they guard carries
	// credential material.
	//
	// They exist because the reads they replaced were unreachable by the
	// identity that needs them most. The pooled-account surface was mapped onto
	// go-qrdb's credentials:* family, which is registered ScopeCross — SYSTEM
	// TENANT ONLY — because qrdb's named-credential store has no tenant
	// namespacing at all (go-qrdb pkg/permission credentialPermissions). Pooled
	// accounts are the opposite: every record is written into the requesting
	// tenant's own orchestration keyspace. So the RBAC cross-scope rule denied
	// every tenant identity a surface that is tenant data, and an enrolled
	// fleet node daemon — a tenant principal by construction — could not list the
	// pool it was supposed to draw from (go-quicbaas#734).
	//
	// This is the same correction go-quicbaas#534 made for /api/v1/secrets, for
	// the same reason and with the same shape: a tenant-scoped surface takes a
	// tenant-scoped permission family rather than borrowing a system-scoped
	// one. See docs/agent-orchestration/accounts.md.
	//
	// The WRITE verbs of the same surface (create / update / delete / rotate /
	// revoke / set-default) deliberately still take credentials:*, and so are
	// still system-tenant operations. Widening who may mint or rotate a pooled
	// login is a policy change in its own right; it is filed separately rather
	// than smuggled in behind a read fix.
	AgentAccountRead qrdbsdk.Permission = "agent_accounts:read"
	AgentAccountList qrdbsdk.Permission = "agent_accounts:list"

	// The scheduler's workload placement verbs (pkg/scheduler/placement).
	// go-qrdb owns the "scheduler" resource family outright — every job verb
	// is its permission, not ours — and TestAll_DoesNotShadowAQRDBPermission
	// forbids extending a family qrdb owns, so placement contributes its own
	// "placement" family beside it.
	//
	// PlacementRead ("placement:read") reads nodes and placements.
	PlacementRead qrdbsdk.Permission = "placement:read"

	// PlacementPlace ("placement:place") places and releases workloads: it
	// consumes node capacity but changes no node.
	PlacementPlace qrdbsdk.Permission = "placement:place"

	// PlacementManage ("placement:manage") registers, renews and
	// deregisters node facts and cordons, uncordons and drains nodes: it
	// changes the pool every placement decides over.
	PlacementManage qrdbsdk.Permission = "placement:manage"
)

// Service registry permissions.
//
// The registry is its own resource family. It is not qrdb's kv:*, lease:* or
// watch:*: a caller that may register an endpoint must not thereby gain the
// raw store verbs the registry is built on (and through them every other
// record of its tenant), and qrdb owns those families outright
// (TestAll_DoesNotShadowAQRDBPermission). The read/write split is the one all
// transports encode (pkg/api/grpc/authz_mapping.go, the REST frozen route
// table, pkg/api/mcp/authz_qrdb.go).
const (
	// ServiceRegistryRegister ("service_registry:register") authorizes
	// listing an instance under a lease and renewing or revoking the leases
	// of the caller tenant's own registrations. It is the write verb of the
	// family.
	ServiceRegistryRegister qrdbsdk.Permission = "service_registry:register"

	// ServiceRegistryRead ("service_registry:read") authorizes resolving a
	// service's instances and watching its changes. It mutates nothing.
	ServiceRegistryRead qrdbsdk.Permission = "service_registry:read"
)

// duty states which of FIPS 140-3 AS10.03's separated duties a contributed
// permission belongs to. It is a distinct type from qrdbsdk.PermissionDuty for
// one reason: qrdb's zero value, PermissionDutyUndeclared, is a legitimate
// OUTCOME ("no part of the Security Officer's duties"), so a bare
// qrdbsdk.PermissionDuty field could not tell an author who decided that apart
// from an author who decided nothing. Here the zero value is dutyUnstated,
// which is never a valid contribution, so a missing duty fails
// TestContributions_DeclareARolePlacementAndADuty exactly as an empty roles
// list does.
type duty int

const (
	// dutyUnstated is the zero value and is never valid: nobody has said which
	// separated duty the permission belongs to. It exists only so that the
	// omission is detectable.
	dutyUnstated duty = iota

	// dutyOutsideSecurityOfficer states that the permission is no part of the
	// Security Officer's duties AND performs no cryptographic primitive, so
	// nothing is declared to qrdb. That is both the accurate and the fail-safe
	// statement: qrdb treats an undeclared permission as not SO-grantable
	// (RegisterRoleGrant panics with UndeclaredDutyError), whereas declaring
	// PermissionDutyGovernance would widen the permission into something the
	// Security Officer may hold. qrdb's RegisterDuty deliberately rejects
	// PermissionDutyUndeclared as a declarable value — "undeclared" is
	// expressed by omitting the call — which is why this maps to no call at
	// all.
	dutyOutsideSecurityOfficer

	// dutyGovernance maps to qrdbsdk.PermissionDutyGovernance: module and
	// security governance, the only classification qrdb admits onto the
	// Security Officer role.
	dutyGovernance

	// dutyCryptoUse maps to qrdbsdk.PermissionDutyCryptoUse: performance of a
	// cryptographic primitive with key material, barred from the Security
	// Officer role at registration time and again by qrdb's runtime RBAC
	// deny-list.
	dutyCryptoUse
)

// qrdbDuties maps the duties that have a qrdb declaration onto it. A duty
// absent from this map produces no RegisterPermissionDuty call: dutyUnstated
// because there is nothing to say (a declaration bug the guard test catches),
// dutyOutsideSecurityOfficer because in qrdb the statement IS the omission.
var qrdbDuties = map[duty]qrdbsdk.PermissionDuty{
	dutyGovernance: qrdbsdk.PermissionDutyGovernance,
	dutyCryptoUse:  qrdbsdk.PermissionDutyCryptoUse,
}

// scope states a contributed permission's tenancy reach. It is a distinct
// type from qrdbsdk.PermissionScope for the same reason duty is distinct from
// qrdbsdk.PermissionDuty: qrdb's zero value is PermissionScopeSelf, a
// legitimate DECISION, so a bare qrdbsdk.PermissionScope field cannot tell an
// author who chose self-scope apart from an author who filled in nothing. The
// difference matters here more than anywhere else in this file, because
// go-qrdb#504 made an undeclared scope a runtime DENY on every call: the
// omission that reads as "self" in the source would read as "refused" in
// production.
type scope int

const (
	// scopeUnstated is the zero value and is never valid: nobody has said how
	// far the permission reaches. It exists only so that the omission is
	// detectable, and Register skips a contribution carrying it rather than
	// declaring a reach its author did not choose —
	// TestEveryContributedPermissionStatesItsScope fails the build first.
	scopeUnstated scope = iota

	// scopeSelf maps to qrdbsdk.PermissionScopeSelf: the permission acts on
	// the identity's own tenant, lifted across tenants for a system-tenant
	// identity. It is the reach every permission below already had before
	// go-qrdb#504 removed the implicit default.
	//
	// qrdb defines two further reaches — tenant-only and self-strict. Neither
	// is listed here because no contribution needs one yet; the entry and its
	// qrdbScopes row are added together when one does.
	scopeSelf

	// scopeCross maps to qrdbsdk.PermissionScopeCross: the permission acts
	// across tenants and is exercisable only by a system-tenant identity. The
	// worker image refresh is the first contribution that needs it — one
	// pass rebinds bindings in every tenant.
	scopeCross
)

// qrdbScopes maps this package's scope statements onto qrdb's. A scope absent
// from the map produces no RegisterPermissionScope call, which leaves the
// permission undeclared and therefore denied — the fail-closed half of the
// pair with the guard test.
var qrdbScopes = map[scope]qrdbsdk.PermissionScope{
	scopeSelf:  qrdbsdk.PermissionScopeSelf,
	scopeCross: qrdbsdk.PermissionScopeCross,
}

// contribution pairs one contributed permission with the built-in roles that
// hold it by default and with the separated duty it belongs to. Keeping the
// three together makes the triple the unit of declaration: a permission cannot
// be added to the vocabulary without the author stating, in the same literal,
// which built-in roles receive it and which side of the FIPS 140-3 AS10.03
// duty boundary it falls on.
type contribution struct {
	permission qrdbsdk.Permission
	roles      []string
	duty       duty

	// scope is the permission's tenancy reach. go-qrdb#504 removed the
	// implicit default: a contributed permission with no declared scope is
	// REFUSED by the authorizer on every call ("no scope declared for
	// <name>"), not admitted as self-scope. Every entry below therefore
	// carries one explicitly, which is what go-qrdb's RegisterPermissionScope
	// asks for — "declare the scope you mean, even when it is
	// PermissionScopeSelf".
	scope scope
}

// contributions is the single source of truth for everything QuicBaaS adds to
// qrdb's RBAC model, in a stable order. Register writes exactly this list, All
// projects the permissions out of it, and the per-transport guard tests diff
// their authz mapping tables against qrdbsdk.AllPermissions() after calling
// Register.
//
// Role placement follows qrdb's own built-in sets, which are FLAT — Admin does
// not inherit Operator's grants (pkg/role.BuiltInRoles materializes five
// independent projections of the grant registry), so every role that should
// hold a permission is listed explicitly.
//
// Grants are always concrete permissions, never an "attestation:*"
// whole-segment wildcard: a wildcard would collapse the read/write split that
// all four transport authz tables carefully encode, handing anyone who may
// verify a caller-supplied statement the ability to drive the host TPM.
var contributions = []contribution{
	{
		// attestation:verify is a pure computation over caller-supplied
		// material — the same strength tier as keys:verify, which Operator
		// already holds — and it is the day-to-day verb a workload or CI
		// identity uses to check a peer's statement. Admin is listed too
		// because it holds attestation:attest: an identity that may request a
		// quote but not verify the resulting statement is incoherent, and Admin
		// holds the read verb of every other family it can write (certs:read
		// beside certs:generate, trust:read beside trust:write). qrdb's roles
		// are flat, so Admin needs the explicit grant.
		permission: AttestationVerify,
		// Self-scope preserves the reach this permission already had: every
		// consumer-registered permission was implicitly ScopeSelf before
		// go-qrdb#504. Tightening any of these is a deliberate change for
		// this repository, not a side effect of declaring what was already true.
		scope: scopeSelf,
		roles: []string{
			qrdbsdk.PermissionRoleAdmin,
			qrdbsdk.PermissionRoleOperator,
		},
		// Neither governance nor cryptographic use. It touches no key at all:
		// the verification runs over a caller-supplied statement or chain,
		// which is why qrdb excludes the "verify" verb from its cryptographic
		// backstop (keys:verify is barred by an explicit DutyCryptoUse
		// declaration, because THERE the key is the module's). It is also no
		// part of the Security Officer's module-governance duties — it is a
		// data-plane read verb held by Admin and Operator — so declaring it
		// governance would overstate it and make it SO-grantable. Leaving it
		// undeclared in qrdb is therefore both the accurate and the fail-safe
		// choice, and it is pinned by
		// TestRegister_KeepsVerifyOutsideTheSecurityOfficerDuties.
		duty: dutyOutsideSecurityOfficer,
	},
	{
		// attestation:attest extends a PCR on the HOST TPM and asks for a
		// signed quote — a host-level mutation that fingerprints platform boot
		// state, sitting beside Admin's certs:generate and trust:write.
		//
		// Deliberately granted to no other built-in role:
		//   - Operator / User: both hold read-strength attestation only; a
		//     routine data-plane identity must not reshape host PCR state.
		//   - Auditor: read-only STATE visibility (FIPS 140-3 AS10.10). A quote
		//     request mutates the TPM, so it is not auditor-shaped even though
		//     its result is evidence.
		//   - SO: a quote is a signing operation, and the Security Officer is
		//     barred from cryptographic operations under FIPS 140-3 AS10.03.
		//     qrdb enforces that structurally for THIS permission, not just for
		//     the keys:* family: RegisterRoleGrant panics with
		//     SoDViolationError for any permission whose action names an
		//     unambiguous cryptographic primitive in any resource family, and
		//     "attest" is one of them, so an SO grant added to the list below
		//     would fail at registration. The DutyCryptoUse declaration states
		//     the same classification explicitly instead of leaning on the
		//     verb, and qrdb's runtime RBAC deny-list rejects the operation a
		//     third time at authorization time. This declaration and
		//     TestRegister_WithholdsAttestFromReadOnlyAndSORoles therefore pin
		//     an outcome qrdb already guarantees rather than being its only
		//     guarantee — which is what the User and Auditor exclusions above
		//     genuinely are.
		permission: AttestationAttest,
		// Self-scope preserves the reach this permission already had: every
		// consumer-registered permission was implicitly ScopeSelf before
		// go-qrdb#504. Tightening any of these is a deliberate change for
		// this repository, not a side effect of declaring what was already true.
		scope: scopeSelf,
		roles: []string{
			qrdbsdk.PermissionRoleAdmin,
		},
		duty: dutyCryptoUse,
	},
	{
		// attestation:provision decides what the Verifier trusts. It is held
		// by Admin alone: an Operator appraises evidence (attestation:verify)
		// but must not be able to install the anchors or baselines that
		// evidence is judged against, or it could approve its own hosts.
		// Not granted to the Security Officer: the duty is declared outside
		// the SO's module-governance set, deliberately, so that installing
		// attestation trust stays with the platform administrator rather than
		// the role that holds the module's own keys and barrier.
		permission: AttestationProvision,
		scope:      scopeSelf,
		roles:      []string{qrdbsdk.PermissionRoleAdmin},
		duty:       dutyOutsideSecurityOfficer,
	},
	{
		// audit:record is granted to Admin and Operator — the day-to-day
		// service identities that emit their own operational events (the
		// scenario RecordAuditEvent exists for: a downstream platform's
		// service identity recording into this IAM plane's chain), mirroring
		// go-qrdb's own AuditRead/AuditQuery role placement on Admin and
		// Operator for the read side of the same resource family.
		//
		// Deliberately NOT granted to Auditor: FIPS 140-3 AS10.10 gives the
		// Auditor read-only STATE visibility (the same reasoning
		// AttestationAttest's comment above states for withholding a write
		// verb from Auditor); a role whose entire purpose is independent
		// oversight must never be able to add entries to the record it
		// oversees. Not granted to SO either — recording an application-level
		// event is an operational duty, not module governance, so it falls
		// outside the Security Officer's role by the same reasoning as
		// AttestationVerify below.
		permission: AuditRecord,
		// Self-scope preserves the reach this permission already had: every
		// consumer-registered permission was implicitly ScopeSelf before
		// go-qrdb#504. Tightening any of these is a deliberate change for
		// this repository, not a side effect of declaring what was already true.
		scope: scopeSelf,
		roles: []string{
			qrdbsdk.PermissionRoleAdmin,
			qrdbsdk.PermissionRoleOperator,
		},
		// Neither governance nor cryptographic use: recording an audit event
		// touches no key material and does not deconfigure module
		// state — it is a plain data-plane write, the audit-resource
		// equivalent of AttestationVerify's read-strength "outside the
		// Security Officer's duties" classification.
		duty: dutyOutsideSecurityOfficer,
	},
	// SDLC bindings. Reads to Admin and Operator; bind and unbind to Admin
	// alone — a change to a project that holds forge credentials and drives
	// autonomous work is administrative, the same reasoning that keeps
	// attestation:attest off Operator. None of it touches key material or
	// module governance, so all three fall outside the Security Officer's
	// duties.
	{permission: SDLCRead, roles: []string{qrdbsdk.PermissionRoleAdmin, qrdbsdk.PermissionRoleOperator}, duty: dutyOutsideSecurityOfficer, scope: scopeSelf},
	{permission: SDLCBind, roles: []string{qrdbsdk.PermissionRoleAdmin}, duty: dutyOutsideSecurityOfficer, scope: scopeSelf},
	{permission: SDLCUnbind, roles: []string{qrdbsdk.PermissionRoleAdmin}, duty: dutyOutsideSecurityOfficer, scope: scopeSelf},
	// Filing an issue is day-to-day operation — the lead splitting work — so
	// Operator holds it beside Admin. It writes to the forge, never to key
	// material or module governance.
	{permission: SDLCIssueCreate, roles: []string{qrdbsdk.PermissionRoleAdmin, qrdbsdk.PermissionRoleOperator}, duty: dutyOutsideSecurityOfficer, scope: scopeSelf},
	// cost_settings:write changes what every local turn of a tenant is
	// costed at, so it is administrative: Admin alone among the built-ins.
	// It touches no key material and no module governance.
	{permission: CostSettingsWrite, roles: []string{qrdbsdk.PermissionRoleAdmin}, duty: dutyOutsideSecurityOfficer, scope: scopeSelf},
	// The AI agent catalog. Reads to Admin and Operator, writes to Admin: an
	// Agent decides what every slot bound to it runs. No key material, no
	// module governance.
	{permission: AgentsRead, roles: []string{qrdbsdk.PermissionRoleAdmin, qrdbsdk.PermissionRoleOperator}, duty: dutyOutsideSecurityOfficer, scope: scopeSelf},
	{permission: AgentsWrite, roles: []string{qrdbsdk.PermissionRoleAdmin}, duty: dutyOutsideSecurityOfficer, scope: scopeSelf},
	// The prompt store. Tenant-scoped: a tenant edits its own prompts and
	// never another's. Revision history is oversight evidence, so the Auditor
	// reads it beside Admin and Operator. No key material, no module
	// governance.
	{permission: PromptsRead, roles: []string{qrdbsdk.PermissionRoleAdmin, qrdbsdk.PermissionRoleOperator}, duty: dutyOutsideSecurityOfficer, scope: scopeSelf},
	{permission: PromptsWrite, roles: []string{qrdbsdk.PermissionRoleAdmin, qrdbsdk.PermissionRoleOperator}, duty: dutyOutsideSecurityOfficer, scope: scopeSelf},
	{permission: PromptsHistory, roles: []string{qrdbsdk.PermissionRoleAdmin, qrdbsdk.PermissionRoleOperator, qrdbsdk.PermissionRoleAuditor}, duty: dutyOutsideSecurityOfficer, scope: scopeSelf},
	// The worker image refresh. Cross-tenant, so only a system-tenant identity
	// holding the role exercises either. Neither touches key material or
	// module governance: outside the Security Officer's duties.
	{permission: WorkerImageRead, roles: []string{qrdbsdk.PermissionRoleAdmin, qrdbsdk.PermissionRoleOperator}, duty: dutyOutsideSecurityOfficer, scope: scopeCross},
	{permission: WorkerImageRefresh, roles: []string{qrdbsdk.PermissionRoleAdmin}, duty: dutyOutsideSecurityOfficer, scope: scopeCross},
	{
		// agent_accounts:materialize releases a pooled login's plaintext
		// credential envelope. Admin alone among the built-ins, by the same
		// reasoning that keeps attestation:attest and sdlc:bind off Operator:
		// this is the strongest verb of its family, and Operator already holds
		// every read it needs (the metadata surface) without it.
		//
		// The identity that actually exercises it in production is not a
		// built-in role at all — it is the tenant-scoped "agent-runtime" role
		// an enrolled daemon holds (iam.FleetRuntimeGrant), which names this
		// permission explicitly. Built-in placement therefore only decides
		// which HUMAN role may release a credential by hand.
		//
		// Neither governance nor cryptographic use: releasing a stored envelope
		// deconfigures no module state and performs no primitive with the
		// module's own key material, so it falls outside the Security Officer's
		// duties exactly as audit_events:record does.
		permission: AgentAccountMaterialize,
		// Self-scope preserves the reach this permission already had: every
		// consumer-registered permission was implicitly ScopeSelf before
		// go-qrdb#504. Tightening any of these is a deliberate change for
		// this repository, not a side effect of declaring what was already true.
		scope: scopeSelf,
		roles: []string{qrdbsdk.PermissionRoleAdmin},
		duty:  dutyOutsideSecurityOfficer,
	},
	// spend_policy:write caps a whole tenant's consumption, so it is
	// administrative, like sdlc:bind: Admin alone among the built-ins. It
	// touches no key material and no module governance.
	{permission: SpendPolicyWrite, roles: []string{qrdbsdk.PermissionRoleAdmin}, duty: dutyOutsideSecurityOfficer, scope: scopeSelf},
	// The metadata reads follow the read/write split every family here takes:
	// Admin and Operator both hold them, because listing the pool and reading
	// an account's health is the day-to-day operational question, and neither
	// answer carries credential material. The Security Officer holds neither —
	// a pool listing is operational data, not module governance.
	{
		permission: AgentAccountRead,
		// Self-scope preserves the reach this permission already had: every
		// consumer-registered permission was implicitly ScopeSelf before
		// go-qrdb#504. Tightening any of these is a deliberate change for
		// this repository, not a side effect of declaring what was already true.
		scope: scopeSelf,
		roles: []string{qrdbsdk.PermissionRoleAdmin, qrdbsdk.PermissionRoleOperator},
		duty:  dutyOutsideSecurityOfficer,
	},
	{
		// service_registry:register is a data-plane write confined to the
		// caller tenant's own registrations (the registry proves lease
		// ownership per tenant), the same tier as the KV and lease writes
		// qrdb grants Operator. Admin holds it too because qrdb's roles are
		// flat. User is withheld for the same reason qrdb withholds kv:put
		// and lease:grant from it: a workload that publishes endpoints is an
		// operational identity, not an end user.
		permission: ServiceRegistryRegister,
		scope:      scopeSelf,
		roles:      []string{qrdbsdk.PermissionRoleAdmin, qrdbsdk.PermissionRoleOperator},
		duty:       dutyOutsideSecurityOfficer,
	},
	{
		// service_registry:read only reveals which endpoints the caller
		// tenant has published. Auditor holds it as read-only state
		// visibility (FIPS 140-3 AS10.10), beside Admin and Operator. It is
		// no part of the Security Officer's governance duties and touches no
		// key, so nothing is declared to qrdb.
		permission: ServiceRegistryRead,
		scope:      scopeSelf,
		roles: []string{
			qrdbsdk.PermissionRoleAdmin,
			qrdbsdk.PermissionRoleOperator,
			qrdbsdk.PermissionRoleAuditor,
		},
		duty: dutyOutsideSecurityOfficer,
	},
	{
		permission: AgentAccountList,
		// Self-scope preserves the reach this permission already had: every
		// consumer-registered permission was implicitly ScopeSelf before
		// go-qrdb#504. Tightening any of these is a deliberate change for
		// this repository, not a side effect of declaring what was already true.
		scope: scopeSelf,
		roles: []string{qrdbsdk.PermissionRoleAdmin, qrdbsdk.PermissionRoleOperator},
		duty:  dutyOutsideSecurityOfficer,
	},
	// Workload placement follows the read/write split every family here
	// takes. Reading the pool and placing or releasing a workload are the
	// day-to-day operational verbs, held by Admin and Operator. Changing the
	// pool itself — registering node facts, cordoning, draining — reshapes
	// every later decision, so it is administrative and Admin's alone among
	// the built-ins; the node agents that register and heartbeat their own
	// facts in production hold a tenant-scoped custom role naming
	// placement:manage. Self-scope: every record is tenant-scoped, and the
	// system-tenant rule lifts it for a system identity. None of the three
	// touches key material or module governance, so all fall outside the
	// Security Officer's duties.
	{permission: PlacementRead, roles: []string{qrdbsdk.PermissionRoleAdmin, qrdbsdk.PermissionRoleOperator}, duty: dutyOutsideSecurityOfficer, scope: scopeSelf},
	{permission: PlacementPlace, roles: []string{qrdbsdk.PermissionRoleAdmin, qrdbsdk.PermissionRoleOperator}, duty: dutyOutsideSecurityOfficer, scope: scopeSelf},
	{permission: PlacementManage, roles: []string{qrdbsdk.PermissionRoleAdmin}, duty: dutyOutsideSecurityOfficer, scope: scopeSelf},
}

// All returns every permission QuicBaaS contributes to qrdb's registry, in a
// stable order.
func All() []qrdbsdk.Permission {
	perms := make([]qrdbsdk.Permission, 0, len(contributions))
	for _, c := range contributions {
		perms = append(perms, c.permission)
	}
	return perms
}

// Register adds every QuicBaaS-contributed permission to qrdb's process-global
// permission registry, declares its separated duty, and records its default
// built-in role placement. It is idempotent (the registrations are map inserts
// and a duty re-declared identically is a no-op) and safe for concurrent use,
// so tests may call it freely.
//
// Ordering: permissions are registered and their duties declared before their
// grants, so that a grant never names a permission the registry has not yet
// admitted and the Security-Officer guard always sees the author's
// classification rather than a not-yet-declared one. The whole call happens
// before any role is seeded.
//
// Production call site: server.NewServer (pkg/server/server.go), the broker's
// composition root. Registering there guarantees the vocabulary, its duty and
// its role placement exist before any role is seeded, any role create/update is
// validated, and any transport PEP resolves a permission.
func Register() {
	qrdbsdk.RegisterPermissions(All()...)
	// Scope before duty and grants: the authorizer resolves a permission's
	// reach on every call, so a grant that lands before its scope would be
	// refused in the window between.
	for _, c := range contributions {
		if qrdbScope, stated := qrdbScopes[c.scope]; stated {
			qrdbsdk.RegisterPermissionScope(qrdbScope, c.permission)
		}
	}
	for _, c := range contributions {
		if qrdbDuty, declared := qrdbDuties[c.duty]; declared {
			qrdbsdk.RegisterPermissionDuty(qrdbDuty, c.permission)
		}
	}
	for _, c := range contributions {
		for _, role := range c.roles {
			qrdbsdk.RegisterPermissionRoleGrant(role, c.permission)
		}
	}
}
