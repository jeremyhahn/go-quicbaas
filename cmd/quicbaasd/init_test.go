// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// =============================================================================
// Officer flag parsing
// =============================================================================

// =============================================================================
// File-handling error paths
// =============================================================================

// =============================================================================
// Trusted-CA loader
// =============================================================================

// =============================================================================
// Credential strategy validator
// =============================================================================

func TestValidateCredentialStrategy(t *testing.T) {
	for _, ok := range []string{"manual", "barrier"} {
		if err := validateCredentialStrategy(ok); err != nil {
			t.Errorf("validateCredentialStrategy(%q) unexpected error: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "shamir", "vault", "MANUAL"} {
		if err := validateCredentialStrategy(bad); err == nil {
			t.Errorf("validateCredentialStrategy(%q) expected error, got nil", bad)
		} else if !errors.Is(err, ErrInvalidCredentialStrategy) {
			t.Errorf("validateCredentialStrategy(%q): got %v, want errors.Is(ErrInvalidCredentialStrategy)", bad, err)
		}
	}
}

// =============================================================================
// Flag wiring on the canonical init command
// =============================================================================

// TestInit_FlagSet pins the init command's flag surface: the locked canonical
// set plus the deployment-local additions.
//
// It also pins the ABSENCE of officer flags. Under go-qrdb's Security Officer
// distribution model officers are registered against the RUNNING daemon
// through the sealed window, so accepting officer material here would either
// do nothing or resurrect the one-shot ceremony that wrote shares to disk.
func TestInit_FlagSet(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"init"})
	if err != nil {
		t.Fatalf("init command not registered: %v", err)
	}
	required := []string{
		// Locked canonical (also enforced by canonical_surface_test.go).
		"so-pin", "user-pin", "barrier-strategy",
		"shares", "threshold", "hostname", "sans",
		// Deployment-local additions.
		"credential-strategy", "init-data-dir",
	}
	for _, name := range required {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("init flag --%s missing", name)
		}
	}
	for _, name := range []string{"so", "trusted-ca"} {
		if cmd.Flags().Lookup(name) != nil {
			t.Errorf("init flag --%s must not exist: officers enrol against the running daemon", name)
		}
	}
}

// TestInit_RunInlineHappyPath drives a full passphrase-strategy run through a
// fresh cobra command: embedded server up, first-boot run, marker written.
func TestInit_RunInlineHappyPath(t *testing.T) {
	requireTestTrustDomain(t)
	tmp := t.TempDir()
	initDir := filepath.Join(tmp, "datadir")
	f := &inlineInitFlags{
		soPin:              "so-pin-1234",
		userPin:            "user-pin-1234",
		barrierStrategy:    "passphrase",
		secret:             "test-barrier-secret-12345",
		hostname:           "test.example",
		credentialStrategy: "barrier",
		dataDir:            initDir,
	}
	cmd := &cobra.Command{Use: "init"}
	if err := runInlineInit(cmd, f); err != nil {
		t.Fatalf("runInlineInit error: %v", err)
	}
}

// TestValidateInlineInitFlags_SoftwareRequiresSecret verifies the passphrase
// (software) barrier requires an admin passphrase at init: software without
// --secret is rejected with ErrSecretRequiredForSoftware (no deployment-derived
// default), while software WITH a secret — and "none", which needs none — pass.
func TestValidateInlineInitFlags_SoftwareRequiresSecret(t *testing.T) {
	requireTestTrustDomain(t)
	t.Run("software without secret is rejected", func(t *testing.T) {
		f := &inlineInitFlags{barrierStrategy: "passphrase", credentialStrategy: "barrier"}
		err := validateInlineInitFlags(f)
		if err == nil || !errors.Is(err, ErrSecretRequiredForSoftware) {
			t.Fatalf("expected ErrSecretRequiredForSoftware, got %v", err)
		}
	})
	t.Run("software with secret passes", func(t *testing.T) {
		f := &inlineInitFlags{barrierStrategy: "passphrase", secret: "pp", credentialStrategy: "barrier"}
		if err := validateInlineInitFlags(f); err != nil {
			t.Fatalf("software with secret must validate, got %v", err)
		}
	})
	t.Run("none needs no secret", func(t *testing.T) {
		f := &inlineInitFlags{barrierStrategy: "none", credentialStrategy: "barrier"}
		if err := validateInlineInitFlags(f); err != nil {
			t.Fatalf("none must validate without a secret, got %v", err)
		}
	})
}

