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
	"reflect"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/connectivity"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/objecttransfer"
	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// objectTransferWait bounds every wait on the fake server, so a missing
// route fails fast instead of hanging the package.
const objectTransferWait = 5 * time.Second

// recordingObjectTransferServer records each request it receives and answers
// with canned responses, or with err when it is set.
type recordingObjectTransferServer struct {
	pb.UnimplementedObjectTransferServiceServer
	fetchReqs   chan *pb.FetchObjectRequest
	streamReqs  chan *pb.StreamFetchObjectsRequest
	fetchResp   *pb.FetchObjectResponse
	streamResps []*pb.FetchObjectResponse
	err         error
}

func newRecordingObjectTransferServer() *recordingObjectTransferServer {
	return &recordingObjectTransferServer{
		fetchReqs:  make(chan *pb.FetchObjectRequest, 1),
		streamReqs: make(chan *pb.StreamFetchObjectsRequest, 1),
	}
}

func (s *recordingObjectTransferServer) FetchObject(_ context.Context, req *pb.FetchObjectRequest) (*pb.FetchObjectResponse, error) {
	s.fetchReqs <- req
	if s.err != nil {
		return nil, s.err
	}
	return s.fetchResp, nil
}

func (s *recordingObjectTransferServer) StreamFetchObjects(req *pb.StreamFetchObjectsRequest, stream grpc.ServerStreamingServer[pb.FetchObjectResponse]) error {
	s.streamReqs <- req
	if s.err != nil {
		return s.err
	}
	for _, resp := range s.streamResps {
		if err := stream.Send(resp); err != nil {
			return err
		}
	}
	return nil
}

// startObjectTransferServer serves srv over bufconn and returns a transport
// whose one-connection pool holds the client side, plus that pooled
// connection so a test can see when it is given back.
func startObjectTransferServer(t *testing.T, srv pb.ObjectTransferServiceServer) (*Transport, *pooledConn) {
	t.Helper()
	lis := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	pb.RegisterObjectTransferServiceServer(server, srv)
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
	// A new client starts Idle and its first RPC moves it through
	// Connecting; grpc-go installs the Ready picker before it publishes the
	// Ready state, so the pool's health check can still read Connecting right
	// after that RPC and report the one-slot pool exhausted. Settle the
	// connection Ready before the pool hands it out.
	awaitReady(t, conn)
	pc := &pooledConn{conn: conn, createdAt: time.Now()}
	pool := &Pool{cfg: &PoolConfig{MaxSize: 1}, connections: []*pooledConn{pc}}
	return &Transport{cfg: &transport.Config{}, pool: pool}, pc
}

// awaitReady connects conn and blocks until it reports Ready, failing the
// test if that takes longer than objectTransferWait.
func awaitReady(t *testing.T, conn *grpc.ClientConn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), objectTransferWait)
	defer cancel()
	conn.Connect()
	for state := conn.GetState(); state != connectivity.Ready; state = conn.GetState() {
		if !conn.WaitForStateChange(ctx, state) {
			t.Fatalf("connection stuck in %s", state)
		}
	}
}

// receiveWithin returns the next value on ch, failing the test when none
// arrives in time.
func receiveWithin[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(objectTransferWait):
		var zero T
		t.Fatalf("nothing received within %v", objectTransferWait)
		return zero
	}
}

// assertConnReturned fails when the pooled connection is still checked out.
func assertConnReturned(t *testing.T, pc *pooledConn) {
	t.Helper()
	if pc.inUse.Load() {
		t.Fatal("pooled connection was not returned")
	}
}

func TestIsObjectTransferService(t *testing.T) {
	if !isObjectTransferService("quicbaas.ObjectTransfer") {
		t.Fatal("quicbaas.ObjectTransfer not routed")
	}
	for _, svc := range []string{"", "quicbaas.Registry", "quicbaas.Object"} {
		if isObjectTransferService(svc) {
			t.Fatalf("%q routed as ObjectTransfer", svc)
		}
	}
}

