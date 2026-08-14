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

// Package main: coverage for ssh_client.go — the CLI's SSH client seam.
//
// The load-bearing assertion in this file is that the CLI reaches SSH
// through quicbaas.SDK().Security().SSH() and NOT through a go-xkms client
// of its own. That is verified structurally: the SDK client is built on a
// recording transport, and the recorded (service, method) pair proves the
// call left the CLI as a QuicBaaS "quicbaas.SSH" request.
package main

import (
	"context"
	stderrors "errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	quicbaas "github.com/jeremyhahn/go-quicbaas/sdk/go"
	sshSDK "github.com/jeremyhahn/go-quicbaas/sdk/go/client/ssh"
	sdktransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// =============================================================================
// CLISSHClientBuildError
// =============================================================================

func TestCLISSHClientBuildError_MessageAndUnwrap(t *testing.T) {
	inner := stderrors.New("permission denied")
	e := &CLISSHClientBuildError{Op: "sdk_client", Err: inner}
	assert.Contains(t, e.Error(), "sdk_client")
	assert.Contains(t, e.Error(), "permission denied")
	assert.ErrorIs(t, e, inner)
}

// =============================================================================
// cliSSHOwner
// =============================================================================

func TestCLISSHOwner_PrefersUSER(t *testing.T) {
	t.Setenv("USER", "alice")
	t.Setenv("LOGNAME", "bob")
	assert.Equal(t, "alice", cliSSHOwner())
}

func TestCLISSHOwner_FallsBackToLOGNAME(t *testing.T) {
	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "bob")
	assert.Equal(t, "bob", cliSSHOwner())
}

func TestCLISSHOwner_FallsBackToDefault(t *testing.T) {
	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "")
	assert.Equal(t, "default", cliSSHOwner())
}

// =============================================================================
// Recording transport — the SDK seam probe
// =============================================================================

// recordingSSHTransport captures the (service, method) pair of the last
// request and returns a canned response.
type recordingSSHTransport struct {
	service string
	method  string
	resp    interface{}
	err     error
}

func (r *recordingSSHTransport) Request(_ context.Context, service, method string, _ interface{}) (interface{}, error) {
	r.service = service
	r.method = method
	return r.resp, r.err
}

func (r *recordingSSHTransport) RequestStream(_ context.Context, _, _ string, _ interface{}) (sdktransport.Stream, error) {
	return nil, stderrors.New("streaming not supported")
}

func (r *recordingSSHTransport) Conn() interface{} { return nil }
func (r *recordingSSHTransport) Close() error      { return nil }
func (r *recordingSSHTransport) Healthy() bool     { return true }

// withSSHFactories swaps the SDK client factory for the duration of a test.
func withSDKClientFactory(t *testing.T, fn SDKClientFactory) {
	t.Helper()
	original := sdkClientFactory
	sdkClientFactory = fn
	t.Cleanup(func() { sdkClientFactory = original })
}

// =============================================================================
// createSDKSSHClient
// =============================================================================

func TestCreateSDKSSHClient_RoutesThroughSDKTransport(t *testing.T) {
	tr := &recordingSSHTransport{resp: &sshSDK.ListKeysResponse{Items: []*sshSDK.Key{{Name: "laptop"}}}}
	withSDKClientFactory(t, func() (quicbaas.Client, error) {
		return quicbaas.NewClientWithTransport(tr), nil
	})

	client, err := createSDKSSHClient()
	require.NoError(t, err)
	require.NotNil(t, client)

	keys, err := client.Keys().List(context.Background(), sshSDK.ListKeysRequest{
		Scope: "user", Owner: "alice",
	})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	assert.Equal(t, "laptop", keys[0].Name)

	// The load-bearing assertion: the call left as a QuicBaaS SSH request,
	// which is only possible through Security().SSH() — a go-xkms client
	// would have issued an HTTP/gRPC call to xkmsd instead.
	assert.Equal(t, sshSDK.ServiceName, tr.service)
	assert.Equal(t, sshSDK.MethodListKeys, tr.method)
}

func TestCreateSDKSSHClient_WrapsFactoryError(t *testing.T) {
	inner := stderrors.New("no server configured")
	withSDKClientFactory(t, func() (quicbaas.Client, error) { return nil, inner })

	client, err := createSDKSSHClient()
	require.Error(t, err)
	assert.Nil(t, client)

	var buildErr *CLISSHClientBuildError
	require.True(t, stderrors.As(err, &buildErr), "expected *CLISSHClientBuildError, got %T", err)
	assert.Equal(t, "sdk_client", buildErr.Op)
	assert.ErrorIs(t, err, inner)
}

// =============================================================================
// buildCLISSHClient
// =============================================================================

func TestBuildCLISSHClient_UsesFactorySeam(t *testing.T) {
	tr := &recordingSSHTransport{resp: &sshSDK.ListKnownHostsResponse{}}
	sentinel := quicbaas.NewClientWithTransport(tr).Security().SSH()

	original := sshClientFactory
	sshClientFactory = func() (sshSDK.Client, error) { return sentinel, nil }
	t.Cleanup(func() { sshClientFactory = original })

	got, err := buildCLISSHClient()
	require.NoError(t, err)
	assert.Same(t, sentinel, got, "buildCLISSHClient must return the factory's client")
}

func TestBuildCLISSHClient_PropagatesFactoryError(t *testing.T) {
	inner := stderrors.New("factory down")
	original := sshClientFactory
	sshClientFactory = func() (sshSDK.Client, error) { return nil, inner }
	t.Cleanup(func() { sshClientFactory = original })

	got, err := buildCLISSHClient()
	require.Error(t, err)
	assert.Nil(t, got)
	assert.ErrorIs(t, err, inner)
}

// =============================================================================
// Session / Console are refused on every remote transport
// =============================================================================

func TestSDKSSHClient_SessionAndConsoleAreUnimplemented(t *testing.T) {
	tr := &recordingSSHTransport{}
	client := quicbaas.NewClientWithTransport(tr).Security().SSH()

	_, err := client.Session().Dial(context.Background(), sshSDK.SessionDialRequest{
		KeyScope: "user", KeyOwner: "alice", KeyName: "laptop", Host: "example.com", User: "root",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local capability")

	_, err = client.Console(context.Background(), sshSDK.ConsoleConnectRequest{
		KeyScope: "user", KeyOwner: "alice", KeyName: "laptop", Host: "example.com", User: "root",
	}, sshSDK.ConsoleConfig{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "local capability")
}
