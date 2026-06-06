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

package operations

import (
	"context"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

const (
	serviceName = "quicbaas.Operations"
)

// Client provides operations management functionality.
type Client interface {
	// MigrateData migrates data between shards or nodes.
	MigrateData(ctx context.Context, req *MigrateDataRequest) (*MigrateDataResponse, error)

	// GetMigrationProgress returns the status of an ongoing migration.
	GetMigrationProgress(ctx context.Context, req *GetMigrationProgressRequest) (*GetMigrationProgressResponse, error)

	// ReplaceNode replaces a failed node with a new node.
	ReplaceNode(ctx context.Context, req *ReplaceNodeRequest) (*ReplaceNodeResponse, error)

	// UpgradeNode performs a rolling upgrade of a node.
	UpgradeNode(ctx context.Context, req *UpgradeNodeRequest) (*UpgradeNodeResponse, error)

	// GetUpgradeStatus returns the status of an ongoing upgrade.
	GetUpgradeStatus(ctx context.Context, req *GetUpgradeStatusRequest) (*GetUpgradeStatusResponse, error)
}

// MigrateDataRequest contains parameters for data migration.
type MigrateDataRequest struct {
	// SourceShard is the shard to migrate data from.
	SourceShard uint64

	// TargetShard is the shard to migrate data to.
	TargetShard uint64

	// EntityTypes specifies which entity types to migrate.
	// Empty means all entity types.
	EntityTypes []string

	// Concurrency is the number of concurrent migration workers.
	// Zero or negative uses the server default.
	Concurrency int
}

// MigrateDataResponse contains the result of starting a migration.
type MigrateDataResponse struct {
	// MigrationID is the unique identifier for the migration job.
	MigrationID string

	// Status is the current status of the migration.
	Status string

	// EstimatedDuration is the estimated completion time in seconds.
	EstimatedDuration int64
}

// GetMigrationProgressRequest contains parameters for getting migration progress.
type GetMigrationProgressRequest struct {
	// MigrationID is the unique identifier of the migration to query.
	MigrationID string
}

// GetMigrationProgressResponse contains migration progress information.
type GetMigrationProgressResponse struct {
	// MigrationID is the unique identifier for the migration.
	MigrationID string

	// Progress is the completion percentage (0-100).
	Progress float64

	// Status is the current status of the migration.
	Status string

	// ItemsMigrated is the number of items migrated so far.
	ItemsMigrated uint64

	// TotalItems is the total number of items to migrate.
	TotalItems uint64

	// ETA is the estimated time of completion as Unix timestamp.
	ETA int64
}

// ReplaceNodeRequest contains parameters for replacing a node.
type ReplaceNodeRequest struct {
	// ShardID is the shard containing the node to replace.
	ShardID uint64

	// OldNodeID is the ID of the node to be replaced.
	OldNodeID uint64

	// NewNodeID is the ID of the replacement node.
	NewNodeID uint64

	// NewAddress is the network address of the replacement node.
	NewAddress string
}

// ReplaceNodeResponse contains the result of a node replacement operation.
type ReplaceNodeResponse struct {
	// JobID is the unique identifier for the replacement job.
	JobID string

	// Status is the current status of the replacement operation.
	Status string
}

// UpgradeNodeRequest contains parameters for upgrading a node.
type UpgradeNodeRequest struct {
	// NodeID is the ID of the node to upgrade.
	NodeID uint64

	// TargetVersion is the version to upgrade to.
	TargetVersion string
}

// UpgradeNodeResponse contains the result of starting a node upgrade.
type UpgradeNodeResponse struct {
	// JobID is the unique identifier for the upgrade job.
	JobID string

	// Status is the current status of the upgrade.
	Status string
}

// GetUpgradeStatusRequest contains parameters for getting upgrade status.
type GetUpgradeStatusRequest struct {
	// JobID is the unique identifier of the upgrade job to query.
	JobID string
}

// GetUpgradeStatusResponse contains upgrade status information.
type GetUpgradeStatusResponse struct {
	// JobID is the unique identifier for the upgrade.
	JobID string

	// Progress is the completion percentage (0-100).
	Progress float64

	// Status is the current status of the upgrade.
	Status string

	// CurrentVersion is the node's current version.
	CurrentVersion string

	// TargetVersion is the version being upgraded to.
	TargetVersion string
}

// client implements the Client interface.
type client struct {
	transport transport.Transport
}

// NewClient creates a new Operations client.
func NewClient(t transport.Transport) Client {
	if t == nil {
		return nil
	}
	return &client{
		transport: t,
	}
}

// MigrateData migrates data between shards or nodes.
func (c *client) MigrateData(ctx context.Context, req *MigrateDataRequest) (*MigrateDataResponse, error) {
	if err := validateMigrateDataRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "MigrateData", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("operations.MigrateData", "nil response from server")
	}

	migrateResp, ok := resp.(*MigrateDataResponse)
	if !ok {
		return nil, errors.NewInternal("operations.MigrateData", "invalid response type")
	}

	return migrateResp, nil
}

