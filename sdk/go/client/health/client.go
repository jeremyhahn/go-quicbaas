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

package health

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Health"
)

// Client provides health check operations.
type Client interface {
	// Ping performs a simple connectivity check.
	Ping(ctx context.Context) error

	// Liveness checks if the server is running.
	Liveness(ctx context.Context) (*HealthResponse, error)

	// Readiness checks if the server is ready to serve traffic.
	Readiness(ctx context.Context) (*HealthResponse, error)
}

// HealthResponse is the response from a health check.
type HealthResponse struct {
	Healthy bool
	Status  string
	Details map[string]interface{}
}

// pingRequest is the internal ping request.
type pingRequest struct{}

// pingResponse is the internal ping response.
type pingResponse struct{}

// livenessRequest is the internal liveness request.
type livenessRequest struct{}

// readinessRequest is the internal readiness request.
type readinessRequest struct{}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new health client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// Ping performs a simple connectivity check.
func (c *client) Ping(ctx context.Context) error {
	if ctx == nil {
		return errors.NewInvalidInput("health.Ping", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Ping", &pingRequest{})
	if err != nil {
		return err
	}

	// Accept either nil response or valid ping response
	if resp == nil {
		return nil
	}

	if _, ok := resp.(*pingResponse); !ok {
		// Also accept HealthResponse as valid
		if _, ok := resp.(*HealthResponse); !ok {
			return errors.NewInternal("health.Ping", "invalid response type")
		}
	}

	return nil
}

// Liveness checks if the server is running.
func (c *client) Liveness(ctx context.Context) (*HealthResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("health.Liveness", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Liveness", &livenessRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("health.Liveness", "nil response from server")
	}

	healthResp, ok := resp.(*HealthResponse)
	if !ok {
		return nil, errors.NewInternal("health.Liveness", "invalid response type")
	}

	return healthResp, nil
}

// Readiness checks if the server is ready to serve traffic.
func (c *client) Readiness(ctx context.Context) (*HealthResponse, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("health.Readiness", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Readiness", &readinessRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("health.Readiness", "nil response from server")
	}

	healthResp, ok := resp.(*HealthResponse)
	if !ok {
		return nil, errors.NewInternal("health.Readiness", "invalid response type")
	}

	return healthResp, nil
}
