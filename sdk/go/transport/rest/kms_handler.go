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

// kms_handler.go provides typed REST routing for the quicbaas.KMS service.
//
// The generic REST dispatch path maps KMS methods to endpoint templates that
// still contain literal "{name}" placeholders (e.g. /api/v1/kms/keys/{name})
// and returns an opaque map[string]interface{} that the SDK KMS client cannot
// type-assert. This handler substitutes the concrete path parameters from the
// strongly-typed SDK request, marshals the correct wire request body, decodes
// the server's REST response wire types, and converts them into the
// service.*Response types the SDK KMS client expects.
//
// Methods that have no server-side REST route (GetPublicKey, RevokeKey,
// GetKeyMetadata, UpdateKeyMetadata, GetSupportedAlgorithms) fall through to
// the generic dispatch path.

package rest

import (
	"context"
	"net/http"
	"net/url"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// kmsServicePrefix is the gRPC-style service identifier the SDK uses to route
// KMS requests to this handler.
const kmsServicePrefix = "quicbaas.KMS"

// isKMSService reports whether the supplied service name resolves to the KMS
// service.
func isKMSService(service string) bool {
	return trimRESTServicePrefix(service) == "KMS"
}

// kmsRESTHandler delegates KMS service calls to the quicbaasd REST endpoints
// with proper path-parameter substitution and typed response conversion.
type kmsRESTHandler struct {
	transport *Transport
}

// newKMSRESTHandler constructs a handler bound to t.
func newKMSRESTHandler(t *Transport) *kmsRESTHandler {
	return &kmsRESTHandler{transport: t}
}

// handleRequest dispatches the KMS service method to the matching REST
// endpoint. Methods without a server route fall through to the default request
// path which returns the raw interface{} body.
func (h *kmsRESTHandler) handleRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	switch method {
	case "GenerateKey":
		return h.handleGenerateKey(ctx, req)
	case "ImportKey":
		return h.handleImportKey(ctx, req)
	case "GetKey":
		return h.handleGetKey(ctx, req)
	case "ListKeys":
		return h.handleListKeys(ctx, req)
	case "RotateKey":
		return h.handleRotateKey(ctx, req)
	case "DeleteKey":
		return h.handleDeleteKey(ctx, req)
	case "ExportKey":
		return h.handleExportKey(ctx, req)
	case "UnwrapKey":
		return h.handleUnwrapKey(ctx, req)
	case "UpdateKeyStatus":
		return h.handleUpdateKeyStatus(ctx, req)
	case "ListKeyVersions":
		return h.handleListKeyVersions(ctx, req)
	case "SetKeyPolicy":
		return h.handleSetKeyPolicy(ctx, req)
	case "GetKeyPolicy":
		return h.handleGetKeyPolicy(ctx, req)
	case "BackendStatus":
		return h.handleBackendStatus(ctx, req)
	default:
		return h.transport.requestWithRetry(ctx, kmsServicePrefix, method, req)
	}
}

// =============================================================================
// REST wire types (mirror pkg/api/rest server response shapes)
// =============================================================================

