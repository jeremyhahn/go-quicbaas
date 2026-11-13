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

package embedded

import (
	"context"
	"reflect"

	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	objclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objectstorage"
	transferclient "github.com/jeremyhahn/go-quicbaas/sdk/go/client/objecttransfer"
)

// The ObjectStorage and ObjectTransfer stream verbs on the in-process
// embedded transport. Each one calls the same service-layer verb its unary
// counterpart calls, so an embedded caller gets the same fields stored and
// the same errors back.

// objectStreamChunkSize bounds the data of each chunk StreamGetObject
// delivers. It matches the objectstorage client's default upload chunk.
const objectStreamChunkSize = 64 * 1024

// putChunk is one decoded StreamPutObject message.
type putChunk struct {
	metadata *objclient.PutObjectRequest
	data     []byte
	isLast   bool
}

var (
	putRequestType = reflect.TypeOf((*objclient.PutObjectRequest)(nil))
	byteSliceType  = reflect.TypeOf([]byte(nil))
)

// handleObjStreamPutObject collects a streamed upload: a metadata chunk, then
// data chunks up to the one marked last. The service stores the object with
// a single PutObject call once the last chunk arrives, so a stream that is
// canceled or ends early stores nothing.
func handleObjStreamPutObject(ctx context.Context, svc service.QuicBaaS, _ interface{}, s *stream) error {
	if svc.ObjectStorageService() == nil {
		return NewServiceNotConfiguredError("ObjectStorage")
	}
	req, err := collectStreamPut(ctx, s)
	if err != nil {
		return err
	}
	// A cancellation that raced the last chunk still stores nothing.
	if err := s.context().Err(); err != nil {
		return err
	}
	resp, err := handleObjPutObject(ctx, svc, req)
	if err != nil {
		return err
	}
	return s.deliver(resp)
}

// collectStreamPut reads upload chunks until the last one and returns the
// metadata request carrying the assembled value.
func collectStreamPut(ctx context.Context, s *stream) (*objclient.PutObjectRequest, error) {
	var (
		req   *objclient.PutObjectRequest
		value []byte
	)
	chunks := s.sendChan()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-s.context().Done():
			return nil, s.context().Err()
		case msg, open := <-chunks:
			if !open {
				return nil, NewIncompleteStreamError("quicbaas.ObjectStorage", "StreamPutObject", "the upload ended before its last chunk")
			}
			chunk, err := decodePutChunk(msg)
			if err != nil {
				return nil, err
			}
			if req == nil {
				if chunk.metadata == nil {
					return nil, NewIncompleteStreamError("quicbaas.ObjectStorage", "StreamPutObject", "the first chunk carries no object metadata")
				}
				meta := *chunk.metadata
				req = &meta
			}
			value = append(value, chunk.data...)
			if chunk.isLast {
				req.Value = value
				return req, nil
			}
		}
	}
}

// decodePutChunk reads the objectstorage client's upload chunk. The client
// sends it as an unexported struct, so its exported fields are read by name
// and checked against the types the client declares.
func decodePutChunk(msg interface{}) (putChunk, error) {
	v := reflect.ValueOf(msg)
	if v.Kind() != reflect.Pointer || v.IsNil() || v.Elem().Kind() != reflect.Struct {
		return putChunk{}, invalidPutChunk(msg)
	}
	chunk := v.Elem()
	if chunk.Type().Name() != "streamPutChunk" || chunk.Type().PkgPath() != putRequestType.Elem().PkgPath() {
		return putChunk{}, invalidPutChunk(msg)
	}
	metadata := chunk.FieldByName("Metadata")
	data := chunk.FieldByName("Data")
	isLast := chunk.FieldByName("IsLast")
	if !metadata.IsValid() || metadata.Type() != putRequestType ||
		!data.IsValid() || data.Type() != byteSliceType ||
		!isLast.IsValid() || isLast.Kind() != reflect.Bool {
		return putChunk{}, invalidPutChunk(msg)
	}
	meta, _ := metadata.Interface().(*objclient.PutObjectRequest)
	return putChunk{metadata: meta, data: data.Bytes(), isLast: isLast.Bool()}, nil
}

func invalidPutChunk(msg interface{}) error {
	return NewInvalidRequestTypeError("quicbaas.ObjectStorage", "StreamPutObject", "*objectstorage.streamPutChunk", typeNameOf(msg))
}

