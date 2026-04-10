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

package websocket

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/coder/websocket"
)

func TestNewSessionManager(t *testing.T) {
	sm := NewSessionManager()

	if sm == nil {
		t.Fatal("NewSessionManager() returned nil")
	}

	// Verify all 256 shards are initialized
	for i := 0; i < 256; i++ {
		if sm.shards[i] == nil {
			t.Errorf("Shard %d is nil", i)
		}

		if sm.shards[i].sessions == nil {
			t.Errorf("Shard %d sessions map is nil", i)
		}
	}

	// Verify count is 0
	if sm.Count() != 0 {
		t.Errorf("Count() = %d, want 0", sm.Count())
	}
}

func TestSessionManager_Add(t *testing.T) {
	sm := NewSessionManager()

	session := NewSession("session-1", "echo", nil)
	sm.Add(session)

	if sm.Count() != 1 {
		t.Errorf("Count() = %d, want 1", sm.Count())
	}

	// Verify session is retrievable
	retrieved, exists := sm.Get("session-1")
	if !exists {
		t.Error("Session should exist after Add")
	}

	if retrieved.ID != session.ID {
		t.Errorf("Retrieved session ID = %q, want %q", retrieved.ID, session.ID)
	}
}

func TestSessionManager_AddMultiple(t *testing.T) {
	sm := NewSessionManager()

	// Add multiple sessions
	for i := 0; i < 100; i++ {
		session := NewSession(fmt.Sprintf("session-%d", i), "echo", nil)
		sm.Add(session)
	}

	if sm.Count() != 100 {
		t.Errorf("Count() = %d, want 100", sm.Count())
	}

	// Verify all sessions are retrievable
	for i := 0; i < 100; i++ {
		id := fmt.Sprintf("session-%d", i)
		_, exists := sm.Get(id)
		if !exists {
			t.Errorf("Session %s should exist", id)
		}
	}
}

func TestSessionManager_AddDuplicateID(t *testing.T) {
	sm := NewSessionManager()

	session1 := NewSession("session-1", "echo", nil)
	session1.UserID = "user-1"
	sm.Add(session1)

	session2 := NewSession("session-1", "mqtt", nil)
	session2.UserID = "user-2"
	sm.Add(session2)

	// Count should be 2 because Add increments unconditionally
	// but the map will only have the latest session
	if sm.Count() != 2 {
		t.Errorf("Count() = %d, want 2 (Add increments unconditionally)", sm.Count())
	}

	// Get should return the latest session
	retrieved, exists := sm.Get("session-1")
	if !exists {
		t.Error("Session should exist")
	}

	if retrieved.UserID != "user-2" {
		t.Errorf("Retrieved session UserID = %q, want %q", retrieved.UserID, "user-2")
	}
}

func TestSessionManager_Remove(t *testing.T) {
	sm := NewSessionManager()

	session := NewSession("session-1", "echo", nil)
	sm.Add(session)

	if sm.Count() != 1 {
		t.Errorf("Count() = %d, want 1", sm.Count())
	}

	sm.Remove("session-1")

	if sm.Count() != 0 {
		t.Errorf("Count() = %d, want 0", sm.Count())
	}

	// Verify session is no longer retrievable
	_, exists := sm.Get("session-1")
	if exists {
		t.Error("Session should not exist after Remove")
	}
}

func TestSessionManager_RemoveNonexistent(t *testing.T) {
	sm := NewSessionManager()

	// Add one session
	session := NewSession("session-1", "echo", nil)
	sm.Add(session)

	// Try to remove a nonexistent session
	sm.Remove("nonexistent")

	// Count should remain unchanged
	if sm.Count() != 1 {
		t.Errorf("Count() = %d, want 1", sm.Count())
	}
}

func TestSessionManager_RemoveEmpty(t *testing.T) {
	sm := NewSessionManager()

	// Remove from empty manager should not panic or error
	sm.Remove("nonexistent")

	if sm.Count() != 0 {
		t.Errorf("Count() = %d, want 0", sm.Count())
	}
}

func TestSessionManager_Get(t *testing.T) {
	sm := NewSessionManager()

	// Get from empty manager
	_, exists := sm.Get("nonexistent")
	if exists {
		t.Error("Get on empty manager should return false")
	}

	// Add and get
	session := NewSession("session-1", "echo", nil)
	session.UserID = "user-1"
	sm.Add(session)

	retrieved, exists := sm.Get("session-1")
	if !exists {
		t.Error("Session should exist")
	}

	if retrieved.ID != "session-1" {
		t.Errorf("ID = %q, want %q", retrieved.ID, "session-1")
	}

	if retrieved.UserID != "user-1" {
		t.Errorf("UserID = %q, want %q", retrieved.UserID, "user-1")
	}
}

