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

// Package grpc provides the gRPC server implementation for QuicBaaS.
// It exposes all database operations via gRPC protocol with support for
// streaming, consistency levels, and comprehensive error handling.
package grpc

import (
	"context"
	"fmt"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	api "github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/interfaces"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/routing"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
	"github.com/jeremyhahn/go-quicbaas/pkg/shards"
	"github.com/jeremyhahn/go-quicbaas/pkg/util/metrics"
)

// Server implements the QuicBaaS gRPC service.
type Server struct {
	pb.UnimplementedQuicBaaSServer
	pb.UnimplementedEmbeddingServiceServer

	service   service.QuicBaaS
	logger    *logger.Logger
	metrics   *serverMetrics
	startTime time.Time
	nodeID    uint64
	version   string
	// address is the server's listen address for client connections
	address string
	// qpsTracker tracks queries per second for metrics reporting
	qpsTracker *QPSTracker
	// shardRouter selects shard for (entityType, id)
	shardRouter interface{ Route(string, uint64) uint64 }
	// sealManager is the injected seal manager for seal/unseal operations
	sealManager interfaces.SealManager
	// vectorShardID is the shard ID for vector operations, set at construction time.
	vectorShardID uint64
}

// serverMetrics holds Prometheus metrics for the gRPC server.
type serverMetrics struct {
	requestsTotal    *metrics.CounterVec
	requestDuration  *metrics.HistogramVec
	activeStreams    *metrics.Gauge
	batchSizeHist    *metrics.HistogramVec
	consistencyLevel *metrics.CounterVec
}

// NewServer creates a new gRPC server instance.
func NewServer(
	svc service.QuicBaaS,
	log *logger.Logger,
	nodeID uint64,
	version string,
) (*Server, error) {
	if svc == nil {
		return nil, api.NewRequiredFieldError("service")
	}

	if log == nil {
		return nil, api.NewRequiredFieldError("logger")
	}

	srv := &Server{
		service:       svc,
		logger:        log.WithField("component", "grpc-server"),
		startTime:     time.Now(),
		nodeID:        nodeID,
		version:       version,
		qpsTracker:    NewQPSTracker(),
		vectorShardID: shards.PlatformShardID,
	}

	// Default to single-shard routing
	srv.shardRouter = routing.New()

	srv.initMetrics()
	srv.logger.Info("gRPC server initialized")

	return srv, nil
}

// SetAddress sets the server's listen address.
// This should be called after the server is bound to a port.
func (s *Server) SetAddress(address string) {
	s.address = address
}

// Address returns the server's listen address.
func (s *Server) Address() string {
	return s.address
}

// SetVectorShardID sets the shard ID used for vector operations.
func (s *Server) SetVectorShardID(id uint64) {
	s.vectorShardID = id
}

// SetSealManager sets the seal manager for seal/unseal operations.
// This should be called during server initialization.
func (s *Server) SetSealManager(mgr interfaces.SealManager) {
	s.sealManager = mgr
}

// RecordQuery increments the QPS counter. Should be called for each query operation.
func (s *Server) RecordQuery() {
	if s.qpsTracker != nil {
		s.qpsTracker.Record()
	}
}

// QPS returns the current queries per second rate.
func (s *Server) QPS() float64 {
	if s.qpsTracker != nil {
		return s.qpsTracker.QPS()
	}
	return 0
}

// initMetrics initializes Prometheus metrics for the server.
func (s *Server) initMetrics() {
	s.metrics = &serverMetrics{
		requestsTotal: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "grpc",
			Name:      "requests_total",
			Help:      "Total number of gRPC requests",
		}, []string{"method", "status"}),

		requestDuration: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "grpc",
			Name:      "request_duration_seconds",
			Help:      "Duration of gRPC requests in seconds",
			Buckets:   []float64{.001, .005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
		}, []string{"method"}),

		activeStreams: metrics.NewGauge(metrics.GaugeOpts{
			Namespace: "quicbaas",
			Subsystem: "grpc",
			Name:      "active_streams",
			Help:      "Number of active streaming RPCs",
		}),

		batchSizeHist: metrics.NewHistogramVec(metrics.HistogramOpts{
			Namespace: "quicbaas",
			Subsystem: "grpc",
			Name:      "batch_size",
			Help:      "Size of batch operations",
			Buckets:   []float64{1, 5, 10, 25, 50, 100, 250, 500, 1000},
		}, []string{"operation"}),

		consistencyLevel: metrics.NewCounterVec(metrics.CounterOpts{
			Namespace: "quicbaas",
			Subsystem: "grpc",
			Name:      "consistency_level_total",
			Help:      "Total requests by consistency level",
		}, []string{"level"}),
	}
}

// SetShardRouter overrides the default shard router.
func (s *Server) SetShardRouter(r interface{ Route(string, uint64) uint64 }) {
	s.shardRouter = r
}

// extractWriteMode extracts write mode from gRPC metadata
// Returns Sync mode by default (STRONG durability guarantee)
// Clients must explicitly request Batched or WAL modes for better performance.
func extractWriteMode(ctx context.Context) service.WriteMode {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return service.WriteModeSync // Default to sync for durability
	}

	values := md.Get("write-mode")
	if len(values) == 0 {
		return service.WriteModeSync // Default to sync for durability
	}

	switch values[0] {
	case "sync":
		return service.WriteModeSync
	case "batched":
		return service.WriteModeBatched
	case "wal_durable":
		return service.WriteModeWALDurable
	default:
		return service.WriteModeSync // Default on unknown value
	}
}

// extractProtocol extracts protocol from gRPC metadata
// Returns JSON protocol by default for backwards compatibility
// Clients can set x-quicbaas-protocol header to "binary" for max performance.
func extractProtocol(ctx context.Context) protocol {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return protocolJSON // Default to JSON
	}

	values := md.Get("x-quicbaas-protocol")
	if len(values) == 0 {
		return protocolJSON // Default to JSON
	}

	if values[0] == "binary" {
		return protocolBinary
	}

	return protocolJSON // Unknown value, default to JSON
}

// protocol represents the encoding protocol.
type protocol int

const (
	protocolJSON protocol = iota
	protocolBinary
)

func (p protocol) IsBinary() bool {
	return p == protocolBinary
}

func (p protocol) IsJSON() bool {
	return p == protocolJSON
}

