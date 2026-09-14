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
	"encoding/json"
	"strings"
	"time"

	"github.com/go-jose/go-jose/v4"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam/dpop"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// OIDCConfig contains configuration for the OIDC provider.
type OIDCConfig struct {
	// Issuer is the OIDC issuer URL (e.g., "https://iam.example.com")
	Issuer string

	// SigningAlgorithm is the JWT signing algorithm (RS256, RS384, RS512)
	SigningAlgorithm string

	// SigningKey is the private key for signing tokens.
	//
	// Deprecated for production use: prefer SigningKeyManager, which is
	// xkms/HSM/TPM-backed and supports kid derivation, rotation overlap, and
	// a multi-kid JWKS. SigningKey is retained for back-compatibility and
	// tests; when SigningKeyManager is non-nil it is authoritative.
	SigningKey crypto.Signer

	// SigningKeyManager is the authoritative source of OIDC signing keys when
	// set. It derives kids from the public key, publishes a multi-kid JWKS,
	// and keeps previous keys valid across a rotation overlap window. When
	// non-nil it overrides SigningAlgorithm/SigningKey for signing and JWKS.
	SigningKeyManager *SigningKeyManager

	// TenantSigningKeys, when set, selects the signing key by the token's
	// tenant: a tenant that opted in to a dedicated key is signed with its own
	// per-tenant ES256 key (own kid) under the SAME single broker issuer; every
	// other tenant uses SigningKeyManager (the global key). The published JWKS
	// aggregates the global key and all active dedicated tenant keys so the
	// single-anchor validator verifies any of them by kid. When nil all tokens
	// use SigningKeyManager (single-key behavior).
	TenantSigningKeys *TenantSigningKeyResolver

	// SigningKeyFile is the path to the signing key file (alternative to SigningKey)
	SigningKeyFile string

	// AccessTokenTTL is the access token time-to-live
	AccessTokenTTL time.Duration

	// RefreshTokenTTL is the refresh token time-to-live
	RefreshTokenTTL time.Duration

	// IDTokenTTL is the ID token time-to-live
	IDTokenTTL time.Duration

	// AuthCodeTTL is the authorization code time-to-live
	AuthCodeTTL time.Duration

	// PKCERequired indicates whether PKCE is mandatory
	PKCERequired bool

	// AllowedMethods is the list of allowed PKCE methods
	AllowedMethods []string
}

// OIDCProvider implements an OAuth2/OIDC provider.
type OIDCProvider struct {
	config        *OIDCConfig
	identityStore *IdentityStore
	appStorage    AppStorage
	tokenService  *TokenService
	jwks          *jose.JSONWebKeySet
	// dpopReplay tracks DPoP proof jti values presented at the token endpoint
	// so a captured proof cannot be replayed to mint multiple bound tokens.
	dpopReplay dpop.ReplayCache
}

// NewOIDCProvider creates a new OIDC provider with the given configuration and dependencies.
func NewOIDCProvider(config *OIDCConfig, deps OIDCDependencies) (*OIDCProvider, error) {
	if config == nil {
		return nil, &OIDCConfigRequiredError{Field: "config"}
	}
	if deps.IdentityStore == nil {
		return nil, &OIDCConfigRequiredError{Field: "identity store"}
	}
	if deps.AppStorage == nil {
		return nil, &OIDCConfigRequiredError{Field: "app storage"}
	}
	if deps.TokenStorage == nil {
		return nil, &OIDCConfigRequiredError{Field: "token storage"}
	}
	if config.SigningKeyManager == nil && config.SigningKey == nil {
		return nil, &OIDCConfigRequiredError{Field: "signing key"}
	}

	// When a SigningKeyManager is configured it is authoritative: the token
	// service signs with the manager's current key (and its derived kid) and
	// the published JWKS is the manager's multi-kid set. Otherwise fall back
	// to the legacy single SigningKey path (used by tests and callers that
	// inject a raw crypto.Signer).
	signingKey := config.SigningKey
	signingAlg := config.SigningAlgorithm
	currentKID := ""

	if config.SigningKeyManager != nil {
		kid, mgrSigner, mgrAlg := config.SigningKeyManager.Current()
		if mgrSigner == nil {
			return nil, &SigningKeyUnavailableError{}
		}
		signingKey = mgrSigner
		signingAlg = mgrAlg
		currentKID = kid
		// Keep config.SigningAlgorithm consistent so discovery metadata and
		// validation advertise the manager's algorithm.
		config.SigningAlgorithm = mgrAlg
	}

	// Create token service
	tokenService := &TokenService{
		storage:      deps.TokenStorage,
		signingKey:   signingKey,
		signingAlg:   signingAlg,
		currentKID:   currentKID,
		keyManager:   config.SigningKeyManager,
		tenantKeys:   config.TenantSigningKeys,
		config:       config,
		permResolver: deps.PermissionResolver,
	}

	// Build the published JWKS.
	var jwks *jose.JSONWebKeySet
	if config.SigningKeyManager != nil {
		jwks = config.SigningKeyManager.JWKS()
	} else {
		var err error
		jwks, err = generateJWKS(config.SigningKey, signingAlg)
		if err != nil {
			return nil, &JWKSGenerationError{Cause: err}
		}
	}

	return &OIDCProvider{
		config:        config,
		identityStore: deps.IdentityStore,
		appStorage:    deps.AppStorage,
		tokenService:  tokenService,
		jwks:          jwks,
		dpopReplay:    dpop.NewMemoryReplayCache(nil),
	}, nil
}

// GetConfiguration returns the OIDC discovery configuration.
func (p *OIDCProvider) GetConfiguration() map[string]interface{} {
	return map[string]interface{}{
		"issuer":                                p.config.Issuer,
		"authorization_endpoint":                p.config.Issuer + "/oauth2/authorize",
		"token_endpoint":                        p.config.Issuer + "/oauth2/token",
		"userinfo_endpoint":                     p.config.Issuer + "/oauth2/userinfo",
		"jwks_uri":                              p.config.Issuer + "/.well-known/jwks.json",
		"registration_endpoint":                 p.config.Issuer + "/oauth2/register",
		"revocation_endpoint":                   p.config.Issuer + "/oauth2/revoke",
		"introspection_endpoint":                p.config.Issuer + "/oauth2/introspect",
		"response_types_supported":              []string{"code"},
		"grant_types_supported":                 []string{"authorization_code", "client_credentials", "refresh_token"},
		"subject_types_supported":               []string{"public"},
		"id_token_signing_alg_values_supported": []string{p.config.SigningAlgorithm},
		"scopes_supported":                      []string{"openid", "profile", "email", "offline_access", "agent:runtime:spawn", "agent:runtime:prompt", "agent:runtime:status"},
		"token_endpoint_auth_methods_supported": []string{
			AuthMethodClientSecretBasic,
			AuthMethodClientSecretPost,
			AuthMethodTLSClientAuth,
			AuthMethodSelfSignedTLSClientAuth,
		},
		"tls_client_certificate_bound_access_tokens": true,
		"mtls_endpoint_aliases": map[string]string{
			"token_endpoint": p.config.Issuer + "/oauth2/token-mtls",
		},
		"claims_supported": []string{
			"sub", "iss", "aud", "exp", "iat", "email", "name",
			"tenant_id", "roles", "groups", "mfa_verified",
			"cnf", "amr", "attest_method", "uid", "ptyp", "sid",
		},
		"code_challenge_methods_supported": []string{CodeChallengeMethodS256}, // OAuth 2.1 mandates S256 only
	}
}

