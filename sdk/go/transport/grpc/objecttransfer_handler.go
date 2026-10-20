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
	"sync/atomic"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/objecttransfer"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// objectTransferServiceName is the service name the objecttransfer client
// sends.
const objectTransferServiceName = "quicbaas.ObjectTransfer"

// isObjectTransferService reports whether the service name targets the
// peer object transfer service. This branch also serves the Unix transport,
// which delegates to gRPC verbatim.
func isObjectTransferService(svc string) bool {
	return svc == objectTransferServiceName
}

// objectTransferUnaryHandlers dispatches the object transfer unary verbs.
var objectTransferUnaryHandlers = map[string]func(context.Context, pb.ObjectTransferServiceClient, interface{}) (interface{}, error){
	"FetchObject": objectTransferFetchObject,
}

// handleObjectTransferRequest converts an objecttransfer SDK request into the
// generated ObjectTransferService call and the reply back into the SDK type.
func handleObjectTransferRequest(ctx context.Context, conn *grpc.ClientConn, method string, req interface{}) (interface{}, error) {
	handler, ok := objectTransferUnaryHandlers[method]
	if !ok {
		return nil, errors.NewUnimplemented("grpc.objecttransfer."+method, "method not exposed by ObjectTransferService")
	}
	return handler(ctx, pb.NewObjectTransferServiceClient(conn), req)
}

func objectTransferFetchObject(ctx context.Context, client pb.ObjectTransferServiceClient, req interface{}) (interface{}, error) {
	r, ok := req.(*objecttransfer.FetchObjectRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("grpc.objecttransfer.FetchObject", "invalid request type, expected *objecttransfer.FetchObjectRequest")
	}
	resp, err := client.FetchObject(ctx, &pb.FetchObjectRequest{
		ShardId:   r.ShardID,
		ObjectRef: r.ObjectRef,
		Checksum:  r.Checksum,
		RequestId: r.RequestID,
	})
	if err != nil {
		return nil, convertGRPCError(err, "grpc.objecttransfer.FetchObject")
	}
	return objectTransferResponseFromProto(resp), nil
}

// openObjectTransferStream opens the ObjectTransferService.StreamFetchObjects
// server stream. The pooled connection is held by the returned stream and
// given back when it ends.
func openObjectTransferStream(ctx context.Context, t *Transport, conn *grpc.ClientConn, method string, req interface{}) (transport.Stream, error) {
	if method != "StreamFetchObjects" {
		return nil, errors.NewUnimplemented("grpc.objecttransfer."+method, "not an ObjectTransferService streaming method")
	}
	r, ok := req.(*objecttransfer.StreamFetchObjectsRequest)
	if !ok || r == nil {
		return nil, errors.NewInvalidInput("grpc.objecttransfer.StreamFetchObjects", "invalid request type, expected *objecttransfer.StreamFetchObjectsRequest")
	}
	items := make([]*pb.ObjectFetchItem, 0, len(r.Items))
	for _, item := range r.Items {
		items = append(items, &pb.ObjectFetchItem{ObjectRef: item.ObjectRef, Checksum: item.Checksum})
	}
	// The stream owns a cancellable context so Close can end the RPC even
	// when the caller's context never ends.
	streamCtx, cancel := context.WithCancel(ctx)
	fs, err := pb.NewObjectTransferServiceClient(conn).StreamFetchObjects(streamCtx, &pb.StreamFetchObjectsRequest{ShardId: r.ShardID, Items: items})
	if err != nil {
		cancel()
		return nil, objectTransferStreamError(err)
	}
	return &objectTransferStream{stream: fs, cancel: cancel, transport: t, conn: conn}, nil
}

// objectTransferStream adapts the generated fetch stream to transport.Stream,
// converting each proto response to the SDK response.
type objectTransferStream struct {
	stream    pb.ObjectTransferService_StreamFetchObjectsClient
	cancel    context.CancelFunc
	transport *Transport
	conn      *grpc.ClientConn
	released  atomic.Bool
}

// Send is not supported: the fetch stream is server-to-client only.
func (s *objectTransferStream) Send(interface{}) error {
	return errors.NewUnimplemented("grpc.objecttransfer.StreamFetchObjects", "send not supported on a server-streaming RPC")
}

// CloseSend is a no-op: the generated client already half-closed the stream.
func (s *objectTransferStream) CloseSend() error { return nil }

// Close cancels the RPC, which ends any blocked Recv, and then returns the
// pooled connection. A second Close is a no-op.
func (s *objectTransferStream) Close() error {
	s.release()
	return nil
}

// Recv reads the next fetched object. The end of the stream is io.EOF.
func (s *objectTransferStream) Recv() (interface{}, error) {
	resp, err := s.stream.Recv()
	if err != nil {
		s.release()
		return nil, objectTransferStreamError(err)
	}
	return objectTransferResponseFromProto(resp), nil
}

// release cancels the RPC and gives the pooled connection back, exactly once.
func (s *objectTransferStream) release() {
	if s.released.CompareAndSwap(false, true) {
		s.cancel()
		s.transport.returnConn(s.conn)
	}
}

// objectTransferStreamError keeps a graceful end of the stream as io.EOF,
// which the objecttransfer client reads as the end of the fetch, and maps
// every other failure to its typed SDK error.
func objectTransferStreamError(err error) error {
	if stderrors.Is(err, io.EOF) {
		return io.EOF
	}
	return convertGRPCError(err, "grpc.objecttransfer.StreamFetchObjects")
}

func objectTransferResponseFromProto(resp *pb.FetchObjectResponse) *objecttransfer.FetchObjectResponse {
	return &objecttransfer.FetchObjectResponse{
		ObjectRef:       resp.GetObjectRef(),
		Data:            resp.GetData(),
		Checksum:        resp.GetChecksum(),
		Size:            resp.GetSize(),
		Compressed:      resp.GetCompressed(),
		CompressionAlgo: objecttransfer.CompressionAlgo(resp.GetCompressionAlgo()),
		Found:           resp.GetFound(),
		ErrorMessage:    resp.GetErrorMessage(),
	}
}
