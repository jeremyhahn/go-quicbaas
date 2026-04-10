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

package server

import (
	"fmt"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
)

// HealthStatus represents the overall health state.
type HealthStatus string

const (
	// HealthStatusHealthy indicates all components are healthy.
	HealthStatusHealthy HealthStatus = "healthy"

	// HealthStatusDegraded indicates some components are unhealthy but service is operational.
	HealthStatusDegraded HealthStatus = "degraded"

	// HealthStatusUnhealthy indicates critical components are unhealthy.
	HealthStatusUnhealthy HealthStatus = "unhealthy"

	// HealthStatusUnknown indicates health status cannot be determined.
	HealthStatusUnknown HealthStatus = "unknown"
)

// ComponentHealth represents the health of a single component.
type ComponentHealth struct {
	Name      string       `json:"name"`
	Status    HealthStatus `json:"status"`
	Message   string       `json:"message,omitempty"`
	LastCheck time.Time    `json:"last_check"`
	Error     string       `json:"error,omitempty"`
}

// OverallHealth represents the complete health status.
type OverallHealth struct {
	Status     HealthStatus               `json:"status"`
	Timestamp  time.Time                  `json:"timestamp"`
	Uptime     time.Duration              `json:"uptime"`
	Components map[string]ComponentHealth `json:"components"`
}

// HealthChecker performs health checks on all server components.
type HealthChecker struct {
	server *Server
	logger *logger.Logger
}

// NewHealthChecker creates a new health checker.
func NewHealthChecker(server *Server, log *logger.Logger) *HealthChecker {
	return &HealthChecker{
		server: server,
		logger: log.WithField("component", "health-checker"),
	}
}

// CheckGRPC checks gRPC server health.
func (h *HealthChecker) CheckGRPC() ComponentHealth {
	health := ComponentHealth{
		Name:      "grpc",
		Status:    HealthStatusHealthy,
		LastCheck: time.Now(),
	}

	if h.server.grpcServer == nil {
		health.Status = HealthStatusUnhealthy
		health.Message = "gRPC server not initialized"
		return health
	}

	if h.server.grpcListener == nil {
		health.Status = HealthStatusUnhealthy
		health.Message = "gRPC listener not available"
		return health
	}

	health.Message = fmt.Sprintf("gRPC server listening on %s", h.server.grpcListener.Addr())
	return health
}

// CheckREST checks REST server health.
func (h *HealthChecker) CheckREST() ComponentHealth {
	health := ComponentHealth{
		Name:      "rest",
		Status:    HealthStatusHealthy,
		LastCheck: time.Now(),
	}

	if h.server.restServer == nil {
		health.Status = HealthStatusUnhealthy
		health.Message = "REST server not initialized"
		return health
	}

	if !h.server.restServer.IsStarted() {
		health.Status = HealthStatusUnhealthy
		health.Message = "REST server not started"
		return health
	}

	if h.server.restServer.IsClosed() {
		health.Status = HealthStatusUnhealthy
		health.Message = "REST server is closed"
		return health
	}

	health.Message = "REST server operational"
	return health
}

// GetOverallHealth returns the complete health status.
func (h *HealthChecker) GetOverallHealth() *OverallHealth {
	components := make(map[string]ComponentHealth)

	// Check all components
	components["grpc"] = h.CheckGRPC()
	components["rest"] = h.CheckREST()

	// Determine overall status
	overallStatus := HealthStatusHealthy
	unhealthyCount := 0
	degradedCount := 0

	for _, comp := range components {
		switch comp.Status {
		case HealthStatusUnhealthy:
			unhealthyCount++
		case HealthStatusDegraded:
			degradedCount++
		}
	}

	if unhealthyCount > 0 {
		if unhealthyCount >= 2 {
			// Multiple critical components unhealthy
			overallStatus = HealthStatusUnhealthy
		} else {
			// One component unhealthy
			overallStatus = HealthStatusDegraded
		}
	} else if degradedCount > 0 {
		overallStatus = HealthStatusDegraded
	}

	return &OverallHealth{
		Status:     overallStatus,
		Timestamp:  time.Now(),
		Uptime:     h.server.Uptime(),
		Components: components,
	}
}

// IsHealthy returns true if all components are healthy.
func (h *HealthChecker) IsHealthy() bool {
	health := h.GetOverallHealth()
	return health.Status == HealthStatusHealthy
}

// IsReady returns true if the server is ready to serve requests.
func (h *HealthChecker) IsReady() bool {
	// Check critical components for readiness
	restHealth := h.CheckREST()
	grpcHealth := h.CheckGRPC()

	return restHealth.Status != HealthStatusUnhealthy &&
		grpcHealth.Status != HealthStatusUnhealthy
}

// GetHealthChecker returns the server's health checker.
func (s *Server) GetHealthChecker() *HealthChecker {
	return s.health
}
