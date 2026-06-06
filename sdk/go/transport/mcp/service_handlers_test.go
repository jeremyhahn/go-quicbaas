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

// service_handlers_test.go exercises the typed MCP service handlers for the
// Certificate, Messaging, and ObjectStorage services end-to-end through a
// fake JSON-RPC MCP endpoint. Each test asserts the tool name and arguments
// the handler sends, returns a canned tool result, and verifies the handler
// decodes that result into the concrete SDK response type.

package mcp

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorage"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// mcpToolExpectation captures what a test expects the SDK to send and what
// the fake server should return.
type mcpToolExpectation struct {
	// wantTool is the MCP tool name the SDK must call.
	wantTool string
	// assertArgs runs custom assertions on the decoded tool arguments.
	assertArgs func(t *testing.T, args map[string]interface{})
	// result is the JSON object the tool returns inside its text content.
	// When toolError is true the result text is surfaced as an error.
	result    map[string]interface{}
	toolError bool
	// rawText overrides result with a literal text content payload
	// (used to test malformed/non-JSON responses).
	rawText string
}

// newFakeMCPServer starts an httptest server that answers the MCP initialize
// handshake and a single tools/call, applying exp. It returns a transport
// wired to the server.
func newFakeMCPServer(t *testing.T, exp mcpToolExpectation) (*Transport, func()) {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req jsonRPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("failed to decode request: %v", err)
			return
		}

		var result interface{}
		switch req.Method {
		case "initialize":
			result = map[string]interface{}{
				"protocolVersion": MCPVersion,
				"serverInfo":      map[string]interface{}{"name": "TestServer", "version": "1.0.0"},
				"capabilities":    map[string]interface{}{},
			}
		case "tools/call":
			params, _ := req.Params.(map[string]interface{})
			toolName, _ := params["name"].(string)
			if toolName != exp.wantTool {
				t.Errorf("tool name = %q, want %q", toolName, exp.wantTool)
			}
			if exp.assertArgs != nil {
				args, _ := params["arguments"].(map[string]interface{})
				exp.assertArgs(t, args)
			}

			text := exp.rawText
			if text == "" {
				text = string(mustMarshalJSON(exp.result))
			}
			result = map[string]interface{}{
				"content": []interface{}{
					map[string]interface{}{"type": "text", "text": text},
				},
				"isError": exp.toolError,
			}
		default:
			t.Errorf("unexpected method %q", req.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(jsonRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  mustMarshalJSON(result),
		}); err != nil {
			t.Errorf("failed to encode response: %v", err)
		}
	}))

	tr, err := New(&transport.Config{Address: srv.URL, Timeout: 5 * time.Second})
	if err != nil {
		srv.Close()
		t.Fatalf("failed to create transport: %v", err)
	}
	return tr, func() {
		tr.Close()
		srv.Close()
	}
}

// =============================================================================
// Tool-name mapping coverage
// =============================================================================

func TestMapCertificateMethodToTool(t *testing.T) {
	cases := map[string]string{
		"IssueCertificate":  "quicbaas_certificate_issue",
		"SignCSR":           "quicbaas_certificate_sign_csr",
		"RevokeCertificate": "quicbaas_certificate_revoke",
		"VerifyCertificate": "quicbaas_certificate_verify",
		"GetCACertificate":  "quicbaas_certificate_get_ca",
		"GetTLSConfig":      "quicbaas_certificate_get_tls_config",
	}
	for method, want := range cases {
		got, ok := mapCertificateMethodToTool(method)
		if !ok || got != want {
			t.Errorf("mapCertificateMethodToTool(%q) = %q,%v want %q,true", method, got, ok, want)
		}
	}
	if _, ok := mapCertificateMethodToTool("GenerateCRL"); ok {
		t.Error("GenerateCRL must have no MCP tool mapping")
	}
}

func TestMapMessagingMethodToTool(t *testing.T) {
	cases := map[string]string{
		"CreateChannel": "quicbaas_msg_create_channel",
		"Publish":       "quicbaas_msg_publish",
		"GetStats":      "quicbaas_msg_stats",
	}
	for method, want := range cases {
		got, ok := mapMessagingMethodToTool(method)
		if !ok || got != want {
			t.Errorf("mapMessagingMethodToTool(%q) = %q,%v want %q,true", method, got, ok, want)
		}
	}
}

