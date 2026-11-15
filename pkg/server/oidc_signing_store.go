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
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"slices"
	"strings"
	"sync"
	"time"

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
	if err := s.useReplicatedOIDCSigningStore(xkmssdk.NewQRDBStorageBackend(shardClient.KV())); err != nil {
		return "", err
	}
	return oidcSigningBackendName, nil
}

// useReplicatedOIDCSigningStore puts the OIDC signing key on the replicated
// shard KV kv: it registers the "oidc-replicated" backend over kv (once per
// process), and sends that backend's key-version records to kv too, so a
// rotation on one node is the current version on every node.
func (s *Server) useReplicatedOIDCSigningStore(kv xkmssdk.StorageBackend) error {
	backend, err := newOIDCSigningServiceBackend(kv)
	if err != nil {
		return err
	}
	versions, err := xkmssdk.NewStoragePrefixBackend(kv, oidcSigningVersionsPrefix)
	if err != nil {
		return err
	}
	if _, err := s.keyVersionStore(); err != nil {
		return err
	}
	if s.xkmsKeyVersionRouter != nil {
		s.xkmsKeyVersionRouter.setReplicated(versions)
	}
	iam.SetRotationLock(oidcSigningBackendName, newOIDCRotationLock(kv))
	if !slices.Contains(dbxkms.Backends(), oidcSigningBackendName) {
		if err := registerXKMSBackendFn(oidcSigningBackendName, backend); err != nil {
			return err
		}
	}
	return nil
}

// oidcSigningVersionsPrefix namespaces the replicated OIDC signing key's
// key-version records on the replicated shard KV.
const oidcSigningVersionsPrefix = "oidc-signing-versions/"

// oidcSigningVersionSegment is the path segment of every key-version record
// of the "oidc-replicated" backend: xkms keys a record as
// {tenant}/key-versions/<backend>/<keyID>.
const oidcSigningVersionSegment = "/key-versions/" + oidcSigningBackendName + "/"

// keyVersionRouter is the storage of the node's key-version records. Every
// record stays on the node-local store, except the "oidc-replicated"
// backend's: once the replicated store is set, those go to it, so every node
// of the cluster reads one record for the one replicated key.
type keyVersionRouter struct {
	local xkmssdk.StorageBackend

	mu         sync.RWMutex
	replicated xkmssdk.StorageBackend
}

var _ xkmssdk.StorageBackend = (*keyVersionRouter)(nil)

func newKeyVersionRouter(local xkmssdk.StorageBackend) *keyVersionRouter {
	return &keyVersionRouter{local: local}
}

// setReplicated sends the "oidc-replicated" records to replicated.
func (r *keyVersionRouter) setReplicated(replicated xkmssdk.StorageBackend) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replicated = replicated
}

func (r *keyVersionRouter) replicatedStore() xkmssdk.StorageBackend {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.replicated
}

// route returns the store the record at key lives on.
func (r *keyVersionRouter) route(key string) xkmssdk.StorageBackend {
	if strings.Contains(key, oidcSigningVersionSegment) {
		if replicated := r.replicatedStore(); replicated != nil {
			return replicated
		}
	}
	return r.local
}

func (r *keyVersionRouter) Get(ctx context.Context, key string) ([]byte, error) {
	return r.route(key).Get(ctx, key)
}

func (r *keyVersionRouter) Put(ctx context.Context, key string, value []byte) error {
	return r.route(key).Put(ctx, key, value)
}

func (r *keyVersionRouter) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return r.route(key).PutIfAbsent(ctx, key, value)
}

func (r *keyVersionRouter) Delete(ctx context.Context, key string) error {
	return r.route(key).Delete(ctx, key)
}

func (r *keyVersionRouter) Exists(ctx context.Context, key string) (bool, error) {
	return r.route(key).Exists(ctx, key)
}

// List lists the keys under prefix on both stores: the node-local records,
// then the replicated ones.
func (r *keyVersionRouter) List(ctx context.Context, prefix string) ([]string, error) {
	keys, err := r.local.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	replicated := r.replicatedStore()
	if replicated == nil {
		return keys, nil
	}
	keys = slices.DeleteFunc(keys, func(k string) bool {
		return strings.Contains(k, oidcSigningVersionSegment)
	})
	more, err := replicated.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	return append(keys, more...), nil
}

