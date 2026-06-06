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

//go:build gcpkms

package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"testing"

	"cloud.google.com/go/kms/apiv1/kmspb"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	gcpkmsbackend "github.com/jeremyhahn/go-xkms/pkg/backend/gcpkms"
	"github.com/jeremyhahn/go-xkms/pkg/storage"
	xkmstypes "github.com/jeremyhahn/go-xkms/pkg/types"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// gcpKMSConfig returns a *config.Config selecting the gcpkms strategy with the
// supplied GCP sub-config, for exercising the factory's validation path.
func gcpKMSConfig(sub config.GCPSealConfig) *config.Config {
	cfg := &config.Config{}
	cfg.Seal.Strategy = string(xkmssdk.StrategyGCPKMS)
	cfg.Seal.GCPKMS = sub
	return cfg
}

// gcpRequired is a fully populated GCP sub-config used to verify that omitting
// any single required field is rejected.
var gcpRequired = config.GCPSealConfig{
	ProjectID:  "proj",
	LocationID: "global",
	KeyRingID:  "ring",
	KeyID:      "barrier-root",
}

// TestBuildGCPKMSStrategy_MissingFields asserts the factory rejects a config
// with any single required field omitted, before any backend construction.
func TestBuildGCPKMSStrategy_MissingFields(t *testing.T) {
	cases := []struct {
		name  string
		field string
		sub   config.GCPSealConfig
	}{
		{"project", "project_id", config.GCPSealConfig{LocationID: "global", KeyRingID: "ring", KeyID: "k"}},
		{"location", "location_id", config.GCPSealConfig{ProjectID: "proj", KeyRingID: "ring", KeyID: "k"}},
		{"keyring", "key_ring_id", config.GCPSealConfig{ProjectID: "proj", LocationID: "global", KeyID: "k"}},
		{"key", "key_id", config.GCPSealConfig{ProjectID: "proj", LocationID: "global", KeyRingID: "ring"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildGCPKMSStrategy(gcpKMSConfig(tc.sub))
			if err == nil {
				t.Fatalf("expected error for missing %s, got nil", tc.field)
			}
			var cfgErr *GCPKMSConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("expected wrapped *GCPKMSConfigError, got %T: %v", err, err)
			}
			if cfgErr.Field != tc.field {
				t.Errorf("expected missing field %q, got %q", tc.field, cfgErr.Field)
			}
		})
	}
}

// TestGCPKMSSealKeyAttributes verifies the seal-key attributes are a valid,
// symmetric, GCP-KMS-resident AES-256-GCM key keyed on the configured KeyID.
func TestGCPKMSSealKeyAttributes(t *testing.T) {
	attrs := gcpKMSSealKeyAttributes("barrier-root")

	if err := attrs.Validate(); err != nil {
		t.Fatalf("seal key attributes failed validation: %v", err)
	}
	if !attrs.IsSymmetric() {
		t.Error("seal key attributes must be symmetric")
	}
	if attrs.StoreType != xkmstypes.StoreGCPKMS {
		t.Errorf("expected store %s, got %s", xkmstypes.StoreGCPKMS, attrs.StoreType)
	}
	if attrs.SymmetricAlgorithm != xkmstypes.SymmetricAES256GCM {
		t.Errorf("expected AES-256-GCM, got %s", attrs.SymmetricAlgorithm)
	}
}

// TestGCPKMSStrategy_SealUnsealRoundTrip drives the full
// SealRootKey -> UnsealRootKey crypto path through CloudKMSStrategy and a
// mock-backed GCP KMS sealer, proving the strategy + backend composition seals
// and recovers a random 32-byte root key without any real GCP call.
func TestGCPKMSStrategy_SealUnsealRoundTrip(t *testing.T) {
	const keyID = "barrier-root"

	rootKey := make([]byte, 32)
	if _, err := rand.Read(rootKey); err != nil {
		t.Fatalf("failed to generate root key: %v", err)
	}

	// The mock returns an ENCRYPT_DECRYPT crypto key so the symmetric path is
	// taken, and relies on the mock's default Encrypt/Decrypt (byte reversal
	// with matching CRC32C) for a deterministic round-trip without real GCP.
	mock := &gcpkmsbackend.MockKMSClient{
		GetCryptoKeyFunc: func(ctx context.Context, req *kmspb.GetCryptoKeyRequest, _ ...interface{}) (*kmspb.CryptoKey, error) {
			return &kmspb.CryptoKey{
				Name:    req.Name,
				Purpose: kmspb.CryptoKey_ENCRYPT_DECRYPT,
				Primary: &kmspb.CryptoKeyVersion{
					Name:  req.Name + "/cryptoKeyVersions/1",
					State: kmspb.CryptoKeyVersion_ENABLED,
				},
			}, nil
		},
	}

	backendCfg := &gcpkmsbackend.Config{
		ProjectID:   "proj",
		LocationID:  "global",
		KeyRingID:   "ring",
		KeyStorage:  storage.New(),
		CertStorage: storage.New(),
	}
	backend, err := gcpkmsbackend.NewBackendWithClient(backendCfg, mock)
	if err != nil {
		t.Fatalf("NewBackendWithClient failed: %v", err)
	}

	attrs := gcpKMSSealKeyAttributes(keyID)
	strategy := xkmssdk.NewCloudKMSStrategy(xkmssdk.StrategyGCPKMS, backend, attrs, nil)
	if !strategy.Available() {
		t.Fatal("strategy reported unavailable with a mock-backed sealer")
	}

	ctx := context.Background()

	sealed, err := strategy.SealRootKey(ctx, rootKey, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("SealRootKey failed: %v", err)
	}
	if sealed.Strategy != xkmssdk.StrategyGCPKMS {
		t.Errorf("expected sealed strategy %s, got %s", xkmssdk.StrategyGCPKMS, sealed.Strategy)
	}

	recovered, err := strategy.UnsealRootKey(ctx, sealed, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("UnsealRootKey failed: %v", err)
	}
	if !bytes.Equal(recovered, rootKey) {
		t.Error("recovered root key does not match the original")
	}
}