// GetJWKS returns the JSON Web Key Set for token verification. When a per-tenant
// signing-key resolver is configured the set aggregates the global key and every
// active dedicated tenant key (each addressable by its derived kid) so the
// single-anchor validator can verify any tenant's token; otherwise it is the
// single signing key manager's (or legacy single key's) JWKS.
func (p *OIDCProvider) GetJWKS() *jose.JSONWebKeySet {
	if p.config.TenantSigningKeys != nil {
		return p.config.TenantSigningKeys.JWKS(context.Background())
	}
	if p.config.SigningKeyManager != nil {
		return p.config.SigningKeyManager.JWKS()
	}
	return p.jwks
}

// Issuer returns the configured OIDC issuer URL (the iss this provider stamps on
// the tokens it signs). The platform-token validator uses it as both the trusted
// issuer and -- under the AS==RS audience convention -- the required audience
// when validating tokens this provider minted.
func (p *OIDCProvider) Issuer() string {
	return p.config.Issuer
}

// OAuthASMetadata represents OAuth 2.0 Authorization Server Metadata per RFC 8414.
type OAuthASMetadata struct {
	// Issuer is the authorization server's issuer identifier.
	Issuer string `json:"issuer"`

	// AuthorizationEndpoint is the URL of the authorization endpoint.
	AuthorizationEndpoint string `json:"authorization_endpoint,omitempty"`

	// TokenEndpoint is the URL of the token endpoint.
	TokenEndpoint string `json:"token_endpoint"`

	// JWKSURI is the URL of the JWK Set document.
	JWKSURI string `json:"jwks_uri,omitempty"`

	// RegistrationEndpoint is the URL of the Dynamic Client Registration endpoint.
	RegistrationEndpoint string `json:"registration_endpoint,omitempty"`

	// RevocationEndpoint is the URL of the token revocation endpoint.
	RevocationEndpoint string `json:"revocation_endpoint,omitempty"`

	// IntrospectionEndpoint is the URL of the RFC 7662 token introspection endpoint.
	IntrospectionEndpoint string `json:"introspection_endpoint,omitempty"`

	// ResponseTypesSupported is a list of OAuth 2.0 response_type values.
	ResponseTypesSupported []string `json:"response_types_supported"`

	// GrantTypesSupported is a list of OAuth 2.0 grant_type values.
	GrantTypesSupported []string `json:"grant_types_supported,omitempty"`

	// TokenEndpointAuthMethodsSupported is a list of client authentication methods.
	TokenEndpointAuthMethodsSupported []string `json:"token_endpoint_auth_methods_supported,omitempty"`

	// TLSClientCertificateBoundAccessTokens indicates RFC 8705 support.
	TLSClientCertificateBoundAccessTokens bool `json:"tls_client_certificate_bound_access_tokens,omitempty"`

	// MTLSEndpointAliases lists the mTLS-specific endpoint aliases (RFC 8705 §5).
	MTLSEndpointAliases map[string]string `json:"mtls_endpoint_aliases,omitempty"`

	// ScopesSupported is a list of OAuth 2.0 scope values.
	ScopesSupported []string `json:"scopes_supported,omitempty"`

	// CodeChallengeMethodsSupported is a list of PKCE code challenge methods.
	CodeChallengeMethodsSupported []string `json:"code_challenge_methods_supported,omitempty"`

	// ResourceIndicatorsSupported indicates whether Resource Indicators (RFC 8707) is supported.
	ResourceIndicatorsSupported bool `json:"resource_indicators_supported,omitempty"`

	// ServiceDocumentation is a URL to the service documentation.
	ServiceDocumentation string `json:"service_documentation,omitempty"`
}

// GetOAuthASMetadata returns the OAuth 2.0 Authorization Server Metadata (RFC 8414).
// This is used for the /.well-known/oauth-authorization-server endpoint and
// provides OAuth 2.0 specific metadata for MCP clients.
func (p *OIDCProvider) GetOAuthASMetadata() *OAuthASMetadata {
	return &OAuthASMetadata{
		Issuer:                 p.config.Issuer,
		AuthorizationEndpoint:  p.config.Issuer + "/oauth2/authorize",
		TokenEndpoint:          p.config.Issuer + "/oauth2/token",
		JWKSURI:                p.config.Issuer + "/.well-known/jwks.json",
		RegistrationEndpoint:   p.config.Issuer + "/oauth2/register",
		RevocationEndpoint:     p.config.Issuer + "/oauth2/revoke",
		IntrospectionEndpoint:  p.config.Issuer + "/oauth2/introspect",
		ResponseTypesSupported: []string{"code"},
		GrantTypesSupported:    []string{"authorization_code", "client_credentials", "refresh_token"},
		TokenEndpointAuthMethodsSupported: []string{
			AuthMethodClientSecretBasic,
			AuthMethodClientSecretPost,
			AuthMethodTLSClientAuth,
			AuthMethodSelfSignedTLSClientAuth,
		},
		TLSClientCertificateBoundAccessTokens: true,
		MTLSEndpointAliases: map[string]string{
			"token_endpoint": p.config.Issuer + "/oauth2/token-mtls",
		},
		ScopesSupported:               []string{"openid", "profile", "email", "offline_access", "mcp:tools", "mcp:resources", "mcp:admin", "agent:runtime:spawn", "agent:runtime:prompt", "agent:runtime:status"},
		CodeChallengeMethodsSupported: []string{CodeChallengeMethodS256}, // OAuth 2.1 mandates S256 only
		ResourceIndicatorsSupported:   true,                              // RFC 8707
	}
}

// Authorize processes an OAuth2 authorization request.
func (p *OIDCProvider) Authorize(ctx context.Context, req *AuthorizeRequest) (*AuthorizeResponse, error) {
	// Validate request
	if err := p.validateAuthorizeRequest(req); err != nil {
		return nil, err
	}

	// Get application
	app, err := p.appStorage.GetApp(ctx, req.ClientID)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthInvalidClient, "invalid client_id")
	}

	// Validate redirect URI
	if !contains(app.RedirectURIs, req.RedirectURI) {
		return nil, NewOAuthError(ErrOAuthInvalidRequest, "invalid redirect_uri")
	}

	// Validate response type
	if !contains(app.ResponseTypes, req.ResponseType) {
		return nil, NewOAuthError(ErrOAuthUnsupportedResponseType, "unsupported response_type")
	}

	// Validate PKCE (mandatory per OAuth 2.1)
	// OAuth 2.1 requires PKCE for all public clients using authorization code flow.
	// We mandate PKCE for all clients as recommended by security best practices.
	if req.CodeChallenge == "" {
		return nil, NewOAuthError(ErrOAuthInvalidRequest, "code_challenge is required (OAuth 2.1 mandates PKCE)")
	}

	// OAuth 2.1 only allows S256 method
	if req.CodeChallengeMethod == "" {
		req.CodeChallengeMethod = CodeChallengeMethodS256
	}
	if req.CodeChallengeMethod != CodeChallengeMethodS256 {
		return nil, NewOAuthError(ErrOAuthInvalidRequest, "code_challenge_method must be S256 (OAuth 2.1)")
	}

	// Generate authorization code
	code, err := p.tokenService.GenerateAuthCode(ctx, req, req.UserID)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthServerError, "failed to generate authorization code")
	}

	return &AuthorizeResponse{
		Code:        code,
		State:       req.State,
		RedirectURI: req.RedirectURI,
	}, nil
}

// Token processes an OAuth2 token request.
func (p *OIDCProvider) Token(ctx context.Context, req *TokenRequest) (*TokenResponse, error) {
	switch req.GrantType {
	case GrantTypeAuthorizationCode:
		return p.handleAuthorizationCodeGrant(ctx, req)
	case GrantTypeClientCredentials:
		return p.handleClientCredentialsGrant(ctx, req)
	case GrantTypeRefreshToken:
		return p.handleRefreshTokenGrant(ctx, req)
	default:
		return nil, NewOAuthError(ErrOAuthUnsupportedGrantType, "unsupported grant_type")
	}
}

