package sdlc

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Defaults a CommandTokenSourceConfig's zero durations take.
const (
	DefaultForgeTokenRefreshBefore = 5 * time.Minute
	DefaultForgeTokenTimeout       = 30 * time.Second
)

// Environment variables a forge token command receives on top of the
// daemon's own environment.
const (
	ForgeTokenEnvType    = "QUICBAAS_FORGE_TYPE"     //nolint:gosec // G101: an environment variable name, not a credential
	ForgeTokenEnvBaseURL = "QUICBAAS_FORGE_BASE_URL" //nolint:gosec // G101: an environment variable name, not a credential
)

// ForgeTokenSource yields the token that authenticates calls to the forge at
// baseURL. A source decides how the token is obtained and how long it is
// reused.
type ForgeTokenSource interface {
	ForgeToken(ctx context.Context, baseURL string) (string, error)
}

// ForgeTokenRunner runs argv with env added to the daemon's environment and
// returns the command's stdout. It never returns stderr.
type ForgeTokenRunner func(ctx context.Context, argv, env []string) ([]byte, error)

// CommandTokenSourceConfig configures a CommandTokenSource.
type CommandTokenSourceConfig struct {
	// Argv is the command and its words. Argv[0] must be an absolute path;
	// no shell runs it. Required.
	Argv []string

	// Env is added to the daemon's environment, as "KEY=value" entries.
	// ForgeTokenEnvBaseURL is added per call.
	Env []string

	// RefreshBefore is how long before expires_at the token is refreshed.
	// Zero takes DefaultForgeTokenRefreshBefore.
	RefreshBefore time.Duration

	// Timeout bounds one run of the command. Zero takes
	// DefaultForgeTokenTimeout.
	Timeout time.Duration

	// Runner runs the command. Nil runs it with exec.CommandContext.
	Runner ForgeTokenRunner

	// Clock reads the time. Nil is time.Now.
	Clock func() time.Time
}

// CommandTokenSource is a ForgeTokenSource that runs an operator-configured
// command, caches the token it prints per base URL, and runs the command
// again once the token is within RefreshBefore of its expiry. Concurrent
// callers that find the token due share one run.
type CommandTokenSource struct {
	argv          []string
	env           []string
	refreshBefore time.Duration
	timeout       time.Duration
	runner        ForgeTokenRunner
	clock         func() time.Time

	// entries maps a base URL to its *commandTokenEntry.
	entries sync.Map
}

// commandTokenEntry is one base URL's cached token and its refresh slot.
type commandTokenEntry struct {
	cached atomic.Pointer[cachedForgeToken]

	// refresh holds one value while a caller runs the command.
	refresh chan struct{}
}

type cachedForgeToken struct {
	token     string
	refreshAt time.Time
}

// forgeTokenOutput is the JSON object the command prints on stdout.
type forgeTokenOutput struct {
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
}

// ForgeTokenCommandConfigError reports a token command configuration that
// cannot be used.
type ForgeTokenCommandConfigError struct {
	Field  string
	Reason string
}

func (e *ForgeTokenCommandConfigError) Error() string {
	return "forge token command: " + e.Field + " " + e.Reason
}

// ForgeTokenCommandError reports a token command that failed or timed out.
// It names the command and its exit code, never its output.
type ForgeTokenCommandError struct {
	Command  string
	ExitCode int
	TimedOut bool
	Err      error
}

func (e *ForgeTokenCommandError) Error() string {
	if e.TimedOut {
		return "forge token command " + e.Command + " timed out"
	}
	if e.ExitCode < 0 {
		return "forge token command " + e.Command + " could not run"
	}
	return "forge token command " + e.Command + " exited with code " + strconv.Itoa(e.ExitCode)
}

// Unwrap returns the runner's error. Callers that print it risk the output
// the runner saw; the Error text above never includes it.
func (e *ForgeTokenCommandError) Unwrap() error { return e.Err }

// ForgeTokenOutputError reports token command output that cannot be used. It
// says what was wrong, never what was printed.
type ForgeTokenOutputError struct {
	Command string
	Reason  string
}

func (e *ForgeTokenOutputError) Error() string {
	return "forge token command " + e.Command + " printed unusable output: " + e.Reason
}

