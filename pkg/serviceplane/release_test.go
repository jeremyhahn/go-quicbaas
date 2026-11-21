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

package serviceplane

import (
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
)

func validRelease() *Release {
	return &Release{
		Kind:            ReleaseService,
		Version:         "1.2.0",
		Manifest:        *validManifest(),
		PolicyDigest:    "sha256:" + strings.Repeat("cd", 32),
		ReferenceValues: []string{"corim:acme/web/1.2.0"},
		Provenance:      "slsa://registry.acme.internal/web/1.2.0",
	}
}

func releaseKey(t *testing.T) (ed25519.PrivateKey, Keyring) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return priv, Keyring{"release": pub}
}

func TestRelease_SignAndVerify(t *testing.T) {
	key, ring := releaseKey(t)
	sr, err := SignRelease(validRelease(), "release", key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ring.VerifyRelease(sr)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != "1.2.0" || got.Manifest.Name != "web" || got.Kind != ReleaseService {
		t.Fatalf("verified %+v", got)
	}
}

func TestRelease_RefusesTamperUnknownKeysAndManifestSignatures(t *testing.T) {
	key, ring := releaseKey(t)
	sr, err := SignRelease(validRelease(), "release", key)
	if err != nil {
		t.Fatal(err)
	}
	var sigErr *SignatureError

	tampered := *sr
	tampered.Payload = []byte(strings.Replace(string(sr.Payload), "1.2.0", "6.6.6", 1))
	if _, err := ring.VerifyRelease(&tampered); !errors.As(err, &sigErr) {
		t.Fatalf("tampered payload = %v", err)
	}
	unknown := *sr
	unknown.KeyID = "other"
	if _, err := ring.VerifyRelease(&unknown); !errors.As(err, &sigErr) {
		t.Fatalf("unknown key = %v", err)
	}
	if _, err := ring.VerifyRelease(nil); !errors.As(err, &sigErr) {
		t.Fatalf("nil release = %v", err)
	}

	// A manifest signature never verifies as a release: the domains differ.
	sm, err := Sign(validManifest(), "release", key)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ring.VerifyRelease(&SignedRelease{Payload: sm.Payload, KeyID: sm.KeyID, Signature: sm.Signature}); !errors.As(err, &sigErr) {
		t.Fatalf("manifest signature accepted as a release: %v", err)
	}
	if _, err := ring.Verify(&SignedManifest{Payload: sr.Payload, KeyID: sr.KeyID, Signature: sr.Signature}); err == nil {
		t.Fatal("release signature accepted as a manifest")
	}
}

func TestRelease_ValidateRefusesMalformedReleases(t *testing.T) {
	key, _ := releaseKey(t)
	for name, mutate := range map[string]func(*Release){
		"kind":          func(r *Release) { r.Kind = "firmware" },
		"version":       func(r *Release) { r.Version = " " },
		"policy_digest": func(r *Release) { r.PolicyDigest = "md5:00" },
		"provenance":    func(r *Release) { r.Provenance = "" },
	} {
		r := validRelease()
		mutate(r)
		var relErr *ReleaseError
		if _, err := SignRelease(r, "release", key); !errors.As(err, &relErr) || relErr.Field != name || !errors.Is(err, ErrInvalidRelease) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	r := validRelease()
	r.Manifest.Name = ""
	if _, err := SignRelease(r, "release", key); err == nil {
		t.Fatal("a release with an invalid manifest signed")
	}
	if _, err := SignRelease(validRelease(), "", key); !errors.Is(err, ErrConfig) {
		t.Fatalf("no key id = %v", err)
	}
	if _, err := SignRelease(validRelease(), "release", nil); !errors.Is(err, ErrConfig) {
		t.Fatalf("no signer = %v", err)
	}
}

func TestRelease_VerifyRefusesUnknownFields(t *testing.T) {
	key, ring := releaseKey(t)
	payload := []byte(`{"kind":"service","extra":true}`)
	data, opts, err := digestFor(key.Public(), append([]byte(releaseDomain), payload...))
	if err != nil {
		t.Fatal(err)
	}
	sig, err := key.Sign(rand.Reader, data, opts)
	if err != nil {
		t.Fatal(err)
	}
	var relErr *ReleaseError
	if _, err := ring.VerifyRelease(&SignedRelease{Payload: payload, KeyID: "release", Signature: sig}); !errors.As(err, &relErr) {
		t.Fatalf("unknown field = %v", err)
	}
}
