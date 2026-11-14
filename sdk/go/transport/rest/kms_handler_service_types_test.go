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
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	kctypes "github.com/jeremyhahn/go-xkms/sdk/go"
)

func writeKMSTestJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func kmsTestGenerateKeyWire() kmsGenerateKeyWire {
	return kmsGenerateKeyWire{
		Key:       &kmsKeyHandleWire{KeyID: "key-1", Version: 1, Algorithm: "AES-256-GCM", Status: "enabled"},
		Metadata:  &kmsKeyMetadataWire{KeyID: "key-1", Algorithm: "AES-256-GCM", Version: 1},
		Timestamp: 1700000000,
	}
}

func checkKMSTestRoute(t *testing.T, r *http.Request, method, path string) {
	t.Helper()
	if r.Method != method {
		t.Errorf("expected %s, got %s", method, r.Method)
	}
	if r.URL.Path != path {
		t.Errorf("expected %s, got %s", path, r.URL.Path)
	}
}

func TestKMSRESTHandler_ServiceGenerateKey_ReachesServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkKMSTestRoute(t, r, http.MethodPost, "/api/v1/kms/keys")
		var body kmsGenerateKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body.Name != "k1" {
			t.Errorf("unexpected name: %q", body.Name)
		}
		if body.KeyAttributes == nil {
			t.Errorf("key attributes missing")
		} else {
			if body.KeyAttributes.Algorithm != "AES-256-GCM" {
				t.Errorf("unexpected algorithm: %q", body.KeyAttributes.Algorithm)
			}
			if !reflect.DeepEqual(body.KeyAttributes.KeyUsage, []string{"encrypt"}) {
				t.Errorf("unexpected usage: %v", body.KeyAttributes.KeyUsage)
			}
			if !body.KeyAttributes.Exportable {
				t.Errorf("expected exportable")
			}
		}
		if body.Tags["a"] != "b" {
			t.Errorf("unexpected tags: %v", body.Tags)
		}
		writeKMSTestJSON(w, http.StatusCreated, kmsTestGenerateKeyWire())
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "GenerateKey", &service.GenerateKeyRequest{
		Name:          "k1",
		KeyAttributes: &kctypes.KeyAttributes{SymmetricAlgorithm: kctypes.SymmetricAES256GCM},
		Usage:         []service.KeyUsage{service.KeyUsageEncrypt},
		Exportable:    true,
		Tags:          map[string]string{"a": "b"},
	})
	if err != nil {
		t.Fatalf("GenerateKey failed: %v", err)
	}
	resp, ok := result.(*service.GenerateKeyResponse)
	if !ok {
		t.Fatalf("expected *service.GenerateKeyResponse, got %T", result)
	}
	if resp.Metadata == nil || resp.Metadata.KeyID != "key-1" {
		t.Errorf("unexpected metadata: %+v", resp.Metadata)
	}
}

func TestKMSRESTHandler_ServiceImportKey_CarriesExportable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkKMSTestRoute(t, r, http.MethodPost, "/api/v1/kms/keys/import")
		var body kmsImportKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body.WrappingKeyID != "w1" {
			t.Errorf("unexpected wrapping key: %q", body.WrappingKeyID)
		}
		if body.KeyAttributes == nil || !body.KeyAttributes.Exportable || !body.KeyAttributes.DeletionProtection {
			t.Errorf("unexpected key attributes: %+v", body.KeyAttributes)
		}
		writeKMSTestJSON(w, http.StatusCreated, kmsTestGenerateKeyWire())
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "ImportKey", &service.ImportKeyRequest{
		KeyMaterial:        []byte{1},
		WrappingKeyID:      "w1",
		Name:               "imp",
		KeyAttributes:      &kctypes.KeyAttributes{SymmetricAlgorithm: kctypes.SymmetricAES256GCM, Exportable: true},
		DeletionProtection: true,
	})
	if err != nil {
		t.Fatalf("ImportKey failed: %v", err)
	}
	if _, ok := result.(*service.ImportKeyResponse); !ok {
		t.Fatalf("expected *service.ImportKeyResponse, got %T", result)
	}
}

