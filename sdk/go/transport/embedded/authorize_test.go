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
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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

	_, err := tr.Request(context.Background(), "quicbaas.Auth", "Login", nil)

	assert.ErrorIs(t, err, operationRefusedError{})
	require.Len(t, calls, 1)
	assert.Empty(t, calls[0].perms)
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

func TestRequest_UnknownMethodNeverAuthorized(t *testing.T) {
	var calls []recordedCall
	tr := newAuthorizedTransport(t, &calls)

	_, err := tr.Request(context.Background(), "quicbaas.Secrets", "NoSuchVerb", nil)

	var unknown *UnknownMethodError
	assert.ErrorAs(t, err, &unknown)
	assert.Empty(t, calls)
}
