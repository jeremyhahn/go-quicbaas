// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
//
// This file is part of go-quicbaas.
//
// go-quicbaas is dual-licensed:
//
//  1. GNU Affero General Public License v3.0 (AGPL-3.0)
//     See LICENSE file or visit https://www.gnu.org/licenses/agpl-3.0.html
//
//  2. Commercial License
//     Contact licensing@automatethethings.com for commercial licensing options.

package signalbridge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/coder/websocket"
	signalapi "github.com/jeremyhahn/go-signal/pkg/api/rest"
	signalws "github.com/jeremyhahn/go-signal/pkg/websocket"
)

// newTransport wires an HTTPTransport against an httptest server.
func newTransport(t *testing.T, h http.Handler) (*HTTPTransport, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	tr, err := NewHTTPTransport(HTTPTransportConfig{
		BaseURL: srv.URL,
		Logger:  noopLogger{},
	})
	if err != nil {
		t.Fatalf("NewHTTPTransport: %v", err)
	}
	return tr, srv
}

// wsRequestFrame builds a JSON-encoded go-signal websocket request frame
// carrying content as a base64 body — the exact shape the transport's
// drainInbound expects from a JSON-emitting client.
func wsRequestFrame(t *testing.T, verb, path string, content []byte) []byte {
	t.Helper()
	msg := &signalws.WebSocketMessage{
		Type: "request",
		Request: &signalws.WebSocketRequest{
			ID:   1,
			Verb: verb,
			Path: path,
			Body: base64.StdEncoding.EncodeToString(content),
		},
	}
	data, err := signalws.MarshalFrame(msg, signalws.FrameFormatJSON)
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	return data
}

// newWSServer starts an httptest server that upgrades to a websocket and
// writes the supplied frame once, then blocks until the client closes the
// connection so the delivered envelope stays readable.
func newWSServer(t *testing.T, frame []byte) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			t.Errorf("ws accept: %v", err)
			return
		}
		ctx := r.Context()
		if err := conn.Write(ctx, websocket.MessageText, frame); err != nil {
			return
		}
		_, _, _ = conn.Read(ctx)
		_ = conn.Close(websocket.StatusNormalClosure, "done")
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestNewHTTPTransport_Validation rejects empty BaseURL, nil Logger, and
// a malformed URL, and accepts a valid config with a default timeout.
func TestNewHTTPTransport_Validation(t *testing.T) {
	if _, err := NewHTTPTransport(HTTPTransportConfig{Logger: noopLogger{}}); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("empty BaseURL: want invalid_input, got %v", err)
	}
	if _, err := NewHTTPTransport(HTTPTransportConfig{BaseURL: "http://x"}); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("nil Logger: want invalid_input, got %v", err)
	}
	if _, err := NewHTTPTransport(HTTPTransportConfig{BaseURL: "://bad", Logger: noopLogger{}}); !IsKind(err, ErrKindInvalidInput) {
		t.Fatalf("bad URL: want invalid_input, got %v", err)
	}
	tr, err := NewHTTPTransport(HTTPTransportConfig{BaseURL: "http://x", Logger: noopLogger{}})
	if err != nil {
		t.Fatalf("valid config: %v", err)
	}
	if tr.httpClient.Timeout == 0 {
		t.Fatalf("expected default timeout to be applied")
	}
}

// TestHTTPTransport_CreateVerificationSession returns the session ID and
// surfaces a transport error on a 500.
func TestHTTPTransport_CreateVerificationSession(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/verification/session" || r.Method != http.MethodPost {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "sess-42"})
	}))
	id, err := tr.CreateVerificationSession(context.Background(), "+15551234567")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if id != "sess-42" {
		t.Fatalf("id = %q, want sess-42", id)
	}
}

func TestHTTPTransport_CreateVerificationSession_Error(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	if _, err := tr.CreateVerificationSession(context.Background(), "+1"); !IsKind(err, ErrKindTransport) {
		t.Fatalf("want transport error, got %v", err)
	}
}

// TestHTTPTransport_RequestVerificationCode posts the transport and
// captcha, and errors on a 400.
func TestHTTPTransport_RequestVerificationCode(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/code") || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["transport"] != "sms" || body["captcha"] != "cap" {
			t.Errorf("unexpected body %+v", body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	if err := tr.RequestVerificationCode(context.Background(), "sid", "sms", "cap"); err != nil {
		t.Fatalf("request: %v", err)
	}
}

func TestHTTPTransport_RequestVerificationCode_Error(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusBadRequest)
	}))
	if err := tr.RequestVerificationCode(context.Background(), "sid", "sms", ""); !IsKind(err, ErrKindTransport) {
		t.Fatalf("want transport, got %v", err)
	}
}

