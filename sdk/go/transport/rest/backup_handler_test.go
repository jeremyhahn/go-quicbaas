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
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func TestNewBackupHandler(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	assert.NotNil(t, handler)
	assert.Equal(t, tr, handler.transport)
}

func TestBackupHandler_HandleRequest_UnknownMethod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, err := w.Write([]byte(`{"key": "value"}`))
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		cfg: &transport.Config{
			Address: server.URL,
		},
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	// The default case delegates to requestWithRetry, which maps the method to
	// an endpoint and performs the HTTP call. With a valid server, the raw map
	// response is returned through convertResponse's default case.
	result, err := handler.handleRequest(ctx, "quicbaas.Backup", "UnknownMethod", nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestBackupHandler_HandleRequest_Create(t *testing.T) {
	var capturedMethod string
	var capturedPath string
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		defer r.Body.Close()

		err = json.Unmarshal(body, &capturedBody)
		assert.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"backup_id": "bkp-12345",
			"size":      float64(1024000),
			"timestamp": float64(1700000000),
			"status":    "completed",
		}
		err = json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	req := &service.CreateBackupRequest{
		Name:        "test-backup",
		Description: "Test backup description",
		Compression: "gzip",
	}
	result, err := handler.handleRequest(ctx, "quicbaas.Backup", "Create", req)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Equal(t, "/api/v1/backups", capturedPath)
	assert.Equal(t, "test-backup", capturedBody["Name"])

	createResp, ok := result.(*service.CreateBackupResponse)
	assert.True(t, ok)
	assert.Equal(t, "bkp-12345", createResp.BackupID)
	assert.Equal(t, uint64(1024000), createResp.Size)
	assert.Equal(t, int64(1700000000), createResp.Timestamp)
	assert.Equal(t, "completed", createResp.Status)
}

func TestBackupHandler_HandleRequest_Create_InvalidType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"backup_id": "bkp-99999",
			"status":    "completed",
		}
		err := json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	// Pass a string instead of a proper request type. The Create handler marshals the
	// request body to JSON via doRequest, so any JSON-serializable type will succeed.
	// This verifies the handler does not panic on unexpected types.
	result, err := handler.handleRequest(ctx, "quicbaas.Backup", "Create", "invalid-type")
	assert.NoError(t, err)
	assert.NotNil(t, result)

	createResp, ok := result.(*service.CreateBackupResponse)
	assert.True(t, ok)
	assert.Equal(t, "bkp-99999", createResp.BackupID)
}

func TestBackupHandler_HandleRequest_List(t *testing.T) {
	var capturedMethod string
	var capturedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"total_count": float64(2),
			"backups": []interface{}{
				map[string]interface{}{
					"backup_id":   "bkp-001",
					"name":        "daily-backup",
					"description": "Daily snapshot",
					"size":        float64(512000),
					"timestamp":   float64(1700000000),
					"status":      "completed",
					"shards":      []interface{}{float64(1), float64(2), float64(3)},
				},
				map[string]interface{}{
					"backup_id": "bkp-002",
					"name":      "weekly-backup",
					"size":      float64(1024000),
					"timestamp": float64(1700086400),
					"status":    "in_progress",
				},
			},
		}
		err := json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	result, err := handler.handleRequest(ctx, "quicbaas.Backup", "List", nil)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	assert.Equal(t, http.MethodGet, capturedMethod)
	assert.Equal(t, "/api/v1/backups", capturedPath)

	listResp, ok := result.(*service.ListBackupsResponse)
	assert.True(t, ok)
	assert.Equal(t, uint64(2), listResp.TotalCount)
	assert.Len(t, listResp.Backups, 2)

	assert.Equal(t, "bkp-001", listResp.Backups[0].BackupID)
	assert.Equal(t, "daily-backup", listResp.Backups[0].Name)
	assert.Equal(t, "Daily snapshot", listResp.Backups[0].Description)
	assert.Equal(t, uint64(512000), listResp.Backups[0].Size)
	assert.Equal(t, int64(1700000000), listResp.Backups[0].Timestamp)
	assert.Equal(t, "completed", listResp.Backups[0].Status)
	assert.Equal(t, []uint64{1, 2, 3}, listResp.Backups[0].Shards)

	assert.Equal(t, "bkp-002", listResp.Backups[1].BackupID)
	assert.Equal(t, "weekly-backup", listResp.Backups[1].Name)
	assert.Equal(t, "in_progress", listResp.Backups[1].Status)
}