// TestInit_RunInlineRejectsBadCredentialStrategy ensures the credential
// strategy validator runs before any state is persisted.
func TestInit_RunInlineRejectsBadCredentialStrategy(t *testing.T) {
	f := &inlineInitFlags{
		soPin:              "so",
		userPin:            "user",
		barrierStrategy:    "passphrase",
		credentialStrategy: "tpm2", // not in the canonical {manual, barrier}
	}
	cmd := &cobra.Command{Use: "init"}
	if err := runInlineInit(cmd, f); err == nil {
		t.Fatal("expected ErrInvalidCredentialStrategy")
	} else if !errors.Is(err, ErrInvalidCredentialStrategy) {
		t.Errorf("got %v, want errors.Is(ErrInvalidCredentialStrategy)", err)
	}
}

// =============================================================================
// Test fixtures (CSR / cert / pubkey / tampered CSR)
// =============================================================================

// writeTestCSRFile generates a P-256 CSR for the given common name and
// writes it as PEM to path.
func writeTestCSRFile(t *testing.T, path, commonName string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ec key: %v", err)
	}
	tmpl := &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: commonName},
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, priv)
	if err != nil {
		t.Fatalf("create CSR: %v", err)
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: der})
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write CSR: %v", err)
	}
}

// writeTamperedCSRFile generates a valid CSR, flips one byte of the
// signature, and writes the result. The PEM still parses but
// CheckSignature fails.
func writeTamperedCSRFile(t *testing.T, path, commonName string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ec key: %v", err)
	}
	tmpl := &x509.CertificateRequest{
		Subject: pkix.Name{CommonName: commonName},
	}
	der, err := x509.CreateCertificateRequest(rand.Reader, tmpl, priv)
	if err != nil {
		t.Fatalf("create CSR: %v", err)
	}
	parsed, err := x509.ParseCertificateRequest(der)
	if err != nil {
		t.Fatalf("parse CSR: %v", err)
	}
	// Re-encode with a corrupted signature byte.
	if len(parsed.Signature) == 0 {
		t.Fatalf("CSR signature unexpectedly empty")
	}
	tampered := make([]byte, len(der))
	copy(tampered, der)
	// Flip the last byte; that lands inside the BIT STRING signature
	// for an ECDSA CSR with a P-256 key.
	tampered[len(tampered)-1] ^= 0xFF
	out := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: tampered})
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write tampered CSR: %v", err)
	}
}

// writeTestCertFile issues a self-signed certificate with the given CN
// and writes it as PEM to path.
func writeTestCertFile(t *testing.T, path, commonName string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ec key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
}

// writeTestPubKeyFile writes a fresh P-256 public key as PEM to path.
func writeTestPubKeyFile(t *testing.T, path, _ string) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate ec key: %v", err)
	}
	der, err := x509.MarshalPKIXPublicKey(&priv.PublicKey)
	if err != nil {
		t.Fatalf("marshal pubkey: %v", err)
	}
	out := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der})
	if err := os.WriteFile(path, out, 0o600); err != nil {
		t.Fatalf("write pubkey: %v", err)
	}
}

// TestInit_AdminFactorFlags pins the login-factor surface. The flag names and
// vocabulary match xkmsd so every daemon in the stack accepts identical
// arguments — an operator should not have to learn three dialects.
func TestInit_AdminFactorFlags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"init"})
	if err != nil {
		t.Fatalf("init command not registered: %v", err)
	}
	for _, name := range []string{"admin-factor", "passkey-device"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("init flag --%s missing", name)
		}
	}

	// The PIN must never be a flag: it would be visible in /proc to every
	// process on the host. xkmsd reads it from stdin or the environment.
	if cmd.Flags().Lookup("passkey-pin") != nil {
		t.Error("--passkey-pin must not exist; read the CTAP2 PIN from stdin or the environment")
	}
}

