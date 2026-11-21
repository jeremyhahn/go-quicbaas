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

package dpop

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

// NewProof builds and signs a DPoP proof JWT for the given key bound to the
// HTTP method and URI, using the system clock for iat. See NewProofWithClock.
// It carries no ath; a request that presents an access token uses
// NewProofForAccessToken.
func NewProof(key crypto.Signer, htm, htu string) (string, error) {
	return NewProofWithClock(key, htm, htu, SystemClock{})
}

// NewProofWithClock builds and signs a DPoP proof JWT for the given key bound
// to the HTTP method and URI. The embedded jwk header carries the corresponding
// public key so a verifier can both check the signature and derive the cnf.jkt
// thumbprint. Supported keys: ECDSA (P-256/384/521), RSA, and Ed25519. The
// clock supplies iat (injectable for deterministic tests).
//
// This is the client/SDK side of RFC 9449: a sender holding the DPoP private
// key produces a fresh single-use proof per request.
func NewProofWithClock(key crypto.Signer, htm, htu string, clock Clock) (string, error) {
	return signProof(key, htm, htu, "", clock)
}

// NewProofForAccessToken builds a DPoP proof for a request that presents
// accessToken, carrying its ath hash (RFC 9449 §4.3), using the system clock.
func NewProofForAccessToken(key crypto.Signer, htm, htu, accessToken string) (string, error) {
	return NewProofForAccessTokenWithClock(key, htm, htu, accessToken, SystemClock{})
}

// NewProofForAccessTokenWithClock is NewProofForAccessToken with an
// injectable clock. An empty accessToken is refused: a proof without ath is
// NewProofWithClock's.
func NewProofForAccessTokenWithClock(key crypto.Signer, htm, htu, accessToken string, clock Clock) (string, error) {
	if accessToken == "" {
		return "", &SignerError{Detail: "empty access token"}
	}
	return signProof(key, htm, htu, accessToken, clock)
}

// signProof signs a proof for htm and htu, with the ath of accessToken when it
// is non-empty.
func signProof(key crypto.Signer, htm, htu, accessToken string, clock Clock) (string, error) {
	if key == nil {
		return "", &SignerError{Detail: "nil signing key"}
	}
	if clock == nil {
		clock = SystemClock{}
	}

	alg, err := algForKey(key)
	if err != nil {
		return "", err
	}

	jwk := jose.JSONWebKey{Key: key.Public()}
	jwkJSON, err := jwk.MarshalJSON()
	if err != nil {
		return "", &SignerError{Detail: "marshal public jwk", Cause: err}
	}
	var jwkMap map[string]interface{}
	if err := json.Unmarshal(jwkJSON, &jwkMap); err != nil {
		return "", &SignerError{Detail: "decode public jwk", Cause: err}
	}

	method := jwt.GetSigningMethod(alg)
	if method == nil {
		return "", &SignerError{Detail: "unsupported algorithm: " + alg}
	}

	now := clock.Now()
	jti, err := randomJTI()
	if err != nil {
		return "", err
	}
	claims := jwt.MapClaims{
		"htm": htm,
		"htu": htu,
		"jti": jti,
		"iat": now.Unix(),
	}
	if accessToken != "" {
		claims["ath"] = AccessTokenHash(accessToken)
	}

	token := jwt.NewWithClaims(method, claims)
	token.Header["typ"] = ProofType
	token.Header["jwk"] = jwkMap

	signed, err := token.SignedString(key)
	if err != nil {
		return "", &SignerError{Detail: "sign proof", Cause: err}
	}
	return signed, nil
}

// algForKey selects the JWS algorithm for a signing key. ECDSA curve selects
// ES256/384/512; RSA uses RS256; Ed25519 uses EdDSA.
func algForKey(key crypto.Signer) (string, error) {
	switch pub := key.Public().(type) {
	case *ecdsa.PublicKey:
		switch pub.Curve {
		case elliptic.P256():
			return "ES256", nil
		case elliptic.P384():
			return "ES384", nil
		case elliptic.P521():
			return "ES512", nil
		default:
			return "", &SignerError{Detail: "unsupported ECDSA curve"}
		}
	case *rsa.PublicKey:
		return "RS256", nil
	case ed25519.PublicKey:
		return "EdDSA", nil
	default:
		return "", &SignerError{Detail: "unsupported key type for DPoP proof"}
	}
}

// randomJTI returns a 128-bit base64url-encoded random proof identifier.
func randomJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", &SignerError{Detail: "generate jti", Cause: err}
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