func TestBackupHandler_HandleRequest_Delete(t *testing.T) {
	var capturedMethod string
	var capturedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"success": true,
			"message": "backup deleted successfully",
		}
		err := json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	req := map[string]interface{}{
		"backup_id": "bkp-delete-001",
	}
	result, err := handler.handleRequest(ctx, "quicbaas.Backup", "Delete", req)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	assert.Equal(t, http.MethodDelete, capturedMethod)
	assert.Equal(t, "/api/v1/backups/bkp-delete-001", capturedPath)

	deleteResp, ok := result.(*service.DeleteBackupResponse)
	assert.True(t, ok)
	assert.True(t, deleteResp.Success)
	assert.Equal(t, "backup deleted successfully", deleteResp.Message)
}

func TestBackupHandler_HandleRequest_Delete_MissingBackupID(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	req := map[string]interface{}{}
	_, err := handler.handleRequest(ctx, "quicbaas.Backup", "Delete", req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backup_id is required")
}

func TestBackupHandler_HandleRequest_Delete_PascalCaseBackupID(t *testing.T) {
	var capturedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"success": true,
			"message": "deleted",
		}
		err := json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	req := map[string]interface{}{
		"BackupID": "bkp-pascal-001",
	}
	result, err := handler.handleRequest(ctx, "quicbaas.Backup", "Delete", req)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "/api/v1/backups/bkp-pascal-001", capturedPath)
}

func TestBackupHandler_HandleRequest_Verify(t *testing.T) {
	var capturedMethod string
	var capturedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"valid":          true,
			"checksum_match": true,
			"issues":         []interface{}{},
		}
		err := json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	req := map[string]interface{}{
		"backup_id": "bkp-verify-001",
	}
	result, err := handler.handleRequest(ctx, "quicbaas.Backup", "Verify", req)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Equal(t, "/api/v1/backups/bkp-verify-001/verify", capturedPath)

	verifyResp, ok := result.(*service.VerifyBackupResponse)
	assert.True(t, ok)
	assert.True(t, verifyResp.Valid)
	assert.True(t, verifyResp.ChecksumMatch)
	assert.Empty(t, verifyResp.Issues)
}

func TestBackupHandler_HandleRequest_Verify_WithIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"valid":          false,
			"checksum_match": false,
			"issues":         []interface{}{"missing shard 3", "corrupt data block"},
		}
		err := json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	req := map[string]interface{}{
		"backup_id": "bkp-verify-002",
	}
	result, err := handler.handleRequest(ctx, "quicbaas.Backup", "Verify", req)
	assert.NoError(t, err)

	verifyResp, ok := result.(*service.VerifyBackupResponse)
	assert.True(t, ok)
	assert.False(t, verifyResp.Valid)
	assert.False(t, verifyResp.ChecksumMatch)
	assert.Len(t, verifyResp.Issues, 2)
	assert.Equal(t, "missing shard 3", verifyResp.Issues[0])
	assert.Equal(t, "corrupt data block", verifyResp.Issues[1])
}

func TestBackupHandler_HandleRequest_Verify_MissingBackupID(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	_, err := handler.handleRequest(ctx, "quicbaas.Backup", "Verify", map[string]interface{}{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backup_id is required")
}

func TestBackupHandler_HandleRequest_Status(t *testing.T) {
	var capturedMethod string
	var capturedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"status":   "in_progress",
			"progress": float64(45.5),
		}
		err := json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	req := map[string]interface{}{
		"backup_id": "bkp-status-001",
	}
	result, err := handler.handleRequest(ctx, "quicbaas.Backup", "Status", req)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	assert.Equal(t, http.MethodGet, capturedMethod)
	assert.Equal(t, "/api/v1/backups/bkp-status-001/status", capturedPath)
}

func TestBackupHandler_HandleRequest_Status_MissingBackupID(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	_, err := handler.handleRequest(ctx, "quicbaas.Backup", "Status", map[string]interface{}{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backup_id is required")
}

func TestBackupHandler_HandleRequest_Restore(t *testing.T) {
	var capturedMethod string
	var capturedPath string
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedMethod = r.Method
		capturedPath = r.URL.Path

		body, err := io.ReadAll(r.Body)
		assert.NoError(t, err)
		defer r.Body.Close()

		err = json.Unmarshal(body, &capturedBody)
		assert.NoError(t, err)

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"job_id":   "restore-job-001",
			"status":   "restoring",
			"progress": float64(0.0),
		}
		err = json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	req := map[string]interface{}{
		"backup_id":     "bkp-restore-001",
		"target_shards": []interface{}{float64(1), float64(2)},
	}
	result, err := handler.handleRequest(ctx, "quicbaas.Backup", "Restore", req)
	assert.NoError(t, err)
	assert.NotNil(t, result)

	assert.Equal(t, http.MethodPost, capturedMethod)
	assert.Equal(t, "/api/v1/backups/bkp-restore-001/restore", capturedPath)

	restoreResp, ok := result.(*service.RestoreBackupResponse)
	assert.True(t, ok)
	assert.Equal(t, "restore-job-001", restoreResp.JobID)
	assert.Equal(t, "restoring", restoreResp.Status)
	assert.Equal(t, float64(0.0), restoreResp.Progress)
}

func TestBackupHandler_HandleRequest_Restore_MissingBackupID(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	_, err := handler.handleRequest(ctx, "quicbaas.Backup", "Restore", map[string]interface{}{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "backup_id is required")
}

func TestBackupHandler_HandleRequest_TransportClosed(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	tr.closed.Store(true)
	handler := newBackupHandler(tr)

	ctx := context.Background()
	req := map[string]interface{}{
		"backup_id": "bkp-001",
	}
	_, err := handler.handleRequest(ctx, "quicbaas.Backup", "Delete", req)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "transport is closed")
}

func TestBackupHandler_HandleRequest_WithBasePath(t *testing.T) {
	var capturedPath string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedPath = r.URL.Path

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"total_count": float64(0),
			"backups":     []interface{}{},
		}
		err := json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "/quicbaas",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	_, err := handler.handleRequest(ctx, "quicbaas.Backup", "List", nil)
	assert.NoError(t, err)

	assert.Equal(t, "/quicbaas/api/v1/backups", capturedPath)
}

func TestBackupHandler_HandleRequest_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		_, err := w.Write([]byte(`{"error": "internal server error", "code": "internal"}`))
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	_, err := handler.handleRequest(ctx, "quicbaas.Backup", "List", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "internal server error")
}

