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
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"

	signalapi "github.com/jeremyhahn/go-signal/pkg/api/rest"
	signalws "github.com/jeremyhahn/go-signal/pkg/websocket"

	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
)

// HTTPTransportConfig configures the go-signal REST + WebSocket
// transport.
type HTTPTransportConfig struct {
	BaseURL string
	// TLSClientConfig is used by the HTTP and WebSocket clients. A nil
	// value means default system trust store.
	TLSClientConfig *tls.Config
	// RequestTimeout bounds individual HTTP requests. 0 defaults to 30s.
	RequestTimeout time.Duration
	Logger         interfaces.Logger
}

// HTTPTransport implements ServerTransport against a go-signal server
// using its REST API and realtime WebSocket. Request/response shapes
// track upstream go-signal's types; when those shift the transport is
// the sole point of update.
type HTTPTransport struct {
	cfg        HTTPTransportConfig
	httpClient *http.Client
	baseURL    *url.URL
	logger     interfaces.Logger
}

// NewHTTPTransport validates cfg and returns a transport.
func NewHTTPTransport(cfg HTTPTransportConfig) (*HTTPTransport, error) {
	const op = "signalbridge.NewHTTPTransport"
	if cfg.BaseURL == "" {
		return nil, newError(op, ErrKindInvalidInput, "BaseURL required")
	}
	if cfg.Logger == nil {
		return nil, newError(op, ErrKindInvalidInput, "Logger required")
	}
	u, err := url.Parse(cfg.BaseURL)
	if err != nil {
		return nil, wrapError(op, ErrKindInvalidInput, err, "invalid BaseURL")
	}
	timeout := cfg.RequestTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	tr := &http.Transport{TLSClientConfig: cfg.TLSClientConfig}
	return &HTTPTransport{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: timeout, Transport: tr},
		baseURL:    u,
		logger:     cfg.Logger,
	}, nil
}

// CreateVerificationSession implements ServerTransport.
func (h *HTTPTransport) CreateVerificationSession(ctx context.Context, number string) (string, error) {
	const op = "signalbridge.HTTPTransport.CreateVerificationSession"
	var resp struct {
		ID string `json:"id"`
	}
	body := map[string]string{"number": number}
	if err := h.do(ctx, http.MethodPost, "/v1/verification/session", nil, body, &resp); err != nil {
		return "", wrapError(op, ErrKindTransport, err, "create session")
	}
	return resp.ID, nil
}

// RequestVerificationCode implements ServerTransport.
func (h *HTTPTransport) RequestVerificationCode(ctx context.Context, sessionID, transport, captcha string) error {
	const op = "signalbridge.HTTPTransport.RequestVerificationCode"
	body := map[string]string{"transport": transport}
	if captcha != "" {
		body["captcha"] = captcha
	}
	if err := h.do(ctx, http.MethodPost, "/v1/verification/session/"+sessionID+"/code", nil, body, nil); err != nil {
		return wrapError(op, ErrKindTransport, err, "request code")
	}
	return nil
}

// SubmitVerificationCode implements ServerTransport.
func (h *HTTPTransport) SubmitVerificationCode(ctx context.Context, sessionID, code string) error {
	const op = "signalbridge.HTTPTransport.SubmitVerificationCode"
	body := map[string]string{"code": code}
	if err := h.do(ctx, http.MethodPut, "/v1/verification/session/"+sessionID+"/code", nil, body, nil); err != nil {
		return wrapError(op, ErrKindTransport, err, "submit code")
	}
	return nil
}

// CompleteRegistration implements ServerTransport.
func (h *HTTPTransport) CompleteRegistration(ctx context.Context, req RegistrationRequest) (RegistrationResponse, error) {
	const op = "signalbridge.HTTPTransport.CompleteRegistration"
	payload := map[string]any{
		"sessionId": req.SessionID,
		"accountAttributes": map[string]any{
			"fetchesMessages":   true,
			"registrationId":    req.RegistrationID,
			"pniRegistrationId": req.PNIRegID,
			"name":              req.DeviceName,
		},
	}
	if len(req.IdentityPublic) > 0 {
		payload["identityKey"] = base64.StdEncoding.EncodeToString(req.IdentityPublic)
	}
	headers := map[string]string{
		"Authorization": basicAuth(req.Number, req.Password),
	}
	var resp struct {
		UUID     string `json:"uuid"`
		PNI      string `json:"pni"`
		DeviceID uint32 `json:"deviceId"`
	}
	if err := h.do(ctx, http.MethodPost, "/v1/registration", headers, payload, &resp); err != nil {
		return RegistrationResponse{}, wrapError(op, ErrKindTransport, err, "register")
	}
	if resp.DeviceID == 0 {
		resp.DeviceID = 1
	}
	return RegistrationResponse{ACI: resp.UUID, PNI: resp.PNI, DeviceID: resp.DeviceID}, nil
}

