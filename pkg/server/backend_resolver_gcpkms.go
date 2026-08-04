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

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// This file registers the Google Cloud KMS backend factory. It is compiled only
// under the `gcpkms` build tag. When the tag is absent the factory is
// unregistered and the resolver returns BackendNotCompiledError. The plaintext
// key never leaves GCP KMS; nothing is reimplemented here.

// buildGCPKMSServiceBackend constructs the GCP KMS ServiceBackend from
// cfg.Seal.GCPKMS. keyStore is the caller's persistence seam: although the
// plaintext key never leaves GCP KMS, the backend's key-metadata store is backed
// by keyStore so per-key references survive restarts.
func buildGCPKMSServiceBackend(cfg *config.Config, spec BackendSpec, keyStore xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
	gcp := cfg.Seal.GCPKMS
	backend, err := xkmssdk.NewGCPKMSBackend(context.Background(), &xkmssdk.GCPKMSBackendConfig{
		ProjectID:       gcp.ProjectID,
		LocationID:      gcp.LocationID,
		KeyRingID:       gcp.KeyRingID,
		CredentialsFile: gcp.CredentialsFile,
		Endpoint:        gcp.Endpoint,
		KeyStorage:      keyStore,
		CertStorage:     xkmssdk.NewStorageMemory(),
	})
	if err != nil {
		return nil, &BackendConstructError{Backend: config.BackendTypeGCPKMS, Cause: err}
	}
	return wrapServiceBackend(config.BackendTypeGCPKMS, backend)
}

// init registers the GCP KMS factory. Reached only under -tags gcpkms.
func init() {
	registerBackendFactory(config.BackendTypeGCPKMS, buildGCPKMSServiceBackend)
}
