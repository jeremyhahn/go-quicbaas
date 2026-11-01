package iam

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeltaLogAppend tests appending events to the delta log.
func TestDeltaLogAppend(t *testing.T) {
	log := NewDeltaLog(100)

	event := &EnhancedPolicyChangeEvent{
		EventID:    uuid.New().String(),
		Type:       PolicyUpdateCreated,
		TenantID:   "tenant-1",
		PolicyName: "policy-1",
		Version:    1,
		Timestamp:  time.Now(),
		NodeID:     "node-1",
	}

	err := log.Append(event)
	assert.NoError(t, err)
	assert.Equal(t, 1, log.Size())
	assert.Equal(t, int64(1), log.GetMinVersion())
	assert.Equal(t, int64(1), log.GetMaxVersion())
}

// TestDeltaLogAppendNil tests appending nil events.
func TestDeltaLogAppendNil(t *testing.T) {
	log := NewDeltaLog(100)

	err := log.Append(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is nil")
}

// TestDeltaLogGetSince tests retrieving events since a specific version.
func TestDeltaLogGetSince(t *testing.T) {
	log := NewDeltaLog(100)

	// Add multiple events
	for i := 1; i <= 5; i++ {
		event := &EnhancedPolicyChangeEvent{
			EventID:    uuid.New().String(),
			Type:       PolicyUpdateCreated,
			TenantID:   "tenant-1",
			PolicyName: "policy-1",
			Version:    int64(i),
			Timestamp:  time.Now(),
			NodeID:     "node-1",
		}
		err := log.Append(event)
		require.NoError(t, err)
	}

	// Get events since version 2
	events, err := log.GetSince("tenant-1", 2)
	assert.NoError(t, err)
	assert.Len(t, events, 3) // versions 3, 4, 5
}

// TestDeltaLogGetSinceNoChanges tests retrieving events when there are no changes.
func TestDeltaLogGetSinceNoChanges(t *testing.T) {
	log := NewDeltaLog(100)

	events, err := log.GetSince("tenant-1", 0)
	assert.NoError(t, err)
	assert.Nil(t, events)
}

// TestDeltaLogGetSinceOldVersion tests retrieving events from an old version.
func TestDeltaLogGetSinceOldVersion(t *testing.T) {
	log := NewDeltaLog(10) // Small log for testing

	// Add events that will wrap around
	for i := 1; i <= 15; i++ {
		event := &EnhancedPolicyChangeEvent{
			EventID:    uuid.New().String(),
			Type:       PolicyUpdateCreated,
			TenantID:   "tenant-1",
			PolicyName: "policy-1",
			Version:    int64(i),
			Timestamp:  time.Now(),
			NodeID:     "node-1",
		}
		err := log.Append(event)
		require.NoError(t, err)
	}

	// Try to get events from version 1 (which is now out of range)
	events, err := log.GetSince("tenant-1", 1)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "older than available minimum version")
	assert.Nil(t, events)
}

// TestBatchProcessorAddNil tests adding a nil event.
func TestBatchProcessorAddNil(t *testing.T) {
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	syncer := &mockPolicyCacheSyncer{}

	bp, err := NewBatchProcessor(config, syncer)
	require.NoError(t, err)
	defer bp.Close()

	err = bp.Add(nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "is nil")
}

// TestBatchProcessorFlush tests flushing the batch.
func TestBatchProcessorFlush(t *testing.T) {
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 10
	config.BatchInterval = 1 * time.Hour // Long interval to test manual flush

	syncer := &mockPolicyCacheSyncer{
		notifyCalled: make(chan PolicyChangeEvent, 10),
	}

	bp, err := NewBatchProcessor(config, syncer)
	require.NoError(t, err)
	defer bp.Close()

	// Add events
	for i := 1; i <= 5; i++ {
		event := &EnhancedPolicyChangeEvent{
			EventID:    uuid.New().String(),
			Type:       PolicyUpdateCreated,
			TenantID:   "tenant-1",
			PolicyName: "policy-1",
			Version:    int64(i),
			Timestamp:  time.Now(),
			NodeID:     "node-1",
		}
		err = bp.Add(event)
		require.NoError(t, err)
	}

	// Flush manually
	err = bp.Flush()
	assert.NoError(t, err)

	// Verify that notify was called
	select {
	case <-syncer.notifyCalled:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("notify was not called")
	}
}

