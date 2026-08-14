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

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// parseCertLoginPEM (the certificate decoder behind bindAdminPIVCert)
// distinguishes empty input, a missing PEM block, and an unparseable
// certificate body, returning the matching typed error for each; on the happy
// path qrdbsdk.ComputeSPKIThumbprint yields the canonical SPKI thumbprint.
func TestParseCertLoginPEM_ErrorBranches(t *testing.T) {
	t.Run("empty input", func(t *testing.T) {
		if _, err := parseCertLoginPEM(nil); !errors.Is(err, ErrCertPEMEmpty) {
			t.Fatalf("want ErrCertPEMEmpty, got %v", err)
		}
	})

	t.Run("no PEM block", func(t *testing.T) {
		if _, err := parseCertLoginPEM([]byte("not a pem block")); !errors.Is(err, ErrCertPEMNoBlock) {
			t.Fatalf("want ErrCertPEMNoBlock, got %v", err)
		}
	})

	t.Run("valid PEM block with unparseable body", func(t *testing.T) {
		// A well-formed CERTIFICATE PEM whose DER body is junk so
		// x509.ParseCertificate fails, exercising CertPEMParseError.
		bad := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not der")})
		_, err := parseCertLoginPEM(bad)
		var typed *CertPEMParseError
		if !errors.As(err, &typed) {
			t.Fatalf("want *CertPEMParseError, got %T (%v)", err, err)
		}
		if typed.Error() == "" {
			t.Fatal("CertPEMParseError.Error() must be non-empty")
		}
	})

	t.Run("valid certificate yields stable digest", func(t *testing.T) {
		certPEM, _ := newTestSOCertWithSPKI(t, "alice")
		cert, err := parseCertLoginPEM(certPEM)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got := qrdbsdk.ComputeSPKIThumbprint(cert)
		again, err := parseCertLoginPEM(certPEM)
		if err != nil {
			t.Fatalf("unexpected error on second call: %v", err)
		}
		if got == "" || got != qrdbsdk.ComputeSPKIThumbprint(again) {
			t.Fatalf("thumbprint unstable: %q vs %q", got, qrdbsdk.ComputeSPKIThumbprint(again))
		}
	})
}
