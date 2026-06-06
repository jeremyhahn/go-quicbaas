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
	"time"

	kmsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/kms"
	messagingclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/messaging"
	secretsclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
	timeseriesclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/timeseries"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/service"
)

// TestKVConsistencyLevels exercises consistencyToString across all levels via
// the Put handler.
func TestKVConsistencyLevels(t *testing.T) {
	var gotBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotBody = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"entity_type":"users","success":true,"id":1,"timestamp":"2025-01-01T00:00:00Z"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	levels := []service.ConsistencyLevel{
		service.ConsistencyLevelStrong,
		service.ConsistencyLevelLinearizable,
		service.ConsistencyLevelEventual,
	}
	for _, lvl := range levels {
		_, err := tr.Request(ctx, "KV", "Put", &service.PutRequest{
			EntityType:  "users",
			ID:          1,
			Data:        []byte(`{"a":1}`),
			Consistency: lvl,
		})
		if err != nil {
			t.Fatalf("level %v: unexpected error: %v", lvl, err)
		}
	}
	_ = gotBody
}

// TestTimeseriesWriteValidation exercises the handleWrite validation guards and
// a happy path.
func TestTimeseriesWriteValidation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	t.Run("happy path", func(t *testing.T) {
		_, err := tr.Request(ctx, "Timeseries", "Write", &timeseriesclient.WriteRequest{
			Metric: "cpu", Value: 0.5, Timestamp: time.Unix(1700000000, 0),
			Tags: map[string]string{"host": "a"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	cases := []struct {
		name string
		req  interface{}
	}{
		{"bad type", "x"},
		{"empty metric", &timeseriesclient.WriteRequest{Timestamp: time.Unix(1, 0)}},
		{"zero timestamp", &timeseriesclient.WriteRequest{Metric: "cpu"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tr.Request(ctx, "Timeseries", "Write", tc.req); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

// TestMessagingPublishPayloads exercises handlePublish and messagingRawPayload
// across JSON, non-JSON and empty payloads, plus messagingWireToChannel via a
// populated GetChannel response.
func TestMessagingPublishPayloads(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/api/v1/messaging/channels/c1" {
			_, _ = w.Write([]byte(`{"channel":{"name":"c1","codec":"json","created_at":1700000000,` +
				`"subscribers":3,"persistent":true,"max_history":100,"buffer_size":16,"ttl_seconds":60}}`))
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	ctx := context.Background()

	payloads := [][]byte{
		[]byte(`{"already":"json"}`), // valid JSON forwarded verbatim
		[]byte("plain text"),         // non-JSON, gets marshaled
		nil,                          // empty -> null
	}
	for i, p := range payloads {
		_, err := tr.Request(ctx, "Messaging", "Publish",
			&messagingclient.PublishRequest{Channel: "c1", Payload: p})
		if err != nil {
			t.Fatalf("payload %d: unexpected error: %v", i, err)
		}
	}

	t.Run("GetChannel populates Channel", func(t *testing.T) {
		resp, err := tr.Request(ctx, "Messaging", "GetChannel",
			&messagingclient.GetChannelRequest{Name: "c1"})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		ch, ok := resp.(*messagingclient.Channel)
		if !ok {
			t.Fatalf("unexpected response type %T", resp)
		}
		if ch.Name != "c1" || ch.Subscribers != 3 || ch.TTL != 60*time.Second {
			t.Errorf("unexpected channel: %+v", ch)
		}
	})
}

// TestKMSListKeysPopulated exercises handleListKeys with a non-empty key list.
func TestKMSListKeysPopulated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"keys":[{"key_id":"k1","version":1,"algorithm":"AES-256","status":"enabled","created_at":1700000000},` +
			`{"key_id":"k2","version":2,"algorithm":"RSA-2048","status":"disabled","created_at":1700000001}],` +
			`"next_cursor":"c","has_more":true,"total_count":2,"timestamp":1700000002}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	resp, err := tr.Request(context.Background(), "KMS", "ListKeys", &kmsKeyIDReq{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out, ok := resp.(*service.ListKeysResponse)
	if !ok {
		t.Fatalf("unexpected response type %T", resp)
	}
	if len(out.Keys) != 2 || out.TotalCount != 2 || !out.HasMore {
		t.Errorf("unexpected list response: %+v", out)
	}
}

// TestSecretsRotateHappyPath exercises handleRotate end to end.
func TestSecretsRotateHappyPath(t *testing.T) {
	var gotURI string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURI = r.URL.RequestURI()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":3}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), "Secrets", "Rotate",
		&secretsclient.RotateSecretRequest{
			SecretID: "app/key",
			NewData:  []byte(`{"value":"new"}`),
			Metadata: map[string]string{"by": "ops"},
		})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotURI != "/api/v1/secrets/app/key?action=rotate" {
		t.Errorf("unexpected URI: %s", gotURI)
	}
}

// TestSecretsRotateValidation exercises the Rotate guards.
func TestSecretsRotateValidation(t *testing.T) {
	tr := newMappingTransport(t)
	ctx := context.Background()

	cases := []struct {
		name string
		req  interface{}
	}{
		{"bad type", "x"},
		{"no secret id", &secretsclient.RotateSecretRequest{NewData: []byte(`{"v":1}`)}},
		{"no data", &secretsclient.RotateSecretRequest{SecretID: "s1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := tr.Request(ctx, "Secrets", "Rotate", tc.req); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

// TestKMSHandlerServerErrorPaths exercises doJSONRequest error propagation on a
// KMS read method that decodes into a typed struct.
func TestKMSHandlerServerErrorPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer server.Close()

	tr := newTestTransport(t, server)
	_, err := tr.Request(context.Background(), "KMS", "ImportKey",
		&kmsclient.ImportKeyRequest{KeyMaterial: []byte("m"), Name: "k1"})
	if err == nil {
		t.Fatal("expected error from server 500")
	}
}
