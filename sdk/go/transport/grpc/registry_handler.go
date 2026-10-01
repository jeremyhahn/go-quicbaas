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
	"strings"
	"sync/atomic"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	registryclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/registry"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// isRegistryService reports whether the service name targets the service
// registry. This branch also serves the Unix transport, which delegates to
// gRPC verbatim.
func isRegistryService(svc string) bool {
	return strings.HasPrefix(svc, registryclient.ServiceName)
}

// registryUnaryHandlers dispatches the registry's unary verbs.
var registryUnaryHandlers = map[string]func(context.Context, pb.RegistryServiceClient, interface{}) (interface{}, error){
	"Register":   registryRegister,
	"Renew":      registryRenew,
	"Deregister": registryDeregister,
	"Resolve":    registryResolve,
}

// handleRegistryRequest converts a registry SDK request into the generated
// RegistryService call and the reply back into the SDK type.
func handleRegistryRequest(ctx context.Context, conn *grpc.ClientConn, method string, req interface{}) (interface{}, error) {
	handler, ok := registryUnaryHandlers[method]
	if !ok {
		return nil, errors.NewUnimplemented("grpc.registry."+method, "method not exposed by RegistryService")
	}
	return handler(ctx, pb.NewRegistryServiceClient(conn), req)
}

func registryRegister(ctx context.Context, client pb.RegistryServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*registryclient.RegisterRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("grpc.registry.Register", "invalid request type, expected *registry.RegisterRequest")
	}
	resp, err := client.Register(ctx, &pb.RegisterServiceInstanceRequest{
		Instance:   registryInstanceToProto(r.Instance),
		TtlSeconds: r.TTLSeconds,
	})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.registry.Register")
	}
	return &registryclient.RegisterResponse{LeaseID: resp.LeaseId, TTLSeconds: resp.TtlSeconds}, nil
}

func registryRenew(ctx context.Context, client pb.RegistryServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*registryclient.RenewRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("grpc.registry.Renew", "invalid request type, expected *registry.RenewRequest")
	}
	resp, err := client.Renew(ctx, &pb.RenewServiceLeaseRequest{LeaseId: r.LeaseID})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.registry.Renew")
	}
	return &registryclient.RenewResponse{LeaseID: resp.LeaseId, TTLSeconds: resp.TtlSeconds}, nil
}

func registryDeregister(ctx context.Context, client pb.RegistryServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*registryclient.DeregisterRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("grpc.registry.Deregister", "invalid request type, expected *registry.DeregisterRequest")
	}
	if _, err := client.Deregister(ctx, &pb.DeregisterServiceInstanceRequest{LeaseId: r.LeaseID}); err != nil {
		return nil, convertGRPCError(err, "grpc.registry.Deregister")
	}
	return nil, nil
}

func registryResolve(ctx context.Context, client pb.RegistryServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*registryclient.ResolveRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("grpc.registry.Resolve", "invalid request type, expected *registry.ResolveRequest")
	}
	resp, err := client.Resolve(ctx, &pb.ResolveServiceRequest{Service: r.Service})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.registry.Resolve")
	}
	out := &registryclient.ResolveResponse{
		Instances: make([]registryclient.ServiceInstance, 0, len(resp.Instances)),
		Revision:  resp.Revision,
	}
	for _, inst := range resp.Instances {
		out.Instances = append(out.Instances, registryInstanceFromProto(inst))
	}
	return out, nil
}

// openRegistryWatch opens the RegistryService.Watch server stream. The pooled
// connection is held by the returned stream and given back when it ends.
func openRegistryWatch(ctx context.Context, t *Transport, conn *grpc.ClientConn, method string, req interface{}) (transport.Stream, error) {
	if method != "Watch" {
		return nil, errors.NewUnimplemented("grpc.registry."+method, "not a RegistryService streaming method")
	}
	r, ok := req.(*registryclient.WatchRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("grpc.registry.Watch", "invalid request type, expected *registry.WatchRequest")
	}
	ws, err := pb.NewRegistryServiceClient(conn).Watch(ctx, &pb.WatchServiceRequest{Service: r.Service, FromRevision: r.FromRevision})
	if err != nil {
		return nil, registryWatchError(r, err)
	}
	return &registryWatchStream{stream: ws, req: r, transport: t, conn: conn}, nil
}

// registryWatchStream adapts the generated watch client to transport.Stream,
// converting each proto event to the SDK event.
type registryWatchStream struct {
	stream    pb.RegistryService_WatchClient
	req       *registryclient.WatchRequest
	transport *Transport
	conn      *grpc.ClientConn
	released  atomic.Bool
}

// Send is not supported: the watch is server-to-client only.
func (s *registryWatchStream) Send(interface{}) error {
	return errors.NewUnimplemented("grpc.registry.Watch", "send not supported on a server-streaming RPC")
}

// CloseSend is a no-op: the generated client already half-closed the stream.
func (s *registryWatchStream) CloseSend() error { return nil }

// Close returns the pooled connection. The stream itself is torn down by
// cancelling the context it was opened with.
func (s *registryWatchStream) Close() error {
	s.release()
	return nil
}

// Recv reads the next change. A compacted start revision arrives as
// OUT_OF_RANGE and is returned as *registry.WatchCompactedError.
func (s *registryWatchStream) Recv() (interface{}, error) {
	ev, err := s.stream.Recv()
	if err != nil {
		s.release()
		return nil, registryWatchError(s.req, err)
	}
	return &registryclient.Event{
		Type:     registryEventTypes[ev.Type],
		Instance: registryInstanceFromProto(ev.Instance),
		Revision: ev.Revision,
	}, nil
}

// release gives the pooled connection back exactly once.
func (s *registryWatchStream) release() {
	if s.released.CompareAndSwap(false, true) {
		s.transport.returnConn(s.conn)
	}
}

// registryWatchError maps a watch failure to the SDK's typed watch errors. A
// graceful end of the stream stays io.EOF, which the registry client reads as
// a connection to resume rather than a failure.
func registryWatchError(req *registryclient.WatchRequest, err error) error {
	if stderrors.Is(err, io.EOF) {
		return io.EOF
	}
	if st, ok := status.FromError(err); ok && st.Code() == codes.OutOfRange {
		return &registryclient.WatchCompactedError{Service: req.Service, FromRevision: req.FromRevision, Message: st.Message()}
	}
	return convertGRPCError(err, "grpc.registry.Watch")
}

// registryEventTypes maps proto event types to SDK event types.
var registryEventTypes = map[pb.RegistryEvent_EventType]registryclient.EventType{
	pb.RegistryEvent_EVENT_TYPE_PUT:    registryclient.EventPut,
	pb.RegistryEvent_EVENT_TYPE_DELETE: registryclient.EventDelete,
}

func registryInstanceToProto(inst registryclient.ServiceInstance) *pb.ServiceInstance {
	return &pb.ServiceInstance{Id: inst.ID, Service: inst.Service, Address: inst.Address, Metadata: inst.Metadata}
}

func registryInstanceFromProto(inst *pb.ServiceInstance) registryclient.ServiceInstance {
	if inst == nil {
		return registryclient.ServiceInstance{}
	}
	return registryclient.ServiceInstance{ID: inst.Id, Service: inst.Service, Address: inst.Address, Metadata: inst.Metadata}
}
