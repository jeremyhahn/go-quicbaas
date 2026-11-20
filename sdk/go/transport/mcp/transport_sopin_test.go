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

// transport_sopin_test.go checks that every MCP barrier and ceremony call
// carrying an SO PIN -- in its request's SOPin field or bound with
// WithSOPIN -- refuses an http:// endpoint and a redirect to a plaintext
// server before the PIN is sent.

package mcp

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	stderrors "errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	mcpserver "github.com/mark3labs/mcp-go/server"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	qrdbtransport "github.com/jeremyhahn/go-qrdb/sdk/go/transport"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const testSOPIN = "so-pin-1234"

// sopinCalls lists every MCP call that carries an SO PIN. Each builds its
// own context: the request-field calls put the PIN in req.SOPin only, and
// BarrierSeal, which has no request, binds it with WithSOPIN.
var sopinCalls = []struct {
	name string
	call func(t *testing.T, tr *Transport) error
}{
	{"CeremonyInitialize", func(_ *testing.T, tr *Transport) error {
		_, err := tr.CeremonyInitialize(context.Background(), &qrdbtransport.CeremonyInitializeRequest{SOPin: testSOPIN})
		return err
	}},
	{"CeremonySignCSR", func(_ *testing.T, tr *Transport) error {
		_, err := tr.CeremonySignCSR(context.Background(), &qrdbtransport.SignCSRInitRequest{SOPin: testSOPIN})
		return err
	}},
	{"BarrierSeal", func(t *testing.T, tr *Transport) error {
		ctx, release, err := qrdbsdk.WithSOPIN(context.Background(), []byte(testSOPIN))
		if err != nil {
			t.Fatalf("WithSOPIN: %v", err)
		}
		defer release()
		return tr.BarrierSeal(ctx)
	}},
	{"BarrierInitializeShamir", func(_ *testing.T, tr *Transport) error {
		_, err := tr.BarrierInitializeShamir(context.Background(), &qrdbtransport.BarrierInitializeShamirRequest{SOPin: testSOPIN})
		return err
	}},
	{"BarrierRekey", func(_ *testing.T, tr *Transport) error {
		_, err := tr.BarrierRekey(context.Background(), &qrdbtransport.BarrierRekeyRequest{SOPin: testSOPIN})
		return err
	}},
	{"BarrierGenerateRecoveryKeys", func(_ *testing.T, tr *Transport) error {
		_, err := tr.BarrierGenerateRecoveryKeys(context.Background(), &qrdbtransport.BarrierGenerateRecoveryKeysRequest{SOPin: testSOPIN})
		return err
	}},
}

func sopinToolNames() []string {
	return append(append([]string{}, barrierToolNames...), ceremonyToolNames...)
}

// TestMCP_SOPINRequiresTLS: each call's PIN is refused to an http:// MCP
// server before any tool call reaches it.
func TestMCP_SOPINRequiresTLS(t *testing.T) {
	for _, tc := range sopinCalls {
		t.Run(tc.name, func(t *testing.T) {
			tr, called, cleanup := newMultiToolMCPServer(t, sopinToolNames(), nil)
			defer cleanup()

			err := tc.call(t, tr)
			var tlsErr *qrdbtransport.SOPINRequiresTLSError
			if !stderrors.As(err, &tlsErr) {
				t.Fatalf("%s with an SO PIN over http://: got %v, want *SOPINRequiresTLSError", tc.name, err)
			}
			if len(*called) != 0 {
				t.Fatalf("the PIN must not be sent over http://, server saw %v", *called)
			}
		})
	}
}

// TestMCP_SOPINRefusesPlainRedirect: a TLS MCP server answers the session
// handshake itself but 307-redirects every tools/call to a plaintext server;
// the call is refused and the plaintext server receives nothing.
func TestMCP_SOPINRefusesPlainRedirect(t *testing.T) {
	for _, tc := range sopinCalls {
		t.Run(tc.name, func(t *testing.T) {
			var plainHits atomic.Int32
			plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				plainHits.Add(1)
				w.WriteHeader(http.StatusOK)
			}))
			t.Cleanup(plain.Close)

			srv, called := newMultiToolServer(t, sopinToolNames(), nil)
			mcpHandler := mcpserver.NewStreamableHTTPServer(srv)
			front := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				r.Body = io.NopCloser(bytes.NewReader(body))
				var msg struct {
					Method string `json:"method"`
				}
				_ = json.Unmarshal(body, &msg)
				if msg.Method == "tools/call" {
					http.Redirect(w, r, plain.URL+r.URL.Path, http.StatusTemporaryRedirect)
					return
				}
				mcpHandler.ServeHTTP(w, r)
			}))
			front.StartTLS()
			t.Cleanup(front.Close)

			pool := x509.NewCertPool()
			pool.AddCert(front.Certificate())
			tr, err := New(&transport.Config{
				Address:   front.URL,
				Timeout:   5 * time.Second,
				TLSConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12},
			})
			if err != nil {
				t.Fatalf("New: %v", err)
			}
			t.Cleanup(func() { _ = tr.Close() })

			err = tc.call(t, tr)
			var tlsErr *qrdbtransport.SOPINRequiresTLSError
			if !stderrors.As(err, &tlsErr) {
				t.Fatalf("%s with an SO PIN redirected to http://: got %v, want *SOPINRequiresTLSError", tc.name, err)
			}
			if n := plainHits.Load(); n != 0 {
				t.Fatalf("requests reaching the plaintext redirect target: got %d, want 0", n)
			}
			if len(*called) != 0 {
				t.Fatalf("no tool call should be served, server saw %v", *called)
			}
		})
	}
}
