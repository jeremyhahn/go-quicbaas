// Copyright (c) 2025 Jeremy Hahn
// Copyright (c) 2025 Automate The Things, LLC
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

package xkmsca

import (
	"errors"
	"testing"

	testca "github.com/jeremyhahn/go-quicbaas/pkg/testutil/xkmsca"
	xkmstransport "github.com/jeremyhahn/go-xkms/sdk/go/transport"
)

// TestXKMSCAServicer_ParseSANs verifies SAN prefix parsing covers DNS,
// IP, Email, URI, and unknown / unprefixed entries. Pure function — no
// CA dependency.
func TestXKMSCAServicer_ParseSANs(t *testing.T) {
	result := parseSANs([]string{
		"DNS:first.example.com",
		"ip:10.0.0.1",
		"Email:op@example.com",
		"URI:spiffe://cluster/agent",
		"unprefixed.example.com",
		" ", // skipped
		"",  // skipped
		"unknown:value",
	})
	if result == nil {
		t.Fatal("expected non-nil SANs")
	}
	if len(result.DNS) != 3 {
		t.Fatalf("DNS entries: want 3, got %d (%v)", len(result.DNS), result.DNS)
	}
	if len(result.IPs) != 1 || result.IPs[0] != "10.0.0.1" {
		t.Fatalf("IPs: %v", result.IPs)
	}
	if len(result.Email) != 1 || result.Email[0] != "op@example.com" {
		t.Fatalf("Email: %v", result.Email)
	}
	if len(result.URIs) != 1 || result.URIs[0] != "spiffe://cluster/agent" {
		t.Fatalf("URIs: %v", result.URIs)
	}
}

// TestXKMSCAServicer_ParseSANs_Empty verifies the empty-input case
// returns nil (adapter then sends nil SANS to the CA, letting the CA
// apply its own defaults).
func TestXKMSCAServicer_ParseSANs_Empty(t *testing.T) {
	if parseSANs(nil) != nil {
		t.Fatal("expected nil for nil input")
	}
	if parseSANs([]string{}) != nil {
		t.Fatal("expected nil for empty slice")
	}
}

// TestXKMSCAServicer_ParseHexSerial verifies a range of serial-number
// formats parse correctly.
func TestXKMSCAServicer_ParseHexSerial(t *testing.T) {
	tests := []struct {
		name   string
		in     string
		wantOK bool
	}{
		{"plain hex", "deadbeef", true},
		{"uppercase prefix", "0XDEAD", true},
		{"lowercase prefix", "0xcafe", true},
		{"zero", "0", true},
		{"empty", "", false},
		{"garbage", "zzz", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseHexSerial(tc.in)
			if tc.wantOK {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if got == nil {
					t.Fatal("expected non-nil big.Int")
				}
				return
			}
			var invalid *XKMSCAInvalidSerialError
			if !errors.As(err, &invalid) {
				t.Fatalf("expected XKMSCAInvalidSerialError, got %T %v", err, err)
			}
			if invalid.Value != tc.in {
				t.Fatalf("Value: want %q got %q", tc.in, invalid.Value)
			}
		})
	}
}

// TestXKMSCAServicer_CAServiceInterface asserts the adapter satisfies
// xkmstransport.CAService at compile time. This is the key structural
// guarantee: because go-xkms's unexported REST caServicer has the
// IDENTICAL method set, Go's structural typing accepts this adapter
// wherever caServicer is required.
func TestXKMSCAServicer_CAServiceInterface(t *testing.T) {
	var _ xkmstransport.CAService = (*xkmsCAServicer)(nil)
}

// TestNew_NilCAReturnsNil asserts New returns a nil interface (not a typed
// nil inside a non-nil interface) when no CA is configured, so a caller's
// nil check skips wiring the CA verbs.
func TestNew_NilCAReturnsNil(t *testing.T) {
	if got := New(nil, "example.test"); got != nil {
		t.Fatalf("New(nil): want nil interface, got %T %v", got, got)
	}
}

// TestNew_WrapsTheCA asserts New returns the adapter around the given CA
// with the given SPIFFE trust domain.
func TestNew_WrapsTheCA(t *testing.T) {
	ca := testca.New(t, "xkmsca-new-ca")
	got := New(ca, "example.test")
	a, ok := got.(*xkmsCAServicer)
	if !ok {
		t.Fatalf("New: want *xkmsCAServicer, got %T", got)
	}
	if a.ca != ca {
		t.Fatal("New: adapter does not wrap the given CA")
	}
	if a.spiffeTrustDomain != "example.test" {
		t.Fatalf("New: spiffeTrustDomain: want %q got %q", "example.test", a.spiffeTrustDomain)
	}
}