// TestHTTPTransport_SubmitVerificationCode puts the code and errors on
// failure.
func TestHTTPTransport_SubmitVerificationCode(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			t.Errorf("want PUT, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	}))
	if err := tr.SubmitVerificationCode(context.Background(), "sid", "123456"); err != nil {
		t.Fatalf("submit: %v", err)
	}
}

func TestHTTPTransport_SubmitVerificationCode_Error(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusForbidden)
	}))
	if err := tr.SubmitVerificationCode(context.Background(), "sid", "bad"); !IsKind(err, ErrKindTransport) {
		t.Fatalf("want transport, got %v", err)
	}
}

// TestHTTPTransport_CompleteRegistration sends identity material and
// defaults the device ID to 1 when the server omits it.
func TestHTTPTransport_CompleteRegistration(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Errorf("missing Authorization header")
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if _, ok := body["identityKey"]; !ok {
			t.Errorf("identityKey not encoded: %+v", body)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"uuid": "aci-1", "pni": "pni-1"})
	}))
	resp, err := tr.CompleteRegistration(context.Background(), RegistrationRequest{
		SessionID:      "sid",
		Number:         "+1",
		Password:       "pw",
		IdentityPublic: []byte{1, 2, 3},
	})
	if err != nil {
		t.Fatalf("complete: %v", err)
	}
	if resp.ACI != "aci-1" || resp.PNI != "pni-1" || resp.DeviceID != 1 {
		t.Fatalf("unexpected resp %+v", resp)
	}
}

func TestHTTPTransport_CompleteRegistration_Error(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "conflict", http.StatusConflict)
	}))
	if _, err := tr.CompleteRegistration(context.Background(), RegistrationRequest{}); !IsKind(err, ErrKindTransport) {
		t.Fatalf("want transport, got %v", err)
	}
}

// TestHTTPTransport_UploadPreKeys encodes the bundle and authenticates.
func TestHTTPTransport_UploadPreKeys(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/v2/keys" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") == "" {
			t.Errorf("missing auth")
		}
		w.WriteHeader(http.StatusOK)
	}))
	bundle := PreKeyBundleUpload{
		IdentityKey:     []byte{1},
		SignedPreKeyID:  7,
		SignedPreKeyPub: []byte{2},
		SignedPreKeySig: []byte{3},
		OneTimePreKeys:  []PreKeyMaterial{{ID: 1, Public: []byte{9}}},
		KyberPreKeys:    []KyberPreKeyMaterial{{ID: 2, Public: []byte{8}, Signature: []byte{7}}},
	}
	if err := tr.UploadPreKeys(context.Background(), SignalAccount{ACI: "aci", DeviceID: 1, Password: "pw"}, bundle); err != nil {
		t.Fatalf("upload: %v", err)
	}
}

func TestHTTPTransport_UploadPreKeys_Error(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusBadRequest)
	}))
	// UploadPreKeys returns the do() error directly; assert it is non-nil
	// so a server failure is not silently swallowed.
	if err := tr.UploadPreKeys(context.Background(), SignalAccount{ACI: "a"}, PreKeyBundleUpload{}); err == nil {
		t.Fatalf("expected error on 400")
	}
}

// TestHTTPTransport_PreKeyCount reads the server's remaining count.
func TestHTTPTransport_PreKeyCount(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]int{"count": 42})
	}))
	n, err := tr.PreKeyCount(context.Background(), SignalAccount{ACI: "a", Password: "p"})
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if n != 42 {
		t.Fatalf("count = %d, want 42", n)
	}
}

func TestHTTPTransport_PreKeyCount_Error(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	if _, err := tr.PreKeyCount(context.Background(), SignalAccount{ACI: "a"}); !IsKind(err, ErrKindTransport) {
		t.Fatalf("want transport, got %v", err)
	}
}

