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
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"net/url"
	"strings"
	"time"

	jose "github.com/go-jose/go-jose/v4"
	"github.com/golang-jwt/jwt/v5"
)

// ProofType is the required JOSE typ header value for a DPoP proof (RFC 9449
// §4.2): the proof JWT MUST carry "dpop+jwt".
const ProofType = "dpop+jwt"

// DefaultSkew is the default allowed clock skew for the proof iat claim.
const DefaultSkew = 60 * time.Second

// DefaultProofLifetime is how long a proof jti is retained in the replay cache
// when the proof itself carries no exp. A proof is single-use; the lifetime
// bounds the replay window to the skew plus a small margin.
const DefaultProofLifetime = 5 * time.Minute

// allowedAlgs is the set of asymmetric JWS algorithms accepted on a DPoP proof.
// Symmetric algorithms (HS*) and "none" are rejected: a DPoP proof must be
// signed by a public-key pair so the holder demonstrates possession.
var allowedAlgs = map[string]struct{}{
	"ES256": {}, "ES384": {}, "ES512": {},
	"RS256": {}, "RS384": {}, "RS512": {},
	"PS256": {}, "PS384": {}, "PS512": {},
	"EdDSA": {},
}

// joseSigAlgs restricts the algorithms the embedded jwk header is parsed for.
var joseSigAlgs = []jose.SignatureAlgorithm{
	jose.ES256, jose.ES384, jose.ES512,
	jose.RS256, jose.RS384, jose.RS512,
	jose.PS256, jose.PS384, jose.PS512,
	jose.EdDSA,
}

// VerifyOptions configures a single proof verification.
type VerifyOptions struct {
	// HTM is the expected HTTP method (e.g. "POST"). Compared case-insensitively.
	HTM string
	// HTU is the expected HTTP target URI: scheme + host + path, with any query
	// and fragment stripped before comparison (RFC 9449 §4.3).
	HTU string
	// ExpectedJkt, when non-empty, requires the SHA-256 JWK thumbprint of the
	// proof key to equal this value (the access-token cnf.jkt binding). Empty
	// means "no binding check" — used at issuance, where the thumbprint is
	// computed and returned rather than compared.
	ExpectedJkt string
	// Skew overrides the allowed iat clock skew. Zero uses DefaultSkew.
	Skew time.Duration
	// Clock overrides the time source. Nil uses SystemClock.
	Clock Clock
	// ReplayCache records the proof jti to reject replays. Nil disables replay
	// protection (only acceptable for stateless issuance-time binding where the
	// caller has its own single-use guarantee).
	ReplayCache ReplayCache
}

// VerifiedProof is the result of a successful verification.
type VerifiedProof struct {
	// Jkt is the RFC 7638 base64url SHA-256 thumbprint of the proof key. At
	// issuance this is bound into the token cnf.jkt; at validation it equals
	// the token cnf.jkt.
	Jkt string
	// JTI is the proof's unique identifier.
	JTI string
	// PublicKey is the embedded proof public key.
	PublicKey crypto.PublicKey
}

