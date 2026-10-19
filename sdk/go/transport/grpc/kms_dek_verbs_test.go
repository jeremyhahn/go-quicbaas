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

package grpc

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	dekclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// dekVerbsServer answers the DEKManagement status, default, safety and
// re-encryption RPCs. Each RPC hands its request to got so the test can
// assert the field mapping, and answers NotFound when fail is set.
type dekVerbsServer struct {
	pb.UnimplementedDEKManagementServer
	got  chan proto.Message
	fail bool
}

func (s *dekVerbsServer) record(req proto.Message) error {
	s.got <- req
	if s.fail {
		return status.Error(codes.NotFound, "dek not found")
	}
	return nil
}

func (s *dekVerbsServer) GetDEKStatus(_ context.Context, req *pb.GetDEKStatusRequest) (*pb.GetDEKStatusResponse, error) {
	if err := s.record(req); err != nil {
		return nil, err
	}
	return &pb.GetDEKStatusResponse{
		Dek: &pb.DEKMetadata{Path: req.Path, Version: 3, Status: "active", IsDefault: true, UsageCount: 42},
		ReencryptionProgress: &pb.ReencryptionProgress{
			InProgress: true, TotalKeys: 10, ProcessedKeys: 5, FailedKeys: 1, StartedAt: 1700000000,
		},
		Timestamp: 1700000001,
	}, nil
}

func (s *dekVerbsServer) SetDefaultDEK(_ context.Context, req *pb.SetDefaultDEKRequest) (*pb.SetDefaultDEKResponse, error) {
	if err := s.record(req); err != nil {
		return nil, err
	}
	return &pb.SetDefaultDEKResponse{Path: req.Path, PreviousDefault: "/dek/old", Timestamp: 1}, nil
}

func (s *dekVerbsServer) SetDEKSafety(_ context.Context, req *pb.SetDEKSafetyRequest) (*pb.SetDEKSafetyResponse, error) {
	if err := s.record(req); err != nil {
		return nil, err
	}
	return &pb.SetDEKSafetyResponse{Path: req.Path, Timestamp: 1}, nil
}

func (s *dekVerbsServer) ClearDEKSafety(_ context.Context, req *pb.ClearDEKSafetyRequest) (*pb.ClearDEKSafetyResponse, error) {
	if err := s.record(req); err != nil {
		return nil, err
	}
	return &pb.ClearDEKSafetyResponse{Path: req.Path, Timestamp: 1}, nil
}

func (s *dekVerbsServer) StartReencryption(_ context.Context, req *pb.StartReencryptionRequest) (*pb.StartReencryptionResponse, error) {
	if err := s.record(req); err != nil {
		return nil, err
	}
	return &pb.StartReencryptionResponse{OperationId: "op-1", Started: true, Path: req.Path, EstimatedItems: 128, Timestamp: 1}, nil
}

func (s *dekVerbsServer) GetReencryptionProgress(_ context.Context, req *pb.GetReencryptionProgressRequest) (*pb.GetReencryptionProgressResponse, error) {
	if err := s.record(req); err != nil {
		return nil, err
	}
	return &pb.GetReencryptionProgressResponse{
		OperationId: req.OperationId,
		Path:        req.Path,
		Status:      "running",
		Progress:    &pb.ReencryptionProgress{InProgress: true, TotalKeys: 4, ProcessedKeys: 2},
		Error:       "one key skipped",
		Timestamp:   1,
	}, nil
}

// newDEKVerbsClient returns the real SDK DEK client over the gRPC transport,
// connected to a dekVerbsServer on a bufconn listener.
func newDEKVerbsClient(t *testing.T, fail bool) (dekclient.Client, chan proto.Message) {
	t.Helper()
	fake := &dekVerbsServer{got: make(chan proto.Message, 1), fail: fail}
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterDEKManagementServer(s, fake)
	})
	tr := &Transport{cfg: &transport.Config{Timeout: 5 * time.Second}, conn: srv.dial(t)}
	return dekclient.NewClient(tr), fake.got
}

// received returns the request the fake server recorded. The client call has
// already returned, so a request that reached the server is waiting in got;
// an empty channel means the call never reached the RPC.
func received(t *testing.T, got chan proto.Message) proto.Message {
	t.Helper()
	select {
	case req := <-got:
		return req
	default:
		t.Fatal("the call did not reach the DEKManagement RPC")
		return nil
	}
}

