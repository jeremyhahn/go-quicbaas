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

package server

import (
	"bytes"
	"context"
	"crypto/x509"
	"log/slog"
	"testing"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	tpm2backend "github.com/jeremyhahn/go-xkms/pkg/backend/tpm2"
	pkgtpm2 "github.com/jeremyhahn/go-xkms/pkg/tpm2"
	xkmstypes "github.com/jeremyhahn/go-xkms/pkg/types"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/testutil/tpmsim"
)

// TestBuildTPM2Strategy_FactoryDispatch_Simulator drives the explicit tpm2
// seal strategy through buildHardwareSealStrategiesShared against a fresh
// in-memory simulator. go-xkms provisions a fresh simulator in place (EK,
// Shared SRK and Platform SRK), so the build returns exactly one TPM2 strategy
// and the open backend it owns. The test closes that backend: it holds the
// process-wide simulator, and every later simulator test would otherwise block
// on it until the package timeout.
func TestBuildTPM2Strategy_FactoryDispatch_Simulator(t *testing.T) {
	cfg := config.TestConfig()
	cfg.Seal.Strategy = string(xkmssdk.StrategyTPM2)
	cfg.Seal.TPM2.UseSimulator = true
	cfg.Seal.TPM2.KeyDir = t.TempDir()

	strategies, backend, err := buildHardwareSealStrategiesShared(cfg, nil)
	if err != nil {
		t.Fatalf("buildHardwareSealStrategiesShared: %v", err)
	}
	if backend == nil {
		t.Fatal("buildHardwareSealStrategiesShared returned no TPM2 backend for the tpm2 strategy")
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Errorf("close backend: %v", err)
		}
	})

	if len(strategies) != 1 {
		t.Fatalf("expected exactly one TPM2 strategy, got %d", len(strategies))
	}
	if strategies[0].ID() != xkmssdk.StrategyTPM2 {
		t.Fatalf("strategy.ID() = %q, want %q", strategies[0].ID(), xkmssdk.StrategyTPM2)
	}
}

// TestBuildTPM2Strategy_RoundTrip_Simulator performs a FULL root-key
// seal -> unseal round trip through the TPM2 sealing strategy backed by a
// provisioned in-memory TPM simulator.
//
// It composes exported go-xkms API only — it does not reimplement any TPM
// crypto:
//
//  1. tpmsim.Provision builds and provisions a TPM via pkgtpm2.NewTPM2 +
//     Provision, then creates the Platform SRK the backend parents under.
//  2. tpm2backend.NewBackendWithTPM wraps that provisioned TPM as the
//     types.Sealer-implementing backend the production factory would use.
//  3. xkmssdk.NewTPM2Strategy — the exact wrapper buildTPM2Strategy uses —
//     turns the backend into the qrdbsdk.SealingStrategy the barrier consumes.
//
// This proves the tpm2 strategy path end-to-end: a 32-byte root key sealed
// under the TPM storage hierarchy and recovered byte-for-byte.
func TestBuildTPM2Strategy_RoundTrip_Simulator(t *testing.T) {
	backend := provisionedSimulatorBackend(t)

	// Wrap the provisioned backend exactly as buildTPM2Strategy does.
	strategy := xkmssdk.NewTPM2Strategy(backend, nil)

	if strategy.ID() != xkmssdk.StrategyTPM2 {
		t.Fatalf("strategy.ID() = %q, want %q", strategy.ID(), xkmssdk.StrategyTPM2)
	}
	if !strategy.HardwareBacked() {
		t.Fatal("TPM2 strategy must report HardwareBacked() == true")
	}
	if !strategy.Available() {
		t.Fatal("TPM2 strategy reports Available() == false over a provisioned, seal-capable backend")
	}

	// Full root-key seal through the strategy (TPM2Strategy.SealRootKey calls
	// the backend's default-key Seal path, which parents the sealed object to
	// the platform SRK).
	ctx := context.Background()
	rootKey := []byte("0123456789abcdef0123456789abcdef")

	sealed, err := strategy.SealRootKey(ctx, rootKey, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("SealRootKey: %v", err)
	}

	recovered, err := strategy.UnsealRootKey(ctx, sealed, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("UnsealRootKey: %v", err)
	}
	if string(recovered) != string(rootKey) {
		t.Fatalf("recovered root key mismatch: got %x want %x", recovered, rootKey)
	}
}

