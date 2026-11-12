package sdlc

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const (
	testForgeToken     = "tok-forge-source-value"
	testStderrMarker   = "stderr-marker-text"
	testForgeTokenArgv = "/usr/local/bin/forge-mint"
	testForgeBaseURL   = "https://api.github.com"
)

var testTokenEpoch = time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)

// fakeTokenClock is a settable clock.
type fakeTokenClock struct{ now atomic.Pointer[time.Time] }

func newFakeTokenClock() *fakeTokenClock {
	c := &fakeTokenClock{}
	c.set(testTokenEpoch)
	return c
}

func (c *fakeTokenClock) set(t time.Time) { c.now.Store(&t) }
func (c *fakeTokenClock) Now() time.Time  { return *c.now.Load() }

// tokenOutput is the stdout a well-behaved command prints.
func tokenOutput(token string, expiresAt time.Time) []byte {
	return []byte(`{"token":"` + token + `","expires_at":"` + expiresAt.Format(time.RFC3339) + `"}`)
}

// countingRunner returns stdout and counts its calls.
type countingRunner struct {
	calls  atomic.Int32
	stdout []byte
	err    error
	env    atomic.Pointer[[]string]
}

func (r *countingRunner) run(_ context.Context, _, env []string) ([]byte, error) {
	r.calls.Add(1)
	r.env.Store(&env)
	return r.stdout, r.err
}

func newTestTokenSource(t *testing.T, runner ForgeTokenRunner, clock *fakeTokenClock) *CommandTokenSource {
	t.Helper()
	s, err := NewCommandTokenSource(CommandTokenSourceConfig{
		Argv:   []string{testForgeTokenArgv, "--app", "pilot"},
		Env:    []string{ForgeTokenEnvType + "=github"},
		Runner: runner,
		Clock:  clock.Now,
	})
	if err != nil {
		t.Fatalf("NewCommandTokenSource: %v", err)
	}
	return s
}

// assertNoLeak fails when err's text carries the token or the stderr marker.
func assertNoLeak(t *testing.T, err error) {
	t.Helper()
	if strings.Contains(err.Error(), testForgeToken) {
		t.Fatalf("error text carries the token: %q", err.Error())
	}
	if strings.Contains(err.Error(), testStderrMarker) {
		t.Fatalf("error text carries stderr: %q", err.Error())
	}
}

func TestCommandTokenSource_FirstCallRunsCommandWithEnv(t *testing.T) {
	clock := newFakeTokenClock()
	runner := &countingRunner{stdout: tokenOutput(testForgeToken, testTokenEpoch.Add(time.Hour))}
	s := newTestTokenSource(t, runner.run, clock)

	got, err := s.ForgeToken(context.Background(), testForgeBaseURL)
	if err != nil {
		t.Fatalf("ForgeToken: %v", err)
	}
	if got != testForgeToken {
		t.Fatal("ForgeToken returned a different token than the command printed")
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("runner calls = %d, want 1", runner.calls.Load())
	}
	env := *runner.env.Load()
	want := []string{ForgeTokenEnvType + "=github", ForgeTokenEnvBaseURL + "=" + testForgeBaseURL}
	if strings.Join(env, "\n") != strings.Join(want, "\n") {
		t.Fatalf("env = %v, want %v", env, want)
	}
}

func TestCommandTokenSource_ReusesTokenUntilRefreshPoint(t *testing.T) {
	clock := newFakeTokenClock()
	runner := &countingRunner{stdout: tokenOutput(testForgeToken, testTokenEpoch.Add(time.Hour))}
	s := newTestTokenSource(t, runner.run, clock)
	ctx := context.Background()

	if _, err := s.ForgeToken(ctx, testForgeBaseURL); err != nil {
		t.Fatalf("first ForgeToken: %v", err)
	}
	clock.set(testTokenEpoch.Add(54 * time.Minute))
	if _, err := s.ForgeToken(ctx, testForgeBaseURL); err != nil {
		t.Fatalf("second ForgeToken: %v", err)
	}
	if runner.calls.Load() != 1 {
		t.Fatalf("runner calls before the refresh point = %d, want 1", runner.calls.Load())
	}

	// At expires_at - refresh_before the next caller runs the command.
	runner.stdout = tokenOutput(testForgeToken, testTokenEpoch.Add(2*time.Hour))
	clock.set(testTokenEpoch.Add(55 * time.Minute))
	if _, err := s.ForgeToken(ctx, testForgeBaseURL); err != nil {
		t.Fatalf("third ForgeToken: %v", err)
	}
	if runner.calls.Load() != 2 {
		t.Fatalf("runner calls after the refresh point = %d, want 2", runner.calls.Load())
	}
}

