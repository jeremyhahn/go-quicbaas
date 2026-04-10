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
	stderrors "errors"

	"github.com/jeremyhahn/go-quicbaas/pkg/errors"
)

// Push-specific error codes extending the base error code system.
const (
	// ErrCodePushChannelNotFound indicates the requested channel does not exist.
	ErrCodePushChannelNotFound errors.ErrorCode = 1000 + iota

	// ErrCodePushChannelExists indicates the channel already exists.
	ErrCodePushChannelExists

	// ErrCodePushHubBufferFull indicates the hub's message buffer is full.
	ErrCodePushHubBufferFull

	// ErrCodePushClientDisconnected indicates the client has disconnected.
	ErrCodePushClientDisconnected

	// ErrCodePushClientSlow indicates the client is consuming messages too slowly.
	ErrCodePushClientSlow

	// ErrCodePushInvalidCodec indicates an invalid or unsupported codec was specified.
	ErrCodePushInvalidCodec

	// ErrCodePushSubscriptionNotFound indicates the subscription does not exist.
	ErrCodePushSubscriptionNotFound

	// ErrCodePushDedupKeyExists indicates a duplicate message was detected.
	ErrCodePushDedupKeyExists

	// ErrCodePushServiceNotRunning indicates the push service is not running.
	ErrCodePushServiceNotRunning

	// ErrCodePushInvalidChannelName indicates an invalid channel name was provided.
	ErrCodePushInvalidChannelName

	// ErrCodePushInvalidMessageID indicates an invalid message ID was provided.
	ErrCodePushInvalidMessageID

	// ErrCodePushMaxSubscriptionsReached indicates the maximum subscriptions limit was reached.
	ErrCodePushMaxSubscriptionsReached

	// ErrCodePushClientNotRegistered indicates the client is not registered with the hub.
	ErrCodePushClientNotRegistered
)

// Sentinel errors for the push notification system.
// These are used with errors.Is for error type checking.
var (
	// ErrNilClient indicates a nil client was provided.
	ErrNilClient = stderrors.New("client is nil")

	// ErrNilMessage indicates a nil message was provided.
	ErrNilMessage = stderrors.New("message is nil")

	// ErrEmptyChannelName indicates an empty channel name was provided.
	ErrEmptyChannelName = stderrors.New("channel name is empty")

	// ErrChannelNotFound indicates the channel does not exist.
	ErrChannelNotFound = stderrors.New("channel not found")

	// ErrChannelExists indicates the channel already exists.
	ErrChannelExists = stderrors.New("channel already exists")

	// ErrClientNotRegistered indicates the client is not registered with the hub.
	ErrClientNotRegistered = stderrors.New("client not registered")

	// ErrMaxSubscribersReached indicates the channel has reached its maximum subscriber limit.
	ErrMaxSubscribersReached = stderrors.New("maximum subscribers reached")

	// ErrHubOverloaded indicates the hub is overloaded and cannot accept more requests.
	ErrHubOverloaded = stderrors.New("hub overloaded")

	// ErrHubNotRunning indicates the hub is not running.
	ErrHubNotRunning = stderrors.New("hub not running")

	// ErrClientDisconnected indicates the client has been disconnected.
	ErrClientDisconnected = stderrors.New("client disconnected")

	// ErrSlowConsumer indicates the client is consuming messages too slowly.
	ErrSlowConsumer = stderrors.New("slow consumer")

	// ErrInvalidCodec indicates an invalid or unsupported codec was specified.
	ErrInvalidCodec = stderrors.New("invalid codec")

	// ErrSubscriptionNotFound indicates the subscription does not exist.
	ErrSubscriptionNotFound = stderrors.New("subscription not found")

	// ErrDedupKeyExists indicates a duplicate message was detected.
	ErrDedupKeyExists = stderrors.New("dedup key exists")

	// ErrServiceNotRunning indicates the push service is not running.
	ErrServiceNotRunning = stderrors.New("service not running")

	// ErrInvalidChannelName indicates an invalid channel name was provided.
	ErrInvalidChannelName = stderrors.New("invalid channel name")

	// ErrInvalidMessageID indicates an invalid message ID was provided.
	ErrInvalidMessageID = stderrors.New("invalid message ID")

	// ErrMaxSubscriptionsReached indicates the maximum subscriptions limit was reached.
	ErrMaxSubscriptionsReached = stderrors.New("max subscriptions reached")

	// ErrHashPoolInvalidType indicates the hash pool returned an invalid type.
	ErrHashPoolInvalidType = stderrors.New("hash pool returned invalid type")
)

