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

package mcp

import (
	"context"
	"encoding/base64"
	"reflect"
	"testing"
	"time"

	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

const kmsServiceName = "quicbaas.KMS"

// kmsStamp is a whole-second instant: the KMS tools send Unix seconds.
var kmsStamp = time.Unix(1767225600, 0).UTC()

// refuseKMSCall fails the test when a refused request reaches the tool.
func refuseKMSCall(t *testing.T, _ map[string]interface{}) {
	t.Error("a refused request must not reach the tool")
}

func TestKMSMCP_ListKeyVersions(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_key_versions",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if !reflect.DeepEqual(args, map[string]interface{}{"key_id": "key-1"}) {
				t.Errorf("tool arguments = %v", args)
			}
		},
		result: map[string]interface{}{
			"key_id": "key-1",
			"versions": []interface{}{
				map[string]interface{}{
					"version": 3, "status": "disabled",
					"algorithm": "ECDSA-P256", "created_at": kmsStamp.Unix(),
				},
			},
			"has_more": false, "next_cursor": "", "timestamp": kmsStamp.Unix(),
		},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).ListKeyVersions(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("ListKeyVersions: %v", err)
	}
	want := &kmsclient.ListKeyVersionsResponse{
		KeyID: "key-1",
		Versions: []kmsclient.KeyVersionInfo{{
			Version:   3,
			Status:    kmsclient.KeyStatusDisabled,
			Algorithm: kmsclient.AlgorithmECDSAP256,
			CreatedAt: kmsStamp,
		}},
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
}

func TestKMSMCP_ListKeyVersions_ToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:      "quicbaas_kms_key_versions",
		result:        map[string]interface{}{"error": "key not found"},
		toolError:     true,
		toolErrorCode: MCPResourceNotFound,
	})
	defer cleanup()

	_, err := kmsclient.NewClient(tr).ListKeyVersions(context.Background(), "missing")
	if !sdkerrors.IsNotFound(err) {
		t.Errorf("err = %v, want NotFound", err)
	}
}

func TestKMSMCP_ListKeyVersions_BadRequest(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:   "quicbaas_kms_key_versions",
		assertArgs: refuseKMSCall,
	})
	defer cleanup()

	_, err := tr.Request(context.Background(), kmsServiceName, "ListKeyVersions", "key-1")
	if !sdkerrors.IsInvalidInput(err) {
		t.Errorf("err = %v, want InvalidInput", err)
	}
}

func TestKMSMCP_UpdateKeyStatus(t *testing.T) {
	for _, tc := range []struct {
		status kmsclient.KeyStatus
		wire   string
	}{
		{kmsclient.KeyStatusEnabled, "enabled"},
		{kmsclient.KeyStatusDisabled, "disabled"},
	} {
		t.Run(tc.wire, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: "quicbaas_kms_key_status_update",
				assertArgs: func(t *testing.T, args map[string]interface{}) {
					want := map[string]interface{}{"key_id": "key-1", "status": tc.wire, "version": float64(2)}
					if !reflect.DeepEqual(args, want) {
						t.Errorf("tool arguments = %v, want %v", args, want)
					}
				},
				result: map[string]interface{}{
					"key_id": "key-1", "version": 2, "status": tc.wire, "timestamp": kmsStamp.Unix(),
				},
			})
			defer cleanup()

			resp, err := kmsclient.NewClient(tr).UpdateKeyStatus(context.Background(),
				&kmsclient.UpdateKeyStatusRequest{KeyID: "key-1", Version: 2, Status: tc.status})
			if err != nil {
				t.Fatalf("UpdateKeyStatus: %v", err)
			}
			want := &kmsclient.UpdateKeyStatusResponse{KeyID: "key-1", Version: 2, Status: tc.status}
			if !reflect.DeepEqual(resp, want) {
				t.Errorf("response = %+v, want %+v", resp, want)
			}
		})
	}
}

func TestKMSMCP_UpdateKeyStatus_UnsupportedStatus(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:   "quicbaas_kms_key_status_update",
		assertArgs: refuseKMSCall,
	})
	defer cleanup()

	for _, status := range []kmsclient.KeyStatus{
		kmsclient.KeyStatusPendingDeletion, kmsclient.KeyStatusDeleted,
		kmsclient.KeyStatusRotating, kmsclient.KeyStatusCompromised, kmsclient.KeyStatus(99),
	} {
		_, err := kmsclient.NewClient(tr).UpdateKeyStatus(context.Background(),
			&kmsclient.UpdateKeyStatusRequest{KeyID: "key-1", Status: status})
		if !sdkerrors.IsInvalidInput(err) {
			t.Errorf("status %v: err = %v, want InvalidInput", status, err)
		}
	}
}

func TestKMSMCP_UpdateKeyStatus_BadRequest(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:   "quicbaas_kms_key_status_update",
		assertArgs: refuseKMSCall,
	})
	defer cleanup()

	for _, req := range []interface{}{
		struct{ KeyID string }{KeyID: "key-1"},
		(*kmsclient.UpdateKeyStatusRequest)(nil),
	} {
		_, err := tr.Request(context.Background(), kmsServiceName, "UpdateKeyStatus", req)
		if !sdkerrors.IsInvalidInput(err) {
			t.Errorf("request %T: err = %v, want InvalidInput", req, err)
		}
	}
}

func TestKMSMCP_GetKeyMetadata(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_get_metadata",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if !reflect.DeepEqual(args, map[string]interface{}{"key_id": "key-1"}) {
				t.Errorf("tool arguments = %v", args)
			}
		},
		result: map[string]interface{}{
			"metadata": map[string]interface{}{
				"key_id": "key-1", "name": "signing", "description": "release signing",
				"algorithm": "ED25519", "status": "enabled", "version": 4,
				"exportable": true, "deletion_protection": true,
				"created_at": kmsStamp.Unix(), "updated_at": kmsStamp.Add(time.Hour).Unix(),
				"last_used_at": kmsStamp.Add(2 * time.Hour).Unix(),
				"usage":        []interface{}{"sign", "verify"},
				"tags":         map[string]interface{}{"team": "release"},
				"owner":        "ops", "timestamp": kmsStamp.Unix(),
			},
			"timestamp": kmsStamp.Unix(),
		},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).GetKeyMetadata(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("GetKeyMetadata: %v", err)
	}
	want := &kmsclient.KeyMetadataResponse{
		KeyID: "key-1", Name: "signing", Description: "release signing",
		Algorithm: kmsclient.AlgorithmED25519, Status: kmsclient.KeyStatusEnabled,
		Usage:   []kmsclient.KeyUsage{kmsclient.KeyUsageSign, kmsclient.KeyUsageVerify},
		Version: 4, Exportable: true, DeletionProtection: true,
		CreatedAt: kmsStamp, UpdatedAt: kmsStamp.Add(time.Hour), LastUsedAt: kmsStamp.Add(2 * time.Hour),
		Tags: map[string]string{"team": "release"}, Owner: "ops",
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
}

func TestKMSMCP_GetKeyMetadata_NeverUsed(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_get_metadata",
		result: map[string]interface{}{
			"metadata": map[string]interface{}{
				"key_id": "key-1", "algorithm": "AES-256-GCM", "status": "enabled",
				"version": 1, "created_at": kmsStamp.Unix(), "updated_at": kmsStamp.Unix(),
			},
		},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).GetKeyMetadata(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("GetKeyMetadata: %v", err)
	}
	if !resp.LastUsedAt.IsZero() || resp.Usage != nil {
		t.Errorf("absent last_used_at and usage must stay zero: %+v", resp)
	}
}

func TestKMSMCP_GetKeyMetadata_BadRequest(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:   "quicbaas_kms_get_metadata",
		assertArgs: refuseKMSCall,
	})
	defer cleanup()

	_, err := tr.Request(context.Background(), kmsServiceName, "GetKeyMetadata",
		&kmsclient.UpdateKeyStatusRequest{KeyID: "key-1"})
	if !sdkerrors.IsInvalidInput(err) {
		t.Errorf("err = %v, want InvalidInput", err)
	}
}

func TestKMSMCP_GetSupportedAlgorithms(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_get_supported_algorithms",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if len(args) != 0 {
				t.Errorf("tool arguments = %v, want none", args)
			}
		},
		result: map[string]interface{}{
			"algorithms": []interface{}{"AES-256-GCM", "ED25519"},
			"timestamp":  kmsStamp.Unix(),
		},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).GetSupportedAlgorithms(context.Background())
	if err != nil {
		t.Fatalf("GetSupportedAlgorithms: %v", err)
	}
	want := &kmsclient.SupportedAlgorithmsResponse{Algorithms: []string{"AES-256-GCM", "ED25519"}}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
}

func TestKMSMCP_GetSupportedAlgorithms_MalformedResult(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_get_supported_algorithms",
		rawText:  `["AES-256-GCM"]`,
	})
	defer cleanup()

	if _, err := kmsclient.NewClient(tr).GetSupportedAlgorithms(context.Background()); sdkerrors.Code(err) != sdkerrors.CodeInternal {
		t.Errorf("err = %v, want Internal", err)
	}
}

