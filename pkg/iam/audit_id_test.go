package iam

import (
	"context"
	"encoding/hex"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// frozenClock returns a clock that never advances.
func frozenClock() func() time.Time {
	at := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	return func() time.Time { return at }
}

// zeroEntropy is a random source that yields only zero bytes, so any
// difference between two IDs must come from the timestamp.
func zeroEntropy(n int) ([]byte, error) {
	return make([]byte, n), nil
}

func TestGenerateAuditLogID_Format(t *testing.T) {
	id, err := generateAuditLogID()
	require.NoError(t, err)

	require.True(t, strings.HasPrefix(id, auditLogIDPrefix), "id %q", id)
	body := strings.TrimPrefix(id, auditLogIDPrefix)
	assert.Len(t, body, 32)
	_, err = hex.DecodeString(body)
	assert.NoError(t, err)
}

func TestAuditIDGenerator_DistinctWhenClockDoesNotAdvance(t *testing.T) {
	g := newAuditIDGenerator(frozenClock(), zeroEntropy)

	previous := ""
	for range 1000 {
		id, err := g.next()
		require.NoError(t, err)
		// Hex of a big-endian timestamp sorts in issue order, so strictly
		// increasing also proves distinct.
		require.Greater(t, id, previous)
		previous = id
	}
}

func TestAuditIDGenerator_DistinctWhenClockStepsBack(t *testing.T) {
	ticks := []time.Time{
		time.Unix(0, 5000),
		time.Unix(0, 1000), // the clock is set back
		time.Unix(0, 1000),
		time.Unix(0, 9000),
	}
	call := 0
	clock := func() time.Time {
		at := ticks[call]
		call++
		return at
	}
	g := newAuditIDGenerator(clock, zeroEntropy)

	var ids []string
	for range ticks {
		id, err := g.next()
		require.NoError(t, err)
		ids = append(ids, id)
	}
	for i := 1; i < len(ids); i++ {
		assert.Greater(t, ids[i], ids[i-1])
	}
}

func TestAuditIDGenerator_ConcurrentIDsAreDistinct(t *testing.T) {
	const workers, perWorker = 32, 500
	// A frozen clock forces every ID onto the counter path, the case
	// where concurrent callers would collide if the CAS were wrong.
	g := newAuditIDGenerator(frozenClock(), zeroEntropy)

	results := make(chan string, workers*perWorker)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for range perWorker {
				id, err := g.next()
				if err != nil {
					t.Error(err)
					return
				}
				results <- id
			}
		})
	}
	wg.Wait()
	close(results)

	seen := make(map[string]struct{}, workers*perWorker)
	for id := range results {
		_, dup := seen[id]
		require.False(t, dup, "duplicate audit log ID %s", id)
		seen[id] = struct{}{}
	}
	assert.Len(t, seen, workers*perWorker)
}

func TestMemoryAuditStorage_SaveEventRejectsDuplicateID(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryAuditStorage()
	first := &AuditEvent{
		ID: "audit_dup", TenantID: "tenant1", Timestamp: time.Now(),
		EventType: AuditEventAuthorization, Category: AuditCategorySecurity, Action: "first", Outcome: AuditOutcomeSuccess,
	}
	second := *first
	second.Action = "second"

	require.NoError(t, store.SaveEvent(ctx, first))
	err := store.SaveEvent(ctx, &second)
	require.ErrorIs(t, err, ErrAuditEventExists)

	stored, err := store.GetEvent(ctx, "tenant1", "audit_dup")
	require.NoError(t, err)
	assert.Equal(t, "first", stored.Action, "the original record must survive")
	_, total, err := store.QueryEvents(ctx, &AuditFilter{TenantID: "tenant1"})
	require.NoError(t, err)
	assert.Equal(t, 1, total)
}

func TestMemoryAuditStorage_SaveEventAcceptsDistinctIDs(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryAuditStorage()
	for range 3 {
		id, err := generateAuditLogID()
		require.NoError(t, err)
		require.NoError(t, store.SaveEvent(ctx, &AuditEvent{
			ID: id, TenantID: "tenant1", Timestamp: time.Now(),
			EventType: AuditEventAuthorization, Category: AuditCategorySecurity, Action: "act", Outcome: AuditOutcomeSuccess,
		}))
	}
	_, total, err := store.QueryEvents(ctx, &AuditFilter{TenantID: "tenant1"})
	require.NoError(t, err)
	assert.Equal(t, 3, total)
}
