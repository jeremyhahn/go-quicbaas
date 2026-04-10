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

// Package cli provides output formatting utilities for the QuicBaaS CLI.
package cli

import "time"

// =============================================================================
// KV Response Types (used by output formatters)
// =============================================================================

// PutResponse represents a put operation response.
type PutResponse struct {
	Key       string    `json:"key"`
	Success   bool      `json:"success"`
	Message   string    `json:"message,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// GetResponse represents a get operation response.
type GetResponse struct {
	Key       string    `json:"key"`
	Value     []byte    `json:"value,omitempty"`
	Found     bool      `json:"found"`
	Timestamp time.Time `json:"timestamp"`
}

// DeleteResponse represents a delete operation response.
type DeleteResponse struct {
	Key       string    `json:"key"`
	Deleted   bool      `json:"deleted"`
	Message   string    `json:"message,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// ListItem represents a single item in a list response.
type ListItem struct {
	Key       string    `json:"key"`
	Value     []byte    `json:"value,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// ListResponse represents a list operation response.
type ListResponse struct {
	Items      []ListItem `json:"items"`
	NextCursor string     `json:"next_cursor,omitempty"`
	Total      int        `json:"total"`
}

// QueryItem represents a single item in a query response.
type QueryItem struct {
	Key       string                 `json:"key"`
	Value     []byte                 `json:"value,omitempty"`
	IndexedBy map[string]interface{} `json:"indexed_by,omitempty"`
	Timestamp time.Time              `json:"timestamp"`
}

// QueryResponse represents a query operation response.
type QueryResponse struct {
	Items []QueryItem `json:"items"`
	Total int         `json:"total"`
}

// =============================================================================
// Cluster Types
// =============================================================================

// NodeInfo represents information about a cluster node.
type NodeInfo struct {
	NodeID    uint64    `json:"node_id"`
	Address   string    `json:"address"`
	IsLeader  bool      `json:"is_leader"`
	IsHealthy bool      `json:"is_healthy"`
	LastSeen  time.Time `json:"last_seen"`
}

// RaftGroupInfo represents information about a Raft group.
type RaftGroupInfo struct {
	GroupID      uint64 `json:"group_id"`
	LeaderID     uint64 `json:"leader_id"`
	MemberCount  int    `json:"member_count"`
	LogIndex     uint64 `json:"log_index"`
	CommitIndex  uint64 `json:"commit_index"`
	AppliedIndex uint64 `json:"applied_index"`
}

// RaftGroupStatus is an alias for RaftGroupInfo for backward compatibility.
type RaftGroupStatus = RaftGroupInfo

// ClusterStatusResponse represents cluster status information.
type ClusterStatusResponse struct {
	Healthy       bool            `json:"healthy"`
	LeaderID      uint64          `json:"leader_id"`
	NodeCount     int             `json:"node_count"`
	ReadyNodes    int             `json:"ready_nodes"`
	Nodes         []NodeInfo      `json:"nodes,omitempty"`
	RaftGroupInfo []RaftGroupInfo `json:"raft_groups,omitempty"`
}

// =============================================================================
// Backup Types
// =============================================================================

// BackupInfo represents information about a backup.
type BackupInfo struct {
	ID          string    `json:"id"`
	Type        string    `json:"type"`
	Size        int64     `json:"size"`
	Compression string    `json:"compression"`
	Encryption  string    `json:"encryption"`
	NumShards   int       `json:"num_shards"`
	NumKeys     int64     `json:"num_keys"`
	Timestamp   time.Time `json:"timestamp"`
}

// BackupCreateResponse represents a backup creation response.
type BackupCreateResponse struct {
	BackupID    string        `json:"backup_id"`
	Type        string        `json:"type"`
	Size        int64         `json:"size"`
	Compression string        `json:"compression"`
	Encryption  string        `json:"encryption"`
	Duration    time.Duration `json:"duration"`
	Timestamp   time.Time     `json:"timestamp"`
	Success     bool          `json:"success"`
	Message     string        `json:"message,omitempty"`
}

// BackupListResponse represents a backup list response.
type BackupListResponse struct {
	Backups     []BackupInfo `json:"backups"`
	TotalCount  int          `json:"total_count"`
	StoragePath string       `json:"storage_path"`
}

// BackupRestoreResponse represents a backup restore response.
type BackupRestoreResponse struct {
	BackupID       string        `json:"backup_id"`
	KeysRestored   int64         `json:"keys_restored"`
	ShardsRestored int           `json:"shards_restored"`
	Duration       time.Duration `json:"duration"`
	Timestamp      time.Time     `json:"timestamp"`
	Success        bool          `json:"success"`
	Message        string        `json:"message,omitempty"`
}

// BackupDeleteResponse represents a backup deletion response.
type BackupDeleteResponse struct {
	BackupID  string    `json:"backup_id"`
	Deleted   bool      `json:"deleted"`
	Message   string    `json:"message,omitempty"`
	Timestamp time.Time `json:"timestamp"`
}

// BackupVerifyResponse represents a backup verification response.
type BackupVerifyResponse struct {
	BackupID      string    `json:"backup_id"`
	Valid         bool      `json:"valid"`
	ChecksumValid bool      `json:"checksum_valid"`
	ShardsValid   int       `json:"shards_valid"`
	Issues        []string  `json:"issues,omitempty"`
	Timestamp     time.Time `json:"timestamp"`
}

// BackupStatusResponse represents backup operation status.
type BackupStatusResponse struct {
	BackupID       string        `json:"backup_id,omitempty"`
	State          string        `json:"state"`
	Progress       int           `json:"progress"`
	ShardsComplete int           `json:"shards_complete"`
	ShardsTotal    int           `json:"shards_total"`
	BytesComplete  int64         `json:"bytes_complete"`
	BytesTotal     int64         `json:"bytes_total"`
	StartTime      time.Time     `json:"start_time,omitempty"`
	ETA            time.Duration `json:"eta,omitempty"`
	Valid          bool          `json:"valid"`
	ChecksumValid  bool          `json:"checksum_valid"`
	Issues         []string      `json:"issues,omitempty"`
	Message        string        `json:"message,omitempty"`
	Timestamp      time.Time     `json:"timestamp"`
}

// =============================================================================
// Cache Types
// =============================================================================

// CacheStats represents cache statistics.
type CacheStats struct {
	Enabled    bool    `json:"enabled"`
	L1Size     int     `json:"l1_size"`
	L1Capacity int     `json:"l1_capacity"`
	L1Hits     int64   `json:"l1_hits"`
	L1Misses   int64   `json:"l1_misses"`
	L1HitRate  float64 `json:"l1_hit_rate"`
}

// CacheEntityStats represents cache statistics for an entity type.
type CacheEntityStats struct {
	EntityType string     `json:"entity_type"`
	Enabled    bool       `json:"enabled"`
	Stats      CacheStats `json:"stats"`
}

// CacheStatsResponse represents cache statistics for a single entity.
type CacheStatsResponse struct {
	EntityType  string     `json:"entity_type"`
	Enabled     bool       `json:"enabled"`
	Stats       CacheStats `json:"stats"`
	ShowDetails bool       `json:"show_details,omitempty"`
}

// CacheAllStatsResponse represents cache statistics for all entities.
type CacheAllStatsResponse struct {
	EntityStats map[string]CacheEntityStats `json:"entity_stats"`
	ShowDetails bool                        `json:"show_details,omitempty"`
}

// CacheClearResponse represents a cache clear operation response.
type CacheClearResponse struct {
	EntityType     string        `json:"entity_type,omitempty"`
	EntriesCleared int           `json:"entries_cleared"`
	Duration       time.Duration `json:"duration"`
	Timestamp      time.Time     `json:"timestamp"`
	Success        bool          `json:"success"`
	Message        string        `json:"message,omitempty"`
}

// CacheEntityConfig represents cache configuration for an entity type.
type CacheEntityConfig struct {
	EntityType string        `json:"entity_type"`
	Enabled    bool          `json:"enabled"`
	TTL        time.Duration `json:"ttl"`
	MaxSize    int           `json:"max_size"`
}

// CacheConfigResponse represents cache configuration for a single entity.
type CacheConfigResponse struct {
	EntityType string        `json:"entity_type"`
	Enabled    bool          `json:"enabled"`
	TTL        time.Duration `json:"ttl"`
	MaxSize    int           `json:"max_size"`
}

// CacheAllConfigResponse represents cache configuration for all entities.
type CacheAllConfigResponse struct {
	GlobalEnabled   bool                         `json:"global_enabled"`
	DefaultTTL      time.Duration                `json:"default_ttl"`
	MaxSize         int                          `json:"max_size"`
	CleanupInterval time.Duration                `json:"cleanup_interval"`
	EntityConfigs   map[string]CacheEntityConfig `json:"entity_configs"`
}

// CacheEnableResponse represents a cache enable operation response.
type CacheEnableResponse struct {
	EntityType string    `json:"entity_type"`
	Enabled    bool      `json:"enabled"`
	Timestamp  time.Time `json:"timestamp"`
	Success    bool      `json:"success"`
	Message    string    `json:"message,omitempty"`
}

// CacheDisableResponse represents a cache disable operation response.
type CacheDisableResponse struct {
	EntityType string    `json:"entity_type"`
	Enabled    bool      `json:"enabled"`
	Timestamp  time.Time `json:"timestamp"`
	Success    bool      `json:"success"`
	Message    string    `json:"message,omitempty"`
}

// CacheWarmResponse represents a cache warm operation response.
type CacheWarmResponse struct {
	Prefixes      []string      `json:"prefixes,omitempty"`
	State         string        `json:"state"`
	StartedAt     time.Time     `json:"started_at,omitempty"`
	CompletedAt   time.Time     `json:"completed_at,omitempty"`
	KeysWarmed    int64         `json:"keys_warmed"`
	KeysFailed    int64         `json:"keys_failed"`
	CurrentPrefix string        `json:"current_prefix,omitempty"`
	Duration      time.Duration `json:"duration"`
	Timestamp     time.Time     `json:"timestamp"`
	Success       bool          `json:"success"`
	Message       string        `json:"message,omitempty"`
}

// =============================================================================
// TimeSeries Types
// =============================================================================

// WritePointRequest represents a time series write request.
type WritePointRequest struct {
	Metric    string            `json:"metric"`
	Tags      map[string]string `json:"tags,omitempty"`
	Value     float64           `json:"value"`
	Timestamp time.Time         `json:"timestamp"`
}

// WritePointResponse represents a time series write response.
type WritePointResponse struct {
	Success   bool      `json:"success"`
	Timestamp time.Time `json:"timestamp"`
}

// WriteBatchRequest represents a time series batch write request.
type WriteBatchRequest struct {
	Points []WritePointRequest `json:"points"`
}

// WriteBatchResponse represents a time series batch write response.
type WriteBatchResponse struct {
	Success       bool      `json:"success"`
	PointsWritten int       `json:"points_written"`
	Timestamp     time.Time `json:"timestamp"`
}

// TSDataPoint represents a time series data point.
type TSDataPoint struct {
	Metric    string            `json:"metric,omitempty"`
	Timestamp time.Time         `json:"timestamp"`
	Value     float64           `json:"value"`
	Tags      map[string]string `json:"tags,omitempty"`
}

// TSQueryRequest represents a time series query request.
type TSQueryRequest struct {
	Metric      string            `json:"metric"`
	Tags        map[string]string `json:"tags,omitempty"`
	StartTime   time.Time         `json:"start_time"`
	EndTime     time.Time         `json:"end_time"`
	Aggregation string            `json:"aggregation,omitempty"`
	IntervalSec int32             `json:"interval_sec,omitempty"`
	GroupBy     []string          `json:"group_by,omitempty"`
	Limit       int               `json:"limit,omitempty"`
	Percentile  float64           `json:"percentile,omitempty"`
}

// TSQueryResponse represents a time series query response.
type TSQueryResponse struct {
	Metric          string            `json:"metric"`
	Tags            map[string]string `json:"tags,omitempty"`
	Points          []TSDataPoint     `json:"points"`
	GroupedResults  []TSGroupedResult `json:"grouped_results,omitempty"`
	AggregatedValue *float64          `json:"aggregated_value,omitempty"`
	Timestamp       time.Time         `json:"timestamp"`
}

// TSGroupedResult represents grouped query results.
type TSGroupedResult struct {
	Tags            map[string]string `json:"tags"`
	Points          []TSDataPoint     `json:"points,omitempty"`
	AggregatedValue *float64          `json:"aggregated_value,omitempty"`
}

// TSDeleteRequest represents a time series delete request.
type TSDeleteRequest struct {
	Metric    string            `json:"metric"`
	Tags      map[string]string `json:"tags,omitempty"`
	StartTime *time.Time        `json:"start_time,omitempty"`
	EndTime   *time.Time        `json:"end_time,omitempty"`
}

// TSDeleteResponse represents a time series delete response.
type TSDeleteResponse struct {
	Success   bool      `json:"success"`
	Timestamp time.Time `json:"timestamp"`
}

// TSRetentionPolicy represents a time series retention policy.
type TSRetentionPolicy struct {
	Duration    time.Duration `json:"duration"`
	Resolution  time.Duration `json:"resolution"`
	Aggregation string        `json:"aggregation"`
}

// TSGetRetentionPolicyRequest represents a get retention policy request.
type TSGetRetentionPolicyRequest struct {
	Metric string `json:"metric"`
}

// TSGetRetentionPolicyResponse represents a get retention policy response.
type TSGetRetentionPolicyResponse struct {
	Metric    string             `json:"metric"`
	Policy    *TSRetentionPolicy `json:"policy,omitempty"`
	Timestamp time.Time          `json:"timestamp"`
}

// TSSetRetentionPolicyRequest represents a set retention policy request.
type TSSetRetentionPolicyRequest struct {
	Metric string             `json:"metric"`
	Policy *TSRetentionPolicy `json:"policy"`
}

// TSSetRetentionPolicyResponse represents a set retention policy response.
type TSSetRetentionPolicyResponse struct {
	Success   bool      `json:"success"`
	Timestamp time.Time `json:"timestamp"`
}

// TSListMetricsRequest represents a list metrics request.
type TSListMetricsRequest struct {
	Prefix string            `json:"prefix,omitempty"`
	Tags   map[string]string `json:"tags,omitempty"`
	Limit  int               `json:"limit,omitempty"`
	Cursor string            `json:"cursor,omitempty"`
}

// TSListMetricsResponse represents a list metrics response.
type TSListMetricsResponse struct {
	Metrics    []string  `json:"metrics"`
	NextCursor string    `json:"next_cursor,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// =============================================================================
// IAM Federation Types
// =============================================================================

// Provider represents an external identity provider.
type Provider struct {
	ID            string    `json:"id"`
	TenantID      string    `json:"tenant_id"`
	Name          string    `json:"name"`
	Type          string    `json:"type"` // oidc, oauth2, saml
	Enabled       bool      `json:"enabled"`
	ClientID      string    `json:"client_id,omitempty"`
	IssuerURL     string    `json:"issuer_url,omitempty"`
	Scopes        []string  `json:"scopes,omitempty"`
	AutoProvision bool      `json:"auto_provision"`
	AllowLinking  bool      `json:"allow_linking"`
	DefaultRoles  []string  `json:"default_roles,omitempty"`
	DefaultGroups []string  `json:"default_groups,omitempty"`
	Created       time.Time `json:"created"`
	Updated       time.Time `json:"updated"`
}

// CreateProviderOptions represents options for creating an identity provider.
type CreateProviderOptions struct {
	TenantID      string   `json:"tenant_id"`
	ProviderID    string   `json:"provider_id"`
	Name          string   `json:"name"`
	Type          string   `json:"type"` // oidc, oauth2, saml
	ClientID      string   `json:"client_id,omitempty"`
	ClientSecret  string   `json:"client_secret,omitempty"`
	IssuerURL     string   `json:"issuer_url,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
	AutoProvision bool     `json:"auto_provision"`
	AllowLinking  bool     `json:"allow_linking"`
	DefaultRoles  []string `json:"default_roles,omitempty"`
	DefaultGroups []string `json:"default_groups,omitempty"`
}

// UpdateProviderOptions represents options for updating an identity provider.
type UpdateProviderOptions struct {
	TenantID      string   `json:"tenant_id"`
	ProviderID    string   `json:"provider_id"`
	Name          *string  `json:"name,omitempty"`
	Enabled       *bool    `json:"enabled,omitempty"`
	ClientID      *string  `json:"client_id,omitempty"`
	ClientSecret  *string  `json:"client_secret,omitempty"`
	IssuerURL     *string  `json:"issuer_url,omitempty"`
	Scopes        []string `json:"scopes,omitempty"`
	AutoProvision *bool    `json:"auto_provision,omitempty"`
	AllowLinking  *bool    `json:"allow_linking,omitempty"`
	DefaultRoles  []string `json:"default_roles,omitempty"`
	DefaultGroups []string `json:"default_groups,omitempty"`
}

// ListProvidersResponse represents a list of providers.
type ListProvidersResponse struct {
	Providers []Provider `json:"providers"`
}

// FederatedIdentity represents a linked external identity.
type FederatedIdentity struct {
	ID            string    `json:"id"`
	UserID        string    `json:"user_id"`
	TenantID      string    `json:"tenant_id"`
	ProviderID    string    `json:"provider_id"`
	ExternalID    string    `json:"external_id"`
	ExternalEmail string    `json:"external_email,omitempty"`
	ExternalName  string    `json:"external_name,omitempty"`
	Created       time.Time `json:"created"`
	LastLogin     time.Time `json:"last_login,omitempty"`
}

// LinkIdentityOptions represents options for linking an external identity.
type LinkIdentityOptions struct {
	TenantID   string `json:"tenant_id"`
	UserID     string `json:"user_id"`
	ProviderID string `json:"provider_id"`
	AuthCode   string `json:"auth_code"`
}

// =============================================================================
// IAM MFA Types
// =============================================================================

// MFAStatus represents the MFA status for a user.
type MFAStatus struct {
	Enabled              bool       `json:"enabled"`
	Method               string     `json:"method,omitempty"`
	EnrolledAt           *time.Time `json:"enrolled_at,omitempty"`
	LastVerified         *time.Time `json:"last_verified,omitempty"`
	BackupCodesRemaining int        `json:"backup_codes_remaining"`
}

// MFAEnrollment represents the result of beginning MFA enrollment.
type MFAEnrollment struct {
	EnrollmentID    string    `json:"enrollment_id"`
	Secret          string    `json:"secret"`
	ProvisioningURI string    `json:"provisioning_uri"`
	ExpiresAt       time.Time `json:"expires_at"`
}

// BackupCodeStatus represents the backup code status for a user.
type BackupCodeStatus struct {
	RemainingCodes int        `json:"remaining_codes"`
	TotalCodes     int        `json:"total_codes"`
	GeneratedAt    *time.Time `json:"generated_at,omitempty"`
}