func TestMapObjectStorageMethodToTool(t *testing.T) {
	cases := map[string]string{
		"PutObject":      "quicbaas_object_put",
		"GetObject":      "quicbaas_object_get",
		"ListObjects":    "quicbaas_object_list",
		"CopyObject":     "quicbaas_object_copy",
		"ListByTag":      "quicbaas_object_list_by_tag",
		"GetTenantUsage": "quicbaas_object_tenant_usage",
	}
	for method, want := range cases {
		got, ok := mapObjectStorageMethodToTool(method)
		if !ok || got != want {
			t.Errorf("mapObjectStorageMethodToTool(%q) = %q,%v want %q,true", method, got, ok, want)
		}
	}
}

func TestServiceDetection(t *testing.T) {
	if !isMCPCertificateService("quicbaas.Certificate") {
		t.Error("Certificate detection failed")
	}
	if !isMCPMessagingService("quicbaas.Messaging") {
		t.Error("Messaging detection failed")
	}
	if !isMCPObjectStorageService("quicbaas.ObjectStorage") {
		t.Error("ObjectStorage detection failed")
	}
	if isMCPObjectStorageService("quicbaas.ObjectStorageAdmin") {
		t.Error("ObjectStorageAdmin must not be treated as ObjectStorage")
	}
	if isMCPCertificateService("quicbaas.KV") {
		t.Error("KV must not be treated as Certificate")
	}
}

// =============================================================================
// Certificate end-to-end
// =============================================================================

func TestCertificateIssueEndToEnd(t *testing.T) {
	notBefore := time.Now().UTC().Truncate(time.Second)
	notAfter := notBefore.Add(24 * time.Hour)

	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_certificate_issue",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["common_name"] != "example.com" {
				t.Errorf("common_name = %v", args["common_name"])
			}
			if args["dns_names"] != `["a.example.com","b.example.com"]` {
				t.Errorf("dns_names = %v", args["dns_names"])
			}
		},
		result: map[string]interface{}{
			"certificate_pem":    "CERT",
			"private_key_pem":    "KEY",
			"ca_certificate_pem": "CA",
			"chain_pem":          "CHAIN",
			"serial_number":      "0A1B",
			"not_before":         notBefore.Format(time.RFC3339),
			"not_after":          notAfter.Format(time.RFC3339),
		},
	})
	defer cleanup()

	client := certificate.NewClient(tr)
	resp, err := client.IssueCertificate(context.Background(), &certificate.IssueCertificateRequest{
		CommonName: "example.com",
		DNSNames:   []string{"a.example.com", "b.example.com"},
	})
	if err != nil {
		t.Fatalf("IssueCertificate: %v", err)
	}
	if resp.CertificatePEM != "CERT" || resp.SerialNumber != "0A1B" {
		t.Errorf("decoded response mismatch: %+v", resp)
	}
	if !resp.NotAfter.Equal(notAfter) {
		t.Errorf("not_after = %v, want %v", resp.NotAfter, notAfter)
	}
}

func TestCertificateVerifyAndGetCA(t *testing.T) {
	t.Run("verify", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
			wantTool: "quicbaas_certificate_verify",
			assertArgs: func(t *testing.T, args map[string]interface{}) {
				if args["certificate_pem"] != "PEM" {
					t.Errorf("certificate_pem = %v", args["certificate_pem"])
				}
			},
			result: map[string]interface{}{"valid": true},
		})
		defer cleanup()
		resp, err := certificate.NewClient(tr).VerifyCertificate(context.Background(),
			&certificate.VerifyCertificateRequest{CertificatePEM: "PEM"})
		if err != nil {
			t.Fatalf("VerifyCertificate: %v", err)
		}
		if !resp.Valid {
			t.Error("expected valid certificate")
		}
	})

	t.Run("get ca", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
			wantTool: "quicbaas_certificate_get_ca",
			result: map[string]interface{}{
				"ca_certificate_pem": "CAPEM",
				"ca_bundle_pem":      "BUNDLE",
				"identity":           "root",
			},
		})
		defer cleanup()
		resp, err := certificate.NewClient(tr).GetCACertificate(context.Background(), nil)
		if err != nil {
			t.Fatalf("GetCACertificate: %v", err)
		}
		if resp.CACertificatePEM != "CAPEM" || resp.Identity != "root" {
			t.Errorf("decoded response mismatch: %+v", resp)
		}
	})
}

