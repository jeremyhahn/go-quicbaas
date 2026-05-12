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
	config     *OIDCConfig
}

// NewTokenService creates a new token service.
func NewTokenService(storage TokenStorage, signingKey crypto.Signer, config *OIDCConfig) *TokenService {
	return &TokenService{
		storage:    storage,
		signingKey: signingKey,
		config:     config,
	}
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

	// Sign token using XKMS JWT
	tokenString, err := xkms.JWTSignWithSigner(s.signingKey, jwtClaims, s.config.SigningAlgorithm)
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

	// Sign token using XKMS JWT
	tokenString, err := xkms.JWTSignWithSigner(s.signingKey, jwtClaims, s.config.SigningAlgorithm)
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
	// Try to parse as JWT first (without verification to extract JTI)
	claims, err := xkms.JWTParse(tokenValue)
	if err == nil && claims != nil {
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

	tokenString, err := xkms.JWTSignWithSigner(s.signingKey, jwtClaims, s.config.SigningAlgorithm)
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
