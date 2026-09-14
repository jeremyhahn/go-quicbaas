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

package clientip

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func request(remoteAddr string, headers map[string]string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remoteAddr
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	return r
}

// TestResolver_ZeroValueTrustsNothing pins the secure default. A Resolver
// nobody configured is the one most likely to appear in a deployment nobody
// thought about, so it must ignore headers rather than honour them.
func TestResolver_ZeroValueTrustsNothing(t *testing.T) {
	req := request("203.0.113.9:44321", map[string]string{
		"X-Forwarded-For": "198.51.100.7",
		"X-Real-IP":       "198.51.100.8",
	})

	for name, r := range map[string]*Resolver{
		"nil":        nil,
		"zero value": {},
		"empty list": mustResolver(t, nil),
	} {
		t.Run(name, func(t *testing.T) {
			if got := r.ClientIP(req); got != "203.0.113.9" {
				t.Errorf("ClientIP = %q, want the peer 203.0.113.9", got)
			}
			if r.TrustsAny() {
				t.Error("TrustsAny = true for a resolver with no configured proxies")
			}
		})
	}
}

func TestResolver_TrustedProxyIsHonoured(t *testing.T) {
	r := mustResolver(t, []string{"203.0.113.0/24"})

	for _, tc := range []struct {
		name    string
		headers map[string]string
		want    string
	}{
		{"forwarded chain takes the first entry", map[string]string{"X-Forwarded-For": "198.51.100.7, 203.0.113.9"}, "198.51.100.7"},
		{"single forwarded value", map[string]string{"X-Forwarded-For": "198.51.100.7"}, "198.51.100.7"},
		{"whitespace is trimmed", map[string]string{"X-Forwarded-For": "  198.51.100.7  "}, "198.51.100.7"},
		{"real-ip when no forwarded-for", map[string]string{"X-Real-IP": "198.51.100.8"}, "198.51.100.8"},
		{"forwarded-for wins over real-ip", map[string]string{"X-Forwarded-For": "198.51.100.7", "X-Real-IP": "198.51.100.8"}, "198.51.100.7"},
		{"a lone comma names nobody, so the peer stands", map[string]string{"X-Forwarded-For": ","}, "203.0.113.9"},
		{"no headers at all", nil, "203.0.113.9"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := r.ClientIP(request("203.0.113.9:44321", tc.headers)); got != tc.want {
				t.Errorf("ClientIP = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolver_BareAddressIsAcceptedAsASingleHost covers the configuration
// ergonomics: naming one load balancer is the common case, and making an
// operator write "/32" invites writing it wrong.
func TestResolver_BareAddressIsAcceptedAsASingleHost(t *testing.T) {
	r := mustResolver(t, []string{"203.0.113.9"})

	if got := r.ClientIP(request("203.0.113.9:1", map[string]string{"X-Forwarded-For": "198.51.100.7"})); got != "198.51.100.7" {
		t.Errorf("ClientIP from the named host = %q, want the forwarded client", got)
	}
	// The neighbour is a different host and is not covered by it.
	if got := r.ClientIP(request("203.0.113.10:1", map[string]string{"X-Forwarded-For": "198.51.100.7"})); got != "203.0.113.10" {
		t.Errorf("ClientIP from an unnamed host = %q, want that host", got)
	}
}

// TestResolver_IPv4MappedPeerMatchesAnIPv4Prefix guards a real footgun: a
// dual-stack listener reports an IPv4 peer as ::ffff:203.0.113.9, which would
// not match a 203.0.113.0/24 prefix compared naively — so a correctly
// configured proxy would silently stop being trusted.
func TestResolver_IPv4MappedPeerMatchesAnIPv4Prefix(t *testing.T) {
	r := mustResolver(t, []string{"203.0.113.0/24"})

	got := r.ClientIP(request("[::ffff:203.0.113.9]:44321", map[string]string{"X-Forwarded-For": "198.51.100.7"}))
	if got != "198.51.100.7" {
		t.Errorf("ClientIP = %q, want the forwarded client: a v4-mapped peer did not match its v4 prefix", got)
	}
}

func TestResolver_RejectsUnparseableProxy(t *testing.T) {
	_, err := NewResolver([]string{"not-an-address"})
	if err == nil {
		t.Fatal("NewResolver accepted a value that is neither a CIDR nor an address")
	}
	var invalid *InvalidProxyError
	if !errors.As(err, &invalid) {
		t.Fatalf("error is %T, want *InvalidProxyError so configuration loading can fail startup on it", err)
	}
	if invalid.Entry != "not-an-address" {
		t.Errorf("Entry = %q, want the value as written so the operator can find it", invalid.Entry)
	}
}

// TestPeerIP covers the address-splitting the two previous implementations got
// wrong in different ways: one returned "[2001:db8::1]" with the brackets, the
// other mangled a bare IPv6 into "2001:db8:" by splitting on its own last colon.
func TestPeerIP(t *testing.T) {
	for _, tc := range []struct{ remoteAddr, want string }{
		{"203.0.113.9:44321", "203.0.113.9"},
		{"[2001:db8::1]:44321", "2001:db8::1"},
		{"203.0.113.9", "203.0.113.9"},
		{"2001:db8::1", "2001:db8::1"},
		{":", ":"},
	} {
		if got := PeerIP(request(tc.remoteAddr, nil)); got != tc.want {
			t.Errorf("PeerIP(%q) = %q, want %q", tc.remoteAddr, got, tc.want)
		}
	}
}

func TestFromRequest_FallsBackToPeerWithoutAResolvedValue(t *testing.T) {
	req := request("203.0.113.9:44321", map[string]string{"X-Forwarded-For": "198.51.100.7"})
	if got := FromRequest(req); got != "203.0.113.9" {
		t.Errorf("FromRequest = %q, want the peer: an unresolved request must not fall back to a header", got)
	}
}

func TestFromRequest_UsesTheResolvedValue(t *testing.T) {
	req := request("203.0.113.9:44321", nil)
	req = req.WithContext(NewContext(req.Context(), "198.51.100.7"))

	if got := FromRequest(req); got != "198.51.100.7" {
		t.Errorf("FromRequest = %q, want the resolved 198.51.100.7", got)
	}
	if ip, ok := FromContext(req.Context()); !ok || ip != "198.51.100.7" {
		t.Errorf("FromContext = (%q, %v), want (198.51.100.7, true)", ip, ok)
	}
	if _, ok := FromContext(context.Background()); ok {
		t.Error("FromContext reported a value on a context that carries none")
	}
}

func mustResolver(t *testing.T, cidrs []string) *Resolver {
	t.Helper()
	r, err := NewResolver(cidrs)
	if err != nil {
		t.Fatalf("NewResolver(%v): %v", cidrs, err)
	}
	return r
}
