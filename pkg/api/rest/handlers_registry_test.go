//go:build rest

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
	"context"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/registry"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// stubRegistryService records the request each handler passed in.
type stubRegistryService struct {
	gotRegister   *service.RegisterServiceInstanceRequest
	gotRenew      *service.RenewServiceLeaseRequest
	gotDeregister *service.DeregisterServiceInstanceRequest
	gotResolve    *service.ResolveServiceRequest
	gotWatch      *service.WatchServiceRequest

	events chan service.RegistryEvent
	err    error
}

func (s *stubRegistryService) Register(_ context.Context, req *service.RegisterServiceInstanceRequest) (*service.RegisterServiceInstanceResponse, error) {
	s.gotRegister = req
	if s.err != nil {
		return nil, s.err
	}
	return &service.RegisterServiceInstanceResponse{LeaseID: 9007199254740993, TTLSeconds: req.TTLSeconds}, nil
}

func (s *stubRegistryService) Renew(_ context.Context, req *service.RenewServiceLeaseRequest) (*service.RenewServiceLeaseResponse, error) {
	s.gotRenew = req
	if s.err != nil {
		return nil, s.err
	}
	return &service.RenewServiceLeaseResponse{LeaseID: req.LeaseID, TTLSeconds: 10}, nil
}

func (s *stubRegistryService) Deregister(_ context.Context, req *service.DeregisterServiceInstanceRequest) error {
	s.gotDeregister = req
	return s.err
}

func (s *stubRegistryService) Resolve(_ context.Context, req *service.ResolveServiceRequest) (*service.ResolveServiceResponse, error) {
	s.gotResolve = req
	if s.err != nil {
		return nil, s.err
	}
	return &service.ResolveServiceResponse{
		Instances: []service.RegistryServiceInstance{{ID: "a", Service: req.Service, Address: "10.0.0.1:80"}},
		Revision:  12,
	}, nil
}

func (s *stubRegistryService) Watch(_ context.Context, req *service.WatchServiceRequest) (<-chan service.RegistryEvent, error) {
	s.gotWatch = req
	if s.err != nil {
		return nil, s.err
	}
	return s.events, nil
}

// registryRouter mounts the registry handlers on a chi router so path
// parameters resolve exactly as they do on the live server.
func registryRouter(t *testing.T, stub service.RegistryService) http.Handler {
	t.Helper()
	server, mockSvc, cleanup := setupMockServer(t)
	t.Cleanup(cleanup)
	if stub != nil {
		mockSvc.RegistrySvc = stub
	}
	r := chi.NewRouter()
	r.Post("/api/v1/registry/instances", server.HandleRegistryRegister)
	r.Post("/api/v1/registry/leases/{lease_id}/renew", server.HandleRegistryRenew)
	r.Delete("/api/v1/registry/leases/{lease_id}", server.HandleRegistryDeregister)
	r.Get("/api/v1/registry/services/{service}", server.HandleRegistryResolve)
	r.Get("/api/v1/registry/services/{service}/watch", server.HandleRegistryWatch)
	return r
}

func serveRegistry(handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	return w
}

func TestHandleRegistryRegister(t *testing.T) {
	stub := &stubRegistryService{}
	w := serveRegistry(registryRouter(t, stub), http.MethodPost, "/api/v1/registry/instances",
		`{"instance":{"id":"i","service":"api","address":"10.0.0.2:443","metadata":{"zone":"z"}},"ttl_seconds":20}`)

	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	require.NotNil(t, stub.gotRegister)
	assert.Equal(t, service.RegistryServiceInstance{ID: "i", Service: "api", Address: "10.0.0.2:443", Metadata: map[string]string{"zone": "z"}}, stub.gotRegister.Instance)
	assert.Equal(t, int64(20), stub.gotRegister.TTLSeconds)
	// A lease ID above 2^53 survives because it travels as a JSON string.
	assert.Contains(t, w.Body.String(), `"lease_id":"9007199254740993"`)
}

func TestHandleRegistryRegister_Errors(t *testing.T) {
	w := serveRegistry(registryRouter(t, &stubRegistryService{}), http.MethodPost, "/api/v1/registry/instances", "{not json")
	assert.Equal(t, http.StatusBadRequest, w.Code)

	invalid := &qberrors.QBError{Code: qberrors.ErrInvalidInput, Op: "x", Err: &registry.InvalidInstanceError{Reason: "address is required"}}
	w = serveRegistry(registryRouter(t, &stubRegistryService{err: invalid}), http.MethodPost, "/api/v1/registry/instances", `{"ttl_seconds":5}`)
	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "address is required")
}