func TestKMSMCP_BackendStatus(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_backend_status",
		result: map[string]interface{}{
			"backend_type":         "pkcs8",
			"status":               "healthy",
			"capabilities":         map[string]interface{}{"encrypt": true, "multi_party": false},
			"supported_algorithms": []interface{}{"AES-256-GCM"},
			"timestamp":            kmsStamp.Unix(),
		},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).BackendStatus(context.Background())
	if err != nil {
		t.Fatalf("BackendStatus: %v", err)
	}
	want := &kmsclient.BackendStatusResponse{
		BackendType:  "pkcs8",
		Status:       "healthy",
		Capabilities: map[string]bool{"encrypt": true, "multi_party": false},
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
}

func TestKMSMCP_BackendStatus_ToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:      "quicbaas_kms_backend_status",
		result:        map[string]interface{}{"error": "KMS manager not available"},
		toolError:     true,
		toolErrorCode: MCPServiceUnavailable,
	})
	defer cleanup()

	if _, err := kmsclient.NewClient(tr).BackendStatus(context.Background()); !sdkerrors.IsUnavailable(err) {
		t.Errorf("err = %v, want Unavailable", err)
	}
}

func TestKMSMCP_GetKey(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_get_key",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if !reflect.DeepEqual(args, map[string]interface{}{"key_id": "key-1"}) {
				t.Errorf("tool arguments = %v", args)
			}
		},
		result: map[string]interface{}{
			"key_id": "key-1", "name": "signing", "algorithm": "ECDSA-P384",
			"status": "enabled", "version": 3, "exportable": false,
			"deletion_protection": true,
			"created_at":          kmsStamp.Unix(), "updated_at": kmsStamp.Add(time.Hour).Unix(),
			"usage": []interface{}{"sign", "verify"}, "timestamp": kmsStamp.Unix(),
		},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).GetKey(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("GetKey: %v", err)
	}
	want := &kmsclient.KeyInfo{
		KeyID: "key-1", Name: "signing", Algorithm: kmsclient.AlgorithmECDSAP384, Version: 3,
		Usage:     []kmsclient.KeyUsage{kmsclient.KeyUsageSign, kmsclient.KeyUsageVerify},
		CreatedAt: kmsStamp, RotatedAt: kmsStamp.Add(time.Hour),
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
}

func TestKMSMCP_GetKey_ToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:      "quicbaas_kms_get_key",
		result:        map[string]interface{}{"error": "key not found"},
		toolError:     true,
		toolErrorCode: MCPResourceNotFound,
	})
	defer cleanup()

	if _, err := kmsclient.NewClient(tr).GetKey(context.Background(), "missing"); !sdkerrors.IsNotFound(err) {
		t.Errorf("err = %v, want NotFound", err)
	}
}

func TestKMSMCP_GetKey_BadRequest(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:   "quicbaas_kms_get_key",
		assertArgs: refuseKMSCall,
	})
	defer cleanup()

	_, err := tr.Request(context.Background(), kmsServiceName, "GetKey",
		&kmsclient.UpdateKeyStatusRequest{Version: 1})
	if !sdkerrors.IsInvalidInput(err) {
		t.Errorf("err = %v, want InvalidInput", err)
	}
}

func TestKMSMCP_ListKeys(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_list_keys",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if len(args) != 0 {
				t.Errorf("tool arguments = %v, want none", args)
			}
		},
		result: map[string]interface{}{
			"keys": []interface{}{
				map[string]interface{}{
					"key_id": "key-1", "algorithm": "AES-256-GCM", "status": "enabled",
					"version": 1, "created_at": kmsStamp.Unix(),
				},
				map[string]interface{}{
					"key_id": "key-2", "algorithm": "RSA-4096", "status": "disabled",
					"version": 2, "created_at": kmsStamp.Add(time.Hour).Unix(),
					"usage": []interface{}{"encrypt", "decrypt", "wrap_key"},
				},
			},
			"has_more": false, "total_count": 2, "timestamp": kmsStamp.Unix(),
		},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	want := &kmsclient.ListKeysResponse{Keys: []kmsclient.KeyInfo{
		{KeyID: "key-1", Algorithm: kmsclient.AlgorithmAES256GCM, Version: 1, CreatedAt: kmsStamp},
		{
			KeyID: "key-2", Algorithm: kmsclient.AlgorithmRSA4096, Version: 2,
			Usage:     []kmsclient.KeyUsage{kmsclient.KeyUsageEncrypt, kmsclient.KeyUsageDecrypt, kmsclient.KeyUsageWrap},
			CreatedAt: kmsStamp.Add(time.Hour),
		},
	}}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
}

func TestKMSMCP_ListKeys_Empty(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_list_keys",
		result: map[string]interface{}{
			"keys": []interface{}{}, "has_more": false, "total_count": 0, "timestamp": kmsStamp.Unix(),
		},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).ListKeys(context.Background())
	if err != nil {
		t.Fatalf("ListKeys: %v", err)
	}
	if resp.Keys == nil || len(resp.Keys) != 0 {
		t.Errorf("Keys = %#v, want empty and non-nil", resp.Keys)
	}
}

func TestKMSMCP_ListKeys_ToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:      "quicbaas_kms_list_keys",
		result:        map[string]interface{}{"error": "KMS manager not available"},
		toolError:     true,
		toolErrorCode: MCPServiceUnavailable,
	})
	defer cleanup()

	if _, err := kmsclient.NewClient(tr).ListKeys(context.Background()); !sdkerrors.IsUnavailable(err) {
		t.Errorf("err = %v, want Unavailable", err)
	}
}

func TestKMSMCP_GetPublicKey(t *testing.T) {
	pub := []byte{0x04, 0x01, 0x02, 0x03}
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_get_public_key",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if !reflect.DeepEqual(args, map[string]interface{}{"key_id": "key-1"}) {
				t.Errorf("tool arguments = %v", args)
			}
		},
		result: map[string]interface{}{
			"key_id": "key-1", "public_key": base64.StdEncoding.EncodeToString(pub),
			"algorithm": "ED25519", "version": 2, "timestamp": kmsStamp.Unix(),
		},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).GetPublicKey(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("GetPublicKey: %v", err)
	}
	want := &kmsclient.GetPublicKeyResponse{
		KeyID: "key-1", PublicKey: pub, Algorithm: kmsclient.AlgorithmED25519, Version: 2,
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
}

func TestKMSMCP_GetPublicKey_BadPublicKey(t *testing.T) {
	for name, result := range map[string]map[string]interface{}{
		"missing":   {"key_id": "key-1", "algorithm": "ED25519", "version": 1},
		"malformed": {"key_id": "key-1", "public_key": "not base64!", "version": 1},
		"empty":     {"key_id": "key-1", "public_key": "", "version": 1},
	} {
		t.Run(name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: "quicbaas_kms_get_public_key",
				result:   result,
			})
			defer cleanup()

			_, err := kmsclient.NewClient(tr).GetPublicKey(context.Background(), "key-1")
			if sdkerrors.Code(err) != sdkerrors.CodeInternal {
				t.Errorf("err = %v, want Internal", err)
			}
		})
	}
}

func TestKMSMCP_GetPublicKey_ToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:      "quicbaas_kms_get_public_key",
		result:        map[string]interface{}{"error": "key not found"},
		toolError:     true,
		toolErrorCode: MCPResourceNotFound,
	})
	defer cleanup()

	if _, err := kmsclient.NewClient(tr).GetPublicKey(context.Background(), "missing"); !sdkerrors.IsNotFound(err) {
		t.Errorf("err = %v, want NotFound", err)
	}
}

func TestKMSMCP_GetPublicKey_BadRequest(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:   "quicbaas_kms_get_public_key",
		assertArgs: refuseKMSCall,
	})
	defer cleanup()

	for _, req := range []interface{}{"key-1", &kmsclient.UpdateKeyStatusRequest{Version: 1}} {
		_, err := tr.Request(context.Background(), kmsServiceName, "GetPublicKey", req)
		if !sdkerrors.IsInvalidInput(err) {
			t.Errorf("request %T: err = %v, want InvalidInput", req, err)
		}
	}
}

// kmsImportMaterial is the key material the ImportKey tests send. Test
// failures never print it.
var kmsImportMaterial = []byte{0x01, 0x02, 0x03, 0xff}

// assertKMSImportArgs checks the base64 key material on its own, then the
// remaining arguments, so no failure message echoes the material.
func assertKMSImportArgs(want map[string]interface{}) func(*testing.T, map[string]interface{}) {
	return func(t *testing.T, args map[string]interface{}) {
		if args["key_material"] != base64.StdEncoding.EncodeToString(kmsImportMaterial) {
			t.Error("key_material is not the base64 of the request key material")
		}
		delete(args, "key_material")
		if !reflect.DeepEqual(args, want) {
			t.Errorf("tool arguments = %v, want %v", args, want)
		}
	}
}

