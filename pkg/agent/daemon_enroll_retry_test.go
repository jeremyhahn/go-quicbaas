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

package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// scriptedEnroller fails with transientErr for the first failures calls
// and then succeeds, so a test can assert the daemon rode out a control
// plane that was not listening yet.
type scriptedEnroller struct {
	failures int
	err      error
	identity *orchestration.AgentIdentity
	calls    atomic.Int32

	// onCall, when set, runs after the call is counted and before the
	// result is returned. Tests use it to observe attempt boundaries.
	onCall func(attempt int)
}

func (s *scriptedEnroller) Enroll(ctx context.Context, cfg *Config) (*orchestration.AgentIdentity, error) {
	attempt := int(s.calls.Add(1))
	if s.onCall != nil {
		s.onCall(attempt)
	}
	if attempt <= s.failures {
		return nil, s.err
	}
	return s.identity, nil
}

// signalLogger reports the daemon's INFO lines to a channel so a test can
// synchronize on the retry-wait announcement without polling or sleeping.
type signalLogger struct {
	*testLogger
	mu    sync.Mutex
	lines []string
	match string
	fired chan struct{}
	once  sync.Once
}

func newSignalLogger(t *testing.T, match string) *signalLogger {
	return &signalLogger{
		testLogger: &testLogger{t: t},
		match:      match,
		fired:      make(chan struct{}),
	}
}

func (l *signalLogger) Infof(format string, args ...interface{}) {
	l.testLogger.Infof(format, args...)
	line := fmt.Sprintf(format, args...)
	l.mu.Lock()
	l.lines = append(l.lines, line)
	l.mu.Unlock()
	if l.match != "" && strings.Contains(line, l.match) {
		l.once.Do(func() { close(l.fired) })
	}
}

func (l *signalLogger) count(substr string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, line := range l.lines {
		if strings.Contains(line, substr) {
			n++
		}
	}
	return n
}

// fastRetryConfig returns a daemon config whose enrollment backoff is
// small enough for a unit test but still exercises the real timer path.
func fastRetryConfig(t *testing.T) *Config {
	t.Helper()
	cfg := newTestConfig(t)
	cfg.EnrollRetryBase = 2 * time.Millisecond
	cfg.EnrollRetryMax = 4 * time.Millisecond
	return cfg
}

