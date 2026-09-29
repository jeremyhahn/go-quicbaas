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

package xkms_test

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms"
)

// Compile-time proof the go-xkms#798 aliases are the go-xkms types, and that
// the lifecycle verbs reach consumers through the TrustedPlatformModule alias.
var (
	_ xkmssdk.ProvisioningState     = xkms.ProvisioningState{}
	_ xkmssdk.CredentialChallenge   = xkms.CredentialChallenge{}
	_ *xkmssdk.ActivationKey        = (*xkms.ActivationKey)(nil)
	_ tpmLifecycle                  = xkms.TrustedPlatformModule(nil)
	_ xkmssdk.TrustedPlatformModule = xkms.TrustedPlatformModule(nil)
)

// tpmLifecycle is the go-xkms#798 method set go-trusted-platform calls.
type tpmLifecycle interface {
	State() (*xkms.ProvisioningState, error)
	ClearForReprovision() (rebootRequired bool, err error)
	CreateActivationKey() (*xkms.ActivationKey, error)
	AnswerCredentialChallenge(key *xkms.ActivationKey, challenge *xkms.CredentialChallenge) ([]byte, error)
}

// sha256Name is a well-formed TPM Name: the TPM_ALG_SHA256 nameAlg followed
// by a 32-byte digest. The challenge binds to it without interpreting it.
var sha256Name = append([]byte{0x00, 0x0b}, bytes.Repeat([]byte{0x5a}, 32)...)

// TestNewCredentialChallenge_BuildsForRSAAndECCEndorsementKeys proves the
// aliased builder is the real software MakeCredential: it produces a
// challenge and a non-empty secret for both EK key types with no TPM present.
func TestNewCredentialChallenge_BuildsForRSAAndECCEndorsementKeys(t *testing.T) {
	rsaKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA EK: %v", err)
	}
	eccKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECC EK: %v", err)
	}

	tests := []struct {
		name  string
		ekPub crypto.PublicKey
	}{
		{name: "RSA-2048", ekPub: &rsaKey.PublicKey},
		{name: "ECC-P256", ekPub: &eccKey.PublicKey},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			challenge, secret, err := xkms.NewCredentialChallenge(tt.ekPub, sha256Name)
			if err != nil {
				t.Fatalf("NewCredentialChallenge: %v", err)
			}
			if len(challenge.CredentialBlob) == 0 || len(challenge.EncryptedSecret) == 0 {
				t.Fatalf("challenge is incomplete: %+v", challenge)
			}
			if len(secret) == 0 {
				t.Fatal("challenge secret is empty")
			}
		})
	}
}

// TestNewCredentialChallenge_RefusesMissingInputs pins the typed refusals to
// the aliased sentinels, so a consumer's errors.Is keeps working.
func TestNewCredentialChallenge_RefusesMissingInputs(t *testing.T) {
	eccKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ECC EK: %v", err)
	}

	tests := []struct {
		name    string
		ekPub   crypto.PublicKey
		keyName []byte
		want    error
	}{
		{name: "nil EK", ekPub: nil, keyName: sha256Name, want: xkms.ErrTPMEKPublicRequired},
		{name: "empty name", ekPub: &eccKey.PublicKey, keyName: nil, want: xkms.ErrTPMActivationKeyNameRequired},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			challenge, secret, err := xkms.NewCredentialChallenge(tt.ekPub, tt.keyName)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}
			if challenge != nil || secret != nil {
				t.Fatal("a refused challenge must not return a challenge or secret")
			}
		})
	}
}

// TestActivationKeyName_RefusesAMalformedPublicArea proves the aliased Name
// function parses the wire bytes rather than hashing whatever it is given.
func TestActivationKeyName_RefusesAMalformedPublicArea(t *testing.T) {
	name, err := xkms.ActivationKeyName([]byte{0x00, 0x01})
	if !errors.Is(err, xkms.ErrTPMCredentialChallenge) {
		t.Fatalf("err = %v, want ErrTPMCredentialChallenge", err)
	}
	if name != nil {
		t.Fatalf("name = %x, want nil", name)
	}
}

// TestTPMLifecycleSentinels_AreTheUpstreamValues pins each sentinel to its
// go-xkms value: an alias that drifted to a lookalike would break errors.Is.
func TestTPMLifecycleSentinels_AreTheUpstreamValues(t *testing.T) {
	pairs := map[string][2]error{
		"ClearLockoutRefused":         {xkms.ErrTPMClearLockoutRefused, xkmssdk.ErrTPMClearLockoutRefused},
		"ClearPlatformRefused":        {xkms.ErrTPMClearPlatformRefused, xkmssdk.ErrTPMClearPlatformRefused},
		"ProvisioningStateRead":       {xkms.ErrTPMProvisioningStateRead, xkmssdk.ErrTPMProvisioningStateRead},
		"CredentialChallenge":         {xkms.ErrTPMCredentialChallenge, xkmssdk.ErrTPMCredentialChallenge},
		"EKPublicRequired":            {xkms.ErrTPMEKPublicRequired, xkmssdk.ErrTPMEKPublicRequired},
		"ActivationKeyNameRequired":   {xkms.ErrTPMActivationKeyNameRequired, xkmssdk.ErrTPMActivationKeyNameRequired},
		"ActivationKeyCreate":         {xkms.ErrTPMActivationKeyCreate, xkmssdk.ErrTPMActivationKeyCreate},
		"ActivationKeyRequired":       {xkms.ErrTPMActivationKeyRequired, xkmssdk.ErrTPMActivationKeyRequired},
		"CredentialChallengeRequired": {xkms.ErrTPMCredentialChallengeRequired, xkmssdk.ErrTPMCredentialChallengeRequired},
	}
	for name, pair := range pairs {
		if pair[0] == nil || pair[0] != pair[1] {
			t.Errorf("%s: alias %v is not the upstream sentinel %v", name, pair[0], pair[1])
		}
	}
}
