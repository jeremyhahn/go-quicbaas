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

// Package auth provides JWT token utilities shared between managers
package auth

import (
	"crypto"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
)

// jwtSigningMethods maps algorithm name strings to jwt.SigningMethod instances.
// Uses map-based dispatch for O(1) constant-time lookup.
var jwtSigningMethods = map[string]jwt.SigningMethod{
	"ES256": jwt.SigningMethodES256,
	"ES384": jwt.SigningMethodES384,
	"ES512": jwt.SigningMethodES512,
	"RS256": jwt.SigningMethodRS256,
	"RS384": jwt.SigningMethodRS384,
	"RS512": jwt.SigningMethodRS512,
}

// resolveSigningMethod returns the jwt.SigningMethod for the given algorithm name.
// Defaults to ES256 if the algorithm is empty or unrecognized.
func resolveSigningMethod(algorithm string) jwt.SigningMethod {
	if method, ok := jwtSigningMethods[algorithm]; ok {
		return method
	}
	return jwt.SigningMethodES256
}

// generateNonce creates a unique random nonce for token uniqueness.
func generateNonce() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// generateTokensForUser generates access and refresh tokens for a user.
// Tokens are signed using the provided crypto.Signer with the configured
// asymmetric algorithm.
func generateTokensForUser(user *User, config *AuthConfig, tokenStore TokenStoreInterface) (*Token, error) {
	now := time.Now()
	expiresAt := now.Add(config.TokenTTL)

	// Generate unique nonce for token uniqueness
	nonce, err := generateNonce()
	if err != nil {
		return nil, api.NewNonceGenerationError("auth.generateTokensForUser", err)
	}

	// Create access token with nonce for uniqueness
	claims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		TenantID: user.TenantID,
		Roles:    user.Roles,
		Nonce:    nonce,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(expiresAt),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Issuer:    "quicbaas",
			Subject:   user.ID,
		},
	}

	accessTokenString, err := signToken(claims, config)
	if err != nil {
		return nil, api.NewTokenSigningError("auth.generateTokensForUser", "access", err)
	}

	// Generate separate nonce for refresh token
	refreshNonce, err := generateNonce()
	if err != nil {
		return nil, api.NewNonceGenerationError("auth.generateTokensForUser", err)
	}

	// Create refresh token with its own nonce
	refreshClaims := &Claims{
		UserID:   user.ID,
		Username: user.Username,
		TenantID: user.TenantID,
		Roles:    user.Roles,
		Nonce:    refreshNonce,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(config.RefreshTokenTTL)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "quicbaas",
			Subject:   user.ID,
		},
	}

	refreshTokenString, err := signToken(refreshClaims, config)
	if err != nil {
		return nil, api.NewTokenSigningError("auth.generateTokensForUser", "refresh", err)
	}

	// Store refresh token
	tokenStore.AddRefreshToken(user.ID, refreshTokenString)

	return &Token{
		AccessToken:  accessTokenString,
		RefreshToken: refreshTokenString,
		ExpiresAt:    expiresAt,
		TokenType:    "Bearer",
	}, nil
}

// signToken signs a JWT claims set using the configured asymmetric signer.
func signToken(claims *Claims, config *AuthConfig) (string, error) {
	return signTokenAsymmetric(claims, config)
}

// signTokenAsymmetric signs a JWT token using the asymmetric crypto.Signer.
func signTokenAsymmetric(claims *Claims, config *AuthConfig) (string, error) {
	if config.jwtSigner == nil {
		return "", &api.OperationFailedError{
			Operation: "sign token: asymmetric signer not initialized",
		}
	}

	method := resolveSigningMethod(config.JWTSigningAlgorithm)
	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = config.JWTSigningKeyID

	tokenString, err := token.SignedString(config.jwtSigner)
	if err != nil {
		return "", err
	}
	return tokenString, nil
}

// validateJWTToken validates a JWT token and returns claims.
// Only asymmetric (ECDSA/RSA) signing methods are accepted.
func validateJWTToken(tokenString string, config *AuthConfig) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return resolveVerificationKey(token, config)
	})
	if err != nil {
		return nil, api.NewInvalidTokenError("auth.validateJWTToken", err)
	}

	claims, ok := token.Claims.(*Claims)
	if !ok || !token.Valid {
		return nil, api.NewInvalidTokenClaimsError("auth.validateJWTToken")
	}

	return claims, nil
}

// resolveVerificationKey returns the appropriate verification key based on
// the token's signing method. Only asymmetric methods are accepted; symmetric
// (HMAC) tokens are rejected to eliminate algorithm-confusion attacks.
func resolveVerificationKey(token *jwt.Token, config *AuthConfig) (interface{}, error) {
	switch token.Method.(type) {
	case *jwt.SigningMethodECDSA, *jwt.SigningMethodRSA:
		if config.jwtPublicKey != nil {
			return config.jwtPublicKey, nil
		}
		return nil, &api.UnexpectedSigningMethodError{
			Algorithm: fmt.Sprintf("%v (no public key configured)", token.Header["alg"]),
		}

	default:
		return nil, &api.UnexpectedSigningMethodError{
			Algorithm: fmt.Sprintf("%v", token.Header["alg"]),
		}
	}
}

// SetAsymmetricSigning configures the AuthConfig for asymmetric JWT signing.
// The signer is used for token signing; its public key is extracted for verification.
// This function is called during manager initialization when JWTSigningKeyID is set.
func SetAsymmetricSigning(config *AuthConfig, signer crypto.Signer) {
	config.jwtSigner = signer
	config.jwtPublicKey = signer.Public()
}

// SetPublicKey configures the AuthConfig with a public key for token verification only.
// This is useful for verification-only nodes that do not need to sign tokens.
func SetPublicKey(config *AuthConfig, publicKey crypto.PublicKey) {
	config.jwtPublicKey = publicKey
}
