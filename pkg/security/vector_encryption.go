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

package security

import (
	"context"
	"encoding/binary"
	"errors"
	"math"
	"sync/atomic"

	xkms "github.com/jeremyhahn/go-xkms/sdk/go"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// Vector encryption format constants.
const (
	// VectorEncryptionVersion is the current encryption format version.
	VectorEncryptionVersion byte = 1

	// vectorNonceSize is the size of the nonce for AES-256-GCM (12 bytes).
	vectorNonceSize = 12

	// vectorAuthTagSize is the size of the GCM authentication tag (16 bytes).
	vectorAuthTagSize = 16

	// vectorMinEncryptedSize is the minimum size of encrypted data:
	// version (1) + nonce (12) + at least 4 bytes ciphertext + tag (16).
	vectorMinEncryptedSize = 1 + vectorNonceSize + 4 + vectorAuthTagSize

	// float32ByteSize is the size of a float32 in bytes.
	float32ByteSize = 4
)

// Vector encryption sentinel errors.
var (
	// ErrNilTenantDEKManager indicates the TenantDEKManager is nil.
	ErrNilTenantDEKManager = errors.New("tenant DEK manager cannot be nil")

	// ErrNilLogger indicates the logger is nil.
	ErrNilLogger = errors.New("logger cannot be nil")

	// ErrServiceClosed indicates the service has been closed.
	ErrServiceClosed = errors.New("vector encryption service is closed")

	// ErrEmptyTenantID indicates the tenant ID is empty.
	ErrEmptyTenantID = errors.New("tenant ID cannot be empty")

	// ErrEmptyNamespace indicates the namespace is empty.
	ErrEmptyNamespace = errors.New("namespace cannot be empty")

	// ErrEmptyVectorID indicates the vector ID is empty.
	ErrEmptyVectorID = errors.New("vector ID cannot be empty")

	// ErrEmptyEmbedding indicates the embedding is empty or nil.
	ErrEmptyEmbedding = errors.New("embedding cannot be empty")

	// ErrInvalidEncryptedData indicates the encrypted data is invalid or too short.
	ErrInvalidEncryptedData = errors.New("invalid encrypted data")

	// ErrUnsupportedEncryptionVersion indicates an unsupported encryption version.
	ErrUnsupportedEncryptionVersion = errors.New("unsupported encryption version")

	// ErrTenantDEKNotFound indicates the tenant DEK was not found.
	ErrTenantDEKNotFound = errors.New("tenant DEK not found")

	// ErrEncryptionFailed indicates encryption failed.
	ErrEncryptionFailed = errors.New("encryption failed")

	// ErrDecryptionFailed indicates decryption failed.
	ErrDecryptionFailed = errors.New("decryption failed")

	// ErrNonceGeneration indicates nonce generation failed.
	ErrNonceGeneration = errors.New("failed to generate nonce")

	// ErrCipherCreation indicates cipher creation failed.
	ErrCipherCreation = errors.New("failed to create cipher")

	// ErrGCMCreation indicates GCM creation failed.
	ErrGCMCreation = errors.New("failed to create GCM")
)

// VectorEncryptionError represents an error during vector encryption operations.
type VectorEncryptionError struct {
	// Op is the operation that failed.
	Op string

	// TenantID is the tenant associated with the operation.
	TenantID string

	// VectorID is the vector ID associated with the operation.
	VectorID string

	// Err is the underlying error.
	Err error
}

// Error implements the error interface.
func (e *VectorEncryptionError) Error() string {
	if e.Err != nil {
		return e.Op + ": " + e.Err.Error()
	}
	return e.Op
}

// Unwrap returns the underlying error.
func (e *VectorEncryptionError) Unwrap() error {
	return e.Err
}

// VectorEncryptionService handles encryption/decryption of vector data per tenant.
type VectorEncryptionService interface {
	// EncryptVector encrypts a vector embedding for storage.
	EncryptVector(ctx context.Context, tenantID, namespace, vectorID string, embedding []float32) ([]byte, error)

	// DecryptVector decrypts a vector embedding from storage.
	DecryptVector(ctx context.Context, tenantID, namespace, vectorID string, encryptedData []byte) ([]float32, error)

	// EncryptVectorBatch encrypts multiple vectors efficiently.
	EncryptVectorBatch(ctx context.Context, tenantID, namespace string, vectors map[string][]float32) (map[string][]byte, error)

	// DecryptVectorBatch decrypts multiple vectors efficiently.
	DecryptVectorBatch(ctx context.Context, tenantID, namespace string, encrypted map[string][]byte) (map[string][]float32, error)

	// IsEncrypted checks if data appears to be encrypted.
	IsEncrypted(data []byte) bool
}

// vectorEncryptionMetrics holds metrics for vector encryption operations.
type vectorEncryptionMetrics struct {
	encryptOps  atomic.Uint64
	decryptOps  atomic.Uint64
	encryptErrs atomic.Uint64
	decryptErrs atomic.Uint64
	bytesIn     atomic.Uint64
	bytesOut    atomic.Uint64
}

// vectorEncryptionService is the default implementation of VectorEncryptionService.
type vectorEncryptionService struct {
	tenantDEKMgr TenantDEKManager
	logger       interfaces.Logger

	// Metrics
	metrics *vectorEncryptionMetrics

	closed atomic.Bool
}

// VectorEncryptionServiceConfig contains configuration for VectorEncryptionService.
type VectorEncryptionServiceConfig struct {
	// EnableCipherCache enables caching of cipher instances per tenant.
	// Default: true
	EnableCipherCache bool
}

// NewVectorEncryptionService creates a new vector encryption service.
//
// Parameters:
//   - tenantDEKMgr: TenantDEKManager for per-tenant keys
//   - logger: Logger for structured logging
//   - config: Optional configuration (nil for defaults)
func NewVectorEncryptionService(
	tenantDEKMgr TenantDEKManager,
	logger interfaces.Logger,
	config *VectorEncryptionServiceConfig,
) (VectorEncryptionService, error) {
	if tenantDEKMgr == nil {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "NewVectorEncryptionService",
			Err:  ErrNilTenantDEKManager,
		}
	}
	if logger == nil {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "NewVectorEncryptionService",
			Err:  ErrNilLogger,
		}
	}

	svc := &vectorEncryptionService{
		tenantDEKMgr: tenantDEKMgr,
		logger:       logger.WithField("component", "vector_encryption"),
		metrics:      &vectorEncryptionMetrics{},
	}

	svc.logger.Info("vector encryption service created")

	return svc, nil
}