func TestCertificateGetTLSConfig(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_certificate_get_tls_config",
		result:   map[string]interface{}{"configured": true},
	})
	defer cleanup()
	resp, err := certificate.NewClient(tr).GetTLSConfig(context.Background(),
		&certificate.GetTLSConfigRequest{ServerName: "svc", MinVersion: "1.3"})
	if err != nil {
		t.Fatalf("GetTLSConfig: %v", err)
	}
	if resp.Config == nil || resp.Config.ServerName != "svc" {
		t.Errorf("expected reconstructed tls.Config with ServerName svc, got %+v", resp.Config)
	}
}

func TestCertificateToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:  "quicbaas_certificate_issue",
		toolError: true,
		rawText:   "issuing CA not initialized",
	})
	defer cleanup()
	_, err := certificate.NewClient(tr).IssueCertificate(context.Background(),
		&certificate.IssueCertificateRequest{CommonName: "x"})
	if err == nil {
		t.Fatal("expected error from tool-level failure")
	}
}

func TestCertificateMalformedResult(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_certificate_issue",
		rawText:  "not json at all",
	})
	defer cleanup()
	// A non-JSON tool text decodes to a string scalar, which asResultMap
	// rejects as an unexpected shape.
	_, err := certificate.NewClient(tr).IssueCertificate(context.Background(),
		&certificate.IssueCertificateRequest{CommonName: "x"})
	if err == nil {
		t.Fatal("expected error decoding malformed result")
	}
}

// =============================================================================
// Messaging end-to-end
// =============================================================================

func TestMessagingCreateChannel(t *testing.T) {
	created := time.Now().UTC().Truncate(time.Second)
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_msg_create_channel",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["name"] != "alerts" {
				t.Errorf("name = %v", args["name"])
			}
			if args["persistent"] != "true" {
				t.Errorf("persistent = %v, want \"true\"", args["persistent"])
			}
		},
		result: map[string]interface{}{
			"name":        "alerts",
			"codec":       "json",
			"created_at":  created.Format(time.RFC3339),
			"subscribers": float64(3),
			"persistent":  true,
			"max_history": float64(10),
			"buffer_size": float64(256),
			"ttl":         "1h0m0s",
		},
	})
	defer cleanup()
	ch, err := messaging.NewClient(tr).CreateChannel(context.Background(),
		&messaging.CreateChannelRequest{Name: "alerts", Codec: "json", Persistent: true})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	if ch.Name != "alerts" || ch.Subscribers != 3 || !ch.Persistent {
		t.Errorf("decoded channel mismatch: %+v", ch)
	}
	if ch.TTL != time.Hour {
		t.Errorf("ttl = %v, want 1h", ch.TTL)
	}
}

func TestMessagingListChannels(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_msg_list_channels",
		result: map[string]interface{}{
			"channels": []interface{}{
				map[string]interface{}{"name": "a", "subscribers": float64(1)},
				map[string]interface{}{"name": "b", "subscribers": float64(2)},
			},
			"count": float64(2),
		},
	})
	defer cleanup()
	resp, err := messaging.NewClient(tr).ListChannels(context.Background())
	if err != nil {
		t.Fatalf("ListChannels: %v", err)
	}
	if len(resp.Channels) != 2 || resp.Channels[1].Name != "b" {
		t.Errorf("decoded channels mismatch: %+v", resp.Channels)
	}
}

func TestMessagingPublishVoidResponse(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_msg_publish",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["channel"] != "alerts" {
				t.Errorf("channel = %v", args["channel"])
			}
			if args["payload"] != `{"level":"warn"}` {
				t.Errorf("payload = %v", args["payload"])
			}
		},
		result: map[string]interface{}{"published": true, "message_id": "m1"},
	})
	defer cleanup()
	err := messaging.NewClient(tr).Publish(context.Background(), &messaging.PublishRequest{
		Channel: "alerts",
		Payload: []byte(`{"level":"warn"}`),
	})
	if err != nil {
		t.Fatalf("Publish: %v", err)
	}
}