// Put stores a key-value pair with optional consistency guarantees.
func (s *Server) Put(ctx context.Context, req *pb.PutRequest) (*pb.PutResponse, error) {
	ctx, endSpan := startGRPCSpan(ctx, "grpc.Put", map[string]interface{}{
		"entity_type": req.EntityType,
		"id":          int64(req.Id), // #nosec G115 -- ID for tracing
	})
	defer endSpan()
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("Put").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := validatePutRequest(req); err != nil {
		s.metrics.requestsTotal.With("Put", "invalid").Inc()
		s.logger.WithError(err).Warn("Invalid Put request")
		return nil, toProtoError(err)
	}

	// Track consistency level
	s.metrics.consistencyLevel.With(req.Consistency.String()).Inc()

	// Log the operation
	s.logger.WithFields(map[string]interface{}{
		"entity_type": req.EntityType,
		"id":          req.Id,
		"consistency": req.Consistency.String(),
		"data_size":   len(req.Data),
	}).Debug("Processing Put request")

	// Extract write mode from metadata (defaults to sync)
	mode := extractWriteMode(ctx)

	// Convert proto request to service request
	serviceReq := &service.PutRequest{
		EntityType:  req.EntityType,
		ID:          req.Id,
		Data:        req.Data,
		Consistency: protoToServiceConsistency(req.Consistency),
		WriteMode:   mode,
	}

	// Call service
	serviceResp, err := s.service.Put(ctx, serviceReq)
	if err != nil {
		s.metrics.requestsTotal.With("Put", "error").Inc()
		s.logger.WithError(err).Error("Failed to Put")
		return nil, toServiceError(err)
	}

	// Build proto response
	resp := &pb.PutResponse{
		Id:        serviceResp.ID,
		Version:   serviceResp.Version,
		Timestamp: serviceResp.Timestamp,
		LeaderId:  serviceResp.LeaderID,
	}

	s.metrics.requestsTotal.With("Put", "success").Inc()
	return resp, nil
}

// Get retrieves a value by key with configurable consistency.
func (s *Server) Get(ctx context.Context, req *pb.GetRequest) (*pb.GetResponse, error) {
	ctx, endSpan := startGRPCSpan(ctx, "grpc.Get", map[string]interface{}{
		"entity_type": req.EntityType,
		"id":          int64(req.Id), // #nosec G115 -- ID for tracing attribute
	})
	defer endSpan()
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("Get").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := validateGetRequest(req); err != nil {
		s.metrics.requestsTotal.With("Get", "invalid").Inc()
		s.logger.WithError(err).Warn("Invalid Get request")
		return nil, toProtoError(err)
	}

	// Track consistency level
	s.metrics.consistencyLevel.With(req.Consistency.String()).Inc()

	s.logger.WithFields(map[string]interface{}{
		"entity_type": req.EntityType,
		"id":          req.Id,
		"consistency": req.Consistency.String(),
	}).Debug("Processing Get request")

	// Convert proto request to service request
	serviceReq := &service.GetRequest{
		EntityType:  req.EntityType,
		ID:          req.Id,
		Consistency: protoToServiceConsistency(req.Consistency),
		TimeoutMs:   5000, // Default timeout as proto doesnt have timeout_ms in GetRequest
	}

	// Call service
	serviceResp, err := s.service.Get(ctx, serviceReq)
	if err != nil {
		s.metrics.requestsTotal.With("Get", "error").Inc()
		s.logger.WithError(err).Error("Failed to Get")
		return nil, toServiceError(err)
	}

	// Check if entity exists
	if !serviceResp.Found {
		s.metrics.requestsTotal.With("Get", "not_found").Inc()
		return nil, status.Error(codes.NotFound, fmt.Sprintf("entity not found: %s/%d", req.EntityType, req.Id))
	}

	// Build proto response
	resp := &pb.GetResponse{
		Id:        serviceResp.ID,
		Data:      serviceResp.Data,
		Found:     serviceResp.Found,
		Version:   serviceResp.Version,
		Timestamp: serviceResp.Timestamp,
	}

	s.metrics.requestsTotal.With("Get", "success").Inc()
	return resp, nil
}

// Delete removes a key-value pair.
func (s *Server) Delete(ctx context.Context, req *pb.DeleteRequest) (*pb.DeleteResponse, error) {
	ctx, endSpan := startGRPCSpan(ctx, "grpc.Delete", map[string]interface{}{
		"entity_type": req.EntityType,
		"id":          int64(req.Id), // #nosec G115 -- ID for tracing attribute
	})
	defer endSpan()
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("Delete").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := validateDeleteRequest(req); err != nil {
		s.metrics.requestsTotal.With("Delete", "invalid").Inc()
		s.logger.WithError(err).Warn("Invalid Delete request")
		return nil, toProtoError(err)
	}

	s.logger.WithFields(map[string]interface{}{
		"entity_type": req.EntityType,
		"id":          req.Id,
		"consistency": req.Consistency.String(),
	}).Debug("Processing Delete request")

	// Convert proto request to service request
	serviceReq := &service.DeleteRequest{
		EntityType:  req.EntityType,
		ID:          req.Id,
		Consistency: protoToServiceConsistency(req.Consistency),
	}

	// Call service
	serviceResp, err := s.service.Delete(ctx, serviceReq)
	if err != nil {
		s.metrics.requestsTotal.With("Delete", "error").Inc()
		s.logger.WithError(err).Error("Failed to Delete")
		return nil, toServiceError(err)
	}

	// Build proto response
	resp := &pb.DeleteResponse{
		Deleted:   serviceResp.Deleted,
		Timestamp: serviceResp.Timestamp,
	}

	s.metrics.requestsTotal.With("Delete", "success").Inc()
	return resp, nil
}

