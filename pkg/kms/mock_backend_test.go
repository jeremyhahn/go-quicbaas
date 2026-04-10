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

package kms

import (
	"context"
	"crypto/rand"
	"sync"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// MockBackend is a test implementation of the Backend interface.
// It provides configurable behavior for testing the Manager.
type MockBackend struct {
	mu sync.RWMutex

	// Storage for keys
	keys map[string]*KeyMetadata

	// Error injection
	errors map[string]error

	// Call tracking
	calls map[string]int

	// Behavior flags
	available bool
}

// NewMockBackend creates a new mock backend for testing.
func NewMockBackend() *MockBackend {
	return &MockBackend{
		keys:      make(map[string]*KeyMetadata),
		errors:    make(map[string]error),
		calls:     make(map[string]int),
		available: true,
	}
}

// SetError configures the mock to return an error for a specific method.
func (m *MockBackend) SetError(method string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errors[method] = err
}

// ClearError removes the configured error for a method.
func (m *MockBackend) ClearError(method string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.errors, method)
}

// GetCallCount returns the number of times a method was called.
func (m *MockBackend) GetCallCount(method string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.calls[method]
}

// ResetCalls resets all call counters.
func (m *MockBackend) ResetCalls() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = make(map[string]int)
}

// SetAvailable sets the availability status.
func (m *MockBackend) SetAvailable(available bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.available = available
}

// trackCall increments the call counter for a method.
func (m *MockBackend) trackCall(method string) {
	m.calls[method]++
}

// checkError returns any configured error for the method.
func (m *MockBackend) checkError(method string) error {
	return m.errors[method]
}

// Type returns the backend type identifier.
func (m *MockBackend) Type() config.BackendType {
	return config.BackendTypeSoftware // Mock uses software type for testing
}

// IsAvailable checks if the backend is available.
func (m *MockBackend) IsAvailable(ctx context.Context) bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.trackCall("IsAvailable")
	return m.available
}

// CreateKey creates a new cryptographic key.
func (m *MockBackend) CreateKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) (*KeyMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trackCall("CreateKey")

	if err := m.checkError("CreateKey"); err != nil {
		return nil, err
	}

	if attrs == nil {
		return nil, errors.InvalidInput("MockBackend.CreateKey", "key attributes cannot be nil")
	}

	keyID := attrs.CN
	if keyID == "" {
		return nil, errors.InvalidInput("MockBackend.CreateKey", "key CN (common name) cannot be empty")
	}

	// Check if key already exists
	if _, exists := m.keys[keyID]; exists {
		return nil, errors.AlreadyExists("MockBackend.CreateKey", keyID)
	}

	// Determine algorithm from attrs
	algorithm := m.attrsToAlgorithmString(attrs)

	now := time.Now()
	metadata := &KeyMetadata{
		KeyID:       keyID,
		Name:        attrs.CN,
		Description: "",
		Algorithm:   algorithm,
		State:       KeyStateEnabled,
		Version:     1,
		Exportable:  attrs.Exportable,
		Created:     now,
		Updated:     now,
		UsageCount:  0,
		Tags:        make(map[string]string),
	}

	m.keys[keyID] = metadata
	return metadata, nil
}

// attrsToAlgorithmString converts KeyAttributes to an algorithm string.
func (m *MockBackend) attrsToAlgorithmString(attrs *xkmssdk.KeyAttributes) string {
	// Check symmetric algorithms first
	switch attrs.SymmetricAlgorithm {
	case xkmssdk.SymmetricAES128GCM:
		return AlgAES128GCM
	case xkmssdk.SymmetricAES192GCM:
		return AlgAES192GCM
	case xkmssdk.SymmetricAES256GCM:
		return AlgAES256GCM
	case xkmssdk.SymmetricChaCha20Poly1305:
		return AlgChaCha20Poly1305
	}

	// Check asymmetric algorithms (RSA)
	if attrs.RSAAttributes != nil {
		switch attrs.RSAAttributes.KeySize {
		case 2048:
			return AlgRSA2048
		case 3072:
			return AlgRSA3072
		case 4096:
			return AlgRSA4096
		}
	}

	// Check ECC algorithms
	if attrs.ECCAttributes != nil && attrs.ECCAttributes.Curve != nil {
		switch attrs.ECCAttributes.Curve.Params().BitSize {
		case 256:
			return AlgECDSAP256
		case 384:
			return AlgECDSAP384
		case 521:
			return AlgECDSAP521
		}
	}

	// Default
	return AlgAES256GCM
}

// ImportKey imports external key material.
func (m *MockBackend) ImportKey(ctx context.Context, keyID string, material []byte, algorithm string, options map[string]interface{}) (*KeyMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trackCall("ImportKey")

	if err := m.checkError("ImportKey"); err != nil {
		return nil, err
	}

	// Check if key already exists
	if _, exists := m.keys[keyID]; exists {
		return nil, errors.AlreadyExists("MockBackend.ImportKey", keyID)
	}

	now := time.Now()
	metadata := &KeyMetadata{
		KeyID:       keyID,
		Description: "",
		Algorithm:   algorithm,
		State:       KeyStateEnabled,
		Version:     1,
		Exportable:  true,
		Created:     now,
		Updated:     now,
		UsageCount:  0,
		Tags:        make(map[string]string),
	}

	// Apply options
	if options != nil {
		if desc, ok := options["description"].(string); ok {
			metadata.Description = desc
		}
		if exp, ok := options["exportable"].(bool); ok {
			metadata.Exportable = exp
		}
		if tags, ok := options["tags"].(map[string]string); ok {
			metadata.Tags = tags
		}
	}

	m.keys[keyID] = metadata
	return metadata, nil
}

