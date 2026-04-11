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

package messaging

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	stderrors "errors"
	"hash"
	"log/slog"
	"sort"
	"sync"
	"time"

	dragonerrors "github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/storage"
)

// Default configuration values for the deduplication store.
const (
	DefaultDedupPrefix = "_push/dedup/"
)

// DedupEntry stores deduplication metadata for a message.
type DedupEntry struct {
	Count       int64     `json:"count"`        // Number of times received
	FirstSeen   time.Time `json:"first_seen"`   // First occurrence timestamp
	LastSeen    time.Time `json:"last_seen"`    // Most recent occurrence timestamp
	MessageID   string    `json:"message_id"`   // Original message ID
	PayloadHash string    `json:"payload_hash"` // Content hash for deduplication
}

// DedupConfig configures deduplication behavior.
type DedupConfig struct {
	Enabled    bool          // Enable deduplication
	Window     time.Duration // How long to remember messages (TTL)
	HashFields []string      // Fields to hash for content dedup (empty = entire payload)
	UseIDDedup bool          // Use client-provided message ID for dedup
}

// DedupStats provides statistics about deduplication for a channel.
type DedupStats struct {
	Channel         string        // Channel name
	TotalMessages   int64         // Total unique messages tracked
	TotalDuplicates int64         // Total duplicate count across all messages
	TopDuplicates   []*DedupEntry // Most duplicated messages
}

// DedupStoreImpl manages message deduplication with persistent storage.
// This is the production implementation of deduplication tracking.
type DedupStoreImpl struct {
	storage  storage.Storage
	prefix   string
	window   time.Duration
	logger   *logger.Logger
	hashPool sync.Pool
}

// DedupStoreOption configures DedupStoreImpl behavior.
type DedupStoreOption func(*DedupStoreImpl)

// WithDedupStorePrefix sets the key prefix for dedup entries in storage.
func WithDedupStorePrefix(prefix string) DedupStoreOption {
	return func(ds *DedupStoreImpl) {
		ds.prefix = prefix
	}
}

// WithDedupStoreWindow sets the TTL window for dedup entries.
func WithDedupStoreWindow(window time.Duration) DedupStoreOption {
	return func(ds *DedupStoreImpl) {
		ds.window = window
	}
}

// NewDedupStoreImpl creates a new DedupStoreImpl with the given storage backend and options.
func NewDedupStoreImpl(s storage.Storage, log *logger.Logger, opts ...DedupStoreOption) *DedupStoreImpl {
	ds := &DedupStoreImpl{
		storage: s,
		prefix:  DefaultDedupPrefix,
		window:  DefaultDedupWindow,
		logger:  log,
		hashPool: sync.Pool{
			New: func() interface{} {
				return sha256.New()
			},
		},
	}

	for _, opt := range opts {
		opt(ds)
	}

	return ds
}

// CheckAndStore checks if a message is a duplicate and stores/updates the entry.
// Returns DedupInfo indicating whether the message was a duplicate and the count.
// When clientDedupKey is non-empty, it is used as the dedup key instead of computing
// one from the message ID or payload hash.
func (ds *DedupStoreImpl) CheckAndStore(ctx context.Context, channel string, msg *Message, clientDedupKey string) (*DedupInfo, error) {
	if msg == nil {
		return nil, ErrNilMessage
	}

	if channel == "" {
		return nil, ErrEmptyChannelName
	}

	// Use client-specified dedup key if provided, otherwise compute from message
	var dedupKey string
	var payloadHash string
	var computeErr error

	if clientDedupKey != "" {
		dedupKey = clientDedupKey
		payloadHash, computeErr = ds.ComputeHash(msg.Payload, nil)
		if computeErr != nil {
			return nil, computeErr
		}
	} else if msg.ID != "" {
		dedupKey = msg.ID
		payloadHash, computeErr = ds.ComputeHash(msg.Payload, nil)
		if computeErr != nil {
			return nil, computeErr
		}
	} else {
		payloadHash, computeErr = ds.ComputeHash(msg.Payload, nil)
		if computeErr != nil {
			return nil, computeErr
		}
		dedupKey = payloadHash
	}

	// Check for existing entry
	entry, err := ds.GetEntry(ctx, channel, dedupKey)
	if err != nil && !isNotFoundError(err) {
		return nil, err
	}

	now := time.Now()

	if entry != nil {
		// Check if entry is within the dedup window
		if now.Sub(entry.FirstSeen) <= ds.window {
			// Update existing entry
			entry.Count++
			entry.LastSeen = now

			storeErr := ds.StoreEntry(ctx, channel, dedupKey, entry)
			if storeErr != nil {
				return nil, storeErr
			}

			ds.logger.DebugAttrs("duplicate message detected",
				slog.String("channel", channel),
				slog.String("dedup_key", dedupKey),
				slog.Int64("count", entry.Count),
			)

			return &DedupInfo{
				IsDuplicate: true,
				Count:       entry.Count,
				FirstSeen:   entry.FirstSeen,
			}, nil
		}

		// Entry expired, treat as new
		ds.logger.DebugAttrs("dedup entry expired, treating as new",
			slog.String("channel", channel),
			slog.String("dedup_key", dedupKey),
		)
	}

	// Create new entry
	newEntry := &DedupEntry{
		Count:       1,
		FirstSeen:   now,
		LastSeen:    now,
		MessageID:   msg.ID,
		PayloadHash: payloadHash,
	}

	storeErr := ds.StoreEntry(ctx, channel, dedupKey, newEntry)
	if storeErr != nil {
		return nil, storeErr
	}

	return &DedupInfo{
		IsDuplicate: false,
		Count:       1,
		FirstSeen:   now,
	}, nil
}

