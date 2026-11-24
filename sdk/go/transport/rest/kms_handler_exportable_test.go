// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
//
// This file is part of go-quicbaas.

package rest

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	xkmssdk "github.com/jeremyhahn/go-xkms/sdk/go"
)

// newBodyRecorder returns a server that answers every request with respBody
// and sends each request body it receives on the returned channel.
func newBodyRecorder(t *testing.T, respBody string) (*httptest.Server, <-chan []byte) {
	t.Helper()
	bodies := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		bodies <- body
		w.Header().Set("Content-Type", "application/json")
		if _, err := w.Write([]byte(respBody)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(server.Close)
	return server, bodies
}

// TestKMSHandlerUnwrapKeySendsExportable checks UnwrapKey sends Exportable in
// the key_attributes the REST server decodes into service.UnwrapKeyRequest.
func TestKMSHandlerUnwrapKeySendsExportable(t *testing.T) {
	for _, exportable := range []bool{true, false} {
		server, bodies := newBodyRecorder(t,
			`{"key_id":"k2","version":1,"algorithm":"AES-256-GCM","status":"enabled","timestamp":1700000000}`)
		tr := newTestTransport(t, server)

		_, err := tr.Request(context.Background(), "KMS", "UnwrapKey", &kmsclient.UnwrapKeyRequest{
			KeyID: "k2", WrappingKeyID: "wk", WrappedMaterial: []byte("wrap"), Exportable: exportable,
		})
		if err != nil {
			t.Fatalf("UnwrapKey: %v", err)
		}
		// The REST server decodes key_attributes into the xkms KeyAttributes.
		var got struct {
			KeyAttributes *xkmssdk.KeyAttributes `json:"key_attributes"`
		}
		if err := json.Unmarshal(<-bodies, &got); err != nil {
			t.Fatalf("decode body as the server does: %v", err)
		}
		if gotExportable := got.KeyAttributes != nil && got.KeyAttributes.Exportable; gotExportable != exportable {
			t.Errorf("exportable = %t, want %t", gotExportable, exportable)
		}
	}
}

// TestKMSHandlerImportKeySendsExportable checks ImportKey sends Exportable in
// key_attributes.exportable, the field the REST server reads.
func TestKMSHandlerImportKeySendsExportable(t *testing.T) {
	for _, exportable := range []bool{true, false} {
		server, bodies := newBodyRecorder(t,
			`{"key":{"key_id":"k1","version":1,"algorithm":"AES-256-GCM","status":"enabled"},"timestamp":1700000000}`)
		tr := newTestTransport(t, server)

		_, err := tr.Request(context.Background(), "KMS", "ImportKey", &kmsclient.ImportKeyRequest{
			KeyMaterial: []byte("material"), Name: "k1", Algorithm: kmsclient.AlgorithmAES256GCM, Exportable: exportable,
		})
		if err != nil {
			t.Fatalf("ImportKey: %v", err)
		}
		var got struct {
			KeyAttributes struct {
				Exportable bool `json:"exportable"`
			} `json:"key_attributes"`
		}
		if err := json.Unmarshal(<-bodies, &got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		if got.KeyAttributes.Exportable != exportable {
			t.Errorf("exportable = %t, want %t", got.KeyAttributes.Exportable, exportable)
		}
	}
}

// TestKMSHandlerUnwrapKeyExportableNeedsMaterial checks an exportable unwrap
// without wrapped material is refused before any request is sent.
func TestKMSHandlerUnwrapKeyExportableNeedsMaterial(t *testing.T) {
	server, bodies := newBodyRecorder(t, `{}`)
	tr := newTestTransport(t, server)

	_, err := tr.Request(context.Background(), "KMS", "UnwrapKey",
		&kmsclient.UnwrapKeyRequest{KeyID: "k2", WrappingKeyID: "wk", Exportable: true})
	if err == nil {
		t.Fatal("expected an error for an unwrap without wrapped material")
	}
	select {
	case <-bodies:
		t.Error("the request must not reach the server")
	default:
	}
}
