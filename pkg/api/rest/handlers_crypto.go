//go:build rest

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

package rest

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// Data Key Request/Response Types for REST API
// =============================================================================

// DataKeyRESTRequest represents the JSON request body for data key generation.
type DataKeyRESTRequest struct {
	// KeySizeBits is the size of the data key in bits (default: 256)
	KeySizeBits int `json:"key_size_bits,omitempty"`

	// Algorithm specifies the encryption algorithm (e.g., "AES-256-GCM")
	Algorithm string `json:"algorithm,omitempty"`

	// Context is optional additional authenticated data
	Context []byte `json:"context,omitempty"`
}

// DataKeyRESTResponse represents the JSON response for data key generation.
type DataKeyRESTResponse struct {
	// KeyID is the KEK identifier used for wrapping
	KeyID string `json:"key_id"`

	// Plaintext is the plaintext data key (handle with care!)
	Plaintext []byte `json:"plaintext"`

	// CiphertextBlob is the encrypted data key
	CiphertextBlob []byte `json:"ciphertext_blob"`

	// Timestamp is when the data key was generated
	Timestamp int64 `json:"timestamp"`
}

// HandleCryptoEncrypt handles POST /v1/crypto/encrypt/{name} - Encrypt.
func (s *Server) HandleCryptoEncrypt(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyName := chi.URLParam(r, "name")
	if keyName == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody struct {
		Plaintext []byte `json:"plaintext"`
		Context   []byte `json:"context,omitempty"`
		Nonce     []byte `json:"nonce,omitempty"`
	}

	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(reqBody.Plaintext) == 0 {
		RespondBadRequest(w, r, "plaintext is required")
		return
	}

	req := &service.EncryptRequest{
		KeyID:     keyName,
		Plaintext: reqBody.Plaintext,
		Context:   reqBody.Context,
		Nonce:     reqBody.Nonce,
	}

	cryptoService := s.service.(interface {
		Encrypt(ctx context.Context, req *service.EncryptRequest) (*service.EncryptResponse, error)
	})

	resp, err := cryptoService.Encrypt(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCryptoDecrypt handles POST /v1/crypto/decrypt/{name} - Decrypt.
func (s *Server) HandleCryptoDecrypt(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyName := chi.URLParam(r, "name")
	if keyName == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody struct {
		Ciphertext []byte `json:"ciphertext"`
		Context    []byte `json:"context,omitempty"`
		Nonce      []byte `json:"nonce,omitempty"`
		KeyVersion uint64 `json:"key_version,omitempty"`
	}

	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(reqBody.Ciphertext) == 0 {
		RespondBadRequest(w, r, "ciphertext is required")
		return
	}

	req := &service.DecryptRequest{
		KeyID:      keyName,
		Ciphertext: reqBody.Ciphertext,
		Context:    reqBody.Context,
		Nonce:      reqBody.Nonce,
		KeyVersion: reqBody.KeyVersion,
	}

	cryptoService := s.service.(interface {
		Decrypt(ctx context.Context, req *service.DecryptRequest) (*service.DecryptResponse, error)
	})

	resp, err := cryptoService.Decrypt(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCryptoRewrap handles POST /v1/crypto/rewrap/{name} - Re-encrypt with latest key.
func (s *Server) HandleCryptoRewrap(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyName := chi.URLParam(r, "name")
	if keyName == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody struct {
		Ciphertext []byte `json:"ciphertext"`
		Context    []byte `json:"context,omitempty"`
		Nonce      []byte `json:"nonce,omitempty"`
	}

	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(reqBody.Ciphertext) == 0 {
		RespondBadRequest(w, r, "ciphertext is required")
		return
	}

	req := &service.RewrapRequest{
		KeyID:      keyName,
		Ciphertext: reqBody.Ciphertext,
		Context:    reqBody.Context,
		Nonce:      reqBody.Nonce,
	}

	cryptoService := s.service.(interface {
		Rewrap(ctx context.Context, req *service.RewrapRequest) (*service.RewrapResponse, error)
	})

	resp, err := cryptoService.Rewrap(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCryptoSign handles POST /v1/crypto/sign/{name} - Sign data.
func (s *Server) HandleCryptoSign(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyName := chi.URLParam(r, "name")
	if keyName == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody struct {
		Message       []byte                `json:"message"`
		HashAlgorithm service.HashAlgorithm `json:"hash_algorithm,omitempty"`
		PreHashed     bool                  `json:"pre_hashed,omitempty"`
	}

	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(reqBody.Message) == 0 {
		RespondBadRequest(w, r, "message is required")
		return
	}

	req := &service.SignRequest{
		KeyID:         keyName,
		Message:       reqBody.Message,
		HashAlgorithm: reqBody.HashAlgorithm,
		PreHashed:     reqBody.PreHashed,
	}

	cryptoService := s.service.(interface {
		Sign(ctx context.Context, req *service.SignRequest) (*service.SignResponse, error)
	})

	resp, err := cryptoService.Sign(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCryptoVerify handles POST /v1/crypto/verify/{name} - Verify signature.
func (s *Server) HandleCryptoVerify(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyName := chi.URLParam(r, "name")
	if keyName == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody struct {
		Message       []byte                `json:"message"`
		Signature     []byte                `json:"signature"`
		HashAlgorithm service.HashAlgorithm `json:"hash_algorithm,omitempty"`
		KeyVersion    uint64                `json:"key_version,omitempty"`
	}

	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(reqBody.Message) == 0 {
		RespondBadRequest(w, r, "message is required")
		return
	}

	if len(reqBody.Signature) == 0 {
		RespondBadRequest(w, r, "signature is required")
		return
	}

	req := &service.VerifyRequest{
		KeyID:         keyName,
		Message:       reqBody.Message,
		Signature:     reqBody.Signature,
		HashAlgorithm: reqBody.HashAlgorithm,
		KeyVersion:    reqBody.KeyVersion,
	}

	cryptoService := s.service.(interface {
		Verify(ctx context.Context, req *service.VerifyRequest) (*service.VerifyResponse, error)
	})

	resp, err := cryptoService.Verify(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCryptoHash handles POST /v1/crypto/hash - Hash data.
func (s *Server) HandleCryptoHash(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody struct {
		Data      []byte                `json:"data"`
		Algorithm service.HashAlgorithm `json:"algorithm"`
	}

	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(reqBody.Data) == 0 {
		RespondBadRequest(w, r, "data is required")
		return
	}

	if reqBody.Algorithm == "" {
		reqBody.Algorithm = service.HashAlgSHA256
	}

	req := &service.HashRequest{
		Data:      reqBody.Data,
		Algorithm: reqBody.Algorithm,
	}

	cryptoService := s.service.(interface {
		Hash(ctx context.Context, req *service.HashRequest) (*service.HashResponse, error)
	})

	resp, err := cryptoService.Hash(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCryptoRandom handles POST /v1/crypto/random - Generate random bytes.
func (s *Server) HandleCryptoRandom(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil && !stderrors.Is(err, io.EOF) {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	numBytes := 32
	if len(body) > 0 {
		var reqBody struct {
			NumBytes int `json:"num_bytes"`
		}

		if err := json.Unmarshal(body, &reqBody); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}

		if reqBody.NumBytes > 0 {
			numBytes = reqBody.NumBytes
		}
	} else {
		bytesParam := r.URL.Query().Get("bytes")
		if bytesParam != "" {
			if n, err := strconv.Atoi(bytesParam); err == nil && n > 0 {
				numBytes = n
			}
		}
	}

	if numBytes <= 0 || numBytes > 1024*1024 {
		RespondBadRequest(w, r, "num_bytes must be between 1 and 1048576")
		return
	}

	req := &service.GenerateRandomRequest{
		NumBytes: numBytes,
	}

	cryptoService := s.service.(interface {
		GenerateRandom(ctx context.Context, req *service.GenerateRandomRequest) (*service.GenerateRandomResponse, error)
	})

	resp, err := cryptoService.GenerateRandom(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCryptoDataKey handles POST /v1/crypto/datakey/{name} - Generate data encryption key.
// This handler calls KMSManager().GenerateDataKey() directly using KeyAttributes.
func (s *Server) HandleCryptoDataKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyName := chi.URLParam(r, "name")
	if keyName == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody DataKeyRESTRequest
	if len(body) > 0 {
		if err := json.Unmarshal(body, &reqBody); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	// Default to 256 bits (32 bytes) for AES-256
	keyBytes := 32
	if reqBody.KeySizeBits > 0 {
		keyBytes = reqBody.KeySizeBits / 8
	}

	// Validate key size
	if keyBytes <= 0 || keyBytes > 256 {
		RespondBadRequest(w, r, "key_size_bits must be between 8 and 2048")
		return
	}

	// Get KMS manager
	kmsMgr := s.service.KMSManager()
	if kmsMgr == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	// Build KeyAttributes - CN specifies the KEK (key encryption key) for wrapping
	attrs := &kctypes.KeyAttributes{
		CN: keyName,
	}

	// Generate data key using KMS manager directly
	plaintext, wrapped, err := kmsMgr.GenerateDataKey(ctx, attrs, keyBytes)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	now := time.Now()

	resp := &DataKeyRESTResponse{
		KeyID:          keyName,
		Plaintext:      plaintext,
		CiphertextBlob: wrapped,
		Timestamp:      now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCryptoBatchEncrypt handles POST /v1/crypto/encrypt/{name}/batch - Batch encrypt.
func (s *Server) HandleCryptoBatchEncrypt(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyName := chi.URLParam(r, "name")
	if keyName == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody struct {
		Items []service.BatchEncryptItem `json:"items"`
	}

	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(reqBody.Items) == 0 {
		RespondBadRequest(w, r, "items are required")
		return
	}

	req := &service.EncryptBatchRequest{
		KeyID: keyName,
		Items: reqBody.Items,
	}

	cryptoService := s.service.(interface {
		EncryptBatch(ctx context.Context, req *service.EncryptBatchRequest) (*service.EncryptBatchResponse, error)
	})

	resp, err := cryptoService.EncryptBatch(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleCryptoBatchDecrypt handles POST /v1/crypto/decrypt/{name}/batch - Batch decrypt.
func (s *Server) HandleCryptoBatchDecrypt(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyName := chi.URLParam(r, "name")
	if keyName == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody struct {
		Items []service.BatchDecryptItem `json:"items"`
	}

	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(reqBody.Items) == 0 {
		RespondBadRequest(w, r, "items are required")
		return
	}

	req := &service.DecryptBatchRequest{
		KeyID: keyName,
		Items: reqBody.Items,
	}

	cryptoService := s.service.(interface {
		DecryptBatch(ctx context.Context, req *service.DecryptBatchRequest) (*service.DecryptBatchResponse, error)
	})

	resp, err := cryptoService.DecryptBatch(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}