type kmsKeyHandleWire struct {
	KeyID     string `json:"key_id"`
	Version   int    `json:"version"`
	Algorithm string `json:"algorithm"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
}

type kmsKeyMetadataWire struct {
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

type kmsGenerateKeyWire struct {
	Key       *kmsKeyHandleWire   `json:"key"`
	Metadata  *kmsKeyMetadataWire `json:"metadata,omitempty"`
	Timestamp int64               `json:"timestamp"`
}

type kmsListKeysWire struct {
	Keys       []*kmsKeyHandleWire `json:"keys"`
	NextCursor string              `json:"next_cursor,omitempty"`
	HasMore    bool                `json:"has_more"`
	TotalCount int                 `json:"total_count"`
	Timestamp  int64               `json:"timestamp"`
}

type kmsRotateKeyWire struct {
	KeyID      string `json:"key_id"`
	OldVersion int    `json:"old_version"`
	NewVersion int    `json:"new_version"`
	Timestamp  int64  `json:"timestamp"`
}

type kmsDeleteKeyWire struct {
	KeyID        string `json:"key_id"`
	Status       string `json:"status"`
	DeletionDate int64  `json:"deletion_date,omitempty"`
	Timestamp    int64  `json:"timestamp"`
}

type kmsKeyVersionWire struct {
	Version   int    `json:"version"`
	Status    string `json:"status"`
	Algorithm string `json:"algorithm"`
	CreatedAt int64  `json:"created_at"`
}

type kmsListKeyVersionsWire struct {
	KeyID      string               `json:"key_id"`
	Versions   []*kmsKeyVersionWire `json:"versions"`
	NextCursor string               `json:"next_cursor,omitempty"`
	HasMore    bool                 `json:"has_more"`
	Timestamp  int64                `json:"timestamp"`
}

type kmsUpdateKeyStatusWire struct {
	KeyID     string `json:"key_id"`
	Status    string `json:"status"`
	Timestamp int64  `json:"timestamp"`
}

type kmsExportKeyWire struct {
	KeyID             string `json:"key_id"`
	Version           uint64 `json:"version,omitempty"`
	WrappedKey        []byte `json:"wrapped_key"`
	WrappingKeyID     string `json:"wrapping_key_id"`
	WrappingAlgorithm string `json:"wrapping_algorithm"`
	Format            string `json:"format"`
	Timestamp         int64  `json:"timestamp"`
}

type kmsUnwrapKeyWire struct {
	KeyID     string            `json:"key_id"`
	Version   uint64            `json:"version"`
	Algorithm string            `json:"algorithm"`
	Status    string            `json:"status"`
	Key       *kmsKeyHandleWire `json:"key,omitempty"`
	Timestamp int64             `json:"timestamp"`
}

type kmsKeyPolicyWire struct {
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

type kmsSetKeyPolicyWire struct {
	KeyID         string `json:"key_id"`
	PolicyVersion uint64 `json:"policy_version"`
	Timestamp     int64  `json:"timestamp"`
}

type kmsGetKeyPolicyWire struct {
	Policy    *kmsKeyPolicyWire `json:"policy"`
	Timestamp int64             `json:"timestamp"`
}

type kmsBackendStatusWire struct {
	BackendType  string          `json:"backend_type"`
	Status       string          `json:"status"`
	Capabilities map[string]bool `json:"capabilities"`
	Timestamp    int64           `json:"timestamp"`
}

// =============================================================================
// REST wire request types
// =============================================================================

type kmsKeyAttributesRequest struct {
	Algorithm  string   `json:"algorithm"`
	KeyUsage   []string `json:"key_usage"`
	Exportable bool     `json:"exportable,omitempty"`
}

type kmsGenerateKeyRequest struct {
	Name          string                   `json:"name,omitempty"`
	Description   string                   `json:"description,omitempty"`
	KeyAttributes *kmsKeyAttributesRequest `json:"key_attributes"`
	Tags          map[string]string        `json:"tags,omitempty"`
}

type kmsImportKeyRequest struct {
	KeyMaterial   []byte                   `json:"key_material"`
	WrappingKeyID string                   `json:"wrapping_key_id,omitempty"`
	KeyAttributes *kmsKeyAttributesRequest `json:"key_attributes,omitempty"`
	Name          string                   `json:"name,omitempty"`
	Description   string                   `json:"description,omitempty"`
	Tags          map[string]string        `json:"tags,omitempty"`
}

type kmsUpdateKeyStatusRequest struct {
	Status string `json:"status"`
}

// =============================================================================
// Handlers
// =============================================================================

func (h *kmsRESTHandler) handleGenerateKey(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*kmsclient.GenerateKeyRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.kms.GenerateKey", "request is required")
	}
	body := &kmsGenerateKeyRequest{
		Name: r.Name,
		KeyAttributes: &kmsKeyAttributesRequest{
			Algorithm: r.Algorithm.String(),
			KeyUsage:  kmsUsagesToStrings(r.Usage),
		},
	}
	var wire kmsGenerateKeyWire
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/kms/keys", body, &wire); err != nil {
		return nil, err
	}
	return kmsWireToGenerateKeyResponse(&wire), nil
}

func (h *kmsRESTHandler) handleImportKey(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*kmsclient.ImportKeyRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.kms.ImportKey", "request is required")
	}
	body := &kmsImportKeyRequest{
		KeyMaterial:   r.KeyMaterial,
		WrappingKeyID: r.WrappingKeyID,
		Name:          r.Name,
		Description:   r.Description,
		Tags:          r.Tags,
		KeyAttributes: &kmsKeyAttributesRequest{
			Algorithm: r.Algorithm.String(),
			KeyUsage:  kmsUsagesToStrings(r.Usage),
		},
	}
	var wire kmsGenerateKeyWire
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/kms/keys/import", body, &wire); err != nil {
		return nil, err
	}
	resp := kmsWireToGenerateKeyResponse(&wire)
	return &service.ImportKeyResponse{
		Key:       resp.Key,
		Metadata:  resp.Metadata,
		Timestamp: resp.Timestamp,
	}, nil
}

func (h *kmsRESTHandler) handleGetKey(ctx context.Context, req interface{}) (interface{}, error) {
	keyID, err := kmsKeyIDFromRequest(req, "rest.kms.GetKey")
	if err != nil {
		return nil, err
	}
	endpoint := "/api/v1/kms/keys/" + url.PathEscape(keyID)
	var wire kmsGenerateKeyWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &wire); err != nil {
		return nil, err
	}
	return &service.GetKeyResponse{
		Key:       kmsWireToKeyHandle(wire.Key),
		Metadata:  kmsWireToKeyMetadata(wire.Metadata),
		Timestamp: time.Unix(wire.Timestamp, 0).UTC(),
	}, nil
}

func (h *kmsRESTHandler) handleListKeys(ctx context.Context, _ interface{}) (interface{}, error) {
	var wire kmsListKeysWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, "/api/v1/kms/keys", nil, &wire); err != nil {
		return nil, err
	}
	out := &service.ListKeysResponse{
		Keys:       make([]*service.KeyHandle, 0, len(wire.Keys)),
		NextCursor: wire.NextCursor,
		HasMore:    wire.HasMore,
		TotalCount: wire.TotalCount,
		Timestamp:  time.Unix(wire.Timestamp, 0).UTC(),
	}
	for _, k := range wire.Keys {
		out.Keys = append(out.Keys, kmsWireToKeyHandle(k))
	}
	return out, nil
}

func (h *kmsRESTHandler) handleRotateKey(ctx context.Context, req interface{}) (interface{}, error) {
	keyID, err := kmsKeyIDFromRequest(req, "rest.kms.RotateKey")
	if err != nil {
		return nil, err
	}
	endpoint := "/api/v1/kms/keys/" + url.PathEscape(keyID) + "/rotate"
	var wire kmsRotateKeyWire
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, nil, &wire); err != nil {
		return nil, err
	}
	return &service.RotateKeyResponse{
		KeyID:      wire.KeyID,
		OldVersion: uint64(wire.OldVersion),
		NewVersion: uint64(wire.NewVersion),
		Timestamp:  time.Unix(wire.Timestamp, 0).UTC(),
	}, nil
}

func (h *kmsRESTHandler) handleDeleteKey(ctx context.Context, req interface{}) (interface{}, error) {
	keyID, err := kmsKeyIDFromRequest(req, "rest.kms.DeleteKey")
	if err != nil {
		return nil, err
	}
	endpoint := "/api/v1/kms/keys/" + url.PathEscape(keyID)
	var wire kmsDeleteKeyWire
	if err := h.transport.doJSONRequest(ctx, http.MethodDelete, endpoint, nil, &wire); err != nil {
		return nil, err
	}
	return &service.DeleteKeyResponse{
		KeyID:        wire.KeyID,
		Status:       service.KeyStatus(wire.Status),
		DeletionDate: time.Unix(wire.DeletionDate, 0).UTC(),
		Timestamp:    time.Unix(wire.Timestamp, 0).UTC(),
	}, nil
}

func (h *kmsRESTHandler) handleExportKey(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*kmsclient.ExportKeyRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.kms.ExportKey", "request is required")
	}
	if r.KeyID == "" {
		return nil, errors.NewInvalidInput("rest.kms.ExportKey", "key ID is required")
	}
	body := &service.ExportKeyRequest{
		KeyID:             r.KeyID,
		WrappingKeyID:     r.WrappingKeyID,
		WrappingAlgorithm: kmsWrappingAlgToString(r.WrappingAlgorithm),
		Format:            r.Format.String(),
		Version:           uint64(r.Version),
	}
	endpoint := "/api/v1/kms/keys/" + url.PathEscape(r.KeyID) + "/export"
	var wire kmsExportKeyWire
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, endpoint, body, &wire); err != nil {
		return nil, err
	}
	return &service.ExportKeyResponse{
		KeyID:             wire.KeyID,
		Version:           wire.Version,
		WrappedKey:        wire.WrappedKey,
		WrappingKeyID:     wire.WrappingKeyID,
		WrappingAlgorithm: wire.WrappingAlgorithm,
		Format:            wire.Format,
		Timestamp:         time.Unix(wire.Timestamp, 0).UTC(),
	}, nil
}

func (h *kmsRESTHandler) handleUnwrapKey(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*kmsclient.UnwrapKeyRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.kms.UnwrapKey", "request is required")
	}
	if len(r.WrappedMaterial) == 0 {
		return nil, errors.NewInvalidInput("rest.kms.UnwrapKey", "wrapped material is required")
	}
	body := &service.UnwrapKeyRequest{
		KeyID:             r.KeyID,
		WrappedKey:        r.WrappedMaterial,
		WrappingKeyID:     r.WrappingKeyID,
		WrappingAlgorithm: kmsWrappingAlgToString(r.WrappingAlgorithm),
		Algorithm:         r.Algorithm.String(),
		SourceKeyID:       r.SourceKeyID,
		SourceVersion:     uint64(r.SourceVersion),
		Name:              r.Name,
	}
	var wire kmsUnwrapKeyWire
	if err := h.transport.doJSONRequest(ctx, http.MethodPost, "/api/v1/kms/keys/unwrap", body, &wire); err != nil {
		return nil, err
	}
	return &service.UnwrapKeyResponse{
		KeyID:     wire.KeyID,
		Version:   wire.Version,
		Algorithm: service.KeyAlgorithm(wire.Algorithm),
		Status:    service.KeyStatus(wire.Status),
		Key:       kmsWireToKeyHandle(wire.Key),
		Timestamp: time.Unix(wire.Timestamp, 0).UTC(),
	}, nil
}

func (h *kmsRESTHandler) handleUpdateKeyStatus(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*kmsclient.UpdateKeyStatusRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.kms.UpdateKeyStatus", "request is required")
	}
	if r.KeyID == "" {
		return nil, errors.NewInvalidInput("rest.kms.UpdateKeyStatus", "key ID is required")
	}
	body := &kmsUpdateKeyStatusRequest{Status: kmsStatusToString(r.Status)}
	endpoint := "/api/v1/kms/keys/" + url.PathEscape(r.KeyID) + "/status"
	var wire kmsUpdateKeyStatusWire
	if err := h.transport.doJSONRequest(ctx, http.MethodPut, endpoint, body, &wire); err != nil {
		return nil, err
	}
	return &service.UpdateKeyStatusResponse{
		KeyID:     wire.KeyID,
		NewStatus: service.KeyStatus(wire.Status),
		Status:    service.KeyStatus(wire.Status),
		Timestamp: time.Unix(wire.Timestamp, 0).UTC(),
	}, nil
}

func (h *kmsRESTHandler) handleListKeyVersions(ctx context.Context, req interface{}) (interface{}, error) {
	keyID, err := kmsKeyIDFromRequest(req, "rest.kms.ListKeyVersions")
	if err != nil {
		return nil, err
	}
	endpoint := "/api/v1/kms/keys/" + url.PathEscape(keyID) + "/versions"
	var wire kmsListKeyVersionsWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &wire); err != nil {
		return nil, err
	}
	out := &service.ListKeyVersionsResponse{
		KeyID:      wire.KeyID,
		Versions:   make([]*service.KeyVersionInfo, 0, len(wire.Versions)),
		NextCursor: wire.NextCursor,
		HasMore:    wire.HasMore,
		Timestamp:  time.Unix(wire.Timestamp, 0).UTC(),
	}
	for _, v := range wire.Versions {
		out.Versions = append(out.Versions, &service.KeyVersionInfo{
			Version:   uint64(v.Version),
			Status:    service.KeyStatus(v.Status),
			Algorithm: service.KeyAlgorithm(v.Algorithm),
			CreatedAt: time.Unix(v.CreatedAt, 0).UTC(),
		})
	}
	return out, nil
}

func (h *kmsRESTHandler) handleSetKeyPolicy(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*kmsclient.SetKeyPolicyRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.kms.SetKeyPolicy", "request is required")
	}
	if r.KeyID == "" {
		return nil, errors.NewInvalidInput("rest.kms.SetKeyPolicy", "key ID is required")
	}
	if r.Policy == nil {
		return nil, errors.NewInvalidInput("rest.kms.SetKeyPolicy", "policy is required")
	}
	body := kmsPolicyToWire(r.KeyID, r.Policy)
	endpoint := "/api/v1/kms/keys/" + url.PathEscape(r.KeyID) + "/policy"
	var wire kmsSetKeyPolicyWire
	if err := h.transport.doJSONRequest(ctx, http.MethodPut, endpoint, body, &wire); err != nil {
		return nil, err
	}
	return &service.SetKeyPolicyResponse{
		KeyID:         wire.KeyID,
		PolicyVersion: wire.PolicyVersion,
		Timestamp:     time.Unix(wire.Timestamp, 0).UTC(),
	}, nil
}

func (h *kmsRESTHandler) handleGetKeyPolicy(ctx context.Context, req interface{}) (interface{}, error) {
	keyID, err := kmsKeyIDFromRequest(req, "rest.kms.GetKeyPolicy")
	if err != nil {
		return nil, err
	}
	endpoint := "/api/v1/kms/keys/" + url.PathEscape(keyID) + "/policy"
	var wire kmsGetKeyPolicyWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, endpoint, nil, &wire); err != nil {
		return nil, err
	}
	return &service.GetKeyPolicyResponse{
		Policy:    kmsWireToPolicy(wire.Policy),
		Timestamp: time.Unix(wire.Timestamp, 0).UTC(),
	}, nil
}

func (h *kmsRESTHandler) handleBackendStatus(ctx context.Context, _ interface{}) (interface{}, error) {
	var wire kmsBackendStatusWire
	if err := h.transport.doJSONRequest(ctx, http.MethodGet, "/api/v1/kms/backend/status", nil, &wire); err != nil {
		return nil, err
	}
	return &service.BackendStatusResponse{
		BackendType:  wire.BackendType,
		Status:       wire.Status,
		Capabilities: wire.Capabilities,
		Timestamp:    time.Unix(wire.Timestamp, 0).UTC(),
	}, nil
}

// =============================================================================
// Conversion helpers
// =============================================================================

func kmsWireToGenerateKeyResponse(wire *kmsGenerateKeyWire) *service.GenerateKeyResponse {
	return &service.GenerateKeyResponse{
		Key:       kmsWireToKeyHandle(wire.Key),
		Metadata:  kmsWireToKeyMetadata(wire.Metadata),
		Timestamp: time.Unix(wire.Timestamp, 0).UTC(),
	}
}

func kmsWireToKeyHandle(wire *kmsKeyHandleWire) *service.KeyHandle {
	if wire == nil {
		return nil
	}
	return &service.KeyHandle{
		KeyID:     wire.KeyID,
		Version:   uint64(wire.Version),
		Algorithm: service.KeyAlgorithm(wire.Algorithm),
		Status:    service.KeyStatus(wire.Status),
		CreatedAt: time.Unix(wire.CreatedAt, 0).UTC(),
	}
}

func kmsWireToKeyMetadata(wire *kmsKeyMetadataWire) *service.KeyMetadata {
	if wire == nil {
		return nil
	}
	meta := &service.KeyMetadata{
		KeyID:              wire.KeyID,
		Name:               wire.Name,
		Description:        wire.Description,
		Algorithm:          service.KeyAlgorithm(wire.Algorithm),
		Status:             service.KeyStatus(wire.Status),
		Usage:              make([]service.KeyUsage, 0, len(wire.Usage)),
		Version:            uint64(wire.Version),
		Exportable:         wire.Exportable,
		DeletionProtection: wire.DeletionProtection,
		CreatedAt:          time.Unix(wire.CreatedAt, 0).UTC(),
		UpdatedAt:          time.Unix(wire.UpdatedAt, 0).UTC(),
		Tags:               wire.Tags,
		Owner:              wire.Owner,
	}
	for _, u := range wire.Usage {
		meta.Usage = append(meta.Usage, service.KeyUsage(u))
	}
	if wire.LastUsedAt != 0 {
		t := time.Unix(wire.LastUsedAt, 0).UTC()
		meta.LastUsedAt = &t
	}
	return meta
}

func kmsWireToPolicy(wire *kmsKeyPolicyWire) *service.KeyPolicy {
	if wire == nil {
		return nil
	}
	policy := &service.KeyPolicy{
		KeyID:             wire.KeyID,
		Version:           wire.Version,
		AllowedOperations: make([]service.KeyUsage, 0, len(wire.AllowedOperations)),
		AllowedPrincipals: wire.AllowedPrincipals,
		DeniedPrincipals:  wire.DeniedPrincipals,
		RotationRequired:  wire.RotationRequired,
		RequireMFA:        wire.RequireMFA,
		UpdatedAt:         time.Unix(wire.UpdatedAt, 0).UTC(),
	}
	for _, op := range wire.AllowedOperations {
		policy.AllowedOperations = append(policy.AllowedOperations, service.KeyUsage(op))
	}
	if wire.MaxUsageCount != 0 {
		v := wire.MaxUsageCount
		policy.MaxUsageCount = &v
	}
	if wire.ExpiresAt != 0 {
		t := time.Unix(wire.ExpiresAt, 0).UTC()
		policy.ExpiresAt = &t
	}
	return policy
}

func kmsPolicyToWire(keyID string, policy *kmsclient.KeyPolicy) *kmsKeyPolicyWire {
	wire := &kmsKeyPolicyWire{
		KeyID:             keyID,
		Version:           uint64(policy.Version),
		AllowedOperations: make([]string, 0, len(policy.AllowedOperations)),
		AllowedPrincipals: policy.AllowedPrincipals,
		DeniedPrincipals:  policy.DeniedPrincipals,
		RotationRequired:  policy.RotationRequired,
		MaxUsageCount:     policy.MaxUsageCount,
		RequireMFA:        policy.RequireMFA,
	}
	for _, op := range policy.AllowedOperations {
		wire.AllowedOperations = append(wire.AllowedOperations, kmsUsageToString(op))
	}
	if !policy.ExpiresAt.IsZero() {
		wire.ExpiresAt = policy.ExpiresAt.Unix()
	}
	return wire
}

// kmsKeyIDFromRequest extracts the KeyID field from any KMS request struct that
// carries one. The internal request types (getKeyRequest, rotateKeyRequest,
// etc.) are unexported, so the field is read via a minimal interface match on
// the concrete pointer types the SDK client constructs.
func kmsKeyIDFromRequest(req interface{}, op string) (string, error) {
	keyID, ok := extractKeyIDField(req)
	if !ok || keyID == "" {
		return "", errors.NewInvalidInput(op, "key ID is required")
	}
	return keyID, nil
}

// kmsUsagesToStrings converts SDK key usages into the lowercase service
// convention the server stores (service.KeyUsage values).
func kmsUsagesToStrings(usages []kmsclient.KeyUsage) []string {
	out := make([]string, 0, len(usages))
	for _, u := range usages {
		out = append(out, kmsUsageToString(u))
	}
	return out
}

func kmsUsageToString(u kmsclient.KeyUsage) string {
	switch u {
	case kmsclient.KeyUsageEncrypt:
		return string(service.KeyUsageEncrypt)
	case kmsclient.KeyUsageDecrypt:
		return string(service.KeyUsageDecrypt)
	case kmsclient.KeyUsageSign:
		return string(service.KeyUsageSign)
	case kmsclient.KeyUsageVerify:
		return string(service.KeyUsageVerify)
	case kmsclient.KeyUsageWrap:
		return string(service.KeyUsageWrapKey)
	case kmsclient.KeyUsageUnwrap:
		return string(service.KeyUsageWrapKey)
	default:
		return ""
	}
}

func kmsStatusToString(s kmsclient.KeyStatus) string {
	switch s {
	case kmsclient.KeyStatusEnabled:
		return string(service.KeyStatusEnabled)
	case kmsclient.KeyStatusDisabled:
		return string(service.KeyStatusDisabled)
	case kmsclient.KeyStatusPendingDeletion:
		return string(service.KeyStatusPendingDeletion)
	case kmsclient.KeyStatusDeleted:
		return string(service.KeyStatusDeleted)
	default:
		return ""
	}
}

// kmsWrappingAlgToString maps the SDK wrapping-algorithm enum to the wire
// string the server's xkms backend understands.
func kmsWrappingAlgToString(a kmsclient.WrappingAlgorithm) string {
	switch a {
	case kmsclient.WrapAlgAESKW:
		return "AES-KW"
	case kmsclient.WrapAlgAESKWP:
		return "AES-KWP"
	case kmsclient.WrapAlgAESGCM:
		return "AES-GCM"
	case kmsclient.WrapAlgRSAOAEPSHA256:
		return "RSA-OAEP-SHA256"
	case kmsclient.WrapAlgRSAOAEPSHA384:
		return "RSA-OAEP-SHA384"
	case kmsclient.WrapAlgRSAOAEPSHA512:
		return "RSA-OAEP-SHA512"
	case kmsclient.WrapAlgECDHESA256KW:
		return "ECDH-ES-A256KW"
	default:
		return ""
	}
}
