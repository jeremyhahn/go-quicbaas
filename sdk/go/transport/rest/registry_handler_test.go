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
	stderrors "errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	registryclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/registry"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// registryRecorder records what the server received.
type registryRecorder struct {
	mu     sync.Mutex
	method string
	path   string
	query  string
	accept string
	body   string
}

func (r *registryRecorder) record(req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		body = []byte("read error: " + err.Error())
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.method, r.path, r.query, r.accept, r.body = req.Method, req.URL.EscapedPath(), req.URL.RawQuery, req.Header.Get("Accept"), string(body)
}

func registryServer(t *testing.T, rec *registryRecorder, status int, contentType, reply string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.record(r)
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		if _, err := io.WriteString(w, reply); err != nil {
			t.Errorf("write reply: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRegistryREST_Register(t *testing.T) {
	rec := &registryRecorder{}
	tr := newTestTransport(t, registryServer(t, rec, http.StatusCreated, "application/json", `{"lease_id":"9007199254740993","ttl_seconds":15}`))
	defer tr.Close()

	out, err := tr.Request(context.Background(), registryclient.ServiceName, "Register", &registryclient.RegisterRequest{
		Instance:   registryclient.ServiceInstance{ID: "i", Service: "api", Address: "10.0.0.9:443"},
		TTLSeconds: 15,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if lease := out.(*registryclient.RegisterResponse); lease.LeaseID != 9007199254740993 || lease.TTLSeconds != 15 {
		t.Fatalf("Register = %+v", lease)
	}
	if rec.method != http.MethodPost || rec.path != "/api/v1/registry/instances" {
		t.Fatalf("server received %s %s", rec.method, rec.path)
	}
	want := `{"instance":{"id":"i","service":"api","address":"10.0.0.9:443"},"ttl_seconds":15}`
	if rec.body != want {
		t.Fatalf("server received body %s, want %s", rec.body, want)
	}
}

func TestRegistryREST_RenewDeregisterResolve(t *testing.T) {
	rec := &registryRecorder{}
	tr := newTestTransport(t, registryServer(t, rec, http.StatusOK, "application/json", `{"lease_id":"77","ttl_seconds":9}`))
	defer tr.Close()
	ctx := context.Background()

	out, err := tr.Request(ctx, registryclient.ServiceName, "Renew", &registryclient.RenewRequest{LeaseID: 77})
	if err != nil || out.(*registryclient.RenewResponse).TTLSeconds != 9 {
		t.Fatalf("Renew = %+v, %v", out, err)
	}
	if rec.method != http.MethodPost || rec.path != "/api/v1/registry/leases/77/renew" {
		t.Fatalf("Renew sent %s %s", rec.method, rec.path)
	}

	del := &registryRecorder{}
	tr2 := newTestTransport(t, registryServer(t, del, http.StatusNoContent, "application/json", ""))
	defer tr2.Close()
	if _, err := tr2.Request(ctx, registryclient.ServiceName, "Deregister", &registryclient.DeregisterRequest{LeaseID: 78}); err != nil {
		t.Fatalf("Deregister: %v", err)
	}
	if del.method != http.MethodDelete || del.path != "/api/v1/registry/leases/78" {
		t.Fatalf("Deregister sent %s %s", del.method, del.path)
	}

	res := &registryRecorder{}
	tr3 := newTestTransport(t, registryServer(t, res, http.StatusOK, "application/json",
		`{"instances":[{"id":"a","service":"my svc","address":"x:1","metadata":{"k":"v"}}],"revision":31}`))
	defer tr3.Close()
	out, err = tr3.Request(ctx, registryclient.ServiceName, "Resolve", &registryclient.ResolveRequest{Service: "my svc"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	resolved := out.(*registryclient.ResolveResponse)
	if resolved.Revision != 31 || len(resolved.Instances) != 1 || resolved.Instances[0].Metadata["k"] != "v" {
		t.Fatalf("Resolve = %+v", resolved)
	}
	if res.method != http.MethodGet || res.path != "/api/v1/registry/services/my%20svc" {
		t.Fatalf("Resolve sent %s %s, want the service name path-escaped", res.method, res.path)
	}
}

func TestRegistryREST_UnaryErrors(t *testing.T) {
	rec := &registryRecorder{}
	tr := newTestTransport(t, registryServer(t, rec, http.StatusNotFound, "application/json", `{"error":"lease 5 does not back a registration of this tenant","code":"NOT_FOUND"}`))
	defer tr.Close()
	ctx := context.Background()
	if _, err := tr.Request(ctx, registryclient.ServiceName, "Renew", &registryclient.RenewRequest{LeaseID: 5}); !sdkerrors.IsNotFound(err) {
		t.Fatalf("Renew = %v, want not found", err)
	}
	for _, method := range []string{"Register", "Renew", "Deregister", "Resolve"} {
		if _, err := tr.Request(ctx, registryclient.ServiceName, method, "wrong"); !sdkerrors.IsInvalidInput(err) {
			t.Fatalf("%s wrong type = %v", method, err)
		}
	}
	if _, err := tr.Request(ctx, registryclient.ServiceName, "Quarantine", nil); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("unknown method = %v", err)
	}
}

func TestRegistryREST_WatchStream(t *testing.T) {
	sse := ": ping 1\n\n" +
		"id: 8\nevent: registry\ndata: {\"type\":\"PUT\",\"instance\":{\"id\":\"a\",\"service\":\"api\",\"address\":\"x:1\"},\"revision\":8}\n\n" +
		"id: 9\nevent: registry\ndata: {\"type\":\"DELETE\",\"instance\":{\"id\":\"a\",\"service\":\"api\"},\"revision\":9}\n\n" +
		"event: error\ndata: {\"code\":\"watch_compacted\",\"error\":\"history gone\"}\n\n"
	rec := &registryRecorder{}
	tr := newTestTransport(t, registryServer(t, rec, http.StatusOK, "text/event-stream", sse))
	defer tr.Close()

	stream, err := tr.RequestStream(context.Background(), registryclient.ServiceName, "Watch", &registryclient.WatchRequest{Service: "api", FromRevision: 8})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer func() {
		if err := stream.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	}()
	if rec.path != "/api/v1/registry/services/api/watch" || rec.query != "from_revision=8" || rec.accept != "text/event-stream" {
		t.Fatalf("server received %s?%s (Accept %q)", rec.path, rec.query, rec.accept)
	}
	for _, want := range []struct {
		typ registryclient.EventType
		rev int64
	}{{registryclient.EventPut, 8}, {registryclient.EventDelete, 9}} {
		msg, err := stream.Recv()
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		if ev := msg.(*registryclient.Event); ev.Type != want.typ || ev.Revision != want.rev {
			t.Fatalf("event = %+v, want %s@%d", ev, want.typ, want.rev)
		}
	}
	_, err = stream.Recv()
	var compacted *registryclient.WatchCompactedError
	if !stderrors.As(err, &compacted) || compacted.Message != "history gone" || compacted.FromRevision != 8 {
		t.Fatalf("end = %v, want *WatchCompactedError", err)
	}
}

func TestRegistryREST_WatchEndings(t *testing.T) {
	ended := registryServer(t, &registryRecorder{}, http.StatusOK, "text/event-stream",
		"event: error\ndata: {\"code\":\"watch_ended\",\"error\":\"evicted\"}\n\n")
	tr := newTestTransport(t, ended)
	defer tr.Close()
	stream, err := tr.RequestStream(context.Background(), registryclient.ServiceName, "Watch", &registryclient.WatchRequest{Service: "api", FromRevision: 1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	var endedErr *registryclient.WatchEndedError
	if _, err := stream.Recv(); !stderrors.As(err, &endedErr) || endedErr.Message != "evicted" {
		t.Fatalf("end = %v, want *WatchEndedError", err)
	}

	eof := registryServer(t, &registryRecorder{}, http.StatusOK, "text/event-stream", "")
	tr2 := newTestTransport(t, eof)
	defer tr2.Close()
	stream, err = tr2.RequestStream(context.Background(), registryclient.ServiceName, "Watch", &registryclient.WatchRequest{Service: "api", FromRevision: 1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("drained body = %v, want io.EOF so the client resumes", err)
	}
}

func TestRegistryREST_WatchOpenRefusals(t *testing.T) {
	gone := registryServer(t, &registryRecorder{}, http.StatusGone, "application/json", `{"code":"watch_compacted","error":"history gone"}`)
	tr := newTestTransport(t, gone)
	defer tr.Close()
	_, err := tr.RequestStream(context.Background(), registryclient.ServiceName, "Watch", &registryclient.WatchRequest{Service: "api", FromRevision: 2})
	var compacted *registryclient.WatchCompactedError
	if !stderrors.As(err, &compacted) || compacted.Message != "history gone" {
		t.Fatalf("410 = %v, want *WatchCompactedError", err)
	}

	forbidden := registryServer(t, &registryRecorder{}, http.StatusForbidden, "application/json", `{"error":"permission denied"}`)
	tr2 := newTestTransport(t, forbidden)
	defer tr2.Close()
	if _, err := tr2.RequestStream(context.Background(), registryclient.ServiceName, "Watch", &registryclient.WatchRequest{Service: "api", FromRevision: 2}); !sdkerrors.IsForbidden(err) {
		t.Fatalf("403 = %v, want forbidden", err)
	}
	if _, err := tr2.RequestStream(context.Background(), registryclient.ServiceName, "Resolve", nil); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("non-stream method = %v", err)
	}
	if _, err := tr2.RequestStream(context.Background(), registryclient.ServiceName, "Watch", "wrong"); !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("wrong type = %v", err)
	}
}

func TestRegistryWatchSSEStream_Decoding(t *testing.T) {
	bad := registryServer(t, &registryRecorder{}, http.StatusOK, "text/event-stream", "event: registry\ndata: {not json\n\n")
	tr := newTestTransport(t, bad)
	defer tr.Close()
	stream, err := tr.RequestStream(context.Background(), registryclient.ServiceName, "Watch", &registryclient.WatchRequest{Service: "api", FromRevision: 1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := stream.Recv(); sdkerrors.Code(err) != sdkerrors.CodeInternal {
		t.Fatalf("malformed event = %v, want internal", err)
	}
	if err := stream.Send(nil); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("Send = %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("CloseSend = %v", err)
	}
	if registryWatchMessage([]byte("plain")) != "plain" {
		t.Fatal("a non-JSON body is reported verbatim")
	}
	badFrame := &registryWatchSSEStream{req: &registryclient.WatchRequest{Service: "api"}}
	if err := badFrame.terminalError("{nope"); sdkerrors.Code(err) != sdkerrors.CodeInternal {
		t.Fatalf("malformed error frame = %v", err)
	}
}