func TestHandleRegistryRenewAndDeregister(t *testing.T) {
	stub := &stubRegistryService{}
	router := registryRouter(t, stub)

	w := serveRegistry(router, http.MethodPost, "/api/v1/registry/leases/77/renew", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, int64(77), stub.gotRenew.LeaseID)
	var renewed service.RenewServiceLeaseResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &renewed))
	assert.Equal(t, int64(10), renewed.TTLSeconds)

	w = serveRegistry(router, http.MethodDelete, "/api/v1/registry/leases/78", "")
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, int64(78), stub.gotDeregister.LeaseID)

	for _, path := range []string{"/api/v1/registry/leases/abc/renew", "/api/v1/registry/leases/0/renew", "/api/v1/registry/leases/-4/renew"} {
		w = serveRegistry(router, http.MethodPost, path, "")
		assert.Equal(t, http.StatusBadRequest, w.Code, path)
	}
}

func TestHandleRegistry_NotFoundLease(t *testing.T) {
	notFound := &qberrors.QBError{Code: qberrors.ErrNotFound, Op: "x", Err: &registry.LeaseNotFoundError{LeaseID: 5}}
	router := registryRouter(t, &stubRegistryService{err: notFound})
	assert.Equal(t, http.StatusNotFound, serveRegistry(router, http.MethodPost, "/api/v1/registry/leases/5/renew", "").Code)
	assert.Equal(t, http.StatusNotFound, serveRegistry(router, http.MethodDelete, "/api/v1/registry/leases/5", "").Code)
}

func TestHandleRegistryResolve(t *testing.T) {
	stub := &stubRegistryService{}
	w := serveRegistry(registryRouter(t, stub), http.MethodGet, "/api/v1/registry/services/api", "")
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.Equal(t, "api", stub.gotResolve.Service)
	var resp service.ResolveServiceResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, int64(12), resp.Revision)
	require.Len(t, resp.Instances, 1)
	assert.Equal(t, "10.0.0.1:80", resp.Instances[0].Address)

	down := &qberrors.QBError{Code: qberrors.ErrUnavailable, Op: "x", Err: stderrors.New("store down")}
	w = serveRegistry(registryRouter(t, &stubRegistryService{err: down}), http.MethodGet, "/api/v1/registry/services/api", "")
	assert.Equal(t, http.StatusServiceUnavailable, w.Code)
}

func TestHandleRegistry_ServiceUnavailable(t *testing.T) {
	router := registryRouter(t, nil)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/registry/instances"},
		{http.MethodPost, "/api/v1/registry/leases/1/renew"},
		{http.MethodDelete, "/api/v1/registry/leases/1"},
		{http.MethodGet, "/api/v1/registry/services/api"},
		{http.MethodGet, "/api/v1/registry/services/api/watch"},
	} {
		assert.Equal(t, http.StatusServiceUnavailable, serveRegistry(router, c.method, c.path, "{}").Code, c.path)
	}
}

// TestHandleRegistryWatch_StreamsEventsThenTheTerminalError reads the SSE
// body a client receives: one frame per change, then the error frame that
// tells it to resolve again.
func TestHandleRegistryWatch_StreamsEventsThenTheTerminalError(t *testing.T) {
	events := make(chan service.RegistryEvent, 3)
	events <- service.RegistryEvent{Type: service.RegistryEventPut, Instance: service.RegistryServiceInstance{ID: "a", Service: "api", Address: "x:1"}, Revision: 5}
	events <- service.RegistryEvent{Type: service.RegistryEventDelete, Instance: service.RegistryServiceInstance{ID: "a", Service: "api"}, Revision: 6}
	events <- service.RegistryEvent{Err: &registry.WatchCompactedError{Service: "api", FromRevision: 4, Err: stderrors.New("c")}}
	close(events)
	stub := &stubRegistryService{events: events}

	w := serveRegistry(registryRouter(t, stub), http.MethodGet, "/api/v1/registry/services/api/watch?from_revision=4", "")
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Equal(t, int64(4), stub.gotWatch.FromRevision)
	assert.Equal(t, "api", stub.gotWatch.Service)

	body := w.Body.String()
	assert.Contains(t, body, "id: 5\nevent: registry\ndata: {\"type\":\"PUT\",\"instance\":{\"id\":\"a\",\"service\":\"api\",\"address\":\"x:1\"},\"revision\":5}\n\n")
	assert.Contains(t, body, "id: 6\nevent: registry\ndata: {\"type\":\"DELETE\"")
	assert.Contains(t, body, "event: error\ndata: {\"code\":\""+RegistryWatchCompactedCode+"\"")
}

