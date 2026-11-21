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

//go:build integration

package init

import (
	"context"
	"encoding/pem"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// sealedMCPStubError dials the SDK's MCP client, uncredentialed, at a TLS stub
// running handler and returns the error of the Liveness call, so the
// classifier sees the error chain the sealed-window check sees.
func sealedMCPStubError(t *testing.T, handler http.HandlerFunc, timeout time.Duration) error {
	t.Helper()
	ports, dataDir := sealedMCPStub(t, handler)

	client, err := newSDKClient("mcp", ports, dataDir, "")
	if err != nil {
		t.Fatalf("newSDKClient(mcp): %v", err)
	}
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_, err = client.Transport().Request(ctx, "quicbaas.Health", "Liveness", struct{}{})
	return err
}

// sealedMCPStub serves handler over TLS and returns the port bundle and a
// data directory whose CA chain trusts the stub's certificate.
func sealedMCPStub(t *testing.T, handler http.HandlerFunc) (PortBundle, string) {
	t.Helper()
	srv := httptest.NewTLSServer(handler)
	t.Cleanup(srv.Close)

	dataDir := t.TempDir()
	certsDir := filepath.Join(dataDir, "certs")
	if err := os.MkdirAll(certsDir, 0o700); err != nil {
		t.Fatal(err)
	}
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	if err := os.WriteFile(filepath.Join(certsDir, "ca-chain.crt"), caPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	port := srv.Listener.Addr().(*net.TCPAddr).Port
	return PortBundle{MCP: port}, dataDir
}

// TestCheckSealedMCPWindow proves the check probes initialize alone: a
// node that answers initialize and refuses only the next call with 401
// fails it, and a node that refuses initialize with 401 passes it.
func TestCheckSealedMCPWindow(t *testing.T) {
	t.Run("initialize allowed then 401 fails", func(t *testing.T) {
		var calls atomic.Int32
		handler := func(w http.ResponseWriter, r *http.Request) {
			if calls.Add(1) == 1 {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Mcp-Session-Id", "stub-session")
				_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2025-03-26",`+
					`"capabilities":{"tools":{}},"serverInfo":{"name":"stub","version":"0.0.0"}}}`)
				return
			}
			http.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
		}
		ports, dataDir := sealedMCPStub(t, handler)
		err := checkSealedMCPWindow(ports, dataDir)
		if err == nil {
			t.Fatal("a stub that served initialize and refused the next call with 401 passed the sealed check")
		}
		if !strings.Contains(err.Error(), "HTTP 200") {
			t.Fatalf("want the initialize probe to report HTTP 200, got: %v", err)
		}
	})
	t.Run("initialize refused with 401 passes", func(t *testing.T) {
		ports, dataDir := sealedMCPStub(t, statusHandler(http.StatusUnauthorized))
		if err := checkSealedMCPWindow(ports, dataDir); err != nil {
			t.Fatalf("a stub that refused initialize with 401 failed the sealed check: %v", err)
		}
	})
}

func statusHandler(code int) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, http.StatusText(code), code)
	}
}

// TestClassifySealedMCPRefusal pins what the shamir MCP cell accepts as the
// sealed window's refusal: only the 401 on the uncredentialed initialize.
func TestClassifySealedMCPRefusal(t *testing.T) {
	t.Run("401 passes", func(t *testing.T) {
		err := sealedMCPStubError(t, statusHandler(http.StatusUnauthorized), 10*time.Second)
		if verdict := classifySealedMCPRefusal(err); verdict != nil {
			t.Fatalf("401 refusal rejected: %v", verdict)
		}
	})

	for _, tc := range []struct {
		name string
		code int
		want string
	}{
		{name: "404 fails", code: http.StatusNotFound, want: "non-401 HTTP 4xx"},
		{name: "500 fails", code: http.StatusInternalServerError, want: "HTTP 500"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := sealedMCPStubError(t, statusHandler(tc.code), 10*time.Second)
			verdict := classifySealedMCPRefusal(err)
			if verdict == nil {
				t.Fatalf("HTTP %d accepted as the sealed refusal (call error: %v)", tc.code, err)
			}
			if !strings.Contains(verdict.Error(), tc.want) {
				t.Errorf("verdict %q does not name %q", verdict, tc.want)
			}
		})
	}

	t.Run("timeout fails", func(t *testing.T) {
		// The handler outlives the client's deadline but returns on its own,
		// so the stub's Close does not wait on a held connection.
		hang := func(_ http.ResponseWriter, r *http.Request) {
			select {
			case <-r.Context().Done():
			case <-time.After(3 * time.Second):
			}
		}
		err := sealedMCPStubError(t, hang, 500*time.Millisecond)
		verdict := classifySealedMCPRefusal(err)
		if verdict == nil {
			t.Fatalf("timeout accepted as the sealed refusal (call error: %v)", err)
		}
		if !strings.Contains(verdict.Error(), "timed out") {
			t.Errorf("verdict %q does not name the timeout", verdict)
		}
	})

	t.Run("success fails", func(t *testing.T) {
		verdict := classifySealedMCPRefusal(nil)
		if verdict == nil {
			t.Fatal("a served uncredentialed call accepted as the sealed refusal")
		}
		if !strings.Contains(verdict.Error(), "served") {
			t.Errorf("verdict %q does not name the served session", verdict)
		}
	})
}
