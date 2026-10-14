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

package iam

import (
	"context"
	"crypto"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// AccessTokenJWTType is the RFC 9068 §2.1 media type stamped into the JOSE "typ"
// header of every OAuth2 JWT access token. A resource server uses it to reject a
// JWT that is not an access token (e.g. an OIDC ID token replayed as a bearer),
// preventing token-type confusion. It is set ONLY on access tokens; ID tokens
// and refresh tokens are unaffected.
const AccessTokenJWTType = "at+jwt"

// PermissionResolver returns the qrdb RBAC client the token service consults to
// resolve a subject's effective permissions at access-token issuance. The
// resolver indirection late-binds the qrdb client (constructed during server
// lifecycle, after the OIDC provider) and mirrors the RoleClientResolver seam:
// the iam package depends only on the narrow qrdb transport RBAC surface, not
// the full QRDBClientProvider. A nil return (qrdb not yet wired) leaves the scp
// claim deriving from the OIDC scope, preserving today's behavior.
type PermissionResolver interface {
	// RBACClient returns the qrdb RBAC client, or nil when qrdb is unavailable.
	RBACClient() qrdbtransport.RBACClient
}

// TokenService handles token generation, validation, and storage.
type TokenService struct {
	storage    TokenStorage
	signingKey crypto.Signer
	// signingAlg is the JWS algorithm used to sign tokens. It is derived from
	// the key manager (ES256) when present, otherwise from config.
	signingAlg string
	// currentKID is the kid stamped onto the "kid" JWT header so verifiers can
	// select the matching public key from a multi-kid JWKS. Empty when no key
	// manager is configured (legacy single-key path).
	currentKID string
	// keyManager, when set, is the authoritative xkms-backed source of signing
	// keys; the token service re-reads the current key/kid per sign so rotation
	// takes effect without rebuilding the service.
	keyManager *SigningKeyManager
	// tenantKeys, when set, resolves the signing key by the token's tenant: a
	// dedicated tenant is signed with its own per-tenant key (own kid) under
	// the single broker issuer, while every other tenant uses keyManager (the
	// global key). When nil all tokens use keyManager (single-key behavior).
	tenantKeys *TenantSigningKeyResolver
	config     *OIDCConfig
	// permResolver, when set, resolves the subject's effective qrdb permissions
	// at access-token issuance so the scp claim carries the user's RBAC
	// authority (colon "resource:action" form) rather than the OAuth-requested
	// scope passthrough. When nil (qrdb not wired) scp falls back to the OIDC
	// scope, preserving today's behavior.
	permResolver PermissionResolver
}

// NewTokenService creates a new token service.
func NewTokenService(storage TokenStorage, signingKey crypto.Signer, config *OIDCConfig) *TokenService {
	alg := config.SigningAlgorithm
	return &TokenService{
		storage:    storage,
		signingKey: signingKey,
		signingAlg: alg,
		config:     config,
	}
}

// signingMaterial returns the signer, algorithm, and kid to use for signing a
// token for tenantID. When a per-tenant resolver is configured it selects the
// dedicated tenant's key (own kid) or the global key for a non-dedicated
// tenant; a dedicated tenant whose key cannot be built FAILS CLOSED (the error
// is returned rather than downgrading to the global key). When no resolver is
// configured the static keyManager (or legacy SigningKey) is used.
//
// A key manager is consulted live so a rotation is picked up immediately.
func (s *TokenService) signingMaterial(ctx context.Context, tenantID string) (crypto.Signer, string, string, error) {
	km := s.keyManager
	if s.tenantKeys != nil {
		resolved, err := s.tenantKeys.ManagerFor(ctx, tenantID)
		if err != nil {
			return nil, "", "", err
		}
		km = resolved
	}
	if km != nil {
		kid, signer, alg := km.Current()
		if signer != nil {
			return signer, alg, kid, nil
		}
	}
	alg := s.signingAlg
	if alg == "" {
		alg = s.config.SigningAlgorithm
	}
	return s.signingKey, alg, s.currentKID, nil
}

// GenerateAccessToken generates a new JWT access token.
//
// It mints claims.SessionID when the caller left it empty, and WRITES IT BACK
// so the caller can freeze the same value into the session it is establishing
// (see Claims.SessionID). Minting here rather than at each login site is what
// makes the claim's guarantee structural: no issuance path can ship a token
// without a sid, and no two logins can share one by omission. A renewal is the
// one caller that supplies the value — it replays its session's — so the only
// way to get sid wrong is to forget to replay it, which yields a NEW session
// identifier and reads to a client as a new session. That is the safe
// direction: the failure costs a client its cache, it does not let one login's
// cache stay in scope under another's.
func (s *TokenService) GenerateAccessToken(ctx context.Context, claims *Claims) (string, error) {
	if claims.SessionID == "" {
		sessionID, err := generateSessionID()
		if err != nil {
			return "", err
		}
		claims.SessionID = sessionID
	}

	// Build claims map for XKMS JWT signing
	jwtClaims := xkms.JWTClaims{
		"iss":          claims.Issuer,
		"sub":          claims.Subject,
		"aud":          claims.Audience,
		"exp":          claims.ExpiresAt.Unix(),
		"iat":          claims.IssuedAt.Unix(),
		"nbf":          claims.NotBefore.Unix(),
		"jti":          claims.JTI,
		"sid":          claims.SessionID,
		"tenant_id":    claims.TenantID,
		"roles":        claims.Roles,
		"groups":       claims.Groups,
		"email":        claims.Email,
		"name":         claims.Name,
		"scope":        claims.Scope,
		"client_id":    claims.ClientID,
		"mfa_verified": claims.MFAVerified,
		"auth_method":  claims.AuthMethod,
	}

	// The principal-identity claims are emitted only when set, so a token never
	// carries an empty uid or ptyp that a verifier could mistake for a present
	// one. Their absence is what marks a token as pre-dating this contract.
	if claims.UserID != 0 {
		jwtClaims["uid"] = strconv.FormatUint(claims.UserID, 10)
	}
	if claims.PrincipalType != "" {
		jwtClaims["ptyp"] = claims.PrincipalType
	}

	// The attestation claims travel with the token that AUTHORIZES, not only
	// with the ID token that describes the login. A resource server deciding
	// whether to admit an operation reserved for hardware-backed credentials
	// reads them off this bearer; see Claims.AttestLevel for why the ID token
	// alone is the wrong place for them.
	//
	// Each is emitted only when the login actually established it, so an
	// unattested session's token stays silent rather than carrying an empty
	// value a verifier could mistake for an answer.
	if claims.AttestLevel != "" {
		jwtClaims["attest_level"] = claims.AttestLevel
	}
	if claims.AttestAAGUID != "" {
		jwtClaims["attest_aaguid"] = claims.AttestAAGUID
	}

	// Emit scp (array-form scope) for the platform access-token contract. The
	// scp claim carries the subject's qrdb-RESOLVED effective permissions in
	// colon "resource:action" form (e.g. "keys:get") so the offline platform
	// validator's exact-match scope check is a faithful RBAC decision. The OIDC
	// "scope" claim (above) keeps the requested OIDC scopes (openid/profile/...).
	//
	// Precedence: an explicitly pre-populated claims.SCP wins (callers that
	// already computed the perm set); otherwise the qrdb resolver computes the
	// union of the subject's role permissions; if the resolver is unavailable
	// (qrdb not wired) scp falls back to the OIDC scope so behavior is unchanged.
	switch {
	case len(claims.SCP) > 0:
		jwtClaims["scp"] = claims.SCP
	default:
		if perms, err := s.resolveEffectivePermissions(ctx, claims); err != nil {
			return "", err
		} else if len(perms) > 0 {
			jwtClaims["scp"] = perms
		} else if claims.Scope != "" {
			jwtClaims["scp"] = parseScopes(claims.Scope)
		}
	}

	// Emit cnf for sender-constrained tokens (DPoP jkt or mTLS x5t#S256). The
	// claim is omitted entirely for bearer tokens so back-compat is preserved.
	if claims.Confirmation != nil {
		cnf := map[string]interface{}{}
		if claims.Confirmation.Jkt != "" {
			cnf["jkt"] = claims.Confirmation.Jkt
		}
		if claims.Confirmation.X5TS256 != "" {
			cnf["x5t#S256"] = claims.Confirmation.X5TS256
		}
		if len(cnf) > 0 {
			jwtClaims["cnf"] = cnf
		}
	}

	// Sign token using XKMS JWT, stamping the current kid so verifiers can
	// select the matching public key from the multi-kid JWKS. The signer is
	// selected by the token's tenant: a dedicated tenant is signed with its
	// own key (own kid), and a dedicated tenant whose key is unavailable fails
	// closed here rather than downgrading to the global key.
	signer, alg, kid, err := s.signingMaterial(ctx, claims.TenantID)
	if err != nil {
		return "", dberrors.Wrapf(err, dberrors.ErrInternal, "iam.TokenService.GenerateAccessToken", "failed to resolve signing key")
	}
	tokenString, err := xkms.JWTSignWithSignerKIDTyp(signer, jwtClaims, alg, kid, AccessTokenJWTType)
	if err != nil {
		return "", dberrors.Wrapf(err, dberrors.ErrInternal, "iam.TokenService.GenerateAccessToken", "failed to sign token")
	}

	// Store token metadata
	tokenMeta := &Token{
		JTI:       claims.JTI,
		Subject:   claims.Subject,
		TenantID:  claims.TenantID,
		Scopes:    parseScopes(claims.Scope),
		Expires:   claims.ExpiresAt.Time,
		Issued:    claims.IssuedAt.Time,
		TokenType: TokenTypeAccess,
		ClientID:  claims.ClientID,
	}

	// Store under the TOKEN's tenant, not whatever tenant happens to be on the
	// caller's context. The revocation reader is tenant-scoped — GetToken
	// resolves tokenKey(contextTenant(ctx), jti) — and a missing record
	// reads as "not revoked", so a write that lands in a different store than
	// the read makes logout silently ineffective until the token expires. The
	// ceremony is exactly that case: it issues from the login request's
	// context, which carries no tenant, while the tenant's own store is where
	// revocation later looks.
	if err := s.storage.StoreToken(withTokenTenant(ctx, claims.TenantID), tokenMeta); err != nil {
		return "", dberrors.Wrapf(err, dberrors.ErrStorage, "iam.TokenService.GenerateAccessToken", "failed to store token")
	}

	return tokenString, nil
}

// resolveEffectivePermissions returns the subject's qrdb-resolved effective
// permissions in colon "resource:action" form (e.g. "keys:get"), to be emitted
// as the scp claim. It calls qrdb RBAC().ListPermissions with an Identity built
// from the token's subject/tenant/roles and returns the union of granted
// permission Names. When no resolver is configured, or the resolver yields a
// nil client (qrdb not yet wired), it returns (nil, nil) so the caller falls
// back to the OIDC scope without failing issuance. A real RBAC error is
// surfaced typed (the token must not ship with an unauthorized perm set).
//
// The resolve runs on a context scoped to the TOKEN's tenant, for the same
// reason StoreToken does above — but through qrdb's OWN tenant binding, which
// is a different context key from quicbaas's (withTokenTenant / iam's
// tenantContextKey addresses quicbaas's token index; qrdbsdk.ContextWithTenant
// addresses qrdb's store resolver). Getting that distinction wrong is silent:
// the wrong key sets a value nothing downstream reads, and the resolve still
// runs in the system tenant.
//
// It matters because qrdb resolves the role store from the CONTEXT, never from
// Identity.TenantID — ListPermissions passes the identity's roles straight to
// roleSvc.ResolvePermissions and consults its TenantID for nothing. The
// unified login ceremony issues from the login request's context, and that
// request is unauthenticated, so it carries no tenant at all. Every role id a
// non-system tenant's user holds then misses in the system store,
// ListPermissions answers "role: not found: <uuid>", and issuance fails: the
// user authenticates with a verified factor and the ceremony answers 401
// anyway. That is #529 — no user in a non-system tenant could log in.
//
// Scoping here rather than at each caller keeps every issuance path — unified
// ceremony, PIV login, federated callback, refresh — resolving in the tenant
// whose roles the claims actually name.
func (s *TokenService) resolveEffectivePermissions(ctx context.Context, claims *Claims) ([]string, error) {
	if s.permResolver == nil {
		return nil, nil
	}
	rbac := s.permResolver.RBACClient()
	if rbac == nil {
		return nil, nil
	}
	identity := &qrdbtransport.Identity{
		Subject:  claims.Subject,
		TenantID: claims.TenantID,
		Roles:    claims.Roles,
	}
	infos, err := rbac.ListPermissions(withTokenTenant(ctx, claims.TenantID), identity)
	if err != nil {
		return nil, dberrors.Wrapf(err, dberrors.ErrInternal, "iam.TokenService.resolveEffectivePermissions", "failed to resolve effective permissions")
	}
	if len(infos) == 0 {
		return nil, nil
	}
	perms := make([]string, 0, len(infos))
	seen := make(map[string]struct{}, len(infos))
	for _, info := range infos {
		if info.Name == "" {
			continue
		}
		if _, dup := seen[info.Name]; dup {
			continue
		}
		seen[info.Name] = struct{}{}
		perms = append(perms, info.Name)
	}
	return perms, nil
}

// GenerateIDToken generates a new JWT ID token.
func (s *TokenService) GenerateIDToken(ctx context.Context, claims *IDTokenClaims) (string, error) {
	// Build claims map for XKMS JWT signing
	jwtClaims := xkms.JWTClaims{
		"iss":            claims.Issuer,
		"sub":            claims.Subject,
		"aud":            claims.Audience,
		"exp":            claims.ExpiresAt.Unix(),
		"iat":            claims.IssuedAt.Unix(),
		"auth_time":      claims.AuthTime.Unix(),
		"email":          claims.Email,
		"email_verified": claims.EmailVerified,
		"name":           claims.Name,
		"tenant_id":      claims.TenantID,
		"roles":          claims.Roles,
		"groups":         claims.Groups,
		"mfa_verified":   claims.MFAVerified,
		"auth_method":    claims.AuthMethod,
	}

	if claims.Nonce != "" {
		jwtClaims["nonce"] = claims.Nonce
	}
	if len(claims.AMR) > 0 {
		jwtClaims["amr"] = claims.AMR
	}
	if claims.ACR != "" {
		jwtClaims["acr"] = claims.ACR
	}
	if claims.AttestAAGUID != "" {
		jwtClaims["attest_aaguid"] = claims.AttestAAGUID
	}
	if claims.AttestLevel != "" {
		jwtClaims["attest_level"] = claims.AttestLevel
	}

	// Sign token using XKMS JWT, stamping the current kid (per-tenant key
	// selection mirrors GenerateAccessToken; dedicated tenants fail closed).
	signer, alg, kid, err := s.signingMaterial(ctx, claims.TenantID)
	if err != nil {
		return "", dberrors.Wrapf(err, dberrors.ErrInternal, "iam.TokenService.GenerateIDToken", "failed to resolve signing key")
	}
	tokenString, err := xkms.JWTSignWithSignerKID(signer, jwtClaims, alg, kid)
	if err != nil {
		return "", dberrors.Wrapf(err, dberrors.ErrInternal, "iam.TokenService.GenerateIDToken", "failed to sign ID token")
	}

	return tokenString, nil
}

// GenerateRefreshToken mints an opaque refresh credential for a session.
//
// It delegates to issueRefreshCredential, the single implementation shared with
// the login-ceremony path, so every refresh token in the system is stored
// hashed-by-value (and is therefore actually resolvable by GetTokenByValue) and
// carries the session context a renewal replays.
func (s *TokenService) GenerateRefreshToken(ctx context.Context, opts SessionRefreshOptions) (string, error) {
	value, _, err := s.issueRefreshCredential(ctx, opts, time.Time{})
	if err != nil {
		return "", err
	}
	return value, nil
}

// GenerateAuthCode generates a new authorization code.
func (s *TokenService) GenerateAuthCode(ctx context.Context, req *AuthorizeRequest, userID uint64) (string, error) {
	// Generate secure random code
	codeBytes, err := xkms.Rand(32)
	if err != nil {
		return "", dberrors.Wrapf(err, dberrors.ErrInternal, "iam.TokenService.GenerateAuthCode", "failed to generate random code")
	}
	code := base64.RawURLEncoding.EncodeToString(codeBytes)

	now := time.Now()

	// Store authorization code
	authCode := &AuthCodeData{
		Code:                code,
		UserID:              userID,
		TenantID:            req.TenantID,
		ClientID:            req.ClientID,
		RedirectURI:         req.RedirectURI,
		Scope:               req.Scope,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: req.CodeChallengeMethod,
		Nonce:               req.Nonce,
		Resource:            req.Resource, // RFC 8707 Resource Indicators
		ExpiresAt:           now.Add(s.config.AuthCodeTTL),
		CreatedAt:           now,
	}

	if err := s.storage.StoreAuthCode(ctx, authCode); err != nil {
		return "", dberrors.Wrapf(err, dberrors.ErrStorage, "iam.TokenService.GenerateAuthCode", "failed to store authorization code")
	}

	return code, nil
}

// ValidateAuthCode validates an authorization code and returns its data.
// The code is deleted after successful validation (one-time use).
func (s *TokenService) ValidateAuthCode(ctx context.Context, code string) (*AuthCodeData, error) {
	// Get authorization code
	authCode, err := s.storage.GetAuthCode(ctx, code)
	if err != nil {
		return nil, dberrors.NotFound("iam.TokenService.ValidateAuthCode", "authorization code")
	}

	// Check if expired
	if authCode.IsExpired() {
		_ = s.storage.DeleteAuthCode(ctx, code)
		return nil, dberrors.New(dberrors.ErrForbidden, "iam.TokenService.ValidateAuthCode", "authorization code expired")
	}

	// Delete code (one-time use)
	if err := s.storage.DeleteAuthCode(ctx, code); err != nil {
		return nil, dberrors.Wrapf(err, dberrors.ErrInternal, "iam.TokenService.ValidateAuthCode", "failed to consume authorization code")
	}

	return authCode, nil
}

// RevokeToken revokes a token by its JTI or value.
func (s *TokenService) RevokeToken(ctx context.Context, tokenValue string) error {
	// Try to parse as JWT first (without verification to extract JTI).
	claims, err := xkms.JWTParse(tokenValue)
	if err == nil && claims != nil {
		// The token tenant is authoritative from the tenant_id claim. Inject
		// it into the context so the tenant-scoped token index is resolved
		// (revocation already parses the JWT, so this is free).
		if tid, ok := claims["tenant_id"].(string); ok && tid != "" {
			ctx = tenantctx.WithTenant(ctx, tid)
		}
		// Extract JTI from JWT
		if jti, ok := claims["jti"].(string); ok {
			return s.storage.RevokeToken(ctx, jti)
		}
	}

	// Try as refresh token (look up by value)
	storedToken, err := s.storage.GetTokenByValue(ctx, tokenValue)
	if err == nil && storedToken != nil {
		return s.storage.RevokeToken(ctx, storedToken.JTI)
	}

	return dberrors.NotFound("iam.TokenService.RevokeToken", "token")
}

// Helper functions

// tokenIDEntropyBytes is the size of the token identifier (the jti claim). 128
// bits makes a collision between two issued tokens unreachable, which is the
// property revocation depends on.
const tokenIDEntropyBytes = 16

// tokenIDEntropy is the randomness generateJTI draws on.
//
// It is a variable for the same single reason sessionIDEntropy below is:
// proving that issuance FAILS CLOSED when the CSPRNG does requires making the
// CSPRNG fail, and xkms.Rand draws on process-global RNG state that a unit test
// must not reach in and break for every other test in the package. Nothing
// outside a test reassigns it.
var tokenIDEntropy = xkms.Rand

// generateJTI returns the identifier of one issued token (the jti claim), or an
// error when the CSPRNG cannot supply one.
//
// It used to answer a CSPRNG failure with time.Now().UnixNano() and report
// success (#372). jti is this provider's REVOCATION HANDLE — IsTokenRevoked
// resolves by it, and logout and RevokeToken are keyed on it — so a
// timestamp-derived jti is a revocation record an attacker can guess, shipped
// with nothing in the response to say generation was degraded. An attacker
// cannot mint a signed token from it, which makes this weaker than a
// predictable session identifier; issuing a security-relevant identifier from a
// clock while reporting success is the wrong shape regardless. There is no
// degraded value worth issuing, so issuance fails and every caller propagates
// it — the same answer the neighbouring refresh-secret and session-identifier
// paths already give.
func generateJTI() (string, error) {
	b, err := tokenIDEntropy(tokenIDEntropyBytes)
	if err != nil {
		return "", &TokenIDEntropyError{Cause: err}
	}
	return hex.EncodeToString(b), nil
}

// sessionIDEntropyBytes is the size of the random session identifier. 128 bits
// makes a collision between two logins unreachable, which is the only property
// that matters here.
const sessionIDEntropyBytes = 16

// sessionIDEntropy is the randomness generateSessionID draws on.
//
// It is a variable for exactly one reason: proving that issuance FAILS CLOSED
// when the CSPRNG does requires making the CSPRNG fail, and xkms.Rand draws on
// process-global RNG state that a unit test must not reach in and break for
// every other test in the package. Nothing outside a test reassigns it.
var sessionIDEntropy = xkms.Rand

// generateSessionID returns the identifier of one login session (the sid claim),
// or an error when the CSPRNG cannot supply one.
//
// Kept separate from generateJTI, which it otherwise resembles, so that the two
// identifiers draw on independent seams a test can fail one at a time. Both now
// refuse rather than degrade: generateJTI did fall back to a timestamp until
// #372, and this comment used to record that difference. sid's whole job is to
// answer whether two credentials belong to the same login, and a predictable
// value answers it wrongly in the one direction that matters — two logins
// minted close enough together would share an identifier, and the first login's
// cached data would stay in scope under the second. There is no degraded value
// worth issuing, so issuance fails instead and the caller surfaces it.
func generateSessionID() (string, error) {
	b, err := sessionIDEntropy(sessionIDEntropyBytes)
	if err != nil {
		return "", &SessionIDEntropyError{Cause: err}
	}
	return hex.EncodeToString(b), nil
}

// parseScopes converts a space-separated scope string to a slice.
func parseScopes(scope string) []string {
	if scope == "" {
		return []string{}
	}
	result := []string{}
	current := ""
	for i := 0; i < len(scope); i++ {
		if scope[i] == ' ' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(scope[i])
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

// HardwareTokenSet is the set of tokens issued after a successful hardware-attested login.
type HardwareTokenSet struct {
	// AccessToken is the signed JWT access token.
	AccessToken string
	// IDToken is the signed OIDC ID token containing amr/acr/attest claims.
	IDToken string
	// RefreshToken is the opaque refresh token.
	RefreshToken string
	// ExpiresIn is the access token lifetime in seconds.
	ExpiresIn int64
	// TokenType is always "Bearer".
	TokenType string
}

// HardwareTokenOptions controls the contents of tokens issued for hardware-attested logins.
type HardwareTokenOptions struct {
	// User is the authenticated IAM user.
	User *User
	// AMR is the list of authentication method references (RFC 8176).
	// E.g. ["webauthn","hwk"] or ["smartcard","mfa"].
	AMR []string
	// ACR is the authentication context class reference.
	// Use ACRHardwareAttested for hardware-backed credentials — and only when
	// Attestation actually proves hardware; see below.
	ACR string
	// Attestation is the VERIFIED provenance of the credential that
	// authenticated this login, produced by a verifier in this package
	// (WebAuthnAttestationGate for FIDO2, PIVAttestationVerifier for
	// smartcards). Its fields are unexported, so a handler cannot mint one:
	// the only way to obtain a hardware result is to have proven hardware.
	//
	// The claims `attest_level`, `attest_aaguid` and `mfa_verified` are derived
	// from it rather than supplied. They used to be caller-supplied strings, and both
	// production call sites passed the literal "hardware" unconditionally —
	// the PIV login handler for any TLS client certificate, and self-service
	// registration for any WebAuthn credential including a virtual one. Tokens
	// asserted hardware that nothing had ever checked.
	//
	// Nil is legal and means "this login proved nothing about storage": the
	// claims are omitted and ACRHardwareAttested is refused.
	Attestation *AttestationResult
	// ClientID is the OAuth2 client on behalf of which tokens are issued.
	// May be empty when no client_id is applicable (direct hardware login).
	ClientID string
	// Scope is the space-separated list of granted scopes (defaults to "openid profile email").
	Scope string
	// groups is the `groups` claim: the names of the roles User is bound to.
	// OIDCProvider.IssueHardwareTokens resolves it from the identity store;
	// it is unexported so no caller can assert a group the store does not bind.
	groups []string
}

// ACRHardwareAttested is the ACR value for hardware-attested authentication.
const ACRHardwareAttested = "urn:quicbaas:auth:hardware-attested"

// IssueHardwareTokens mints an access token, ID token, and refresh token for a
// successfully hardware-attested login (WebAuthn FIDO2 or PIV smartcard).
func (s *TokenService) IssueHardwareTokens(ctx context.Context, opts HardwareTokenOptions) (*HardwareTokenSet, error) {
	if opts.User == nil {
		return nil, dberrors.New(dberrors.ErrInvalidInput, "iam.TokenService.IssueHardwareTokens", "user is required")
	}

	// A token may claim hardware only when a verifier in this package
	// established it. Refusing here — rather than trusting the handler that set
	// the claim — is what makes it mean something to every consumer
	// downstream: an access token asserting hardware is now evidence, not a
	// label.
	//
	// BOTH the ACR and the AMR are checked. The ACR is the obvious one, but
	// RFC 8176 amr values carry the same assertion in a different field: "hwk"
	// is proof-of-possession of a HARDWARE key and "sc" is a smart card. A
	// caller that set only those would produce a token without the
	// hardware-attested ACR that still tells every relying party it holds a
	// hardware credential. This function is the issuance boundary, so the rule
	// belongs here and not in the handlers that happen to derive it correctly
	// today.
	if err := requireProvenHardware(opts); err != nil {
		return nil, err
	}

	// MFA assurance is DERIVED from the same evidence, never asserted by the
	// caller. The rule, ratified on go-quicbaas#416 (option 1): a credential
	// whose attestation proves hardware satisfies mfa_verified; anything less
	// does not. Hardware attestation proves where the key lives — on an
	// authenticator it cannot be copied off — and that possession is accepted
	// as the second factor this login ceremony rests on. An unattested
	// credential — the certificate an operator admits through
	// iam.piv_login.allow_unattested — establishes possession of an ordinary
	// software key and nothing else, so it proves ONE factor.
	//
	// The rule does not yet look at evidence of a knowledge or inherence factor
	// (the WebAuthn user-verification flag, the PIV PIN policy):
	// AttestationResult does not carry it. Recording that evidence is the
	// follow-up on go-quicbaas#416.
	// TestIssueHardwareTokens_ProvenHardwareSatisfiesMFA pins the rule.
	//
	// This used to be the literal true, on every path. A software-held TLS key
	// produced an ID token asserting mfa_verified, and the same literal was
	// frozen into the refresh session, so every renewal for the whole refresh
	// window repeated the assertion. A relying party enforcing an MFA policy
	// admitted the weakest login this endpoint can issue.
	mfaVerified := opts.Attestation.IsHardware()

	now := time.Now()
	scope := opts.Scope
	if scope == "" {
		scope = "openid profile email"
	}
	issuer := s.config.Issuer

	accessJTI, err := generateJTI()
	if err != nil {
		return nil, err
	}

	accessClaims := &Claims{
		Issuer:    issuer,
		Subject:   strconv.FormatUint(opts.User.ID, 10),
		Audience:  []string{issuer},
		ExpiresAt: UnixTime{Time: now.Add(s.config.AccessTokenTTL)},
		IssuedAt:  UnixTime{Time: now},
		NotBefore: UnixTime{Time: now},
		JTI:       accessJTI,
		TenantID:  opts.User.TenantID,
		UserID:    opts.User.ID,
		Roles:     opts.User.Roles,
		Groups:    opts.groups,
		Email:     opts.User.Email,
		Name:      opts.User.Name,
		Scope:     scope,
		ClientID:  opts.ClientID,
		AMR:       opts.AMR,
		ACR:       opts.ACR,
		// Carried on the bearer and not only on the ID token: renewal replays
		// it from the frozen session, so a bearer that omitted it would gain
		// the claim at the first refresh and read as an UPGRADE in assurance
		// that no ceremony granted.
		MFAVerified: mfaVerified,
		// Derived from the verified evidence, exactly as the ID token's are.
		// The bearer is what policy is applied to, so it has to say what was
		// proven or the whole verification above buys nothing downstream.
		AttestLevel:  string(opts.Attestation.Level()),
		AttestAAGUID: opts.Attestation.AAGUID(),
	}

	accessToken, err := s.GenerateAccessToken(ctx, accessClaims)
	if err != nil {
		return nil, err
	}

	idClaims := &IDTokenClaims{
		Issuer:        issuer,
		Subject:       strconv.FormatUint(opts.User.ID, 10),
		Audience:      []string{issuer},
		ExpiresAt:     UnixTime{Time: now.Add(s.config.IDTokenTTL)},
		IssuedAt:      UnixTime{Time: now},
		AuthTime:      UnixTime{Time: now},
		Email:         opts.User.Email,
		EmailVerified: true,
		Name:          opts.User.Name,
		TenantID:      opts.User.TenantID,
		Roles:         opts.User.Roles,
		Groups:        opts.groups,
		MFAVerified:   mfaVerified,
		AMR:           opts.AMR,
		ACR:           opts.ACR,
		AttestAAGUID:  opts.Attestation.AAGUID(),
		AttestLevel:   string(opts.Attestation.Level()),
	}

	idToken, err := s.GenerateIDToken(ctx, idClaims)
	if err != nil {
		return nil, err
	}

	// The refresh credential freezes the hardware login's authentication
	// context so a renewed access token keeps its amr/acr assurance instead of
	// silently degrading to an unattested session.
	//
	// UserID is frozen alongside it for the same reason the access token above
	// carries uid: renewal decides liveness from THIS session and re-mints from
	// these fields, so omitting the immutable handle would mint a renewed token
	// carrying neither uid nor a service marker — which principalLive DENIES,
	// killing the hardware session at its next liveness check.
	//
	// THE BYPASS THIS ONCE DESCRIBED IS CLOSED, and describing it in the
	// present tense read as a live hole. Before #390 a uid-less token was
	// resolved from its subject string, a deleted account resolved to nobody,
	// and that was read as a service principal — so a hardware session outlived
	// its user for the whole refresh lifetime. That is the past-tense account of
	// what TestHardwareSessionDoesNotOutliveADeletedUser guards; the guessing
	// path is gone and refusal is what a uid-less renewal gets now (#278).
	refreshToken, err := s.GenerateRefreshToken(ctx, SessionRefreshOptions{
		Subject:  strconv.FormatUint(opts.User.ID, 10),
		TenantID: opts.User.TenantID,
		ClientID: opts.ClientID,
		Scopes:   parseScopes(scope),
		Session: TokenSessionContext{
			// The sid GenerateAccessToken minted for the bearer above. Freezing
			// it is what makes a renewal of THIS hardware login carry the same
			// session identifier the login handed out.
			SessionID: accessClaims.SessionID,
			UserID:    opts.User.ID,
			Roles:     opts.User.Roles,
			Groups:    opts.groups,
			Audience:  []string{issuer},
			Email:     opts.User.Email,
			Name:      opts.User.Name,
			// Frozen honestly: a renewal rebuilds the bearer entirely from this
			// record, so a true here would resurrect the assertion the login
			// never earned for the whole refresh lifetime.
			MFAVerified: mfaVerified,
			AMR:         opts.AMR,
			ACR:         opts.ACR,
			// Frozen from the same verified evidence the bearer above carries,
			// so a renewal of this login asserts the provenance the login
			// proved instead of dropping to an unattested session.
			AttestLevel:  accessClaims.AttestLevel,
			AttestAAGUID: accessClaims.AttestAAGUID,
		},
	})
	if err != nil {
		return nil, err
	}

	return &HardwareTokenSet{
		AccessToken:  accessToken,
		IDToken:      idToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(s.config.AccessTokenTTL.Seconds()),
		TokenType:    "Bearer",
	}, nil
}

// ServiceAccountTokenSet holds tokens issued for a service-account mTLS login.
type ServiceAccountTokenSet struct {
	// AccessToken is the signed JWT with cnf/x5t#S256 proof-of-possession.
	AccessToken string
	// ExpiresIn is the access token lifetime in seconds.
	ExpiresIn int64
	// TokenType is always "Bearer".
	TokenType string
}

// ServiceAccountTokenOptions controls the content of tokens issued for mTLS
// service-account authentication.
type ServiceAccountTokenOptions struct {
	// ClientID is the authenticated OAuth2 client identifier.
	ClientID string
	// TenantID is the tenant context.
	TenantID string
	// Scope is the space-separated list of granted scopes.
	Scope string
	// SCP is the array-form authority claim: the granted PERMISSIONS, in
	// qrdb's canonical "resource:action" vocabulary. It is what a resource
	// server's authorizer reads, so it is stamped explicitly rather than left
	// to the OIDC scope fallback -- this path signs its own claims and would
	// otherwise ship an authority claim inferred from a protocol one, which is
	// how "openid" ended up standing in for a permission set. Empty omits scp.
	SCP []string
	// CertThumbprintS256 is the base64url-encoded SHA-256 thumbprint of the
	// client certificate used during authentication (RFC 8705 §3.1).
	CertThumbprintS256 string
	// AttestMethod is "tpm", "xkey", or "manual".
	AttestMethod string
	// AMR is the list of authentication method references (RFC 8176).
	AMR []string
}

// IssueServiceAccountTokens mints an mTLS-bound access token for a service
// account that authenticated via mutual TLS (RFC 8705).
//
// The resulting JWT contains:
//   - cnf: { "x5t#S256": "<thumbprint>" }  per RFC 8705 §3.1
//   - amr: ["mtls"] or ["mtls","hwk"] for TPM IDevID certs
//   - attest_method: "tpm" | "xkey" | "manual"
func (s *TokenService) IssueServiceAccountTokens(ctx context.Context, opts ServiceAccountTokenOptions) (*ServiceAccountTokenSet, error) {
	if opts.ClientID == "" {
		return nil, dberrors.New(dberrors.ErrInvalidInput, "iam.TokenService.IssueServiceAccountTokens", "client_id is required")
	}
	if opts.CertThumbprintS256 == "" {
		return nil, dberrors.New(dberrors.ErrInvalidInput, "iam.TokenService.IssueServiceAccountTokens", "cert thumbprint is required")
	}

	now := time.Now()
	scope := opts.Scope
	if scope == "" {
		scope = "openid"
	}
	issuer := s.config.Issuer

	// Minted before the claim map is built so a CSPRNG failure refuses the token
	// rather than signing one with a missing or degraded session identifier.
	serviceSessionID, err := generateSessionID()
	if err != nil {
		return nil, err
	}

	jti, err := generateJTI()
	if err != nil {
		return nil, err
	}

	jwtClaims := xkms.JWTClaims{
		"iss": issuer,
		"sub": opts.ClientID,
		"aud": []string{issuer},
		"exp": now.Add(s.config.AccessTokenTTL).Unix(),
		"iat": now.Unix(),
		"nbf": now.Unix(),
		"jti": jti,
		// This path signs its own claims rather than going through
		// GenerateAccessToken, so it carries sid explicitly: every access token
		// this provider mints answers "which login is this" the same way, and a
		// consumer never has to know which issuance path produced its bearer. An
		// mTLS service login holds no renewable session, so each issuance is its
		// own — which is what a fresh identifier per token says.
		"sid":           serviceSessionID,
		"tenant_id":     opts.TenantID,
		"scope":         scope,
		"client_id":     opts.ClientID,
		"auth_method":   "mtls",
		"ptyp":          PrincipalTypeService,
		"attest_method": opts.AttestMethod,
		"amr":           opts.AMR,
		"cnf": map[string]interface{}{
			"x5t#S256": opts.CertThumbprintS256,
		},
	}

	if len(opts.SCP) > 0 {
		jwtClaims["scp"] = opts.SCP
	}

	signer, alg, kid, err := s.signingMaterial(ctx, opts.TenantID)
	if err != nil {
		return nil, dberrors.Wrapf(err, dberrors.ErrInternal, "iam.TokenService.IssueServiceAccountTokens", "failed to resolve signing key")
	}
	tokenString, err := xkms.JWTSignWithSignerKIDTyp(signer, jwtClaims, alg, kid, AccessTokenJWTType)
	if err != nil {
		return nil, dberrors.Wrapf(err, dberrors.ErrInternal, "iam.TokenService.IssueServiceAccountTokens", "failed to sign service account token")
	}

	tokenMeta := &Token{
		JTI:       jti,
		Subject:   opts.ClientID,
		TenantID:  opts.TenantID,
		Scopes:    parseScopes(scope),
		Expires:   now.Add(s.config.AccessTokenTTL),
		Issued:    now,
		TokenType: TokenTypeAccess,
		ClientID:  opts.ClientID,
	}
	if err := s.storage.StoreToken(ctx, tokenMeta); err != nil {
		return nil, dberrors.Wrapf(err, dberrors.ErrStorage, "iam.TokenService.IssueServiceAccountTokens", "failed to store token")
	}

	return &ServiceAccountTokenSet{
		AccessToken: tokenString,
		ExpiresIn:   int64(s.config.AccessTokenTTL.Seconds()),
		TokenType:   "Bearer",
	}, nil
}

// validatePKCES256 validates a PKCE code verifier against a S256 challenge.
func validatePKCES256(verifier, challenge string) bool {
	// Compute SHA256 hash of verifier
	h := sha256.New()
	h.Write([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	// Compare with challenge
	return computed == challenge
}

// hardwareAMRValues are the RFC 8176 authentication method references that
// assert a HARDWARE-held key. A token carrying one is telling a relying party
// the same thing ACRHardwareAttested does, so it is held to the same evidence.
//
//   - "hwk": proof-of-possession of a hardware-secured key.
//   - "sc":  a smart card.
//
// Values describing the CEREMONY rather than the key's storage — "webauthn",
// "smartcard" as a method name, "mfa", "pwd" — are not listed: they say how the
// user authenticated, not where the key lives.
var hardwareAMRValues = map[string]struct{}{
	"hwk": {},
	"sc":  {},
}

// requireProvenHardware refuses a token that would assert hardware without
// verified evidence, whether the assertion is made through the ACR or through
// an AMR value.
func requireProvenHardware(opts HardwareTokenOptions) error {
	if opts.Attestation.IsHardware() {
		return nil
	}
	claim := ""
	if opts.ACR == ACRHardwareAttested {
		claim = "acr=" + ACRHardwareAttested
	} else {
		for _, amr := range opts.AMR {
			if _, ok := hardwareAMRValues[strings.ToLower(strings.TrimSpace(amr))]; ok {
				claim = "amr=" + amr
				break
			}
		}
	}
	if claim == "" {
		return nil
	}
	return &AttestationRequiredError{
		Operation: "token issuance claiming " + claim,
		Required:  AttestLevelHardware,
		Proven:    attestLevelOrNone(opts.Attestation),
		Format:    opts.Attestation.Format(),
	}
}
