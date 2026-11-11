// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

// Package kmstenant builds the real KMS service over an in-memory key
// manager, with one key owned by OwnerTenant, so a transport test can
// prove that OtherTenant asking for that key gets not-found (#1476).
package kmstenant

import (
	"bytes"
	"context"
	"crypto"
	"crypto/elliptic"
	"crypto/x509"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/auth"
	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/tenantctx"
)

const (
	// OwnerTenant owns KeyName.
	OwnerTenant = "tenant-a"
	// OtherTenant owns no key.
	OtherTenant = "tenant-b"
	// KeyName is the name OwnerTenant gave its symmetric key.
	KeyName = "scope-key"
	// SigningKeyName is the name OwnerTenant gave its asymmetric key.
	SigningKeyName = "scope-signing-key"
	// DEKPath is the path of OwnerTenant's data encryption key.
	DEKPath = "scope/dek"
	// MissingID names no key and no DEK in any tenant.
	MissingID = "missing-id"
)

// NewService builds the real KMS service and creates KeyName,
// SigningKeyName and DEKPath as OwnerTenant.
func NewService(t *testing.T) service.KMSService {
	t.Helper()
	// Key policies fail closed without a store, so every shard reads and
	// writes one in-memory store.
	store := qrdbsdk.NewMemoryKVStore()
	svc, err := service.NewServiceBuilder().
		WithConfig(&service.ServiceConfig{NodeID: 1, RequestTimeout: 5 * time.Second}).
		WithRaftNode(&unusedRaftNode{}).
		WithKMSManager(NewManager()).
		WithKVStoreProvider(func(uint64) interface{} { return store }).
		Build()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, svc.Close()) })

	kmsSvc := svc.KMS()
	ownerCtx := tenantctx.WithTenant(context.Background(), OwnerTenant)
	_, err = kmsSvc.GenerateKey(ownerCtx, &service.GenerateKeyRequest{
		Name:          KeyName,
		KeyAttributes: &xkmssdk.KeyAttributes{CN: KeyName, SymmetricAlgorithm: xkmssdk.SymmetricAES256GCM},
		Usage:         []service.KeyUsage{service.KeyUsageEncrypt, service.KeyUsageDecrypt},
	})
	require.NoError(t, err)
	_, err = kmsSvc.GenerateKey(ownerCtx, &service.GenerateKeyRequest{
		Name: SigningKeyName,
		KeyAttributes: &xkmssdk.KeyAttributes{
			CN:            SigningKeyName,
			KeyType:       xkmssdk.KeyTypeSigning,
			KeyAlgorithm:  x509.ECDSA,
			ECCAttributes: &xkmssdk.ECCAttributes{Curve: elliptic.P256()},
		},
		Usage: []service.KeyUsage{service.KeyUsageSign, service.KeyUsageVerify},
	})
	require.NoError(t, err)
	_, err = kmsSvc.CreateDEK(ownerCtx, &service.CreateDEKRequest{Path: DEKPath, Algorithm: service.KeyAlgAES256GCM})
	require.NoError(t, err)
	return kmsSvc
}

// OwnerState is what OwnerTenant reads back of its keys, their policies
// and its DEK. A cross-tenant call must leave it unchanged.
type OwnerState struct {
	Keys             map[string]*service.KeyMetadata
	Policies         map[string]*service.KeyPolicy
	RotationPolicies map[string]*service.RotationPolicy
	DEK              *service.DEKMetadata
}

// ReadOwnerState reads OwnerState through svc as OwnerTenant. It is
// ground-truth inspection of stored state, not a tenant caller's request,
// so it reads under the system context, which bypasses the per-key
// authorization gate.
func ReadOwnerState(t *testing.T, svc service.KMSService) OwnerState {
	t.Helper()
	ctx := auth.NewSystemContext(tenantctx.WithTenant(context.Background(), OwnerTenant))
	state := OwnerState{
		Keys:             make(map[string]*service.KeyMetadata),
		Policies:         make(map[string]*service.KeyPolicy),
		RotationPolicies: make(map[string]*service.RotationPolicy),
	}
	for _, name := range []string{KeyName, SigningKeyName} {
		key, err := svc.GetKey(ctx, &service.GetKeyRequest{KeyID: name})
		require.NoError(t, err)
		state.Keys[name] = key.Metadata
		policy, err := svc.GetKeyPolicy(ctx, &service.GetKeyPolicyRequest{KeyID: name})
		require.NoError(t, err)
		// A key with no stored policy reads back a default policy stamped
		// with the read time; the stamp is not state.
		stored := *policy.Policy
		stored.UpdatedAt = time.Time{}
		state.Policies[name] = &stored
		rotation, err := svc.GetKeyRotationPolicy(ctx, &service.GetKeyRotationPolicyRequest{KeyID: name})
		require.NoError(t, err)
		state.RotationPolicies[name] = rotation.Policy
	}
	dek, err := svc.GetDEK(ctx, &service.GetDEKRequest{Path: DEKPath})
	require.NoError(t, err)
	state.DEK = dek.DEK
	return state
}

// unusedRaftNode satisfies the builder's raft node requirement; the KMS
// verbs never reach it.
type unusedRaftNode struct {
	interfaces.RaftNode
}

// Manager is an in-memory interfaces.KMSManager that answers an unknown
// key id with kms.ErrKeyNotFound, as the production manager does.
type Manager struct {
	mu   sync.Mutex
	keys map[string]*kms.KeyMetadata
}

