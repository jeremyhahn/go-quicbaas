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
	"crypto/elliptic"
	"crypto/x509"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/kms"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// kmsListServiceFetchLimit is the upper bound the REST list handler passes to
// the KMS service. The handler must filter system keys after the service
// returns; passing the caller's limit directly would let the service truncate
// before the filter ran and produce an empty operator-visible page (see
// TestRESTKeyListing/list_with_limit). 10000 is well above any realistic
// per-tenant key count while still bounding memory.
const kmsListServiceFetchLimit = 10000

// isSystemKey returns true for keys that are internal cryptographic materials
// managed by the daemon itself (CA, TLS server certs, agent identity certs).
// These keys are not operator-managed and are excluded from the KMS list API
// so the response only contains keys the operator explicitly created.
//
// A key is classified as a system key when ANY of the following are true:
//   - algorithm is empty (all CA/TLS/cert keys use the raw certificate store)
//   - key_id matches a well-known system name ("QuicBaaS Root CA", "localhost", "127.0.0.1")
//   - key_id starts with "agent-embedded-" (embedded-agent identity certs)
func isSystemKey(keyID, algorithm string) bool {
	if algorithm == "" {
		return true
	}
	switch keyID {
	case "QuicBaaS Root CA", "localhost", "127.0.0.1":
		return true
	}
	return strings.HasPrefix(keyID, "agent-embedded-")
}

// =============================================================================
// Request/Response Types for REST API
// =============================================================================

// KeyAttributesRequest represents key attributes in REST request.
// This maps directly to kctypes.KeyAttributes fields that are relevant for REST API.
type KeyAttributesRequest struct {
	// Algorithm specifies the cryptographic algorithm (e.g., "AES-256-GCM", "RSA-2048", "ECDSA-P256", "ED25519")
	Algorithm string `json:"algorithm"`

	// KeySize is the key size in bits (e.g., 256 for AES-256, 2048 for RSA-2048)
	// This is derived from Algorithm but can be explicitly specified
	KeySize int `json:"key_size,omitempty"`

	// KeyUsage specifies allowed operations (e.g., ["encrypt", "decrypt", "sign", "verify"])
	KeyUsage []string `json:"key_usage"`

	// Exportable indicates whether the key can be exported
	Exportable bool `json:"exportable,omitempty"`

	// DeletionProtection prevents accidental deletion of the key
	DeletionProtection bool `json:"deletion_protection,omitempty"`

	// AutoRotate enables automatic key rotation (stored in key metadata, not KeyAttributes)
	AutoRotate bool `json:"auto_rotate,omitempty"`

	// RotationPeriod is the duration between automatic rotations (e.g., "720h" for 30 days)
	// Stored in key metadata, not KeyAttributes
	RotationPeriod string `json:"rotation_period,omitempty"`

	// ExpiresAt is when the key expires (RFC3339 format)
	// Stored in key metadata, not KeyAttributes
	ExpiresAt string `json:"expires_at,omitempty"`

	// Labels are key-value pairs for organization and filtering
	// Stored in key metadata, not KeyAttributes
	Labels map[string]string `json:"labels,omitempty"`
}

// GenerateKeyRESTRequest represents the JSON request body for key generation.
type GenerateKeyRESTRequest struct {
	Name          string                `json:"name,omitempty"`
	Description   string                `json:"description,omitempty"`
	KeyAttributes *KeyAttributesRequest `json:"key_attributes"`
	Tags          map[string]string     `json:"tags,omitempty"`
	Owner         string                `json:"owner,omitempty"`
}

// ImportKeyRESTRequest represents the JSON request body for key import.
type ImportKeyRESTRequest struct {
	KeyMaterial   []byte                `json:"key_material"`
	WrappingKeyID string                `json:"wrapping_key_id"`
	KeyAttributes *KeyAttributesRequest `json:"key_attributes,omitempty"`
	Name          string                `json:"name,omitempty"`
	Description   string                `json:"description,omitempty"`
	Tags          map[string]string     `json:"tags,omitempty"`
}

// GenerateKeyRESTResponse represents the JSON response for key generation.
type GenerateKeyRESTResponse struct {
	Key       *KeyHandleResponse   `json:"key"`
	Metadata  *KeyMetadataResponse `json:"metadata,omitempty"`
	Timestamp int64                `json:"timestamp"`
}

