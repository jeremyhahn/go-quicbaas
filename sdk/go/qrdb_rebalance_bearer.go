// Copyright (c) 2025 Jeremy Hahn
// SPDX-License-Identifier: AGPL-3.0-or-later

package quicbaas

import (
	"context"

	"google.golang.org/grpc/metadata"
)

// qrdbBearerRebalanceClient forwards to go-qrdb's rebalance client and adds
// the platform bearer as outgoing gRPC authorization metadata on every call,
// the way the SDK's own gRPC transport does.
type qrdbBearerRebalanceClient struct {
	inner  QRDBRebalanceClient
	bearer string
}

var _ QRDBRebalanceClient = (*qrdbBearerRebalanceClient)(nil)

// GetShardStats forwards to go-qrdb's GetShardStats with the bearer attached.
func (b *qrdbBearerRebalanceClient) GetShardStats(ctx context.Context, shardID uint64) (*QRDBShardStatsResponse, error) {
	return b.inner.GetShardStats(metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer), shardID)
}

// CreateRebalancePlan forwards to go-qrdb's CreateRebalancePlan with the
// bearer attached.
func (b *qrdbBearerRebalanceClient) CreateRebalancePlan(ctx context.Context, req *QRDBRebalancePlanRequest) (*QRDBRebalancePlan, error) {
	return b.inner.CreateRebalancePlan(metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer), req)
}

// ExecuteRebalance forwards to go-qrdb's ExecuteRebalance with the bearer
// attached.
func (b *qrdbBearerRebalanceClient) ExecuteRebalance(ctx context.Context, plan *QRDBRebalancePlan) (*QRDBRebalanceResult, error) {
	return b.inner.ExecuteRebalance(metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer), plan)
}

// GetImbalanceReport forwards to go-qrdb's GetImbalanceReport with the bearer
// attached.
func (b *qrdbBearerRebalanceClient) GetImbalanceReport(ctx context.Context) (*QRDBImbalanceReport, error) {
	return b.inner.GetImbalanceReport(metadata.AppendToOutgoingContext(ctx, "authorization", b.bearer))
}
