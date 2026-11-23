package mcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// pinCounter wraps next and counts the requests whose body carries the SO
// PIN, and all requests.
type pinCounter struct {
	next        http.Handler
	withPIN     atomic.Int32
	withUserPIN atomic.Int32
	soPINHeader atomic.Int32
	all         atomic.Int32
}

func (p *pinCounter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.all.Add(1)
	body, _ := io.ReadAll(r.Body)
	if bytes.Contains(body, []byte(testSOPIN)) {
		p.withPIN.Add(1)
	}
	if bytes.Contains(body, []byte(testUserPIN)) {
		p.withUserPIN.Add(1)
	}
	if r.Header.Get(qrdbtransport.SOPINHeader) != "" {
		p.soPINHeader.Add(1)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	if p.next == nil {
		w.WriteHeader(http.StatusOK)
		return
	}
	p.next.ServeHTTP(w, r)
}

// mcpHandler is the fake MCP server the barrier tests use.
func mcpHandler(t *testing.T) http.Handler {
	t.Helper()
	got := map[string]map[string]interface{}{}
	reply := map[string]interface{}{"threshold": 2, "total_shares": 2, "total": 2, "share_count": 2}
	srv, _ := newMultiToolServer(t, barrierPINToolNames, recordArgs(got, reply))
	return mcpserver.NewStreamableHTTPServer(srv)
}

// redirectTool serves MCP from next, but answers a call of the Shamir
// initialize tool with a 307 to location.
func redirectTool(next http.Handler, location string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte("qrdb_barrier_shamir_initialize")) {
			http.Redirect(w, r, location+r.URL.Path, http.StatusTemporaryRedirect)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}

func shamirInit(ctx context.Context, tr *Transport) error {
	_, err := tr.BarrierInitializeShamir(ctx, &qrdbtransport.BarrierInitializeShamirRequest{
		UserPin: testUserPIN, Secret: []byte("pw"), Threshold: 2, TotalShares: 2,
	})
	return err
}

// A bound SO PIN never leaves the process over plaintext http.
func TestSOPINHop_PlainEndpointRefused(t *testing.T) {
	verbs := map[string]func(context.Context, *Transport) error{
		"BarrierInitializeShamir": shamirInit,
		"BarrierRekey": func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierRekey(ctx, &qrdbtransport.BarrierRekeyRequest{Threshold: 2, Total: 2})
			return err
		},
		"BarrierGenerateRecoveryKeys": func(ctx context.Context, tr *Transport) error {
			_, err := tr.BarrierGenerateRecoveryKeys(ctx, &qrdbtransport.BarrierGenerateRecoveryKeysRequest{Threshold: 2, Total: 2})
			return err
		},
	}
	for name, call := range verbs {
		t.Run(name, func(t *testing.T) {
			counter := &pinCounter{next: mcpHandler(t)}
			srv := httptest.NewServer(counter)
			t.Cleanup(srv.Close)
			tr, err := New(&transport.Config{Address: srv.URL, Timeout: 5 * time.Second})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = tr.Close() })

			err = call(boundSOPIN(t), tr)
			var tlsErr *qrdbtransport.SOPINRequiresTLSError
			if !errors.As(err, &tlsErr) {
				t.Fatalf("%s over http: err = %v, want *SOPINRequiresTLSError", name, err)
			}
			if n := counter.withPIN.Load(); n != 0 {
				t.Fatalf("plain server received %d requests carrying the SO PIN, want 0", n)
			}
		})
	}
}

