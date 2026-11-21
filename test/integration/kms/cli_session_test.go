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

//go:build integration && quic

package kms

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errInjectedLogin = errors.New("injected login failure")

// okLogin is a login that always succeeds.
func okLogin(context.Context) (string, time.Time, error) {
	return "test-bearer", time.Now().Add(time.Hour), nil
}

// newSessionSetupTestContext builds a context whose CLI binary does not
// exist, so a run that gets past session setup fails as a command.
func newSessionSetupTestContext(t *testing.T, login func(context.Context) (string, time.Time, error)) *cliTestContext {
	return &cliTestContext{
		t:         t,
		binary:    filepath.Join(t.TempDir(), "no-such-cli"),
		protocol:  "grpc",
		address:   "127.0.0.1:1",
		timeout:   5 * time.Second,
		outputFmt: "json",
		home:      t.TempDir(),
		login:     login,
	}
}

// TestCLISessionSetupErrorLogin checks that a login failure wraps errSessionSetup.
func TestCLISessionSetupErrorLogin(t *testing.T) {
	c := newSessionSetupTestContext(t, func(context.Context) (string, time.Time, error) {
		return "", time.Time{}, errInjectedLogin
	})
	_, err := c.run("kms", "key", "list")
	require.ErrorIs(t, err, errSessionSetup)
	require.ErrorIs(t, err, errInjectedLogin)
}

// TestCLISessionSetupErrorTokenSave checks that a token-save failure wraps errSessionSetup.
func TestCLISessionSetupErrorTokenSave(t *testing.T) {
	c := newSessionSetupTestContext(t, okLogin)
	// A regular file as $HOME: SaveToken cannot create $HOME/.quicbaas.
	c.home = filepath.Join(t.TempDir(), "home-is-a-file")
	require.NoError(t, os.WriteFile(c.home, nil, 0o600))
	_, err := c.run("kms", "key", "list")
	require.ErrorIs(t, err, errSessionSetup)
	require.ErrorContains(t, err, "persist the CLI bearer")
}

// TestCLISessionSetupErrorCommandFailure checks that a command failure does not wrap errSessionSetup.
func TestCLISessionSetupErrorCommandFailure(t *testing.T) {
	c := newSessionSetupTestContext(t, okLogin)
	_, err := c.run("kms", "key", "list")
	require.Error(t, err)
	require.NotErrorIs(t, err, errSessionSetup)
}

// sessionSetupChildEnv selects the mode of TestCLISessionSetupChildRunExpectError
// as "<setup>:<call>"; unset, that test skips.
const sessionSetupChildEnv = "QUICBAAS_TEST_SESSION_SETUP_CHILD"

// TestCLISessionSetupChildRunExpectError runs only as a child process of the
// TestCLISessionSetupError*Login/TokenSave/Accepted tests: it calls the CLI
// from a real subtest, then runs a sibling subtest.
func TestCLISessionSetupChildRunExpectError(t *testing.T) {
	mode := os.Getenv(sessionSetupChildEnv)
	if mode == "" {
		t.Skip("runs only as a child process of the session setup tests")
	}
	setup, call, _ := strings.Cut(mode, ":")

	login := okLogin
	if setup == "login" {
		login = func(context.Context) (string, time.Time, error) {
			return "", time.Time{}, errInjectedLogin
		}
	}
	c := newSessionSetupTestContext(t, login)
	if setup == "token-save" {
		c.home = filepath.Join(t.TempDir(), "home-is-a-file")
		require.NoError(t, os.WriteFile(c.home, nil, 0o600))
	}

	t.Run("call", func(t *testing.T) {
		switch call {
		case "expect-error":
			_, err := c.runExpectError("kms", "key", "list")
			assert.Error(t, err)
		case "accept-any-error":
			_, err := c.run("kms", "key", "list")
			failOnSessionSetup(t, err)
			assert.Error(t, err)
		default:
			t.Fatalf("unknown call %q", call)
		}
	})
	t.Run("sibling", func(t *testing.T) {
		t.Log("sibling ran")
	})
}

// sessionSetupChildEnviron returns the environment for a child run in mode:
// without the embedded-server switch, so the child never boots a server.
func sessionSetupChildEnviron(mode string) []string {
	env := []string{sessionSetupChildEnv + "=" + mode}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "QUICBAAS_EMBEDDED_ENABLED=") || strings.HasPrefix(kv, sessionSetupChildEnv+"=") {
			continue
		}
		env = append(env, kv)
	}
	return env
}

// runSessionSetupChild re-runs this test binary on the child test in mode and
// returns its output and whether it passed.
func runSessionSetupChild(t *testing.T, mode string) (string, bool) {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run", "^TestCLISessionSetupChildRunExpectError$", "-test.v", "-test.count=1")
	cmd.Env = sessionSetupChildEnviron(mode)
	out, err := cmd.CombinedOutput()
	if err != nil {
		var exitErr *exec.ExitError
		require.ErrorAs(t, err, &exitErr, "child test binary did not run:\n%s", out)
	}
	return string(out), err == nil
}

// requireSetupFailureOnParent checks a child run in which runExpectError met a
// session setup failure: the run fails on the parent, the subtest and its
// sibling pass, and the output names the cause.
func requireSetupFailureOnParent(t *testing.T, mode, cause string) {
	t.Helper()
	out, passed := runSessionSetupChild(t, mode)
	require.False(t, passed, "a session setup failure in runExpectError must fail the run:\n%s", out)
	require.Contains(t, out, "--- FAIL: TestCLISessionSetupChildRunExpectError (")
	require.Contains(t, out, "--- PASS: TestCLISessionSetupChildRunExpectError/call (")
	require.Contains(t, out, "--- PASS: TestCLISessionSetupChildRunExpectError/sibling (")
	require.Contains(t, out, errSessionSetup.Error())
	require.Contains(t, out, cause)
}

// TestCLISessionSetupErrorRunExpectErrorLogin checks that a login failure in runExpectError fails the run.
func TestCLISessionSetupErrorRunExpectErrorLogin(t *testing.T) {
	requireSetupFailureOnParent(t, "login:expect-error", errInjectedLogin.Error())
}

// TestCLISessionSetupErrorRunExpectErrorTokenSave checks that a token-save failure in runExpectError fails the run.
func TestCLISessionSetupErrorRunExpectErrorTokenSave(t *testing.T) {
	requireSetupFailureOnParent(t, "token-save:expect-error", "persist the CLI bearer")
}

// TestCLISessionSetupErrorAcceptAnyErrorLogin checks that a subtest accepting any run error fails on a login failure.
func TestCLISessionSetupErrorAcceptAnyErrorLogin(t *testing.T) {
	out, passed := runSessionSetupChild(t, "login:accept-any-error")
	require.False(t, passed, "a login failure must fail the subtest that accepts any error:\n%s", out)
	require.Contains(t, out, "--- FAIL: TestCLISessionSetupChildRunExpectError/call (")
	require.Contains(t, out, "--- PASS: TestCLISessionSetupChildRunExpectError/sibling (")
}

// TestCLISessionSetupErrorCommandFailureAccepted checks that a command failure stays an accepted rejection.
func TestCLISessionSetupErrorCommandFailureAccepted(t *testing.T) {
	for _, mode := range []string{"none:expect-error", "none:accept-any-error"} {
		out, passed := runSessionSetupChild(t, mode)
		require.True(t, passed, "mode %s: a command failure must pass as the expected rejection:\n%s", mode, out)
	}
}
