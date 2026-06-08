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
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/pkg/orchestration"
)

// -----------------------------------------------------------------------------
// writeAtomic / writeCredentials error paths
// -----------------------------------------------------------------------------

// TestWriteAtomic_TempCreateError covers the temp-create branch of writeAtomic:
// when the target directory does not exist, os.CreateTemp fails and the call
// returns a typed PersistSessionIOError with Op="temp-create".
func TestWriteAtomic_TempCreateError(t *testing.T) {
	t.Parallel()
	// Directory component does not exist, so CreateTemp(dir, ...) fails.
	path := filepath.Join(t.TempDir(), "missing-subdir", "session.json")

	err := writeAtomic(path, []byte(`{"x":1}`))
	if err == nil {
		t.Fatal("expected error when temp dir does not exist")
	}
	var ioErr *PersistSessionIOError
	if !errors.As(err, &ioErr) {
		t.Fatalf("want *PersistSessionIOError, got %T: %v", err, err)
	}
	if ioErr.Op != "temp-create" {
		t.Fatalf("want Op=temp-create, got %q", ioErr.Op)
	}
}

// TestWriteCredentials_MkdirError covers the MkdirAll error branch of
// writeCredentials: when a regular file occupies the path where a parent
// directory must be created, MkdirAll fails and a typed
// EnrollmentCredentialsSaveError is returned.
func TestWriteCredentials_MkdirError(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	// Create a regular file, then attempt to write credentials *under* it so
	// MkdirAll must create a directory whose parent is a file -> ENOTDIR.
	fileAsDir := filepath.Join(base, "not-a-dir")
	if err := os.WriteFile(fileAsDir, []byte("x"), 0o600); err != nil {
		t.Fatalf("seed file: %v", err)
	}
	path := filepath.Join(fileAsDir, "nested", "credentials.json")

	err := writeCredentials(path, &persistedCredentials{ClientID: "c"})
	if err == nil {
		t.Fatal("expected error when parent path is a file")
	}
	var saveErr *EnrollmentCredentialsSaveError
	if !errors.As(err, &saveErr) {
		t.Fatalf("want *EnrollmentCredentialsSaveError, got %T: %v", err, err)
	}
}

// -----------------------------------------------------------------------------
// RefreshTokenFn error / default branches
// -----------------------------------------------------------------------------

// TestRefreshTokenFn_ReadCredentialsError covers the branch where the
// credentials file exists but is corrupt: readCredentials returns an error
// that RefreshTokenFn surfaces unchanged (not ErrEnrollmentMissingCredentials).
func TestRefreshTokenFn_ReadCredentialsError(t *testing.T) {
	t.Parallel()
	cfg := newEnrollerConfig(t, "https://unused.test")
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// Write a corrupt credentials file so readCredentials fails to unmarshal.
	if err := os.WriteFile(credentialsPath(cfg), []byte("{not-json"), 0o600); err != nil {
		t.Fatalf("seed corrupt creds: %v", err)
	}

	enroller := newProdEnroller(t, nil)
	_, _, err := enroller.RefreshTokenFn(cfg)(context.Background())
	if err == nil {
		t.Fatal("expected error from corrupt credentials")
	}
	if errors.Is(err, ErrEnrollmentMissingCredentials) {
		t.Fatalf("corrupt file should surface a read error, not missing-credentials: %v", err)
	}
	var readErr *EnrollmentCredentialsReadError
	if !errors.As(err, &readErr) {
		t.Fatalf("want *EnrollmentCredentialsReadError, got %T: %v", err, err)
	}
}