// KeyHandleResponse represents key handle in REST response.
type KeyHandleResponse struct {
	KeyID     string `json:"key_id"`
	Version   int    `json:"version"`
	Algorithm string `json:"algorithm"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
}

// KeyMetadataResponse represents key metadata in REST response.
type KeyMetadataResponse struct {
	KeyID              string            `json:"key_id"`
	Name               string            `json:"name,omitempty"`
	Description        string            `json:"description,omitempty"`
	Algorithm          string            `json:"algorithm"`
	Status             string            `json:"status"`
	Usage              []string          `json:"usage,omitempty"`
	Version            int               `json:"version"`
	Exportable         bool              `json:"exportable"`
	DeletionProtection bool              `json:"deletion_protection"`
	CreatedAt          int64             `json:"created_at"`
	UpdatedAt          int64             `json:"updated_at"`
	LastUsedAt         int64             `json:"last_used_at,omitempty"`
	Tags               map[string]string `json:"tags,omitempty"`
	Owner              string            `json:"owner,omitempty"`
}

// GetKeyRESTResponse represents the JSON response for getting a key.
type GetKeyRESTResponse struct {
	Key       *KeyHandleResponse   `json:"key"`
	Metadata  *KeyMetadataResponse `json:"metadata,omitempty"`
	Timestamp int64                `json:"timestamp"`
}

// ListKeysRESTResponse represents the JSON response for listing keys.
type ListKeysRESTResponse struct {
	Keys       []*KeyHandleResponse `json:"keys"`
	NextCursor string               `json:"next_cursor,omitempty"`
	HasMore    bool                 `json:"has_more"`
	TotalCount int                  `json:"total_count"`
	Timestamp  int64                `json:"timestamp"`
}

// RotateKeyRESTResponse represents the JSON response for rotating a key.
type RotateKeyRESTResponse struct {
	KeyID      string `json:"key_id"`
	OldVersion int    `json:"old_version"`
	NewVersion int    `json:"new_version"`
	Timestamp  int64  `json:"timestamp"`
}

// DeleteKeyRESTResponse represents the JSON response for deleting a key.
type DeleteKeyRESTResponse struct {
	KeyID        string `json:"key_id"`
	Status       string `json:"status"`
	DeletionDate int64  `json:"deletion_date,omitempty"`
	Timestamp    int64  `json:"timestamp"`
}

// UpdateKeyStatusRESTRequest represents the JSON request for updating key status.
type UpdateKeyStatusRESTRequest struct {
	Status string `json:"status"`
}

// UpdateKeyStatusRESTResponse represents the JSON response for updating key status.
type UpdateKeyStatusRESTResponse struct {
	KeyID     string `json:"key_id"`
	Status    string `json:"status"`
	Timestamp int64  `json:"timestamp"`
}

// ListKeyVersionsRESTResponse represents the JSON response for listing key versions.
type ListKeyVersionsRESTResponse struct {
	KeyID      string                `json:"key_id"`
	Versions   []*KeyVersionResponse `json:"versions"`
	NextCursor string                `json:"next_cursor,omitempty"`
	HasMore    bool                  `json:"has_more"`
	Timestamp  int64                 `json:"timestamp"`
}

// KeyVersionResponse represents a key version in REST response.
type KeyVersionResponse struct {
	Version   int    `json:"version"`
	Status    string `json:"status"`
	Algorithm string `json:"algorithm"`
	CreatedAt int64  `json:"created_at"`
}

// BackendStatusRESTResponse represents the JSON response for backend status.
type BackendStatusRESTResponse struct {
	BackendType  string          `json:"backend_type"`
	Status       string          `json:"status"`
	Capabilities map[string]bool `json:"capabilities"`
	Timestamp    int64           `json:"timestamp"`
}

// BackendTestRESTResponse represents the JSON response for backend test.
type BackendTestRESTResponse struct {
	Success    bool   `json:"success"`
	DurationNs int64  `json:"duration_ns"`
	Message    string `json:"message"`
	Timestamp  int64  `json:"timestamp"`
}

// EncryptRESTResponse represents the JSON response for encryption.
type EncryptRESTResponse struct {
	KeyID      string `json:"key_id"`
	KeyVersion uint64 `json:"key_version"`
	Ciphertext []byte `json:"ciphertext"`
	Nonce      []byte `json:"nonce,omitempty"`
	Timestamp  int64  `json:"timestamp"`
}

// DecryptRESTResponse represents the JSON response for decryption.
type DecryptRESTResponse struct {
	KeyID      string `json:"key_id"`
	KeyVersion uint64 `json:"key_version"`
	Plaintext  []byte `json:"plaintext"`
	Timestamp  int64  `json:"timestamp"`
}

// ExportKeyRESTResponse represents the JSON response for key export.
type ExportKeyRESTResponse struct {
	KeyID             string `json:"key_id"`
	Version           uint64 `json:"version,omitempty"`
	WrappedKey        []byte `json:"wrapped_key"`
	WrappingKeyID     string `json:"wrapping_key_id"`
	WrappingAlgorithm string `json:"wrapping_algorithm"`
	Format            string `json:"format"`
	Timestamp         int64  `json:"timestamp"`
}

// UnwrapKeyRESTResponse represents the JSON response for key unwrap.
type UnwrapKeyRESTResponse struct {
	KeyID     string             `json:"key_id"`
	Version   uint64             `json:"version"`
	Algorithm string             `json:"algorithm"`
	Status    string             `json:"status"`
	Key       *KeyHandleResponse `json:"key,omitempty"`
	Timestamp int64              `json:"timestamp"`
}

// SetKeySafetyRESTResponse represents the JSON response for setting key safety.
type SetKeySafetyRESTResponse struct {
	KeyID     string                    `json:"key_id"`
	Config    *AEADSafetyConfigResponse `json:"config"`
	Timestamp int64                     `json:"timestamp"`
}

// AEADSafetyConfigResponse represents AEAD safety configuration in REST response.
type AEADSafetyConfigResponse struct {
	MaxOperations     uint64 `json:"max_operations,omitempty"`
	MaxBytes          uint64 `json:"max_bytes,omitempty"`
	WarningPercent    int    `json:"warning_percent,omitempty"`
	AutoRotatePercent int    `json:"auto_rotate_percent,omitempty"`
}

// ClearKeySafetyRESTResponse represents the JSON response for clearing key safety.
type ClearKeySafetyRESTResponse struct {
	KeyID     string `json:"key_id"`
	Timestamp int64  `json:"timestamp"`
}

// SafetyCheckItemResponse represents a safety check item in REST response.
type SafetyCheckItemResponse struct {
	ID             string  `json:"id"`
	Type           string  `json:"type"`
	OperationCount uint64  `json:"operation_count"`
	BytesProcessed uint64  `json:"bytes_processed"`
	UsagePercent   float64 `json:"usage_percent"`
	MaxOperations  uint64  `json:"max_operations"`
	MaxBytes       uint64  `json:"max_bytes"`
}

// SafetyCheckRESTResponse represents the JSON response for safety check.
type SafetyCheckRESTResponse struct {
	Items            []SafetyCheckItemResponse `json:"items"`
	ThresholdPercent float64                   `json:"threshold_percent"`
	TotalKeysChecked int                       `json:"total_keys_checked"`
	TotalDEKsChecked int                       `json:"total_deks_checked"`
	Timestamp        int64                     `json:"timestamp"`
}

// AEADServerConfigResponse represents server-wide AEAD configuration in REST response.
type AEADServerConfigResponse struct {
	Enforce           bool   `json:"enforce"`
	MaxOperations     uint64 `json:"max_operations"`
	MaxBytes          uint64 `json:"max_bytes"`
	WarningPercent    int    `json:"warning_percent"`
	AutoRotatePercent int    `json:"auto_rotate_percent"`
}

// SafetyReportRESTResponse represents the JSON response for safety report.
type SafetyReportRESTResponse struct {
	Items               []SafetyCheckItemResponse `json:"items"`
	TotalOperations     uint64                    `json:"total_operations"`
	TotalBytesProcessed uint64                    `json:"total_bytes_processed"`
	ServerConfig        AEADServerConfigResponse  `json:"server_config"`
	Timestamp           int64                     `json:"timestamp"`
}

// KeyPolicyResponse represents key policy in REST response.
type KeyPolicyResponse struct {
	KeyID             string   `json:"key_id"`
	Version           uint64   `json:"version"`
	AllowedOperations []string `json:"allowed_operations"`
	AllowedPrincipals []string `json:"allowed_principals,omitempty"`
	DeniedPrincipals  []string `json:"denied_principals,omitempty"`
	ExpiresAt         int64    `json:"expires_at,omitempty"`
	RotationRequired  bool     `json:"rotation_required"`
	MaxUsageCount     uint64   `json:"max_usage_count,omitempty"`
	RequireMFA        bool     `json:"require_mfa"`
	UpdatedAt         int64    `json:"updated_at"`
}

// SetKeyPolicyRESTResponse represents the JSON response for setting key policy.
type SetKeyPolicyRESTResponse struct {
	KeyID         string `json:"key_id"`
	PolicyVersion uint64 `json:"policy_version"`
	Timestamp     int64  `json:"timestamp"`
}

// GetKeyPolicyRESTResponse represents the JSON response for getting key policy.
type GetKeyPolicyRESTResponse struct {
	Policy    *KeyPolicyResponse `json:"policy"`
	Timestamp int64              `json:"timestamp"`
}

// RotationPolicyRESTRequest is the JSON body for set-rotation-policy.
type RotationPolicyRESTRequest struct {
	Enabled         bool  `json:"enabled"`
	IntervalSeconds int64 `json:"interval_seconds"`
	RetainVersions  int   `json:"retain_versions,omitempty"`
	AutoRotate      bool  `json:"auto_rotate"`
}

// RotationPolicyRESTResponse represents a rotation policy in REST responses.
type RotationPolicyRESTResponse struct {
	Enabled          bool  `json:"enabled"`
	IntervalSeconds  int64 `json:"interval_seconds"`
	NextRotationUnix int64 `json:"next_rotation_unix,omitempty"`
	RetainVersions   int   `json:"retain_versions,omitempty"`
	AutoRotate       bool  `json:"auto_rotate"`
}

// SetKeyRotationPolicyRESTResponse is the JSON response for setting a rotation policy.
type SetKeyRotationPolicyRESTResponse struct {
	KeyID     string                      `json:"key_id"`
	Policy    *RotationPolicyRESTResponse `json:"policy"`
	Timestamp int64                       `json:"timestamp"`
}

// GetKeyRotationPolicyRESTResponse is the JSON response for getting a rotation policy.
type GetKeyRotationPolicyRESTResponse struct {
	KeyID     string                      `json:"key_id"`
	Policy    *RotationPolicyRESTResponse `json:"policy,omitempty"`
	Timestamp int64                       `json:"timestamp"`
}

// DeleteKeyRotationPolicyRESTResponse is the JSON response for deleting a rotation policy.
type DeleteKeyRotationPolicyRESTResponse struct {
	KeyID     string `json:"key_id"`
	Timestamp int64  `json:"timestamp"`
}

// =============================================================================
// Handler Functions
// =============================================================================

// HandleGenerateKey handles POST /v1/kms/keys - Generate new key.
func (s *Server) HandleGenerateKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req GenerateKeyRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if req.KeyAttributes == nil || req.KeyAttributes.Algorithm == "" {
		RespondBadRequest(w, r, "key algorithm is required")
		return
	}

	if len(req.KeyAttributes.KeyUsage) == 0 {
		RespondBadRequest(w, r, "key usage is required")
		return
	}

	// Route through the service layer for tenant-aware key generation
	if kmsSvc := s.service.KMS(); kmsSvc != nil {
		keyID := req.Name
		if keyID == "" {
			keyID = fmt.Sprintf("key-%d", time.Now().UnixNano())
		}

		attrs, err := keyAttributesRequestToXKMS(keyID, req.KeyAttributes)
		if err != nil {
			RespondBadRequest(w, r, err.Error())
			return
		}

		usage := make([]service.KeyUsage, len(req.KeyAttributes.KeyUsage))
		for i, u := range req.KeyAttributes.KeyUsage {
			usage[i] = service.KeyUsage(u)
		}

		svcReq := &service.GenerateKeyRequest{
			KeyAttributes:      attrs,
			Name:               req.Name,
			Description:        req.Description,
			Usage:              usage,
			Exportable:         req.KeyAttributes.Exportable,
			DeletionProtection: req.KeyAttributes.DeletionProtection,
			Tags:               req.Tags,
			Owner:              req.Owner,
		}

		svcResp, err := kmsSvc.GenerateKey(ctx, svcReq)
		if err != nil {
			s.handleKMSError(w, r, err)
			return
		}

		resp := &GenerateKeyRESTResponse{
			Key:       svcKeyHandleToResponse(svcResp.Key),
			Metadata:  svcMetadataToRESTResponse(svcResp.Metadata),
			Timestamp: svcResp.Timestamp.Unix(),
		}
		RespondJSON(w, r, http.StatusCreated, resp)
		return
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr := s.service.KMSManager()
	if kmsMgr == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	keyID := req.Name
	if keyID == "" {
		keyID = fmt.Sprintf("key-%d", time.Now().UnixNano())
	}

	attrs, err := keyAttributesRequestToXKMS(keyID, req.KeyAttributes)
	if err != nil {
		RespondBadRequest(w, r, err.Error())
		return
	}

	result, err := kmsMgr.GenerateKey(ctx, attrs)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrUnexpectedKMSResponse)
		return
	}

	now := time.Now()

	resp := &GenerateKeyRESTResponse{
		Key:       kmsMetadataToKeyHandle(metadata),
		Metadata:  kmsMetadataToResponse(metadata, req.Name, req.Description, req.Tags, req.Owner),
		Timestamp: now.Unix(),
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleImportKey handles POST /v1/kms/keys/import - Import key.
func (s *Server) HandleImportKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req ImportKeyRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(req.KeyMaterial) == 0 {
		RespondBadRequest(w, r, "key material is required")
		return
	}

	if req.WrappingKeyID == "" {
		RespondBadRequest(w, r, "wrapping key ID is required")
		return
	}

	kmsMgr := s.service.KMSManager()
	if kmsMgr == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	// Use user-provided name directly, or generate unique key ID if not provided
	keyID := req.Name
	if keyID == "" {
		keyID = fmt.Sprintf("key-import-%d", time.Now().UnixNano())
	}

	// Determine algorithm from request
	algorithm := "AES-256-GCM" // Default
	if req.KeyAttributes != nil && req.KeyAttributes.Algorithm != "" {
		algorithm = req.KeyAttributes.Algorithm
	}

	// Call KMS manager to import key
	result, err := kmsMgr.ImportKey(ctx, keyID, req.KeyMaterial, algorithm)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	// Type assert to *kms.KeyMetadata
	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrUnexpectedKMSResponse)
		return
	}

	now := time.Now()

	resp := &GenerateKeyRESTResponse{
		Key:       kmsMetadataToKeyHandle(metadata),
		Metadata:  kmsMetadataToResponseFull(metadata),
		Timestamp: now.Unix(),
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// HandleGetKey handles GET /v1/kms/keys/{name} - Get key metadata.
func (s *Server) HandleGetKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	// Route through the service layer for tenant-aware key retrieval
	if kmsSvc := s.service.KMS(); kmsSvc != nil {
		svcReq := &service.GetKeyRequest{KeyID: keyID}
		svcResp, err := kmsSvc.GetKey(ctx, svcReq)
		if err != nil {
			s.handleKMSError(w, r, err)
			return
		}

		resp := &GetKeyRESTResponse{
			Key:       svcKeyHandleToResponse(svcResp.Key),
			Metadata:  svcMetadataToRESTResponse(svcResp.Metadata),
			Timestamp: svcResp.Timestamp.Unix(),
		}
		RespondJSON(w, r, http.StatusOK, resp)
		return
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr := s.service.KMSManager()
	if kmsMgr == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	result, err := kmsMgr.GetKey(ctx, keyID)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrUnexpectedKMSResponse)
		return
	}

	now := time.Now()

	resp := &GetKeyRESTResponse{
		Key:       kmsMetadataToKeyHandle(metadata),
		Metadata:  kmsMetadataToResponseFull(metadata),
		Timestamp: now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListKeys handles GET /v1/kms/keys - List keys.
func (s *Server) HandleListKeys(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	limit := 100
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		l, err := strconv.Atoi(limitStr)
		if err == nil && l > 0 && l <= 1000 {
			limit = l
		}
	}

	prefix := r.URL.Query().Get("prefix")
	// cursor is currently not used but kept for API compatibility
	// cursor := r.URL.Query().Get("cursor")

	// Route through the service layer for tenant-aware key listing
	if kmsSvc := s.service.KMS(); kmsSvc != nil {
		filter := make(map[string]string)
		if prefix != "" {
			filter["prefix"] = prefix
		}

		// System keys (CA, TLS, agent identity) are filtered out here at the REST
		// boundary. If we paginate inside the service, the visible result can be
		// short or empty when system keys sort first (see TestRESTKeyListing/list_with_limit).
		// Request a high upper bound so the service does not truncate before
		// filtering; final pagination is applied below on the filtered slice.
		svcReq := &service.ListKeysRequest{
			Limit:  kmsListServiceFetchLimit,
			Filter: filter,
		}

		svcResp, err := kmsSvc.ListKeys(ctx, svcReq)
		if err != nil {
			s.handleKMSError(w, r, err)
			return
		}

		keys := make([]*KeyHandleResponse, 0, len(svcResp.Keys))
		for _, kh := range svcResp.Keys {
			h := svcKeyHandleToResponse(kh)
			if !isSystemKey(h.KeyID, h.Algorithm) {
				keys = append(keys, h)
			}
		}

		totalCount := len(keys)
		hasMore := svcResp.HasMore
		if limit > 0 && limit < len(keys) {
			keys = keys[:limit]
			hasMore = true
		}

		resp := &ListKeysRESTResponse{
			Keys:       keys,
			NextCursor: svcResp.NextCursor,
			HasMore:    hasMore,
			TotalCount: totalCount,
			Timestamp:  svcResp.Timestamp.Unix(),
		}
		RespondJSON(w, r, http.StatusOK, resp)
		return
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr := s.service.KMSManager()
	if kmsMgr == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	results, err := kmsMgr.ListKeys(ctx, prefix)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	allKeys := make([]*KeyHandleResponse, 0, len(results))
	for _, result := range results {
		if metadata, ok := result.(*kms.KeyMetadata); ok {
			allKeys = append(allKeys, kmsMetadataToKeyHandle(metadata))
		}
	}

	keys := make([]*KeyHandleResponse, 0, len(allKeys))
	for _, k := range allKeys {
		if !isSystemKey(k.KeyID, k.Algorithm) {
			keys = append(keys, k)
		}
	}

	now := time.Now()
	totalCount := len(keys) // capture before limit truncation

	hasMore := false
	if limit > 0 && limit < len(keys) {
		keys = keys[:limit]
		hasMore = true
	}

	resp := &ListKeysRESTResponse{
		Keys:       keys,
		NextCursor: "",
		HasMore:    hasMore,
		TotalCount: totalCount,
		Timestamp:  now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleRotateKey handles POST /v1/kms/keys/{name}/rotate - Rotate key.
func (s *Server) HandleRotateKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	// Allow empty body or JSON body for rotation request
	if len(body) > 0 {
		var reqBody map[string]interface{}
		if err := json.Unmarshal(body, &reqBody); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
	}

	// Route through the service layer for tenant-aware key rotation
	if kmsSvc := s.service.KMS(); kmsSvc != nil {
		svcReq := &service.RotateKeyRequest{KeyID: keyID}
		svcResp, err := kmsSvc.RotateKey(ctx, svcReq)
		if err != nil {
			s.handleKMSError(w, r, err)
			return
		}

		resp := &RotateKeyRESTResponse{
			KeyID:      svcResp.KeyID,
			OldVersion: int(svcResp.OldVersion),
			NewVersion: int(svcResp.NewVersion),
			Timestamp:  svcResp.Timestamp.Unix(),
		}
		RespondJSON(w, r, http.StatusOK, resp)
		return
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr := s.service.KMSManager()
	if kmsMgr == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	oldResult, err := kmsMgr.GetKey(ctx, keyID)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	oldMetadata, ok := oldResult.(*kms.KeyMetadata)
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrUnexpectedKMSResponse)
		return
	}
	oldVersion := oldMetadata.Version

	result, err := kmsMgr.RotateKey(ctx, keyID)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrUnexpectedKMSResponse)
		return
	}

	now := time.Now()

	resp := &RotateKeyRESTResponse{
		KeyID:      keyID,
		OldVersion: oldVersion,
		NewVersion: metadata.Version,
		Timestamp:  now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleDeleteKey handles DELETE /v1/kms/keys/{name} - Delete key.
func (s *Server) HandleDeleteKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil && !stderrors.Is(err, io.EOF) {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	// Parse optional body for force and pending window
	force := false
	pendingDays := 7
	if len(body) > 0 {
		var reqBody struct {
			Force             bool `json:"force"`
			PendingWindowDays int  `json:"pending_window_days"`
		}
		if err := json.Unmarshal(body, &reqBody); err != nil {
			RespondBadRequest(w, r, "invalid JSON body")
			return
		}
		force = reqBody.Force
		if reqBody.PendingWindowDays > 0 {
			pendingDays = reqBody.PendingWindowDays
		}
	}

	// Route through the service layer for tenant-aware key deletion
	if kmsSvc := s.service.KMS(); kmsSvc != nil {
		svcReq := &service.DeleteKeyRequest{
			KeyID:             keyID,
			PendingWindowDays: pendingDays,
			Force:             force,
		}
		svcResp, err := kmsSvc.DeleteKey(ctx, svcReq)
		if err != nil {
			s.handleKMSError(w, r, err)
			return
		}

		resp := &DeleteKeyRESTResponse{
			KeyID:        svcResp.KeyID,
			Status:       string(svcResp.Status),
			DeletionDate: svcResp.DeletionDate.Unix(),
			Timestamp:    svcResp.Timestamp.Unix(),
		}
		RespondJSON(w, r, http.StatusOK, resp)
		return
	}

	// Fallback: call KMS manager directly (no tenant isolation)
	kmsMgr := s.service.KMSManager()
	if kmsMgr == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	if !force {
		if err := kmsMgr.DisableKey(ctx, keyID); err != nil {
			s.handleKMSError(w, r, err)
			return
		}
	}

	err = kmsMgr.DeleteKey(ctx, keyID)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	now := time.Now()
	deletionDate := now.AddDate(0, 0, pendingDays)

	resp := &DeleteKeyRESTResponse{
		KeyID:        keyID,
		Status:       "pending_deletion",
		DeletionDate: deletionDate.Unix(),
		Timestamp:    now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleUpdateKeyStatus handles PUT /v1/kms/keys/{name}/status - Update key status.
func (s *Server) HandleUpdateKeyStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req UpdateKeyStatusRESTRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid request body: "+err.Error())
		return
	}

	if req.Status == "" {
		RespondBadRequest(w, r, "status is required")
		return
	}

	// Validate status value before accessing KMS manager
	status := strings.ToLower(req.Status)
	if status != "enabled" && status != "disabled" {
		RespondBadRequest(w, r, "unsupported status: must be 'enabled' or 'disabled'")
		return
	}

	kmsMgr := s.service.KMSManager()
	if kmsMgr == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	// Update status based on request
	switch status {
	case "enabled":
		err = kmsMgr.EnableKey(ctx, keyID)
	case "disabled":
		err = kmsMgr.DisableKey(ctx, keyID)
	}

	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	now := time.Now()

	resp := &UpdateKeyStatusRESTResponse{
		KeyID:     keyID,
		Status:    status,
		Timestamp: now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleListKeyVersions handles GET /v1/kms/keys/{name}/versions - List key versions.
func (s *Server) HandleListKeyVersions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	// Parse pagination parameters (not used currently, but kept for API compatibility)
	// limit := 100
	// cursor := r.URL.Query().Get("cursor")

	kmsMgr := s.service.KMSManager()
	if kmsMgr == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	// Get key metadata to determine version info
	result, err := kmsMgr.GetKey(ctx, keyID)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	metadata, ok := result.(*kms.KeyMetadata)
	if !ok {
		RespondError(w, r, http.StatusInternalServerError, ErrUnexpectedKMSResponse)
		return
	}

	// Build version list by iterating from version 1 to current version
	// This generates synthetic version entries based on the current version counter
	currentVersion := int(metadata.Version)
	versions := make([]*KeyVersionResponse, 0, currentVersion)
	for v := 1; v <= currentVersion; v++ {
		status := string(metadata.State)
		// Current version has the current status, older versions default to enabled
		// (they remain enabled during grace period until explicitly disabled)
		if v != currentVersion {
			status = "ENABLED"
		}
		versions = append(versions, &KeyVersionResponse{
			Version:   v,
			Status:    status,
			Algorithm: metadata.Algorithm,
			CreatedAt: metadata.Created.Unix(),
		})
	}

	now := time.Now()

	resp := &ListKeyVersionsRESTResponse{
		KeyID:      keyID,
		Versions:   versions,
		NextCursor: "",
		HasMore:    false,
		Timestamp:  now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleKMSEncrypt handles POST /v1/kms/encrypt/{name} - Encrypt data.
func (s *Server) HandleKMSEncrypt(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
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
	}

	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(reqBody.Plaintext) == 0 {
		RespondBadRequest(w, r, "plaintext is required")
		return
	}

	cryptoService := s.service.Crypto()
	if cryptoService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrCryptoServiceUnavailable)
		return
	}

	req := &service.EncryptRequest{
		KeyID:     keyID,
		Plaintext: reqBody.Plaintext,
		Context:   reqBody.Context,
	}

	svcResp, err := cryptoService.Encrypt(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert service response to REST response with int64 timestamp
	resp := &EncryptRESTResponse{
		KeyID:      svcResp.KeyID,
		KeyVersion: svcResp.KeyVersion,
		Ciphertext: svcResp.Ciphertext,
		Nonce:      svcResp.Nonce,
		Timestamp:  svcResp.Timestamp.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleKMSDecrypt handles POST /v1/kms/decrypt/{name} - Decrypt data.
func (s *Server) HandleKMSDecrypt(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
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
	}

	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(reqBody.Ciphertext) == 0 {
		RespondBadRequest(w, r, "ciphertext is required")
		return
	}

	cryptoService := s.service.Crypto()
	if cryptoService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrCryptoServiceUnavailable)
		return
	}

	req := &service.DecryptRequest{
		KeyID:      keyID,
		Ciphertext: reqBody.Ciphertext,
		Context:    reqBody.Context,
	}

	svcResp, err := cryptoService.Decrypt(ctx, req)
	if err != nil {
		s.handleServiceError(w, r, err)
		return
	}

	// Convert service response to REST response with int64 timestamp
	resp := &DecryptRESTResponse{
		KeyID:      svcResp.KeyID,
		KeyVersion: svcResp.KeyVersion,
		Plaintext:  svcResp.Plaintext,
		Timestamp:  svcResp.Timestamp.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleBackendStatus returns status and capabilities of KMS backends.
// GET /api/v1/kms/backend/status.
func (s *Server) HandleBackendStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	kmsMgr := s.service.KMSManager()
	if kmsMgr == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	backendType := kmsMgr.GetBackendType()
	isAvailable := kmsMgr.IsAvailable(ctx)

	statusStr := "healthy"
	if !isAvailable {
		statusStr = "unavailable"
	}

	now := time.Now()

	resp := &BackendStatusRESTResponse{
		BackendType: string(backendType),
		Status:      statusStr,
		Capabilities: map[string]bool{
			"encrypt":     true,
			"decrypt":     true,
			"sign":        true,
			"verify":      true,
			"generate":    true,
			"import":      true,
			"rotate":      true,
			"data_key":    true,
			"key_wrap":    true,
			"key_unwrap":  true,
			"multi_party": false,
		},
		Timestamp: now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleBackendTest performs a seal/unseal test cycle to verify backend functionality.
// POST /api/v1/kms/backend/test.
func (s *Server) HandleBackendTest(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	kmsMgr := s.service.KMSManager()
	if kmsMgr == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	start := time.Now()

	// Test by checking availability
	isAvailable := kmsMgr.IsAvailable(ctx)

	duration := time.Since(start)
	now := time.Now()

	if !isAvailable {
		resp := &BackendTestRESTResponse{
			Success:    false,
			DurationNs: duration.Nanoseconds(),
			Message:    "backend is not available",
			Timestamp:  now.Unix(),
		}
		RespondJSON(w, r, http.StatusOK, resp)
		return
	}

	resp := &BackendTestRESTResponse{
		Success:    true,
		DurationNs: duration.Nanoseconds(),
		Message:    "backend test completed successfully",
		Timestamp:  now.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Key Export/Import Handlers
// =============================================================================

// HandleExportKey exports a key wrapped with a wrapping key.
// POST /api/v1/kms/keys/{name}/export.
func (s *Server) HandleExportKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.ExportKeyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Set key ID from URL path
	req.KeyID = keyID

	if req.WrappingKeyID == "" {
		RespondBadRequest(w, r, "wrapping_key_id is required")
		return
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	svcResp, err := kmsService.ExportKey(ctx, &req)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	// Convert service response to REST response with int64 timestamp
	resp := &ExportKeyRESTResponse{
		KeyID:             svcResp.KeyID,
		Version:           svcResp.Version,
		WrappedKey:        svcResp.WrappedKey,
		WrappingKeyID:     svcResp.WrappingKeyID,
		WrappingAlgorithm: svcResp.WrappingAlgorithm,
		Format:            svcResp.Format,
		Timestamp:         svcResp.Timestamp.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleUnwrapKey imports a wrapped key into the KMS.
// POST /api/v1/kms/keys/unwrap.
func (s *Server) HandleUnwrapKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.UnwrapKeyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	if len(req.WrappedKey) == 0 {
		RespondBadRequest(w, r, "wrapped_key is required")
		return
	}

	if req.WrappingKeyID == "" {
		RespondBadRequest(w, r, "wrapping_key_id is required")
		return
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	svcResp, err := kmsService.UnwrapKey(ctx, &req)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	// Convert service response to REST response with int64 timestamp
	resp := &UnwrapKeyRESTResponse{
		KeyID:     svcResp.KeyID,
		Version:   svcResp.Version,
		Algorithm: string(svcResp.Algorithm),
		Status:    string(svcResp.Status),
		Timestamp: svcResp.Timestamp.Unix(),
	}

	// Convert key handle if present
	if svcResp.Key != nil {
		resp.Key = &KeyHandleResponse{
			KeyID:     svcResp.Key.KeyID,
			Version:   int(svcResp.Key.Version), //nolint:gosec // G115: Version bounded by rotation limits
			Algorithm: string(svcResp.Key.Algorithm),
			Status:    string(svcResp.Key.Status),
			CreatedAt: svcResp.Key.CreatedAt.Unix(),
		}
	}

	RespondJSON(w, r, http.StatusCreated, resp)
}

// =============================================================================
// Key Safety Handlers
// =============================================================================

// HandleSetKeySafety sets AEAD safety limits for a key.
// PUT /api/v1/kms/keys/{name}/safety.
func (s *Server) HandleSetKeySafety(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.SetKeySafetyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	// Set key ID from URL path
	req.KeyID = keyID

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	svcResp, err := kmsService.SetKeySafety(ctx, &req)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	// Convert service response to REST response with int64 timestamp
	resp := &SetKeySafetyRESTResponse{
		KeyID: svcResp.KeyID,
		Config: &AEADSafetyConfigResponse{
			MaxOperations:     svcResp.Config.MaxOperations,
			MaxBytes:          svcResp.Config.MaxBytes,
			WarningPercent:    svcResp.Config.WarningPercent,
			AutoRotatePercent: svcResp.Config.AutoRotatePercent,
		},
		Timestamp: svcResp.Timestamp.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleClearKeySafety clears AEAD safety limits for a key.
// DELETE /api/v1/kms/keys/{name}/safety.
func (s *Server) HandleClearKeySafety(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	req := &service.ClearKeySafetyRequest{
		KeyID: keyID,
	}

	svcResp, err := kmsService.ClearKeySafety(ctx, req)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	// Convert service response to REST response with int64 timestamp
	resp := &ClearKeySafetyRESTResponse{
		KeyID:     svcResp.KeyID,
		Timestamp: svcResp.Timestamp.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSafetyCheck returns warnings for keys/DEKs approaching safety limits.
// GET /api/v1/kms/safety/check.
func (s *Server) HandleSafetyCheck(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req service.SafetyCheckRequest

	// Parse optional threshold_percent query parameter
	if thresholdStr := r.URL.Query().Get("threshold_percent"); thresholdStr != "" {
		threshold, err := strconv.ParseFloat(thresholdStr, 64)
		if err == nil && threshold > 0 && threshold <= 100 {
			req.ThresholdPercent = threshold
		}
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	svcResp, err := kmsService.SafetyCheck(ctx, &req)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	// Convert service response to REST response with int64 timestamp
	items := make([]SafetyCheckItemResponse, len(svcResp.Items))
	for i, item := range svcResp.Items {
		items[i] = SafetyCheckItemResponse{
			ID:             item.ID,
			Type:           item.Type,
			OperationCount: item.OperationCount,
			BytesProcessed: item.BytesProcessed,
			UsagePercent:   item.UsagePercent,
			MaxOperations:  item.MaxOperations,
			MaxBytes:       item.MaxBytes,
		}
	}

	resp := &SafetyCheckRESTResponse{
		Items:            items,
		ThresholdPercent: svcResp.ThresholdPercent,
		TotalKeysChecked: svcResp.TotalKeysChecked,
		TotalDEKsChecked: svcResp.TotalDEKsChecked,
		Timestamp:        svcResp.Timestamp.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSafetyReport returns a comprehensive safety report for all keys/DEKs.
// GET /api/v1/kms/safety/report.
func (s *Server) HandleSafetyReport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req service.SafetyReportRequest

	// Parse optional include_details query parameter
	if r.URL.Query().Get("include_details") == "true" {
		req.IncludeDetails = true
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	svcResp, err := kmsService.SafetyReport(ctx, &req)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	// Convert service response to REST response with int64 timestamp
	items := make([]SafetyCheckItemResponse, len(svcResp.Items))
	for i, item := range svcResp.Items {
		items[i] = SafetyCheckItemResponse{
			ID:             item.ID,
			Type:           item.Type,
			OperationCount: item.OperationCount,
			BytesProcessed: item.BytesProcessed,
			UsagePercent:   item.UsagePercent,
			MaxOperations:  item.MaxOperations,
			MaxBytes:       item.MaxBytes,
		}
	}

	resp := &SafetyReportRESTResponse{
		Items:               items,
		TotalOperations:     svcResp.TotalOperations,
		TotalBytesProcessed: svcResp.TotalBytesProcessed,
		ServerConfig: AEADServerConfigResponse{
			Enforce:           svcResp.ServerConfig.Enforce,
			MaxOperations:     svcResp.ServerConfig.MaxOperations,
			MaxBytes:          svcResp.ServerConfig.MaxBytes,
			WarningPercent:    svcResp.ServerConfig.WarningPercent,
			AutoRotatePercent: svcResp.ServerConfig.AutoRotatePercent,
		},
		Timestamp: svcResp.Timestamp.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Key Policy Handlers
// =============================================================================

// HandleSetKeyPolicy sets the access control policy for a key.
// PUT /api/v1/kms/keys/{name}/policy.
func (s *Server) HandleSetKeyPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var req service.SetKeyPolicyRequest
	if err := json.Unmarshal(body, &req); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	req.KeyID = keyID

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	svcResp, err := kmsService.SetKeyPolicy(ctx, &req)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	// Convert service response to REST response with int64 timestamp
	resp := &SetKeyPolicyRESTResponse{
		KeyID:         svcResp.KeyID,
		PolicyVersion: svcResp.PolicyVersion,
		Timestamp:     svcResp.Timestamp.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleGetKeyPolicy retrieves the access control policy for a key.
// GET /api/v1/kms/keys/{name}/policy.
func (s *Server) HandleGetKeyPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	req := &service.GetKeyPolicyRequest{
		KeyID: keyID,
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	svcResp, err := kmsService.GetKeyPolicy(ctx, req)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	// Convert service response to REST response with int64 timestamp
	var policyResp *KeyPolicyResponse
	if svcResp.Policy != nil {
		// Convert allowed operations from KeyUsage to strings
		allowedOps := make([]string, len(svcResp.Policy.AllowedOperations))
		for i, op := range svcResp.Policy.AllowedOperations {
			allowedOps[i] = string(op)
		}

		var expiresAt int64
		if svcResp.Policy.ExpiresAt != nil {
			expiresAt = svcResp.Policy.ExpiresAt.Unix()
		}

		var maxUsageCount uint64
		if svcResp.Policy.MaxUsageCount != nil {
			maxUsageCount = *svcResp.Policy.MaxUsageCount
		}

		policyResp = &KeyPolicyResponse{
			KeyID:             svcResp.Policy.KeyID,
			Version:           svcResp.Policy.Version,
			AllowedOperations: allowedOps,
			AllowedPrincipals: svcResp.Policy.AllowedPrincipals,
			DeniedPrincipals:  svcResp.Policy.DeniedPrincipals,
			ExpiresAt:         expiresAt,
			RotationRequired:  svcResp.Policy.RotationRequired,
			MaxUsageCount:     maxUsageCount,
			RequireMFA:        svcResp.Policy.RequireMFA,
			UpdatedAt:         svcResp.Policy.UpdatedAt.Unix(),
		}
	}

	resp := &GetKeyPolicyRESTResponse{
		Policy:    policyResp,
		Timestamp: svcResp.Timestamp.Unix(),
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// HandleSetKeyRotationPolicy sets or replaces the automatic rotation policy for a key.
// PUT /api/v1/kms/keys/{name}/rotation-policy.
func (s *Server) HandleSetKeyRotationPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		RespondBadRequest(w, r, "failed to read request body")
		return
	}
	defer r.Body.Close()

	var reqBody RotationPolicyRESTRequest
	if err := json.Unmarshal(body, &reqBody); err != nil {
		RespondBadRequest(w, r, "invalid JSON body")
		return
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	policy := &service.RotationPolicy{
		Enabled:        reqBody.Enabled,
		Interval:       time.Duration(reqBody.IntervalSeconds) * time.Second,
		RetainVersions: reqBody.RetainVersions,
		AutoRotate:     reqBody.AutoRotate,
	}

	svcReq := &service.SetKeyRotationPolicyRequest{
		KeyID:  keyID,
		Policy: policy,
	}

	resp, err := kmsService.SetKeyRotationPolicy(ctx, svcReq)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	restResp := &SetKeyRotationPolicyRESTResponse{
		KeyID:     resp.KeyID,
		Policy:    serviceToRESTRotationPolicy(resp.Policy),
		Timestamp: resp.Timestamp.Unix(),
	}
	RespondJSON(w, r, http.StatusOK, restResp)
}

// HandleGetKeyRotationPolicy retrieves the current automatic rotation policy for a key.
// GET /api/v1/kms/keys/{name}/rotation-policy.
func (s *Server) HandleGetKeyRotationPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	svcReq := &service.GetKeyRotationPolicyRequest{KeyID: keyID}

	resp, err := kmsService.GetKeyRotationPolicy(ctx, svcReq)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	restResp := &GetKeyRotationPolicyRESTResponse{
		KeyID:     resp.KeyID,
		Policy:    serviceToRESTRotationPolicy(resp.Policy),
		Timestamp: resp.Timestamp.Unix(),
	}
	RespondJSON(w, r, http.StatusOK, restResp)
}

// HandleDeleteKeyRotationPolicy removes the rotation policy for a key, disabling auto-rotation.
// DELETE /api/v1/kms/keys/{name}/rotation-policy.
func (s *Server) HandleDeleteKeyRotationPolicy(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	keyID := chi.URLParam(r, "name")
	if keyID == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	kmsService := s.service.KMS()
	if kmsService == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	svcReq := &service.DeleteKeyRotationPolicyRequest{KeyID: keyID}

	resp, err := kmsService.DeleteKeyRotationPolicy(ctx, svcReq)
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	restResp := &DeleteKeyRotationPolicyRESTResponse{
		KeyID:     resp.KeyID,
		Timestamp: resp.Timestamp.Unix(),
	}
	RespondJSON(w, r, http.StatusOK, restResp)
}

// serviceToRESTRotationPolicy converts a service RotationPolicy to the REST response type.
func serviceToRESTRotationPolicy(p *service.RotationPolicy) *RotationPolicyRESTResponse {
	if p == nil {
		return nil
	}
	resp := &RotationPolicyRESTResponse{
		Enabled:         p.Enabled,
		IntervalSeconds: int64(p.Interval / time.Second),
		RetainVersions:  p.RetainVersions,
		AutoRotate:      p.AutoRotate,
	}
	if !p.NextRotation.IsZero() {
		resp.NextRotationUnix = p.NextRotation.Unix()
	}
	return resp
}

// =============================================================================
// Conversion Helpers - KeyAttributesRequest to kctypes.KeyAttributes
// =============================================================================

// keyAttributesRequestToXKMS converts a KeyAttributesRequest to kctypes.KeyAttributes.
// Note: Fields like AutoRotate, Labels, RotationPeriod, ExpiresAt are part of the REST API
// request but are NOT part of go-xkms's KeyAttributes. They should be handled separately
// as key metadata in the KMS service layer.
func keyAttributesRequestToXKMS(keyID string, req *KeyAttributesRequest) (*kctypes.KeyAttributes, error) {
	if req == nil {
		return nil, ErrKeyAttributesNil
	}

	attrs := &kctypes.KeyAttributes{
		CN:         keyID,
		Exportable: req.Exportable,
	}

	// Note: AutoRotate, Labels, RotationPeriod, ExpiresAt are NOT fields in kctypes.KeyAttributes
	// These values from the request should be stored separately as key metadata by the KMS service

	// Map algorithm string to go-xkms types
	algorithm := strings.ToUpper(req.Algorithm)
	switch {
	case strings.Contains(algorithm, "AES-128") || strings.Contains(algorithm, "AES128"):
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES128GCM
		attrs.KeyType = kctypes.KeyTypeEncryption
	case strings.Contains(algorithm, "AES-192") || strings.Contains(algorithm, "AES192"):
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES192GCM
		attrs.KeyType = kctypes.KeyTypeEncryption
	case strings.Contains(algorithm, "AES-256") || strings.Contains(algorithm, "AES256"):
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES256GCM
		attrs.KeyType = kctypes.KeyTypeEncryption
	case strings.Contains(algorithm, "XCHACHA20"):
		attrs.SymmetricAlgorithm = kctypes.SymmetricXChaCha20Poly1305
		attrs.KeyType = kctypes.KeyTypeEncryption
	case strings.Contains(algorithm, "CHACHA20"):
		attrs.SymmetricAlgorithm = kctypes.SymmetricChaCha20Poly1305
		attrs.KeyType = kctypes.KeyTypeEncryption
	case strings.Contains(algorithm, "RSA-2048") || strings.Contains(algorithm, "RSA2048"):
		attrs.KeyAlgorithm = x509.RSA
		attrs.RSAAttributes = &kctypes.RSAAttributes{KeySize: 2048}
		attrs.KeyType = kctypes.KeyTypeSigning
	case strings.Contains(algorithm, "RSA-3072") || strings.Contains(algorithm, "RSA3072"):
		attrs.KeyAlgorithm = x509.RSA
		attrs.RSAAttributes = &kctypes.RSAAttributes{KeySize: 3072}
		attrs.KeyType = kctypes.KeyTypeSigning
	case strings.Contains(algorithm, "RSA-4096") || strings.Contains(algorithm, "RSA4096"):
		attrs.KeyAlgorithm = x509.RSA
		attrs.RSAAttributes = &kctypes.RSAAttributes{KeySize: 4096}
		attrs.KeyType = kctypes.KeyTypeSigning
	case strings.Contains(algorithm, "P256") || strings.Contains(algorithm, "ECDSA-P256"):
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P256()}
		attrs.KeyType = kctypes.KeyTypeSigning
	case strings.Contains(algorithm, "P384") || strings.Contains(algorithm, "ECDSA-P384"):
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P384()}
		attrs.KeyType = kctypes.KeyTypeSigning
	case strings.Contains(algorithm, "P521") || strings.Contains(algorithm, "ECDSA-P521"):
		attrs.KeyAlgorithm = x509.ECDSA
		attrs.ECCAttributes = &kctypes.ECCAttributes{Curve: elliptic.P521()}
		attrs.KeyType = kctypes.KeyTypeSigning
	case strings.Contains(algorithm, "ED25519"):
		attrs.KeyAlgorithm = x509.Ed25519
		attrs.KeyType = kctypes.KeyTypeSigning
	default:
		// Default to AES-256-GCM for unknown algorithms
		attrs.SymmetricAlgorithm = kctypes.SymmetricAES256GCM
		attrs.KeyType = kctypes.KeyTypeEncryption
	}

	return attrs, nil
}

// =============================================================================
// Conversion Helpers - KMS types to REST Response
// =============================================================================

// kmsMetadataToKeyHandle creates a KeyHandleResponse from kms.KeyMetadata.
func kmsMetadataToKeyHandle(metadata *kms.KeyMetadata) *KeyHandleResponse {
	return &KeyHandleResponse{
		KeyID:     metadata.KeyID,
		Version:   metadata.Version,
		Algorithm: metadata.Algorithm,
		Status:    string(metadata.State),
		CreatedAt: metadata.Created.Unix(),
	}
}

// kmsMetadataToResponse creates a KeyMetadataResponse from kms.KeyMetadata with request data.
func kmsMetadataToResponse(metadata *kms.KeyMetadata, name, description string, tags map[string]string, owner string) *KeyMetadataResponse {
	var lastUsed int64
	if metadata.LastUsedAt != nil {
		lastUsed = metadata.LastUsedAt.Unix()
	}

	// Convert usage to string slice
	usage := make([]string, len(metadata.Usage))
	for i, u := range metadata.Usage {
		usage[i] = string(u)
	}

	return &KeyMetadataResponse{
		KeyID:              metadata.KeyID,
		Name:               name,
		Description:        description,
		Algorithm:          metadata.Algorithm,
		Status:             string(metadata.State),
		Usage:              usage,
		Version:            metadata.Version,
		Exportable:         metadata.Exportable,
		DeletionProtection: metadata.DeletionProtection,
		CreatedAt:          metadata.Created.Unix(),
		UpdatedAt:          metadata.Updated.Unix(),
		LastUsedAt:         lastUsed,
		Tags:               tags,
		Owner:              owner,
	}
}

// kmsMetadataToResponseFull creates a KeyMetadataResponse from kms.KeyMetadata with all fields.
func kmsMetadataToResponseFull(metadata *kms.KeyMetadata) *KeyMetadataResponse {
	var lastUsed int64
	if metadata.LastUsedAt != nil {
		lastUsed = metadata.LastUsedAt.Unix()
	}

	// Convert usage to string slice
	usage := make([]string, len(metadata.Usage))
	for i, u := range metadata.Usage {
		usage[i] = string(u)
	}

	return &KeyMetadataResponse{
		KeyID:              metadata.KeyID,
		Name:               metadata.Name,
		Description:        metadata.Description,
		Algorithm:          metadata.Algorithm,
		Status:             string(metadata.State),
		Usage:              usage,
		Version:            metadata.Version,
		Exportable:         metadata.Exportable,
		DeletionProtection: metadata.DeletionProtection,
		CreatedAt:          metadata.Created.Unix(),
		UpdatedAt:          metadata.Updated.Unix(),
		LastUsedAt:         lastUsed,
		Tags:               metadata.Tags,
		Owner:              metadata.Owner,
	}
}

// svcKeyHandleToResponse converts a service KeyHandle to REST KeyHandleResponse.
func svcKeyHandleToResponse(kh *service.KeyHandle) *KeyHandleResponse {
	return &KeyHandleResponse{
		KeyID: kh.KeyID,
		//nolint:gosec // G115: Version bounded by KMS backend, safe for int conversion
		Version:   int(kh.Version),
		Algorithm: string(kh.Algorithm),
		Status:    string(kh.Status),
		CreatedAt: kh.CreatedAt.Unix(),
	}
}

// svcMetadataToRESTResponse converts a service KeyMetadata to REST KeyMetadataResponse.
func svcMetadataToRESTResponse(md *service.KeyMetadata) *KeyMetadataResponse {
	var lastUsed int64
	if md.LastUsedAt != nil {
		lastUsed = md.LastUsedAt.Unix()
	}

	usage := make([]string, len(md.Usage))
	for i, u := range md.Usage {
		usage[i] = string(u)
	}

	return &KeyMetadataResponse{
		KeyID:       md.KeyID,
		Name:        md.Name,
		Description: md.Description,
		Algorithm:   string(md.Algorithm),
		Status:      string(md.Status),
		Usage:       usage,
		//nolint:gosec // G115: Version bounded by KMS backend, safe for int conversion
		Version:            int(md.Version),
		Exportable:         md.Exportable,
		DeletionProtection: md.DeletionProtection,
		CreatedAt:          md.CreatedAt.Unix(),
		UpdatedAt:          md.UpdatedAt.Unix(),
		LastUsedAt:         lastUsed,
		Tags:               md.Tags,
		Owner:              md.Owner,
	}
}

// HandleSetDefaultKey handles PUT /v1/kms/keys/{name}/default - Mark a key as the system default.
func (s *Server) HandleSetDefaultKey(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	name := chi.URLParam(r, "name")
	if name == "" {
		RespondBadRequest(w, r, "key name is required")
		return
	}

	kmsSvc := s.service.KMS()
	if kmsSvc == nil {
		RespondError(w, r, http.StatusInternalServerError, ErrKMSServiceUnavailable)
		return
	}

	resp, err := kmsSvc.SetDefaultKey(ctx, &service.SetDefaultKeyRequest{Name: name})
	if err != nil {
		s.handleKMSError(w, r, err)
		return
	}

	RespondJSON(w, r, http.StatusOK, resp)
}

// =============================================================================
// Error Handling - KMS specific
// =============================================================================

// handleKMSError converts internal errors to HTTP responses.
func (s *Server) handleKMSError(w http.ResponseWriter, r *http.Request, err error) {
	if err == nil {
		return
	}

	var dErr *errors.QBError
	if !stderrors.As(err, &dErr) {
		RespondError(w, r, http.StatusInternalServerError, err)
		return
	}

	switch dErr.Code {
	case errors.ErrNotFound:
		RespondError(w, r, http.StatusNotFound, dErr)
	case errors.ErrAlreadyExists:
		RespondError(w, r, http.StatusConflict, dErr)
	case errors.ErrInvalidInput:
		RespondError(w, r, http.StatusBadRequest, dErr)
	case errors.ErrUnauthorized:
		RespondError(w, r, http.StatusUnauthorized, dErr)
	case errors.ErrForbidden:
		RespondError(w, r, http.StatusForbidden, dErr)
	case errors.ErrTimeout:
		RespondError(w, r, http.StatusGatewayTimeout, dErr)
	case errors.ErrResourceExhausted:
		RespondError(w, r, http.StatusTooManyRequests, dErr)
	case errors.ErrUnavailable:
		RespondError(w, r, http.StatusServiceUnavailable, dErr)
	default:
		RespondError(w, r, http.StatusInternalServerError, dErr)
	}
}
