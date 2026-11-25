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

package signalbridge

import (
	"context"
	"errors"
	"strconv"

	"github.com/jeremyhahn/go-quicbaas/pkg/notify"
)

// notifyChannelName is the Name of every NotifyChannel.
const notifyChannelName = "signal"

// NotifySender sends one Signal message. SignalClient implements it.
type NotifySender interface {
	Send(ctx context.Context, msg OutboundMessage) error
}

var _ NotifySender = (*SignalClient)(nil)

var _ notify.Channel = (*NotifyChannel)(nil)

// NotifyTarget is one Signal direct-message recipient of notifications.
// Only direct messages are supported: the Signal protocol adapter has no
// group messaging, so a group target is refused.
type NotifyTarget struct {
	// ConversationID is the recipient's ACI.
	ConversationID string

	// Kind must be ConversationDM; ConversationGroup is refused.
	Kind ConversationKind

	// SubjectID is the QuicBaaS subject the recipient's ACI is linked to.
	// Required: the recipient is a person who must pass the permission
	// check.
	SubjectID string
}

// NotifyChannelConfig configures a NotifyChannel.
type NotifyChannelConfig struct {
	// Sender sends the messages. Required.
	Sender NotifySender

	// Targets are the direct-message recipients of notifications. At
	// least one is required; a group target is refused, so list each
	// member as a direct-message target.
	Targets []NotifyTarget

	// Authorizer decides whether a target's subject may receive a
	// tenant's notifications. Required.
	Authorizer notify.RecipientAuthorizer
}

// NotifyChannel delivers notifications over Signal, the end-to-end
// encrypted channel the dispatcher always tries first.
type NotifyChannel struct {
	sender     NotifySender
	targets    []NotifyTarget
	authorizer notify.RecipientAuthorizer
}

// NewNotifyChannel validates cfg and returns a NotifyChannel. Every refusal
// is an *Error of kind ErrKindInvalidInput.
func NewNotifyChannel(cfg NotifyChannelConfig) (*NotifyChannel, error) {
	const op = "signalbridge.NewNotifyChannel"
	if cfg.Sender == nil {
		return nil, newError(op, ErrKindInvalidInput, "sender is required")
	}
	if len(cfg.Targets) == 0 {
		return nil, newError(op, ErrKindInvalidInput, "at least one target is required")
	}
	for i, t := range cfg.Targets {
		at := "target " + strconv.Itoa(i)
		if t.ConversationID == "" {
			return nil, newError(op, ErrKindInvalidInput, at+": conversation id is required")
		}
		switch t.Kind {
		case ConversationDM:
			if t.SubjectID == "" {
				return nil, newError(op, ErrKindInvalidInput, at+": a direct-message target needs a subject id")
			}
			if cfg.Authorizer == nil {
				return nil, newError(op, ErrKindInvalidInput, at+": a direct-message target needs a recipient authorizer")
			}
		case ConversationGroup:
			return nil, newError(op, ErrKindInvalidInput, at+": group delivery is not supported; list each member as a direct-message target")
		default:
			return nil, newError(op, ErrKindInvalidInput, at+": kind must be direct message")
		}
	}
	return &NotifyChannel{
		sender:     cfg.Sender,
		targets:    append([]NotifyTarget(nil), cfg.Targets...),
		authorizer: cfg.Authorizer,
	}, nil
}

// Name returns "signal".
func (c *NotifyChannel) Name() string { return notifyChannelName }

// Secure returns true: Signal is end-to-end encrypted.
func (c *NotifyChannel) Secure() bool { return true }

// Deliver sends notify.Text(n) as a direct message to every target in
// order; ConversationID is the recipient's ACI. A target whose subject the
// authorizer does not permit, or whose check errors, is skipped (fail
// closed). Deliver returns nil when at least one target received the
// message; otherwise an *Error of kind ErrKindUnauthorized when every target
// was refused by the authorizer, else ErrKindTransport. Errors name the
// subject, never the recipient's ACI.
func (c *NotifyChannel) Deliver(ctx context.Context, n notify.Notification) error {
	const op = "signalbridge.NotifyChannel.Deliver"
	body := notify.Text(n)
	var errs []error
	delivered := 0
	refused := 0
	for i, t := range c.targets {
		ok, err := c.authorizer.MayReceive(ctx, n.TenantID, t.SubjectID)
		if err != nil {
			refused++
			errs = append(errs, wrapError(op, ErrKindUnauthorized, err,
				"subject "+t.SubjectID+" could not be checked for tenant notifications"))
			continue
		}
		if !ok {
			refused++
			errs = append(errs, newError(op, ErrKindUnauthorized,
				"subject "+t.SubjectID+" may not receive tenant notifications"))
			continue
		}
		err = c.sender.Send(ctx, OutboundMessage{ConversationID: t.ConversationID, Kind: ConversationDM, Body: body})
		if err != nil {
			errs = append(errs, wrapError(op, ErrKindTransport, err, "target "+strconv.Itoa(i)+": send failed"))
			continue
		}
		delivered++
	}
	if delivered > 0 {
		return nil
	}
	kind := ErrKindTransport
	if refused == len(c.targets) {
		kind = ErrKindUnauthorized
	}
	return &Error{Op: op, Kind: kind, Msg: "no target received the notification", Cause: errors.Join(errs...)}
}
