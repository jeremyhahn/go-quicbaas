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

//go:build tpm_simulator

package tpmsim_test

import (
	"bytes"
	"crypto"
	"crypto/x509"
	"errors"
	"runtime"
	"testing"

	pkgtpm2 "github.com/jeremyhahn/go-xkms/pkg/tpm2"

	"github.com/jeremyhahn/go-quicbaas/pkg/service/attestationtest"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/tpmsim"
)

var errIssuerRefused = errors.New("issuer refused")

// Provisioning creates the IAK but no certificate for it; a CA enrolls it.
func TestProvision_IAKHasNoCertificateUntilEnrolled(t *testing.T) {
	sim := tpmsim.Provision(t)

	if _, err := sim.TPM.IAKCertificate(); !errors.Is(err, pkgtpm2.ErrIAKCertNotFound) {
		t.Fatalf("IAKCertificate before enrollment = %v, want ErrIAKCertNotFound", err)
	}
}

func TestSimulator_EnrollIAK(t *testing.T) {
	sim := tpmsim.Provision(t)
	ca, err := attestationtest.NewAuthority("IAK CA")
	if err != nil {
		t.Fatal(err)
	}

	enrolled := sim.EnrollIAK(t, func(pub crypto.PublicKey) (*x509.Certificate, error) {
		return attestationtest.CertifyAttestationKey(ca, pub, true)
	})

	got, err := sim.TPM.IAKCertificate()
	if err != nil {
		t.Fatalf("IAKCertificate: %v", err)
	}
	if !bytes.Equal(got.Raw, enrolled.Raw) {
		t.Fatal("IAKCertificate does not return the enrolled certificate")
	}
	if err := got.CheckSignatureFrom(ca.Certificate); err != nil {
		t.Fatalf("enrolled certificate does not chain to the issuer: %v", err)
	}
}

func TestSimulator_EnrollIAK_IssuerFailureFailsTheTest(t *testing.T) {
	sim := tpmsim.Provision(t)
	probe := &fatalRecorder{TB: t}

	done := make(chan struct{})
	go func() {
		defer close(done)
		sim.EnrollIAK(probe, func(crypto.PublicKey) (*x509.Certificate, error) {
			return nil, errIssuerRefused
		})
	}()
	<-done

	if !probe.fataled {
		t.Fatal("an issuer failure must fail the test")
	}
	if _, err := sim.TPM.IAKCertificate(); !errors.Is(err, pkgtpm2.ErrIAKCertNotFound) {
		t.Fatalf("IAKCertificate after a failed enrollment = %v, want ErrIAKCertNotFound", err)
	}
}

// fatalRecorder records Fatalf instead of failing the enclosing test, and
// stops the calling goroutine the way testing.T does.
type fatalRecorder struct {
	testing.TB
	fataled bool
}

func (f *fatalRecorder) Fatalf(string, ...any) {
	f.fataled = true
	runtime.Goexit()
}

func (f *fatalRecorder) Helper() {}
