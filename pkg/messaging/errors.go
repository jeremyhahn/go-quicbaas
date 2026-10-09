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
	"strings"

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

	// ErrCodePushHubStopTimeout indicates a hub was still running when Stop's deadline fired.
	ErrCodePushHubStopTimeout
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

	// ErrHubStopTimeout indicates a hub did not stop before Stop's deadline.
	ErrHubStopTimeout = stderrors.New("hub did not stop before the deadline")

	// ErrChannelNotFound indicates the channel does not exist.
	ErrChannelNotFound = stderrors.New("channel not found")

	// ErrChannelExists indicates the channel already exists.
	ErrChannelExists = stderrors.New("channel already exists")

	// ErrHubStopped indicates the hub's loop is not running, so a queued
	// request can never be answered.
	ErrHubStopped = stderrors.New("hub stopped")

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

// HubStopTimeoutError represents hubs that were still running when Stop's
// deadline fired, so they were abandoned rather than stopped.
type HubStopTimeoutError struct {
	Hubs []string
}

// Error implements the error interface.
func (e *HubStopTimeoutError) Error() string {
	return "hubs did not stop before the deadline: " + strings.Join(e.Hubs, ", ")
}

// Is makes errors.Is(err, ErrHubStopTimeout) match.
func (e *HubStopTimeoutError) Is(target error) bool {
	return target == ErrHubStopTimeout
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

// NewChannelNotFoundError creates a new QBError for channel not found.
func NewChannelNotFoundError(op, channel string) *errors.QBError {
	return &errors.QBError{
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

// NewChannelExistsError creates a new QBError for channel already exists.
func NewChannelExistsError(op, channel string) *errors.QBError {
	return &errors.QBError{
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

// NewHubBufferFullError creates a new QBError for hub buffer full.
func NewHubBufferFullError(op, channel string, bufferSize int) *errors.QBError {
	return &errors.QBError{
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

// NewClientDisconnectedError creates a new QBError for client disconnected.
func NewClientDisconnectedError(op, clientID string) *errors.QBError {
	return &errors.QBError{
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

// NewClientSlowError creates a new QBError for slow client.
func NewClientSlowError(op, clientID string, droppedCount int64, bufferCapacity int) *errors.QBError {
	return &errors.QBError{
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

// NewInvalidCodecError creates a new QBError for invalid codec.
func NewInvalidCodecError(op, codec string) *errors.QBError {
	return &errors.QBError{
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

// NewSubscriptionNotFoundError creates a new QBError for subscription not found.
func NewSubscriptionNotFoundError(op, subscriptionID string) *errors.QBError {
	return &errors.QBError{
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

// NewDedupKeyExistsError creates a new QBError for duplicate message.
func NewDedupKeyExistsError(op, key, firstSeen string, count int64) *errors.QBError {
	return &errors.QBError{
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

// NewServiceNotRunningError creates a new QBError for service not running.
func NewServiceNotRunningError(op, operation string) *errors.QBError {
	return &errors.QBError{
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

// NewHubStopTimeoutError creates a new QBError naming the hubs that did not stop.
func NewHubStopTimeoutError(op string, hubs ...string) *errors.QBError {
	return &errors.QBError{
		Code: ErrCodePushHubStopTimeout,
		Op:   op,
		Err:  &HubStopTimeoutError{Hubs: hubs},
		Context: map[string]interface{}{
			"hubs": hubs,
		},
	}
}

// IsHubStopTimeout returns true if the error reports a hub abandoned at Stop's deadline.
func IsHubStopTimeout(err error) bool {
	return isPushError[*HubStopTimeoutError](err, ErrCodePushHubStopTimeout, ErrHubStopTimeout)
}

// NewInvalidChannelNameError creates a new QBError for invalid channel name.
func NewInvalidChannelNameError(op, name, reason string) *errors.QBError {
	return &errors.QBError{
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

// NewInvalidMessageIDError creates a new QBError for invalid message ID.
func NewInvalidMessageIDError(op, id, reason string) *errors.QBError {
	return &errors.QBError{
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

// NewMaxSubscriptionsReachedError creates a new QBError for max subscriptions reached.
func NewMaxSubscriptionsReachedError(op, channel string, max int) *errors.QBError {
	return &errors.QBError{
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

// NewClientNotRegisteredError creates a new QBError for client not registered.
func NewClientNotRegisteredError(op, clientID string) *errors.QBError {
	return &errors.QBError{
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

// isPushError reports whether err belongs to one push error family.
//
// A FAMILY HAS THREE INDEPENDENT SIGNATURES AND ANY ONE OF THEM IS ENOUGH: the
// concrete type T, a QBError carrying the family's code, or the family's
// sentinel anywhere in the chain. They are independent because the package
// produces all three — NewChannelNotFoundError builds the typed form, the hub's
// handlers build a QBError with a GENERIC code (errors.ErrNotFound) wrapping
// the sentinel, and some call sites return the bare sentinel.
//
// NO SIGNATURE IS TERMINAL, WHICH IS THE WHOLE POINT (#695). Each predicate
// used to return de.Code == code from inside the QBError branch, so a
// non-matching code answered false and never reached the sentinel check below
// it — and the hub's own channel-not-found error is exactly that shape.
// IsChannelNotFound therefore denied the errors handleDeleteChannel and
// handleUnsubscribe produce, and every caller branching on the public predicate
// mis-handled a hub-originated miss.
//
// ONE DEFINITION FOR THE WHOLE FAMILY, because thirteen copies of a
// classification rule is thirteen places for the next such defect to hide, and
// #695 was found in one of them after being written into all thirteen.
func isPushError[T error](err error, code errors.ErrorCode, sentinel error) bool {
	if err == nil {
		return false
	}
	var typed T
	if stderrors.As(err, &typed) {
		return true
	}
	var qb *errors.QBError
	if stderrors.As(err, &qb) && qb.Code == code {
		return true
	}
	return stderrors.Is(err, sentinel)
}

// IsChannelNotFound returns true if the error is a channel not found error.
func IsChannelNotFound(err error) bool {
	return isPushError[*ChannelNotFoundError](err, ErrCodePushChannelNotFound, ErrChannelNotFound)
}

// IsChannelExists returns true if the error is a channel exists error.
func IsChannelExists(err error) bool {
	return isPushError[*ChannelExistsError](err, ErrCodePushChannelExists, ErrChannelExists)
}

// IsHubBufferFull returns true if the error is a hub buffer full error.
func IsHubBufferFull(err error) bool {
	return isPushError[*HubBufferFullError](err, ErrCodePushHubBufferFull, ErrHubOverloaded)
}

// IsClientDisconnected returns true if the error is a client disconnected error.
func IsClientDisconnected(err error) bool {
	return isPushError[*ClientDisconnectedError](err, ErrCodePushClientDisconnected, ErrClientDisconnected)
}

// IsClientSlow returns true if the error is a client slow error.
func IsClientSlow(err error) bool {
	return isPushError[*ClientSlowError](err, ErrCodePushClientSlow, ErrSlowConsumer)
}

// IsInvalidCodec returns true if the error is an invalid codec error.
func IsInvalidCodec(err error) bool {
	return isPushError[*InvalidCodecError](err, ErrCodePushInvalidCodec, ErrInvalidCodec)
}

// IsSubscriptionNotFound returns true if the error is a subscription not found error.
func IsSubscriptionNotFound(err error) bool {
	return isPushError[*SubscriptionNotFoundError](err, ErrCodePushSubscriptionNotFound, ErrSubscriptionNotFound)
}

// IsDedupKeyExists returns true if the error is a dedup key exists error.
func IsDedupKeyExists(err error) bool {
	return isPushError[*DedupKeyExistsError](err, ErrCodePushDedupKeyExists, ErrDedupKeyExists)
}

// IsServiceNotRunning returns true if the error is a service not running error.
func IsServiceNotRunning(err error) bool {
	return isPushError[*ServiceNotRunningError](err, ErrCodePushServiceNotRunning, ErrServiceNotRunning)
}

// IsInvalidChannelName returns true if the error is an invalid channel name error.
func IsInvalidChannelName(err error) bool {
	return isPushError[*InvalidChannelNameError](err, ErrCodePushInvalidChannelName, ErrInvalidChannelName)
}

// IsInvalidMessageID returns true if the error is an invalid message ID error.
func IsInvalidMessageID(err error) bool {
	return isPushError[*InvalidMessageIDError](err, ErrCodePushInvalidMessageID, ErrInvalidMessageID)
}

// IsMaxSubscriptionsReached returns true if the error is a max subscriptions reached error.
func IsMaxSubscriptionsReached(err error) bool {
	return isPushError[*MaxSubscriptionsReachedError](err, ErrCodePushMaxSubscriptionsReached, ErrMaxSubscriptionsReached)
}

// IsClientNotRegistered returns true if the error is a client not registered error.
func IsClientNotRegistered(err error) bool {
	return isPushError[*ClientNotRegisteredError](err, ErrCodePushClientNotRegistered, ErrClientNotRegistered)
}