// List returns all entities of a type with pagination (streaming).
func (s *Server) List(req *pb.ListRequest, stream pb.QuicBaaS_ListServer) error {
	start := time.Now()
	s.metrics.activeStreams.Inc()
	defer func() {
		s.metrics.activeStreams.Dec()
		s.metrics.requestDuration.With("List").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := validateListRequest(req); err != nil {
		s.metrics.requestsTotal.With("List", "invalid").Inc()
		s.logger.WithError(err).Warn("Invalid List request")
		return toProtoError(err)
	}

	s.logger.WithFields(map[string]interface{}{
		"entity_type": req.EntityType,
		"page_size":   req.PageSize,
	}).Debug("Processing List request")

	// Convert proto request to service request
	serviceReq := &service.ListRequest{
		EntityType: req.EntityType,
		PageSize:   uint64(req.PageSize),
		PageToken:  req.PageToken,
	}

	// Create channel for streaming results
	responseChan := make(chan *service.ListResponse, 1)

	// Call service in goroutine
	go func() {
		err := s.service.List(stream.Context(), serviceReq, responseChan)
		if err != nil {
			s.logger.WithError(err).Error("List operation failed")
		}
	}()

	// Stream results to client
	for serviceResp := range responseChan {
		// Convert service entities to proto entities
		protoEntities := make([]*pb.Entity, 0, len(serviceResp.Entities))
		for _, entity := range serviceResp.Entities {
			protoEntities = append(protoEntities, &pb.Entity{
				EntityType: entity.EntityType,
				Id:         entity.ID,
				Data:       entity.Data,
				Version:    entity.Version,
				Timestamp:  entity.Timestamp,
			})
		}

		// Send batch to client
		resp := &pb.ListResponse{
			Entities:      protoEntities,
			NextPageToken: serviceResp.NextPageToken,
			TotalCount:    serviceResp.TotalCount,
		}

		if err := stream.Send(resp); err != nil {
			s.metrics.requestsTotal.With("List", "error").Inc()
			return toServiceError(&errors.QBError{
				Code: errors.ErrInternal,
				Op:   "grpc.List",
				Err:  err,
			})
		}
	}

	s.metrics.requestsTotal.With("List", "success").Inc()
	return nil
}

// ListEntityTypes returns all unique entity types stored in the database.
func (s *Server) ListEntityTypes(ctx context.Context, req *pb.ListEntityTypesRequest) (*pb.ListEntityTypesResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("ListEntityTypes").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if req == nil {
		req = &pb.ListEntityTypesRequest{}
	}

	s.logger.WithFields(map[string]interface{}{
		"page_size":  req.PageSize,
		"page_token": req.PageToken,
	}).Debug("Processing ListEntityTypes request")

	// Convert proto request to service request
	serviceReq := &service.ListEntityTypesRequest{
		PageSize:  int(req.PageSize),
		PageToken: req.PageToken,
	}

	// Call service
	serviceResp, err := s.service.ListEntityTypes(ctx, serviceReq)
	if err != nil {
		s.metrics.requestsTotal.With("ListEntityTypes", "error").Inc()
		s.logger.WithError(err).Error("ListEntityTypes operation failed")
		return nil, toProtoError(err)
	}

	// Build proto response
	resp := &pb.ListEntityTypesResponse{
		EntityTypes:   serviceResp.EntityTypes,
		NextPageToken: serviceResp.NextPageToken,
		TotalCount:    uint64(serviceResp.TotalCount), // #nosec G115 -- TotalCount is always non-negative
	}

	s.metrics.requestsTotal.With("ListEntityTypes", "success").Inc()
	return resp, nil
}

// Watch monitors a key or key prefix for changes (streaming).
func (s *Server) Watch(req *pb.WatchRequest, stream pb.QuicBaaS_WatchServer) error {
	start := time.Now()
	s.metrics.activeStreams.Inc()
	defer func() {
		s.metrics.activeStreams.Dec()
		s.metrics.requestDuration.With("Watch").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if len(req.Key) == 0 {
		s.metrics.requestsTotal.With("Watch", "invalid").Inc()
		s.logger.Warn("Invalid Watch request: key is required")
		return toProtoError(api.NewRequiredFieldError("key"))
	}

	s.logger.WithFields(map[string]interface{}{
		"key":       string(req.Key),
		"is_prefix": req.IsPrefix,
	}).Debug("Processing Watch request")

	// Convert proto filters to service filters
	var serviceFilters []service.WatchEventType
	for _, f := range req.Filters {
		serviceFilters = append(serviceFilters, service.WatchEventType(f))
	}

	// Convert proto request to service request
	serviceReq := &service.WatchRequest{
		Key:            req.Key,
		IsPrefix:       req.IsPrefix,
		StartRevision:  req.StartRevision,
		PrevKV:         req.PrevKv,
		ProgressNotify: req.ProgressNotify,
		Filters:        serviceFilters,
		WatchID:        req.WatchId,
	}

	// Create channel for streaming results
	responseChan := make(chan *service.WatchResponse, 100)

	// Call service to start watching
	watchID, err := s.service.Watch(stream.Context(), serviceReq, responseChan)
	if err != nil {
		s.metrics.requestsTotal.With("Watch", "error").Inc()
		s.logger.WithError(err).Error("Watch operation failed")
		return toProtoError(err)
	}

	s.logger.WithFields(map[string]interface{}{
		"watch_id": watchID,
		"key":      string(req.Key),
	}).Debug("Watch created successfully")

	// Stream events to client
	for serviceResp := range responseChan {
		// Convert service events to proto events
		protoEvents := make([]*pb.WatchEvent, 0, len(serviceResp.Events))
		for _, event := range serviceResp.Events {
			protoEvents = append(protoEvents, &pb.WatchEvent{
				Type:      pb.WatchEventType(event.Type), // #nosec G115 -- event.Type is bounded enum value
				Key:       event.Key,
				Value:     event.Value,
				PrevValue: event.PrevValue,
				Version:   event.Version,
				Revision:  event.Revision,
				Timestamp: event.Timestamp,
			})
		}

		// Send response to client
		resp := &pb.WatchResponse{
			WatchId:         serviceResp.WatchID,
			Created:         serviceResp.Created,
			Canceled:        serviceResp.Canceled,
			CancelReason:    serviceResp.CancelReason,
			CompactRevision: serviceResp.CompactRevision,
			Events:          protoEvents,
			Revision:        serviceResp.Revision,
		}

		if err := stream.Send(resp); err != nil {
			s.metrics.requestsTotal.With("Watch", "error").Inc()
			s.logger.WithError(err).Error("Failed to send watch response")
			return toServiceError(&errors.QBError{
				Code: errors.ErrInternal,
				Op:   "grpc.Watch",
				Err:  err,
			})
		}
	}

	s.metrics.requestsTotal.With("Watch", "success").Inc()
	return nil
}

// Query searches for entities using secondary indexes (streaming).
func (s *Server) Query(req *pb.QueryRequest, stream pb.QuicBaaS_QueryServer) error {
	start := time.Now()
	s.metrics.activeStreams.Inc()
	defer func() {
		s.metrics.activeStreams.Dec()
		s.metrics.requestDuration.With("Query").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := validateQueryRequest(req); err != nil {
		s.metrics.requestsTotal.With("Query", "invalid").Inc()
		s.logger.WithError(err).Warn("Invalid Query request")
		return toProtoError(err)
	}

	s.logger.WithFields(map[string]interface{}{
		"entity_type": req.EntityType,
		"index_name":  req.IndexName,
	}).Debug("Processing Query request")

	// Convert proto request to service request
	serviceReq := &service.QueryRequest{
		EntityType:  req.EntityType,
		IndexName:   req.IndexName,
		Value:       req.Value,
		Consistency: protoToServiceConsistency(req.Consistency),
	}

	// Create channel for streaming results
	responseChan := make(chan *service.QueryResponse, 1)

	// Call service in goroutine
	go func() {
		err := s.service.Query(stream.Context(), serviceReq, responseChan)
		if err != nil {
			s.logger.WithError(err).Error("Query operation failed")
		}
	}()

	// Stream results to client
	for serviceResp := range responseChan {
		// Convert service entities to proto entities
		protoEntities := make([]*pb.Entity, 0, len(serviceResp.Entities))
		for _, entity := range serviceResp.Entities {
			protoEntities = append(protoEntities, &pb.Entity{
				EntityType: entity.EntityType,
				Id:         entity.ID,
				Data:       entity.Data,
				Version:    entity.Version,
				Timestamp:  entity.Timestamp,
			})
		}

		// Send batch to client
		resp := &pb.QueryResponse{
			Entities:      protoEntities,
			TotalCount:    serviceResp.TotalCount,
			NextPageToken: serviceResp.NextPageToken,
		}

		if err := stream.Send(resp); err != nil {
			s.metrics.requestsTotal.With("Query", "error").Inc()
			return toServiceError(&errors.QBError{
				Code: errors.ErrInternal,
				Op:   "grpc.Query",
				Err:  err,
			})
		}
	}

	s.metrics.requestsTotal.With("Query", "success").Inc()
	return nil
}

// ScanIndex performs a full index scan (streaming).
func (s *Server) ScanIndex(req *pb.ScanRequest, stream pb.QuicBaaS_ScanIndexServer) error {
	start := time.Now()
	s.metrics.activeStreams.Inc()
	defer func() {
		s.metrics.activeStreams.Dec()
		s.metrics.requestDuration.With("ScanIndex").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := validateScanRequest(req); err != nil {
		s.metrics.requestsTotal.With("ScanIndex", "invalid").Inc()
		s.logger.WithError(err).Warn("Invalid ScanIndex request")
		return toProtoError(err)
	}

	s.logger.WithFields(map[string]interface{}{
		"entity_type": req.EntityType,
		"index_name":  req.IndexName,
	}).Debug("Processing ScanIndex request")

	// Convert proto request to service request
	serviceReq := &service.ScanRequest{
		EntityType: req.EntityType,
		IndexName:  req.IndexName,
		// Proto ScanRequest doesnt have StartKey/EndKey/Limit - use defaults
		// EndKey not in proto
		Limit: 100, // Default limit
	}

	// Create channel for streaming results
	responseChan := make(chan *service.ScanResponse, 1)

	// Call service in goroutine
	go func() {
		err := s.service.ScanIndex(stream.Context(), serviceReq, responseChan)
		if err != nil {
			s.logger.WithError(err).Error("ScanIndex operation failed")
		}
	}()

	// Stream results to client
	for serviceResp := range responseChan {
		// Convert service entities to proto entities
		protoEntities := make([]*pb.Entity, 0, len(serviceResp.Entities))
		for _, entity := range serviceResp.Entities {
			protoEntities = append(protoEntities, &pb.Entity{
				EntityType: entity.EntityType,
				Id:         entity.ID,
				Data:       entity.Data,
				Version:    entity.Version,
				Timestamp:  entity.Timestamp,
			})
		}

		// Send batch to client
		resp := &pb.ScanResponse{
			Entities:      protoEntities,
			TotalCount:    serviceResp.TotalCount,
			NextPageToken: serviceResp.NextPageToken,
		}

		if err := stream.Send(resp); err != nil {
			s.metrics.requestsTotal.With("ScanIndex", "error").Inc()
			return toServiceError(&errors.QBError{
				Code: errors.ErrInternal,
				Op:   "grpc.ScanIndex",
				Err:  err,
			})
		}
	}

	s.metrics.requestsTotal.With("ScanIndex", "success").Inc()
	return nil
}

// ClusterStatus returns the current state of all cluster nodes.
func (s *Server) ClusterStatus(ctx context.Context, req *pb.ClusterStatusRequest) (*pb.ClusterStatusResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("ClusterStatus").Observe(time.Since(start).Seconds())
	}()

	s.logger.Debug("Processing ClusterStatus request")

	// Convert proto request to service request
	serviceReq := &service.ClusterStatusRequest{
		IncludeDetails: req.IncludeMetrics, // Proto has include_metrics, service expects IncludeDetails
	}

	// Call service
	serviceResp, err := s.service.ClusterStatus(ctx, serviceReq)
	if err != nil {
		s.metrics.requestsTotal.With("ClusterStatus", "error").Inc()
		s.logger.WithError(err).Error("Failed to get cluster status")
		return nil, toServiceError(err)
	}

	// Convert service response to proto response
	protoNodes := make([]*pb.NodeInfo, 0, len(serviceResp.Nodes))
	for _, node := range serviceResp.Nodes {
		protoNodes = append(protoNodes, &pb.NodeInfo{
			NodeId: node.NodeID,
			// Address:       node.Address, // Not in service.NodeInfo
			IsLeader:      node.IsLeader,
			HealthStatus:  serviceHealthToProto(node.HealthStatus),
			Version:       node.Version,
			UptimeSeconds: node.UptimeSeconds,
		})
	}

	protoShards := make([]*pb.ShardInfo, 0, len(serviceResp.Shards))
	for _, shard := range serviceResp.Shards {
		protoShards = append(protoShards, &pb.ShardInfo{
			ShardId:  shard.ShardID,
			LeaderId: shard.LeaderID,
			Replicas: shard.Replicas,
			Status:   serviceHealthToProto(shard.Status),
			// DataSize not in proto ShardInfo
		})
	}

	resp := &pb.ClusterStatusResponse{
		ClusterId: serviceResp.ClusterID,
		Nodes:     protoNodes,
		Shards:    protoShards,
		Timestamp: serviceResp.Timestamp,
	}

	s.metrics.requestsTotal.With("ClusterStatus", "success").Inc()
	return resp, nil
}

// GetLeader returns the current leader for a specific shard.
func (s *Server) GetLeader(ctx context.Context, req *pb.GetLeaderRequest) (*pb.GetLeaderResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("GetLeader").Observe(time.Since(start).Seconds())
	}()

	s.logger.WithField("shard_id", req.ShardId).Debug("Processing GetLeader request")

	// Convert proto request to service request
	serviceReq := &service.GetLeaderRequest{
		ShardID: req.ShardId,
	}

	// Call service
	serviceResp, err := s.service.GetLeader(ctx, serviceReq)
	if err != nil {
		s.metrics.requestsTotal.With("GetLeader", "error").Inc()
		s.logger.WithError(err).Error("Failed to get leader")
		return nil, toServiceError(err)
	}

	resp := &pb.GetLeaderResponse{
		ShardId:  serviceResp.ShardID,
		LeaderId: serviceResp.LeaderID,
		// Address field from service (proto has leader_address)
	}

	s.metrics.requestsTotal.With("GetLeader", "success").Inc()
	return resp, nil
}

// Aggregate performs time-series aggregation.
func (s *Server) Aggregate(ctx context.Context, req *pb.AggregateRequest) (*pb.AggregateResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("Aggregate").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := validateAggregateRequest(req); err != nil {
		s.metrics.requestsTotal.With("Aggregate", "invalid").Inc()
		s.logger.WithError(err).Warn("Invalid Aggregate request")
		return nil, toProtoError(err)
	}

	s.logger.WithFields(map[string]interface{}{
		"entity_type": req.EntityType,
		"field_name":  req.FieldName,
		"function":    req.Function.String(),
	}).Debug("Processing Aggregate request")

	// Convert proto request to service request
	serviceReq := &service.AggregateRequest{
		EntityType: req.EntityType,
		FieldName:  req.FieldName,
		Function:   protoToServiceAggregateFunction(req.Function),
		StartTime:  req.StartTime,
		EndTime:    req.EndTime,
		// Interval not in proto AggregateRequest
	}

	// Call service
	serviceResp, err := s.service.Aggregate(ctx, serviceReq)
	if err != nil {
		s.metrics.requestsTotal.With("Aggregate", "error").Inc()
		s.logger.WithError(err).Error("Failed to aggregate")
		return nil, toServiceError(err)
	}

	resp := &pb.AggregateResponse{
		Result: serviceResp.Result,
		Count:  serviceResp.Count,
		// StartTime not in proto AggregateResponse
		// EndTime not in proto AggregateResponse
	}

	s.metrics.requestsTotal.With("Aggregate", "success").Inc()
	return resp, nil
}

// TimeSeriesQuery retrieves time-series data within a time range (streaming).
func (s *Server) TimeSeriesQuery(req *pb.TimeSeriesQueryRequest, stream pb.QuicBaaS_TimeSeriesQueryServer) error {
	start := time.Now()
	s.metrics.activeStreams.Inc()
	defer func() {
		s.metrics.activeStreams.Dec()
		s.metrics.requestDuration.With("TimeSeriesQuery").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := validateTimeSeriesQueryRequest(req); err != nil {
		s.metrics.requestsTotal.With("TimeSeriesQuery", "invalid").Inc()
		s.logger.WithError(err).Warn("Invalid TimeSeriesQuery request")
		return toProtoError(err)
	}

	s.logger.WithFields(map[string]interface{}{
		"entity_type": req.EntityType,
		"start_time":  req.StartTime,
		"end_time":    req.EndTime,
	}).Debug("Processing TimeSeriesQuery request")

	// Convert proto request to service request
	serviceReq := &service.TimeSeriesQueryRequest{
		EntityType: req.EntityType,
		StartTime:  req.StartTime,
		EndTime:    req.EndTime,
		// Interval not in proto AggregateRequest
		Limit: 100, // Default limit
	}

	// Create channel for streaming results
	responseChan := make(chan *service.TimeSeriesQueryResponse, 1)

	// Call service in goroutine
	go func() {
		err := s.service.TimeSeriesQuery(stream.Context(), serviceReq, responseChan)
		if err != nil {
			s.logger.WithError(err).Error("TimeSeriesQuery operation failed")
		}
	}()

	// Stream results to client
	for serviceResp := range responseChan {
		// Convert service data points to proto data points
		protoPoints := make([]*pb.TimeSeriesPoint, 0, len(serviceResp.DataPoints))
		for _, point := range serviceResp.DataPoints {
			protoPoints = append(protoPoints, &pb.TimeSeriesPoint{
				Timestamp: point.Timestamp,
				Id:        point.ID,
				Data:      point.Data,
				Metadata:  point.Metadata,
			})
		}

		// Send batch to client
		resp := &pb.TimeSeriesQueryResponse{
			DataPoints:    protoPoints,
			TotalCount:    serviceResp.TotalCount,
			NextPageToken: serviceResp.NextPageToken,
		}

		if err := stream.Send(resp); err != nil {
			s.metrics.requestsTotal.With("TimeSeriesQuery", "error").Inc()
			return toServiceError(&errors.QBError{
				Code: errors.ErrInternal,
				Op:   "grpc.TimeSeriesQuery",
				Err:  err,
			})
		}
	}

	s.metrics.requestsTotal.With("TimeSeriesQuery", "success").Inc()
	return nil
}

// BatchPut performs multiple Put operations atomically.
func (s *Server) BatchPut(ctx context.Context, req *pb.BatchPutRequest) (*pb.BatchPutResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("BatchPut").Observe(time.Since(start).Seconds())
	}()

	// Track batch size
	s.metrics.batchSizeHist.With("put").Observe(float64(len(req.Requests)))

	s.logger.WithField("batch_size", len(req.Requests)).Debug("Processing atomic BatchPut request")

	if len(req.Requests) == 0 {
		s.metrics.requestsTotal.With("BatchPut", "invalid").Inc()
		return nil, status.Error(codes.InvalidArgument, "batch cannot be empty")
	}

	// Convert proto requests to service requests
	servicePutReqs := make([]*service.PutRequest, 0, len(req.Requests))
	for _, putReq := range req.Requests {
		servicePutReqs = append(servicePutReqs, &service.PutRequest{
			EntityType:  putReq.EntityType,
			ID:          putReq.Id,
			Data:        putReq.Data,
			Consistency: protoToServiceConsistency(putReq.Consistency),
			WriteMode:   extractWriteMode(ctx),
		})
	}

	// Create service batch request
	serviceBatchReq := &service.BatchPutRequest{
		Requests: servicePutReqs,
	}

	// Call service
	serviceBatchResp, err := s.service.BatchPut(ctx, serviceBatchReq)
	if err != nil {
		s.metrics.requestsTotal.With("BatchPut", "error").Inc()
		s.logger.WithError(err).Error("Batch put failed")
		return nil, toServiceError(err)
	}

	// Convert service responses to proto responses
	protoResponses := make([]*pb.PutResponse, 0, len(serviceBatchResp.Responses))
	for _, serviceResp := range serviceBatchResp.Responses {
		protoResponses = append(protoResponses, &pb.PutResponse{
			Id:        serviceResp.ID,
			Version:   serviceResp.Version,
			Timestamp: serviceResp.Timestamp,
			LeaderId:  serviceResp.LeaderID,
		})
	}

	resp := &pb.BatchPutResponse{
		Responses: protoResponses,
		Timestamp: serviceBatchResp.Timestamp,
	}

	s.logger.WithFields(map[string]interface{}{
		"batch_size": len(req.Requests),
	}).Debug("Atomic batch put completed")

	s.metrics.requestsTotal.With("BatchPut", "success").Inc()
	return resp, nil
}

// BatchGet retrieves multiple values in a single request.
func (s *Server) BatchGet(ctx context.Context, req *pb.BatchGetRequest) (*pb.BatchGetResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("BatchGet").Observe(time.Since(start).Seconds())
	}()

	// Track batch size
	s.metrics.batchSizeHist.With("get").Observe(float64(len(req.Requests)))

	s.logger.WithField("batch_size", len(req.Requests)).Debug("Processing BatchGet request")

	if len(req.Requests) == 0 {
		s.metrics.requestsTotal.With("BatchGet", "invalid").Inc()
		return nil, status.Error(codes.InvalidArgument, "batch cannot be empty")
	}

	// Convert proto requests to service requests
	serviceGetReqs := make([]*service.GetRequest, 0, len(req.Requests))
	for _, getReq := range req.Requests {
		serviceGetReqs = append(serviceGetReqs, &service.GetRequest{
			EntityType:  getReq.EntityType,
			ID:          getReq.Id,
			Consistency: protoToServiceConsistency(getReq.Consistency),
			TimeoutMs:   5000, // Default timeout
		})
	}

	// Create service batch request
	serviceBatchReq := &service.BatchGetRequest{
		Requests: serviceGetReqs,
	}

	// Call service
	serviceBatchResp, err := s.service.BatchGet(ctx, serviceBatchReq)
	if err != nil {
		s.metrics.requestsTotal.With("BatchGet", "error").Inc()
		return nil, toServiceError(err)
	}

	// Convert service responses to proto responses
	protoResponses := make([]*pb.GetResponse, 0, len(serviceBatchResp.Responses))
	for _, serviceResp := range serviceBatchResp.Responses {
		protoResponses = append(protoResponses, &pb.GetResponse{
			Id:        serviceResp.ID,
			Data:      serviceResp.Data,
			Found:     serviceResp.Found,
			Version:   serviceResp.Version,
			Timestamp: serviceResp.Timestamp,
		})
	}

	resp := &pb.BatchGetResponse{
		Responses: protoResponses,
	}

	s.metrics.requestsTotal.With("BatchGet", "success").Inc()
	return resp, nil
}

// BatchDelete removes multiple keys atomically.
func (s *Server) BatchDelete(ctx context.Context, req *pb.BatchDeleteRequest) (*pb.BatchDeleteResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("BatchDelete").Observe(time.Since(start).Seconds())
	}()

	// Track batch size
	s.metrics.batchSizeHist.With("delete").Observe(float64(len(req.Requests)))

	s.logger.WithField("batch_size", len(req.Requests)).Debug("Processing BatchDelete request")

	if len(req.Requests) == 0 {
		s.metrics.requestsTotal.With("BatchDelete", "invalid").Inc()
		return nil, status.Error(codes.InvalidArgument, "batch cannot be empty")
	}

	// Convert proto requests to service requests
	serviceDeleteReqs := make([]*service.DeleteRequest, 0, len(req.Requests))
	for _, delReq := range req.Requests {
		serviceDeleteReqs = append(serviceDeleteReqs, &service.DeleteRequest{
			EntityType:  delReq.EntityType,
			ID:          delReq.Id,
			Consistency: protoToServiceConsistency(delReq.Consistency),
		})
	}

	// Create service batch request
	serviceBatchReq := &service.BatchDeleteRequest{
		Requests: serviceDeleteReqs,
	}

	// Call service
	serviceBatchResp, err := s.service.BatchDelete(ctx, serviceBatchReq)
	if err != nil {
		s.metrics.requestsTotal.With("BatchDelete", "error").Inc()
		s.logger.WithError(err).Error("Batch delete failed")
		return nil, toServiceError(err)
	}

	// Convert service responses to proto responses
	protoResponses := make([]*pb.DeleteResponse, 0, len(serviceBatchResp.Responses))
	for _, serviceResp := range serviceBatchResp.Responses {
		protoResponses = append(protoResponses, &pb.DeleteResponse{
			Deleted:   serviceResp.Deleted,
			Timestamp: serviceResp.Timestamp,
		})
	}

	resp := &pb.BatchDeleteResponse{
		Responses: protoResponses,
		Timestamp: serviceBatchResp.Timestamp,
	}

	s.logger.WithFields(map[string]interface{}{
		"batch_size": len(req.Requests),
	}).Debug("Atomic batch delete completed")

	s.metrics.requestsTotal.With("BatchDelete", "success").Inc()
	return resp, nil
}