func TestMessagingGetStats(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_msg_stats",
		result: map[string]interface{}{
			"available":           true,
			"total_channels":      float64(5),
			"total_subscriptions": float64(12),
			"messages_published":  float64(100),
			"buffer_utilization":  0.42,
		},
	})
	defer cleanup()
	stats, err := messaging.NewClient(tr).GetStats(context.Background())
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.TotalChannels != 5 || stats.TotalSubscriptions != 12 || stats.BufferUtilization != 0.42 {
		t.Errorf("decoded stats mismatch: %+v", stats)
	}
}

func TestMessagingToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:  "quicbaas_msg_get_channel",
		toolError: true,
		rawText:   "channel not found",
	})
	defer cleanup()
	_, err := messaging.NewClient(tr).GetChannel(context.Background(), "missing")
	if err == nil {
		t.Fatal("expected error from tool-level failure")
	}
}

func TestMessagingUnsupportedMethod(t *testing.T) {
	tr, err := New(&transport.Config{Address: "http://localhost:1"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer tr.Close()
	h := newMessagingHandler(tr)
	if _, err := h.transformRequest("WatchChannel", &messaging.WatchChannelRequest{}); err == nil {
		t.Error("WatchChannel must be reported as unsupported")
	}
}

// =============================================================================
// ObjectStorage end-to-end
// =============================================================================

func TestObjectStoragePutGet(t *testing.T) {
	checksum := []byte{0xde, 0xad, 0xbe, 0xef}

	t.Run("put", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
			wantTool: "quicbaas_object_put",
			assertArgs: func(t *testing.T, args map[string]interface{}) {
				if args["key"] != "ns:obj" {
					t.Errorf("key = %v", args["key"])
				}
				if args["value"] != base64.StdEncoding.EncodeToString([]byte("hello")) {
					t.Errorf("value = %v", args["value"])
				}
			},
			result: map[string]interface{}{
				"key":        "ns:obj",
				"object_id":  float64(42),
				"checksum":   hex.EncodeToString(checksum),
				"size":       float64(5),
				"backend":    "local",
				"version":    float64(1),
				"created_at": float64(1700000000000000000),
			},
		})
		defer cleanup()
		resp, err := objectstorage.NewClient(tr).PutObject(context.Background(),
			&objectstorage.PutObjectRequest{Key: "ns:obj", Value: []byte("hello")})
		if err != nil {
			t.Fatalf("PutObject: %v", err)
		}
		if resp.ObjectID != 42 || resp.Size != 5 || resp.Backend != "local" {
			t.Errorf("decoded put mismatch: %+v", resp)
		}
		if hex.EncodeToString(resp.Checksum) != hex.EncodeToString(checksum) {
			t.Errorf("checksum mismatch: %x", resp.Checksum)
		}
	})

	t.Run("get", func(t *testing.T) {
		tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
			wantTool: "quicbaas_object_get",
			result: map[string]interface{}{
				"key":          "ns:obj",
				"value":        base64.StdEncoding.EncodeToString([]byte("hello")),
				"content_type": "text/plain",
				"size":         float64(5),
				"checksum":     hex.EncodeToString(checksum),
				"version":      float64(1),
				"backend":      "local",
				"tags":         map[string]interface{}{"env": "prod"},
			},
		})
		defer cleanup()
		resp, err := objectstorage.NewClient(tr).GetObject(context.Background(),
			&objectstorage.GetObjectRequest{Key: "ns:obj"})
		if err != nil {
			t.Fatalf("GetObject: %v", err)
		}
		if string(resp.Value) != "hello" || resp.ContentType != "text/plain" {
			t.Errorf("decoded get mismatch: %+v", resp)
		}
		if resp.Tags["env"] != "prod" {
			t.Errorf("tags = %v", resp.Tags)
		}
	})
}

