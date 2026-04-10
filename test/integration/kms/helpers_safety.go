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

//go:build integration && quic

package kms

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// =============================================================================
// REST Response Types (for JSON unmarshaling with int64 timestamps)
// =============================================================================

// setKeySafetyRESTResponse is the REST API response for SetKeySafety.
type setKeySafetyRESTResponse struct {
	KeyID     string                    `json:"key_id"`
	Config    *aeadSafetyConfigResponse `json:"config"`
	Timestamp int64                     `json:"timestamp"`
}

// clearKeySafetyRESTResponse is the REST API response for ClearKeySafety.
type clearKeySafetyRESTResponse struct {
	KeyID     string `json:"key_id"`
	Timestamp int64  `json:"timestamp"`
}

// safetyCheckItemResponse represents a safety check item in REST response.
type safetyCheckItemResponse struct {
	ID             string  `json:"id"`
	Type           string  `json:"type"`
	OperationCount uint64  `json:"operation_count"`
	BytesProcessed uint64  `json:"bytes_processed"`
	UsagePercent   float64 `json:"usage_percent"`
	MaxOperations  uint64  `json:"max_operations"`
	MaxBytes       uint64  `json:"max_bytes"`
}

// safetyCheckRESTResponse is the REST API response for SafetyCheck.
type safetyCheckRESTResponse struct {
	Items            []safetyCheckItemResponse `json:"items"`
	ThresholdPercent float64                   `json:"threshold_percent"`
	TotalKeysChecked int                       `json:"total_keys_checked"`
	TotalDEKsChecked int                       `json:"total_deks_checked"`
	Timestamp        int64                     `json:"timestamp"`
}

// aeadServerConfigResponse represents server-wide AEAD configuration in REST response.
type aeadServerConfigResponse struct {
	Enforce           bool   `json:"enforce"`
	MaxOperations     uint64 `json:"max_operations,omitempty"`
	MaxBytes          uint64 `json:"max_bytes,omitempty"`
	WarningPercent    int    `json:"warning_percent,omitempty"`
	AutoRotatePercent int    `json:"auto_rotate_percent,omitempty"`
}

// safetyReportRESTResponse is the REST API response for SafetyReport.
type safetyReportRESTResponse struct {
	Items               []safetyCheckItemResponse `json:"items"`
	TotalOperations     uint64                    `json:"total_operations"`
	TotalBytesProcessed uint64                    `json:"total_bytes_processed"`
	ServerConfig        aeadServerConfigResponse  `json:"server_config"`
	Timestamp           int64                     `json:"timestamp"`
}

// aeadSafetyConfigResponse represents AEAD safety configuration in REST response.
type aeadSafetyConfigResponse struct {
	MaxOperations     uint64 `json:"max_operations,omitempty"`
	MaxBytes          uint64 `json:"max_bytes,omitempty"`
	WarningPercent    int    `json:"warning_percent,omitempty"`
	AutoRotatePercent int    `json:"auto_rotate_percent,omitempty"`
}

// exportKeyRESTResponse is the REST API response for ExportKey.
type exportKeyRESTResponse struct {
	KeyID             string `json:"key_id"`
	Version           uint64 `json:"version,omitempty"`
	WrappedKey        []byte `json:"wrapped_key"`
	WrappingKeyID     string `json:"wrapping_key_id"`
	WrappingAlgorithm string `json:"wrapping_algorithm"`
	Format            string `json:"format"`
	Timestamp         int64  `json:"timestamp"`
}

// unwrapKeyRESTResponse is the REST API response for UnwrapKey.
type unwrapKeyRESTResponse struct {
	KeyID     string               `json:"key_id"`
	Version   uint64               `json:"version"`
	Algorithm service.KeyAlgorithm `json:"algorithm"`
	Status    service.KeyStatus    `json:"status"`
	Key       *service.KeyHandle   `json:"key,omitempty"`
	Timestamp int64                `json:"timestamp"`
}

// =============================================================================
// REST Client - Key Export/Import Operations
// =============================================================================

// ExportKey exports a key wrapped with a wrapping key via REST API.
func (c *RESTClient) ExportKey(ctx context.Context, req *service.ExportKeyRequest) (*service.ExportKeyResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/keys/%s/export", c.baseURL, req.KeyID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	// Unmarshal to REST response type with int64 timestamp
	var restResult exportKeyRESTResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	// Convert to service type with time.Time
	result := &service.ExportKeyResponse{
		KeyID:             restResult.KeyID,
		Version:           restResult.Version,
		WrappedKey:        restResult.WrappedKey,
		WrappingKeyID:     restResult.WrappingKeyID,
		WrappingAlgorithm: restResult.WrappingAlgorithm,
		Format:            restResult.Format,
		Timestamp:         time.Unix(restResult.Timestamp, 0),
	}

	return result, nil
}

