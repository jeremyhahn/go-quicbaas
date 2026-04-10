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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// isBackupService returns true if the service is a Backup service.
func isBackupService(svc string) bool {
	return strings.HasPrefix(svc, "quicbaas.Backup") || svc == "Backup"
}

// backupHandler handles Backup-specific request/response conversions for REST transport.
type backupHandler struct {
	transport *Transport
}

// newBackupHandler creates a new Backup handler.
func newBackupHandler(t *Transport) *backupHandler {
	return &backupHandler{transport: t}
}

// handleRequest routes Backup requests to the REST transport and converts responses.
func (h *backupHandler) handleRequest(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
	reqMap, _ := toMap(req)

	switch method {
	case "Create":
		raw, err := h.doRequest(ctx, http.MethodPost, "/api/v1/backups", req)
		if err != nil {
			return nil, err
		}
		return h.convertResponse(method, raw)

	case "List":
		raw, err := h.doRequest(ctx, http.MethodGet, "/api/v1/backups", nil)
		if err != nil {
			return nil, err
		}
		return h.convertResponse(method, raw)

	case "Delete":
		backupID := getBackupID(reqMap)
		if backupID == "" {
			return nil, errors.NewInvalidInput("rest.backupHandler.handleRequest", "backup_id is required")
		}
		raw, err := h.doRequest(ctx, http.MethodDelete, "/api/v1/backups/"+backupID, nil)
		if err != nil {
			return nil, err
		}
		return h.convertResponse(method, raw)

	case "Restore":
		backupID := getBackupID(reqMap)
		if backupID == "" {
			return nil, errors.NewInvalidInput("rest.backupHandler.handleRequest", "backup_id is required")
		}
		raw, err := h.doRequest(ctx, http.MethodPost, "/api/v1/backups/"+backupID+"/restore", req)
		if err != nil {
			return nil, err
		}
		return h.convertResponse(method, raw)

	case "Verify":
		backupID := getBackupID(reqMap)
		if backupID == "" {
			return nil, errors.NewInvalidInput("rest.backupHandler.handleRequest", "backup_id is required")
		}
		raw, err := h.doRequest(ctx, http.MethodPost, "/api/v1/backups/"+backupID+"/verify", req)
		if err != nil {
			return nil, err
		}
		return h.convertResponse(method, raw)

	case "Status":
		backupID := getBackupID(reqMap)
		if backupID == "" {
			return nil, errors.NewInvalidInput("rest.backupHandler.handleRequest", "backup_id is required")
		}
		raw, err := h.doRequest(ctx, http.MethodGet, "/api/v1/backups/"+backupID+"/status", nil)
		if err != nil {
			return nil, err
		}
		return h.convertResponse(method, raw)

	default:
		raw, err := h.transport.requestWithRetry(ctx, svc, method, req)
		if err != nil {
			return nil, err
		}
		return h.convertResponse(method, raw)
	}
}

// doRequest performs an HTTP request with the given method, path, and optional body.
func (h *backupHandler) doRequest(ctx context.Context, httpMethod, path string, body interface{}) (interface{}, error) {
	if h.transport.closed.Load() {
		return nil, errors.NewConnection("rest.backupHandler.doRequest", "transport is closed")
	}

	url := h.transport.baseURL + h.transport.basePath + path

	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, errors.Wrap(err, errors.CodeInvalidInput, "rest.backupHandler.doRequest", "failed to encode request body")
		}
		reqBody = bytes.NewReader(data)
	}

	httpReq, err := http.NewRequestWithContext(ctx, httpMethod, url, reqBody)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "rest.backupHandler.doRequest", "failed to create HTTP request")
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	h.transport.mu.RLock()
	for k, v := range h.transport.headers {
		httpReq.Header.Set(k, v)
	}
	h.transport.mu.RUnlock()

	resp, err := h.transport.client.Do(httpReq)
	if err != nil {
		return nil, h.transport.convertHTTPError(err, "rest.backupHandler.doRequest")
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, "rest.backupHandler.doRequest", "failed to read response body")
	}

	if resp.StatusCode >= 400 {
		return nil, h.transport.parseErrorResponse(resp.StatusCode, respBody)
	}

	var result interface{}
	if len(respBody) > 0 {
		if err := json.Unmarshal(respBody, &result); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal, "rest.backupHandler.doRequest", "failed to decode response")
		}
	}

	return result, nil
}

