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
	"time"

	pb "github.com/jeremyhahn/go-quicbaas/api/proto"
	"github.com/jeremyhahn/go-quicbaas/pkg/service"
)

// KMSBackendStatus returns the status and capabilities of the KMS backend.
func (c *Client) KMSBackendStatus(ctx context.Context) (*service.BackendStatusResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}
	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbResp, err := kmsClient.BackendStatus(ctx, &pb.BackendStatusRequest{})
	if err != nil {
		return nil, convertError(err)
	}

	// Convert PCR registers from int32 to int
	pcrRegs := make([]int, len(pbResp.PcrRegisters))
	for i, r := range pbResp.PcrRegisters {
		pcrRegs[i] = int(r)
	}

	return &service.BackendStatusResponse{
		BackendType:  pbResp.BackendType,
		Status:       pbResp.Status,
		Device:       pbResp.Device,
		Region:       pbResp.Region,
		KeyID:        pbResp.KeyId,
		PCRBanks:     pbResp.PcrBanks,
		PCRRegisters: pcrRegs,
		Capabilities: pbResp.Capabilities,
		Timestamp:    time.Unix(pbResp.Timestamp, 0),
	}, nil
}

// KMSBackendTest performs a test cycle to verify backend functionality.
func (c *Client) KMSBackendTest(ctx context.Context) (*service.BackendTestResponse, error) {
	if err := c.ensureConnected(ctx); err != nil {
		return nil, err
	}
	kmsClient := pb.NewKeyManagementClient(c.conn)

	pbResp, err := kmsClient.BackendTest(ctx, &pb.BackendTestRequest{})
	if err != nil {
		return nil, convertError(err)
	}

	return &service.BackendTestResponse{
		Success:   pbResp.Success,
		Duration:  time.Duration(pbResp.DurationMs) * time.Millisecond,
		Message:   pbResp.Message,
		Timestamp: time.Unix(pbResp.Timestamp, 0),
	}, nil
}
