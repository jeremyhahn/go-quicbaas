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

// Package watchbridge connects the watch.Manager to the messaging/pub-sub system.
// It bridges watch events to messaging channels, enabling real-time notifications
// when data changes occur.
package watchbridge

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/cluster"
	"github.com/jeremyhahn/go-quicbaas/pkg/common/safeconv"
	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
	"github.com/jeremyhahn/go-quicbaas/pkg/watch"
)

// WatchBridge error codes extending the base error code system.
const (
	// ErrCodeWatchBridgeNotRunning indicates the watch bridge is not running.
	ErrCodeWatchBridgeNotRunning errors.ErrorCode = 1200 + iota

	// ErrCodeWatchBridgeAlreadyRunning indicates the watch bridge is already running.
	ErrCodeWatchBridgeAlreadyRunning

	// ErrCodeWatchBridgeNilManager indicates a nil watch manager was provided.
	ErrCodeWatchBridgeNilManager

	// ErrCodeWatchBridgeNilService indicates a nil messaging service was provided.
	ErrCodeWatchBridgeNilService

	// ErrCodeWatchBridgeInvalidMapping indicates an invalid key prefix to channel mapping.
	ErrCodeWatchBridgeInvalidMapping

	// ErrCodeWatchBridgePublishFailed indicates publishing to a channel failed.
	ErrCodeWatchBridgePublishFailed
)

// Sentinel errors for the watch bridge system.
var (
	// ErrWatchBridgeNotRunning indicates the watch bridge is not running.
	ErrWatchBridgeNotRunning = stderrors.New("watch bridge not running")

	// ErrWatchBridgeAlreadyRunning indicates the watch bridge is already running.
	ErrWatchBridgeAlreadyRunning = stderrors.New("watch bridge already running")

	// ErrWatchBridgeNilManager indicates a nil watch manager was provided.
	ErrWatchBridgeNilManager = stderrors.New("watch manager is nil")

	// ErrWatchBridgeNilService indicates a nil messaging service was provided.
	ErrWatchBridgeNilService = stderrors.New("messaging service is nil")

	// ErrWatchBridgeInvalidMapping indicates an invalid key prefix to channel mapping.
	ErrWatchBridgeInvalidMapping = stderrors.New("invalid key prefix to channel mapping")
)

// WatchBridgeNotRunningError represents an error when the bridge is not running.
type WatchBridgeNotRunningError struct {
	Operation string
}

// Error implements the error interface.
func (e *WatchBridgeNotRunningError) Error() string {
	return "watch bridge not running, cannot perform: " + e.Operation
}

// WatchBridgeAlreadyRunningError represents an error when the bridge is already running.
type WatchBridgeAlreadyRunningError struct{}

// Error implements the error interface.
func (e *WatchBridgeAlreadyRunningError) Error() string {
	return "watch bridge is already running"
}

// WatchBridgeNilManagerError represents an error when a nil watch manager is provided.
type WatchBridgeNilManagerError struct{}

// Error implements the error interface.
func (e *WatchBridgeNilManagerError) Error() string {
	return "watch manager cannot be nil"
}

// WatchBridgeNilServiceError represents an error when a nil messaging service is provided.
type WatchBridgeNilServiceError struct{}

// Error implements the error interface.
func (e *WatchBridgeNilServiceError) Error() string {
	return "messaging service cannot be nil"
}

// WatchBridgeInvalidMappingError represents an error for invalid mapping configuration.
type WatchBridgeInvalidMappingError struct {
	KeyPrefix string
	Reason    string
}

// Error implements the error interface.
func (e *WatchBridgeInvalidMappingError) Error() string {
	return "invalid watch bridge mapping for prefix '" + e.KeyPrefix + "': " + e.Reason
}

// WatchBridgePublishFailedError represents a publish failure.
type WatchBridgePublishFailedError struct {
	Channel string
	Cause   string
}

// Error implements the error interface.
func (e *WatchBridgePublishFailedError) Error() string {
	return "failed to publish to channel '" + e.Channel + "': " + e.Cause
}

