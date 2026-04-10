//go:build quicbaas_signal

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

package signal

import (
	"context"

	"github.com/jeremyhahn/go-signal/pkg/messages"

	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

const (
	// messageChannelPrefix is the channel name prefix for message delivery events.
	messageChannelPrefix = "signal:messages:"

	// accountChannelPrefix is the channel name prefix for account lifecycle events.
	accountChannelPrefix = "signal:accounts:"
)

// EventBridge bridges Signal protocol events to QuicBaaS messaging channels,
// enabling real-time push notifications for connected clients.
type EventBridge struct {
	messaging messaging.Service
	logger    *logger.Logger
}

// NewEventBridge creates a new EventBridge that publishes Signal events to the
// given messaging service.
func NewEventBridge(ms messaging.Service, log *logger.Logger) *EventBridge {
	return &EventBridge{
		messaging: ms,
		logger:    log.WithField("component", "signal-event-bridge"),
	}
}

// PublishMessageReceived publishes a message delivery event to the
// signal:messages:{accountUUID} channel. This enables connected clients
// to receive real-time notifications when new messages arrive.
func (b *EventBridge) PublishMessageReceived(ctx context.Context, accountUUID string, envelope *messages.Envelope) error {
	channel := messageChannelPrefix + accountUUID

	_, err := b.messaging.Publish(ctx, channel, envelope)
	if err != nil {
		b.logger.WithField("accountUUID", accountUUID).
			WithField("channel", channel).
			WithError(err).
			Error("failed to publish message received event")
		return err
	}

	b.logger.WithField("accountUUID", accountUUID).
		WithField("channel", channel).
		Debug("published message received event")

	return nil
}

// PublishAccountEvent publishes an account lifecycle event to the
// signal:accounts:{eventType} channel. Event types include "registered",
// "deleted", and "updated".
func (b *EventBridge) PublishAccountEvent(ctx context.Context, eventType string, accountUUID string) error {
	channel := accountChannelPrefix + eventType

	payload := struct {
		AccountUUID string `json:"accountUuid"`
		EventType   string `json:"eventType"`
	}{
		AccountUUID: accountUUID,
		EventType:   eventType,
	}

	_, err := b.messaging.Publish(ctx, channel, payload)
	if err != nil {
		b.logger.WithField("accountUUID", accountUUID).
			WithField("eventType", eventType).
			WithField("channel", channel).
			WithError(err).
			Error("failed to publish account event")
		return err
	}

	b.logger.WithField("accountUUID", accountUUID).
		WithField("eventType", eventType).
		WithField("channel", channel).
		Debug("published account event")

	return nil
}
