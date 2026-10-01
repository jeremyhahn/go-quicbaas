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

package grpc

import (
	"context"
	stderrors "errors"
	"io"
	"net"
	"sync"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	registryclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/registry"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// recordingRegistryServer is an in-memory RegistryService that records what
// each RPC RECEIVED, so the tests assert the wire conversion rather than a
// nil error.
type recordingRegistryServer struct {
	mu            sync.Mutex
	gotRegister   *pb.RegisterServiceInstanceRequest
	gotRenew      *pb.RenewServiceLeaseRequest
	gotDeregister *pb.DeregisterServiceInstanceRequest
	gotResolve    *pb.ResolveServiceRequest
	gotWatch      *pb.WatchServiceRequest

	watchEvents []*pb.RegistryEvent
	watchEnd    error
	failWith    error
}

func (s *recordingRegistryServer) Register(_ context.Context, req *pb.RegisterServiceInstanceRequest) (*pb.RegisterServiceInstanceResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gotRegister = req
	if s.failWith != nil {
		return nil, s.failWith
	}
	return &pb.RegisterServiceInstanceResponse{LeaseId: 9007199254740993, TtlSeconds: req.TtlSeconds}, nil
}

func (s *recordingRegistryServer) Renew(_ context.Context, req *pb.RenewServiceLeaseRequest) (*pb.RenewServiceLeaseResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gotRenew = req
	if s.failWith != nil {
		return nil, s.failWith
	}
	return &pb.RenewServiceLeaseResponse{LeaseId: req.LeaseId, TtlSeconds: 11}, nil
}

func (s *recordingRegistryServer) Deregister(_ context.Context, req *pb.DeregisterServiceInstanceRequest) (*pb.DeregisterServiceInstanceResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gotDeregister = req
	if s.failWith != nil {
		return nil, s.failWith
	}
	return &pb.DeregisterServiceInstanceResponse{}, nil
}

func (s *recordingRegistryServer) Resolve(_ context.Context, req *pb.ResolveServiceRequest) (*pb.ResolveServiceResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gotResolve = req
	if s.failWith != nil {
		return nil, s.failWith
	}
	return &pb.ResolveServiceResponse{
		Instances: []*pb.ServiceInstance{{Id: "a", Service: req.Service, Address: "10.0.0.1:80", Metadata: map[string]string{"k": "v"}}},
		Revision:  17,
	}, nil
}

func (s *recordingRegistryServer) Watch(req *pb.WatchServiceRequest, stream pb.RegistryService_WatchServer) error {
	s.mu.Lock()
	s.gotWatch = req
	events, end := s.watchEvents, s.watchEnd
	s.mu.Unlock()
	for _, ev := range events {
		if err := stream.Send(ev); err != nil {
			return err
		}
	}
	return end
}

// startRegistryServer serves the recording server over an in-memory listener
// and returns a client connection to it.
func startRegistryServer(t *testing.T, srv *recordingRegistryServer) *grpc.ClientConn {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterRegistryServiceServer(server, srv)
	served := make(chan error, 1)
	go func() { served <- server.Serve(lis) }()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close conn: %v", err)
		}
		server.Stop()
		if err := <-served; err != nil && !stderrors.Is(err, grpc.ErrServerStopped) {
			t.Errorf("serve: %v", err)
		}
	})
	return conn
}

func TestIsRegistryService(t *testing.T) {
	if !isRegistryService("quicbaas.Registry") {
		t.Fatal("quicbaas.Registry must branch into the registry handler")
	}
	for _, svc := range []string{"quicbaas.Scheduler", "quicbaas.KV", ""} {
		if isRegistryService(svc) {
			t.Errorf("%q must not branch into the registry handler", svc)
		}
	}
}

