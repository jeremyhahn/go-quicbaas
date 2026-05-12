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

	"github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// Deprecated seal-CLI client adapter. The seal/* CLI surface and its
// gRPC RPCs (SealStatus, SealInit, Unseal, Seal, SealRekey) are
// retained for one release with deprecation aliases — see
// docs/migrations/seal-to-barrier.md. New code SHOULD use the
// /v1/barrier/* surface and the BarrierClient SDK adapter
// (pkg/barrier).
//
// These methods are thin wrappers over the existing proto RPCs that
// translate the proto wire shape into the service-layer DTOs the CLI
// SealClient interface (cmd/quicbaas/factories.go) expects. They
// remain in place until Phase 12 of the seal→barrier rename
// (cmd/quicbaas/seal.go split into barrier/init/bootstrap) lands and
// the SealClient interface is retired in favour of BarrierClient.

// SealStatus returns the current seal status of the database.
func (c *Client) SealStatus(ctx context.Context) (*service.SealStatusResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}
	ctx = c.addMetadata(ctx)
	pc, err := c.getClient()
	if err != nil {
		return nil, convertGRPCError(err, "SealStatus")
	}
	var resp *proto.SealStatusResponse
	err = retryWithBackoff(ctx, c.opts.maxRetries, c.opts.retryBackoff, c.opts.maxRetryBackoff, func() error {
		var rpcErr error
		resp, rpcErr = pc.SealStatus(ctx, &proto.SealStatusRequest{})
		return rpcErr
	})
	if err != nil {
		return nil, convertGRPCError(err, "SealStatus")
	}
	return &service.SealStatusResponse{
		Sealed:         resp.GetSealed(),
		Initialized:    resp.GetInitialized(),
		Threshold:      int(resp.GetThreshold()),
		TotalShares:    int(resp.GetTotalShares()),
		SharesProvided: int(resp.GetSharesProvided()),
		Version:        resp.GetVersion(),
	}, nil
}

// SealInit initializes the seal manager and returns generated shares.
func (c *Client) SealInit(ctx context.Context, req *service.SealInitRequest) (*service.SealInitResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}
	ctx = c.addMetadata(ctx)
	pc, err := c.getClient()
	if err != nil {
		return nil, convertGRPCError(err, "SealInit")
	}
	var resp *proto.SealInitResponse
	err = retryWithBackoff(ctx, c.opts.maxRetries, c.opts.retryBackoff, c.opts.maxRetryBackoff, func() error {
		var rpcErr error
		resp, rpcErr = pc.SealInit(ctx, &proto.SealInitRequest{
			Threshold:   int32(req.Threshold),
			TotalShares: int32(req.TotalShares),
		})
		return rpcErr
	})
	if err != nil {
		return nil, convertGRPCError(err, "SealInit")
	}
	return &service.SealInitResponse{
		Shares:    resp.GetShares(),
		RootToken: resp.GetRootToken(),
		Version:   resp.GetVersion(),
	}, nil
}

// Unseal submits one share toward unseal quorum.
func (c *Client) Unseal(ctx context.Context, req *service.UnsealRequest) (*service.UnsealResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}
	ctx = c.addMetadata(ctx)
	pc, err := c.getClient()
	if err != nil {
		return nil, convertGRPCError(err, "Unseal")
	}
	var resp *proto.UnsealResponse
	err = retryWithBackoff(ctx, c.opts.maxRetries, c.opts.retryBackoff, c.opts.maxRetryBackoff, func() error {
		var rpcErr error
		resp, rpcErr = pc.Unseal(ctx, &proto.UnsealRequest{Share: req.Share})
		return rpcErr
	})
	if err != nil {
		return nil, convertGRPCError(err, "Unseal")
	}
	return &service.UnsealResponse{
		Sealed:         resp.GetSealed(),
		SharesProvided: int(resp.GetSharesProvided()),
		Threshold:      int(resp.GetThreshold()),
	}, nil
}

// Seal re-seals the database.
func (c *Client) Seal(ctx context.Context) error {
	if err := c.ensureConnected(ctx); err != nil {
		return err
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}
	ctx = c.addMetadata(ctx)
	pc, err := c.getClient()
	if err != nil {
		return convertGRPCError(err, "Seal")
	}
	err = retryWithBackoff(ctx, c.opts.maxRetries, c.opts.retryBackoff, c.opts.maxRetryBackoff, func() error {
		_, rpcErr := pc.Seal(ctx, &proto.SealRequest{})
		return rpcErr
	})
	if err != nil {
		return convertGRPCError(err, "Seal")
	}
	return nil
}

// Rekey generates new key shares with a new threshold configuration.
func (c *Client) Rekey(ctx context.Context, req *service.RekeyRequest) (*service.RekeyResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.opts.timeout)
		defer cancel()
	}
	ctx = c.addMetadata(ctx)
	pc, err := c.getClient()
	if err != nil {
		return nil, convertGRPCError(err, "Rekey")
	}
	var resp *proto.SealRekeyResponse
	err = retryWithBackoff(ctx, c.opts.maxRetries, c.opts.retryBackoff, c.opts.maxRetryBackoff, func() error {
		var rpcErr error
		resp, rpcErr = pc.SealRekey(ctx, &proto.SealRekeyRequest{
			NewThreshold: int32(req.NewThreshold),
			NewTotal:     int32(req.NewTotalShares),
		})
		return rpcErr
	})
	if err != nil {
		return nil, convertGRPCError(err, "Rekey")
	}
	return &service.RekeyResponse{
		Shares:  resp.GetShares(),
		Version: resp.GetVersion(),
	}, nil
}
