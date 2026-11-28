package rest

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const auditPageJSON = `{"events":[{"actor":"alice","tenant_id":"t1","client_ip":"10.0.0.1","request_id":"r1","prev_hash":"h0","attributes":{"k":"v"}}],"next":"c2","total":1}`

func TestQRDBAuditClient_QueryAudit_MethodPathQueryAndBearer(t *testing.T) {
	var (
		mu                          sync.Mutex
		hits                        int
		method, path, query, bearer string
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		method, path, query = r.Method, r.URL.Path, r.URL.RawQuery
		bearer = r.Header.Get("Authorization")
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(auditPageJSON))
	}))
	t.Cleanup(srv.Close)

	tr, err := New(&transport.Config{Address: srv.URL, MaxRetries: 0}, WithAuthToken("tok-audit"))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	page, err := tr.QRDBAudit().QueryAudit(context.Background(), qrdbtransport.AuditFilter{Actor: "alice", TenantID: "t1", Limit: 5})
	if err != nil {
		t.Fatalf("QueryAudit: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if hits != 1 {
		t.Fatalf("requests reaching the server: got %d, want 1", hits)
	}
	if method != http.MethodGet {
		t.Errorf("method: got %s, want GET", method)
	}
	if path != "/v1/audit/events" {
		t.Errorf("path: got %s, want /v1/audit/events", path)
	}
	if want := "actor=alice&limit=5&tenant_id=t1"; query != want {
		t.Errorf("query: got %q, want %q", query, want)
	}
	if want := "Bearer tok-audit"; bearer != want {
		t.Errorf("Authorization: got %q, want %q", bearer, want)
	}

	want := &qrdbtransport.AuditPage{
		Events: []qrdbtransport.AuditEvent{{
			Actor:      "alice",
			TenantID:   "t1",
			ClientIP:   "10.0.0.1",
			RequestID:  "r1",
			PrevHash:   "h0",
			Attributes: map[string]any{"k": "v"},
		}},
		Next:  "c2",
		Total: 1,
	}
	if !reflect.DeepEqual(page, want) {
		t.Errorf("page:\n got %+v\nwant %+v", page, want)
	}
}

func TestQRDBAuditClient_EmitAudit_NotImplemented(t *testing.T) {
	tr, err := New(&transport.Config{Address: "http://127.0.0.1:1", MaxRetries: 0})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	err = tr.QRDBAudit().EmitAudit(context.Background(), qrdbtransport.AuditEvent{Actor: "a"})
	var ni *qrdbtransport.NotImplementedError
	if !errors.As(err, &ni) {
		t.Fatalf("EmitAudit: got %v, want *NotImplementedError", err)
	}
}