// bindDPoP verifies a DPoP proof presented on a token request and returns the
// cnf claim binding the issued token to the proof key (RFC 9449). It returns
// (nil, nil) when no proof was presented so the caller issues a plain bearer
// token (back-compat). A presented-but-invalid proof is a hard failure: the
// token endpoint must fail closed with invalid_dpop_proof rather than silently
// downgrade to a bearer.
func (p *OIDCProvider) bindDPoP(req *TokenRequest) (*ConfirmationClaim, error) {
	if req.DPoPProof == "" {
		return nil, nil
	}
	verified, err := dpop.VerifyProof(req.DPoPProof, dpop.VerifyOptions{
		HTM:         req.HTTPMethod,
		HTU:         req.HTTPURI,
		ReplayCache: p.dpopReplay,
	})
	if err != nil {
		return nil, NewOAuthError(ErrOAuthInvalidDPoPProof, "invalid DPoP proof")
	}
	return &ConfirmationClaim{Jkt: verified.Jkt}, nil
}

// tokenTypeForConfirmation returns the OAuth token_type for an issued access
// token: "DPoP" (RFC 9449 §5) when the token is sender-constrained via cnf.jkt,
// otherwise "Bearer". Downstream services key on this to decide whether a DPoP
// proof must accompany the token.
func tokenTypeForConfirmation(cnf *ConfirmationClaim) string {
	if cnf != nil && cnf.Jkt != "" {
		return TokenTypeDPoP
	}
	return TokenTypeBearer
}

// MTLSTokenRequest carries the parameters needed to issue a token for a
// client that authenticated via mutual TLS (RFC 8705).
type MTLSTokenRequest struct {
	// GrantType must be "client_credentials" (other grant types use the
	// human-user token endpoint with FIDO2/PIV, not mTLS client auth).
	GrantType string

	// ClientID is the registered OAuth2 client identifier.
	ClientID string

	// Scope is the space-separated list of requested scopes.
	Scope string

	// CertThumbprintS256 is the base64url SHA-256 thumbprint of the
	// presenting certificate, computed by the HTTP handler from r.TLS.
	CertThumbprintS256 string

	// AttestMethod is resolved by TPMAttestationResolver before calling here.
	// One of: AttestMethodTPM, AttestMethodXKey, AttestMethodManual.
	AttestMethod string

	// Resource is the optional target resource server URI (RFC 8707).
	Resource string
}

// TokenMTLS processes a token request where the client already authenticated
// via mutual TLS. The handler is responsible for verifying the cert before
// calling this method and populating CertThumbprintS256 and AttestMethod.
func (p *OIDCProvider) TokenMTLS(ctx context.Context, req *MTLSTokenRequest) (*TokenResponse, error) {
	if req.GrantType != GrantTypeClientCredentials {
		return nil, NewOAuthError(ErrOAuthUnsupportedGrantType, "mTLS token endpoint only supports client_credentials")
	}
	if req.ClientID == "" {
		return nil, NewOAuthError(ErrOAuthInvalidRequest, "client_id is required")
	}
	if req.CertThumbprintS256 == "" {
		return nil, NewOAuthError(ErrOAuthInvalidClient, "certificate thumbprint is required")
	}

	app, err := p.appStorage.GetApp(ctx, req.ClientID)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthInvalidClient, "client not found")
	}

	if !contains(app.GrantTypes, GrantTypeClientCredentials) {
		return nil, NewOAuthError(ErrOAuthUnauthorizedClient, "client_credentials grant not allowed for this client")
	}

	// Same grant resolution as the secret-authenticated client_credentials
	// path: mTLS proves WHICH principal is asking, never WHAT it may do
	// (go-quicbaas#653).
	grantedScopes, err := ResolveGrantedScopes(req.ClientID, app.Scopes, strings.Fields(req.Scope))
	if err != nil {
		return nil, NewOAuthError(ErrOAuthInvalidScope, err.Error())
	}
	grantedScope := strings.Join(grantedScopes, " ")

	amr := ServiceAccountAMR(req.AttestMethod)

	opts := ServiceAccountTokenOptions{
		ClientID:           req.ClientID,
		TenantID:           app.TenantID,
		Scope:              grantedScope,
		SCP:                PermissionScopes(grantedScopes),
		CertThumbprintS256: req.CertThumbprintS256,
		AttestMethod:       req.AttestMethod,
		AMR:                amr,
	}

	tokenSet, err := p.tokenService.IssueServiceAccountTokens(ctx, opts)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthServerError, "failed to issue service account token")
	}

	return &TokenResponse{
		AccessToken: tokenSet.AccessToken,
		TokenType:   tokenSet.TokenType,
		ExpiresIn:   tokenSet.ExpiresIn,
		Scope:       grantedScope,
	}, nil
}

// Refresh processes a token refresh request.
func (p *OIDCProvider) Refresh(ctx context.Context, req *RefreshRequest) (*TokenResponse, error) {
	tokenReq := &TokenRequest{
		GrantType:    GrantTypeRefreshToken,
		RefreshToken: req.RefreshToken,
		ClientID:     req.ClientID,
		ClientSecret: req.ClientSecret,
		Scope:        req.Scope,
	}
	return p.Token(ctx, tokenReq)
}

// Revoke processes a token revocation request.
func (p *OIDCProvider) Revoke(ctx context.Context, req *RevokeRequest) error {
	// Validate client
	valid, err := p.appStorage.ValidateClientSecret(ctx, req.ClientID, req.ClientSecret)
	if err != nil || !valid {
		return NewOAuthError(ErrOAuthInvalidClient, "client authentication failed")
	}

	// Revoke the token
	if err := p.tokenService.RevokeToken(ctx, req.Token); err != nil {
		return NewOAuthError(ErrOAuthServerError, "failed to revoke token")
	}

	return nil
}

// Introspect implements RFC 7662 token introspection. It verifies the token
// signature, derives the tenant from the token's tenant_id claim (so the
// per-tenant revocation lookup resolves without any global token table), and
// checks the revocation store. It returns an inactive response (Active=false,
// no claims) for any token that fails signature verification, is expired, or
// has been revoked, per RFC 7662 §2.2 (fail-closed: never leak claims for an
// invalid token). Caller authentication is enforced at the transport before
// this method is reached (RFC 7662 §2.1).
func (p *OIDCProvider) Introspect(ctx context.Context, req *IntrospectRequest) (*IntrospectionResponse, error) {
	if req == nil || req.Token == "" {
		return nil, NewOAuthError(ErrOAuthInvalidRequest, "token is required")
	}

	// Verify the token signature and parse claims WITHOUT the revocation check
	// first, so we can derive the tenant from the verified claim and route the
	// revocation lookup to the correct tenant store.
	claims, err := p.parseVerifiedAccessTokenClaims(req.Token)
	if err != nil {
		// Signature/parse failure: the token is not active. RFC 7662 says
		// respond active=false rather than surface an error to the caller.
		return &IntrospectionResponse{Active: false}, nil
	}

	// Reject expired tokens (active=false). exp is in Unix seconds.
	now := time.Now()
	if !claims.ExpiresAt.IsZero() && now.After(claims.ExpiresAt.Time) {
		return &IntrospectionResponse{Active: false}, nil
	}
	if !claims.NotBefore.IsZero() && now.Before(claims.NotBefore.Time) {
		return &IntrospectionResponse{Active: false}, nil
	}

	// Liveness: the token's jti must not be revoked AND its subject must be an
	// enabled user. TokenLiveness derives the tenant from the verified claim and
	// scopes the context so the per-tenant revocation lookup resolves (there is
	// no global token table; the tenant_id claim is the authoritative routing
	// key). It is fail-closed: any store error yields a non-live result, so a
	// revocation-store outage reports active=false rather than leaking claims.
	live, err := p.TokenLiveness(ctx, claims.TenantID, claims.Subject, claims.UserID, claims.PrincipalType, claims.JTI)
	if err != nil || !live {
		return &IntrospectionResponse{Active: false}, nil
	}

	scope := claims.Scope
	if scope == "" && len(claims.SCP) > 0 {
		scope = strings.Join(claims.SCP, " ")
	}

	resp := &IntrospectionResponse{
		Active:       true,
		Scope:        scope,
		SCP:          claims.SCP,
		ClientID:     claims.ClientID,
		Subject:      claims.Subject,
		TenantID:     claims.TenantID,
		Audience:     claims.Audience,
		Issuer:       claims.Issuer,
		JTI:          claims.JTI,
		TokenType:    "Bearer",
		Username:     claims.Name,
		Confirmation: claims.Confirmation,
		Roles:        claims.Roles,
	}
	if !claims.ExpiresAt.IsZero() {
		resp.ExpiresAt = claims.ExpiresAt.Unix()
	}
	if !claims.IssuedAt.IsZero() {
		resp.IssuedAt = claims.IssuedAt.Unix()
	}
	if !claims.NotBefore.IsZero() {
		resp.NotBefore = claims.NotBefore.Unix()
	}
	return resp, nil
}

