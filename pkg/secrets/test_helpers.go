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

// Package secrets test helpers - shared test utilities and mocks.
//go:build !production

package secrets

import (
	"context"
	"crypto"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"strings"
	"sync"
	"time"

	types "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// testMockSecretsEncryptor implements interfaces.SecretsEncryptor for testing.
type testMockSecretsEncryptor struct {
	mu                      sync.RWMutex
	keys                    map[string][]byte // keyIdentifier -> key material
	encryptFn               func(ctx context.Context, keyIdentifier string, plaintext []byte, opts *interfaces.EncryptOptions) (*interfaces.EncryptedBlob, error)
	decryptFn               func(ctx context.Context, blob *interfaces.EncryptedBlob) ([]byte, error)
	validateKeyIdentifierFn func(ctx context.Context, keyIdentifier string) error
	encryptionCounter       int
	decryptionCounter       int
}

func newTestMockSecretsEncryptor() *testMockSecretsEncryptor {
	enc := &testMockSecretsEncryptor{
		keys: make(map[string][]byte),
	}

	// Add default global DEK (32 bytes for AES-256)
	globalKey := make([]byte, 32)
	for i := range globalKey {
		globalKey[i] = byte(i)
	}
	enc.keys[string(DEKIDGlobal)] = globalKey

	return enc
}

func (m *testMockSecretsEncryptor) Encrypt(ctx context.Context, keyIdentifier string, plaintext []byte, opts *interfaces.EncryptOptions) (*interfaces.EncryptedBlob, error) {
	if m.encryptFn != nil {
		return m.encryptFn(ctx, keyIdentifier, plaintext, opts)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.encryptionCounter++

	// Get key material
	key, ok := m.keys[keyIdentifier]
	if !ok {
		return nil, interfaces.ErrKeyNotFound
	}

	// Set defaults
	algorithm := interfaces.DefaultAlgorithm
	var aad []byte
	if opts != nil {
		if opts.Algorithm != "" {
			algorithm = opts.Algorithm
		}
		aad = opts.AAD
	}

	// Perform AES-GCM encryption
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, &dberrors.WrappedMessageError{Message: "failed to create cipher", Cause: err}
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, &dberrors.WrappedMessageError{Message: "failed to create GCM", Cause: err}
	}

	// Generate nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, &dberrors.WrappedMessageError{Message: "failed to generate nonce", Cause: err}
	}

	// Encrypt
	sealed := gcm.Seal(nil, nonce, plaintext, aad)

	// Split ciphertext and tag
	tagSize := 16
	ciphertext := sealed[:len(sealed)-tagSize]
	tag := sealed[len(sealed)-tagSize:]

	blob := &interfaces.EncryptedBlob{
		KeyIdentifier: keyIdentifier,
		KeyVersion:    1,
		Algorithm:     algorithm,
		Nonce:         nonce,
		Ciphertext:    ciphertext,
		Tag:           tag,
		AAD:           aad,
		EncryptedAt:   time.Now().UTC(),
	}

	return blob, nil
}

func (m *testMockSecretsEncryptor) Decrypt(ctx context.Context, blob *interfaces.EncryptedBlob) ([]byte, error) {
	if m.decryptFn != nil {
		return m.decryptFn(ctx, blob)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.decryptionCounter++

	// Validate blob
	if err := blob.Validate(); err != nil {
		return nil, err
	}

	// Get key material
	key, ok := m.keys[blob.KeyIdentifier]
	if !ok {
		return nil, interfaces.ErrKeyNotFound
	}

	// Perform AES-GCM decryption
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, &dberrors.WrappedMessageError{Message: "failed to create cipher", Cause: err}
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, &dberrors.WrappedMessageError{Message: "failed to create GCM", Cause: err}
	}

	// Reconstruct sealed data
	sealed := make([]byte, len(blob.Ciphertext)+len(blob.Tag))
	copy(sealed, blob.Ciphertext)
	copy(sealed[len(blob.Ciphertext):], blob.Tag)

	// Decrypt
	plaintext, err := gcm.Open(nil, blob.Nonce, sealed, blob.AAD)
	if err != nil {
		return nil, &dberrors.WrappedMessageError{Message: "decryption failed", Cause: err}
	}

	return plaintext, nil
}

