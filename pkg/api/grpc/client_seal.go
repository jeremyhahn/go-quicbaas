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
	stderrors "errors"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// SealStatus returns the current seal status.
func (c *Client) SealStatus(ctx context.Context) (*service.SealStatusResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}
	pbResp, err := c.client.SealStatus(ctx, &pb.SealStatusRequest{})
	if err != nil {
		return nil, convertError(err)
	}

	if pbResp.Error != "" {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Client.SealStatus",
			Err:  stderrors.New(pbResp.Error),
		}
	}

	return &service.SealStatusResponse{
		Sealed:         pbResp.Sealed,
		Initialized:    pbResp.Initialized,
		Mode:           convertProtoSealMode(pbResp.Mode),
		Threshold:      int(pbResp.Threshold),
		TotalShares:    int(pbResp.TotalShares),
		SharesProvided: int(pbResp.SharesProvided),
		TPMAvailable:   pbResp.TpmAvailable,
		ClusterNodes:   int(pbResp.ClusterNodes),
		Version:        pbResp.Version,
	}, nil
}

// SealInit initializes the seal and generates key shares.
func (c *Client) SealInit(ctx context.Context, req *service.SealInitRequest) (*service.SealInitResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.SealInit",
			Err:  ErrRequestNil,
		}
	}

	pbReq := &pb.SealInitRequest{
		Threshold:           int32(req.Threshold),   //nolint:gosec // G115: small values
		TotalShares:         int32(req.TotalShares), //nolint:gosec // G115: small values
		RecoveryKeysEnabled: req.RecoveryKeysEnabled,
		RecoveryThreshold:   int32(req.RecoveryThreshold), //nolint:gosec // G115: small values
		RecoveryTotalKeys:   int32(req.RecoveryTotalKeys), //nolint:gosec // G115: small values
	}

	pbResp, err := c.client.SealInit(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	if pbResp.Error != "" {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Client.SealInit",
			Err:  stderrors.New(pbResp.Error),
		}
	}

	return &service.SealInitResponse{
		Shares:       pbResp.Shares,
		RecoveryKeys: pbResp.RecoveryKeys,
		RootToken:    pbResp.RootToken,
		Version:      pbResp.Version,
	}, nil
}

// Unseal provides a key share to unseal the database.
func (c *Client) Unseal(ctx context.Context, req *service.UnsealRequest) (*service.UnsealResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.Unseal",
			Err:  ErrRequestNil,
		}
	}

	pbReq := &pb.UnsealRequest{
		Share:     req.Share,
		NodeId:    req.NodeID,
		Signature: req.Signature,
	}

	pbResp, err := c.client.Unseal(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	if pbResp.Error != "" {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Client.Unseal",
			Err:  stderrors.New(pbResp.Error),
		}
	}

	return &service.UnsealResponse{
		Sealed:         pbResp.Sealed,
		SharesProvided: int(pbResp.SharesProvided),
		Threshold:      int(pbResp.Threshold),
	}, nil
}

// Seal re-seals the database.
func (c *Client) Seal(ctx context.Context) error {
	if err := c.ensureConnected(ctx); err != nil {
		return err
	}
	pbResp, err := c.client.Seal(ctx, &pb.SealRequest{Force: false})
	if err != nil {
		return convertError(err)
	}

	if pbResp.Error != "" {
		return &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Client.Seal",
			Err:  stderrors.New(pbResp.Error),
		}
	}

	return nil
}

// Rekey generates new key shares with a new threshold configuration.
func (c *Client) Rekey(ctx context.Context, req *service.RekeyRequest) (*service.RekeyResponse, error) {
	if req == nil {
		return nil, &errors.QBError{
			Code: errors.ErrInvalidInput,
			Op:   "Client.Rekey",
			Err:  ErrRequestNil,
		}
	}

	pbReq := &pb.SealRekeyRequest{
		NewThreshold: int32(req.NewThreshold),   //nolint:gosec // G115: small values
		NewTotal:     int32(req.NewTotalShares), //nolint:gosec // G115: small values
	}

	pbResp, err := c.client.SealRekey(ctx, pbReq)
	if err != nil {
		return nil, convertError(err)
	}

	if pbResp.Error != "" {
		return nil, &errors.QBError{
			Code: errors.ErrInternal,
			Op:   "Client.Rekey",
			Err:  stderrors.New(pbResp.Error),
		}
	}

	return &service.RekeyResponse{
		Shares:  pbResp.Shares,
		Version: pbResp.Version,
	}, nil
}

// convertProtoSealMode converts proto SealMode to string.
func convertProtoSealMode(mode pb.SealMode) string {
	switch mode {
	case pb.SealMode_SEAL_MODE_MANUAL:
		return "manual"
	case pb.SealMode_SEAL_MODE_AUTO:
		return "auto"
	case pb.SealMode_SEAL_MODE_CRYPTO:
		return "crypto"
	default:
		return "unspecified"
	}
}