// VerifyProof validates a DPoP proof JWT per RFC 9449: it checks the typ header,
// that the algorithm is asymmetric, that the signature verifies against the
// embedded jwk, that htm/htu match the request, that iat is within skew, that
// the jti has not been replayed, and — when opts.ExpectedJkt is set — that the
// proof-key thumbprint equals the access-token cnf.jkt binding. Every failure
// returns a typed error so callers can fail closed with 401.
func VerifyProof(proofJWT string, opts VerifyOptions) (*VerifiedProof, error) {
	if strings.TrimSpace(proofJWT) == "" {
		return nil, &MalformedProofError{Detail: "empty proof"}
	}

	clock := opts.Clock
	if clock == nil {
		clock = SystemClock{}
	}
	skew := opts.Skew
	if skew <= 0 {
		skew = DefaultSkew
	}

	// Parse unverified first to read the protected header (typ + embedded jwk).
	// jwt.MapClaims is used so DPoP's bespoke claim set (htm/htu) is preserved
	// without colliding with RegisteredClaims field semantics. The standard
	// registered-claim validators (exp/nbf) still run; the DPoP-specific iat
	// skew and jti replay checks are applied explicitly below.
	parser := jwt.NewParser(
		jwt.WithValidMethods(allowedAlgList()),
		jwt.WithTimeFunc(clock.Now),
	)
	unverified, _, err := parser.ParseUnverified(proofJWT, jwt.MapClaims{})
	if err != nil {
		return nil, &MalformedProofError{Detail: "cannot parse proof", Cause: err}
	}

	// typ MUST be dpop+jwt.
	if typ, _ := unverified.Header["typ"].(string); typ != ProofType {
		return nil, &MalformedProofError{Detail: "header typ must be " + ProofType}
	}

	// alg MUST be an asymmetric algorithm.
	alg, _ := unverified.Header["alg"].(string)
	if _, ok := allowedAlgs[alg]; !ok {
		return nil, &MalformedProofError{Detail: "unsupported or non-asymmetric alg: " + alg}
	}

	// The proof key is embedded in the jwk header. Parse it with go-jose so we
	// get RFC 7638 thumbprint support.
	rawJWK, ok := unverified.Header["jwk"]
	if !ok {
		return nil, &MalformedProofError{Detail: "missing jwk header"}
	}
	jwk, err := parseEmbeddedJWK(rawJWK)
	if err != nil {
		return nil, err
	}
	if !jwk.IsPublic() {
		return nil, &MalformedProofError{Detail: "jwk header must contain a public key"}
	}

	pub := jwk.Key
	if !isAsymmetricPublic(pub) {
		return nil, &MalformedProofError{Detail: "jwk header key is not an asymmetric public key"}
	}

	// Verify the signature against the embedded key, enforcing the same alg.
	claims := jwt.MapClaims{}
	verified, err := parser.ParseWithClaims(proofJWT, claims, func(t *jwt.Token) (interface{}, error) {
		if t.Method.Alg() != alg {
			return nil, &MalformedProofError{Detail: "alg header mismatch"}
		}
		return pub, nil
	})
	if err != nil {
		return nil, &SignatureError{Cause: err}
	}
	if !verified.Valid {
		return nil, &SignatureError{}
	}

	htm := stringClaim(claims, "htm")
	htu := stringClaim(claims, "htu")
	jti := stringClaim(claims, "jti")

	// htm must match the request method (case-insensitive per HTTP semantics).
	if !strings.EqualFold(htm, opts.HTM) {
		return nil, &HTTPBindingError{Field: "htm", Expected: opts.HTM, Actual: htm}
	}

	// htu must match the canonicalized request URI.
	wantHTU, err := CanonicalHTU(opts.HTU)
	if err != nil {
		return nil, &HTTPBindingError{Field: "htu", Expected: opts.HTU, Actual: htu}
	}
	gotHTU, err := CanonicalHTU(htu)
	if err != nil {
		return nil, &HTTPBindingError{Field: "htu", Expected: wantHTU, Actual: htu}
	}
	if gotHTU != wantHTU {
		return nil, &HTTPBindingError{Field: "htu", Expected: wantHTU, Actual: gotHTU}
	}

	// iat within skew window (must be present).
	iatSecs, ok := numericClaim(claims, "iat")
	if !ok {
		return nil, &MalformedProofError{Detail: "missing iat"}
	}
	now := clock.Now()
	iat := time.Unix(iatSecs, 0)
	if iat.After(now.Add(skew)) {
		return nil, &StaleProofError{Detail: "iat is in the future beyond skew"}
	}
	if iat.Before(now.Add(-skew)) {
		return nil, &StaleProofError{Detail: "iat is too old"}
	}

	// jti must be present and not replayed.
	if strings.TrimSpace(jti) == "" {
		return nil, &MalformedProofError{Detail: "missing jti"}
	}
	if opts.ReplayCache != nil {
		expiresAt := iat.Add(DefaultProofLifetime)
		if expSecs, ok := numericClaim(claims, "exp"); ok && expSecs != 0 {
			expiresAt = time.Unix(expSecs, 0)
		}
		if opts.ReplayCache.SeenBefore(jti, expiresAt) {
			return nil, &ReplayError{JTI: jti}
		}
	}

	// Compute the RFC 7638 thumbprint and, when bound, compare to cnf.jkt.
	jkt, err := Thumbprint(pub)
	if err != nil {
		return nil, err
	}
	if opts.ExpectedJkt != "" && jkt != opts.ExpectedJkt {
		return nil, &ThumbprintMismatchError{Expected: opts.ExpectedJkt, Actual: jkt}
	}

	return &VerifiedProof{Jkt: jkt, JTI: jti, PublicKey: pub}, nil
}