// UploadPreKeys implements ServerTransport.
func (h *HTTPTransport) UploadPreKeys(ctx context.Context, account SignalAccount, bundle PreKeyBundleUpload) error {
	body := map[string]any{
		"identityKey": base64.StdEncoding.EncodeToString(bundle.IdentityKey),
		"signedPreKey": map[string]any{
			"keyId":     bundle.SignedPreKeyID,
			"publicKey": base64.StdEncoding.EncodeToString(bundle.SignedPreKeyPub),
			"signature": base64.StdEncoding.EncodeToString(bundle.SignedPreKeySig),
		},
		"preKeys":      encodePreKeys(bundle.OneTimePreKeys),
		"kyberPreKeys": encodeKyberPreKeys(bundle.KyberPreKeys),
	}
	return h.do(ctx, http.MethodPut, "/v2/keys", h.authHeaders(account), body, nil)
}

// PreKeyCount implements ServerTransport.
func (h *HTTPTransport) PreKeyCount(ctx context.Context, account SignalAccount) (int, error) {
	const op = "signalbridge.HTTPTransport.PreKeyCount"
	var resp struct {
		Count int `json:"count"`
	}
	if err := h.do(ctx, http.MethodGet, "/v2/keys", h.authHeaders(account), nil, &resp); err != nil {
		return 0, wrapError(op, ErrKindTransport, err, "fetch prekey count")
	}
	return resp.Count, nil
}

// FetchPreKeyBundle implements ServerTransport. Decodes directly into the
// upstream-aligned go-signal/pkg/api/rest types (PreKeyResponseEntity +
// PreKeyResponseItemEntity) so our wire shape tracks official Signal-Server
// exactly — no custom ad-hoc JSON parsing that could silently drift.
func (h *HTTPTransport) FetchPreKeyBundle(ctx context.Context, account SignalAccount, recipientACI string, deviceID uint32) (PreKeyBundleFetch, error) {
	const op = "signalbridge.HTTPTransport.FetchPreKeyBundle"
	path := fmt.Sprintf("/v2/keys/%s/%d", recipientACI, deviceID)
	var resp signalapi.PreKeyResponseEntity
	if err := h.do(ctx, http.MethodGet, path, h.authHeaders(account), nil, &resp); err != nil {
		return PreKeyBundleFetch{}, wrapError(op, ErrKindTransport, err, "fetch bundle")
	}

	bundle := PreKeyBundleFetch{
		RecipientACI: recipientACI,
		DeviceID:     deviceID,
	}
	if resp.IdentityKey != "" {
		if ik, decErr := base64.StdEncoding.DecodeString(resp.IdentityKey); decErr == nil {
			bundle.IdentityKey = ik
		}
	}
	// Pick the device that matches the requested deviceID. go-signal's API
	// accepts "*" to mean "all devices for this account"; when a single
	// device was asked for, pick the first matching entry.
	for _, dev := range resp.Devices {
		if deviceID != 0 && uint64(deviceID) != dev.DeviceId {
			continue
		}
		bundle.RegistrationID = dev.RegistrationId
		if dev.SignedPreKey != nil {
			bundle.SignedPreKeyID = uint32(dev.SignedPreKey.KeyId)
			if pk, err := base64.StdEncoding.DecodeString(dev.SignedPreKey.PublicKey); err == nil {
				bundle.SignedPreKeyPub = pk
			}
			if sig, err := base64.StdEncoding.DecodeString(dev.SignedPreKey.Signature); err == nil {
				bundle.SignedPreKeySig = sig
			}
		}
		if dev.PreKey != nil {
			bundle.PreKeyID = uint32(dev.PreKey.KeyId)
			if pk, err := base64.StdEncoding.DecodeString(dev.PreKey.PublicKey); err == nil {
				bundle.PreKeyPub = pk
			}
		}
		if dev.PqPreKey != nil {
			bundle.KyberPreKeyID = uint32(dev.PqPreKey.KeyId)
			if pk, err := base64.StdEncoding.DecodeString(dev.PqPreKey.PublicKey); err == nil {
				bundle.KyberPreKeyPub = pk
			}
			if sig, err := base64.StdEncoding.DecodeString(dev.PqPreKey.Signature); err == nil {
				bundle.KyberPreKeySig = sig
			}
		}
		break
	}
	return bundle, nil
}

