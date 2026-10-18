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

//go:build integration

package helpers

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// errLoginRefused is the failure fakeTenantAdminLogin returns when told to.
var errLoginRefused = errors.New("login refused")

// fakeTenantAdminLogin stands in for the server's PIV login: each login
// issues a new bearer valid for lifetime on the fake clock.
type fakeTenantAdminLogin struct {
	clock    time.Time
	lifetime time.Duration
	logins   int
	fail     bool
}

func (f *fakeTenantAdminLogin) now() time.Time { return f.clock }

func (f *fakeTenantAdminLogin) login(_ context.Context, _ string, _, _ []byte) (string, time.Time, error) {
	if f.fail {
		return "", time.Time{}, errLoginRefused
	}
	f.logins++
	return "bearer-" + strconv.Itoa(f.logins), f.clock.Add(f.lifetime), nil
}

func newFakeSession(fake *fakeTenantAdminLogin) *TenantAdminSession {
	tenant := &ProvisionedTenant{TenantID: "acme", AdminCertPEM: []byte("cert"), AdminKeyPEM: []byte("key")}
	return newTenantAdminSession("https://node:8443/", tenant, fake.login, fake.now)
}

func TestTenantAdminSession_ReusesBearerUntilExpiry(t *testing.T) {
	fake := &fakeTenantAdminLogin{clock: time.Unix(1_000_000, 0), lifetime: 15 * time.Minute}
	session := newFakeSession(fake)

	first, err := session.Bearer(context.Background())
	require.NoError(t, err)
	fake.clock = fake.clock.Add(14 * time.Minute)
	second, err := session.Bearer(context.Background())
	require.NoError(t, err)

	require.Equal(t, "bearer-1", first)
	require.Equal(t, first, second, "a bearer still inside its lifetime is reused")
	require.Equal(t, 1, fake.logins)
}

func TestTenantAdminSession_RenewsExpiredBearer(t *testing.T) {
	fake := &fakeTenantAdminLogin{clock: time.Unix(1_000_000, 0), lifetime: 15 * time.Minute}
	session := newFakeSession(fake)

	first, err := session.Bearer(context.Background())
	require.NoError(t, err)

	// A soak iteration runs past the bearer's lifetime.
	fake.clock = fake.clock.Add(15 * time.Minute)
	renewed, err := session.Bearer(context.Background())
	require.NoError(t, err)

	require.Equal(t, "bearer-1", first)
	require.Equal(t, "bearer-2", renewed, "an expired bearer is renewed with the admin certificate")
	require.Equal(t, 2, fake.logins)
}

func TestTenantAdminSession_RenewalFailureIsTyped(t *testing.T) {
	fake := &fakeTenantAdminLogin{clock: time.Unix(1_000_000, 0), lifetime: 15 * time.Minute}
	session := newFakeSession(fake)
	_, err := session.Bearer(context.Background())
	require.NoError(t, err)

	fake.clock = fake.clock.Add(time.Hour)
	fake.fail = true
	bearer, err := session.Bearer(context.Background())

	require.Empty(t, bearer, "an expired bearer is never handed out")
	var provisionErr *TenantProvisionError
	require.ErrorAs(t, err, &provisionErr)
	require.Equal(t, "acme", provisionErr.TenantID)
	require.ErrorIs(t, err, errLoginRefused)
}
