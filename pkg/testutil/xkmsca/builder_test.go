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

package xkmsca

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// Build returns a real CA that is NOT up: every issuance is refused. That is the
// honest "the CA is not running" condition, and it is what NewUninitialized
// hands a failure test.
func TestBuild_ReturnsACAThatIsNotYetUp(t *testing.T) {
	ca, err := Build(context.Background(), Config{CommonName: "build-not-up"})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if ca == nil {
		t.Fatal("Build returned a nil CA with no error")
	}
	if _, err := ca.CACertificate(); err == nil {
		t.Fatal("a CA that has not been initialized produced a certificate")
	}
}

// A Config with no common name is refused. The CA addresses its own key and
// certificate BY common name, so an empty one builds a CA that cannot find its
// own material — a failure that would otherwise surface much later as a missing
// certificate.
func TestBuild_RejectsAnEmptyCommonName(t *testing.T) {
	ca, err := Build(context.Background(), Config{})
	if !errors.Is(err, ErrNoCommonName) {
		t.Fatalf("Build error = %v, want ErrNoCommonName", err)
	}
	if ca != nil {
		t.Error("Build returned a CA alongside the error")
	}
}

// Open brings the CA up and it can issue.
func TestOpen_BringsTheCAUp(t *testing.T) {
	ca, err := Open(context.Background(), Config{CommonName: "open-memory"})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	cert, err := ca.CACertificate()
	if err != nil {
		t.Fatalf("CACertificate: %v", err)
	}
	if cert.Subject.CommonName != "open-memory" {
		t.Errorf("root CN = %q, want %q", cert.Subject.CommonName, "open-memory")
	}
	if cert.Subject.Organization[0] != DefaultOrganization {
		t.Errorf("root O = %v, want %q", cert.Subject.Organization, DefaultOrganization)
	}
	if !cert.IsCA {
		t.Error("root certificate is not a CA")
	}
}

// Open on a bad Config reports the Config's error rather than trying to bring
// anything up.
func TestOpen_RejectsAnEmptyCommonName(t *testing.T) {
	if _, err := Open(context.Background(), Config{}); !errors.Is(err, ErrNoCommonName) {
		t.Fatalf("Open error = %v, want ErrNoCommonName", err)
	}
}

// A Dir'd CA persists, and a SECOND Open over the same directory ADOPTS the
// existing root instead of minting another one under the same subject name.
//
// This is the property the integration fixtures depend on. A second Init would
// leave every certificate the first root signed failing verification against an
// authority the verifier can name — which reads as a broken cluster rather than
// as rotated trust. Compose starts the fixture service twice, and the suites
// that run afterwards open the same CA again to issue runtime credentials.
func TestOpen_AdoptsAnExistingCAOnTheSecondCall(t *testing.T) {
	dir := t.TempDir()
	cfg := Config{CommonName: "persistent-root", Dir: dir}

	first, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("first Open: %v", err)
	}
	firstCert, err := first.CACertificate()
	if err != nil {
		t.Fatalf("first CACertificate: %v", err)
	}

	second, err := Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("second Open: %v", err)
	}
	secondCert, err := second.CACertificate()
	if err != nil {
		t.Fatalf("second CACertificate: %v", err)
	}

	if firstCert.SerialNumber.Cmp(secondCert.SerialNumber) != 0 {
		t.Fatalf("second Open minted a new root: serial %s then %s",
			firstCert.SerialNumber, secondCert.SerialNumber)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) == 0 {
		t.Errorf("a CA with Dir=%s wrote nothing", dir)
	}
}

// An empty Dir selects in-memory storage, so a unit test writes nothing and
// needs no cleanup; a non-empty Dir selects the file backends the daemon uses.
func TestStorage_SelectsMemoryOrFiles(t *testing.T) {
	keys, certs, revocations, err := storage("")
	if err != nil {
		t.Fatalf("storage(\"\"): %v", err)
	}
	if keys == nil || certs == nil || revocations == nil {
		t.Fatal("storage(\"\") returned a nil backend")
	}

	dir := t.TempDir()
	if _, _, _, err := storage(dir); err != nil {
		t.Fatalf("storage(%s): %v", dir, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	if len(entries) == 0 {
		t.Errorf("storage(%s) created no file storage under it", dir)
	}
	if _, err := os.Stat(filepath.Join(dir, "revocations")); err != nil {
		t.Errorf("storage(%s) created no revocation store: %v", dir, err)
	}
}

// A Dir that cannot be created is reported, not ignored.
func TestBuild_ReportsAnUnusableDir(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write %s: %v", blocker, err)
	}
	if _, err := Build(context.Background(), Config{CommonName: "unusable-dir", Dir: blocker}); err == nil {
		t.Fatal("Build over a file path succeeded; want a storage error")
	}
}

// The defaults are applied to an otherwise empty Config, and an explicit value
// overrides them.
func TestCAIdentity_AppliesAndOverridesDefaults(t *testing.T) {
	id := caIdentity(Config{CommonName: "defaults"})
	if id.Subject.Organization != DefaultOrganization {
		t.Errorf("organization = %q, want %q", id.Subject.Organization, DefaultOrganization)
	}
	if id.Valid != DefaultValidYears {
		t.Errorf("valid years = %d, want %d", id.Valid, DefaultValidYears)
	}
	if !id.IsRoot || id.GetStoreType() != xkmssdk.StoreSoftware {
		t.Errorf("identity = %+v, want a software root", id)
	}

	id = caIdentity(Config{CommonName: "explicit", Organization: "Acme", ValidYears: 2})
	if id.Subject.Organization != "Acme" || id.Valid != 2 {
		t.Errorf("identity = %+v, want O=Acme valid=2", id)
	}
}

// validityDays applies the default only when nothing was configured.
func TestValidityDays(t *testing.T) {
	if got := validityDays(Config{}); got != DefaultCertValidityDays {
		t.Errorf("validityDays(zero) = %d, want %d", got, DefaultCertValidityDays)
	}
	if got := validityDays(Config{CertValidityDays: 7}); got != 7 {
		t.Errorf("validityDays(7) = %d, want 7", got)
	}
	if got := validityDays(Config{CertValidityDays: -1}); got != DefaultCertValidityDays {
		t.Errorf("validityDays(-1) = %d, want %d", got, DefaultCertValidityDays)
	}
}