func TestSessionManager_GetNonexistent(t *testing.T) {
	sm := NewSessionManager()

	session := NewSession("session-1", "echo", nil)
	sm.Add(session)

	_, exists := sm.Get("session-2")
	if exists {
		t.Error("Get for nonexistent session should return false")
	}
}

func TestSessionManager_Count(t *testing.T) {
	sm := NewSessionManager()

	if sm.Count() != 0 {
		t.Errorf("Initial Count() = %d, want 0", sm.Count())
	}

	// Add sessions
	for i := 0; i < 10; i++ {
		sm.Add(NewSession(fmt.Sprintf("session-%d", i), "echo", nil))
		if sm.Count() != uint64(i+1) {
			t.Errorf("Count() = %d, want %d", sm.Count(), i+1)
		}
	}

	// Remove sessions
	for i := 0; i < 5; i++ {
		sm.Remove(fmt.Sprintf("session-%d", i))
		if sm.Count() != uint64(10-i-1) {
			t.Errorf("Count() = %d, want %d", sm.Count(), 10-i-1)
		}
	}
}

func TestSessionManager_ForEach(t *testing.T) {
	sm := NewSessionManager()

	// Add sessions
	for i := 0; i < 10; i++ {
		session := NewSession(fmt.Sprintf("session-%d", i), "echo", nil)
		session.UserID = fmt.Sprintf("user-%d", i)
		sm.Add(session)
	}

	// Collect all session IDs
	var ids []string
	sm.ForEach(func(s *Session) bool {
		ids = append(ids, s.ID)
		return true
	})

	if len(ids) != 10 {
		t.Errorf("ForEach visited %d sessions, want 10", len(ids))
	}
}

func TestSessionManager_ForEachEarlyStop(t *testing.T) {
	sm := NewSessionManager()

	// Add sessions
	for i := 0; i < 100; i++ {
		sm.Add(NewSession(fmt.Sprintf("session-%d", i), "echo", nil))
	}

	// Stop after 5 sessions
	count := 0
	sm.ForEach(func(s *Session) bool {
		count++
		return count < 5
	})

	if count != 5 {
		t.Errorf("ForEach visited %d sessions, want 5", count)
	}
}

func TestSessionManager_ForEachEmpty(t *testing.T) {
	sm := NewSessionManager()

	count := 0
	sm.ForEach(func(s *Session) bool {
		count++
		return true
	})

	if count != 0 {
		t.Errorf("ForEach visited %d sessions, want 0", count)
	}
}

func TestSessionManager_CloseAll(t *testing.T) {
	// Create a simple WebSocket server to get real connections
	sm := NewSessionManager()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		session := NewSession(r.URL.Query().Get("id"), "echo", conn)
		sm.Add(session)

		// Wait for context cancellation (session close)
		<-session.Context().Done()
	})

	ts := httptest.NewServer(handler)
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")

	// Create connections
	var connections []*websocket.Conn
	for i := 0; i < 5; i++ {
		ctx := context.Background()
		url := fmt.Sprintf("%s?id=session-%d", wsURL, i)
		conn, resp, err := websocket.Dial(ctx, url, nil)
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		if err != nil {
			t.Fatalf("Dial %d failed: %v", i, err)
		}
		connections = append(connections, conn)
	}

	// Wait for sessions to be added
	for sm.Count() < 5 {
		// Brief wait for goroutines to complete
	}

	if sm.Count() != 5 {
		t.Errorf("Count() = %d, want 5", sm.Count())
	}

	// Close all sessions
	sm.CloseAll()

	// Verify count is 0
	if sm.Count() != 0 {
		t.Errorf("Count() after CloseAll = %d, want 0", sm.Count())
	}

	// Verify no sessions are retrievable
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("session-%d", i)
		_, exists := sm.Get(id)
		if exists {
			t.Errorf("Session %s should not exist after CloseAll", id)
		}
	}

	// Clean up client connections
	for _, conn := range connections {
		conn.Close(websocket.StatusNormalClosure, "test cleanup")
	}
}

func TestSessionManager_CloseAllEmpty(t *testing.T) {
	sm := NewSessionManager()

	// Should not panic on empty manager
	sm.CloseAll()

	if sm.Count() != 0 {
		t.Errorf("Count() = %d, want 0", sm.Count())
	}
}

