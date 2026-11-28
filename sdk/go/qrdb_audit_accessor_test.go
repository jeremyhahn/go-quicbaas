// Copyright 2026 Jeremy Hahn
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package quicbaas

import (
	"context"
	"testing"
	"time"

	qrdbtenant "github.com/jeremyhahn/go-qrdb/pkg/tenant"
	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	sdkerrors "github.com/jeremyhahn/go-quicbaas/sdk/go/errors"
	qbtransport "github.com/jeremyhahn/go-quicbaas/sdk/go/transport"
)

// TestSecurityQRDBAudit_EmbeddedRoundTrip proves Security().QRDBAudit()
// reaches go-qrdb's audit service on the embedded transport: an event
// emitted through the accessor lands in the node sink wired through
// QRDBEmbeddedServicers.Audit and comes back from QueryAudit.
func TestSecurityQRDBAudit_EmbeddedRoundTrip(t *testing.T) {
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:    stubKVService{},
		Admin: stubAdminService{},
		Audit: qrdbsdk.NewAuditRingBuffer(16),
	})

	ac, err := c.Security().QRDBAudit()
	require.NoError(t, err)
	require.NotNil(t, ac)

	// go-qrdb confines every audit query to the caller's tenant, so bind
	// the event's tenant to the context the way a tenant-scoped caller is.
	ctx := qrdbtenant.WithTenantID(context.Background(), "tenant-a")
	ev := QRDBAuditEvent{
		Timestamp: time.Now().UTC(),
		Actor:     "alice",
		TenantID:  "tenant-a",
		Resource:  "kv/key-1",
		Action:    "kv.put",
		Outcome:   "success",
	}
	require.NoError(t, ac.EmitAudit(ctx, ev))

	var page *QRDBAuditPage
	require.Eventually(t, func() bool {
		page, err = ac.QueryAudit(ctx, QRDBAuditFilter{})
		return err == nil && page != nil && len(page.Events) == 1
	}, 5*time.Second, 10*time.Millisecond, "QueryAudit did not return the emitted event (err=%v)", err)

	got := page.Events[0]
	assert.Equal(t, ev.Actor, got.Actor)
	assert.Equal(t, ev.Resource, got.Resource)
	assert.Equal(t, ev.Action, got.Action)
	assert.Equal(t, ev.Outcome, got.Outcome)
}

// TestSecurityQRDBAudit_EmbeddedSDKTenantContext: an embedded caller that
// names its tenant with the public SDK helper sees only its own tenant's
// events and may filter on its own tenant.
func TestSecurityQRDBAudit_EmbeddedSDKTenantContext(t *testing.T) {
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:    stubKVService{},
		Admin: stubAdminService{},
		Audit: qrdbsdk.NewAuditRingBuffer(16),
	})

	ac, err := c.Security().QRDBAudit()
	require.NoError(t, err)
	require.NotNil(t, ac)

	ctxA := qbtransport.ContextWithTenant(context.Background(), "tenant-a")
	ctxB := qbtransport.ContextWithTenant(context.Background(), "tenant-b")
	require.NoError(t, ac.EmitAudit(ctxA, QRDBAuditEvent{
		Timestamp: time.Now().UTC(),
		Actor:     "alice",
		TenantID:  "tenant-a",
		Resource:  "kv/key-1",
		Action:    "kv.put",
		Outcome:   "success",
	}))
	require.NoError(t, ac.EmitAudit(ctxB, QRDBAuditEvent{
		Timestamp: time.Now().UTC(),
		Actor:     "bob",
		TenantID:  "tenant-b",
		Resource:  "kv/key-1",
		Action:    "kv.put",
		Outcome:   "success",
	}))

	t.Run("empty filter", func(t *testing.T) {
		var page *QRDBAuditPage
		var err error
		require.Eventually(t, func() bool {
			page, err = ac.QueryAudit(ctxA, QRDBAuditFilter{})
			return err == nil && page != nil && len(page.Events) == 1
		}, 5*time.Second, 10*time.Millisecond, "QueryAudit did not return tenant-a's event only (err=%v)", err)
		assert.Equal(t, "alice", page.Events[0].Actor)
	})

	t.Run("own tenant filter", func(t *testing.T) {
		page, err := ac.QueryAudit(ctxA, QRDBAuditFilter{TenantID: "tenant-a"})
		require.NoError(t, err)
		require.NotNil(t, page)
		require.Len(t, page.Events, 1)
		assert.Equal(t, "alice", page.Events[0].Actor)
	})

	t.Run("existing qrdb binding wins", func(t *testing.T) {
		ctx := qbtransport.ContextWithTenant(qrdbtenant.WithTenantID(context.Background(), "tenant-b"), "tenant-a")
		page, err := ac.QueryAudit(ctx, QRDBAuditFilter{})
		require.NoError(t, err)
		require.NotNil(t, page)
		require.Len(t, page.Events, 1)
		assert.Equal(t, "bob", page.Events[0].Actor)
	})
}

// TestSecurityQRDBAudit_EmbeddedWithoutSinkIsUnavailable proves that an
// embedded client built without an audit sink answers Unavailable rather
// than returning a nil client.
func TestSecurityQRDBAudit_EmbeddedWithoutSinkIsUnavailable(t *testing.T) {
	c := buildEmbeddedQRDBClient(t, QRDBEmbeddedServicers{
		KV:    stubKVService{},
		Admin: stubAdminService{},
	})

	ac, err := c.Security().QRDBAudit()
	require.Error(t, err)
	assert.Nil(t, ac)
	assert.True(t, sdkerrors.IsUnavailable(err), "want Unavailable, got %v", err)
}