func (m *testMockSecretsEncryptor) ValidateKeyIdentifier(ctx context.Context, keyIdentifier string) error {
	if m.validateKeyIdentifierFn != nil {
		return m.validateKeyIdentifierFn(ctx, keyIdentifier)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	if _, ok := m.keys[keyIdentifier]; !ok {
		return interfaces.ErrKeyNotFound
	}

	return nil
}

// AddKey adds a key to the mock encryptor for testing.
func (m *testMockSecretsEncryptor) AddKey(keyIdentifier string, key []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.keys[keyIdentifier] = key
}

// testMockKMSManager implements interfaces.KMSManager for testing.
// This is a shared mock to avoid redeclaration errors.
type testMockKMSManager struct {
	mu                sync.RWMutex
	keys              map[string][]byte
	generateDataKeyFn func(ctx context.Context, attrs *types.KeyAttributes, keyBytes int) (plaintext, wrapped []byte, err error)
	encryptFn         func(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error)
	decryptFn         func(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error)
	generateKeyFn     func(ctx context.Context, attrs *types.KeyAttributes) (interface{}, error)
	importKeyFn       func(ctx context.Context, keyID string, material []byte, algorithm string) (interface{}, error)
	getKeyFn          func(ctx context.Context, keyID string) (interface{}, error)
	listKeysFn        func(ctx context.Context, prefix string) ([]interface{}, error)
	rotateKeyFn       func(ctx context.Context, keyID string) (interface{}, error)
	revokeKeyFn       func(ctx context.Context, keyID string) error
	deleteKeyFn       func(ctx context.Context, keyID string) error
	signFn            func(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error)
	verifyFn          func(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error)
	getBackendTypeFn  func() config.BackendType
	isAvailableFn     func(ctx context.Context) bool
	dataKeyCounter    int
	encryptionCounter int
	decryptionCounter int
}

func newTestMockKMSManager() *testMockKMSManager {
	return &testMockKMSManager{
		keys: make(map[string][]byte),
	}
}

func (m *testMockKMSManager) GenerateDataKey(ctx context.Context, attrs *types.KeyAttributes, keyBytes int) (plaintext, wrapped []byte, err error) {
	if m.generateDataKeyFn != nil {
		return m.generateDataKeyFn(ctx, attrs, keyBytes)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate plaintext key
	plaintext = make([]byte, keyBytes)
	for i := range plaintext {
		plaintext[i] = byte(m.dataKeyCounter)
	}
	m.dataKeyCounter++

	// Wrap the key (simple XOR for testing)
	wrapped = make([]byte, keyBytes)
	copy(wrapped, plaintext)
	for i := range wrapped {
		wrapped[i] ^= 0xFF
	}

	return plaintext, wrapped, nil
}

func (m *testMockKMSManager) Encrypt(ctx context.Context, keyID string, plaintext, aad []byte) ([]byte, error) {
	if m.encryptFn != nil {
		return m.encryptFn(ctx, keyID, plaintext, aad)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.encryptionCounter++

	// Simple XOR encryption for testing
	ciphertext := make([]byte, len(plaintext))
	copy(ciphertext, plaintext)
	for i := range ciphertext {
		ciphertext[i] ^= 0xFF
	}

	return ciphertext, nil
}

func (m *testMockKMSManager) Decrypt(ctx context.Context, keyID string, ciphertext, aad []byte) ([]byte, error) {
	if m.decryptFn != nil {
		return m.decryptFn(ctx, keyID, ciphertext, aad)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.decryptionCounter++

	// Simple XOR decryption for testing (same as encryption)
	plaintext := make([]byte, len(ciphertext))
	copy(plaintext, ciphertext)
	for i := range plaintext {
		plaintext[i] ^= 0xFF
	}

	return plaintext, nil
}

func (m *testMockKMSManager) GenerateKey(ctx context.Context, attrs *types.KeyAttributes) (interface{}, error) {
	if m.generateKeyFn != nil {
		return m.generateKeyFn(ctx, attrs)
	}
	return nil, nil
}

func (m *testMockKMSManager) ImportKey(ctx context.Context, keyID string, material []byte, algorithm string, _ bool) (interface{}, error) {
	if m.importKeyFn != nil {
		return m.importKeyFn(ctx, keyID, material, algorithm)
	}
	return nil, nil
}

func (m *testMockKMSManager) GetKey(ctx context.Context, keyID string) (interface{}, error) {
	if m.getKeyFn != nil {
		return m.getKeyFn(ctx, keyID)
	}
	return nil, nil
}

func (m *testMockKMSManager) UpdateStoredMetadata(metadata interface{}) error {
	// No-op for mock
	return nil
}

func (m *testMockKMSManager) ListKeys(ctx context.Context, prefix string) ([]interface{}, error) {
	if m.listKeysFn != nil {
		return m.listKeysFn(ctx, prefix)
	}
	return nil, nil
}

func (m *testMockKMSManager) RotateKey(ctx context.Context, keyID string) (interface{}, error) {
	if m.rotateKeyFn != nil {
		return m.rotateKeyFn(ctx, keyID)
	}
	return nil, nil
}

func (m *testMockKMSManager) RevokeKey(ctx context.Context, keyID string) error {
	if m.revokeKeyFn != nil {
		return m.revokeKeyFn(ctx, keyID)
	}
	return nil
}

func (m *testMockKMSManager) DeleteKey(ctx context.Context, keyID string) error {
	if m.deleteKeyFn != nil {
		return m.deleteKeyFn(ctx, keyID)
	}
	return nil
}

func (m *testMockKMSManager) EnableKey(ctx context.Context, keyID string) error {
	return nil
}

func (m *testMockKMSManager) DisableKey(ctx context.Context, keyID string) error {
	return nil
}

func (m *testMockKMSManager) Sign(ctx context.Context, keyID string, digest []byte, algorithm string) ([]byte, error) {
	if m.signFn != nil {
		return m.signFn(ctx, keyID, digest, algorithm)
	}
	return nil, nil
}

func (m *testMockKMSManager) Verify(ctx context.Context, keyID string, digest, signature []byte, algorithm string) (bool, error) {
	if m.verifyFn != nil {
		return m.verifyFn(ctx, keyID, digest, signature, algorithm)
	}
	return false, nil
}

func (m *testMockKMSManager) GetBackendType() config.BackendType {
	if m.getBackendTypeFn != nil {
		return m.getBackendTypeFn()
	}
	return config.BackendTypeSoftware
}

func (m *testMockKMSManager) IsAvailable(ctx context.Context) bool {
	if m.isAvailableFn != nil {
		return m.isAvailableFn(ctx)
	}
	return true
}

func (m *testMockKMSManager) Close() error {
	return nil
}

func (m *testMockKMSManager) GetSupportedAlgorithms(ctx context.Context) ([]string, error) {
	return []string{
		"AES-128-GCM", "AES-192-GCM", "AES-256-GCM",
		"CHACHA20-POLY1305",
		"RSA-2048", "RSA-3072", "RSA-4096",
		"ECDSA-P256", "ECDSA-P384", "ECDSA-P521",
		"ED25519", "ED448",
	}, nil
}

func (m *testMockKMSManager) GetPublicKey(ctx context.Context, keyID string) ([]byte, error) {
	// Return mock public key bytes
	return []byte("mock-public-key-" + keyID), nil
}

// testMockKVStore implements interfaces.KVStore for testing.
// This is a shared mock to avoid redeclaration errors.
type testMockKVStore struct {
	mu       sync.RWMutex
	data     map[string][]byte
	putFn    func(ctx context.Context, key string, value []byte) error
	getFn    func(ctx context.Context, key string) ([]byte, error)
	deleteFn func(ctx context.Context, key string) error
	scanFn   func(ctx context.Context, prefix string, fn func(key string, value []byte) error) error
	listFn   func(ctx context.Context, prefix string) ([]string, error)
}

func newTestMockKVStore() *testMockKVStore {
	return &testMockKVStore{
		data: make(map[string][]byte),
	}
}

func (m *testMockKVStore) Put(ctx context.Context, key string, value []byte) error {
	if m.putFn != nil {
		return m.putFn(ctx, key, value)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.data[key] = value
	return nil
}

func (m *testMockKVStore) Get(ctx context.Context, key string) ([]byte, error) {
	if m.getFn != nil {
		return m.getFn(ctx, key)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()
	if val, ok := m.data[key]; ok {
		return val, nil
	}
	return nil, ErrSecretNotFound
}

func (m *testMockKVStore) Delete(ctx context.Context, key string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, key)
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.data, key)
	return nil
}

func (m *testMockKVStore) Scan(ctx context.Context, prefix string, fn func(key string, value []byte) error) error {
	if m.scanFn != nil {
		return m.scanFn(ctx, prefix, fn)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	for k, v := range m.data {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			if err := fn(k, v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (m *testMockKVStore) List(ctx context.Context, prefix string) ([]string, error) {
	if m.listFn != nil {
		return m.listFn(ctx, prefix)
	}

	m.mu.RLock()
	defer m.mu.RUnlock()

	var keys []string
	for k := range m.data {
		if prefix == "" || strings.HasPrefix(k, prefix) {
			keys = append(keys, k)
		}
	}
	return keys, nil
}

func (m *testMockKVStore) Exists(ctx context.Context, key string) (bool, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	_, ok := m.data[key]
	return ok, nil
}

// testMockLogger implements interfaces.Logger for testing.
// This is a shared mock to avoid redeclaration errors.
type testMockLogger struct {
	mu      sync.Mutex
	entries []string
}

func newTestMockLogger() *testMockLogger {
	return &testMockLogger{
		entries: make([]string, 0),
	}
}

func (l *testMockLogger) Debug(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, "DEBUG: "+msg)
}

func (l *testMockLogger) Debugf(format string, args ...interface{}) {
	l.Debug(format)
}

func (l *testMockLogger) Info(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, "INFO: "+msg)
}

func (l *testMockLogger) Infof(format string, args ...interface{}) {
	l.Info(format)
}

func (l *testMockLogger) Warn(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, "WARN: "+msg)
}

func (l *testMockLogger) Warnf(format string, args ...interface{}) {
	l.Warn(format)
}

func (l *testMockLogger) Error(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, "ERROR: "+msg)
}

func (l *testMockLogger) Errorf(format string, args ...interface{}) {
	l.Error(format)
}

func (l *testMockLogger) Fatal(msg string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, "FATAL: "+msg)
}

func (l *testMockLogger) Fatalf(format string, args ...interface{}) {
	l.Fatal(format)
}

func (l *testMockLogger) WithField(key string, value interface{}) interfaces.Logger {
	return l
}

func (l *testMockLogger) WithFields(fields map[string]interface{}) interfaces.Logger {
	return l
}

func (l *testMockLogger) WithError(err error) interfaces.Logger {
	return l
}

// Signer returns a crypto.Signer for the given keyID. Tests that don't
// exercise the signer path receive a nil signer and no error; tests
// that do can stub this by redefining the method on a wrapped type.
func (m *testMockKMSManager) Signer(_ context.Context, _ string) (crypto.Signer, error) {
	return nil, nil
}
