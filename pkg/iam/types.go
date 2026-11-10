// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

// Package iam provides Identity and Access Management functionality for QuicBaaS.
// It implements multi-tenant authentication, authorization, and access control with
// support for ABAC policies, OAuth2/OIDC, and WebAuthn.
package iam

import (
	"time"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// Lifecycle is the canonical account state, owned by go-qrdb and re-exported
// here so this package names it without redefining it.
//
// It replaces the State/Suspended/Locked triple this package used to carry.
// That triple could describe a record as suspended and active at once, and it
// was a second source of truth for a question qrdb already answers; qrdb's
// transition table now owns which moves are legal, and Enabled survives only as
// a derived projection (exactly Lifecycle == active).
type Lifecycle = qrdbsdk.Lifecycle

// Authentication method names carried in User.AuthMethods. QuicBaaS is
// passwordless: these are the credential kinds an account may activate and
// authenticate with.
const (
	// AuthMethodWebAuthn is a FIDO2 / WebAuthn passkey.
	AuthMethodWebAuthn = "webauthn"

	// AuthMethodPIV is a PIV smartcard certificate.
	AuthMethodPIV = "piv"
)

// Lifecycle states, re-exported from go-qrdb. A consumer must not define its
// own vocabulary here: a state this package invented would not survive a write.
const (
	// LifecyclePending is an account created but not yet activated via an
	// invite flow.
	LifecyclePending = qrdbsdk.LifecyclePending

	// LifecycleActive is the only state in which an account may authenticate.
	LifecycleActive = qrdbsdk.LifecycleActive

	// LifecycleSuspended is an administrative hold.
	LifecycleSuspended = qrdbsdk.LifecycleSuspended

	// LifecycleLocked is an automated lockout.
	LifecycleLocked = qrdbsdk.LifecycleLocked

	// LifecycleDisabled is an administratively deactivated account.
	LifecycleDisabled = qrdbsdk.LifecycleDisabled

	// LifecycleDeleting marks a deletion in progress. It is terminal, which is
	// what lets a deletion close the account to credential ceremonies before it
	// begins sweeping credentials: nothing can transition back out and race it.
	LifecycleDeleting = qrdbsdk.LifecycleDeleting
)

// Tenant represents a logical isolation boundary in QuicBaaS IAM.
// All users, groups, roles, and policies belong to a specific tenant.
type Tenant struct {
	// ID is the unique tenant identifier (URL-safe, lowercase)
	ID string `json:"id" yaml:"id"`

	// Name is the human-readable tenant name
	Name string `json:"name" yaml:"name"`

	// Settings contains tenant-specific configuration
	Settings TenantSettings `json:"settings" yaml:"settings"`

	// Quotas defines resource limits for this tenant
	Quotas TenantQuotas `json:"quotas,omitempty" yaml:"quotas,omitempty"`

	// Created is the tenant creation timestamp
	Created time.Time `json:"created" yaml:"created"`

	// Updated is the last modification timestamp
	Updated time.Time `json:"updated" yaml:"updated"`
}

// TenantSettings contains tenant-specific configuration options.
type TenantSettings struct {
	// MaxUsers is the maximum number of users allowed in this tenant
	MaxUsers int `json:"max_users,omitempty" yaml:"max_users,omitempty"`

	// DisplayName is the human-readable tenant label shown in login UIs.
	// When empty the tenant's Name field is used as a fallback.
	DisplayName string `json:"display_name,omitempty" yaml:"display_name,omitempty"`

	// MFARequired indicates whether multi-factor authentication is mandatory.
	//
	// Deprecated: Use MFAPolicy.Required instead.
	MFARequired bool `json:"mfa_required" yaml:"mfa_required"`

	// MFAPolicy defines MFA requirements and configuration for this tenant
	MFAPolicy *MFAPolicy `json:"mfa_policy,omitempty" yaml:"mfa_policy,omitempty"`

	// NamedNetworks maps network names to CIDR blocks for policy evaluation
	NamedNetworks map[string][]string `json:"named_networks,omitempty" yaml:"named_networks,omitempty"`

	// SessionTimeout is the default session timeout duration
	SessionTimeout time.Duration `json:"session_timeout,omitempty" yaml:"session_timeout,omitempty"`

	// SessionIdleTimeout is the idle session timeout
	SessionIdleTimeout time.Duration `json:"session_idle_timeout,omitempty" yaml:"session_idle_timeout,omitempty"`

	// TokenTTL is the default access token lifetime
	TokenTTL time.Duration `json:"token_ttl,omitempty" yaml:"token_ttl,omitempty"`

	// RefreshTokenTTL is the refresh token lifetime
	RefreshTokenTTL time.Duration `json:"refresh_token_ttl,omitempty" yaml:"refresh_token_ttl,omitempty"`

	// AuditPolicy defines audit logging configuration for this tenant
	// AuditPolicy (disabled) *AuditPolicy `json:"audit_policy,omitempty" yaml:"audit_policy,omitempty"`

	// SandboxPolicy overrides the orchestrator's server-level SandboxPolicy
	// for this tenant. Allowed values: "required" (containers only; raw
	// exec forbidden), "preferred" (containers by default but raw exec
	// allowed when explicitly requested), "opt-in" (raw exec by default).
	// Empty string defers to the server default.
	//
	// Intended for compliance-sensitive tenants (e.g., FedRAMP/HIPAA) that
	// must force SandboxPolicyRequired regardless of the host daemon's
	// configured policy.
	SandboxPolicy string `json:"sandbox_policy,omitempty" yaml:"sandbox_policy,omitempty"`

	// WebAuthn overrides the global/server WebAuthn relying-party configuration
	// for this tenant. Each field falls back to the global config when empty so
	// a tenant served from its own virtual host can pin its own RPID/origins
	// while tenants that configure nothing keep today's global default. Nil
	// defers entirely to the global config.
	WebAuthn *TenantWebAuthnConfig `json:"webauthn,omitempty" yaml:"webauthn,omitempty"`

	// OIDC carries the tenant's OIDC issuance overrides. Nil defers entirely to
	// the global/server defaults (single broker issuer, shared signing key).
	OIDC *TenantOIDCConfig `json:"oidc,omitempty" yaml:"oidc,omitempty"`

	// AuthzMode optionally overrides the deployment-level security.authz_mode for
	// this tenant. It governs whether an online liveness/revocation check is
	// enforced in addition to the always-offline scope decision. Valid values:
	// "offline", "online", "hybrid". Empty means "use the deployment default".
	AuthzMode string `json:"authz_mode,omitempty" yaml:"authz_mode,omitempty"`
}

// TenantWebAuthnConfig is the per-tenant WebAuthn relying-party override. Each
// field falls back to the global/server WebAuthn config when empty (per-field
// fallback), mirroring the DisplayName->Name / SandboxPolicy->server-default
// idiom: an empty RPID inherits the global RPID, empty origins inherit the
// global origins, etc.
type TenantWebAuthnConfig struct {
	// RPID is the relying-party identifier (effective domain). Empty inherits
	// the global RPID.
	RPID string `json:"rp_id,omitempty" yaml:"rp_id,omitempty"`

	// RPDisplayName is the human-readable relying-party name shown by the
	// authenticator. Empty inherits the global display name.
	RPDisplayName string `json:"rp_display_name,omitempty" yaml:"rp_display_name,omitempty"`

	// RPOrigins is the list of allowed WebAuthn origins. Empty (nil or
	// zero-length) inherits the global origins.
	RPOrigins []string `json:"rp_origins,omitempty" yaml:"rp_origins,omitempty"`
}

// TenantOIDCConfig carries a tenant's OIDC issuance overrides. The single
// broker issuer + tenant_id claim is the default; per-tenant cryptographic
// isolation is opt-in via DedicatedSigningKey.
type TenantOIDCConfig struct {
	// DedicatedSigningKey, when true, makes the broker sign this tenant's
	// tokens with a dedicated per-tenant ES256 key (its own kid) under the
	// SAME single broker issuer. The validator still trusts one issuer and one
	// JWKS (now publishing multiple kids), and kid-aware validation resolves
	// the right key. When false (default) the tenant's tokens are signed with
	// the shared global signing key.
	DedicatedSigningKey bool `json:"dedicated_signing_key,omitempty" yaml:"dedicated_signing_key,omitempty"`

	// IssuerURL seam: a per-tenant ISSUER URL (a distinct trust anchor) is
	// intentionally OUT OF SCOPE here -- it would break the single-anchor
	// validator contract. A future phase may add it behind an explicit flag;
	// until then the broker always uses its single issuer.
	// IssuerURL string `json:"issuer_url,omitempty" yaml:"issuer_url,omitempty"`
}

// TenantQuotas defines BaaS-layer resource limits for a tenant.
// Engine-level quotas (users, roles, storage, databases, keys, etc.) are owned
// and enforced exclusively by qrdb's TenantServicer. Only IAM-subsystem concepts
// that are not engine primitives belong here.
type TenantQuotas struct {
	// MaxPolicies is the maximum number of ABAC policies (BaaS-only concept).
	MaxPolicies int `json:"max_policies,omitempty" yaml:"max_policies,omitempty"`

	// MaxApps is the maximum number of OAuth2/OIDC applications (BaaS-only concept).
	MaxApps int `json:"max_apps,omitempty" yaml:"max_apps,omitempty"`
}

// User is an authenticated principal, and it is a PROJECTION of the go-qrdb SDK
// user rather than a model of its own. Every field below is stored by, and read
// back from, qrdb's canonical user record: this package keeps no side-record and
// no second identifier space.
//
// The identity is qrdb's Snowflake, held as the same uint64 the store assigns
// and addresses the record by, so there is nothing to convert between the
// broker and the store. Its JSON encoding is the decimal string qrdb's own
// transport type uses, because a JSON number above 2^53 would lose precision in
// a JavaScript client. YAML has no string option, so YAML carries the plain
// integer. The uuid<->uint64
// IDMap that used to correlate two identifier spaces is gone, along with the "id
// correlation not found" failure that could sever a verified login from its
// record.
type User struct {
	// ID is the qrdb Snowflake user id. Zero on a user that has not been created
	// yet; qrdb assigns the id server-side.
	ID uint64 `json:"id,string" yaml:"id"`

	// Email is the CANONICAL tenant-unique login key — what a login ceremony
	// resolves a verified factor to, and what every layer above keys a user by.
	Email string `json:"email" yaml:"email"`

	// Name is the display name. Presentation only; never a lookup key.
	Name string `json:"name" yaml:"name"`

	// Username is the tenant-unique administrative handle — qrdb's Username,
	// the name an administrator types to assign a role and the subject a
	// ceremony login presents. It is constrained (3-64 of [A-Za-z0-9_-]) so it
	// can carry a uniqueness constraint; Name cannot, which is why the two are
	// separate fields here exactly as they are in qrdb.
	//
	// They used to be one. CreateUser required and validated a Username, stored
	// the display Name over it, and the projection then wrote that display name
	// into qrdb's unique Username — so a lookup by the handle the caller chose
	// missed, and two people with the same display name collided on a
	// uniqueness constraint neither of them had asked to share.
	Username string `json:"username" yaml:"username"`

	// TenantID is the owning tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Roles is the list of role IDs assigned to this user
	Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`

	// Attributes contains custom user attributes for policy evaluation. It is
	// map[string]string because that is qrdb's shape: a map[string]interface{}
	// could hold values the store cannot persist, which is a field that silently
	// loses data rather than refusing it.
	Attributes map[string]string `json:"attributes,omitempty" yaml:"attributes,omitempty"`

	// Created is the user creation timestamp
	Created time.Time `json:"created" yaml:"created"`

	// Updated is the last modification timestamp
	Updated time.Time `json:"updated" yaml:"updated"`

	// LastAuth is the last successful authentication timestamp. qrdb commits it
	// as a high-water mark, so a delayed or retried login cannot roll it back.
	LastAuth *time.Time `json:"last_auth,omitempty" yaml:"last_auth,omitempty"`

	// Lifecycle is the account state — pending, active, suspended, locked,
	// disabled or deleting. It is the single source of truth for whether this
	// account may authenticate; ask CanAuthenticate rather than comparing.
	Lifecycle Lifecycle `json:"lifecycle,omitempty" yaml:"lifecycle,omitempty"`

	// AuthMethods lists the allowed authentication methods for this user (e.g.
	// "webauthn", "piv"). A policy statement about what the account may present,
	// distinct from what it has actually enrolled. Empty states no restriction.
	AuthMethods []string `json:"auth_methods,omitempty" yaml:"auth_methods,omitempty"`

	// CertBindings holds the qrdb-canonical mTLS certificate bindings for this
	// user. PIV activation appends a binding of kind
	// qrdbsdk.CertBindingPIVAttested whose Thumbprint is the login lookup key
	// (hex SHA-256 of the certificate DER). A user may hold more than one binding
	// (e.g. a renewed or additional PIV card). Empty unless the user has bound at
	// least one certificate.
	CertBindings []qrdbsdk.CertBinding `json:"cert_bindings,omitempty" yaml:"cert_bindings,omitempty"`
}

// CanAuthenticate reports whether this account's state permits authentication.
// It defers to qrdb's rule rather than restating it, so a state written by a
// newer node fails closed here exactly as it does in the store.
//
// An ABSENT state DENIES, like every other non-active state. It briefly did the
// opposite, mirroring qrdb's own fallback for records written before the field
// existed — but that is a fail-open on an authentication gate, and its only
// justification was protecting a population of legacy records that, under the
// pre-release policy, we do not carry. A record from the store always has a
// state; an empty one belongs to a value assembled by hand, and a value nobody
// gave a state to is not evidence that it may authenticate.
func (u *User) CanAuthenticate() bool {
	if u == nil {
		return false
	}
	return u.Lifecycle.CanAuthenticate()
}

// Role represents a named set of permissions.
type Role struct {
	// ID is the unique role identifier within the tenant
	ID string `json:"id" yaml:"id"`

	// Name is the role display name
	Name string `json:"name" yaml:"name"`

	// Description explains the role's purpose
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// TenantID is the owning tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Permissions is the list of permission strings
	Permissions []string `json:"permissions,omitempty" yaml:"permissions,omitempty"`

	// RetiredPermissions lists the members of Permissions the running build's
	// permission registry no longer defines. It is derived on every read from
	// the live registry and never stored, so it names the drift between the
	// definition this tenant was seeded with and the one the build now grants
	// — visible on an ordinary listing, before ReconcileBuiltInRoles repairs
	// it. An empty slice means the role agrees with the build.
	RetiredPermissions []string `json:"retired_permissions,omitempty" yaml:"retired_permissions,omitempty"`

	// Created is the role creation timestamp
	Created time.Time `json:"created" yaml:"created"`

	// Updated is the last modification timestamp
	Updated time.Time `json:"updated" yaml:"updated"`
}

// RoleReconcileResult records what reconciling one built-in role changed.
// Only roles that changed are reported, so a zero-valued result never
// appears in a report.
type RoleReconcileResult struct {
	// RoleID is the reconciled role's identifier.
	RoleID string `json:"role_id" yaml:"role_id"`

	// Name is the built-in role's name (admin, operator, ...).
	Name string `json:"name" yaml:"name"`

	// Created is true when the built-in was absent from the tenant and was
	// seeded. Added then carries its whole permission set.
	Created bool `json:"created,omitempty" yaml:"created,omitempty"`

	// Added lists the permissions the build grants that the stored role did
	// not carry, sorted.
	Added []string `json:"added,omitempty" yaml:"added,omitempty"`

	// Removed lists the permissions the stored role carried that the build no
	// longer grants, sorted.
	Removed []string `json:"removed,omitempty" yaml:"removed,omitempty"`
}

// RoleReconcileReport is one tenant's reconciliation outcome. An empty Roles
// slice means the tenant already agreed with the build and nothing was
// written.
type RoleReconcileReport struct {
	// TenantID is the reconciled tenant.
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Roles carries one entry per built-in role that changed.
	Roles []RoleReconcileResult `json:"roles,omitempty" yaml:"roles,omitempty"`
}

// Changed reports whether reconciliation altered anything in the tenant.
func (r *RoleReconcileReport) Changed() bool {
	return r != nil && len(r.Roles) > 0
}

// Application represents an OAuth2/OIDC client application.
type Application struct {
	// ClientID is the unique OAuth client identifier
	ClientID string `json:"client_id" yaml:"client_id"`

	// ClientSecret is the OAuth client secret (hashed)
	ClientSecret string `json:"client_secret,omitempty" yaml:"client_secret,omitempty"`

	// RegistrationTokenHash is the SHA-256 hash (HashTokenValue) of the RFC 7592
	// registration access token issued when this client was created through
	// Dynamic Client Registration. It is the ONLY credential that authorizes the
	// client-configuration endpoints (GET/PUT/DELETE /oauth2/register/{id}).
	//
	// Empty means the client was NOT created through DCR and therefore has no
	// RFC 7592 management credential: the management endpoints refuse it
	// (fail-closed). The plaintext token exists only in the registration
	// response; the manager redacts this hash from every Application it returns.
	RegistrationTokenHash string `json:"registration_token_hash,omitempty" yaml:"registration_token_hash,omitempty"`

	// Name is the application display name
	Name string `json:"name" yaml:"name"`

	// TenantID is the owning tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// RedirectURIs is the list of allowed OAuth redirect URIs
	RedirectURIs []string `json:"redirect_uris" yaml:"redirect_uris"`

	// ResourcePrefix is the resource namespace for this application
	ResourcePrefix string `json:"resource_prefix,omitempty" yaml:"resource_prefix,omitempty"`

	// Scopes is the permission set GRANTED to this application, in qrdb's
	// canonical "resource:action" vocabulary (e.g. "orchestrator:agent_heartbeat").
	// It is the server-side grant a client_credentials token is issued
	// against: the issued scope is the requested scope narrowed to this set,
	// or the whole set when the client requests none (RFC 6749 §3.3). A client
	// therefore cannot mint itself authority nobody granted it.
	//
	// Empty means the application holds no permissions and its tokens
	// authorize nothing beyond what the subject's own qrdb roles resolve to.
	// The field is set by the server-side registration paths that speak for a
	// grantor (agent enrollment, orchestrator bootstrap); no client-facing
	// registration surface populates it.
	Scopes []string `json:"scopes,omitempty" yaml:"scopes,omitempty"`

	// DefaultAudience is the per-app default service audience stamped into a
	// token's `aud` claim when the client does NOT send an RFC 8707 `resource`
	// indicator. It lets downstream resource servers configured for a fixed
	// service audience (e.g. "api://orchestrator") accept tokens from clients
	// that never send a resource indicator. A client-supplied `resource`
	// always overrides this default. Empty means no default (the audience is
	// built from the client/issuer only — fully backward compatible).
	DefaultAudience string `json:"default_audience,omitempty" yaml:"default_audience,omitempty"`

	// GrantTypes is the list of allowed OAuth grant types
	GrantTypes []string `json:"grant_types" yaml:"grant_types"`

	// ResponseTypes is the list of allowed OAuth response types
	ResponseTypes []string `json:"response_types" yaml:"response_types"`

	// TokenEndpointAuthMethod is the client authentication method used at the
	// token endpoint. One of: "client_secret_basic" (default),
	// "client_secret_post", "tls_client_auth", "self_signed_tls_client_auth".
	// RFC 8705 §2.
	TokenEndpointAuthMethod string `json:"token_endpoint_auth_method,omitempty" yaml:"token_endpoint_auth_method,omitempty"`

	// MTLSCertThumbprint is the base64url-encoded SHA-256 thumbprint of the
	// pinned client certificate, used when TokenEndpointAuthMethod is
	// "self_signed_tls_client_auth" (RFC 8705 §2.2).
	MTLSCertThumbprint string `json:"mtls_cert_thumbprint,omitempty" yaml:"mtls_cert_thumbprint,omitempty"`

	// MTLSSubjectDN is the expected certificate Subject Distinguished Name,
	// used when TokenEndpointAuthMethod is "tls_client_auth" (RFC 8705 §2.1).
	MTLSSubjectDN string `json:"mtls_subject_dn,omitempty" yaml:"mtls_subject_dn,omitempty"`

	// MTLSSANDNS is the expected certificate SAN dNSName,
	// used when TokenEndpointAuthMethod is "tls_client_auth" (RFC 8705 §2.1).
	MTLSSANDNS string `json:"mtls_san_dns,omitempty" yaml:"mtls_san_dns,omitempty"`

	// Created is the application creation timestamp
	Created time.Time `json:"created" yaml:"created"`

	// Updated is the last modification timestamp
	Updated time.Time `json:"updated" yaml:"updated"`
}

// Session represents an authenticated user session.
type Session struct {
	// ID is the unique session identifier
	ID string `json:"id" yaml:"id"`

	// UserID is the authenticated user's qrdb Snowflake id
	UserID uint64 `json:"user_id,string" yaml:"user_id"`

	// TenantID is the session tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Created is the session creation timestamp
	Created time.Time `json:"created" yaml:"created"`

	// Expires is the absolute session expiration time
	Expires time.Time `json:"expires" yaml:"expires"`

	// IdleTimeout is the session idle timeout duration
	IdleTimeout time.Duration `json:"idle_timeout" yaml:"idle_timeout"`

	// LastActivity is the last activity timestamp
	LastActivity time.Time `json:"last_activity" yaml:"last_activity"`

	// MFAVerified indicates whether MFA was verified for this session
	MFAVerified bool `json:"mfa_verified" yaml:"mfa_verified"`

	// AuthMethod is the authentication method used (webauthn, oauth, etc.)
	AuthMethod string `json:"auth_method" yaml:"auth_method"`

	// IPAddress is the client IP address
	IPAddress string `json:"ip_address,omitempty" yaml:"ip_address,omitempty"`

	// UserAgent is the client user agent
	UserAgent string `json:"user_agent,omitempty" yaml:"user_agent,omitempty"`
}

// Token represents an OAuth2 access or refresh token.
type Token struct {
	// JTI is the unique token identifier (JWT ID)
	JTI string `json:"jti" yaml:"jti"`

	// Subject is the user or service ID this token represents
	Subject string `json:"subject" yaml:"subject"`

	// TenantID is the token tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Scopes is the list of granted scopes
	Scopes []string `json:"scopes" yaml:"scopes"`

	// Expires is the token expiration timestamp
	Expires time.Time `json:"expires" yaml:"expires"`

	// Issued is the token issuance timestamp
	Issued time.Time `json:"issued" yaml:"issued"`

	// TokenType is the type of token (access, refresh, id)
	TokenType string `json:"token_type" yaml:"token_type"`

	// Revoked indicates whether the token has been revoked
	Revoked bool `json:"revoked,omitempty" yaml:"revoked,omitempty"`

	// ClientID is the OAuth client that requested this token
	ClientID string `json:"client_id,omitempty" yaml:"client_id,omitempty"`

	// ValueHash is the SHA-256 (hex) of an OPAQUE token's value. Opaque tokens
	// (refresh tokens) are presented by value, not by JTI, so the storage layer
	// indexes them under this hash: GetTokenByValue hashes the presented secret
	// and resolves the record. Hashing keeps the bearer secret itself out of the
	// key space, and is empty for JWT-shaped tokens (which are found by JTI).
	ValueHash string `json:"value_hash,omitempty" yaml:"value_hash,omitempty"`

	// Session records the authentication context of the login that minted this
	// refresh token. It is the AUTHORITATIVE source for the claims of every
	// access token minted from it, so a renewal can never silently downgrade
	// assurance (acr/amr/mfa) or drop the sender-constraint (cnf.jkt).
	Session *TokenSessionContext `json:"session,omitempty" yaml:"session,omitempty"`
}

// TokenSessionContext is the frozen authentication context of a login session.
//
// A refresh renews a session; it does not re-authenticate and it does not
// re-authorize. The claims recorded here are therefore replayed verbatim onto
// each renewed access token. Authority changes take effect through the two
// mechanisms that already bound the session: the 15-minute access-token
// lifetime (after which the request-time RBAC gate re-evaluates the subject)
// and refresh-token revocation (which ends the session immediately).
type TokenSessionContext struct {
	// SessionID is the identifier of the login this session IS (Claims.SessionID
	// / the sid claim), frozen here so every renewal replays the value its login
	// established instead of minting a new one. That is what makes sid stable
	// across a refresh and different for every login — the property a client
	// needs to tell a renewal of its own session apart from somebody's second
	// login as the same principal into the same tenant (go-quicbaas#326).
	//
	// Empty for sessions minted before the claim existed; a renewal of one of
	// those mints a fresh sid per access token, which reads to a client as a new
	// session — the safe direction.
	SessionID string `json:"session_id,omitempty" yaml:"session_id,omitempty"`

	// UserID is the IMMUTABLE IAM user id of the principal, frozen at login so
	// a renewal identifies the same account even if the login's subject
	// (a mutable username on the ceremony path) has since been renamed.
	UserID uint64 `json:"user_id,string,omitempty" yaml:"user_id,omitempty"`

	// PrincipalType carries the session's positive principal marker (see
	// Claims.PrincipalType) onto every renewed access token, so a renewal is
	// classified the same way the original login was.
	PrincipalType string `json:"principal_type,omitempty" yaml:"principal_type,omitempty"`

	// Roles are the roles resolved at login.
	Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`

	// Groups are the domain groups resolved at login.
	Groups []string `json:"groups,omitempty" yaml:"groups,omitempty"`

	// Audience is the audience the session's access tokens are minted for.
	Audience []string `json:"audience,omitempty" yaml:"audience,omitempty"`

	// Email and Name are profile claims carried by the session.
	Email string `json:"email,omitempty" yaml:"email,omitempty"`
	Name  string `json:"name,omitempty" yaml:"name,omitempty"`

	// AuthMethod records the factor used (mtls, fido2, piv).
	AuthMethod string `json:"auth_method,omitempty" yaml:"auth_method,omitempty"`

	// MFAVerified reports whether the login satisfied multi-factor assurance.
	MFAVerified bool `json:"mfa_verified,omitempty" yaml:"mfa_verified,omitempty"`

	// AMR / ACR are the RFC 8176 authentication method references and the
	// authentication context class of the originating login.
	AMR []string `json:"amr,omitempty" yaml:"amr,omitempty"`
	ACR string   `json:"acr,omitempty" yaml:"acr,omitempty"`

	// AttestLevel and AttestAAGUID are the provenance a verifier PROVED about
	// the credential that performed the login (see Claims.AttestLevel).
	//
	// They are frozen here for the same reason acr/amr are: a renewal rebuilds
	// the bearer from this record alone, so an attestation left out of it is
	// gone the moment the session is first refreshed. A login that really did
	// prove a hardware key would quietly degrade into one that proves nothing
	// within a single access-token lifetime, and a hardware-only policy would
	// start refusing a session that never lost its hardware.
	//
	// Empty means the login established nothing — a renewal must never invent
	// an attestation the login did not make.
	AttestLevel  string `json:"attest_level,omitempty" yaml:"attest_level,omitempty"`
	AttestAAGUID string `json:"attest_aaguid,omitempty" yaml:"attest_aaguid,omitempty"`

	// Jkt is the RFC 9449 DPoP key thumbprint the session bearer was bound to
	// (cnf.jkt). When set, a refresh MUST present a DPoP proof over the same
	// key, and the renewed access token stays bound to it.
	Jkt string `json:"jkt,omitempty" yaml:"jkt,omitempty"`
}

// Policy represents an ABAC policy document.
type Policy struct {
	// APIVersion is the policy schema version
	APIVersion string `json:"apiVersion" yaml:"apiVersion"`

	// Kind is the resource type (always "Policy")
	Kind string `json:"kind" yaml:"kind"`

	// Metadata contains policy metadata
	Metadata PolicyMetadata `json:"metadata" yaml:"metadata"`

	// Spec contains the policy specification
	Spec PolicySpec `json:"spec" yaml:"spec"`

	// LoadError is set only on an entry ListPolicies returns for a stored
	// record that does not decode strictly. Such an entry carries the
	// record's name and tenant and no rules: it is never applied.
	LoadError *StoredPolicyDecodeError `json:"-" yaml:"-"`
}

// PolicyMetadata contains policy identification and description.
type PolicyMetadata struct {
	// Name is the unique policy name within the tenant
	Name string `json:"name" yaml:"name"`

	// Tenant is the owning tenant ID
	Tenant string `json:"tenant" yaml:"tenant"`

	// Description explains the policy's purpose
	Description string `json:"description,omitempty" yaml:"description,omitempty"`

	// Labels are arbitrary key-value labels
	Labels map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`

	// Priority determines evaluation order (higher = first)
	Priority int `json:"priority,omitempty" yaml:"priority,omitempty"`

	// Created is the policy creation timestamp
	Created time.Time `json:"created,omitempty" yaml:"created,omitempty"`

	// Updated is the last modification timestamp
	Updated time.Time `json:"updated,omitempty" yaml:"updated,omitempty"`
}

// PolicySpec contains the policy specification.
type PolicySpec struct {
	// Subjects defines who this policy applies to
	Subjects SubjectSelector `json:"subjects" yaml:"subjects"`

	// Variables defines reusable expressions
	Variables map[string]string `json:"variables,omitempty" yaml:"variables,omitempty"`

	// Rules defines the authorization rules
	Rules []Rule `json:"rules" yaml:"rules"`
}

// SubjectSelector defines criteria for matching subjects.
type SubjectSelector struct {
	// Users is the list of specific user IDs
	Users []string `json:"users,omitempty" yaml:"users,omitempty"`

	// Groups is the list of group IDs
	Groups []string `json:"groups,omitempty" yaml:"groups,omitempty"`

	// Roles is the list of role IDs
	Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`

	// Services is the list of service account IDs
	Services []string `json:"services,omitempty" yaml:"services,omitempty"`

	// Match contains attribute-based matching criteria
	Match map[string]interface{} `json:"match,omitempty" yaml:"match,omitempty"`

	// Authenticated matches all authenticated users
	Authenticated bool `json:"authenticated,omitempty" yaml:"authenticated,omitempty"`

	// Any matches all subjects including anonymous
	Any bool `json:"any,omitempty" yaml:"any,omitempty"`
}

// Rule represents a single authorization rule within a policy.
type Rule struct {
	// Name is the unique rule identifier within the policy
	Name string `json:"name" yaml:"name"`

	// Effect is the authorization decision (allow or deny)
	Effect Effect `json:"effect" yaml:"effect"`

	// Priority determines rule evaluation order (higher = first)
	Priority int `json:"priority,omitempty" yaml:"priority,omitempty"`

	// Actions is the list of operations this rule applies to
	Actions []string `json:"actions" yaml:"actions"`

	// Resources is the list of resource patterns this rule applies to
	Resources []string `json:"resources" yaml:"resources"`

	// When contains conditions that must all be true
	When []Condition `json:"when,omitempty" yaml:"when,omitempty"`

	// Unless contains conditions that must all be false
	Unless []Condition `json:"unless,omitempty" yaml:"unless,omitempty"`
}

// Effect represents the authorization decision.
type Effect string

const (
	// EffectAllow grants access.
	EffectAllow Effect = "allow"

	// EffectDeny denies access.
	EffectDeny Effect = "deny"
)

// Condition represents a policy evaluation condition.
type Condition struct {
	// Expression is the condition expression string
	Expression string `json:"expression,omitempty" yaml:"expression,omitempty"`

	// And contains sub-conditions that must all be true
	And []Condition `json:"and,omitempty" yaml:"and,omitempty"`

	// Or contains sub-conditions where at least one must be true
	Or []Condition `json:"or,omitempty" yaml:"or,omitempty"`

	// Not negates the condition
	Not *Condition `json:"not,omitempty" yaml:"not,omitempty"`
}

// CheckRequest represents an authorization check request.
type CheckRequest struct {
	// Subject is the principal requesting access
	Subject Subject `json:"subject" yaml:"subject"`

	// Action is the operation being performed
	Action string `json:"action" yaml:"action"`

	// Resource is the target resource identifier
	Resource string `json:"resource" yaml:"resource"`

	// Context contains additional request context
	Context map[string]interface{} `json:"context,omitempty" yaml:"context,omitempty"`
}

// Subject represents a principal in an authorization check.
type Subject struct {
	// ID is the subject identifier
	ID string `json:"id" yaml:"id"`

	// Name is the subject's display name
	Name string `json:"name,omitempty" yaml:"name,omitempty"`

	// Email is the subject's email address
	Email string `json:"email,omitempty" yaml:"email,omitempty"`

	// Tenant is the subject's tenant identifier (alias for TenantID for compatibility)
	Tenant string `json:"tenant,omitempty" yaml:"tenant,omitempty"`

	// TenantID is the subject's tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// Roles is the list of role IDs
	Roles []string `json:"roles,omitempty" yaml:"roles,omitempty"`

	// Groups is the list of group IDs
	Groups []string `json:"groups,omitempty" yaml:"groups,omitempty"`

	// Attributes contains custom attributes
	Attributes map[string]interface{} `json:"attributes,omitempty" yaml:"attributes,omitempty"`

	// Attrs is an alias for Attributes for backward compatibility
	Attrs map[string]interface{} `json:"attrs,omitempty" yaml:"attrs,omitempty"`

	// Authenticated indicates whether the subject is authenticated
	Authenticated bool `json:"authenticated" yaml:"authenticated"`

	// MFAVerified indicates whether MFA was verified
	MFAVerified bool `json:"mfa_verified,omitempty" yaml:"mfa_verified,omitempty"`

	// MFATimestamp is the last MFA verification time
	MFATimestamp *time.Time `json:"mfa_timestamp,omitempty" yaml:"mfa_timestamp,omitempty"`

	// LastLogin is the last login timestamp
	LastLogin *time.Time `json:"last_login,omitempty" yaml:"last_login,omitempty"`
}

// CheckResponse represents an authorization check response.
type CheckResponse struct {
	// Allowed indicates whether access is granted
	Allowed bool `json:"allowed" yaml:"allowed"`

	// MatchedPolicy is the name of the policy that matched
	MatchedPolicy string `json:"matched_policy,omitempty" yaml:"matched_policy,omitempty"`

	// MatchedRule is the name of the rule that matched
	MatchedRule string `json:"matched_rule,omitempty" yaml:"matched_rule,omitempty"`

	// EvaluationTimeNs is the evaluation duration in nanoseconds
	EvaluationTimeNs int64 `json:"evaluation_time_ns" yaml:"evaluation_time_ns"`

	// Reason provides additional context for the decision
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// PolicySimulationRequest represents a request to simulate policy evaluation.
type PolicySimulationRequest struct {
	// Policy is the policy to simulate (may be unsaved draft)
	Policy *Policy `json:"policy" yaml:"policy"`

	// CheckRequest is the authorization check to simulate
	CheckRequest *CheckRequest `json:"check_request" yaml:"check_request"`

	// IncludeTrace enables detailed evaluation tracing
	IncludeTrace bool `json:"include_trace,omitempty" yaml:"include_trace,omitempty"`
}

// PolicySimulationResult represents the result of a policy simulation.
type PolicySimulationResult struct {
	// Allowed indicates whether access would be granted
	Allowed bool `json:"allowed" yaml:"allowed"`

	// MatchedPolicy is the name of the policy that matched
	MatchedPolicy string `json:"matched_policy,omitempty" yaml:"matched_policy,omitempty"`

	// MatchedRule is the name of the rule that matched
	MatchedRule string `json:"matched_rule,omitempty" yaml:"matched_rule,omitempty"`

	// EvaluationSteps provides a trace of evaluation steps (when IncludeTrace=true)
	EvaluationSteps []EvaluationStep `json:"evaluation_steps,omitempty" yaml:"evaluation_steps,omitempty"`

	// EvaluationTimeNs is the evaluation duration in nanoseconds
	EvaluationTimeNs int64 `json:"evaluation_time_ns" yaml:"evaluation_time_ns"`

	// Reason provides additional context for the decision
	Reason string `json:"reason,omitempty" yaml:"reason,omitempty"`
}

// EvaluationStep represents a single step in policy evaluation.
type EvaluationStep struct {
	// StepNumber is the ordinal step number
	StepNumber int `json:"step_number" yaml:"step_number"`

	// PolicyName is the policy being evaluated
	PolicyName string `json:"policy_name" yaml:"policy_name"`

	// RuleName is the rule being evaluated (if applicable)
	RuleName string `json:"rule_name,omitempty" yaml:"rule_name,omitempty"`

	// Action describes the evaluation action
	Action string `json:"action" yaml:"action"`

	// Result is the outcome of this step
	Result bool `json:"result" yaml:"result"`

	// Details provides additional context
	Details string `json:"details,omitempty" yaml:"details,omitempty"`
}

// EffectivePermissionsRequest represents a request to calculate effective permissions.
type EffectivePermissionsRequest struct {
	// Subject is the subject to calculate permissions for
	Subject *Subject `json:"subject" yaml:"subject"`

	// ResourcePatterns are resource patterns to check (e.g., "orders://*")
	ResourcePatterns []string `json:"resource_patterns" yaml:"resource_patterns"`

	// Actions are specific actions to check (if empty, all actions are checked)
	Actions []string `json:"actions,omitempty" yaml:"actions,omitempty"`

	// IncludeDenyDetails includes details about denied permissions
	IncludeDenyDetails bool `json:"include_deny_details,omitempty" yaml:"include_deny_details,omitempty"`
}

// EffectivePermissionsResult represents the calculated effective permissions.
type EffectivePermissionsResult struct {
	// Subject is the subject these permissions are for
	Subject *Subject `json:"subject" yaml:"subject"`

	// Permissions maps resource patterns to their permission sets
	Permissions map[string]*PermissionSet `json:"permissions" yaml:"permissions"`

	// TotalPoliciesEvaluated is the number of policies checked
	TotalPoliciesEvaluated int `json:"total_policies_evaluated" yaml:"total_policies_evaluated"`

	// EvaluationTimeNs is the evaluation duration in nanoseconds
	EvaluationTimeNs int64 `json:"evaluation_time_ns" yaml:"evaluation_time_ns"`
}

// PermissionSet represents allowed and denied actions for a resource.
type PermissionSet struct {
	// Allowed are actions that are permitted
	Allowed []string `json:"allowed" yaml:"allowed"`

	// Denied are actions that are explicitly denied
	Denied []string `json:"denied,omitempty" yaml:"denied,omitempty"`

	// Sources maps actions to the policies that grant/deny them
	Sources map[string][]string `json:"sources,omitempty" yaml:"sources,omitempty"`
}

// Credential represents a WebAuthn credential.
type Credential struct {
	// ID is the credential identifier
	ID string `json:"id" yaml:"id"`

	// UserID is the owning user's qrdb Snowflake id
	UserID uint64 `json:"user_id,string" yaml:"user_id"`

	// TenantID is the credential tenant
	TenantID string `json:"tenant_id" yaml:"tenant_id"`

	// PublicKey is the credential public key
	PublicKey []byte `json:"public_key" yaml:"public_key"`

	// AttestationType is the attestation type
	AttestationType string `json:"attestation_type" yaml:"attestation_type"`

	// AAGUID is the authenticator AAGUID
	AAGUID []byte `json:"aaguid,omitempty" yaml:"aaguid,omitempty"`

	// SignCount is the signature counter
	SignCount uint32 `json:"sign_count" yaml:"sign_count"`

	// Created is the credential creation timestamp
	Created time.Time `json:"created" yaml:"created"`

	// LastUsed is the last usage timestamp
	LastUsed *time.Time `json:"last_used,omitempty" yaml:"last_used,omitempty"`
}

// Validate validates a Tenant for correctness.
func (t *Tenant) Validate() error {
	if t.ID == "" {
		return NewIAMError(ErrTenantInvalid, "iam.tenant.validate", "tenant ID is required", nil)
	}
	if t.Name == "" {
		return NewIAMError(ErrTenantInvalid, "iam.tenant.validate", "tenant name is required", nil)
	}
	return nil
}

// Validate validates a User for correctness.
func (u *User) Validate() error {
	if u.ID == 0 {
		return NewIAMError(ErrUserInvalid, "iam.user.validate", "user ID is required", nil)
	}
	if u.Email == "" {
		return NewIAMError(ErrUserInvalid, "iam.user.validate", "user email is required", nil)
	}
	if u.TenantID == "" {
		return NewIAMError(ErrUserInvalid, "iam.user.validate", "user tenant ID is required", nil)
	}
	return nil
}

// PIVThumbprints returns the lookup thumbprints of every PIV-attested cert
// binding. The order mirrors CertBindings so callers can index the storage
// thumbprint->user index by each value. It never returns nil entries.
func (u *User) PIVThumbprints() []string {
	out := make([]string, 0, len(u.CertBindings))
	for i := range u.CertBindings {
		if u.CertBindings[i].Kind == qrdbsdk.CertBindingPIVAttested && u.CertBindings[i].Thumbprint != "" {
			out = append(out, u.CertBindings[i].Thumbprint)
		}
	}
	return out
}

// PIVThumbprint returns the lookup thumbprint of the user's first PIV-attested
// cert binding, or the empty string when the user has none. It is a convenience
// for the bootstrap/SO-bind paths that establish exactly one PIV binding.
func (u *User) PIVThumbprint() string {
	for i := range u.CertBindings {
		if u.CertBindings[i].Kind == qrdbsdk.CertBindingPIVAttested && u.CertBindings[i].Thumbprint != "" {
			return u.CertBindings[i].Thumbprint
		}
	}
	return ""
}

// HasPIVThumbprint reports whether the user holds a non-expired PIV-attested
// binding for the given lookup thumbprint. Expiry is enforced on every
// resolution so a decommissioned credential cannot be replayed after NotAfter.
func (u *User) HasPIVThumbprint(thumbprint string) bool {
	if thumbprint == "" {
		return false
	}
	now := time.Now().UTC()
	for i := range u.CertBindings {
		b := u.CertBindings[i]
		if b.Kind == qrdbsdk.CertBindingPIVAttested && b.Thumbprint == thumbprint && !b.IsExpired(now) {
			return true
		}
	}
	return false
}

// SetPIVThumbprints replaces the user's PIV-attested bindings with one binding
// per supplied non-empty thumbprint, de-duplicating. Blank entries are skipped;
// when every entry is blank all PIV-attested bindings are cleared. Non-PIV
// bindings are preserved untouched. PIV activation supplies both the canonical
// SPKI thumbprint and the legacy hex digest; qrdb's cert-thumbprint index
// resolves both.
func (u *User) SetPIVThumbprints(thumbprints ...string) {
	kept := u.CertBindings[:0:0]
	for i := range u.CertBindings {
		if u.CertBindings[i].Kind != qrdbsdk.CertBindingPIVAttested {
			kept = append(kept, u.CertBindings[i])
		}
	}
	seen := make(map[string]struct{}, len(thumbprints))
	for _, tp := range thumbprints {
		if tp == "" {
			continue
		}
		if _, dup := seen[tp]; dup {
			continue
		}
		seen[tp] = struct{}{}
		kept = append(kept, qrdbsdk.CertBinding{
			Thumbprint: tp,
			Kind:       qrdbsdk.CertBindingPIVAttested,
		})
	}
	if len(kept) == 0 {
		u.CertBindings = nil
		return
	}
	u.CertBindings = kept
}

// SetPIVThumbprint replaces the user's PIV-attested bindings with a single
// binding for thumbprint. A blank thumbprint clears all PIV-attested bindings.
// Non-PIV bindings are preserved untouched. This mirrors the historical
// single-thumbprint setter so bootstrap/activation call sites stay one-liners.
func (u *User) SetPIVThumbprint(thumbprint string) {
	kept := u.CertBindings[:0:0]
	for i := range u.CertBindings {
		if u.CertBindings[i].Kind != qrdbsdk.CertBindingPIVAttested {
			kept = append(kept, u.CertBindings[i])
		}
	}
	if thumbprint != "" {
		kept = append(kept, qrdbsdk.CertBinding{
			Thumbprint: thumbprint,
			Kind:       qrdbsdk.CertBindingPIVAttested,
		})
	}
	if len(kept) == 0 {
		u.CertBindings = nil
		return
	}
	u.CertBindings = kept
}

// SPKIThumbprint returns the thumbprint of the user's first canonical
// SPKI-thumbprint cert binding, or the empty string when the user has none.
// This is the binding the admin-mTLS enrollment path establishes and the one
// qrdb's UserByCertThumbprint index resolves natively.
func (u *User) SPKIThumbprint() string {
	for i := range u.CertBindings {
		if u.CertBindings[i].Kind == qrdbsdk.CertBindingSPKIThumbprint && u.CertBindings[i].Thumbprint != "" {
			return u.CertBindings[i].Thumbprint
		}
	}
	return ""
}

// SetSPKIThumbprint replaces the user's canonical SPKI-thumbprint bindings with
// a single binding for thumbprint. A blank thumbprint clears them. Bindings of
// other kinds (notably PIV-attested) are preserved untouched. The thumbprint
// MUST come from qrdbsdk.ComputeSPKIThumbprint — it is the single canonical
// mTLS identifier and is stable across certificate renewal with the same key.
func (u *User) SetSPKIThumbprint(thumbprint string) {
	kept := u.CertBindings[:0:0]
	for i := range u.CertBindings {
		if u.CertBindings[i].Kind != qrdbsdk.CertBindingSPKIThumbprint {
			kept = append(kept, u.CertBindings[i])
		}
	}
	if thumbprint != "" {
		kept = append(kept, qrdbsdk.CertBinding{
			Thumbprint: thumbprint,
			Kind:       qrdbsdk.CertBindingSPKIThumbprint,
		})
	}
	if len(kept) == 0 {
		u.CertBindings = nil
		return
	}
	u.CertBindings = kept
}

// Validate validates a Role for correctness.
func (r *Role) Validate() error {
	if r.ID == "" {
		return NewIAMError(ErrRoleInvalid, "iam.role.validate", "role ID is required", nil)
	}
	if r.Name == "" {
		return NewIAMError(ErrRoleInvalid, "iam.role.validate", "role name is required", nil)
	}
	if r.TenantID == "" {
		return NewIAMError(ErrRoleInvalid, "iam.role.validate", "role tenant ID is required", nil)
	}
	return nil
}

// Validate validates a Policy for correctness.
func (p *Policy) Validate() error {
	if p.APIVersion == "" {
		return NewIAMError(ErrPolicyInvalid, "iam.policy.validate", "policy apiVersion is required", nil)
	}
	if p.Kind != "Policy" {
		return NewIAMError(ErrPolicyInvalid, "iam.policy.validate", "policy kind must be 'Policy'", nil)
	}
	if p.Metadata.Name == "" {
		return NewIAMError(ErrPolicyInvalid, "iam.policy.validate", "policy name is required", nil)
	}
	if p.Metadata.Tenant == "" {
		return NewIAMError(ErrPolicyInvalid, "iam.policy.validate", "policy tenant is required", nil)
	}
	if len(p.Spec.Rules) == 0 {
		return NewIAMError(ErrPolicyInvalid, "iam.policy.validate", "policy must have at least one rule", nil)
	}
	for i, rule := range p.Spec.Rules {
		if err := rule.Validate(); err != nil {
			return NewIAMError(ErrPolicyInvalid, "iam.policy.validate", "invalid rule at index", err).
				WithContext("index", i).
				WithContext("rule_name", rule.Name)
		}
	}
	return nil
}

// Validate validates a Rule for correctness.
func (r *Rule) Validate() error {
	if r.Name == "" {
		return NewIAMError(ErrRuleInvalid, "iam.rule.validate", "rule name is required", nil)
	}
	if r.Effect != EffectAllow && r.Effect != EffectDeny {
		return NewIAMError(ErrRuleInvalid, "iam.rule.validate", "rule effect must be 'allow' or 'deny'", nil)
	}
	if len(r.Actions) == 0 {
		return NewIAMError(ErrRuleInvalid, "iam.rule.validate", "rule must have at least one action", nil)
	}
	if len(r.Resources) == 0 {
		return NewIAMError(ErrRuleInvalid, "iam.rule.validate", "rule must have at least one resource", nil)
	}
	if err := ValidateConditions("when", r.When); err != nil {
		return err
	}
	return ValidateConditions("unless", r.Unless)
}

// ValidateConditions refuses any condition that nests And, Or or Not.
// go-qrdb's ABAC condition carries only a flat Expression, so a nested
// condition could not be enforced; it is refused at write time instead of
// being silently dropped. Field names the rule's condition list for the
// error ("when" or "unless").
func ValidateConditions(field string, conds []Condition) error {
	for i, c := range conds {
		if len(c.And) > 0 || len(c.Or) > 0 || c.Not != nil {
			return NewIAMError(ErrConditionNested, "iam.condition.validate",
				"nested conditions are not supported",
				&NestedConditionError{Field: field, Index: i})
		}
	}
	return nil
}

// IsExpired checks if a session has expired.
func (s *Session) IsExpired() bool {
	now := time.Now()
	if now.After(s.Expires) {
		return true
	}
	if s.IdleTimeout > 0 && now.Sub(s.LastActivity) > s.IdleTimeout {
		return true
	}
	return false
}

// IsExpired checks if a token has expired.
func (t *Token) IsExpired() bool {
	return time.Now().After(t.Expires)
}

// PolicyEngineOptions configures the policy engine.
type PolicyEngineOptions struct {
	Storage       PolicyStorage
	Cache         *PolicyCache
	CacheTTL      time.Duration
	MaxCacheSize  int
	EnableMetrics bool

	// RBACAuthorizer is the SINGLE decision point every Evaluate/Check call
	// delegates to (P6.1/P6.1-end-state of the unified login-plane plan):
	// on a composed node this is qrdb's own RBAC-authorizer-decorated-with-
	// live-reloaded-ABAC (abac.Decorator over auth.Authorizer, reached via
	// the embedded qrdb node's RBACClient), so ABAC and RBAC are evaluated
	// together as ONE decision by qrdb, not layered locally. Nil restores
	// the historical default-deny (embedded/test contexts with no qrdb node
	// composed).
	RBACAuthorizer RBACAuthorizer

	// ABACSync is the seam PolicyEngine drives after every policy CRUD
	// mutation (Create/Update/Delete), and once at startup, to keep qrdb's
	// live ABAC decorator synchronized with the FULL current policy store
	// across every tenant. Nil disables live-reload synchronization
	// (embedded/test contexts with no qrdb node composed) -- CRUD still
	// mutates storage, it just does not push a Reload anywhere.
	ABACSync ABACSync

	// Simulator is the seam PolicyEngine.SimulatePolicy delegates to for
	// evaluating a single draft policy against a check request in
	// isolation, without persisting the policy or mutating the live
	// enforcement state. Nil disables SimulatePolicy (returns
	// ErrSimulatorUnavailable).
	Simulator PolicySimulator

	// Repair lets the system admin repair a held tenant's stored policies
	// one at a time (see PolicyRepair). Nil keeps every write strict: a
	// failed sync always rejects it.
	Repair PolicyRepair
}