// NewWatchBridgeNotRunningError creates a new QBError for bridge not running.
func NewWatchBridgeNotRunningError(op, operation string) *errors.QBError {
	return &errors.QBError{
		Code: ErrCodeWatchBridgeNotRunning,
		Op:   op,
		Err: &WatchBridgeNotRunningError{
			Operation: operation,
		},
		Context: map[string]interface{}{
			"operation": operation,
		},
	}
}

// NewWatchBridgeAlreadyRunningError creates a new QBError for bridge already running.
func NewWatchBridgeAlreadyRunningError(op string) *errors.QBError {
	return &errors.QBError{
		Code: ErrCodeWatchBridgeAlreadyRunning,
		Op:   op,
		Err:  &WatchBridgeAlreadyRunningError{},
	}
}

// NewWatchBridgeNilManagerError creates a new QBError for nil watch manager.
func NewWatchBridgeNilManagerError(op string) *errors.QBError {
	return &errors.QBError{
		Code: ErrCodeWatchBridgeNilManager,
		Op:   op,
		Err:  &WatchBridgeNilManagerError{},
	}
}

// NewWatchBridgeNilServiceError creates a new QBError for nil messaging service.
func NewWatchBridgeNilServiceError(op string) *errors.QBError {
	return &errors.QBError{
		Code: ErrCodeWatchBridgeNilService,
		Op:   op,
		Err:  &WatchBridgeNilServiceError{},
	}
}

// NewWatchBridgeInvalidMappingError creates a new QBError for invalid mapping.
func NewWatchBridgeInvalidMappingError(op, keyPrefix, reason string) *errors.QBError {
	return &errors.QBError{
		Code: ErrCodeWatchBridgeInvalidMapping,
		Op:   op,
		Err: &WatchBridgeInvalidMappingError{
			KeyPrefix: keyPrefix,
			Reason:    reason,
		},
		Context: map[string]interface{}{
			"key_prefix": keyPrefix,
			"reason":     reason,
		},
	}
}

// NewWatchBridgePublishFailedError creates a new QBError for publish failure.
func NewWatchBridgePublishFailedError(op, channel, cause string) *errors.QBError {
	return &errors.QBError{
		Code: ErrCodeWatchBridgePublishFailed,
		Op:   op,
		Err: &WatchBridgePublishFailedError{
			Channel: channel,
			Cause:   cause,
		},
		Context: map[string]interface{}{
			"channel": channel,
			"cause":   cause,
		},
		Retry: true,
	}
}

// IsWatchBridgeNotRunning returns true if the error is a watch bridge not running error.
func IsWatchBridgeNotRunning(err error) bool {
	if err == nil {
		return false
	}
	var wbnr *WatchBridgeNotRunningError
	if stderrors.As(err, &wbnr) {
		return true
	}
	var de *errors.QBError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeWatchBridgeNotRunning
	}
	return stderrors.Is(err, ErrWatchBridgeNotRunning)
}

// IsWatchBridgeAlreadyRunning returns true if the error is a watch bridge already running error.
func IsWatchBridgeAlreadyRunning(err error) bool {
	if err == nil {
		return false
	}
	var wbar *WatchBridgeAlreadyRunningError
	if stderrors.As(err, &wbar) {
		return true
	}
	var de *errors.QBError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeWatchBridgeAlreadyRunning
	}
	return stderrors.Is(err, ErrWatchBridgeAlreadyRunning)
}

// IsWatchBridgeNilManager returns true if the error is a nil manager error.
func IsWatchBridgeNilManager(err error) bool {
	if err == nil {
		return false
	}
	var wnm *WatchBridgeNilManagerError
	if stderrors.As(err, &wnm) {
		return true
	}
	var de *errors.QBError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeWatchBridgeNilManager
	}
	return stderrors.Is(err, ErrWatchBridgeNilManager)
}