func TestObjectStorageList(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_object_list",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if args["prefix"] != "ns:" {
				t.Errorf("prefix = %v", args["prefix"])
			}
		},
		result: map[string]interface{}{
			"objects": []interface{}{
				map[string]interface{}{"key": "ns:a", "size": float64(10), "version": float64(1)},
				map[string]interface{}{"key": "ns:b", "size": float64(20), "version": float64(2)},
			},
			"next_cursor": "cur",
			"has_more":    true,
			"total_count": float64(2),
		},
	})
	defer cleanup()
	resp, err := objectstorage.NewClient(tr).ListObjects(context.Background(),
		&objectstorage.ListObjectsRequest{Prefix: "ns:"})
	if err != nil {
		t.Fatalf("ListObjects: %v", err)
	}
	if len(resp.Objects) != 2 || resp.NextCursor != "cur" || !resp.HasMore {
		t.Errorf("decoded list mismatch: %+v", resp)
	}
	if resp.Objects[1].Key != "ns:b" || resp.Objects[1].Size != 20 {
		t.Errorf("object summary mismatch: %+v", resp.Objects[1])
	}
}

func TestObjectStorageBatchPut(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_object_batch_put",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			objs, ok := args["objects"].([]interface{})
			if !ok || len(objs) != 2 {
				t.Fatalf("objects arg = %v", args["objects"])
			}
		},
		result: map[string]interface{}{
			"results": []interface{}{
				map[string]interface{}{"key": "a", "success": true, "object_id": float64(1)},
				map[string]interface{}{"key": "b", "success": false, "error": "boom"},
			},
			"success_count": float64(1),
			"fail_count":    float64(1),
		},
	})
	defer cleanup()
	resp, err := objectstorage.NewClient(tr).BatchPutObjects(context.Background(),
		&objectstorage.BatchPutObjectsRequest{Objects: []*objectstorage.PutObjectRequest{
			{Key: "a", Value: []byte("1")},
			{Key: "b", Value: []byte("2")},
		}})
	if err != nil {
		t.Fatalf("BatchPutObjects: %v", err)
	}
	if resp.Succeeded != 1 || resp.Failed != 1 {
		t.Errorf("counts mismatch: succeeded=%d failed=%d", resp.Succeeded, resp.Failed)
	}
	if resp.Errors["b"] != "boom" {
		t.Errorf("errors = %v", resp.Errors)
	}
}

func TestObjectStorageGetPolicy(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_object_get_policy",
		result: map[string]interface{}{
			"key": "ns:obj",
			"replication": map[string]interface{}{
				"enabled":       true,
				"min_replicas":  float64(2),
				"mode":          "async",
				"priority":      float64(5),
				"target_stores": []interface{}{"s3", "gcs"},
			},
			"retention": map[string]interface{}{
				"enabled": true,
				"days":    float64(30),
			},
		},
	})
	defer cleanup()
	resp, err := objectstorage.NewClient(tr).GetObjectPolicy(context.Background(),
		&objectstorage.GetObjectPolicyRequest{Key: "ns:obj"})
	if err != nil {
		t.Fatalf("GetObjectPolicy: %v", err)
	}
	if resp.ReplicationPolicy == nil || resp.ReplicationPolicy.MinReplicas != 2 {
		t.Fatalf("replication policy mismatch: %+v", resp.ReplicationPolicy)
	}
	if len(resp.ReplicationPolicy.TargetStores) != 2 {
		t.Errorf("target_stores = %v", resp.ReplicationPolicy.TargetStores)
	}
	if resp.RetentionPolicy == nil || resp.RetentionPolicy.RetentionDays != 30 {
		t.Errorf("retention policy mismatch: %+v", resp.RetentionPolicy)
	}
}

func TestObjectStorageToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:  "quicbaas_object_get",
		toolError: true,
		rawText:   "object not found",
	})
	defer cleanup()
	_, err := objectstorage.NewClient(tr).GetObject(context.Background(),
		&objectstorage.GetObjectRequest{Key: "ns:missing"})
	if err == nil {
		t.Fatal("expected error from tool-level failure")
	}
}

func TestObjectStorageMalformedValue(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_object_get",
		result: map[string]interface{}{
			"key":   "ns:obj",
			"value": "%%%not-base64%%%",
		},
	})
	defer cleanup()
	_, err := objectstorage.NewClient(tr).GetObject(context.Background(),
		&objectstorage.GetObjectRequest{Key: "ns:obj"})
	if err == nil {
		t.Fatal("expected error decoding malformed base64 value")
	}
}
