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

//go:build awskms

package server

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	awskmsbackend "github.com/jeremyhahn/go-xkms/pkg/backend/awskms"
	"github.com/jeremyhahn/go-xkms/pkg/storage"
	xkmstypes "github.com/jeremyhahn/go-xkms/pkg/types"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// awsKMSConfig returns a *config.Config selecting the awskms strategy with the
// supplied AWS sub-config, for exercising the factory's validation path.
func awsKMSConfig(sub config.AWSSealConfig) *config.Config {
	cfg := &config.Config{}
	cfg.Seal.Strategy = string(xkmssdk.StrategyAWSKMS)
	cfg.Seal.AWSKMS = sub
	return cfg
}

// TestBuildAWSKMSStrategy_MissingKeyID asserts the factory rejects a config
// with no KMS key ID before attempting any backend construction.
func TestBuildAWSKMSStrategy_MissingKeyID(t *testing.T) {
	_, err := buildAWSKMSStrategy(awsKMSConfig(config.AWSSealConfig{Region: "us-east-1"}))
	if err == nil {
		t.Fatal("expected error for missing key_id, got nil")
	}

	var buildErr *StrategyBuildError
	if !errors.As(err, &buildErr) {
		t.Fatalf("expected *StrategyBuildError, got %T: %v", err, err)
	}
	var cfgErr *AWSKMSConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected wrapped *AWSKMSConfigError, got %v", err)
	}
	if cfgErr.Field != "key_id" {
		t.Errorf("expected missing field key_id, got %q", cfgErr.Field)
	}
}

// TestBuildAWSKMSStrategy_MissingRegion asserts the factory rejects a config
// with a key ID but no region.
func TestBuildAWSKMSStrategy_MissingRegion(t *testing.T) {
	_, err := buildAWSKMSStrategy(awsKMSConfig(config.AWSSealConfig{KeyID: "alias/root"}))
	if err == nil {
		t.Fatal("expected error for missing region, got nil")
	}

	var cfgErr *AWSKMSConfigError
	if !errors.As(err, &cfgErr) {
		t.Fatalf("expected wrapped *AWSKMSConfigError, got %T: %v", err, err)
	}
	if cfgErr.Field != "region" {
		t.Errorf("expected missing field region, got %q", cfgErr.Field)
	}
}

// TestAWSKMSSealKeyAttributes verifies the seal-key attributes are a valid,
// symmetric, AWS-KMS-resident AES-256-GCM key keyed on the configured KeyID.
func TestAWSKMSSealKeyAttributes(t *testing.T) {
	attrs := awsKMSSealKeyAttributes("alias/barrier-root")

	if err := attrs.Validate(); err != nil {
		t.Fatalf("seal key attributes failed validation: %v", err)
	}
	if !attrs.IsSymmetric() {
		t.Error("seal key attributes must be symmetric")
	}
	if attrs.CN != "alias/barrier-root" {
		t.Errorf("expected CN alias/barrier-root, got %q", attrs.CN)
	}
	if attrs.StoreType != xkmstypes.StoreAWSKMS {
		t.Errorf("expected store %s, got %s", xkmstypes.StoreAWSKMS, attrs.StoreType)
	}
	if attrs.SymmetricAlgorithm != xkmstypes.SymmetricAES256GCM {
		t.Errorf("expected AES-256-GCM, got %s", attrs.SymmetricAlgorithm)
	}
}