// ChannelNotFoundError represents an error when a channel is not found.
type ChannelNotFoundError struct {
	Channel string
}

// Error implements the error interface.
func (e *ChannelNotFoundError) Error() string {
	return "push channel not found: " + e.Channel
}

// ChannelExistsError represents an error when a channel already exists.
type ChannelExistsError struct {
	Channel string
}

// Error implements the error interface.
func (e *ChannelExistsError) Error() string {
	return "push channel already exists: " + e.Channel
}

// HubBufferFullError represents an error when the hub buffer is full.
type HubBufferFullError struct {
	BufferSize int
	Channel    string
}

// Error implements the error interface.
func (e *HubBufferFullError) Error() string {
	return "push hub buffer full for channel: " + e.Channel
}

// ClientDisconnectedError represents an error when a client has disconnected.
type ClientDisconnectedError struct {
	ClientID string
}

// Error implements the error interface.
func (e *ClientDisconnectedError) Error() string {
	return "push client disconnected: " + e.ClientID
}

// ClientSlowError represents an error when a client is consuming too slowly.
type ClientSlowError struct {
	ClientID       string
	DroppedCount   int64
	BufferCapacity int
}

// Error implements the error interface.
func (e *ClientSlowError) Error() string {
	return "push client too slow: " + e.ClientID
}

// InvalidCodecError represents an error when an invalid codec is specified.
type InvalidCodecError struct {
	Codec string
}

// Error implements the error interface.
func (e *InvalidCodecError) Error() string {
	return "invalid push codec: " + e.Codec
}

// SubscriptionNotFoundError represents an error when a subscription is not found.
type SubscriptionNotFoundError struct {
	SubscriptionID string
}

// Error implements the error interface.
func (e *SubscriptionNotFoundError) Error() string {
	return "push subscription not found: " + e.SubscriptionID
}

// DedupKeyExistsError represents an error when a duplicate message is detected.
type DedupKeyExistsError struct {
	Key       string
	FirstSeen string
	Count     int64
}

// Error implements the error interface.
func (e *DedupKeyExistsError) Error() string {
	return "push dedup key exists: " + e.Key
}

// ServiceNotRunningError represents an error when the service is not running.
type ServiceNotRunningError struct {
	Operation string
}

// Error implements the error interface.
func (e *ServiceNotRunningError) Error() string {
	return "push service not running, cannot perform: " + e.Operation
}

// InvalidChannelNameError represents an error when a channel name is invalid.
type InvalidChannelNameError struct {
	Name   string
	Reason string
}

// Error implements the error interface.
func (e *InvalidChannelNameError) Error() string {
	return "invalid push channel name '" + e.Name + "': " + e.Reason
}

// InvalidMessageIDError represents an error when a message ID is invalid.
type InvalidMessageIDError struct {
	ID     string
	Reason string
}

// Error implements the error interface.
func (e *InvalidMessageIDError) Error() string {
	return "invalid push message ID '" + e.ID + "': " + e.Reason
}

// MaxSubscriptionsReachedError represents an error when max subscriptions limit is reached.
type MaxSubscriptionsReachedError struct {
	Max     int
	Channel string
}

// Error implements the error interface.
func (e *MaxSubscriptionsReachedError) Error() string {
	return "maximum subscriptions reached for channel: " + e.Channel
}

// ClientNotRegisteredError represents an error when a client is not registered.
type ClientNotRegisteredError struct {
	ClientID string
}

// Error implements the error interface.
func (e *ClientNotRegisteredError) Error() string {
	return "push client not registered: " + e.ClientID
}

// NewChannelNotFoundError creates a new DragonError for channel not found.
func NewChannelNotFoundError(op, channel string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodePushChannelNotFound,
		Op:   op,
		Err: &ChannelNotFoundError{
			Channel: channel,
		},
		Context: map[string]interface{}{
			"channel": channel,
		},
	}
}

// NewChannelExistsError creates a new DragonError for channel already exists.
func NewChannelExistsError(op, channel string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodePushChannelExists,
		Op:   op,
		Err: &ChannelExistsError{
			Channel: channel,
		},
		Context: map[string]interface{}{
			"channel": channel,
		},
	}
}

