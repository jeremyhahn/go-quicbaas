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

package config

import (
	"errors"
	"strings"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/fips"
)

// activateFIPS makes the config gate behave as it does on a FIPS-activated
// process and restores the real predicate afterwards, so one build can reach
// both sides of the check.
func activateFIPS(t *testing.T, activated bool) {
	t.Helper()
	previous := fipsActivated
	fipsActivated = func() bool { return activated }
	t.Cleanup(func() { fipsActivated = previous })
}

func TestValidateFIPSDEKAlgorithm(t *testing.T) {
	tests := []struct {
		name      string
		activated bool
		algorithm string
		wantErr   bool
	}{
		{name: "not activated permits chacha20", algorithm: "CHACHA20-POLY1305"},
		{name: "not activated permits xchacha20", algorithm: "XCHACHA20-POLY1305"},
		{name: "activated permits aes-256-gcm", activated: true, algorithm: "AES-256-GCM"},
		{name: "activated permits aes-128-gcm", activated: true, algorithm: "AES-128-GCM"},
		{name: "activated permits an unset algorithm", activated: true, algorithm: ""},
		{name: "activated permits whitespace as unset", activated: true, algorithm: "  "},
		{name: "activated refuses chacha20", activated: true, algorithm: "CHACHA20-POLY1305", wantErr: true},
		{name: "activated refuses xchacha20", activated: true, algorithm: "XCHACHA20-POLY1305", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activateFIPS(t, tt.activated)

			err := validateFIPSDEKAlgorithm(tt.algorithm)

			if !tt.wantErr {
				if err != nil {
					t.Fatalf("validateFIPSDEKAlgorithm(%q) = %v, want nil", tt.algorithm, err)
				}
				return
			}

			var validation *ValidationError
			if !errors.As(err, &validation) {
				t.Fatalf("err = %T, want *ValidationError", err)
			}
			if validation.Field != "kms.defaults.algorithm" {
				t.Fatalf("Field = %q, want %q", validation.Field, "kms.defaults.algorithm")
			}

			var notApproved *fips.AEADNotApprovedError
			if !errors.As(err, &notApproved) {
				t.Fatalf("err does not unwrap to *fips.AEADNotApprovedError: %v", err)
			}
			if notApproved.Algorithm != tt.algorithm {
				t.Fatalf("Algorithm = %q, want %q", notApproved.Algorithm, tt.algorithm)
			}
			if !strings.Contains(err.Error(), "AES-256-GCM") {
				t.Fatalf("Error() = %q, want it to name an approved algorithm", err.Error())
			}
		})
	}
}

func TestKMSConfigValidateRefusesAChaChaOnlyDeploymentUnderFIPS(t *testing.T) {
	activateFIPS(t, true)

	cfg := DefaultKMSConfig()
	cfg.Enabled = true
	cfg.Defaults.Algorithm = "CHACHA20-POLY1305"

	err := cfg.Validate()

	var notApproved *fips.AEADNotApprovedError
	if !errors.As(err, &notApproved) {
		t.Fatalf("Validate() = %v, want an error unwrapping to *fips.AEADNotApprovedError", err)
	}
}

func TestKMSConfigValidateKeepsChaChaOutsideFIPS(t *testing.T) {
	activateFIPS(t, false)

	cfg := DefaultKMSConfig()
	cfg.Enabled = true
	cfg.Defaults.Algorithm = "CHACHA20-POLY1305"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil: ChaCha20-Poly1305 stays an option outside FIPS", err)
	}
}

func TestKMSConfigValidateAcceptsTheDefaultsUnderFIPS(t *testing.T) {
	activateFIPS(t, true)

	cfg := DefaultKMSConfig()
	cfg.Enabled = true

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil: the shipped default is AES-256-GCM", err)
	}
}

// TestKMSConfigValidateIgnoresADisabledKMS keeps the FIPS check inside the
// existing contract: a disabled KMS section is not validated at all.
func TestKMSConfigValidateIgnoresADisabledKMS(t *testing.T) {
	activateFIPS(t, true)

	cfg := DefaultKMSConfig()
	cfg.Enabled = false
	cfg.Defaults.Algorithm = "CHACHA20-POLY1305"

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil for a disabled KMS", err)
	}
}

// TestValidateSurfacesTheTypedFIPSErrorThroughStartup pins the typed-error
// contract on the path the daemon actually runs: the top-level config.Validate
// aggregate must still unwrap to *fips.AEADNotApprovedError, not just carry
// its message.
func TestValidateSurfacesTheTypedFIPSErrorThroughStartup(t *testing.T) {
	activateFIPS(t, true)

	cfg := DefaultConfig()
	cfg.KMS.Enabled = true
	cfg.KMS.Defaults.Algorithm = "CHACHA20-POLY1305"

	err := Validate(cfg)

	var notApproved *fips.AEADNotApprovedError
	if !errors.As(err, &notApproved) {
		t.Fatalf("Validate() = %v, want an error unwrapping to *fips.AEADNotApprovedError", err)
	}
	if notApproved.Algorithm != "CHACHA20-POLY1305" {
		t.Fatalf("Algorithm = %q, want %q", notApproved.Algorithm, "CHACHA20-POLY1305")
	}

	// The aggregate may carry unrelated failures from the bare defaults; the
	// KMS entry itself must still hold its typed cause.
	var aggregate ValidationErrors
	if !errors.As(err, &aggregate) {
		t.Fatalf("Validate() = %T, want ValidationErrors", err)
	}
	for i := range aggregate {
		if aggregate[i].Field != "kms.defaults.algorithm" {
			continue
		}
		if !errors.As(&aggregate[i], &notApproved) {
			t.Fatalf("kms.defaults.algorithm entry lost its cause: %v", &aggregate[i])
		}
		return
	}
	t.Fatalf("Validate() = %v, want a kms.defaults.algorithm entry", err)
}
