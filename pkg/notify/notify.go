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
	"strings"
)

// Severity grades how urgently a notification needs a person.
type Severity string

const (
	// SeverityInfo is a notification that needs no prompt action.
	SeverityInfo Severity = "info"
	// SeverityWarning is a notification that needs action soon.
	SeverityWarning Severity = "warning"
	// SeverityCritical is a notification that needs action now.
	SeverityCritical Severity = "critical"
)

// Valid reports whether s is a member of the closed set.
func (s Severity) Valid() bool {
	switch s {
	case SeverityInfo, SeverityWarning, SeverityCritical:
		return true
	default:
		return false
	}
}

// Notification is one message for the operators of a tenant.
type Notification struct {
	// TenantID names the tenant whose operators receive it. Required.
	TenantID string

	// Key is a stable id callers use for deduplication. Required.
	Key string

	// Severity grades the notification. Required, and must be Valid.
	Severity Severity

	// Subject is the one-line headline. Required; a newline is refused.
	Subject string

	// Body is the optional detail.
	Body string

	// Link is an optional URL to the item the notification is about.
	Link string

	// Sensitive restricts Body to secure channels: an insecure channel
	// receives the notification without it.
	Sensitive bool
}

// Validate reports the first missing or invalid field, in the order
// TenantID, Key, Severity, Subject, as an *InvalidNotificationError.
func (n Notification) Validate() error {
	switch {
	case n.TenantID == "":
		return &InvalidNotificationError{Field: "tenant_id", Message: "is required"}
	case n.Key == "":
		return &InvalidNotificationError{Field: "key", Message: "is required"}
	case !n.Severity.Valid():
		return &InvalidNotificationError{Field: "severity", Message: "must be info, warning or critical"}
	case n.Subject == "":
		return &InvalidNotificationError{Field: "subject", Message: "is required"}
	case strings.ContainsAny(n.Subject, "\r\n"):
		return &InvalidNotificationError{Field: "subject", Message: "must be one line"}
	}
	return nil
}

// Channel delivers notifications to one destination.
type Channel interface {
	// Name identifies the channel; it is unique within a Dispatcher.
	Name() string

	// Secure reports end-to-end encryption between the service and the
	// recipient.
	Secure() bool

	// Deliver sends n, returning an error when no recipient received it.
	Deliver(ctx context.Context, n Notification) error
}

// RecipientAuthorizer decides whether a QuicBaaS subject may receive a
// tenant's notifications. A false answer or an error means the subject
// receives nothing (fail closed). The production implementation, backed by
// the stack's logins and permissions, is a later change; this package only
// defines the seam.
type RecipientAuthorizer interface {
	MayReceive(ctx context.Context, tenantID, subjectID string) (bool, error)
}