func TestDEKClientOverGRPC_GetDEKStatus(t *testing.T) {
	client, got := newDEKVerbsClient(t, false)

	resp, err := client.GetDEKStatus(context.Background(), "/dek/a")
	require.NoError(t, err)

	req := received(t, got).(*pb.GetDEKStatusRequest)
	assert.Equal(t, "/dek/a", req.Path)

	require.NotNil(t, resp.DEK)
	assert.Equal(t, "/dek/a", resp.DEK.Path)
	assert.Equal(t, 3, resp.DEK.Version)
	assert.Equal(t, "active", resp.DEK.Status)
	assert.True(t, resp.DEK.IsDefault)
	assert.Equal(t, uint64(42), resp.DEK.UsageCount)
	require.NotNil(t, resp.ReencryptionProgress)
	assert.True(t, resp.ReencryptionProgress.InProgress)
	assert.Equal(t, 10, resp.ReencryptionProgress.TotalKeys)
	assert.Equal(t, 5, resp.ReencryptionProgress.ProcessedKeys)
	assert.Equal(t, 1, resp.ReencryptionProgress.FailedKeys)
	assert.Equal(t, time.Unix(1700000000, 0), resp.ReencryptionProgress.StartedAt)
}

func TestDEKClientOverGRPC_SetDefaultDEK(t *testing.T) {
	client, got := newDEKVerbsClient(t, false)

	resp, err := client.SetDefaultDEK(context.Background(), "/dek/new")
	require.NoError(t, err)

	req := received(t, got).(*pb.SetDefaultDEKRequest)
	assert.Equal(t, "/dek/new", req.Path)
	assert.Equal(t, "/dek/new", resp.Path)
	assert.Equal(t, "/dek/old", resp.PreviousDefault)
}

func TestDEKClientOverGRPC_SetDEKSafety(t *testing.T) {
	client, got := newDEKVerbsClient(t, false)

	err := client.SetDEKSafety(context.Background(), &dekclient.SetDEKSafetyRequest{
		Path: "/dek/a", MaxOperations: 1000, MaxBytes: 1 << 20, WarningPercent: 80,
	})
	require.NoError(t, err)

	req := received(t, got).(*pb.SetDEKSafetyRequest)
	assert.Equal(t, "/dek/a", req.Path)
	assert.Equal(t, uint64(1000), req.MaxOperations)
	assert.Equal(t, uint64(1<<20), req.MaxBytes)
	assert.EqualValues(t, 80, req.WarningPercent)
}

func TestDEKClientOverGRPC_ClearDEKSafety(t *testing.T) {
	client, got := newDEKVerbsClient(t, false)

	require.NoError(t, client.ClearDEKSafety(context.Background(), "/dek/a"))

	req := received(t, got).(*pb.ClearDEKSafetyRequest)
	assert.Equal(t, "/dek/a", req.Path)
}

func TestDEKClientOverGRPC_StartReencryption(t *testing.T) {
	client, got := newDEKVerbsClient(t, false)

	resp, err := client.StartReencryption(context.Background(), &dekclient.StartReencryptionRequest{
		Path: "/dek/a", FromVersion: 1, ToVersion: 2, BatchSize: 64, Force: true,
	})
	require.NoError(t, err)

	req := received(t, got).(*pb.StartReencryptionRequest)
	assert.Equal(t, "/dek/a", req.Path)
	assert.Equal(t, uint64(1), req.FromVersion)
	assert.Equal(t, uint64(2), req.ToVersion)
	assert.EqualValues(t, 64, req.BatchSize)
	assert.True(t, req.Force)

	assert.Equal(t, "op-1", resp.OperationID)
	assert.Equal(t, "/dek/a", resp.Path)
	assert.True(t, resp.Started)
	assert.Equal(t, 128, resp.EstimatedItems)
}

