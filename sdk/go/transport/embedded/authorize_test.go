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

package embedded

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/qrdbservice"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// operationRefusedError is what a refusing test authorizer returns.
type operationRefusedError struct{}

func (operationRefusedError) Error() string { return "operation refused" }

// boundKey marks a context the test binder bound.
type boundKey struct{}

// recordedCall is one call a recordingAuthorizer saw.
type recordedCall struct {
	operation string
	perms     []string
	bound     bool
}

// recordingAuthorizer records what it is asked and refuses every operation,
// so no call reaches the (nil) service facade.
func recordingAuthorizer(calls *[]recordedCall) OperationAuthorizer {
	return func(ctx context.Context, operation string, perms []string) error {
		*calls = append(*calls, recordedCall{operation: operation, perms: perms, bound: ctx.Value(boundKey{}) != nil})
		return operationRefusedError{}
	}
}

func newAuthorizedTransport(t *testing.T, calls *[]recordedCall) *Transport {
	t.Helper()
	bind := func(ctx context.Context) (context.Context, error) {
		return context.WithValue(ctx, boundKey{}, true), nil
	}
	tr, err := New(struct{ service.QuicBaaS }{},
		WithContextBinder(bind),
		WithOperationAuthorizer(recordingAuthorizer(calls)))
	require.NoError(t, err)
	return tr
}

// Every declared operation names a dispatchable unary or stream handler, so
// no permission is declared for a key the transport never serves.
func TestOperationPermissions_KeysAreHandlers(t *testing.T) {
	for key, perms := range operationPermissions {
		_, unary := unaryHandlers[key]
		_, stream := streamHandlers[key]
		assert.True(t, unary || stream, "%s declares %v but has no handler", key, perms)
		assert.NotEmpty(t, perms, "%s declares an empty permission list", key)
	}
}

func TestRequest_AuthorizerRefusalStopsDispatch(t *testing.T) {
	var calls []recordedCall
	tr := newAuthorizedTransport(t, &calls)

	_, err := tr.Request(context.Background(), "quicbaas.Secrets", "Get", nil)

	assert.ErrorIs(t, err, operationRefusedError{})
	require.Len(t, calls, 1)
	assert.Equal(t, recordedCall{operation: "quicbaas.Secrets/Get", perms: []string{"secrets:read"}, bound: true}, calls[0])
}

func TestRequest_UndeclaredOperationPassesNoPermissions(t *testing.T) {
	var calls []recordedCall
	tr := newAuthorizedTransport(t, &calls)

	_, err := tr.Request(context.Background(), "quicbaas.IAM.Policy", "Create", nil)

	assert.ErrorIs(t, err, operationRefusedError{})
	require.Len(t, calls, 1)
	assert.Empty(t, calls[0].perms)
}

// A public operation is served without asking the authorizer, so a caller
// holding no grant reads the OIDC discovery documents.
func TestRequest_PublicOperationSkipsTheAuthorizer(t *testing.T) {
	var calls []recordedCall
	tr, err := New(&authTestService{oidc: newTestOIDCProvider(t)},
		WithOperationAuthorizer(recordingAuthorizer(&calls)))
	require.NoError(t, err)

	jwks, err := tr.Request(context.Background(), "quicbaas.Auth", "GetJWKS", nil)
	require.NoError(t, err)
	assert.NotEmpty(t, jwks.(*service.JWKS).Keys)

	cfg, err := tr.Request(context.Background(), "quicbaas.Auth", "GetOpenIDConfiguration", nil)
	require.NoError(t, err)
	assert.Equal(t, "https://issuer.example.com", cfg.(*service.OpenIDConfiguration).Issuer)

	assert.Empty(t, calls, "a public operation reached the authorizer")
}

// Every Auth verb is classified: public, or declaring the permission the
// authorizer checks. An unclassified verb would fall to default deny.
func TestAuthOperations_PublicOrDeclared(t *testing.T) {
	for key := range unaryHandlers {
		if !strings.HasPrefix(key, "quicbaas.Auth/") {
			continue
		}
		_, public := publicOperations[key]
		_, declared := operationPermissions[key]
		assert.True(t, public != declared, "%s: public=%v declared=%v, want exactly one", key, public, declared)
	}
}

