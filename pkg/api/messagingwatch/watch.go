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

// Package messagingwatch is the one mapping from a WatchChannel request onto
// the messaging service call. The gRPC, REST and MCP servers each decode
// their own wire form into a Request and open the watch through Watch, so the
// option building and the validation cannot drift between transports.
package messagingwatch

import (
	"context"
	"strings"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// channelNamePrefix prefixes the key prefix to name the channel a watch is
// served on.
const channelNamePrefix = "watch:"

// Request is a WatchChannel request in transport-neutral form.
type Request struct {
	KeyPrefix  string
	SessionID  string
	Filter     *messaging.SubscriptionFilter
	BufferSize int
}

// Options returns the subscribe options the request asks for.
func (r Request) Options() []messaging.SubscribeOption {
	var opts []messaging.SubscribeOption
	if r.SessionID != "" {
		opts = append(opts, messaging.WithSession(r.SessionID))
	}
	if r.Filter != nil {
		opts = append(opts, messaging.WithFilter(r.Filter))
	}
	if r.BufferSize > 0 {
		opts = append(opts, messaging.WithSubscriptionBufferSize(r.BufferSize))
	}
	return opts
}

// Watch validates the request and opens the watch subscription on svc.
func Watch(ctx context.Context, svc messaging.Service, req Request) (*messaging.Subscription, error) {
	if req.KeyPrefix == "" {
		return nil, api.NewRequiredFieldError("key_prefix")
	}
	return svc.WatchChannel(ctx, req.KeyPrefix, req.Options()...)
}

// ChannelName returns the name of the channel a watch on keyPrefix is
// served on.
func ChannelName(keyPrefix string) string {
	return channelNamePrefix + keyPrefix
}

// eventTypesByName maps the wire names of event types, as
// messaging.EventType.String spells them, to their values.
var eventTypesByName = map[string]messaging.EventType{
	messaging.EventTypePut.String():    messaging.EventTypePut,
	messaging.EventTypeDelete.String(): messaging.EventTypeDelete,
}

// ParseEventTypes maps wire event-type names, in any case, to their values.
// An unknown name is refused rather than dropped, so a filter never widens
// silently.
func ParseEventTypes(names []string) ([]messaging.EventType, error) {
	if len(names) == 0 {
		return nil, nil
	}
	types := make([]messaging.EventType, 0, len(names))
	for _, name := range names {
		et, ok := eventTypesByName[strings.ToUpper(name)]
		if !ok {
			return nil, api.NewInvalidInputError("unknown event type " + name)
		}
		types = append(types, et)
	}
	return types, nil
}
