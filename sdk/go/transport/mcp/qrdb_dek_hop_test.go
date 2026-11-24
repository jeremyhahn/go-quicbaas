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

package mcp

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"

	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	qberrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	dekHopTenant     = "dek-hop-tenant"
	dekHopPlaintext  = "dek-hop-plaintext"
	dekHopCiphertext = "dek-hop-ciphertext"
	dekHopAAD        = "dek-hop-aad"
)

var dekToolNames = []string{
	"qrdb_dek_status", "qrdb_dek_rotate", "qrdb_dek_retire",
	"qrdb_dek_encrypt", "qrdb_dek_decrypt", "qrdb_dek_rewrap",
}

// dekVerb is one MCP keyring verb: the tool it calls, whether it carries
// key-protected data, and a call with fixed fake arguments.
type dekVerb struct {
	name string
	tool string
	data bool
	call func(ctx context.Context, c *QRDBDEKClient) error
}

var dekVerbs = []dekVerb{
	{"TenantDEKStatus", "qrdb_dek_status", false, func(ctx context.Context, c *QRDBDEKClient) error {
		_, err := c.TenantDEKStatus(ctx, &qrdbtransport.TenantDEKRequest{TenantID: dekHopTenant})
		return err
	}},
	{"TenantDEKRotate", "qrdb_dek_rotate", false, func(ctx context.Context, c *QRDBDEKClient) error {
		_, err := c.TenantDEKRotate(ctx, &qrdbtransport.TenantDEKRequest{TenantID: dekHopTenant})
		return err
	}},
	{"TenantDEKRetire", "qrdb_dek_retire", false, func(ctx context.Context, c *QRDBDEKClient) error {
		_, err := c.TenantDEKRetire(ctx, &qrdbtransport.TenantDEKRetireRequest{TenantID: dekHopTenant, Version: 1})
		return err
	}},
	{"TenantDEKEncrypt", "qrdb_dek_encrypt", true, func(ctx context.Context, c *QRDBDEKClient) error {
		_, err := c.TenantDEKEncrypt(ctx, &qrdbtransport.TenantDEKEncryptRequest{
			TenantID: dekHopTenant, Plaintext: []byte(dekHopPlaintext), AAD: []byte(dekHopAAD)})
		return err
	}},
	{"TenantDEKDecrypt", "qrdb_dek_decrypt", true, func(ctx context.Context, c *QRDBDEKClient) error {
		_, err := c.TenantDEKDecrypt(ctx, &qrdbtransport.TenantDEKDecryptRequest{
			TenantID: dekHopTenant, Ciphertext: []byte(dekHopCiphertext), AAD: []byte(dekHopAAD)})
		return err
	}},
	{"TenantDEKRewrap", "qrdb_dek_rewrap", true, func(ctx context.Context, c *QRDBDEKClient) error {
		_, err := c.TenantDEKRewrap(ctx, &qrdbtransport.TenantDEKRewrapRequest{
			TenantID: dekHopTenant, Ciphertext: []byte(dekHopCiphertext), AAD: []byte(dekHopAAD)})
		return err
	}},
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// dekReply returns a canned keyring reply for tool and records its args.
func dekReply(got map[string]map[string]interface{}) toolResultFn {
	return func(_ *testing.T, tool string, args map[string]interface{}) map[string]interface{} {
		if got != nil {
			got[tool] = args
		}
		switch tool {
		case "qrdb_dek_encrypt":
			return map[string]interface{}{"ciphertext": b64(dekHopCiphertext), "version": 1}
		case "qrdb_dek_decrypt":
			return map[string]interface{}{"plaintext": b64(dekHopPlaintext), "version": 1}
		case "qrdb_dek_rewrap":
			return map[string]interface{}{"ciphertext": b64(dekHopCiphertext), "from_version": 1, "to_version": 2}
		}
		return map[string]interface{}{"tenant_id": dekHopTenant, "algorithm": "aes-256-gcm", "current_version": 1}
	}
}

func dekHandler(t *testing.T, got map[string]map[string]interface{}) http.Handler {
	t.Helper()
	srv, _ := newMultiToolServer(t, dekToolNames, dekReply(got))
	return mcpserver.NewStreamableHTTPServer(srv)
}

// redirectNamedTool redirects a request whose body names tool to location,
// and passes every other request to next.
func redirectNamedTool(next http.Handler, tool, location string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if bytes.Contains(body, []byte(`"`+tool+`"`)) {
			http.Redirect(w, r, location+r.URL.Path, http.StatusTemporaryRedirect)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		next.ServeHTTP(w, r)
	})
}

