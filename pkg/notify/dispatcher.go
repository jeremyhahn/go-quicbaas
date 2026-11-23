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
	"context"
	"sort"
	"strconv"
	"strings"
)

// withheldBody replaces a Sensitive body on an insecure channel when no
// secure channel delivered the notification.
const withheldBody = "Details are withheld from this channel; open the attention inbox."

// DispatcherConfig configures a Dispatcher.
type DispatcherConfig struct {
	// Channels are the destinations. At least one is required, and each
	// must have a unique, non-empty Name.
	Channels []Channel
}

// Dispatcher fans a notification out to every channel, secure channels
// first.
type Dispatcher struct {
	channels []Channel
}

// NewDispatcher validates cfg and returns a Dispatcher. Refusals are
// *ConfigError.
func NewDispatcher(cfg DispatcherConfig) (*Dispatcher, error) {
	if len(cfg.Channels) == 0 {
		return nil, &ConfigError{Field: "channels", Message: "at least one channel is required"}
	}
	seen := make(map[string]struct{}, len(cfg.Channels))
	channels := make([]Channel, 0, len(cfg.Channels))
	for i, ch := range cfg.Channels {
		field := "channels[" + strconv.Itoa(i) + "]"
		if ch == nil {
			return nil, &ConfigError{Field: field, Message: "is nil"}
		}
		name := ch.Name()
		if name == "" {
			return nil, &ConfigError{Field: field, Message: "has an empty name"}
		}
		if _, dup := seen[name]; dup {
			return nil, &ConfigError{Field: field, Message: "duplicates the channel name " + name}
		}
		seen[name] = struct{}{}
		channels = append(channels, ch)
	}
	sort.SliceStable(channels, func(i, j int) bool {
		return channels[i].Secure() && !channels[j].Secure()
	})
	return &Dispatcher{channels: channels}, nil
}

// Report lists which channels delivered a notification and which failed.
type Report struct {
	Delivered []string
	Failed    []ChannelFailure
}

// Notify validates n and delivers it to every secure channel, then to every
// insecure one. A Sensitive body never reaches an insecure channel: that
// channel receives a pointer to the secure channels that delivered it, or a
// pointer to the attention inbox when none did. The error is a
// *DeliveryError only when no channel delivered n; a partial success
// returns nil.
func (d *Dispatcher) Notify(ctx context.Context, n Notification) (Report, error) {
	if err := n.Validate(); err != nil {
		return Report{}, err
	}
	var report Report
	var secureDelivered []string
	for _, ch := range d.channels {
		if !ch.Secure() {
			continue
		}
		if err := ch.Deliver(ctx, n); err != nil {
			report.Failed = append(report.Failed, ChannelFailure{Channel: ch.Name(), Err: err})
			continue
		}
		secureDelivered = append(secureDelivered, ch.Name())
		report.Delivered = append(report.Delivered, ch.Name())
	}
	insecure := n
	if n.Sensitive {
		if len(secureDelivered) > 0 {
			insecure.Body = "Details were sent over " + strings.Join(secureDelivered, ", ") + "."
		} else {
			insecure.Body = withheldBody
		}
	}
	for _, ch := range d.channels {
		if ch.Secure() {
			continue
		}
		if err := ch.Deliver(ctx, insecure); err != nil {
			report.Failed = append(report.Failed, ChannelFailure{Channel: ch.Name(), Err: err})
			continue
		}
		report.Delivered = append(report.Delivered, ch.Name())
	}
	if len(report.Delivered) == 0 {
		return report, &DeliveryError{Key: n.Key, Failures: report.Failed}
	}
	return report, nil
}
