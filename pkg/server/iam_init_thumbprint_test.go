//go:build !integration

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

package server

import (
	"encoding/pem"
	"errors"
	"testing"
)

// soCertLoginThumbprint distinguishes empty input, a missing PEM block, and an
// unparseable certificate body, returning the matching typed error for each and
// the canonical hex(SHA-256(cert.Raw)) digest on the happy path.
func TestSOCertLoginThumbprint_ErrorBranches(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		if _, err := soCertLoginThumbprint(nil); !errors.Is(err, ErrSOCertLoginEmpty) {
			t.Fatalf("want ErrSOCertLoginEmpty, got %v", err)
		}
	})

	t.Run("no PEM block", func(t *testing.T) {
		if _, err := soCertLoginThumbprint([]byte("not a pem block")); !errors.Is(err, ErrSOCertLoginNoPEMBlock) {
			t.Fatalf("want ErrSOCertLoginNoPEMBlock, got %v", err)
		}
	})

	t.Run("valid PEM block with unparseable body", func(t *testing.T) {
		// A well-formed CERTIFICATE PEM whose DER body is junk so
		// x509.ParseCertificate fails, exercising SOCertLoginThumbprintError.
		bad := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not der")})
		_, err := soCertLoginThumbprint(bad)
		var typed *SOCertLoginThumbprintError
		if !errors.As(err, &typed) {
			t.Fatalf("want *SOCertLoginThumbprintError, got %T (%v)", err, err)
		}
		if typed.Error() == "" {
			t.Fatal("SOCertLoginThumbprintError.Error() must be non-empty")
		}
	})

	t.Run("valid certificate yields stable digest", func(t *testing.T) {
		certPEM, _ := newTestSOCertWithSPKI(t, "alice")
		got, err := soCertLoginThumbprint(certPEM)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		again, err := soCertLoginThumbprint(certPEM)
		if err != nil {
			t.Fatalf("unexpected error on second call: %v", err)
		}
		if got == "" || got != again {
			t.Fatalf("digest unstable: %q vs %q", got, again)
		}
	})
}
