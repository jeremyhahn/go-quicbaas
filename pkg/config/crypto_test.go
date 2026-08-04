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
	"path/filepath"
	"testing"
)

func TestCryptoConfig_ZeroValueIsSoftware(t *testing.T) {
	// The zero value must validate and represent "software everywhere", which is
	// byte-for-byte the historical behavior for configs with no crypto: block.
	var c CryptoConfig
	if err := c.Validate(); err != nil {
		t.Fatalf("zero CryptoConfig.Validate() = %v, want nil", err)
	}
	if !IsAutoBackend(c.Backend) {
		t.Fatalf("zero CryptoConfig.Backend = %q, want empty (auto -> software)", c.Backend)
	}
	if !IsAutoBackend(c.Subsystems.KMS.Backend) ||
		!IsAutoBackend(c.Subsystems.CA.Backend) ||
		!IsAutoBackend(c.Subsystems.PIN.Backend) ||
		!IsAutoBackend(c.Subsystems.BarrierAEAD.Backend) {
		t.Fatal("zero CryptoConfig subsystems should all inherit (empty backend)")
	}
}

func TestCryptoConfig_DefaultRoundTrip(t *testing.T) {
	d := DefaultCryptoConfig()
	if err := d.Validate(); err != nil {
		t.Fatalf("DefaultCryptoConfig().Validate() = %v, want nil", err)
	}
	if DefaultCryptoConfig() != (CryptoConfig{}) {
		t.Fatal("DefaultCryptoConfig() must equal the zero value (software everywhere)")
	}
}

func TestCryptoConfig_ValidBackends(t *testing.T) {
	cases := []BackendType{
		"", BackendAuto,
		BackendTypeSoftware, BackendTypeTPM2, BackendTypePKCS11,
		BackendTypeAWSKMS, BackendTypeGCPKMS, BackendTypeAzureKV,
		BackendTypeVault, BackendTypeQuantum,
	}
	for _, bt := range cases {
		c := CryptoConfig{Backend: bt}
		if err := c.Validate(); err != nil {
			t.Errorf("CryptoConfig{Backend:%q}.Validate() = %v, want nil", bt, err)
		}
	}
}

func TestCryptoConfig_UnknownBackendRejected(t *testing.T) {
	c := CryptoConfig{Backend: BackendType("nope")}
	err := c.Validate()
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("CryptoConfig.Validate() error = %v, want *ValidationError", err)
	}
	if ve.Field != "crypto.backend" {
		t.Fatalf("ValidationError.Field = %q, want crypto.backend", ve.Field)
	}
}

func TestCryptoConfig_UnknownSubsystemBackendRejected(t *testing.T) {
	cases := []struct {
		name  string
		cfg   CryptoConfig
		field string
	}{
		{"kms", CryptoConfig{Subsystems: CryptoSubsystemsConfig{KMS: BackendSpec{Backend: "x"}}}, "crypto.subsystems.kms.backend"},
		{"ca", CryptoConfig{Subsystems: CryptoSubsystemsConfig{CA: BackendSpec{Backend: "x"}}}, "crypto.subsystems.ca.backend"},
		{"pin", CryptoConfig{Subsystems: CryptoSubsystemsConfig{PIN: BackendSpec{Backend: "x"}}}, "crypto.subsystems.pin.backend"},
		{"barrier_aead", CryptoConfig{Subsystems: CryptoSubsystemsConfig{BarrierAEAD: BackendSpec{Backend: "x"}}}, "crypto.subsystems.barrier_aead.backend"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.cfg.Validate()
			var ve *ValidationError
			if !errors.As(err, &ve) {
				t.Fatalf("Validate() error = %v, want *ValidationError", err)
			}
			if ve.Field != tc.field {
				t.Fatalf("ValidationError.Field = %q, want %q", ve.Field, tc.field)
			}
		})
	}
}

func TestIsValidBackendType(t *testing.T) {
	if !IsValidBackendType(BackendTypeSoftware) {
		t.Error("software must be a valid backend type")
	}
	if IsValidBackendType(BackendAuto) {
		t.Error("auto must NOT be a valid backend type (it is an auto-detect sentinel)")
	}
	if IsValidBackendType(BackendType("")) {
		t.Error("empty must NOT be a valid backend type")
	}
	if IsValidBackendType(BackendType("bogus")) {
		t.Error("bogus must not be a valid backend type")
	}
}

func TestConfig_CryptoBackwardCompatible(t *testing.T) {
	// A config built with no crypto: block (zero CryptoConfig) must pass the
	// full validator, and Clone must carry the crypto policy intact.
	cfg := DefaultConfig()
	// Point storage at a writable temp dir so the unrelated filesystem checks in
	// the full validator pass; this test asserts crypto back-compat, not storage.
	dir := t.TempDir()
	cfg.Storage.DataDir = filepath.Join(dir, "data")
	cfg.Storage.WALDir = filepath.Join(dir, "wal")
	if err := Validate(cfg); err != nil {
		t.Fatalf("DefaultConfig with zero Crypto failed validation: %v", err)
	}

	cfg.Crypto = CryptoConfig{
		Backend: BackendTypeTPM2,
		Subsystems: CryptoSubsystemsConfig{
			KMS: BackendSpec{Backend: BackendTypeSoftware, KeyRef: "kms-key"},
		},
	}
	clone := cfg.Clone()
	if clone.Crypto.Backend != BackendTypeTPM2 {
		t.Fatalf("Clone Crypto.Backend = %q, want tpm2", clone.Crypto.Backend)
	}
	if clone.Crypto.Subsystems.KMS.KeyRef != "kms-key" {
		t.Fatalf("Clone Crypto.Subsystems.KMS.KeyRef = %q, want kms-key", clone.Crypto.Subsystems.KMS.KeyRef)
	}
}