// Transaction performs an atomic compare-and-swap transaction.
func (s *Server) Transaction(ctx context.Context, req *pb.TransactionRequest) (*pb.TransactionResponse, error) {
	ctx, endSpan := startGRPCSpan(ctx, "grpc.Transaction", map[string]interface{}{
		"compare_count": len(req.Compare),
		"success_count": len(req.Success),
		"failure_count": len(req.Failure),
	})
	defer endSpan()

	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("Transaction").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if err := validateTransactionRequest(req); err != nil {
		s.metrics.requestsTotal.With("Transaction", "invalid").Inc()
		s.logger.WithError(err).Warn("Invalid Transaction request")
		return nil, toProtoError(err)
	}

	s.logger.WithFields(map[string]interface{}{
		"compare_count": len(req.Compare),
		"success_count": len(req.Success),
		"failure_count": len(req.Failure),
	}).Debug("Processing Transaction request")

	// Convert proto compare conditions to service compare conditions
	serviceCompare := make([]*service.CompareCondition, 0, len(req.Compare))
	for _, cmp := range req.Compare {
		serviceCompare = append(serviceCompare, &service.CompareCondition{
			EntityType: cmp.EntityType,
			ID:         cmp.Id,
			Target:     protoToServiceCompareTarget(cmp.Target),
			Operator:   protoToServiceCompareOperator(cmp.Operator),
			Version:    cmp.Version,
			Value:      cmp.Value,
		})
	}

	// Convert proto success operations to service operations
	serviceSuccess := make([]*service.TransactionOp, 0, len(req.Success))
	for _, op := range req.Success {
		serviceSuccess = append(serviceSuccess, &service.TransactionOp{
			OpType:     protoToServiceTransactionOpType(op.OpType),
			EntityType: op.EntityType,
			ID:         op.Id,
			Data:       op.Data,
		})
	}

	// Convert proto failure operations to service operations
	serviceFailure := make([]*service.TransactionOp, 0, len(req.Failure))
	for _, op := range req.Failure {
		serviceFailure = append(serviceFailure, &service.TransactionOp{
			OpType:     protoToServiceTransactionOpType(op.OpType),
			EntityType: op.EntityType,
			ID:         op.Id,
			Data:       op.Data,
		})
	}

	// Create service transaction request
	serviceReq := &service.TransactionRequest{
		Compare: serviceCompare,
		Success: serviceSuccess,
		Failure: serviceFailure,
	}

	// Call service
	serviceResp, err := s.service.Transaction(ctx, serviceReq)
	if err != nil {
		s.metrics.requestsTotal.With("Transaction", "error").Inc()
		s.logger.WithError(err).Error("Failed to execute transaction")
		return nil, toServiceError(err)
	}

	// Convert service responses to proto responses
	protoResponses := make([]*pb.TransactionOpResponse, 0, len(serviceResp.Responses))
	for _, serviceOpResp := range serviceResp.Responses {
		protoResponses = append(protoResponses, &pb.TransactionOpResponse{
			OpType:     serviceTransactionOpTypeToProto(serviceOpResp.OpType),
			EntityType: serviceOpResp.EntityType,
			Id:         serviceOpResp.ID,
			Data:       serviceOpResp.Data,
			Success:    serviceOpResp.Success,
			Version:    serviceOpResp.Version,
			Timestamp:  serviceOpResp.Timestamp,
			Error:      serviceOpResp.Error,
		})
	}

	// Build response
	resp := &pb.TransactionResponse{
		Succeeded: serviceResp.Succeeded,
		Responses: protoResponses,
		Timestamp: serviceResp.Timestamp,
		Message:   serviceResp.Message,
		Revision:  serviceResp.Revision,
	}

	s.metrics.requestsTotal.With("Transaction", "success").Inc()
	return resp, nil
}