// IsWatchBridgeNilService returns true if the error is a nil service error.
func IsWatchBridgeNilService(err error) bool {
	if err == nil {
		return false
	}
	var wns *WatchBridgeNilServiceError
	if stderrors.As(err, &wns) {
		return true
	}
	var de *errors.QBError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeWatchBridgeNilService
	}
	return stderrors.Is(err, ErrWatchBridgeNilService)
}

// IsWatchBridgeInvalidMapping returns true if the error is an invalid mapping error.
func IsWatchBridgeInvalidMapping(err error) bool {
	if err == nil {
		return false
	}
	var wim *WatchBridgeInvalidMappingError
	if stderrors.As(err, &wim) {
		return true
	}
	var de *errors.QBError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodeWatchBridgeInvalidMapping
	}
	return stderrors.Is(err, ErrWatchBridgeInvalidMapping)
}

// WatchEventFilter defines which events should be bridged.
type WatchEventFilter struct {
	// NoPut filters out PUT events if true.
	NoPut bool
	// NoDelete filters out DELETE events if true.
	NoDelete bool
}

// KeyChannelMapping defines how key prefixes map to messaging channels.
type KeyChannelMapping struct {
	// KeyPrefix is the watch key prefix to match (e.g., "/users/").
	KeyPrefix string
	// ChannelPrefix is the channel prefix to publish to (e.g., "watch:/users/").
	// If empty, defaults to "watch:" + KeyPrefix.
	ChannelPrefix string
	// Filter specifies which event types to bridge for this mapping.
	Filter WatchEventFilter
	// EntityType is an optional pointer to a struct type for delta computation.
	// If provided, the bridge will compute deltas for events matching this prefix.
	EntityType interface{}
	// IncludeOldValues includes old values in delta payloads when true.
	IncludeOldValues bool
}

// watchBridgeState represents the running state of the bridge.
type watchBridgeState int32

const (
	bridgeStateStopped watchBridgeState = iota
	bridgeStateStarting
	bridgeStateRunning
	bridgeStateStopping
)

// WatchBridgeStats contains statistics about the watch bridge.
type WatchBridgeStats struct {
	// EventsReceived is the total number of watch events received.
	EventsReceived int64
	// EventsPublished is the number of events successfully published.
	EventsPublished int64
	// EventsFiltered is the number of events filtered out.
	EventsFiltered int64
	// EventsDroppedNotLeader is the number of events dropped due to not being leader.
	EventsDroppedNotLeader int64
	// PublishErrors is the number of publish failures.
	PublishErrors int64
}

// bridgeOptions holds configuration for the WatchBridge.
type bridgeOptions struct {
	leaderOnly           bool
	bufferSize           int
	defaultChannelPrefix string
	autoCreateChannels   bool
}

// defaultBridgeOptions returns default bridge options.
func defaultBridgeOptions() *bridgeOptions {
	return &bridgeOptions{
		leaderOnly:           true,
		bufferSize:           1024,
		defaultChannelPrefix: "watch:",
		autoCreateChannels:   true,
	}
}

// WatchBridgeOption configures the WatchBridge.
type WatchBridgeOption func(*bridgeOptions)

// WithLeaderOnly configures whether to only publish when the node is leader.
// Default is true.
func WithLeaderOnly(leaderOnly bool) WatchBridgeOption {
	return func(o *bridgeOptions) {
		o.leaderOnly = leaderOnly
	}
}

// WithBridgeBufferSize sets the internal event buffer size.
func WithBridgeBufferSize(size int) WatchBridgeOption {
	return func(o *bridgeOptions) {
		if size > 0 {
			o.bufferSize = size
		}
	}
}

// WithDefaultChannelPrefix sets the default prefix for generated channel names.
func WithDefaultChannelPrefix(prefix string) WatchBridgeOption {
	return func(o *bridgeOptions) {
		if prefix != "" {
			o.defaultChannelPrefix = prefix
		}
	}
}

// WithAutoCreateChannels enables or disables automatic channel creation.
func WithAutoCreateChannels(autoCreate bool) WatchBridgeOption {
	return func(o *bridgeOptions) {
		o.autoCreateChannels = autoCreate
	}
}