// EncryptVector encrypts a vector embedding for storage.
func (s *vectorEncryptionService) EncryptVector(
	ctx context.Context,
	tenantID, namespace, vectorID string,
	embedding []float32,
) ([]byte, error) {
	if s.closed.Load() {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrUnavailable,
			Op:   "VectorEncryptionService.EncryptVector",
			Err:  ErrServiceClosed,
		}
	}

	if err := s.validateEncryptInput(tenantID, namespace, vectorID, embedding); err != nil {
		return nil, err
	}

	s.metrics.encryptOps.Add(1)

	// Get or create tenant DEK
	dek, err := s.tenantDEKMgr.GetOrCreateTenantDEK(ctx, tenantID)
	if err != nil {
		s.metrics.encryptErrs.Add(1)
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrEncryption,
			Op:   "VectorEncryptionService.EncryptVector",
			Err:  err,
		}
	}

	// Defer wiping the DEK from memory
	defer wipeDEK(dek)

	// Convert embedding to bytes
	plaintext := float32SliceToBytes(embedding)
	s.metrics.bytesIn.Add(uint64(len(plaintext)))

	// Encrypt with AAD
	aad := buildAAD(namespace, tenantID, vectorID)
	ciphertext, err := s.encrypt(dek, plaintext, aad)
	if err != nil {
		s.metrics.encryptErrs.Add(1)
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrEncryption,
			Op:   "VectorEncryptionService.EncryptVector",
			Err:  err,
		}
	}

	s.metrics.bytesOut.Add(uint64(len(ciphertext)))

	s.logger.WithFields(map[string]interface{}{
		"tenant_id":       tenantID,
		"namespace":       namespace,
		"vector_id":       vectorID,
		"embedding_dims":  len(embedding),
		"encrypted_bytes": len(ciphertext),
	}).Debug("encrypted vector")

	return ciphertext, nil
}