// getBackupID extracts the backup ID from a request map, checking both snake_case and PascalCase keys.
func getBackupID(m map[string]interface{}) string {
	if m == nil {
		return ""
	}
	if v, ok := m["backup_id"].(string); ok && v != "" {
		return v
	}
	if v, ok := m["BackupID"].(string); ok && v != "" {
		return v
	}
	return ""
}

// convertResponse converts a raw map response into a typed service response.
func (h *backupHandler) convertResponse(method string, raw interface{}) (interface{}, error) {
	if raw == nil {
		return nil, nil
	}

	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil, errors.NewInternal("rest.backupHandler.convertResponse", "unexpected response type")
	}

	switch method {
	case "Create":
		return h.mapToCreateBackupResponse(m), nil
	case "List":
		return h.mapToListBackupsResponse(m), nil
	case "Delete":
		return h.mapToDeleteBackupResponse(m), nil
	case "Restore":
		return h.mapToRestoreBackupResponse(m), nil
	case "Verify":
		return h.mapToVerifyBackupResponse(m), nil
	default:
		return raw, nil
	}
}

// mapToCreateBackupResponse converts a raw map to a CreateBackupResponse.
func (h *backupHandler) mapToCreateBackupResponse(data map[string]interface{}) *service.CreateBackupResponse {
	resp := &service.CreateBackupResponse{}

	if v, ok := getStringField(data, "backup_id", "BackupID"); ok {
		resp.BackupID = v
	}
	if v, ok := getUint64Field(data, "size", "Size"); ok {
		resp.Size = v
	}
	if v, ok := getInt64Field(data, "timestamp", "Timestamp"); ok {
		resp.Timestamp = v
	}
	if v, ok := getStringField(data, "status", "Status"); ok {
		resp.Status = v
	}

	return resp
}

// mapToListBackupsResponse converts a raw map to a ListBackupsResponse.
func (h *backupHandler) mapToListBackupsResponse(data map[string]interface{}) *service.ListBackupsResponse {
	resp := &service.ListBackupsResponse{}

	if v, ok := getUint64Field(data, "total_count", "TotalCount"); ok {
		resp.TotalCount = v
	}

	backups := getSliceField(data, "backups", "Backups")
	if backups != nil {
		resp.Backups = make([]*service.BackupMetadata, 0, len(backups))
		for _, item := range backups {
			if m, ok := item.(map[string]interface{}); ok {
				resp.Backups = append(resp.Backups, mapToBackupMetadata(m))
			}
		}
	}

	return resp
}

// mapToBackupMetadata converts a raw map to a BackupMetadata.
func mapToBackupMetadata(data map[string]interface{}) *service.BackupMetadata {
	meta := &service.BackupMetadata{}

	if v, ok := getStringField(data, "backup_id", "BackupID"); ok {
		meta.BackupID = v
	}
	if v, ok := getStringField(data, "name", "Name"); ok {
		meta.Name = v
	}
	if v, ok := getStringField(data, "description", "Description"); ok {
		meta.Description = v
	}
	if v, ok := getUint64Field(data, "size", "Size"); ok {
		meta.Size = v
	}
	if v, ok := getInt64Field(data, "timestamp", "Timestamp"); ok {
		meta.Timestamp = v
	}
	if v, ok := getStringField(data, "status", "Status"); ok {
		meta.Status = v
	}

	shards := getSliceField(data, "shards", "Shards")
	if shards != nil {
		meta.Shards = make([]uint64, 0, len(shards))
		for _, item := range shards {
			if f, ok := item.(float64); ok {
				meta.Shards = append(meta.Shards, uint64(f))
			}
		}
	}

	return meta
}

