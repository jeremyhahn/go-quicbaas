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

// Package crypto provides encryption-as-a-service for QuicBaaS.
package crypto

import (
	"context"
	"fmt"
	"sync"
	"time"

	types "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
)

// Engine provides crypto encryption services.
// All crypto operations are delegated to go-xkms via KMS.
type Engine struct {
	kms    *kms.KMSService
	keys   map[string]*KeyMetadata
	mu     sync.RWMutex
	logger interfaces.Logger
}

// EngineOptions configures the crypto engine.
type EngineOptions struct {
	// KMS is the key management service (required).
	KMS *kms.KMSService

	// Logger for operation logging (optional).
	Logger interfaces.Logger
}

// NewEngine creates a new crypto encryption engine.
func NewEngine(opts *EngineOptions) (*Engine, error) {
	if opts == nil {
		return nil, errors.InvalidInput("crypto.NewEngine", "options cannot be nil")
	}

	if opts.KMS == nil {
		return nil, errors.InvalidInput("crypto.NewEngine", "KMS manager is required")
	}

	return &Engine{
		kms:    opts.KMS,
		keys:   make(map[string]*KeyMetadata),
		logger: opts.Logger,
	}, nil
}

// CreateKey creates a new encryption key in the hierarchy.
func (e *Engine) CreateKey(ctx context.Context, cfg *KeyConfig) (*KeyMetadata, error) {
	if cfg == nil {
		return nil, errors.InvalidInput("Engine.CreateKey", "key config cannot be nil")
	}

	if cfg.Name == "" {
		return nil, errors.InvalidInput("Engine.CreateKey", "key name is required")
	}

	// Default algorithm
	algorithm := cfg.Algorithm
	if algorithm == "" {
		algorithm = kms.AlgAES256GCM
	}

	// Build KeyAttributes for go-xkms
	attrs := e.algorithmToKeyAttributes(cfg.Name, algorithm)

	// Generate key via KMS (delegates to go-xkms)
	result, err := e.kms.GenerateKey(ctx, attrs)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, "Engine.CreateKey", "failed to generate key")
	}

	// Extract key ID from result if possible
	keyID := cfg.Name
	if kmsMeta, ok := result.(*kms.KeyMetadata); ok && kmsMeta != nil {
		keyID = kmsMeta.KeyID
	}

	now := time.Now()
	var expiresAt *time.Time
	if cfg.AutoRotateAfter > 0 {
		exp := now.Add(cfg.AutoRotateAfter)
		expiresAt = &exp
	}

	meta := &KeyMetadata{
		ID:               keyID,
		Name:             cfg.Name,
		Scope:            cfg.Scope,
		Version:          1,
		Algorithm:        algorithm,
		ParentID:         cfg.ParentName,
		RotationStrategy: cfg.RotationStrategy,
		Entity:           cfg.Entity,
		CreatedAt:        now,
		RotatedAt:        now,
		ExpiresAt:        expiresAt,
		Metadata:         cfg.Metadata,
	}

	e.mu.Lock()
	e.keys[cfg.Name] = meta
	e.mu.Unlock()

	if e.logger != nil {
		e.logger.WithFields(map[string]interface{}{
			"key":   cfg.Name,
			"scope": cfg.Scope,
		}).Info("Crypto key created")
	}

	return meta, nil
}

// GetKey returns metadata for a key.
func (e *Engine) GetKey(name string) (*KeyMetadata, error) {
	e.mu.RLock()
	meta, exists := e.keys[name]
	e.mu.RUnlock()

	if !exists {
		return nil, errors.NotFound("Engine.GetKey", name)
	}

	return meta, nil
}

