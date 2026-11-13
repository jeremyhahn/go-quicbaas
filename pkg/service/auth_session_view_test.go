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

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/iam"
)

func TestNewAuthSession_MapsView(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	v := &iam.SessionView{
		ID:          "sess-1",
		UserID:      30001,
		TenantID:    "tenant-a",
		MFAVerified: true,
		DeviceInfo:  &iam.DeviceInfo{ID: "dev-1", Name: "laptop"},
		IPAddress:   "192.0.2.1",
		UserAgent:   "ua",
		Created:     now,
		LastActive:  now.Add(time.Minute),
		ExpiresAt:   now.Add(time.Hour),
		Metadata:    map[string]string{"auth_method": "piv"},
	}

	got := NewAuthSession(v)
	require.NotNil(t, got)
	assert.Equal(t, &AuthSession{
		ID:          "sess-1",
		UserID:      "30001",
		TenantID:    "tenant-a",
		CreatedAt:   now,
		LastActive:  now.Add(time.Minute),
		ExpiresAt:   now.Add(time.Hour),
		MFAVerified: true,
		IPAddress:   "192.0.2.1",
		UserAgent:   "ua",
		DeviceInfo:  "laptop",
		AuthMethod:  "piv",
	}, got)
}

func TestNewAuthSession_NoDeviceNoMetadata(t *testing.T) {
	got := NewAuthSession(&iam.SessionView{ID: "sess-2", UserID: 7})
	require.NotNil(t, got)
	assert.Equal(t, "7", got.UserID)
	assert.Empty(t, got.DeviceInfo)
	assert.Empty(t, got.AuthMethod)
}

func TestNewAuthSession_NilView(t *testing.T) {
	assert.Nil(t, NewAuthSession(nil))
}