func TestObjectTransferHandler_FetchObjectRoundTrip(t *testing.T) {
	srv := newRecordingObjectTransferServer()
	srv.fetchResp = &pb.FetchObjectResponse{
		ObjectRef:       "bucket:key",
		Data:            []byte("payload"),
		Checksum:        []byte{0xab, 0xcd},
		Size:            1024,
		Compressed:      true,
		CompressionAlgo: "zstd",
		Found:           true,
		ErrorMessage:    "partial",
	}
	tr, pc := startObjectTransferServer(t, srv)

	got, err := objecttransfer.NewClient(tr).FetchObject(context.Background(), &objecttransfer.FetchObjectRequest{
		ShardID:   7,
		ObjectRef: "bucket:key",
		Checksum:  []byte{0x01, 0x02},
		RequestID: "req-1",
	})
	if err != nil {
		t.Fatalf("FetchObject: %v", err)
	}

	sent := receiveWithin(t, srv.fetchReqs)
	if sent.ShardId != 7 || sent.ObjectRef != "bucket:key" || !reflect.DeepEqual(sent.Checksum, []byte{0x01, 0x02}) || sent.RequestId != "req-1" {
		t.Fatalf("server received %+v", sent)
	}
	want := &objecttransfer.FetchObjectResponse{
		ObjectRef:       "bucket:key",
		Data:            []byte("payload"),
		Checksum:        []byte{0xab, 0xcd},
		Size:            1024,
		Compressed:      true,
		CompressionAlgo: objecttransfer.CompressionZstd,
		Found:           true,
		ErrorMessage:    "partial",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("response = %+v, want %+v", got, want)
	}
	assertConnReturned(t, pc)
}

func TestObjectTransferHandler_StreamFetchObjects(t *testing.T) {
	srv := newRecordingObjectTransferServer()
	srv.streamResps = []*pb.FetchObjectResponse{
		{ObjectRef: "b:1", Data: []byte("one"), Size: 3, Found: true},
		{ObjectRef: "b:2", Found: false, ErrorMessage: "missing"},
		{ObjectRef: "b:3", Data: []byte("three"), Checksum: []byte{0x03}, Size: 5, Compressed: true, CompressionAlgo: "lz4", Found: true},
	}
	tr, pc := startObjectTransferServer(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	responses, err := objecttransfer.NewClient(tr).StreamFetchObjects(ctx, &objecttransfer.StreamFetchObjectsRequest{
		ShardID: 9,
		Items: []objecttransfer.ObjectFetchItem{
			{ObjectRef: "b:1", Checksum: []byte{0x01}},
			{ObjectRef: "b:2"},
			{ObjectRef: "b:3", Checksum: []byte{0x03}},
		},
	})
	if err != nil {
		t.Fatalf("StreamFetchObjects: %v", err)
	}

	sent := receiveWithin(t, srv.streamReqs)
	if sent.ShardId != 9 || len(sent.Items) != 3 {
		t.Fatalf("server received %+v", sent)
	}
	for i, ref := range []string{"b:1", "b:2", "b:3"} {
		if sent.Items[i].ObjectRef != ref {
			t.Fatalf("item %d = %q, want %q", i, sent.Items[i].ObjectRef, ref)
		}
	}
	if !reflect.DeepEqual(sent.Items[0].Checksum, []byte{0x01}) || len(sent.Items[1].Checksum) != 0 {
		t.Fatalf("item checksums = %x, %x", sent.Items[0].Checksum, sent.Items[1].Checksum)
	}

	want := []*objecttransfer.FetchObjectResponse{
		{ObjectRef: "b:1", Data: []byte("one"), Size: 3, Found: true},
		{ObjectRef: "b:2", Found: false, ErrorMessage: "missing"},
		{ObjectRef: "b:3", Data: []byte("three"), Checksum: []byte{0x03}, Size: 5, Compressed: true, CompressionAlgo: objecttransfer.CompressionLZ4, Found: true},
	}
	for i, w := range want {
		got, ok := receiveOrClosed(t, responses)
		if !ok {
			t.Fatalf("stream closed after %d responses", i)
		}
		if !reflect.DeepEqual(got, w) {
			t.Fatalf("response %d = %+v, want %+v", i, got, w)
		}
	}
	if extra, ok := receiveOrClosed(t, responses); ok {
		t.Fatalf("unexpected response after the last item: %+v", extra)
	}
	assertConnReturned(t, pc)
}

// receiveOrClosed is receiveWithin for a channel that may close: ok is false
// once it has.
func receiveOrClosed[T any](t *testing.T, ch <-chan T) (T, bool) {
	t.Helper()
	select {
	case v, ok := <-ch:
		return v, ok
	case <-time.After(objectTransferWait):
		var zero T
		t.Fatalf("nothing received within %v", objectTransferWait)
		return zero, false
	}
}

func TestObjectTransferHandler_ServerErrors(t *testing.T) {
	cases := []struct {
		name string
		code codes.Code
		want sdkerrors.ErrorCode
	}{
		{name: "not found", code: codes.NotFound, want: sdkerrors.CodeNotFound},
		{name: "invalid argument", code: codes.InvalidArgument, want: sdkerrors.CodeInvalidInput},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := newRecordingObjectTransferServer()
			srv.err = status.Error(tc.code, "refused by server")
			tr, pc := startObjectTransferServer(t, srv)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			_, err := objecttransfer.NewClient(tr).FetchObject(ctx, &objecttransfer.FetchObjectRequest{ShardID: 1, ObjectRef: "b:k"})
			if sdkerrors.Code(err) != tc.want {
				t.Fatalf("FetchObject error = %v, want code %v", err, tc.want)
			}
			receiveWithin(t, srv.fetchReqs)
			assertConnReturned(t, pc)

			stream, err := tr.RequestStream(ctx, "quicbaas.ObjectTransfer", "StreamFetchObjects",
				&objecttransfer.StreamFetchObjectsRequest{ShardID: 1, Items: []objecttransfer.ObjectFetchItem{{ObjectRef: "b:k"}}})
			if err != nil {
				t.Fatalf("open stream: %v", err)
			}
			if _, err := stream.Recv(); sdkerrors.Code(err) != tc.want {
				t.Fatalf("Recv error = %v, want code %v", err, tc.want)
			}
			receiveWithin(t, srv.streamReqs)
			assertConnReturned(t, pc)
		})
	}
}

