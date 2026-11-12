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

// Package service provides the unified service facade for QuicBaaS.
// This file contains KV service request/response types.
package service

// =============================================================================
// KV Operations Request/Response Types
// =============================================================================

// PutRequest represents a put operation request.
// It contains all parameters needed to store a key-value pair.
type PutRequest struct {
	// EntityType is the logical type/namespace for the entity (e.g., "users", "orders").
	EntityType string `json:"entity_type"`

	// ID is the unique numeric identifier for the entity within its type.
	ID uint64 `json:"id,omitempty"`

	// Data contains the serialized entity payload.
	Data []byte `json:"data"`

	// Consistency specifies the write consistency level.
	// Default: ConsistencyLevelLinearizable
	Consistency ConsistencyLevel `json:"consistency,omitempty"`

	// ShardID specifies the target shard (0 = auto-route based on key).
	ShardID uint64 `json:"shard_id,omitempty"`

	// WriteMode specifies the durability guarantee for this write.
	// Default: WriteModeSync
	WriteMode WriteMode `json:"write_mode,omitempty"`
}

// PutResponse represents a put operation response.
// It contains metadata about the stored entity.
type PutResponse struct {
	// ID is the entity identifier (may be auto-generated if not provided).
	ID uint64 `json:"id"`

	// Version is the MVCC version number after this write.
	Version uint64 `json:"version"`

	// Timestamp is when the write was committed (Unix nanoseconds).
	Timestamp int64 `json:"timestamp"`

	// LeaderID is the Raft leader that processed this write.
	LeaderID uint64 `json:"leader_id,omitempty"`
}

// CompareAndSwapRequest atomically replaces an entity's value when its stored
// value equals Expect. The entity must exist: an absent entity never matches,
// even when Expect is empty. Use PutIfAbsentRequest to create an entity.
type CompareAndSwapRequest struct {
	// EntityType is the logical type/namespace for the entity.
	EntityType string `json:"entity_type"`

	// ID is the entity identifier within its type.
	ID uint64 `json:"id,omitempty"`

	// Expect is the value the entity must currently hold.
	Expect []byte `json:"expect"`

	// Set is the value stored when Expect matches.
	Set []byte `json:"set"`

	// ShardID optionally specifies which shard to target.
	// If zero, defaults to shard 1.
	ShardID uint64 `json:"shard_id,omitempty"`
}

// PutIfAbsentRequest atomically stores an entity only when it does not exist.
type PutIfAbsentRequest struct {
	// EntityType is the logical type/namespace for the entity.
	EntityType string `json:"entity_type"`

	// ID is the entity identifier within its type.
	ID uint64 `json:"id,omitempty"`

	// Data is the value stored when the entity is absent.
	Data []byte `json:"data"`

	// ShardID optionally specifies which shard to target.
	// If zero, defaults to shard 1.
	ShardID uint64 `json:"shard_id,omitempty"`
}

// ConditionalPutResponse reports the outcome of a conditional write
// (CompareAndSwap or PutIfAbsent). A failed precondition is a normal outcome,
// reported as Applied == false with no error.
type ConditionalPutResponse struct {
	// Applied is true when the precondition held and the write was stored.
	Applied bool `json:"applied"`
}

// GetRequest represents a get operation request.
// It contains parameters to retrieve a specific entity.
type GetRequest struct {
	// EntityType is the logical type/namespace for the entity.
	EntityType string `json:"entity_type"`

	// ID is the unique numeric identifier for the entity.
	ID uint64 `json:"id,omitempty"`

	// Consistency specifies the read consistency level.
	// Default: ConsistencyLevelLinearizable
	Consistency ConsistencyLevel `json:"consistency,omitempty"`

	// ShardID specifies the target shard (0 = auto-route based on key).
	ShardID uint64 `json:"shard_id,omitempty"`

	// TimeoutMs is the operation timeout in milliseconds (0 = default).
	TimeoutMs uint64 `json:"timeout_ms,omitempty"`
}

// GetResponse represents a get operation response.
// It contains the retrieved entity and its metadata.
type GetResponse struct {
	// ID is the entity identifier.
	ID uint64 `json:"id"`

	// Data contains the serialized entity payload.
	Data []byte `json:"data,omitempty"`

	// Version is the MVCC version number of the entity.
	Version uint64 `json:"version"`

	// Timestamp is when the entity was last modified (Unix nanoseconds).
	Timestamp int64 `json:"timestamp"`

	// Found indicates whether the entity was found.
	Found bool `json:"found"`
}

// DeleteRequest represents a delete operation request.
// It contains parameters to remove a specific entity.
type DeleteRequest struct {
	// EntityType is the logical type/namespace for the entity.
	EntityType string `json:"entity_type"`

	// ID is the unique numeric identifier for the entity.
	ID uint64 `json:"id,omitempty"`

	// Consistency specifies the write consistency level.
	// Default: ConsistencyLevelLinearizable
	Consistency ConsistencyLevel `json:"consistency,omitempty"`

	// ShardID specifies the target shard (0 = auto-route based on key).
	ShardID uint64 `json:"shard_id,omitempty"`
}

// DeleteResponse represents a delete operation response.
// It contains the result of the delete operation.
type DeleteResponse struct {
	// Deleted indicates whether an entity was actually deleted.
	Deleted bool `json:"deleted"`

	// Timestamp is when the delete was committed (Unix nanoseconds).
	Timestamp int64 `json:"timestamp"`
}

