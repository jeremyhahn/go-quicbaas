package iam

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/asn1"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"testing"

	"github.com/go-jose/go-jose/v4"
)

// signES256WithKID signs a compact JWS over a small claim set with signer,
// writing kid into the protected header, as the xkms module does with the
// key's CN.
func signES256WithKID(t *testing.T, signer crypto.Signer, kid string) string {
	t.Helper()
	header, err := json.Marshal(map[string]string{"alg": "ES256", "kid": kid, "typ": "JWT"})
	if err != nil {
		t.Fatalf("marshal header: %v", err)
	}
	payload, err := json.Marshal(map[string]string{"sub": "cn-alias-test"})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	enc := base64.RawURLEncoding
	input := enc.EncodeToString(header) + "." + enc.EncodeToString(payload)
	digest := sha256.Sum256([]byte(input))
	der, err := signer.Sign(rand.Reader, digest[:], crypto.SHA256)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	var sig struct{ R, S *big.Int }
	if _, err := asn1.Unmarshal(der, &sig); err != nil {
		t.Fatalf("parse ECDSA signature: %v", err)
	}
	raw := make([]byte, 64)
	sig.R.FillBytes(raw[:32])
	sig.S.FillBytes(raw[32:])
	return input + "." + enc.EncodeToString(raw)
}

// verifiesWith reports whether token's signature verifies under pub.
func verifiesWith(t *testing.T, token string, pub crypto.PublicKey) bool {
	t.Helper()
	jws, err := jose.ParseSigned(token, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		t.Fatalf("parse token: %v", err)
	}
	_, err = jws.Verify(pub)
	return err == nil
}

// jwksEntries returns the JWKS entries of mgr whose KeyID is kid.
func jwksEntries(mgr *SigningKeyManager, kid string) []jose.JSONWebKey {
	var out []jose.JSONWebKey
	for _, k := range mgr.JWKS().Keys {
		if k.KeyID == kid {
			out = append(out, k)
		}
	}
	return out
}

func TestSigningKeyManager_CNAliasPublishedForGlobalKey(t *testing.T) {
	setupTestXKMS(t)

	mgr, err := NewSigningKeyManagerFromXKMS(OIDCSigningKeyCN, "")
	if err != nil {
		t.Fatalf("manager init failed: %v", err)
	}

	kid, _, _ := mgr.Current()
	jwks := mgr.JWKS()
	if len(jwks.Keys) < 2 {
		t.Fatalf("expected the derived kid and the CN alias in the JWKS, got %d keys", len(jwks.Keys))
	}
	if jwks.Keys[0].KeyID != kid {
		t.Fatalf("first JWKS kid %q != current kid %q", jwks.Keys[0].KeyID, kid)
	}
	aliases := jwksEntries(mgr, OIDCSigningKeyCN)
	if len(aliases) != 1 {
		t.Fatalf("expected exactly one JWKS entry with kid %q, got %d", OIDCSigningKeyCN, len(aliases))
	}
	alias := aliases[0]
	current, ok := jwks.Keys[0].Key.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("expected ECDSA current key, got %T", jwks.Keys[0].Key)
	}
	aliasKey, ok := alias.Key.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("expected ECDSA alias key, got %T", alias.Key)
	}
	if !current.Equal(aliasKey) {
		t.Fatal("the alias entry does not carry the current public key")
	}
	if alias.Algorithm != SigningAlgorithmES256 {
		t.Fatalf("expected alias Algorithm ES256, got %q", alias.Algorithm)
	}
	if alias.Use != "sig" {
		t.Fatalf("expected alias Use sig, got %q", alias.Use)
	}
}

