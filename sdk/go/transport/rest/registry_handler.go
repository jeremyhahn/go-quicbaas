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

package rest

import (
	"bufio"
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	registryclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/registry"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// The service registry over REST (and QUIC, which is this REST dialect over
// HTTP/3). The routes are quicbaasd's /api/v1/registry mount
// (pkg/api/rest/handlers_registry.go); the watch is its Server-Sent Events
// stream.

const (
	// registryWatchCompactedCode and registryWatchEndedCode are the error
	// codes the server's watch reports (pkg/api/rest RegistryWatch*Code).
	registryWatchCompactedCode = "watch_compacted"
	registryWatchEndedCode     = "watch_ended"
)

// isRegistryService reports whether the service name targets the service
// registry.
func isRegistryService(service string) bool {
	return strings.HasPrefix(service, registryclient.ServiceName)
}

// registryMethods dispatches the registry's unary verbs.
var registryMethods = map[string]func(*Transport, context.Context, interface{}) (interface{}, error){
	"Register":   (*Transport).registryRegister,
	"Renew":      (*Transport).registryRenew,
	"Deregister": (*Transport).registryDeregister,
	"Resolve":    (*Transport).registryResolve,
}

// handleRegistryRequest dispatches a unary registry verb.
func (t *Transport) handleRegistryRequest(ctx context.Context, method string, req interface{}) (interface{}, error) {
	fn, ok := registryMethods[method]
	if !ok {
		return nil, errors.NewUnimplemented("rest.registry."+method, "method not exposed by the registry REST surface")
	}
	return fn(t, ctx, req)
}

// registryRegister calls POST /api/v1/registry/instances.
func (t *Transport) registryRegister(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*registryclient.RegisterRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.registry.Register", "invalid request type, expected *registry.RegisterRequest")
	}
	var resp registryclient.RegisterResponse
	if err := t.doJSONRequest(ctx, http.MethodPost, "/api/v1/registry/instances", r, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// registryRenew calls POST /api/v1/registry/leases/{lease_id}/renew.
func (t *Transport) registryRenew(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*registryclient.RenewRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.registry.Renew", "invalid request type, expected *registry.RenewRequest")
	}
	var resp registryclient.RenewResponse
	path := "/api/v1/registry/leases/" + strconv.FormatInt(r.LeaseID, 10) + "/renew"
	if err := t.doJSONRequest(ctx, http.MethodPost, path, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// registryDeregister calls DELETE /api/v1/registry/leases/{lease_id}.
func (t *Transport) registryDeregister(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*registryclient.DeregisterRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.registry.Deregister", "invalid request type, expected *registry.DeregisterRequest")
	}
	path := "/api/v1/registry/leases/" + strconv.FormatInt(r.LeaseID, 10)
	if err := t.doJSONRequest(ctx, http.MethodDelete, path, nil, nil); err != nil {
		return nil, err
	}
	return nil, nil
}

// registryResolve calls GET /api/v1/registry/services/{service}.
func (t *Transport) registryResolve(ctx context.Context, req interface{}) (interface{}, error) {
	r, ok := req.(*registryclient.ResolveRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("rest.registry.Resolve", "invalid request type, expected *registry.ResolveRequest")
	}
	var resp registryclient.ResolveResponse
	if err := t.doJSONRequest(ctx, http.MethodGet, "/api/v1/registry/services/"+url.PathEscape(r.Service), nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// openRegistryWatch opens the watch's SSE stream.
func (t *Transport) openRegistryWatch(ctx context.Context, method string, req interface{}) (transport.Stream, error) {
	const op = "rest.registry.Watch"
	if method != "Watch" {
		return nil, errors.NewUnimplemented("rest.registry."+method, "not a registry streaming method")
	}
	r, ok := req.(*registryclient.WatchRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput(op, "invalid request type, expected *registry.WatchRequest")
	}
	if t.closed.Load() {
		return nil, errors.NewConnection(op, "transport is closed")
	}

	target := t.baseURL + t.basePath + "/api/v1/registry/services/" + url.PathEscape(r.Service) +
		"/watch?from_revision=" + strconv.FormatInt(r.FromRevision, 10)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, errors.Wrap(err, errors.CodeInternal, op, "failed to create HTTP request")
	}
	httpReq.Header.Set("Accept", "text/event-stream")
	t.mu.RLock()
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}
	t.mu.RUnlock()

	// The stream outlives the transport's unary request timeout, so it rides
	// the deadline-free stream client; the caller's context bounds it.
	resp, err := t.streamHTTPClient().Do(httpReq)
	if err != nil {
		return nil, t.convertHTTPError(err, op)
	}
	if resp.StatusCode >= 400 {
		body, readErr := io.ReadAll(resp.Body)
		closeErr := resp.Body.Close()
		if readErr != nil || closeErr != nil {
			return nil, errors.Wrap(stderrors.Join(readErr, closeErr), errors.CodeTransport, op, "failed to read the refusal")
		}
		if resp.StatusCode == http.StatusGone {
			return nil, &registryclient.WatchCompactedError{Service: r.Service, FromRevision: r.FromRevision, Message: registryWatchMessage(body)}
		}
		return nil, t.parseErrorResponse(resp.StatusCode, body)
	}
	return &registryWatchSSEStream{req: r, body: resp.Body, reader: bufio.NewReaderSize(resp.Body, 64*1024)}, nil
}

// registryWatchErrorFrame is the server's terminal `event: error` payload and
// its HTTP 410 body.
type registryWatchErrorFrame struct {
	Code  string `json:"code"`
	Error string `json:"error"`
}

// registryWatchMessage extracts the description from an error frame or body,
// falling back to the raw text.
func registryWatchMessage(body []byte) string {
	var frame registryWatchErrorFrame
	if err := json.Unmarshal(body, &frame); err == nil && frame.Error != "" {
		return frame.Error
	}
	return string(body)
}

// registryWatchSSEStream adapts the watch's SSE body to transport.Stream.
// Resumption after a dropped connection is the registry client's job (it
// reopens from the next undelivered revision), so this adapter only decodes.
type registryWatchSSEStream struct {
	req    *registryclient.WatchRequest
	body   io.ReadCloser
	reader *bufio.Reader

	closeOnce sync.Once
	closeErr  error
	closed    atomic.Bool
}

// Send is not supported: the watch is server-to-client only.
func (s *registryWatchSSEStream) Send(interface{}) error {
	return errors.NewUnimplemented("rest.registry.Watch", "the watch stream does not accept client messages")
}

// CloseSend is a no-op: there is no client-to-server half.
func (s *registryWatchSSEStream) CloseSend() error { return nil }

// Close releases the response body, unblocking a read in progress.
func (s *registryWatchSSEStream) Close() error {
	s.closeOnce.Do(func() {
		s.closed.Store(true)
		s.closeErr = s.body.Close()
	})
	return s.closeErr
}

// Recv decodes the next `event: registry` frame. Keep-alive comments are
// skipped; the server's `event: error` frame becomes the typed watch error;
// the end of the body is io.EOF.
func (s *registryWatchSSEStream) Recv() (interface{}, error) {
	for {
		eventType, data, err := readSSEFrame(s.reader)
		if err != nil {
			if s.closed.Load() || stderrors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			return nil, errors.Wrap(err, errors.CodeConnection, "rest.registry.Watch", "watch stream read failed")
		}
		if data == "" {
			continue
		}
		if eventType == "error" {
			return nil, s.terminalError(data)
		}
		var ev registryclient.Event
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			return nil, errors.Wrap(err, errors.CodeInternal, "rest.registry.Watch", "failed to decode watch event")
		}
		return &ev, nil
	}
}

// terminalError converts the server's error frame into the typed error.
func (s *registryWatchSSEStream) terminalError(data string) error {
	var frame registryWatchErrorFrame
	if err := json.Unmarshal([]byte(data), &frame); err != nil {
		return errors.Wrap(err, errors.CodeInternal, "rest.registry.Watch", "failed to decode watch error frame")
	}
	if frame.Code == registryWatchCompactedCode {
		return &registryclient.WatchCompactedError{Service: s.req.Service, FromRevision: s.req.FromRevision, Message: frame.Error}
	}
	return &registryclient.WatchEndedError{Service: s.req.Service, Message: frame.Error}
}
