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
	"bytes"
	"context"
	"crypto/sha256"
	"io"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// ObjectStoreReader provides read access to the local object store.
// This interface is implemented by the ObjectStoreStateMachine.
type ObjectStoreReader interface {
	// ReadObject reads object data by its storage reference.
	ReadObject(objectRef string) ([]byte, error)
}

// objectTransferServer implements the gRPC ObjectTransferServiceServer interface.
type objectTransferServer struct {
	pb.UnimplementedObjectTransferServiceServer
	stores map[uint64]ObjectStoreReader // shardID -> object store
	logger *logger.Logger
}

// NewObjectTransferServer creates a new object transfer gRPC server.
func NewObjectTransferServer(log *logger.Logger) *objectTransferServer {
	return &objectTransferServer{
		stores: make(map[uint64]ObjectStoreReader),
		logger: log.WithField("component", "object-transfer-server"),
	}
}

// RegisterStore registers an object store for a shard.
func (s *objectTransferServer) RegisterStore(shardID uint64, store ObjectStoreReader) {
	s.stores[shardID] = store
	s.logger.WithField("shard_id", shardID).Debug("registered object store for transfer")
}

// UnregisterStore removes an object store registration.
func (s *objectTransferServer) UnregisterStore(shardID uint64) {
	delete(s.stores, shardID)
	s.logger.WithField("shard_id", shardID).Debug("unregistered object store for transfer")
}

// RegisterObjectTransferServer registers the object transfer service with a gRPC server.
func RegisterObjectTransferServer(server *grpc.Server, transferServer *objectTransferServer) {
	pb.RegisterObjectTransferServiceServer(server, transferServer)
}

// FetchObject retrieves object data from the local object store.
func (s *objectTransferServer) FetchObject(ctx context.Context, req *pb.FetchObjectRequest) (*pb.FetchObjectResponse, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is nil")
	}

	if req.ObjectRef == "" {
		return nil, status.Error(codes.InvalidArgument, "object_ref is required")
	}

	// Get object store for the shard
	store, ok := s.stores[req.ShardId]
	if !ok {
		return &pb.FetchObjectResponse{
			ObjectRef:    req.ObjectRef,
			Found:        false,
			ErrorMessage: "shard not found on this node",
		}, nil
	}

	// Read object from local store
	data, err := store.ReadObject(req.ObjectRef)
	if err != nil {
		s.logger.WithFields(map[string]interface{}{
			"shard_id":   req.ShardId,
			"object_ref": req.ObjectRef,
			"error":      err.Error(),
		}).Debug("object not found in local store")

		return &pb.FetchObjectResponse{
			ObjectRef:    req.ObjectRef,
			Found:        false,
			ErrorMessage: err.Error(),
		}, nil
	}

	// Compute checksum
	hash := sha256.Sum256(data)
	checksum := hash[:]

	// Verify checksum if provided in request
	if len(req.Checksum) > 0 && !bytes.Equal(req.Checksum, checksum) {
		return &pb.FetchObjectResponse{
			ObjectRef:    req.ObjectRef,
			Found:        true,
			ErrorMessage: "checksum mismatch",
		}, nil
	}

	s.logger.WithFields(map[string]interface{}{
		"shard_id":   req.ShardId,
		"object_ref": req.ObjectRef,
		"size":       len(data),
	}).Debug("object fetched successfully")

	return &pb.FetchObjectResponse{
		ObjectRef: req.ObjectRef,
		Data:      data,
		Checksum:  checksum,
		Size:      int64(len(data)),
		Found:     true,
	}, nil
}

// StreamFetchObjects retrieves multiple objects in a single stream.
func (s *objectTransferServer) StreamFetchObjects(req *pb.StreamFetchObjectsRequest, stream pb.ObjectTransferService_StreamFetchObjectsServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is nil")
	}

	// Get object store for the shard
	store, ok := s.stores[req.ShardId]
	if !ok {
		return status.Errorf(codes.NotFound, "shard %d not found on this node", req.ShardId)
	}

	for _, item := range req.Items {
		// Check context for cancellation
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		default:
		}

		resp := s.fetchSingleObject(store, item)
		if err := stream.Send(resp); err != nil {
			return err
		}
	}

	return nil
}

// fetchSingleObject fetches a single object and builds the response.
func (s *objectTransferServer) fetchSingleObject(store ObjectStoreReader, item *pb.ObjectFetchItem) *pb.FetchObjectResponse {
	data, err := store.ReadObject(item.ObjectRef)
	if err != nil {
		return &pb.FetchObjectResponse{
			ObjectRef:    item.ObjectRef,
			Found:        false,
			ErrorMessage: err.Error(),
		}
	}

	// Compute checksum
	hash := sha256.Sum256(data)
	checksum := hash[:]

	// Verify checksum if provided
	if len(item.Checksum) > 0 && !bytes.Equal(item.Checksum, checksum) {
		return &pb.FetchObjectResponse{
			ObjectRef:    item.ObjectRef,
			Found:        true,
			ErrorMessage: "checksum mismatch",
		}
	}

	return &pb.FetchObjectResponse{
		ObjectRef: item.ObjectRef,
		Data:      data,
		Checksum:  checksum,
		Size:      int64(len(data)),
		Found:     true,
	}
}

// ObjectStoreReaderFunc adapts a function to ObjectStoreReader interface.
type ObjectStoreReaderFunc func(objectRef string) ([]byte, error)

// ReadObject implements ObjectStoreReader.
func (f ObjectStoreReaderFunc) ReadObject(objectRef string) ([]byte, error) {
	return f(objectRef)
}

// ObjectStoreReaderFromStorage creates an ObjectStoreReader from a storage backend.
// This is a helper for common.Storage implementations.
func ObjectStoreReaderFromStorage(storage interface {
	Get(key string) (io.ReadCloser, error)
}) ObjectStoreReader {
	return ObjectStoreReaderFunc(func(objectRef string) ([]byte, error) {
		reader, err := storage.Get(objectRef)
		if err != nil {
			return nil, err
		}
		defer reader.Close()
		return io.ReadAll(reader)
	})
}