// Encrypt encrypts data using the specified key.
// The nonce is managed internally by go-xkms and prepended to ciphertext.
func (e *Engine) Encrypt(ctx context.Context, req *EncryptRequest) (*EncryptResponse, error) {
	if req == nil {
		return nil, errors.InvalidInput("Engine.Encrypt", "request cannot be nil")
	}

	if req.KeyName == "" {
		return nil, errors.InvalidInput("Engine.Encrypt", "key name is required")
	}

	meta, err := e.GetKey(req.KeyName)
	if err != nil {
		return nil, err
	}

	// Encrypt via KMS - nonce is handled internally by go-xkms
	ciphertext, err := e.kms.Encrypt(ctx, req.KeyName, req.Plaintext, req.Context)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, "Engine.Encrypt", "encryption failed")
	}

	return &EncryptResponse{
		Ciphertext: ciphertext,
		KeyID:      meta.ID,
		KeyVersion: meta.Version,
	}, nil
}

// Decrypt decrypts data using the specified key.
// The nonce is extracted from the ciphertext by go-xkms.
func (e *Engine) Decrypt(ctx context.Context, req *DecryptRequest) (*DecryptResponse, error) {
	if req == nil {
		return nil, errors.InvalidInput("Engine.Decrypt", "request cannot be nil")
	}

	if req.KeyName == "" {
		return nil, errors.InvalidInput("Engine.Decrypt", "key name is required")
	}

	// Decrypt via KMS - nonce is extracted internally by go-xkms
	plaintext, err := e.kms.Decrypt(ctx, req.KeyName, req.Ciphertext, req.Context)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, "Engine.Decrypt", "decryption failed")
	}

	return &DecryptResponse{
		Plaintext: plaintext,
	}, nil
}

// GenerateDataKey generates a new DEK and returns both plaintext and encrypted forms.
func (e *Engine) GenerateDataKey(ctx context.Context, parentKeyName string, keyBytes int) (*DataKey, error) {
	if parentKeyName == "" {
		return nil, errors.InvalidInput("Engine.GenerateDataKey", "parent key name is required")
	}

	if keyBytes <= 0 {
		keyBytes = 32 // Default to 256 bits
	}

	// Check parent exists
	parentMeta, err := e.GetKey(parentKeyName)
	if err != nil {
		return nil, err
	}

	// Generate DEK via KMS - returns plaintext and wrapped key
	kekAttrs := &types.KeyAttributes{CN: parentKeyName}
	plaintext, encrypted, err := e.kms.GenerateDataKey(ctx, kekAttrs, keyBytes)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, "Engine.GenerateDataKey", "failed to generate data key")
	}

	dekID := fmt.Sprintf("dek-%d", time.Now().UnixNano())

	return &DataKey{
		Plaintext: plaintext,
		Encrypted: &EncryptedKey{
			KeyID:             dekID,
			EncryptedMaterial: encrypted,
			ParentKeyID:       parentMeta.ID,
			ParentVersion:     parentMeta.Version,
		},
		Metadata: &KeyMetadata{
			ID:        dekID,
			Name:      dekID,
			Scope:     ScopeData,
			Version:   1,
			Algorithm: kms.AlgAES256GCM,
			ParentID:  parentMeta.ID,
			CreatedAt: time.Now(),
			RotatedAt: time.Now(),
		},
	}, nil
}

// DecryptDataKey decrypts an encrypted DEK using its parent key.
func (e *Engine) DecryptDataKey(ctx context.Context, parentKeyName string, encrypted *EncryptedKey) ([]byte, error) {
	if parentKeyName == "" {
		return nil, errors.InvalidInput("Engine.DecryptDataKey", "parent key name is required")
	}

	if encrypted == nil {
		return nil, errors.InvalidInput("Engine.DecryptDataKey", "encrypted key cannot be nil")
	}

	// Decrypt the DEK using the parent key
	plaintext, err := e.kms.Decrypt(ctx, parentKeyName, encrypted.EncryptedMaterial, nil)
	if err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, "Engine.DecryptDataKey", "failed to decrypt DEK")
	}

	return plaintext, nil
}