// ComputeHash computes a SHA256 hash of the payload.
// If fields is non-empty, only those fields are extracted and hashed from JSON payloads.
func (ds *DedupStoreImpl) ComputeHash(payload []byte, fields []string) (string, error) {
	if payload == nil {
		return "", &InvalidPayloadError{Reason: "payload is nil"}
	}

	// Get hash writer from pool.
	poolObj := ds.hashPool.Get()
	h, ok := poolObj.(hash.Hash)
	if !ok {
		return "", &HashComputeError{Cause: ErrHashPoolInvalidType}
	}
	h.Reset()
	defer ds.hashPool.Put(h)

	if len(fields) == 0 {
		// Hash entire payload
		_, writeErr := h.Write(payload)
		if writeErr != nil {
			return "", &HashComputeError{Cause: writeErr}
		}
		return hex.EncodeToString(h.Sum(nil)), nil
	}

	// Extract specified fields from JSON and hash them
	var data map[string]interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		return "", &HashComputeError{Cause: err}
	}

	// Sort fields for consistent hashing
	sortedFields := make([]string, len(fields))
	copy(sortedFields, fields)
	sort.Strings(sortedFields)

	// Build buffer with selected fields
	var buf bytes.Buffer
	for _, field := range sortedFields {
		if value, exists := data[field]; exists {
			fieldBytes, marshalErr := json.Marshal(value)
			if marshalErr != nil {
				return "", &HashComputeError{Cause: marshalErr}
			}
			buf.WriteString(field)
			buf.WriteByte(':')
			buf.Write(fieldBytes)
			buf.WriteByte(';')
		}
	}

	_, writeErr := h.Write(buf.Bytes())
	if writeErr != nil {
		return "", &HashComputeError{Cause: writeErr}
	}

	return hex.EncodeToString(h.Sum(nil)), nil
}

// GetEntry retrieves a dedup entry for the given channel and key.
func (ds *DedupStoreImpl) GetEntry(ctx context.Context, channel, key string) (*DedupEntry, error) {
	if channel == "" {
		return nil, ErrEmptyChannelName
	}

	if key == "" {
		return nil, &InvalidDedupKeyError{Key: key, Reason: "key is empty"}
	}

	storageKey := ds.buildStorageKey(channel, key)
	data, err := ds.storage.Get([]byte(storageKey))
	if err != nil {
		return nil, err
	}

	var entry DedupEntry
	if unmarshalErr := json.Unmarshal(data, &entry); unmarshalErr != nil {
		return nil, &DedupEntryDecodeError{Key: key, Cause: unmarshalErr}
	}

	return &entry, nil
}

// StoreEntry stores a dedup entry for the given channel and key.
func (ds *DedupStoreImpl) StoreEntry(ctx context.Context, channel, key string, entry *DedupEntry) error {
	if channel == "" {
		return ErrEmptyChannelName
	}

	if key == "" {
		return &InvalidDedupKeyError{Key: key, Reason: "key is empty"}
	}

	if entry == nil {
		return &InvalidDedupEntryError{Reason: "entry is nil"}
	}

	storageKey := ds.buildStorageKey(channel, key)
	data, marshalErr := json.Marshal(entry)
	if marshalErr != nil {
		return &DedupEntryEncodeError{Key: key, Cause: marshalErr}
	}

	return ds.storage.Set([]byte(storageKey), data)
}

