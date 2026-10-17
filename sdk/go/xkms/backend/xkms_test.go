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

package backend_test

import (
	"errors"
	"fmt"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/backend"
)

// TestKeyErrorSentinels_AreTheUpstreamValues proves each alias is the
// sentinel the go-xkms key backends return, not a lookalike. A lookalike
// compiles and reads the same, but a consumer's errors.Is against a real
// backend error would never match.
func TestKeyErrorSentinels_AreTheUpstreamValues(t *testing.T) {
	for _, tc := range []struct {
		name      string
		local, up error
	}{
		{"KeyNotFound", backend.ErrKeyNotFound, xkmssdk.ErrBackendKeyNotFound},
		{"KeyAlreadyExists", backend.ErrKeyAlreadyExists, xkmssdk.ErrBackendKeyAlreadyExists},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.local != tc.up {
				t.Fatalf("Err%s is not the upstream sentinel: %v vs %v", tc.name, tc.local, tc.up)
			}
			wrapped := fmt.Errorf("pkcs8 backend: %w", tc.up)
			if !errors.Is(wrapped, tc.local) {
				t.Fatalf("a wrapped upstream Err%s does not match the alias", tc.name)
			}
		})
	}
}

// TestKeyErrorSentinels_StayDistinct pins the separation a caller acts on:
// "the key is missing, generate it" versus "the key exists, load it". Both
// aliases pointing at one value would turn a generate-if-absent flow into a
// loop or an overwrite.
func TestKeyErrorSentinels_StayDistinct(t *testing.T) {
	if errors.Is(backend.ErrKeyNotFound, backend.ErrKeyAlreadyExists) ||
		errors.Is(backend.ErrKeyAlreadyExists, backend.ErrKeyNotFound) {
		t.Fatal("ErrKeyNotFound and ErrKeyAlreadyExists must stay distinct")
	}
}