// Every public operation is a served unary verb that declares no permission,
// so the public set cannot shadow a declared grant.
func TestPublicOperations_AreUndeclaredHandlers(t *testing.T) {
	for key := range publicOperations {
		_, ok := unaryHandlers[key]
		assert.True(t, ok, "%s is public but has no handler", key)
		_, declared := operationPermissions[key]
		assert.False(t, declared, "%s is public and declares a permission", key)
	}
}

func TestRequestStream_AuthorizerRefusalStopsDispatch(t *testing.T) {
	var calls []recordedCall
	tr := newAuthorizedTransport(t, &calls)

	s, err := tr.RequestStream(context.Background(), "quicbaas.KV", "Watch", nil)

	assert.Nil(t, s)
	assert.ErrorIs(t, err, operationRefusedError{})
	require.Len(t, calls, 1)
	assert.Equal(t, "quicbaas.KV/Watch", calls[0].operation)
	assert.True(t, calls[0].bound, "the authorizer sees the bound context")
}

// TestReadOperations_DeclareTheServingVerb pins the read operations to the
// permission of the go-qrdb verb that serves them, as their proto RPCs
// declare (api/proto/quicbaas.proto).
func TestReadOperations_DeclareTheServingVerb(t *testing.T) {
	unary := map[string]string{
		"quicbaas.KV/ListEntityTypes":   "kv:range",          // Scan of the tenant keyspace
		"quicbaas.Cluster/Status":       "admin:topology",    // GetTopology
		"quicbaas.Cluster/GetLeader":    "admin:shard_state", // GetShardState
		"quicbaas.Cluster/Nodes":        "admin:topology",    // ClusterStatus, so GetTopology
		"quicbaas.TimeSeries/Aggregate": "kv:range",          // Scan of entity points
		"quicbaas.TimeSeries/Query":     "timeseries:query",  // TSQuery
	}
	for key, want := range unary {
		assert.Equal(t, []string{want}, operationPermissions[key], key)
	}

	stream := map[string]string{
		"quicbaas.KV/List":          "kv:range", // Scan
		"quicbaas.TimeSeries/Query": "kv:range", // TimeSeriesQuery scans entities
	}
	for key, want := range stream {
		assert.Equal(t, []string{want}, streamPermissions(key), key)
	}
}

// TestRequestStream_PassesTheStreamPermissions proves a stream sharing its
// key with a unary handler is authorized for the stream's own verb.
func TestRequestStream_PassesTheStreamPermissions(t *testing.T) {
	var calls []recordedCall
	tr := newAuthorizedTransport(t, &calls)

	_, err := tr.Request(context.Background(), "quicbaas.TimeSeries", "Query", nil)
	assert.ErrorIs(t, err, operationRefusedError{})
	_, err = tr.RequestStream(context.Background(), "quicbaas.TimeSeries", "Query", nil)
	assert.ErrorIs(t, err, operationRefusedError{})

	require.Len(t, calls, 2)
	assert.Equal(t, []string{"timeseries:query"}, calls[0].perms, "unary Query is TSQuery")
	assert.Equal(t, []string{"kv:range"}, calls[1].perms, "streamed Query is TimeSeriesQuery")
}

// TestStreamOperationPermissions_KeysAreStreamHandlers guards the stream
// overrides against a key no stream serves.
func TestStreamOperationPermissions_KeysAreStreamHandlers(t *testing.T) {
	for key, perms := range streamOperationPermissions {
		_, ok := streamHandlers[key]
		assert.True(t, ok, "%s declares %v but has no stream handler", key, perms)
		assert.NotEmpty(t, perms, "%s declares an empty permission list", key)
	}
}

