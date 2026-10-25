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

package tpmsim

import (
	"crypto"
	"crypto/x509"
	"errors"
	"log/slog"
	"testing"

	pkgtpm2 "github.com/jeremyhahn/go-xkms/pkg/tpm2"
	tpmstore "github.com/jeremyhahn/go-xkms/pkg/tpm2/store"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// platformSRKHandle is the persistent handle of the Platform SRK, go-xkms's
// default.
const platformSRKHandle = 0x81000002

// Simulator is a provisioned go-xkms TPM on the in-process simulator.
type Simulator struct {
	// TPM is the provisioned TPM.
	TPM *pkgtpm2.TPM2

	// Storage holds the provisioned keys; its KeyBackend backs a go-xkms
	// TPM key backend built over TPM.
	Storage *tpmstore.StorageFactory

	certificates tpmstore.CertificateStorer
}

// Provision opens the in-process simulator, provisions an EK, a shared SRK
// and an RSA IAK on it, and closes it when t ends.
//
// This mirrors the simulator provisioning recipe go-xkms uses in its own
// pkg/tpm2 tests (createSim), composed here through the exported API.
// Provisioning creates the IAK but issues no certificate for it: in
// production a CA enrolls it. EnrollIAK plays that CA.
func Provision(t testing.TB) *Simulator {
	t.Helper()

	logger := slog.Default()
	storageFactory, err := tpmstore.NewStorageFactory(logger, t.TempDir())
	if err != nil {
		t.Fatalf("NewStorageFactory: %v", err)
	}
	hierarchyAuth, err := xkmssdk.NewLiteralSecretValue(t.Context(), []byte(tpmstore.DEFAULT_PASSWORD))
	if err != nil {
		t.Fatalf("NewLiteralSecretValue: %v", err)
	}

	config := &pkgtpm2.Config{
		Device:       "/dev/tpmrm0",
		UseSimulator: true,
		Hash:         "SHA-256",
		EK: &pkgtpm2.EKConfig{
			CertHandle:    0x01C00002,
			Handle:        0x81010001,
			HierarchyAuth: hierarchyAuth,
			KeyAlgorithm:  x509.RSA.String(),
			RSAConfig:     &tpmstore.RSAConfig{KeySize: 2048},
		},
		IdentityProvisioningStrategy: string(pkgtpm2.EnrollmentStrategyIAK),
		IAK: &pkgtpm2.IAKConfig{
			CN:                 "quicbaas-simulator",
			Hash:               "SHA-256",
			Handle:             0x81010002,
			KeyAlgorithm:       x509.RSA.String(),
			RSAConfig:          &tpmstore.RSAConfig{KeySize: 2048},
			SignatureAlgorithm: x509.SHA256WithRSAPSS.String(),
		},
		PlatformPCRSelection: pkgtpm2.PlatformPCRSelection{
			PlatformPCRs:    pkgtpm2.DefaultPlatformPCRs(),
			PlatformPCRBank: pkgtpm2.PCRBankSHA256,
		},
		GoldenPCRs: []uint{0, 7},
		SSRK: &pkgtpm2.SRKConfig{
			Handle:        0x81000001,
			HierarchyAuth: hierarchyAuth,
			KeyAlgorithm:  x509.RSA.String(),
			RSAConfig:     &tpmstore.RSAConfig{KeySize: 2048},
		},
		PlatformSRK: &pkgtpm2.PlatformSRKConfig{SRKHandle: platformSRKHandle, PlatformPolicy: true},
	}

	certificates := tpmstore.NewBlobCertStore(logger, storageFactory.KeyBackend())
	tpm, err := pkgtpm2.NewTPM2(&pkgtpm2.Params{
		Logger:    logger,
		Config:    config,
		CertStore: certificates,
		Backend:   storageFactory.KeyBackend(),
		FQDN:      "node1.quicbaas.test",
	})
	if err != nil {
		if !errors.Is(err, pkgtpm2.ErrNotInitialized) || tpm == nil {
			t.Fatalf("NewTPM2: %v", err)
		}
		if perr := tpm.Provision(nil); perr != nil {
			if cerr := tpm.Close(); cerr != nil {
				t.Errorf("Close after failed Provision: %v", cerr)
			}
			t.Fatalf("Provision: %v", perr)
		}
		if perr := createPlatformSRK(tpm, config); perr != nil {
			if cerr := tpm.Close(); cerr != nil {
				t.Errorf("Close after failed Platform SRK creation: %v", cerr)
			}
			t.Fatalf("create Platform SRK: %v", perr)
		}
	}
	t.Cleanup(func() {
		if err := tpm.Close(); err != nil {
			t.Errorf("Close simulator TPM: %v", err)
		}
	})

	concrete, ok := tpm.(*pkgtpm2.TPM2)
	if !ok {
		t.Fatalf("NewTPM2 returned %T, want *tpm2.TPM2", tpm)
	}
	return &Simulator{TPM: concrete, Storage: storageFactory, certificates: certificates}
}

// createPlatformSRK creates the Platform SRK, with empty auth, under the
// endorsement key, as pkgtpm2.ProvisionFresh does for a fresh simulator.
// TPM2.Provision creates the EK, the Shared SRK and the IAK but not the
// Platform SRK, and a go-xkms TPM key backend parents every object it seals or
// creates under the Platform SRK: without it, NewBackendWithTPM fails with
// TPM_RC_HANDLE on the Platform SRK's handle.
func createPlatformSRK(tpm pkgtpm2.TrustedPlatformModule, config *pkgtpm2.Config) error {
	ekAttrs, err := tpm.EKAttributes()
	if err != nil {
		return err
	}
	srkAttrs, err := pkgtpm2.PlatformSRKAttributesFromConfig(config)
	if err != nil {
		return err
	}
	srkAttrs.Parent = ekAttrs
	return tpm.CreateSRK(srkAttrs)
}

// IAKPublicKey returns the public half of the provisioned IAK.
func (s *Simulator) IAKPublicKey(t testing.TB) crypto.PublicKey {
	t.Helper()
	attrs, err := s.TPM.IAKAttributes()
	if err != nil {
		t.Fatalf("IAKAttributes: %v", err)
	}
	pub, err := pkgtpm2.ExtractPublicKeyFromTPMPublic(pkgtpm2.MarshalPublic(&attrs.TPMAttributes.Public))
	if err != nil {
		t.Fatalf("IAK public key: %v", err)
	}
	return pub
}

// EnrollIAK has issue certify the IAK's public key, as the enrolling CA would,
// and stores the certificate where TPM.IAKCertificate reads it.
func (s *Simulator) EnrollIAK(t testing.TB, issue func(crypto.PublicKey) (*x509.Certificate, error)) *x509.Certificate {
	t.Helper()
	cert, err := issue(s.IAKPublicKey(t))
	if err != nil {
		t.Fatalf("issue IAK certificate: %v", err)
	}
	attrs, err := s.TPM.IAKAttributes()
	if err != nil {
		t.Fatalf("IAKAttributes: %v", err)
	}
	if err := s.certificates.Save(attrs, cert); err != nil {
		t.Fatalf("save IAK certificate: %v", err)
	}
	return cert
}