// TokenLiveness reports whether an already-signature-verified token is still
// live. It is the single in-process liveness authority shared by RFC 7662
// introspection (this provider) and the SDK platform-token validator's
// request-time authorization, so a token's "still good?" decision has exactly
// one implementation.
//
// Liveness is two independent signals, evaluated in order and fail-closed:
//
//  1. Revocation. The jti must not be present in the per-tenant revocation
//     store. The tenant is scoped from tenantID (the authoritative routing key;
//     there is no global token table). Any store error returns (false, err) so
//     a revocation-store outage denies rather than admits.
//
//  2. Principal enablement, decided from what the token SAYS it is rather than
//     from what a lookup fails to find:
//
//     - uid present (a user token): the account is resolved by that immutable
//     id and must be enabled. An unresolvable uid DENIES — the principal the
//     token names is gone, which is not a licence to keep using it.
//     - ptyp == service (a positive service marker): a service principal has
//     no user record by construction, so revocation is the whole test.
//     - neither claim: DENIED. The guessing path that used to resolve such a
//     token from its subject string is gone; principalLive states what it did
//     and why nothing needs it back, and this bullet deliberately says no more
//     than that — see the note on duplication at the UserInfo call site.
//
//     A resolved user is live only when its LIFECYCLE is `active`, which is the
//     only state qrdb permits authentication in. Pending, suspended, locked,
//     disabled and deleting all deny, and a state this build does not recognise
//     denies too. Any store error other than not-found returns (false, err),
//     fail-closed.
//
// This is the fail-closed rule applied to identity: service-principal status is
// never INFERRED from a lookup coming up empty. Inferring it was a live
// authorization bypass — a ceremony bearer's username subject never resolved,
// was read as a service token, and reported live forever, so disabling an
// account did not end its session; and even after resolving usernames, renaming
// the account made the subject miss again and restored the bypass.
func (p *OIDCProvider) TokenLiveness(ctx context.Context, tenantID, subject, userID, principalType, jti string) (bool, error) {
	if tenantID != "" {
		ctx = ContextWithTenant(ctx, tenantID)
	}

	revoked, err := p.tokenService.storage.IsTokenRevoked(ctx, jti)
	if err != nil {
		return false, err
	}
	if revoked {
		return false, nil
	}

	return p.principalLive(ctx, tenantID, subject, userID, principalType)
}

// principalLive reports whether the principal a token names may still hold a
// live session. See TokenLiveness for the decision order.
func (p *OIDCProvider) principalLive(ctx context.Context, tenantID, subject, userID, principalType string) (bool, error) {
	// A user token carries an immutable handle on its account. Renaming the
	// account cannot detach the session from it, and deleting the account ends
	// the session rather than promoting it to a service principal.
	if userID != "" {
		user, err := p.identityStore.GetUser(ctx, tenantID, userID)
		if err != nil {
			// IsUserNotFound names the one condition that means "the account
			// this token points at is gone". errors.Is would have compared the
			// QBError CODE, so any not-found the lookup could raise -- an
			// absent tenant, a store answering 404 -- was reported as a
			// deleted account rather than as the store failure it is
			// (go-quicbaas#434). Both answers deny; only one is true.
			if IsUserNotFound(err) {
				return false, nil
			}
			return false, err
		}
		return userEnabled(user), nil
	}

	// A service principal says so. Nothing else may claim the exemption.
	if principalType == PrincipalTypeService {
		return true, nil
	}

	// Neither claim: the token predates the uid/ptyp contract, and under the
	// pre-release policy there is no such population to carry. It is DENIED
	// rather than resolved from the subject string.
	//
	// What the deleted path did, and why nothing current needs it: it resolved
	// the subject under both naming conventions and required every record either
	// lookup found to be enabled, because nothing keeps the id and username
	// namespaces apart. It also carried two weaknesses it could not fix — a
	// subject resolving to nobody was read as a service principal, and a service
	// token colliding with a disabled user's name was over-denied — and argued
	// they were acceptable because the tokens age out. Once the tokens are not
	// carried, a guessing path with known holes is strictly worse than a denial.
	//
	// Every current mint sets one of the two claims: the authorization-code and
	// hardware paths set uid (oidc_tokens.go, oidc.go), the ceremony session
	// path sets uid (oidc_session.go), client_credentials sets ptyp=service
	// (oidc.go), and refresh copies whichever the stored session held. So a
	// token reaching here today is one this build did not mint.
	return false, nil
}

// userInfoSubject resolves the principal a userinfo request is about.
//
// It REQUIRES the uid claim. The claim names the record directly and cannot be
// confused with anybody else's name or address, and every current mint sets it
// for a user token.
//
// The subject-string fallback is gone, and deleting it closed more than the
// legacy population it was written for. It resolved a non-email subject by user
// id and then by USERNAME, and the only subject that reaches it today is a
// SERVICE token's client id — so a user could take the username "app_<tenant>_
// <suffix>" matching a registered client and have this endpoint answer that
// client's userinfo request with their own profile: email, roles, groups and
// attributes, under the token's own sub. The UUID guard that used to sit here
// closed one shape of that collision (a pre-#359 user subject) and left the
// rest open, which is why removing the guard without removing the fallback
// would have been the wrong half to delete.
//
// A token carrying no uid is either pre-contract — a population the pre-release
// policy does not carry — or a service principal, which has no user profile by
// construction. Both answer "no user", which is what this now returns.
func (p *OIDCProvider) userInfoSubject(ctx context.Context, claims *Claims) (*User, error) {
	if claims.UserID == "" {
		return nil, ErrUserNotFound
	}
	return p.identityStore.GetUser(ctx, claims.TenantID, claims.UserID)
}

// TenantAuthzMode returns the per-tenant authorization-mode override stored in
// TenantSettings.AuthzMode for the given tenant. An empty return value means the
// tenant has no override and the deployment-level default applies. A lookup
// failure (e.g. unknown tenant or unavailable tenant manager) is surfaced to the
// caller, which is expected to fail SAFE by falling back to the deployment
// default rather than panicking.
func (p *OIDCProvider) TenantAuthzMode(ctx context.Context, tenantID string) (string, error) {
	if tenantID == "" {
		return "", nil
	}
	tenant, err := p.identityStore.GetTenant(ctx, tenantID)
	if err != nil {
		return "", err
	}
	if tenant == nil {
		return "", nil
	}
	return tenant.Settings.AuthzMode, nil
}