func TestKMSMCP_ImportKey(t *testing.T) {
	for _, tc := range []struct {
		name     string
		req      *kmsclient.ImportKeyRequest
		wantArgs map[string]interface{}
	}{
		{
			name: "all fields",
			req: &kmsclient.ImportKeyRequest{
				KeyMaterial: kmsImportMaterial, WrappingKeyID: "wrap-1", Name: "imported",
				Description: "imported signing key", Algorithm: kmsclient.AlgorithmED25519,
				Usage: []kmsclient.KeyUsage{kmsclient.KeyUsageSign, kmsclient.KeyUsageVerify},
				Tags:  map[string]string{"team": "release"},
			},
			wantArgs: map[string]interface{}{
				"name": "imported", "algorithm": "ED25519", "wrapping_key_id": "wrap-1",
				"description": "imported signing key", "usage": []interface{}{"sign", "verify"},
				"tags": map[string]interface{}{"team": "release"},
			},
		},
		{
			name:     "required only",
			req:      &kmsclient.ImportKeyRequest{KeyMaterial: kmsImportMaterial, Name: "imported"},
			wantArgs: map[string]interface{}{"name": "imported", "algorithm": "AES-256-GCM"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool:   "quicbaas_kms_import_key",
				assertArgs: assertKMSImportArgs(tc.wantArgs),
				result: map[string]interface{}{
					"key_id": "imported", "name": "imported", "algorithm": "ED25519",
					"status": "enabled", "version": 3, "created_at": kmsStamp.Unix(),
					"updated_at": kmsStamp.Unix(), "timestamp": kmsStamp.Unix(),
				},
			})
			defer cleanup()

			resp, err := kmsclient.NewClient(tr).ImportKey(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("ImportKey: %v", err)
			}
			want := &kmsclient.ImportKeyResponse{KeyID: "imported", Version: 3, Algorithm: kmsclient.AlgorithmED25519}
			if !reflect.DeepEqual(resp, want) {
				t.Errorf("response = %+v, want %+v", resp, want)
			}
		})
	}
}

func TestKMSMCP_ImportKey_BadRequest(t *testing.T) {
	for name, req := range map[string]interface{}{
		"wrong type":            &kmsclient.UpdateKeyStatusRequest{KeyID: "key-1"},
		"nil request":           (*kmsclient.ImportKeyRequest)(nil),
		"no name":               &kmsclient.ImportKeyRequest{KeyMaterial: kmsImportMaterial},
		"no key material":       &kmsclient.ImportKeyRequest{Name: "imported"},
		"unsupported algorithm": &kmsclient.ImportKeyRequest{Name: "imported", KeyMaterial: kmsImportMaterial, Algorithm: kmsclient.KeyAlgorithm(99)},
		"unsupported usage": &kmsclient.ImportKeyRequest{Name: "imported", KeyMaterial: kmsImportMaterial,
			Usage: []kmsclient.KeyUsage{kmsclient.KeyUsage(99)}},
	} {
		t.Run(name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool:   "quicbaas_kms_import_key",
				assertArgs: refuseKMSCall,
			})
			defer cleanup()

			if _, err := tr.Request(context.Background(), kmsServiceName, "ImportKey", req); !sdkerrors.IsInvalidInput(err) {
				t.Errorf("err = %v, want InvalidInput", err)
			}
		})
	}
}

func TestKMSMCP_RevokeKey(t *testing.T) {
	for _, tc := range []struct {
		name     string
		reason   string
		wantArgs map[string]interface{}
	}{
		{"with reason", "compromised", map[string]interface{}{"key_id": "key-1", "reason": "compromised"}},
		{"without reason", "", map[string]interface{}{"key_id": "key-1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: "quicbaas_kms_revoke_key",
				assertArgs: func(t *testing.T, args map[string]interface{}) {
					if !reflect.DeepEqual(args, tc.wantArgs) {
						t.Errorf("tool arguments = %v, want %v", args, tc.wantArgs)
					}
				},
				result: map[string]interface{}{"key_id": "key-1", "status": "disabled", "timestamp": kmsStamp.Unix()},
			})
			defer cleanup()

			if err := kmsclient.NewClient(tr).RevokeKey(context.Background(), "key-1", tc.reason); err != nil {
				t.Errorf("RevokeKey: %v", err)
			}
		})
	}
}

func TestKMSMCP_RevokeKey_BadRequest(t *testing.T) {
	for name, req := range map[string]interface{}{
		"wrong type": &kmsclient.UpdateKeyStatusRequest{Version: 1},
		"no key id":  map[string]interface{}{"reason": "compromised"},
	} {
		t.Run(name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool:   "quicbaas_kms_revoke_key",
				assertArgs: refuseKMSCall,
			})
			defer cleanup()

			if _, err := tr.Request(context.Background(), kmsServiceName, "RevokeKey", req); !sdkerrors.IsInvalidInput(err) {
				t.Errorf("err = %v, want InvalidInput", err)
			}
		})
	}
}

func TestKMSMCP_UpdateKeyMetadata(t *testing.T) {
	for _, tc := range []struct {
		name     string
		req      *kmsclient.UpdateKeyMetadataRequest
		wantArgs map[string]interface{}
	}{
		{
			name: "all fields",
			req: &kmsclient.UpdateKeyMetadataRequest{
				KeyID: "key-1", Name: "signing", Description: "release signing",
				Tags: map[string]string{"team": "release"}, RemoveTags: []string{"old"},
			},
			wantArgs: map[string]interface{}{
				"key_id": "key-1", "name": "signing", "description": "release signing",
				"tags": map[string]interface{}{"team": "release"}, "remove_tags": []interface{}{"old"},
			},
		},
		{
			name:     "key only",
			req:      &kmsclient.UpdateKeyMetadataRequest{KeyID: "key-1"},
			wantArgs: map[string]interface{}{"key_id": "key-1"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: "quicbaas_kms_update_metadata",
				assertArgs: func(t *testing.T, args map[string]interface{}) {
					if !reflect.DeepEqual(args, tc.wantArgs) {
						t.Errorf("tool arguments = %v, want %v", args, tc.wantArgs)
					}
				},
				result: map[string]interface{}{
					"metadata": map[string]interface{}{
						"key_id": "key-1", "name": "signing", "description": "release signing",
						"algorithm": "ED25519", "status": "enabled", "version": 4,
						"exportable": true, "deletion_protection": true,
						"created_at": kmsStamp.Unix(), "updated_at": kmsStamp.Add(time.Hour).Unix(),
						"last_used_at": kmsStamp.Add(2 * time.Hour).Unix(),
						"usage":        []interface{}{"sign", "verify"},
						"tags":         map[string]interface{}{"team": "release"},
						"owner":        "ops", "timestamp": kmsStamp.Unix(),
					},
					"timestamp": kmsStamp.Unix(),
				},
			})
			defer cleanup()

			resp, err := kmsclient.NewClient(tr).UpdateKeyMetadata(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("UpdateKeyMetadata: %v", err)
			}
			want := &kmsclient.KeyMetadataResponse{
				KeyID: "key-1", Name: "signing", Description: "release signing",
				Algorithm: kmsclient.AlgorithmED25519, Status: kmsclient.KeyStatusEnabled,
				Usage:   []kmsclient.KeyUsage{kmsclient.KeyUsageSign, kmsclient.KeyUsageVerify},
				Version: 4, Exportable: true, DeletionProtection: true,
				CreatedAt: kmsStamp, UpdatedAt: kmsStamp.Add(time.Hour), LastUsedAt: kmsStamp.Add(2 * time.Hour),
				Tags: map[string]string{"team": "release"}, Owner: "ops",
			}
			if !reflect.DeepEqual(resp, want) {
				t.Errorf("response = %+v, want %+v", resp, want)
			}
		})
	}
}

func TestKMSMCP_UpdateKeyMetadata_BadRequest(t *testing.T) {
	for name, req := range map[string]interface{}{
		"wrong type":  &kmsclient.UpdateKeyStatusRequest{KeyID: "key-1"},
		"nil request": (*kmsclient.UpdateKeyMetadataRequest)(nil),
		"no key id":   &kmsclient.UpdateKeyMetadataRequest{Name: "signing"},
	} {
		t.Run(name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool:   "quicbaas_kms_update_metadata",
				assertArgs: refuseKMSCall,
			})
			defer cleanup()

			if _, err := tr.Request(context.Background(), kmsServiceName, "UpdateKeyMetadata", req); !sdkerrors.IsInvalidInput(err) {
				t.Errorf("err = %v, want InvalidInput", err)
			}
		})
	}
}

// kmsWriteVerbCalls drives ImportKey, RevokeKey and UpdateKeyMetadata
// through the KMS client, keyed by tool name.
var kmsWriteVerbCalls = map[string]func(kmsclient.Client) error{
	"quicbaas_kms_import_key": func(c kmsclient.Client) error {
		_, err := c.ImportKey(context.Background(),
			&kmsclient.ImportKeyRequest{Name: "imported", KeyMaterial: kmsImportMaterial})
		return err
	},
	"quicbaas_kms_revoke_key": func(c kmsclient.Client) error {
		return c.RevokeKey(context.Background(), "key-1", "compromised")
	},
	"quicbaas_kms_update_metadata": func(c kmsclient.Client) error {
		_, err := c.UpdateKeyMetadata(context.Background(),
			&kmsclient.UpdateKeyMetadataRequest{KeyID: "key-1", Name: "signing"})
		return err
	},
}

func TestKMSMCP_WriteVerbs_MissingKeyID(t *testing.T) {
	results := map[string]map[string]interface{}{
		"quicbaas_kms_import_key":      {"name": "imported", "algorithm": "ED25519", "version": 1},
		"quicbaas_kms_revoke_key":      {"status": "disabled", "timestamp": kmsStamp.Unix()},
		"quicbaas_kms_update_metadata": {"metadata": map[string]interface{}{"name": "signing"}},
	}
	for tool, call := range kmsWriteVerbCalls {
		t.Run(tool, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{wantTool: tool, result: results[tool]})
			defer cleanup()

			if err := call(kmsclient.NewClient(tr)); sdkerrors.Code(err) != sdkerrors.CodeInternal {
				t.Errorf("err = %v, want Internal", err)
			}
		})
	}
}