func TestIsBackupService(t *testing.T) {
	tests := []struct {
		name     string
		service  string
		expected bool
	}{
		{"fully qualified prefix", "quicbaas.Backup", true},
		{"fully qualified with method", "quicbaas.Backup.Create", true},
		{"bare name", "Backup", true},
		{"non-backup KV", "quicbaas.KV", false},
		{"non-backup Vector", "quicbaas.Vector", false},
		{"empty string", "", false},
		{"partial match", "quicbaas.BackupSchedule", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isBackupService(tt.service)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGetBackupID(t *testing.T) {
	tests := []struct {
		name     string
		input    map[string]interface{}
		expected string
	}{
		{
			name:     "nil map",
			input:    nil,
			expected: "",
		},
		{
			name:     "empty map",
			input:    map[string]interface{}{},
			expected: "",
		},
		{
			name:     "snake_case key",
			input:    map[string]interface{}{"backup_id": "bkp-snake-001"},
			expected: "bkp-snake-001",
		},
		{
			name:     "PascalCase key",
			input:    map[string]interface{}{"BackupID": "bkp-pascal-001"},
			expected: "bkp-pascal-001",
		},
		{
			name: "snake_case preferred over PascalCase",
			input: map[string]interface{}{
				"backup_id": "bkp-snake",
				"BackupID":  "bkp-pascal",
			},
			expected: "bkp-snake",
		},
		{
			name:     "empty string value",
			input:    map[string]interface{}{"backup_id": ""},
			expected: "",
		},
		{
			name:     "non-string value",
			input:    map[string]interface{}{"backup_id": 12345},
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getBackupID(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBackupHandler_HandleRequest_CustomHeaders(t *testing.T) {
	var capturedAuthHeader string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuthHeader = r.Header.Get("Authorization")

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		resp := map[string]interface{}{
			"total_count": float64(0),
			"backups":     []interface{}{},
		}
		err := json.NewEncoder(w).Encode(resp)
		assert.NoError(t, err)
	}))
	defer server.Close()

	tr := &Transport{
		baseURL:  server.URL,
		basePath: "",
		client:   server.Client(),
		headers: map[string]string{
			"Authorization": "Bearer test-token-abc",
		},
	}
	handler := newBackupHandler(tr)

	ctx := context.Background()
	_, err := handler.handleRequest(ctx, "quicbaas.Backup", "List", nil)
	assert.NoError(t, err)

	assert.Equal(t, "Bearer test-token-abc", capturedAuthHeader)
}

func TestBackupHandler_ConvertResponse_NilRaw(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	result, err := handler.convertResponse("Create", nil)
	assert.NoError(t, err)
	assert.Nil(t, result)
}

func TestBackupHandler_ConvertResponse_UnexpectedType(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	_, err := handler.convertResponse("Create", "not-a-map")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected response type")
}

func TestBackupHandler_ConvertResponse_DefaultMethod(t *testing.T) {
	tr := &Transport{
		baseURL:  "http://localhost:8080",
		basePath: "",
		headers:  map[string]string{},
	}
	handler := newBackupHandler(tr)

	data := map[string]interface{}{
		"some_key": "some_value",
	}
	result, err := handler.convertResponse("Status", data)
	assert.NoError(t, err)
	assert.Equal(t, data, result)
}
