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
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// fakePlatformAuth is a hardware-free stand-in for the TPM PlatformSRK auth
// provider (xkmssdk.TPMPlatformAuthProvider). It lets the PIN selection logic be
// exercised without a live TPM. provisioned drives the pintpm2 backend's
// "user PIN already set" detection (it queries IsProvisioned at construction).
type fakePlatformAuth struct{ provisioned bool }

func (f fakePlatformAuth) VerifyAuth(string) error         { return nil }
func (f fakePlatformAuth) ChangeAuth(string, string) error { return nil }
func (f fakePlatformAuth) GetLockoutInfo() (int, int, int, int, error) {
	return 0, 32, 0, 0, nil
}
func (f fakePlatformAuth) DictionaryAttackLockoutReset([]byte) error { return nil }
func (f fakePlatformAuth) IsProvisioned() bool                       { return f.provisioned }

// fakeTPMProvider is a hardware-free xkmssdk.PINTPMProvider. available=false
// models a TPM seal backend present but with no initialized PlatformSRK (the
// documented gap), which must make pinselect fall through to software.
type fakeTPMProvider struct {
	available   bool
	provisioned bool
	called      *bool
}

func (p fakeTPMProvider) PlatformAuth() (xkmssdk.TPMPlatformAuthProvider, bool) {
	if p.called != nil {
		*p.called = true
	}
	if !p.available {
		return nil, false
	}
	return fakePlatformAuth{provisioned: p.provisioned}, true
}

