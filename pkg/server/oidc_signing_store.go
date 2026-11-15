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
	"context"
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
	dbxkms "github.com/jeremyhahn/go-quicbaas/pkg/xkms"
)

// oidcSigningBackendName is the xkms backend that holds the OIDC (platform
// token) signing keys on the replicated datastore.
const oidcSigningBackendName = "oidc-replicated"

// newOIDCSigningServiceBackend builds the xkms software backend for the OIDC
// signing keys over kv: keys and certificates under their own prefixes.
func newOIDCSigningServiceBackend(kv xkmssdk.StorageBackend) (xkmssdk.ServiceBackend, error) {
	keys, err := newOIDCKeyMaterialStore(kv)
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

// oidcSigningKeysPrefix is the prefix of the replicated kv under which the
// software backend of oidc-replicated keeps its key material.
const oidcSigningKeysPrefix = "oidc-signing-keys/"

// newOIDCKeyMaterialStore returns the key storage of oidc-replicated: the
// oidcSigningKeysPrefix of kv, with every write of key material gated by the
// rotation fence the context carries, whichever entry point started it.
func newOIDCKeyMaterialStore(kv xkmssdk.StorageBackend) (*oidcKeyMaterialStore, error) {
	keys, err := xkmssdk.NewStoragePrefixBackend(kv, oidcSigningKeysPrefix)
	if err != nil {
		return nil, err
	}
	guarded, err := guardedOIDCStore(keys)
	if err != nil {
		return nil, err
	}
	return &oidcKeyMaterialStore{oidcGuardedKV: guarded}, nil
}

// oidcKeyMaterialStore is the key storage of oidc-replicated. Without the
// rotation fence in the context only the first creation of the signing key
// passes: a Put or PutIfAbsent of a name that is absent and is not a
// rotation version. A Put over existing material, any write of a version
// ("K--v<n>"), and a Delete are refused with *OIDCRotationUnfencedError.
type oidcKeyMaterialStore struct {
	oidcGuardedKV
}

// oidcKeyVersionName matches a storage name that holds a rotation version of
// a key, "K--v<n>", which only a fenced rotation creates.
var oidcKeyVersionName = regexp.MustCompile(`--v[0-9]+`)

// Put writes key material. Under the rotation fence it writes from the bytes
// this holder observed or wrote under key in this rotation (CompareAndSwap),
// or creates an absent key (PutIfAbsent); material that changed meanwhile is
// left as it is and *OIDCRotationFencedError returned. Without the fence it
// passes only as the first creation of a key that is not a rotation version.
func (s *oidcKeyMaterialStore) Put(ctx context.Context, key string, value []byte) error {
	if fence, ok := oidcFenceFrom(ctx); ok {
		return fence.putMaterial(ctx, s.oidcGuardedKV, key, value)
	}
	if oidcKeyVersionName.MatchString(key) {
		return &OIDCRotationUnfencedError{Key: key, Op: "put"}
	}
	created, err := s.oidcGuardedKV.PutIfAbsent(ctx, key, value)
	if err != nil {
		return err
	}
	if !created {
		return &OIDCRotationUnfencedError{Key: key, Op: "put"}
	}
	return nil
}

// PutIfAbsent creates key material; without the rotation fence a rotation
// version is refused.
func (s *oidcKeyMaterialStore) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	if _, ok := oidcFenceFrom(ctx); !ok && oidcKeyVersionName.MatchString(key) {
		return false, &OIDCRotationUnfencedError{Key: key, Op: "put"}
	}
	return s.oidcGuardedKV.PutIfAbsent(ctx, key, value)
}

// Delete removes key material under the rotation fence, and only the bytes
// this holder observed or wrote under key in this rotation
// (CompareAndDelete): a stale holder's cleanup cannot remove a key a peer
// has written since. Material the holder neither observed nor wrote, or that
// changed meanwhile, is left as it is and *OIDCRotationFencedError returned.
func (s *oidcKeyMaterialStore) Delete(ctx context.Context, key string) error {
	fence, ok := oidcFenceFrom(ctx)
	if !ok {
		return &OIDCRotationUnfencedError{Key: key, Op: "delete"}
	}
	return fence.deleteMaterial(ctx, s.oidcGuardedKV, key)
}