// WatchBridge connects the watch.Manager to the messaging/pub-sub system.
// It subscribes to watch events and publishes them to appropriate channels.
type WatchBridge struct {
	logger           *logger.Logger
	watchManager     *watch.Manager
	messagingService messaging.Service
	clusterInfo      cluster.ClusterInfo
	deltaComputer    *messaging.DeltaComputer
	mappings         []KeyChannelMapping
	options          *bridgeOptions

	// Lifecycle state (atomic for lock-free operations)
	state atomic.Int32

	// Context for lifecycle management
	ctx    context.Context
	cancel context.CancelFunc

	// Channels for coordination
	started chan struct{}
	stopped chan struct{}

	// WaitGroup to track watch loop goroutines
	watchLoopWg sync.WaitGroup

	// Channel to signal that all watch subscriptions are ready
	watchesReady chan struct{}

	// Statistics (atomic for lock-free updates)
	eventsReceived         atomic.Int64
	eventsPublished        atomic.Int64
	eventsFiltered         atomic.Int64
	eventsDroppedNotLeader atomic.Int64
	publishErrors          atomic.Int64
}

// NewWatchBridge creates a new WatchBridge with the given dependencies and options.
func NewWatchBridge(
	log *logger.Logger,
	watchManager *watch.Manager,
	messagingService messaging.Service,
	clusterInfo cluster.ClusterInfo,
	mappings []KeyChannelMapping,
	opts ...WatchBridgeOption,
) (*WatchBridge, error) {
	const op = "WatchBridge.New"

	if watchManager == nil {
		return nil, NewWatchBridgeNilManagerError(op)
	}
	if messagingService == nil {
		return nil, NewWatchBridgeNilServiceError(op)
	}

	options := defaultBridgeOptions()
	for _, opt := range opts {
		opt(options)
	}

	// Validate and normalize mappings
	normalizedMappings, err := normalizeMappings(mappings, options.defaultChannelPrefix)
	if err != nil {
		return nil, err
	}

	var deltaComputer *messaging.DeltaComputer
	if log != nil {
		deltaComputer = messaging.NewDeltaComputer(log)
	}

	bridge := &WatchBridge{
		logger:           log.WithField("component", "watch-bridge"),
		watchManager:     watchManager,
		messagingService: messagingService,
		clusterInfo:      clusterInfo,
		deltaComputer:    deltaComputer,
		mappings:         normalizedMappings,
		options:          options,
		started:          make(chan struct{}),
		stopped:          make(chan struct{}),
	}

	bridge.state.Store(int32(bridgeStateStopped))

	return bridge, nil
}

// normalizeMappings validates and normalizes key channel mappings.
func normalizeMappings(mappings []KeyChannelMapping, defaultPrefix string) ([]KeyChannelMapping, error) {
	const op = "WatchBridge.normalizeMappings"

	result := make([]KeyChannelMapping, len(mappings))
	for i, m := range mappings {
		if m.KeyPrefix == "" {
			return nil, NewWatchBridgeInvalidMappingError(op, "", "key prefix cannot be empty")
		}

		// Normalize channel prefix
		channelPrefix := m.ChannelPrefix
		if channelPrefix == "" {
			channelPrefix = defaultPrefix + m.KeyPrefix
		}

		result[i] = KeyChannelMapping{
			KeyPrefix:        m.KeyPrefix,
			ChannelPrefix:    channelPrefix,
			Filter:           m.Filter,
			EntityType:       m.EntityType,
			IncludeOldValues: m.IncludeOldValues,
		}
	}

	return result, nil
}