// GetKeyMetadata retrieves metadata for a key.
func (m *MockBackend) GetKeyMetadata(ctx context.Context, keyID string) (*KeyMetadata, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.trackCall("GetKeyMetadata")

	if err := m.checkError("GetKeyMetadata"); err != nil {
		return nil, err
	}

	metadata, exists := m.keys[keyID]
	if !exists {
		return nil, ErrKeyNotFound
	}

	// Return a copy to prevent external modification
	metadataCopy := *metadata
	if metadata.Tags != nil {
		metadataCopy.Tags = make(map[string]string, len(metadata.Tags))
		for k, v := range metadata.Tags {
			metadataCopy.Tags[k] = v
		}
	}

	return &metadataCopy, nil
}

// ListKeys lists keys matching the prefix.
func (m *MockBackend) ListKeys(ctx context.Context, prefix string, limit int) ([]*KeyMetadata, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.trackCall("ListKeys")

	if err := m.checkError("ListKeys"); err != nil {
		return nil, "", err
	}

	var result []*KeyMetadata
	for keyID, metadata := range m.keys {
		if prefix == "" || len(keyID) >= len(prefix) && keyID[:len(prefix)] == prefix {
			metadataCopy := *metadata
			if metadata.Tags != nil {
				metadataCopy.Tags = make(map[string]string, len(metadata.Tags))
				for k, v := range metadata.Tags {
					metadataCopy.Tags[k] = v
				}
			}
			result = append(result, &metadataCopy)
		}
	}

	// Apply limit if specified
	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}

	return result, "", nil
}

// RotateKey rotates a key to a new version.
func (m *MockBackend) RotateKey(ctx context.Context, keyID string) (*KeyMetadata, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trackCall("RotateKey")

	if err := m.checkError("RotateKey"); err != nil {
		return nil, err
	}

	metadata, exists := m.keys[keyID]
	if !exists {
		return nil, ErrKeyNotFound
	}

	if metadata.State != KeyStateEnabled {
		return nil, errors.New(errors.ErrPreconditionFailed, "MockBackend.RotateKey", "cannot rotate disabled key")
	}

	// Increment version and update timestamp
	metadata.Version++
	metadata.Updated = time.Now()

	return metadata, nil
}

// DisableKey temporarily disables a key.
func (m *MockBackend) DisableKey(ctx context.Context, keyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trackCall("DisableKey")

	if err := m.checkError("DisableKey"); err != nil {
		return err
	}

	metadata, exists := m.keys[keyID]
	if !exists {
		return ErrKeyNotFound
	}

	metadata.State = KeyStateDisabled
	metadata.Updated = time.Now()
	return nil
}

// EnableKey enables a disabled key.
func (m *MockBackend) EnableKey(ctx context.Context, keyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trackCall("EnableKey")

	if err := m.checkError("EnableKey"); err != nil {
		return err
	}

	metadata, exists := m.keys[keyID]
	if !exists {
		return ErrKeyNotFound
	}

	metadata.State = KeyStateEnabled
	metadata.Updated = time.Now()
	return nil
}

// DeleteKey permanently deletes a key.
func (m *MockBackend) DeleteKey(ctx context.Context, keyID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trackCall("DeleteKey")

	if err := m.checkError("DeleteKey"); err != nil {
		return err
	}

	if _, exists := m.keys[keyID]; !exists {
		return ErrKeyNotFound
	}

	delete(m.keys, keyID)
	return nil
}

// Encrypt encrypts plaintext using the specified key.
func (m *MockBackend) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.trackCall("Encrypt")

	if err := m.checkError("Encrypt"); err != nil {
		return nil, err
	}

	metadata, exists := m.keys[keyID]
	if !exists {
		return nil, ErrKeyNotFound
	}

	if metadata.State != KeyStateEnabled {
		return nil, errors.New(errors.ErrPreconditionFailed, "MockBackend.Encrypt", "key is not enabled")
	}

	// Simple mock encryption: just prepend a marker and reverse the plaintext
	ciphertext := make([]byte, len(plaintext)+4)
	copy(ciphertext[:4], []byte("MOCK"))
	for i, b := range plaintext {
		ciphertext[i+4] = b ^ 0x55 // Simple XOR "encryption"
	}

	return ciphertext, nil
}