// toolCounter counts the requests whose body names tool.
type toolCounter struct {
	next http.Handler
	tool string
	n    atomic.Int32
}

func (c *toolCounter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	if bytes.Contains(body, []byte(`"`+c.tool+`"`)) {
		c.n.Add(1)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	c.next.ServeHTTP(w, r)
}

func newDEKTransport(t *testing.T, addr string, opts ...Option) *Transport {
	t.Helper()
	tr, err := New(&transport.Config{Address: addr, Timeout: 5 * time.Second}, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = tr.Close() })
	return tr
}

func assertDEKHopError(t *testing.T, verb string, err error) {
	t.Helper()
	var hopErr *DEKHopError
	if !errors.As(err, &hopErr) {
		t.Fatalf("%s: err = %v, want *DEKHopError", verb, err)
	}
	if hopErr.Verb != verb {
		t.Fatalf("%s: DEKHopError.Verb = %q", verb, hopErr.Verb)
	}
}

func TestDEKHop_PlainEndpointRefused(t *testing.T) {
	for _, v := range dekVerbs {
		t.Run(v.name, func(t *testing.T) {
			counter := &pinCounter{}
			srv := httptest.NewServer(counter)
			t.Cleanup(srv.Close)
			tr := newDEKTransport(t, srv.URL)

			err := v.call(context.Background(), tr.QRDBDEK())
			assertDEKHopError(t, v.name, err)
			if n := counter.all.Load(); n != 0 {
				t.Fatalf("%s over http: server received %d requests, want 0", v.name, n)
			}
		})
	}
}

func TestDEKHop_RedirectToPlainRefused(t *testing.T) {
	for _, v := range dekVerbs {
		t.Run(v.name, func(t *testing.T) {
			plain := &pinCounter{}
			plainSrv := httptest.NewServer(plain)
			t.Cleanup(plainSrv.Close)
			tlsSrv := httptest.NewTLSServer(redirectNamedTool(dekHandler(t, nil), v.tool, plainSrv.URL))
			t.Cleanup(tlsSrv.Close)
			tr := newDEKTransport(t, tlsSrv.URL, WithHTTPClient(tlsSrv.Client()))

			err := v.call(context.Background(), tr.QRDBDEK())
			assertDEKHopError(t, v.name, err)
			if n := plain.all.Load(); n != 0 {
				t.Fatalf("%s: plain server received %d requests, want 0", v.name, n)
			}
		})
	}
}

func TestDEKHop_RedirectToForeignHostRefused(t *testing.T) {
	for _, v := range dekVerbs {
		t.Run(v.name, func(t *testing.T) {
			foreign := &pinCounter{}
			foreignSrv := httptest.NewTLSServer(foreign)
			t.Cleanup(foreignSrv.Close)
			tlsSrv := httptest.NewTLSServer(redirectNamedTool(dekHandler(t, nil), v.tool, foreignSrv.URL))
			t.Cleanup(tlsSrv.Close)
			tr := newDEKTransport(t, tlsSrv.URL, WithHTTPClient(tlsSrv.Client()))

			err := v.call(context.Background(), tr.QRDBDEK())
			assertDEKHopError(t, v.name, err)
			if n := foreign.all.Load(); n != 0 {
				t.Fatalf("%s: foreign server received %d requests, want 0", v.name, n)
			}
		})
	}
}

