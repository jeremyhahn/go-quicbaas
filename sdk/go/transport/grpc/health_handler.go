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
	"strings"

	"google.golang.org/grpc"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/health"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
)

// Health service prefix for routing.
const healthServicePrefix = "quicbaas.Health"

// isHealthService checks if the service name is a Health service.
func isHealthService(svc string) bool {
	return strings.HasPrefix(svc, healthServicePrefix)
}

// healthHandler handles Health service requests by converting between SDK types and protobuf types.
type healthHandler struct {
	conn *grpc.ClientConn
}

// newHealthHandler creates a new health handler.
func newHealthHandler(conn *grpc.ClientConn) *healthHandler {
	return &healthHandler{conn: conn}
}

// handleRequest routes Health requests to the appropriate handler based on method name.
func (h *healthHandler) handleRequest(ctx context.Context, service, method string, req interface{}) (interface{}, error) {
	client := pb.NewQuicBaaSClient(h.conn)

	// Route based on method name.
	switch method {
	case "Ping":
		return h.handlePing(ctx, client)
	case "Liveness":
		return h.handleLiveness(ctx, client)
	case "Readiness":
		return h.handleReadiness(ctx, client)
	case "Health":
		return h.handleHealth(ctx, client, req)
	default:
		return nil, errors.NewUnimplemented("healthHandler.handleRequest",
			"unsupported method: "+method)
	}
}

// handlePing handles Ping requests by calling the Health endpoint with minimal details.
func (h *healthHandler) handlePing(ctx context.Context, client pb.QuicBaaSClient) (interface{}, error) {
	// Use Health endpoint for ping - just check if we get a response
	pbReq := &pb.HealthRequest{
		IncludeDetails: false,
	}

	pbResp, err := client.Health(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "healthHandler.handlePing")
	}

	// Convert to SDK response
	return &health.HealthResponse{
		Healthy: pbResp.Status == pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		Status:  pbResp.Status.String(),
	}, nil
}

// handleLiveness handles Liveness requests.
func (h *healthHandler) handleLiveness(ctx context.Context, client pb.QuicBaaSClient) (interface{}, error) {
	// Use Health endpoint for liveness check
	pbReq := &pb.HealthRequest{
		IncludeDetails: false,
	}

	pbResp, err := client.Health(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "healthHandler.handleLiveness")
	}

	// Convert to SDK response
	return &health.HealthResponse{
		Healthy: pbResp.Status == pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		Status:  pbResp.Status.String(),
	}, nil
}

// handleReadiness handles Readiness requests.
func (h *healthHandler) handleReadiness(ctx context.Context, client pb.QuicBaaSClient) (interface{}, error) {
	// Use Health endpoint with details for readiness check
	pbReq := &pb.HealthRequest{
		IncludeDetails: true,
	}

	pbResp, err := client.Health(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "healthHandler.handleReadiness")
	}

	// Convert details to SDK format
	details := make(map[string]interface{})
	for name, compHealth := range pbResp.Details {
		details[name] = map[string]interface{}{
			"status":  compHealth.Status.String(),
			"message": compHealth.Message,
		}
	}

	return &health.HealthResponse{
		Healthy: pbResp.Status == pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		Status:  pbResp.Status.String(),
		Details: details,
	}, nil
}

// handleHealth handles Health requests with optional details.
func (h *healthHandler) handleHealth(ctx context.Context, client pb.QuicBaaSClient, req interface{}) (interface{}, error) {
	// Default to no details if req is nil or wrong type
	includeDetails := false
	if healthReq, ok := req.(*health.HealthResponse); ok && healthReq != nil {
		// If the request has details, include them
		includeDetails = healthReq.Details != nil
	}

	pbReq := &pb.HealthRequest{
		IncludeDetails: includeDetails,
	}

	pbResp, err := client.Health(ctx, pbReq)
	if err != nil {
		return nil, convertGRPCError(err, "healthHandler.handleHealth")
	}

	// Convert details to SDK format
	var details map[string]interface{}
	if pbResp.Details != nil {
		details = make(map[string]interface{})
		for name, compHealth := range pbResp.Details {
			details[name] = map[string]interface{}{
				"status":  compHealth.Status.String(),
				"message": compHealth.Message,
			}
		}
	}

	return &health.HealthResponse{
		Healthy: pbResp.Status == pb.HealthStatus_HEALTH_STATUS_HEALTHY,
		Status:  pbResp.Status.String(),
		Details: details,
	}, nil
}