// NewHubBufferFullError creates a new DragonError for hub buffer full.
func NewHubBufferFullError(op, channel string, bufferSize int) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodePushHubBufferFull,
		Op:   op,
		Err: &HubBufferFullError{
			Channel:    channel,
			BufferSize: bufferSize,
		},
		Context: map[string]interface{}{
			"channel":     channel,
			"buffer_size": bufferSize,
		},
		Retry: true,
	}
}

// NewClientDisconnectedError creates a new DragonError for client disconnected.
func NewClientDisconnectedError(op, clientID string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodePushClientDisconnected,
		Op:   op,
		Err: &ClientDisconnectedError{
			ClientID: clientID,
		},
		Context: map[string]interface{}{
			"client_id": clientID,
		},
	}
}

// NewClientSlowError creates a new DragonError for slow client.
func NewClientSlowError(op, clientID string, droppedCount int64, bufferCapacity int) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodePushClientSlow,
		Op:   op,
		Err: &ClientSlowError{
			ClientID:       clientID,
			DroppedCount:   droppedCount,
			BufferCapacity: bufferCapacity,
		},
		Context: map[string]interface{}{
			"client_id":       clientID,
			"dropped_count":   droppedCount,
			"buffer_capacity": bufferCapacity,
		},
	}
}

// NewInvalidCodecError creates a new DragonError for invalid codec.
func NewInvalidCodecError(op, codec string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodePushInvalidCodec,
		Op:   op,
		Err: &InvalidCodecError{
			Codec: codec,
		},
		Context: map[string]interface{}{
			"codec": codec,
		},
	}
}

// NewSubscriptionNotFoundError creates a new DragonError for subscription not found.
func NewSubscriptionNotFoundError(op, subscriptionID string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodePushSubscriptionNotFound,
		Op:   op,
		Err: &SubscriptionNotFoundError{
			SubscriptionID: subscriptionID,
		},
		Context: map[string]interface{}{
			"subscription_id": subscriptionID,
		},
	}
}

// NewDedupKeyExistsError creates a new DragonError for duplicate message.
func NewDedupKeyExistsError(op, key, firstSeen string, count int64) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodePushDedupKeyExists,
		Op:   op,
		Err: &DedupKeyExistsError{
			Key:       key,
			FirstSeen: firstSeen,
			Count:     count,
		},
		Context: map[string]interface{}{
			"dedup_key":  key,
			"first_seen": firstSeen,
			"count":      count,
		},
	}
}

// NewServiceNotRunningError creates a new DragonError for service not running.
func NewServiceNotRunningError(op, operation string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodePushServiceNotRunning,
		Op:   op,
		Err: &ServiceNotRunningError{
			Operation: operation,
		},
		Context: map[string]interface{}{
			"operation": operation,
		},
	}
}

// NewInvalidChannelNameError creates a new DragonError for invalid channel name.
func NewInvalidChannelNameError(op, name, reason string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodePushInvalidChannelName,
		Op:   op,
		Err: &InvalidChannelNameError{
			Name:   name,
			Reason: reason,
		},
		Context: map[string]interface{}{
			"name":   name,
			"reason": reason,
		},
	}
}

// NewInvalidMessageIDError creates a new DragonError for invalid message ID.
func NewInvalidMessageIDError(op, id, reason string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodePushInvalidMessageID,
		Op:   op,
		Err: &InvalidMessageIDError{
			ID:     id,
			Reason: reason,
		},
		Context: map[string]interface{}{
			"id":     id,
			"reason": reason,
		},
	}
}

// NewMaxSubscriptionsReachedError creates a new DragonError for max subscriptions reached.
func NewMaxSubscriptionsReachedError(op, channel string, max int) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodePushMaxSubscriptionsReached,
		Op:   op,
		Err: &MaxSubscriptionsReachedError{
			Max:     max,
			Channel: channel,
		},
		Context: map[string]interface{}{
			"max":     max,
			"channel": channel,
		},
	}
}

// NewClientNotRegisteredError creates a new DragonError for client not registered.
func NewClientNotRegisteredError(op, clientID string) *errors.DragonError {
	return &errors.DragonError{
		Code: ErrCodePushClientNotRegistered,
		Op:   op,
		Err: &ClientNotRegisteredError{
			ClientID: clientID,
		},
		Context: map[string]interface{}{
			"client_id": clientID,
		},
	}
}

