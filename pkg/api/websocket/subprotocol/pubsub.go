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

// Package subprotocol provides WebSocket subprotocol handlers.
package subprotocol

import (
	"sync"
)

// PubSubHub defines the interface for a topic-based pub/sub system.
// This interface is shared between MQTT and AMQP subprotocol handlers.
type PubSubHub interface {
	// Subscribe adds a subscription for a topic filter with given QoS.
	Subscribe(clientID string, topicFilter string, qos byte) error

	// Unsubscribe removes a subscription for a topic filter.
	Unsubscribe(clientID string, topicFilter string) error

	// Publish publishes a message to a topic.
	Publish(topic string, payload []byte, qos byte, retain bool) error

	// RegisterClient registers a client with a message callback.
	RegisterClient(clientID string, callback func(topic string, payload []byte, qos byte, retain bool)) error

	// UnregisterClient removes a client from the hub.
	UnregisterClient(clientID string) error
}

// InMemoryPubSubHub provides a simple in-memory pub/sub implementation.
type InMemoryPubSubHub struct {
	subscriptions map[string]map[string]byte                  // clientID -> topicFilter -> QoS
	callbacks     map[string]func(string, []byte, byte, bool) // clientID -> callback
	retained      map[string]retainedMessage                  // topic -> retained message
	mu            sync.RWMutex
}

// retainedMessage stores a retained message for a topic.
type retainedMessage struct {
	payload []byte
	qos     byte
}

// NewInMemoryPubSubHub creates a new in-memory pub/sub hub.
func NewInMemoryPubSubHub() *InMemoryPubSubHub {
	return &InMemoryPubSubHub{
		subscriptions: make(map[string]map[string]byte),
		callbacks:     make(map[string]func(string, []byte, byte, bool)),
		retained:      make(map[string]retainedMessage),
	}
}

// Subscribe implements PubSubHub.Subscribe.
func (h *InMemoryPubSubHub) Subscribe(clientID string, topicFilter string, qos byte) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.subscriptions[clientID] == nil {
		h.subscriptions[clientID] = make(map[string]byte)
	}
	h.subscriptions[clientID][topicFilter] = qos
	return nil
}

// Unsubscribe implements PubSubHub.Unsubscribe.
func (h *InMemoryPubSubHub) Unsubscribe(clientID string, topicFilter string) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if subs := h.subscriptions[clientID]; subs != nil {
		delete(subs, topicFilter)
	}
	return nil
}

// Publish implements PubSubHub.Publish.
func (h *InMemoryPubSubHub) Publish(topic string, payload []byte, qos byte, retain bool) error {
	h.mu.Lock()
	if retain {
		if len(payload) == 0 {
			delete(h.retained, topic)
		} else {
			h.retained[topic] = retainedMessage{payload: payload, qos: qos}
		}
	}

	// Collect matching subscribers
	type subscriber struct {
		clientID string
		callback func(string, []byte, byte, bool)
		qos      byte
	}
	var subscribers []subscriber

	for clientID, subs := range h.subscriptions {
		for filter, subQos := range subs {
			if topicMatches(filter, topic) {
				// Use minimum of publish QoS and subscription QoS
				effectiveQos := qos
				if subQos < effectiveQos {
					effectiveQos = subQos
				}
				if cb := h.callbacks[clientID]; cb != nil {
					subscribers = append(subscribers, subscriber{
						clientID: clientID,
						callback: cb,
						qos:      effectiveQos,
					})
				}
				break
			}
		}
	}
	h.mu.Unlock()

	// Deliver messages outside the lock
	for _, sub := range subscribers {
		sub.callback(topic, payload, sub.qos, retain)
	}

	return nil
}

// RegisterClient implements PubSubHub.RegisterClient.
func (h *InMemoryPubSubHub) RegisterClient(clientID string, callback func(topic string, payload []byte, qos byte, retain bool)) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.callbacks[clientID] = callback
	return nil
}

// UnregisterClient implements PubSubHub.UnregisterClient.
func (h *InMemoryPubSubHub) UnregisterClient(clientID string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.subscriptions, clientID)
	delete(h.callbacks, clientID)
	return nil
}

// topicMatches checks if a topic matches a filter (supports + and # wildcards).
func topicMatches(filter, topic string) bool {
	if filter == topic {
		return true
	}

	filterParts := splitTopic(filter)
	topicParts := splitTopic(topic)

	for i, fp := range filterParts {
		if fp == "#" {
			return true
		}
		if i >= len(topicParts) {
			return false
		}
		if fp != "+" && fp != topicParts[i] {
			return false
		}
	}

	return len(filterParts) == len(topicParts)
}

// splitTopic splits a topic string by '/'.
func splitTopic(topic string) []string {
	if topic == "" {
		return nil
	}
	var parts []string
	start := 0
	for i := 0; i < len(topic); i++ {
		if topic[i] == '/' {
			parts = append(parts, topic[start:i])
			start = i + 1
		}
	}
	parts = append(parts, topic[start:])
	return parts
}