// TestHTTPTransport_FetchPreKeyBundle decodes a matching device entry and
// base64-decodes the key material, skipping non-matching devices.
func TestHTTPTransport_FetchPreKeyBundle(t *testing.T) {
	enc := func(b []byte) string { return base64.StdEncoding.EncodeToString(b) }
	resp := signalapi.PreKeyResponseEntity{
		IdentityKey: enc([]byte{0xaa}),
		Devices: []signalapi.PreKeyResponseItemEntity{
			{DeviceId: 2, RegistrationId: 1}, // non-matching device, skipped
			{
				DeviceId:       1,
				RegistrationId: 55,
				SignedPreKey:   &signalapi.SignedPreKeyEntity{KeyId: 7, PublicKey: enc([]byte{0x01}), Signature: enc([]byte{0x02})},
				PreKey:         &signalapi.PreKeyEntity{KeyId: 8, PublicKey: enc([]byte{0x03})},
				PqPreKey:       &signalapi.KEMSignedPreKeyEntity{KeyId: 9, PublicKey: enc([]byte{0x04}), Signature: enc([]byte{0x05})},
			},
		},
	}
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/v2/keys/") {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(&resp)
	}))
	bundle, err := tr.FetchPreKeyBundle(context.Background(), SignalAccount{ACI: "a", Password: "p"}, "recip", 1)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if bundle.RegistrationID != 55 {
		t.Fatalf("registration id = %d, want 55", bundle.RegistrationID)
	}
	if bundle.SignedPreKeyID != 7 || bundle.PreKeyID != 8 || bundle.KyberPreKeyID != 9 {
		t.Fatalf("key ids wrong: %+v", bundle)
	}
	if len(bundle.IdentityKey) != 1 || bundle.IdentityKey[0] != 0xaa {
		t.Fatalf("identity key not decoded: %v", bundle.IdentityKey)
	}
}

func TestHTTPTransport_FetchPreKeyBundle_Error(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusNotFound)
	}))
	if _, err := tr.FetchPreKeyBundle(context.Background(), SignalAccount{ACI: "a"}, "r", 1); !IsKind(err, ErrKindTransport) {
		t.Fatalf("want transport, got %v", err)
	}
}

// TestHTTPTransport_SendEnvelope dispatches and reports needsSync.
func TestHTTPTransport_SendEnvelope(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || !strings.HasPrefix(r.URL.Path, "/v1/messages/") {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"needsSync": true})
	}))
	res, err := tr.SendEnvelope(context.Background(), SignalAccount{ACI: "a", Password: "p"}, OutboundEnvelope{
		DestinationACI:    "dest",
		DestinationDevice: 1,
		Content:           []byte{1, 2},
		Timestamp:         99,
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	if !res.NeedsSync {
		t.Fatalf("expected needsSync true")
	}
}

func TestHTTPTransport_SendEnvelope_RateLimited(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	_, err := tr.SendEnvelope(context.Background(), SignalAccount{ACI: "a"}, OutboundEnvelope{DestinationACI: "d"})
	if !IsKind(err, ErrKindRateLimited) {
		t.Fatalf("want rate_limited, got %v", err)
	}
}

// TestHTTPTransport_AckInbound deletes the envelope by id.
func TestHTTPTransport_AckInbound(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("want DELETE, got %s", r.Method)
		}
		if !strings.Contains(r.URL.Path, "/v1/messages/uuid/77") {
			t.Errorf("path = %s", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))
	if err := tr.AckInbound(context.Background(), SignalAccount{ACI: "a", Password: "p"}, 77); err != nil {
		t.Fatalf("ack: %v", err)
	}
}

func TestHTTPTransport_AckInbound_Error(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", http.StatusInternalServerError)
	}))
	if err := tr.AckInbound(context.Background(), SignalAccount{ACI: "a"}, 1); !IsKind(err, ErrKindTransport) {
		t.Fatalf("want transport, got %v", err)
	}
}