func TestKMSMCP_UpdateKeyMetadata_NoMetadata(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_update_metadata",
		result:   map[string]interface{}{"timestamp": kmsStamp.Unix()},
	})
	defer cleanup()

	_, err := kmsclient.NewClient(tr).UpdateKeyMetadata(context.Background(),
		&kmsclient.UpdateKeyMetadataRequest{KeyID: "key-1"})
	if sdkerrors.Code(err) != sdkerrors.CodeInternal {
		t.Errorf("err = %v, want Internal", err)
	}
}

func TestKMSMCP_WriteVerbs_ToolError(t *testing.T) {
	for tool, call := range kmsWriteVerbCalls {
		t.Run(tool, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool:      tool,
				result:        map[string]interface{}{"error": "key not found"},
				toolError:     true,
				toolErrorCode: MCPResourceNotFound,
			})
			defer cleanup()

			if err := call(kmsclient.NewClient(tr)); !sdkerrors.IsNotFound(err) {
				t.Errorf("err = %v, want NotFound", err)
			}
		})
	}
}

func TestKMSMCP_ToolNames(t *testing.T) {
	want := map[string]string{
		"ListKeyVersions":         "quicbaas_kms_key_versions",
		"UpdateKeyStatus":         "quicbaas_kms_key_status_update",
		"GetKeyMetadata":          "quicbaas_kms_get_metadata",
		"GetSupportedAlgorithms":  "quicbaas_kms_get_supported_algorithms",
		"BackendStatus":           "quicbaas_kms_backend_status",
		"GetKey":                  "quicbaas_kms_get_key",
		"ListKeys":                "quicbaas_kms_list_keys",
		"ImportKey":               "quicbaas_kms_import_key",
		"RevokeKey":               "quicbaas_kms_revoke_key",
		"UpdateKeyMetadata":       "quicbaas_kms_update_metadata",
		"GenerateKey":             "quicbaas_kms_generate_key",
		"RotateKey":               "quicbaas_kms_rotate_key",
		"DeleteKey":               "quicbaas_kms_delete_key",
		"SetKeyPolicy":            "quicbaas_kms_set_policy",
		"GetKeyPolicy":            "quicbaas_kms_get_policy",
		"SetKeyRotationPolicy":    "quicbaas_kms_set_rotation_policy",
		"GetKeyRotationPolicy":    "quicbaas_kms_get_rotation_policy",
		"DeleteKeyRotationPolicy": "quicbaas_kms_delete_rotation_policy",
		"ExportKey":               "quicbaas_kms_export_key",
		"UnwrapKey":               "quicbaas_kms_unwrap_key",
	}
	for method, tool := range want {
		if got := ToolName(kmsServiceName, method); got != tool {
			t.Errorf("ToolName(%q) = %q, want %q", method, got, tool)
		}
		if got := kmsVerbs[method].tool; got != tool {
			t.Errorf("kmsVerbs[%q].tool = %q, want %q", method, got, tool)
		}
	}
	if len(kmsVerbs) != len(want) {
		t.Errorf("kmsVerbs has %d verbs, want %d", len(kmsVerbs), len(want))
	}
	if got := ToolName(kmsServiceName, "GetPublicKey"); got != kmsPublicKeyTool {
		t.Errorf("ToolName(GetPublicKey) = %q, want %q", got, kmsPublicKeyTool)
	}
}

func TestKMSMCP_GenerateKey(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_generate_key",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			want := map[string]interface{}{
				"name": "signing", "algorithm": "ED25519",
				"usage":      []interface{}{"sign", "verify"},
				"exportable": true, "deletion_protection": true,
			}
			if !reflect.DeepEqual(args, want) {
				t.Errorf("tool arguments = %v, want %v", args, want)
			}
		},
		result: map[string]interface{}{
			"key_id": "key-1", "name": "signing", "algorithm": "ED25519",
			"status": "enabled", "version": 1, "exportable": true,
			"deletion_protection": true, "usage": []interface{}{"sign", "verify"},
			"created_at": kmsStamp.Unix(), "updated_at": kmsStamp.Unix(), "timestamp": kmsStamp.Unix(),
		},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).GenerateKey(context.Background(), &kmsclient.GenerateKeyRequest{
		Name: "signing", Algorithm: kmsclient.AlgorithmED25519,
		Usage:      []kmsclient.KeyUsage{kmsclient.KeyUsageSign, kmsclient.KeyUsageVerify},
		Exportable: true, DeletionProtection: true,
	})
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	want := &kmsclient.GenerateKeyResponse{
		KeyID: "key-1", Version: 1, Algorithm: kmsclient.AlgorithmED25519,
		Exportable: true, DeletionProtection: true,
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
}

// TestKMSMCP_GenerateKey_CarriesDescriptionTagsOwner proves the description,
// tags and owner reach the tool arguments.
func TestKMSMCP_GenerateKey_CarriesDescriptionTagsOwner(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_generate_key",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			want := map[string]interface{}{
				"name": "k", "algorithm": "AES-256-GCM",
				"description": "d",
				"tags":        map[string]interface{}{"env": "prod"},
				"owner":       "svc-a",
			}
			if !reflect.DeepEqual(args, want) {
				t.Errorf("tool arguments = %v, want %v", args, want)
			}
		},
		result: map[string]interface{}{
			"key_id": "key-1", "name": "k", "algorithm": "AES-256-GCM",
			"status": "enabled", "version": 1,
			"created_at": kmsStamp.Unix(), "updated_at": kmsStamp.Unix(), "timestamp": kmsStamp.Unix(),
		},
	})
	defer cleanup()

	_, err := kmsclient.NewClient(tr).GenerateKey(context.Background(), &kmsclient.GenerateKeyRequest{
		Name: "k", Algorithm: kmsclient.AlgorithmAES256GCM,
		Description: "d", Tags: map[string]string{"env": "prod"}, Owner: "svc-a",
	})
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
}

func TestKMSMCP_GenerateKey_OptionalUnset(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_generate_key",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			want := map[string]interface{}{"name": "data", "algorithm": "AES-256-GCM"}
			if !reflect.DeepEqual(args, want) {
				t.Errorf("tool arguments = %v, want %v", args, want)
			}
		},
		result: map[string]interface{}{"key_id": "key-2", "algorithm": "AES-256-GCM", "version": 1},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).GenerateKey(context.Background(), &kmsclient.GenerateKeyRequest{
		Name: "data", Algorithm: kmsclient.AlgorithmAES256GCM,
	})
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	want := &kmsclient.GenerateKeyResponse{KeyID: "key-2", Version: 1, Algorithm: kmsclient.AlgorithmAES256GCM}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
}

func TestKMSMCP_GenerateKey_MissingKeyID(t *testing.T) {
	for name, result := range map[string]map[string]interface{}{
		"absent": {"algorithm": "AES-256-GCM", "version": 1},
		"empty":  {"key_id": "", "algorithm": "AES-256-GCM", "version": 1},
	} {
		t.Run(name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: "quicbaas_kms_generate_key",
				result:   result,
			})
			defer cleanup()

			_, err := kmsclient.NewClient(tr).GenerateKey(context.Background(), &kmsclient.GenerateKeyRequest{
				Name: "data", Algorithm: kmsclient.AlgorithmAES256GCM,
			})
			if sdkerrors.Code(err) != sdkerrors.CodeInternal {
				t.Errorf("err = %v, want Internal", err)
			}
		})
	}
}

func TestKMSMCP_GenerateKey_ToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:      "quicbaas_kms_generate_key",
		result:        map[string]interface{}{"error": "key exists"},
		toolError:     true,
		toolErrorCode: MCPAlreadyExists,
	})
	defer cleanup()

	_, err := kmsclient.NewClient(tr).GenerateKey(context.Background(), &kmsclient.GenerateKeyRequest{
		Name: "data", Algorithm: kmsclient.AlgorithmAES256GCM,
	})
	if !sdkerrors.IsAlreadyExists(err) {
		t.Errorf("err = %v, want AlreadyExists", err)
	}
}

func TestKMSMCP_GenerateKey_BadRequest(t *testing.T) {
	for name, req := range map[string]interface{}{
		"wrong type":        &kmsclient.UpdateKeyStatusRequest{KeyID: "key-1"},
		"nil request":       (*kmsclient.GenerateKeyRequest)(nil),
		"unknown algorithm": &kmsclient.GenerateKeyRequest{Name: "data", Algorithm: kmsclient.KeyAlgorithm(99)},
		"unknown usage": &kmsclient.GenerateKeyRequest{
			Name: "data", Algorithm: kmsclient.AlgorithmAES256GCM, Usage: []kmsclient.KeyUsage{kmsclient.KeyUsage(99)},
		},
	} {
		t.Run(name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool:   "quicbaas_kms_generate_key",
				assertArgs: refuseKMSCall,
			})
			defer cleanup()

			_, err := tr.Request(context.Background(), kmsServiceName, "GenerateKey", req)
			if !sdkerrors.IsInvalidInput(err) {
				t.Errorf("err = %v, want InvalidInput", err)
			}
		})
	}
}

