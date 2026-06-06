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

// Package main: untagged test helpers for the SDK-wrapping factory clients.
//
// These helpers are deliberately free of any build tag so they remain
// available to factory tests that are themselves untagged (for example
// ca_factory_test.go). The exhaustive per-method behavioral coverage for
// every SDK wrapper lives in factories_sdk_test.go.
package main

import (
	"context"
	"testing"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// fakeSDKTransport returns the configured (resp, err) pair on every
// Request and no-ops the rest of the transport surface.
type fakeSDKTransport struct {
	resp interface{}
	err  error
}

func (f *fakeSDKTransport) Request(_ context.Context, _, _ string, _ interface{}) (interface{}, error) {
	return f.resp, f.err
}
func (f *fakeSDKTransport) RequestStream(_ context.Context, _, _ string, _ interface{}) (sdktransport.Stream, error) {
	return nil, nil
}
func (f *fakeSDKTransport) Conn() interface{} { return nil }
func (f *fakeSDKTransport) Close() error      { return nil }
func (f *fakeSDKTransport) Healthy() bool     { return true }

// fakeSDKClient embeds mockClient (untagged, defined in client_test.go)
// and supplies a configurable Transport. The embedded mockClient satisfies
// every other method on quicbaas.Client with a nil return.
type fakeSDKClient struct {
	mockClient
	transport quicbaas.Transport
}

func (c *fakeSDKClient) Transport() quicbaas.Transport { return c.transport }

// newFakeSDKClient builds a fakeSDKClient whose transport returns the
// supplied (resp, err) pair on every Request.
func newFakeSDKClient(resp interface{}, err error) *fakeSDKClient {
	return &fakeSDKClient{transport: &fakeSDKTransport{resp: resp, err: err}}
}

// withFakeSDKFactory installs a sdkClientFactory returning the supplied
// client and restores the previous factory on cleanup.
func withFakeSDKFactory(t *testing.T, c quicbaas.Client, err error) {
	t.Helper()
	orig := sdkClientFactory
	sdkClientFactory = func() (quicbaas.Client, error) { return c, err }
	t.Cleanup(func() { sdkClientFactory = orig })
}