// TestDaemon_Start_RetriesTransientEnrollFailure is the regression for
// issue #643: a control plane that is not listening yet must be waited
// out, not turned into an exit(1) restart loop.
func TestDaemon_Start_RetriesTransientEnrollFailure(t *testing.T) {
	t.Parallel()
	cfg := fastRetryConfig(t)
	logger := newSignalLogger(t, "")
	enroller := &scriptedEnroller{
		failures: 3,
		err:      &EnrollmentTokenExchangeError{Op: "post token request", Cause: dialRefused()},
		identity: &orchestration.AgentIdentity{AgentID: "a", TenantID: cfg.TenantID},
	}
	server := &fakeSpawnServer{addr: "127.0.0.1:0"}
	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    enroller,
		SpawnServer: server,
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := enroller.calls.Load(); got != 4 {
		t.Errorf("enroll calls: got %d, want 4", got)
	}
	if got := d.State(); got != "running" {
		t.Errorf("state: got %q, want %q", got, "running")
	}
	if !server.started.Load() {
		t.Error("spawn server should have started after the retried enrollment succeeded")
	}
	// Every wait must be announced so an operator reading the journal can
	// tell a waiting daemon from a wedged one.
	if got := logger.count("retrying in"); got != 3 {
		t.Errorf("retry-wait log lines: got %d, want 3", got)
	}
	if err := d.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

// TestDaemon_Start_TerminalEnrollFailureDoesNotRetry asserts a rejected
// identity still exits immediately: retrying a misconfiguration would
// hide it behind an indefinite wait.
func TestDaemon_Start_TerminalEnrollFailureDoesNotRetry(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "invalid_client",
			err:  &EnrollmentTokenExchangeError{Op: "post token request", Status: 401, Detail: `{"error":"invalid_client"}`},
			want: "invalid_client",
		},
		{
			name: "forbidden",
			err:  &EnrollmentRejectedError{Status: 403, Detail: "tenant not permitted"},
			want: "rejected",
		},
		{
			name: "tls",
			err:  &EnrollmentTLSError{Op: "load agent keypair", Cause: errors.New("no such file")},
			want: "tls",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := fastRetryConfig(t)
			// A base this long guarantees the assertion below fails on
			// timeout rather than passing by accident if a retry happens.
			cfg.EnrollRetryBase = time.Hour
			cfg.EnrollRetryMax = time.Hour
			enroller := &scriptedEnroller{failures: 1 << 30, err: tc.err}
			server := &fakeSpawnServer{}
			d, err := NewDaemon(cfg, DaemonDeps{
				Runtime:     newTestRuntime(t),
				Enroller:    enroller,
				SpawnServer: server,
				Logger:      &testLogger{t: t},
			})
			if err != nil {
				t.Fatalf("NewDaemon: %v", err)
			}
			startErr := startWithin(t, d, context.Background(), 10*time.Second)
			if startErr == nil {
				t.Fatal("Start must fail on a terminal enrollment error")
			}
			var terminal *EnrollmentTerminalError
			if !errors.As(startErr, &terminal) {
				t.Fatalf("Start err: got %T (%v), want *EnrollmentTerminalError", startErr, startErr)
			}
			if !strings.Contains(terminal.Reason, tc.want) {
				t.Errorf("reason %q does not name the class (%q)", terminal.Reason, tc.want)
			}
			if !errors.Is(startErr, tc.err) {
				t.Errorf("Start err must wrap the underlying cause %v", tc.err)
			}
			if got := enroller.calls.Load(); got != 1 {
				t.Errorf("enroll calls: got %d, want 1 (terminal errors are not retried)", got)
			}
			if got := d.State(); got != "failed" {
				t.Errorf("state: got %q, want %q", got, "failed")
			}
			if server.started.Load() {
				t.Error("spawn server must not start after a terminal enrollment failure")
			}
		})
	}
}

// TestDaemon_Start_EnrollRetryStopsOnContextCancel asserts a shutdown
// signal interrupts a pending backoff wait promptly instead of leaving
// the daemon parked until the timer expires.
func TestDaemon_Start_EnrollRetryStopsOnContextCancel(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	// An hour-long first wait: only cancellation can end this test in time.
	cfg.EnrollRetryBase = time.Hour
	cfg.EnrollRetryMax = time.Hour

	logger := newSignalLogger(t, "retrying in")
	enroller := &scriptedEnroller{
		failures: 1 << 30,
		err:      &EnrollmentRejectedError{Cause: dialRefused()},
	}
	server := &fakeSpawnServer{}
	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    enroller,
		SpawnServer: server,
		Logger:      logger,
	})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- d.Start(ctx) }()

	// Cancel only once the daemon has announced that it is waiting, so the
	// cancellation lands inside the backoff timer rather than before it.
	select {
	case <-logger.fired:
	case <-time.After(10 * time.Second):
		t.Fatal("daemon never announced a retry wait")
	}
	cancel()

	select {
	case startErr := <-errCh:
		if startErr == nil {
			t.Fatal("Start must fail when the context is cancelled mid-wait")
		}
		var abandoned *EnrollmentAbandonedError
		if !errors.As(startErr, &abandoned) {
			t.Fatalf("Start err: got %T (%v), want *EnrollmentAbandonedError", startErr, startErr)
		}
		if abandoned.Attempts < 1 {
			t.Errorf("attempts: got %d, want >=1", abandoned.Attempts)
		}
		if !errors.Is(startErr, context.Canceled) {
			t.Errorf("Start err must unwrap to context.Canceled, got %v", startErr)
		}
		if got := d.State(); got != "failed" {
			t.Errorf("state: got %q, want %q", got, "failed")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Start did not return promptly after context cancellation")
	}
}

