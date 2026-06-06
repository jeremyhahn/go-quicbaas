//go:build quic

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

package quic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"

	pb "github.com/jeremyhahn/go-qrdb/pkg/api/grpc/proto/qrdbv1"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// =============================================================================
// Mock CredentialServicer
// =============================================================================

type quicMockCredentialServicer struct {
	submitFn      func(ctx context.Context, name string, value []byte) error
	deleteFn      func(ctx context.Context, name string) error
	strategyVal   string
	listFn        func(ctx context.Context) ([]string, error)
	rotateFn      func(ctx context.Context, name string, opts qrdbsdk.RotateOptions) (*qrdbsdk.RotateResult, error)
	retrieveVerFn func(ctx context.Context, name string, version uint64) ([]byte, error)
	listVerFn     func(ctx context.Context, name string) ([]qrdbsdk.CredentialVersion, error)
	gcFn          func(ctx context.Context, name string) (int, error)
}

func (m *quicMockCredentialServicer) CredentialStore(context.Context, string, []byte) error {
	return nil
}
func (m *quicMockCredentialServicer) CredentialRetrieve(context.Context, string) ([]byte, error) {
	return []byte("v"), nil
}
func (m *quicMockCredentialServicer) CredentialSubmit(ctx context.Context, name string, value []byte) error {
	if m.submitFn != nil {
		return m.submitFn(ctx, name, value)
	}
	return nil
}
func (m *quicMockCredentialServicer) CredentialDelete(ctx context.Context, name string) error {
	if m.deleteFn != nil {
		return m.deleteFn(ctx, name)
	}
	return nil
}
func (m *quicMockCredentialServicer) CredentialStrategy() string {
	if m.strategyVal != "" {
		return m.strategyVal
	}
	return "manual"
}
func (m *quicMockCredentialServicer) CredentialList(ctx context.Context) ([]string, error) {
	if m.listFn != nil {
		return m.listFn(ctx)
	}
	return []string{"cred-a"}, nil
}
func (m *quicMockCredentialServicer) CredentialRotate(ctx context.Context, name string, opts qrdbsdk.RotateOptions) (*qrdbsdk.RotateResult, error) {
	if m.rotateFn != nil {
		return m.rotateFn(ctx, name, opts)
	}
	return &qrdbsdk.RotateResult{Name: name, PreviousVersion: 1, NewVersion: 2, GraceUntil: time.Now().Add(time.Hour)}, nil
}
func (m *quicMockCredentialServicer) CredentialRetrieveVersion(ctx context.Context, name string, version uint64) ([]byte, error) {
	if m.retrieveVerFn != nil {
		return m.retrieveVerFn(ctx, name, version)
	}
	return []byte("version-value"), nil
}
func (m *quicMockCredentialServicer) CredentialListVersions(ctx context.Context, name string) ([]qrdbsdk.CredentialVersion, error) {
	if m.listVerFn != nil {
		return m.listVerFn(ctx, name)
	}
	return []qrdbsdk.CredentialVersion{
		{Name: name, Version: 2, IsCurrent: true, CreatedAt: time.Now()},
		{Name: name, Version: 1, IsCurrent: false, CreatedAt: time.Now(), GraceUntil: time.Now().Add(time.Hour)},
	}, nil
}
func (m *quicMockCredentialServicer) CredentialGCExpired(ctx context.Context, name string) (int, error) {
	if m.gcFn != nil {
		return m.gcFn(ctx, name)
	}
	return 3, nil
}

func newQRDBCredentialServer(t *testing.T, svc *quicMockCredentialServicer) *Server {
	t.Helper()
	config := DefaultConfig()
	config.TLSConfig = generateTestTLSConfig(t)
	server, err := NewServer(&mockService{}, config, createServerTestLogger())
	require.NoError(t, err)
	if svc != nil {
		server.SetQRDBCredentialService(svc)
	}
	return server
}

func mustMarshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.Marshal(m)
	require.NoError(t, err)
	return b
}

// =============================================================================
// Typed errors
// =============================================================================

