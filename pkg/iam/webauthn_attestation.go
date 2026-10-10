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

package iam

import (
	"context"
	"encoding/hex"
	"strings"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

// =============================================================================
// WebAuthn enrollment assurance gate
//
// WHERE THIS RUNS, AND WHY THERE
//
// The gate is evaluated inside webAuthnFactorWriteThrough.Save — the single
// point every completed WebAuthn registration converges on, because xkms's
// Service.FinishRegistration persists through CredentialStore.Save no matter
// which entry point drove the ceremony.
//
// Gating at the XKMSWebAuthnManager instead (where the old AAGUID check lived)
// covered only the paths that go THROUGH the manager. The live enrollment
// route does not: POST /api/v1/iam/{tenant_id}/webauthn/registration/finish is
// served by moduleWebAuthnHandler, which dispatches straight into xkms's own
// HTTP handler and never touches the manager. The allowlist was therefore
// absent from the primary registration route while appearing, from the
// manager's code, to be enforced.
//
// WHAT THE EVIDENCE IS, AND WHY IT IS TRUSTWORTHY HERE
//
// By the time Save is reached, go-webauthn has already run the attestation
// statement's verification procedure (protocol.AttestationObject.Verify ->
// VerifyAttestation) and written the RESULT to Credential.AttestationType:
// "basic_full", "attca", "anonca", "basic_surrogate" or "none". That is a
// verified value, not a browser-supplied claim, which is what makes the AAGUID
// alongside it meaningful. Checking an AAGUID without that verification is
// theatre — an unattested ceremony reports no AAGUID at all, so the allowlist
// has nothing to test and passes everything.
// =============================================================================

// WebAuthnAssurance is the minimum attestation level an enrollment must prove
// before the credential is persisted.
type WebAuthnAssurance int

const (
	// WebAuthnAssuranceHardware requires a manufacturer-attested authenticator:
	// the attestation statement must chain to a batch attestation key and the
	// reported AAGUID must be allowlisted.
	//
	// It is the ZERO VALUE. A gate nobody configured demands hardware, so the
	// safe path is the one an operator gets by doing nothing — and a wiring
	// mistake that drops the policy fails closed instead of silently admitting
	// software authenticators.
	WebAuthnAssuranceHardware WebAuthnAssurance = iota

	// WebAuthnAssuranceAny accepts any authenticator, including one that
	// conveys no attestation. The proven level is still recorded on the stored
	// factor, so a credential enrolled this way is visibly software-backed and
	// cannot later be mistaken for hardware at login.
	//
	// This is the explicit opt-in for tenant (non-administrative) enrollment.
	// It is never reached by default and it NEVER applies to an administrative
	// enrollment: resolveHardwareRequired ignores it for administrators, so
	// loosening the tenant policy cannot loosen the control that guards the
	// platform.
	WebAuthnAssuranceAny

	// WebAuthnAssuranceInsecureAllowSoftwareAdministrators additionally drops
	// the administrator bar, so a software or virtual authenticator may become
	// an ADMINISTRATOR credential.
	//
	// This exists for one reason: an automated test environment that drives a
	// headless software authenticator (our own integration suite, and the
	// CDP virtual authenticator the browser E2E probes use) enrolls
	// administrators, and under any other policy every such probe is refused —
	// correctly, because a software authenticator genuinely is not hardware.
	//
	// It is spelled the way it is on purpose. Its config value is
	// "insecure-allow-software-administrators", which nobody sets by accident
	// and nobody leaves in a production file without a reviewer noticing. It
	// cannot be reached by omitting configuration, by a typo (an unrecognised
	// value is refused, not defaulted), or by any shorter alias. A deployment
	// that selects it is announced at startup and every credential it admits
	// still records the level it actually proved.
	//
	// It is not a fallback for "the gate is inconvenient". If a real
	// deployment cannot enroll hardware, the answer is an allowlist entry, not
	// this.
	WebAuthnAssuranceInsecureAllowSoftwareAdministrators
)

// String implements fmt.Stringer. The value it returns is the same one
// ParseWebAuthnAssurance accepts, so a startup log line names a policy an
// operator can look up in their own configuration file.
func (a WebAuthnAssurance) String() string {
	switch a {
	case WebAuthnAssuranceHardware:
		return "hardware"
	case WebAuthnAssuranceAny:
		return "any"
	case WebAuthnAssuranceInsecureAllowSoftwareAdministrators:
		return "insecure-allow-software-administrators"
	default:
		return "unknown"
	}
}

// AllowsSoftwareAdministrators reports whether this policy permits a software
// authenticator to become an administrator credential. It is the predicate the
// startup path uses to decide whether to announce the deployment as insecure.
func (a WebAuthnAssurance) AllowsSoftwareAdministrators() bool {
	return a == WebAuthnAssuranceInsecureAllowSoftwareAdministrators
}

// ParseWebAuthnAssurance maps an operator-configured string onto a
// WebAuthnAssurance. An empty value yields WebAuthnAssuranceHardware — absent
// configuration must not weaken the gate. An unrecognised value is refused
// rather than defaulted, so a typo cannot silently become a policy.
//
// There is deliberately no short alias for
// "insecure-allow-software-administrators": the only spelling that selects it
// is the one that says what it does.
func ParseWebAuthnAssurance(value string) (WebAuthnAssurance, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "hardware":
		return WebAuthnAssuranceHardware, nil
	case "any":
		return WebAuthnAssuranceAny, nil
	case "insecure-allow-software-administrators":
		return WebAuthnAssuranceInsecureAllowSoftwareAdministrators, nil
	default:
		return WebAuthnAssuranceHardware, &WebAuthnAssuranceValueError{Value: value}
	}
}