// Decrypt decrypts ciphertext using the specified key.
func (m *MockBackend) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.trackCall("Decrypt")

	if err := m.checkError("Decrypt"); err != nil {
		return nil, err
	}

	metadata, exists := m.keys[keyID]
	if !exists {
		return nil, ErrKeyNotFound
	}

	if metadata.State != KeyStateEnabled {
		return nil, errors.New(errors.ErrPreconditionFailed, "MockBackend.Decrypt", "key is not enabled")
	}

	// Reverse our mock encryption
	if len(ciphertext) < 4 {
		return nil, errors.InvalidInput("MockBackend.Decrypt", "invalid ciphertext")
	}

	plaintext := make([]byte, len(ciphertext)-4)
	for i := range plaintext {
		plaintext[i] = ciphertext[i+4] ^ 0x55
	}

	return plaintext, nil
}

// Sign creates a digital signature for the digest.
func (m *MockBackend) Sign(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.trackCall("Sign")

	if err := m.checkError("Sign"); err != nil {
		return nil, err
	}

	metadata, exists := m.keys[keyID]
	if !exists {
		return nil, ErrKeyNotFound
	}

	if metadata.State != KeyStateEnabled {
		return nil, errors.New(errors.ErrPreconditionFailed, "MockBackend.Sign", "key is not enabled")
	}

	// Mock signature: hash of digest with prefix
	signature := make([]byte, 64)
	copy(signature[:4], []byte("SIG1"))
	for i := 0; i < len(digest) && i < 60; i++ {
		signature[i+4] = digest[i] ^ 0xAA
	}

	return signature, nil
}

// Verify verifies a digital signature.
func (m *MockBackend) Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.trackCall("Verify")

	if err := m.checkError("Verify"); err != nil {
		return false, err
	}

	metadata, exists := m.keys[keyID]
	if !exists {
		return false, ErrKeyNotFound
	}

	if metadata.State != KeyStateEnabled {
		return false, errors.New(errors.ErrPreconditionFailed, "MockBackend.Verify", "key is not enabled")
	}

	// Verify our mock signature
	if len(signature) < 4 {
		return false, nil
	}

	if string(signature[:4]) != "SIG1" {
		return false, nil
	}

	// Check that signature matches digest
	for i := 0; i < len(digest) && i < 60; i++ {
		if signature[i+4] != (digest[i] ^ 0xAA) {
			return false, nil
		}
	}

	return true, nil
}

// GenerateDataKey generates a new data encryption key.
func (m *MockBackend) GenerateDataKey(ctx context.Context, keyID string, keyBytes int) (plaintext, wrapped []byte, err error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.trackCall("GenerateDataKey")

	if err := m.checkError("GenerateDataKey"); err != nil {
		return nil, nil, err
	}

	metadata, exists := m.keys[keyID]
	if !exists {
		return nil, nil, ErrKeyNotFound
	}

	if metadata.State != KeyStateEnabled {
		return nil, nil, errors.New(errors.ErrPreconditionFailed, "MockBackend.GenerateDataKey", "key is not enabled")
	}

	// Generate random plaintext key
	plaintext = make([]byte, keyBytes)
	if _, err := rand.Read(plaintext); err != nil {
		return nil, nil, errors.Wrapf(err, errors.ErrInternal, "MockBackend.GenerateDataKey", "failed to generate random key")
	}

	// Wrap it using our mock encryption
	wrapped = make([]byte, len(plaintext)+4)
	copy(wrapped[:4], []byte("WRAP"))
	for i, b := range plaintext {
		wrapped[i+4] = b ^ 0x55
	}

	return plaintext, wrapped, nil
}

// SupportedAlgorithms returns the list of cryptographic algorithms supported by this backend.
func (m *MockBackend) SupportedAlgorithms(ctx context.Context) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.trackCall("SupportedAlgorithms")

	// Return all supported algorithms for the mock backend
	return []string{
		AlgAES256GCM,
		AlgAES128GCM,
		AlgChaCha20Poly1305,
		AlgRSA2048,
		AlgRSA4096,
		AlgECDSAP256,
		AlgECDSAP384,
		AlgEd25519,
	}
}

// ExportPublicKey exports the DER-encoded public key for an asymmetric key.
func (m *MockBackend) ExportPublicKey(ctx context.Context, keyID string) ([]byte, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	m.trackCall("ExportPublicKey")

	if err := m.checkError("ExportPublicKey"); err != nil {
		return nil, err
	}

	// Check if key exists
	key, exists := m.keys[keyID]
	if !exists {
		return nil, ErrKeyNotFound
	}

	// Only asymmetric keys have public keys
	switch key.Algorithm {
	case AlgRSA2048, AlgRSA4096, AlgECDSAP256, AlgECDSAP384, AlgEd25519:
		// Return mock public key bytes
		return []byte("mock-public-key-" + keyID), nil
	default:
		return nil, errors.Newf(errors.ErrInvalidInput, "MockBackend.ExportPublicKey", "key %s is symmetric and does not have a public key", keyID)
	}
}

// Close releases any resources held by the backend.
func (m *MockBackend) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.trackCall("Close")

	if err := m.checkError("Close"); err != nil {
		return err
	}

	// Clear all data
	m.keys = make(map[string]*KeyMetadata)
	m.errors = make(map[string]error)
	m.calls = make(map[string]int)
	m.available = false

	return nil
}
