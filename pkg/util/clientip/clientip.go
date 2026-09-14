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

// Package clientip decides which address a request is attributed to.
//
// It exists because that decision was being made in two places with two
// implementations and one shared defect (go-quicbaas#156): both honoured
// X-Forwarded-For unconditionally, so the address used for rate-limit keys,
// audit records and trace attributes was whatever the caller typed.
//
// The rule here is that a forwarding header is evidence only when the peer
// that sent it is a proxy the operator named. Everything else is attributed to
// the transport peer, which cannot be forged over a TCP connection.
package clientip

import (
	"context"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// Resolver attributes requests to a client address.
//
// The zero value, and a nil *Resolver, trust no proxy at all and therefore
// always report the transport peer. That is deliberate: a Resolver that has
// not been configured is the one most likely to appear in a deployment nobody
// thought about, and the safe answer there is the address that cannot be
// forged. Trusting a proxy is opt-in.
type Resolver struct {
	trusted []netip.Prefix
}

// NewResolver builds a Resolver that honours forwarding headers only from the
// given CIDRs. An empty list yields a Resolver that trusts nothing.
//
// Single addresses are accepted as well as prefixes: "10.0.0.7" is read as
// "10.0.0.7/32", because naming one load balancer is the common case and
// making an operator write the mask invites writing it wrong.
func NewResolver(cidrs []string) (*Resolver, error) {
	r := &Resolver{}
	for _, raw := range cidrs {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}

		if prefix, err := netip.ParsePrefix(entry); err == nil {
			r.trusted = append(r.trusted, prefix.Masked())
			continue
		}

		addr, err := netip.ParseAddr(entry)
		if err != nil {
			return nil, &InvalidProxyError{Entry: raw, Cause: err}
		}
		r.trusted = append(r.trusted, netip.PrefixFrom(addr, addr.BitLen()))
	}
	return r, nil
}

// TrustsAny reports whether any proxy is trusted. It is here so a caller can
// log the difference between "configured to trust nothing" and "not configured
// at all" rather than inferring it.
func (r *Resolver) TrustsAny() bool {
	return r != nil && len(r.trusted) > 0
}

// ClientIP returns the address the request should be attributed to.
//
// Forwarding headers are consulted ONLY when the transport peer is a trusted
// proxy. When it is not — the common case for a listener reachable directly —
// the peer address is returned and the headers are ignored entirely, so a
// caller cannot choose its own rate-limit bucket or its own audit trail.
func (r *Resolver) ClientIP(req *http.Request) string {
	peer := PeerIP(req)
	if !r.trustsPeer(peer) {
		return peer
	}

	// X-Forwarded-For is appended left to right, so the leftmost entry is the
	// originating client as reported by the first proxy. Only a trusted peer
	// gets us here, so the chain is as good as that peer's word.
	if xff := req.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, found := strings.Cut(xff, ","); found {
			if candidate := strings.TrimSpace(first); candidate != "" {
				return candidate
			}
		} else if candidate := strings.TrimSpace(xff); candidate != "" {
			return candidate
		}
	}

	if xri := strings.TrimSpace(req.Header.Get("X-Real-IP")); xri != "" {
		return xri
	}

	return peer
}

func (r *Resolver) trustsPeer(peer string) bool {
	if r == nil || len(r.trusted) == 0 {
		return false
	}
	addr, err := netip.ParseAddr(peer)
	if err != nil {
		return false
	}
	// Compare on the unmapped form so a v4-in-v6 peer matches a v4 prefix.
	addr = addr.Unmap()
	for _, prefix := range r.trusted {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// PeerIP returns the transport peer's address, never a header value.
//
// SplitHostPort rather than a split on the last colon: net/http writes an IPv6
// peer as "[2001:db8::1]:1234", and the naive form returns "[2001:db8::1]" --
// brackets included, which no IP parser accepts -- while a bare "2001:db8::1"
// gets mangled into "2001:db8:" by splitting on its own last colon. Both then
// flow into rate-limit keys and audit records, where a mangled address is worse
// than an absent one.
func PeerIP(req *http.Request) string {
	if host, _, err := net.SplitHostPort(req.RemoteAddr); err == nil && host != "" {
		return host
	}
	// No port to split, or nothing useful in it. A bracket-less multi-colon
	// value such as "::1" is a valid address in its own right, so it is left
	// whole rather than truncated at its last colon.
	return req.RemoteAddr
}

type contextKey struct{}

// NewContext returns a context carrying the resolved client address.
func NewContext(ctx context.Context, ip string) context.Context {
	return context.WithValue(ctx, contextKey{}, ip)
}

// FromContext returns the client address resolved earlier in the request, and
// whether one was resolved at all.
//
// Call sites use this instead of re-reading headers, so the trusted-proxy
// decision is made once per request rather than repeated -- and cannot be made
// differently in two places, which is how the two implementations this package
// replaces drifted apart.
func FromContext(ctx context.Context) (string, bool) {
	ip, ok := ctx.Value(contextKey{}).(string)
	return ip, ok
}

// FromRequest returns the client address resolved for this request, falling
// back to the transport peer when nothing resolved it.
//
// The fallback is the safe direction: a handler reached without the resolving
// middleware attributes to the peer rather than to a header.
func FromRequest(req *http.Request) string {
	if ip, ok := FromContext(req.Context()); ok {
		return ip
	}
	return PeerIP(req)
}