// A bound SO PIN does not follow a redirect from the https endpoint to a
// plaintext host.
func TestSOPINHop_RedirectToPlainRefused(t *testing.T) {
	plain := &pinCounter{}
	plainSrv := httptest.NewServer(plain)
	t.Cleanup(plainSrv.Close)

	tlsSrv := httptest.NewTLSServer(redirectTool(mcpHandler(t), plainSrv.URL))
	t.Cleanup(tlsSrv.Close)
	tr, err := New(&transport.Config{Address: tlsSrv.URL, Timeout: 5 * time.Second},
		WithHTTPClient(tlsSrv.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	err = shamirInit(boundSOPIN(t), tr)
	var tlsErr *qrdbtransport.SOPINRequiresTLSError
	if !errors.As(err, &tlsErr) {
		t.Fatalf("redirect to http: err = %v, want *SOPINRequiresTLSError", err)
	}
	if n := plain.all.Load(); n != 0 {
		t.Fatalf("plain server received %d requests, want 0", n)
	}
}

// A bound SO PIN does not follow a redirect to another https host.
func TestSOPINHop_RedirectToForeignHostRefused(t *testing.T) {
	foreign := &pinCounter{}
	foreignSrv := httptest.NewTLSServer(foreign)
	t.Cleanup(foreignSrv.Close)

	tlsSrv := httptest.NewTLSServer(redirectTool(mcpHandler(t), foreignSrv.URL))
	t.Cleanup(tlsSrv.Close)
	tr, err := New(&transport.Config{Address: tlsSrv.URL, Timeout: 5 * time.Second},
		WithHTTPClient(tlsSrv.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	err = shamirInit(boundSOPIN(t), tr)
	var hostErr *qrdbtransport.SOPINForeignHostError
	if !errors.As(err, &hostErr) {
		t.Fatalf("redirect to another host: err = %v, want *SOPINForeignHostError", err)
	}
	if n := foreign.all.Load(); n != 0 {
		t.Fatalf("foreign server received %d requests, want 0", n)
	}
}

// requestShamirInit runs BarrierInitializeShamir with the SO PIN in the
// request's own SOPin field and nothing bound in the context.
func requestShamirInit(tr *Transport) error {
	_, err := tr.BarrierInitializeShamir(context.Background(), &qrdbtransport.BarrierInitializeShamirRequest{
		SOPin: testSOPIN, UserPin: testUserPIN, Secret: []byte("pw"), Threshold: 2, TotalShares: 2,
	})
	return err
}

// TestSOPINHop_RequestPINPlainEndpointRefused: an SO PIN supplied in the
// request, with none bound by WithSOPIN, is refused before it is sent to an
// http:// endpoint.
func TestSOPINHop_RequestPINPlainEndpointRefused(t *testing.T) {
	verbs := map[string]func(*Transport) error{
		"BarrierInitialize": func(tr *Transport) error {
			return tr.BarrierInitialize(context.Background(), &qrdbtransport.BarrierInitializeRequest{
				SOPin: testSOPIN, UserPin: testUserPIN, Secret: []byte("pw"),
			})
		},
		"BarrierInitializeShamir": requestShamirInit,
		"BarrierRekey": func(tr *Transport) error {
			_, err := tr.BarrierRekey(context.Background(), &qrdbtransport.BarrierRekeyRequest{
				SOPin: testSOPIN, Threshold: 2, Total: 2,
			})
			return err
		},
		"BarrierGenerateRecoveryKeys": func(tr *Transport) error {
			_, err := tr.BarrierGenerateRecoveryKeys(context.Background(), &qrdbtransport.BarrierGenerateRecoveryKeysRequest{
				SOPin: testSOPIN, Threshold: 2, Total: 2,
			})
			return err
		},
	}
	for name, call := range verbs {
		t.Run(name, func(t *testing.T) {
			counter := &pinCounter{next: mcpHandler(t)}
			srv := httptest.NewServer(counter)
			t.Cleanup(srv.Close)
			tr, err := New(&transport.Config{Address: srv.URL, Timeout: 5 * time.Second})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = tr.Close() })

			err = call(tr)
			var tlsErr *qrdbtransport.SOPINRequiresTLSError
			if !errors.As(err, &tlsErr) {
				t.Fatalf("%s over http with a request PIN: err = %v, want *SOPINRequiresTLSError", name, err)
			}
			if n := counter.withPIN.Load(); n != 0 {
				t.Fatalf("plain server received %d requests carrying the SO PIN, want 0", n)
			}
		})
	}
}

// TestSOPINHop_RequestPINRedirectToPlainRefused: a request-supplied SO PIN
// does not follow a redirect from the https endpoint to http.
func TestSOPINHop_RequestPINRedirectToPlainRefused(t *testing.T) {
	plain := &pinCounter{}
	plainSrv := httptest.NewServer(plain)
	t.Cleanup(plainSrv.Close)

	tlsSrv := httptest.NewTLSServer(redirectTool(mcpHandler(t), plainSrv.URL))
	t.Cleanup(tlsSrv.Close)
	tr, err := New(&transport.Config{Address: tlsSrv.URL, Timeout: 5 * time.Second},
		WithHTTPClient(tlsSrv.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	err = requestShamirInit(tr)
	var tlsErr *qrdbtransport.SOPINRequiresTLSError
	if !errors.As(err, &tlsErr) {
		t.Fatalf("redirect to http with a request PIN: err = %v, want *SOPINRequiresTLSError", err)
	}
	if n := plain.all.Load(); n != 0 {
		t.Fatalf("plain server received %d requests, want 0", n)
	}
}

// TestSOPINHop_RequestPINRedirectToForeignHostRefused: a request-supplied SO
// PIN does not follow a redirect to a host other than the endpoint's.
func TestSOPINHop_RequestPINRedirectToForeignHostRefused(t *testing.T) {
	foreign := &pinCounter{}
	foreignSrv := httptest.NewTLSServer(foreign)
	t.Cleanup(foreignSrv.Close)

	tlsSrv := httptest.NewTLSServer(redirectTool(mcpHandler(t), foreignSrv.URL))
	t.Cleanup(tlsSrv.Close)
	tr, err := New(&transport.Config{Address: tlsSrv.URL, Timeout: 5 * time.Second},
		WithHTTPClient(tlsSrv.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	err = requestShamirInit(tr)
	var hostErr *qrdbtransport.SOPINForeignHostError
	if !errors.As(err, &hostErr) {
		t.Fatalf("redirect to another host with a request PIN: err = %v, want *SOPINForeignHostError", err)
	}
	if n := foreign.all.Load(); n != 0 {
		t.Fatalf("foreign server received %d requests, want 0", n)
	}
}

// TestSOPINHop_RequestPINReachesHTTPSEndpoint: over https with no redirect, a
// request-supplied SO PIN still reaches the server in the tool arguments.
func TestSOPINHop_RequestPINReachesHTTPSEndpoint(t *testing.T) {
	got := map[string]map[string]interface{}{}
	reply := map[string]interface{}{"threshold": 2, "total_shares": 2, "total": 2, "share_count": 2}
	srv, _ := newMultiToolServer(t, barrierPINToolNames, recordArgs(got, reply))
	tlsSrv := httptest.NewTLSServer(mcpserver.NewStreamableHTTPServer(srv))
	t.Cleanup(tlsSrv.Close)
	tr, err := New(&transport.Config{Address: tlsSrv.URL, Timeout: 5 * time.Second},
		WithHTTPClient(tlsSrv.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	if err := requestShamirInit(tr); err != nil {
		t.Fatalf("BarrierInitializeShamir over https with a request PIN: %v", err)
	}
	assertStringArg(t, "qrdb_barrier_shamir_initialize", got["qrdb_barrier_shamir_initialize"], "so_pin", testSOPIN)
}

// New guards a supplied client on a copy and leaves the caller's client as
// it was.
func TestSOPINHop_SuppliedClientNotMutated(t *testing.T) {
	srv := httptest.NewTLSServer(http.NotFoundHandler())
	t.Cleanup(srv.Close)
	client := srv.Client()
	orig := client.Transport

	tr, err := New(&transport.Config{Address: srv.URL, Timeout: 5 * time.Second}, WithHTTPClient(client))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	if client.Transport != orig {
		t.Fatal("New replaced the supplied client's Transport")
	}
	if tr.client == client || tr.client.Transport == orig {
		t.Fatal("New did not guard the supplied client on a copy")
	}
}

// userPINShamirInit sends a user PIN with no SO PIN, and nothing bound.
func userPINShamirInit(tr *Transport) error {
	_, err := tr.BarrierInitializeShamir(context.Background(), &qrdbtransport.BarrierInitializeShamirRequest{
		UserPin: testUserPIN, Secret: []byte("pw"), Threshold: 2, TotalShares: 2,
	})
	return err
}

// The TestUserPINHop_ tests cover calls that carry only a user PIN. At
// 886c719d2 these calls were not guarded: they reached the plain server in
// clear and followed redirects to plaintext and to foreign hosts.
func TestUserPINHop_PlainEndpointRefused(t *testing.T) {
	verbs := map[string]func(*Transport) error{
		"BarrierInitialize": func(tr *Transport) error {
			return tr.BarrierInitialize(context.Background(), &qrdbtransport.BarrierInitializeRequest{
				UserPin: testUserPIN, Secret: []byte("pw"),
			})
		},
		"BarrierInitializeShamir": userPINShamirInit,
		"CeremonyInitialize": func(tr *Transport) error {
			_, err := tr.CeremonyInitialize(context.Background(), &qrdbtransport.CeremonyInitializeRequest{
				UserPin: testUserPIN, Threshold: 2,
			})
			return err
		},
	}
	for name, call := range verbs {
		t.Run(name, func(t *testing.T) {
			counter := &pinCounter{next: mcpHandler(t)}
			srv := httptest.NewServer(counter)
			t.Cleanup(srv.Close)
			tr, err := New(&transport.Config{Address: srv.URL, Timeout: 5 * time.Second})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = tr.Close() })

			err = call(tr)
			var tlsErr *PINRequiresTLSError
			if !errors.As(err, &tlsErr) {
				t.Fatalf("%s over http with a user PIN: err = %v, want *PINRequiresTLSError", name, err)
			}
			if n := counter.withUserPIN.Load(); n != 0 {
				t.Fatalf("plain server received %d requests carrying the user PIN, want 0", n)
			}
		})
	}
}

func TestUserPINHop_RedirectToPlainRefused(t *testing.T) {
	plain := &pinCounter{}
	plainSrv := httptest.NewServer(plain)
	t.Cleanup(plainSrv.Close)

	tlsSrv := httptest.NewTLSServer(redirectTool(mcpHandler(t), plainSrv.URL))
	t.Cleanup(tlsSrv.Close)
	tr, err := New(&transport.Config{Address: tlsSrv.URL, Timeout: 5 * time.Second},
		WithHTTPClient(tlsSrv.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	err = userPINShamirInit(tr)
	var tlsErr *PINRequiresTLSError
	if !errors.As(err, &tlsErr) {
		t.Fatalf("redirect to http with a user PIN: err = %v, want *PINRequiresTLSError", err)
	}
	if n := plain.all.Load(); n != 0 {
		t.Fatalf("plain server received %d requests, want 0", n)
	}
}

func TestUserPINHop_RedirectToForeignHostRefused(t *testing.T) {
	foreign := &pinCounter{}
	foreignSrv := httptest.NewTLSServer(foreign)
	t.Cleanup(foreignSrv.Close)

	tlsSrv := httptest.NewTLSServer(redirectTool(mcpHandler(t), foreignSrv.URL))
	t.Cleanup(tlsSrv.Close)
	tr, err := New(&transport.Config{Address: tlsSrv.URL, Timeout: 5 * time.Second},
		WithHTTPClient(tlsSrv.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	err = userPINShamirInit(tr)
	var hostErr *PINForeignHostError
	if !errors.As(err, &hostErr) {
		t.Fatalf("redirect to another host with a user PIN: err = %v, want *PINForeignHostError", err)
	}
	if n := foreign.all.Load(); n != 0 {
		t.Fatalf("foreign server received %d requests, want 0", n)
	}
}

func TestUserPINHop_ReachesHTTPSEndpointWithoutSOPINHeader(t *testing.T) {
	got := map[string]map[string]interface{}{}
	reply := map[string]interface{}{"threshold": 2, "total_shares": 2, "total": 2, "share_count": 2}
	srv, _ := newMultiToolServer(t, barrierPINToolNames, recordArgs(got, reply))
	counter := &pinCounter{next: mcpserver.NewStreamableHTTPServer(srv)}
	tlsSrv := httptest.NewTLSServer(counter)
	t.Cleanup(tlsSrv.Close)
	tr, err := New(&transport.Config{Address: tlsSrv.URL, Timeout: 5 * time.Second},
		WithHTTPClient(tlsSrv.Client()))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })

	if err := userPINShamirInit(tr); err != nil {
		t.Fatalf("BarrierInitializeShamir over https with a user PIN: %v", err)
	}
	args := got["qrdb_barrier_shamir_initialize"]
	assertStringArg(t, "qrdb_barrier_shamir_initialize", args, "user_pin", testUserPIN)
	if so, _ := args["so_pin"].(string); so != "" {
		t.Fatalf("qrdb_barrier_shamir_initialize carried so_pin %q, want none", so)
	}
	if n := counter.soPINHeader.Load(); n != 0 {
		t.Fatalf("server received %d requests with the %s header, want 0", n, qrdbtransport.SOPINHeader)
	}
}