// DeleteEntry removes a dedup entry for the given channel and key.
func (ds *DedupStoreImpl) DeleteEntry(ctx context.Context, channel, key string) error {
	if channel == "" {
		return ErrEmptyChannelName
	}

	if key == "" {
		return &InvalidDedupKeyError{Key: key, Reason: "key is empty"}
	}

	storageKey := ds.buildStorageKey(channel, key)
	return ds.storage.Delete([]byte(storageKey))
}

// GetStats retrieves deduplication statistics for a channel.
func (ds *DedupStoreImpl) GetStats(ctx context.Context, channel string) (*DedupStats, error) {
	if channel == "" {
		return nil, ErrEmptyChannelName
	}

	prefix := ds.prefix + channel + "/"
	opts := &storage.IteratorOptions{
		LowerBound: []byte(prefix),
		UpperBound: []byte(prefix + "\xff"),
	}

	iter := ds.storage.NewIterator(opts)
	defer iter.Close()

	stats := &DedupStats{
		Channel:       channel,
		TopDuplicates: make([]*DedupEntry, 0, 10),
	}

	var allEntries []*DedupEntry

	for valid := iter.SeekToFirst(); valid; valid = iter.Next() {
		if iterErr := iter.Error(); iterErr != nil {
			return nil, &DedupStatsError{Channel: channel, Cause: iterErr}
		}

		var entry DedupEntry
		if unmarshalErr := json.Unmarshal(iter.Value(), &entry); unmarshalErr != nil {
			ds.logger.WarnAttrs("failed to unmarshal dedup entry during stats collection",
				slog.String("key", string(iter.Key())),
				slog.String("error", unmarshalErr.Error()),
			)
			continue
		}

		stats.TotalMessages++
		if entry.Count > 1 {
			stats.TotalDuplicates += entry.Count - 1
		}
		allEntries = append(allEntries, &DedupEntry{
			Count:       entry.Count,
			FirstSeen:   entry.FirstSeen,
			LastSeen:    entry.LastSeen,
			MessageID:   entry.MessageID,
			PayloadHash: entry.PayloadHash,
		})
	}

	// Sort by count descending and take top 10
	sort.Slice(allEntries, func(i, j int) bool {
		return allEntries[i].Count > allEntries[j].Count
	})

	topCount := 10
	if len(allEntries) < topCount {
		topCount = len(allEntries)
	}
	stats.TopDuplicates = allEntries[:topCount]

	return stats, nil
}

// Cleanup removes expired dedup entries from all channels.
func (ds *DedupStoreImpl) Cleanup(ctx context.Context) error {
	opts := &storage.IteratorOptions{
		LowerBound: []byte(ds.prefix),
		UpperBound: []byte(ds.prefix + "\xff\xff\xff"),
	}

	iter := ds.storage.NewIterator(opts)
	defer iter.Close()

	now := time.Now()
	var keysToDelete [][]byte

	for valid := iter.SeekToFirst(); valid; valid = iter.Next() {
		// Check context cancellation
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if iterErr := iter.Error(); iterErr != nil {
			return &DedupCleanupError{Cause: iterErr}
		}

		var entry DedupEntry
		if unmarshalErr := json.Unmarshal(iter.Value(), &entry); unmarshalErr != nil {
			ds.logger.WarnAttrs("failed to unmarshal dedup entry during cleanup",
				slog.String("key", string(iter.Key())),
				slog.String("error", unmarshalErr.Error()),
			)
			continue
		}

		// Check if entry has expired
		if now.Sub(entry.FirstSeen) > ds.window {
			// Copy key since iterator may reuse underlying buffer
			keyCopy := make([]byte, len(iter.Key()))
			copy(keyCopy, iter.Key())
			keysToDelete = append(keysToDelete, keyCopy)
		}
	}

	if len(keysToDelete) == 0 {
		return nil
	}

	// Delete expired entries using batch for efficiency
	batch := ds.storage.Batch()
	defer batch.Close()

	for _, key := range keysToDelete {
		if batchErr := batch.Delete(key); batchErr != nil {
			return &DedupCleanupError{Cause: batchErr}
		}
	}

	if commitErr := batch.Commit(); commitErr != nil {
		return &DedupCleanupError{Cause: commitErr}
	}

	ds.logger.InfoAttrs("dedup cleanup completed",
		slog.Int("entries_deleted", len(keysToDelete)),
	)

	return nil
}