// Health returns the health status of the current node.
func (s *Server) Health(ctx context.Context, req *pb.HealthRequest) (*pb.HealthResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("Health").Observe(time.Since(start).Seconds())
	}()

	s.logger.Debug("Processing Health request")

	// Convert proto request to service request
	serviceReq := &service.HealthRequest{
		IncludeDetails: req.IncludeDetails, // Both proto and service use IncludeDetails
	}

	// Call service
	serviceResp, err := s.service.Health(ctx, serviceReq)
	if err != nil {
		s.metrics.requestsTotal.With("Health", "error").Inc()
		return nil, toServiceError(err)
	}

	// Convert service response to proto response
	resp := &pb.HealthResponse{
		Status:        serviceHealthToProto(serviceResp.Status),
		NodeId:        serviceResp.NodeID,
		Version:       serviceResp.Version,
		UptimeSeconds: serviceResp.UptimeSeconds,
	}

	if req.IncludeDetails {
		resp.Details = make(map[string]*pb.ComponentHealth)
		for component, health := range serviceResp.Details {
			resp.Details[component] = &pb.ComponentHealth{
				Status:    serviceHealthToProto(health.Status),
				Message:   health.Message,
				LastCheck: health.LastCheck,
			}
		}
	}

	s.metrics.requestsTotal.With("Health", "success").Inc()
	return resp, nil
}

