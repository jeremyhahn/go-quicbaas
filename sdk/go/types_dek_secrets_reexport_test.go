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

package quicbaas

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/dek"
	"github.com/jeremyhahn/go-quicbaas/sdk/go/client/secrets"
)

// These tests hold the root package to its promise: a consumer reaches every
// DEK and secrets operation, and names every request and response type,
// through sdk/go alone.

var errRecordingTransport = errors.New("recording transport: fixed error")

// recordingTransport stores the service and method of each Request call and
// answers every call with errRecordingTransport.
type recordingTransport struct {
	mu    sync.Mutex
	calls []string
}

func (r *recordingTransport) Request(_ context.Context, svc, method string, _ interface{}) (interface{}, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, svc+"/"+method)
	return nil, errRecordingTransport
}

func (r *recordingTransport) RequestStream(context.Context, string, string, interface{}) (Stream, error) {
	return nil, errRecordingTransport
}

func (r *recordingTransport) Conn() interface{} { return nil }

func (r *recordingTransport) Close() error { return nil }

func (r *recordingTransport) Healthy() bool { return true }

// last returns the most recent recorded call and how many calls were made.
func (r *recordingTransport) last() (string, int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return "", 0
	}
	return r.calls[len(r.calls)-1], len(r.calls)
}

// runOperations calls each operation, checks it returns the transport's
// error after exactly one Request, and checks every recorded service/method
// is non-empty and distinct.
func runOperations(t *testing.T, rt *recordingTransport, ops []struct {
	name string
	call func() error
}) {
	t.Helper()
	seen := make(map[string]string, len(ops))
	for i, op := range ops {
		err := op.call()
		if !errors.Is(err, errRecordingTransport) {
			t.Errorf("%s: err = %v, want the transport's error", op.name, err)
			continue
		}
		got, n := rt.last()
		if n != i+1 {
			t.Errorf("%s: transport saw %d calls, want %d", op.name, n, i+1)
			continue
		}
		if got == "/" || got == "" || got[0] == '/' || got[len(got)-1] == '/' {
			t.Errorf("%s: recorded service/method %q has an empty part", op.name, got)
		}
		if prev, dup := seen[got]; dup {
			t.Errorf("%s: recorded %q, already used by %s", op.name, got, prev)
		}
		seen[got] = op.name
	}
}

func TestDEKSecretsReexport_TypeIdentity(t *testing.T) {
	// DEK types.
	var _ *dek.SetDefaultDEKResponse = (*SetDefaultDEKResponse)(nil)
	var _ *dek.SetDEKSafetyRequest = (*SetDEKSafetyRequest)(nil)
	var _ *dek.StartReencryptionRequest = (*StartReencryptionRequest)(nil)
	var _ *dek.StartReencryptionResponse = (*StartReencryptionResponse)(nil)
	var _ *dek.GetReencryptionProgressRequest = (*GetReencryptionProgressRequest)(nil)
	var _ *dek.ReencryptionProgressResponse = (*ReencryptionProgressResponse)(nil)
	var _ *dek.ReencryptionProgress = (*ReencryptionProgress)(nil)

	// Secrets types.
	var _ *secrets.EnsureSecretRequest = (*EnsureSecretRequest)(nil)
	var _ *secrets.EnsureSecretResponse = (*EnsureSecretResponse)(nil)
	var status secrets.SecretStatus = SecretStatus("")
	_ = status

	// Constructors return the package clients.
	var _ func(Transport) dek.Client = NewDEKClient
	var _ func(Transport) secrets.Client = NewSecretsClient

	for _, c := range []struct {
		name      string
		got, want secrets.SecretStatus
	}{
		{"SecretStatusEnabled", SecretStatusEnabled, secrets.SecretStatusEnabled},
		{"SecretStatusDisabled", SecretStatusDisabled, secrets.SecretStatusDisabled},
		{"SecretStatusDeleted", SecretStatusDeleted, secrets.SecretStatusDeleted},
	} {
		if c.got != c.want {
			t.Errorf("%s = %q, client package says %q", c.name, c.got, c.want)
		}
	}
}

func TestNewDEKClient_NilTransport(t *testing.T) {
	if c := NewDEKClient(nil); c != nil {
		t.Fatalf("NewDEKClient(nil) = %v, want nil", c)
	}
}

func TestNewSecretsClient_NilTransport(t *testing.T) {
	if c := NewSecretsClient(nil); c != nil {
		t.Fatalf("NewSecretsClient(nil) = %v, want nil", c)
	}
}