// TestEnsureGCPKMSSealKey_ExistingKeyReused proves the registration helper
// detects a pre-provisioned crypto key (the operator's normal case) and reuses
// it without attempting creation. CreateCryptoKey is wired to fail the test if
// called, guaranteeing the existing key is never rotated or recreated.
func TestEnsureGCPKMSSealKey_ExistingKeyReused(t *testing.T) {
	const keyID = "barrier-root"

	mock := &gcpkmsbackend.MockKMSClient{
		GetCryptoKeyFunc: func(ctx context.Context, req *kmspb.GetCryptoKeyRequest, _ ...interface{}) (*kmspb.CryptoKey, error) {
			return &kmspb.CryptoKey{
				Name:    req.Name,
				Purpose: kmspb.CryptoKey_ENCRYPT_DECRYPT,
				Primary: &kmspb.CryptoKeyVersion{
					Name:  req.Name + "/cryptoKeyVersions/1",
					State: kmspb.CryptoKeyVersion_ENABLED,
				},
			}, nil
		},
		CreateCryptoKeyFunc: func(ctx context.Context, req *kmspb.CreateCryptoKeyRequest, _ ...interface{}) (*kmspb.CryptoKey, error) {
			t.Fatal("CreateCryptoKey must not be called when the key already exists")
			return nil, nil
		},
	}

	backendCfg := &gcpkmsbackend.Config{
		ProjectID:   "proj",
		LocationID:  "global",
		KeyRingID:   "ring",
		KeyStorage:  storage.New(),
		CertStorage: storage.New(),
	}
	backend, err := gcpkmsbackend.NewBackendWithClient(backendCfg, mock)
	if err != nil {
		t.Fatalf("NewBackendWithClient failed: %v", err)
	}

	if err := ensureGCPKMSSealKey(backend, gcpKMSSealKeyAttributes(keyID)); err != nil {
		t.Fatalf("ensureGCPKMSSealKey failed for existing key: %v", err)
	}
}

// TestEnsureGCPKMSSealKey_CreatesOnFirstInit proves the helper provisions a
// crypto key on a fresh deployment: when GetCryptoKey reports the key absent,
// it falls through to CreateCryptoKey. This is the only path that creates a key.
func TestEnsureGCPKMSSealKey_CreatesOnFirstInit(t *testing.T) {
	const keyID = "barrier-root"

	var created bool
	mock := &gcpkmsbackend.MockKMSClient{
		GetCryptoKeyFunc: func(ctx context.Context, req *kmspb.GetCryptoKeyRequest, _ ...interface{}) (*kmspb.CryptoKey, error) {
			if !created {
				return nil, errors.New("rpc error: code = NotFound desc = key not found")
			}
			return &kmspb.CryptoKey{
				Name:    req.Name,
				Purpose: kmspb.CryptoKey_ENCRYPT_DECRYPT,
			}, nil
		},
		CreateCryptoKeyFunc: func(ctx context.Context, req *kmspb.CreateCryptoKeyRequest, _ ...interface{}) (*kmspb.CryptoKey, error) {
			created = true
			name := req.Parent + "/cryptoKeys/" + req.CryptoKeyId
			return &kmspb.CryptoKey{
				Name:    name,
				Purpose: kmspb.CryptoKey_ENCRYPT_DECRYPT,
				Primary: &kmspb.CryptoKeyVersion{
					Name:  name + "/cryptoKeyVersions/1",
					State: kmspb.CryptoKeyVersion_ENABLED,
				},
			}, nil
		},
		GetCryptoKeyVersionFunc: func(ctx context.Context, req *kmspb.GetCryptoKeyVersionRequest, _ ...interface{}) (*kmspb.CryptoKeyVersion, error) {
			return &kmspb.CryptoKeyVersion{Name: req.Name, State: kmspb.CryptoKeyVersion_ENABLED}, nil
		},
	}

	backendCfg := &gcpkmsbackend.Config{
		ProjectID:   "proj",
		LocationID:  "global",
		KeyRingID:   "ring",
		KeyStorage:  storage.New(),
		CertStorage: storage.New(),
	}
	backend, err := gcpkmsbackend.NewBackendWithClient(backendCfg, mock)
	if err != nil {
		t.Fatalf("NewBackendWithClient failed: %v", err)
	}

	if err := ensureGCPKMSSealKey(backend, gcpKMSSealKeyAttributes(keyID)); err != nil {
		t.Fatalf("ensureGCPKMSSealKey failed on first init: %v", err)
	}
	if !created {
		t.Fatal("expected CreateCryptoKey to be invoked on first init")
	}
}
