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

// validatePKCES256 validates a PKCE code verifier against a S256 challenge.
func validatePKCES256(verifier, challenge string) bool {
	// Compute SHA256 hash of verifier
	h := sha256.New()
	h.Write([]byte(verifier))
	computed := base64.RawURLEncoding.EncodeToString(h.Sum(nil))

	// Compare with challenge
	return computed == challenge
}