func TestKMSMCP_RotateKey(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_rotate_key",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if !reflect.DeepEqual(args, map[string]interface{}{"key_id": "key-1"}) {
				t.Errorf("tool arguments = %v", args)
			}
		},
		result: map[string]interface{}{
			"key_id": "key-1", "old_version": 2, "new_version": 3, "timestamp": kmsStamp.Unix(),
		},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).RotateKey(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("RotateKey: %v", err)
	}
	want := &kmsclient.RotateKeyResponse{KeyID: "key-1", NewVersion: 3}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
}

func TestKMSMCP_RotateKey_MissingResultField(t *testing.T) {
	for name, result := range map[string]map[string]interface{}{
		"no key_id":        {"old_version": 2, "new_version": 3},
		"no new_version":   {"key_id": "key-1", "old_version": 2},
		"zero new_version": {"key_id": "key-1", "old_version": 0, "new_version": 0},
	} {
		t.Run(name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: "quicbaas_kms_rotate_key",
				result:   result,
			})
			defer cleanup()

			_, err := kmsclient.NewClient(tr).RotateKey(context.Background(), "key-1")
			if sdkerrors.Code(err) != sdkerrors.CodeInternal {
				t.Errorf("err = %v, want Internal", err)
			}
		})
	}
}

func TestKMSMCP_RotateKey_ToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:      "quicbaas_kms_rotate_key",
		result:        map[string]interface{}{"error": "key not found"},
		toolError:     true,
		toolErrorCode: MCPResourceNotFound,
	})
	defer cleanup()

	if _, err := kmsclient.NewClient(tr).RotateKey(context.Background(), "missing"); !sdkerrors.IsNotFound(err) {
		t.Errorf("err = %v, want NotFound", err)
	}
}

func TestKMSMCP_RotateKey_BadRequest(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:   "quicbaas_kms_rotate_key",
		assertArgs: refuseKMSCall,
	})
	defer cleanup()

	_, err := tr.Request(context.Background(), kmsServiceName, "RotateKey",
		&kmsclient.UpdateKeyStatusRequest{Version: 1})
	if !sdkerrors.IsInvalidInput(err) {
		t.Errorf("err = %v, want InvalidInput", err)
	}
}

func TestKMSMCP_DeleteKey(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_delete_key",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if !reflect.DeepEqual(args, map[string]interface{}{"key_id": "key-1"}) {
				t.Errorf("tool arguments = %v", args)
			}
		},
		result: map[string]interface{}{
			"key_id": "key-1", "status": "pending_deletion",
			"deletion_date": kmsStamp.Unix(), "timestamp": kmsStamp.Unix(),
		},
	})
	defer cleanup()

	if err := kmsclient.NewClient(tr).DeleteKey(context.Background(), "key-1"); err != nil {
		t.Fatalf("DeleteKey: %v", err)
	}
	resp, err := tr.Request(context.Background(), kmsServiceName, "DeleteKey", map[string]string{"key_id": "key-1"})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	want := &service.DeleteKeyResponse{
		KeyID: "key-1", Status: service.KeyStatusPendingDeletion,
		DeletionDate: kmsStamp, Timestamp: kmsStamp,
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
}

func TestKMSMCP_DeleteKey_MissingKeyID(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_delete_key",
		result:   map[string]interface{}{"status": "pending_deletion"},
	})
	defer cleanup()

	err := kmsclient.NewClient(tr).DeleteKey(context.Background(), "key-1")
	if sdkerrors.Code(err) != sdkerrors.CodeInternal {
		t.Errorf("err = %v, want Internal", err)
	}
}

func TestKMSMCP_DeleteKey_ToolError(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:      "quicbaas_kms_delete_key",
		result:        map[string]interface{}{"error": "key not found"},
		toolError:     true,
		toolErrorCode: MCPResourceNotFound,
	})
	defer cleanup()

	if err := kmsclient.NewClient(tr).DeleteKey(context.Background(), "missing"); !sdkerrors.IsNotFound(err) {
		t.Errorf("err = %v, want NotFound", err)
	}
}

func TestKMSMCP_DeleteKey_BadRequest(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool:   "quicbaas_kms_delete_key",
		assertArgs: refuseKMSCall,
	})
	defer cleanup()

	_, err := tr.Request(context.Background(), kmsServiceName, "DeleteKey",
		&kmsclient.UpdateKeyStatusRequest{Version: 1})
	if !sdkerrors.IsInvalidInput(err) {
		t.Errorf("err = %v, want InvalidInput", err)
	}
}

func TestKMSMCP_SetKeyPolicy(t *testing.T) {
	expires := kmsStamp.Add(24 * time.Hour)
	for _, tc := range []struct {
		name   string
		policy *kmsclient.KeyPolicy
		want   map[string]interface{}
	}{
		{
			name: "every field",
			policy: &kmsclient.KeyPolicy{
				KeyID: "key-1", Version: 3,
				AllowedOperations: []kmsclient.KeyUsage{kmsclient.KeyUsageSign, kmsclient.KeyUsageUnwrap},
				AllowedPrincipals: []string{"svc-a"}, DeniedPrincipals: []string{"svc-b"},
				ExpiresAt: expires, RotationRequired: true, MaxUsageCount: 100, RequireMFA: true,
			},
			want: map[string]interface{}{
				"key_id": "key-1", "version": float64(3),
				"allowed_operations": []interface{}{"sign", "wrap_key"},
				"allowed_principals": []interface{}{"svc-a"}, "denied_principals": []interface{}{"svc-b"},
				"expires_at": expires.Format(time.RFC3339Nano), "rotation_required": true,
				"max_usage_count": float64(100), "require_mfa": true,
			},
		},
		{
			name:   "operations only",
			policy: &kmsclient.KeyPolicy{AllowedOperations: []kmsclient.KeyUsage{kmsclient.KeyUsageEncrypt}},
			want:   map[string]interface{}{"allowed_operations": []interface{}{"encrypt"}},
		},
		{
			name:   "no operations",
			policy: &kmsclient.KeyPolicy{},
			want:   map[string]interface{}{"allowed_operations": []interface{}{}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: "quicbaas_kms_set_policy",
				assertArgs: func(t *testing.T, args map[string]interface{}) {
					want := map[string]interface{}{"key_id": "key-1", "policy": tc.want}
					if !reflect.DeepEqual(args, want) {
						t.Errorf("tool arguments = %v, want %v", args, want)
					}
				},
				result: map[string]interface{}{"key_id": "key-1", "policy_version": 4, "timestamp": kmsStamp.Unix()},
			})
			defer cleanup()

			err := kmsclient.NewClient(tr).SetKeyPolicy(context.Background(),
				&kmsclient.SetKeyPolicyRequest{KeyID: "key-1", Policy: tc.policy})
			if err != nil {
				t.Fatalf("SetKeyPolicy: %v", err)
			}
		})
	}
}

func TestKMSMCP_GetKeyPolicy(t *testing.T) {
	expires := kmsStamp.Add(24 * time.Hour)
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_get_policy",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if !reflect.DeepEqual(args, map[string]interface{}{"key_id": "key-1"}) {
				t.Errorf("tool arguments = %v", args)
			}
		},
		result: map[string]interface{}{
			"policy": map[string]interface{}{
				"key_id": "key-1", "version": 2,
				"allowed_operations": []interface{}{"sign", "verify", "wrap_key"},
				"allowed_principals": []interface{}{"svc-a"}, "denied_principals": []interface{}{"svc-b"},
				"expires_at": expires.Format(time.RFC3339Nano), "rotation_required": true,
				"max_usage_count": 100, "require_mfa": true, "updated_at": kmsStamp.Unix(),
			},
			"timestamp": kmsStamp.Unix(),
		},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).GetKeyPolicy(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("GetKeyPolicy: %v", err)
	}
	want := &kmsclient.KeyPolicy{
		KeyID: "key-1", Version: 2,
		AllowedOperations: []kmsclient.KeyUsage{kmsclient.KeyUsageSign, kmsclient.KeyUsageVerify, kmsclient.KeyUsageWrap},
		AllowedPrincipals: []string{"svc-a"}, DeniedPrincipals: []string{"svc-b"},
		ExpiresAt: expires, RotationRequired: true, MaxUsageCount: 100, RequireMFA: true,
		UpdatedAt: kmsStamp,
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
}

// kmsUnrestrictedPolicyResult is the policy object the server returns for a
// key with no principal restrictions, expiry or usage limit.
func kmsUnrestrictedPolicyResult() map[string]interface{} {
	return map[string]interface{}{
		"key_id": "key-1", "version": 1,
		"allowed_operations": []interface{}{"encrypt"},
		"allowed_principals": []interface{}{}, "denied_principals": []interface{}{},
		"expires_at": nil, "rotation_required": false,
		"max_usage_count": nil, "require_mfa": false, "updated_at": kmsStamp.Unix(),
	}
}

func TestKMSMCP_GetKeyPolicy_Unrestricted(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_get_policy",
		result:   map[string]interface{}{"policy": kmsUnrestrictedPolicyResult(), "timestamp": kmsStamp.Unix()},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).GetKeyPolicy(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("GetKeyPolicy: %v", err)
	}
	want := &kmsclient.KeyPolicy{
		KeyID: "key-1", Version: 1,
		AllowedOperations: []kmsclient.KeyUsage{kmsclient.KeyUsageEncrypt},
		AllowedPrincipals: []string{}, DeniedPrincipals: []string{},
		UpdatedAt: kmsStamp,
	}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
}