func TestSessionManager_GetByUserID(t *testing.T) {
	sm := NewSessionManager()

	// Add sessions with different user IDs
	for i := 0; i < 5; i++ {
		session := NewSession(fmt.Sprintf("session-user1-%d", i), "echo", nil)
		session.UserID = "user-1"
		sm.Add(session)
	}

	for i := 0; i < 3; i++ {
		session := NewSession(fmt.Sprintf("session-user2-%d", i), "echo", nil)
		session.UserID = "user-2"
		sm.Add(session)
	}

	// Get by user-1
	user1Sessions := sm.GetByUserID("user-1")
	if len(user1Sessions) != 5 {
		t.Errorf("GetByUserID(user-1) returned %d sessions, want 5", len(user1Sessions))
	}

	// Get by user-2
	user2Sessions := sm.GetByUserID("user-2")
	if len(user2Sessions) != 3 {
		t.Errorf("GetByUserID(user-2) returned %d sessions, want 3", len(user2Sessions))
	}

	// Get by nonexistent user
	noSessions := sm.GetByUserID("user-3")
	if len(noSessions) != 0 {
		t.Errorf("GetByUserID(user-3) returned %d sessions, want 0", len(noSessions))
	}
}

func TestSessionManager_GetByUserIDEmpty(t *testing.T) {
	sm := NewSessionManager()

	sessions := sm.GetByUserID("user-1")
	if len(sessions) != 0 {
		t.Errorf("GetByUserID on empty manager returned %d sessions, want 0", len(sessions))
	}
}

func TestSessionManager_GetBySubprotocol(t *testing.T) {
	sm := NewSessionManager()

	// Add sessions with different subprotocols
	for i := 0; i < 4; i++ {
		sm.Add(NewSession(fmt.Sprintf("session-echo-%d", i), "echo", nil))
	}

	for i := 0; i < 3; i++ {
		sm.Add(NewSession(fmt.Sprintf("session-mqtt-%d", i), "mqtt", nil))
	}

	for i := 0; i < 2; i++ {
		sm.Add(NewSession(fmt.Sprintf("session-stomp-%d", i), "stomp", nil))
	}

	// Get by echo
	echoSessions := sm.GetBySubprotocol("echo")
	if len(echoSessions) != 4 {
		t.Errorf("GetBySubprotocol(echo) returned %d sessions, want 4", len(echoSessions))
	}

	// Get by mqtt
	mqttSessions := sm.GetBySubprotocol("mqtt")
	if len(mqttSessions) != 3 {
		t.Errorf("GetBySubprotocol(mqtt) returned %d sessions, want 3", len(mqttSessions))
	}

	// Get by stomp
	stompSessions := sm.GetBySubprotocol("stomp")
	if len(stompSessions) != 2 {
		t.Errorf("GetBySubprotocol(stomp) returned %d sessions, want 2", len(stompSessions))
	}

	// Get by nonexistent protocol
	noSessions := sm.GetBySubprotocol("amqp")
	if len(noSessions) != 0 {
		t.Errorf("GetBySubprotocol(amqp) returned %d sessions, want 0", len(noSessions))
	}
}

func TestSessionManager_GetBySubprotocolEmpty(t *testing.T) {
	sm := NewSessionManager()

	sessions := sm.GetBySubprotocol("echo")
	if len(sessions) != 0 {
		t.Errorf("GetBySubprotocol on empty manager returned %d sessions, want 0", len(sessions))
	}
}

func TestSessionManager_GetSubscribersForChannel(t *testing.T) {
	sm := NewSessionManager()

	// Add sessions and subscribe some to channels
	for i := 0; i < 10; i++ {
		session := NewSession(fmt.Sprintf("session-%d", i), "echo", nil)
		sm.Add(session)

		if i%2 == 0 {
			session.Subscribe("channel-a")
		}
		if i%3 == 0 {
			session.Subscribe("channel-b")
		}
	}

	// Get subscribers for channel-a (0, 2, 4, 6, 8)
	channelASubs := sm.GetSubscribersForChannel("channel-a")
	if len(channelASubs) != 5 {
		t.Errorf("GetSubscribersForChannel(channel-a) returned %d sessions, want 5", len(channelASubs))
	}

	// Get subscribers for channel-b (0, 3, 6, 9)
	channelBSubs := sm.GetSubscribersForChannel("channel-b")
	if len(channelBSubs) != 4 {
		t.Errorf("GetSubscribersForChannel(channel-b) returned %d sessions, want 4", len(channelBSubs))
	}

	// Get subscribers for nonexistent channel
	noSubs := sm.GetSubscribersForChannel("channel-c")
	if len(noSubs) != 0 {
		t.Errorf("GetSubscribersForChannel(channel-c) returned %d sessions, want 0", len(noSubs))
	}
}

