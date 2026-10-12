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

package main

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/iam"
)

// TestWriteCreatedUser_ShowsTheInvite: the server returns the activation
// token on create only, so text output that dropped it would strand the user.
func TestWriteCreatedUser_ShowsTheInvite(t *testing.T) {
	expires := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	var out bytes.Buffer
	writeCreatedUser(&out, &iam.User{
		ID: "u1", Email: "pat@acme.test", TenantID: "acme",
		InviteToken:     "tok",
		InviteURL:       "https://id.acme.test/ui/#/invite/acme/tok",
		InviteExpiresAt: &expires,
	})

	assert.Contains(t, out.String(), "  URL: https://id.acme.test/ui/#/invite/acme/tok\n")
	assert.Contains(t, out.String(), "  Token: tok\n")
	assert.Contains(t, out.String(), "  Expires: 2026-10-13 12:00:00 UTC\n")
}

// TestWriteCreatedUser_NoInviteNoSection: an account created active has no
// invite, and the output must not suggest one.
func TestWriteCreatedUser_NoInviteNoSection(t *testing.T) {
	var out bytes.Buffer
	writeCreatedUser(&out, &iam.User{ID: "u1", Email: "quinn@acme.test", TenantID: "acme"})

	assert.Contains(t, out.String(), "  ID: u1\n")
	assert.NotContains(t, out.String(), "invite")
	assert.NotContains(t, out.String(), "Token:")
}
