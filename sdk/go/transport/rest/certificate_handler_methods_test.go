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

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/certificate"
)

// TestCertificateHandlerReadMethods drives the CA-administration read methods
// that previously lacked coverage, asserting the HTTP method and request URI.
func TestCertificateHandlerReadMethods(t *testing.T) {
	var gotMethod, gotURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotURI = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/certificate/ca/info":
			_, _ = w.Write([]byte(`{"common_name":"Root CA","subject":"CN=Root CA","issuer":"CN=Root CA",` +
				`"serial_number":"01","not_before":1700000000,"not_after":1800000000,"is_ca":true,` +
				`"key_type":"ECDSA","key_bits":256,"signature_algorithm":"ECDSA-SHA256",` +
				`"certificate_pem":"PEM","ca_bundle_pem":"BUNDLE"}`))
		case "/api/v1/certificate/ca/bundle":
			_, _ = w.Write([]byte(`{"bundle_pem":"BUNDLE"}`))
		case "/api/v1/certificate/ca/list":
			_, _ = w.Write([]byte(`{"certificates":[{"common_name":"leaf","subject":"CN=leaf","issuer":"CN=Root CA",` +
				`"serial_number":"02","not_before":1700000000,"not_after":1800000000,"is_ca":false,` +
				`"key_usage":"digital_signature","dns_names":["a.example.com"]}],"total_count":1}`))
		case "/api/v1/certificate/ca":
			_, _ = w.Write([]byte(`{"ca_certificate_pem":"PEM","ca_bundle_pem":"BUNDLE","identity":"root"}`))
		case "/api/v1/certificate/revoked/0a":
			_, _ = w.Write([]byte(`{"revoked":true,"reason":1,"message":"key compromise"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
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
		{"CAInfo", "CAInfo", nil, http.MethodGet, "/api/v1/certificate/ca/info"},
		{"GetCABundle", "GetCABundle", nil, http.MethodGet, "/api/v1/certificate/ca/bundle"},
		{"ListCertificates", "ListCertificates",
			&certificate.ListCertificatesRequest{CommonNameFilter: "leaf", IncludeRevoked: true, IncludeExpired: true, Limit: 10},
			http.MethodGet, "/api/v1/certificate/ca/list?common_name=leaf&include_expired=true&include_revoked=true&limit=10"},
		{"GetCACertificate", "GetCACertificate", nil, http.MethodGet, "/api/v1/certificate/ca"},
		{"IsRevoked", "IsRevoked",
			&certificate.IsRevokedRequest{SerialNumber: "0a"},
			http.MethodGet, "/api/v1/certificate/revoked/0a"},
		{"IsInitialized", "IsInitialized", nil, http.MethodGet, "/api/v1/certificate/ca"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotMethod, gotURI = "", ""
			resp, err := tr.Request(ctx, "Certificate", tc.method, tc.req)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp == nil {
				t.Fatal("expected non-nil response")
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

// TestCertificateHandlerIsRevokedValidation exercises the request/serial guards.
func TestCertificateHandlerIsRevokedValidation(t *testing.T) {
	tr := newMappingTransport(t)
	ctx := context.Background()

	t.Run("bad type", func(t *testing.T) {
		if _, err := tr.Request(ctx, "Certificate", "IsRevoked", "x"); err == nil {
			t.Fatal("expected invalid-input error")
		}
	})
	t.Run("missing serial", func(t *testing.T) {
		if _, err := tr.Request(ctx, "Certificate", "IsRevoked", &certificate.IsRevokedRequest{}); err == nil {
			t.Fatal("expected serial-required error")
		}
	})
}

// TestCertificateHandlerIsInitializedFalseOnError verifies IsInitialized maps a
// server failure to a non-error (false) result per the SDK contract.
func TestCertificateHandlerIsInitializedFalseOnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"down"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	resp, err := tr.Request(context.Background(), "Certificate", "IsInitialized", nil)
	if err != nil {
		t.Fatalf("IsInitialized should swallow the error, got: %v", err)
	}
	r, ok := resp.(*certificate.IsInitializedResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if r.Initialized {
		t.Error("expected Initialized=false on server error")
	}
}

// TestCertificateHandlerServerError exercises the doJSONRequest error path on a
// read method.
func TestCertificateHandlerServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	if _, err := tr.Request(context.Background(), "Certificate", "CAInfo", nil); err == nil {
		t.Fatal("expected error from server 500")
	}
}