func TestSigningKeyManager_CNAliasVerifiesTokenSignedUnderCN(t *testing.T) {
	setupTestXKMS(t)

	mgr, err := NewSigningKeyManagerFromXKMS(OIDCSigningKeyCN, "")
	if err != nil {
		t.Fatalf("manager init failed: %v", err)
	}

	_, signer, alg := mgr.Current()
	if alg != SigningAlgorithmES256 {
		t.Fatalf("expected ES256, got %q", alg)
	}
	token := signES256WithKID(t, signer, OIDCSigningKeyCN)

	pub, err := mgr.PublicForKID(context.Background(), OIDCSigningKeyCN)
	if err != nil {
		t.Fatalf("PublicForKID(%q) failed: %v", OIDCSigningKeyCN, err)
	}
	if !verifiesWith(t, token, pub) {
		t.Fatal("the key PublicForKID resolves for the CN alias does not verify the token")
	}

	aliases := jwksEntries(mgr, OIDCSigningKeyCN)
	if len(aliases) != 1 {
		t.Fatalf("expected one JWKS entry with kid %q, got %d", OIDCSigningKeyCN, len(aliases))
	}
	if !verifiesWith(t, token, aliases[0].Key) {
		t.Fatal("the JWKS alias entry does not verify the token")
	}
}

func TestSigningKeyManager_CNAliasFollowsRotation(t *testing.T) {
	setupTestXKMS(t)

	mgr, err := NewSigningKeyManagerFromXKMS(OIDCSigningKeyCN, "")
	if err != nil {
		t.Fatalf("manager init failed: %v", err)
	}

	oldKID, oldSigner, _ := mgr.Current()
	oldToken := signES256WithKID(t, oldSigner, OIDCSigningKeyCN)

	if err := mgr.Rotate(); err != nil {
		t.Fatalf("Rotate failed: %v", err)
	}
	newKID, newSigner, _ := mgr.Current()
	if newKID == oldKID {
		t.Fatal("rotation did not change the current kid")
	}

	aliases := jwksEntries(mgr, OIDCSigningKeyCN)
	if len(aliases) != 1 {
		t.Fatalf("expected one JWKS entry with kid %q after rotation, got %d", OIDCSigningKeyCN, len(aliases))
	}
	newPub, ok := newSigner.Public().(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("expected ECDSA current key, got %T", newSigner.Public())
	}
	aliasKey, ok := aliases[0].Key.(*ecdsa.PublicKey)
	if !ok {
		t.Fatalf("expected ECDSA alias key, got %T", aliases[0].Key)
	}
	if !newPub.Equal(aliasKey) {
		t.Fatal("the alias entry does not name the new current key after rotation")
	}

	pub, err := mgr.PublicForKID(context.Background(), OIDCSigningKeyCN)
	if err != nil {
		t.Fatalf("PublicForKID(%q) after rotation failed: %v", OIDCSigningKeyCN, err)
	}
	if verifiesWith(t, oldToken, pub) {
		t.Fatal("a token signed under the old key still verifies through the CN alias")
	}
	if !verifiesWith(t, signES256WithKID(t, newSigner, OIDCSigningKeyCN), pub) {
		t.Fatal("a token signed under the new key does not verify through the CN alias")
	}
}

func TestSigningKeyManager_CNAliasOnlyForGlobalOIDCKey(t *testing.T) {
	setupTestXKMS(t)

	other, err := NewSigningKeyManagerFromXKMS("oidc-signing-test", "")
	if err != nil {
		t.Fatalf("manager for another CN failed: %v", err)
	}
	tenant, err := NewSigningKeyManagerFromXKMS(OIDCSigningKeyCN, "t1")
	if err != nil {
		t.Fatalf("tenant manager failed: %v", err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	fromSigner, err := NewSigningKeyManagerFromSigner(key, SigningAlgorithmES256)
	if err != nil {
		t.Fatalf("signer manager failed: %v", err)
	}

	for name, mgr := range map[string]*SigningKeyManager{
		"other CN":    other,
		"tenant":      tenant,
		"from signer": fromSigner,
	} {
		t.Run(name, func(t *testing.T) {
			if got := jwksEntries(mgr, OIDCSigningKeyCN); len(got) != 0 {
				t.Fatalf("JWKS publishes kid %q, want none", OIDCSigningKeyCN)
			}
			_, err := mgr.PublicForKID(context.Background(), OIDCSigningKeyCN)
			var unknownErr *UnknownKeyIDError
			if !errors.As(err, &unknownErr) {
				t.Fatalf("expected UnknownKeyIDError, got %T (%v)", err, err)
			}
		})
	}
}
