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
)

// =============================================================================
// Officer flag parsing
// =============================================================================

// TestParseOfficerSpec_AllKinds covers parseOfficerSpec across the full
// officerKind dispatch table (csr, cert, pubkey).
func TestParseOfficerSpec_AllKinds(t *testing.T) {
	cases := []struct {
		name     string
		raw      string
		wantUser string
		wantKind officerKind
		wantPath string
	}{
		{"csr lowercase", "alice:csr:/tmp/a.csr", "alice", officerKindCSR, "/tmp/a.csr"},
		{"cert lowercase", "bob:cert:/tmp/b.crt", "bob", officerKindCert, "/tmp/b.crt"},
		{"pubkey lowercase", "carol:pubkey:/tmp/c.pub", "carol", officerKindPubKey, "/tmp/c.pub"},
		{"csr uppercase normalised", "alice:CSR:/tmp/a.csr", "alice", officerKindCSR, "/tmp/a.csr"},
		{"path with colon", "dave:cert:/tmp/d:weird.crt", "dave", officerKindCert, "/tmp/d:weird.crt"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			spec, err := parseOfficerSpec(c.raw)
			if err != nil {
				t.Fatalf("parseOfficerSpec(%q) error: %v", c.raw, err)
			}
			if spec.Username != c.wantUser {
				t.Errorf("username: got %q, want %q", spec.Username, c.wantUser)
			}
			if spec.Kind != c.wantKind {
				t.Errorf("kind: got %q, want %q", spec.Kind, c.wantKind)
			}
			if spec.Path != c.wantPath {
				t.Errorf("path: got %q, want %q", spec.Path, c.wantPath)
			}
		})
	}
}

// TestParseOfficerSpec_RejectsBadFormat exercises every typed-error path
// in parseOfficerSpec.
func TestParseOfficerSpec_RejectsBadFormat(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		wantErr error
	}{
		{"missing kind separator", "alice/path/to/file", ErrInvalidOfficerFormat},
		{"missing path", "alice:csr", ErrInvalidOfficerFormat},
		{"empty username", ":csr:/tmp/a.csr", ErrEmptyOfficerUsername},
		{"empty path", "alice:csr:", ErrEmptyOfficerPath},
		{"unknown kind", "alice:badkind:/tmp/a.csr", ErrUnknownOfficerKind},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := parseOfficerSpec(c.raw); err == nil {
				t.Fatalf("parseOfficerSpec(%q) expected error %v, got nil", c.raw, c.wantErr)
			} else if !errors.Is(err, c.wantErr) {
				t.Errorf("parseOfficerSpec(%q): got %v, want errors.Is(%v)", c.raw, err, c.wantErr)
			}
		})
	}
}

// TestOfficerLoaders_DispatchTable verifies every kind in the
// officerLoaders map is invoked exactly once and returns the expected
// material shape. This guards against silent regressions in the
// map-based dispatch table.
func TestOfficerLoaders_DispatchTable(t *testing.T) {
	tmp := t.TempDir()
	csrPath := filepath.Join(tmp, "alice.csr.pem")
	certPath := filepath.Join(tmp, "bob.cert.pem")
	pubPath := filepath.Join(tmp, "carol.pub.pem")

	writeTestCSRFile(t, csrPath, "alice")
	writeTestCertFile(t, certPath, "bob")
	writeTestPubKeyFile(t, pubPath, "carol")

	cases := []struct {
		name      string
		spec      officerSpec
		checkBody func(t *testing.T, m *officerMaterial)
	}{
		{
			name: "csr loader populates CSRPEM",
			spec: officerSpec{Username: "alice", Kind: officerKindCSR, Path: csrPath},
			checkBody: func(t *testing.T, m *officerMaterial) {
				if len(m.CSRPEM) == 0 {
					t.Errorf("csr loader: CSRPEM is empty")
				}
				if len(m.CertPEM) != 0 || len(m.PubKeyPEM) != 0 {
					t.Errorf("csr loader: only CSRPEM should be populated")
				}
			},
		},
		{
			name: "cert loader populates CertPEM",
			spec: officerSpec{Username: "bob", Kind: officerKindCert, Path: certPath},
			checkBody: func(t *testing.T, m *officerMaterial) {
				if len(m.CertPEM) == 0 {
					t.Errorf("cert loader: CertPEM is empty")
				}
				if len(m.CSRPEM) != 0 || len(m.PubKeyPEM) != 0 {
					t.Errorf("cert loader: only CertPEM should be populated")
				}
			},
		},
		{
			name: "pubkey loader populates PubKeyPEM",
			spec: officerSpec{Username: "carol", Kind: officerKindPubKey, Path: pubPath},
			checkBody: func(t *testing.T, m *officerMaterial) {
				if len(m.PubKeyPEM) == 0 {
					t.Errorf("pubkey loader: PubKeyPEM is empty")
				}
				if len(m.CSRPEM) != 0 || len(m.CertPEM) != 0 {
					t.Errorf("pubkey loader: only PubKeyPEM should be populated")
				}
			},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			loader, ok := officerLoaders[c.spec.Kind]
			if !ok {
				t.Fatalf("officerLoaders has no entry for %q", c.spec.Kind)
			}
			m, err := loader(c.spec)
			if err != nil {
				t.Fatalf("loader returned error: %v", err)
			}
			if m == nil {
				t.Fatalf("loader returned nil material")
			}
			if m.Username != c.spec.Username {
				t.Errorf("username: got %q, want %q", m.Username, c.spec.Username)
			}
			if m.Kind != c.spec.Kind {
				t.Errorf("kind: got %q, want %q", m.Kind, c.spec.Kind)
			}
			c.checkBody(t, m)
		})
	}
}

