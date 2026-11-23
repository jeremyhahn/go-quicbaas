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
	"testing"
)

// TestKMSConfig_AVRBackendAccepted proves the trusted-firmware device backend
// is a backend QuicBaaS composes: kms.backend avr with a device validates.
func TestKMSConfig_AVRBackendAccepted(t *testing.T) {
	if !IsValidBackendType(BackendTypeAVR) {
		t.Fatalf("IsValidBackendType(%q) = false, want true", BackendTypeAVR)
	}
	if BackendTypeAVR != "avr" {
		t.Fatalf("BackendTypeAVR = %q, want avr", BackendTypeAVR)
	}
	c := KMSConfig{Enabled: true, Backend: BackendTypeAVR, AVR: AVRBackendConfig{Device: "/dev/ttyUSB0"}}
	if err := c.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil", err)
	}
}

// TestKMSConfig_AVRBackendNeedsDevice proves an avr backend without its
// serial port, or with a negative baud, fails validation with a typed error.
func TestKMSConfig_AVRBackendNeedsDevice(t *testing.T) {
	cases := map[string]struct {
		avr   AVRBackendConfig
		field string
	}{
		"no device":     {AVRBackendConfig{}, "kms.avr.device"},
		"negative baud": {AVRBackendConfig{Device: "/dev/ttyUSB0", Baud: -1}, "kms.avr.baud"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			c := KMSConfig{Enabled: true, Backend: BackendTypeAVR, AVR: tc.avr}
			err := c.Validate()
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("Validate() = %v, want *ValidationError", err)
			}
			if ve.Field != tc.field {
				t.Fatalf("ValidationError.Field = %q, want %q", ve.Field, tc.field)
			}
		})
	}
}

// TestCryptoConfig_AVRBackendAccepted proves crypto.backend avr validates.
func TestCryptoConfig_AVRBackendAccepted(t *testing.T) {
	c := CryptoConfig{Backend: BackendTypeAVR}
	if err := c.Validate(); err != nil {
		t.Fatalf("CryptoConfig{Backend:avr}.Validate() = %v, want nil", err)
	}
}
