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
	"path/filepath"
	"testing"
	"time"

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
