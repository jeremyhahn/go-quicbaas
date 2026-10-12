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

package knownhosts_test

import (
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/xkms/ssh/knownhosts"
)

// Compile-time proof InvalidFingerprintError is the go-xkms type, assignable
// both ways, so callers can errors.As against either name.
var (
	_ *xkmssdk.SSHInvalidFingerprintError = (*knownhosts.InvalidFingerprintError)(nil)
	_ *knownhosts.InvalidFingerprintError = (*xkmssdk.SSHInvalidFingerprintError)(nil)
)

// TestValidateFingerprint_AcceptsOpenSSHForm proves a fingerprint in the form
// `ssh-keygen -lf` prints passes through the re-export.
func TestValidateFingerprint_AcceptsOpenSSHForm(t *testing.T) {
	digest := sha256.Sum256([]byte("host key"))
	fp := "SHA256:" + base64.RawStdEncoding.EncodeToString(digest[:])

	if err := knownhosts.ValidateFingerprint(fp); err != nil {
		t.Fatalf("ValidateFingerprint(%q) = %v, want nil", fp, err)
	}
}

// TestValidateFingerprint_RejectsMalformed proves malformed fingerprints are
// refused with the re-exported *InvalidFingerprintError.
func TestValidateFingerprint_RejectsMalformed(t *testing.T) {
	digest := sha256.Sum256([]byte("host key"))
	body := base64.RawStdEncoding.EncodeToString(digest[:])

	for _, tc := range []struct {
		name string
		fp   string
	}{
		{"empty", ""},
		{"missing prefix", body},
		{"MD5 form", "MD5:" + body},
		{"short digest", "SHA256:" + body[:20]},
		{"not base64", "SHA256:" + "!!" + body[2:]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := knownhosts.ValidateFingerprint(tc.fp)
			var invalid *knownhosts.InvalidFingerprintError
			if !errors.As(err, &invalid) {
				t.Fatalf("ValidateFingerprint(%q) = %v, want *InvalidFingerprintError", tc.fp, err)
			}
			if invalid.Value != tc.fp {
				t.Fatalf("Value = %q, want %q", invalid.Value, tc.fp)
			}
		})
	}
}