// Start begins the watch bridge event processing.
func (wb *WatchBridge) Start(ctx context.Context) error {
	const op = "WatchBridge.Start"

	// Attempt to transition from stopped to starting
	if !wb.state.CompareAndSwap(int32(bridgeStateStopped), int32(bridgeStateStarting)) {
		currentState := watchBridgeState(wb.state.Load())
		if currentState == bridgeStateRunning || currentState == bridgeStateStarting {
			return NewWatchBridgeAlreadyRunningError(op)
		}
		// If stopping, wait for it to complete and try again
		<-wb.stopped
		if !wb.state.CompareAndSwap(int32(bridgeStateStopped), int32(bridgeStateStarting)) {
			return NewWatchBridgeAlreadyRunningError(op)
		}
	}

	// Reset channels
	wb.started = make(chan struct{})
	wb.stopped = make(chan struct{})
	wb.watchesReady = make(chan struct{})

	// Create cancellable context
	wb.ctx, wb.cancel = context.WithCancel(ctx) // #nosec G118 -- cancel stored in wb.cancel, called in Stop()

	// Auto-create channels if enabled
	if wb.options.autoCreateChannels {
		for _, m := range wb.mappings {
			channelName := m.ChannelPrefix
			// Ensure channel name is valid (remove trailing slashes for channel name)
			channelName = strings.TrimSuffix(channelName, "/")
			if channelName == "" {
				channelName = wb.options.defaultChannelPrefix
			}

			_, err := wb.messagingService.GetChannel(ctx, channelName)
			if err != nil {
				if messaging.IsChannelNotFound(err) {
					_, createErr := wb.messagingService.CreateChannel(ctx, channelName, messaging.WithPersistence(false))
					if createErr != nil && !messaging.IsChannelExists(createErr) {
						wb.logger.WithError(createErr).WithField("channel", channelName).Warn("failed to auto-create channel")
					}
				}
			}
		}
	}

	// Track the number of mappings that need to subscribe
	numMappings := len(wb.mappings)
	if numMappings == 0 {
		// No mappings, immediately close watchesReady
		close(wb.watchesReady)
	} else {
		// Use a counter to track ready subscriptions
		var readyCount atomic.Int32
		readySignal := wb.watchesReady

		// Start watch subscriptions for each mapping
		for _, mapping := range wb.mappings {
			wb.watchLoopWg.Add(1)
			go wb.runWatchLoop(mapping, func() {
				// Signal when this watch is ready
				if readyCount.Add(1) == safeconv.IntToInt32(numMappings) {
					close(readySignal)
				}
			})
		}

		// Wait for all watches to be subscribed before signaling started
		select {
		case <-wb.watchesReady:
			// All watches are ready
		case <-wb.ctx.Done():
			// Context cancelled during startup
			wb.state.Store(int32(bridgeStateStopped))
			close(wb.stopped)
			return wb.ctx.Err()
		}
	}

	// Mark as running and signal started
	wb.state.Store(int32(bridgeStateRunning))
	close(wb.started)

	wb.logger.Info("watch bridge started")

	return nil
}

