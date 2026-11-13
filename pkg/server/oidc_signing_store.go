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

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
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
// default backend is used; so it is when the default backend is not software
// (TPM, PKCS#11, cloud KMS), which stays the operator's choice.
func (s *Server) oidcSigningStoreType() (string, error) {
	if s.qrdbClient == nil {
		return dbxkms.DefaultBackendName()
	}
	defaultName, err := dbxkms.DefaultBackendName()
	if err != nil {
		return "", err
	}
	defaultType, err := defaultBackendType()
	if err != nil {
		return "", err
	}
	if name := selectOIDCSigningBackend(defaultType, defaultName); name != oidcSigningBackendName {
		return name, nil
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

// selectOIDCSigningBackend picks the OIDC signing backend from the process
// default backend's type: only a software default moves the key to the
// replicated software store. A TPM, PKCS#11 or cloud-KMS default the
// operator configured stays in use (defaultName), so the key never leaves
// hardware or the cloud KMS for plaintext PKCS#8 in qrdb.
func selectOIDCSigningBackend(defaultType xkmssdk.BackendType, defaultName string) string {
	if defaultType == xkmssdk.BackendTypeSoftware {
		return oidcSigningBackendName
	}
	return defaultName
}

// defaultBackendType returns the key provider type of the process default
// xkms backend.
func defaultBackendType() (xkmssdk.BackendType, error) {
	def, err := xkmssdk.ServiceDefaultBackend()
	if err != nil {
		return "", err
	}
	return def.KeyProvider().Type(), nil
}

// newOIDCSigningKeyManager builds the OIDC signing key manager on storeType.
// When storeType is not the process default backend (the replicated store),
// a pre-upgrade key the default backend already holds is kept as a previous
// key, so tokens it signed keep verifying and nodes not yet upgraded agree
// with this one through the overlap window. The default backend is only
// read; no key is created there.
func newOIDCSigningKeyManager(storeType string) (*iam.SigningKeyManager, error) {
	m, err := iam.NewSigningKeyManagerFromXKMSBackend(iam.OIDCSigningKeyCN, "", storeType)
	if err != nil {
		return nil, err
	}
	defaultName, err := dbxkms.DefaultBackendName()
	if err != nil || defaultName == storeType {
		return m, nil
	}
	if _, err := m.AdoptPreviousFromXKMSBackend(defaultName); err != nil {
		return nil, err
	}
	return m, nil
}
