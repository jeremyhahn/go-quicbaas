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

package iam

import (
	"testing"
	"time"
)

const inviteExpiry = "2026-10-13T12:00:00Z"

// TestConvertToUser_DecodesTheInvite: the token is returned only by Create and
// cannot be fetched again, so a decode that drops it leaves a pending account
// nobody can activate. REST puts it on the user, MCP beside the nested user.
func TestConvertToUser_DecodesTheInvite(t *testing.T) {
	invite := map[string]interface{}{
		"invite_token":      "tok",
		"invite_url":        "https://id.acme.test/ui/#/invite/acme/tok",
		"invite_expires_at": inviteExpiry,
	}
	flat := map[string]interface{}{"id": "1001", "lifecycle": "pending"}
	nested := map[string]interface{}{"user": map[string]interface{}{"id": "1001", "lifecycle": "pending"}}
	for k, v := range invite {
		flat[k] = v
		nested[k] = v
	}

	want, err := time.Parse(time.RFC3339, inviteExpiry)
	if err != nil {
		t.Fatal(err)
	}
	for name, resp := range map[string]map[string]interface{}{"rest": flat, "mcp": nested} {
		t.Run(name, func(t *testing.T) {
			user, ok := convertToUser(resp)
			if !ok {
				t.Fatal("convertToUser did not decode the response")
			}
			if user.ID != "1001" {
				t.Errorf("ID = %q, want 1001", user.ID)
			}
			if user.InviteToken != "tok" || user.InviteURL != invite["invite_url"] {
				t.Errorf("invite = %q / %q, want the values the server sent", user.InviteToken, user.InviteURL)
			}
			if user.InviteExpiresAt == nil || !user.InviteExpiresAt.Equal(want) {
				t.Errorf("InviteExpiresAt = %v, want %v", user.InviteExpiresAt, want)
			}
		})
	}
}

// TestConvertToUser_NoInviteMeansNone: a user without an invite, or with an
// unparseable expiry, carries no expiry rather than a zero one.
func TestConvertToUser_NoInviteMeansNone(t *testing.T) {
	user, ok := convertToUser(map[string]interface{}{"id": "1001", "invite_expires_at": "tomorrow"})
	if !ok {
		t.Fatal("convertToUser did not decode the response")
	}
	if user.InviteToken != "" || user.InviteURL != "" || user.InviteExpiresAt != nil {
		t.Errorf("want no invite, got %q / %q / %v", user.InviteToken, user.InviteURL, user.InviteExpiresAt)
	}
}
