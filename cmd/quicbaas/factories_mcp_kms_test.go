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

package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	kctypes "github.com/jeremyhahn/go-xkms/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	dberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

func TestParseKMSGenerateKeyResponse(t *testing.T) {
	result := map[string]interface{}{
		"key_id":     "test-key-123",
		"algorithm":  "AES-256-GCM",
		"status":     "active",
		"version":    float64(1),
		"created_at": float64(time.Now().Unix()),
	}

	resp, err := parseKMSGenerateKeyResponse(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Key.KeyID != "test-key-123" {
		t.Errorf("expected key_id 'test-key-123', got '%s'", resp.Key.KeyID)
	}

	if resp.Key.Algorithm != "AES-256-GCM" {
		t.Errorf("expected algorithm 'AES-256-GCM', got '%s'", resp.Key.Algorithm)
	}

	if resp.Key.Status != "active" {
		t.Errorf("expected status 'active', got '%s'", resp.Key.Status)
	}

	if resp.Key.Version != 1 {
		t.Errorf("expected version 1, got %d", resp.Key.Version)
	}
}

// TestParseKMSGenerateKeyResponseInvalidData verifies error handling.
func TestParseKMSGenerateKeyResponseInvalidData(t *testing.T) {
	// Test with nil
	resp, err := parseKMSGenerateKeyResponse(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Key.KeyID != "" {
		t.Errorf("expected empty key_id for nil input, got '%s'", resp.Key.KeyID)
	}

	// Test with empty map
	resp, err = parseKMSGenerateKeyResponse(map[string]interface{}{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Key.KeyID != "" {
		t.Errorf("expected empty key_id for empty map, got '%s'", resp.Key.KeyID)
	}
}

// TestParseKMSListKeysResponse verifies list parsing.
func TestParseKMSListKeysResponse(t *testing.T) {
	result := map[string]interface{}{
		"keys": []interface{}{
			map[string]interface{}{
				"key_id":    "key-1",
				"algorithm": "AES-256-GCM",
				"status":    "active",
				"version":   float64(1),
			},
			map[string]interface{}{
				"key_id":    "key-2",
				"algorithm": "CHACHA20-POLY1305",
				"status":    "disabled",
				"version":   float64(2),
			},
		},
	}

	resp, err := parseKMSListKeysResponse(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Keys) != 2 {
		t.Fatalf("expected 2 keys, got %d", len(resp.Keys))
	}

	if resp.Keys[0].KeyID != "key-1" {
		t.Errorf("expected first key_id 'key-1', got '%s'", resp.Keys[0].KeyID)
	}

	if resp.Keys[1].KeyID != "key-2" {
		t.Errorf("expected second key_id 'key-2', got '%s'", resp.Keys[1].KeyID)
	}
}

// TestParseKMSListKeysResponseInvalidData verifies error handling.
func TestParseKMSListKeysResponseInvalidData(t *testing.T) {
	// Test with empty keys
	result := map[string]interface{}{
		"keys": []interface{}{},
	}

	resp, err := parseKMSListKeysResponse(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(resp.Keys))
	}

	// Test with no keys field
	result = map[string]interface{}{}

	resp, err = parseKMSListKeysResponse(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Keys) != 0 {
		t.Errorf("expected 0 keys, got %d", len(resp.Keys))
	}
}

// TestParseSafetyCheckResponse verifies safety check parsing.
func TestParseSafetyCheckResponse(t *testing.T) {
	result := map[string]interface{}{
		"threshold_percent":  float64(80.0),
		"total_keys_checked": float64(10),
		"total_deks_checked": float64(5),
		"items": []interface{}{
			map[string]interface{}{
				"id":              "key-1",
				"type":            "key",
				"usage_percent":   float64(85.5),
				"operation_count": float64(8550),
				"bytes_processed": float64(1024000),
			},
		},
	}

	resp, err := parseSafetyCheckResponse(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.ThresholdPercent != 80.0 {
		t.Errorf("expected threshold 80.0, got %f", resp.ThresholdPercent)
	}

	if resp.TotalKeysChecked != 10 {
		t.Errorf("expected 10 keys checked, got %d", resp.TotalKeysChecked)
	}

	if len(resp.Items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(resp.Items))
	}

	if resp.Items[0].UsagePercent != 85.5 {
		t.Errorf("expected usage_percent 85.5, got %f", resp.Items[0].UsagePercent)
	}
}

// TestParseSafetyCheckResponseInvalidData verifies error handling.
func TestParseSafetyCheckResponseInvalidData(t *testing.T) {
	// Test with no items
	result := map[string]interface{}{
		"threshold_percent": float64(80.0),
	}

	resp, err := parseSafetyCheckResponse(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(resp.Items) != 0 {
		t.Errorf("expected 0 items, got %d", len(resp.Items))
	}
}

// TestParseBackendTestResponse verifies backend test response parsing.
func TestParseBackendTestResponse(t *testing.T) {
	result := map[string]interface{}{
		"success":  true,
		"duration": "1.234s",
		"message":  "Backend test passed",
	}

	resp, err := parseBackendTestResponse(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !resp.Success {
		t.Errorf("expected success true, got false")
	}

	expectedDuration := 1234 * time.Millisecond
	if resp.Duration != expectedDuration {
		t.Errorf("expected duration %v, got %v", expectedDuration, resp.Duration)
	}

	if resp.Message != "Backend test passed" {
		t.Errorf("expected message 'Backend test passed', got '%s'", resp.Message)
	}
}

// TestParseBackendTestResponseInvalidData verifies error handling.
func TestParseBackendTestResponseInvalidData(t *testing.T) {
	// Test with empty duration
	result := map[string]interface{}{
		"success":  false,
		"duration": "",
		"message":  "Backend unavailable",
	}

	resp, err := parseBackendTestResponse(result)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Success {
		t.Errorf("expected success false, got true")
	}

	if resp.Duration != 0 {
		t.Errorf("expected duration 0, got %v", resp.Duration)
	}
}

// TestToJSONMap verifies the JSON map conversion.
func TestToJSONMap(t *testing.T) {
	// Test with nil
	result, err := toJSONMap(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 0 {
		t.Errorf("expected empty map for nil, got %d items", len(result))
	}

	// Test with existing map
	input := map[string]interface{}{"key": "value"}
	result, err = toJSONMap(input)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["key"] != "value" {
		t.Errorf("expected 'value', got '%v'", result["key"])
	}
}

// TestToJSONMapInvalidJSON verifies error handling for invalid input.
func TestToJSONMapInvalidJSON(t *testing.T) {
	// Create a channel which cannot be marshaled to JSON
	ch := make(chan int)
	_, err := toJSONMap(ch)
	if err == nil {
		t.Errorf("expected error for unmarshalable type, got nil")
	}
}

// TestHelperFunctions verifies the helper functions.
func TestHelperFunctions(t *testing.T) {
	m := map[string]interface{}{
		"string": "hello",
		"int":    float64(42),
		"float":  float64(3.14),
		"bool":   true,
		"nested": map[string]interface{}{"inner": "value"},
	}

	// getString
	if getString(m, "string") != "hello" {
		t.Errorf("getString failed")
	}
	if getString(m, "missing") != "" {
		t.Errorf("getString should return empty for missing key")
	}
	if getString(nil, "string") != "" {
		t.Errorf("getString should return empty for nil map")
	}

	// getInt64
	if getInt64(m, "int") != 42 {
		t.Errorf("getInt64 failed")
	}
	if getInt64(m, "missing") != 0 {
		t.Errorf("getInt64 should return 0 for missing key")
	}
	if getInt64(nil, "int") != 0 {
		t.Errorf("getInt64 should return 0 for nil map")
	}

	// getFloat64
	if getFloat64(m, "float") != 3.14 {
		t.Errorf("getFloat64 failed")
	}
	if getFloat64(m, "missing") != 0 {
		t.Errorf("getFloat64 should return 0 for missing key")
	}
	if getFloat64(nil, "float") != 0 {
		t.Errorf("getFloat64 should return 0 for nil map")
	}

	// getBool
	if !getBool(m, "bool") {
		t.Errorf("getBool failed")
	}
	if getBool(m, "missing") {
		t.Errorf("getBool should return false for missing key")
	}
	if getBool(nil, "bool") {
		t.Errorf("getBool should return false for nil map")
	}

	// getMap
	nested := getMap(m, "nested")
	if nested == nil || nested["inner"] != "value" {
		t.Errorf("getMap failed")
	}
	if getMap(m, "missing") != nil {
		t.Errorf("getMap should return nil for missing key")
	}
	if getMap(nil, "nested") != nil {
		t.Errorf("getMap should return nil for nil map")
	}
}

// TestHelperFunctionsInvalidTypes verifies error handling for invalid types.
func TestHelperFunctionsInvalidTypes(t *testing.T) {
	m := map[string]interface{}{
		"wrong_type": 123, // integer instead of string
	}

	// getString returns empty for wrong type
	if getString(m, "wrong_type") != "" {
		t.Errorf("getString should return empty for wrong type")
	}

	// getBytes returns nil for wrong type
	m["wrong_bytes"] = 123
	if getBytes(m, "wrong_bytes") != nil {
		t.Errorf("getBytes should return nil for wrong type")
	}
}

// TestParseTimestamp verifies timestamp parsing.
func TestParseTimestamp(t *testing.T) {
	// Test seconds-based timestamp (e.g., from KMS)
	ts := time.Now().Unix()
	result := parseTimestamp(ts)
	if result.Unix() != ts {
		t.Errorf("expected Unix timestamp %d, got %d", ts, result.Unix())
	}

	// Test nanosecond-based timestamp (e.g., from backup service)
	now := time.Now()
	tsNano := now.UnixNano()
	result = parseTimestamp(tsNano)
	if result.UnixNano() != tsNano {
		t.Errorf("expected nanosecond timestamp %d, got %d", tsNano, result.UnixNano())
	}
	if result.Year() < 2020 || result.Year() > 2100 {
		t.Errorf("nanosecond timestamp produced invalid year %d", result.Year())
	}

	// Test zero timestamp
	result = parseTimestamp(0)
	if !result.IsZero() {
		t.Errorf("expected zero time for 0 timestamp")
	}

	// Test negative nanosecond timestamp
	negNano := int64(-2e15)
	result = parseTimestamp(negNano)
	if result.UnixNano() != negNano {
		t.Errorf("expected negative nano timestamp %d, got %d", negNano, result.UnixNano())
	}
}

// TestParseDuration verifies duration parsing.
func TestParseDuration(t *testing.T) {
	// Test valid duration
	d := parseDuration("1h30m")
	expected := 90 * time.Minute
	if d != expected {
		t.Errorf("expected %v, got %v", expected, d)
	}

	// Test empty string
	d = parseDuration("")
	if d != 0 {
		t.Errorf("expected 0 for empty string, got %v", d)
	}

	// Test invalid duration (returns 0 on error)
	d = parseDuration("invalid")
	if d != 0 {
		t.Errorf("expected 0 for invalid duration, got %v", d)
	}
}

// TestParseKeyAlgorithm verifies algorithm parsing.
func TestParseKeyAlgorithm(t *testing.T) {
	alg := parseKeyAlgorithm("AES-256-GCM")
	if alg != "AES-256-GCM" {
		t.Errorf("expected 'AES-256-GCM', got '%s'", alg)
	}

	// Test empty string
	alg = parseKeyAlgorithm("")
	if alg != "" {
		t.Errorf("expected empty string, got '%s'", alg)
	}
}

type mockMCPTransport struct {
	requestFunc func(ctx context.Context, svc, method string, req interface{}) (interface{}, error)
}

func (m *mockMCPTransport) Request(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
	if m.requestFunc != nil {
		return m.requestFunc(ctx, svc, method, req)
	}
	return map[string]interface{}{}, nil
}

func (m *mockMCPTransport) RequestStream(ctx context.Context, svc, method string, req interface{}) (sdktransport.Stream, error) {
	return nil, &dberrors.MessageError{Message: "not implemented"}
}

func (m *mockMCPTransport) Conn() interface{} { return nil }

func (m *mockMCPTransport) Close() error { return nil }

func (m *mockMCPTransport) Healthy() bool { return true }

// mockSDKClientForMCP implements quicbaas.Client returning a mock transport.
type mockSDKClientForMCP struct {
	mockTransport sdktransport.Transport
}

func (m *mockSDKClientForMCP) KV() quicbaas.KVClient { return nil }

func (m *mockSDKClientForMCP) Batch() quicbaas.BatchClient { return nil }

func (m *mockSDKClientForMCP) Transaction() quicbaas.TransactionClient { return nil }

func (m *mockSDKClientForMCP) Query() quicbaas.QueryClient { return nil }

func (m *mockSDKClientForMCP) Cluster() quicbaas.ClusterClient { return nil }

func (m *mockSDKClientForMCP) Health() quicbaas.HealthClient { return nil }

func (m *mockSDKClientForMCP) Operations() quicbaas.OperationsClient { return nil }

func (m *mockSDKClientForMCP) Cache() quicbaas.CacheClient { return nil }

func (m *mockSDKClientForMCP) IAM() quicbaas.IAMClient { return nil }

func (m *mockSDKClientForMCP) Vector() quicbaas.VectorClient { return nil }

func (m *mockSDKClientForMCP) TimeSeries() quicbaas.TimeSeriesClient { return nil }

func (m *mockSDKClientForMCP) Backup() quicbaas.BackupClient { return nil }

func (m *mockSDKClientForMCP) ObjectStorage() quicbaas.ObjectStorageClient { return nil }

func (m *mockSDKClientForMCP) ObjectStorageAdmin() quicbaas.ObjectStorageAdminClient { return nil }

func (m *mockSDKClientForMCP) ObjectTransfer() quicbaas.ObjectTransferClient { return nil }

func (m *mockSDKClientForMCP) ParameterStore() quicbaas.ParameterStoreClient { return nil }

func (m *mockSDKClientForMCP) Scheduler() quicbaas.SchedulerClient { return nil }

func (m *mockSDKClientForMCP) Messaging() quicbaas.MessagingClient { return nil }

func (m *mockSDKClientForMCP) Embedding() quicbaas.EmbeddingClient { return nil }

func (m *mockSDKClientForMCP) Ollama() quicbaas.OllamaClient { return nil }

func (m *mockSDKClientForMCP) RAG() quicbaas.RAGClient { return nil }

func (m *mockSDKClientForMCP) CoreData() quicbaas.CoreDataService { return nil }

func (m *mockSDKClientForMCP) ClusterMgmt() quicbaas.ClusterManagementService { return nil }

func (m *mockSDKClientForMCP) Security() quicbaas.SecurityService { return nil }

func (m *mockSDKClientForMCP) Identity() quicbaas.IdentityService { return nil }

func (m *mockSDKClientForMCP) Search() quicbaas.SearchService { return nil }

func (m *mockSDKClientForMCP) Storage() quicbaas.StorageService { return nil }

func (m *mockSDKClientForMCP) Async() quicbaas.AsyncService { return nil }

func (m *mockSDKClientForMCP) AI() quicbaas.AIService { return nil }

func (m *mockSDKClientForMCP) Transport() quicbaas.Transport { return m.mockTransport }

func (m *mockSDKClientForMCP) Close() error { return nil }

func (m *mockSDKClientForMCP) DataStore() (quicbaas.QRDBKVStore, error) { return nil, nil }

func (m *mockSDKClientForMCP) BarrierCeremonyMgmt() quicbaas.BarrierCeremonyService  { return nil }
func (m *mockSDKClientForMCP) TenantMgmtService() quicbaas.TenantService             { return nil }
func (m *mockSDKClientForMCP) Barrier() (quicbaas.QRDBBarrierClient, error)          { return nil, nil }
func (m *mockSDKClientForMCP) BarrierCeremony() (quicbaas.QRDBCeremonyClient, error) { return nil, nil }
func (m *mockSDKClientForMCP) SealStore() (quicbaas.QRDBSealStoreClient, error)      { return nil, nil }
func (m *mockSDKClientForMCP) TenantMgmt() (quicbaas.QRDBTenantClient, error)        { return nil, nil }
func (m *mockSDKClientForMCP) CustodianGroup() (quicbaas.QRDBCustodianGroupClient, error) {
	return nil, nil
}
func (m *mockSDKClientForMCP) Share() (quicbaas.QRDBShareClient, error)  { return nil, nil }
func (m *mockSDKClientForMCP) Orchestrator() quicbaas.OrchestratorClient { return nil }
func (m *mockSDKClientForMCP) Project() quicbaas.ProjectClient           { return nil }
func (m *mockSDKClientForMCP) SDLC() quicbaas.SDLCClient                 { return nil }
func (m *mockSDKClientForMCP) Prompts() quicbaas.PromptsClient           { return nil }
func (m *mockSDKClientForMCP) Agents() quicbaas.AgentsClient             { return nil }
func (m *mockSDKClientForMCP) Account() quicbaas.AccountClient           { return nil }

var _ quicbaas.Client = (*mockSDKClientForMCP)(nil)

// newMockMCPSDKClient creates a mock SDK client with a transport that returns
// a JSON-like map for any tool call.
func newMockMCPSDKClient(toolResponses map[string]interface{}) quicbaas.Client {
	tr := &mockMCPTransport{
		requestFunc: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			if toolResponses != nil {
				if resp, ok := toolResponses[method]; ok {
					return resp, nil
				}
			}
			return map[string]interface{}{}, nil
		},
	}
	return &mockSDKClientForMCP{mockTransport: tr}
}

// toolResp is a helper to build a result map that the parse functions expect.
// It marshals the given map to JSON and back to ensure it matches the
// interface{} types the parsers work with (e.g., float64 for numbers).
func toolResp(m map[string]interface{}) interface{} {
	data, _ := json.Marshal(m)
	var result interface{}
	_ = json.Unmarshal(data, &result)
	return result
}

// =============================================================================
// Test MCP KMS Adapter Factory Functions
// =============================================================================

func TestCreateMCPKMSClient(t *testing.T) {
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	t.Run("Success", func(t *testing.T) {
		sdkClientFactory = func() (quicbaas.Client, error) {
			return newMockMCPSDKClient(nil), nil
		}
		client, err := createMCPKMSClient()
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("Error", func(t *testing.T) {
		sdkClientFactory = func() (quicbaas.Client, error) {
			return nil, &dberrors.MessageError{Message: "sdk error"}
		}
		client, err := createMCPKMSClient()
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}

func TestCreateMCPBackendClient(t *testing.T) {
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	t.Run("Success", func(t *testing.T) {
		sdkClientFactory = func() (quicbaas.Client, error) {
			return newMockMCPSDKClient(nil), nil
		}
		client, err := createMCPBackendClient()
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("Error", func(t *testing.T) {
		sdkClientFactory = func() (quicbaas.Client, error) {
			return nil, &dberrors.MessageError{Message: "sdk error"}
		}
		client, err := createMCPBackendClient()
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}

func TestCreateMCPSafetyClient(t *testing.T) {
	origFactory := sdkClientFactory
	defer func() { sdkClientFactory = origFactory }()

	t.Run("Success", func(t *testing.T) {
		sdkClientFactory = func() (quicbaas.Client, error) {
			return newMockMCPSDKClient(nil), nil
		}
		client, err := createMCPSafetyClient()
		require.NoError(t, err)
		assert.NotNil(t, client)
	})

	t.Run("Error", func(t *testing.T) {
		sdkClientFactory = func() (quicbaas.Client, error) {
			return nil, &dberrors.MessageError{Message: "sdk error"}
		}
		client, err := createMCPSafetyClient()
		assert.Error(t, err)
		assert.Nil(t, client)
	})
}

// =============================================================================
// Test MCP KMS Client Methods
// =============================================================================

func TestMCPKMSClientMethods(t *testing.T) {
	now := time.Now().Format(time.RFC3339)
	responses := map[string]interface{}{
		"quicbaas_kms_generate_key": toolResp(map[string]interface{}{
			"key_id": "key-001", "name": "test-key", "version": float64(1),
			"algorithm": "AES_256", "status": "active", "created_at": now,
		}),
		"quicbaas_kms_import_key": toolResp(map[string]interface{}{
			"key_id": "key-002", "name": "imported", "version": float64(1),
			"algorithm": "AES_256", "status": "active", "created_at": now,
		}),
		"quicbaas_kms_get_key": toolResp(map[string]interface{}{
			"key": map[string]interface{}{
				"key_id": "key-001", "name": "test-key", "version": float64(1),
				"algorithm": "AES_256", "status": "active", "created_at": now,
			},
		}),
		"quicbaas_kms_list_keys": toolResp(map[string]interface{}{
			"keys": []interface{}{
				map[string]interface{}{
					"key_id": "key-001", "name": "test-key", "version": float64(1),
					"algorithm": "AES_256", "status": "active", "created_at": now,
				},
			},
			"total": float64(1),
		}),
		"quicbaas_kms_rotate_key": toolResp(map[string]interface{}{
			"key_id": "key-001", "new_version": float64(2), "rotated_at": now,
		}),
		"quicbaas_kms_delete_key": toolResp(map[string]interface{}{
			"key_id": "key-001", "status": "pending_deletion", "scheduled_at": now,
		}),
		"quicbaas_kms_key_status_update": toolResp(map[string]interface{}{
			"key_id": "key-001", "status": "disabled",
		}),
		"quicbaas_kms_key_versions": toolResp(map[string]interface{}{
			"versions": []interface{}{
				map[string]interface{}{"version": float64(1), "status": "active", "created_at": now},
			},
		}),
		"quicbaas_kms_export_key": toolResp(map[string]interface{}{
			"key_material": "base64data", "format": "raw",
		}),
		"quicbaas_kms_unwrap_key": toolResp(map[string]interface{}{
			"key_id": "key-001", "unwrapped": true,
		}),
		"quicbaas_kms_set_key_safety": toolResp(map[string]interface{}{
			"key_id": "key-001", "safety_configured": true,
		}),
		"quicbaas_kms_clear_key_safety": toolResp(map[string]interface{}{
			"key_id": "key-001", "safety_cleared": true,
		}),
		"quicbaas_kms_encrypt": toolResp(map[string]interface{}{
			"ciphertext": "encrypted-data", "key_id": "key-001",
		}),
		"quicbaas_kms_decrypt": toolResp(map[string]interface{}{
			"plaintext": "decrypted-data", "key_id": "key-001",
		}),
	}

	sdk := newMockMCPSDKClient(responses)
	client := &mcpKMSClient{sdk: sdk}
	ctx := context.Background()

	t.Run("GenerateKey", func(t *testing.T) {
		resp, err := client.KMSGenerateKey(ctx, &service.GenerateKeyRequest{
			Name:        "test-key",
			Description: "test desc",
			Exportable:  true,
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ImportKey", func(t *testing.T) {
		resp, err := client.KMSImportKey(ctx, &service.ImportKeyRequest{
			Name:        "imported",
			Description: "test import",
			KeyMaterial: []byte("key-data"),
			KeyAttributes: &kctypes.KeyAttributes{
				SymmetricAlgorithm: kctypes.SymmetricAES256GCM,
			},
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("GetKey", func(t *testing.T) {
		resp, err := client.KMSGetKey(ctx, &service.GetKeyRequest{
			KeyID:   "key-001",
			Version: 1,
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ListKeys", func(t *testing.T) {
		resp, err := client.KMSListKeys(ctx, &service.ListKeysRequest{
			Limit:  10,
			Filter: map[string]string{"status": "active"},
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("RotateKey", func(t *testing.T) {
		resp, err := client.KMSRotateKey(ctx, &service.RotateKeyRequest{
			KeyID:            "key-001",
			RetainOldVersion: true,
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("DeleteKey", func(t *testing.T) {
		resp, err := client.KMSDeleteKey(ctx, &service.DeleteKeyRequest{
			KeyID:             "key-001",
			PendingWindowDays: 7,
			Force:             true,
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("UpdateKeyStatus", func(t *testing.T) {
		resp, err := client.KMSUpdateKeyStatus(ctx, &service.UpdateKeyStatusRequest{
			KeyID:   "key-001",
			Status:  "disabled",
			Version: 1,
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ListKeyVersions", func(t *testing.T) {
		resp, err := client.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{
			KeyID: "key-001",
			Limit: 10,
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ExportKey", func(t *testing.T) {
		resp, err := client.KMSExportKey(ctx, &service.ExportKeyRequest{
			KeyID:             "key-001",
			WrappingKeyID:     "wrap-key",
			WrappingAlgorithm: "RSA_OAEP",
			Format:            "raw",
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("UnwrapKey", func(t *testing.T) {
		resp, err := client.KMSUnwrapKey(ctx, &service.UnwrapKeyRequest{
			KeyID:         "key-001",
			WrappingKeyID: "wrap-key",
			WrappedKey:    []byte("wrapped-data"),
			Algorithm:     "AES_256",
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("SetKeySafety", func(t *testing.T) {
		resp, err := client.KMSSetKeySafety(ctx, &service.SetKeySafetyRequest{
			KeyID:          "key-001",
			MaxOperations:  1000,
			MaxBytes:       1024 * 1024,
			WarningPercent: 80,
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("ClearKeySafety", func(t *testing.T) {
		resp, err := client.KMSClearKeySafety(ctx, &service.ClearKeySafetyRequest{
			KeyID: "key-001",
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Encrypt", func(t *testing.T) {
		resp, err := client.KMSEncrypt(ctx, &service.EncryptRequest{
			KeyID:     "key-001",
			Plaintext: []byte("secret-data"),
			Context:   []byte(`{"purpose":"test"}`),
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Decrypt", func(t *testing.T) {
		resp, err := client.KMSDecrypt(ctx, &service.DecryptRequest{
			KeyID:      "key-001",
			Ciphertext: []byte("encrypted-data"),
			Context:    []byte(`{"purpose":"test"}`),
		})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Close", func(t *testing.T) {
		err := client.Close()
		assert.NoError(t, err)
	})
}

// =============================================================================
// Test MCP DEK Client Methods
// =============================================================================

// =============================================================================
// Test MCP Backend Client Methods
// =============================================================================

func TestMCPBackendClientMethods(t *testing.T) {
	responses := map[string]interface{}{
		"quicbaas_kms_backend_status": toolResp(map[string]interface{}{
			"backend": "pebble", "healthy": true, "latency_ms": float64(5),
		}),
		"quicbaas_kms_backend_test": toolResp(map[string]interface{}{
			"success": true, "read_latency": "1ms", "write_latency": "2ms",
			"tests_passed": float64(5), "tests_failed": float64(0),
		}),
	}

	sdk := newMockMCPSDKClient(responses)
	client := &mcpBackendClient{sdk: sdk}
	ctx := context.Background()

	t.Run("BackendStatus", func(t *testing.T) {
		resp, err := client.KMSBackendStatus(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("BackendTest", func(t *testing.T) {
		resp, err := client.KMSBackendTest(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Close", func(t *testing.T) {
		err := client.Close()
		assert.NoError(t, err)
	})
}

// =============================================================================
// Test MCP Safety Client Methods
// =============================================================================

func TestMCPSafetyClientMethods(t *testing.T) {
	responses := map[string]interface{}{
		"quicbaas_kms_safety_check": toolResp(map[string]interface{}{
			"safe": true, "issues": []interface{}{},
		}),
		"quicbaas_kms_safety_report": toolResp(map[string]interface{}{
			"safe": true, "total_keys": float64(10),
		}),
	}

	sdk := newMockMCPSDKClient(responses)
	client := &mcpSafetyClient{sdk: sdk}
	ctx := context.Background()

	t.Run("SafetyCheck", func(t *testing.T) {
		resp, err := client.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{ThresholdPercent: 80})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("SafetyReport", func(t *testing.T) {
		resp, err := client.KMSSafetyReport(ctx, &service.SafetyReportRequest{IncludeDetails: true})
		assert.NoError(t, err)
		assert.NotNil(t, resp)
	})

	t.Run("Close", func(t *testing.T) {
		err := client.Close()
		assert.NoError(t, err)
	})
}

// =============================================================================
// Test MCP Adapter Error Paths
// =============================================================================

func TestMCPKMSClientTransportError(t *testing.T) {
	tr := &mockMCPTransport{
		requestFunc: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return nil, &dberrors.MessageError{Message: "transport error: " + method}
		},
	}
	sdk := &mockSDKClientForMCP{mockTransport: tr}
	client := &mcpKMSClient{sdk: sdk}
	ctx := context.Background()

	t.Run("GenerateKeyError", func(t *testing.T) {
		_, err := client.KMSGenerateKey(ctx, &service.GenerateKeyRequest{
			KeyAttributes: &kctypes.KeyAttributes{SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
		})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "transport error")
	})

	t.Run("ImportKeyError", func(t *testing.T) {
		_, err := client.KMSImportKey(ctx, &service.ImportKeyRequest{
			KeyAttributes: &kctypes.KeyAttributes{SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
		})
		assert.Error(t, err)
	})

	t.Run("GetKeyError", func(t *testing.T) {
		_, err := client.KMSGetKey(ctx, &service.GetKeyRequest{KeyID: "key-1"})
		assert.Error(t, err)
	})

	t.Run("ListKeysError", func(t *testing.T) {
		_, err := client.KMSListKeys(ctx, &service.ListKeysRequest{})
		assert.Error(t, err)
	})

	t.Run("RotateKeyError", func(t *testing.T) {
		_, err := client.KMSRotateKey(ctx, &service.RotateKeyRequest{KeyID: "key-1"})
		assert.Error(t, err)
	})

	t.Run("DeleteKeyError", func(t *testing.T) {
		_, err := client.KMSDeleteKey(ctx, &service.DeleteKeyRequest{KeyID: "key-1"})
		assert.Error(t, err)
	})

	t.Run("EncryptError", func(t *testing.T) {
		_, err := client.KMSEncrypt(ctx, &service.EncryptRequest{KeyID: "key-1", Plaintext: []byte("data")})
		assert.Error(t, err)
	})

	t.Run("DecryptError", func(t *testing.T) {
		_, err := client.KMSDecrypt(ctx, &service.DecryptRequest{KeyID: "key-1", Ciphertext: []byte("data")})
		assert.Error(t, err)
	})

	t.Run("UpdateKeyStatusError", func(t *testing.T) {
		_, err := client.KMSUpdateKeyStatus(ctx, &service.UpdateKeyStatusRequest{KeyID: "key-1", Status: "disabled"})
		assert.Error(t, err)
	})

	t.Run("ListKeyVersionsError", func(t *testing.T) {
		_, err := client.KMSListKeyVersions(ctx, &service.ListKeyVersionsRequest{KeyID: "key-1"})
		assert.Error(t, err)
	})

	t.Run("ExportKeyError", func(t *testing.T) {
		_, err := client.KMSExportKey(ctx, &service.ExportKeyRequest{KeyID: "key-1"})
		assert.Error(t, err)
	})

	t.Run("UnwrapKeyError", func(t *testing.T) {
		_, err := client.KMSUnwrapKey(ctx, &service.UnwrapKeyRequest{KeyID: "key-1", WrappingKeyID: "w", WrappedKey: []byte("d"), Algorithm: "a"})
		assert.Error(t, err)
	})

	t.Run("SetKeySafetyError", func(t *testing.T) {
		_, err := client.KMSSetKeySafety(ctx, &service.SetKeySafetyRequest{KeyID: "key-1"})
		assert.Error(t, err)
	})

	t.Run("ClearKeySafetyError", func(t *testing.T) {
		_, err := client.KMSClearKeySafety(ctx, &service.ClearKeySafetyRequest{KeyID: "key-1"})
		assert.Error(t, err)
	})
}

func TestMCPBackendClientTransportError(t *testing.T) {
	tr := &mockMCPTransport{
		requestFunc: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return nil, &dberrors.MessageError{Message: "transport error"}
		},
	}
	sdk := &mockSDKClientForMCP{mockTransport: tr}
	client := &mcpBackendClient{sdk: sdk}
	ctx := context.Background()

	t.Run("BackendStatusError", func(t *testing.T) {
		_, err := client.KMSBackendStatus(ctx)
		assert.Error(t, err)
	})
	t.Run("BackendTestError", func(t *testing.T) {
		_, err := client.KMSBackendTest(ctx)
		assert.Error(t, err)
	})
}

func TestMCPSafetyClientTransportError(t *testing.T) {
	tr := &mockMCPTransport{
		requestFunc: func(ctx context.Context, svc, method string, req interface{}) (interface{}, error) {
			return nil, &dberrors.MessageError{Message: "transport error"}
		},
	}
	sdk := &mockSDKClientForMCP{mockTransport: tr}
	client := &mcpSafetyClient{sdk: sdk}
	ctx := context.Background()

	t.Run("SafetyCheckError", func(t *testing.T) {
		_, err := client.KMSSafetyCheck(ctx, &service.SafetyCheckRequest{})
		assert.Error(t, err)
	})
	t.Run("SafetyReportError", func(t *testing.T) {
		_, err := client.KMSSafetyReport(ctx, &service.SafetyReportRequest{})
		assert.Error(t, err)
	})
}
