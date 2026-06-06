// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
)

// dekPathReq is a minimal request carrying an exported Path field, matching the
// reflective extractStringField contract used by the Get/Status/SetDefault/
// ClearSafety DEK handlers.
type dekPathReq struct {
	Path string
}

// TestDEKHandlerMethodsHappyPaths drives the DEK methods that lacked coverage,
// asserting the HTTP method and path each builds.
func TestDEKHandlerMethodsHappyPaths(t *testing.T) {
	var gotMethod, gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	cases := []struct {
		name       string
		method     string
		req        interface{}
		wantMethod string
		wantPath   string
	}{
		{"ListDEKs", "ListDEKs",
			&dekclient.ListDEKsRequest{Prefix: "app/", Recursive: true},
			http.MethodGet, "/api/v1/kms/deks"},
		{"RotateDEK", "RotateDEK",
			&dekclient.RotateDEKRequest{Path: "app/key", Background: true},
			http.MethodPost, "/api/v1/kms/deks/app/key/rotate"},
		{"GetDEKStatus", "GetDEKStatus",
			&dekPathReq{Path: "app/key"},
			http.MethodGet, "/api/v1/kms/deks/app/key/status"},
		{"SetDefaultDEK", "SetDefaultDEK",
			&dekPathReq{Path: "app/key"},
			http.MethodPost, "/api/v1/kms/deks/app/key/set-default"},
		{"SetDEKSafety", "SetDEKSafety",
			&dekclient.SetDEKSafetyRequest{Path: "app/key", MaxOperations: 100, MaxBytes: 1024, WarningPercent: 80},
			http.MethodPut, "/api/v1/kms/deks/app/key/safety"},
		{"ClearDEKSafety", "ClearDEKSafety",
			&dekPathReq{Path: "app/key"},
			http.MethodDelete, "/api/v1/kms/deks/app/key/safety"},
		{"StartReencryption", "StartReencryption",
			&service.StartReencryptionRequest{Path: "app/key", FromVersion: 1, ToVersion: 2},
			http.MethodPost, "/api/v1/kms/deks/app/key/reencrypt"},
		{"GetReencryptionProgress", "GetReencryptionProgress",
			&service.GetReencryptionProgressRequest{Path: "app/key", OperationID: "op1", Verbose: true},
			http.MethodGet, "/api/v1/kms/deks/app/key/reencrypt"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMethod, gotPath = "", ""
			resp, err := tr.Request(ctx, "DEK", tc.method, tc.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("expected non-nil response")
			}
			if gotMethod != tc.wantMethod {
				t.Errorf("HTTP method: want %s, got %s", tc.wantMethod, gotMethod)
			}
			if gotPath != tc.wantPath {
				t.Errorf("path: want %s, got %s", tc.wantPath, gotPath)
			}
		})
	}
}

// TestDEKHandlerMethodsValidation exercises the request-required and
// path-required guards.
func TestDEKHandlerMethodsValidation(t *testing.T) {
	tr := newMappingTransport(t)
	ctx := context.Background()

	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"RotateBadType", "RotateDEK", "wrong"},
		{"RotateNoPath", "RotateDEK", &dekclient.RotateDEKRequest{}},
		{"DeleteBadType", "DeleteDEK", 5},
		{"DeleteNoPath", "DeleteDEK", &dekclient.DeleteDEKRequest{}},
		{"SetSafetyBadType", "SetDEKSafety", "x"},
		{"SetSafetyNoPath", "SetDEKSafety", &dekclient.SetDEKSafetyRequest{}},
		{"StatusNoPath", "GetDEKStatus", &dekPathReq{}},
		{"SetDefaultNoPath", "SetDefaultDEK", &dekPathReq{}},
		{"ClearSafetyNoPath", "ClearDEKSafety", &dekPathReq{}},
		{"StartReencryptBadType", "StartReencryption", "x"},
		{"StartReencryptNoPath", "StartReencryption", &service.StartReencryptionRequest{}},
		{"ProgressBadType", "GetReencryptionProgress", "x"},
		{"ProgressNoPath", "GetReencryptionProgress", &service.GetReencryptionProgressRequest{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tr.Request(ctx, "DEK", tc.method, tc.req)
			if err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

// TestDEKHandlerUnknownMethodFallback verifies an unknown method falls through
// to the generic retry path.
func TestDEKHandlerUnknownMethodFallback(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{}"))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), "DEK", "Inventory", map[string]string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotPath != "/api/v1/dek/inventory" {
		t.Errorf("unexpected fallback path: %s", gotPath)
	}
}
