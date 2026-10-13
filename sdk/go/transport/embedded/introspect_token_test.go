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

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	qberrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	authclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/auth"
)

// introspectedSession answers like the service: an empty token is invalid
// input, "garbage" is inactive and "live" is an attested session.
func introspectedSession(_ context.Context, req *service.IntrospectTokenRequest) (*service.TokenIntrospection, error) {
	switch req.Token {
	case "":
		return nil, &qberrors.QBError{
			Code: qberrors.ErrInvalidInput,
			Op:   "service.IntrospectToken",
			Err:  &api.RequiredFieldError{Field: "token"},
		}
	case "live":
		return &service.TokenIntrospection{Active: true, Subject: "user-1", AttestLevel: "hardware"}, nil
	}
	return &service.TokenIntrospection{Active: false}, nil
}

func TestAuthClientIntrospectToken_OverEmbeddedTransport(t *testing.T) {
	tr, err := New(&mockQuicBaaS{introspectTokenFunc: introspectedSession})
	require.NoError(t, err)
	client := authclient.NewClient(tr)

	got, err := client.IntrospectToken(context.Background(), "live")
	require.NoError(t, err)
	assert.Equal(t, &service.TokenIntrospection{Active: true, Subject: "user-1", AttestLevel: "hardware"}, got)

	got, err = client.IntrospectToken(context.Background(), "garbage")
	require.NoError(t, err)
	assert.Equal(t, &service.TokenIntrospection{Active: false}, got)
}

func TestHandleIntrospectToken_MissingTokenIsInvalidInput(t *testing.T) {
	tr, err := New(&mockQuicBaaS{introspectTokenFunc: introspectedSession})
	require.NoError(t, err)

	resp, err := tr.Request(context.Background(), "quicbaas.Auth", "IntrospectToken", &service.IntrospectTokenRequest{})
	var qbErr *qberrors.QBError
	require.ErrorAs(t, err, &qbErr)
	assert.Equal(t, qberrors.ErrInvalidInput, qbErr.Code)
	assert.Nil(t, resp)
}

func TestHandleIntrospectToken_RejectsWrongRequestType(t *testing.T) {
	tr, err := New(&mockQuicBaaS{introspectTokenFunc: introspectedSession})
	require.NoError(t, err)

	_, err = tr.Request(context.Background(), "quicbaas.Auth", "IntrospectToken", "wrong")
	var typeErr *InvalidRequestTypeError
	assert.ErrorAs(t, err, &typeErr)
}