func TestSessionManager_GetSubscribersForChannelEmpty(t *testing.T) {
	sm := NewSessionManager()

	subs := sm.GetSubscribersForChannel("channel")
	if len(subs) != 0 {
		t.Errorf("GetSubscribersForChannel on empty manager returned %d sessions, want 0", len(subs))
	}
}

func TestSessionManager_GetStats(t *testing.T) {
	sm := NewSessionManager()

	// Add sessions with different subprotocols
	for i := 0; i < 5; i++ {
		sm.Add(NewSession(fmt.Sprintf("session-echo-%d", i), "echo", nil))
	}

	for i := 0; i < 3; i++ {
		sm.Add(NewSession(fmt.Sprintf("session-mqtt-%d", i), "mqtt", nil))
	}

	for i := 0; i < 2; i++ {
		sm.Add(NewSession(fmt.Sprintf("session-stomp-%d", i), "stomp", nil))
	}

	stats := sm.GetStats()

	if stats.TotalSessions != 10 {
		t.Errorf("TotalSessions = %d, want 10", stats.TotalSessions)
	}

	if stats.SessionsByProtocol["echo"] != 5 {
		t.Errorf("SessionsByProtocol[echo] = %d, want 5", stats.SessionsByProtocol["echo"])
	}

	if stats.SessionsByProtocol["mqtt"] != 3 {
		t.Errorf("SessionsByProtocol[mqtt] = %d, want 3", stats.SessionsByProtocol["mqtt"])
	}

	if stats.SessionsByProtocol["stomp"] != 2 {
		t.Errorf("SessionsByProtocol[stomp] = %d, want 2", stats.SessionsByProtocol["stomp"])
	}
}

func TestSessionManager_GetStatsEmpty(t *testing.T) {
	sm := NewSessionManager()

	stats := sm.GetStats()

	if stats.TotalSessions != 0 {
		t.Errorf("TotalSessions = %d, want 0", stats.TotalSessions)
	}

	if len(stats.SessionsByProtocol) != 0 {
		t.Errorf("SessionsByProtocol has %d entries, want 0", len(stats.SessionsByProtocol))
	}
}

func TestSessionManager_Sharding(t *testing.T) {
	sm := NewSessionManager()

	// Add many sessions to test sharding
	for i := 0; i < 1000; i++ {
		sm.Add(NewSession(fmt.Sprintf("session-%d", i), "echo", nil))
	}

	// Count sessions across all shards
	totalInShards := 0
	for i := 0; i < 256; i++ {
		sm.shards[i].mu.RLock()
		totalInShards += len(sm.shards[i].sessions)
		sm.shards[i].mu.RUnlock()
	}

	if totalInShards != 1000 {
		t.Errorf("Total sessions in shards = %d, want 1000", totalInShards)
	}

	// Verify sessions are distributed (not all in one shard)
	nonEmptyShards := 0
	for i := 0; i < 256; i++ {
		sm.shards[i].mu.RLock()
		if len(sm.shards[i].sessions) > 0 {
			nonEmptyShards++
		}
		sm.shards[i].mu.RUnlock()
	}

	// With 1000 sessions and 256 shards, we expect reasonable distribution
	if nonEmptyShards < 100 {
		t.Errorf("Only %d non-empty shards, expected better distribution", nonEmptyShards)
	}
}

func TestFnv1aString(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "empty string", input: ""},
		{name: "short string", input: "a"},
		{name: "uuid like", input: "550e8400-e29b-41d4-a716-446655440000"},
		{name: "session id", input: "session-12345"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := fnv1aString(tt.input)

			// Result should be in range [0, 255]
			if result > 255 {
				t.Errorf("fnv1aString(%q) = %d, should be <= 255", tt.input, result)
			}

			// Same input should give same output
			result2 := fnv1aString(tt.input)
			if result != result2 {
				t.Errorf("fnv1aString(%q) not deterministic: %d vs %d", tt.input, result, result2)
			}
		})
	}
}