// Get reads key material; under the rotation fence the bytes read are what
// a later fenced write or delete of key starts from.
func (s *oidcKeyMaterialStore) Get(ctx context.Context, key string) ([]byte, error) {
	value, err := s.oidcGuardedKV.Get(ctx, key)
	if fence, ok := oidcFenceFrom(ctx); ok && err == nil {
		fence.observeMaterial(key, value)
	}
	return value, err
}

// List lists key material; under the rotation fence each listed key's bytes
// are observed, so the rotation that finds an interrupted rotation's key can
// remove exactly what it found.
func (s *oidcKeyMaterialStore) List(ctx context.Context, prefix string) ([]string, error) {
	keys, err := s.oidcGuardedKV.List(ctx, prefix)
	if err != nil {
		return nil, err
	}
	if fence, ok := oidcFenceFrom(ctx); ok {
		for _, key := range keys {
			if value, getErr := s.oidcGuardedKV.Get(ctx, key); getErr == nil {
				fence.observeMaterial(key, value)
			}
		}
	}
	return keys, nil
}

// Scan visits key material; under the rotation fence each visited value is
// observed.
func (s *oidcKeyMaterialStore) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	fence, ok := oidcFenceFrom(ctx)
	if !ok {
		return s.oidcGuardedKV.Scan(ctx, prefix, fn)
	}
	return s.oidcGuardedKV.Scan(ctx, prefix, func(key string, value []byte) error {
		fence.observeMaterial(key, value)
		return fn(key, value)
	})
}

// oidcFenceFrom returns the rotation fence ctx carries, if any.
func oidcFenceFrom(ctx context.Context) (*oidcVersionFence, bool) {
	fence, ok := ctx.Value(oidcVersionFenceKey{}).(*oidcVersionFence)
	return fence, ok && fence != nil
}

// OIDCRotationUnfencedError reports a write of oidc-replicated rotation state
// (the version record or key material) made without the rotation lease's
// fence in its context: an entry point that did not take the lease, such as
// a go-xkms rotate route, cannot change the shared signing key.
type OIDCRotationUnfencedError struct {
	Key string
	Op  string
}

// Error describes the refused write.
func (e *OIDCRotationUnfencedError) Error() string {
	return "server: " + e.Op + " of oidc signing key state " + e.Key +
		" refused: it is not made under the rotation lease; rotate the OIDC signing key through the signing key manager"
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
	guarded, err := guardedOIDCStore(kv)
	if err != nil {
		return err
	}
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
	guardedVersions, err := guardedOIDCStore(versions)
	if err != nil {
		return err
	}
	iam.SetRotationLock(oidcSigningBackendName, newOIDCRotationLock(guarded, guardedVersions))
	iam.SetAdoptionCutoffs(oidcSigningBackendName, &oidcAdoptionCutoffs{
		store:    guarded,
		lifetime: oidcSignedTokenLifetime(&s.config.Auth),
		now:      oidcAdoptionNow,
	})
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
	if fence, ok := oidcFenceFrom(ctx); ok && fence.key == key {
		return fence.commit(ctx, value)
	}
	if r.replicatedRecord(key) {
		return &OIDCRotationUnfencedError{Key: key, Op: "put"}
	}
	return r.route(key).Put(ctx, key, value)
}

// replicatedRecord reports whether key is an oidc-replicated version record
// kept in the replicated store, which only the rotation fence may change.
func (r *keyVersionRouter) replicatedRecord(key string) bool {
	return strings.Contains(key, oidcSigningVersionSegment) && r.replicatedStore() != nil
}

func (r *keyVersionRouter) PutIfAbsent(ctx context.Context, key string, value []byte) (bool, error) {
	return r.route(key).PutIfAbsent(ctx, key, value)
}