// TestBatchProcessorAutoFlush tests automatic flushing when batch is full.
func TestBatchProcessorAutoFlush(t *testing.T) {
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 3 // Small batch for testing
	config.BatchInterval = 1 * time.Hour

	syncer := &mockPolicyCacheSyncer{
		notifyCalled: make(chan PolicyChangeEvent, 10),
	}

	bp, err := NewBatchProcessor(config, syncer)
	require.NoError(t, err)
	defer bp.Close()

	// Add events to trigger auto-flush
	for i := 1; i <= 3; i++ {
		event := &EnhancedPolicyChangeEvent{
			EventID:    uuid.New().String(),
			Type:       PolicyUpdateCreated,
			TenantID:   "tenant-1",
			PolicyName: "policy-1",
			Version:    int64(i),
			Timestamp:  time.Now(),
			NodeID:     "node-1",
		}
		err = bp.Add(event)
		require.NoError(t, err)
	}

	// Verify that notify was called
	select {
	case <-syncer.notifyCalled:
		// Success
	case <-time.After(1 * time.Second):
		t.Fatal("auto-flush did not trigger")
	}
}

// TestBatchProcessorCompression tests compression of batch data.
func TestBatchProcessorCompression(t *testing.T) {
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	config.BatchSize = 5
	config.EnableCompression = true
	config.CompressionLevel = 6

	syncer := &mockPolicyCacheSyncer{
		notifyCalled: make(chan PolicyChangeEvent, 10),
	}

	bp, err := NewBatchProcessor(config, syncer)
	require.NoError(t, err)
	defer bp.Close()

	// Add events
	for i := 1; i <= 5; i++ {
		event := &EnhancedPolicyChangeEvent{
			EventID:    uuid.New().String(),
			Type:       PolicyUpdateCreated,
			TenantID:   "tenant-1",
			PolicyName: "policy-1",
			Version:    int64(i),
			Timestamp:  time.Now(),
			NodeID:     "node-1",
		}
		err = bp.Add(event)
		require.NoError(t, err)
	}

	// Verify compression metrics
	stats := bp.GetMetrics()
	assert.Greater(t, stats.CompressionRatio, 0.0)
}

// TestEnhancedCacheVersionTrackerInvalidation tests invalidation callbacks.
func TestEnhancedCacheVersionTrackerInvalidation(t *testing.T) {
	tracker := NewCacheVersionTracker()

	var called bool
	var receivedTenantID, receivedPolicyID string
	var receivedVersion int64

	callback := func(tenantID, policyID string, version int64) {
		called = true
		receivedTenantID = tenantID
		receivedPolicyID = policyID
		receivedVersion = version
	}

	tracker.RegisterInvalidationCallback(callback)
	tracker.NotifyInvalidation("tenant-1", "policy-1", 5)

	assert.True(t, called)
	assert.Equal(t, "tenant-1", receivedTenantID)
	assert.Equal(t, "policy-1", receivedPolicyID)
	assert.Equal(t, int64(5), receivedVersion)
}

// TestPolicyChangeEventSerialization tests event serialization.
func TestPolicyChangeEventSerialization(t *testing.T) {
	event := &EnhancedPolicyChangeEvent{
		EventID:    uuid.New().String(),
		Type:       PolicyUpdateCreated,
		TenantID:   "tenant-1",
		PolicyName: "policy-1",
		Policy: &Policy{
			Metadata: PolicyMetadata{Name: "policy-1", Tenant: "tenant-1"},
			Spec:     PolicySpec{Subjects: SubjectSelector{Users: []string{"user-1"}}},
		},
		Version:   1,
		Timestamp: time.Now(),
		NodeID:    "node-1",
		Metadata: map[string]string{
			"key": "value",
		},
	}

	// Serialize
	data, err := event.MarshalBinary()
	require.NoError(t, err)

	// Deserialize
	var decoded EnhancedPolicyChangeEvent
	err = decoded.UnmarshalBinary(data)
	require.NoError(t, err)

	assert.Equal(t, event.EventID, decoded.EventID)
	assert.Equal(t, event.Type, decoded.Type)
	assert.Equal(t, event.TenantID, decoded.TenantID)
	assert.Equal(t, event.PolicyName, decoded.PolicyName)
	assert.Equal(t, event.Version, decoded.Version)
	assert.Equal(t, event.NodeID, decoded.NodeID)
	assert.Equal(t, event.Metadata["key"], decoded.Metadata["key"])
	require.NotNil(t, decoded.Policy)
	assert.Equal(t, event.Policy.Metadata.Name, decoded.Policy.Metadata.Name)
	assert.Equal(t, event.Policy.Spec.Subjects.Users, decoded.Policy.Spec.Subjects.Users)

	// Malformed data is rejected.
	assert.Error(t, decoded.UnmarshalBinary([]byte("not an event")))
}