// parseVerifiedAccessTokenClaims verifies a token signature and returns its
// parsed claims WITHOUT performing the revocation check. It is the shared
// signature-verification + claim-parsing path used by ValidateAccessToken and
// Introspect so the crypto lives in exactly one place.
func (p *OIDCProvider) parseVerifiedAccessTokenClaims(tokenString string) (*Claims, error) {
	publicKey, err := p.resolveVerificationKey(tokenString)
	if err != nil {
		return nil, err
	}

	jwtClaims, err := xkms.JWTVerifyWithKeyAndClaims(tokenString, publicKey, p.config.SigningAlgorithm)
	if err != nil {
		return nil, err
	}

	var claims Claims
	claimsBytes, err := json.Marshal(jwtClaims)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, err
	}
	return &claims, nil
}

// AuthenticateClient validates a registered confidential client's credentials.
// It is the caller-authentication primitive RFC 7662 §2.1 requires for the
// introspection endpoint when the resource server authenticates with a
// client_id/client_secret pair (as opposed to mTLS). It returns nil when the
// secret is valid and an invalid_client OAuth error otherwise (fail-closed).
func (p *OIDCProvider) AuthenticateClient(ctx context.Context, clientID, clientSecret string) error {
	if clientID == "" {
		return NewOAuthError(ErrOAuthInvalidClient, "client_id is required")
	}
	valid, err := p.appStorage.ValidateClientSecret(ctx, clientID, clientSecret)
	if err != nil || !valid {
		return NewOAuthError(ErrOAuthInvalidClient, "client authentication failed")
	}
	return nil
}

// userEnabled reports whether a user's LIFECYCLE permits a live session. It is
// the single definition of "this principal may hold one", shared by session
// issuance (mint time) and TokenLiveness (request time) so the two gates cannot
// disagree.
//
// `active` is the ONLY state that passes. It defers to qrdb's rule rather than
// restating it, so a state written by a newer node fails closed here exactly as
// it does in the store, and this layer cannot grant what the store would refuse.
//
// It used to read three separate signals — Suspended, Locked and a State that
// excluded only `disabled` — which meant PENDING passed. An account waiting for
// its owner to activate could hold a session, and one waiting on email
// verification could log in without verifying (#334). Both close here.
func userEnabled(user *User) bool {
	return user.CanAuthenticate()
}

// UserInfo returns user information for the given access token.
func (p *OIDCProvider) UserInfo(ctx context.Context, accessToken string) (*UserInfoResponse, error) {
	// Validate access token
	claims, err := p.ValidateAccessToken(ctx, accessToken)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthInvalidGrant, "invalid access token")
	}

	// Resolve the user by the IMMUTABLE uid claim, which userInfoSubject
	// REQUIRES — there is no fallback to the subject string, and a token
	// carrying no uid resolves to nobody. See that function for why the
	// fallback was removed rather than guarded.
	//
	// THE ARGUMENT LIVES THERE AND NOT ALSO HERE. This comment used to restate
	// it, and when the fallback was deleted (#390) the copy on the function was
	// updated and this one was not — so for a time the call site described a
	// username fallback, and described it as safe because of a disjoint-shape
	// rule, neither of which existed. A stale comment about behaviour costs a
	// reader some confusion; one that describes a GUARD costs them their
	// conclusion, and the next person to touch this would have reasoned from it
	// (#278). One statement, one place.
	user, err := p.userInfoSubject(ctx, claims)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthServerError, "failed to get user info")
	}

	// A token outliving its subject's right to use it is exactly what the
	// lifecycle exists to catch, and userinfo is a read of that subject's
	// profile. Checking here keeps a disabled, suspended, locked or deleting
	// account from continuing to describe itself over an unexpired bearer —
	// revocation cannot retroactively un-issue one.
	if !userEnabled(user) {
		return nil, NewOAuthError(ErrOAuthInvalidGrant, "user is not enabled")
	}

	// OIDC Core 5.3.2: the userinfo sub MUST match the token sub, so it is
	// ECHOED FROM THE CLAIMS AND NEVER DERIVED FROM THE RESOLVED RECORD. The
	// record was found by uid, and a ceremony-minted token's subject is not its
	// id — answering with user.ID would return a sub the caller never presented
	// and break the match this line exists to satisfy.
	return &UserInfoResponse{
		Sub:           claims.Subject,
		Name:          user.Name,
		Email:         user.Email,
		EmailVerified: true,
		TenantID:      user.TenantID,
		Roles:         claims.Roles,
		Groups:        claims.Groups,
		Attributes:    user.Attributes,
	}, nil
}

// ValidateAccessToken validates an access token and returns its claims.
func (p *OIDCProvider) ValidateAccessToken(ctx context.Context, tokenString string) (*Claims, error) {
	// Verify the signature and parse claims using the shared verification path
	// (kid-aware key resolution lives there). The crypto is not duplicated.
	claims, err := p.parseVerifiedAccessTokenClaims(tokenString)
	if err != nil {
		return nil, err
	}

	// Scope the revocation lookup to the tenant carried by the verified claim
	// so the per-tenant token store resolves; there is no global token table.
	if claims.TenantID != "" {
		ctx = ContextWithTenant(ctx, claims.TenantID)
	}

	// Check if token is revoked
	revoked, err := p.tokenService.storage.IsTokenRevoked(ctx, claims.JTI)
	if err == nil && revoked {
		return nil, &TokenRevokedError{}
	}

	return claims, nil
}

// ValidateAccessTokenWithDPoP validates an access token (signature, issuer
// audience, revocation — exactly as ValidateAccessToken) and, when the token is
// DPoP sender-constrained via cnf.jkt (RFC 9449), additionally enforces the
// presented DPoP proof against that binding: the proof signature, htm/htu match,
// iat skew, jti single-use replay protection, and that the SHA-256 thumbprint of
// the proof key equals cnf.jkt.
//
// This is the inbound counterpart to the bindDPoP issuance path: both share this
// provider's single dpopReplay cache and the canonical pkg/iam/dpop verifier, so
// a proof captured at issuance cannot be replayed at validation and vice versa.
//
// Enforcement is fail-closed for bound tokens: a cnf.jkt-bound token presented
// as a plain bearer (empty proof) is rejected with *dpop.MissingProofError, and
// a proof that does not match the binding/request is rejected with the verifier's
// typed error. A token WITHOUT a cnf.jkt binding is a plain bearer and validates
// exactly as ValidateAccessToken — the proof argument is ignored for it. Every
// failure is a typed error the caller maps to 401.
func (p *OIDCProvider) ValidateAccessTokenWithDPoP(ctx context.Context, tokenString, dpopProof, htm, htu string) (*Claims, error) {
	claims, err := p.ValidateAccessToken(ctx, tokenString)
	if err != nil {
		return nil, err
	}

	// Unbound (plain bearer) token: no proof-of-possession required.
	if claims.Confirmation == nil || claims.Confirmation.Jkt == "" {
		return claims, nil
	}

	// Bound token: a proof is mandatory. A bearer presentation fails closed.
	if strings.TrimSpace(dpopProof) == "" {
		return nil, &dpop.MissingProofError{}
	}

	if _, err := dpop.VerifyProof(dpopProof, dpop.VerifyOptions{
		HTM:         htm,
		HTU:         htu,
		ExpectedJkt: claims.Confirmation.Jkt,
		ReplayCache: p.dpopReplay,
	}); err != nil {
		return nil, err
	}

	return claims, nil
}