// TestRefreshTokenFn_ZeroExpiresInDefaults covers the ttl<=0 branch where the
// control plane returns expires_in=0 and the refresher applies the 5-minute
// default expiry instead of an immediately-expired token.
func TestRefreshTokenFn_ZeroExpiresInDefaults(t *testing.T) {
	t.Parallel()
	stub := &serverStub{
		t: t,
		tokenBody: mustJSON(t, map[string]any{
			"access_token": "jwt-no-expiry",
			"token_type":   "Bearer",
			"expires_in":   0,
		}),
	}
	srv := httptest.NewTLSServer(stub.handler())
	defer srv.Close()

	cfg := newEnrollerConfig(t, srv.URL)
	if err := os.MkdirAll(cfg.WorkDir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	creds := &persistedCredentials{
		ClientID:         "c",
		ClientSecret:     "s",
		AgentID:          "a",
		ServiceAccountID: "sa",
		Role:             "agent-runtime",
		Scopes: []string{
			orchestration.AgentScopeRuntimeSpawn,
			orchestration.AgentScopeRuntimePrompt,
			orchestration.AgentScopeRuntimeStatus,
		},
	}
	if err := writeCredentials(credentialsPath(cfg), creds); err != nil {
		t.Fatalf("seed creds: %v", err)
	}

	enroller := newProdEnroller(t, srv.Client())
	before := time.Now()
	tok, exp, err := enroller.RefreshTokenFn(cfg)(context.Background())
	if err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if tok != "jwt-no-expiry" {
		t.Fatalf("tok=%q", tok)
	}
	// With the 5-minute default applied, the expiry must be comfortably in the
	// future rather than at/near now.
	if !exp.After(before.Add(time.Minute)) {
		t.Fatalf("expected default 5m expiry well in the future, got %v (before=%v)", exp, before)
	}
}

// -----------------------------------------------------------------------------
// handleNotFound: re-register succeeds but the retry POST fails under budget
// -----------------------------------------------------------------------------

// TestHandleNotFound_RetryFailsUnderBudget covers the branch where the
// Reregisterer succeeds, the token is invalidated, but the immediate retry POST
// fails for a reason that is still under the failure budget. The error is
// returned to the caller (not yet a ReregisterFailedError).
func TestHandleNotFound_RetryFailsUnderBudget(t *testing.T) {
	t.Parallel()

	var calls atomic.Int32
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := calls.Add(1)
		if n == 1 {
			// Initial heartbeat -> 404 triggers re-register.
			w.WriteHeader(http.StatusNotFound)
			return
		}
		// Post-re-register retry -> 500 (a generic failure, under budget).
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	rereg := &staticReregisterer{errs: []error{nil}}
	tokens := &staticToken{tokens: []string{"tok-1", "tok-2"}}
	sender, err := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr:   srv.URL,
		TenantID:     "tenant-a",
		HTTPClient:   tlsClientFromServer(srv),
		Tokens:       tokens,
		Logger:       &testLogger{t: t},
		Reregisterer: rereg,
	})
	if err != nil {
		t.Fatalf("NewHTTPHeartbeatSender: %v", err)
	}

	err = sender.SendHeartbeat(context.Background(), captureIdentity("agent-1", "tenant-a"), HeartbeatStatus{})
	if err == nil {
		t.Fatal("expected retry POST failure to surface")
	}
	// Under budget: must NOT be the terminal ReregisterFailedError yet.
	var failed *ReregisterFailedError
	if errors.As(err, &failed) {
		t.Fatalf("first retry failure should be under budget, got terminal error: %v", err)
	}
	var se *HeartbeatSendError
	if !errors.As(err, &se) || se.Status != http.StatusInternalServerError {
		t.Fatalf("want HeartbeatSendError status=500, got %v", err)
	}
	if rereg.calls.Load() != 1 {
		t.Fatalf("expected exactly one re-register, got %d", rereg.calls.Load())
	}
	if atomic.LoadInt32(&tokens.invalidated) != 1 {
		t.Fatalf("expected token invalidate after re-register, got %d", tokens.invalidated)
	}
}

// TestBuildEndpoint_ParseError covers buildEndpoint's url.Parse failure branch:
// a server address containing a control character cannot be parsed, so the
// sender surfaces a typed HeartbeatSendError instead of building an endpoint.
func TestBuildEndpoint_ParseError(t *testing.T) {
	t.Parallel()
	sender, err := NewHTTPHeartbeatSender(HTTPHeartbeatConfig{
		ServerAddr: "http://\x7f-control-char", // ASCII DEL is invalid in a URL
		TenantID:   "tenant-a",
		HTTPClient: http.DefaultClient,
		Tokens:     &staticToken{tokens: []string{"tok"}},
		Logger:     &testLogger{t: t},
	})
	if err != nil {
		t.Fatalf("NewHTTPHeartbeatSender: %v", err)
	}

	_, bErr := sender.buildEndpoint("agent-1")
	if bErr == nil {
		t.Fatal("expected parse error for control-char server addr")
	}
	var se *HeartbeatSendError
	if !errors.As(bErr, &se) || se.Op != "parse control plane addr" {
		t.Fatalf("want HeartbeatSendError(parse control plane addr), got %v", bErr)
	}
}

// TestWriteAtomic_RenameError covers the rename failure branch of writeAtomic:
// when the destination path is an existing directory, os.Rename(file, dir)
// fails and a typed PersistSessionIOError with Op="rename" is returned.
func TestWriteAtomic_RenameError(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	// Make the target path a directory; renaming a temp file onto it must fail.
	dirAsTarget := filepath.Join(base, "session.json")
	if err := os.Mkdir(dirAsTarget, 0o700); err != nil {
		t.Fatalf("mkdir target: %v", err)
	}

	err := writeAtomic(dirAsTarget, []byte(`{"x":1}`))
	if err == nil {
		t.Fatal("expected rename error when target is a directory")
	}
	var ioErr *PersistSessionIOError
	if !errors.As(err, &ioErr) {
		t.Fatalf("want *PersistSessionIOError, got %T: %v", err, err)
	}
	if ioErr.Op != "rename" {
		t.Fatalf("want Op=rename, got %q", ioErr.Op)
	}
	// The temp file must have been cleaned up on the error path.
	entries, _ := os.ReadDir(base)
	for _, e := range entries {
		if e.Name() != "session.json" {
			t.Fatalf("temp file not cleaned up: %s", e.Name())
		}
	}
}
