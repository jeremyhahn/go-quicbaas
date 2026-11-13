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

package server

import (
	"slices"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// oidcSigningBackendName is the xkms backend that holds the OIDC (platform
// token) signing keys on the replicated datastore.
const oidcSigningBackendName = "oidc-replicated"

// newOIDCSigningServiceBackend builds the xkms software backend for the OIDC
// signing keys over kv: keys and certificates under their own prefixes.
func newOIDCSigningServiceBackend(kv xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
	keys, err := xkmssdk.NewStoragePrefixBackend(kv, "oidc-signing-keys/")
	if err != nil {
		return nil, err
	}
	certs, err := xkmssdk.NewStoragePrefixBackend(kv, "oidc-signing-certs/")
	if err != nil {
		return nil, err
	}
	sw, err := xkmssdk.NewSoftwareBackend(&xkmssdk.SoftwareBackendConfig{KeyStorage: keys})
	if err != nil {
		return nil, err
	}
	return xkmssdk.NewServiceBackend(&xkmssdk.ServiceBackendConfig{Backend: sw, CertStorage: certs})
}

// oidcSigningStoreType returns the xkms backend name the OIDC signing key
// manager uses. On a node with a qrdb client the key lives in the replicated,
// barrier-encrypted datastore (the cluster CA shard), so every node signs and
// verifies platform tokens with one key and one kid; there is no node-local
// copy. Without a qrdb client there is nothing to share, and the process
// default backend is used.
func (s *Server) oidcSigningStoreType() (string, error) {
	if s.qrdbClient == nil {
		return dbxkms.DefaultBackendName()
	}
	shardClient, err := s.qrdbClient.ForShard(clusterCAShardID)
	if err != nil {
		return "", err
	}
	backend, err := newOIDCSigningServiceBackend(xkmssdk.NewQRDBStorageBackend(shardClient.KV()))
	if err != nil {
		return "", err
	}
	if !slices.Contains(dbxkms.Backends(), oidcSigningBackendName) {
		if err := registerXKMSBackendFn(oidcSigningBackendName, backend); err != nil {
			return "", err
		}
	}
	return oidcSigningBackendName, nil
}