// TestHTTPTransport_OpenInbound dials the websocket and streams inbound
// envelopes parsed from JSON frames. Exercises OpenInbound, drainInbound,
// and envelopeFromWSFrame end-to-end against a real WS handshake.
func TestHTTPTransport_OpenInbound(t *testing.T) {
	frame := wsRequestFrame(t, "PUT", "/api/v1/message", []byte("payload"))
	srv := newWSServer(t, frame)
	tr, err := NewHTTPTransport(HTTPTransportConfig{BaseURL: srv.URL, Logger: noopLogger{}})
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch, closeFn, err := tr.OpenInbound(ctx, SignalAccount{ACI: "aci", Password: "pw"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	select {
	case env, ok := <-ch:
		if !ok {
			t.Fatalf("channel closed before delivering envelope")
		}
		if string(env.Content) != "payload" {
			t.Fatalf("content = %q, want payload", env.Content)
		}
	case <-ctx.Done():
		t.Fatalf("context cancelled before envelope")
	}
	if err := closeFn(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestHTTPTransport_OpenInbound_DialError surfaces a transport error when
// the handshake cannot be completed.
func TestHTTPTransport_OpenInbound_DialError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no upgrade", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)
	tr, err := NewHTTPTransport(HTTPTransportConfig{BaseURL: srv.URL, Logger: noopLogger{}})
	if err != nil {
		t.Fatalf("transport: %v", err)
	}
	if _, _, err := tr.OpenInbound(context.Background(), SignalAccount{ACI: "a"}); !IsKind(err, ErrKindTransport) {
		t.Fatalf("want transport, got %v", err)
	}
}

// TestBasicAuth_Encoding produces a decodable RFC 7617 credential.
func TestBasicAuth_Encoding(t *testing.T) {
	cred := basicAuth("user", "pass")
	if !strings.HasPrefix(cred, "Basic ") {
		t.Fatalf("missing Basic prefix: %q", cred)
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(cred, "Basic "))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if string(raw) != "user:pass" {
		t.Fatalf("decoded = %q", raw)
	}
}

// TestAuthHeaders_DeviceSuffix appends the device id to the login when the
// account runs as a secondary device.
func TestAuthHeaders_DeviceSuffix(t *testing.T) {
	tr, _ := newTransport(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	primary := tr.authHeaders(SignalAccount{ACI: "aci", DeviceID: 1, Password: "pw"})
	secondary := tr.authHeaders(SignalAccount{ACI: "aci", DeviceID: 3, Password: "pw"})
	decode := func(h string) string {
		raw, _ := base64.StdEncoding.DecodeString(strings.TrimPrefix(h, "Basic "))
		return string(raw)
	}
	if got := decode(primary["Authorization"]); got != "aci:pw" {
		t.Fatalf("primary login = %q, want aci:pw", got)
	}
	if got := decode(secondary["Authorization"]); got != "aci.3:pw" {
		t.Fatalf("secondary login = %q, want aci.3:pw", got)
	}
}

// TestEncodePreKeys_Encoding renders one-time pre-keys as base64 records.
func TestEncodePreKeys_Encoding(t *testing.T) {
	out := encodePreKeys([]PreKeyMaterial{{ID: 5, Public: []byte{0xde}}})
	if len(out) != 1 {
		t.Fatalf("len = %d", len(out))
	}
	if out[0]["keyId"] != uint32(5) {
		t.Fatalf("keyId = %v", out[0]["keyId"])
	}
	if out[0]["publicKey"] != base64.StdEncoding.EncodeToString([]byte{0xde}) {
		t.Fatalf("publicKey encoding wrong: %v", out[0]["publicKey"])
	}
	if len(encodePreKeys(nil)) != 0 {
		t.Fatalf("nil input should yield empty slice")
	}
}

// TestEncodeKyberPreKeys_Encoding renders Kyber pre-keys with signatures.
func TestEncodeKyberPreKeys_Encoding(t *testing.T) {
	out := encodeKyberPreKeys([]KyberPreKeyMaterial{{ID: 6, Public: []byte{0x01}, Signature: []byte{0x02}}})
	if len(out) != 1 {
		t.Fatalf("len = %d", len(out))
	}
	if out[0]["signature"] != base64.StdEncoding.EncodeToString([]byte{0x02}) {
		t.Fatalf("signature encoding wrong: %v", out[0]["signature"])
	}
}

// TestEnvelopeFromWSFrame_Filters rejects nil frames, frames without a
// request, and non-PUT verbs; accepts a PUT message frame and decodes its
// base64 body.
func TestEnvelopeFromWSFrame_Filters(t *testing.T) {
	if _, ok := envelopeFromWSFrame(nil); ok {
		t.Fatalf("nil frame should be rejected")
	}
	if _, ok := envelopeFromWSFrame(&signalws.WebSocketMessage{Type: "response"}); ok {
		t.Fatalf("frame without request should be rejected")
	}
	getFrame := &signalws.WebSocketMessage{Request: &signalws.WebSocketRequest{Verb: "GET", Path: "/x"}}
	if _, ok := envelopeFromWSFrame(getFrame); ok {
		t.Fatalf("non-PUT verb should be rejected")
	}
	putFrame := &signalws.WebSocketMessage{Request: &signalws.WebSocketRequest{
		ID:   7,
		Verb: "PUT",
		Path: "/api/v1/message",
		Body: base64.StdEncoding.EncodeToString([]byte("hello")),
	}}
	env, ok := envelopeFromWSFrame(putFrame)
	if !ok {
		t.Fatalf("PUT message frame should be accepted")
	}
	if env.ID != 7 || string(env.Content) != "hello" {
		t.Fatalf("unexpected envelope %+v", env)
	}
	// Non-base64 body falls back to raw bytes.
	rawFrame := &signalws.WebSocketMessage{Request: &signalws.WebSocketRequest{
		Verb: "PUT",
		Body: "\x00\x01not base64!!",
	}}
	env2, ok := envelopeFromWSFrame(rawFrame)
	if !ok {
		t.Fatalf("raw-body PUT frame should be accepted")
	}
	if len(env2.Content) == 0 {
		t.Fatalf("raw body should fall back to bytes")
	}
}