func TestHandleRegistryWatch_OpenErrors(t *testing.T) {
	w := serveRegistry(registryRouter(t, &stubRegistryService{}), http.MethodGet, "/api/v1/registry/services/api/watch?from_revision=x", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
	w = serveRegistry(registryRouter(t, &stubRegistryService{}), http.MethodGet, "/api/v1/registry/services/api/watch?from_revision=-1", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)

	compacted := &qberrors.QBError{Code: qberrors.ErrOutOfRange, Op: "x", Err: &registry.WatchCompactedError{Service: "api", FromRevision: 2, Err: stderrors.New("c")}}
	w = serveRegistry(registryRouter(t, &stubRegistryService{err: compacted}), http.MethodGet, "/api/v1/registry/services/api/watch?from_revision=2", "")
	assert.Equal(t, http.StatusGone, w.Code)
	var body RegistryWatchErrorREST
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, RegistryWatchCompactedCode, body.Code)

	invalid := &qberrors.QBError{Code: qberrors.ErrInvalidInput, Op: "x", Err: &registry.InvalidInstanceError{Reason: "service must not contain '/'"}}
	w = serveRegistry(registryRouter(t, &stubRegistryService{err: invalid}), http.MethodGet, "/api/v1/registry/services/api/watch", "")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestServeRegistryWatchSSE_HeartbeatAndOtherEndings(t *testing.T) {
	// An idle stream emits comment frames until the client leaves.
	ctx, cancel := context.WithCancel(context.Background())
	pr, pw := io.Pipe()
	w := &pipeResponseWriter{header: http.Header{}, out: pw}
	done := make(chan error, 1)
	go func() {
		serveErr := serveRegistryWatchSSE(ctx, w, make(chan service.RegistryEvent), 10*time.Millisecond)
		done <- stderrors.Join(serveErr, pw.Close())
	}()
	buf := make([]byte, 64)
	n, err := pr.Read(buf)
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(string(buf[:n]), ": ping "), "first frame %q is a heartbeat", buf[:n])
	cancel()
	drained := make(chan error, 1)
	go func() {
		_, copyErr := io.Copy(io.Discard, pr)
		drained <- copyErr
	}()
	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not end when the client left")
	}
	assert.NoError(t, <-drained)

	// A store-ended watch other than compaction reports watch_ended.
	ended := make(chan service.RegistryEvent, 1)
	ended <- service.RegistryEvent{Err: &registry.WatchEndedError{Service: "api", Err: stderrors.New("evicted")}}
	rec := httptest.NewRecorder()
	require.NoError(t, serveRegistryWatchSSE(context.Background(), rec, ended, time.Hour))
	assert.Contains(t, rec.Body.String(), `"code":"`+RegistryWatchEndedCode+`"`)

	// A write failure ends the stream with that failure.
	failing := make(chan service.RegistryEvent, 1)
	failing <- service.RegistryEvent{Type: service.RegistryEventPut, Revision: 1}
	writeErr := stderrors.New("connection reset")
	err = serveRegistryWatchSSE(context.Background(), &failingResponseWriter{header: http.Header{}, err: writeErr}, failing, time.Hour)
	assert.ErrorIs(t, err, writeErr)
}

// pipeResponseWriter streams a response body through a pipe so a test can read
// frames as they are written.
type pipeResponseWriter struct {
	header http.Header
	out    *io.PipeWriter
}

func (p *pipeResponseWriter) Header() http.Header         { return p.header }
func (p *pipeResponseWriter) Write(b []byte) (int, error) { return p.out.Write(b) }
func (p *pipeResponseWriter) WriteHeader(int)             {}

// failingResponseWriter accepts headers and fails every body write.
type failingResponseWriter struct {
	header http.Header
	err    error
}

func (f *failingResponseWriter) Header() http.Header       { return f.header }
func (f *failingResponseWriter) Write([]byte) (int, error) { return 0, f.err }
func (f *failingResponseWriter) WriteHeader(int)           {}