// TestResolvePINBackendType_Precedence covers the PIN backend resolution
// precedence and the key policy that PIN NEVER auto-selects hardware: only an
// explicit tpm2 (named on the PIN subsystem or inherited from an explicit
// crypto.backend) opts in; "", auto, and an "auto" crypto default all resolve to
// software.
func TestResolvePINBackendType_Precedence(t *testing.T) {
	mk := func(pin, cryptoDefault config.BackendType) *config.Config {
		c := &config.Config{}
		c.Crypto.Subsystems.PIN.Backend = pin
		c.Crypto.Backend = cryptoDefault
		return c
	}
	cases := []struct {
		name string
		cfg  *config.Config
		want config.BackendType
	}{
		{"nil config", nil, config.BackendTypeSoftware},
		{"all empty -> software", mk("", ""), config.BackendTypeSoftware},
		{"pin auto -> software", mk(config.BackendAuto, ""), config.BackendTypeSoftware},
		{"crypto auto, pin empty -> software (never auto hardware)", mk("", config.BackendAuto), config.BackendTypeSoftware},
		{"explicit pin software", mk(config.BackendTypeSoftware, ""), config.BackendTypeSoftware},
		{"explicit pin tpm2", mk(config.BackendTypeTPM2, ""), config.BackendTypeTPM2},
		{"inherit tpm2 from crypto.backend", mk("", config.BackendTypeTPM2), config.BackendTypeTPM2},
		{"pin software overrides crypto tpm2", mk(config.BackendTypeSoftware, config.BackendTypeTPM2), config.BackendTypeSoftware},
		{"pin tpm2 overrides crypto software", mk(config.BackendTypeTPM2, config.BackendTypeSoftware), config.BackendTypeTPM2},
		{"unsupported passes through (pkcs11)", mk(config.BackendTypePKCS11, ""), config.BackendTypePKCS11},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolvePINBackendType(tc.cfg); got != tc.want {
				t.Fatalf("resolvePINBackendType = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestBuildPINBackend_SoftwareDefault verifies the default/auto config yields the
// software backend, with no TPM provider involved — byte-for-byte the historical
// behavior.
func TestBuildPINBackend_SoftwareDefault(t *testing.T) {
	for _, name := range []string{"empty", "auto"} {
		t.Run(name, func(t *testing.T) {
			cfg := &config.Config{}
			if name == "auto" {
				cfg.Crypto.Backend = config.BackendAuto
			}
			cfg.Storage.DataDir = t.TempDir()

			be, err := buildPINBackend(cfg, nil, nil)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if be == nil {
				t.Fatal("nil backend")
			}
			if got := string(be.Strategy()); got != string(qrdbsdk.PINStrategySoftware) {
				t.Fatalf("strategy = %q, want software", got)
			}
		})
	}
}

// TestBuildPINBackend_SoftwareIgnoresProvider verifies that an explicit
// software PIN opts OUT of hardware even when a usable TPM provider is supplied.
func TestBuildPINBackend_SoftwareIgnoresProvider(t *testing.T) {
	called := false
	cfg := &config.Config{}
	cfg.Crypto.Subsystems.PIN.Backend = config.BackendTypeSoftware
	cfg.Storage.DataDir = t.TempDir()

	be, err := buildPINBackend(cfg, nil, fakeTPMProvider{available: true, provisioned: true, called: &called})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := string(be.Strategy()); got != string(qrdbsdk.PINStrategySoftware) {
		t.Fatalf("strategy = %q, want software", got)
	}
	if called {
		t.Fatal("software PIN must not consult the TPM provider (opt-out violated)")
	}
}

// TestBuildPINBackend_TPM2SelectsTPMProvider verifies that an explicit tpm2 PIN
// with a usable shared-TPM provider selects the TPM2 backend (in-hardware
// verification) — proving the provider is wired into the pinselect decision.
func TestBuildPINBackend_TPM2SelectsTPMProvider(t *testing.T) {
	called := false
	cfg := &config.Config{}
	cfg.Crypto.Subsystems.PIN.Backend = config.BackendTypeTPM2
	cfg.Seal.Strategy = string(xkmssdk.StrategyTPM2)
	cfg.Storage.DataDir = t.TempDir()

	be, err := buildPINBackend(cfg, nil, fakeTPMProvider{available: true, provisioned: true, called: &called})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !called {
		t.Fatal("tpm2 PIN did not consult the shared-TPM provider")
	}
	if got := string(be.Strategy()); got != string(qrdbsdk.PINStrategyTPM2) {
		t.Fatalf("strategy = %q, want tpm2", got)
	}
}

// TestBuildPINBackend_TPM2ProviderUnavailableFallsBackToSoftware covers the
// documented gap: a TPM2 seal is active but no PlatformSRK is initialized, so the
// provider reports unavailable. The PIN backend must degrade cleanly to the
// persistent software backend rather than fail boot.
func TestBuildPINBackend_TPM2ProviderUnavailableFallsBackToSoftware(t *testing.T) {
	cfg := &config.Config{}
	cfg.Crypto.Subsystems.PIN.Backend = config.BackendTypeTPM2
	cfg.Seal.Strategy = string(xkmssdk.StrategyTPM2)
	cfg.Storage.DataDir = t.TempDir()

	be, err := buildPINBackend(cfg, nil, fakeTPMProvider{available: false})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := string(be.Strategy()); got != string(qrdbsdk.PINStrategySoftware) {
		t.Fatalf("strategy = %q, want software (fallback)", got)
	}
}

// TestBuildPINBackend_TPM2WithoutSealReturnsTypedError verifies the fail-closed
// path: PIN=tpm2 but no TPM2 seal strategy is active (nil provider) yields a
// typed PINRequiresTPMSealError and never a silent software downgrade.
func TestBuildPINBackend_TPM2WithoutSealReturnsTypedError(t *testing.T) {
	cfg := &config.Config{}
	cfg.Crypto.Subsystems.PIN.Backend = config.BackendTypeTPM2
	cfg.Seal.Strategy = string(qrdbsdk.StrategyPassphrase)
	cfg.Storage.DataDir = t.TempDir()

	be, err := buildPINBackend(cfg, nil, nil)
	if err == nil {
		t.Fatal("expected PINRequiresTPMSealError, got nil")
	}
	if be != nil {
		t.Fatalf("expected nil backend on error, got %v", be)
	}
	var typed *PINRequiresTPMSealError
	if !errors.As(err, &typed) {
		t.Fatalf("expected *PINRequiresTPMSealError, got %T: %v", err, err)
	}
	if typed.SealStrategy != string(qrdbsdk.StrategyPassphrase) {
		t.Fatalf("SealStrategy = %q, want passphrase", typed.SealStrategy)
	}
}

// TestBuildPINBackend_UnsupportedBackend verifies a resolved PIN type the
// subsystem does not support (e.g. pkcs11) yields a typed
// PINBackendUnsupportedError.
func TestBuildPINBackend_UnsupportedBackend(t *testing.T) {
	cfg := &config.Config{}
	cfg.Crypto.Subsystems.PIN.Backend = config.BackendTypePKCS11
	cfg.Storage.DataDir = t.TempDir()

	be, err := buildPINBackend(cfg, nil, nil)
	if err == nil {
		t.Fatal("expected PINBackendUnsupportedError, got nil")
	}
	if be != nil {
		t.Fatalf("expected nil backend on error, got %v", be)
	}
	var typed *PINBackendUnsupportedError
	if !errors.As(err, &typed) {
		t.Fatalf("expected *PINBackendUnsupportedError, got %T: %v", err, err)
	}
	if typed.Backend != config.BackendTypePKCS11 {
		t.Fatalf("Backend = %q, want pkcs11", typed.Backend)
	}
}

// TestNewSealSharedTPMPINProvider_NilInputs verifies the provider constructor
// returns nil (no TPM PIN) when there is no shared backend or no config.
func TestNewSealSharedTPMPINProvider_NilInputs(t *testing.T) {
	if p := newSealSharedTPMPINProvider(nil, &config.Config{}, nil); p != nil {
		t.Fatal("nil shared backend must yield a nil provider")
	}
	if p := newSealSharedTPMPINProvider(nil, nil, nil); p != nil {
		t.Fatal("nil config must yield a nil provider")
	}
}

// TestPINErrorTypes covers the typed error messages and Unwrap behavior.
func TestPINErrorTypes(t *testing.T) {
	cause := errors.New("boom")
	be := &PINBackendError{Stage: "select pin backend", Cause: cause}
	if !errors.Is(be, cause) {
		t.Fatal("PINBackendError does not unwrap to its cause")
	}
	if be.Error() == "" || (&PINBackendError{Stage: "x"}).Error() == "" {
		t.Fatal("empty PINBackendError message")
	}

	req := &PINRequiresTPMSealError{SealStrategy: "passphrase"}
	if req.Error() == "" {
		t.Fatal("empty PINRequiresTPMSealError message")
	}
	if (&PINRequiresTPMSealError{}).Error() == "" {
		t.Fatal("empty PINRequiresTPMSealError message for empty strategy")
	}

	uns := &PINBackendUnsupportedError{Backend: config.BackendTypePKCS11}
	if uns.Error() == "" {
		t.Fatal("empty PINBackendUnsupportedError message")
	}
}