// GetMigrationProgress returns the status of an ongoing migration.
func (c *client) GetMigrationProgress(ctx context.Context, req *GetMigrationProgressRequest) (*GetMigrationProgressResponse, error) {
	if err := validateGetMigrationProgressRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetMigrationProgress", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("operations.GetMigrationProgress", "nil response from server")
	}

	progressResp, ok := resp.(*GetMigrationProgressResponse)
	if !ok {
		return nil, errors.NewInternal("operations.GetMigrationProgress", "invalid response type")
	}

	return progressResp, nil
}

// ReplaceNode replaces a failed node with a new node.
func (c *client) ReplaceNode(ctx context.Context, req *ReplaceNodeRequest) (*ReplaceNodeResponse, error) {
	if err := validateReplaceNodeRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "ReplaceNode", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("operations.ReplaceNode", "nil response from server")
	}

	replaceResp, ok := resp.(*ReplaceNodeResponse)
	if !ok {
		return nil, errors.NewInternal("operations.ReplaceNode", "invalid response type")
	}

	return replaceResp, nil
}

// UpgradeNode performs a rolling upgrade of a node.
func (c *client) UpgradeNode(ctx context.Context, req *UpgradeNodeRequest) (*UpgradeNodeResponse, error) {
	if err := validateUpgradeNodeRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "UpgradeNode", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("operations.UpgradeNode", "nil response from server")
	}

	upgradeResp, ok := resp.(*UpgradeNodeResponse)
	if !ok {
		return nil, errors.NewInternal("operations.UpgradeNode", "invalid response type")
	}

	return upgradeResp, nil
}

// GetUpgradeStatus returns the status of an ongoing upgrade.
func (c *client) GetUpgradeStatus(ctx context.Context, req *GetUpgradeStatusRequest) (*GetUpgradeStatusResponse, error) {
	if err := validateGetUpgradeStatusRequest(req); err != nil {
		return nil, err
	}

	resp, err := c.transport.Request(ctx, serviceName, "GetUpgradeStatus", req)
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, errors.NewInternal("operations.GetUpgradeStatus", "nil response from server")
	}

	statusResp, ok := resp.(*GetUpgradeStatusResponse)
	if !ok {
		return nil, errors.NewInternal("operations.GetUpgradeStatus", "invalid response type")
	}

	return statusResp, nil
}

// Validation functions

func validateMigrateDataRequest(req *MigrateDataRequest) error {
	if req == nil {
		return errors.NewInvalidInput("operations.MigrateData", "request is required")
	}
	if req.SourceShard == 0 {
		return errors.NewInvalidInput("operations.MigrateData", "source shard is required")
	}
	if req.TargetShard == 0 {
		return errors.NewInvalidInput("operations.MigrateData", "target shard is required")
	}
	if req.SourceShard == req.TargetShard {
		return errors.NewInvalidInput("operations.MigrateData", "source and target shards must be different")
	}
	return nil
}

func validateGetMigrationProgressRequest(req *GetMigrationProgressRequest) error {
	if req == nil {
		return errors.NewInvalidInput("operations.GetMigrationProgress", "request is required")
	}
	if req.MigrationID == "" {
		return errors.NewInvalidInput("operations.GetMigrationProgress", "migration ID is required")
	}
	return nil
}

func validateReplaceNodeRequest(req *ReplaceNodeRequest) error {
	if req == nil {
		return errors.NewInvalidInput("operations.ReplaceNode", "request is required")
	}
	if req.ShardID == 0 {
		return errors.NewInvalidInput("operations.ReplaceNode", "shard ID is required")
	}
	if req.OldNodeID == 0 {
		return errors.NewInvalidInput("operations.ReplaceNode", "old node ID is required")
	}
	if req.NewNodeID == 0 {
		return errors.NewInvalidInput("operations.ReplaceNode", "new node ID is required")
	}
	if req.NewAddress == "" {
		return errors.NewInvalidInput("operations.ReplaceNode", "new address is required")
	}
	if req.OldNodeID == req.NewNodeID {
		return errors.NewInvalidInput("operations.ReplaceNode", "old and new node IDs must be different")
	}
	return nil
}

func validateUpgradeNodeRequest(req *UpgradeNodeRequest) error {
	if req == nil {
		return errors.NewInvalidInput("operations.UpgradeNode", "request is required")
	}
	// NodeID == 0 is the canonical "upgrade all nodes" sentinel for a rolling
	// upgrade (see the `quicbaas upgrade start` --node-id flag: "0 = all
	// nodes"); it must not be rejected here.
	if req.TargetVersion == "" {
		return errors.NewInvalidInput("operations.UpgradeNode", "target version is required")
	}
	return nil
}

func validateGetUpgradeStatusRequest(req *GetUpgradeStatusRequest) error {
	if req == nil {
		return errors.NewInvalidInput("operations.GetUpgradeStatus", "request is required")
	}
	if req.JobID == "" {
		return errors.NewInvalidInput("operations.GetUpgradeStatus", "job ID is required")
	}
	return nil
}