// TestDaemon_Start_EnrollRetryHonoursBackoffFloor asserts the daemon
// actually waits between attempts. Without a wait the loop degenerates
// into the tight spin this issue is about.
func TestDaemon_Start_EnrollRetryHonoursBackoffFloor(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	cfg.EnrollRetryBase = 40 * time.Millisecond
	cfg.EnrollRetryMax = 40 * time.Millisecond
	enroller := &scriptedEnroller{
		failures: 2,
		err:      &EnrollmentRejectedError{Status: 503},
		identity: &orchestration.AgentIdentity{AgentID: "a", TenantID: cfg.TenantID},
	}
	d, err := NewDaemon(cfg, DaemonDeps{
		Runtime:     newTestRuntime(t),
		Enroller:    enroller,
		SpawnServer: &fakeSpawnServer{addr: "127.0.0.1:0"},
		Logger:      &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewDaemon: %v", err)
	}
	start := time.Now()
	if err := d.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	elapsed := time.Since(start)
	// Equal jitter guarantees at least half the window per wait, so two
	// waits cannot complete in under 40ms.
	if elapsed < 40*time.Millisecond {
		t.Errorf("elapsed %s: two backoff waits must take at least 40ms", elapsed)
	}
	if err := d.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
}

// startWithin runs Start on a goroutine and fails the test if it does not
// return inside the budget. It keeps a mis-classified terminal error from
// hanging the suite for an hour.
func startWithin(t *testing.T, d *Daemon, ctx context.Context, budget time.Duration) error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() { errCh <- d.Start(ctx) }()
	select {
	case err := <-errCh:
		return err
	case <-time.After(budget):
		t.Fatalf("Start did not return within %s", budget)
		return nil
	}
}

func TestConfig_EffectiveEnrollRetry(t *testing.T) {
	t.Parallel()
	cfg := &Config{}
	if got := cfg.EffectiveEnrollRetryBase(); got != defaultEnrollRetryBase {
		t.Errorf("default base: got %s, want %s", got, defaultEnrollRetryBase)
	}
	if got := cfg.EffectiveEnrollRetryMax(); got != defaultEnrollRetryMax {
		t.Errorf("default max: got %s, want %s", got, defaultEnrollRetryMax)
	}

	cfg.EnrollRetryBase = 250 * time.Millisecond
	cfg.EnrollRetryMax = 5 * time.Second
	if got := cfg.EffectiveEnrollRetryBase(); got != 250*time.Millisecond {
		t.Errorf("base: got %s", got)
	}
	if got := cfg.EffectiveEnrollRetryMax(); got != 5*time.Second {
		t.Errorf("max: got %s", got)
	}

	// A cap below the base would make the schedule shrink; the base wins.
	cfg.EnrollRetryBase = 10 * time.Second
	cfg.EnrollRetryMax = time.Second
	if got := cfg.EffectiveEnrollRetryMax(); got != 10*time.Second {
		t.Errorf("max below base: got %s, want the base %s", got, 10*time.Second)
	}
}

func TestConfig_Validate_EnrollRetry(t *testing.T) {
	t.Parallel()
	cfg := newTestConfig(t)
	cfg.EnrollRetryBase = -time.Second
	if err := cfg.Validate(); !errors.Is(err, ErrConfigInvalidRetryWindow) {
		t.Errorf("negative base: got %v, want ErrConfigInvalidRetryWindow", err)
	}
	cfg.EnrollRetryBase = time.Second
	cfg.EnrollRetryMax = -time.Second
	if err := cfg.Validate(); !errors.Is(err, ErrConfigInvalidRetryWindow) {
		t.Errorf("negative max: got %v, want ErrConfigInvalidRetryWindow", err)
	}
	cfg.EnrollRetryMax = time.Minute
	if err := cfg.Validate(); err != nil {
		t.Errorf("valid retry window rejected: %v", err)
	}
}
