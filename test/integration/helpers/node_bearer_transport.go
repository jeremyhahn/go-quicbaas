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
	"net/url"
	"strings"
	"sync"

	"google.golang.org/grpc/credentials"

	"github.com/jeremyhahn/go-quicbaas/test/integration/dataplane"
)

// nodeTokenSource returns the source of a principal's bearer at node.
type nodeTokenSource func(ctx context.Context, node string) (dataplane.TokenSource, error)

// nodeCache holds, per node, a value built over the bearer a principal holds
// at that node. A node accepts only the bearers its own OIDC provider issued,
// so a suite that calls every node of the cluster holds one bearer per node.
type nodeCache[T any] struct {
	principal string
	step      string
	sourceFor nodeTokenSource
	build     func(dataplane.TokenSource) (T, error)
	nodes     sync.Map // node base URL -> T
}

// get returns the value for node, obtaining the principal's bearer there on
// first use.
func (c *nodeCache[T]) get(ctx context.Context, node string) (T, error) {
	if value, ok := c.nodes.Load(node); ok {
		return value.(T), nil
	}
	var zero T
	source, err := c.sourceFor(ctx, node)
	if err != nil {
		return zero, err
	}
	value, err := c.build(source)
	if err != nil {
		return zero, &PrincipalAuthError{Principal: c.principal, Step: c.step, Cause: err}
	}
	stored, _ := c.nodes.LoadOrStore(node, value)
	return stored.(T), nil
}

// nodeBearerTransport sends each request with the bearer a principal holds at
// the node the request goes to.
type nodeBearerTransport struct {
	principal string
	nodes     *nodeCache[http.RoundTripper]
}

// newNodeBearerTransport returns a transport over base that sends principal's
// bearer from sourceFor at each node.
func newNodeBearerTransport(base http.RoundTripper, principal string, sourceFor nodeTokenSource) *nodeBearerTransport {
	return &nodeBearerTransport{principal: principal, nodes: &nodeCache[http.RoundTripper]{
		principal: principal,
		step:      "HTTP client",
		sourceFor: sourceFor,
		build: func(source dataplane.TokenSource) (http.RoundTripper, error) {
			return dataplane.NewBearerTransport(source, base)
		},
	}}
}

// nodeTransport returns the bearer transport for node, obtaining the
// principal's bearer there on first use.
func (n *nodeBearerTransport) nodeTransport(ctx context.Context, node string) (http.RoundTripper, error) {
	return n.nodes.get(ctx, node)
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

// nodeBearerCredentials presents, on each RPC, the bearer a principal holds at
// the node the RPC goes to: the gRPC counterpart of nodeBearerTransport. It
// implements credentials.PerRPCCredentials.
type nodeBearerCredentials struct {
	home  string
	nodes *nodeCache[credentials.PerRPCCredentials]
}

// newNodeBearerCredentials returns credentials that send principal's bearer
// from sourceFor at each node. sourceFor is keyed by the node's REST base URL,
// the URL the principal logs in at; home is the REST base URL of the node that
// an RPC naming no cluster node is taken to go to.
func newNodeBearerCredentials(home, principal string, sourceFor nodeTokenSource) *nodeBearerCredentials {
	return &nodeBearerCredentials{home: strings.TrimRight(home, "/"), nodes: &nodeCache[credentials.PerRPCCredentials]{
		principal: principal,
		step:      "gRPC credentials",
		sourceFor: sourceFor,
		build: func(source dataplane.TokenSource) (credentials.PerRPCCredentials, error) {
			return dataplane.NewBearerCredentials(source)
		},
	}}
}

// GetRequestMetadata returns the authorization metadata for one RPC, carrying
// the bearer of the node the RPC's URI names.
func (n *nodeBearerCredentials) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	node := n.home
	if restNode, ok := restNodeOf(uri...); ok {
		node = strings.TrimRight(restNode, "/")
	}
	creds, err := n.nodes.get(ctx, node)
	if err != nil {
		return nil, err
	}
	return creds.GetRequestMetadata(ctx, uri...)
}

// RequireTransportSecurity reports that the bearer is only sent over TLS.
func (n *nodeBearerCredentials) RequireTransportSecurity() bool {
	return true
}

// restNodeOf returns the REST base URL of the cluster node whose gRPC endpoint
// (GetClusterEndpoints) the RPC's URI names; gRPC passes the URI as
// "https://<authority>/<service>". It reports false when the URI names no
// cluster node.
func restNodeOf(uri ...string) (string, bool) {
	if len(uri) == 0 {
		return "", false
	}
	target, err := url.Parse(uri[0])
	if err != nil {
		return "", false
	}
	restNodes := GetRESTEndpoints()
	for i, endpoint := range GetClusterEndpoints() {
		if endpoint == target.Host && i < len(restNodes) {
			return restNodes[i], true
		}
	}
	return "", false
}