// Stop gracefully stops the watch bridge.
func (wb *WatchBridge) Stop(ctx context.Context) error {
	// Attempt to transition to stopping state
	currentState := watchBridgeState(wb.state.Load())
	if currentState == bridgeStateStopped {
		return nil // Already stopped
	}
	if currentState == bridgeStateStopping {
		// Wait for stop to complete
		select {
		case <-wb.stopped:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	if !wb.state.CompareAndSwap(int32(bridgeStateRunning), int32(bridgeStateStopping)) &&
		!wb.state.CompareAndSwap(int32(bridgeStateStarting), int32(bridgeStateStopping)) {
		return nil // Already stopping or stopped
	}

	// Cancel the context to stop all watch loops
	if wb.cancel != nil {
		wb.cancel()
	}

	// Wait for all watch loop goroutines to complete
	done := make(chan struct{})
	go func() {
		wb.watchLoopWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All goroutines finished
	case <-ctx.Done():
		// Context timeout - continue with shutdown
	}

	wb.state.Store(int32(bridgeStateStopped))
	close(wb.stopped)

	wb.logger.Info("watch bridge stopped")

	return nil
}

// IsRunning returns true if the watch bridge is currently running.
func (wb *WatchBridge) IsRunning() bool {
	return watchBridgeState(wb.state.Load()) == bridgeStateRunning
}

// WaitStarted waits for the bridge to start.
func (wb *WatchBridge) WaitStarted(ctx context.Context) error {
	select {
	case <-wb.started:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// GetStats returns current bridge statistics.
func (wb *WatchBridge) GetStats() WatchBridgeStats {
	return WatchBridgeStats{
		EventsReceived:         wb.eventsReceived.Load(),
		EventsPublished:        wb.eventsPublished.Load(),
		EventsFiltered:         wb.eventsFiltered.Load(),
		EventsDroppedNotLeader: wb.eventsDroppedNotLeader.Load(),
		PublishErrors:          wb.publishErrors.Load(),
	}
}

// AddMapping adds a new key-channel mapping at runtime.
// This requires the bridge to be stopped.
func (wb *WatchBridge) AddMapping(mapping KeyChannelMapping) error {
	const op = "WatchBridge.AddMapping"

	if wb.IsRunning() {
		return NewWatchBridgeNotRunningError(op, "add mapping while running")
	}

	if mapping.KeyPrefix == "" {
		return NewWatchBridgeInvalidMappingError(op, "", "key prefix cannot be empty")
	}

	// Normalize the mapping
	channelPrefix := mapping.ChannelPrefix
	if channelPrefix == "" {
		channelPrefix = wb.options.defaultChannelPrefix + mapping.KeyPrefix
	}

	wb.mappings = append(wb.mappings, KeyChannelMapping{
		KeyPrefix:        mapping.KeyPrefix,
		ChannelPrefix:    channelPrefix,
		Filter:           mapping.Filter,
		EntityType:       mapping.EntityType,
		IncludeOldValues: mapping.IncludeOldValues,
	})

	return nil
}

// runWatchLoop runs the watch loop for a specific mapping.
// The onReady callback is invoked once the watch subscription is established.
func (wb *WatchBridge) runWatchLoop(mapping KeyChannelMapping, onReady func()) {
	defer wb.watchLoopWg.Done()

	// Create a watch on the key prefix
	eventChan, err := wb.watchManager.Watch(wb.ctx, mapping.KeyPrefix, watch.WithPrefix())
	if err != nil {
		wb.logger.WithError(err).WithField("key_prefix", mapping.KeyPrefix).Error("failed to create watch")
		// Signal ready even on error to unblock Start()
		if onReady != nil {
			onReady()
		}
		return
	}

	// Signal that this watch subscription is ready
	if onReady != nil {
		onReady()
	}

	for {
		select {
		case <-wb.ctx.Done():
			return

		case event, ok := <-eventChan:
			if !ok {
				// Channel closed, recreate watch
				wb.logger.WithField("key_prefix", mapping.KeyPrefix).Debug("watch channel closed, recreating")
				eventChan, err = wb.watchManager.Watch(wb.ctx, mapping.KeyPrefix, watch.WithPrefix())
				if err != nil {
					wb.logger.WithError(err).WithField("key_prefix", mapping.KeyPrefix).Error("failed to recreate watch")
					return
				}
				continue
			}

			wb.eventsReceived.Add(1)

			// Apply event type filter
			if wb.shouldFilterEvent(event, mapping.Filter) {
				wb.eventsFiltered.Add(1)
				continue
			}

			// Check leader-only gating
			if wb.options.leaderOnly && wb.clusterInfo != nil && !wb.clusterInfo.IsLeader() {
				wb.eventsDroppedNotLeader.Add(1)
				continue
			}

			// Transform and publish the event
			wb.publishWatchEvent(event, mapping)
		}
	}
}

// shouldFilterEvent returns true if the event should be filtered out.
func (wb *WatchBridge) shouldFilterEvent(event watch.Event, filter WatchEventFilter) bool {
	switch event.Type {
	case watch.EventTypePut:
		return filter.NoPut
	case watch.EventTypeDelete:
		return filter.NoDelete
	default:
		return false
	}
}

// publishWatchEvent transforms a watch event and publishes it to the appropriate channel.
func (wb *WatchBridge) publishWatchEvent(event watch.Event, mapping KeyChannelMapping) {
	// Determine the channel name
	channelName := wb.buildChannelName(event.Key, mapping)

	// Build the message
	msg, err := wb.buildMessage(event, mapping)
	if err != nil {
		wb.logger.WithError(err).WithField("key", string(event.Key)).Warn("failed to build message")
		wb.publishErrors.Add(1)
		return
	}

	// Publish to the messaging service
	_, err = wb.messagingService.Publish(wb.ctx, channelName, msg)
	if err != nil {
		wb.logger.WithError(err).WithFields(map[string]interface{}{
			"channel": channelName,
			"key":     string(event.Key),
		}).Warn("failed to publish watch event")
		wb.publishErrors.Add(1)
		return
	}

	wb.eventsPublished.Add(1)

	wb.logger.WithFields(map[string]interface{}{
		"channel":  channelName,
		"key":      string(event.Key),
		"type":     event.Type,
		"revision": event.Revision,
	}).Debug("watch event published")
}

// buildChannelName builds the channel name for a watch event.
func (wb *WatchBridge) buildChannelName(key []byte, mapping KeyChannelMapping) string {
	// Use the channel prefix directly (which already includes the key prefix pattern)
	channelName := strings.TrimSuffix(mapping.ChannelPrefix, "/")
	if channelName == "" {
		channelName = wb.options.defaultChannelPrefix
	}
	return channelName
}

// WatchEventPayload is the payload structure for watch events published to channels.
type WatchEventPayload struct {
	// Type is the event type (PUT or DELETE).
	Type string `json:"type"`
	// Key is the key that changed.
	Key string `json:"key"`
	// Value is the new value (nil/empty for delete).
	Value []byte `json:"value,omitempty"`
	// OldValue is the previous value if available.
	OldValue []byte `json:"old_value,omitempty"`
	// Revision is the MVCC revision when this change occurred.
	Revision int64 `json:"revision"`
	// Version is the version number for this key.
	Version int64 `json:"version"`
	// Timestamp is when the event was bridged.
	Timestamp time.Time `json:"timestamp"`
	// Delta contains computed field changes if delta mode is enabled.
	Delta *messaging.DeltaPayload `json:"delta,omitempty"`
}

// buildMessage builds a messaging payload from a watch event.
func (wb *WatchBridge) buildMessage(event watch.Event, mapping KeyChannelMapping) ([]byte, error) {
	payload := &WatchEventPayload{
		Type:      event.Type,
		Key:       string(event.Key),
		Value:     event.Value,
		Revision:  event.Revision,
		Version:   event.Version,
		Timestamp: time.Now(),
	}

	// Compute delta if entity type is specified
	if mapping.EntityType != nil && wb.deltaComputer != nil && event.Type == watch.EventTypePut {
		delta := wb.computeDelta(event, mapping)
		if delta != nil {
			payload.Delta = delta
		}
	}

	return json.Marshal(payload)
}

// computeDelta computes a delta payload for the event if possible.
func (wb *WatchBridge) computeDelta(event watch.Event, mapping KeyChannelMapping) *messaging.DeltaPayload {
	if mapping.EntityType == nil || wb.deltaComputer == nil {
		return nil
	}

	// For PUT events, we need old and new values
	var oldData []byte
	newData := event.Value

	// The watch.Event doesn't include old value, so we can only compute
	// a "full" delta showing all fields of the new value
	if len(newData) == 0 {
		return nil
	}

	delta, err := wb.deltaComputer.ComputeBytes(oldData, newData, mapping.EntityType)
	if err != nil {
		wb.logger.WithError(err).WithField("key", string(event.Key)).Debug("delta computation failed")
		return nil
	}

	// Clear old values if not requested
	if !mapping.IncludeOldValues && delta != nil {
		for i := range delta.Changes {
			delta.Changes[i].OldValue = nil
		}
	}

	return delta
}

// GetMappings returns the current key-channel mappings.
func (wb *WatchBridge) GetMappings() []KeyChannelMapping {
	result := make([]KeyChannelMapping, len(wb.mappings))
	copy(result, wb.mappings)
	return result
}
