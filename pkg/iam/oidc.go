//go:build iam

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
			"cnf", "amr", "attest_method",
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

	amr := ServiceAccountAMR(req.AttestMethod)

	opts := ServiceAccountTokenOptions{
		ClientID:           req.ClientID,
		TenantID:           app.TenantID,
		Scope:              req.Scope,
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
		Scope:       req.Scope,
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

	// Derive the tenant from the verified claim and scope the context so the
	// per-tenant revocation lookup resolves. There is no global token table;
	// the tenant_id claim is the authoritative routing key.
	if claims.TenantID != "" {
		ctx = ContextWithTenant(ctx, claims.TenantID)
	}

	revoked, err := p.tokenService.storage.IsTokenRevoked(ctx, claims.JTI)
	if err == nil && revoked {
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

// UserInfo returns user information for the given access token.
func (p *OIDCProvider) UserInfo(ctx context.Context, accessToken string) (*UserInfoResponse, error) {
	// Validate access token
	claims, err := p.ValidateAccessToken(ctx, accessToken)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthInvalidGrant, "invalid access token")
	}

	// Get user
	user, err := p.identityStore.GetUser(ctx, claims.TenantID, claims.Subject)
	if err != nil {
		return nil, NewOAuthError(ErrOAuthServerError, "failed to get user info")
	}

	return &UserInfoResponse{
		Sub:           user.ID,
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
	// Validate client
	valid, err := p.appStorage.ValidateClientSecret(ctx, req.ClientID, req.ClientSecret)
	if err != nil || !valid {
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
	claims := &Claims{
		Issuer:       p.config.Issuer,
		Subject:      req.ClientID,
		Audience:     audience,
		ExpiresAt:    NewUnixTime(now.Add(p.config.AccessTokenTTL)),
		IssuedAt:     NewUnixTime(now),
		NotBefore:    NewUnixTime(now),
		JTI:          generateJTI(),
		TenantID:     app.TenantID,
		ClientID:     req.ClientID,
		Scope:        joinScopes(req.Scope),
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
		Scope:       joinScopes(req.Scope),
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

	// Refresh tokens inherit audience from original token (no resource param)
	return p.generateTokenResponse(ctx, user, token.ClientID, scopes, "", "", cnf)
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

func (p *OIDCProvider) generateTokenResponse(ctx context.Context, user *User, clientID string, scopes []string, nonce string, resource string, cnf *ConfirmationClaim) (*TokenResponse, error) {
	now := time.Now()

	// Build audience with resource indicator (RFC 8707). When the request
	// carried no resource indicator, fall back to the client app's configured
	// default service audience (if any) so downstream validators accept the
	// token. A client-supplied resource always wins.
	audience := BuildAudienceWithDefault(p.config.Issuer, resource, p.defaultAudienceForClient(ctx, clientID))

	// Generate access token
	accessClaims := &Claims{
		Issuer:       p.config.Issuer,
		Subject:      user.ID,
		Audience:     audience,
		ExpiresAt:    NewUnixTime(now.Add(p.config.AccessTokenTTL)),
		IssuedAt:     NewUnixTime(now),
		NotBefore:    NewUnixTime(now),
		JTI:          generateJTI(),
		TenantID:     user.TenantID,
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

	// Generate refresh token
	refreshToken, err := p.tokenService.GenerateRefreshToken(ctx, user.ID, clientID)
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
			MFAVerified:   user.MFAVerified,
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