func TestRegistryHandler_UnaryRoundTrip(t *testing.T) {
	srv := &recordingRegistryServer{}
	conn := startRegistryServer(t, srv)
	ctx := context.Background()

	out, err := handleRegistryRequest(ctx, conn, "Register", &registryclient.RegisterRequest{
		Instance:   registryclient.ServiceInstance{ID: "i", Service: "api", Address: "10.0.0.9:443", Metadata: map[string]string{"zone": "z"}},
		TTLSeconds: 15,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if lease := out.(*registryclient.RegisterResponse); lease.LeaseID != 9007199254740993 || lease.TTLSeconds != 15 {
		t.Fatalf("Register = %+v", lease)
	}
	if got := srv.gotRegister; got.Instance.Id != "i" || got.Instance.Address != "10.0.0.9:443" || got.Instance.Metadata["zone"] != "z" || got.TtlSeconds != 15 {
		t.Fatalf("server received %+v", got)
	}

	out, err = handleRegistryRequest(ctx, conn, "Renew", &registryclient.RenewRequest{LeaseID: 44})
	if err != nil || out.(*registryclient.RenewResponse).TTLSeconds != 11 || srv.gotRenew.LeaseId != 44 {
		t.Fatalf("Renew = %+v, %v (server got %+v)", out, err, srv.gotRenew)
	}

	if _, err := handleRegistryRequest(ctx, conn, "Deregister", &registryclient.DeregisterRequest{LeaseID: 45}); err != nil || srv.gotDeregister.LeaseId != 45 {
		t.Fatalf("Deregister: %v (server got %+v)", err, srv.gotDeregister)
	}

	out, err = handleRegistryRequest(ctx, conn, "Resolve", &registryclient.ResolveRequest{Service: "api"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	resolved := out.(*registryclient.ResolveResponse)
	if srv.gotResolve.Service != "api" || resolved.Revision != 17 || len(resolved.Instances) != 1 || resolved.Instances[0].Metadata["k"] != "v" {
		t.Fatalf("Resolve = %+v (server got %+v)", resolved, srv.gotResolve)
	}
}

func TestRegistryHandler_ErrorsAndValidation(t *testing.T) {
	srv := &recordingRegistryServer{failWith: status.Error(codes.NotFound, "lease gone")}
	conn := startRegistryServer(t, srv)
	ctx := context.Background()
	for method, req := range map[string]interface{}{
		"Register":   &registryclient.RegisterRequest{Instance: registryclient.ServiceInstance{ID: "i", Service: "s", Address: "a"}, TTLSeconds: 5},
		"Renew":      &registryclient.RenewRequest{LeaseID: 1},
		"Deregister": &registryclient.DeregisterRequest{LeaseID: 1},
		"Resolve":    &registryclient.ResolveRequest{Service: "s"},
	} {
		if _, err := handleRegistryRequest(ctx, conn, method, req); !sdkerrors.IsNotFound(err) {
			t.Fatalf("%s err = %v, want not found", method, err)
		}
		if _, err := handleRegistryRequest(ctx, conn, method, "wrong"); !sdkerrors.IsInvalidInput(err) {
			t.Fatalf("%s wrong type err = %v, want invalid input", method, err)
		}
	}
	if _, err := handleRegistryRequest(ctx, nil, "Quarantine", nil); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("unknown method err = %v", err)
	}
}

func TestRegistryHandler_WatchStream(t *testing.T) {
	srv := &recordingRegistryServer{
		watchEvents: []*pb.RegistryEvent{
			{Type: pb.RegistryEvent_EVENT_TYPE_PUT, Instance: &pb.ServiceInstance{Id: "a", Service: "api", Address: "x:1"}, Revision: 8},
			{Type: pb.RegistryEvent_EVENT_TYPE_DELETE, Instance: &pb.ServiceInstance{Id: "a", Service: "api"}, Revision: 9},
		},
		watchEnd: status.Error(codes.OutOfRange, "compacted"),
	}
	conn := startRegistryServer(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := openRegistryWatch(ctx, &Transport{}, conn, "Watch", &registryclient.WatchRequest{Service: "api", FromRevision: 8})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	first, err := stream.Recv()
	if err != nil {
		t.Fatalf("Recv: %v", err)
	}
	if ev := first.(*registryclient.Event); ev.Type != registryclient.EventPut || ev.Revision != 8 || ev.Instance.Address != "x:1" {
		t.Fatalf("first = %+v", ev)
	}
	second, err := stream.Recv()
	if err != nil || second.(*registryclient.Event).Type != registryclient.EventDelete {
		t.Fatalf("second = %+v, %v", second, err)
	}
	_, err = stream.Recv()
	var compacted *registryclient.WatchCompactedError
	if !stderrors.As(err, &compacted) || compacted.FromRevision != 8 || compacted.Service != "api" {
		t.Fatalf("end = %v, want *WatchCompactedError from 8", err)
	}
	if srv.gotWatch.Service != "api" || srv.gotWatch.FromRevision != 8 {
		t.Fatalf("server received %+v", srv.gotWatch)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := stream.Send(nil); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("Send = %v", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("CloseSend = %v", err)
	}
}

func TestRegistryHandler_WatchCleanEndAndOpenErrors(t *testing.T) {
	conn := startRegistryServer(t, &recordingRegistryServer{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	stream, err := openRegistryWatch(ctx, &Transport{}, conn, "Watch", &registryclient.WatchRequest{Service: "api", FromRevision: 1})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("clean end = %v, want io.EOF", err)
	}

	if _, err := openRegistryWatch(ctx, &Transport{}, conn, "Resolve", nil); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("non-stream method = %v", err)
	}
	if _, err := openRegistryWatch(ctx, &Transport{}, conn, "Watch", "wrong"); !sdkerrors.IsInvalidInput(err) {
		t.Fatalf("wrong type = %v", err)
	}
}