// ValidateIDToken validates an ID token and returns its claims.
func (p *OIDCProvider) ValidateIDToken(ctx context.Context, tokenString string) (*IDTokenClaims, error) {
	// Resolve the verification key (kid-aware when a key manager is present).
	publicKey, err := p.resolveVerificationKey(tokenString)
	if err != nil {
		return nil, err
	}

	// Verify token using XKMS JWT
	jwtClaims, err := xkms.JWTVerifyWithKeyAndClaims(tokenString, publicKey, p.config.SigningAlgorithm)
	if err != nil {
		return nil, err
	}

	// Parse custom claims
	var claims IDTokenClaims
	claimsBytes, err := json.Marshal(jwtClaims)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(claimsBytes, &claims); err != nil {
		return nil, err
	}

	return &claims, nil
}

// resolveVerificationKey selects the public key used to verify a token. When a
// SigningKeyManager is configured, the token's "kid" header picks the matching
// public key (current key or a previous key still inside the rotation overlap),
// which keeps tokens minted by a superseded key valid until they expire. When
// no kid is present, or no manager is configured, it falls back to the legacy
// single SigningKey.
func (p *OIDCProvider) resolveVerificationKey(tokenString string) (crypto.PublicKey, error) {
	// When a per-tenant resolver is configured the kid in the token header
	// selects the matching public key from the aggregated JWKS (global or any
	// dedicated tenant key). This keeps the OIDC provider able to verify its
	// own per-tenant-signed tokens by kid.
	if p.config.TenantSigningKeys != nil {
		kid, _ := xkms.JWTExtractKID(tokenString)
		if kid != "" {
			if pub := publicForKIDInJWKS(p.config.TenantSigningKeys.JWKS(context.Background()), kid); pub != nil {
				return pub, nil
			}
			return nil, &UnknownKeyIDError{KID: kid}
		}
		_, signer, _ := p.config.TenantSigningKeys.Global().Current()
		if signer != nil {
			return signer.Public(), nil
		}
	}
	if p.config.SigningKeyManager != nil {
		kid, _ := xkms.JWTExtractKID(tokenString)
		if kid != "" {
			pub, err := p.config.SigningKeyManager.PublicForKID(kid)
			if err != nil {
				return nil, err
			}
			return pub, nil
		}
		// No kid header: verify against the manager's current key.
		_, signer, _ := p.config.SigningKeyManager.Current()
		if signer != nil {
			return signer.Public(), nil
		}
	}
	if p.config.SigningKey == nil {
		return nil, &SigningKeyUnavailableError{}
	}
	return p.config.SigningKey.Public(), nil
}

// publicForKIDInJWKS extracts the public key for kid from a JWKS, returning nil
// when no entry matches.
func publicForKIDInJWKS(jwks *jose.JSONWebKeySet, kid string) crypto.PublicKey {
	if jwks == nil {
		return nil
	}
	for i := range jwks.Keys {
		if jwks.Keys[i].KeyID == kid {
			k := jwks.Keys[i]
			if k.IsPublic() {
				return k.Key
			}
			return k.Public().Key
		}
	}
	return nil
}

// Helper methods

func (p *OIDCProvider) validateAuthorizeRequest(req *AuthorizeRequest) error {
	if req.ClientID == "" {
		return NewOAuthError(ErrOAuthInvalidRequest, "client_id is required")
	}
	if req.RedirectURI == "" {
		return NewOAuthError(ErrOAuthInvalidRequest, "redirect_uri is required")
	}
	if req.ResponseType == "" {
		return NewOAuthError(ErrOAuthInvalidRequest, "response_type is required")
	}
	if req.ResponseType != ResponseTypeCode {
		return NewOAuthError(ErrOAuthUnsupportedResponseType, "only 'code' response_type is supported")
	}
	return nil
}

func (p *OIDCProvider) handleAuthorizationCodeGrant(ctx context.Context, req *TokenRequest) (*TokenResponse, error) {
	// Validate client
	valid, err := p.appStorage.ValidateClientSecret(ctx, req.ClientID, req.ClientSecret)
	if err != nil || !valid {
		return nil, NewOAuthError(ErrOAuthInvalidClient, "client authentication failed")
	}

	// Get and validate authorization code
	authCode, err := p.tokenService.ValidateAuthCode(ctx, req.Code)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthInvalidGrant, "invalid authorization code")
	}

	// Validate redirect URI
	if authCode.RedirectURI != req.RedirectURI {
		return nil, NewOAuthError(ErrOAuthInvalidGrant, "redirect_uri mismatch")
	}

	// Validate client ID
	if authCode.ClientID != req.ClientID {
		return nil, NewOAuthError(ErrOAuthInvalidGrant, "client_id mismatch")
	}

	// Validate PKCE if present
	if err := authCode.ValidatePKCE(req.CodeVerifier); err != nil {
		return nil, err
	}

	// Get user
	user, err := p.identityStore.GetUser(ctx, authCode.TenantID, authCode.UserID)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthServerError, "failed to get user")
	}

	// Validate resource indicator if provided
	resource := authCode.Resource
	if req.Resource != "" && req.Resource != resource {
		// Token request resource must match authorization request resource
		return nil, NewOAuthError(ErrOAuthInvalidGrant, "resource mismatch")
	}

	// Bind a presented DPoP proof (RFC 9449) into cnf.jkt. No proof => bearer.
	cnf, err := p.bindDPoP(req)
	if err != nil {
		return nil, err
	}

	// Generate tokens with resource in audience (RFC 8707)
	return p.generateTokenResponse(ctx, user, authCode.ClientID, authCode.Scope, authCode.Nonce, resource, cnf)
}