// TestProvisionedTPM2Backend_SealUnseal_Simulator proves that the composed
// go-xkms TPM2 backend performs real seal -> unseal crypto through the
// in-memory simulator. It seals under the backend's storage parent, the
// Platform SRK, bound to the platform PCR policy, via explicit SealOptions,
// which exercises the same
// tpm2backend.Backend that the production factory wraps with
// xkmssdk.NewTPM2Strategy.
func TestProvisionedTPM2Backend_SealUnseal_Simulator(t *testing.T) {
	backend := provisionedSimulatorBackend(t)

	ssrk := backend.SRKAttributes()
	if ssrk == nil {
		t.Fatal("provisioned backend returned nil SRK attributes")
	}

	ctx := context.Background()
	rootKey := []byte("0123456789abcdef0123456789abcdef")

	opts := &xkmstypes.SealOptions{
		KeyAttributes: &xkmstypes.KeyAttributes{
			CN:        "quicbaas-seal-roundtrip",
			Parent:    ssrk,
			StoreType: xkmstypes.StoreTPM2,
			TPMPolicy: xkmstypes.TPMPolicyPlatform,
		},
	}

	sealed, err := backend.Seal(ctx, rootKey, opts)
	if err != nil {
		t.Fatalf("backend.Seal: %v", err)
	}
	if len(sealed.TPMPublic) == 0 || len(sealed.TPMPrivate) == 0 {
		t.Fatal("sealed data missing TPM public/private blobs")
	}

	recovered, err := backend.Unseal(ctx, sealed, &xkmstypes.UnsealOptions{
		KeyAttributes: &xkmstypes.KeyAttributes{
			CN:        sealed.KeyID,
			Parent:    ssrk,
			StoreType: xkmstypes.StoreTPM2,
			TPMPolicy: xkmstypes.TPMPolicyPlatform,
		},
	})
	if err != nil {
		t.Fatalf("backend.Unseal: %v", err)
	}
	if string(recovered) != string(rootKey) {
		t.Fatalf("recovered root key mismatch: got %x want %x", recovered, rootKey)
	}
}

// TestProvisionedTPM2Backend_ActivateCredentialIAK_Simulator proves the
// quicbaas TPM provisioning recipe carries ek.hierarchy-auth into go-xkms.
// go-xkms v0.49.0-alpha requires that authorization when ActivateCredential
// administers the IAK under the TCG IDevID/IAK policy.
func TestProvisionedTPM2Backend_ActivateCredentialIAK_Simulator(t *testing.T) {
	tpm := tpmsim.Provision(t).TPM

	ekAttrs, err := tpm.EKAttributes()
	if err != nil {
		t.Fatalf("EKAttributes: %v", err)
	}
	if ekAttrs == nil || ekAttrs.TPMAttributes == nil || len(ekAttrs.TPMAttributes.PublicKeyBytes) == 0 {
		t.Fatal("provisioned EK attributes are missing public key bytes")
	}
	ekPub, err := x509.ParsePKIXPublicKey(ekAttrs.TPMAttributes.PublicKeyBytes)
	if err != nil {
		t.Fatalf("parse EK public key: %v", err)
	}

	iakAttrs, err := tpm.IAKAttributes()
	if err != nil {
		t.Fatalf("IAKAttributes: %v", err)
	}
	if iakAttrs == nil || iakAttrs.TPMAttributes == nil || len(iakAttrs.TPMAttributes.Name.Buffer) == 0 {
		t.Fatal("provisioned IAK attributes are missing the TPM name")
	}

	challenge, wantSecret, err := pkgtpm2.NewCredentialChallenge(ekPub, iakAttrs.TPMAttributes.Name.Buffer)
	if err != nil {
		t.Fatalf("NewCredentialChallenge: %v", err)
	}
	gotSecret, err := tpm.ActivateCredential(challenge.CredentialBlob, challenge.EncryptedSecret)
	if err != nil {
		t.Fatalf("ActivateCredential: %v", err)
	}
	if !bytes.Equal(gotSecret, wantSecret) {
		t.Fatalf("activated secret mismatch: got %x want %x", gotSecret, wantSecret)
	}
}

// provisionedSimulatorBackend builds a provisioned TPM2 backend backed by the
// in-memory simulator and returns it as a *tpm2backend.Backend (which
// implements types.Sealer), closed when the test ends. It composes exported
// go-xkms API; no TPM crypto is reimplemented here.
func provisionedSimulatorBackend(t *testing.T) *tpm2backend.Backend {
	t.Helper()

	sim := tpmsim.Provision(t)

	backend, err := tpm2backend.NewBackendWithTPM(&tpm2backend.ExternalTPMConfig{
		TPM:        sim.TPM,
		KeyBackend: sim.Storage.KeyBackend(),
		Logger:     slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewBackendWithTPM: %v", err)
	}
	t.Cleanup(func() {
		if err := backend.Close(); err != nil {
			t.Errorf("close backend: %v", err)
		}
	})
	if !backend.CanSeal() {
		t.Fatal("provisioned backend reports CanSeal() == false")
	}
	return backend
}