// WebAuthnAssuranceValueError reports an unrecognised assurance setting.
type WebAuthnAssuranceValueError struct {
	// Value is the operator-supplied string that did not parse.
	Value string
}

// Error implements error.
func (e *WebAuthnAssuranceValueError) Error() string {
	if e == nil {
		return "iam.webauthn.assurance: invalid value"
	}
	return "iam.webauthn.assurance: unrecognised value \"" + e.Value +
		"\"; must be \"hardware\" (require a manufacturer-attested security key), " +
		"\"any\" (accept any authenticator for non-administrative accounts), or " +
		"\"insecure-allow-software-administrators\" (automated test environments only)"
}

// WebAuthnAdminResolver reports whether the account a registration is being
// completed for holds an administrative role. The production resolver asks the
// same canonical user and role stores authorization resolves against rather
// than keeping its own idea of who is an administrator.
type WebAuthnAdminResolver interface {
	// IsAdministrativeAccount reports whether the account identified by email
	// within tenantID holds an administrative role.
	//
	// FAILING TO ANSWER IS NOT ANSWERING "no". An implementation that cannot
	// resolve the account — including because it does not exist yet — must
	// return an error, and the gate treats that as administrative. Self-service
	// registration completes the WebAuthn ceremony BEFORE it creates the user
	// and then grants that user Admin on the new tenant, so "unknown account"
	// is precisely the case that must be held to the administrator bar.
	IsAdministrativeAccount(ctx context.Context, tenantID, email string) (bool, error)
}

// WebAuthnRoleResolver resolves tenant system role names through qrdb's
// canonical role-name resolver.
type WebAuthnRoleResolver interface {
	qrdbsdk.RoleNameResolver
}

type WebAuthnRoleResolverError struct {
	RoleID string
	Reason string
	Err    error
}

