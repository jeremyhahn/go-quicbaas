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
	"crypto/x509"
	"encoding/json"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	awskmsbackend "github.com/jeremyhahn/go-xkms/pkg/backend/awskms"
	"github.com/jeremyhahn/go-xkms/pkg/storage"
	xkmstypes "github.com/jeremyhahn/go-xkms/pkg/types"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file registers the AWS KMS sealing strategy with the barrier composer.
// It is compiled only under the `awskms` build tag, which pulls in
// go-xkms/pkg/backend/awskms (aws-sdk-go-v2). When the tag is absent the
// factory is not registered and buildHardwareSealStrategies returns a
// StrategyNotCompiledError instructing the operator to rebuild with -tags
// awskms.
//
// QuicBaaS composes go-xkms: an awskmsbackend.Backend (which implements
// xkmstypes.Sealer) is constructed from the operator config and wrapped by
// xkmssdk.NewCloudKMSStrategy. No KMS crypto is reimplemented here — the
// plaintext root key is sealed by the AWS KMS Encrypt API under a customer
// master key and never leaves KMS in plaintext form.

// AWSKMSConfigError reports that the operator's AWS KMS seal configuration is
// missing a required field. It is returned before any backend construction is
// attempted so the operator gets a precise, library-independent diagnostic.
// Field names the missing configuration key.
type AWSKMSConfigError struct {
	Field string
}

// Error implements the error interface.
func (e *AWSKMSConfigError) Error() string {
	return "seal strategy: awskms config missing required field: " + e.Field
}

// AWSKMSKeyRegistrationError reports that the factory could not register the
// operator's pre-provisioned KMS key with the backend. KeyID names the
// configured customer master key; Cause carries the underlying go-xkms failure.
type AWSKMSKeyRegistrationError struct {
	KeyID string
	Cause error
}

// Error implements the error interface.
func (e *AWSKMSKeyRegistrationError) Error() string {
	return "seal strategy: awskms sealing key registration failed for key " + e.KeyID + ": " + e.Cause.Error()
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *AWSKMSKeyRegistrationError) Unwrap() error { return e.Cause }

// buildAWSKMSStrategy constructs the AWS KMS sealing strategy from
// cfg.Seal.AWSKMS.
//
// It validates the operator config up front and surfaces any missing required
// field as a *StrategyBuildError wrapping an *AWSKMSConfigError, so the failure
// is distinguishable from a backend-construction failure. The required fields
// are:
//
//   - Region: the AWS region hosting the KMS key.
//   - KeyID: the KMS customer master key (key ID, ARN, or alias).
//
// On success it builds an awskmsbackend.Backend from the config (with in-memory
// metadata stores — the actual key material stays in AWS KMS), registers the
// operator's pre-provisioned KMS key with the backend (see ensureAWSKMSSealKey),
// and wraps the backend plus the xkmstypes.KeyAttributes that identify the
// KMS-managed AES-256-GCM key by its configured CN (the operator's KeyID) in
// xkmssdk.NewCloudKMSStrategy.
//
// The optional hardware SymmetricEncrypter is left nil: barrier data
// encryption then derives a software DEK from the KMS-protected root key,
// keeping the root key cloud-bound while avoiding a KMS round-trip per data
// operation.
func buildAWSKMSStrategy(cfg *config.Config) (qrdbsdk.SealingStrategy, error) {
	aws := cfg.Seal.AWSKMS

	if aws.KeyID == "" {
		return nil, &StrategyBuildError{Backend: "awskms", Cause: &AWSKMSConfigError{Field: "key_id"}}
	}
	if aws.Region == "" {
		return nil, &StrategyBuildError{Backend: "awskms", Cause: &AWSKMSConfigError{Field: "region"}}
	}

	backendCfg := &awskmsbackend.Config{
		Region:          aws.Region,
		KeyID:           aws.KeyID,
		AccessKeyID:     aws.AccessKeyID,
		SecretAccessKey: aws.SecretAccessKey,
		SessionToken:    aws.SessionToken,
		Endpoint:        aws.Endpoint,
		KeyStorage:      storage.New(),
		CertStorage:     storage.New(),
	}

	backend, err := awskmsbackend.NewBackend(backendCfg)
	if err != nil {
		return nil, &StrategyBuildError{Backend: "awskms", Cause: err}
	}

	keyAttrs := awsKMSSealKeyAttributes(aws.KeyID)

	if err := ensureAWSKMSSealKey(backend, keyAttrs, aws.KeyID); err != nil {
		return nil, &StrategyBuildError{Backend: "awskms", Cause: err}
	}

	return xkmssdk.NewCloudKMSStrategy(xkmssdk.StrategyAWSKMS, backend, keyAttrs, nil), nil
}

// ensureAWSKMSSealKey registers the operator's pre-provisioned KMS customer
// master key with the backend so the sealer's SymmetricEncrypter can resolve it.
//
// The AWS KMS backend never creates a CMK for sealing: the operator provisions
// it out of band and supplies its KeyID (key ID, ARN, or alias) in config. The
// backend resolves the key through its in-process metadata store, keyed on CN,
// from which Backend.getKeyID reads the "key_id" entry and which
// Backend.SymmetricEncrypter's existence check requires. That store starts empty
// on every daemon boot, so without this seeding the very first barrier init
// fails with ErrKeyNotFound.
//
// Idempotency: Save is called with overwrite=true so it is an upsert — on the
// first boot it seeds the metadata, and on every subsequent restart it rewrites
// the identical "key_id" mapping. No KMS key is created, rotated, or deleted; a
// restart resolves the same operator CMK and previously-sealed data unseals. A
// failure is wrapped in *AWSKMSKeyRegistrationError.
func ensureAWSKMSSealKey(backend *awskmsbackend.Backend, keyAttrs *xkmstypes.KeyAttributes, keyID string) error {
	metadata, err := json.Marshal(map[string]interface{}{"key_id": keyID})
	if err != nil {
		return &AWSKMSKeyRegistrationError{KeyID: keyID, Cause: err}
	}
	if err := backend.Save(keyAttrs, metadata, xkmstypes.FSExtPrivateBlob, true); err != nil {
		return &AWSKMSKeyRegistrationError{KeyID: keyID, Cause: err}
	}
	return nil
}

// awsKMSSealKeyAttributes builds the xkmstypes.KeyAttributes that identify the
// AWS KMS-managed symmetric key used to seal and unseal the barrier root key.
// The AWS KMS backend resolves the key from CN, so CN carries the operator-
// configured KeyID (key ID, ARN, or alias).
//
// The attributes describe a 256-bit AES-GCM symmetric key resident in the
// AWS KMS store. SymmetricAlgorithm being non-empty is what makes the backend
// treat the request as a symmetric seal (Backend.Seal rejects asymmetric keys);
// KeyType KeyTypeEncryption marks the key's purpose.
func awsKMSSealKeyAttributes(keyID string) *xkmstypes.KeyAttributes {
	return &xkmstypes.KeyAttributes{
		CN:                 keyID,
		StoreType:          xkmstypes.StoreAWSKMS,
		KeyType:            xkmstypes.KeyTypeEncryption,
		KeyAlgorithm:       x509.UnknownPublicKeyAlgorithm,
		SymmetricAlgorithm: xkmstypes.SymmetricAES256GCM,
	}
}

// init registers the AWS KMS strategy factory. Reached only when the binary is
// built with -tags awskms.
func init() {
	registerHardwareStrategyFactory(xkmssdk.StrategyAWSKMS, buildAWSKMSStrategy)
}