func (p *OIDCProvider) handleClientCredentialsGrant(ctx context.Context, req *TokenRequest) (*TokenResponse, error) {
	// Validate client.
	//
	// The two failures are kept apart HERE even though they answer the same
	// thing: `err != nil || !valid` made a lookup that never reached the record
	// and a secret that simply did not match indistinguishable in the code, so
	// a fleet-wide enrollment outage read as "every agent is sending the wrong
	// password" (go-quicbaas#644). The storage layer names the failing check in
	// the operator log (BackendStorage.warnClientAuthFailed); this branch keeps
	// the RESPONSE identical for both, because a caller that could tell an
	// unknown client from a wrong secret could enumerate the client ids.
	valid, err := p.appStorage.ValidateClientSecret(ctx, req.ClientID, req.ClientSecret)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthInvalidClient, "client authentication failed")
	}
	if !valid {
		return nil, NewOAuthError(ErrOAuthInvalidClient, "client authentication failed")
	}

	// Get application
	app, err := p.appStorage.GetApp(ctx, req.ClientID)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthInvalidClient, "invalid client")
	}

	// Check if grant type is allowed
	if !contains(app.GrantTypes, GrantTypeClientCredentials) {
		return nil, NewOAuthError(ErrOAuthUnauthorizedClient, "client_credentials grant not allowed")
	}

	// Resolve the scope the token may carry from the GRANT on the application
	// record, not from the client's request. A client_credentials principal
	// has no qrdb user and therefore no store-resolved roles, so scp is the
	// whole of its authority; copying the request into it let any client mint
	// itself any permission it could name (go-quicbaas#653). A request for a
	// scope outside the grant is refused rather than trimmed, so the caller
	// learns which entitlement it lacks.
	grantedScopes, err := ResolveGrantedScopes(req.ClientID, app.Scopes, req.Scope)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthInvalidScope, err.Error())
	}

	// Build audience with resource indicator (RFC 8707). When the client sends
	// no resource indicator, fall back to the app's configured default service
	// audience so downstream validators accept the token.
	audience := BuildAudienceWithDefault(p.config.Issuer, req.Resource, p.validDefaultAudience(app))

	// Bind a presented DPoP proof (RFC 9449) into cnf.jkt. No proof => bearer.
	cnf, err := p.bindDPoP(req)
	if err != nil {
		return nil, err
	}

	// Create service account claims
	now := time.Now()
	jti, err := generateJTI()
	if err != nil {
		return nil, err
	}

	claims := &Claims{
		Issuer:    p.config.Issuer,
		Subject:   req.ClientID,
		Audience:  audience,
		ExpiresAt: NewUnixTime(now.Add(p.config.AccessTokenTTL)),
		IssuedAt:  NewUnixTime(now),
		NotBefore: NewUnixTime(now),
		JTI:       jti,
		TenantID:  app.TenantID,
		// A client_credentials grant has no user by construction; say so
		// positively rather than leaving liveness to infer it from a miss.
		PrincipalType: PrincipalTypeService,
		ClientID:      req.ClientID,
		Scope:         joinScopes(grantedScopes),
		// SCP is stamped explicitly so GenerateAccessToken emits the resolved
		// grant verbatim instead of re-deriving it: a service principal has no
		// user record, so the role resolver would answer empty and fall back to
		// the OIDC scope string. This is the "caller already computed the perm
		// set" precedence branch, which is exactly this case. Only the
		// permission-shaped scopes belong here — scp is the authority claim.
		SCP:          PermissionScopes(grantedScopes),
		Confirmation: cnf,
	}

	// Generate access token
	accessToken, err := p.tokenService.GenerateAccessToken(ctx, claims)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthServerError, "failed to generate access token")
	}

	return &TokenResponse{
		AccessToken: accessToken,
		TokenType:   tokenTypeForConfirmation(cnf),
		ExpiresIn:   int64(p.config.AccessTokenTTL.Seconds()),
		Scope:       joinScopes(grantedScopes),
	}, nil
}

func (p *OIDCProvider) handleRefreshTokenGrant(ctx context.Context, req *TokenRequest) (*TokenResponse, error) {
	// Validate client
	valid, err := p.appStorage.ValidateClientSecret(ctx, req.ClientID, req.ClientSecret)
	if err != nil || !valid {
		return nil, NewOAuthError(ErrOAuthInvalidClient, "client authentication failed")
	}

	// Get refresh token
	token, err := p.tokenService.storage.GetTokenByValue(ctx, req.RefreshToken)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthInvalidGrant, "invalid refresh token")
	}

	// Validate token
	if token.TokenType != TokenTypeRefresh {
		return nil, NewOAuthError(ErrOAuthInvalidGrant, "not a refresh token")
	}
	if token.IsExpired() {
		return nil, NewOAuthError(ErrOAuthInvalidGrant, "refresh token expired")
	}
	if token.Revoked {
		return nil, NewOAuthError(ErrOAuthInvalidGrant, "refresh token revoked")
	}
	if token.ClientID != req.ClientID {
		return nil, NewOAuthError(ErrOAuthInvalidGrant, "client_id mismatch")
	}

	// Get user
	user, err := p.identityStore.GetUser(ctx, token.TenantID, token.Subject)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthServerError, "failed to get user")
	}
	// A renewal must not outlive the principal's right to hold a session. The
	// OAuth subject is the user id, so the record fetched above is the whole
	// answer — no second lookup needed. Without this the grant keeps handing a
	// suspended account fresh credentials; they are denied at request time by
	// TokenLiveness, but issuing them at all is the wrong answer and leaves the
	// two refresh paths disagreeing about what a disabled account may do.
	if !userEnabled(user) {
		return nil, NewOAuthError(ErrOAuthInvalidGrant, "invalid refresh token")
	}

	// Generate new tokens
	scopes := req.Scope
	if len(scopes) == 0 {
		scopes = token.Scopes
	}

	// Bind a presented DPoP proof (RFC 9449) into cnf.jkt. No proof => bearer.
	cnf, err := p.bindDPoP(req)
	if err != nil {
		return nil, err
	}

	// Rotate: the presented credential is consumed. generateTokenResponse mints
	// its replacement, so leaving the old one live would let a captured refresh
	// token be replayed for the rest of its TTL (OAuth 2.1 §4.13.2).
	if err := p.tokenService.storage.RevokeToken(withTokenTenant(ctx, token.TenantID), token.JTI); err != nil {
		return nil, NewOAuthError(ErrOAuthServerError, "failed to rotate refresh token")
	}

	// Refresh tokens inherit audience from original token (no resource param),
	// and the SESSION IDENTIFIER the login established: a rotation renews a
	// session, it does not start one, so the renewed bearer must carry the sid
	// its login handed out. A session recorded before the claim existed carries
	// none, and the renewed token gets a fresh one — which reads to a client as
	// a new session rather than as the same one, the safe direction.
	return p.generateTokenResponseForSession(ctx, user, token.ClientID, scopes, "", "", cnf,
		grantSession{ID: refreshedSessionID(token), Renewal: true})
}

// sessionEstablishedID returns the identifier to freeze into a grant's refresh
// record: what a renewal was given, or what a fresh login just minted. See
// grantSession for why a renewal must not adopt a minted value.
func sessionEstablishedID(session grantSession, minted string) string {
	if session.Renewal {
		return session.ID
	}
	return minted
}

// refreshedSessionID returns the session identifier frozen into a refresh
// token's record, or "" when the record predates the claim (or carries no
// session context at all, as a token stored by an older writer may not).
func refreshedSessionID(token *Token) string {
	if token == nil || token.Session == nil {
		return ""
	}
	return token.Session.SessionID
}

// defaultAudienceForClient resolves the configured default service audience for
// the given OAuth client, returning "" when the client is unknown, has no
// default configured, or the stored default fails validation. It is used by the
// token-issuance path to stamp a service audience when the request carried no
// RFC 8707 resource indicator. A lookup failure is treated as "no default"
// (fail-open to today's behavior) rather than failing token issuance: the
// default audience is an opt-in convenience, not a security gate.
func (p *OIDCProvider) defaultAudienceForClient(ctx context.Context, clientID string) string {
	if clientID == "" {
		return ""
	}
	app, err := p.appStorage.GetApp(ctx, clientID)
	if err != nil {
		return ""
	}
	return p.validDefaultAudience(app)
}

// validDefaultAudience returns the app's configured default service audience
// only when it passes validation; an absent or malformed default yields "" so
// the audience falls back to today's client/issuer-only behavior.
func (p *OIDCProvider) validDefaultAudience(app *Application) string {
	if app == nil || app.DefaultAudience == "" {
		return ""
	}
	if err := ValidateDefaultAudience(app.DefaultAudience); err != nil {
		return ""
	}
	return app.DefaultAudience
}

// grantSession names the login an OAuth grant's tokens belong to.
//
// Renewal is not decoration: it decides what the ROTATED refresh record freezes
// when ID is empty, and the two cases want opposite things. A fresh login
// freezes the identifier its access token was minted with, because the login is
// where that identifier is established. A renewal freezes what it was GIVEN —
// including nothing at all, for a session recorded before the claim existed.
// Promoting the identifier minted for one of those would make a value invented
// at a rotation indistinguishable from one a login established, and every later
// renewal would replay it as though the session's identity had been recovered.
// It was not, and it is unrecoverable: a claim that says "this is the login"
// must not be answered by a guess. This is the same rule RefreshSession follows
// by carrying its stored session context forward untouched, stated explicitly
// here because this path rebuilds the context from the user record.
type grantSession struct {
	// ID is the session identifier the login established, or "" when this is a
	// fresh login (one is minted) or a session that predates the claim.
	ID string

	// Renewal marks a grant that RENEWS an existing session rather than
	// establishing one.
	Renewal bool
}