// Scan visits the records under prefix on both stores: the node-local
// records, then the replicated ones.
func (r *keyVersionRouter) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	replicated := r.replicatedStore()
	if err := r.local.Scan(ctx, prefix, func(key string, value []byte) error {
		if replicated != nil && strings.Contains(key, oidcSigningVersionSegment) {
			return nil
		}
		return fn(key, value)
	}); err != nil {
		return err
	}
	if replicated == nil {
		return nil
	}
	return replicated.Scan(ctx, prefix, fn)
}

// Close closes the node-local store. The replicated store belongs to the
// shard client and closes with it.
func (r *keyVersionRouter) Close() error {
	return r.local.Close()
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
	if err := m.AdoptDefaultBackendKey(); err != nil {
		return nil, err
	}
	return m, nil
}

// oidcRotationLeasePrefix is the replicated KV prefix of the leases that
// serialize OIDC signing key rotations across the cluster.
const oidcRotationLeasePrefix = "oidc-signing-rotation/"

// oidcRotationLeaseTTL is how long a rotation lease is held before another
// node may take it over, so a node that dies mid-rotation does not block the
// key's rotations for ever.
var oidcRotationLeaseTTL = 30 * time.Second

// oidcRotationLeaseWait is how long a rotation waits for another node's
// rotation of the same key to finish.
var oidcRotationLeaseWait = time.Minute

// oidcRotationLeasePoll is how often a waiting rotation retries the lease.
const oidcRotationLeasePoll = 50 * time.Millisecond

// OIDCRotationBusyError reports a rotation of an OIDC signing key that could
// not start because another node held the key's rotation lease throughout
// the wait.
type OIDCRotationBusyError struct {
	Key  string
	Wait time.Duration
}

// Error implements the error interface.
func (e *OIDCRotationBusyError) Error() string {
	return "server: OIDC signing key rotation lease " + e.Key + " still held after " + e.Wait.String()
}

// newOIDCRotationLock returns the iam.RotationLock of the replicated OIDC
// signing backend: a lease on kv every node shares. A node holds a key's
// lease from reading its version record until the rotated version is
// recorded; a lease past its expiry is taken over.
func newOIDCRotationLock(kv xkmssdk.StorageBackend) iam.RotationLock {
	return func(ctx context.Context, tenantID, cn string) (func(), error) {
		tenant := tenantID
		if tenant == "" {
			tenant = "system"
		}
		key := oidcRotationLeasePrefix + tenant + "/" + cn
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return nil, err
		}
		owner := hex.EncodeToString(nonce)
		deadline := time.Now().Add(oidcRotationLeaseWait)
		for {
			value := []byte(time.Now().Add(oidcRotationLeaseTTL).UTC().Format(time.RFC3339Nano) + " " + owner)
			ok, err := kv.PutIfAbsent(ctx, key, value)
			if err != nil {
				return nil, err
			}
			if ok {
				return func() {
					if held, err := kv.Get(context.Background(), key); err == nil && bytes.Equal(held, value) {
						_ = kv.Delete(context.Background(), key)
					}
				}, nil
			}
			if held, err := kv.Get(ctx, key); err == nil && oidcRotationLeaseExpired(held) {
				if err := kv.Delete(ctx, key); err != nil {
					return nil, err
				}
				continue
			}
			if time.Now().After(deadline) {
				return nil, &OIDCRotationBusyError{Key: key, Wait: oidcRotationLeaseWait}
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(oidcRotationLeasePoll):
			}
		}
	}
}

// oidcRotationLeaseExpired reports whether a stored lease value is past its
// expiry. A value that does not parse is treated as expired, so it cannot
// block rotations.
func oidcRotationLeaseExpired(value []byte) bool {
	expiry, _, _ := strings.Cut(string(value), " ")
	t, err := time.Parse(time.RFC3339Nano, expiry)
	return err != nil || time.Now().After(t)
}
