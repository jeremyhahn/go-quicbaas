//go:build iam

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

package iam

import (
	"context"
	"errors"
	"testing"
)

// Global returns the resolver's global key manager; Algorithm reports ES256.
func TestResolver_GlobalAndAlgorithm(t *testing.T) {
	setupTestXKMS(t)
	settings := &fakeOIDCSettings{dedicated: map[string]bool{}}
	r, global := newResolverWithGlobal(t, settings)

	if r.Global() != global {
		t.Fatal("Global() did not return the configured global manager")
	}
	if alg := global.Algorithm(); alg != SigningAlgorithmES256 {
		t.Fatalf("Algorithm() = %q, want ES256", alg)
	}
}

// A nil-snapshot manager reports the default algorithm without panicking.
func TestSigningKeyManager_AlgorithmDefault(t *testing.T) {
	var mgr SigningKeyManager // zero value: no snapshot loaded
	if alg := mgr.Algorithm(); alg != SigningAlgorithmES256 {
		t.Fatalf("zero-value Algorithm() = %q, want ES256", alg)
	}
}

// When settings resolution errors for a tenant with no cached dedicated manager,
// ManagerFor fails open to the global key (it cannot prove dedication).
func TestResolver_SettingsErrorFailsOpenToGlobal(t *testing.T) {
	setupTestXKMS(t)
	settings := &fakeOIDCSettings{err: errors.New("settings read failed")}
	r, global := newResolverWithGlobal(t, settings)

	mgr, err := r.ManagerFor(context.Background(), "unknown-tenant")
	if err != nil {
		t.Fatalf("ManagerFor: %v", err)
	}
	if mgr != global {
		t.Fatal("settings error for uncached tenant must fall back to the global manager")
	}
}