// TestKMSMCP_GetKeyPolicy_IncompleteResult proves the decoder refuses a
// policy that omits or garbles a restriction rather than reading it as
// unrestricted: a read-modify-write caller would otherwise erase it.
func TestKMSMCP_GetKeyPolicy_IncompleteResult(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(policy map[string]interface{})
	}{
		{"no allowed_principals", func(p map[string]interface{}) { delete(p, "allowed_principals") }},
		{"no denied_principals", func(p map[string]interface{}) { delete(p, "denied_principals") }},
		{"no expires_at", func(p map[string]interface{}) { delete(p, "expires_at") }},
		{"no max_usage_count", func(p map[string]interface{}) { delete(p, "max_usage_count") }},
		{"principal not a string", func(p map[string]interface{}) { p["denied_principals"] = []interface{}{7} }},
		{"principals not a list", func(p map[string]interface{}) { p["allowed_principals"] = "svc-a" }},
		{"expires_at not RFC 3339", func(p map[string]interface{}) { p["expires_at"] = "tomorrow" }},
		{"expires_at not a string", func(p map[string]interface{}) { p["expires_at"] = kmsStamp.Unix() }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy := kmsUnrestrictedPolicyResult()
			tc.mutate(policy)
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: "quicbaas_kms_get_policy",
				result:   map[string]interface{}{"policy": policy, "timestamp": kmsStamp.Unix()},
			})
			defer cleanup()

			if _, err := kmsclient.NewClient(tr).GetKeyPolicy(context.Background(), "key-1"); sdkerrors.Code(err) != sdkerrors.CodeInternal {
				t.Errorf("err = %v, want Internal", err)
			}
		})
	}
}

func TestKMSMCP_SetKeyRotationPolicy(t *testing.T) {
	for _, tc := range []struct {
		name string
		req  *kmsclient.SetKeyRotationPolicyRequest
		want map[string]interface{}
	}{
		{
			name: "every field",
			req: &kmsclient.SetKeyRotationPolicyRequest{
				KeyID: "key-1", AutoRotate: true, IntervalSeconds: 86400, RetainVersions: 3, Enabled: true,
			},
			want: map[string]interface{}{
				"key_id": "key-1", "auto_rotate": true, "interval_seconds": float64(86400),
				"retain_versions": float64(3), "enabled": true,
			},
		},
		{
			name: "required fields only",
			req:  &kmsclient.SetKeyRotationPolicyRequest{KeyID: "key-1"},
			want: map[string]interface{}{"key_id": "key-1", "auto_rotate": false, "interval_seconds": float64(0)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: "quicbaas_kms_set_rotation_policy",
				assertArgs: func(t *testing.T, args map[string]interface{}) {
					if !reflect.DeepEqual(args, tc.want) {
						t.Errorf("tool arguments = %v, want %v", args, tc.want)
					}
				},
				result: map[string]interface{}{
					"key_id": "key-1",
					"policy": map[string]interface{}{
						"enabled": true, "auto_rotate": true, "interval_seconds": 86400,
						"next_rotation_unix": kmsStamp.Unix(), "retain_versions": 3,
					},
					"timestamp": kmsStamp.Unix(),
				},
			})
			defer cleanup()

			resp, err := kmsclient.NewClient(tr).SetKeyRotationPolicy(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("SetKeyRotationPolicy: %v", err)
			}
			want := &kmsclient.KeyRotationPolicy{
				Enabled: true, AutoRotate: true, IntervalSeconds: 86400,
				NextRotationAt: kmsStamp, RetainVersions: 3,
			}
			if !reflect.DeepEqual(resp, want) {
				t.Errorf("response = %+v, want %+v", resp, want)
			}
		})
	}
}

func TestKMSMCP_GetKeyRotationPolicy(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_get_rotation_policy",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if !reflect.DeepEqual(args, map[string]interface{}{"key_id": "key-1"}) {
				t.Errorf("tool arguments = %v", args)
			}
		},
		result: map[string]interface{}{
			"key_id": "key-1",
			"policy": map[string]interface{}{
				"enabled": false, "auto_rotate": false, "interval_seconds": 3600,
				"next_rotation_unix": time.Time{}.Unix(), "retain_versions": 0,
			},
			"timestamp": kmsStamp.Unix(),
		},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).GetKeyRotationPolicy(context.Background(), "key-1")
	if err != nil {
		t.Fatalf("GetKeyRotationPolicy: %v", err)
	}
	want := &kmsclient.KeyRotationPolicy{IntervalSeconds: 3600}
	if !reflect.DeepEqual(resp, want) {
		t.Errorf("response = %+v, want %+v", resp, want)
	}
}

// TestKMSMCP_GetKeyRotationPolicy_NoPolicy covers the tool's explicit null
// policy, which the client reports as no policy configured.
func TestKMSMCP_GetKeyRotationPolicy_NoPolicy(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_get_rotation_policy",
		result:   map[string]interface{}{"key_id": "key-1", "policy": nil, "timestamp": kmsStamp.Unix()},
	})
	defer cleanup()

	resp, err := kmsclient.NewClient(tr).GetKeyRotationPolicy(context.Background(), "key-1")
	if err != nil || resp != nil {
		t.Errorf("GetKeyRotationPolicy = %+v, %v, want nil, nil", resp, err)
	}
}

func TestKMSMCP_DeleteKeyRotationPolicy(t *testing.T) {
	tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
		wantTool: "quicbaas_kms_delete_rotation_policy",
		assertArgs: func(t *testing.T, args map[string]interface{}) {
			if !reflect.DeepEqual(args, map[string]interface{}{"key_id": "key-1"}) {
				t.Errorf("tool arguments = %v", args)
			}
		},
		result: map[string]interface{}{"key_id": "key-1", "timestamp": kmsStamp.Unix()},
	})
	defer cleanup()

	if err := kmsclient.NewClient(tr).DeleteKeyRotationPolicy(context.Background(), "key-1"); err != nil {
		t.Fatalf("DeleteKeyRotationPolicy: %v", err)
	}
}

// kmsPolicyCall runs one policy verb through the KMS client.
type kmsPolicyCall func(c kmsclient.Client) error

var kmsPolicyCalls = map[string]kmsPolicyCall{
	"SetKeyPolicy": func(c kmsclient.Client) error {
		return c.SetKeyPolicy(context.Background(), &kmsclient.SetKeyPolicyRequest{
			KeyID: "key-1", Policy: &kmsclient.KeyPolicy{AllowedOperations: []kmsclient.KeyUsage{kmsclient.KeyUsageSign}},
		})
	},
	"GetKeyPolicy": func(c kmsclient.Client) error {
		_, err := c.GetKeyPolicy(context.Background(), "key-1")
		return err
	},
	"SetKeyRotationPolicy": func(c kmsclient.Client) error {
		_, err := c.SetKeyRotationPolicy(context.Background(), &kmsclient.SetKeyRotationPolicyRequest{KeyID: "key-1"})
		return err
	},
	"GetKeyRotationPolicy": func(c kmsclient.Client) error {
		_, err := c.GetKeyRotationPolicy(context.Background(), "key-1")
		return err
	},
	"DeleteKeyRotationPolicy": func(c kmsclient.Client) error {
		return c.DeleteKeyRotationPolicy(context.Background(), "key-1")
	},
}

// TestKMSMCP_PolicyMissingResultField proves a result missing a field that
// identifies it or carries its meaning is a typed internal error, never a
// zero value.
func TestKMSMCP_PolicyMissingResultField(t *testing.T) {
	rotation := func(drop string) map[string]interface{} {
		policy := map[string]interface{}{"enabled": true, "auto_rotate": true, "interval_seconds": 60}
		delete(policy, drop)
		return map[string]interface{}{"key_id": "key-1", "policy": policy}
	}
	policy := func(p map[string]interface{}) map[string]interface{} {
		return map[string]interface{}{"policy": p}
	}
	for _, tc := range []struct {
		name   string
		method string
		result map[string]interface{}
	}{
		{"set policy key_id", "SetKeyPolicy", map[string]interface{}{"policy_version": 1}},
		{"get policy body", "GetKeyPolicy", map[string]interface{}{"timestamp": kmsStamp.Unix()}},
		{"get policy key_id", "GetKeyPolicy", policy(map[string]interface{}{"allowed_operations": []interface{}{"sign"}})},
		{"get policy operations", "GetKeyPolicy", policy(map[string]interface{}{"key_id": "key-1"})},
		{"get policy unknown operation", "GetKeyPolicy",
			policy(map[string]interface{}{"key_id": "key-1", "allowed_operations": []interface{}{"derive_key"}})},
		{"set rotation key_id", "SetKeyRotationPolicy", map[string]interface{}{"policy": map[string]interface{}{}}},
		{"set rotation null policy", "SetKeyRotationPolicy", map[string]interface{}{"key_id": "key-1", "policy": nil}},
		{"set rotation enabled", "SetKeyRotationPolicy", rotation("enabled")},
		{"set rotation interval", "SetKeyRotationPolicy", rotation("interval_seconds")},
		{"get rotation policy", "GetKeyRotationPolicy", map[string]interface{}{"key_id": "key-1"}},
		{"get rotation not an object", "GetKeyRotationPolicy", map[string]interface{}{"key_id": "key-1", "policy": "on"}},
		{"get rotation auto_rotate", "GetKeyRotationPolicy", rotation("auto_rotate")},
		{"delete rotation key_id", "DeleteKeyRotationPolicy", map[string]interface{}{"timestamp": kmsStamp.Unix()}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{wantTool: kmsVerbs[tc.method].tool, result: tc.result})
			defer cleanup()

			if err := kmsPolicyCalls[tc.method](kmsclient.NewClient(tr)); sdkerrors.Code(err) != sdkerrors.CodeInternal {
				t.Errorf("err = %v, want a typed internal error", err)
			}
		})
	}
}