func (r *keyVersionRouter) Delete(ctx context.Context, key string) error {
	if fence, ok := oidcFenceFrom(ctx); r.replicatedRecord(key) && (!ok || fence.key != key) {
		return &OIDCRotationUnfencedError{Key: key, Op: "delete"}
	}
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

// oidcAdoptionCutoffPrefix is the replicated KV prefix of the cutoffs after
// which an adopted pre-upgrade OIDC signing key stops verifying.
const oidcAdoptionCutoffPrefix = "oidc-signing-adoption/"

// oidcAdoptionNow is the clock adoption cutoffs are recorded and checked
// against.
var oidcAdoptionNow = time.Now

// oidcSignedTokenLifetime is the longest lifetime of any token the OIDC
// signing key signs: the access token (the authorization-code, refresh and
// service-account grants all mint it with the access TTL) and the ID token.
// The refresh token is an opaque stored credential, not a signed JWT, and
// the provider signs no logout token.
func oidcSignedTokenLifetime(auth *config.AuthConfig) time.Duration {
	return max(auth.OIDCAccessTokenTTL(), auth.OIDCIDTokenTTL())
}

// oidcAdoptionCutoffs is the iam.AdoptionCutoffs of the replicated OIDC
// signing store: one create-if-absent record per adopted key in the
// replicated KV, so every node ends the key's trust at the same instant.
type oidcAdoptionCutoffs struct {
	store    oidcGuardedKV
	lifetime time.Duration
	now      func() time.Time
}

// Cutoff implements iam.AdoptionCutoffs.
func (c *oidcAdoptionCutoffs) Cutoff(ctx context.Context, tenantID, cn, kid string) (time.Time, error) {
	tenant := tenantID
	if tenant == "" {
		tenant = "system"
	}
	key := oidcAdoptionCutoffPrefix + tenant + "/" + cn + "/" + kid
	proposed := c.now().Add(c.lifetime).UTC().Format(time.RFC3339Nano)
	if _, err := c.store.PutIfAbsent(ctx, key, []byte(proposed)); err != nil {
		return time.Time{}, err
	}
	recorded, err := c.store.Get(ctx, key)
	if err != nil {
		return time.Time{}, err
	}
	until, err := time.Parse(time.RFC3339Nano, string(recorded))
	if err != nil {
		return time.Time{}, &OIDCAdoptionCutoffRecordError{Key: key, Cause: err}
	}
	return until, nil
}

// Now implements iam.AdoptionCutoffs.
func (c *oidcAdoptionCutoffs) Now() time.Time {
	return c.now()
}

// OIDCAdoptionCutoffRecordError reports an adoption cutoff record in the
// replicated OIDC signing store that does not parse as a time.
type OIDCAdoptionCutoffRecordError struct {
	Key   string
	Cause error
}

// Error implements error.
func (e *OIDCAdoptionCutoffRecordError) Error() string {
	return "server: oidc signing key adoption cutoff " + e.Key + " is not a time: " + e.Cause.Error()
}

// Unwrap returns the parse error.
func (e *OIDCAdoptionCutoffRecordError) Unwrap() error {
	return e.Cause
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
// oidcGuardedKV is a replicated store with the guarded writes the rotation
// lease and the fenced version-record commit rely on: a delete and a swap
// conditioned on the value the caller last observed, evaluated where the
// store serialises writes.
type oidcGuardedKV interface {
	xkmssdk.StorageBackend
	CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error)
	CompareAndSwap(ctx context.Context, key string, expected, newValue []byte) (bool, []byte, error)
}

// OIDCSigningStoreGuardedWritesError reports a replicated OIDC signing store
// that lacks a guarded write the cluster rotation needs. Without it a node
// could remove a lease or overwrite a version record another node has just
// written, so the store is refused rather than used unguarded.
type OIDCSigningStoreGuardedWritesError struct {
	// Missing names the verb the store does not offer.
	Missing string
}

// Error implements error.
func (e *OIDCSigningStoreGuardedWritesError) Error() string {
	return "server: replicated oidc signing store lacks " + e.Missing +
		"; the rotation lease and the version-record commit need guarded writes"
}

// guardedOIDCStore returns kv as an oidcGuardedKV, or
// *OIDCSigningStoreGuardedWritesError naming the first verb it lacks.
func guardedOIDCStore(kv xkmssdk.StorageBackend) (oidcGuardedKV, error) {
	if _, ok := kv.(interface {
		CompareAndDelete(ctx context.Context, key string, expected []byte) (bool, error)
	}); !ok {
		return nil, &OIDCSigningStoreGuardedWritesError{Missing: "CompareAndDelete"}
	}
	guarded, ok := kv.(oidcGuardedKV)
	if !ok {
		return nil, &OIDCSigningStoreGuardedWritesError{Missing: "CompareAndSwap"}
	}
	return guarded, nil
}

func newOIDCRotationLock(kv, versions oidcGuardedKV) iam.RotationLock {
	return func(ctx context.Context, tenantID, cn string) (context.Context, func(), error) {
		tenant := tenantID
		if tenant == "" {
			tenant = "system"
		}
		key := oidcRotationLeasePrefix + tenant + "/" + cn
		nonce := make([]byte, 16)
		if _, err := rand.Read(nonce); err != nil {
			return nil, nil, err
		}
		owner := hex.EncodeToString(nonce)
		deadline := time.Now().Add(oidcRotationLeaseWait)
		ttl := oidcRotationLeaseTTL
		for {
			value := oidcRotationLeaseValue(owner, ttl)
			ok, err := kv.PutIfAbsent(ctx, key, value)
			if err != nil {
				return nil, nil, err
			}
			if ok {
				return holdOIDCRotationLease(ctx, kv, versions, key, tenant+oidcSigningVersionSegment+cn, owner, value, ttl)
			}
			// An expired lease is removed only while it still holds the
			// value observed here, so of two nodes taking over the same
			// lease the second cannot remove the lease the first has just
			// taken; either way the loop retries the create.
			if held, err := kv.Get(ctx, key); err == nil && oidcRotationLeaseExpired(held) {
				if _, err := kv.CompareAndDelete(ctx, key, held); err != nil {
					return nil, nil, err
				}
				continue
			}
			if time.Now().After(deadline) {
				return nil, nil, &OIDCRotationBusyError{Key: key, Wait: oidcRotationLeaseWait}
			}
			select {
			case <-ctx.Done():
				return nil, nil, ctx.Err()
			case <-time.After(oidcRotationLeasePoll):
			}
		}
	}
}

// oidcRotationLeaseValue is a lease value for owner that expires ttl from
// now.
func oidcRotationLeaseValue(owner string, ttl time.Duration) []byte {
	return []byte(time.Now().Add(ttl).UTC().Format(time.RFC3339Nano) + " " + owner)
}

// oidcRotationLeaseAcquiredHook runs once a lease is taken and the version
// record it fences has been read; tests use it to pause a holder.
var oidcRotationLeaseAcquiredHook = func() {}

// oidcRotationLeaseRenewHook runs before each renewal of a held lease; tests
// use it to stop a holder's renewals.
var oidcRotationLeaseRenewHook = func(context.Context) {}

// holdOIDCRotationLease starts holding the lease just taken at key with
// value. It reads the version record at record, the value the holder's
// commit must still find, and returns a context derived from ctx that
// carries the commit fence and is canceled when a renewal fails or is
// refused, with the release that stops the renewals and removes this
// holder's own lease value only.
func holdOIDCRotationLease(ctx context.Context, kv, versions oidcGuardedKV, key, record, owner string, value []byte, ttl time.Duration) (context.Context, func(), error) {
	fence := &oidcVersionFence{key: record, store: versions}
	current, err := versions.Get(ctx, record)
	if err == nil {
		fence.expected, fence.present = current, true
	} else if exists, existsErr := versions.Exists(ctx, record); existsErr != nil || exists {
		_, _ = kv.CompareAndDelete(context.Background(), key, value)
		if existsErr != nil {
			return nil, nil, existsErr
		}
		return nil, nil, err
	}
	oidcRotationLeaseAcquiredHook()

	leaseCtx, cancel := context.WithCancel(context.WithValue(ctx, oidcVersionFenceKey{}, fence))
	var (
		mu      sync.Mutex
		held    = value
		stopped bool
		done    = make(chan struct{})
	)
	go func() {
		ticker := time.NewTicker(ttl / 3)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-leaseCtx.Done():
				return
			case <-ticker.C:
			}
			oidcRotationLeaseRenewHook(leaseCtx)
			mu.Lock()
			if stopped {
				mu.Unlock()
				return
			}
			next := oidcRotationLeaseValue(owner, ttl)
			renewCtx, cancelRenew := context.WithTimeout(leaseCtx, ttl/3)
			swapped, _, err := kv.CompareAndSwap(renewCtx, key, held, next)
			cancelRenew()
			if err != nil || !swapped {
				cancel()
				mu.Unlock()
				return
			}
			held = next
			mu.Unlock()
		}
	}()
	var once sync.Once
	unlock := func() {
		once.Do(func() {
			mu.Lock()
			defer mu.Unlock()
			stopped = true
			close(done)
			cancel()
			_, _ = kv.CompareAndDelete(context.Background(), key, held)
		})
	}
	return leaseCtx, unlock, nil
}

