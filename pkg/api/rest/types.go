//go:build rest

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

// Package rest provides RESTful HTTP API for QuicBaaS.
// It exposes CRUD operations, cluster management, and query capabilities via HTTP/JSON.
package rest

import (
	"encoding/json"
	"time"
)

// PutRequest represents a request to create or update an entity.
type PutRequest struct {
	// EntityType is the type/category of the entity
	EntityType string `json:"entity_type"`

	// ID is the entity ID (optional for auto-assignment)
	ID uint64 `json:"id,omitempty"`

	// Data is the entity data (JSON object)
	Data json.RawMessage `json:"data"`

	// ConsistencyLevel specifies read consistency (quorum/local)
	ConsistencyLevel string `json:"consistency_level,omitempty"`
}

// PutResponse represents the response from a Put operation.
type PutResponse struct {
	// ID is the assigned or provided entity ID
	ID uint64 `json:"id"`

	// EntityType is the type of the entity
	EntityType string `json:"entity_type"`

	// Created indicates if this was a new entity (true) or update (false)
	Created bool `json:"created"`

	// Timestamp is when the operation completed
	Timestamp time.Time `json:"timestamp"`
}

// CompareAndSwapRequest is the body of POST
// /api/v1/entities/{type}/{id}/compare-and-swap. Expect and Set are raw bytes,
// base64-encoded in JSON, so the stored value is compared byte for byte.
type CompareAndSwapRequest struct {
	// Expect is the value the entity must currently hold
	Expect []byte `json:"expect"`

	// Set is the value stored when Expect matches
	Set []byte `json:"set"`
}

// PutIfAbsentRequest is the body of POST
// /api/v1/entities/{type}/{id}/put-if-absent. Data is raw bytes,
// base64-encoded in JSON.
type PutIfAbsentRequest struct {
	// Data is the value stored when the entity is absent
	Data []byte `json:"data"`
}

// ConditionalPutResponse reports whether a conditional write was stored.
// A missed precondition is a normal outcome, reported as Applied == false.
type ConditionalPutResponse struct {
	// Applied is true when the precondition held and the write was stored
	Applied bool `json:"applied"`
}

// GetRequest represents a request to retrieve an entity.
type GetRequest struct {
	// EntityType is the type/category of the entity
	EntityType string `json:"entity_type"`

	// ID is the entity ID
	ID uint64 `json:"id"`

	// ConsistencyLevel specifies read consistency (quorum/local)
	ConsistencyLevel string `json:"consistency_level,omitempty"`
}

// GetResponse represents the response from a Get operation.
type GetResponse struct {
	// ID is the entity ID
	ID uint64 `json:"id"`

	// EntityType is the type of the entity
	EntityType string `json:"entity_type"`

	// Data is the entity data (JSON object)
	Data json.RawMessage `json:"data"`

	// Version is the entity version (for optimistic locking)
	Version uint64 `json:"version,omitempty"`

	// Timestamp is when the entity was last modified
	Timestamp time.Time `json:"timestamp"`
}

// DeleteRequest represents a request to delete an entity.
type DeleteRequest struct {
	// EntityType is the type/category of the entity
	EntityType string `json:"entity_type"`

	// ID is the entity ID
	ID uint64 `json:"id"`
}

// DeleteResponse represents the response from a Delete operation.
type DeleteResponse struct {
	// ID is the deleted entity ID
	ID uint64 `json:"id"`

	// EntityType is the type of the entity
	EntityType string `json:"entity_type"`

	// Deleted indicates if the entity was actually deleted
	Deleted bool `json:"deleted"`

	// Timestamp is when the operation completed
	Timestamp time.Time `json:"timestamp"`
}

// ListRequest represents a request to list entities.
type ListRequest struct {
	// EntityType is the type/category of entities to list
	EntityType string `json:"entity_type"`

	// Limit is the maximum number of results to return
	Limit int `json:"limit,omitempty"`

	// Cursor is the pagination cursor from previous response
	Cursor string `json:"cursor,omitempty"`

	// ConsistencyLevel specifies read consistency (quorum/local)
	ConsistencyLevel string `json:"consistency_level,omitempty"`
}