func TestKMSMCP_PolicyToolError(t *testing.T) {
	for method, call := range kmsPolicyCalls {
		t.Run(method, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool:      kmsVerbs[method].tool,
				result:        map[string]interface{}{"error": "key not found"},
				toolError:     true,
				toolErrorCode: MCPResourceNotFound,
			})
			defer cleanup()

			if err := call(kmsclient.NewClient(tr)); !sdkerrors.IsNotFound(err) {
				t.Errorf("err = %v, want NotFound", err)
			}
		})
	}
}

func TestKMSMCP_PolicyBadRequest(t *testing.T) {
	for _, tc := range []struct {
		name   string
		method string
		req    interface{}
	}{
		{"set policy wrong type", "SetKeyPolicy", &kmsclient.UpdateKeyStatusRequest{KeyID: "key-1"}},
		{"set policy no key_id", "SetKeyPolicy", &kmsclient.SetKeyPolicyRequest{Policy: &kmsclient.KeyPolicy{}}},
		{"set policy no policy", "SetKeyPolicy", &kmsclient.SetKeyPolicyRequest{KeyID: "key-1"}},
		{"set policy unknown usage", "SetKeyPolicy", &kmsclient.SetKeyPolicyRequest{
			KeyID: "key-1", Policy: &kmsclient.KeyPolicy{AllowedOperations: []kmsclient.KeyUsage{kmsclient.KeyUsage(99)}},
		}},
		{"get policy no key_id", "GetKeyPolicy", &kmsclient.UpdateKeyStatusRequest{Version: 1}},
		{"set rotation no key_id", "SetKeyRotationPolicy",
			map[string]interface{}{"auto_rotate": true, "interval_seconds": 60}},
		{"set rotation no auto_rotate", "SetKeyRotationPolicy",
			map[string]interface{}{"key_id": "key-1", "interval_seconds": 60}},
		{"set rotation no interval", "SetKeyRotationPolicy",
			map[string]interface{}{"key_id": "key-1", "auto_rotate": false}},
		{"set rotation not json", "SetKeyRotationPolicy", make(chan int)},
		{"get rotation no key_id", "GetKeyRotationPolicy", nil},
		{"delete rotation no key_id", "DeleteKeyRotationPolicy", &kmsclient.SetKeyPolicyRequest{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool:   kmsVerbs[tc.method].tool,
				assertArgs: refuseKMSCall,
			})
			defer cleanup()

			if _, err := tr.Request(context.Background(), kmsServiceName, tc.method, tc.req); !sdkerrors.IsInvalidInput(err) {
				t.Errorf("err = %v, want InvalidInput", err)
			}
		})
	}
}

func TestKMSMCP_RequireResultFields(t *testing.T) {
	check := kmsVerbs["RotateKey"].check
	if err := check("mcp.kms.RotateKey", map[string]interface{}{"key_id": "key-1", "new_version": float64(2)}); err != nil {
		t.Fatalf("complete result: err = %v, want nil", err)
	}
	for name, result := range map[string]map[string]interface{}{
		"no key_id":        {"new_version": float64(2)},
		"empty key_id":     {"key_id": "", "new_version": float64(2)},
		"no new_version":   {"key_id": "key-1"},
		"zero new_version": {"key_id": "key-1", "new_version": float64(0)},
		"null new_version": {"key_id": "key-1", "new_version": nil},
		"no fields at all": {},
	} {
		t.Run(name, func(t *testing.T) {
			err := check("mcp.kms.RotateKey", result)
			if sdkerrors.Code(err) != sdkerrors.CodeInternal {
				t.Errorf("err = %v, want Internal", err)
			}
		})
	}
}

// kmsExportResult is the quicbaas_kms_export_key result for key-1 wrapped
// by wrap-1, with the given wrapping algorithm and format.
func kmsExportResult(wrappingAlgorithm, format string) map[string]interface{} {
	return map[string]interface{}{
		"key_id":             "key-1",
		"version":            3,
		"wrapped_key":        base64.StdEncoding.EncodeToString([]byte("wrapped-bytes")),
		"wrapping_key_id":    "wrap-1",
		"wrapping_algorithm": wrappingAlgorithm,
		"format":             format,
		"timestamp":          kmsStamp.Unix(),
	}
}

func TestKMSMCP_ExportKey(t *testing.T) {
	for _, tc := range []struct {
		name       string
		req        *kmsclient.ExportKeyRequest
		wrapWire   string
		formatWire string
	}{
		{"zero enums, latest version",
			&kmsclient.ExportKeyRequest{KeyID: "key-1", WrappingKeyID: "wrap-1"}, "AES-KW", "raw"},
		{"aes-kwp jwk", &kmsclient.ExportKeyRequest{KeyID: "key-1", WrappingKeyID: "wrap-1",
			WrappingAlgorithm: kmsclient.WrapAlgAESKWP, Format: kmsclient.ExportFormatJWK, Version: 3}, "AES-KWP", "jwk"},
		{"aes-gcm pem", &kmsclient.ExportKeyRequest{KeyID: "key-1", WrappingKeyID: "wrap-1",
			WrappingAlgorithm: kmsclient.WrapAlgAESGCM, Format: kmsclient.ExportFormatPEM, Version: 3}, "AES-GCM", "pem"},
		{"rsa-oaep-sha256", &kmsclient.ExportKeyRequest{KeyID: "key-1", WrappingKeyID: "wrap-1",
			WrappingAlgorithm: kmsclient.WrapAlgRSAOAEPSHA256, Version: 3}, "RSA-OAEP-SHA256", "raw"},
		{"rsa-oaep-sha384", &kmsclient.ExportKeyRequest{KeyID: "key-1", WrappingKeyID: "wrap-1",
			WrappingAlgorithm: kmsclient.WrapAlgRSAOAEPSHA384, Version: 3}, "RSA-OAEP-SHA384", "raw"},
		{"rsa-oaep-sha512", &kmsclient.ExportKeyRequest{KeyID: "key-1", WrappingKeyID: "wrap-1",
			WrappingAlgorithm: kmsclient.WrapAlgRSAOAEPSHA512, Version: 3}, "RSA-OAEP-SHA512", "raw"},
		{"ecdh-es", &kmsclient.ExportKeyRequest{KeyID: "key-1", WrappingKeyID: "wrap-1",
			WrappingAlgorithm: kmsclient.WrapAlgECDHESA256KW, Version: 3}, "ECDH-ES+A256KW", "raw"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: "quicbaas_kms_export_key",
				assertArgs: func(t *testing.T, args map[string]interface{}) {
					want := map[string]interface{}{
						"key_id":             "key-1",
						"wrapping_key_id":    "wrap-1",
						"wrapping_algorithm": tc.wrapWire,
						"format":             tc.formatWire,
					}
					if tc.req.Version > 0 {
						want["version"] = float64(tc.req.Version)
					}
					if !reflect.DeepEqual(args, want) {
						t.Errorf("tool arguments = %v, want %v", args, want)
					}
				},
				result: kmsExportResult(tc.wrapWire, tc.formatWire),
			})
			defer cleanup()

			resp, err := kmsclient.NewClient(tr).ExportKey(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("ExportKey: %v", err)
			}
			want := &kmsclient.ExportKeyResponse{
				KeyID:             "key-1",
				Format:            tc.req.Format,
				WrappingKeyID:     "wrap-1",
				WrappingAlgorithm: tc.req.WrappingAlgorithm,
				WrappedMaterial:   []byte("wrapped-bytes"),
				Version:           3,
			}
			if !reflect.DeepEqual(resp, want) {
				t.Errorf("response = %+v, want %+v", resp, want)
			}
		})
	}
}