// securityServices are the embedded services the SDK's Security() surface
// dispatches to.
var securityServices = []string{
	"quicbaas.Auth", "quicbaas.FIDO2", "quicbaas.PIV", "quicbaas.KMS",
	"quicbaas.DEK", "quicbaas.Secrets", "quicbaas.Crypto", "quicbaas.Certificate",
	"quicbaas.DynamicSecrets", "quicbaas.RBAC", "quicbaas.ACME", "quicbaas.Trust",
	"quicbaas.Attestation", "quicbaas.SSH",
}

// TestSecurityOperationPermissions_WithinPrincipalGrants guards the embedded
// principal's grant ceiling: go-qrdb intersects a delegation's scope with the
// principal's grants, so a Security permission the ceiling lacks is refused
// to every delegation, whatever its scope grants.
func TestSecurityOperationPermissions_WithinPrincipalGrants(t *testing.T) {
	grants, err := qrdbservice.Grants()
	require.NoError(t, err)
	granted := make(map[string]bool, len(grants))
	for _, p := range grants {
		granted[p.String()] = true
	}
	checked := 0
	for key, perms := range operationPermissions {
		service, _, _ := strings.Cut(key, "/")
		if !slices.Contains(securityServices, service) {
			continue
		}
		for _, perm := range perms {
			checked++
			assert.True(t, granted[perm], "%s needs %s, which the embedded principal is not granted", key, perm)
		}
	}
	require.NotZero(t, checked, "no Security operation was checked")
}

// schedulerOperationPermissions is the permission each Scheduler RPC declares
// in api/proto/scheduler.proto, keyed by the embedded handler method name.
var schedulerOperationPermissions = map[string]string{
	"CreateJob":      "scheduler:create",
	"GetJob":         "scheduler:read",
	"UpdateJob":      "scheduler:update",
	"DeleteJob":      "scheduler:delete",
	"ListJobs":       "scheduler:list",
	"TriggerJob":     "scheduler:trigger",
	"PauseJob":       "scheduler:pause",
	"ResumeJob":      "scheduler:resume",
	"CancelJob":      "scheduler:cancel",
	"GetJobHistory":  "scheduler:history",
	"GetStatus":      "scheduler:status",
	"RegisterNode":   "placement:manage",
	"RenewNode":      "placement:manage",
	"DeregisterNode": "placement:manage",
	"CordonNode":     "placement:manage",
	"UncordonNode":   "placement:manage",
	"DrainNode":      "placement:manage",
	"GetNode":        "placement:read",
	"ListNodes":      "placement:read",
	"GetPlacement":   "placement:read",
	"ListPlacements": "placement:read",
	"Place":          "placement:place",
	"Release":        "placement:place",
}

func TestSchedulerOperations_DeclareTheProtoPermission(t *testing.T) {
	checked := 0
	for key := range unaryHandlers {
		method, ok := strings.CutPrefix(key, "quicbaas.Scheduler/")
		if !ok {
			continue
		}
		checked++
		want, known := schedulerOperationPermissions[method]
		require.True(t, known, "%s has no proto permission in the test table", key)
		assert.Equal(t, []string{want}, operationPermissions[key], "%s must declare its proto permission", key)
	}
	require.Len(t, schedulerOperationPermissions, checked, "every Scheduler RPC must have an embedded handler")
}

func TestSchedulerOperationPermissions_WithinPrincipalGrants(t *testing.T) {
	grants, err := qrdbservice.Grants()
	require.NoError(t, err)
	granted := make(map[string]bool, len(grants))
	for _, p := range grants {
		granted[p.String()] = true
	}
	for method, perm := range schedulerOperationPermissions {
		assert.True(t, granted[perm], "quicbaas.Scheduler/%s needs %s, which the embedded principal is not granted", method, perm)
	}
}

func TestRequest_UnknownMethodNeverAuthorized(t *testing.T) {
	var calls []recordedCall
	tr := newAuthorizedTransport(t, &calls)

	_, err := tr.Request(context.Background(), "quicbaas.Secrets", "NoSuchVerb", nil)

	var unknown *UnknownMethodError
	assert.ErrorAs(t, err, &unknown)
	assert.Empty(t, calls)
}
