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
	"context"
	"crypto/x509"
	"errors"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file registers the Google Cloud KMS sealing strategy with the barrier
// composer. It is compiled only under the `gcpkms` build tag, which pulls in
// go-xkms/pkg/backend/gcpkms (cloud.google.com/go/kms). When the tag is absent
// the factory is not registered and buildHardwareSealStrategies returns a
// StrategyNotCompiledError instructing the operator to rebuild with -tags
// gcpkms.
//
// QuicBaaS composes go-xkms: a gcpkmsbackend.Backend (which implements
// xkmstypes.Sealer) is constructed from the operator config and wrapped by
// xkmssdk.NewCloudKMSStrategy. No KMS crypto is reimplemented here — the
// plaintext root key is sealed by the Cloud KMS Encrypt API and never leaves
// KMS in plaintext form.

// GCPKMSConfigError reports that the operator's GCP KMS seal configuration is
// missing a required field. It is returned before any backend construction is
// attempted so the operator gets a precise, library-independent diagnostic.
// Field names the missing configuration key.
type GCPKMSConfigError struct {
	Field string
}

// Error implements the error interface.
func (e *GCPKMSConfigError) Error() string {
	return "seal strategy: gcpkms config missing required field: " + e.Field
}

// GCPKMSKeyRegistrationError reports that the factory could not ensure the
// operator's Cloud KMS crypto key is usable by the backend. KeyID names the
// configured crypto key; Cause carries the underlying go-xkms failure.
type GCPKMSKeyRegistrationError struct {
	KeyID string
	Cause error
}

// Error implements the error interface.
func (e *GCPKMSKeyRegistrationError) Error() string {
	return "seal strategy: gcpkms sealing key registration failed for key " + e.KeyID + ": " + e.Cause.Error()
}

// Unwrap exposes the underlying cause for errors.Is / errors.As.
func (e *GCPKMSKeyRegistrationError) Unwrap() error { return e.Cause }

// buildGCPKMSStrategy constructs the GCP KMS sealing strategy from
// cfg.Seal.GCPKMS.
//
// It validates the operator config up front and surfaces any missing required
// field as a *StrategyBuildError wrapping a *GCPKMSConfigError, so the failure
// is distinguishable from a backend-construction failure. The required fields
// are:
//
//   - ProjectID: the GCP project hosting the key ring.
//   - LocationID: the Cloud KMS location.
//   - KeyRingID: the Cloud KMS key ring name.
//   - KeyID: the Cloud KMS crypto key name within the key ring.
//
// On success it builds a gcpkmsbackend.Backend from the config (with in-memory
// metadata stores — the actual key material stays in Cloud KMS), ensures the
// operator's crypto key is resolvable by the backend (see ensureGCPKMSSealKey),
// and wraps the backend plus the xkmstypes.KeyAttributes that identify the
// KMS-managed AES-256-GCM crypto key by its configured CN (the operator's
// KeyID) in xkmssdk.NewCloudKMSStrategy.
//
// gcpkmsbackend.NewBackend takes a context because it dials the Cloud KMS gRPC
// client at construction time. Barrier strategy construction happens during
// server startup, so context.Background() is the appropriate root context.
//
// The optional hardware SymmetricEncrypter is left nil: barrier data
// encryption then derives a software DEK from the KMS-protected root key,
// keeping the root key cloud-bound while avoiding a KMS round-trip per data
// operation.
func buildGCPKMSStrategy(cfg *config.Config) (qrdbsdk.SealingStrategy, error) {
	gcp := cfg.Seal.GCPKMS

	if gcp.ProjectID == "" {
		return nil, &StrategyBuildError{Backend: "gcpkms", Cause: &GCPKMSConfigError{Field: "project_id"}}
	}
	if gcp.LocationID == "" {
		return nil, &StrategyBuildError{Backend: "gcpkms", Cause: &GCPKMSConfigError{Field: "location_id"}}
	}
	if gcp.KeyRingID == "" {
		return nil, &StrategyBuildError{Backend: "gcpkms", Cause: &GCPKMSConfigError{Field: "key_ring_id"}}
	}
	if gcp.KeyID == "" {
		return nil, &StrategyBuildError{Backend: "gcpkms", Cause: &GCPKMSConfigError{Field: "key_id"}}
	}

	backendCfg := &xkmssdk.GCPKMSBackendConfig{
		ProjectID:       gcp.ProjectID,
		LocationID:      gcp.LocationID,
		KeyRingID:       gcp.KeyRingID,
		CredentialsFile: gcp.CredentialsFile,
		Endpoint:        gcp.Endpoint,
		KeyStorage:      xkmssdk.NewStorageMemory(),
		CertStorage:     xkmssdk.NewStorageMemory(),
	}

	backend, err := xkmssdk.NewGCPKMSBackend(context.Background(), backendCfg)
	if err != nil {
		return nil, &StrategyBuildError{Backend: "gcpkms", Cause: err}
	}

	keyAttrs := gcpKMSSealKeyAttributes(gcp.KeyID)

	if err := ensureGCPKMSSealKey(backend, keyAttrs); err != nil {
		return nil, &StrategyBuildError{Backend: "gcpkms", Cause: err}
	}

	return xkmssdk.NewCloudKMSStrategy(xkmssdk.StrategyGCPKMS, backend, keyAttrs, nil), nil
}