func TestKMSMCP_UnwrapKey(t *testing.T) {
	wrapped := []byte("wrapped-bytes")
	for _, tc := range []struct {
		name     string
		req      *kmsclient.UnwrapKeyRequest
		wantArgs map[string]interface{}
		result   map[string]interface{}
		want     *kmsclient.UnwrapKeyResponse
	}{
		{
			name: "required fields only",
			req: &kmsclient.UnwrapKeyRequest{KeyID: "key-2", WrappingKeyID: "wrap-1",
				WrappedMaterial: wrapped, Algorithm: kmsclient.AlgorithmAES256GCM},
			wantArgs: map[string]interface{}{
				"key_id": "key-2", "wrapping_key_id": "wrap-1", "wrapping_algorithm": "AES-KW",
				"wrapped_key": base64.StdEncoding.EncodeToString(wrapped), "algorithm": "AES-256-GCM",
			},
			result: map[string]interface{}{
				"key_id": "key-2", "version": 1, "algorithm": "AES-256-GCM", "status": "enabled",
				"timestamp": kmsStamp.Unix(),
			},
			want: &kmsclient.UnwrapKeyResponse{KeyID: "key-2", Version: 1, Algorithm: kmsclient.AlgorithmAES256GCM},
		},
		{
			name: "every optional field",
			req: &kmsclient.UnwrapKeyRequest{KeyID: "key-2", WrappingKeyID: "wrap-1",
				WrappingAlgorithm: kmsclient.WrapAlgRSAOAEPSHA256, WrappedMaterial: wrapped,
				Algorithm: kmsclient.AlgorithmED25519, Name: "imported", SourceKeyID: "key-1", SourceVersion: 3},
			wantArgs: map[string]interface{}{
				"key_id": "key-2", "wrapping_key_id": "wrap-1", "wrapping_algorithm": "RSA-OAEP-SHA256",
				"wrapped_key": base64.StdEncoding.EncodeToString(wrapped), "algorithm": "ED25519",
				"name": "imported", "source_key_id": "key-1", "source_version": float64(3),
			},
			result: map[string]interface{}{
				"key_id": "key-2", "version": 4, "algorithm": "ED25519", "status": "enabled",
				"timestamp": kmsStamp.Unix(),
				"key": map[string]interface{}{"key_id": "key-2", "version": 4, "algorithm": "ED25519",
					"status": "enabled", "created_at": kmsStamp.Unix()},
			},
			want: &kmsclient.UnwrapKeyResponse{KeyID: "key-2", Version: 4, Algorithm: kmsclient.AlgorithmED25519},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool: "quicbaas_kms_unwrap_key",
				assertArgs: func(t *testing.T, args map[string]interface{}) {
					if !reflect.DeepEqual(args, tc.wantArgs) {
						t.Errorf("tool arguments = %v, want %v", args, tc.wantArgs)
					}
				},
				result: tc.result,
			})
			defer cleanup()

			resp, err := kmsclient.NewClient(tr).UnwrapKey(context.Background(), tc.req)
			if err != nil {
				t.Fatalf("UnwrapKey: %v", err)
			}
			if !reflect.DeepEqual(resp, tc.want) {
				t.Errorf("response = %+v, want %+v", resp, tc.want)
			}
		})
	}
}

func TestKMSMCP_ExportUnwrapMalformedResult(t *testing.T) {
	unwrapReq := &kmsclient.UnwrapKeyRequest{KeyID: "key-2", WrappingKeyID: "wrap-1",
		WrappedMaterial: []byte("wrapped-bytes"), Algorithm: kmsclient.AlgorithmED25519}
	withField := func(m map[string]interface{}, field string, value interface{}) map[string]interface{} {
		if value == nil {
			delete(m, field)
		} else {
			m[field] = value
		}
		return m
	}
	unwrapResult := func() map[string]interface{} {
		return map[string]interface{}{"key_id": "key-2", "version": 1, "algorithm": "ED25519", "status": "enabled"}
	}
	for _, tc := range []struct {
		name   string
		method string
		result map[string]interface{}
	}{
		{"export no wrapped_key", "ExportKey", withField(kmsExportResult("AES-KW", "raw"), "wrapped_key", nil)},
		{"export empty wrapped_key", "ExportKey", withField(kmsExportResult("AES-KW", "raw"), "wrapped_key", "")},
		{"export malformed wrapped_key", "ExportKey", withField(kmsExportResult("AES-KW", "raw"), "wrapped_key", "%%%")},
		{"export no key_id", "ExportKey", withField(kmsExportResult("AES-KW", "raw"), "key_id", nil)},
		{"export unknown format", "ExportKey", kmsExportResult("AES-KW", "der")},
		{"export unknown wrapping algorithm", "ExportKey", kmsExportResult("ROT13", "raw")},
		{"unwrap no key_id", "UnwrapKey", withField(unwrapResult(), "key_id", nil)},
		{"unwrap no algorithm", "UnwrapKey", withField(unwrapResult(), "algorithm", nil)},
		{"unwrap unknown algorithm", "UnwrapKey", withField(unwrapResult(), "algorithm", "ED448")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{wantTool: kmsVerbs[tc.method].tool, result: tc.result})
			defer cleanup()

			client := kmsclient.NewClient(tr)
			var err error
			if tc.method == "ExportKey" {
				_, err = client.ExportKey(context.Background(),
					&kmsclient.ExportKeyRequest{KeyID: "key-1", WrappingKeyID: "wrap-1"})
			} else {
				_, err = client.UnwrapKey(context.Background(), unwrapReq)
			}
			if sdkerrors.Code(err) != sdkerrors.CodeInternal {
				t.Errorf("err = %v, want a typed internal error", err)
			}
		})
	}
}

func TestKMSMCP_ExportUnwrapToolError(t *testing.T) {
	for _, method := range []string{"ExportKey", "UnwrapKey"} {
		t.Run(method, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool:      kmsVerbs[method].tool,
				result:        map[string]interface{}{"error": "key not found"},
				toolError:     true,
				toolErrorCode: MCPResourceNotFound,
			})
			defer cleanup()

			client := kmsclient.NewClient(tr)
			var err error
			if method == "ExportKey" {
				_, err = client.ExportKey(context.Background(),
					&kmsclient.ExportKeyRequest{KeyID: "missing", WrappingKeyID: "wrap-1"})
			} else {
				_, err = client.UnwrapKey(context.Background(), &kmsclient.UnwrapKeyRequest{KeyID: "key-2",
					WrappingKeyID: "missing", WrappedMaterial: []byte("wrapped-bytes")})
			}
			if !sdkerrors.IsNotFound(err) {
				t.Errorf("err = %v, want NotFound", err)
			}
		})
	}
}

func TestKMSMCP_ExportUnwrapBadRequest(t *testing.T) {
	export := func(mutate func(*kmsclient.ExportKeyRequest)) *kmsclient.ExportKeyRequest {
		r := &kmsclient.ExportKeyRequest{KeyID: "key-1", WrappingKeyID: "wrap-1"}
		mutate(r)
		return r
	}
	unwrap := func(mutate func(*kmsclient.UnwrapKeyRequest)) *kmsclient.UnwrapKeyRequest {
		r := &kmsclient.UnwrapKeyRequest{KeyID: "key-2", WrappingKeyID: "wrap-1", WrappedMaterial: []byte("w")}
		mutate(r)
		return r
	}
	for _, tc := range []struct {
		name   string
		method string
		req    interface{}
	}{
		{"export wrong type", "ExportKey", &kmsclient.GenerateKeyRequest{Name: "key-1"}},
		{"export nil", "ExportKey", (*kmsclient.ExportKeyRequest)(nil)},
		{"export no key_id", "ExportKey", export(func(r *kmsclient.ExportKeyRequest) { r.KeyID = "" })},
		{"export no wrapping_key_id", "ExportKey", export(func(r *kmsclient.ExportKeyRequest) { r.WrappingKeyID = "" })},
		{"export negative version", "ExportKey", export(func(r *kmsclient.ExportKeyRequest) { r.Version = -1 })},
		{"export unknown wrapping algorithm", "ExportKey",
			export(func(r *kmsclient.ExportKeyRequest) { r.WrappingAlgorithm = kmsclient.WrappingAlgorithm(99) })},
		{"export unknown format", "ExportKey",
			export(func(r *kmsclient.ExportKeyRequest) { r.Format = kmsclient.ExportFormat(99) })},
		{"unwrap wrong type", "UnwrapKey", &kmsclient.ExportKeyRequest{KeyID: "key-1"}},
		{"unwrap nil", "UnwrapKey", (*kmsclient.UnwrapKeyRequest)(nil)},
		{"unwrap no key_id", "UnwrapKey", unwrap(func(r *kmsclient.UnwrapKeyRequest) { r.KeyID = "" })},
		{"unwrap no wrapping_key_id", "UnwrapKey", unwrap(func(r *kmsclient.UnwrapKeyRequest) { r.WrappingKeyID = "" })},
		{"unwrap no wrapped material", "UnwrapKey", unwrap(func(r *kmsclient.UnwrapKeyRequest) { r.WrappedMaterial = nil })},
		{"unwrap negative source version", "UnwrapKey",
			unwrap(func(r *kmsclient.UnwrapKeyRequest) { r.SourceVersion = -1 })},
		{"unwrap unknown algorithm", "UnwrapKey",
			unwrap(func(r *kmsclient.UnwrapKeyRequest) { r.Algorithm = kmsclient.KeyAlgorithm(99) })},
		{"unwrap unknown wrapping algorithm", "UnwrapKey",
			unwrap(func(r *kmsclient.UnwrapKeyRequest) { r.WrappingAlgorithm = kmsclient.WrappingAlgorithm(99) })},
		{"unwrap description", "UnwrapKey",
			unwrap(func(r *kmsclient.UnwrapKeyRequest) { r.Description = "cannot be carried" })},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tr, cleanup := newFakeMCPServer(t, mcpToolExpectation{
				wantTool:   kmsVerbs[tc.method].tool,
				assertArgs: refuseKMSCall,
			})
			defer cleanup()

			if _, err := tr.Request(context.Background(), kmsServiceName, tc.method, tc.req); !sdkerrors.IsInvalidInput(err) {
				t.Errorf("err = %v, want InvalidInput", err)
			}
		})
	}
}