func TestDEKClientOverGRPC_GetReencryptionProgress(t *testing.T) {
	client, got := newDEKVerbsClient(t, false)

	resp, err := client.GetReencryptionProgress(context.Background(), &dekclient.GetReencryptionProgressRequest{
		Path: "/dek/a", OperationID: "op-1",
	})
	require.NoError(t, err)

	req := received(t, got).(*pb.GetReencryptionProgressRequest)
	assert.Equal(t, "/dek/a", req.Path)
	assert.Equal(t, "op-1", req.OperationId)

	assert.Equal(t, "op-1", resp.OperationID)
	assert.Equal(t, "/dek/a", resp.Path)
	assert.Equal(t, "running", resp.Status)
	assert.Equal(t, "one key skipped", resp.Error)
	require.NotNil(t, resp.Progress)
	assert.True(t, resp.Progress.InProgress)
	assert.Equal(t, 4, resp.Progress.TotalKeys)
	assert.Equal(t, 2, resp.Progress.ProcessedKeys)
	assert.InDelta(t, 50.0, resp.Progress.PercentComplete, 0.001)
}

// TestDEKClientOverGRPC_ServerErrorIsTyped checks that a NotFound status from
// each DEKManagement RPC reaches the caller as the typed NotFound error.
func TestDEKClientOverGRPC_ServerErrorIsTyped(t *testing.T) {
	ctx := context.Background()
	verbs := map[string]func(dekclient.Client) error{
		"GetDEKStatus": func(c dekclient.Client) error {
			_, err := c.GetDEKStatus(ctx, "/dek/a")
			return err
		},
		"SetDefaultDEK": func(c dekclient.Client) error {
			_, err := c.SetDefaultDEK(ctx, "/dek/a")
			return err
		},
		"SetDEKSafety": func(c dekclient.Client) error {
			return c.SetDEKSafety(ctx, &dekclient.SetDEKSafetyRequest{Path: "/dek/a"})
		},
		"ClearDEKSafety": func(c dekclient.Client) error {
			return c.ClearDEKSafety(ctx, "/dek/a")
		},
		"StartReencryption": func(c dekclient.Client) error {
			_, err := c.StartReencryption(ctx, &dekclient.StartReencryptionRequest{Path: "/dek/a"})
			return err
		},
		"GetReencryptionProgress": func(c dekclient.Client) error {
			_, err := c.GetReencryptionProgress(ctx, &dekclient.GetReencryptionProgressRequest{Path: "/dek/a"})
			return err
		},
	}
	for name, call := range verbs {
		t.Run(name, func(t *testing.T) {
			client, got := newDEKVerbsClient(t, true)
			err := call(client)
			received(t, got)
			assert.True(t, errors.IsNotFound(err), "got %v", err)
		})
	}
}

// TestKMSHandler_UnknownDEKMethodIsUnimplemented checks that a DEK method the
// handler does not route returns the typed unimplemented error instead of
// falling through to the untyped conn.Invoke path.
// TestKMSHandler_DEKVerbsRejectWrongRequestType checks that each new DEK
// handler refuses a request it cannot map with the typed InvalidInput error.
func TestKMSHandler_DEKVerbsRejectWrongRequestType(t *testing.T) {
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterDEKManagementServer(s, &dekVerbsServer{got: make(chan proto.Message, 1)})
	})
	h, err := newKMSHandler(srv.dial(t))
	require.NoError(t, err)

	methods := []string{
		"GetDEKStatus", "SetDefaultDEK", "SetDEKSafety",
		"ClearDEKSafety", "StartReencryption", "GetReencryptionProgress",
	}
	for _, method := range methods {
		t.Run(method, func(t *testing.T) {
			_, err := h.handleRequest(context.Background(), "quicbaas.DEK", method, 42)
			assert.True(t, errors.IsInvalidInput(err), "got %v", err)
		})
	}
}

func TestKMSHandler_UnknownDEKMethodIsUnimplemented(t *testing.T) {
	srv := newHandlerServer(t, func(s *grpc.Server) {
		pb.RegisterDEKManagementServer(s, &dekVerbsServer{got: make(chan proto.Message, 1)})
	})
	h, err := newKMSHandler(srv.dial(t))
	require.NoError(t, err)

	_, err = h.handleRequest(context.Background(), "quicbaas.DEK", "NoSuchDEKMethod", &dekclient.GetReencryptionProgressRequest{})
	assert.True(t, errors.IsUnimplemented(err), "got %v", err)
}