// generateTokenResponse mints the credential set for a FRESH login: the access
// token carries a newly minted session identifier (sid), so this grant is a
// different session from every other. A renewal must not go through here — see
// generateTokenResponseForSession.
func (p *OIDCProvider) generateTokenResponse(ctx context.Context, user *User, clientID string, scopes []string, nonce string, resource string, cnf *ConfirmationClaim) (*TokenResponse, error) {
	return p.generateTokenResponseForSession(ctx, user, clientID, scopes, nonce, resource, cnf, grantSession{})
}

// generateTokenResponseForSession is generateTokenResponse with the grant's
// session stated: a fresh login mints an identifier and freezes it, and a
// renewal replays and re-freezes the one it was given. It exists as the named
// variant rather than an eighth positional parameter on the common path so the
// two intents are stated at the call site instead of encoded in a bare string.
func (p *OIDCProvider) generateTokenResponseForSession(ctx context.Context, user *User, clientID string, scopes []string, nonce string, resource string, cnf *ConfirmationClaim, session grantSession) (*TokenResponse, error) {
	now := time.Now()

	// Build audience with resource indicator (RFC 8707). When the request
	// carried no resource indicator, fall back to the client app's configured
	// default service audience (if any) so downstream validators accept the
	// token. A client-supplied resource always wins.
	audience := BuildAudienceWithDefault(p.config.Issuer, resource, p.defaultAudienceForClient(ctx, clientID))

	// Generate access token
	accessJTI, err := generateJTI()
	if err != nil {
		return nil, err
	}

	accessClaims := &Claims{
		Issuer:       p.config.Issuer,
		Subject:      user.ID,
		Audience:     audience,
		ExpiresAt:    NewUnixTime(now.Add(p.config.AccessTokenTTL)),
		IssuedAt:     NewUnixTime(now),
		NotBefore:    NewUnixTime(now),
		JTI:          accessJTI,
		SessionID:    session.ID,
		TenantID:     user.TenantID,
		UserID:       user.ID,
		Roles:        user.Roles,
		Groups:       user.Groups,
		Email:        user.Email,
		Name:         user.Name,
		ClientID:     clientID,
		Scope:        joinScopes(scopes),
		Confirmation: cnf,
	}

	accessToken, err := p.tokenService.GenerateAccessToken(ctx, accessClaims)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthServerError, "failed to generate access token")
	}

	// Generate refresh token. The session context is frozen here so a renewal
	// mints an access token with the same audience, roles, and (when the grant
	// was sender-constrained) the same cnf.jkt binding.
	//
	// UserID is frozen for the same reason the access token above carries it:
	// refresh credentials from either issuance path live in ONE store, so this
	// one can be redeemed through the session-refresh endpoint too. Without the
	// immutable handle here that renewal would mint a token carrying no uid —
	// and a token with neither uid nor a service marker is DENIED outright by
	// principalLive, so the renewed session is dead at its next liveness check.
	//
	// THIS LINE IS LOAD-BEARING, NOT BELT-AND-BRACES. The comment here used to
	// say such a token landed on a lenient "legacy subject rule" that would
	// drain as old tokens aged out. That rule was deleted with the guessing
	// path (#390) and nothing drains; the consequence is refusal, not
	// degradation. Anyone tidying this on the strength of the old wording would
	// have caused silent session death rather than a soft landing (#278).
	refreshSession := TokenSessionContext{
		// What this session was ESTABLISHED with, which is not always what the
		// bearer above carries. A fresh login establishes it here, so the minted
		// value is frozen and every later rotation replays it. A renewal
		// establishes nothing: it freezes what it was given, and a session that
		// predates the claim therefore stays without one rather than adopting
		// the identifier this rotation had to mint. See grantSession.
		SessionID: sessionEstablishedID(session, accessClaims.SessionID),
		UserID:    user.ID,
		Roles:     user.Roles,
		Groups:    user.Groups,
		Audience:  audience,
		Email:     user.Email,
		Name:      user.Name,
	}
	if cnf != nil {
		refreshSession.Jkt = cnf.Jkt
	}
	refreshToken, err := p.tokenService.GenerateRefreshToken(ctx, SessionRefreshOptions{
		Subject:  user.ID,
		TenantID: user.TenantID,
		ClientID: clientID,
		Scopes:   scopes,
		Session:  refreshSession,
	})
	if err != nil {
		return nil, NewOAuthError(ErrOAuthServerError, "failed to generate refresh token")
	}

	response := &TokenResponse{
		AccessToken:  accessToken,
		TokenType:    tokenTypeForConfirmation(cnf),
		ExpiresIn:    int64(p.config.AccessTokenTTL.Seconds()),
		RefreshToken: refreshToken,
		Scope:        joinScopes(scopes),
	}

	// Generate ID token if openid scope is present
	if contains(scopes, "openid") {
		idTokenClaims := &IDTokenClaims{
			Issuer:        p.config.Issuer,
			Subject:       user.ID,
			Audience:      []string{clientID},
			ExpiresAt:     NewUnixTime(now.Add(p.config.IDTokenTTL)),
			IssuedAt:      NewUnixTime(now),
			AuthTime:      NewUnixTime(now),
			Nonce:         nonce,
			Email:         user.Email,
			EmailVerified: true,
			Name:          user.Name,
			TenantID:      user.TenantID,
			Roles:         user.Roles,
			Groups:        user.Groups,
			// MFAVerified is deliberately not set. It asserts that MFA was
			// verified in THIS session, and this authorization-code exchange
			// verified none. It used to be copied off the user record, where a
			// field of the same name recorded something else entirely and was
			// never cleared — so a user who once passed MFA carried a true
			// claim into every session afterwards.
		}

		idToken, err := p.tokenService.GenerateIDToken(ctx, idTokenClaims)
		if err != nil {
			return nil, NewOAuthError(ErrOAuthServerError, "failed to generate ID token")
		}

		response.IDToken = idToken
	}

	return response, nil
}

// IssueHardwareTokens mints a full token set (access + ID + refresh) for a
// successful hardware-attested authentication (WebAuthn FIDO2 or PIV smartcard).
// The caller is responsible for verifying the credential before calling this.
func (p *OIDCProvider) IssueHardwareTokens(ctx context.Context, opts HardwareTokenOptions) (*HardwareTokenSet, error) {
	return p.tokenService.IssueHardwareTokens(ctx, opts)
}

// Helper functions

// generateJWKS builds a single-key JWKS for the legacy (non-manager) signing
// path. The kid is derived from the key's public SPKI so it is stable across
// restarts and matches the kid stamped onto tokens, rather than a hardcoded
// "default".
func generateJWKS(key crypto.Signer, algorithm string) (*jose.JSONWebKeySet, error) {
	kid, err := computeKID(key.Public())
	if err != nil {
		return nil, err
	}
	if algorithm == "" {
		algorithm = string(jose.RS256)
	}
	jwk := jose.JSONWebKey{
		Key:       key.Public(),
		KeyID:     kid,
		Algorithm: algorithm,
		Use:       "sig",
	}

	return &jose.JSONWebKeySet{
		Keys: []jose.JSONWebKey{jwk},
	}, nil
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}

func joinScopes(scopes []string) string {
	if len(scopes) == 0 {
		return ""
	}
	result := ""
	for i, s := range scopes {
		if i > 0 {
			result += " "
		}
		result += s
	}
	return result
}
