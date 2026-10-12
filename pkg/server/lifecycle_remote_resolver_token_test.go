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

package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

// testTokenWait bounds how long a test waits for a result that must arrive
// promptly. It is a failure guard, not a delay: no test waits on it to pass.
const testTokenWait = 10 * time.Second

// tokenEndpoint is a stub central /oauth2/token endpoint that counts its
// exchanges and, when wedged, holds each one until release closes.
type tokenEndpoint struct {
	hits    atomic.Int32
	entered chan struct{}
	release chan struct{}
	status  int
}

func newTokenEndpoint(wedged bool) *tokenEndpoint {
	e := &tokenEndpoint{entered: make(chan struct{}, 1), release: make(chan struct{}), status: http.StatusOK}
	if !wedged {
		close(e.release)
	}
	return e
}

func (e *tokenEndpoint) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.hits.Add(1)
	select {
	case e.entered <- struct{}{}:
	default:
	}
	select {
	case <-e.release:
	case <-r.Context().Done():
		return
	}
	if e.status != http.StatusOK {
		w.WriteHeader(e.status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if _, err := w.Write([]byte(`{"access_token":"fresh","token_type":"Bearer","expires_in":3600}`)); err != nil {
		return
	}
}

// newEndpointTokenSource starts e behind an httptest server and returns a
// token source for it. A wedged endpoint is released before the server closes.
func newEndpointTokenSource(t *testing.T, e *tokenEndpoint) *centralOAuth2TokenSource {
	t.Helper()
	srv := httptest.NewServer(e)
	t.Cleanup(srv.Close)
	t.Cleanup(func() {
		select {
		case <-e.release:
		default:
			close(e.release)
		}
	})
	ts := newCentralOAuth2TokenSource(&OrchestratorDaemonClient{ClientID: "c", ClientSecret: "s"}, srv.URL, "")
	ts.loopback = false
	return ts
}

func awaitEntered(t *testing.T, e *tokenEndpoint) {
	t.Helper()
	select {
	case <-e.entered:
	case <-time.After(testTokenWait):
		t.Fatal("the token exchange never reached the endpoint")
	}
}

type tokenResult struct {
	tok *oauth2.Token
	err error
}

func TestCentralOAuth2TokenSource_CallerLeavesWedgedEndpoint(t *testing.T) {
	cases := map[string]func() (context.Context, context.CancelFunc){
		"cancelled": func() (context.Context, context.CancelFunc) {
			return context.WithCancel(context.Background())
		},
		"past deadline": func() (context.Context, context.CancelFunc) {
			return context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
		},
	}
	for name, newCtx := range cases {
		t.Run(name, func(t *testing.T) {
			e := newTokenEndpoint(true)
			ts := newEndpointTokenSource(t, e)
			ctx, cancel := newCtx()
			defer cancel()

			done := make(chan tokenResult, 1)
			go func() {
				tok, err := ts.Token(ctx)
				done <- tokenResult{tok, err}
			}()
			awaitEntered(t, e)
			cancel()

			select {
			case r := <-done:
				if r.tok != nil || !errors.Is(r.err, ctx.Err()) {
					t.Fatalf("Token = (%v, %v), want the caller's %v", r.tok, r.err, ctx.Err())
				}
			case <-time.After(testTokenWait):
				t.Fatal("Token did not return when its caller's context ended")
			}
		})
	}
}

// The exchange a departed caller started still lands, and serves the next
// caller from the cache.
func TestCentralOAuth2TokenSource_ExchangeOutlivesDepartedCaller(t *testing.T) {
	e := newTokenEndpoint(true)
	ts := newEndpointTokenSource(t, e)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan tokenResult, 1)
	go func() {
		tok, err := ts.Token(ctx)
		done <- tokenResult{tok, err}
	}()
	awaitEntered(t, e)
	cancel()
	<-done

	waiting := make(chan tokenResult, 1)
	go func() {
		tok, err := ts.Token(context.Background())
		waiting <- tokenResult{tok, err}
	}()
	close(e.release)
	select {
	case r := <-waiting:
		if r.err != nil || r.tok.AccessToken != "fresh" {
			t.Fatalf("Token = (%v, %v), want the fresh token", r.tok, r.err)
		}
	case <-time.After(testTokenWait):
		t.Fatal("Token never returned the exchanged token")
	}
	if got := e.hits.Load(); got != 1 {
		t.Errorf("token exchanges = %d, want 1", got)
	}
}

func TestCentralOAuth2TokenSource_ReusesValidCachedToken(t *testing.T) {
	e := newTokenEndpoint(false)
	ts := newEndpointTokenSource(t, e)
	ts.cached = &oauth2.Token{AccessToken: "cached", Expiry: time.Now().Add(time.Hour)}

	tok, err := ts.Token(context.Background())
	if err != nil || tok.AccessToken != "cached" {
		t.Fatalf("Token = (%v, %v), want the cached token", tok, err)
	}
	if got := e.hits.Load(); got != 0 {
		t.Errorf("token exchanges = %d, want 0", got)
	}
}

func TestCentralOAuth2TokenSource_ExpiredTokenRefreshedOnceForConcurrentCallers(t *testing.T) {
	e := newTokenEndpoint(true)
	ts := newEndpointTokenSource(t, e)
	ts.cached = &oauth2.Token{AccessToken: "stale", Expiry: time.Now().Add(-time.Minute)}

	const callers = 8
	results := make(chan tokenResult, callers)
	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tok, err := ts.Token(context.Background())
			results <- tokenResult{tok, err}
		}()
	}
	awaitEntered(t, e)
	close(e.release)
	wg.Wait()
	close(results)

	for r := range results {
		if r.err != nil || r.tok.AccessToken != "fresh" {
			t.Errorf("Token = (%v, %v), want the fresh token", r.tok, r.err)
		}
	}
	if got := e.hits.Load(); got != 1 {
		t.Errorf("token exchanges = %d, want 1", got)
	}
}

// A failed exchange reaches every caller and is not cached: the next caller
// starts a new exchange.
func TestCentralOAuth2TokenSource_FailedExchangeIsRetried(t *testing.T) {
	e := newTokenEndpoint(false)
	e.status = http.StatusInternalServerError
	ts := newEndpointTokenSource(t, e)

	for range 2 {
		if _, err := ts.Token(context.Background()); err == nil {
			t.Fatal("Token succeeded against a failing endpoint")
		}
	}
	if got := e.hits.Load(); got != 2 {
		t.Errorf("token exchanges = %d, want 2", got)
	}
}