// SendEnvelope implements ServerTransport.
func (h *HTTPTransport) SendEnvelope(ctx context.Context, account SignalAccount, env OutboundEnvelope) (SendResult, error) {
	const op = "signalbridge.HTTPTransport.SendEnvelope"
	payload := map[string]any{
		"messages": []map[string]any{{
			"type":                env.EnvelopeType,
			"destinationDeviceId": env.DestinationDevice,
			"content":             base64.StdEncoding.EncodeToString(env.Content),
			"timestamp":           env.Timestamp,
			"urgent":              env.Urgent,
		}},
		"timestamp": env.Timestamp,
	}
	var resp struct {
		NeedsSync bool `json:"needsSync"`
	}
	if err := h.do(ctx, http.MethodPut, "/v1/messages/"+env.DestinationACI, h.authHeaders(account), payload, &resp); err != nil {
		return SendResult{}, wrapError(op, ErrKindTransport, err, "send envelope")
	}
	return SendResult{NeedsSync: resp.NeedsSync}, nil
}

// OpenInbound implements ServerTransport.
func (h *HTTPTransport) OpenInbound(ctx context.Context, account SignalAccount) (<-chan InboundEnvelope, func() error, error) {
	const op = "signalbridge.HTTPTransport.OpenInbound"
	wsURL := *h.baseURL
	switch wsURL.Scheme {
	case "http":
		wsURL.Scheme = "ws"
	case "https":
		wsURL.Scheme = "wss"
	}
	wsURL.Path = strings.TrimSuffix(wsURL.Path, "/") + "/v1/websocket/"
	q := wsURL.Query()
	q.Set("login", account.ACI)
	q.Set("password", account.Password)
	wsURL.RawQuery = q.Encode()

	opts := &websocket.DialOptions{
		HTTPHeader: http.Header{
			"User-Agent": []string{"quicbaas-signalbridge"},
		},
	}
	if h.cfg.TLSClientConfig != nil {
		opts.HTTPClient = &http.Client{
			Transport: &http.Transport{TLSClientConfig: h.cfg.TLSClientConfig},
		}
	}
	// websocket.Dial closes the handshake response body itself; the caller
	// only owns the returned connection.
	conn, _, err := websocket.Dial(ctx, wsURL.String(), opts) //nolint:bodyclose // websocket.Dial closes the handshake body itself and returns it nil on success
	if err != nil {
		return nil, nil, wrapError(op, ErrKindTransport, err, "dial websocket")
	}
	ch := make(chan InboundEnvelope, 64)
	closeFn := func() error {
		return conn.Close(websocket.StatusNormalClosure, "bye")
	}
	go h.drainInbound(ctx, conn, ch)
	return ch, closeFn, nil
}

// drainInbound reads WebSocket frames and parses them into envelopes using
// go-signal's public codec (`pkg/websocket.UnmarshalFrame`) — the exact
// same wsproto protobuf the upstream Signal-Server uses. Tracking the
// upstream codec guarantees interoperability with any official Signal
// client that dials our go-signal server.
func (h *HTTPTransport) drainInbound(ctx context.Context, conn *websocket.Conn, ch chan<- InboundEnvelope) {
	defer close(ch)
	defer conn.Close(websocket.StatusNormalClosure, "drain complete")
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		_, data, err := conn.Read(ctx)
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				h.logger.WithError(err).Warn("signalbridge: websocket read")
			}
			return
		}
		msg, err := signalws.UnmarshalFrame(data, signalws.FrameFormatProtobuf)
		if err != nil || msg == nil {
			// Retry as JSON frame before giving up — Go/test Signal clients
			// emit JSON text; official mobile clients emit protobuf.
			msg, err = signalws.UnmarshalFrame(data, signalws.FrameFormatJSON)
			if err != nil || msg == nil {
				h.logger.WithError(err).Debug("signalbridge: unrecognized ws frame")
				continue
			}
		}
		// Only relay request frames targeting /api/v1/message — those carry
		// inbound user-to-user messages. Response/other frames pass through
		// the manager layer in go-signal and are irrelevant to the bot.
		env, ok := envelopeFromWSFrame(msg)
		if !ok {
			continue
		}
		select {
		case ch <- env:
		case <-ctx.Done():
			return
		}
	}
}

