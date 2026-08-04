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
	"errors"
	"os"
	"path/filepath"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// noHardwareConfig returns a config whose TPM device path cannot exist, so
// DetectHardware deterministically reports the TPM absent regardless of the
// test host. This keeps the foundation tests hardware-free.
func noHardwareConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Seal.TPM2.Device = "/nonexistent/quicbaas-test-tpm"
	return cfg
}

func TestBackendResolver_ResolveSoftware(t *testing.T) {
	r := NewBackendResolver(noHardwareConfig())

	bt, err := r.Resolve(BackendSpec{Backend: config.BackendTypeSoftware})
	if err != nil {
		t.Fatalf("Resolve(software) returned error: %v", err)
	}
	if bt != config.BackendTypeSoftware {
		t.Fatalf("Resolve(software) = %q, want software", bt)
	}
}

func TestBackendResolver_ResolveAutoNoHardwareIsSoftware(t *testing.T) {
	r := NewBackendResolver(noHardwareConfig())

	for _, spec := range []BackendSpec{
		{Backend: ""},
		{Backend: config.BackendAuto},
	} {
		bt, err := r.Resolve(spec)
		if err != nil {
			t.Fatalf("Resolve(%q) returned error: %v", spec.Backend, err)
		}
		if bt != config.BackendTypeSoftware {
			t.Fatalf("Resolve(%q) = %q, want software (no hardware present)", spec.Backend, bt)
		}
	}
}

func TestBackendResolver_ResolveUnknownBackend(t *testing.T) {
	r := NewBackendResolver(noHardwareConfig())

	_, err := r.Resolve(BackendSpec{Backend: config.BackendType("does-not-exist")})
	var unknown *UnknownBackendError
	if !errors.As(err, &unknown) {
		t.Fatalf("Resolve(unknown) error = %v, want *UnknownBackendError", err)
	}
	if unknown.Backend != config.BackendType("does-not-exist") {
		t.Fatalf("UnknownBackendError.Backend = %q, want does-not-exist", unknown.Backend)
	}
}

func TestBackendResolver_NotCompiledBackend(t *testing.T) {
	// In the default test binary the pkcs11/cloud tags are absent, so their
	// factories are unregistered: Resolve must return BackendNotCompiledError
	// (NOT a silent software fallback, NOT a panic).
	r := NewBackendResolver(noHardwareConfig())

	cases := []struct {
		backend config.BackendType
		tag     string
	}{
		{config.BackendTypePKCS11, "pkcs11"},
		{config.BackendTypeAWSKMS, "awskms"},
		{config.BackendTypeGCPKMS, "gcpkms"},
		{config.BackendTypeAzureKV, "azurekv"},
		{config.BackendTypeVault, "vault"},
	}
	for _, tc := range cases {
		_, err := r.Resolve(BackendSpec{Backend: tc.backend})
		var notCompiled *BackendNotCompiledError
		if !errors.As(err, &notCompiled) {
			t.Fatalf("Resolve(%q) error = %v, want *BackendNotCompiledError", tc.backend, err)
		}
		if notCompiled.Backend != tc.backend {
			t.Errorf("BackendNotCompiledError.Backend = %q, want %q", notCompiled.Backend, tc.backend)
		}
		if notCompiled.Tag != tc.tag {
			t.Errorf("BackendNotCompiledError.Tag = %q, want %q", notCompiled.Tag, tc.tag)
		}
	}
}

func TestBackendResolver_ServiceBackendSoftware(t *testing.T) {
	r := NewBackendResolver(noHardwareConfig())

	sb, err := r.ServiceBackend(BackendSpec{Backend: config.BackendTypeSoftware})
	if err != nil {
		t.Fatalf("ServiceBackend(software) returned error: %v", err)
	}
	if sb == nil {
		t.Fatal("ServiceBackend(software) returned nil backend")
	}
}

func TestBackendResolver_ServiceBackendNotCompiled(t *testing.T) {
	r := NewBackendResolver(noHardwareConfig())

	sb, err := r.ServiceBackend(BackendSpec{Backend: config.BackendTypePKCS11})
	if sb != nil {
		t.Fatal("ServiceBackend(pkcs11) returned a backend; want nil with not-compiled error")
	}
	var notCompiled *BackendNotCompiledError
	if !errors.As(err, &notCompiled) {
		t.Fatalf("ServiceBackend(pkcs11) error = %v, want *BackendNotCompiledError", err)
	}
}

func TestBackendResolver_StoreType(t *testing.T) {
	r := NewBackendResolver(noHardwareConfig())

	st, err := r.StoreType(BackendSpec{Backend: config.BackendTypeSoftware})
	if err != nil {
		t.Fatalf("StoreType(software) returned error: %v", err)
	}
	if st != xkmssdk.StoreSoftware {
		t.Fatalf("StoreType(software) = %q, want %q", st, xkmssdk.StoreSoftware)
	}

	// auto with no hardware resolves to software store.
	st, err = r.StoreType(BackendSpec{Backend: config.BackendAuto})
	if err != nil {
		t.Fatalf("StoreType(auto) returned error: %v", err)
	}
	if st != xkmssdk.StoreSoftware {
		t.Fatalf("StoreType(auto) = %q, want software", st)
	}
}