func TestNewDEKClient_EveryOperation(t *testing.T) {
	rt := &recordingTransport{}
	var c DEKClient = NewDEKClient(rt)
	if c == nil {
		t.Fatal("NewDEKClient returned nil for a non-nil transport")
	}
	ctx := context.Background()
	const path = "tenants/acme/dek"

	ops := []struct {
		name string
		call func() error
	}{
		{"CreateDEK", func() error {
			_, err := c.CreateDEK(ctx, &CreateDEKSDKRequest{Path: path, Algorithm: "AES-256-GCM"})
			return err
		}},
		{"GetDEK", func() error { _, err := c.GetDEK(ctx, path); return err }},
		{"ListDEKs", func() error {
			_, err := c.ListDEKs(ctx, &ListDEKsSDKRequest{Prefix: "tenants/"})
			return err
		}},
		{"RotateDEK", func() error {
			_, err := c.RotateDEK(ctx, &RotateDEKSDKRequest{Path: path})
			return err
		}},
		{"DeleteDEK", func() error {
			_, err := c.DeleteDEK(ctx, &DeleteDEKSDKRequest{Path: path, Force: true})
			return err
		}},
		{"GetDEKStatus", func() error {
			var resp *DEKStatusResponse
			resp, err := c.GetDEKStatus(ctx, path)
			_ = resp
			return err
		}},
		{"SetDefaultDEK", func() error {
			var resp *SetDefaultDEKResponse
			resp, err := c.SetDefaultDEK(ctx, path)
			_ = resp
			return err
		}},
		{"SetDEKSafety", func() error {
			return c.SetDEKSafety(ctx, &SetDEKSafetyRequest{Path: path, MaxOperations: 10, MaxBytes: 1024, WarningPercent: 80})
		}},
		{"ClearDEKSafety", func() error { return c.ClearDEKSafety(ctx, path) }},
		{"StartReencryption", func() error {
			var resp *StartReencryptionResponse
			resp, err := c.StartReencryption(ctx, &StartReencryptionRequest{Path: path, FromVersion: 1, ToVersion: 2, BatchSize: 10})
			_ = resp
			return err
		}},
		{"GetReencryptionProgress", func() error {
			var resp *ReencryptionProgressResponse
			resp, err := c.GetReencryptionProgress(ctx, &GetReencryptionProgressRequest{Path: path, OperationID: "op-1"})
			_ = resp
			return err
		}},
	}
	runOperations(t, rt, ops)
}

func TestNewSecretsClient_EveryOperation(t *testing.T) {
	rt := &recordingTransport{}
	var c SecretsClient = NewSecretsClient(rt)
	if c == nil {
		t.Fatal("NewSecretsClient returned nil for a non-nil transport")
	}
	ctx := context.Background()
	const path = "app/db"

	ops := []struct {
		name string
		call func() error
	}{
		{"Store", func() error {
			_, err := c.Store(ctx, &StoreSecretRequest{Path: path, Data: map[string]interface{}{"password": "x"}})
			return err
		}},
		{"Ensure", func() error {
			var resp *EnsureSecretResponse
			resp, err := c.Ensure(ctx, &EnsureSecretRequest{Path: path, Length: 32, Encoding: "base64"})
			_ = resp
			return err
		}},
		{"Get", func() error { _, err := c.Get(ctx, path, WithSecretVersion(1)); return err }},
		{"GetWithBytes", func() error {
			return c.GetWithBytes(ctx, path+"/bytes", "password", func([]byte) error { return nil })
		}},
		{"List", func() error { _, err := c.List(ctx, "app/"); return err }},
		{"Delete", func() error { return c.Delete(ctx, path) }},
		{"GetVersion", func() error {
			_, err := c.GetVersion(ctx, &GetSecretVersionRequest{SecretID: path, Version: 1})
			return err
		}},
		{"ListVersions", func() error { _, err := c.ListVersions(ctx, path); return err }},
		{"Rotate", func() error {
			_, err := c.Rotate(ctx, &RotateSecretRequest{SecretID: path, NewData: []byte("new")})
			return err
		}},
		{"Rollback", func() error {
			_, err := c.Rollback(ctx, &RollbackSecretRequest{SecretID: path, TargetVersion: 1})
			return err
		}},
		{"UpdateStatus", func() error {
			return c.UpdateStatus(ctx, &UpdateSecretStatusRequest{SecretID: path, Status: SecretStatusDisabled})
		}},
		{"SetRotationPolicy", func() error {
			return c.SetRotationPolicy(ctx, &SetSecretRotationPolicyRequest{
				SecretID: path,
				Policy:   &SecretRotationPolicy{Enabled: true, Interval: time.Hour, RetainVersions: 3},
			})
		}},
		{"GetRotationPolicy", func() error { _, err := c.GetRotationPolicy(ctx, path); return err }},
	}
	runOperations(t, rt, ops)
}
