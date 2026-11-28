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
	"context"
	"net/http"
)

// dekHopKey marks the context of one tenant DEK keyring call: its session
// initialize, its authentication and its tool call. The marker holds the
// verb and whether the call carries key-protected data, never the data.
type dekHopKey struct{}

type dekHopMark struct {
	verb string
	data bool
}

// withDEKHopGuard marks ctx so that every hop of a request made with it must
// be https and name the configured host. When data is set (encrypt, decrypt,
// rewrap), no redirect is followed at all, even to the same host.
func withDEKHopGuard(ctx context.Context, verb string, data bool) context.Context {
	return context.WithValue(ctx, dekHopKey{}, dekHopMark{verb: verb, data: data})
}

func dekHopGuard(ctx context.Context) (dekHopMark, bool) {
	m, ok := ctx.Value(dekHopKey{}).(dekHopMark)
	return m, ok
}

// DEKHopError is returned, wrapped in a CodeForbidden error, when a tenant
// DEK keyring call would send the platform bearer or key-protected data over
// a hop that is not https, to a host other than the configured one, or (for
// encrypt, decrypt and rewrap) to a redirect target. Nothing is sent on the
// refused hop. The message names the verb and the reason only.
type DEKHopError struct {
	Verb   string
	Reason string
}

func (e *DEKHopError) Error() string {
	return "mcp transport: refused tenant DEK " + e.Verb + ": " + e.Reason
}

const (
	dekHopReasonPlaintext = "the connection is not TLS"
	dekHopReasonForeign   = "the hop names a host other than the configured one"
	dekHopReasonRedirect  = "key-protected data is never sent to a redirect target"
)

// checkDEKHop returns the refusal for req under mark, or nil when the hop
// is allowed.
func checkDEKHop(req *http.Request, host string, mark dekHopMark) *DEKHopError {
	switch {
	case req.URL.Scheme != "https":
		return &DEKHopError{Verb: mark.verb, Reason: dekHopReasonPlaintext}
	case req.URL.Host != host:
		return &DEKHopError{Verb: mark.verb, Reason: dekHopReasonForeign}
	case mark.data && req.Response != nil:
		return &DEKHopError{Verb: mark.verb, Reason: dekHopReasonRedirect}
	}
	return nil
}