// TestValidateInlineInitFlags_AdminFactor verifies the selection is validated
// through go-qrdb's canonical parser, in the validation seam, so a typo fails
// before any state exists rather than midway through first boot.
func TestValidateInlineInitFlags_AdminFactor(t *testing.T) {
	requireTestTrustDomain(t)

	t.Run("valid factors pass", func(t *testing.T) {
		f := &inlineInitFlags{
			barrierStrategy:    "none",
			credentialStrategy: "barrier",
			adminFactors:       []string{"mtls", "fido2"},
		}
		if err := validateInlineInitFlags(f); err != nil {
			t.Fatalf("valid --admin-factor rejected: %v", err)
		}
	})

	t.Run("unknown factor is rejected", func(t *testing.T) {
		f := &inlineInitFlags{
			barrierStrategy:    "none",
			credentialStrategy: "barrier",
			adminFactors:       []string{"smartwatch"},
		}
		err := validateInlineInitFlags(f)
		if err == nil {
			t.Fatal("expected InvalidAdminFactorError")
		}
		var typed *InvalidAdminFactorError
		if !errors.As(err, &typed) {
			t.Fatalf("got %v, want InvalidAdminFactorError", err)
		}
	})
}

// TestBuildInitConfig_PropagatesAdminFactors is the regression guard for the
// wiring: without this the operator's selection is collected and discarded, and
// an admin who asked for a passkey silently gets the mTLS baseline instead —
// discovered only at their first login attempt.
func TestBuildInitConfig_PropagatesAdminFactors(t *testing.T) {
	f := &inlineInitFlags{
		barrierStrategy: "none",
		adminFactors:    []string{"mtls", "fido2"},
		passkeyDevice:   "Yubico",
	}
	cfg := buildInitConfig(t.TempDir(), f, false)

	if len(cfg.Auth.AdminFactors) != 2 {
		t.Fatalf("cfg.Auth.AdminFactors = %v, want 2 entries", cfg.Auth.AdminFactors)
	}
	if cfg.Auth.PasskeyDevice != "Yubico" {
		t.Errorf("cfg.Auth.PasskeyDevice = %q, want Yubico", cfg.Auth.PasskeyDevice)
	}
}

// TestBuildInitConfig_HonoursTheOperatorStorageBackend is the regression guard
// for a silent split-brain: buildInitConfig starts from config.Default(), so a
// hard-coded backend here overrides storage.backend / QUICBAAS_STORAGE_BACKEND.
// Init would then write first-boot IAM state to one engine while the daemon
// later read from another — init appears to succeed and `quicbaasd start`
// comes up with no admin.
func TestBuildInitConfig_HonoursTheOperatorStorageBackend(t *testing.T) {
	origCfg := cfg
	t.Cleanup(func() { cfg = origCfg })

	for _, backend := range []string{
		config.StorageBackendMemory,
		config.StorageBackendFile,
		config.StorageBackendPebble,
	} {
		t.Run(backend, func(t *testing.T) {
			operator := config.Default()
			operator.Storage.Backend = backend
			cfg = operator

			got := buildInitConfig(t.TempDir(), &inlineInitFlags{barrierStrategy: "none"}, false)
			if got.Storage.Backend != backend {
				t.Errorf("cfg.Storage.Backend = %q, want %q (init must not override the operator's engine)",
					got.Storage.Backend, backend)
			}
		})
	}
}

// TestBuildInitConfig_DefaultsToPebble verifies a config that names no backend
// still gets the durable default rather than an empty string.
func TestBuildInitConfig_DefaultsToPebble(t *testing.T) {
	origCfg := cfg
	t.Cleanup(func() { cfg = origCfg })

	operator := config.Default()
	operator.Storage.Backend = ""
	cfg = operator

	got := buildInitConfig(t.TempDir(), &inlineInitFlags{barrierStrategy: "none"}, false)
	if got.Storage.Backend != config.StorageBackendPebble {
		t.Errorf("cfg.Storage.Backend = %q, want pebble", got.Storage.Backend)
	}
}