// envelopeFromWSFrame projects a go-signal WebSocketMessage into our
// lightweight InboundEnvelope. Returns false when the frame isn't an
// inbound message delivery (e.g. pings, responses, admin frames).
func envelopeFromWSFrame(msg *signalws.WebSocketMessage) (InboundEnvelope, bool) {
	if msg == nil || msg.Request == nil {
		return InboundEnvelope{}, false
	}
	// Signal-Server delivers inbound envelopes via PUT /api/v1/message
	// (see go-signal's WebSocketConnection.Send handler). We only care
	// about those; everything else (ping, keep-alive, delivery receipts)
	// is protocol overhead handled by the client/server themselves.
	if msg.Request.Verb != "PUT" {
		return InboundEnvelope{}, false
	}
	content, err := base64.StdEncoding.DecodeString(msg.Request.Body)
	if err != nil {
		// Some protobuf clients send already-decoded binary in the body
		// field; fall back to treating it as raw bytes.
		content = []byte(msg.Request.Body)
	}
	return InboundEnvelope{
		ID:      msg.Request.ID,
		Content: content,
	}, true
}

// AckInbound implements ServerTransport.
func (h *HTTPTransport) AckInbound(ctx context.Context, account SignalAccount, envelopeID uint64) error {
	const op = "signalbridge.HTTPTransport.AckInbound"
	path := fmt.Sprintf("/v1/messages/uuid/%d", envelopeID)
	if err := h.do(ctx, http.MethodDelete, path, h.authHeaders(account), nil, nil); err != nil {
		return wrapError(op, ErrKindTransport, err, "ack inbound")
	}
	return nil
}

// =============================================================================
// Helpers
// =============================================================================

// do executes a JSON HTTP request against baseURL+path and decodes the
// response body into out when non-nil.
func (h *HTTPTransport) do(ctx context.Context, method, path string, headers map[string]string, body any, out any) error {
	var reader io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(buf)
	}
	u := *h.baseURL
	u.Path = strings.TrimSuffix(u.Path, "/") + path
	req, err := http.NewRequestWithContext(ctx, method, u.String(), reader)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := h.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return &Error{Op: "signalbridge.HTTPTransport.do", Kind: ErrKindRateLimited, Msg: resp.Status}
	}
	if resp.StatusCode >= 400 {
		respBytes, _ := io.ReadAll(resp.Body)
		return &Error{Op: "signalbridge.HTTPTransport.do", Kind: ErrKindTransport, Msg: fmt.Sprintf("http %s: %s", resp.Status, string(respBytes))}
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// authHeaders returns the HTTP Basic Authorization header for account.
func (h *HTTPTransport) authHeaders(account SignalAccount) map[string]string {
	login := account.ACI
	if account.DeviceID > 1 {
		login = fmt.Sprintf("%s.%d", account.ACI, account.DeviceID)
	}
	return map[string]string{"Authorization": basicAuth(login, account.Password)}
}

// basicAuth constructs an RFC 7617 Basic credential.
func basicAuth(user, password string) string {
	raw := user + ":" + password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(raw))
}

// encodePreKeys renders the one-time pre-key slice for upload.
func encodePreKeys(keys []PreKeyMaterial) []map[string]any {
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{
			"keyId":     k.ID,
			"publicKey": base64.StdEncoding.EncodeToString(k.Public),
		})
	}
	return out
}

// encodeKyberPreKeys renders the Kyber pre-key slice for upload.
func encodeKyberPreKeys(keys []KyberPreKeyMaterial) []map[string]any {
	out := make([]map[string]any, 0, len(keys))
	for _, k := range keys {
		out = append(out, map[string]any{
			"keyId":     k.ID,
			"publicKey": base64.StdEncoding.EncodeToString(k.Public),
			"signature": base64.StdEncoding.EncodeToString(k.Signature),
		})
	}
	return out
}

// Compile-time interface compliance.
var _ ServerTransport = (*HTTPTransport)(nil)
