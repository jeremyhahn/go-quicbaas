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
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// errOpaque is an error no table knows, standing in for a service fault.
var errOpaque = stderrors.New("disk on fire")

// getFailing returns an embedded transport whose KV Get fails with err.
func getFailing(t *testing.T, err error) *Transport {
	t.Helper()
	tr, newErr := New(&mockQuicBaaS{
		getFunc: func(context.Context, *service.GetRequest) (*service.GetResponse, error) {
			return nil, err
		},
	})
	require.NoError(t, newErr)
	return tr
}

func TestEmbeddedRequest_ClassifiesServiceErrors(t *testing.T) {
	cases := []struct {
		name string
		err  *pkgerrors.QBError
		want sdkerrors.ErrorCode
	}{
		{"not found", pkgerrors.New(pkgerrors.ErrNotFound, "kv.Get", "key k"), sdkerrors.CodeNotFound},
		{"invalid input", pkgerrors.New(pkgerrors.ErrInvalidInput, "kv.Get", "key is empty"), sdkerrors.CodeInvalidInput},
		{"permission denied", pkgerrors.New(pkgerrors.ErrForbidden, "kv.Get", "not yours"), sdkerrors.CodeForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := getFailing(t, tc.err).Request(context.Background(), "quicbaas.KV", "Get",
				&service.GetRequest{})

			var coded *sdkerrors.QBError
			require.ErrorAs(t, err, &coded)
			assert.Equal(t, tc.want, coded.Code)
			assert.Equal(t, tc.err.Op, coded.Op)
			assert.Equal(t, tc.err.Err.Error(), coded.Message)
			assert.Same(t, tc.err, stderrors.Unwrap(err), "the service error stays the cause")
		})
	}
}

func TestEmbeddedRequest_PassesSDKErrorThrough(t *testing.T) {
	coded := sdkerrors.NewConflict("kv.Get", "revision moved")

	_, err := getFailing(t, coded).Request(context.Background(), "quicbaas.KV", "Get",
		&service.GetRequest{})

	assert.Same(t, coded, err)
}

func TestEmbeddedRequest_UnknownErrorIsInternal(t *testing.T) {
	_, err := getFailing(t, errOpaque).Request(context.Background(), "quicbaas.KV", "Get",
		&service.GetRequest{})

	assert.Equal(t, sdkerrors.CodeInternal, sdkerrors.Code(err))
	assert.ErrorIs(t, err, errOpaque)
}

func TestEmbeddedRequestStream_ClassifiesHandlerError(t *testing.T) {
	notFound := pkgerrors.New(pkgerrors.ErrNotFound, "kv.List", "prefix p")
	tr, err := New(&mockQuicBaaS{
		listFunc: func(_ context.Context, _ *service.ListRequest, out chan<- *service.ListResponse) error {
			close(out)
			return notFound
		},
	})
	require.NoError(t, err)

	s, err := tr.RequestStream(context.Background(), "quicbaas.KV", "List", &service.ListRequest{})
	require.NoError(t, err)
	msg, err := s.Recv()
	require.NoError(t, err)

	streamErr, ok := msg.(error)
	require.True(t, ok, "the stream ends with its handler's error, got %T", msg)
	assert.Equal(t, sdkerrors.CodeNotFound, sdkerrors.Code(streamErr))
	assert.ErrorIs(t, streamErr, notFound)
}

func TestClassifyServiceError_CodesTransportErrors(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want sdkerrors.ErrorCode
	}{
		{"invalid request type", NewInvalidRequestTypeError("s", "m", "want", "got"), sdkerrors.CodeInvalidInput},
		{"invalid user id", &InvalidUserIDError{Service: "s", Method: "m", Value: "x"}, sdkerrors.CodeInvalidInput},
		{"permission denied", NewPermissionDeniedError("s", "m", "other subject"), sdkerrors.CodeForbidden},
		{"service not configured", NewServiceNotConfiguredError("s"), sdkerrors.CodeUnavailable},
		{"unknown method", NewUnknownMethodError("s", "m"), sdkerrors.CodeUnimplemented},
		{"stream not supported", NewStreamNotSupportedError("s", "m"), sdkerrors.CodeUnimplemented},
		{"canceled", context.Canceled, sdkerrors.CodeCanceled},
		{"deadline", context.DeadlineExceeded, sdkerrors.CodeTimeout},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := classifyServiceError("s", "m", tc.err)
			assert.Equal(t, tc.want, sdkerrors.Code(got))
			assert.ErrorIs(t, got, tc.err)
		})
	}
}
