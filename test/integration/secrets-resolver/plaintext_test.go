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

package secretsresolver

import (
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config/secrets"
)

// plaintextOf copies a resolved secret out of its scoped borrow for an
// assertion. go-qrdb v0.47 SecretValue exposes its plaintext only through
// WithBytes.
func plaintextOf(t *testing.T, v *secrets.SecretValue) string {
	t.Helper()
	var plaintext string
	err := v.WithBytes(t.Context(), func(b []byte) error {
		plaintext = string(b) //secure:unwiped test assertion only
		return nil
	})
	if err != nil {
		t.Fatalf("WithBytes: %v", err)
	}
	return plaintext
}