// DecryptVector decrypts a vector embedding from storage.
func (s *vectorEncryptionService) DecryptVector(
	ctx context.Context,
	tenantID, namespace, vectorID string,
	encryptedData []byte,
) ([]float32, error) {
	if s.closed.Load() {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrUnavailable,
			Op:   "VectorEncryptionService.DecryptVector",
			Err:  ErrServiceClosed,
		}
	}

	if err := s.validateDecryptInput(tenantID, namespace, vectorID, encryptedData); err != nil {
		return nil, err
	}

	s.metrics.decryptOps.Add(1)

	// Check encryption version
	if encryptedData[0] != VectorEncryptionVersion {
		s.metrics.decryptErrs.Add(1)
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrDecryption,
			Op:   "VectorEncryptionService.DecryptVector",
			Err:  ErrUnsupportedEncryptionVersion,
		}
	}

	// Get tenant DEK
	dek, err := s.tenantDEKMgr.GetTenantDEK(ctx, tenantID)
	if err != nil {
		s.metrics.decryptErrs.Add(1)
		if dberrors.IsNotFound(err) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrDecryption,
				Op:   "VectorEncryptionService.DecryptVector",
				Err:  ErrTenantDEKNotFound,
			}
		}
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrDecryption,
			Op:   "VectorEncryptionService.DecryptVector",
			Err:  err,
		}
	}

	// Defer wiping the DEK from memory
	defer wipeDEK(dek)

	// Decrypt with AAD
	aad := buildAAD(namespace, tenantID, vectorID)
	plaintext, err := s.decrypt(dek, encryptedData, aad)
	if err != nil {
		s.metrics.decryptErrs.Add(1)
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrDecryption,
			Op:   "VectorEncryptionService.DecryptVector",
			Err:  err,
		}
	}

	// Convert bytes back to float32 slice
	embedding := bytesToFloat32Slice(plaintext)

	s.metrics.bytesIn.Add(uint64(len(encryptedData)))
	s.metrics.bytesOut.Add(uint64(len(plaintext)))

	s.logger.WithFields(map[string]interface{}{
		"tenant_id":      tenantID,
		"namespace":      namespace,
		"vector_id":      vectorID,
		"embedding_dims": len(embedding),
	}).Debug("decrypted vector")

	return embedding, nil
}

// EncryptVectorBatch encrypts multiple vectors efficiently.
func (s *vectorEncryptionService) EncryptVectorBatch(
	ctx context.Context,
	tenantID, namespace string,
	vectors map[string][]float32,
) (map[string][]byte, error) {
	if s.closed.Load() {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrUnavailable,
			Op:   "VectorEncryptionService.EncryptVectorBatch",
			Err:  ErrServiceClosed,
		}
	}

	if tenantID == "" {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "VectorEncryptionService.EncryptVectorBatch",
			Err:  ErrEmptyTenantID,
		}
	}

	if namespace == "" {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "VectorEncryptionService.EncryptVectorBatch",
			Err:  ErrEmptyNamespace,
		}
	}

	if len(vectors) == 0 {
		return make(map[string][]byte), nil
	}

	// Get or create tenant DEK once for the batch
	dek, err := s.tenantDEKMgr.GetOrCreateTenantDEK(ctx, tenantID)
	if err != nil {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrEncryption,
			Op:   "VectorEncryptionService.EncryptVectorBatch",
			Err:  err,
		}
	}

	// Defer wiping the DEK from memory
	defer wipeDEK(dek)

	results := make(map[string][]byte, len(vectors))
	var totalBytesIn, totalBytesOut uint64

	for vectorID, embedding := range vectors {
		if embedding == nil {
			continue
		}

		// Convert embedding to bytes
		plaintext := float32SliceToBytes(embedding)
		totalBytesIn += uint64(len(plaintext))

		// Build AAD for this vector
		aad := buildAAD(namespace, tenantID, vectorID)

		// Encrypt using xkms AES-256-GCM with AAD, then prepend version byte
		ciphertext, err := s.encryptWithKey(dek, plaintext, aad)
		if err != nil {
			s.metrics.encryptErrs.Add(1)
			return nil, &VectorEncryptionError{
				Op:       "VectorEncryptionService.EncryptVectorBatch",
				TenantID: tenantID,
				VectorID: vectorID,
				Err:      err,
			}
		}

		results[vectorID] = ciphertext
		totalBytesOut += uint64(len(ciphertext))
	}

	s.metrics.encryptOps.Add(uint64(len(results)))
	s.metrics.bytesIn.Add(totalBytesIn)
	s.metrics.bytesOut.Add(totalBytesOut)

	s.logger.WithFields(map[string]interface{}{
		"tenant_id":  tenantID,
		"namespace":  namespace,
		"batch_size": len(results),
	}).Debug("encrypted vector batch")

	return results, nil
}