func TestObjectTransferHandler_RefusesBadRequests(t *testing.T) {
	srv := newRecordingObjectTransferServer()
	tr, pc := startObjectTransferServer(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	const svc = "quicbaas.ObjectTransfer"

	for _, req := range []interface{}{"wrong", nil, (*objecttransfer.FetchObjectRequest)(nil)} {
		if _, err := tr.Request(ctx, svc, "FetchObject", req); !sdkerrors.IsInvalidInput(err) {
			t.Fatalf("FetchObject(%#v) = %v, want invalid input", req, err)
		}
	}
	for _, req := range []interface{}{"wrong", nil, (*objecttransfer.StreamFetchObjectsRequest)(nil)} {
		if _, err := tr.RequestStream(ctx, svc, "StreamFetchObjects", req); !sdkerrors.IsInvalidInput(err) {
			t.Fatalf("StreamFetchObjects(%#v) = %v, want invalid input", req, err)
		}
	}
	if _, err := tr.Request(ctx, svc, "StreamFetchObjects", nil); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("unary StreamFetchObjects = %v, want unimplemented", err)
	}
	if _, err := tr.RequestStream(ctx, svc, "FetchObject", nil); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("streaming FetchObject = %v, want unimplemented", err)
	}

	select {
	case got := <-srv.fetchReqs:
		t.Fatalf("refused FetchObject reached the server: %+v", got)
	case got := <-srv.streamReqs:
		t.Fatalf("refused StreamFetchObjects reached the server: %+v", got)
	default:
	}
	assertConnReturned(t, pc)
}