// oidcVersionFenceKey is the context key of a held lease's commit fence.
type oidcVersionFenceKey struct{}

// oidcVersionFence fences the commit of one version record: while its lease
// is held, a write of the record succeeds only from the value the holder
// last read or wrote.
type oidcVersionFence struct {
	key      string
	store    oidcGuardedKV
	mu       sync.Mutex
	expected []byte
	present  bool

	// materialMu guards material: per key-material name, the bytes this
	// holder observed or wrote under it in this rotation.
	materialMu sync.Mutex
	material   map[string]oidcMaterial
}

// oidcMaterial is what one rotation knows of one key-material name: the
// bytes it holds, whether it is present, and whether this holder wrote it.
// A name the holder wrote is not re-observed: what it wrote is what its
// cleanup may remove.
type oidcMaterial struct {
	value   []byte
	present bool
	written bool
}

// observeMaterial records value as read under key, unless this holder wrote
// key in this rotation.
func (f *oidcVersionFence) observeMaterial(key string, value []byte) {
	f.materialMu.Lock()
	defer f.materialMu.Unlock()
	if f.material == nil {
		f.material = make(map[string]oidcMaterial)
	}
	if f.material[key].written {
		return
	}
	f.material[key] = oidcMaterial{value: slices.Clone(value), present: true}
}