// =============================================================================
// Backup Operations
// =============================================================================

// CreateBackup creates a new backup of the database.
func (s *Server) CreateBackup(ctx context.Context, req *pb.CreateBackupRequest) (*pb.CreateBackupResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("CreateBackup").Observe(time.Since(start).Seconds())
	}()

	s.logger.WithFields(map[string]interface{}{
		"backup_type": req.BackupType.String(),
		"compression": req.Compression.String(),
		"encryption":  req.Encryption.String(),
	}).Debug("Processing CreateBackup request")

	// Convert proto backup type to service name
	backupName := protoBackupTypeToServiceName(req.BackupType)

	// Convert proto request to service request
	serviceReq := &service.CreateBackupRequest{
		Name:        backupName,
		Description: "",
		Compression: protoCompressionTypeToString(req.Compression),
		Encryption:  protoEncryptionTypeToString(req.Encryption),
	}

	// Call service
	serviceResp, err := s.service.CreateBackup(ctx, serviceReq)
	if err != nil {
		s.metrics.requestsTotal.With("CreateBackup", "error").Inc()
		s.logger.WithError(err).Error("Failed to create backup")
		return nil, toServiceError(err)
	}

	// Build proto response
	resp := &pb.CreateBackupResponse{
		BackupId:  serviceResp.BackupID,
		Timestamp: serviceResp.Timestamp,
		Size:      int64(serviceResp.Size), // #nosec G115 -- Size is always positive
	}

	s.metrics.requestsTotal.With("CreateBackup", "success").Inc()
	return resp, nil
}