func TestKMSRESTHandler_ServiceExportKey_ReachesServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkKMSTestRoute(t, r, http.MethodPost, "/api/v1/kms/keys/k1/export")
		var body service.ExportKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body.WrappingAlgorithm != "AES-KWP" || body.Version != 2 {
			t.Errorf("unexpected body: %+v", body)
		}
		writeKMSTestJSON(w, http.StatusOK, kmsExportKeyWire{
			KeyID:             "k1",
			Version:           2,
			WrappedKey:        []byte{7, 7},
			WrappingKeyID:     "w1",
			WrappingAlgorithm: "AES-KWP",
			Format:            "raw",
			Timestamp:         1700000000,
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "ExportKey", &service.ExportKeyRequest{
		KeyID:             "k1",
		WrappingKeyID:     "w1",
		WrappingAlgorithm: "AES-KWP",
		Version:           2,
	})
	if err != nil {
		t.Fatalf("ExportKey failed: %v", err)
	}
	resp, ok := result.(*service.ExportKeyResponse)
	if !ok {
		t.Fatalf("expected *service.ExportKeyResponse, got %T", result)
	}
	if !reflect.DeepEqual(resp.WrappedKey, []byte{7, 7}) {
		t.Errorf("unexpected wrapped key: %v", resp.WrappedKey)
	}
}

func TestKMSRESTHandler_ServiceUnwrapKey_ReachesServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkKMSTestRoute(t, r, http.MethodPost, "/api/v1/kms/keys/unwrap")
		var body service.UnwrapKeyRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body.Name != "u1" || !reflect.DeepEqual(body.WrappedKey, []byte{9}) {
			t.Errorf("unexpected body: %+v", body)
		}
		writeKMSTestJSON(w, http.StatusOK, kmsUnwrapKeyWire{
			KeyID:     "u-key",
			Version:   1,
			Algorithm: "AES-256-GCM",
			Status:    "enabled",
			Timestamp: 1700000000,
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "UnwrapKey", &service.UnwrapKeyRequest{
		WrappedKey:        []byte{9},
		WrappingKeyID:     "w1",
		WrappingAlgorithm: "AES-KWP",
		Algorithm:         "AES-256-GCM",
		Name:              "u1",
	})
	if err != nil {
		t.Fatalf("UnwrapKey failed: %v", err)
	}
	resp, ok := result.(*service.UnwrapKeyResponse)
	if !ok {
		t.Fatalf("expected *service.UnwrapKeyResponse, got %T", result)
	}
	if resp.KeyID != "u-key" {
		t.Errorf("unexpected key ID: %q", resp.KeyID)
	}
}

func TestKMSRESTHandler_ServiceUpdateKeyStatus_ReachesServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkKMSTestRoute(t, r, http.MethodPut, "/api/v1/kms/keys/k1/status")
		var body kmsUpdateKeyStatusRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode: %v", err)
		}
		if body.Status != string(service.KeyStatusDisabled) {
			t.Errorf("unexpected status: %q", body.Status)
		}
		writeKMSTestJSON(w, http.StatusOK, kmsUpdateKeyStatusWire{
			KeyID:     "k1",
			Status:    string(service.KeyStatusDisabled),
			Timestamp: 1700000000,
		})
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	result, err := tr.Request(context.Background(), "quicbaas.KMS", "UpdateKeyStatus", &service.UpdateKeyStatusRequest{
		KeyID:  "k1",
		Status: service.KeyStatusDisabled,
	})
	if err != nil {
		t.Fatalf("UpdateKeyStatus failed: %v", err)
	}
	resp, ok := result.(*service.UpdateKeyStatusResponse)
	if !ok {
		t.Fatalf("expected *service.UpdateKeyStatusResponse, got %T", result)
	}
	if resp.Status != service.KeyStatusDisabled {
		t.Errorf("unexpected status: %q", resp.Status)
	}
}

func TestKMSRESTHandler_ServiceExportKey_EmptyKeyIDRefused(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	defer tr.Close()

	if _, err := tr.Request(context.Background(), "quicbaas.KMS", "ExportKey", &service.ExportKeyRequest{}); err == nil {
		t.Fatal("expected an error for an empty key ID")
	}
}
