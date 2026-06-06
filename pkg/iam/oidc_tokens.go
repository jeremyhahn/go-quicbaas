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
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"time"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

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
func (s *TokenService) GenerateAccessToken(ctx context.Context, claims *Claims) (string, error) {
	// Build claims map for XKMS JWT signing
	jwtClaims := xkms.JWTClaims{
		"iss":          claims.Issuer,
		"sub":          claims.Subject,
		"aud":          claims.Audience,
		"exp":          claims.ExpiresAt.Unix(),
		"iat":          claims.IssuedAt.Unix(),
		"nbf":          claims.NotBefore.Unix(),
		"jti":          claims.JTI,
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

	// Emit scp (array-form scope) for the platform access-token contract. When
	// the caller did not populate SCP, derive it from the space-separated Scope
	// so downstream RBAC has a consistent array to map.
	if len(claims.SCP) > 0 {
		jwtClaims["scp"] = claims.SCP
	} else if claims.Scope != "" {
		jwtClaims["scp"] = parseScopes(claims.Scope)
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
	tokenString, err := xkms.JWTSignWithSignerKID(signer, jwtClaims, alg, kid)
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

	if err := s.storage.StoreToken(ctx, tokenMeta); err != nil {
		return "", dberrors.Wrapf(err, dberrors.ErrStorage, "iam.TokenService.GenerateAccessToken", "failed to store token")
	}

	return tokenString, nil
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

// GenerateRefreshToken generates a new refresh token.
func (s *TokenService) GenerateRefreshToken(ctx context.Context, userID, clientID string) (string, error) {
	// Generate secure random token
	tokenBytes, err := xkms.Rand(32)
	if err != nil {
		return "", dberrors.Wrapf(err, dberrors.ErrInternal, "iam.TokenService.GenerateRefreshToken", "failed to generate random token")
	}
	tokenString := base64.RawURLEncoding.EncodeToString(tokenBytes)

	now := time.Now()
	jti := generateJTI()

	// Store token
	token := &Token{
		JTI:       jti,
		Subject:   userID,
		ClientID:  clientID,
		Expires:   now.Add(s.config.RefreshTokenTTL),
		Issued:    now,
		TokenType: TokenTypeRefresh,
	}

	if err := s.storage.StoreToken(ctx, token); err != nil {
		return "", dberrors.Wrapf(err, dberrors.ErrStorage, "iam.TokenService.GenerateRefreshToken", "failed to store refresh token")
	}

	// Return the token value (JTI is used for internal tracking, tokenString is returned to client)
	return tokenString, nil
}

// GenerateAuthCode generates a new authorization code.
func (s *TokenService) GenerateAuthCode(ctx context.Context, req *AuthorizeRequest, userID string) (string, error) {
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
			ctx = ContextWithTenant(ctx, tid)
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

// generateJTI generates a unique JWT ID.
func generateJTI() string {
	b, err := xkms.Rand(16)
	if err != nil {
		// Fallback to timestamp-based ID if random fails
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b)
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
	// Use ACRHardwareAttested for hardware-backed credentials.
	ACR string
	// AttestAAGUID is the hex-encoded FIDO2 AAGUID (WebAuthn only; empty for PIV).
	AttestAAGUID string
	// AttestLevel is a human-readable attestation level.
	AttestLevel string
	// ClientID is the OAuth2 client on behalf of which tokens are issued.
	// May be empty when no client_id is applicable (direct hardware login).
	ClientID string
	// Scope is the space-separated list of granted scopes (defaults to "openid profile email").
	Scope string
}

// ACRHardwareAttested is the ACR value for hardware-attested authentication.
const ACRHardwareAttested = "urn:quicbaas:auth:hardware-attested"

// IssueHardwareTokens mints an access token, ID token, and refresh token for a
// successfully hardware-attested login (WebAuthn FIDO2 or PIV smartcard).
func (s *TokenService) IssueHardwareTokens(ctx context.Context, opts HardwareTokenOptions) (*HardwareTokenSet, error) {
	if opts.User == nil {
		return nil, dberrors.New(dberrors.ErrInvalidInput, "iam.TokenService.IssueHardwareTokens", "user is required")
	}

	now := time.Now()
	scope := opts.Scope
	if scope == "" {
		scope = "openid profile email"
	}
	issuer := s.config.Issuer

	accessClaims := &Claims{
		Issuer:    issuer,
		Subject:   opts.User.ID,
		Audience:  []string{issuer},
		ExpiresAt: UnixTime{Time: now.Add(s.config.AccessTokenTTL)},
		IssuedAt:  UnixTime{Time: now},
		NotBefore: UnixTime{Time: now},
		JTI:       generateJTI(),
		TenantID:  opts.User.TenantID,
		Roles:     opts.User.Roles,
		Groups:    opts.User.Groups,
		Email:     opts.User.Email,
		Name:      opts.User.Name,
		Scope:     scope,
		ClientID:  opts.ClientID,
		AMR:       opts.AMR,
		ACR:       opts.ACR,
	}

	accessToken, err := s.GenerateAccessToken(ctx, accessClaims)
	if err != nil {
		return nil, err
	}

	idClaims := &IDTokenClaims{
		Issuer:        issuer,
		Subject:       opts.User.ID,
		Audience:      []string{issuer},
		ExpiresAt:     UnixTime{Time: now.Add(s.config.IDTokenTTL)},
		IssuedAt:      UnixTime{Time: now},
		AuthTime:      UnixTime{Time: now},
		Email:         opts.User.Email,
		EmailVerified: true,
		Name:          opts.User.Name,
		TenantID:      opts.User.TenantID,
		Roles:         opts.User.Roles,
		Groups:        opts.User.Groups,
		MFAVerified:   true,
		AMR:           opts.AMR,
		ACR:           opts.ACR,
		AttestAAGUID:  opts.AttestAAGUID,
		AttestLevel:   opts.AttestLevel,
	}

	idToken, err := s.GenerateIDToken(ctx, idClaims)
	if err != nil {
		return nil, err
	}

	refreshToken, err := s.GenerateRefreshToken(ctx, opts.User.ID, opts.ClientID)
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

	jwtClaims := xkms.JWTClaims{
		"iss":           issuer,
		"sub":           opts.ClientID,
		"aud":           []string{issuer},
		"exp":           now.Add(s.config.AccessTokenTTL).Unix(),
		"iat":           now.Unix(),
		"nbf":           now.Unix(),
		"jti":           generateJTI(),
		"tenant_id":     opts.TenantID,
		"scope":         scope,
		"client_id":     opts.ClientID,
		"auth_method":   "mtls",
		"attest_method": opts.AttestMethod,
		"amr":           opts.AMR,
		"cnf": map[string]interface{}{
			"x5t#S256": opts.CertThumbprintS256,
		},
	}

	signer, alg, kid, err := s.signingMaterial(ctx, opts.TenantID)
	if err != nil {
		return nil, dberrors.Wrapf(err, dberrors.ErrInternal, "iam.TokenService.IssueServiceAccountTokens", "failed to resolve signing key")
	}
	tokenString, err := xkms.JWTSignWithSignerKID(signer, jwtClaims, alg, kid)
	if err != nil {
		return nil, dberrors.Wrapf(err, dberrors.ErrInternal, "iam.TokenService.IssueServiceAccountTokens", "failed to sign service account token")
	}

	tokenMeta := &Token{
		JTI:       jwtClaims["jti"].(string),
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