// handleObjStreamGetObject reads the object with the service's GetObject and
// delivers its value as offset-ordered chunks; every chunk carries the total
// size and the final one is marked last.
func handleObjStreamGetObject(ctx context.Context, svc service.QuicBaaS, req interface{}, s *stream) error {
	resp, err := handleObjGetObject(ctx, svc, req)
	if err != nil {
		return err
	}
	var value []byte
	if obj, ok := resp.(*objclient.GetObjectResponse); ok && obj != nil {
		value = obj.Value
	}
	total := len(value)
	for offset := 0; ; {
		end := min(offset+objectStreamChunkSize, total)
		chunk := &objclient.ObjectChunk{
			Data:      value[offset:end],
			Offset:    int64(offset),
			IsLast:    end == total,
			TotalSize: int64(total),
		}
		if err := s.deliver(chunk); err != nil {
			return err
		}
		if chunk.IsLast {
			return nil
		}
		offset = end
	}
}

// handleTransferStreamFetchObjects fetches every requested object from the
// shard with the service's StreamFetchObjects and delivers one response per
// object, then the nil message the objecttransfer client reads as the end of
// the stream.
func handleTransferStreamFetchObjects(ctx context.Context, svc service.QuicBaaS, req interface{}, s *stream) error {
	transferSvc := svc.ObjectTransferService()
	if transferSvc == nil {
		return NewServiceNotConfiguredError("ObjectTransfer")
	}
	r, ok := req.(*transferclient.StreamFetchObjectsRequest)
	if !ok || r == nil {
		return NewInvalidRequestTypeError("quicbaas.ObjectTransfer", "StreamFetchObjects", "*objecttransfer.StreamFetchObjectsRequest", typeNameOf(req))
	}

	items := make([]service.ObjectFetchItem, len(r.Items))
	for i, item := range r.Items {
		items[i] = service.ObjectFetchItem{ObjectRef: item.ObjectRef, Checksum: item.Checksum}
	}
	resp, err := transferSvc.StreamFetchObjects(ctx, &service.StreamFetchObjectsRequest{ShardID: r.ShardID, Items: items})
	if err != nil {
		return err
	}

	if resp != nil {
		for i := range resp.Objects {
			if err := s.deliver(fetchObjectToClient(&resp.Objects[i])); err != nil {
				return err
			}
		}
	}
	return s.deliver(nil)
}

// handleTransferFetchObject serves the unary quicbaas.ObjectTransfer/FetchObject
// verb. A nil service response is refused rather than returned, since the
// client would otherwise read a zero-valued response as Found:false.
func handleTransferFetchObject(ctx context.Context, svc service.QuicBaaS, req interface{}) (interface{}, error) {
	transferSvc := svc.ObjectTransferService()
	if transferSvc == nil {
		return nil, NewServiceNotConfiguredError("ObjectTransfer")
	}
	r, ok := req.(*transferclient.FetchObjectRequest)
	if !ok || r == nil {
		return nil, NewInvalidRequestTypeError("quicbaas.ObjectTransfer", "FetchObject", "*objecttransfer.FetchObjectRequest", typeNameOf(req))
	}

	resp, err := transferSvc.FetchObject(ctx, &service.FetchObjectRequest{
		ShardID:   r.ShardID,
		ObjectRef: r.ObjectRef,
		Checksum:  r.Checksum,
		RequestID: r.RequestID,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, NewEmptyServiceResponseError("quicbaas.ObjectTransfer", "FetchObject")
	}
	return fetchObjectToClient(resp), nil
}

// fetchObjectToClient converts one fetched object from the service type to
// the client type, shared by the unary and streaming fetch verbs.
func fetchObjectToClient(obj *service.FetchObjectResponse) *transferclient.FetchObjectResponse {
	return &transferclient.FetchObjectResponse{
		ObjectRef:       obj.ObjectRef,
		Data:            obj.Data,
		Checksum:        obj.Checksum,
		Size:            obj.Size,
		Compressed:      obj.Compressed,
		CompressionAlgo: transferclient.CompressionAlgo(obj.CompressionAlgo),
		Found:           obj.Found,
		ErrorMessage:    obj.ErrorMessage,
	}
}