// ensureGCPKMSSealKey ensures the operator's Cloud KMS crypto key is resolvable
// by the backend so the sealer's SymmetricEncrypter can find it.
//
// The GCP KMS backend resolves a symmetric key by a live GetCryptoKey lookup
// against the configured project / location / key ring, so the crypto key must
// exist in Cloud KMS. The operator normally pre-provisions it. This routine
// performs a check-and-skip: it first calls GetSymmetricKey, and only if the
// key is genuinely absent (backend.ErrKeyNotFound) does it create it via
// GenerateSymmetricKey, which a fresh deployment needs.
//
// Idempotency: an existing key is detected by GetSymmetricKey and reused
// unchanged — the factory never rotates or destroys it, so previously-sealed
// data unseals after a restart. Only the not-found path creates a key. Any
// other GetSymmetricKey error (or a creation failure) is wrapped in
// *GCPKMSKeyRegistrationError.
func ensureGCPKMSSealKey(b *xkmssdk.GCPKMSBackend, keyAttrs *xkmssdk.KeyAttributes) error {
	if _, err := b.GetSymmetricKey(context.Background(), keyAttrs); err == nil {
		return nil
	} else if !errors.Is(err, xkmssdk.ErrBackendKeyNotFound) {
		return &GCPKMSKeyRegistrationError{KeyID: keyAttrs.CN, Cause: err}
	}

	if _, err := b.GenerateSymmetricKey(context.Background(), keyAttrs); err != nil {
		return &GCPKMSKeyRegistrationError{KeyID: keyAttrs.CN, Cause: err}
	}
	return nil
}

// gcpKMSSealKeyAttributes builds the xkmstypes.KeyAttributes that identify the
// GCP KMS-managed symmetric crypto key used to seal and unseal the barrier root
// key. The GCP KMS backend resolves the crypto key from CN against the
// project / location / key ring configured on the backend, so CN carries the
// operator-configured KeyID (the crypto key name).
//
// The attributes describe a 256-bit AES-GCM symmetric key resident in the
// GCP KMS store. SymmetricAlgorithm being non-empty is what makes the backend
// treat the request as a symmetric seal (Backend.Seal rejects asymmetric keys);
// KeyType KeyTypeEncryption marks the key's purpose.
func gcpKMSSealKeyAttributes(keyID string) *xkmssdk.KeyAttributes {
	return &xkmssdk.KeyAttributes{
		CN:                 keyID,
		StoreType:          xkmssdk.StoreGCPKMS,
		KeyType:            xkmssdk.KeyTypeEncryption,
		KeyAlgorithm:       x509.UnknownPublicKeyAlgorithm,
		SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM,
	}
}

// init registers the GCP KMS strategy factory. Reached only when the binary is
// built with -tags gcpkms.
func init() {
	registerHardwareStrategyFactory(xkmssdk.StrategyGCPKMS, buildGCPKMSStrategy)
}