// ListResponse represents the response from a List operation.
type ListResponse struct {
	// Entities is the list of entities
	Entities []EntityItem `json:"entities"`

	// Total is the total count of entities (if available)
	Total int `json:"total,omitempty"`

	// NextCursor is the cursor for fetching next page
	NextCursor string `json:"next_cursor,omitempty"`

	// HasMore indicates if there are more results
	HasMore bool `json:"has_more"`
}

// ListEntityTypesResponse represents the response from a ListEntityTypes operation.
type ListEntityTypesResponse struct {
	// EntityTypes is the list of unique entity types discovered
	EntityTypes []string `json:"entity_types"`

	// NextPageToken is the token for fetching the next page
	NextPageToken string `json:"next_page_token,omitempty"`

	// TotalCount is the total count of entity types (if available)
	TotalCount int `json:"total_count,omitempty"`
}

// EntityItem represents a single entity in a list.
type EntityItem struct {
	// ID is the entity ID
	ID uint64 `json:"id"`

	// EntityType is the type of the entity
	EntityType string `json:"entity_type"`

	// Data is the entity data (JSON object)
	Data json.RawMessage `json:"data"`

	// Timestamp is when the entity was last modified
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// QueryRequest represents a request to query entities by index.
type QueryRequest struct {
	// EntityType is the type/category of entities to query
	EntityType string `json:"entity_type"`

	// Index is the index name (field name)
	Index string `json:"index"`

	// Value is the value to search for
	Value interface{} `json:"value"`

	// ConsistencyLevel specifies read consistency (quorum/local)
	ConsistencyLevel string `json:"consistency_level,omitempty"`
}

// RangeQueryRequest represents a request for range queries.
type RangeQueryRequest struct {
	// EntityType is the type/category of entities to query
	EntityType string `json:"entity_type"`

	// Index is the index name (field name)
	Index string `json:"index"`

	// Start is the range start value (inclusive)
	Start interface{} `json:"start"`

	// End is the range end value (exclusive)
	End interface{} `json:"end"`

	// Limit is the maximum number of results to return
	Limit int `json:"limit,omitempty"`

	// ConsistencyLevel specifies read consistency (quorum/local)
	ConsistencyLevel string `json:"consistency_level,omitempty"`
}

// QueryResponse represents the response from a Query operation.
type QueryResponse struct {
	// Results is the list of matching entities
	Results []EntityItem `json:"results"`

	// Count is the number of results returned
	Count int `json:"count"`

	// Index is the index that was queried
	Index string `json:"index"`
}

// ClusterStatusResponse represents cluster status information.
type ClusterStatusResponse struct {
	// NodeID is the current node ID
	NodeID uint64 `json:"node_id"`

	// RaftAddress is the Raft communication address
	RaftAddress string `json:"raft_address"`

	// LeaderID is the ID of the cluster leader (from first shard)
	LeaderID string `json:"leader_id,omitempty"`

	// TotalNodes is the total number of nodes in the cluster
	TotalNodes int `json:"total_nodes,omitempty"`

	// Members is the list of member node IDs
	Members []uint64 `json:"members,omitempty"`

	// Shards is the list of Raft shards on this node
	Shards []ShardInfo `json:"shards"`

	// Healthy indicates if the node is healthy
	Healthy bool `json:"healthy"`

	// Timestamp is when the status was collected
	Timestamp time.Time `json:"timestamp"`
}

// ShardInfo represents information about a Raft shard.
type ShardInfo struct {
	// ShardID is the unique shard identifier
	ShardID uint64 `json:"shard_id"`

	// LeaderID is the current leader node ID
	LeaderID uint64 `json:"leader_id"`

	// IsLeader indicates if this node is the leader
	IsLeader bool `json:"is_leader"`

	// Members is the list of member node IDs
	Members []uint64 `json:"members"`

	// State is the shard state (follower, candidate, leader)
	State string `json:"state"`
}

// LeaderResponse represents leader information for a shard.
type LeaderResponse struct {
	// ShardID is the shard identifier
	ShardID uint64 `json:"shard_id"`

	// LeaderID is the current leader node ID
	LeaderID uint64 `json:"leader_id"`

	// LeaderAddress is the leader's Raft address
	LeaderAddress string `json:"leader_address,omitempty"`

	// Term is the current Raft term
	Term uint64 `json:"term,omitempty"`

	// Valid indicates if the leader information is valid
	Valid bool `json:"valid"`
}

// ClusterLeaderResponse represents the current cluster leader information.
type ClusterLeaderResponse struct {
	// Leader contains the leader node information
	Leader NodeInfoResponse `json:"leader"`

	// Timestamp is when the leader information was retrieved
	Timestamp time.Time `json:"timestamp"`
}

// HealthResponse represents health check response.
type HealthResponse struct {
	// Status is the overall health status (healthy, degraded, unhealthy)
	Status string `json:"status"`

	// Checks is a map of health check results
	Checks map[string]CheckResult `json:"checks"`

	// Timestamp is when the health check was performed
	Timestamp time.Time `json:"timestamp"`

	// Uptime is the server uptime duration
	Uptime string `json:"uptime"`
}

// CheckResult represents the result of a single health check.
type CheckResult struct {
	// Status is the check status (pass, warn, fail)
	Status string `json:"status"`

	// Message is a human-readable message
	Message string `json:"message,omitempty"`

	// Data contains additional check-specific data
	Data map[string]interface{} `json:"data,omitempty"`
}

// ErrorResponse represents an error response.
type ErrorResponse struct {
	// Error is the error message
	Error string `json:"error"`

	// Code is the error code
	Code string `json:"code,omitempty"`

	// Details contains additional error context
	Details map[string]interface{} `json:"details,omitempty"`

	// FaultClass names the spawn configuration-fault class of the failure
	// (orchestration.SpawnFaultClass), when it has one. It is a separate field
	// from Code because the two are different vocabularies: Code classifies the
	// error for any client, FaultClass tells a fleet node daemon which
	// orchestration sentinel to rebuild (orchestration.SpawnFaultSentinel) so
	// a spawn it could not start is parked or charged exactly as an
	// in-process one would be.
	FaultClass string `json:"fault_class,omitempty"`

	// Timestamp is when the error occurred
	Timestamp time.Time `json:"timestamp"`

	// RequestID is the request identifier for tracing
	RequestID string `json:"request_id,omitempty"`

	// Retryable indicates if the operation can be retried
	Retryable bool `json:"retryable"`
}

// SuccessResponse represents a generic success response.
type SuccessResponse struct {
	// Success indicates operation success
	Success bool `json:"success"`

	// Message is a human-readable message
	Message string `json:"message,omitempty"`

	// Data contains response-specific data
	Data interface{} `json:"data,omitempty"`

	// Timestamp is when the operation completed
	Timestamp time.Time `json:"timestamp"`
}

// PaginatedResponse wraps paginated results.
type PaginatedResponse struct {
	// Data contains the actual results
	Data interface{} `json:"data"`

	// Pagination contains pagination metadata
	Pagination PaginationMetadata `json:"pagination"`
}

// PaginationMetadata contains pagination information.
type PaginationMetadata struct {
	// Limit is the maximum number of results per page
	Limit int `json:"limit"`

	// Cursor is the current page cursor
	Cursor string `json:"cursor,omitempty"`

	// NextCursor is the cursor for the next page
	NextCursor string `json:"next_cursor,omitempty"`

	// HasMore indicates if there are more results
	HasMore bool `json:"has_more"`

	// TotalCount is the total count (if available)
	TotalCount int `json:"total_count,omitempty"`
}

// ConsistencyLevel represents read consistency options.
type ConsistencyLevel string

const (
	// ConsistencyStrong requires full Raft consensus (10-50ms latency).
	ConsistencyStrong ConsistencyLevel = "strong"

	// ConsistencyLinearizable uses ReadIndex optimization (1-5ms latency, RECOMMENDED).
	ConsistencyLinearizable ConsistencyLevel = "linearizable"

	// ConsistencyEventual allows stale reads (<1ms latency).
	ConsistencyEventual ConsistencyLevel = "eventual"
)

// Validate validates the consistency level.
func (c ConsistencyLevel) Validate() error {
	switch c {
	case ConsistencyStrong, ConsistencyLinearizable, ConsistencyEventual, "":
		return nil
	default:
		return &InvalidConsistencyLevelError{Level: string(c)}
	}
}

// InvalidConsistencyLevelError is returned for invalid consistency levels.
type InvalidConsistencyLevelError struct {
	Level string
}

func (e *InvalidConsistencyLevelError) Error() string {
	return "invalid consistency level: " + e.Level + " (must be 'strong', 'linearizable', or 'eventual')"
}

// BatchPutRequest represents a request to perform multiple put operations atomically.
type BatchPutRequest struct {
	// Operations is the list of put operations to perform
	Operations []BatchPutOperation `json:"operations"`
}

// BatchPutOperation represents a single put operation in a batch.
type BatchPutOperation struct {
	// EntityType is the type/category of the entity
	EntityType string `json:"entity_type"`

	// ID is the entity ID
	ID uint64 `json:"id"`

	// Data is the entity data (JSON object)
	Data json.RawMessage `json:"data"`
}

// BatchPutResponse represents the response from a batch put operation.
type BatchPutResponse struct {
	// Success indicates if the batch operation succeeded
	Success bool `json:"success"`

	// Responses is the list of individual operation responses
	Responses []PutResponse `json:"responses"`

	// Timestamp is when the operation completed
	Timestamp int64 `json:"timestamp"`
}

// BatchGetRequest represents a request to retrieve multiple entities in a single request.
type BatchGetRequest struct {
	// Operations is the list of get operations to perform
	Operations []BatchGetOperation `json:"operations"`

	// ConsistencyLevel specifies read consistency for all operations
	ConsistencyLevel string `json:"consistency_level,omitempty"`
}

// BatchGetOperation represents a single get operation in a batch.
type BatchGetOperation struct {
	// EntityType is the type/category of the entity
	EntityType string `json:"entity_type"`

	// ID is the entity ID
	ID uint64 `json:"id"`
}

// BatchGetResponse represents the response from a batch get operation.
type BatchGetResponse struct {
	// Success indicates if the batch operation succeeded
	Success bool `json:"success"`

	// Responses is the list of individual operation responses
	Responses []BatchGetItemResponse `json:"responses"`

	// Timestamp is when the operation completed
	Timestamp int64 `json:"timestamp"`
}

// BatchGetItemResponse represents a single get result in a batch response.
type BatchGetItemResponse struct {
	// ID is the entity ID
	ID uint64 `json:"id"`

	// EntityType is the type of the entity
	EntityType string `json:"entity_type"`

	// Data is the entity data (JSON object), null if not found
	Data json.RawMessage `json:"data,omitempty"`

	// Found indicates if the entity was found
	Found bool `json:"found"`

	// Timestamp is when the entity was last modified
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// BatchDeleteRequest represents a request to perform multiple delete operations atomically.
type BatchDeleteRequest struct {
	// Operations is the list of delete operations to perform
	Operations []BatchDeleteOperation `json:"operations"`
}

// BatchDeleteOperation represents a single delete operation in a batch.
type BatchDeleteOperation struct {
	// EntityType is the type/category of the entity
	EntityType string `json:"entity_type"`

	// ID is the entity ID
	ID uint64 `json:"id"`
}

// BatchDeleteResponse represents the response from a batch delete operation.
type BatchDeleteResponse struct {
	// Success indicates if the batch operation succeeded
	Success bool `json:"success"`

	// Responses is the list of individual operation responses
	Responses []DeleteResponse `json:"responses"`

	// Timestamp is when the operation completed
	Timestamp int64 `json:"timestamp"`
}

// =============================================================================
// Key-Value (KV) Types - String Key Based Operations
// =============================================================================

// KVPutRequest represents a request to store a key-value pair with string keys.
type KVPutRequest struct {
	// Value is the data to store (JSON or raw bytes)
	Value json.RawMessage `json:"value"`

	// ConsistencyLevel specifies write consistency
	ConsistencyLevel string `json:"consistency_level,omitempty"`
}

// KVPutResponse represents the response from a KV Put operation.
type KVPutResponse struct {
	// EntityType is the type/namespace of the key
	EntityType string `json:"entity_type"`

	// Key is the string key
	Key string `json:"key"`

	// Success indicates if the operation succeeded
	Success bool `json:"success"`

	// Message provides additional information
	Message string `json:"message,omitempty"`

	// Timestamp is when the operation completed
	Timestamp time.Time `json:"timestamp"`
}

// KVGetResponse represents the response from a KV Get operation.
type KVGetResponse struct {
	// EntityType is the type/namespace of the key
	EntityType string `json:"entity_type"`

	// Key is the string key
	Key string `json:"key"`

	// Value is the stored data
	Value json.RawMessage `json:"value"`

	// Found indicates if the key was found
	Found bool `json:"found"`

	// Timestamp is when the value was last modified
	Timestamp time.Time `json:"timestamp"`
}

// KVDeleteResponse represents the response from a KV Delete operation.
type KVDeleteResponse struct {
	// EntityType is the type/namespace of the key
	EntityType string `json:"entity_type"`

	// Key is the string key
	Key string `json:"key"`

	// Deleted indicates if the key was deleted
	Deleted bool `json:"deleted"`

	// Message provides additional information
	Message string `json:"message,omitempty"`

	// Timestamp is when the operation completed
	Timestamp time.Time `json:"timestamp"`
}

// KVListItem represents a single item in a KV list response.
type KVListItem struct {
	// Key is the string key
	Key string `json:"key"`

	// Value is the stored data
	Value json.RawMessage `json:"value"`

	// Timestamp is when the value was last modified
	Timestamp time.Time `json:"timestamp,omitempty"`
}

// KVListResponse represents the response from a KV List operation.
type KVListResponse struct {
	// EntityType is the type/namespace being listed
	EntityType string `json:"entity_type"`

	// Items is the list of key-value pairs
	Items []KVListItem `json:"items"`

	// Total is the total count of items (if available)
	Total int `json:"total,omitempty"`

	// NextCursor is the cursor for fetching next page
	NextCursor string `json:"next_cursor,omitempty"`

	// HasMore indicates if there are more results
	HasMore bool `json:"has_more"`
}

// =============================================================================
// Cluster Management Types
// =============================================================================

// AddNodeRequest represents a request to add a node to a shard.
type AddNodeRequest struct {
	// ShardID is the shard to add the node to
	ShardID uint64 `json:"shard_id"`

	// NodeID is the unique identifier for the new node
	NodeID uint64 `json:"node_id"`

	// Address is the Raft address of the new node (host:port)
	Address string `json:"address"`

	// Identity is the new node's Raft member identity (spki:<sha256-hex>,
	// as `qrdbd node-identity --cert` prints it). Optional when the cluster
	// has already committed one for the node or its host.
	Identity string `json:"identity,omitempty"`
}

// AddNodeResponse represents the response from adding a node.
type AddNodeResponse struct {
	// Success indicates if the node was added
	Success bool `json:"success"`

	// Message provides additional information
	Message string `json:"message"`

	// ShardID is the shard the node was added to
	ShardID uint64 `json:"shard_id"`

	// NodeID is the added node ID
	NodeID uint64 `json:"node_id"`

	// Timestamp is when the operation completed
	Timestamp time.Time `json:"timestamp"`
}

// RemoveNodeResponse represents the response from removing a node.
type RemoveNodeResponse struct {
	// Success indicates if the node was removed
	Success bool `json:"success"`

	// Message provides additional information
	Message string `json:"message"`

	// ShardID is the shard the node was removed from
	ShardID uint64 `json:"shard_id"`

	// NodeID is the removed node ID
	NodeID uint64 `json:"node_id"`

	// Timestamp is when the operation completed
	Timestamp time.Time `json:"timestamp"`
}

// ReplaceNodeRequest represents a request to replace a failed node.
type ReplaceNodeRequest struct {
	// ReplacementNodeID is the ID of the replacement node
	ReplacementNodeID uint64 `json:"replacement_node_id"`

	// ReplacementAddress is the address of the replacement node
	ReplacementAddress string `json:"replacement_address"`

	// ReplacementIdentity is the replacement node's Raft member identity
	// (spki:<sha256-hex>); optional when the cluster already committed one.
	ReplacementIdentity string `json:"replacement_identity,omitempty"`
}

// ReplaceNodeResponse represents the response from replacing a node.
type ReplaceNodeResponse struct {
	// Success indicates if the node was replaced
	Success bool `json:"success"`

	// Message provides additional information
	Message string `json:"message"`

	// AffectedShards lists shards where the node was replaced
	AffectedShards []uint64 `json:"affected_shards"`

	// Timestamp is when the operation completed
	Timestamp time.Time `json:"timestamp"`
}

// NodeInfoResponse represents detailed node information.
type NodeInfoResponse struct {
	// NodeID uniquely identifies the node
	NodeID uint64 `json:"node_id"`

	// Address is the network address (host:port)
	Address string `json:"address"`

	// RaftAddress is the Raft protocol address
	RaftAddress string `json:"raft_address"`

	// IsLeader indicates if this node is a cluster leader
	IsLeader bool `json:"is_leader"`

	// HealthStatus indicates node health
	HealthStatus string `json:"health_status"`

	// Version is the QuicBaaS version running on this node
	Version string `json:"version"`

	// UptimeSeconds is how long the node has been running
	UptimeSeconds uint64 `json:"uptime_seconds"`
}

// ListNodesResponse represents the response from listing nodes.
type ListNodesResponse struct {
	// Nodes is the list of all cluster nodes
	Nodes []NodeInfoResponse `json:"nodes"`

	// TotalCount is the total number of nodes
	TotalCount int `json:"total_count"`

	// Timestamp is when the list was generated
	Timestamp time.Time `json:"timestamp"`
}

// RebalanceRequest represents a request to start cluster rebalancing.
type RebalanceRequest struct {
	// Strategy is the rebalancing strategy (round_robin, consistent_hash)
	Strategy string `json:"strategy"`

	// TargetShardCount is the desired number of shards (optional)
	TargetShardCount uint64 `json:"target_shard_count,omitempty"`
}

// RebalanceResponse represents the response from starting rebalancing.
type RebalanceResponse struct {
	// OperationID uniquely identifies this rebalance operation
	OperationID string `json:"operation_id"`

	// Started is when the rebalance started
	Started time.Time `json:"started"`

	// Status is the current status
	Status string `json:"status"`

	// Timestamp is when the response was generated
	Timestamp time.Time `json:"timestamp"`
}

// RebalanceStatusResponse represents rebalance progress information.
type RebalanceStatusResponse struct {
	// OperationID is the rebalance operation ID
	OperationID string `json:"operation_id"`

	// Status is the rebalance status (running, completed, failed)
	Status string `json:"status"`

	// ShardsMoved is the number of shards moved
	ShardsMoved int32 `json:"shards_moved,omitempty"`

	// TotalShards is the total number of shards to move
	TotalShards int32 `json:"total_shards,omitempty"`

	// PercentComplete is the completion percentage
	PercentComplete float64 `json:"percent_complete"`

	// ElapsedTime is the elapsed time in nanoseconds
	ElapsedTime int64 `json:"elapsed_time"`

	// Timestamp is when the status was checked
	Timestamp time.Time `json:"timestamp"`
}

// RebalancePlanResponse represents a rebalancing plan.
type RebalancePlanResponse struct {
	// CurrentShards is the current number of shards
	CurrentShards int `json:"current_shards"`

	// TargetShards is the target number of shards
	TargetShards int `json:"target_shards"`

	// Strategy is the rebalancing strategy
	Strategy string `json:"strategy"`

	// EstimatedTime is estimated completion time in nanoseconds
	EstimatedTime int64 `json:"estimated_time"`

	// Timestamp is when the plan was generated
	Timestamp time.Time `json:"timestamp"`
}

// MigrationRequest represents a request to start data migration.
type MigrationRequest struct {
	// SourceShardID is the source shard
	SourceShardID uint64 `json:"source_shard_id"`

	// TargetShardID is the target shard
	TargetShardID uint64 `json:"target_shard_id"`

	// EntityTypes optionally filters which entity types to migrate
	EntityTypes []string `json:"entity_types,omitempty"`
}

// MigrationResponse represents the response from starting migration.
type MigrationResponse struct {
	// OperationID uniquely identifies this migration
	OperationID string `json:"operation_id"`

	// Status is the current status
	Status string `json:"status"`

	// SourceShardID is the source shard
	SourceShardID uint64 `json:"source_shard_id"`

	// TargetShardID is the target shard
	TargetShardID uint64 `json:"target_shard_id"`

	// EstimatedTime is estimated completion time in nanoseconds
	EstimatedTime int64 `json:"estimated_time"`

	// Timestamp is when the response was generated
	Timestamp time.Time `json:"timestamp"`
}

// MigrationStatusResponse represents migration progress information.
type MigrationStatusResponse struct {
	// OperationID is the migration operation ID
	OperationID string `json:"operation_id"`

	// Status is the migration status (running, completed, failed, not_found)
	Status string `json:"status"`

	// KeysProcessed is the number of keys processed
	KeysProcessed uint64 `json:"keys_processed"`

	// BytesProcessed is the number of bytes processed
	BytesProcessed uint64 `json:"bytes_processed"`

	// EstimatedTotal is the estimated total keys
	EstimatedTotal uint64 `json:"estimated_total,omitempty"`

	// PercentComplete is the completion percentage
	PercentComplete float64 `json:"percent_complete"`

	// ElapsedTime is the elapsed time in nanoseconds
	ElapsedTime int64 `json:"elapsed_time"`

	// EstimatedRemaining is the estimated remaining time in nanoseconds
	EstimatedRemaining int64 `json:"estimated_remaining,omitempty"`

	// CurrentRate is the current processing rate (keys/sec)
	CurrentRate float64 `json:"current_rate,omitempty"`

	// Timestamp is when the status was checked
	Timestamp time.Time `json:"timestamp"`
}

// CancelMigrationResponse represents the response from cancelling migration.
type CancelMigrationResponse struct {
	// OperationID is the migration operation ID
	OperationID string `json:"operation_id"`

	// Cancelled indicates if the migration was cancelled
	Cancelled bool `json:"cancelled"`

	// Message provides additional information
	Message string `json:"message"`

	// Timestamp is when the cancellation occurred
	Timestamp time.Time `json:"timestamp"`
}

// UpgradeRequest represents a request to start a rolling upgrade.
type UpgradeRequest struct {
	// TargetVersion is the version to upgrade to
	TargetVersion string `json:"target_version"`

	// NodeID optionally specifies a single node to upgrade (0 = all nodes)
	NodeID uint64 `json:"node_id,omitempty"`
}

// UpgradeResponse represents the response from starting an upgrade.
type UpgradeResponse struct {
	// OperationID uniquely identifies this upgrade
	OperationID string `json:"operation_id"`

	// Status is the current status
	Status string `json:"status"`

	// TargetVersion is the version to upgrade to
	TargetVersion string `json:"target_version"`

	// Timestamp is when the response was generated
	Timestamp time.Time `json:"timestamp"`
}

// UpgradeStatusResponse represents upgrade progress information.
type UpgradeStatusResponse struct {
	// OperationID is the upgrade operation ID
	OperationID string `json:"operation_id"`

	// Status is the upgrade status (running, completed, failed, not_running)
	Status string `json:"status"`

	// NodesUpgraded is the number of nodes upgraded
	NodesUpgraded int32 `json:"nodes_upgraded"`

	// TotalNodes is the total number of nodes to upgrade
	TotalNodes int32 `json:"total_nodes"`

	// PercentComplete is the completion percentage
	PercentComplete float64 `json:"percent_complete"`

	// CurrentVersion is the current version
	CurrentVersion string `json:"current_version"`

	// TargetVersion is the target version
	TargetVersion string `json:"target_version"`

	// Timestamp is when the status was checked
	Timestamp time.Time `json:"timestamp"`
}