// TestAWSKMSStrategy_SealUnsealRoundTrip drives the full
// SealRootKey -> UnsealRootKey crypto path through CloudKMSStrategy and a
// mock-backed AWS KMS sealer, proving the strategy + backend composition seals
// and recovers a random 32-byte root key without any real AWS call.
func TestAWSKMSStrategy_SealUnsealRoundTrip(t *testing.T) {
	const keyID = "alias/barrier-root"

	rootKey := make([]byte, 32)
	if _, err := rand.Read(rootKey); err != nil {
		t.Fatalf("failed to generate root key: %v", err)
	}

	// The mock KMS encrypts by reversing the plaintext into an opaque blob and
	// decrypts by reversing it back, so the round-trip is deterministic without
	// touching real AWS. It also echoes the key for DescribeKey lookups.
	mock := &awskmsbackend.MockKMSClient{
		EncryptFunc: func(ctx context.Context, params *kms.EncryptInput, _ ...func(*kms.Options)) (*kms.EncryptOutput, error) {
			blob := make([]byte, len(params.Plaintext))
			for i := range params.Plaintext {
				blob[len(blob)-1-i] = params.Plaintext[i]
			}
			return &kms.EncryptOutput{CiphertextBlob: blob, KeyId: aws.String(keyID)}, nil
		},
		DecryptFunc: func(ctx context.Context, params *kms.DecryptInput, _ ...func(*kms.Options)) (*kms.DecryptOutput, error) {
			plain := make([]byte, len(params.CiphertextBlob))
			for i := range params.CiphertextBlob {
				plain[len(plain)-1-i] = params.CiphertextBlob[i]
			}
			return &kms.DecryptOutput{Plaintext: plain, KeyId: aws.String(keyID)}, nil
		},
	}

	backendCfg := &awskmsbackend.Config{
		Region:      "us-east-1",
		KeyID:       keyID,
		KeyStorage:  storage.New(),
		CertStorage: storage.New(),
	}
	backend, err := awskmsbackend.NewBackendWithClient(backendCfg, mock)
	if err != nil {
		t.Fatalf("NewBackendWithClient failed: %v", err)
	}

	attrs := awsKMSSealKeyAttributes(keyID)

	// Register the operator's pre-provisioned KMS key through the production
	// factory helper rather than seeding metadata by hand: this is the wiring the
	// live daemon relies on so the sealer's SymmetricEncrypter resolves the
	// (mock-managed) key.
	if err := ensureAWSKMSSealKey(backend, attrs, keyID); err != nil {
		t.Fatalf("ensureAWSKMSSealKey failed: %v", err)
	}

	strategy := xkmssdk.NewCloudKMSStrategy(xkmssdk.StrategyAWSKMS, backend, attrs, nil)
	if !strategy.Available() {
		t.Fatal("strategy reported unavailable with a mock-backed sealer")
	}

	ctx := context.Background()

	sealed, err := strategy.SealRootKey(ctx, rootKey, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("SealRootKey failed: %v", err)
	}
	if sealed.Strategy != xkmssdk.StrategyAWSKMS {
		t.Errorf("expected sealed strategy %s, got %s", xkmssdk.StrategyAWSKMS, sealed.Strategy)
	}

	recovered, err := strategy.UnsealRootKey(ctx, sealed, qrdbsdk.Credentials{})
	if err != nil {
		t.Fatalf("UnsealRootKey failed: %v", err)
	}
	if !bytes.Equal(recovered, rootKey) {
		t.Error("recovered root key does not match the original")
	}
}

// TestEnsureAWSKMSSealKey_Idempotent proves the registration helper is safe to
// run on every daemon boot. The first call seeds the operator's KeyID into the
// backend metadata; a second call (modelling a restart, which re-runs the
// factory) upserts the identical mapping without error, and the sealing key
// stays resolvable. No KMS key is created or rotated — only local metadata is
// (re)written, so previously-sealed data continues to unseal.
func TestEnsureAWSKMSSealKey_Idempotent(t *testing.T) {
	const keyID = "alias/barrier-root"

	mock := &awskmsbackend.MockKMSClient{}
	backendCfg := &awskmsbackend.Config{
		Region:      "us-east-1",
		KeyID:       keyID,
		KeyStorage:  storage.New(),
		CertStorage: storage.New(),
	}
	backend, err := awskmsbackend.NewBackendWithClient(backendCfg, mock)
	if err != nil {
		t.Fatalf("NewBackendWithClient failed: %v", err)
	}

	attrs := awsKMSSealKeyAttributes(keyID)

	if err := ensureAWSKMSSealKey(backend, attrs, keyID); err != nil {
		t.Fatalf("first ensureAWSKMSSealKey failed: %v", err)
	}
	if err := ensureAWSKMSSealKey(backend, attrs, keyID); err != nil {
		t.Fatalf("second ensureAWSKMSSealKey failed (not idempotent): %v", err)
	}

	if _, err := backend.SymmetricEncrypter(attrs); err != nil {
		t.Fatalf("sealing key not resolvable after re-registration: %v", err)
	}
}
