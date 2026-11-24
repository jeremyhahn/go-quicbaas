// Copyright (c) 2026 Jeremy Hahn
// Copyright (c) 2026 Automate The Things, LLC
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

package notify

import (
	"errors"
	"strconv"
	"strings"
)

var (
	// ErrInvalidNotification is wrapped by every notification refusal.
	ErrInvalidNotification = errors.New("notify: invalid notification")

	// ErrInvalidConfig is wrapped by every configuration refusal.
	ErrInvalidConfig = errors.New("notify: invalid config")

	// ErrDeliveryFailed is wrapped by every delivery failure.
	ErrDeliveryFailed = errors.New("notify: delivery failed")
)

// InvalidNotificationError reports a missing or invalid notification field.
type InvalidNotificationError struct {
	Field   string
	Message string
}

// Error implements error.
func (e *InvalidNotificationError) Error() string {
	return "notify: invalid notification " + e.Field + ": " + e.Message
}

// Unwrap returns ErrInvalidNotification.
func (e *InvalidNotificationError) Unwrap() error { return ErrInvalidNotification }

// ConfigError reports a missing or invalid configuration field.
type ConfigError struct {
	Field   string
	Message string
}

// Error implements error.
func (e *ConfigError) Error() string {
	return "notify: invalid config " + e.Field + ": " + e.Message
}

// Unwrap returns ErrInvalidConfig.
func (e *ConfigError) Unwrap() error { return ErrInvalidConfig }

// ChannelFailure is one channel's delivery error.
type ChannelFailure struct {
	Channel string
	Err     error
}

// DeliveryError reports a notification that no channel delivered.
type DeliveryError struct {
	Key      string
	Failures []ChannelFailure
}

// Error implements error.
func (e *DeliveryError) Error() string {
	parts := make([]string, 0, len(e.Failures))
	for _, f := range e.Failures {
		parts = append(parts, f.Channel+": "+errString(f.Err))
	}
	return "notify: notification " + e.Key + " reached no channel: " + strings.Join(parts, "; ")
}

// Unwrap returns ErrDeliveryFailed followed by each failure's error, so
// errors.Is matches the sentinel and every channel's own error.
func (e *DeliveryError) Unwrap() []error {
	out := make([]error, 0, len(e.Failures)+1)
	out = append(out, ErrDeliveryFailed)
	for _, f := range e.Failures {
		if f.Err != nil {
			out = append(out, f.Err)
		}
	}
	return out
}

// SlackStatusError reports a non-2xx answer from a Slack webhook.
type SlackStatusError struct {
	StatusCode int
}

// Error implements error.
func (e *SlackStatusError) Error() string {
	return "notify: slack webhook answered HTTP " + strconv.Itoa(e.StatusCode)
}

// Unwrap returns ErrDeliveryFailed.
func (e *SlackStatusError) Unwrap() error { return ErrDeliveryFailed }

// SlackTransportError reports a Slack webhook that could not be reached. It
// never contains the webhook URL, because the URL is the credential.
type SlackTransportError struct {
	Err error
}

// Error implements error.
func (e *SlackTransportError) Error() string {
	return "notify: slack webhook unreachable: " + errString(e.Err)
}

// Unwrap returns the underlying transport error.
func (e *SlackTransportError) Unwrap() error { return e.Err }

// errString renders err, tolerating nil.
func errString(err error) string {
	if err == nil {
		return "unknown error"
	}
	return err.Error()
}