func (e *WebAuthnRoleResolverError) Error() string {
	if e == nil {
		return "iam.webauthn.admin: role resolution failed"
	}
	msg := "iam.webauthn.admin: role resolution failed"
	if e.RoleID != "" {
		msg += " for role " + e.RoleID
	}
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *WebAuthnRoleResolverError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// WebAuthnAttestationGate decides whether a completed registration may be
// persisted, and records what it proved.
//
// The zero value is usable and maximally strict: hardware assurance, no
// allowlist (which denies), no admin resolver (which treats every registrant as
// an administrator). Every field that could weaken it has to be supplied
// deliberately.
type WebAuthnAttestationGate struct {
	// assurance is the requirement applied to NON-administrative accounts.
	// Administrative accounts are always held to hardware.
	assurance WebAuthnAssurance

	// allowlist is the AAGUID gate. Nil denies whenever hardware is required
	// (see AAGUIDAllowlist.Check).
	allowlist *AAGUIDAllowlist

	// admins resolves whether a registrant is an administrator. Nil means
	// "cannot tell", which the gate resolves to administrative.
	admins WebAuthnAdminResolver
}

// NewWebAuthnAttestationGate builds a gate. Passing a nil allowlist or a nil
// resolver is legal and produces the strict behaviour described on the type;
// they are parameters rather than required dependencies so a deployment that
// has not configured them is refused at ceremony time with an operator-facing
// reason, instead of failing to construct at startup and taking the whole
// WebAuthn surface down.
func NewWebAuthnAttestationGate(
	assurance WebAuthnAssurance,
	allowlist *AAGUIDAllowlist,
	admins WebAuthnAdminResolver,
) *WebAuthnAttestationGate {
	return &WebAuthnAttestationGate{
		assurance: assurance,
		allowlist: allowlist,
		admins:    admins,
	}
}

// Assurance returns the requirement applied to non-administrative accounts.
func (g *WebAuthnAttestationGate) Assurance() WebAuthnAssurance {
	if g == nil {
		return WebAuthnAssuranceHardware
	}
	return g.assurance
}

// Evaluate decides whether a registration may be persisted for the account
// identified by (tenantID, email), and returns the attestation it proved.
//
// attestationType is the VERIFIED attestation type go-webauthn wrote onto the
// credential; aaguid is the authenticator model identifier from the same
// attested authenticator data.
//
// It returns a non-nil *AttestationResult on success — including when the
// policy is WebAuthnAssuranceAny and the credential proved nothing — so the
// caller always has a level to record. On refusal it returns a typed error and
// no result, and the caller must not persist anything.
func (g *WebAuthnAttestationGate) Evaluate(
	ctx context.Context,
	tenantID, email, attestationType string,
	aaguid []byte,
) (*AttestationResult, error) {
	credentialAAGUID := hex.EncodeToString(aaguid)
	chain := attestationChainFromContext(ctx)

	// The RECORDED level is a function of the attestation type AND the trust
	// path — never of the type alone.
	//
	// go-webauthn assigns "basic_full" after checking only that the statement
	// is internally consistent; it does not walk the certificate chain. So an
	// unverified basic_full proves exactly what self-attestation proves —
	// possession of the key, not where it lives — and must be RECORDED as
	// software, not merely refused for administrators.
	//
	// Getting this wrong is subtle and was the defect: capping only at the
	// admin gate would let a tenant enrollment under the permissive policy
	// persist attest_level=hardware for an authenticator nothing vouched for,
	// and login policy reads that record. A credential must never be described
	// as more than was proven, whichever policy admitted it.
	proven := webAuthnAttestLevel(attestationType)
	if proven.IsHardware() && !chainVouchesFor(chain, credentialAAGUID) {
		proven = AttestLevelSoftware
	}

	result := &AttestationResult{
		level:  proven,
		aaguid: credentialAAGUID,
		format: normalizedAttestationFormat(attestationType),
		method: AttestMethodWebAuthn,
	}

	if g != nil && !g.resolveHardwareRequired(ctx, tenantID, email) {
		return result, nil
	}

	// Hardware is required from here on. The level above already accounts for
	// the trust path, so anything short of hardware is refused with the reason
	// that fits: an unverified manufacturer statement is a distinct failure
	// from no statement at all, and an operator debugging a rejected security
	// key needs to be told which one they hit.
	if !proven.IsHardware() {
		if webAuthnAttestLevel(attestationType).IsHardware() {
			// A verified path that covers a DIFFERENT model is its own
			// failure, and naming it saves an operator from hunting a trust
			// store problem that is not there.
			if chain.Verified() {
				return nil, refusedByAttestationPolicy(&AttestationChainMismatchError{
					VerifiedAAGUID:   chain.AAGUID(),
					CredentialAAGUID: credentialAAGUID,
				})
			}
			return nil, refusedByAttestationPolicy(&AttestationChainUnverifiedError{AAGUID: credentialAAGUID})
		}
		return nil, refusedByAttestationPolicy(&AttestationRequiredError{
			Operation: "webauthn enrollment",
			Required:  AttestLevelHardware,
			Proven:    proven,
			Format:    result.format,
		})
	}

	// Only now is the AAGUID worth testing: a manufacturer the operator trusts
	// has vouched for it. A nil allowlist denies here rather than being
	// skipped — and so does a nil GATE, which is why the allowlist is read
	// through a nil-safe accessor rather than dereferenced.
	if err := g.allowlistOrNil().Check(aaguid); err != nil {
		return nil, err
	}

	return result, nil
}

// refusedByAttestationPolicy gives an enrollment refusal the repo's canonical
// classification: forbidden.
//
// The classification belongs HERE, at the one place that knows the refusal is
// a decision about the caller's authenticator rather than a fault in the
// server. Every transport in this repo already routes an error by its QBError
// code — REST's handleServiceError to 403, gRPC's toProtoError to
// PermissionDenied, and the SDK transports likewise — so saying it once at the
// source is what makes a surface correct that was never taught these three
// types exist. Registration reaches six of them (self-service, invite
// activation, the xkms compat bridge, gRPC, MCP and the embedded transport);
// classifying per transport instead is how the seventh gets missed, which is
// exactly how these refusals shipped as 500s.
//
// The sibling AAGUID refusal next door (NewErrAAGUIDNotAllowed) has always
// been spelled this way; these were the anomaly.
//
// Wrapping preserves detail: QBError.Unwrap means errors.As still finds the
// concrete type, so a caller that needs to know WHICH level was proven, or
// which AAGUID was claimed, still can.
func refusedByAttestationPolicy(cause error) error {
	return &dberrors.QBError{
		Code: dberrors.ErrForbidden,
		Op:   "iam.webauthn.enrollment",
		Err:  cause,
	}
}

// chainVouchesFor reports whether a verified trust path covers the
// authenticator model this credential reports.
//
// Both halves matter. An unverified path is no evidence at all; a verified path
// for a DIFFERENT model is evidence about someone else's authenticator, and
// accepting it would let a chain recorded for one registration vouch for
// another.
func chainVouchesFor(chain *WebAuthnAttestationChain, credentialAAGUID string) bool {
	return chain.Verified() && chain.AAGUID() == credentialAAGUID
}

// allowlistOrNil returns the gate's allowlist without dereferencing a nil
// gate. It returns a nil *AAGUIDAllowlist rather than a non-nil empty one so
// the refusal names the missing CONFIGURATION ("no allowlist is configured")
// rather than the authenticator ("this model is not allowlisted") — the
// operator's problem is the former.
func (g *WebAuthnAttestationGate) allowlistOrNil() *AAGUIDAllowlist {
	if g == nil {
		return nil
	}
	return g.allowlist
}

// resolveHardwareRequired answers whether THIS registration must prove
// hardware. Administrators always must. Everyone else is held to the
// configured assurance.
//
// The resolver failing, or being absent, resolves to true. See
// WebAuthnAdminResolver for why an unresolvable account is the administrator
// case rather than the tenant-user case.
func (g *WebAuthnAttestationGate) resolveHardwareRequired(ctx context.Context, tenantID, email string) bool {
	if g == nil {
		return true
	}
	if g.assurance == WebAuthnAssuranceHardware {
		return true
	}
	// The only policy that drops the administrator bar. Checked before the
	// resolver so a test environment does not need a working user store to be
	// permissive — and so this is the single, greppable line that decides an
	// administrator may hold a software credential.
	if g.assurance.AllowsSoftwareAdministrators() {
		return false
	}
	if g.admins == nil {
		return true
	}
	// An empty email means the caller could not identify the registrant at all.
	// That is decided HERE rather than delegated: a resolver is free to answer
	// "no such account, not an administrator" for an empty key, and a gate that
	// accepted that answer would admit an unidentified registrant under the
	// permissive tenant policy. Who the credential is for is a question the
	// gate must have an answer to, not one it can leave to a lookup.
	if strings.TrimSpace(email) == "" {
		return true
	}
	isAdmin, err := g.admins.IsAdministrativeAccount(ctx, tenantID, email)
	if err != nil {
		return true
	}
	return isAdmin
}

// normalizedAttestationFormat lowercases the verified attestation type for
// recording. An empty type is reported as "none" so the stored factor and the
// refusal message name the same thing the WebAuthn spec does.
func normalizedAttestationFormat(attestationType string) string {
	trimmed := strings.ToLower(strings.TrimSpace(attestationType))
	if trimmed == "" {
		return "none"
	}
	return trimmed
}

// =============================================================================
// Admin resolution over the canonical user store
// =============================================================================

// adminRoleResolver answers WebAuthnAdminResolver from the canonical broker
// user store and qrdb's system-role identifier resolver. User.Roles contains
// role IDs, so the gate only checks membership in qrdb-owned Admin/SO ID sets.
type adminRoleResolver struct {
	users WebAuthnUserResolver
	roles WebAuthnRoleResolver
}

// NewWebAuthnAdminResolver adapts the canonical user and role stores to the
// gate's resolver seam. A nil store yields a nil resolver, which the gate
// treats as "cannot tell" and resolves to administrative.
func NewWebAuthnAdminResolver(users WebAuthnUserResolver, roles WebAuthnRoleResolver) WebAuthnAdminResolver {
	if users == nil || roles == nil {
		return nil
	}
	return &adminRoleResolver{users: users, roles: roles}
}

// IsAdministrativeAccount implements WebAuthnAdminResolver. A user lookup
// failure or absent account is propagated as an error, NOT reported as "not an
// administrator": the gate reads an error as the administrator case. A system
// role resolver failure is treated as administrative, which is fail-safe.
func (r *adminRoleResolver) IsAdministrativeAccount(ctx context.Context, tenantID, email string) (bool, error) {
	if r == nil || r.users == nil {
		return false, &WebAuthnUserResolverError{Reason: "canonical user resolver is not configured"}
	}
	if r.roles == nil {
		return false, &WebAuthnRoleResolverError{Reason: "canonical role resolver is not configured"}
	}
	// The tenant is passed EXPLICITLY, and also bound into the context.
	//
	// The explicit argument is the contract: GetUserByEmail is tenant-scoped
	// (#359), so the tenant cannot be left to whatever the caller's context
	// happened to carry. The context binding stays because the qrdb-backed
	// storage consults its TENANT-SCOPED email index off the context, and
	// several ceremony entry points carry the tenant as a separate argument
	// while forwarding the original request context. Without one or the other
	// the lookup can fall back to a global index and resolve a different
	// account of the same email in another tenant — answering the "is this an
	// administrator" question about the wrong person.
	lookupCtx := tenantctx.WithTenant(ctx, tenantID)
	user, err := r.users.GetUserByEmail(lookupCtx, tenantID, email)
	if err != nil {
		return false, err
	}
	if user == nil {
		return false, &WebAuthnUserResolverError{Reason: "canonical user resolver returned nil user"}
	}

	adminRoleIDs, err := qrdbsdk.SystemRoleIDs(lookupCtx, r.roles, tenantID, qrdbsdk.RoleNameAdmin)
	if err != nil {
		return true, nil
	}
	soRoleIDs, err := qrdbsdk.SystemRoleIDs(lookupCtx, r.roles, tenantID, qrdbsdk.RoleNameSO)
	if err != nil {
		return true, nil
	}

	administrativeRoleIDs := make(map[string]struct{}, len(adminRoleIDs)+len(soRoleIDs))
	for _, roleID := range adminRoleIDs {
		administrativeRoleIDs[roleID] = struct{}{}
	}
	for _, roleID := range soRoleIDs {
		administrativeRoleIDs[roleID] = struct{}{}
	}
	for _, roleID := range user.Roles {
		if _, ok := administrativeRoleIDs[roleID]; ok {
			return true, nil
		}
	}
	return false, nil
}
