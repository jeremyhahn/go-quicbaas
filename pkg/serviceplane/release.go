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
	"bytes"
	"crypto"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// releaseDomain separates a release signature from a manifest signature
// and every other signature the same key makes.
const releaseDomain = "quicbaas.serviceplane.release.v1\n"

// ReleaseKind says what a release upgrades.
type ReleaseKind string

const (
	// ReleaseService is a service redeploy.
	ReleaseService ReleaseKind = "service"
	// ReleaseNode is a staged node upgrade (go-trusted-platform).
	ReleaseNode ReleaseKind = "node"
)

// Release is the one object a rollout delivers, for service redeploys and
// node upgrades alike: the image, manifest, policy, reference values and
// provenance, signed together.
type Release struct {
	Kind ReleaseKind `json:"kind"`
	// Version names the release within its service; rollouts order nothing
	// by it, they move to the release they are given.
	Version string `json:"version"`
	// Manifest is the service manifest; its Image is the release's image.
	Manifest Manifest `json:"manifest"`
	// PolicyDigest pins the policy bundle the release runs under.
	PolicyDigest string `json:"policy_digest"`
	// ReferenceValues are the CoRIM reference value IDs appraisal checks the
	// release's measurements against.
	ReferenceValues []string `json:"reference_values,omitempty"`
	// Provenance is the reference to the release's SLSA provenance.
	Provenance string `json:"provenance"`
}

// ErrInvalidRelease marks a release that fails validation.
var ErrInvalidRelease = errors.New("serviceplane: invalid release")

// ReleaseError reports a release that is not well formed.
type ReleaseError struct {
	Field  string
	Reason string
}

// Error returns a human-readable description.
func (e *ReleaseError) Error() string {
	return "serviceplane: release " + e.Field + ": " + e.Reason
}

// Unwrap returns ErrInvalidRelease.
func (e *ReleaseError) Unwrap() error { return ErrInvalidRelease }

// Validate checks the release and its manifest.
func (r *Release) Validate() error {
	switch r.Kind {
	case ReleaseService, ReleaseNode:
	default:
		return &ReleaseError{Field: "kind", Reason: fmt.Sprintf("%q is neither service nor node", r.Kind)}
	}
	if strings.TrimSpace(r.Version) == "" {
		return &ReleaseError{Field: "version", Reason: "is required"}
	}
	if err := validateDigest(r.PolicyDigest); err != nil {
		return &ReleaseError{Field: "policy_digest", Reason: err.Error()}
	}
	if strings.TrimSpace(r.Provenance) == "" {
		return &ReleaseError{Field: "provenance", Reason: "is required"}
	}
	return r.Manifest.Validate()
}

// SignedRelease is a release as signed: the payload exactly as the signature
// covers it.
type SignedRelease struct {
	Payload   []byte `json:"payload"`
	KeyID     string `json:"key_id"`
	Signature []byte `json:"signature"`
}

// SignRelease signs r with signer under keyID.
func SignRelease(r *Release, keyID string, signer crypto.Signer) (*SignedRelease, error) {
	if signer == nil || strings.TrimSpace(keyID) == "" {
		return nil, fmt.Errorf("%w: a signer and a key id are required", ErrConfig)
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(r)
	if err != nil {
		return nil, err
	}
	data, opts, err := digestFor(signer.Public(), append([]byte(releaseDomain), payload...))
	if err != nil {
		return nil, &SignatureError{KeyID: keyID, Reason: err.Error()}
	}
	sig, err := signer.Sign(rand.Reader, data, opts)
	if err != nil {
		return nil, &SignatureError{KeyID: keyID, Reason: "signing failed: " + err.Error()}
	}
	return &SignedRelease{Payload: payload, KeyID: keyID, Signature: sig}, nil
}

// VerifyRelease checks a signed release against the keyring and returns
// the release it carries.
func (k Keyring) VerifyRelease(sr *SignedRelease) (*Release, error) {
	if sr == nil {
		return nil, &SignatureError{Reason: "no signed release"}
	}
	pub, ok := k[sr.KeyID]
	if !ok {
		return nil, &SignatureError{KeyID: sr.KeyID, Reason: "unknown key"}
	}
	if !verifySignature(pub, append([]byte(releaseDomain), sr.Payload...), sr.Signature) {
		return nil, &SignatureError{KeyID: sr.KeyID, Reason: "signature mismatch"}
	}
	var r Release
	dec := json.NewDecoder(bytes.NewReader(sr.Payload))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&r); err != nil {
		return nil, &ReleaseError{Field: "release", Reason: "does not decode: " + err.Error()}
	}
	if err := r.Validate(); err != nil {
		return nil, err
	}
	return &r, nil
}