func TestCommandTokenSource_CachesPerBaseURL(t *testing.T) {
	clock := newFakeTokenClock()
	runner := &countingRunner{stdout: tokenOutput(testForgeToken, testTokenEpoch.Add(time.Hour))}
	s := newTestTokenSource(t, runner.run, clock)
	ctx := context.Background()

	for _, baseURL := range []string{testForgeBaseURL, "https://github.example.test/api/v3", testForgeBaseURL} {
		if _, err := s.ForgeToken(ctx, baseURL); err != nil {
			t.Fatalf("ForgeToken(%s): %v", baseURL, err)
		}
	}
	if runner.calls.Load() != 2 {
		t.Fatalf("runner calls = %d, want one per base URL (2)", runner.calls.Load())
	}
}

func TestCommandTokenSource_ConcurrentCallersShareOneRun(t *testing.T) {
	clock := newFakeTokenClock()
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	runner := func(context.Context, []string, []string) ([]byte, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return tokenOutput(testForgeToken, testTokenEpoch.Add(time.Hour)), nil
	}
	s := newTestTokenSource(t, runner, clock)

	const callers = 20
	var wg sync.WaitGroup
	errs := make(chan error, callers)
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			token, err := s.ForgeToken(context.Background(), testForgeBaseURL)
			if err == nil && token != testForgeToken {
				err = errors.New("a caller got a different token")
			}
			errs <- err
		}()
	}
	<-started
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("ForgeToken: %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("runner calls = %d, want 1", calls.Load())
	}
}

func TestCommandTokenSource_FailingRunnerIsTypedAndRetried(t *testing.T) {
	clock := newFakeTokenClock()
	runner := &countingRunner{err: errors.New(testForgeToken + " " + testStderrMarker)}
	s := newTestTokenSource(t, runner.run, clock)
	ctx := context.Background()

	_, err := s.ForgeToken(ctx, testForgeBaseURL)
	var cmdErr *ForgeTokenCommandError
	if !errors.As(err, &cmdErr) {
		t.Fatalf("err = %v, want *ForgeTokenCommandError", err)
	}
	if cmdErr.Command != testForgeTokenArgv || cmdErr.ExitCode != -1 || cmdErr.TimedOut {
		t.Fatalf("command error = %+v, want argv[0], exit -1, not timed out", cmdErr)
	}
	if !strings.Contains(err.Error(), testForgeTokenArgv) {
		t.Fatalf("error %q does not name argv[0]", err.Error())
	}
	assertNoLeak(t, err)

	runner.err = nil
	runner.stdout = tokenOutput(testForgeToken, testTokenEpoch.Add(time.Hour))
	if _, err := s.ForgeToken(ctx, testForgeBaseURL); err != nil {
		t.Fatalf("ForgeToken after a failure: %v", err)
	}
	if runner.calls.Load() != 2 {
		t.Fatalf("runner calls = %d, want 2 (the failure is not cached)", runner.calls.Load())
	}
}

func TestCommandTokenSource_TimeoutIsTyped(t *testing.T) {
	runner := func(ctx context.Context, _, _ []string) ([]byte, error) {
		<-ctx.Done()
		return nil, errors.New(testStderrMarker)
	}
	s, err := NewCommandTokenSource(CommandTokenSourceConfig{
		Argv:    []string{testForgeTokenArgv},
		Timeout: time.Millisecond,
		Runner:  runner,
	})
	if err != nil {
		t.Fatalf("NewCommandTokenSource: %v", err)
	}

	_, err = s.ForgeToken(context.Background(), testForgeBaseURL)
	var cmdErr *ForgeTokenCommandError
	if !errors.As(err, &cmdErr) || !cmdErr.TimedOut {
		t.Fatalf("err = %v, want a timed-out *ForgeTokenCommandError", err)
	}
	assertNoLeak(t, err)
}

