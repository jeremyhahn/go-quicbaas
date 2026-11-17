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

package iam

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSessionExt_OwnedBy(t *testing.T) {
	s := &SessionExt{ID: "sess-1", UserID: 30001, TenantID: "tenant-a"}

	tests := []struct {
		name     string
		session  *SessionExt
		userID   string
		tenantID string
		want     bool
	}{
		{name: "owner in own tenant", session: s, userID: "30001", tenantID: "tenant-a", want: true},
		{name: "other user", session: s, userID: "30002", tenantID: "tenant-a", want: false},
		{name: "owner in other tenant", session: s, userID: "30001", tenantID: "tenant-b", want: false},
		{name: "non-decimal user ID", session: s, userID: "0x7531", tenantID: "tenant-a", want: false},
		{name: "empty identity", session: s, userID: "", tenantID: "", want: false},
		{name: "nil session belongs to nobody", session: nil, userID: "0", tenantID: "", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.session.OwnedBy(tt.userID, tt.tenantID))
		})
	}
}

func TestSessionExt_View_OmitsHashes(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	s := &SessionExt{
		ID:          "sess-1",
		UserID:      30001,
		TenantID:    "tenant-a",
		TokenHash:   "token-hash-secret",
		RefreshHash: "refresh-hash-secret",
		MFAVerified: true,
		DeviceInfo:  &DeviceInfo{Name: "laptop"},
		IPAddress:   "192.0.2.1",
		UserAgent:   "ua",
		Created:     now,
		LastActive:  now,
		ExpiresAt:   now.Add(time.Hour),
		Metadata:    map[string]string{"auth_method": "piv"},
	}

	v := s.View()
	require.NotNil(t, v)
	assert.Equal(t, s.ID, v.ID)
	assert.Equal(t, s.UserID, v.UserID)
	assert.Equal(t, s.TenantID, v.TenantID)
	assert.True(t, v.MFAVerified)
	assert.Equal(t, "laptop", v.DeviceInfo.Name)
	assert.Equal(t, s.Created, v.Created)
	assert.Equal(t, s.ExpiresAt, v.ExpiresAt)
	assert.Equal(t, "piv", v.Metadata["auth_method"])

	raw, err := json.Marshal(v)
	require.NoError(t, err)
	assert.NotContains(t, string(raw), "token_hash")
	assert.NotContains(t, string(raw), "refresh_hash")
	assert.NotContains(t, string(raw), "hash-secret")
}

func TestSessionExt_View_Nil(t *testing.T) {
	var s *SessionExt
	assert.Nil(t, s.View())
}