// ListRequest represents a list operation request.
// It contains parameters for paginated entity listing.
type ListRequest struct {
	// EntityType is the logical type/namespace to list.
	EntityType string `json:"entity_type"`

	// Consistency specifies the read consistency level.
	Consistency ConsistencyLevel `json:"consistency,omitempty"`

	// PageSize is the maximum number of entities to return per page.
	PageSize uint64 `json:"page_size,omitempty"`

	// PageToken is the pagination cursor for subsequent pages.
	PageToken string `json:"page_token,omitempty"`

	// ShardID specifies the target shard (0 = all shards).
	ShardID uint64 `json:"shard_id,omitempty"`
}

// ListResponse represents a list operation response.
// It contains a page of entities and pagination metadata.
type ListResponse struct {
	// Entities contains the entities in this page.
	Entities []*Entity `json:"entities,omitempty"`

	// NextPageToken is the cursor for the next page (empty if no more pages).
	NextPageToken string `json:"next_page_token,omitempty"`

	// TotalCount is the total number of entities matching the query.
	TotalCount uint64 `json:"total_count,omitempty"`
}

// Entity represents a stored entity.
// It contains the entity data and associated metadata.
type Entity struct {
	// EntityType is the logical type/namespace for this entity.
	EntityType string `json:"entity_type,omitempty"`

	// ID is the unique numeric identifier.
	ID uint64 `json:"id"`

	// Data contains the serialized entity payload.
	Data []byte `json:"data,omitempty"`

	// Version is the MVCC version number.
	Version uint64 `json:"version"`

	// Timestamp is when the entity was last modified (Unix nanoseconds).
	Timestamp int64 `json:"timestamp"`
}

// ListEntityTypesRequest contains parameters for listing entity types.
// It supports pagination for databases with many entity types.
type ListEntityTypesRequest struct {
	// PageSize is the maximum number of entity types to return.
	PageSize int `json:"page_size,omitempty"`

	// PageToken is the pagination cursor (empty for first page).
	PageToken string `json:"page_token,omitempty"`

	// ShardID specifies the shard to query (0 = all shards).
	ShardID uint64 `json:"shard_id,omitempty"`
}

// ListEntityTypesResponse contains the discovered entity types.
// It provides the unique entity type names found in the database.
type ListEntityTypesResponse struct {
	// EntityTypes contains the unique entity type names found.
	EntityTypes []string `json:"entity_types"`

	// NextPageToken is the cursor for the next page (empty if no more).
	NextPageToken string `json:"next_page_token,omitempty"`

	// TotalCount is the total number of entity types.
	TotalCount int `json:"total_count,omitempty"`
}

// =============================================================================
// Watch Operations Types
// =============================================================================

// WatchEventType represents the type of watch event.
type WatchEventType int

// WatchEventType constants define the possible watch event types.
const (
	// WatchEventTypeUnspecified is the default/unset value.
	WatchEventTypeUnspecified WatchEventType = 0

	// WatchEventTypePut indicates a key was created or updated.
	WatchEventTypePut WatchEventType = 1

	// WatchEventTypeDelete indicates a key was deleted.
	WatchEventTypeDelete WatchEventType = 2
)

// WatchRequest contains parameters for a watch operation.
// It specifies the key or prefix to monitor and watch options.
type WatchRequest struct {
	// Key is the key or prefix to watch.
	Key []byte `json:"key"`

	// IsPrefix indicates whether to watch all keys with this prefix.
	IsPrefix bool `json:"is_prefix,omitempty"`

	// StartRevision is the MVCC revision to start watching from (0 = current).
	StartRevision int64 `json:"start_revision,omitempty"`

	// PrevKV requests inclusion of previous key-value in events.
	PrevKV bool `json:"prev_kv,omitempty"`

	// ProgressNotify requests periodic progress notifications.
	ProgressNotify bool `json:"progress_notify,omitempty"`

	// Filters specifies event types to filter OUT (exclude from results).
	Filters []WatchEventType `json:"filters,omitempty"`

	// WatchID is a client-specified watch identifier (0 = auto-assign).
	WatchID int64 `json:"watch_id,omitempty"`
}

// WatchEvent represents a single change event.
// It contains details about the modification that occurred.
type WatchEvent struct {
	// Type indicates whether this was a PUT or DELETE.
	Type WatchEventType `json:"type"`

	// Key is the key that was affected.
	Key []byte `json:"key"`

	// Value is the new value (nil for deletes).
	Value []byte `json:"value,omitempty"`

	// PrevValue is the previous value (if PrevKV was requested).
	PrevValue []byte `json:"prev_value,omitempty"`

	// Version is the key version after this event.
	Version uint64 `json:"version"`

	// Revision is the MVCC revision of this event.
	Revision int64 `json:"revision"`

	// Timestamp is when the event occurred (Unix nanoseconds).
	Timestamp int64 `json:"timestamp"`
}

// WatchResponse contains watch events sent to the client.
// It provides event batches and watch status information.
type WatchResponse struct {
	// WatchID is the watch subscription identifier.
	WatchID int64 `json:"watch_id"`

	// Created is true for the initial creation confirmation message.
	Created bool `json:"created,omitempty"`

	// Canceled is true if the watch was canceled.
	Canceled bool `json:"canceled,omitempty"`

	// CancelReason provides the reason for cancellation.
	CancelReason string `json:"cancel_reason,omitempty"`

	// CompactRevision is the minimum revision after compaction.
	CompactRevision int64 `json:"compact_revision,omitempty"`

	// Events contains the watch events in this response.
	Events []*WatchEvent `json:"events,omitempty"`

	// Revision is the current MVCC revision.
	Revision int64 `json:"revision"`
}