// IsChannelNotFound returns true if the error is a channel not found error.
func IsChannelNotFound(err error) bool {
	if err == nil {
		return false
	}
	var cnf *ChannelNotFoundError
	if stderrors.As(err, &cnf) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodePushChannelNotFound
	}
	return stderrors.Is(err, ErrChannelNotFound)
}

// IsChannelExists returns true if the error is a channel exists error.
func IsChannelExists(err error) bool {
	if err == nil {
		return false
	}
	var ce *ChannelExistsError
	if stderrors.As(err, &ce) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodePushChannelExists
	}
	return stderrors.Is(err, ErrChannelExists)
}

// IsHubBufferFull returns true if the error is a hub buffer full error.
func IsHubBufferFull(err error) bool {
	if err == nil {
		return false
	}
	var hbf *HubBufferFullError
	if stderrors.As(err, &hbf) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodePushHubBufferFull
	}
	return stderrors.Is(err, ErrHubOverloaded)
}

// IsClientDisconnected returns true if the error is a client disconnected error.
func IsClientDisconnected(err error) bool {
	if err == nil {
		return false
	}
	var cd *ClientDisconnectedError
	if stderrors.As(err, &cd) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodePushClientDisconnected
	}
	return stderrors.Is(err, ErrClientDisconnected)
}

// IsClientSlow returns true if the error is a client slow error.
func IsClientSlow(err error) bool {
	if err == nil {
		return false
	}
	var cs *ClientSlowError
	if stderrors.As(err, &cs) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodePushClientSlow
	}
	return stderrors.Is(err, ErrSlowConsumer)
}

// IsInvalidCodec returns true if the error is an invalid codec error.
func IsInvalidCodec(err error) bool {
	if err == nil {
		return false
	}
	var ic *InvalidCodecError
	if stderrors.As(err, &ic) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodePushInvalidCodec
	}
	return stderrors.Is(err, ErrInvalidCodec)
}

// IsSubscriptionNotFound returns true if the error is a subscription not found error.
func IsSubscriptionNotFound(err error) bool {
	if err == nil {
		return false
	}
	var snf *SubscriptionNotFoundError
	if stderrors.As(err, &snf) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodePushSubscriptionNotFound
	}
	return stderrors.Is(err, ErrSubscriptionNotFound)
}

// IsDedupKeyExists returns true if the error is a dedup key exists error.
func IsDedupKeyExists(err error) bool {
	if err == nil {
		return false
	}
	var dke *DedupKeyExistsError
	if stderrors.As(err, &dke) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodePushDedupKeyExists
	}
	return stderrors.Is(err, ErrDedupKeyExists)
}

// IsServiceNotRunning returns true if the error is a service not running error.
func IsServiceNotRunning(err error) bool {
	if err == nil {
		return false
	}
	var snr *ServiceNotRunningError
	if stderrors.As(err, &snr) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodePushServiceNotRunning
	}
	return stderrors.Is(err, ErrServiceNotRunning)
}

// IsInvalidChannelName returns true if the error is an invalid channel name error.
func IsInvalidChannelName(err error) bool {
	if err == nil {
		return false
	}
	var icn *InvalidChannelNameError
	if stderrors.As(err, &icn) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodePushInvalidChannelName
	}
	return stderrors.Is(err, ErrInvalidChannelName)
}

// IsInvalidMessageID returns true if the error is an invalid message ID error.
func IsInvalidMessageID(err error) bool {
	if err == nil {
		return false
	}
	var imi *InvalidMessageIDError
	if stderrors.As(err, &imi) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodePushInvalidMessageID
	}
	return stderrors.Is(err, ErrInvalidMessageID)
}

// IsMaxSubscriptionsReached returns true if the error is a max subscriptions reached error.
func IsMaxSubscriptionsReached(err error) bool {
	if err == nil {
		return false
	}
	var msr *MaxSubscriptionsReachedError
	if stderrors.As(err, &msr) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodePushMaxSubscriptionsReached
	}
	return stderrors.Is(err, ErrMaxSubscriptionsReached)
}

// IsClientNotRegistered returns true if the error is a client not registered error.
func IsClientNotRegistered(err error) bool {
	if err == nil {
		return false
	}
	var cnr *ClientNotRegisteredError
	if stderrors.As(err, &cnr) {
		return true
	}
	var de *errors.DragonError
	if stderrors.As(err, &de) {
		return de.Code == ErrCodePushClientNotRegistered
	}
	return stderrors.Is(err, ErrClientNotRegistered)
}
