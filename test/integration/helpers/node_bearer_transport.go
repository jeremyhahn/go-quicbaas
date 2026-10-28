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

package helpers

import (
	"context"
	"net/http"
	"sync"

	"github.com/jeremyhahn/go-quicbaas/test/integration/dataplane"
)

// nodeTokenSource returns the source of a principal's bearer at node.
type nodeTokenSource func(ctx context.Context, node string) (dataplane.TokenSource, error)

// nodeBearerTransport sends each request with the bearer a principal holds at
// the node the request goes to. A node accepts only the bearers its own OIDC
// provider issued, so a suite that calls every node of the cluster holds one
// bearer per node.
type nodeBearerTransport struct {
	base      http.RoundTripper
	principal string
	sourceFor nodeTokenSource
	nodes     sync.Map // node base URL -> bearer http.RoundTripper
}

// newNodeBearerTransport returns a transport over base that sends principal's
// bearer from sourceFor at each node.
func newNodeBearerTransport(base http.RoundTripper, principal string, sourceFor nodeTokenSource) *nodeBearerTransport {
	return &nodeBearerTransport{base: base, principal: principal, sourceFor: sourceFor}
}

// nodeTransport returns the bearer transport for node, obtaining the
// principal's bearer there on first use.
func (n *nodeBearerTransport) nodeTransport(ctx context.Context, node string) (http.RoundTripper, error) {
	if transport, ok := n.nodes.Load(node); ok {
		return transport.(http.RoundTripper), nil
	}
	source, err := n.sourceFor(ctx, node)
	if err != nil {
		return nil, err
	}
	transport, err := dataplane.NewBearerTransport(source, n.base)
	if err != nil {
		return nil, &PrincipalAuthError{Principal: n.principal, Step: "HTTP client", Cause: err}
	}
	stored, _ := n.nodes.LoadOrStore(node, transport)
	return stored.(http.RoundTripper), nil
}

// RoundTrip implements http.RoundTripper.
func (n *nodeBearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	transport, err := n.nodeTransport(req.Context(), req.URL.Scheme+"://"+req.URL.Host)
	if err != nil {
		if req.Body != nil {
			if closeErr := req.Body.Close(); closeErr != nil {
				return nil, &PrincipalAuthError{Principal: n.principal, Step: "close request body", Cause: closeErr}
			}
		}
		return nil, err
	}
	return transport.RoundTrip(req)
}
