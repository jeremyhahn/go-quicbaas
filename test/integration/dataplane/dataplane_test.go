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

package dataplane

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// errSourceFailed stands in for an enrollment or login failure.
var errSourceFailed = errors.New("token source failed")

func staticSource(bearer string) TokenSource {
	return func(context.Context) (string, error) { return bearer, nil }
}

func failingSource(context.Context) (string, error) {
	return "", errSourceFailed
}

// recordingTransport captures the request it is asked to send.
type recordingTransport struct {
	got *http.Request
}

func (r *recordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r.got = req
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

func TestBearerCredentials_AttachesAuthorization(t *testing.T) {
	creds, err := NewBearerCredentials(staticSource("tok"))
	require.NoError(t, err)

	md, err := creds.GetRequestMetadata(context.Background())
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"authorization": "Bearer tok"}, md)
	assert.True(t, creds.RequireTransportSecurity(), "a bearer must never travel in clear")
}

func TestBearerCredentials_Errors(t *testing.T) {
	_, err := NewBearerCredentials(nil)
	require.ErrorIs(t, err, ErrNoTokenSource)

	creds, err := NewBearerCredentials(failingSource)
	require.NoError(t, err)
	_, err = creds.GetRequestMetadata(context.Background())
	require.ErrorIs(t, err, errSourceFailed)

	creds, err = NewBearerCredentials(staticSource(""))
	require.NoError(t, err)
	_, err = creds.GetRequestMetadata(context.Background())
	require.ErrorIs(t, err, ErrEmptyBearer, "an empty bearer must fail the call, not send it anonymously")
}

func TestBearerTransport_SetsHeaderOnClone(t *testing.T) {
	base := &recordingTransport{}
	transport, err := NewBearerTransport(staticSource("tok"), base)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, "https://node.invalid/api/v1/entities/t/1", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.NotNil(t, base.got)
	assert.Equal(t, "Bearer tok", base.got.Header.Get("Authorization"))
	assert.Empty(t, req.Header.Get("Authorization"), "the caller's request must not be modified")
}

func TestBearerTransport_Errors(t *testing.T) {
	_, err := NewBearerTransport(nil, nil)
	require.ErrorIs(t, err, ErrNoTokenSource)

	base := &recordingTransport{}
	transport, err := NewBearerTransport(failingSource, base)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, "https://node.invalid/", nil)
	require.NoError(t, err)
	resp, err := transport.RoundTrip(req)
	if resp != nil {
		require.NoError(t, resp.Body.Close())
	}
	require.ErrorIs(t, err, errSourceFailed)
	assert.Nil(t, resp)
	assert.Nil(t, base.got, "no request may be sent when the bearer cannot be obtained")
}

func TestNewBearerTransport_DefaultsBase(t *testing.T) {
	transport, err := NewBearerTransport(staticSource("tok"), nil)
	require.NoError(t, err)
	assert.Same(t, http.DefaultTransport, transport.base)
}

func targetSource(ctx context.Context) (string, error) {
	return "tok-" + Target(ctx), nil
}

func TestBearerCredentials_RecordsTheTarget(t *testing.T) {
	creds, err := NewBearerCredentials(targetSource)
	require.NoError(t, err)

	md, err := creds.GetRequestMetadata(context.Background(), "https://test-node2:50051/pkg.Svc")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"authorization": "Bearer tok-https://test-node2:50051/pkg.Svc"}, md)
}

func TestBearerTransport_RecordsTheTarget(t *testing.T) {
	base := &recordingTransport{}
	transport, err := NewBearerTransport(targetSource, base)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodGet, "https://test-node2:8080/api/v1/kv/a/b", nil)
	require.NoError(t, err)

	resp, err := transport.RoundTrip(req)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())

	require.NotNil(t, base.got)
	assert.Equal(t, "Bearer tok-https://test-node2:8080/api/v1/kv/a/b", base.got.Header.Get("Authorization"))
}