func TestQRDBCredentialTypedErrors(t *testing.T) {
	assert.Equal(t, "qrdb credential service not configured", (&QRDBCredentialNotConfiguredError{}).Error())
	assert.Equal(t, "qrdb-quic: unknown method: foo", (&QRDBUnknownMethodError{Method: "foo"}).Error())

	assert.Equal(t, "qrdb-quic: payload unmarshal failed", (&QRDBUnmarshalError{}).Error())
	wrapped := errors.New("boom")
	um := &QRDBUnmarshalError{Err: wrapped}
	assert.Contains(t, um.Error(), "boom")
	assert.ErrorIs(t, um, wrapped)

	assert.Equal(t, "qrdb-quic: response marshal failed", (&QRDBMarshalError{}).Error())
	mm := &QRDBMarshalError{Err: wrapped}
	assert.Contains(t, mm.Error(), "boom")
	assert.ErrorIs(t, mm, wrapped)
}

// =============================================================================
// Dispatch table + wiring
// =============================================================================

func TestBuildQRDBHandlers(t *testing.T) {
	server := newQRDBCredentialServer(t, &quicMockCredentialServicer{})
	handlers := server.buildQRDBHandlers()
	for _, method := range []string{
		"credential.Submit", "credential.Strategy", "credential.Delete",
		"credential.List", "credential.Rotate", "credential.RetrieveVersion",
		"credential.ListVersions", "credential.GCExpired",
	} {
		_, ok := handlers[method]
		assert.True(t, ok, "missing handler for %s", method)
	}
}

func TestSetQRDBCredentialService_Toggle(t *testing.T) {
	server := newQRDBCredentialServer(t, nil)
	server.SetQRDBCredentialService(&quicMockCredentialServicer{})
	server.SetQRDBCredentialService(nil) // disable; must not panic
}

func TestLogQRDBRequest(t *testing.T) {
	server := newQRDBCredentialServer(t, &quicMockCredentialServicer{})
	// Smoke test: emits one debug line without panicking.
	server.logQRDBRequest("credential.List", 7, 0, time.Now())
}

// =============================================================================
// Handler happy paths
// =============================================================================

func TestQRDBCredentialHandlers_HappyPaths(t *testing.T) {
	server := newQRDBCredentialServer(t, &quicMockCredentialServicer{})
	ctx := context.Background()

	_, err := server.qrdbCredentialSubmit(ctx, mustMarshal(t, &pb.CredentialSubmitRequest{Name: "c1", Value: "v1"}))
	require.NoError(t, err)

	strategy, err := server.qrdbCredentialStrategy(ctx, nil)
	require.NoError(t, err)
	var sResp pb.CredentialStrategyResponse
	require.NoError(t, proto.Unmarshal(strategy, &sResp))
	assert.Equal(t, "manual", sResp.GetStrategy())

	_, err = server.qrdbCredentialDelete(ctx, mustMarshal(t, &pb.CredentialDeleteRequest{Name: "c1"}))
	require.NoError(t, err)

	listOut, err := server.qrdbCredentialList(ctx, mustMarshal(t, &pb.CredentialListRequest{}))
	require.NoError(t, err)
	var lResp pb.CredentialListResponse
	require.NoError(t, proto.Unmarshal(listOut, &lResp))
	assert.Equal(t, []string{"cred-a"}, lResp.GetNames())

	rotateOut, err := server.qrdbCredentialRotate(ctx, mustMarshal(t, &pb.CredentialRotateRequest{
		Name: "c1", NewValue: "v2", GracePeriodSeconds: 60,
	}))
	require.NoError(t, err)
	var rResp pb.CredentialRotateResponse
	require.NoError(t, proto.Unmarshal(rotateOut, &rResp))
	assert.Equal(t, uint64(2), rResp.GetNewVersion())
	assert.NotZero(t, rResp.GetGraceUntilUnix())

	verOut, err := server.qrdbCredentialRetrieveVersion(ctx, mustMarshal(t, &pb.CredentialRetrieveVersionRequest{Name: "c1", Version: 1}))
	require.NoError(t, err)
	var vResp pb.CredentialRetrieveVersionResponse
	require.NoError(t, proto.Unmarshal(verOut, &vResp))
	assert.Equal(t, "version-value", vResp.GetValue())

	listVerOut, err := server.qrdbCredentialListVersions(ctx, mustMarshal(t, &pb.CredentialListVersionsRequest{Name: "c1"}))
	require.NoError(t, err)
	var lvResp pb.CredentialListVersionsResponse
	require.NoError(t, proto.Unmarshal(listVerOut, &lvResp))
	assert.Len(t, lvResp.GetVersions(), 2)

	gcOut, err := server.qrdbCredentialGCExpired(ctx, mustMarshal(t, &pb.CredentialGCRequest{Name: "c1"}))
	require.NoError(t, err)
	var gcResp pb.CredentialGCResponse
	require.NoError(t, proto.Unmarshal(gcOut, &gcResp))
	assert.Equal(t, int32(3), gcResp.GetReaped())
}