func TestCommandTokenSource_UnusableOutputIsRefused(t *testing.T) {
	cases := []struct {
		name   string
		stdout string
	}{
		{"malformed JSON", `{"token":"` + testForgeToken + `", ` + testStderrMarker},
		{"two objects", `{"token":"` + testForgeToken + `"} {}`},
		{"empty token", `{"token":"","expires_at":"2026-11-07T13:00:00Z"}`},
		{"missing token", `{"expires_at":"2026-11-07T13:00:00Z"}`},
		{"missing expires_at", `{"token":"` + testForgeToken + `"}`},
		{"expires_at not RFC 3339", `{"token":"` + testForgeToken + `","expires_at":"tomorrow ` + testForgeToken + `"}`},
		{"already expiring", `{"token":"` + testForgeToken + `","expires_at":"2026-11-07T12:04:00Z"}`},
		{"already expired", `{"token":"` + testForgeToken + `","expires_at":"2026-11-07T11:00:00Z"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			runner := &countingRunner{stdout: []byte(tc.stdout)}
			s := newTestTokenSource(t, runner.run, newFakeTokenClock())

			_, err := s.ForgeToken(context.Background(), testForgeBaseURL)
			var outErr *ForgeTokenOutputError
			if !errors.As(err, &outErr) {
				t.Fatalf("err = %v, want *ForgeTokenOutputError", err)
			}
			if outErr.Command != testForgeTokenArgv {
				t.Fatalf("output error names %q, want argv[0]", outErr.Command)
			}
			assertNoLeak(t, err)
		})
	}
}

func TestCommandTokenSource_CancelledContext(t *testing.T) {
	runner := &countingRunner{stdout: tokenOutput(testForgeToken, testTokenEpoch.Add(time.Hour))}
	s := newTestTokenSource(t, runner.run, newFakeTokenClock())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := s.ForgeToken(ctx, testForgeBaseURL); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if runner.calls.Load() != 0 {
		t.Fatalf("runner calls = %d, want 0", runner.calls.Load())
	}
}

func TestCommandTokenSource_CancelledWhileWaitingForRefresh(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	runner := func(context.Context, []string, []string) ([]byte, error) {
		close(started)
		<-release
		return tokenOutput(testForgeToken, testTokenEpoch.Add(time.Hour)), nil
	}
	s := newTestTokenSource(t, runner, newFakeTokenClock())

	done := make(chan error, 1)
	go func() {
		_, err := s.ForgeToken(context.Background(), testForgeBaseURL)
		done <- err
	}()
	<-started

	ctx, cancel := context.WithCancel(context.Background())
	waiting := make(chan error, 1)
	go func() {
		_, err := s.ForgeToken(ctx, testForgeBaseURL)
		waiting <- err
	}()
	cancel()
	if err := <-waiting; !errors.Is(err, context.Canceled) {
		t.Fatalf("waiting caller err = %v, want context.Canceled", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("refreshing caller: %v", err)
	}
}

func TestCommandTokenSource_RunnerCancelledReturnsCtxErr(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	runner := func(runCtx context.Context, _, _ []string) ([]byte, error) {
		cancel()
		<-runCtx.Done()
		return nil, errors.New(testStderrMarker)
	}
	s := newTestTokenSource(t, runner, newFakeTokenClock())

	if _, err := s.ForgeToken(ctx, testForgeBaseURL); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

// TestRunForgeTokenCommand_TimeoutBoundsInheritedPipes checks that the
// command's timeout also bounds the wait for its output pipes when a
// descendant that inherited stdout outlives the killed command.
func TestRunForgeTokenCommand_TimeoutBoundsInheritedPipes(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := runForgeTokenCommand(ctx, []string{"/bin/sh", "-c", "sleep 30 & sleep 30"}, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("runForgeTokenCommand returned no error, want a timeout error")
	}
	if elapsed > 5*time.Second {
		t.Fatalf("runForgeTokenCommand took %v, want under 5s", elapsed)
	}
}

func TestNewCommandTokenSource_Defaults(t *testing.T) {
	s, err := NewCommandTokenSource(CommandTokenSourceConfig{Argv: []string{testForgeTokenArgv}})
	if err != nil {
		t.Fatalf("NewCommandTokenSource: %v", err)
	}
	if s.refreshBefore != DefaultForgeTokenRefreshBefore || s.timeout != DefaultForgeTokenTimeout {
		t.Fatalf("defaults = %v/%v, want %v/%v", s.refreshBefore, s.timeout,
			DefaultForgeTokenRefreshBefore, DefaultForgeTokenTimeout)
	}
	if s.runner == nil || s.clock == nil {
		t.Fatal("runner and clock must default")
	}
}

func TestNewCommandTokenSource_RefusesBadConfig(t *testing.T) {
	cases := []struct {
		name  string
		cfg   CommandTokenSourceConfig
		field string
	}{
		{"empty argv", CommandTokenSourceConfig{}, "argv"},
		{"empty argv[0]", CommandTokenSourceConfig{Argv: []string{""}}, "argv"},
		{"relative argv[0]", CommandTokenSourceConfig{Argv: []string{"bin/forge-mint"}}, "argv"},
		{"negative refresh_before", CommandTokenSourceConfig{Argv: []string{testForgeTokenArgv}, RefreshBefore: -time.Second}, "refresh_before"},
		{"negative timeout", CommandTokenSourceConfig{Argv: []string{testForgeTokenArgv}, Timeout: -time.Second}, "timeout"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewCommandTokenSource(tc.cfg)
			var cfgErr *ForgeTokenCommandConfigError
			if !errors.As(err, &cfgErr) {
				t.Fatalf("err = %v, want *ForgeTokenCommandConfigError", err)
			}
			if cfgErr.Field != tc.field {
				t.Fatalf("field = %q, want %q", cfgErr.Field, tc.field)
			}
		})
	}
}