// UnwrapKey imports a wrapped key via REST API.
func (c *RESTClient) UnwrapKey(ctx context.Context, req *service.UnwrapKeyRequest) (*service.UnwrapKeyResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/keys/unwrap", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	// Unmarshal to REST response type with int64 timestamp
	var restResult unwrapKeyRESTResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	// Convert to service type with time.Time
	result := &service.UnwrapKeyResponse{
		KeyID:     restResult.KeyID,
		Version:   restResult.Version,
		Algorithm: restResult.Algorithm,
		Status:    restResult.Status,
		Key:       restResult.Key,
		Timestamp: time.Unix(restResult.Timestamp, 0),
	}

	return result, nil
}

// =============================================================================
// REST Client - Key Safety Operations
// =============================================================================

// SetKeySafety sets AEAD safety limits for a key via REST API.
func (c *RESTClient) SetKeySafety(ctx context.Context, req *service.SetKeySafetyRequest) (*service.SetKeySafetyResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/keys/%s/safety", c.baseURL, req.KeyID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	// Unmarshal to REST response type with int64 timestamp
	var restResult setKeySafetyRESTResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	// Convert to service type with time.Time
	result := &service.SetKeySafetyResponse{
		KeyID:     restResult.KeyID,
		Timestamp: time.Unix(restResult.Timestamp, 0),
	}
	if restResult.Config != nil {
		result.Config = service.AEADSafetyConfigDTO{
			MaxOperations:     restResult.Config.MaxOperations,
			MaxBytes:          restResult.Config.MaxBytes,
			WarningPercent:    restResult.Config.WarningPercent,
			AutoRotatePercent: restResult.Config.AutoRotatePercent,
		}
	}

	return result, nil
}

// ClearKeySafety clears AEAD safety limits for a key via REST API.
func (c *RESTClient) ClearKeySafety(ctx context.Context, req *service.ClearKeySafetyRequest) (*service.ClearKeySafetyResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/keys/%s/safety", c.baseURL, req.KeyID)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	// Unmarshal to REST response type with int64 timestamp
	var restResult clearKeySafetyRESTResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	// Convert to service type with time.Time
	result := &service.ClearKeySafetyResponse{
		KeyID:     restResult.KeyID,
		Timestamp: time.Unix(restResult.Timestamp, 0),
	}

	return result, nil
}