// ListBackups returns a list of available backups.
func (s *Server) ListBackups(ctx context.Context, req *pb.ListBackupsRequest) (*pb.ListBackupsResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("ListBackups").Observe(time.Since(start).Seconds())
	}()

	s.logger.WithFields(map[string]interface{}{
		"limit": req.Limit,
	}).Debug("Processing ListBackups request")

	// Convert proto request to service request
	serviceReq := &service.ListBackupsRequest{
		Limit: uint64(req.Limit), // #nosec G115 -- Limit is always non-negative
	}

	// Call service
	serviceResp, err := s.service.ListBackups(ctx, serviceReq)
	if err != nil {
		s.metrics.requestsTotal.With("ListBackups", "error").Inc()
		s.logger.WithError(err).Error("Failed to list backups")
		return nil, toServiceError(err)
	}

	// Convert service backups to proto backups
	protoBackups := make([]*pb.BackupInfo, 0, len(serviceResp.Backups))
	for _, backup := range serviceResp.Backups {
		protoBackups = append(protoBackups, &pb.BackupInfo{
			BackupId:   backup.BackupID,
			BackupType: serviceNameToProtoBackupType(backup.Name),
			Timestamp:  backup.Timestamp,
			Size:       int64(backup.Size),        // #nosec G115 -- Size is always positive
			NumShards:  int32(len(backup.Shards)), // #nosec G115 -- slice length is always bounded by practical limits
		})
	}

	// Build proto response
	resp := &pb.ListBackupsResponse{
		Backups:   protoBackups,
		NextToken: "", // Pagination not yet implemented in service
	}

	s.metrics.requestsTotal.With("ListBackups", "success").Inc()
	return resp, nil
}