// NewCommandTokenSource validates cfg and builds the source.
func NewCommandTokenSource(cfg CommandTokenSourceConfig) (*CommandTokenSource, error) {
	if len(cfg.Argv) == 0 || cfg.Argv[0] == "" {
		return nil, &ForgeTokenCommandConfigError{Field: "argv", Reason: "must name a command"}
	}
	if !filepath.IsAbs(cfg.Argv[0]) {
		return nil, &ForgeTokenCommandConfigError{Field: "argv", Reason: "must start with an absolute path"}
	}
	if cfg.RefreshBefore < 0 {
		return nil, &ForgeTokenCommandConfigError{Field: "refresh_before", Reason: "must not be negative"}
	}
	if cfg.Timeout < 0 {
		return nil, &ForgeTokenCommandConfigError{Field: "timeout", Reason: "must not be negative"}
	}
	s := &CommandTokenSource{
		argv:          append([]string(nil), cfg.Argv...),
		env:           append([]string(nil), cfg.Env...),
		refreshBefore: cfg.RefreshBefore,
		timeout:       cfg.Timeout,
		runner:        cfg.Runner,
		clock:         cfg.Clock,
	}
	if s.refreshBefore == 0 {
		s.refreshBefore = DefaultForgeTokenRefreshBefore
	}
	if s.timeout == 0 {
		s.timeout = DefaultForgeTokenTimeout
	}
	if s.runner == nil {
		s.runner = runForgeTokenCommand
	}
	if s.clock == nil {
		s.clock = time.Now
	}
	return s, nil
}

// ForgeToken returns the cached token for baseURL while it is fresh, and
// otherwise runs the command. Only one caller per base URL runs it; the
// others wait for that run or for ctx.
func (s *CommandTokenSource) ForgeToken(ctx context.Context, baseURL string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	entry := s.entry(baseURL)
	if token, ok := entry.fresh(s.clock()); ok {
		return token, nil
	}
	select {
	case entry.refresh <- struct{}{}:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { <-entry.refresh }()

	// The caller that held the slot before this one may have refreshed.
	if token, ok := entry.fresh(s.clock()); ok {
		return token, nil
	}
	cached, err := s.run(ctx, baseURL)
	if err != nil {
		return "", err
	}
	entry.cached.Store(cached)
	return cached.token, nil
}

func (s *CommandTokenSource) entry(baseURL string) *commandTokenEntry {
	if found, ok := s.entries.Load(baseURL); ok {
		if e, ok := found.(*commandTokenEntry); ok {
			return e
		}
	}
	fresh := &commandTokenEntry{refresh: make(chan struct{}, 1)}
	found, _ := s.entries.LoadOrStore(baseURL, fresh)
	e, ok := found.(*commandTokenEntry)
	if !ok {
		s.entries.Store(baseURL, fresh)
		return fresh
	}
	return e
}

func (e *commandTokenEntry) fresh(now time.Time) (string, bool) {
	cached := e.cached.Load()
	if cached == nil || !now.Before(cached.refreshAt) {
		return "", false
	}
	return cached.token, true
}

// run runs the command once and parses what it printed.
func (s *CommandTokenSource) run(ctx context.Context, baseURL string) (*cachedForgeToken, error) {
	runCtx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	env := append(append([]string(nil), s.env...), ForgeTokenEnvBaseURL+"="+baseURL)
	stdout, err := s.runner(runCtx, s.argv, env)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		cmdErr := &ForgeTokenCommandError{Command: s.argv[0], ExitCode: -1, Err: err}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			cmdErr.ExitCode = exitErr.ExitCode()
		}
		if runCtx.Err() != nil {
			cmdErr.TimedOut = true
		}
		return nil, cmdErr
	}
	return s.parse(stdout)
}

func (s *CommandTokenSource) parse(stdout []byte) (*cachedForgeToken, error) {
	var out forgeTokenOutput
	if err := json.Unmarshal(stdout, &out); err != nil {
		return nil, s.outputError("stdout is not one JSON object")
	}
	if out.Token == "" {
		return nil, s.outputError("token is missing or empty")
	}
	if out.ExpiresAt == "" {
		return nil, s.outputError("expires_at is missing")
	}
	expiresAt, err := time.Parse(time.RFC3339, out.ExpiresAt)
	if err != nil {
		return nil, s.outputError("expires_at is not an RFC 3339 time")
	}
	refreshAt := expiresAt.Add(-s.refreshBefore)
	if !s.clock().Before(refreshAt) {
		return nil, s.outputError("expires_at is already within refresh_before of now")
	}
	return &cachedForgeToken{token: out.Token, refreshAt: refreshAt}, nil
}

func (s *CommandTokenSource) outputError(reason string) error {
	return &ForgeTokenOutputError{Command: s.argv[0], Reason: reason}
}

// runForgeTokenCommand is the default ForgeTokenRunner: argv runs without a
// shell, stdout is captured and stderr is discarded, so neither reaches an
// error value.
func runForgeTokenCommand(ctx context.Context, argv, env []string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Env = append(os.Environ(), env...)
	cmd.Stderr = io.Discard
	return cmd.Output()
}
