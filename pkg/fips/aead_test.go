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

package fips

import (
	"errors"
	"strings"
	"testing"
)

func TestApprovedAEADAcceptsTheApprovedAESSuites(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
	}{
		{name: "aes-128-gcm", algorithm: "AES-128-GCM"},
		{name: "aes-192-gcm", algorithm: "AES-192-GCM"},
		{name: "aes-256-gcm", algorithm: "AES-256-GCM"},
		{name: "lower case", algorithm: "aes-256-gcm"},
		{name: "surrounding whitespace", algorithm: "  AES-256-GCM  "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !ApprovedAEAD(tt.algorithm) {
				t.Fatalf("ApprovedAEAD(%q) = false, want true", tt.algorithm)
			}
		})
	}
}

func TestApprovedAEADRefusesEverythingElse(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
	}{
		{name: "chacha20-poly1305 has no FIPS approval", algorithm: "CHACHA20-POLY1305"},
		{name: "xchacha20-poly1305 has no FIPS approval", algorithm: "XCHACHA20-POLY1305"},
		{name: "unset is not a decision", algorithm: ""},
		{name: "whitespace only", algorithm: "   "},
		{name: "unknown spelling fails closed", algorithm: "aes256"},
		{name: "unknown algorithm fails closed", algorithm: "AES-256-CBC"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if ApprovedAEAD(tt.algorithm) {
				t.Fatalf("ApprovedAEAD(%q) = true, want false", tt.algorithm)
			}
		})
	}
}

func TestApprovedAEADsIsStableAndSelfConsistent(t *testing.T) {
	approved := ApprovedAEADs()

	want := []string{"AES-128-GCM", "AES-192-GCM", "AES-256-GCM"}
	if len(approved) != len(want) {
		t.Fatalf("ApprovedAEADs() = %v, want %v", approved, want)
	}
	for i, algorithm := range approved {
		if algorithm != want[i] {
			t.Fatalf("ApprovedAEADs()[%d] = %q, want %q (the list is sorted so operator output is stable)",
				i, algorithm, want[i])
		}
		if !ApprovedAEAD(algorithm) {
			t.Fatalf("ApprovedAEADs() advertises %q but ApprovedAEAD refuses it", algorithm)
		}
	}
}

func TestAEADNotApprovedErrorNamesTheAlgorithmAndTheWayOut(t *testing.T) {
	err := NewAEADNotApprovedError("kms.CreateDEK", "CHACHA20-POLY1305")

	if err.Operation != "kms.CreateDEK" {
		t.Fatalf("Operation = %q, want %q", err.Operation, "kms.CreateDEK")
	}
	if err.Algorithm != "CHACHA20-POLY1305" {
		t.Fatalf("Algorithm = %q, want %q", err.Algorithm, "CHACHA20-POLY1305")
	}

	message := err.Error()
	for _, want := range []string{"kms.CreateDEK", "CHACHA20-POLY1305", "AES-256-GCM", "FIPS"} {
		if !strings.Contains(message, want) {
			t.Fatalf("Error() = %q, want it to mention %q", message, want)
		}
	}
}

func TestAEADNotApprovedErrorSaysTheModeIsTheConstraint(t *testing.T) {
	message := NewAEADNotApprovedError("kms.RotateDEK", "XCHACHA20-POLY1305").Error()

	// The operator must be able to tell "this deployment is FIPS-activated"
	// from "this algorithm does not exist"; the two have different fixes.
	if !strings.Contains(message, "FIPS 140-3") {
		t.Fatalf("Error() = %q, want it to name FIPS 140-3 as the constraint", message)
	}
	if !strings.Contains(message, "approved") {
		t.Fatalf("Error() = %q, want it to say the algorithm is not approved", message)
	}
}

// TestAEADNotApprovedErrorNamesAnUnsetAlgorithm covers the case a caller
// reaches by failing closed on an algorithm nobody selected: "" would
// otherwise render as an empty gap in the message.
func TestAEADNotApprovedErrorNamesAnUnsetAlgorithm(t *testing.T) {
	tests := []struct {
		name      string
		algorithm string
	}{
		{name: "empty", algorithm: ""},
		{name: "whitespace only", algorithm: "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := NewAEADNotApprovedError("kms.CreateDEK", tt.algorithm).Error()

			if !strings.Contains(message, "(unset)") {
				t.Fatalf("Error() = %q, want it to say the algorithm is unset", message)
			}
		})
	}
}

func TestRequireApprovedAEADFollowsActivation(t *testing.T) {
	tests := []struct {
		name      string
		activated bool
		algorithm string
		wantErr   bool
	}{
		{name: "not activated permits chacha20", activated: false, algorithm: "CHACHA20-POLY1305"},
		{name: "not activated permits an unknown name", activated: false, algorithm: "whatever"},
		{name: "activated permits aes-256-gcm", activated: true, algorithm: "AES-256-GCM"},
		{name: "activated permits aes-128-gcm", activated: true, algorithm: "AES-128-GCM"},
		{name: "activated refuses chacha20", activated: true, algorithm: "CHACHA20-POLY1305", wantErr: true},
		{name: "activated refuses xchacha20", activated: true, algorithm: "XCHACHA20-POLY1305", wantErr: true},
		{name: "activated fails closed on an unknown name", activated: true, algorithm: "AES-256-CBC", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := requireApprovedAEAD(tt.activated, "kms.CreateDEK", tt.algorithm)

			if tt.wantErr {
				var notApproved *AEADNotApprovedError
				if !errors.As(err, &notApproved) {
					t.Fatalf("requireApprovedAEAD(...) = %v, want *AEADNotApprovedError", err)
				}
				if notApproved.Algorithm != tt.algorithm {
					t.Fatalf("Algorithm = %q, want %q", notApproved.Algorithm, tt.algorithm)
				}
				return
			}
			if err != nil {
				t.Fatalf("requireApprovedAEAD(...) = %v, want nil", err)
			}
		})
	}
}

// TestRequireApprovedAEADMatchesThisBuild pins the exported entry point to the
// build it runs on: the fips140 job proves the refusing branch, the ordinary
// test run proves the permitting one.
func TestRequireApprovedAEADMatchesThisBuild(t *testing.T) {
	err := RequireApprovedAEAD("kms.CreateDEK", "CHACHA20-POLY1305")

	if Enabled() {
		if err == nil {
			t.Fatal("a FIPS-activated build must refuse CHACHA20-POLY1305")
		}
		return
	}
	if err != nil {
		t.Fatalf("a build that is not FIPS-activated must permit CHACHA20-POLY1305, got %v", err)
	}
}