// RestoreBackup restores the database from a backup.
func (s *Server) RestoreBackup(ctx context.Context, req *pb.RestoreBackupRequest) (*pb.RestoreBackupResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("RestoreBackup").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if req.BackupId == "" {
		s.metrics.requestsTotal.With("RestoreBackup", "invalid").Inc()
		s.logger.Warn("Invalid RestoreBackup request: backup_id is required")
		return nil, status.Error(codes.InvalidArgument, "backup_id is required")
	}

	s.logger.WithFields(map[string]interface{}{
		"backup_id":       req.BackupId,
		"verify_checksum": req.VerifyChecksum,
		"skip_existing":   req.SkipExisting,
		"target_shards":   req.TargetShards,
	}).Debug("Processing RestoreBackup request")

	// Convert proto request to service request
	serviceReq := &service.RestoreBackupRequest{
		BackupID:     req.BackupId,
		TargetShards: req.TargetShards,
	}

	// Call service
	serviceResp, err := s.service.RestoreBackup(ctx, serviceReq)
	if err != nil {
		s.metrics.requestsTotal.With("RestoreBackup", "error").Inc()
		s.logger.WithError(err).Error("Failed to restore backup")
		return nil, toServiceError(err)
	}

	// Build proto response
	resp := &pb.RestoreBackupResponse{
		BackupId:  req.BackupId,
		Timestamp: time.Now().UnixNano(),
	}

	// Map service status to duration (best effort)
	if serviceResp.Status == "completed" {
		resp.Duration = 0 // Immediate completion
	}

	s.metrics.requestsTotal.With("RestoreBackup", "success").Inc()
	return resp, nil
}

// DeleteBackup deletes a backup.
func (s *Server) DeleteBackup(ctx context.Context, req *pb.DeleteBackupRequest) (*pb.DeleteBackupResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("DeleteBackup").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if req.BackupId == "" {
		s.metrics.requestsTotal.With("DeleteBackup", "invalid").Inc()
		s.logger.Warn("Invalid DeleteBackup request: backup_id is required")
		return nil, status.Error(codes.InvalidArgument, "backup_id is required")
	}

	s.logger.WithField("backup_id", req.BackupId).Debug("Processing DeleteBackup request")

	// Convert proto request to service request
	serviceReq := &service.DeleteBackupRequest{
		BackupID: req.BackupId,
	}

	// Call service
	serviceResp, err := s.service.DeleteBackup(ctx, serviceReq)
	if err != nil {
		s.metrics.requestsTotal.With("DeleteBackup", "error").Inc()
		s.logger.WithError(err).Error("Failed to delete backup")
		return nil, toServiceError(err)
	}

	// Build proto response
	resp := &pb.DeleteBackupResponse{
		Success: serviceResp.Success,
	}

	s.metrics.requestsTotal.With("DeleteBackup", "success").Inc()
	return resp, nil
}

// VerifyBackup verifies the integrity of a backup.
func (s *Server) VerifyBackup(ctx context.Context, req *pb.VerifyBackupRequest) (*pb.VerifyBackupResponse, error) {
	start := time.Now()
	defer func() {
		s.metrics.requestDuration.With("VerifyBackup").Observe(time.Since(start).Seconds())
	}()

	// Validate request
	if req.BackupId == "" {
		s.metrics.requestsTotal.With("VerifyBackup", "invalid").Inc()
		s.logger.Warn("Invalid VerifyBackup request: backup_id is required")
		return nil, status.Error(codes.InvalidArgument, "backup_id is required")
	}

	s.logger.WithField("backup_id", req.BackupId).Debug("Processing VerifyBackup request")

	// Convert proto request to service request
	serviceReq := &service.VerifyBackupRequest{
		BackupID: req.BackupId,
	}

	// Call service
	serviceResp, err := s.service.VerifyBackup(ctx, serviceReq)
	if err != nil {
		s.metrics.requestsTotal.With("VerifyBackup", "error").Inc()
		s.logger.WithError(err).Error("Failed to verify backup")
		return nil, toServiceError(err)
	}

	// Build verification message from issues
	message := "Backup verification passed"
	if !serviceResp.Valid {
		if len(serviceResp.Issues) > 0 {
			message = fmt.Sprintf("Backup verification failed: %v", serviceResp.Issues)
		} else {
			message = "Backup verification failed"
		}
	}

	// Build proto response
	resp := &pb.VerifyBackupResponse{
		Valid:         serviceResp.Valid,
		Message:       message,
		ChecksumValid: serviceResp.ChecksumMatch,
	}

	s.metrics.requestsTotal.With("VerifyBackup", "success").Inc()
	return resp, nil
}

// =============================================================================
// Backup Type Conversion Helpers
// =============================================================================

// protoBackupTypeToServiceName converts proto BackupType to service backup name string.
func protoBackupTypeToServiceName(backupType pb.BackupType) string {
	switch backupType {
	case pb.BackupType_BACKUP_TYPE_FULL:
		return "full"
	case pb.BackupType_BACKUP_TYPE_INCREMENTAL:
		return "incremental"
	case pb.BackupType_BACKUP_TYPE_SNAPSHOT:
		return "snapshot"
	default:
		return "full" // Default to full backup
	}
}

// serviceNameToProtoBackupType converts service backup name to proto BackupType.
func serviceNameToProtoBackupType(name string) pb.BackupType {
	switch name {
	case "full":
		return pb.BackupType_BACKUP_TYPE_FULL
	case "incremental":
		return pb.BackupType_BACKUP_TYPE_INCREMENTAL
	case "snapshot":
		return pb.BackupType_BACKUP_TYPE_SNAPSHOT
	default:
		return pb.BackupType_BACKUP_TYPE_UNSPECIFIED
	}
}

// protoCompressionTypeToString converts proto CompressionType to string.
func protoCompressionTypeToString(compression pb.CompressionType) string {
	switch compression {
	case pb.CompressionType_COMPRESSION_TYPE_NONE:
		return "none"
	case pb.CompressionType_COMPRESSION_TYPE_GZIP:
		return "gzip"
	case pb.CompressionType_COMPRESSION_TYPE_SNAPPY:
		return "snappy"
	case pb.CompressionType_COMPRESSION_TYPE_LZ4:
		return "lz4"
	default:
		return "" // Server default
	}
}

// protoEncryptionTypeToString converts proto EncryptionType to string.
func protoEncryptionTypeToString(encryption pb.EncryptionType) string {
	switch encryption {
	case pb.EncryptionType_ENCRYPTION_TYPE_NONE:
		return "none"
	case pb.EncryptionType_ENCRYPTION_TYPE_AES256:
		return "aes256"
	default:
		return "" // Server default
	}
}