// TestEnhancedCacheSyncConfig tests configuration validation.
func TestEnhancedCacheSyncConfig(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(c *EnhancedCacheSyncConfig)
		errorMsg string
	}{
		{name: "valid config", mutate: func(*EnhancedCacheSyncConfig) {}},
		{
			name:     "zero batch size",
			mutate:   func(c *EnhancedCacheSyncConfig) { c.BatchSize = 0 },
			errorMsg: "batch size must be positive",
		},
		{
			name:     "zero batch interval",
			mutate:   func(c *EnhancedCacheSyncConfig) { c.BatchInterval = 0 },
			errorMsg: "batch interval must be positive",
		},
		{
			name:     "compression level above 9",
			mutate:   func(c *EnhancedCacheSyncConfig) { c.CompressionLevel = 10 },
			errorMsg: "compression level must be between 0 and 9",
		},
		{
			name:     "negative compression level",
			mutate:   func(c *EnhancedCacheSyncConfig) { c.CompressionLevel = -1 },
			errorMsg: "compression level must be between 0 and 9",
		},
		{
			name:     "zero delta log max entries",
			mutate:   func(c *EnhancedCacheSyncConfig) { c.DeltaLogMaxEntries = 0 },
			errorMsg: "delta log max entries must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
			tt.mutate(cfg)

			err := cfg.Validate()
			if tt.errorMsg == "" {
				assert.NoError(t, err)
				return
			}
			var cfgErr *InvalidSyncConfigError
			require.ErrorAs(t, err, &cfgErr)
			assert.Contains(t, err.Error(), tt.errorMsg)
		})
	}
}

// TestEnhancedCacheSyncMetrics tests metrics recording.
func TestEnhancedCacheSyncMetrics(t *testing.T) {
	metrics := NewEnhancedCacheSyncMetrics()

	metrics.RecordBatchSent()
	metrics.RecordBatchSent()
	metrics.RecordBatchReceived()
	metrics.RecordCompressionRatio(2.5)
	metrics.RecordDeltaLogSize(100)
	metrics.RecordIncrementalSync()
	metrics.RecordFullSync()

	stats := metrics.GetStats()
	assert.Equal(t, int64(2), stats.BatchesSent, "counters accumulate")
	assert.Equal(t, int64(1), stats.BatchesReceived)
	assert.Equal(t, 2.5, stats.CompressionRatio)
	assert.Equal(t, int64(100), stats.DeltaLogSize)
	assert.Equal(t, int64(1), stats.IncrementalSyncs)
	assert.Equal(t, int64(1), stats.FullSyncs)
}

// mockPolicyCacheSyncer is a mock implementation of PolicyCacheSyncer for testing.
type mockPolicyCacheSyncer struct {
	notifyCalled chan PolicyChangeEvent
	subscribed   bool
}

func (m *mockPolicyCacheSyncer) NotifyPolicyChange(ctx context.Context, event PolicyChangeEvent) error {
	if m.notifyCalled != nil {
		select {
		case m.notifyCalled <- event:
		default:
		}
	}
	return nil
}

func (m *mockPolicyCacheSyncer) Subscribe(ctx context.Context, handler PolicyChangeHandler) error {
	m.subscribed = true
	return nil
}

func (m *mockPolicyCacheSyncer) Unsubscribe(ctx context.Context) error {
	m.subscribed = false
	return nil
}

func (m *mockPolicyCacheSyncer) SyncFromLeader(ctx context.Context) error {
	return nil
}

func (m *mockPolicyCacheSyncer) GetSyncStatus() *SyncStatus {
	return &SyncStatus{
		IsSubscribed: m.subscribed,
	}
}

func (m *mockPolicyCacheSyncer) Close() error {
	if m.notifyCalled != nil {
		close(m.notifyCalled)
	}
	return nil
}

