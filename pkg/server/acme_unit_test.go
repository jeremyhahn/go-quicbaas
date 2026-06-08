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

package server

import (
	"errors"
	"testing"

	"github.com/jeremyhahn/go-quicbaas/pkg/config"
)

// acmeBaseURI derives the externally reachable ACME base URL from the REST
// listener configuration and returns empty when REST is unusable.
func TestAcmeBaseURI(t *testing.T) {
	tests := []struct {
		name        string
		restEnabled bool
		port        int
		host        string
		want        string
	}{
		{"rest disabled yields empty", false, 8443, "example.com", ""},
		{"non-positive port yields empty", true, 0, "example.com", ""},
		{"explicit host preserved", true, 8443, "example.com", "https://example.com:8443"},
		{"empty host defaults to localhost", true, 9443, "", "https://localhost:9443"},
		{"wildcard host defaults to localhost", true, 9443, "0.0.0.0", "https://localhost:9443"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Server{config: &config.Config{}}
			s.config.Server.REST.Enabled = tt.restEnabled
			s.config.Server.REST.Port = tt.port
			s.config.Server.Host = tt.host
			if got := s.acmeBaseURI(); got != tt.want {
				t.Fatalf("acmeBaseURI() = %q, want %q", got, tt.want)
			}
		})
	}
}

// buildACMEService returns (nil, nil) — "ACME not configured" — when its
// prerequisites are absent, and never errors on those benign paths.
func TestBuildACMEService_NotConfigured(t *testing.T) {
	t.Run("nil ca adapter", func(t *testing.T) {
		s := &Server{config: &config.Config{}}
		svc, err := s.buildACMEService(nil)
		if err != nil || svc != nil {
			t.Fatalf("want (nil, nil), got (%v, %v)", svc, err)
		}
	})

	t.Run("nil xkmsCA / qrdbClient", func(t *testing.T) {
		ca := newFakeTrustedCA(t)
		adapter, _ := newCAServiceAdapter(ca)
		s := &Server{config: &config.Config{}}
		// xkmsCA and qrdbClient are nil, so the early guard returns (nil, nil).
		svc, err := s.buildACMEService(adapter)
		if err != nil || svc != nil {
			t.Fatalf("want (nil, nil), got (%v, %v)", svc, err)
		}
	})
}

// The ACME error types carry their cause and unwrap correctly so callers can
// classify ACME wiring failures with errors.Is / errors.As.
func TestACMEErrorTypes(t *testing.T) {
	t.Run("ACMECAAssertError message", func(t *testing.T) {
		e := &ACMECAAssertError{}
		if e.Error() == "" {
			t.Fatal("ACMECAAssertError.Error() must be non-empty")
		}
	})

	t.Run("ACMEStorageError wraps cause", func(t *testing.T) {
		cause := errors.New("kv unavailable")
		e := &ACMEStorageError{Cause: cause}
		if e.Error() == "" {
			t.Fatal("ACMEStorageError.Error() must be non-empty")
		}
		if !errors.Is(e, cause) {
			t.Fatalf("ACMEStorageError must unwrap to its cause")
		}
	})
}
