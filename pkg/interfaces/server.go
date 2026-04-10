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

package interfaces

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
)

// GRPCServer defines the interface for the gRPC API server.
// It implements the QuicBaaS protobuf service definition.
type GRPCServer interface {
	pb.QuicBaaSServer

	// Additional methods beyond the protobuf definition can be added here
	// for server lifecycle management, metrics, etc.
}

// RESTServer defines the interface for the REST API server.
// It provides HTTP endpoints for all QuicBaaS operations.
type RESTServer interface {
	// Lifecycle management

	// Start starts the HTTP server on the specified address.
	// If address is empty, uses the configured default.
	Start(address string) error

	// Shutdown gracefully shuts down the server.
	// All active connections are allowed to complete within the context deadline.
	Shutdown(ctx context.Context) error

	// Close closes the server (alias for Shutdown with background context).
	Close() error

	// Server state

	// IsStarted returns true if the server is started.
	IsStarted() bool

	// IsClosed returns true if the server is closed.
	IsClosed() bool

	// Uptime returns the server uptime duration.
	Uptime() time.Duration

	// GetRouter returns the underlying chi router (for testing).
	GetRouter() *chi.Mux
}

// HTTPHandler defines the interface for HTTP request handlers.
// Implementations handle specific API endpoints.
type HTTPHandler interface {
	// ServeHTTP handles an HTTP request.
	ServeHTTP(w http.ResponseWriter, r *http.Request)
}

// Middleware defines the interface for HTTP middleware.
// Middleware can intercept and modify requests/responses.
type Middleware interface {
	// Handler wraps an HTTP handler with middleware logic.
	Handler(next http.Handler) http.Handler
}