// TestDecompress tests decompression of gzip data.
func TestDecompress(t *testing.T) {
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	syncer := &mockPolicyCacheSyncer{}

	bp, err := NewBatchProcessor(config, syncer)
	require.NoError(t, err)
	defer bp.Close()

	originalData := []byte("test data for compression")

	// Compress
	compressed, err := bp.compress(originalData)
	require.NoError(t, err)

	// Decompress
	decompressed, err := decompress(compressed)
	require.NoError(t, err)

	assert.Equal(t, originalData, decompressed)
}

// TestDecompressInvalidData tests decompression of invalid data.
func TestDecompressInvalidData(t *testing.T) {
	invalidData := []byte("not gzip data")

	_, err := decompress(invalidData)
	assert.Error(t, err)
}

// TestPolicyUpdateTypeString tests string representation of update types.
func TestPolicyUpdateTypeString(t *testing.T) {
	tests := []struct {
		updateType PolicyUpdateType
		expected   string
	}{
		{PolicyUpdateSnapshot, "snapshot"},
		{PolicyUpdateCreated, "created"},
		{PolicyUpdateUpdated, "updated"},
		{PolicyUpdateDeleted, "deleted"},
		{PolicyUpdateBatch, "batch"},
		{PolicyUpdateType(999), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.expected, func(t *testing.T) {
			assert.Equal(t, tt.expected, tt.updateType.String())
		})
	}
}

// TestBatchProcessorClose tests closing the batch processor.
func TestBatchProcessorClose(t *testing.T) {
	config := DefaultEnhancedCacheSyncConfig("node-1", "tenant-1")
	syncer := &mockPolicyCacheSyncer{}

	bp, err := NewBatchProcessor(config, syncer)
	require.NoError(t, err)

	// Add an event
	event := &EnhancedPolicyChangeEvent{
		EventID:    uuid.New().String(),
		Type:       PolicyUpdateCreated,
		TenantID:   "tenant-1",
		PolicyName: "policy-1",
		Version:    1,
		Timestamp:  time.Now(),
		NodeID:     "node-1",
	}
	err = bp.Add(event)
	require.NoError(t, err)

	// Close should flush pending events
	err = bp.Close()
	assert.NoError(t, err)

	// Verify closed
	err = bp.Add(event)
	assert.Error(t, err)
	assert.Equal(t, ErrSyncerClosed, err)

	// A second Close is a no-op.
	assert.NoError(t, bp.Close())
}

func TestEnhancedCacheVersionTracker_NewAndGetPolicyVersion(t *testing.T) {
	tracker := NewEnhancedCacheVersionTracker()
	if tracker == nil {
		t.Fatal("expected non-nil tracker")
	}

	// Getting a nonexistent policy version should return 0
	v := tracker.GetPolicyVersion("tenant1", "policy1")
	if v != 0 {
		t.Fatalf("expected 0 for nonexistent policy, got %d", v)
	}
}

func TestEnhancedCacheVersionTracker_SetPolicyVersion_MultiplePolicies(t *testing.T) {
	tracker := NewEnhancedCacheVersionTracker()

	tracker.SetPolicyVersion("tenant1", "policy1", 3)
	tracker.SetPolicyVersion("tenant1", "policy2", 7)
	tracker.SetPolicyVersion("tenant2", "policy1", 10)

	v1 := tracker.GetPolicyVersion("tenant1", "policy1")
	if v1 != 3 {
		t.Fatalf("expected 3 for tenant1:policy1, got %d", v1)
	}

	v2 := tracker.GetPolicyVersion("tenant1", "policy2")
	if v2 != 7 {
		t.Fatalf("expected 7 for tenant1:policy2, got %d", v2)
	}

	v3 := tracker.GetPolicyVersion("tenant2", "policy1")
	if v3 != 10 {
		t.Fatalf("expected 10 for tenant2:policy1, got %d", v3)
	}

	// Tenant versions should reflect the highest policy version
	tv1 := tracker.GetTenantVersion("tenant1")
	if tv1 < 7 {
		t.Fatalf("expected tenant1 version >= 7, got %d", tv1)
	}

	tv2 := tracker.GetTenantVersion("tenant2")
	if tv2 < 10 {
		t.Fatalf("expected tenant2 version >= 10, got %d", tv2)
	}
}

