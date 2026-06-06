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

	secretsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
)

// secretsListVersionsReq mirrors the internal request the SDK builds for
// ListVersions: a struct exposing an exported SecretID field.
type secretsListVersionsReq struct {
	SecretID string
}

// TestSecretsHandlerMethodsHappyPaths drives the Secrets methods that lacked
// coverage, asserting the HTTP method and request URI each builds.
func TestSecretsHandlerMethodsHappyPaths(t *testing.T) {
	var gotMethod, gotURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotURI = r.URL.RequestURI()
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
		wantURI    string
	}{
		{"Ensure", "Ensure",
			&secretsclient.EnsureSecretRequest{Path: "app/key", Length: 16, Encoding: "base64", TenantID: "t1"},
			http.MethodPost, "/api/v1/secrets/app/key/ensure"},
		{"ListVersions", "ListVersions",
			&secretsListVersionsReq{SecretID: "app/key"},
			http.MethodGet, "/api/v1/secrets/app/key?versions=true"},
		{"Rollback", "Rollback",
			&secretsclient.RollbackSecretRequest{SecretID: "app/key", TargetVersion: 2},
			http.MethodPost, "/api/v1/secrets/app/key?action=rollback"},
		{"UpdateStatusEnable", "UpdateStatus",
			&secretsclient.UpdateSecretStatusRequest{SecretID: "app/key", Status: secretsclient.SecretStatusEnabled},
			http.MethodPost, "/api/v1/secrets/app/key/enable"},
		{"UpdateStatusDisable", "UpdateStatus",
			&secretsclient.UpdateSecretStatusRequest{SecretID: "app/key", Status: secretsclient.SecretStatusDisabled},
			http.MethodPost, "/api/v1/secrets/app/key/disable"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMethod, gotURI = "", ""
			_, err := tr.Request(ctx, "Secrets", tc.method, tc.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if gotMethod != tc.wantMethod {
				t.Errorf("HTTP method: want %s, got %s", tc.wantMethod, gotMethod)
			}
			if gotURI != tc.wantURI {
				t.Errorf("URI: want %s, got %s", tc.wantURI, gotURI)
			}
		})
	}
}

// TestSecretsHandlerMethodsValidation exercises the request-required and
// path/version guards.
func TestSecretsHandlerMethodsValidation(t *testing.T) {
	tr := newMappingTransport(t)
	ctx := context.Background()

	cases := []struct {
		name   string
		method string
		req    interface{}
	}{
		{"EnsureBadType", "Ensure", "x"},
		{"EnsureNoPath", "Ensure", &secretsclient.EnsureSecretRequest{}},
		{"ListVersionsNoID", "ListVersions", &secretsListVersionsReq{}},
		{"RollbackBadType", "Rollback", 1},
		{"RollbackNoID", "Rollback", &secretsclient.RollbackSecretRequest{}},
		{"RollbackBadVersion", "Rollback", &secretsclient.RollbackSecretRequest{SecretID: "s1", TargetVersion: 0}},
		{"UpdateStatusBadType", "UpdateStatus", "x"},
		{"UpdateStatusNoID", "UpdateStatus", &secretsclient.UpdateSecretStatusRequest{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tr.Request(ctx, "Secrets", tc.method, tc.req)
			if err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}
