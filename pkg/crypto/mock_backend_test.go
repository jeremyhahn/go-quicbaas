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

package crypto

import (
	"context"
	"crypto/rand"
	"errors"
	"sync"
	"time"

	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// mockKMSBackend implements kms.Backend for testing the crypto engine.
type mockKMSBackend struct {
	mu   sync.RWMutex
	keys map[string]*kms.KeyMetadata
	data map[string][]byte // stores encrypted data

	// Configurable error injection
	errorMethods map[string]error
}

// newMockKMSBackend creates a new mock KMS backend for tests.
func newMockKMSBackend() *mockKMSBackend {
	return &mockKMSBackend{
		keys:         make(map[string]*kms.KeyMetadata),
		data:         make(map[string][]byte),
		errorMethods: make(map[string]error),
	}
}

// SetError configures an error to be returned for a specific method.
func (m *mockKMSBackend) SetError(method string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.errorMethods[method] = err
}

// ClearError removes a configured error for a method.
func (m *mockKMSBackend) ClearError(method string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.errorMethods, method)
}

func (m *mockKMSBackend) getError(method string) error {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.errorMethods[method]
}

func (m *mockKMSBackend) Type() config.BackendType {
	return config.BackendTypeSoftware
}

func (m *mockKMSBackend) IsAvailable(ctx context.Context) bool {
	return true
}

func (m *mockKMSBackend) CreateKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) (*kms.KeyMetadata, error) {
	if err := m.getError("CreateKey"); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	keyID := attrs.CN
	algorithm := m.attrsToAlgorithm(attrs)

	meta := &kms.KeyMetadata{
		KeyID:      keyID,
		Algorithm:  algorithm,
		State:      kms.KeyStateEnabled,
		Version:    1,
		Exportable: attrs.Exportable,
		Created:    time.Now(),
		Updated:    time.Now(),
		UsageCount: 0,
		Tags:       make(map[string]string),
	}

	m.keys[keyID] = meta

	// Generate random key material
	keyBytes := 32 // AES-256
	if algorithm == kms.AlgAES128GCM {
		keyBytes = 16
	}
	keyMaterial := make([]byte, keyBytes)
	_, _ = rand.Read(keyMaterial)
	m.data[keyID] = keyMaterial

	return meta, nil
}

// attrsToAlgorithm converts KeyAttributes to an algorithm string.
func (m *mockKMSBackend) attrsToAlgorithm(attrs *xkmssdk.KeyAttributes) string {
	switch attrs.SymmetricAlgorithm {
	case xkmssdk.SymmetricAES128GCM:
		return kms.AlgAES128GCM
	case xkmssdk.SymmetricAES256GCM:
		return kms.AlgAES256GCM
	}
	return kms.AlgAES256GCM
}

func (m *mockKMSBackend) ImportKey(ctx context.Context, keyID string, material []byte, algorithm string, options map[string]interface{}) (*kms.KeyMetadata, error) {
	if err := m.getError("ImportKey"); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	meta := &kms.KeyMetadata{
		KeyID:      keyID,
		Algorithm:  algorithm,
		State:      kms.KeyStateEnabled,
		Version:    1,
		Exportable: true,
		Created:    time.Now(),
		Updated:    time.Now(),
		UsageCount: 0,
		Tags:       make(map[string]string),
	}

	m.keys[keyID] = meta
	m.data[keyID] = material

	return meta, nil
}

func (m *mockKMSBackend) GetKeyMetadata(ctx context.Context, keyID string) (*kms.KeyMetadata, error) {
	if err := m.getError("GetKeyMetadata"); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	meta, ok := m.keys[keyID]
	if !ok {
		return nil, kms.ErrKeyNotFound
	}

	return meta, nil
}

