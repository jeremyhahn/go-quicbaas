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

	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	messagingclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// TestTypedHandlerErrorPaths fires a range of typed-response handlers at a
// server that always returns 500, exercising the error-propagation branch each
// handler shares.
func TestTypedHandlerErrorPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom","code":"internal"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	cases := []struct {
		name    string
		service string
		method  string
		req     interface{}
	}{
		{"KMSGenerateKey", "KMS", "GenerateKey", &service.GenerateKeyRequest{Name: "k1"}},
		{"KMSRotateKey", "KMS", "RotateKey", &kmsKeyIDReq{KeyID: "k1"}},
		{"KMSBackendStatus", "KMS", "BackendStatus", nil},
		{"KMSGetKey", "KMS", "GetKey", &kmsKeyIDReq{KeyID: "k1"}},
		{"KMSDeleteKey", "KMS", "DeleteKey", &kmsKeyIDReq{KeyID: "k1"}},
		{"KMSExportKey", "KMS", "ExportKey", &kmsclient.ExportKeyRequest{KeyID: "k1"}},
		{"KMSListKeyVersions", "KMS", "ListKeyVersions", &kmsKeyIDReq{KeyID: "k1"}},
		{"KMSGetKeyPolicy", "KMS", "GetKeyPolicy", &kmsKeyIDReq{KeyID: "k1"}},
		{"KVGet", "KV", "Get", &service.GetRequest{EntityType: "u", ID: 1}},
		{"KVDelete", "KV", "Delete", &service.DeleteRequest{EntityType: "u", ID: 1}},
		{"KVList", "KV", "List", &service.ListRequest{EntityType: "u"}},
		{"OrchestratorListBackends", "Orchestrator", "ListBackends", nil},
		{"MessagingPublish", "Messaging", "Publish",
			&messagingclient.PublishRequest{Channel: "c1", Payload: []byte("x")}},
		{"MessagingGetStats", "Messaging", "GetStats", nil},
		{"CertGetCABundle", "Certificate", "GetCABundle", nil},
		{"CertGetCACertificate", "Certificate", "GetCACertificate", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tr.Request(ctx, tc.service, tc.method, tc.req); err == nil {
				t.Fatalf("expected error from 500 for %s", tc.name)
			}
		})
	}
}
