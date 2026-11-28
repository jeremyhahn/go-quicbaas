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
	"testing"
)

func validNotification() Notification {
	return Notification{
		TenantID: "tenant-1",
		Key:      "item-1@1",
		Severity: SeverityWarning,
		Subject:  "proj: build is parked",
		Body:     "Question: which branch?",
		Link:     "https://forge.example/owner/repo/issues/1",
	}
}

func TestNotification_Validate(t *testing.T) {
	if err := validNotification().Validate(); err != nil {
		t.Fatalf("valid notification refused: %v", err)
	}
	cases := []struct {
		name  string
		edit  func(*Notification)
		field string
	}{
		{"no tenant", func(n *Notification) { n.TenantID = "" }, "tenant_id"},
		{"no key", func(n *Notification) { n.Key = "" }, "key"},
		{"no severity", func(n *Notification) { n.Severity = "" }, "severity"},
		{"bad severity", func(n *Notification) { n.Severity = "panic" }, "severity"},
		{"no subject", func(n *Notification) { n.Subject = "" }, "subject"},
		{"newline in subject", func(n *Notification) { n.Subject = "one\ntwo" }, "subject"},
		{"tenant before key", func(n *Notification) { n.TenantID = ""; n.Key = "" }, "tenant_id"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			n := validNotification()
			tc.edit(&n)
			err := n.Validate()
			if !errors.Is(err, ErrInvalidNotification) {
				t.Fatalf("want ErrInvalidNotification, got %v", err)
			}
			var inv *InvalidNotificationError
			if !errors.As(err, &inv) || inv.Field != tc.field {
				t.Fatalf("want field %q, got %v", tc.field, err)
			}
		})
	}
}