func TestDEKHop_DataVerbSameHostRedirectRefused(t *testing.T) {
	for _, v := range dekVerbs {
		if !v.data {
			continue
		}
		t.Run(v.name, func(t *testing.T) {
			var self string
			inner := dekHandler(t, nil)
			counter := &toolCounter{tool: v.tool}
			counter.next = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				redirectNamedTool(inner, v.tool, self).ServeHTTP(w, r)
			})
			tlsSrv := httptest.NewTLSServer(counter)
			t.Cleanup(tlsSrv.Close)
			self = tlsSrv.URL
			tr := newDEKTransport(t, tlsSrv.URL, WithHTTPClient(tlsSrv.Client()))

			err := v.call(context.Background(), tr.QRDBDEK())
			assertDEKHopError(t, v.name, err)
			if n := counter.n.Load(); n != 1 {
				t.Fatalf("%s: server received the %s call %d times, want 1 (no replay to the redirect)", v.name, v.tool, n)
			}
		})
	}
}

func TestDEKHop_HTTPSRoundTrip(t *testing.T) {
	got := map[string]map[string]interface{}{}
	tlsSrv := httptest.NewTLSServer(dekHandler(t, got))
	t.Cleanup(tlsSrv.Close)
	tr := newDEKTransport(t, tlsSrv.URL, WithHTTPClient(tlsSrv.Client()))

	for _, v := range dekVerbs {
		if err := v.call(context.Background(), tr.QRDBDEK()); err != nil {
			t.Fatalf("%s over https: %v", v.name, err)
		}
	}
	for _, tool := range dekToolNames {
		assertStringArg(t, tool, got[tool], "tenant_id", dekHopTenant)
	}
	if ver, _ := got["qrdb_dek_retire"]["version"].(float64); ver != 1 {
		t.Fatalf("qrdb_dek_retire version = %v, want 1", got["qrdb_dek_retire"]["version"])
	}
	assertStringArg(t, "qrdb_dek_encrypt", got["qrdb_dek_encrypt"], "plaintext", b64(dekHopPlaintext))
	assertStringArg(t, "qrdb_dek_decrypt", got["qrdb_dek_decrypt"], "ciphertext", b64(dekHopCiphertext))
	assertStringArg(t, "qrdb_dek_rewrap", got["qrdb_dek_rewrap"], "ciphertext", b64(dekHopCiphertext))
	for _, tool := range []string{"qrdb_dek_encrypt", "qrdb_dek_decrypt", "qrdb_dek_rewrap"} {
		assertStringArg(t, tool, got[tool], "aad", b64(dekHopAAD))
	}
}

func TestDEKHop_RefusalIsForbidden(t *testing.T) {
	plainSrv := httptest.NewServer(&pinCounter{})
	t.Cleanup(plainSrv.Close)
	tr := newDEKTransport(t, plainSrv.URL)
	tlsPlain := httptest.NewServer(&pinCounter{})
	t.Cleanup(tlsPlain.Close)
	tlsSrv := httptest.NewTLSServer(redirectNamedTool(dekHandler(t, nil), "qrdb_dek_encrypt", tlsPlain.URL))
	t.Cleanup(tlsSrv.Close)
	trTLS := newDEKTransport(t, tlsSrv.URL, WithHTTPClient(tlsSrv.Client()))

	cases := map[string]error{
		"precheck": dekVerbs[3].call(context.Background(), tr.QRDBDEK()),
		"hop":      dekVerbs[3].call(context.Background(), trTLS.QRDBDEK()),
	}
	for name, err := range cases {
		assertDEKHopError(t, "TenantDEKEncrypt", err)
		if c := qberrors.Code(err); c != qberrors.CodeForbidden {
			t.Fatalf("%s refusal: code = %v, want CodeForbidden", name, c)
		}
		if qberrors.IsRetryable(err) {
			t.Fatalf("%s refusal is retryable, want not", name)
		}
		if bytes.Contains([]byte(err.Error()), []byte(dekHopPlaintext)) ||
			bytes.Contains([]byte(err.Error()), []byte(b64(dekHopPlaintext))) {
			t.Fatalf("%s refusal message carries the plaintext: %v", name, err)
		}
	}
}