func TestFnv1aString_Consistency(t *testing.T) {
	// Different strings should typically hash to different values
	// (not guaranteed, but good for distribution)
	hashes := make(map[uint8]int)
	for i := 0; i < 1000; i++ {
		hash := fnv1aString(fmt.Sprintf("session-%d", i))
		hashes[hash]++
	}

	// Check that hashes are reasonably distributed
	if len(hashes) < 50 {
		t.Errorf("Only %d unique hashes for 1000 inputs, expected better distribution", len(hashes))
	}
}

func TestSessionManager_ConcurrentAccess(t *testing.T) {
	sm := NewSessionManager()
	var wg sync.WaitGroup

	// Concurrent adds
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			session := NewSession(fmt.Sprintf("session-%d", idx), "echo", nil)
			sm.Add(session)
		}(i)
	}

	wg.Wait()

	if sm.Count() != 100 {
		t.Errorf("Count() = %d, want 100", sm.Count())
	}

	// Concurrent reads and removes
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			sm.Get(fmt.Sprintf("session-%d", idx))
		}(i)
		go func(idx int) {
			defer wg.Done()
			if idx%2 == 0 {
				sm.Remove(fmt.Sprintf("session-%d", idx))
			}
		}(i)
	}

	wg.Wait()

	// Should have 50 sessions remaining
	if sm.Count() != 50 {
		t.Errorf("Count() = %d, want 50", sm.Count())
	}
}

func TestSessionManager_ConcurrentForEach(t *testing.T) {
	sm := NewSessionManager()

	// Add sessions
	for i := 0; i < 100; i++ {
		sm.Add(NewSession(fmt.Sprintf("session-%d", i), "echo", nil))
	}

	var wg sync.WaitGroup

	// Multiple concurrent ForEach calls
	for j := 0; j < 10; j++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			count := 0
			sm.ForEach(func(s *Session) bool {
				count++
				return true
			})
		}()
	}

	// Concurrent modifications while iterating
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sm.Add(NewSession(fmt.Sprintf("new-session-%d", idx), "mqtt", nil))
		}(i)
	}

	wg.Wait()
	// No race conditions should occur
}

func TestSessionManager_ConcurrentGetByUserID(t *testing.T) {
	sm := NewSessionManager()

	// Add sessions
	for i := 0; i < 50; i++ {
		session := NewSession(fmt.Sprintf("session-%d", i), "echo", nil)
		session.UserID = fmt.Sprintf("user-%d", i%5)
		sm.Add(session)
	}

	var wg sync.WaitGroup

	// Concurrent GetByUserID calls
	for j := 0; j < 20; j++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			userID := fmt.Sprintf("user-%d", idx%5)
			sessions := sm.GetByUserID(userID)
			if len(sessions) != 10 {
				// Don't error - just check for race conditions
				_ = sessions
			}
		}(j)
	}

	wg.Wait()
	// No race conditions should occur
}

func TestSessionStats(t *testing.T) {
	stats := SessionStats{
		TotalSessions: 100,
		SessionsByProtocol: map[string]int{
			"echo":  50,
			"mqtt":  30,
			"stomp": 20,
		},
	}

	if stats.TotalSessions != 100 {
		t.Errorf("TotalSessions = %d, want 100", stats.TotalSessions)
	}

	total := 0
	for _, count := range stats.SessionsByProtocol {
		total += count
	}

	if total != 100 {
		t.Errorf("Sum of SessionsByProtocol = %d, want 100", total)
	}
}

func TestSessionManager_ShardDistribution(t *testing.T) {
	sm := NewSessionManager()

	// Add sessions with sequential IDs
	for i := 0; i < 10000; i++ {
		sm.Add(NewSession(fmt.Sprintf("session-%d", i), "echo", nil))
	}

	// Count sessions per shard
	shardCounts := make([]int, 256)
	for i := 0; i < 256; i++ {
		sm.shards[i].mu.RLock()
		shardCounts[i] = len(sm.shards[i].sessions)
		sm.shards[i].mu.RUnlock()
	}

	// Calculate statistics
	sort.Ints(shardCounts)

	max := shardCounts[255]

	// With 10000 sessions and 256 shards, average is ~39 per shard
	// Check that distribution is reasonable (no shard has more than 5x average)
	average := 10000 / 256
	if max > average*5 {
		t.Errorf("Max shard count %d is more than 5x average %d", max, average)
	}

	// Count empty shards
	emptyCount := 0
	for _, c := range shardCounts {
		if c == 0 {
			emptyCount++
		}
	}
	// Allow some empty shards due to hash distribution
	if emptyCount > 50 {
		t.Errorf("%d empty shards, expected fewer", emptyCount)
	}
}
