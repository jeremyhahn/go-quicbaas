//go:build rest

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

package rest

import (
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLimitListener_EnforcesMaxConnections(t *testing.T) {
	const maxConns = 3

	inner, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer inner.Close()

	ll := newLimitListener(inner, maxConns)
	addr := ll.Addr().String()

	// Track peak concurrent connections seen by the server side
	var active int64
	var peak int64

	// Accept connections in background
	accepted := make(chan net.Conn, 10)
	go func() {
		for {
			conn, err := ll.Accept()
			if err != nil {
				return
			}
			cur := atomic.AddInt64(&active, 1)
			for {
				old := atomic.LoadInt64(&peak)
				if cur <= old || atomic.CompareAndSwapInt64(&peak, old, cur) {
					break
				}
			}
			accepted <- conn
		}
	}()

	// Open maxConns connections - these should all succeed
	clients := make([]net.Conn, 0, maxConns)
	for i := 0; i < maxConns; i++ {
		c, err := net.DialTimeout("tcp", addr, 2*time.Second)
		require.NoError(t, err)
		clients = append(clients, c)
	}

	// Give the acceptor time to process all connections
	time.Sleep(50 * time.Millisecond)

	// Drain accepted channel to count server-side connections
	serverConns := make([]net.Conn, 0, maxConns)
	for i := 0; i < maxConns; i++ {
		select {
		case sc := <-accepted:
			serverConns = append(serverConns, sc)
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for accepted connection %d", i)
		}
	}

	assert.Equal(t, int64(maxConns), atomic.LoadInt64(&active))

	// Try to open one more connection. The dial itself succeeds at the TCP
	// level (SYN queue), but Accept on the limitListener should block because
	// the semaphore is full. Verify no new server-side connection appears.
	extra, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer extra.Close()

	// Wait briefly - no new accept should happen
	select {
	case <-accepted:
		t.Fatal("accepted a connection beyond the limit before any were closed")
	case <-time.After(100 * time.Millisecond):
		// Expected: no new accept
	}

	// Close one server-side connection to free a semaphore slot
	serverConns[0].Close()
	atomic.AddInt64(&active, -1)

	// Now the extra connection should be accepted
	select {
	case sc := <-accepted:
		sc.Close()
		atomic.AddInt64(&active, -1)
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for blocked connection to be accepted after freeing a slot")
	}

	// The peak should never have exceeded maxConns
	assert.LessOrEqual(t, atomic.LoadInt64(&peak), int64(maxConns))

	// Cleanup
	for _, c := range clients {
		c.Close()
	}
	for _, sc := range serverConns[1:] {
		sc.Close()
	}
	ll.Close()
}

func TestLimitListener_AcceptErrorReleasesSemaphore(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ll := newLimitListener(inner, 1)

	// Close the inner listener so Accept returns an error
	inner.Close()

	_, err = ll.Accept()
	assert.Error(t, err)

	// The semaphore slot should have been released. Verify by checking
	// that the semaphore channel is empty (non-blocking send succeeds).
	select {
	case ll.sem <- struct{}{}:
		// Slot was released correctly - drain it back
		<-ll.sem
	default:
		t.Fatal("semaphore slot was not released after Accept error")
	}
}

func TestLimitConn_CloseReleasesSemaphore(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer inner.Close()

	ll := newLimitListener(inner, 1)
	addr := ll.Addr().String()

	// Accept in background
	var serverConn net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		var acceptErr error
		serverConn, acceptErr = ll.Accept()
		if acceptErr != nil {
			return
		}
	}()

	client, err := net.DialTimeout("tcp", addr, 2*time.Second)
	require.NoError(t, err)
	defer client.Close()

	<-done
	require.NotNil(t, serverConn)

	// Semaphore should be full (1/1)
	select {
	case ll.sem <- struct{}{}:
		<-ll.sem // drain it back
		t.Fatal("semaphore should be full while connection is active")
	default:
		// Expected: semaphore full
	}

	// Close the server-side connection (limitConn)
	serverConn.Close()

	// Semaphore should now be empty (0/1)
	select {
	case ll.sem <- struct{}{}:
		<-ll.sem
	default:
		t.Fatal("semaphore was not released after closing limitConn")
	}

	ll.Close()
}

func TestLimitListener_ConcurrentAcceptAndClose(t *testing.T) {
	const maxConns = 5

	inner, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ll := newLimitListener(inner, maxConns)
	addr := ll.Addr().String()

	// Accept loop: immediately close each accepted connection to
	// free the semaphore slot, preventing deadlocks.
	acceptDone := make(chan struct{})
	var accepted int64
	go func() {
		defer close(acceptDone)
		for {
			conn, acceptErr := ll.Accept()
			if acceptErr != nil {
				return
			}
			atomic.AddInt64(&accepted, 1)
			conn.Close()
		}
	}()

	// Rapidly open connections concurrently.
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c, dialErr := net.DialTimeout("tcp", addr, 2*time.Second)
			if dialErr != nil {
				return
			}
			c.Close()
		}()
	}

	wg.Wait()

	ll.Close()
	<-acceptDone

	assert.Greater(t, atomic.LoadInt64(&accepted), int64(0), "should have accepted at least one connection")
}

func TestNewLimitListener_Fields(t *testing.T) {
	inner, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer inner.Close()

	ll := newLimitListener(inner, 10)
	assert.Equal(t, cap(ll.sem), 10)
	assert.Equal(t, inner.Addr(), ll.Addr())
}