func (m *mockKMSBackend) ListKeys(ctx context.Context, prefix string, limit int) ([]*kms.KeyMetadata, string, error) {
	if err := m.getError("ListKeys"); err != nil {
		return nil, "", err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*kms.KeyMetadata
	for _, meta := range m.keys {
		if prefix == "" || len(meta.KeyID) >= len(prefix) && meta.KeyID[:len(prefix)] == prefix {
			result = append(result, meta)
		}
	}

	return result, "", nil
}

func (m *mockKMSBackend) RotateKey(ctx context.Context, keyID string) (*kms.KeyMetadata, error) {
	if err := m.getError("RotateKey"); err != nil {
		return nil, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	meta, ok := m.keys[keyID]
	if !ok {
		return nil, kms.ErrKeyNotFound
	}

	meta.Version++
	meta.Updated = time.Now()

	// Generate new key material
	keyBytes := len(m.data[keyID])
	newMaterial := make([]byte, keyBytes)
	_, _ = rand.Read(newMaterial)
	m.data[keyID] = newMaterial

	return meta, nil
}

func (m *mockKMSBackend) DisableKey(ctx context.Context, keyID string) error {
	if err := m.getError("DisableKey"); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	meta, ok := m.keys[keyID]
	if !ok {
		return kms.ErrKeyNotFound
	}

	meta.State = kms.KeyStateDisabled
	meta.Updated = time.Now()
	return nil
}

func (m *mockKMSBackend) EnableKey(ctx context.Context, keyID string) error {
	if err := m.getError("EnableKey"); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	meta, ok := m.keys[keyID]
	if !ok {
		return kms.ErrKeyNotFound
	}

	meta.State = kms.KeyStateEnabled
	meta.Updated = time.Now()
	return nil
}

func (m *mockKMSBackend) DeleteKey(ctx context.Context, keyID string) error {
	if err := m.getError("DeleteKey"); err != nil {
		return err
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.keys[keyID]; !ok {
		return kms.ErrKeyNotFound
	}

	delete(m.keys, keyID)
	delete(m.data, keyID)
	return nil
}

func (m *mockKMSBackend) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	if err := m.getError("Encrypt"); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.keys[keyID]; !ok {
		return nil, kms.ErrKeyNotFound
	}

	// Mock encryption: XOR with key material for reversibility
	keyMaterial := m.data[keyID]
	ciphertext := make([]byte, len(plaintext))
	for i := range plaintext {
		ciphertext[i] = plaintext[i] ^ keyMaterial[i%len(keyMaterial)]
	}

	return ciphertext, nil
}

func (m *mockKMSBackend) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	if err := m.getError("Decrypt"); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.keys[keyID]; !ok {
		return nil, kms.ErrKeyNotFound
	}

	// Mock decryption: XOR with key material
	keyMaterial := m.data[keyID]
	plaintext := make([]byte, len(ciphertext))
	for i := range ciphertext {
		plaintext[i] = ciphertext[i] ^ keyMaterial[i%len(keyMaterial)]
	}

	return plaintext, nil
}

func (m *mockKMSBackend) Sign(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error) {
	if err := m.getError("Sign"); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.keys[keyID]; !ok {
		return nil, kms.ErrKeyNotFound
	}

	// Mock signature: simple hash-like operation
	signature := make([]byte, 64)
	copy(signature, digest)
	return signature, nil
}

func (m *mockKMSBackend) Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error) {
	if err := m.getError("Verify"); err != nil {
		return false, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.keys[keyID]; !ok {
		return false, kms.ErrKeyNotFound
	}

	// Mock verification
	if len(signature) < len(digest) {
		return false, nil
	}
	for i := range digest {
		if signature[i] != digest[i] {
			return false, nil
		}
	}
	return true, nil
}

func (m *mockKMSBackend) GenerateDataKey(ctx context.Context, keyID string, keyBytes int) (plaintext, wrapped []byte, err error) {
	if err := m.getError("GenerateDataKey"); err != nil {
		return nil, nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.keys[keyID]; !ok {
		return nil, nil, kms.ErrKeyNotFound
	}

	if keyBytes <= 0 {
		keyBytes = 32
	}

	// Generate random DEK
	dek := make([]byte, keyBytes)
	_, _ = rand.Read(dek)

	// Wrap DEK with key material (simple XOR)
	keyMaterial := m.data[keyID]
	wrappedDEK := make([]byte, len(dek))
	for i := range dek {
		wrappedDEK[i] = dek[i] ^ keyMaterial[i%len(keyMaterial)]
	}

	return dek, wrappedDEK, nil
}

func (m *mockKMSBackend) Close() error {
	return nil
}

func (m *mockKMSBackend) SupportedAlgorithms(_ context.Context) []string {
	return []string{kms.AlgAES256GCM, kms.AlgAES128GCM}
}

// ExportPublicKey exports the DER-encoded public key for an asymmetric key.
// For this mock, it returns nil since the mock only supports symmetric keys.
func (m *mockKMSBackend) ExportPublicKey(ctx context.Context, keyID string) ([]byte, error) {
	if err := m.getError("ExportPublicKey"); err != nil {
		return nil, err
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.keys[keyID]; !ok {
		return nil, kms.ErrKeyNotFound
	}

	// Mock implementation returns nil as this mock only supports symmetric keys
	return nil, nil
}

// mockErrorBackend is a mock backend that can simulate errors for specific methods.
type mockErrorBackend struct {
	*mockKMSBackend
	failCreate          bool
	failEncrypt         bool
	failDecrypt         bool
	failRotate          bool
	failDelete          bool
	failDataKey         bool
	failExportPublicKey bool
}

func newMockErrorBackend() *mockErrorBackend {
	return &mockErrorBackend{
		mockKMSBackend: newMockKMSBackend(),
	}
}

func (m *mockErrorBackend) CreateKey(ctx context.Context, attrs *xkmssdk.KeyAttributes) (*kms.KeyMetadata, error) {
	if m.failCreate {
		return nil, errors.New("mock: create key failed")
	}
	return m.mockKMSBackend.CreateKey(ctx, attrs)
}

func (m *mockErrorBackend) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	if m.failEncrypt {
		return nil, errors.New("mock: encryption failed")
	}
	return m.mockKMSBackend.Encrypt(ctx, keyID, plaintext, aad)
}

func (m *mockErrorBackend) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	if m.failDecrypt {
		return nil, errors.New("mock: decryption failed")
	}
	return m.mockKMSBackend.Decrypt(ctx, keyID, ciphertext, aad)
}

func (m *mockErrorBackend) RotateKey(ctx context.Context, keyID string) (*kms.KeyMetadata, error) {
	if m.failRotate {
		return nil, errors.New("mock: rotate key failed")
	}
	return m.mockKMSBackend.RotateKey(ctx, keyID)
}

func (m *mockErrorBackend) DeleteKey(ctx context.Context, keyID string) error {
	if m.failDelete {
		return errors.New("mock: delete key failed")
	}
	return m.mockKMSBackend.DeleteKey(ctx, keyID)
}

func (m *mockErrorBackend) GenerateDataKey(ctx context.Context, keyID string, keyBytes int) (plaintext, wrapped []byte, err error) {
	if m.failDataKey {
		return nil, nil, errors.New("mock: generate data key failed")
	}
	return m.mockKMSBackend.GenerateDataKey(ctx, keyID, keyBytes)
}

// ExportPublicKey exports the DER-encoded public key for an asymmetric key.
func (m *mockErrorBackend) ExportPublicKey(ctx context.Context, keyID string) ([]byte, error) {
	if m.failExportPublicKey {
		return nil, errors.New("mock: export public key failed")
	}
	return m.mockKMSBackend.ExportPublicKey(ctx, keyID)
}

// mockLogger is a simple logger for tests.
type mockLogger struct {
	infoCalled  bool
	debugCalled bool
	warnCalled  bool
	errorCalled bool
}

func (m *mockLogger) Debug(msg string, args ...any)                          { m.debugCalled = true }
func (m *mockLogger) Info(msg string, args ...any)                           { m.infoCalled = true }
func (m *mockLogger) Warn(msg string, args ...any)                           { m.warnCalled = true }
func (m *mockLogger) Error(msg string, args ...any)                          { m.errorCalled = true }
func (m *mockLogger) Fatal(msg string)                          {}
func (m *mockLogger) Debugf(format string, args ...interface{}) { m.debugCalled = true }
func (m *mockLogger) Infof(format string, args ...interface{})  { m.infoCalled = true }
func (m *mockLogger) Warnf(format string, args ...interface{})  { m.warnCalled = true }
func (m *mockLogger) Errorf(format string, args ...interface{}) { m.errorCalled = true }
func (m *mockLogger) Fatalf(format string, args ...interface{}) {}
func (m *mockLogger) WithField(key string, value interface{}) interfaces.Logger {
	return m
}
func (m *mockLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	return m
}
func (m *mockLogger) WithError(err error) interfaces.Logger {
	return m
}