func TestEnhancedCacheVersionTracker_MakePolicyKey(t *testing.T) {
	tracker := NewEnhancedCacheVersionTracker()

	// Set and get using same key should be consistent
	tracker.SetPolicyVersion("t1", "p1", 42)
	v := tracker.GetPolicyVersion("t1", "p1")
	if v != 42 {
		t.Fatalf("expected 42, got %d", v)
	}

	// Different tenant/policy combinations should not collide
	tracker.SetPolicyVersion("t1:p", "1", 99)
	vOriginal := tracker.GetPolicyVersion("t1", "p1")
	if vOriginal != 42 {
		t.Fatalf("expected 42 after setting different key, got %d", vOriginal)
	}
}

// appendDeltaVersions appends one event per version 1..n for tenant t1.
func appendDeltaVersions(t *testing.T, dl *DeltaLog, n int64) {
	t.Helper()
	for i := int64(1); i <= n; i++ {
		event := &EnhancedPolicyChangeEvent{
			EventID:    "evt",
			TenantID:   "t1",
			PolicyName: "p1",
			Version:    i,
		}
		require.NoError(t, dl.Append(event), "Append version %d", i)
	}
}

// TestDeltaLogTrim tests trimming old entries.
func TestDeltaLogTrim(t *testing.T) {
	tests := []struct {
		name       string
		maxEntries int
		appended   int64
		wantSize   int
	}{
		{name: "under max entries is a no-op", maxEntries: 100, appended: 10, wantSize: 10},
		{name: "over max entries keeps the newest", maxEntries: 5, appended: 10, wantSize: 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dl := NewDeltaLog(tt.maxEntries)
			appendDeltaVersions(t, dl, tt.appended)

			dl.Trim()

			assert.Equal(t, tt.wantSize, dl.Size())
			assert.Equal(t, tt.appended, dl.GetMaxVersion())
			assert.Positive(t, dl.GetMinVersion())
		})
	}
}

func TestDeltaLogSize(t *testing.T) {
	tests := []struct {
		name       string
		maxEntries int
		appended   int64
		wantSize   int
	}{
		{name: "under capacity", maxEntries: 100, appended: 1, wantSize: 1},
		{name: "overflow is capped at max entries", maxEntries: 3, appended: 10, wantSize: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dl := NewDeltaLog(tt.maxEntries)
			appendDeltaVersions(t, dl, tt.appended)

			assert.Equal(t, tt.wantSize, dl.Size())
		})
	}
}

func TestNewDeltaLogNonPositiveMaxEntries(t *testing.T) {
	for _, maxEntries := range []int{0, -5} {
		t.Run(fmt.Sprintf("maxEntries=%d", maxEntries), func(t *testing.T) {
			dl := NewDeltaLog(maxEntries)
			require.NotNil(t, dl)
			assert.Equal(t, 10000, dl.maxEntries, "non-positive maxEntries falls back to the default")
		})
	}
}

func TestDeltaLog_GetSince_MultiTenant(t *testing.T) {
	dl := NewDeltaLog(100)

	// Add events for two tenants
	for i := int64(1); i <= 5; i++ {
		e1 := &EnhancedPolicyChangeEvent{
			EventID:    "evt",
			TenantID:   "tenant1",
			PolicyName: "p1",
			Version:    i,
		}
		e2 := &EnhancedPolicyChangeEvent{
			EventID:    "evt",
			TenantID:   "tenant2",
			PolicyName: "p1",
			Version:    i + 10,
		}
		if err := dl.Append(e1); err != nil {
			t.Fatalf("Append tenant1 failed: %v", err)
		}
		if err := dl.Append(e2); err != nil {
			t.Fatalf("Append tenant2 failed: %v", err)
		}
	}

	// Get events since version 3 for tenant1
	events, err := dl.GetSince("tenant1", 3)
	if err != nil {
		t.Fatalf("GetSince failed: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events for tenant1 since v3, got %d", len(events))
	}

	// Get events for nonexistent tenant
	noEvents, err := dl.GetSince("nonexistent", 0)
	if err != nil {
		t.Fatalf("GetSince nonexistent failed: %v", err)
	}
	if noEvents != nil {
		t.Fatalf("expected nil for nonexistent tenant, got %d events", len(noEvents))
	}
}
