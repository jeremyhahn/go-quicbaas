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

package messagingwatch

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/jeremyhahn/go-quicbaas/pkg/api"
	"github.com/jeremyhahn/go-quicbaas/pkg/logger"
	"github.com/jeremyhahn/go-quicbaas/pkg/messaging"
)

// newRunningService starts an in-memory messaging service for one test.
func newRunningService(t *testing.T) *messaging.PushService {
	t.Helper()
	log, err := logger.New(logger.LogConfig{Level: "error", Format: "json", Output: "stdout"})
	require.NoError(t, err)
	svc := messaging.NewPushService(log, nil)
	ctx := context.Background()
	require.NoError(t, svc.Start(ctx))
	t.Cleanup(func() {
		if err := svc.Stop(ctx); err != nil {
			t.Errorf("stop messaging service: %v", err)
		}
	})
	return svc
}

// TestWatch_PassesEveryOption pins that the session and filter a request
// carries reach the subscription the service opens.
func TestWatch_PassesEveryOption(t *testing.T) {
	svc := newRunningService(t)
	filter := &messaging.SubscriptionFilter{
		EventTypes: []messaging.EventType{messaging.EventTypeDelete},
		KeyPrefix:  "orders/eu/",
	}

	sub, err := Watch(context.Background(), svc, Request{
		KeyPrefix:  "orders/",
		SessionID:  "sess-1",
		Filter:     filter,
		BufferSize: 8,
	})
	require.NoError(t, err)

	assert.Equal(t, ChannelName("orders/"), sub.Channel)
	assert.Equal(t, "sess-1", sub.ClientID, "the session names the subscribing client")
	assert.Equal(t, filter, sub.Filters)
	assert.NotEmpty(t, sub.ID)
	assert.False(t, sub.CreatedAt.IsZero())
}

// TestWatch_DefaultsFilterToKeyPrefix pins that a request without a filter
// watches exactly its key prefix.
func TestWatch_DefaultsFilterToKeyPrefix(t *testing.T) {
	svc := newRunningService(t)

	sub, err := Watch(context.Background(), svc, Request{KeyPrefix: "orders/"})
	require.NoError(t, err)
	require.NotNil(t, sub.Filters)
	assert.Equal(t, "orders/", sub.Filters.KeyPrefix)
}

// TestWatch_RequiresKeyPrefix pins that an empty key prefix is refused as
// invalid input before the service is called.
func TestWatch_RequiresKeyPrefix(t *testing.T) {
	_, err := Watch(context.Background(), newRunningService(t), Request{SessionID: "sess-1"})
	var required *api.RequiredFieldError
	require.ErrorAs(t, err, &required)
	assert.Equal(t, "key_prefix", required.Field)
}

// TestOptions_EmptyRequestAddsNothing pins that unset fields add no option.
func TestOptions_EmptyRequestAddsNothing(t *testing.T) {
	assert.Empty(t, Request{KeyPrefix: "orders/"}.Options())
	assert.Len(t, Request{SessionID: "s", Filter: &messaging.SubscriptionFilter{}, BufferSize: 1}.Options(), 3)
}

// TestParseEventTypes pins the accepted spellings and that an unknown name is
// refused rather than dropped.
func TestParseEventTypes(t *testing.T) {
	got, err := ParseEventTypes([]string{"PUT", "delete"})
	require.NoError(t, err)
	assert.Equal(t, []messaging.EventType{messaging.EventTypePut, messaging.EventTypeDelete}, got)

	got, err = ParseEventTypes(nil)
	require.NoError(t, err)
	assert.Nil(t, got)

	_, err = ParseEventTypes([]string{"PUT", "RENAME"})
	var invalid *api.InvalidInputError
	assert.ErrorAs(t, err, &invalid)
}