// TestLoadOfficer_RoutesViaDispatch verifies loadOfficer delegates to the
// correct table entry rather than an inlined switch.
func TestLoadOfficer_RoutesViaDispatch(t *testing.T) {
	tmp := t.TempDir()
	csrPath := filepath.Join(tmp, "alice.csr.pem")
	writeTestCSRFile(t, csrPath, "alice")

	m, err := loadOfficer(officerSpec{Username: "alice", Kind: officerKindCSR, Path: csrPath})
	if err != nil {
		t.Fatalf("loadOfficer error: %v", err)
	}
	if len(m.CSRPEM) == 0 {
		t.Error("loadOfficer should have populated CSRPEM via the csr loader")
	}
}

// TestLoadOfficer_UnknownKindReturnsTypedError ensures an officerSpec with
// an unregistered kind cannot reach a loader and instead surfaces
// ErrUnknownOfficerKind. parseOfficerSpec already rejects unknown kinds,
// but a programmatic caller could synthesise a bogus spec; this test
// guards that defence-in-depth path.
func TestLoadOfficer_UnknownKindReturnsTypedError(t *testing.T) {
	if _, err := loadOfficer(officerSpec{Username: "x", Kind: "phantom", Path: "/tmp/x"}); err == nil {
		t.Fatal("loadOfficer should reject unknown kind")
	} else if !errors.Is(err, ErrUnknownOfficerKind) {
		t.Errorf("got %v, want errors.Is(ErrUnknownOfficerKind)", err)
	}
}

// =============================================================================
// File-handling error paths
// =============================================================================

// TestReadOfficerFile_ErrorPaths exercises the typed errors emitted by
// readOfficerFile.
func TestReadOfficerFile_ErrorPaths(t *testing.T) {
	tmp := t.TempDir()

	t.Run("not found", func(t *testing.T) {
		_, err := readOfficerFile(filepath.Join(tmp, "missing.pem"))
		if err == nil {
			t.Fatal("expected ErrOfficerFileNotFound")
		}
		if !errors.Is(err, ErrOfficerFileNotFound) {
			t.Errorf("got %v, want errors.Is(ErrOfficerFileNotFound)", err)
		}
	})

	t.Run("directory rejected", func(t *testing.T) {
		_, err := readOfficerFile(tmp)
		if err == nil {
			t.Fatal("expected ErrOfficerFileRead for directory")
		}
		if !errors.Is(err, ErrOfficerFileRead) {
			t.Errorf("got %v, want errors.Is(ErrOfficerFileRead)", err)
		}
	})
}

// TestLoadOfficerCSR_RejectsTamperedSignature verifies the CSR loader
// rejects a CSR whose self-signature was corrupted.
func TestLoadOfficerCSR_RejectsTamperedSignature(t *testing.T) {
	tmp := t.TempDir()
	csrPath := filepath.Join(tmp, "tampered.csr.pem")
	writeTamperedCSRFile(t, csrPath, "mallory")

	_, err := loadOfficerCSR(officerSpec{Username: "mallory", Kind: officerKindCSR, Path: csrPath})
	if err == nil {
		t.Fatal("expected ErrCSRSignatureInvalid for tampered CSR")
	}
	if !errors.Is(err, ErrCSRSignatureInvalid) {
		t.Errorf("got %v, want errors.Is(ErrCSRSignatureInvalid)", err)
	}
}