// mapToDeleteBackupResponse converts a raw map to a DeleteBackupResponse.
func (h *backupHandler) mapToDeleteBackupResponse(data map[string]interface{}) *service.DeleteBackupResponse {
	resp := &service.DeleteBackupResponse{}

	if v, ok := getBoolField(data, "success", "Success"); ok {
		resp.Success = v
	}
	if v, ok := getStringField(data, "message", "Message"); ok {
		resp.Message = v
	}

	return resp
}

// mapToRestoreBackupResponse converts a raw map to a RestoreBackupResponse.
func (h *backupHandler) mapToRestoreBackupResponse(data map[string]interface{}) *service.RestoreBackupResponse {
	resp := &service.RestoreBackupResponse{}

	if v, ok := getStringField(data, "job_id", "JobID"); ok {
		resp.JobID = v
	}
	if v, ok := getStringField(data, "status", "Status"); ok {
		resp.Status = v
	}
	if v, ok := getFloat64Field(data, "progress", "Progress"); ok {
		resp.Progress = v
	}

	return resp
}

// mapToVerifyBackupResponse converts a raw map to a VerifyBackupResponse.
func (h *backupHandler) mapToVerifyBackupResponse(data map[string]interface{}) *service.VerifyBackupResponse {
	resp := &service.VerifyBackupResponse{}

	if v, ok := getBoolField(data, "valid", "Valid"); ok {
		resp.Valid = v
	}
	if v, ok := getBoolField(data, "checksum_match", "ChecksumMatch"); ok {
		resp.ChecksumMatch = v
	}

	issues := getSliceField(data, "issues", "Issues")
	if issues != nil {
		resp.Issues = make([]string, 0, len(issues))
		for _, item := range issues {
			if s, ok := item.(string); ok {
				resp.Issues = append(resp.Issues, s)
			}
		}
	}

	return resp
}

// getStringField retrieves a string value from a map, checking both snake_case and PascalCase keys.
func getStringField(data map[string]interface{}, snakeKey, pascalKey string) (string, bool) {
	if v, ok := data[snakeKey].(string); ok {
		return v, true
	}
	if v, ok := data[pascalKey].(string); ok {
		return v, true
	}
	return "", false
}

// getUint64Field retrieves a uint64 value from a map, checking both snake_case and PascalCase keys.
// JSON numbers decode as float64, so the conversion handles that.
func getUint64Field(data map[string]interface{}, snakeKey, pascalKey string) (uint64, bool) {
	if v, ok := data[snakeKey].(float64); ok {
		return uint64(v), true
	}
	if v, ok := data[pascalKey].(float64); ok {
		return uint64(v), true
	}
	return 0, false
}

// getInt64Field retrieves an int64 value from a map, checking both snake_case and PascalCase keys.
// JSON numbers decode as float64, so the conversion handles that.
func getInt64Field(data map[string]interface{}, snakeKey, pascalKey string) (int64, bool) {
	if v, ok := data[snakeKey].(float64); ok {
		return int64(v), true
	}
	if v, ok := data[pascalKey].(float64); ok {
		return int64(v), true
	}
	return 0, false
}

// getFloat64Field retrieves a float64 value from a map, checking both snake_case and PascalCase keys.
func getFloat64Field(data map[string]interface{}, snakeKey, pascalKey string) (float64, bool) {
	if v, ok := data[snakeKey].(float64); ok {
		return v, true
	}
	if v, ok := data[pascalKey].(float64); ok {
		return v, true
	}
	return 0, false
}

// getBoolField retrieves a bool value from a map, checking both snake_case and PascalCase keys.
func getBoolField(data map[string]interface{}, snakeKey, pascalKey string) (bool, bool) {
	if v, ok := data[snakeKey].(bool); ok {
		return v, true
	}
	if v, ok := data[pascalKey].(bool); ok {
		return v, true
	}
	return false, false
}

// getSliceField retrieves a slice value from a map, checking both snake_case and PascalCase keys.
func getSliceField(data map[string]interface{}, snakeKey, pascalKey string) []interface{} {
	if v, ok := data[snakeKey].([]interface{}); ok {
		return v
	}
	if v, ok := data[pascalKey].([]interface{}); ok {
		return v
	}
	return nil
}
