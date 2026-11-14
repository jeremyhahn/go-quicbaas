package serviceplane

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/json"
	"fmt"
	"strings"
)

// SignedManifest is a manifest exactly as it was signed: the payload bytes
// are kept, so verification never depends on re-encoding.
type SignedManifest struct {
	// Payload is the manifest's JSON encoding the signature covers (after
	// the domain prefix).
	Payload []byte `json:"payload"`
	// KeyID names the signing key in the verifier's keyring.
	KeyID string `json:"key_id"`
	// Signature is the signature over the domain prefix and Payload.
	Signature []byte `json:"signature"`
}

// Sign validates m and signs it with signer under keyID. Ed25519 signs the
// message; ECDSA signs its SHA-256 (P-256) or SHA-384 (P-384) digest.
func Sign(m *Manifest, keyID string, signer crypto.Signer) (*SignedManifest, error) {
	if signer == nil || strings.TrimSpace(keyID) == "" {
		return nil, fmt.Errorf("%w: a signer and a key id are required", ErrConfig)
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	msg, err := signingPayload(m)
	if err != nil {
		return nil, err
	}
	data, opts, err := digestFor(signer.Public(), msg)
	if err != nil {
		return nil, &SignatureError{KeyID: keyID, Reason: err.Error()}
	}
	sig, err := signer.Sign(rand.Reader, data, opts)
	if err != nil {
		return nil, &SignatureError{KeyID: keyID, Reason: "signing failed: " + err.Error()}
	}
	return &SignedManifest{Payload: msg[len(manifestDomain):], KeyID: keyID, Signature: sig}, nil
}

// Verifier checks a signed manifest and returns the manifest it carries.
type Verifier interface {
	Verify(sm *SignedManifest) (*Manifest, error)
}

// Keyring is a Verifier over a fixed set of trusted public keys by key id.
type Keyring map[string]crypto.PublicKey

// Verify checks the signature against the key sm names, then decodes the
// payload strictly and validates it.
func (k Keyring) Verify(sm *SignedManifest) (*Manifest, error) {
	if sm == nil {
		return nil, &SignatureError{Reason: "no signed manifest"}
	}
	pub, ok := k[sm.KeyID]
	if !ok {
		return nil, &SignatureError{KeyID: sm.KeyID, Reason: "unknown key"}
	}
	msg := append([]byte(manifestDomain), sm.Payload...)
	if !verifySignature(pub, msg, sm.Signature) {
		return nil, &SignatureError{KeyID: sm.KeyID, Reason: "signature mismatch"}
	}
	var m Manifest
	dec := json.NewDecoder(bytes.NewReader(sm.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&m); err != nil {
		return nil, &ManifestError{Field: "manifest", Reason: "does not decode: " + err.Error()}
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func digestFor(pub crypto.PublicKey, msg []byte) ([]byte, crypto.SignerOpts, error) {
	switch p := pub.(type) {
	case ed25519.PublicKey:
		return msg, crypto.Hash(0), nil
	case *ecdsa.PublicKey:
		switch p.Curve {
		case elliptic.P256():
			d := sha256.Sum256(msg)
			return d[:], crypto.SHA256, nil
		case elliptic.P384():
			d := sha512.Sum384(msg)
			return d[:], crypto.SHA384, nil
		}
	}
	return nil, nil, fmt.Errorf("unsupported key type %T: Ed25519, ECDSA P-256 or P-384", pub)
}

func verifySignature(pub crypto.PublicKey, msg, sig []byte) bool {
	data, _, err := digestFor(pub, msg)
	if err != nil {
		return false
	}
	switch p := pub.(type) {
	case ed25519.PublicKey:
		return ed25519.Verify(p, data, sig)
	case *ecdsa.PublicKey:
		return ecdsa.VerifyASN1(p, data, sig)
	}
	return false
}