// Thumbprint computes the RFC 7638 / RFC 9449 jkt: base64url(SHA-256(canonical
// JWK)) of a public key. This is the value stored in a token's cnf.jkt.
func Thumbprint(pub crypto.PublicKey) (string, error) {
	jwk := jose.JSONWebKey{Key: pub}
	sum, err := jwk.Thumbprint(crypto.SHA256)
	if err != nil {
		return "", &SignerError{Detail: "compute jwk thumbprint", Cause: err}
	}
	return base64.RawURLEncoding.EncodeToString(sum), nil
}

// CanonicalHTU strips the query and fragment from a URI and returns scheme +
// host + path, the form RFC 9449 §4.3 compares for htu. The host is
// lower-cased; the scheme is lower-cased by url.Parse.
func CanonicalHTU(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", &MalformedProofError{Detail: "invalid htu URI", Cause: err}
	}
	if u.Scheme == "" || u.Host == "" {
		return "", &MalformedProofError{Detail: "htu must be an absolute URI"}
	}
	canon := url.URL{
		Scheme: strings.ToLower(u.Scheme),
		Host:   strings.ToLower(u.Host),
		Path:   u.Path,
	}
	return canon.String(), nil
}

// parseEmbeddedJWK reconstructs a jose.JSONWebKey from the proof's jwk header,
// which the JWT library hands back as a decoded map. It is re-marshaled to JSON
// and parsed so go-jose validates the key structure.
func parseEmbeddedJWK(raw interface{}) (*jose.JSONWebKey, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, &MalformedProofError{Detail: "cannot re-marshal jwk header", Cause: err}
	}
	var jwk jose.JSONWebKey
	if err := jwk.UnmarshalJSON(data); err != nil {
		return nil, &MalformedProofError{Detail: "cannot parse jwk header", Cause: err}
	}
	if !jwk.Valid() {
		return nil, &MalformedProofError{Detail: "jwk header is not a valid key"}
	}
	return &jwk, nil
}

// isAsymmetricPublic reports whether k is one of the supported asymmetric
// public-key types.
func isAsymmetricPublic(k crypto.PublicKey) bool {
	switch k.(type) {
	case *ecdsa.PublicKey, *rsa.PublicKey, ed25519.PublicKey:
		return true
	default:
		return false
	}
}

// stringClaim returns the string value of a MapClaims key, or "" if absent or
// not a string.
func stringClaim(c jwt.MapClaims, key string) string {
	if v, ok := c[key].(string); ok {
		return v
	}
	return ""
}

// numericClaim returns the integer seconds value of a numeric MapClaims key.
// JSON numbers decode to float64; the bool reports whether the key was a number.
func numericClaim(c jwt.MapClaims, key string) (int64, bool) {
	switch v := c[key].(type) {
	case float64:
		return int64(v), true
	case int64:
		return v, true
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

// allowedAlgList returns the allowed algorithms as a slice for jwt.WithValidMethods.
func allowedAlgList() []string {
	out := make([]string, 0, len(allowedAlgs))
	for a := range allowedAlgs {
		out = append(out, a)
	}
	return out
}
