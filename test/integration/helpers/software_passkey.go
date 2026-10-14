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

//go:build integration

// Package helpers provides shared utilities for integration tests.
package helpers

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/descope/virtualwebauthn"

	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// SoftwarePasskey is a software WebAuthn authenticator holding one ES256
// (P-256) credential, so an integration test can complete real registration
// and login ceremonies against a running daemon.
//
// It answers credential-creation options with a "packed" SELF attestation: the
// credential key signs its own attestation statement and no x5c chain is
// sent. It answers credential-request options with an assertion from the same
// key. It proves possession of a key, not that the key lives in hardware, so a
// daemon enrolls it as an administrator credential only under
// iam.webauthn.assurance "insecure-allow-software-administrators".
type SoftwarePasskey struct {
	rp   virtualwebauthn.RelyingParty
	auth virtualwebauthn.Authenticator
	cred virtualwebauthn.Credential
}

// SoftwarePasskeyError reports a ceremony the software authenticator could
// not answer.
type SoftwarePasskeyError struct {
	// Op names the step that failed.
	Op string

	// Cause is the underlying failure.
	Cause error
}

func (e *SoftwarePasskeyError) Error() string {
	return "software passkey: " + e.Op + ": " + e.Cause.Error()
}

func (e *SoftwarePasskeyError) Unwrap() error { return e.Cause }

// NewSoftwarePasskey returns an authenticator with a fresh credential that
// signs client data for the relying party rpID as seen from origin. origin
// must be one of the daemon's iam.webauthn.rp_origins.
func NewSoftwarePasskey(rpID, origin string) (*SoftwarePasskey, error) {
	key, err := fullWidthP256Key()
	if err != nil {
		return nil, &SoftwarePasskeyError{Op: "generate credential key", Cause: err}
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, &SoftwarePasskeyError{Op: "encode credential key", Cause: err}
	}
	return &SoftwarePasskey{
		rp:   virtualwebauthn.RelyingParty{ID: rpID, Origin: origin},
		auth: virtualwebauthn.NewAuthenticator(),
		cred: virtualwebauthn.NewCredentialWithImportedKey(virtualwebauthn.KeyTypeEC2, pkcs8),
	}, nil
}

// CredentialID returns the credential ID, base64url-encoded without padding,
// the form the daemon uses as the credential's lookup key.
func (p *SoftwarePasskey) CredentialID() string {
	return base64.RawURLEncoding.EncodeToString(p.cred.ID)
}

// Register answers PublicKeyCredentialCreationOptions (bare, or wrapped in a
// "publicKey" member) with the attestation credential a browser would post
// back, decoded into the map the registration finish request carries.
func (p *SoftwarePasskey) Register(optionsJSON []byte) (map[string]interface{}, error) {
	options, err := virtualwebauthn.ParseAttestationOptions(string(optionsJSON))
	if err != nil {
		return nil, &SoftwarePasskeyError{Op: "parse creation options", Cause: err}
	}
	rp := p.rp
	rp.Name = options.RelyingPartyName
	p.auth.Options.UserHandle = []byte(options.UserID)

	raw := virtualwebauthn.CreateAttestationResponse(rp, p.auth, p.cred, *options)
	var credential map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &credential); err != nil {
		return nil, &SoftwarePasskeyError{Op: "decode attestation", Cause: err}
	}
	return credential, nil
}

// Assert answers PublicKeyCredentialRequestOptions with the assertion
// credential JSON a browser would post back.
func (p *SoftwarePasskey) Assert(optionsJSON string) (string, error) {
	options, err := virtualwebauthn.ParseAssertionOptions(optionsJSON)
	if err != nil {
		return "", &SoftwarePasskeyError{Op: "parse request options", Cause: err}
	}
	return virtualwebauthn.CreateAssertionResponse(p.rp, p.auth, p.cred, *options), nil
}

// fullWidthP256Key returns a P-256 key whose coordinates both encode to the
// full 32 bytes. The authenticator writes the COSE key from the coordinates'
// minimal big-endian form, and a relying party refuses a coordinate shorter
// than the curve size, so a key with a leading zero byte would make roughly
// one ceremony in a hundred fail for no reason the test is about.
func fullWidthP256Key() (*ecdsa.PrivateKey, error) {
	for {
		signer, _, err := dbxkms.GenerateRequesterKey(dbxkms.KeyAlgorithmECDSAP256)
		if err != nil {
			return nil, err
		}
		key, ok := signer.(*ecdsa.PrivateKey)
		if !ok {
			return nil, &UnexpectedCredentialKeyError{Key: signer}
		}
		if len(key.X.Bytes()) == 32 && len(key.Y.Bytes()) == 32 {
			return key, nil
		}
	}
}

// UnexpectedCredentialKeyError reports that the requester-key generator
// returned a signer that is not an ECDSA private key.
type UnexpectedCredentialKeyError struct {
	Key crypto.Signer
}

func (e *UnexpectedCredentialKeyError) Error() string {
	return fmt.Sprintf("software passkey: credential key is %T, want *ecdsa.PrivateKey", e.Key)
}