// putMaterial writes value under key from what this holder knows of key: a
// CompareAndSwap from the bytes it observed or wrote, or a PutIfAbsent.
func (f *oidcVersionFence) putMaterial(ctx context.Context, kv oidcGuardedKV, key string, value []byte) error {
	f.materialMu.Lock()
	defer f.materialMu.Unlock()
	known := f.material[key]
	var (
		ok  bool
		err error
	)
	if known.present {
		ok, _, err = kv.CompareAndSwap(ctx, key, known.value, value)
	} else {
		ok, err = kv.PutIfAbsent(ctx, key, value)
	}
	if err != nil {
		return err
	}
	if !ok {
		return &OIDCRotationFencedError{Key: key}
	}
	if f.material == nil {
		f.material = make(map[string]oidcMaterial)
	}
	f.material[key] = oidcMaterial{value: slices.Clone(value), present: true, written: true}
	return nil
}

// deleteMaterial removes key only while it holds the bytes this holder
// observed or wrote under it (CompareAndDelete).
func (f *oidcVersionFence) deleteMaterial(ctx context.Context, kv oidcGuardedKV, key string) error {
	f.materialMu.Lock()
	defer f.materialMu.Unlock()
	known := f.material[key]
	if !known.present {
		return &OIDCRotationFencedError{Key: key}
	}
	ok, err := kv.CompareAndDelete(ctx, key, known.value)
	if err != nil {
		return err
	}
	if !ok {
		return &OIDCRotationFencedError{Key: key}
	}
	f.material[key] = oidcMaterial{written: known.written}
	return nil
}

// commit writes value to the record from the expected value and advances the
// expected value; a record another holder changed is left as it is.
func (f *oidcVersionFence) commit(ctx context.Context, value []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	var (
		ok  bool
		err error
	)
	if f.present {
		ok, _, err = f.store.CompareAndSwap(ctx, f.key, f.expected, value)
	} else {
		ok, err = f.store.PutIfAbsent(ctx, f.key, value)
	}
	if err != nil {
		return err
	}
	if !ok {
		return &OIDCRotationFencedError{Key: f.key}
	}
	f.expected, f.present = slices.Clone(value), true
	return nil
}

// OIDCRotationFencedError reports a write under the rotation lease refused
// because the state it would change is not what its holder saw: the version
// record, or key material, changed after the holder read or wrote it (the
// lease expired and another node rotated the key), or the holder deletes key
// material it never saw. The stale holder changes nothing.
type OIDCRotationFencedError struct {
	Key string
}

// Error describes the fenced commit.
func (e *OIDCRotationFencedError) Error() string {
	return "server: oidc signing key state " + e.Key +
		" changed since the rotation lease was taken, or was never seen under it; the holder's write is refused"
}

// oidcRotationLeaseExpired reports whether a stored lease value is past its
// expiry. A value that does not parse is treated as expired, so it cannot
// block rotations.
func oidcRotationLeaseExpired(value []byte) bool {
	expiry, _, _ := strings.Cut(string(value), " ")
	t, err := time.Parse(time.RFC3339Nano, expiry)
	return err != nil || time.Now().After(t)
}
