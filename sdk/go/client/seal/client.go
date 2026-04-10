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

package seal

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Seal"
)

// Client provides seal/unseal operations.
type Client interface {
	// Status returns the current seal status.
	Status(ctx context.Context) (*SealStatus, error)

	// Init initializes the seal with Shamir secret sharing.
	Init(ctx context.Context, req *InitRequest) (*InitResponse, error)

	// Unseal provides an unseal key to progress toward unsealing.
	Unseal(ctx context.Context, req *UnsealRequest) (*UnsealResponse, error)

	// Seal seals the server, requiring unseal keys to access data.
	Seal(ctx context.Context) error
}

// SealStatus represents the seal status.
type SealStatus struct {
	Sealed         bool
	Initialized    bool
	Progress       int
	Required       int
	ClusterName    string
	ClusterVersion string
}

// InitRequest is a request to initialize the seal.
type InitRequest struct {
	SecretShares    int
	SecretThreshold int
}

// InitResponse is the response from initialization.
type InitResponse struct {
	Keys      []string
	RootToken string
}

// UnsealRequest is a request to unseal.
type UnsealRequest struct {
	Key   string
	Reset bool
}

// UnsealResponse is the response from unsealing.
type UnsealResponse struct {
	Sealed   bool
	Progress int
	Required int
}

// statusRequest is the internal status request.
type statusRequest struct{}

// sealRequest is the internal seal request.
type sealRequest struct{}

// sealResponse is the internal seal response.
type sealResponse struct{}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new seal client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// Status returns the current seal status.
func (c *client) Status(ctx context.Context) (*SealStatus, error) {
	if ctx == nil {
		return nil, errors.NewInvalidInput("seal.Status", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Status", &statusRequest{})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("seal.Status", "nil response from server")
	}

	status, ok := resp.(*SealStatus)
	if !ok {
		return nil, errors.NewInternal("seal.Status", "invalid response type")
	}

	return status, nil
}

// Init initializes the seal with Shamir secret sharing.
func (c *client) Init(ctx context.Context, req *InitRequest) (*InitResponse, error) {
	if err := validateInitRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("seal.Init", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Init", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("seal.Init", "nil response from server")
	}

	initResp, ok := resp.(*InitResponse)
	if !ok {
		return nil, errors.NewInternal("seal.Init", "invalid response type")
	}

	return initResp, nil
}

// Unseal provides an unseal key to progress toward unsealing.
func (c *client) Unseal(ctx context.Context, req *UnsealRequest) (*UnsealResponse, error) {
	if err := validateUnsealRequest(req); err != nil {
		return nil, err
	}

	if ctx == nil {
		return nil, errors.NewInvalidInput("seal.Unseal", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Unseal", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("seal.Unseal", "nil response from server")
	}

	unsealResp, ok := resp.(*UnsealResponse)
	if !ok {
		return nil, errors.NewInternal("seal.Unseal", "invalid response type")
	}

	return unsealResp, nil
}

// Seal seals the server, requiring unseal keys to access data.
func (c *client) Seal(ctx context.Context) error {
	if ctx == nil {
		return errors.NewInvalidInput("seal.Seal", "context is required")
	}

	resp, err := c.transport.Request(ctx, serviceName, "Seal", &sealRequest{})
	if err != nil {
		return err
	}

	// Accept nil response or valid response
	if resp == nil {
		return nil
	}

	if _, ok := resp.(*sealResponse); !ok {
		return errors.NewInternal("seal.Seal", "invalid response type")
	}

	return nil
}

// Validation functions

func validateInitRequest(req *InitRequest) error {
	if req == nil {
		return errors.NewInvalidInput("seal.Init", "request is required")
	}
	if req.SecretShares < 1 {
		return errors.NewInvalidInput("seal.Init", "secret shares must be at least 1")
	}
	if req.SecretThreshold < 1 {
		return errors.NewInvalidInput("seal.Init", "secret threshold must be at least 1")
	}
	if req.SecretThreshold > req.SecretShares {
		return errors.NewInvalidInput("seal.Init", "secret threshold cannot exceed secret shares")
	}
	return nil
}

func validateUnsealRequest(req *UnsealRequest) error {
	if req == nil {
		return errors.NewInvalidInput("seal.Unseal", "request is required")
	}
	// Allow empty key only if Reset is true
	if req.Key == "" && !req.Reset {
		return errors.NewInvalidInput("seal.Unseal", "key is required unless resetting")
	}
	return nil
}