// DecryptVectorBatch decrypts multiple vectors efficiently.
func (s *vectorEncryptionService) DecryptVectorBatch(
	ctx context.Context,
	tenantID, namespace string,
	encrypted map[string][]byte,
) (map[string][]float32, error) {
	if s.closed.Load() {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrUnavailable,
			Op:   "VectorEncryptionService.DecryptVectorBatch",
			Err:  ErrServiceClosed,
		}
	}

	if tenantID == "" {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "VectorEncryptionService.DecryptVectorBatch",
			Err:  ErrEmptyTenantID,
		}
	}

	if namespace == "" {
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "VectorEncryptionService.DecryptVectorBatch",
			Err:  ErrEmptyNamespace,
		}
	}

	if len(encrypted) == 0 {
		return make(map[string][]float32), nil
	}

	// Get tenant DEK once for the batch
	dek, err := s.tenantDEKMgr.GetTenantDEK(ctx, tenantID)
	if err != nil {
		if dberrors.IsNotFound(err) {
			return nil, &dberrors.DragonError{
				Code: dberrors.ErrDecryption,
				Op:   "VectorEncryptionService.DecryptVectorBatch",
				Err:  ErrTenantDEKNotFound,
			}
		}
		return nil, &dberrors.DragonError{
			Code: dberrors.ErrDecryption,
			Op:   "VectorEncryptionService.DecryptVectorBatch",
			Err:  err,
		}
	}

	// Defer wiping the DEK from memory
	defer wipeDEK(dek)

	results := make(map[string][]float32, len(encrypted))
	var totalBytesIn, totalBytesOut uint64

	for vectorID, ciphertext := range encrypted {
		if len(ciphertext) < vectorMinEncryptedSize {
			continue
		}

		totalBytesIn += uint64(len(ciphertext))

		// Check encryption version
		if ciphertext[0] != VectorEncryptionVersion {
			s.metrics.decryptErrs.Add(1)
			return nil, &VectorEncryptionError{
				Op:       "VectorEncryptionService.DecryptVectorBatch",
				TenantID: tenantID,
				VectorID: vectorID,
				Err:      ErrUnsupportedEncryptionVersion,
			}
		}

		// Build AAD for this vector
		aad := buildAAD(namespace, tenantID, vectorID)

		// Decrypt using xkms AES-256-GCM with AAD, stripping version byte
		plaintext, err := s.decryptWithKey(dek, ciphertext, aad)
		if err != nil {
			s.metrics.decryptErrs.Add(1)
			return nil, &VectorEncryptionError{
				Op:       "VectorEncryptionService.DecryptVectorBatch",
				TenantID: tenantID,
				VectorID: vectorID,
				Err:      err,
			}
		}

		// Convert bytes back to float32 slice
		embedding := bytesToFloat32Slice(plaintext)
		results[vectorID] = embedding
		totalBytesOut += uint64(len(plaintext))
	}

	s.metrics.decryptOps.Add(uint64(len(results)))
	s.metrics.bytesIn.Add(totalBytesIn)
	s.metrics.bytesOut.Add(totalBytesOut)

	s.logger.WithFields(map[string]interface{}{
		"tenant_id":  tenantID,
		"namespace":  namespace,
		"batch_size": len(results),
	}).Debug("decrypted vector batch")

	return results, nil
}

// IsEncrypted checks if data appears to be encrypted.
func (s *vectorEncryptionService) IsEncrypted(data []byte) bool {
	if len(data) < vectorMinEncryptedSize {
		return false
	}

	// Check for valid version byte
	version := data[0]
	if version == 0 || version > VectorEncryptionVersion {
		return false
	}

	// Verify the data length is consistent with encrypted format
	// After removing version + nonce + tag, remaining should be multiple of 4 (float32)
	payloadLen := len(data) - 1 - vectorNonceSize - vectorAuthTagSize
	if payloadLen <= 0 || payloadLen%float32ByteSize != 0 {
		return false
	}

	return true
}

// validateEncryptInput validates input parameters for encryption.
func (s *vectorEncryptionService) validateEncryptInput(tenantID, namespace, vectorID string, embedding []float32) error {
	if tenantID == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "VectorEncryptionService.EncryptVector",
			Err:  ErrEmptyTenantID,
		}
	}

	if namespace == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "VectorEncryptionService.EncryptVector",
			Err:  ErrEmptyNamespace,
		}
	}

	if vectorID == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "VectorEncryptionService.EncryptVector",
			Err:  ErrEmptyVectorID,
		}
	}

	if len(embedding) == 0 {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "VectorEncryptionService.EncryptVector",
			Err:  ErrEmptyEmbedding,
		}
	}

	return nil
}