// GetWindow returns the deduplication window duration.
func (ds *DedupStoreImpl) GetWindow() time.Duration {
	return ds.window
}

// GetPrefix returns the storage key prefix.
func (ds *DedupStoreImpl) GetPrefix() string {
	return ds.prefix
}

// buildStorageKey constructs the full storage key for a dedup entry.
func (ds *DedupStoreImpl) buildStorageKey(channel, key string) string {
	return ds.prefix + channel + "/" + key
}

// isNotFoundError checks if an error indicates a key was not found.
// This is a helper to handle storage-specific not found errors.
func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	var de *dragonerrors.QBError
	if stderrors.As(err, &de) {
		return de.Code == dragonerrors.ErrNotFound
	}
	errStr := err.Error()
	return errStr == "not found" || errStr == "key not found"
}

// InvalidPayloadError indicates an invalid payload was provided.
type InvalidPayloadError struct {
	Reason string
}

// Error implements the error interface.
func (e *InvalidPayloadError) Error() string {
	return "invalid payload: " + e.Reason
}

// HashComputeError indicates an error computing a hash.
type HashComputeError struct {
	Cause error
}

// Error implements the error interface.
func (e *HashComputeError) Error() string {
	if e.Cause != nil {
		return "hash compute error: " + e.Cause.Error()
	}
	return "hash compute error"
}

// Unwrap returns the underlying error.
func (e *HashComputeError) Unwrap() error {
	return e.Cause
}

// InvalidDedupKeyError indicates an invalid dedup key was provided.
type InvalidDedupKeyError struct {
	Key    string
	Reason string
}

// Error implements the error interface.
func (e *InvalidDedupKeyError) Error() string {
	return "invalid dedup key '" + e.Key + "': " + e.Reason
}

// InvalidDedupEntryError indicates an invalid dedup entry was provided.
type InvalidDedupEntryError struct {
	Reason string
}

// Error implements the error interface.
func (e *InvalidDedupEntryError) Error() string {
	return "invalid dedup entry: " + e.Reason
}

// DedupEntryDecodeError indicates an error decoding a dedup entry.
type DedupEntryDecodeError struct {
	Key   string
	Cause error
}

// Error implements the error interface.
func (e *DedupEntryDecodeError) Error() string {
	if e.Cause != nil {
		return "dedup entry decode error for key '" + e.Key + "': " + e.Cause.Error()
	}
	return "dedup entry decode error for key '" + e.Key + "'"
}

// Unwrap returns the underlying error.
func (e *DedupEntryDecodeError) Unwrap() error {
	return e.Cause
}

// DedupEntryEncodeError indicates an error encoding a dedup entry.
type DedupEntryEncodeError struct {
	Key   string
	Cause error
}

// Error implements the error interface.
func (e *DedupEntryEncodeError) Error() string {
	if e.Cause != nil {
		return "dedup entry encode error for key '" + e.Key + "': " + e.Cause.Error()
	}
	return "dedup entry encode error for key '" + e.Key + "'"
}

// Unwrap returns the underlying error.
func (e *DedupEntryEncodeError) Unwrap() error {
	return e.Cause
}

// DedupStatsError indicates an error retrieving dedup statistics.
type DedupStatsError struct {
	Channel string
	Cause   error
}

// Error implements the error interface.
func (e *DedupStatsError) Error() string {
	if e.Cause != nil {
		return "dedup stats error for channel '" + e.Channel + "': " + e.Cause.Error()
	}
	return "dedup stats error for channel '" + e.Channel + "'"
}

// Unwrap returns the underlying error.
func (e *DedupStatsError) Unwrap() error {
	return e.Cause
}

// DedupCleanupError indicates an error during dedup cleanup.
type DedupCleanupError struct {
	Cause error
}

// Error implements the error interface.
func (e *DedupCleanupError) Error() string {
	if e.Cause != nil {
		return "dedup cleanup error: " + e.Cause.Error()
	}
	return "dedup cleanup error"
}

// Unwrap returns the underlying error.
func (e *DedupCleanupError) Unwrap() error {
	return e.Cause
}