// Rewrap re-encrypts ciphertext with a new key version.
func (e *Engine) Rewrap(ctx context.Context, req *RewrapRequest) (*RewrapResponse, error) {
	if req == nil {
		return nil, errors.InvalidInput("Engine.Rewrap", "request cannot be nil")
	}

	// Decrypt with old version
	decryptReq := &DecryptRequest{
		KeyName:    req.KeyName,
		Ciphertext: req.Ciphertext,
		Context:    req.Context,
		KeyVersion: req.OldKeyVersion,
	}

	decryptResp, err := e.Decrypt(ctx, decryptReq)
	if err != nil {
		return nil, err
	}

	// Re-encrypt with new version
	encryptReq := &EncryptRequest{
		KeyName:    req.KeyName,
		Plaintext:  decryptResp.Plaintext,
		Context:    req.Context,
		KeyVersion: req.NewKeyVersion,
	}

	encryptResp, err := e.Encrypt(ctx, encryptReq)
	if err != nil {
		return nil, err
	}

	return &RewrapResponse{
		Ciphertext:    encryptResp.Ciphertext,
		NewKeyVersion: encryptResp.KeyVersion,
	}, nil
}

// RotateKey rotates a key according to its configured strategy.
func (e *Engine) RotateKey(ctx context.Context, keyName string) (*KeyMetadata, error) {
	meta, err := e.GetKey(keyName)
	if err != nil {
		return nil, err
	}

	// Rotate via KMS (creates new version)
	if _, err := e.kms.RotateKey(ctx, keyName); err != nil {
		return nil, errors.Wrapf(err, errors.ErrInternal, "Engine.RotateKey", "key rotation failed")
	}

	// Update metadata
	e.mu.Lock()
	meta.Version++
	meta.RotatedAt = time.Now()
	if meta.ExpiresAt != nil {
		// Reset expiration based on original interval
		interval := meta.ExpiresAt.Sub(meta.CreatedAt)
		newExpiry := time.Now().Add(interval)
		meta.ExpiresAt = &newExpiry
	}
	e.mu.Unlock()

	if e.logger != nil {
		e.logger.WithFields(map[string]interface{}{
			"key":     keyName,
			"version": meta.Version,
		}).Info("Crypto key rotated")
	}

	return meta, nil
}

// ListKeys returns all key metadata.
func (e *Engine) ListKeys() []*KeyMetadata {
	e.mu.RLock()
	defer e.mu.RUnlock()

	keys := make([]*KeyMetadata, 0, len(e.keys))
	for _, meta := range e.keys {
		keys = append(keys, meta)
	}
	return keys
}

// DeleteKey removes a key from the engine.
func (e *Engine) DeleteKey(ctx context.Context, keyName string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if _, exists := e.keys[keyName]; !exists {
		return errors.NotFound("Engine.DeleteKey", keyName)
	}

	// Delete from KMS
	if err := e.kms.DeleteKey(ctx, keyName); err != nil {
		return errors.Wrapf(err, errors.ErrInternal, "Engine.DeleteKey", "failed to delete key")
	}

	delete(e.keys, keyName)

	if e.logger != nil {
		e.logger.WithField("key", keyName).Info("Crypto key deleted")
	}

	return nil
}

// Close releases resources.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.keys = make(map[string]*KeyMetadata)
	return nil
}

// algorithmToKeyAttributes converts an algorithm string to go-xkms KeyAttributes.
func (e *Engine) algorithmToKeyAttributes(keyName, algorithm string) *types.KeyAttributes {
	attrs := &types.KeyAttributes{
		CN: keyName,
	}

	// Default to AES-256-GCM for symmetric
	switch algorithm {
	case kms.AlgAES128GCM:
		attrs.SymmetricAlgorithm = types.SymmetricAES128GCM
		attrs.KeyType = types.KeyTypeEncryption
	case kms.AlgAES192GCM:
		attrs.SymmetricAlgorithm = types.SymmetricAES192GCM
		attrs.KeyType = types.KeyTypeEncryption
	case kms.AlgAES256GCM:
		attrs.SymmetricAlgorithm = types.SymmetricAES256GCM
		attrs.KeyType = types.KeyTypeEncryption
	case kms.AlgChaCha20Poly1305:
		attrs.SymmetricAlgorithm = types.SymmetricChaCha20Poly1305
		attrs.KeyType = types.KeyTypeEncryption
	default:
		// Default to AES-256-GCM
		attrs.SymmetricAlgorithm = types.SymmetricAES256GCM
		attrs.KeyType = types.KeyTypeEncryption
	}

	return attrs
}