// =============================================================================
// Handler unmarshal-error paths
// =============================================================================

func TestQRDBCredentialHandlers_UnmarshalErrors(t *testing.T) {
	server := newQRDBCredentialServer(t, &quicMockCredentialServicer{})
	ctx := context.Background()
	bad := []byte{0xff, 0xff, 0xff, 0xff}

	handlers := []func(context.Context, []byte) ([]byte, error){
		server.qrdbCredentialSubmit,
		server.qrdbCredentialDelete,
		server.qrdbCredentialList,
		server.qrdbCredentialRotate,
		server.qrdbCredentialRetrieveVersion,
		server.qrdbCredentialListVersions,
		server.qrdbCredentialGCExpired,
	}
	for i, h := range handlers {
		_, err := h(ctx, bad)
		require.Error(t, err, "handler %d should reject bad payload", i)
		var unmarshalErr *QRDBUnmarshalError
		assert.ErrorAs(t, err, &unmarshalErr)
	}
}

// =============================================================================
// Handler service-error paths
// =============================================================================

func TestQRDBCredentialHandlers_ServiceErrors(t *testing.T) {
	sentinel := errors.New("svc boom")
	svc := &quicMockCredentialServicer{
		submitFn: func(context.Context, string, []byte) error { return sentinel },
		deleteFn: func(context.Context, string) error { return sentinel },
		listFn:   func(context.Context) ([]string, error) { return nil, sentinel },
		rotateFn: func(context.Context, string, qrdbsdk.RotateOptions) (*qrdbsdk.RotateResult, error) {
			return nil, sentinel
		},
		retrieveVerFn: func(context.Context, string, uint64) ([]byte, error) { return nil, sentinel },
		listVerFn:     func(context.Context, string) ([]qrdbsdk.CredentialVersion, error) { return nil, sentinel },
		gcFn:          func(context.Context, string) (int, error) { return 0, sentinel },
	}
	server := newQRDBCredentialServer(t, svc)
	ctx := context.Background()

	cases := []struct {
		name string
		call func() ([]byte, error)
	}{
		{"submit", func() ([]byte, error) {
			return server.qrdbCredentialSubmit(ctx, mustMarshal(t, &pb.CredentialSubmitRequest{Name: "c"}))
		}},
		{"delete", func() ([]byte, error) {
			return server.qrdbCredentialDelete(ctx, mustMarshal(t, &pb.CredentialDeleteRequest{Name: "c"}))
		}},
		{"list", func() ([]byte, error) {
			return server.qrdbCredentialList(ctx, mustMarshal(t, &pb.CredentialListRequest{}))
		}},
		{"rotate", func() ([]byte, error) {
			return server.qrdbCredentialRotate(ctx, mustMarshal(t, &pb.CredentialRotateRequest{Name: "c", NewValue: "v"}))
		}},
		{"retrieve-version", func() ([]byte, error) {
			return server.qrdbCredentialRetrieveVersion(ctx, mustMarshal(t, &pb.CredentialRetrieveVersionRequest{Name: "c"}))
		}},
		{"list-versions", func() ([]byte, error) {
			return server.qrdbCredentialListVersions(ctx, mustMarshal(t, &pb.CredentialListVersionsRequest{Name: "c"}))
		}},
		{"gc-expired", func() ([]byte, error) {
			return server.qrdbCredentialGCExpired(ctx, mustMarshal(t, &pb.CredentialGCRequest{Name: "c"}))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.call()
			require.ErrorIs(t, err, sentinel)
		})
	}
}