// TestLoadOfficer_NotPEM rejects garbage input across all three kinds.
func TestLoadOfficer_NotPEM(t *testing.T) {
	tmp := t.TempDir()
	bogus := filepath.Join(tmp, "garbage.pem")
	if err := os.WriteFile(bogus, []byte("not a pem file at all\n"), 0o600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	for _, kind := range []officerKind{officerKindCSR, officerKindCert, officerKindPubKey} {
		t.Run(string(kind), func(t *testing.T) {
			loader := officerLoaders[kind]
			_, err := loader(officerSpec{Username: "x", Kind: kind, Path: bogus})
			if err == nil {
				t.Fatal("expected PEM decode error")
			}
			if !errors.Is(err, ErrOfficerPEMDecode) {
				t.Errorf("got %v, want errors.Is(ErrOfficerPEMDecode)", err)
			}
		})
	}
}

// =============================================================================
// Trusted-CA loader
// =============================================================================

// TestLoadTrustedCAs_NilOnEmpty verifies the loader returns (nil, nil)
// when no paths are supplied so callers can short-circuit external-CA
// validation.
func TestLoadTrustedCAs_NilOnEmpty(t *testing.T) {
	pool, err := loadTrustedCAs(nil)
	if err != nil {
		t.Fatalf("loadTrustedCAs(nil) error: %v", err)
	}
	if pool != nil {
		t.Errorf("expected nil pool for empty input")
	}
}

// TestLoadTrustedCAs_RejectsMissingPath ensures missing paths surface
// the canonical typed error.
func TestLoadTrustedCAs_RejectsMissingPath(t *testing.T) {
	tmp := t.TempDir()
	_, err := loadTrustedCAs([]string{filepath.Join(tmp, "absent.pem")})
	if err == nil {
		t.Fatal("expected ErrTrustedCANotFound")
	}
	if !errors.Is(err, ErrTrustedCANotFound) {
		t.Errorf("got %v, want errors.Is(ErrTrustedCANotFound)", err)
	}
}

// TestLoadTrustedCAs_LoadsValidCert validates the happy path: a single
// PEM cert is appended to the pool.
func TestLoadTrustedCAs_LoadsValidCert(t *testing.T) {
	tmp := t.TempDir()
	caPath := filepath.Join(tmp, "ca.pem")
	writeTestCertFile(t, caPath, "root-ca")
	pool, err := loadTrustedCAs([]string{caPath})
	if err != nil {
		t.Fatalf("loadTrustedCAs error: %v", err)
	}
	if pool == nil {
		t.Fatal("expected non-nil pool")
	}
}

// TestLoadTrustedCAs_RejectsNonCertContent ensures a file that decodes
// as something other than a certificate surfaces ErrTrustedCANoCerts.
func TestLoadTrustedCAs_RejectsNonCertContent(t *testing.T) {
	tmp := t.TempDir()
	bogus := filepath.Join(tmp, "garbage.pem")
	if err := os.WriteFile(bogus, []byte("not a cert\n"), 0o600); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	if _, err := loadTrustedCAs([]string{bogus}); err == nil {
		t.Fatal("expected ErrTrustedCANoCerts")
	} else if !errors.Is(err, ErrTrustedCANoCerts) {
		t.Errorf("got %v, want errors.Is(ErrTrustedCANoCerts)", err)
	}
}

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

// TestInit_FlagSetIncludesPhase11Flags ensures the canonical init command
// exposes every Phase 11 flag in addition to the locked canonical surface.
func TestInit_FlagSetIncludesPhase11Flags(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"init"})
	if err != nil {
		t.Fatalf("init command not registered: %v", err)
	}
	required := []string{
		// Locked canonical (also enforced by canonical_surface_test.go).
		"so-pin", "user-pin", "barrier-strategy",
		"shares", "threshold", "hostname", "sans", "admin-bundle",
		// Phase 11 additions.
		"so", "trusted-ca", "credential-strategy", "init-data-dir",
	}
	for _, name := range required {
		if cmd.Flags().Lookup(name) == nil {
			t.Errorf("init flag --%s missing", name)
		}
	}
}

// TestInit_RunInlineHappyPath drives a full run through a fresh cobra
// command using a temporary CSR / cert / pubkey trio. It exercises the
// dispatch table end-to-end and the trusted-CA loader.
func TestInit_RunInlineHappyPath(t *testing.T) {
	tmp := t.TempDir()
	csrPath := filepath.Join(tmp, "alice.csr.pem")
	certPath := filepath.Join(tmp, "bob.cert.pem")
	pubPath := filepath.Join(tmp, "carol.pub.pem")
	caPath := filepath.Join(tmp, "ca.pem")
	writeTestCSRFile(t, csrPath, "alice")
	writeTestCertFile(t, certPath, "bob")
	writeTestPubKeyFile(t, pubPath, "carol")
	writeTestCertFile(t, caPath, "external-ca")

	initDir := filepath.Join(tmp, "datadir")
	f := &inlineInitFlags{
		soPin:              "so-pin-1234",
		userPin:            "user-pin-1234",
		barrierStrategy:    "passphrase",
		secret:             "test-barrier-secret-12345",
		hostname:           "test.example",
		credentialStrategy: "barrier",
		adminBundle:        "-",
		officers:           []string{"alice:csr:" + csrPath, "bob:cert:" + certPath, "carol:pubkey:" + pubPath},
		trustedCAs:         []string{caPath},
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
// strategy validator runs before any officer file is touched.
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
