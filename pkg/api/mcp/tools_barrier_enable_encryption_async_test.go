//go:build mcp

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

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	qrdbsdk "github.com/jeremyhahn/go-qrdb/sdk/go"
)

// blockingEnableEncryptionService holds EnableEncryption open until the test
// releases it, so the test can cancel the tool call while the migration runs.
type blockingEnableEncryptionService struct {
	*fakeBarrierService
	started chan string
	release chan struct{}
	done    chan error
	secret  chan []byte
}

func newBlockingEnableEncryptionService() *blockingEnableEncryptionService {
	return &blockingEnableEncryptionService{
		fakeBarrierService: &fakeBarrierService{},
		started:            make(chan string, 1),
		release:            make(chan struct{}),
		done:               make(chan error, 1),
		secret:             make(chan []byte, 1),
	}
}

func (s *blockingEnableEncryptionService) EnableEncryption(ctx context.Context, tenantID string, opts qrdbsdk.MigrationOptions) error {
	s.started <- tenantID
	select {
	case <-s.release:
	case <-ctx.Done():
		s.done <- ctx.Err()
		return ctx.Err()
	}
	var got []byte
	if opts.Credentials.Secret != nil {
		if plain, err := opts.Credentials.Secret.Open(); err == nil {
			defer plain.Destroy()
			if b, openErr := plain.Open(); openErr == nil {
				got = append([]byte(nil), b...)
			}
		}
	}
	s.secret <- got
	s.done <- nil
	return nil
}

// TestBarrierEnableEncryption_MigrationOutlivesCanceledRequest pins that the
// enable-encryption wrappers answer migration_started and keep the migration
// (and its credential) alive after the tool call's context is cancelled, as
// go-qrdb's native qrdb_barrier_enable_encryption does.
func TestBarrierEnableEncryption_MigrationOutlivesCanceledRequest(t *testing.T) {
	cases := []struct {
		tool      string
		args      map[string]interface{}
		wantScope string
		wantID    string
	}{
		{
			tool:      "qrdb_barrier_enable_encryption",
			args:      map[string]interface{}{"strategy": "passphrase", "passphrase": "hunter2"},
			wantScope: "system",
		},
		{
			tool:      "qrdb_tenant_barrier_enable_encryption",
			args:      map[string]interface{}{"tenant_id": "t1", "strategy": "passphrase", "passphrase": "hunter2"},
			wantScope: "tenant",
			wantID:    "t1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.tool, func(t *testing.T) {
			svc := newBlockingEnableEncryptionService()
			r := newBarrierTestRegistry(t, svc)

			type result struct {
				out interface{}
				err error
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			resCh := make(chan result, 1)
			go func() {
				out, err := r.ExecuteTool(ctx, tc.tool, tc.args)
				resCh <- result{out: out, err: err}
			}()

			select {
			case got := <-svc.started:
				require.Equal(t, tc.wantID, got)
			case <-time.After(5 * time.Second):
				t.Fatal("migration did not start")
			}
			cancel()

			var res result
			select {
			case res = <-resCh:
			case <-time.After(5 * time.Second):
				t.Fatal("tool call did not return")
			}
			require.NoError(t, res.err)
			m, ok := res.out.(map[string]interface{})
			require.True(t, ok, "result is %T", res.out)
			require.Equal(t, "migration_started", m["status"])
			require.Equal(t, tc.wantScope, m["scope"])

			close(svc.release)
			select {
			case err := <-svc.done:
				require.NoError(t, err, "migration saw the request's cancellation")
			case <-time.After(5 * time.Second):
				t.Fatal("migration did not finish")
			}
			select {
			case b := <-svc.secret:
				require.Equal(t, []byte("hunter2"), b, "credential destroyed under the running migration")
			case <-time.After(5 * time.Second):
				t.Fatal("migration did not report its secret")
			}
		})
	}
}
