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

// Package xkmsca builds a REAL go-xkms Certificate Authority for tests, rather
// than a mock of one.
//
// That distinction has already cost this project twice. go-xkms
// pkg/acme/challenges/enroll-01/ca_compliance.go records a shipped bug where a
// runtime type assertion against a locally-declared CA interface silently
// stopped matching after an upstream signature drifted, making TPM IDevID
// enrollment impossible on every deployment — while the package's own tests kept
// passing, because they were written against a mock of the unsatisfiable
// declaration. go-quicbaas#910 found the same shape: XKMSFleetNodeCertIssuer asked
// TrustedCA.IssueCertificate for an exportable private key, which no go-xkms
// backend has ever returned, so the issuer failed on every real invocation while
// its tests passed against a stub that returned one.
//
// A mock cannot detect either failure. A real CA can, and this one costs nothing
// to build: in-memory key storage, in-memory certificate storage, no filesystem
// access and no goroutine that outlives the test.
//
// It is a leaf package on purpose: pkg/testutil imports pkg/service, so a CA
// builder living there could not be used from pkg/service's own tests.
package xkmsca

import (
	"context"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// New builds and initializes a genuine go-xkms TrustedCA over
// in-memory PKCS#8 key storage and an in-memory certificate store, with an
// ECDSA P-256 root valid for five years.
//
// commonName names the root. Give each test its own so a failure message says
// which CA it came from.
//
// The construction lives in builder.go, so the CA a unit test gets here is the
// same CA the integration cert fixtures are issued from — one construction,
// not two that can drift.
func New(t *testing.T, commonName string) xkmssdk.TrustedCA {
	t.Helper()

	ca, err := Open(context.Background(), Config{CommonName: commonName})
	if err != nil {
		t.Fatalf("xkmsca: open CA %q: %v", commonName, err)
	}
	return ca
}

// NewUninitialized builds the same CA as New but does NOT
// call Init, so it refuses every issuance with ErrNotInitialized.
//
// It stands for the honest "the CA is not up" condition in a failure test. The
// alternative — a stub whose method returns an invented error — is the pattern
// that let go-quicbaas#910 hide: a fake CA agrees to whatever the test needs,
// including things no real CA does.
func NewUninitialized(t *testing.T, commonName string) xkmssdk.TrustedCA {
	t.Helper()

	ca, err := Build(t.Context(), Config{CommonName: commonName})
	if err != nil {
		t.Fatalf("xkmsca: build CA %q: %v", commonName, err)
	}
	return ca
}

// noBundleCA is a real CA whose CABundle call fails. Everything else is the CA
// itself; only the one method is faulted.
type noBundleCA struct {
	xkmssdk.TrustedCA
	err error
}

// CABundle reports the configured failure instead of the chain.
func (c *noBundleCA) CABundle() ([]byte, error) { return nil, c.err }

// WithoutCABundle returns ca with its CABundle call failing, for callers that
// must fall back to the CA's own certificate as the trust anchor.
//
// Faulting one method of a real CA rather than substituting a fake one keeps the
// rest of the behaviour — the signing, the profiles, the certificate store —
// exactly as deployed, so the fallback is tested against real material.
func WithoutCABundle(ca xkmssdk.TrustedCA, err error) xkmssdk.TrustedCA {
	return &noBundleCA{TrustedCA: ca, err: err}
}