// NewManager returns an empty Manager.
func NewManager() *Manager {
	return &Manager{keys: make(map[string]*kms.KeyMetadata)}
}

func (m *Manager) store(keyID, algorithm string) *kms.KeyMetadata {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	key := &kms.KeyMetadata{
		KeyID: keyID, Name: keyID, Algorithm: algorithm, Version: 1,
		State: kms.KeyState("enabled"), Created: now, Updated: now,
	}
	m.keys[keyID] = key
	return key
}

func (m *Manager) lookup(keyID string) (*kms.KeyMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key, ok := m.keys[keyID]
	if !ok {
		return nil, kms.ErrKeyNotFound
	}
	return key, nil
}

// GenerateKey stores a key under attrs.CN.
func (m *Manager) GenerateKey(_ context.Context, attrs *xkmssdk.KeyAttributes) (interface{}, error) {
	algorithm := string(attrs.SymmetricAlgorithm)
	if algorithm == "" {
		algorithm = attrs.KeyAlgorithm.String()
	}
	if attrs.ECCAttributes != nil {
		// Name the curve as the transports do: "ECDSA-P256", not "P-256".
		algorithm += "-" + strings.ReplaceAll(attrs.ECCAttributes.Curve.Params().Name, "-", "")
	}
	return m.store(attrs.CN, algorithm), nil
}

// ImportKey stores a key under keyID.
func (m *Manager) ImportKey(_ context.Context, keyID string, _ []byte, algorithm string) (interface{}, error) {
	return m.store(keyID, algorithm), nil
}

// GetKey returns the key's metadata.
func (m *Manager) GetKey(_ context.Context, keyID string) (interface{}, error) {
	return m.lookup(keyID)
}

// GetPublicKey returns placeholder public key bytes for a stored key.
func (m *Manager) GetPublicKey(_ context.Context, keyID string) ([]byte, error) {
	if _, err := m.lookup(keyID); err != nil {
		return nil, err
	}
	return []byte("public-" + keyID), nil
}

// UpdateStoredMetadata keeps nothing: the service holds the metadata.
func (m *Manager) UpdateStoredMetadata(interface{}) error { return nil }

// ListKeys returns every stored key.
func (m *Manager) ListKeys(context.Context, string) ([]interface{}, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]interface{}, 0, len(m.keys))
	for _, key := range m.keys {
		out = append(out, key)
	}
	return out, nil
}

// RotateKey bumps the key's version.
func (m *Manager) RotateKey(_ context.Context, keyID string) (interface{}, error) {
	key, err := m.lookup(keyID)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	key.Version++
	return key, nil
}

// RevokeKey checks the key exists.
func (m *Manager) RevokeKey(_ context.Context, keyID string) error {
	_, err := m.lookup(keyID)
	return err
}

// EnableKey checks the key exists.
func (m *Manager) EnableKey(_ context.Context, keyID string) error {
	_, err := m.lookup(keyID)
	return err
}

// DisableKey checks the key exists.
func (m *Manager) DisableKey(_ context.Context, keyID string) error {
	_, err := m.lookup(keyID)
	return err
}

// DeleteKey removes the key.
func (m *Manager) DeleteKey(_ context.Context, keyID string) error {
	if _, err := m.lookup(keyID); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.keys, keyID)
	return nil
}

// Encrypt returns the plaintext tagged with the key id.
func (m *Manager) Encrypt(_ context.Context, keyID string, plaintext, _ []byte) ([]byte, error) {
	if _, err := m.lookup(keyID); err != nil {
		return nil, err
	}
	return append([]byte(keyID+":"), plaintext...), nil
}

// Decrypt strips the key id tag Encrypt added.
func (m *Manager) Decrypt(_ context.Context, keyID string, ciphertext, _ []byte) ([]byte, error) {
	if _, err := m.lookup(keyID); err != nil {
		return nil, err
	}
	return ciphertext[len(keyID)+1:], nil
}

// Sign returns the digest tagged with the key id.
func (m *Manager) Sign(_ context.Context, keyID string, digest []byte, _ string) ([]byte, error) {
	if _, err := m.lookup(keyID); err != nil {
		return nil, err
	}
	return append([]byte(keyID+":"), digest...), nil
}

// Signer is not served by this manager.
func (m *Manager) Signer(_ context.Context, keyID string) (crypto.Signer, error) {
	if _, err := m.lookup(keyID); err != nil {
		return nil, err
	}
	return nil, kms.ErrKeyNotFound
}

// Verify checks a signature Sign produced.
func (m *Manager) Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error) {
	expected, err := m.Sign(ctx, keyID, digest, algorithm)
	if err != nil {
		return false, err
	}
	return bytes.Equal(expected, signature), nil
}

// GenerateDataKey returns a zero key and its tagged wrapping.
func (m *Manager) GenerateDataKey(_ context.Context, _ *xkmssdk.KeyAttributes, keyBytes int) ([]byte, []byte, error) {
	plaintext := make([]byte, keyBytes)
	return plaintext, append([]byte("wrapped:"), plaintext...), nil
}

// GetBackendType reports the software backend.
func (m *Manager) GetBackendType() config.BackendType { return config.BackendTypeSoftware }

// IsAvailable reports true.
func (m *Manager) IsAvailable(context.Context) bool { return true }

// GetSupportedAlgorithms lists the algorithms NewService uses.
func (m *Manager) GetSupportedAlgorithms(context.Context) ([]string, error) {
	return []string{"AES-256-GCM", "ECDSA-P256"}, nil
}

// Close releases nothing.
func (m *Manager) Close() error { return nil }
