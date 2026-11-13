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

// Package dataplane describes the principal the integration suites act as
// when they read and write entities, and carries its bearer on the wire.
//
// Every data-plane RPC and route is permission-gated: a call with no bearer is
// refused as unauthenticated before any handler runs, and a bearer whose scp
// lacks the RPC's permission is refused as denied. The suites therefore act as
// a real tenant user whose only role holds exactly Permissions, never as an
// anonymous caller or the system admin.
//
// The package carries no build tag so the unit tier can pin Permissions to the
// server's authorization mapping; the enrollment that mints the bearer lives
// in test/integration/helpers.
package dataplane

import (
	"context"
	"errors"
	"net/http"
)

// RoleID names the tenant role the data-plane principal is bound to. It is
// the role's name; its id is TenantRoleID, because qrdb claims a role id
// across ALL tenants, so one fixed id would collide in every tenant after the
// first.
const RoleID = "integration-data-plane"

// TenantRoleID returns the id of the data-plane role in tenantID: RoleID
// suffixed with the tenant, so each suite process's tenant claims its own id.
func TenantRoleID(tenantID string) string {
	return RoleID + "-" + tenantID
}

// TenantPrefix prefixes the tenant each suite process provisions for its
// data-plane principal.
const TenantPrefix = "itest-dp"

// Permissions is the complete permission set of the data-plane role: the
// entity CRUD, list and range verbs and the batch verb. It is what the server's
// authorization mapping requires for Get, Put, Delete, List, Query, the Batch*
// RPCs, StreamBatchPut and the REST entity and batch routes.
var Permissions = []string{
	"kv:get",
	"kv:put",
	"kv:delete",
	"kv:list",
	"kv:range",
	"batch:execute",
}

// ErrNoTokenSource is returned when bearer credentials are built without a
// token source.
var ErrNoTokenSource = errors.New("dataplane: token source is required")

// ErrEmptyBearer is returned when the token source yields an empty bearer.
// Sending the call without one would only reproduce the unauthenticated
// refusal this package exists to avoid.
var ErrEmptyBearer = errors.New("dataplane: token source returned an empty bearer")

// TokenSource returns the current bearer. It is called on every request, so
// an implementation can refresh an expired token. The source may read the
// endpoint the call is addressed to with Target.
type TokenSource func(ctx context.Context) (string, error)

// targetKey is the context key under which the bearer credentials record
// the endpoint a call is addressed to.
type targetKey struct{}

// WithTarget returns ctx carrying target, the URL or host[:port] a call is
// addressed to, for a TokenSource to read with Target.
func WithTarget(ctx context.Context, target string) context.Context {
	return context.WithValue(ctx, targetKey{}, target)
}

// Target returns the target WithTarget recorded in ctx, or "".
func Target(ctx context.Context) string {
	target, _ := ctx.Value(targetKey{}).(string)
	return target
}

// BearerCredentials attaches the bearer as gRPC "authorization" metadata on
// every RPC. It implements credentials.PerRPCCredentials.
type BearerCredentials struct {
	source TokenSource
}

// NewBearerCredentials returns credentials that read the bearer from source.
func NewBearerCredentials(source TokenSource) (*BearerCredentials, error) {
	if source == nil {
		return nil, ErrNoTokenSource
	}
	return &BearerCredentials{source: source}, nil
}

// GetRequestMetadata returns the authorization metadata for one RPC. The
// source sees the RPC's URI (gRPC's "https://<authority>/<service>") through
// Target.
func (c *BearerCredentials) GetRequestMetadata(ctx context.Context, uri ...string) (map[string]string, error) {
	if len(uri) > 0 {
		ctx = WithTarget(ctx, uri[0])
	}
	bearer, err := bearerFrom(ctx, c.source)
	if err != nil {
		return nil, err
	}
	return map[string]string{"authorization": "Bearer " + bearer}, nil
}

// RequireTransportSecurity reports true: a bearer is never sent in clear.
func (c *BearerCredentials) RequireTransportSecurity() bool {
	return true
}

// BearerTransport sets the Authorization header on every request it forwards
// to its base transport.
type BearerTransport struct {
	source TokenSource
	base   http.RoundTripper
}

// NewBearerTransport returns a transport that reads the bearer from source and
// forwards to base, or to http.DefaultTransport when base is nil.
func NewBearerTransport(source TokenSource, base http.RoundTripper) (*BearerTransport, error) {
	if source == nil {
		return nil, ErrNoTokenSource
	}
	if base == nil {
		base = http.DefaultTransport
	}
	return &BearerTransport{source: source, base: base}, nil
}

// RoundTrip sends a clone of req carrying the bearer. The caller's request is
// left unmodified, as http.RoundTripper requires.
func (t *BearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	bearer, err := bearerFrom(WithTarget(req.Context(), req.URL.String()), t.source)
	if err != nil {
		return nil, err
	}
	authed := req.Clone(req.Context())
	authed.Header.Set("Authorization", "Bearer "+bearer)
	return t.base.RoundTrip(authed)
}

func bearerFrom(ctx context.Context, source TokenSource) (string, error) {
	bearer, err := source(ctx)
	if err != nil {
		return "", err
	}
	if bearer == "" {
		return "", ErrEmptyBearer
	}
	return bearer, nil
}