// SafetyCheck performs a safety check for keys approaching limits via REST API.
func (c *RESTClient) SafetyCheck(ctx context.Context, req *service.SafetyCheckRequest) (*service.SafetyCheckResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/safety/check", c.baseURL)
	if req.ThresholdPercent > 0 {
		url += fmt.Sprintf("?threshold_percent=%.2f", req.ThresholdPercent)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	// Unmarshal to REST response type with int64 timestamp
	var restResult safetyCheckRESTResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	// Convert items to service type
	items := make([]service.SafetyCheckItem, len(restResult.Items))
	for i, item := range restResult.Items {
		items[i] = service.SafetyCheckItem{
			ID:             item.ID,
			Type:           item.Type,
			OperationCount: item.OperationCount,
			BytesProcessed: item.BytesProcessed,
			UsagePercent:   item.UsagePercent,
			MaxOperations:  item.MaxOperations,
			MaxBytes:       item.MaxBytes,
		}
	}

	// Convert to service type with time.Time
	result := &service.SafetyCheckResponse{
		Items:            items,
		ThresholdPercent: restResult.ThresholdPercent,
		TotalKeysChecked: restResult.TotalKeysChecked,
		TotalDEKsChecked: restResult.TotalDEKsChecked,
		Timestamp:        time.Unix(restResult.Timestamp, 0),
	}

	return result, nil
}

// SafetyReport gets a comprehensive safety report via REST API.
func (c *RESTClient) SafetyReport(ctx context.Context, req *service.SafetyReportRequest) (*service.SafetyReportResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/safety/report", c.baseURL)
	if req.IncludeDetails {
		url += "?include_details=true"
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	// Unmarshal to REST response type with int64 timestamp
	var restResult safetyReportRESTResponse
	if err := json.Unmarshal(respBody, &restResult); err != nil {
		return nil, err
	}

	// Convert items to service type
	items := make([]service.SafetyCheckItem, len(restResult.Items))
	for i, item := range restResult.Items {
		items[i] = service.SafetyCheckItem{
			ID:             item.ID,
			Type:           item.Type,
			OperationCount: item.OperationCount,
			BytesProcessed: item.BytesProcessed,
			UsagePercent:   item.UsagePercent,
			MaxOperations:  item.MaxOperations,
			MaxBytes:       item.MaxBytes,
		}
	}

	// Convert to service type with time.Time
	result := &service.SafetyReportResponse{
		Items:               items,
		TotalOperations:     restResult.TotalOperations,
		TotalBytesProcessed: restResult.TotalBytesProcessed,
		ServerConfig: service.AEADServerConfig{
			Enforce:           restResult.ServerConfig.Enforce,
			MaxOperations:     restResult.ServerConfig.MaxOperations,
			MaxBytes:          restResult.ServerConfig.MaxBytes,
			WarningPercent:    restResult.ServerConfig.WarningPercent,
			AutoRotatePercent: restResult.ServerConfig.AutoRotatePercent,
		},
		Timestamp: time.Unix(restResult.Timestamp, 0),
	}

	return result, nil
}

// =============================================================================
// REST Client - DEK Operations
// =============================================================================

// CreateDEK creates a new DEK via REST API.
func (c *RESTClient) CreateDEK(ctx context.Context, req *service.CreateDEKRequest) (*service.CreateDEKResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/deks", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.CreateDEKResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetDEK retrieves a DEK via REST API.
func (c *RESTClient) GetDEK(ctx context.Context, req *service.GetDEKRequest) (*service.GetDEKResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/deks/%s", c.baseURL, req.Path)
	if req.Version > 0 {
		url += fmt.Sprintf("?version=%d", req.Version)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.GetDEKResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ListDEKs lists DEKs via REST API.
func (c *RESTClient) ListDEKs(ctx context.Context, req *service.ListDEKsRequest) (*service.ListDEKsResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/deks", c.baseURL)

	// Build query parameters
	params := []string{}
	if req.Prefix != "" {
		params = append(params, fmt.Sprintf("prefix=%s", req.Prefix))
	}
	if req.Limit > 0 {
		params = append(params, fmt.Sprintf("limit=%d", req.Limit))
	}
	if req.Cursor != "" {
		params = append(params, fmt.Sprintf("cursor=%s", req.Cursor))
	}
	if req.Recursive {
		params = append(params, "recursive=true")
	}
	if len(params) > 0 {
		url += "?"
		for i, p := range params {
			if i > 0 {
				url += "&"
			}
			url += p
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.ListDEKsResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// RotateDEK rotates a DEK via REST API.
func (c *RESTClient) RotateDEK(ctx context.Context, req *service.RotateDEKRequest) (*service.RotateDEKResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/deks/%s/rotate", c.baseURL, req.Path)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.RotateDEKResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// DeleteDEK deletes a DEK via REST API.
func (c *RESTClient) DeleteDEK(ctx context.Context, req *service.DeleteDEKRequest) (*service.DeleteDEKResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/deks/%s", c.baseURL, req.Path)

	// Add query parameters
	params := []string{}
	if req.Force {
		params = append(params, "force=true")
	}
	if req.MigrateTo != "" {
		params = append(params, fmt.Sprintf("migrate_to=%s", req.MigrateTo))
	}
	if len(params) > 0 {
		url += "?"
		for i, p := range params {
			if i > 0 {
				url += "&"
			}
			url += p
		}
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.DeleteDEKResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// GetDEKStatus retrieves DEK status via REST API.
func (c *RESTClient) GetDEKStatus(ctx context.Context, req *service.GetDEKStatusRequest) (*service.GetDEKStatusResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/deks/%s/status", c.baseURL, req.Path)

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.GetDEKStatusResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SetDefaultDEK sets the default DEK via REST API.
func (c *RESTClient) SetDefaultDEK(ctx context.Context, req *service.SetDefaultDEKRequest) (*service.SetDefaultDEKResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/deks/default", c.baseURL)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.SetDefaultDEKResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SetDEKSafety sets AEAD safety limits for a DEK via REST API.
func (c *RESTClient) SetDEKSafety(ctx context.Context, req *service.SetDEKSafetyRequest) (*service.SetDEKSafetyResponse, error) {
	bodyBytes, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("%s/api/v1/kms/deks/%s/safety", c.baseURL, req.Path)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPut, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.SetDEKSafetyResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}

// ClearDEKSafety clears AEAD safety limits for a DEK via REST API.
func (c *RESTClient) ClearDEKSafety(ctx context.Context, req *service.ClearDEKSafetyRequest) (*service.ClearDEKSafetyResponse, error) {
	url := fmt.Sprintf("%s/api/v1/kms/deks/%s/safety", c.baseURL, req.Path)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d: %s", resp.StatusCode, string(respBody))
	}

	var result service.ClearDEKSafetyResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, err
	}

	return &result, nil
}