func TestObjectTransferHandler_StreamSendAndClose(t *testing.T) {
	srv := newRecordingObjectTransferServer()
	srv.streamResps = []*pb.FetchObjectResponse{{ObjectRef: "b:1", Found: true}}
	tr, pc := startObjectTransferServer(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := tr.RequestStream(ctx, "quicbaas.ObjectTransfer", "StreamFetchObjects",
		&objecttransfer.StreamFetchObjectsRequest{ShardID: 1, Items: []objecttransfer.ObjectFetchItem{{ObjectRef: "b:1"}}})
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if err := stream.Send(&objecttransfer.ObjectFetchItem{ObjectRef: "b:2"}); sdkerrors.Code(err) != sdkerrors.CodeUnimplemented {
		t.Fatalf("Send = %v, want unimplemented", err)
	}
	if err := stream.CloseSend(); err != nil {
		t.Fatalf("CloseSend = %v", err)
	}
	if !pc.inUse.Load() {
		t.Fatal("open stream does not hold the pooled connection")
	}

	if err := stream.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	assertConnReturned(t, pc)

	// Another caller takes the connection; a second Close must not give it
	// back from under them.
	if _, err := tr.pool.Get(); err != nil {
		t.Fatalf("pool.Get: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	if !pc.inUse.Load() {
		t.Fatal("second Close returned the connection again")
	}
}

// holdingObjectTransferServer sends one object, then holds the stream open
// until the client cancels it, reporting the cancellation on cancelled.
type holdingObjectTransferServer struct {
	pb.UnimplementedObjectTransferServiceServer
	cancelled chan struct{}
}

func (s *holdingObjectTransferServer) StreamFetchObjects(_ *pb.StreamFetchObjectsRequest, stream grpc.ServerStreamingServer[pb.FetchObjectResponse]) error {
	if err := stream.Send(&pb.FetchObjectResponse{ObjectRef: "b:1", Found: true}); err != nil {
		return err
	}
	<-stream.Context().Done()
	close(s.cancelled)
	return stream.Context().Err()
}

// Close on a partly read stream opened under a background context must
// cancel the RPC: a blocked Recv returns and the server sees the cancellation.
func TestObjectTransferHandler_StreamCloseCancelsTheRPC(t *testing.T) {
	srv := &holdingObjectTransferServer{cancelled: make(chan struct{})}
	tr, pc := startObjectTransferServer(t, srv)

	stream, err := tr.RequestStream(context.Background(), "quicbaas.ObjectTransfer", "StreamFetchObjects",
		&objecttransfer.StreamFetchObjectsRequest{ShardID: 1, Items: []objecttransfer.ObjectFetchItem{{ObjectRef: "b:1"}}})
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if _, err := stream.Recv(); err != nil {
		t.Fatalf("first Recv = %v", err)
	}

	recvErr := make(chan error, 1)
	go func() {
		_, err := stream.Recv()
		recvErr <- err
	}()

	if err := stream.Close(); err != nil {
		t.Fatalf("Close = %v", err)
	}
	if err := receiveWithin(t, recvErr); err == nil {
		t.Fatal("Recv after Close returned no error")
	}
	receiveWithin(t, (<-chan struct{})(srv.cancelled))
	assertConnReturned(t, pc)

	// A second Close is a no-op: it must not give back a connection another
	// caller now holds.
	if _, err := tr.pool.Get(); err != nil {
		t.Fatalf("pool.Get: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("second Close = %v", err)
	}
	if !pc.inUse.Load() {
		t.Fatal("second Close returned the connection again")
	}
}

func TestObjectTransferHandler_StreamCleanEnd(t *testing.T) {
	srv := newRecordingObjectTransferServer()
	tr, pc := startObjectTransferServer(t, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stream, err := tr.RequestStream(ctx, "quicbaas.ObjectTransfer", "StreamFetchObjects",
		&objecttransfer.StreamFetchObjectsRequest{ShardID: 1, Items: []objecttransfer.ObjectFetchItem{{ObjectRef: "b:1"}}})
	if err != nil {
		t.Fatalf("open stream: %v", err)
	}
	if _, err := stream.Recv(); err != io.EOF {
		t.Fatalf("clean end = %v, want io.EOF", err)
	}
	assertConnReturned(t, pc)
}
