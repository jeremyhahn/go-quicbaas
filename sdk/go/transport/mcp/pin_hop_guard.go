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

package mcp

import (
	"context"
	"net/http"
)

// pinHopKey marks the context of one barrier or ceremony tool call whose
// arguments carry a PIN that go-qrdb's SO PIN guard does not cover: a user
// PIN sent without an SO PIN. The marker holds no secret.
type pinHopKey struct{}

// withPINHopGuard marks ctx so that every hop of a request made with it,
// redirects included, must be https and name the configured host.
func withPINHopGuard(ctx context.Context) context.Context {
	return context.WithValue(ctx, pinHopKey{}, true)
}

func pinHopGuarded(ctx context.Context) bool {
	v, _ := ctx.Value(pinHopKey{}).(bool)
	return v
}

// PINRequiresTLSError is returned when a barrier or ceremony call that
// carries a user PIN would travel over a hop that is not https, the
// endpoint itself or a redirect. Nothing is sent.
type PINRequiresTLSError struct{}

func (e *PINRequiresTLSError) Error() string {
	return "mcp transport: a barrier PIN is sent only over TLS; this connection is not TLS"
}

// PINForeignHostError is returned when such a call is redirected to a host
// other than the configured endpoint's. Nothing is sent to that host.
type PINForeignHostError struct {
	Host string
}

func (e *PINForeignHostError) Error() string {
	return "mcp transport: refusing to send a barrier PIN to " + e.Host + ", which is not the configured host"
}

// pinHopRoundTripper refuses a request whose context carries the pinHopKey
// marker on any hop that is not https or names a host other than host.
// Other requests pass through unchanged.
type pinHopRoundTripper struct {
	base http.RoundTripper
	host string
}

func (rt *pinHopRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	if !pinHopGuarded(req.Context()) {
		return rt.base.RoundTrip(req)
	}
	if req.URL.Scheme != "https" {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, &PINRequiresTLSError{}
	}
	if req.URL.Host != rt.host {
		if req.Body != nil {
			_ = req.Body.Close()
		}
		return nil, &PINForeignHostError{Host: req.URL.Host}
	}
	return rt.base.RoundTrip(req)
}