func TestBackendResolver_StoreTypeUnsupported(t *testing.T) {
	r := NewBackendResolver(noHardwareConfig())

	// quantum is a valid, always-compiled backend but has no CA key-store type.
	_, err := r.StoreType(BackendSpec{Backend: config.BackendTypeQuantum})
	var unsupported *StoreTypeUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("StoreType(quantum) error = %v, want *StoreTypeUnsupportedError", err)
	}
}

func TestBackendResolver_StoreTypeNotCompiled(t *testing.T) {
	r := NewBackendResolver(noHardwareConfig())

	// pkcs11 has a store type mapping but its backend is not compiled in the
	// default binary: the not-compiled check must win (security-correct).
	_, err := r.StoreType(BackendSpec{Backend: config.BackendTypePKCS11})
	var notCompiled *BackendNotCompiledError
	if !errors.As(err, &notCompiled) {
		t.Fatalf("StoreType(pkcs11) error = %v, want *BackendNotCompiledError", err)
	}
}

func TestBackendResolver_AEADProvider(t *testing.T) {
	r := NewBackendResolver(noHardwareConfig())

	// software resolves to nil (qrdb uses its software AES-256-GCM default).
	ap, err := r.AEADProvider(BackendSpec{Backend: config.BackendTypeSoftware})
	if err != nil {
		t.Fatalf("AEADProvider(software) returned error: %v", err)
	}
	if ap != nil {
		t.Fatalf("AEADProvider(software) = %v, want nil (qrdb default)", ap)
	}
}

func TestBackendResolver_AEADProviderUnsupported(t *testing.T) {
	r := NewBackendResolver(noHardwareConfig())

	// quantum is valid + compiled but not a supported AEAD backend.
	_, err := r.AEADProvider(BackendSpec{Backend: config.BackendTypeQuantum})
	var unsupported *AEADUnsupportedError
	if !errors.As(err, &unsupported) {
		t.Fatalf("AEADProvider(quantum) error = %v, want *AEADUnsupportedError", err)
	}
}

func TestDetectHardware_NoHardwarePicksSoftware(t *testing.T) {
	cfg := noHardwareConfig()
	probe := DetectHardware(cfg)

	if probe.TPM2Present {
		t.Fatalf("DetectHardware reported TPM present for device %q; want absent", probe.TPM2Device)
	}
	if probe.PKCS11Present {
		t.Fatalf("DetectHardware reported PKCS11 present (%q); want absent (none configured)", probe.PKCS11Library)
	}

	// With the probe reporting absent, auto must resolve to software.
	r := &BackendResolver{cfg: cfg, probe: probe}
	bt, err := r.Resolve(BackendSpec{Backend: config.BackendAuto})
	if err != nil {
		t.Fatalf("Resolve(auto) returned error: %v", err)
	}
	if bt != config.BackendTypeSoftware {
		t.Fatalf("Resolve(auto) = %q, want software", bt)
	}
}

func TestDetectHardware_AutoPrefersTPM2WhenPresent(t *testing.T) {
	// Drive the policy directly with a synthesised probe so the test needs no
	// real TPM: auto must prefer tpm2 when a device is reported present.
	cfg := noHardwareConfig()
	r := &BackendResolver{cfg: cfg, probe: HardwareProbe{TPM2Present: true, TPM2Device: "/dev/tpmrm0"}}

	bt, err := r.Resolve(BackendSpec{Backend: config.BackendAuto})
	if err != nil {
		t.Fatalf("Resolve(auto) returned error: %v", err)
	}
	if bt != config.BackendTypeTPM2 {
		t.Fatalf("Resolve(auto) = %q, want tpm2 (device present)", bt)
	}
}

func TestProbePKCS11_DetectsConfiguredLibrary(t *testing.T) {
	// probePKCS11 reports presence via os.Stat, never dlopen — so any path that
	// exists on disk stands in for a real PKCS#11 library. A temp file makes the
	// test deterministic and host-independent.
	cfg := &config.Config{}
	cfg.Seal.PKCS11.Library = "/nonexistent/quicbaas-test-lib.so"
	if present, _ := probePKCS11(cfg); present {
		t.Fatal("probePKCS11 reported present for a nonexistent library")
	}

	fakeLib := filepath.Join(t.TempDir(), "libfakehsm.so")
	if err := os.WriteFile(fakeLib, []byte("stand-in"), 0o600); err != nil {
		t.Fatalf("write fake library: %v", err)
	}
	cfg.Seal.PKCS11.Library = fakeLib
	present, lib := probePKCS11(cfg)
	if !present {
		t.Fatalf("probePKCS11 reported absent for existing library %q", fakeLib)
	}
	if lib != fakeLib {
		t.Fatalf("probePKCS11 library = %q, want %q", lib, fakeLib)
	}

	// Nothing configured anywhere = absent (no error path).
	if present, _ := probePKCS11(&config.Config{}); present {
		t.Fatal("probePKCS11 reported present with no library configured")
	}
}