// validateDecryptInput validates input parameters for decryption.
func (s *vectorEncryptionService) validateDecryptInput(tenantID, namespace, vectorID string, encryptedData []byte) error {
	if tenantID == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "VectorEncryptionService.DecryptVector",
			Err:  ErrEmptyTenantID,
		}
	}

	if namespace == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "VectorEncryptionService.DecryptVector",
			Err:  ErrEmptyNamespace,
		}
	}

	if vectorID == "" {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "VectorEncryptionService.DecryptVector",
			Err:  ErrEmptyVectorID,
		}
	}

	if len(encryptedData) < vectorMinEncryptedSize {
		return &dberrors.DragonError{
			Code: dberrors.ErrInvalidInput,
			Op:   "VectorEncryptionService.DecryptVector",
			Err:  ErrInvalidEncryptedData,
		}
	}

	return nil
}

// encrypt encrypts plaintext using AES-256-GCM with the given DEK and AAD.
// Output format: [version:1][nonce:12][ciphertext+tag].
func (s *vectorEncryptionService) encrypt(dek, plaintext, aad []byte) ([]byte, error) {
	return s.encryptWithKey(dek, plaintext, aad)
}

// encryptWithKey encrypts plaintext using xkms AES-256-GCM and prepends the version byte.
// Output format: [version:1][nonce:12][ciphertext+tag].
func (s *vectorEncryptionService) encryptWithKey(dek, plaintext, aad []byte) ([]byte, error) {
	// xkms AESGCMEncryptWithAAD returns [nonce:12][ciphertext+tag]
	sealed, err := xkms.AESGCMEncryptWithAAD(dek, plaintext, aad)
	if err != nil {
		return nil, ErrEncryptionFailed
	}

	// Prepend version byte: [version:1][nonce:12][ciphertext+tag]
	result := make([]byte, 1+len(sealed))
	result[0] = VectorEncryptionVersion
	copy(result[1:], sealed)

	return result, nil
}

// decrypt decrypts ciphertext using AES-256-GCM with the given DEK and AAD.
// Input format: [version:1][nonce:12][ciphertext+tag].
func (s *vectorEncryptionService) decrypt(dek, encryptedData, aad []byte) ([]byte, error) {
	return s.decryptWithKey(dek, encryptedData, aad)
}

// decryptWithKey decrypts using xkms AES-256-GCM after stripping the version byte.
// Input format: [version:1][nonce:12][ciphertext+tag].
func (s *vectorEncryptionService) decryptWithKey(dek, encryptedData, aad []byte) ([]byte, error) {
	// Strip the version byte; remainder is [nonce:12][ciphertext+tag]
	sealed := encryptedData[1:]

	plaintext, err := xkms.AESGCMDecryptWithAAD(dek, sealed, aad)
	if err != nil {
		return nil, ErrDecryptionFailed
	}

	return plaintext, nil
}

// buildAAD constructs the Additional Authenticated Data string.
// Format: {namespace}:{tenantID}:{vectorID}.
func buildAAD(namespace, tenantID, vectorID string) []byte {
	return []byte(namespace + ":" + tenantID + ":" + vectorID)
}

// float32SliceToBytes converts a slice of float32 to bytes using little-endian encoding.
func float32SliceToBytes(floats []float32) []byte {
	buf := make([]byte, len(floats)*float32ByteSize)
	for i, f := range floats {
		binary.LittleEndian.PutUint32(buf[i*float32ByteSize:], math.Float32bits(f))
	}
	return buf
}

// bytesToFloat32Slice converts bytes to a slice of float32 using little-endian encoding.
func bytesToFloat32Slice(data []byte) []float32 {
	if len(data)%float32ByteSize != 0 {
		return nil
	}
	floats := make([]float32, len(data)/float32ByteSize)
	for i := range floats {
		floats[i] = math.Float32frombits(binary.LittleEndian.Uint32(data[i*float32ByteSize:]))
	}
	return floats
}

// wipeDEK securely wipes a DEK from memory.
func wipeDEK(dek []byte) {
	xkms.SecureMemZero(dek)
}
